package handlers

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/gin-gonic/gin"
)

var hookPricingInitOnce sync.Once

func ensureHookTestPricingManager(t *testing.T) {
	t.Helper()

	hookPricingInitOnce.Do(func() {
		cfg := pricing.PricingConfig{
			Currency: "USD",
			Models: map[string]pricing.ModelPricing{
				"claude-opus-4-6": {
					InputPrice:  15,
					OutputPrice: 75,
				},
			},
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal pricing config: %v", err)
		}

		tmpDir, err := os.MkdirTemp("", "hook-pricing-*")
		if err != nil {
			t.Fatalf("failed to create temp pricing dir: %v", err)
		}

		path := filepath.Join(tmpDir, "pricing.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("failed to write pricing config: %v", err)
		}

		if _, err := pricing.InitManager(path); err != nil {
			t.Fatalf("failed to init pricing manager: %v", err)
		}
	})
}

func newHookLogTestHandler(t *testing.T) (*RequestLogHandler, *requestlog.Manager) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "request_logs.db")
	mgr, err := requestlog.NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create request log manager: %v", err)
	}
	t.Cleanup(func() {
		_ = mgr.Close()
	})

	return NewRequestLogHandler(mgr), mgr
}

func performHookIngestRequest(t *testing.T, router *gin.Engine, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/logs/hooks/anthropic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestIngestAnthropicHookLog_MinimalPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mgr := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId":    "session-1:assistant-1",
		"status":       "completed",
		"model":        "claude-opus-4-6",
		"inputTokens":  12,
		"outputTokens": 3,
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ID      string `json:"id"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !resp.Created {
		t.Fatalf("expected created=true")
	}
	if resp.ID != "session-1:assistant-1" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}

	rec, err := mgr.GetByID(resp.ID)
	if err != nil {
		t.Fatalf("failed to get record by id: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected record to exist")
	}

	if rec.Type != "claude" {
		t.Fatalf("expected type claude, got %s", rec.Type)
	}
	if rec.Endpoint != "/v1/messages" {
		t.Fatalf("expected endpoint /v1/messages, got %s", rec.Endpoint)
	}
	if rec.ChannelID != 1 {
		t.Fatalf("expected channelId=1, got %d", rec.ChannelID)
	}
	if rec.ChannelUID != "oauth:default" {
		t.Fatalf("expected channelUid oauth:default, got %s", rec.ChannelUID)
	}
	if rec.ChannelName != "Anthropic OAuth" {
		t.Fatalf("expected channelName Anthropic OAuth, got %s", rec.ChannelName)
	}
	if rec.HTTPStatus != 200 {
		t.Fatalf("expected httpStatus 200, got %d", rec.HTTPStatus)
	}
	if rec.TotalTokens != 15 {
		t.Fatalf("expected totalTokens 15, got %d", rec.TotalTokens)
	}

	list, err := mgr.GetRecent(&requestlog.RequestLogFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list recent logs: %v", err)
	}
	if len(list.Requests) == 0 {
		t.Fatalf("expected at least one log in GetRecent")
	}
	if list.Requests[0].ProviderName != "anthropic-oauth" {
		t.Fatalf("expected providerName anthropic-oauth in GetRecent, got %s", list.Requests[0].ProviderName)
	}
	if list.Requests[0].Status != "completed" {
		t.Fatalf("expected status completed in GetRecent, got %s", list.Requests[0].Status)
	}
}

func TestIngestAnthropicHookLog_IdempotentDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId": "session-2:assistant-1",
		"status":    "completed",
		"model":     "claude-opus-4-6",
	}

	w1 := performHookIngestRequest(t, r, payload)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d, body=%s", w1.Code, w1.Body.String())
	}

	var resp1 struct {
		Created bool `json:"created"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to parse first response: %v", err)
	}
	if !resp1.Created {
		t.Fatalf("expected first request created=true")
	}

	w2 := performHookIngestRequest(t, r, payload)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d, body=%s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		Created bool `json:"created"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to parse second response: %v", err)
	}
	if resp2.Created {
		t.Fatalf("expected second request created=false")
	}
}

func TestIngestAnthropicHookLog_InvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId": "session-3:assistant-1",
		"status":    "invalid_status",
		"model":     "claude-opus-4-6",
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestIngestAnthropicHookLog_MissingRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"status": "completed",
		"model":  "claude-opus-4-6",
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestIngestAnthropicHookLog_InvalidTimestamp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId":   "session-4:assistant-1",
		"status":      "completed",
		"model":       "claude-opus-4-6",
		"initialTime": "not-a-time",
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestIngestAnthropicHookLog_PendingNormalization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mgr := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId":    "session-5:assistant-1",
		"status":       "pending",
		"model":        "claude-opus-4-6",
		"durationMs":   1234,
		"httpStatus":   200,
		"completeTime": "2026-02-16T03:10:04Z",
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	rec, err := mgr.GetByID("session-5:assistant-1")
	if err != nil {
		t.Fatalf("failed to get record by id: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected record to exist")
	}
	if rec.DurationMs != 0 {
		t.Fatalf("expected pending duration 0, got %d", rec.DurationMs)
	}
	if rec.HTTPStatus != 0 {
		t.Fatalf("expected pending httpStatus 0, got %d", rec.HTTPStatus)
	}

	list, err := mgr.GetRecent(&requestlog.RequestLogFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list recent logs: %v", err)
	}
	if len(list.Requests) == 0 {
		t.Fatalf("expected at least one log in GetRecent")
	}
	if list.Requests[0].Status != "pending" {
		t.Fatalf("expected status pending in GetRecent, got %s", list.Requests[0].Status)
	}
}

func TestIngestAnthropicHookLog_UsesAPIKeyIDFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mgr := newHookLogTestHandler(t)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyAPIKeyID, int64(0))
		c.Next()
	})
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId": "session-6:assistant-1",
		"status":    "completed",
		"model":     "claude-opus-4-6",
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	list, err := mgr.GetRecent(&requestlog.RequestLogFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list recent logs: %v", err)
	}
	if len(list.Requests) == 0 {
		t.Fatalf("expected at least one log in GetRecent")
	}
	if list.Requests[0].APIKeyID == nil {
		t.Fatalf("expected apiKeyId to be set")
	}
	if *list.Requests[0].APIKeyID != 0 {
		t.Fatalf("expected apiKeyId 0 (master), got %d", *list.Requests[0].APIKeyID)
	}
}

func TestIngestAnthropicHookLog_AutoCalculatesPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ensureHookTestPricingManager(t)

	h, mgr := newHookLogTestHandler(t)
	r := gin.New()
	r.POST("/api/logs/hooks/anthropic", h.IngestAnthropicHookLog)

	payload := map[string]interface{}{
		"requestId":                "session-7:assistant-1",
		"status":                   "completed",
		"model":                    "claude-opus-4-6",
		"inputTokens":              1000,
		"outputTokens":             200,
		"cacheCreationInputTokens": 0,
		"cacheReadInputTokens":     0,
	}

	w := performHookIngestRequest(t, r, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	list, err := mgr.GetRecent(&requestlog.RequestLogFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list recent logs: %v", err)
	}
	if len(list.Requests) == 0 {
		t.Fatalf("expected at least one log in GetRecent")
	}

	rec := list.Requests[0]
	const expected = 0.03 // 1000*15/1e6 + 200*75/1e6
	if math.Abs(rec.Price-expected) > 1e-9 {
		t.Fatalf("expected price %.8f, got %.8f", expected, rec.Price)
	}
	if rec.InputCost <= 0 {
		t.Fatalf("expected inputCost > 0, got %.8f", rec.InputCost)
	}
	if rec.OutputCost <= 0 {
		t.Fatalf("expected outputCost > 0, got %.8f", rec.OutputCost)
	}
}
