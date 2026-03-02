package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeCh chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeCh:          make(chan bool, 1),
	}
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closeCh
}

func TestHandleMultiChannelProxy_CompositeChainContinuesBeyondActiveChannelCap(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var responsesCalls int32
	responsesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&responsesCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"responses primary failed"}}`)
	}))
	defer responsesServer.Close()

	var messagesCalls int32
	messagesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&messagesCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"msg_test_1",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"ok from messages failover"}],
			"stop_reason":"end_turn",
			"model":"claude-3-haiku-20240307"
		}`)
	}))
	defer messagesServer.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
		Failover: config.FailoverConfig{
			Rules: []config.FailoverRule{
				{
					ErrorCodes: "500",
					ActionChain: []config.ActionStep{
						{Action: config.ActionFailover},
					},
				},
			},
		},
	}

	cfg.Upstream = append(cfg.Upstream,
		config.UpstreamConfig{
			ID:          "msg-disabled",
			Name:        "Messages Disabled Target",
			BaseURL:     messagesServer.URL,
			ServiceType: "claude",
			Status:      "disabled",
			APIKeys:     []string{"msg-key"},
		},
		config.UpstreamConfig{
			ID:          "cmp-main",
			Name:        "Composite Main",
			ServiceType: "composite",
			Status:      "active",
			Priority:    0,
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetPool:      config.CompositeTargetPoolResponses,
					TargetChannelID: "resp-primary",
					FailoverTargets: []config.CompositeTargetRef{
						{
							Pool:      config.CompositeTargetPoolMessages,
							ChannelID: "msg-disabled",
						},
					},
				},
			},
		},
	)
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-primary",
		Name:        "Responses Primary",
		BaseURL:     responsesServer.URL,
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)
	failoverTracker := config.NewFailoverTracker()

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleMultiChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		"user-1",
		"",
		nil,
		time.Now(),
		nil,
		"",
		nil,
		nil,
		nil,
		failoverTracker,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	if got := atomic.LoadInt32(&responsesCalls); got != 1 {
		t.Fatalf("expected responses primary to be called once, got %d", got)
	}
	if got := atomic.LoadInt32(&messagesCalls); got != 1 {
		t.Fatalf("expected messages failover target to be called once, got %d", got)
	}

	var claudeResp types.ClaudeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &claudeResp); err != nil {
		t.Fatalf("expected valid Claude response JSON, got err=%v body=%s", err, rec.Body.String())
	}
	if len(claudeResp.Content) == 0 || claudeResp.Content[0].Text != "ok from messages failover" {
		t.Fatalf("unexpected Claude response content: %+v", claudeResp.Content)
	}
}

func TestHandleMultiChannelProxy_CompositeDeniedThenUnavailable_ReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream,
		config.UpstreamConfig{
			ID:          "msg-no-keys",
			Name:        "Messages Unavailable",
			BaseURL:     "http://127.0.0.1:1",
			ServiceType: "claude",
			Status:      "active",
			APIKeys:     []string{"msg-key"},
		},
		config.UpstreamConfig{
			ID:          "cmp-denied-unavailable",
			Name:        "Composite Denied Then Unavailable",
			ServiceType: "composite",
			Status:      "active",
			Priority:    0,
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetPool:      config.CompositeTargetPoolResponses,
					TargetChannelID: "resp-denied",
					FailoverTargets: []config.CompositeTargetRef{
						{Pool: config.CompositeTargetPoolMessages, ChannelID: "msg-no-keys"},
					},
				},
			},
		},
	)
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-denied",
		Name:        "Responses Denied Target",
		BaseURL:     "http://127.0.0.1:1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleMultiChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		"user-1",
		"",
		nil,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-denied-unavailable", "msg-no-keys"},
		[]string{"resp-other"},
		nil,
		nil,
	)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON body, got err=%v body=%s", err, rec.Body.String())
	}
	if body["code"] == "CHANNEL_NOT_ALLOWED" {
		t.Fatalf("expected non-permission terminal status, got CHANNEL_NOT_ALLOWED body=%s", rec.Body.String())
	}
}

