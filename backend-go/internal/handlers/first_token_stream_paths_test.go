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
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

func TestHandleStreamResponse_RecordsFirstTokenForMessagesPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-150 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "claude", "claude-sonnet-4", true, 1, "messages-1")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":true}`))

	sseBody := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":5,"output_tokens":1}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":7}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(sseBody)),
	}

	upstream := &config.UpstreamConfig{
		Index:       1,
		Name:        "messages-1",
		ServiceType: "claude",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	handleStreamResponse(
		c,
		resp,
		providers.GetProvider("claude"),
		envCfg,
		cfgManager,
		startTime,
		upstream,
		reqLogManager,
		requestLogID,
		"claude-sonnet-4",
		nil,
		1,
		"messages-1",
	)

	got, err := reqLogManager.GetByID(requestLogID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected request log")
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime to be captured for messages stream path")
	}
	if got.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
	}
}

func TestHandleResponsesSuccess_StreamRecordsFirstToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)
	envCfg := &config.EnvConfig{LogLevel: "error"}
	upstream := &config.UpstreamConfig{
		Index:       2,
		Name:        "responses-2",
		ServiceType: "responses",
	}

	tests := []struct {
		name    string
		stream  string
		wantMsg string
	}{
		{
			name: "detects from output_text.delta",
			stream: strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"hello"}`,
				`data: {"type":"response.completed"}`,
				"data: [DONE]",
				"",
			}, "\n"),
			wantMsg: "delta",
		},
		{
			name: "detects from output_text.done fallback",
			stream: strings.Join([]string{
				`data: {"type":"response.output_text.done","text":"final answer"}`,
				`data: {"type":"response.completed"}`,
				"data: [DONE]",
				"",
			}, "\n"),
			wantMsg: "done fallback",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			startTime := time.Now().Add(-120 * time.Millisecond)
			requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/responses", "responses", "gpt-5", true, 2, "responses-2")

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"stream":true}`))

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(strings.NewReader(tc.stream)),
			}

			handleResponsesSuccess(
				c,
				resp,
				nil,
				upstream,
				envCfg,
				cfgManager,
				nil,
				startTime,
				&types.ResponsesRequest{Model: "gpt-5", Stream: true},
				nil,
				reqLogManager,
				requestLogID,
				nil,
				2,
				"responses-2",
			)

			got, err := reqLogManager.GetByID(requestLogID)
			if err != nil {
				t.Fatalf("GetByID failed: %v", err)
			}
			if got == nil {
				t.Fatalf("expected request log")
			}
			if got.FirstTokenTime == nil {
				t.Fatalf("expected firstTokenTime captured via %s", tc.wantMsg)
			}
			if got.FirstTokenDurationMs <= 0 {
				t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
			}
		})
	}
}

