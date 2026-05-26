package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func newResponsesWebSocketTestConfigManager(t *testing.T) *config.ConfigManager {
	t.Helper()

	cfgManager, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })
	return cfgManager
}

func TestGetAndUpdateResponsesWebSocketConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	router := gin.New()
	router.GET("/api/config/responses-websocket", GetResponsesWebSocketConfig(cfgManager))
	router.PUT("/api/config/responses-websocket", UpdateResponsesWebSocketConfig(cfgManager))

	getReq := httptest.NewRequest(http.MethodGet, "/api/config/responses-websocket", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200, body=%s", getRec.Code, getRec.Body.String())
	}
	var got map[string]bool
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if got["enabled"] {
		t.Fatalf("GET enabled = true, want false by default")
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/config/responses-websocket", bytes.NewReader([]byte(`{"enabled":true}`)))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200, body=%s", putRec.Code, putRec.Body.String())
	}
	got = map[string]bool{}
	if err := json.Unmarshal(putRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if !got["enabled"] {
		t.Fatalf("PUT enabled = false, want true")
	}
}

func TestResponsesWebSocketHandlerReturnsNotFoundWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	router := gin.New()
	router.GET("/v1/responses", ResponsesWebSocketHandler(nil, cfgManager, nil, nil, nil, nil, nil))

	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestResponsesWebSocketHandlerProxiesOpenAIOAuthWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan []byte, 1)
	receivedHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		receivedHeaders <- r.Header.Clone()
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("upstream read failed: %v", err)
			return
		}
		if msgType != websocket.TextMessage {
			t.Errorf("upstream msgType = %d, want text", msgType)
			return
		}
		received <- payload
		time.Sleep(5 * time.Millisecond)
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"hello"}`)); err != nil {
			t.Errorf("upstream delta write failed: %v", err)
			return
		}
		completed := `{"type":"response.completed","response":{"id":"resp_test","model":"gpt-5","output":[],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":4},"output_tokens":7,"total_tokens":19}}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(completed)); err != nil {
			t.Errorf("upstream write failed: %v", err)
		}
	}))
	defer upstream.Close()

	oldEndpoint := codexOAuthResponsesEndpoint
	codexOAuthResponsesEndpoint = upstream.URL
	t.Cleanup(func() { codexOAuthResponsesEndpoint = oldEndpoint })

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.UpdateResponsesWebSocketConfig(config.ResponsesWebSocketConfig{Enabled: true}); err != nil {
		t.Fatalf("enable websocket config: %v", err)
	}
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:        "oauth-ws",
		BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
		ServiceType: "openai-oauth",
		Status:      "active",
		OAuthTokens: &config.OAuthTokens{
			AccessToken: "access-token",
			AccountID:   "account-id",
		},
	}); err != nil {
		t.Fatalf("AddResponsesUpstream() failed: %v", err)
	}

	sch := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		session.NewTraceAffinityManager(),
	)
	reqLogManager := newTestRequestLogManager(t)

	router := gin.New()
	router.GET("/v1/responses", ResponsesWebSocketHandler(&config.EnvConfig{
		ProxyAccessKey:    "test-proxy-access-key",
		MaxRequestBodyMB:  20,
		RequestTimeout:    300000,
		EnableRequestLogs: true,
	}, cfgManager, sch, reqLogManager, nil, nil, nil))
	bridge := httptest.NewServer(router)
	defer bridge.Close()

	target := "ws" + strings.TrimPrefix(bridge.URL, "http")
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse ws url: %v", err)
	}
	parsed.Path = "/v1/responses"

	conn, resp, err := websocket.DefaultDialer.Dial(parsed.String(), http.Header{
		"Authorization":                         []string{"Bearer test-proxy-access-key"},
		"User-Agent":                            []string{"codex_cli_rs/0.73.0 (Linux; x86_64)"},
		"Originator":                            []string{"codex_cli_rs"},
		"X-Codex-Turn-State":                    []string{"turn-state"},
		"X-Codex-Turn-Metadata":                 []string{"turn-metadata"},
		"X-Client-Request-Id":                   []string{"request-id"},
		"X-Responsesapi-Include-Timing-Metrics": []string{"true"},
		"Version":                               []string{"1"},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	clientPayload := []byte(`{"type":"response.create","model":"gpt-5","input":[]}`)
	if err := conn.WriteMessage(websocket.TextMessage, clientPayload); err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	_, deltaPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("client read delta failed: %v", err)
	}
	if string(deltaPayload) != `{"type":"response.output_text.delta","delta":"hello"}` {
		t.Fatalf("delta payload = %s", deltaPayload)
	}
	_, responsePayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("client read completed failed: %v", err)
	}
	if string(responsePayload) != `{"type":"response.completed","response":{"id":"resp_test","model":"gpt-5","output":[],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":4},"output_tokens":7,"total_tokens":19}}}` {
		t.Fatalf("response payload = %s", responsePayload)
	}
	_ = conn.Close()

	select {
	case got := <-received:
		if !bytes.Equal(got, clientPayload) {
			t.Fatalf("upstream payload = %s, want %s", got, clientPayload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}

	select {
	case headers := <-receivedHeaders:
		if got := headers.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("upstream Authorization = %q", got)
		}
		if got := headers.Get("ChatGPT-Account-ID"); got != "account-id" {
			t.Fatalf("upstream ChatGPT-Account-ID = %q", got)
		}
		if got := headers.Get("OpenAI-Beta"); got != "responses_websockets=2026-02-06" {
			t.Fatalf("upstream OpenAI-Beta = %q", got)
		}
		if got := headers.Get("X-Codex-Turn-State"); got != "turn-state" {
			t.Fatalf("upstream X-Codex-Turn-State = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream headers")
	}

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("request log count = %d, want 1", len(recent.Requests))
	}
	logRecord := recent.Requests[0]
	if logRecord.Status != requestlog.StatusCompleted {
		t.Fatalf("request log status = %q, want %q", logRecord.Status, requestlog.StatusCompleted)
	}
	if logRecord.Endpoint != "/v1/responses" {
		t.Fatalf("request log endpoint = %q", logRecord.Endpoint)
	}
	if logRecord.Type != "openai-oauth" {
		t.Fatalf("request log type = %q", logRecord.Type)
	}
	if logRecord.ProviderName != "oauth-ws" {
		t.Fatalf("request log provider = %q", logRecord.ProviderName)
	}
	if logRecord.Model != "gpt-5" {
		t.Fatalf("request log model = %q", logRecord.Model)
	}
	if logRecord.HTTPStatus != http.StatusOK {
		t.Fatalf("request log HTTPStatus = %d", logRecord.HTTPStatus)
	}
	if logRecord.FirstTokenTime == nil {
		t.Fatalf("request log firstTokenTime is nil")
	}
	if logRecord.FirstTokenDurationMs <= 0 {
		t.Fatalf("request log firstTokenDurationMs = %d, want > 0", logRecord.FirstTokenDurationMs)
	}
	if logRecord.InputTokens != 8 || logRecord.CacheReadInputTokens != 4 || logRecord.OutputTokens != 7 {
		t.Fatalf("request log usage = input:%d cached:%d output:%d", logRecord.InputTokens, logRecord.CacheReadInputTokens, logRecord.OutputTokens)
	}
}