func TestHandleMultiChannelProxy_CompositeDeniedThenSkippedNoCredentials_ReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream,
		config.UpstreamConfig{
			ID:          "msg-no-credentials",
			Name:        "Messages No Credentials",
			BaseURL:     "http://127.0.0.1:1",
			ServiceType: "claude",
			Status:      "active",
			APIKeys:     []string{},
		},
		config.UpstreamConfig{
			ID:          "cmp-denied-skip-creds",
			Name:        "Composite Denied Then Skip Credentials",
			ServiceType: "composite",
			Status:      "active",
			Priority:    0,
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetPool:      config.CompositeTargetPoolResponses,
					TargetChannelID: "resp-denied",
					FailoverTargets: []config.CompositeTargetRef{
						{Pool: config.CompositeTargetPoolMessages, ChannelID: "msg-no-credentials"},
					},
				},
			},
		},
	)
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-denied",
		Name:        "Responses Denied Target",
		BaseURL:     "http://127.0.0.1:1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleMultiChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		"user-1",
		"",
		nil,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-denied-skip-creds"},
		[]string{"resp-other"},
		nil,
		nil,
	)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON body, got err=%v body=%s", err, rec.Body.String())
	}
	if body["code"] == "CHANNEL_NOT_ALLOWED" {
		t.Fatalf("expected non-permission terminal status for skipped-credentials failover, got body=%s", rec.Body.String())
	}
}

func TestHandleSingleChannelProxy_CompositeResponsesTarget_BridgesToClaude(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responsesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_1",
			"status":"completed",
			"model":"gpt-5-codex",
			"output":[
				{
					"type":"message",
					"content":[
						{"type":"output_text","text":"hello from responses bridge"}
					]
				}
			],
			"usage":{"prompt_tokens":12,"completion_tokens":8}
		}`)
	}))
	defer responsesServer.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-single",
		Name:        "Composite Single",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-target",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-target",
		Name:        "Responses Target",
		BaseURL:     responsesServer.URL,
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-single"},
		[]string{"resp-target"},
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var claudeResp types.ClaudeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &claudeResp); err != nil {
		t.Fatalf("expected valid Claude response JSON, got err=%v body=%s", err, rec.Body.String())
	}
	if len(claudeResp.Content) == 0 || claudeResp.Content[0].Text != "hello from responses bridge" {
		t.Fatalf("unexpected Claude response content: %+v", claudeResp.Content)
	}
}

func TestHandleSingleChannelProxy_CompositeResponsesTarget_StreamBridgeToClaude(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamAuthHeader string
	var upstreamSawStream bool
	responsesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuthHeader = r.Header.Get("Authorization")
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var reqBody map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &reqBody)
		if stream, ok := reqBody["stream"].(bool); ok {
			upstreamSawStream = stream
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream hello\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer responsesServer.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-stream",
		Name:        "Composite Stream",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-target-stream",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-target-stream",
		Name:        "Responses Stream Target",
		BaseURL:     responsesServer.URL,
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"stream":true,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-stream"},
		[]string{"resp-target-stream"},
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if upstreamAuthHeader != "Bearer resp-key" {
		t.Fatalf("expected upstream Authorization header to be Bearer resp-key, got %q", upstreamAuthHeader)
	}
	if !upstreamSawStream {
		t.Fatalf("expected converted responses request stream=true")
	}
	if gotCT := rec.Header().Get("Content-Type"); !strings.Contains(gotCT, "text/event-stream") {
		t.Fatalf("expected stream content-type, got %q", gotCT)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: content_block_delta") {
		t.Fatalf("expected Claude stream delta event in body, got %s", body)
	}
	if !strings.Contains(body, "stream hello") {
		t.Fatalf("expected bridged stream text in body, got %s", body)
	}
	if !strings.Contains(body, "event: message_delta") {
		t.Fatalf("expected Claude stream message_delta event in body, got %s", body)
	}
}

func TestHandleSingleChannelProxy_CompositeOAuthTarget_401TriggersForcedRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"unauthorized"}}`)
	}))
	defer oauthServer.Close()

	oldEndpoint := codexOAuthResponsesEndpoint
	oldGetValid := getValidOAuthTokenForMessagesBridge
	oldRefresh := refreshOAuthTokensForMessagesBridge
	codexOAuthResponsesEndpoint = oauthServer.URL
	defer func() {
		codexOAuthResponsesEndpoint = oldEndpoint
		getValidOAuthTokenForMessagesBridge = oldGetValid
		refreshOAuthTokensForMessagesBridge = oldRefresh
	}()

	getValidOAuthTokenForMessagesBridge = func(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
		return "access-token", "account-1", nil, nil
	}

	var refreshCalls int32
	refreshOAuthTokensForMessagesBridge = func(refreshToken string, retries int) (*config.OAuthTokens, error) {
		atomic.AddInt32(&refreshCalls, 1)
		return &config.OAuthTokens{
			AccessToken:  "refreshed-access",
			AccountID:    "account-1",
			RefreshToken: "ref-1",
			LastRefresh:  time.Now().Format(time.RFC3339),
		}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-oauth",
		Name:        "Composite OAuth",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-oauth",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-oauth",
		Name:        "Responses OAuth",
		ServiceType: "openai-oauth",
		Status:      "active",
		OAuthTokens: &config.OAuthTokens{
			AccessToken:  "seed-access",
			AccountID:    "account-1",
			RefreshToken: "ref-1",
		},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-oauth"},
		[]string{"resp-oauth"},
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("expected forced refresh to be attempted once, got %d", got)
	}

	currentCfg := cfgManager.GetConfig()
	if len(currentCfg.ResponsesUpstream) == 0 || currentCfg.ResponsesUpstream[0].OAuthTokens == nil {
		t.Fatalf("expected oauth tokens in responses config")
	}
	if currentCfg.ResponsesUpstream[0].OAuthTokens.AccessToken != "refreshed-access" {
		t.Fatalf("expected refreshed token to be persisted, got %q", currentCfg.ResponsesUpstream[0].OAuthTokens.AccessToken)
	}
}

