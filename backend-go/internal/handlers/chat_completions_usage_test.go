package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
)

func TestChatCompletionsHandler_RecordsNonStreamOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ensureChatTestPricingManager(t)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal upstream request body: %v", err)
		}
		if payload["model"] != "deepseek-v4-flash" {
			t.Fatalf("expected redirected model deepseek-v4-flash, got %#v", payload["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"3e1d3872-997e-4b9b-9d2c-733279b456f1","object":"chat.completion","created":1778721028,"model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"OK","reasoning_content":"reasoning"},"logprobs":null,"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9},"system_fingerprint":"fp_8b330d02d0_prod0820_fp8_kvcache_20260402"}`)
	}))
	defer upstreamServer.Close()

	cfgManager := createTestConfigManager(t, config.Config{
		ChatUpstream: []config.UpstreamConfig{
			{
				ID:          "chat-usage",
				Name:        "Chat Usage",
				BaseURL:     upstreamServer.URL,
				ServiceType: "openai_chat",
				Status:      "active",
				APIKeys:     []string{"sk-chat"},
				Index:       0,
			},
		},
		ChatLoadBalance: "failover",
	})

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	channelScheduler := scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		traceAffinity,
	)
	reqLogManager := newTestRequestLogManager(t)
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "test-proxy-key",
		RequestTimeout: 30000,
		LogLevel:       "error",
	}

	router := gin.New()
	router.POST("/v1/chat/completions", ChatCompletionsHandlerWithAPIKey(
		envCfg,
		cfgManager,
		channelScheduler,
		reqLogManager,
		nil,
		nil,
		nil,
		nil,
	))

	requestBody := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"please only reply OK"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer test-proxy-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	recent, err := reqLogManager.GetRecent(nil)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("expected 1 request log, got %d", len(recent.Requests))
	}
	got := recent.Requests[0]
	if got.InputTokens != 8 {
		t.Fatalf("expected input tokens 8, got %d", got.InputTokens)
	}
	if got.OutputTokens != 1 {
		t.Fatalf("expected output tokens 1, got %d", got.OutputTokens)
	}
	if got.TotalTokens != 9 {
		t.Fatalf("expected total tokens 9, got %d", got.TotalTokens)
	}
	if got.ResponseModel != "deepseek-v4-flash" {
		t.Fatalf("expected response model deepseek-v4-flash, got %q", got.ResponseModel)
	}
	expectedInputCost := float64(8) * 1.0 / 1_000_000
	expectedOutputCost := float64(1) * 2.0 / 1_000_000
	expectedPrice := expectedInputCost + expectedOutputCost
	if math.Abs(got.InputCost-expectedInputCost) > 1e-12 {
		t.Fatalf("expected input cost %.12f, got %.12f", expectedInputCost, got.InputCost)
	}
	if math.Abs(got.OutputCost-expectedOutputCost) > 1e-12 {
		t.Fatalf("expected output cost %.12f, got %.12f", expectedOutputCost, got.OutputCost)
	}
	if math.Abs(got.Price-expectedPrice) > 1e-12 {
		t.Fatalf("expected price %.12f, got %.12f", expectedPrice, got.Price)
	}
}

func ensureChatTestPricingManager(t *testing.T) {
	t.Helper()

	cfg := pricing.PricingConfig{
		Currency: "USD",
		Models: map[string]pricing.ModelPricing{
			"claude-opus-4-6": {
				InputPrice:  15,
				OutputPrice: 75,
			},
			"deepseek-v4-flash": {
				InputPrice:  1,
				OutputPrice: 2,
			},
		},
	}

	if pm := pricing.GetManager(); pm != nil {
		pm.UpdateConfigFromDB(cfg)
		return
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal pricing config: %v", err)
	}
	tmpDir, err := os.MkdirTemp("", "chat-pricing-*")
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
}
