package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/quota"
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

func assertJSONBytesEqual(t *testing.T, got []byte, want []byte) {
	t.Helper()

	var gotValue interface{}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("failed to parse got JSON %s: %v", got, err)
	}
	var wantValue interface{}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("failed to parse want JSON %s: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON payload = %s, want %s", got, want)
	}
}

func TestLegacyResponsesWebSocketConfigEndpointMigratesToChannelFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:        "oauth",
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
	if got["enabled"] {
		t.Fatalf("PUT enabled = true, want false after legacy migration")
	}

	cfg := cfgManager.GetConfig()
	if !cfg.ResponsesUpstream[0].ResponsesWebSocketEnabled {
		t.Fatalf("openai-oauth channel responsesWebSocketEnabled = false, want true")
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

func TestResponsesWebSocketHandlerReturnsNotFoundWhenNoChannelEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:        "responses-without-ws",
		BaseURL:     "https://api.openai.com/v1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"api-key"},
	}); err != nil {
		t.Fatalf("AddResponsesUpstream() failed: %v", err)
	}

	sch := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		session.NewTraceAffinityManager(),
	)

	router := gin.New()
	router.GET("/v1/responses", ResponsesWebSocketHandler(nil, cfgManager, sch, nil, nil, nil, nil))

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

func TestResponsesWebSocketHandlerClosesActiveConnectionWhenChannelDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreResponsesWebSocketConnectionManager(t)

	bridgeURL, received := startResponsesWebSocketInvalidationBridge(t, func(router *gin.Engine, cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) {
		router.PATCH("/api/responses/channels/:id/status", SetResponsesChannelStatus(cfgManager, sch))
	})

	conn := dialResponsesWebSocketBridge(t, bridgeURL)
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5","input":[]}`)); err != nil {
		t.Fatalf("client first write failed: %v", err)
	}
	waitForResponsesWebSocketPayload(t, received)

	req := httptest.NewRequest(http.MethodPatch, "/api/responses/channels/0/status", bytes.NewReader([]byte(`{"status":"disabled"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	bridgeURL.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	assertResponsesWebSocketClosed(t, conn)
	assertResponsesWebSocketReceivesNoMorePayloads(t, conn, received)
}

func TestResponsesWebSocketHandlerClosesActiveConnectionWhenWebSocketFlagDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreResponsesWebSocketConnectionManager(t)

	bridgeURL, received := startResponsesWebSocketInvalidationBridge(t, func(router *gin.Engine, cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) {
		router.PUT("/api/responses/channels/:id", UpdateResponsesUpstream(cfgManager, sch))
	})

	conn := dialResponsesWebSocketBridge(t, bridgeURL)
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5","input":[]}`)); err != nil {
		t.Fatalf("client first write failed: %v", err)
	}
	waitForResponsesWebSocketPayload(t, received)

	req := httptest.NewRequest(http.MethodPut, "/api/responses/channels/0", bytes.NewReader([]byte(`{"responsesWebSocketEnabled":false}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	bridgeURL.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	assertResponsesWebSocketClosed(t, conn)
	assertResponsesWebSocketReceivesNoMorePayloads(t, conn, received)
}

func TestResponsesWebSocketHandlerClosesActiveConnectionWhenChannelDeleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreResponsesWebSocketConnectionManager(t)

	bridgeURL, received := startResponsesWebSocketInvalidationBridge(t, func(router *gin.Engine, cfgManager *config.ConfigManager, _ *scheduler.ChannelScheduler) {
		router.DELETE("/api/responses/channels/:id", DeleteResponsesUpstream(cfgManager, nil))
	})

	conn := dialResponsesWebSocketBridge(t, bridgeURL)
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5","input":[]}`)); err != nil {
		t.Fatalf("client first write failed: %v", err)
	}
	waitForResponsesWebSocketPayload(t, received)

	req := httptest.NewRequest(http.MethodDelete, "/api/responses/channels/0", nil)
	rec := httptest.NewRecorder()
	bridgeURL.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	assertResponsesWebSocketClosed(t, conn)
	assertResponsesWebSocketReceivesNoMorePayloads(t, conn, received)
}

type responsesWebSocketInvalidationBridge struct {
	url    string
	router *gin.Engine
}

func restoreResponsesWebSocketConnectionManager(t *testing.T) {
	t.Helper()
	previous := activeResponsesWebSockets
	activeResponsesWebSockets = newResponsesWebSocketConnectionManager()
	t.Cleanup(func() {
		activeResponsesWebSockets = previous
	})
}

func startResponsesWebSocketInvalidationBridge(t *testing.T, addRoutes func(*gin.Engine, *config.ConfigManager, *scheduler.ChannelScheduler)) (*responsesWebSocketInvalidationBridge, <-chan []byte) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan []byte, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- payload
		}
	}))
	t.Cleanup(upstream.Close)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		ID:                        "resp-ws",
		Name:                      "responses-ws",
		BaseURL:                   upstream.URL,
		ServiceType:               "responses",
		Status:                    "active",
		APIKeys:                   []string{"api-key"},
		ResponsesWebSocketEnabled: true,
	}); err != nil {
		t.Fatalf("AddResponsesUpstream() failed: %v", err)
	}

	sch := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		session.NewTraceAffinityManager(),
	)

	router := gin.New()
	router.GET("/v1/responses", ResponsesWebSocketHandler(nil, cfgManager, sch, nil, nil, nil, nil))
	addRoutes(router, cfgManager, sch)

	bridge := httptest.NewServer(router)
	t.Cleanup(bridge.Close)

	return &responsesWebSocketInvalidationBridge{url: bridge.URL, router: router}, received
}