func TestHandleSingleChannelProxy_CompositeOAuthTarget_HeaderAndUAParity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type observedRequest struct {
		Authorization   string
		AccountID       string
		Originator      string
		ConversationID  string
		SessionID       string
		Accept          string
		UserAgent       string
		ContentType     string
		StreamInPayload bool
		PromptCacheKey  string
	}

	testCases := []struct {
		name            string
		stream          bool
		incomingUA      string
		expectedAccept  string
		expectedUA      string
		expectedText    string
		responseBuilder func(http.ResponseWriter)
	}{
		{
			name:           "non-stream keeps codex user-agent and json accept",
			stream:         false,
			incomingUA:     "codex_cli_rs/0.91.0 (Linux; x86_64)",
			expectedAccept: "application/json",
			expectedUA:     "codex_cli_rs/0.91.0 (Linux; x86_64)",
			expectedText:   "hello from oauth bridge",
			responseBuilder: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{
					"id":"resp_oauth_1",
					"status":"completed",
					"model":"gpt-5-codex",
					"output":[{"type":"message","content":[{"type":"output_text","text":"hello from oauth bridge"}]}]
				}`)
			},
		},
		{
			name:           "stream falls back to default codex user-agent and sse accept",
			stream:         true,
			incomingUA:     "Mozilla/5.0",
			expectedAccept: "text/event-stream",
			expectedUA:     config.DefaultResponsesUserAgent,
			expectedText:   "oauth stream text",
			responseBuilder: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"oauth stream text\"}\n\n")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var observed observedRequest
			oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bodyBytes, _ := io.ReadAll(r.Body)
				_ = r.Body.Close()
				var payload map[string]interface{}
				_ = json.Unmarshal(bodyBytes, &payload)
				if stream, ok := payload["stream"].(bool); ok {
					observed.StreamInPayload = stream
				}
				if promptCacheKey, ok := payload["prompt_cache_key"].(string); ok {
					observed.PromptCacheKey = promptCacheKey
				}

				observed.Authorization = r.Header.Get("Authorization")
				observed.AccountID = r.Header.Get("Chatgpt-Account-Id")
				observed.Originator = r.Header.Get("Originator")
				observed.ConversationID = r.Header.Get("Conversation_id")
				observed.SessionID = r.Header.Get("Session_id")
				observed.Accept = r.Header.Get("Accept")
				observed.UserAgent = r.Header.Get("User-Agent")
				observed.ContentType = r.Header.Get("Content-Type")

				tc.responseBuilder(w)
			}))
			defer oauthServer.Close()

			oldEndpoint := codexOAuthResponsesEndpoint
			oldGetValid := getValidOAuthTokenForMessagesBridge
			oldRefresh := refreshOAuthTokensForMessagesBridge
			codexOAuthResponsesEndpoint = oauthServer.URL
			defer func() {
				codexOAuthResponsesEndpoint = oldEndpoint
				getValidOAuthTokenForMessagesBridge = oldGetValid
				refreshOAuthTokensForMessagesBridge = oldRefresh
			}()

			getValidOAuthTokenForMessagesBridge = func(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
				return "access-token", "account-1", nil, nil
			}
			refreshOAuthTokensForMessagesBridge = func(refreshToken string, retries int) (*config.OAuthTokens, error) {
				t.Fatalf("unexpected refresh call in parity test")
				return nil, nil
			}

			cfgPath := filepath.Join(t.TempDir(), "config.json")
			cfg := config.Config{
				LoadBalance:              "failover",
				ResponsesLoadBalance:     "failover",
				GeminiLoadBalance:        "failover",
				CurrentUpstream:          0,
				CurrentResponsesUpstream: 0,
			}
			cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
				ID:          "cmp-oauth-parity",
				Name:        "Composite OAuth Parity",
				ServiceType: "composite",
				Status:      "active",
				CompositeMappings: []config.CompositeMapping{
					{
						Pattern:         "haiku",
						TargetPool:      config.CompositeTargetPoolResponses,
						TargetChannelID: "resp-oauth-parity",
					},
				},
			})
			cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
				ID:          "resp-oauth-parity",
				Name:        "Responses OAuth Parity",
				ServiceType: "openai-oauth",
				Status:      "active",
				OAuthTokens: &config.OAuthTokens{
					AccessToken:  "seed-access",
					AccountID:    "account-1",
					RefreshToken: "ref-1",
				},
			})

			configBytes, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfgManager, err := config.NewConfigManager(cfgPath)
			if err != nil {
				t.Fatalf("NewConfigManager: %v", err)
			}
			t.Cleanup(func() { _ = cfgManager.Close() })

			traceAffinity := session.NewTraceAffinityManager()
			t.Cleanup(traceAffinity.Stop)
			s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

			bodyBytes := []byte(`{
				"model":"claude-3-haiku-20240307",
				"max_tokens":64,
				"stream":` + func() string {
				if tc.stream {
					return "true"
				}
				return "false"
			}() + `,
				"messages":[{"role":"user","content":"hello"}]
			}`)
			var claudeReq types.ClaudeRequest
			if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
				t.Fatalf("unmarshal claude request: %v", err)
			}

			rec := newCloseNotifyRecorder()
			c, _ := gin.CreateTestContext(rec)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", tc.incomingUA)
			req.Header.Set("Conversation_id", "conv-123")
			req.Header.Set("Session_id", "sess-456")
			req.Header.Set("Originator", "originator-bridge-test")
			c.Request = req

			envCfg := &config.EnvConfig{
				LogLevel:       "error",
				RequestTimeout: 30000,
			}

			handleSingleChannelProxy(
				c,
				envCfg,
				cfgManager,
				s,
				bodyBytes,
				claudeReq,
				time.Now(),
				nil,
				"",
				nil,
				[]string{"cmp-oauth-parity"},
				[]string{"resp-oauth-parity"},
				nil,
				"user-1",
				"",
				nil,
				nil,
			)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			if observed.Authorization != "Bearer access-token" {
				t.Fatalf("expected OAuth Authorization header, got %q", observed.Authorization)
			}
			if observed.AccountID != "account-1" {
				t.Fatalf("expected Chatgpt-Account-Id account-1, got %q", observed.AccountID)
			}
			if observed.Originator != "originator-bridge-test" {
				t.Fatalf("expected Originator forwarded, got %q", observed.Originator)
			}
			if observed.ConversationID != "conv-123" {
				t.Fatalf("expected Conversation_id forwarded, got %q", observed.ConversationID)
			}
			if observed.SessionID != "sess-456" {
				t.Fatalf("expected Session_id forwarded, got %q", observed.SessionID)
			}
			if observed.PromptCacheKey != "sess-456" {
				t.Fatalf("expected prompt_cache_key derived from Session_id, got %q", observed.PromptCacheKey)
			}
			if observed.Accept != tc.expectedAccept {
				t.Fatalf("expected Accept %q, got %q", tc.expectedAccept, observed.Accept)
			}
			if observed.UserAgent != tc.expectedUA {
				t.Fatalf("expected User-Agent %q, got %q", tc.expectedUA, observed.UserAgent)
			}
			if observed.ContentType != "application/json" {
				t.Fatalf("expected Content-Type application/json, got %q", observed.ContentType)
			}
			if observed.StreamInPayload != tc.stream {
				t.Fatalf("expected stream in payload to be %v, got %v", tc.stream, observed.StreamInPayload)
			}

			if tc.stream {
				if gotCT := rec.Header().Get("Content-Type"); !strings.Contains(gotCT, "text/event-stream") {
					t.Fatalf("expected stream response content-type, got %q", gotCT)
				}
				body := rec.Body.String()
				if !strings.Contains(body, "event: content_block_delta") {
					t.Fatalf("expected Claude stream delta event in body, got %s", body)
				}
				if !strings.Contains(body, tc.expectedText) {
					t.Fatalf("expected streamed text %q in body, got %s", tc.expectedText, body)
				}
			} else {
				var claudeResp types.ClaudeResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &claudeResp); err != nil {
					t.Fatalf("expected valid Claude response JSON, got err=%v body=%s", err, rec.Body.String())
				}
				if len(claudeResp.Content) == 0 || claudeResp.Content[0].Text != tc.expectedText {
					t.Fatalf("unexpected Claude response content: %+v", claudeResp.Content)
				}
			}
		})
	}
}

func TestHandleSingleChannelProxy_CompositeOAuthTarget_SessionFallbackFromMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type observedRequest struct {
		SessionID      string
		PromptCacheKey string
	}

	var observed observedRequest
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var payload map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &payload)
		if promptCacheKey, ok := payload["prompt_cache_key"].(string); ok {
			observed.PromptCacheKey = promptCacheKey
		}
		observed.SessionID = r.Header.Get("Session_id")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_oauth_1",
			"status":"completed",
			"model":"gpt-5-codex",
			"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]
		}`)
	}))
	defer oauthServer.Close()

	oldEndpoint := codexOAuthResponsesEndpoint
	oldGetValid := getValidOAuthTokenForMessagesBridge
	oldRefresh := refreshOAuthTokensForMessagesBridge
	codexOAuthResponsesEndpoint = oauthServer.URL
	defer func() {
		codexOAuthResponsesEndpoint = oldEndpoint
		getValidOAuthTokenForMessagesBridge = oldGetValid
		refreshOAuthTokensForMessagesBridge = oldRefresh
	}()

	getValidOAuthTokenForMessagesBridge = func(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
		return "access-token", "account-1", nil, nil
	}
	refreshOAuthTokensForMessagesBridge = func(refreshToken string, retries int) (*config.OAuthTokens, error) {
		t.Fatalf("unexpected refresh call in fallback test")
		return nil, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-oauth-fallback",
		Name:        "Composite OAuth Fallback",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-oauth-fallback",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-oauth-fallback",
		Name:        "Responses OAuth Fallback",
		ServiceType: "openai-oauth",
		Status:      "active",
		OAuthTokens: &config.OAuthTokens{
			AccessToken:  "seed-access",
			AccountID:    "account-1",
			RefreshToken: "ref-1",
		},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"stream":false,
		"metadata":{"user_id":"user_abc_account__session_sess-meta-bridge"},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.91.0 (Linux; x86_64)")
	// Intentionally do not set Session_id header to verify fallback behavior.
	req.Header.Set("Originator", "originator-fallback-test")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-oauth-fallback"},
		[]string{"resp-oauth-fallback"},
		nil,
		"user-1",
		"sess-meta-bridge",
		nil,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if observed.PromptCacheKey != "sess-meta-bridge" {
		t.Fatalf("expected prompt_cache_key from metadata session fallback, got %q", observed.PromptCacheKey)
	}
	if observed.SessionID != "sess-meta-bridge" {
		t.Fatalf("expected Session_id header fallback from prompt_cache_key, got %q", observed.SessionID)
	}
}