func TestHandleGeminiSuccess_StreamRecordsFirstToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-120 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/gemini/models/generateContent", "gemini", "gemini-2.5-pro", true, 3, "gemini-3")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/gemini/models/generateContent", strings.NewReader(`{}`))

	streamBody := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"hello gemini"}]}}]}`,
		`data: {"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":5,"totalTokenCount":8},"modelVersion":"gemini-2.5-pro"}`,
		"",
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(streamBody)),
	}

	upstream := &config.UpstreamConfig{
		Index:       3,
		Name:        "gemini-3",
		ServiceType: "gemini",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	if failoverErr := handleGeminiSuccess(
		c,
		resp,
		upstream,
		envCfg,
		cfgManager,
		true,
		startTime,
		"gemini-2.5-pro",
		reqLogManager,
		requestLogID,
		nil,
		3,
		"gemini-3",
	); failoverErr != nil {
		t.Fatalf("expected nil failover error, got %+v", failoverErr)
	}

	got, err := reqLogManager.GetByID(requestLogID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected request log")
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime captured for gemini stream path")
	}
	if got.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
	}
}

func TestChatStreamAndFinalize_PropagatesFirstTokenToRequestLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqLogManager := newTestRequestLogManager(t)
	startTime := time.Now().Add(-150 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/chat/completions", "openai_chat", "gpt-5", true, 4, "chat-4")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	detector := utils.NewFirstTokenDetector(utils.FirstTokenProtocolOpenAIChatSSE)
	var capture bytes.Buffer
	firstTokenTime, err := streamReaderToClient(
		c,
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello chat\"}}]}\n"),
		&capture,
		detector,
	)
	if err != nil {
		t.Fatalf("streamReaderToClient failed: %v", err)
	}
	if firstTokenTime == nil {
		t.Fatalf("expected firstTokenTime from chat stream helper")
	}

	upstream := &config.UpstreamConfig{
		Index:       4,
		Name:        "chat-4",
		ServiceType: "openai_chat",
	}
	finalizeChatSuccessLog(reqLogManager, requestLogID, startTime, "gpt-5", upstream, http.StatusOK, firstTokenTime)

	got, err := reqLogManager.GetByID(requestLogID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected request log")
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime propagated by finalizeChatSuccessLog")
	}
	if got.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
	}
}

func TestTryMessagesChannelWithAllKeys_RetryWaitUsesNewPendingInitialTime(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var callCount int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		if current == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"temporary failure"}}`)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_retry","model":"claude-sonnet-4-20250514","usage":{"input_tokens":5,"output_tokens":1}}}`+"\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"retry success"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer upstreamServer.Close()

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		Failover: config.FailoverConfig{
			Rules: []config.FailoverRule{
				{
					ErrorCodes: "500",
					ActionChain: []config.ActionStep{
						{Action: config.ActionRetry, WaitSeconds: 0, MaxAttempts: 1},
					},
				},
			},
		},
		Upstream: []config.UpstreamConfig{
			{
				ID:          "msg-retry",
				Name:        "messages-retry",
				BaseURL:     upstreamServer.URL,
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"sk-retry"},
				Index:       0,
			},
		},
		LoadBalance: "failover",
	})

	upstream, err := cfgManager.GetCurrentUpstream()
	if err != nil {
		t.Fatalf("GetCurrentUpstream failed: %v", err)
	}

	reqLogManager := newTestRequestLogManager(t)
	initialStart := time.Now().Add(-300 * time.Millisecond)
	initialLogID := addPendingLogForTest(t, reqLogManager, initialStart, "/v1/messages", "claude", "claude-sonnet-4", true, 0, "messages-retry")

	bodyBytes := []byte(`{"model":"claude-sonnet-4","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(bodyBytes, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request: %v", err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}
	startTime := initialStart
	failoverTracker := config.NewFailoverTracker()

	success, failoverErr, finalLogID := tryChannelWithAllKeys(
		c,
		envCfg,
		cfgManager,
		upstream,
		bodyBytes,
		claudeReq,
		&startTime,
		reqLogManager,
		initialLogID,
		nil,
		failoverTracker,
		0,
		"messages-retry",
		"user-test",
		"",
		nil,
	)

	if !success {
		t.Fatalf("expected success after retry_wait, got failoverErr=%+v", failoverErr)
	}
	if finalLogID == initialLogID {
		t.Fatalf("expected retry_wait flow to create a new pending log id")
	}
	if atomic.LoadInt32(&callCount) < 2 {
		t.Fatalf("expected at least two upstream calls (retry), got %d", callCount)
	}

	initialLog, err := reqLogManager.GetByID(initialLogID)
	if err != nil {
		t.Fatalf("GetByID(initial) failed: %v", err)
	}
	finalLog, err := reqLogManager.GetByID(finalLogID)
	if err != nil {
		t.Fatalf("GetByID(final) failed: %v", err)
	}
	if initialLog == nil || finalLog == nil {
		t.Fatalf("expected both initial and final request logs")
	}

	initialRecent := mustGetRecentLogByID(t, reqLogManager, initialLogID)
	finalRecent := mustGetRecentLogByID(t, reqLogManager, finalLogID)
	if initialRecent.Status != requestlog.StatusRetryWait {
		t.Fatalf("expected initial log status retry_wait, got %s", initialRecent.Status)
	}
	if finalRecent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected final log status completed, got %s", finalRecent.Status)
	}
	if !finalLog.InitialTime.After(initialLog.InitialTime) {
		t.Fatalf("expected final log InitialTime to be newer after retry_wait pending recreation")
	}
	if finalLog.FirstTokenTime == nil || finalLog.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected final log to capture first-token timing, got firstTokenTime=%v firstTokenDurationMs=%d", finalLog.FirstTokenTime, finalLog.FirstTokenDurationMs)
	}
	if !startTime.Equal(finalLog.InitialTime) {
		t.Fatalf("expected startTime baseline to be updated to final pending initial time")
	}
}

func newTestConfigManager(t *testing.T) *config.ConfigManager {
	t.Helper()
	return newTestConfigManagerWithConfig(t, config.Config{
		LoadBalance:          "failover",
		ResponsesLoadBalance: "failover",
		GeminiLoadBalance:    "failover",
	})
}

func newTestConfigManagerWithConfig(t *testing.T, cfg config.Config) *config.ConfigManager {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager failed: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })
	return cfgManager
}

func newTestRequestLogManager(t *testing.T) *requestlog.Manager {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "request_logs.db")
	manager, err := requestlog.NewManager(dbPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func addPendingLogForTest(t *testing.T, manager *requestlog.Manager, startTime time.Time, endpoint, providerType, model string, stream bool, channelID int, channelName string) string {
	t.Helper()

	record := &requestlog.RequestLog{
		Status:       requestlog.StatusPending,
		InitialTime:  startTime,
		Type:         providerType,
		ProviderName: channelName,
		Model:        model,
		Stream:       stream,
		Endpoint:     endpoint,
		ChannelID:    channelID,
		ChannelName:  channelName,
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending log: %v", err)
	}
	return record.ID
}

func mustGetRecentLogByID(t *testing.T, manager *requestlog.Manager, id string) requestlog.RequestLog {
	t.Helper()

	recent, err := manager.GetRecent(&requestlog.RequestLogFilter{Limit: 100})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	for _, record := range recent.Requests {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("record %s not found in recent logs", id)
	return requestlog.RequestLog{}
}