func dialResponsesWebSocketBridge(t *testing.T, bridge *responsesWebSocketInvalidationBridge) *websocket.Conn {
	t.Helper()

	target := "ws" + strings.TrimPrefix(bridge.url, "http")
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse bridge URL: %v", err)
	}
	parsed.Path = "/v1/responses"

	conn, resp, err := websocket.DefaultDialer.Dial(parsed.String(), http.Header{
		"OpenAI-Beta": []string{"responses_websockets=2026-02-06"},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func waitForResponsesWebSocketPayload(t *testing.T, received <-chan []byte) {
	t.Helper()

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream websocket payload")
	}
}

func assertResponsesWebSocketClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline failed: %v", err)
	}
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("expected websocket read to fail after channel invalidation")
	}
}

func assertResponsesWebSocketReceivesNoMorePayloads(t *testing.T, conn *websocket.Conn, received <-chan []byte) {
	t.Helper()

	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5","input":["after-disable"]}`))

	select {
	case payload := <-received:
		t.Fatalf("upstream received payload after channel invalidation: %s", payload)
	case <-time.After(100 * time.Millisecond):
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

	usageRequests := make(chan http.Header, 1)
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageRequests <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"plan_type": "pro",
			"rate_limit": {
				"primary_window": {
					"used_percent": 3,
					"limit_window_seconds": 18000,
					"reset_at": 1893456000
				},
				"secondary_window": {
					"used_percent": 11,
					"limit_window_seconds": 604800,
					"reset_at": 1894060800
				}
			}
		}`)
	}))
	defer usageServer.Close()
	oldUsageEndpoints := codexOAuthUsageEndpoints
	oldResetStatusEndpoints := codexOAuthResetCreditStatusEndpoints
	oldUsageHTTPClient := codexOAuthUsageHTTPClient
	codexOAuthUsageEndpoints = []string{usageServer.URL + "/backend-api/codex/usage"}
	codexOAuthResetCreditStatusEndpoints = nil
	codexOAuthUsageHTTPClient = usageServer.Client()
	t.Cleanup(func() {
		codexOAuthUsageEndpoints = oldUsageEndpoints
		codexOAuthResetCreditStatusEndpoints = oldResetStatusEndpoints
		codexOAuthUsageHTTPClient = oldUsageHTTPClient
	})

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	cfg := cfgManager.GetConfig()
	cfg.DebugLog = config.DebugLogConfig{Enabled: true, MaxBodySize: 100000}
	if err := cfgManager.RestoreConfig(cfg); err != nil {
		t.Fatalf("enable debug log config: %v", err)
	}
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:                      "oauth-ws",
		BaseURL:                   "https://chatgpt.com/backend-api/codex/responses",
		ServiceType:               "openai-oauth",
		Status:                    "active",
		ResponsesWebSocketEnabled: true,
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
		assertJSONBytesEqual(t, got, clientPayload)
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
	debugEntry, err := reqLogManager.GetDebugLog(logRecord.ID)
	if err != nil {
		t.Fatalf("GetDebugLog failed: %v", err)
	}
	if debugEntry == nil {
		t.Fatalf("expected debug log for websocket request")
	}
	if debugEntry.RequestMethod != http.MethodGet || debugEntry.RequestPath != "/v1/responses" {
		t.Fatalf("debug request target = %s %s", debugEntry.RequestMethod, debugEntry.RequestPath)
	}
	if !strings.Contains(debugEntry.RequestBody, `"type":"response.create"`) {
		t.Fatalf("debug request body = %s", debugEntry.RequestBody)
	}
	if !strings.Contains(debugEntry.ResponseBody, `"type":"response.output_text.delta"`) ||
		!strings.Contains(debugEntry.ResponseBody, `"type":"response.completed"`) {
		t.Fatalf("debug response body = %s", debugEntry.ResponseBody)
	}

	select {
	case headers := <-usageRequests:
		if got := headers.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("usage Authorization = %q", got)
		}
		if got := headers.Get("Chatgpt-Account-Id"); got != "account-id" {
			t.Fatalf("usage Chatgpt-Account-Id = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket quota refresh")
	}

	cfgAfter := cfgManager.GetConfig()
	stableID := cfgAfter.ResponsesUpstream[0].ID
	var stored *quota.QuotaStatus
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored = quota.GetManager().GetStatusForChannel(logRecord.ChannelID, stableID, "oauth-ws")
		if stored != nil && stored.CodexQuota != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stored == nil || stored.CodexQuota == nil {
		t.Fatalf("expected stored websocket Codex quota, got %+v", stored)
	}
	if stored.CodexQuota.PrimaryUsedPercentValue() != 3 || stored.CodexQuota.SecondaryUsedPercentValue() != 11 {
		t.Fatalf("stored quota = primary:%v secondary:%v, want primary:3 secondary:11",
			stored.CodexQuota.PrimaryUsedPercentValue(),
			stored.CodexQuota.SecondaryUsedPercentValue())
	}
}

func TestSanitizeCodexOAuthWebSocketClientPayload_StripsEncryptedReasoning(t *testing.T) {
	payload := []byte(`{
		"type":"response.create",
		"model":"gpt-5",
		"include":["reasoning.encrypted_content","file_search_call.results"],
		"input":[
			{"type":"reasoning","encrypted_content":"QVhO...fQ=="},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello","encrypted_content":"nested"}]}
		]
	}`)

	sanitized := sanitizeCodexOAuthWebSocketClientPayload(
		&config.UpstreamConfig{ServiceType: "openai-oauth"},
		payload,
	)

	var got map[string]interface{}
	if err := json.Unmarshal(sanitized, &got); err != nil {
		t.Fatalf("failed to parse sanitized payload %s: %v", sanitized, err)
	}
	include := got["include"].([]interface{})
	if len(include) != 1 || include[0] != "file_search_call.results" {
		t.Fatalf("include = %#v, want file_search_call.results only", include)
	}
	input := got["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input length = %d, want encrypted reasoning item removed", len(input))
	}
	message := input[0].(map[string]interface{})
	content := message["content"].([]interface{})
	contentPart := content[0].(map[string]interface{})
	if _, exists := contentPart["encrypted_content"]; exists {
		t.Fatalf("nested encrypted_content was not stripped: %#v", contentPart)
	}
}

func TestResponsesWebSocketHandlerProxiesAPIKeyResponsesWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	receivedPath := make(chan string, 1)
	receivedHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		receivedPath <- r.URL.Path
		receivedHeaders <- r.Header.Clone()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("upstream read failed: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"hi"}`)); err != nil {
			t.Errorf("upstream delta write failed: %v", err)
			return
		}
		completed := `{"type":"response.completed","response":{"id":"resp_key","model":"gpt-5","output":[],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(completed)); err != nil {
			t.Errorf("upstream completed write failed: %v", err)
		}
	}))
	defer upstream.Close()

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:                      "responses-ws",
		BaseURL:                   upstream.URL,
		ServiceType:               "responses",
		Status:                    "active",
		APIKeys:                   []string{"upstream-api-key"},
		ResponsesWebSocketEnabled: true,
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
	router.GET("/v1/responses", ResponsesWebSocketHandler(nil, cfgManager, sch, reqLogManager, nil, nil, nil))
	bridge := httptest.NewServer(router)
	defer bridge.Close()

	parsed, err := url.Parse("ws" + strings.TrimPrefix(bridge.URL, "http"))
	if err != nil {
		t.Fatalf("parse bridge ws url: %v", err)
	}
	parsed.Path = "/v1/responses"

	conn, resp, err := websocket.DefaultDialer.Dial(parsed.String(), http.Header{
		"OpenAI-Beta": []string{"responses_websockets=2026-02-06"},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5","input":[]}`)); err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("client read delta failed: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("client read completed failed: %v", err)
	}
	_ = conn.Close()

	select {
	case gotPath := <-receivedPath:
		if gotPath != "/v1/responses" {
			t.Fatalf("upstream path = %q, want /v1/responses", gotPath)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream path")
	}

	select {
	case headers := <-receivedHeaders:
		if got := headers.Get("Authorization"); got != "Bearer upstream-api-key" {
			t.Fatalf("upstream Authorization = %q", got)
		}
		if got := headers.Get("OpenAI-Beta"); got != "responses_websockets=2026-02-06" {
			t.Fatalf("upstream OpenAI-Beta = %q", got)
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
	if recent.Requests[0].Type != "responses" {
		t.Fatalf("request log type = %q, want responses", recent.Requests[0].Type)
	}
	if recent.Requests[0].ProviderName != "responses-ws" {
		t.Fatalf("request log provider = %q, want responses-ws", recent.Requests[0].ProviderName)
	}
}

func TestResponsesWebSocketFallbackRecordsFailureAfterUpstreamConnectBeforeFirstRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstreamConnected := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade failed: %v", err)
			return
		}
		upstreamConnected <- struct{}{}
		_ = conn.UnderlyingConn().Close()
	}))
	defer upstream.Close()

	oldEndpoint := codexOAuthResponsesEndpoint
	codexOAuthResponsesEndpoint = upstream.URL
	t.Cleanup(func() { codexOAuthResponsesEndpoint = oldEndpoint })

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{
		Name:                      "oauth-ws",
		BaseURL:                   "https://chatgpt.com/backend-api/codex/responses",
		ServiceType:               "openai-oauth",
		Status:                    "active",
		ResponsesWebSocketEnabled: true,
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
	router.GET("/v1/responses", ResponsesWebSocketHandler(nil, cfgManager, sch, reqLogManager, nil, nil, nil))
	bridge := httptest.NewServer(router)
	defer bridge.Close()

	parsed, err := url.Parse("ws" + strings.TrimPrefix(bridge.URL, "http"))
	if err != nil {
		t.Fatalf("parse bridge ws url: %v", err)
	}
	parsed.Path = "/v1/responses"

	conn, resp, err := websocket.DefaultDialer.Dial(parsed.String(), http.Header{
		"OpenAI-Beta": []string{"responses_websockets=2026-02-06"},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	select {
	case <-upstreamConnected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream websocket connection")
	}

	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("client read succeeded, want proxy error after abrupt upstream close")
	}

	deadline := time.After(time.Second)
	for {
		got := sch.GetResponsesMetricsManager().GetMetrics(0)
		if got != nil && got.FailureCount == 1 {
			if got.SuccessCount != 0 {
				t.Fatalf("success count = %d, want 0", got.SuccessCount)
			}
			if len(got.RecentCalls) != 1 || got.RecentCalls[0].Success {
				t.Fatalf("recent calls = %+v, want one failure", got.RecentCalls)
			}
			break
		}

		select {
		case <-deadline:
			t.Fatalf("metrics after fallback = %+v, want one failure and zero successes", got)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 0 {
		t.Fatalf("request log count = %d, want 0 before first response.create", len(recent.Requests))
	}
}

func TestResponsesWebSocketResponseDoneRestoresAsRecentCallSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newResponsesWebSocketTestConfigManager(t)
	sch := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		session.NewTraceAffinityManager(),
	)
	reqLogManager := newTestRequestLogManager(t)
	tracker := &responsesWebSocketLogTracker{
		manager:            reqLogManager,
		scheduler:          sch,
		upstream:           &config.UpstreamConfig{Name: "oauth-ws", ServiceType: "openai-oauth"},
		startTime:          time.Now().Add(-time.Second),
		header:             http.Header{},
		channelID:          4,
		channelName:        "oauth-ws",
		firstTokenDetector: streamDetectorForServiceType("openai-oauth"),
	}

	tracker.observeClientMessage([]byte(`{"type":"response.create","model":"gpt-5","input":[]}`))
	tracker.observeUpstreamMessage([]byte(`{"type":"response.done","response":{"id":"resp_done","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`))
	tracker.finish(nil)

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("request log count = %d, want 1", len(recent.Requests))
	}
	if recent.Requests[0].Status != requestlog.StatusCompleted {
		t.Fatalf("request log status = %q, want %q", recent.Requests[0].Status, requestlog.StatusCompleted)
	}
	if recent.Requests[0].InputTokens != 5 || recent.Requests[0].OutputTokens != 1 {
		t.Fatalf("request log usage = input:%d output:%d, want input:5 output:1", recent.Requests[0].InputTokens, recent.Requests[0].OutputTokens)
	}

	calls, err := reqLogManager.GetRecentChannelCalls(20)
	if err != nil {
		t.Fatalf("GetRecentChannelCalls failed: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("recent channel calls count = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.Endpoint != "/v1/responses" || call.ChannelID != 4 {
		t.Fatalf("unexpected recent call target: %+v", call)
	}
	if !call.Success || call.HTTPStatus != http.StatusOK {
		t.Fatalf("recent call should be success with HTTP 200, got %+v", call)
	}

	metrics := sch.GetResponsesMetricsManager().GetMetrics(4)
	if metrics == nil {
		t.Fatalf("expected metrics for channel 4")
	}
	if metrics.SuccessCount != 1 || metrics.FailureCount != 0 {
		t.Fatalf("metrics = success:%d failure:%d, want success:1 failure:0", metrics.SuccessCount, metrics.FailureCount)
	}
	if len(metrics.RecentCalls) != 1 || !metrics.RecentCalls[0].Success {
		t.Fatalf("metrics recent calls = %+v, want one success", metrics.RecentCalls)
	}
}