func TestHandleSingleChannelProxy_CompositeResponsesTarget_DeniedByResolvedTargetACL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-acl",
		Name:        "Composite ACL",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-target",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-target",
		Name:        "Responses Target",
		BaseURL:     "http://127.0.0.1:1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-acl"},    // top-level composite allowed
		[]string{"resp-other"}, // non-empty responses ACL that does not include resp-target
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON body, got err=%v body=%s", err, rec.Body.String())
	}
	if body["code"] != "CHANNEL_NOT_ALLOWED" {
		t.Fatalf("expected code CHANNEL_NOT_ALLOWED, got %#v", body["code"])
	}
}

func TestHandleSingleChannelProxy_CompositeMessagesTarget_Unchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	messagesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"msg_test_2",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"hello from messages composite"}],
			"stop_reason":"end_turn",
			"model":"claude-3-haiku-20240307"
		}`)
	}))
	defer messagesServer.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream,
		config.UpstreamConfig{
			ID:          "cmp-msg",
			Name:        "Composite Messages",
			ServiceType: "composite",
			Status:      "active",
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetChannelID: "msg-target",
				},
			},
		},
		config.UpstreamConfig{
			ID:          "msg-target",
			Name:        "Messages Target",
			BaseURL:     messagesServer.URL,
			ServiceType: "claude",
			Status:      "active",
			APIKeys:     []string{"msg-key"},
		},
	)

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-msg", "msg-target"},
		[]string{},
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var claudeResp types.ClaudeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &claudeResp); err != nil {
		t.Fatalf("expected valid Claude response JSON, got err=%v body=%s", err, rec.Body.String())
	}
	if len(claudeResp.Content) == 0 || claudeResp.Content[0].Text != "hello from messages composite" {
		t.Fatalf("unexpected Claude response content: %+v", claudeResp.Content)
	}
}

func TestHandleSingleChannelProxy_CompositeOAuthTarget_DoesNotDoubleRedirectModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamModel string
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var payload map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &payload)
		if model, ok := payload["model"].(string); ok {
			upstreamModel = model
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_no_double_map",
			"status":"completed",
			"model":"gpt-5-mini",
			"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]
		}`)
	}))
	defer oauthServer.Close()

	oldEndpoint := codexOAuthResponsesEndpoint
	oldGetValid := getValidOAuthTokenForMessagesBridge
	oldRefresh := refreshOAuthTokensForMessagesBridge
	codexOAuthResponsesEndpoint = oauthServer.URL
	defer func() {
		codexOAuthResponsesEndpoint = oldEndpoint
		getValidOAuthTokenForMessagesBridge = oldGetValid
		refreshOAuthTokensForMessagesBridge = oldRefresh
	}()

	getValidOAuthTokenForMessagesBridge = func(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
		return "access-token", "account-1", nil, nil
	}
	refreshOAuthTokensForMessagesBridge = func(refreshToken string, retries int) (*config.OAuthTokens, error) {
		t.Fatalf("unexpected refresh call in no-double-redirect test")
		return nil, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-oauth-model-map",
		Name:        "Composite OAuth Model Map",
		ServiceType: "composite",
		Status:      "active",
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-oauth-model-map",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-oauth-model-map",
		Name:        "Responses OAuth Model Map",
		ServiceType: "openai-oauth",
		Status:      "active",
		ModelMapping: map[string]string{
			"claude-3-haiku-20240307": "gpt-5-mini",
			"gpt-5-mini":              "gpt-5-ultra",
		},
		OAuthTokens: &config.OAuthTokens{
			AccessToken:  "seed-access",
			AccountID:    "account-1",
			RefreshToken: "ref-1",
		},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleSingleChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-oauth-model-map"},
		[]string{"resp-oauth-model-map"},
		nil,
		"user-1",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if upstreamModel != "gpt-5-mini" {
		t.Fatalf("expected model to be redirected once to gpt-5-mini, got %q", upstreamModel)
	}
}

func TestHandleMultiChannelProxy_CompositeResponsesTarget_DeniedByResolvedTargetACL_ReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "cmp-multi-acl",
		Name:        "Composite Multi ACL",
		ServiceType: "composite",
		Status:      "active",
		Priority:    0,
		CompositeMappings: []config.CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      config.CompositeTargetPoolResponses,
				TargetChannelID: "resp-target",
			},
		},
	})
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-target",
		Name:        "Responses Target",
		BaseURL:     "http://127.0.0.1:1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
	})

	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, configBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	s := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	bodyBytes := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := newCloseNotifyRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}

	handleMultiChannelProxy(
		c,
		envCfg,
		cfgManager,
		s,
		bodyBytes,
		claudeReq,
		"user-1",
		"",
		nil,
		time.Now(),
		nil,
		"",
		nil,
		[]string{"cmp-multi-acl"}, // top-level composite allowed
		[]string{"resp-other"},    // non-empty responses ACL that excludes resolved target
		nil,
		nil,
	)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON body, got err=%v body=%s", err, rec.Body.String())
	}
	if body["code"] != "CHANNEL_NOT_ALLOWED" {
		t.Fatalf("expected code CHANNEL_NOT_ALLOWED, got %#v", body["code"])
	}
}
