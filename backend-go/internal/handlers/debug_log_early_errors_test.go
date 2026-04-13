package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

func TestHandleSingleChannelProxy_NoUpstreamStillPersistsDebugLogWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		LoadBalance: "failover",
		DebugLog: config.DebugLogConfig{
			Enabled:        true,
			RetentionHours: 24,
			MaxBodySize:    1024 * 1024,
		},
	})
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-50 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "claude", "claude-sonnet-4", false, -1, "")

	bodyBytes := []byte(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	StoreDebugRequestData(c, bodyBytes)

	handleSingleChannelProxy(
		c,
		&config.EnvConfig{LogLevel: "error"},
		cfgManager,
		nil,
		bodyBytes,
		types.ClaudeRequest{Model: "claude-sonnet-4"},
		startTime,
		reqLogManager,
		requestLogID,
		nil,
		nil,
		nil,
		nil,
		"",
		"",
		nil,
		nil,
	)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}

	var debugEntry *requestlog.DebugLogEntry
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var err error
		debugEntry, err = reqLogManager.GetDebugLog(requestLogID)
		if err != nil {
			t.Fatalf("GetDebugLog failed: %v", err)
		}
		if debugEntry != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if debugEntry == nil {
		t.Fatalf("expected debug log for early no-upstream error")
	}
	if debugEntry.ResponseStatus != http.StatusServiceUnavailable {
		t.Fatalf("expected debug response status 503, got %d", debugEntry.ResponseStatus)
	}
	if debugEntry.RequestBody == "" {
		t.Fatalf("expected request body to be captured in debug log")
	}
}
