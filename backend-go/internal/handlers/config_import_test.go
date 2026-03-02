package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
)

func newTestScheduler(t *testing.T, cfgManager *config.ConfigManager) *scheduler.ChannelScheduler {
	t.Helper()

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	return scheduler.NewChannelScheduler(
		cfgManager,
		metrics.NewMetricsManager(),
		metrics.NewMetricsManager(),
		traceAffinity,
	)
}

func TestAddUpstream_ImportFromResponsesChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream:          []config.UpstreamConfig{},
		LoadBalance:       "failover",
		GeminiUpstream:    []config.UpstreamConfig{},
		GeminiLoadBalance: "failover",
		ChatUpstream:      []config.UpstreamConfig{},
		ChatLoadBalance:   "failover",
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-src",
				Name:        "Codex Source",
				ServiceType: "responses",
				BaseURL:     "https://api.openai.com/v1",
				APIKeys:     []string{"sk-resp-1", "sk-resp-2"},
			},
		},
		ResponsesLoadBalance: "failover",
	})

	r := gin.New()
	r.POST("/api/channels", AddUpstream(cfgManager))

	body, err := json.Marshal(map[string]any{
		"name":                         "Messages via imported codex",
		"serviceType":                  "responses",
		"baseUrl":                      "https://should-be-overwritten.example",
		"apiKeys":                      []string{"manual-should-not-be-used"},
		"importFromResponsesChannelId": "resp-src",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/channels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	upstreams := cfgManager.GetConfig().Upstream
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}

	if upstreams[0].BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected imported baseUrl, got %q", upstreams[0].BaseURL)
	}
	if !slices.Equal(upstreams[0].APIKeys, []string{"sk-resp-1", "sk-resp-2"}) {
		t.Fatalf("expected imported api keys, got %#v", upstreams[0].APIKeys)
	}
}

func TestAddUpstream_ImportFromResponsesChannelRejectsNonResponsesServiceType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream:    []config.UpstreamConfig{},
		LoadBalance: "failover",
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-src",
				Name:        "Codex Source",
				ServiceType: "responses",
				BaseURL:     "https://api.openai.com/v1",
				APIKeys:     []string{"sk-resp-1"},
			},
		},
		ResponsesLoadBalance: "failover",
		GeminiUpstream:       []config.UpstreamConfig{},
		GeminiLoadBalance:    "failover",
		ChatUpstream:         []config.UpstreamConfig{},
		ChatLoadBalance:      "failover",
	})

	r := gin.New()
	r.POST("/api/channels", AddUpstream(cfgManager))

	body, err := json.Marshal(map[string]any{
		"name":                         "not-allowed",
		"serviceType":                  "claude",
		"baseUrl":                      "https://api.anthropic.com",
		"apiKeys":                      []string{"k"},
		"importFromResponsesChannelId": "resp-src",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/channels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateUpstream_ImportFromResponsesChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "msg-1",
				Name:        "Messages responses",
				ServiceType: "responses",
				BaseURL:     "https://old.example/v1",
				APIKeys:     []string{"old-key"},
				Status:      "active",
			},
		},
		LoadBalance: "failover",
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-src",
				Name:        "Codex Source",
				ServiceType: "responses",
				BaseURL:     "https://api.openai.com/v1",
				APIKeys:     []string{"sk-resp-1", "sk-resp-2"},
				Status:      "active",
			},
		},
		ResponsesLoadBalance: "failover",
		GeminiUpstream:       []config.UpstreamConfig{},
		GeminiLoadBalance:    "failover",
		ChatUpstream:         []config.UpstreamConfig{},
		ChatLoadBalance:      "failover",
	})
	sch := newTestScheduler(t, cfgManager)

	r := gin.New()
	r.PUT("/api/channels/:id", UpdateUpstream(cfgManager, sch))

	body, err := json.Marshal(map[string]any{
		"importFromResponsesChannelId": "resp-src",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/channels/0", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	got := cfgManager.GetConfig().Upstream
	if len(got) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(got))
	}

	if got[0].BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected imported baseUrl, got %q", got[0].BaseURL)
	}
	if !slices.Equal(got[0].APIKeys, []string{"sk-resp-1", "sk-resp-2"}) {
		t.Fatalf("expected imported api keys, got %#v", got[0].APIKeys)
	}
}

func TestUpdateUpstream_ImportFromResponsesChannelInvalidIndexReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "msg-1",
				Name:        "Messages responses",
				ServiceType: "responses",
				BaseURL:     "https://old.example/v1",
				APIKeys:     []string{"old-key"},
				Status:      "active",
			},
		},
		LoadBalance: "failover",
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-src",
				Name:        "Codex Source",
				ServiceType: "responses",
				BaseURL:     "https://api.openai.com/v1",
				APIKeys:     []string{"sk-resp-1"},
				Status:      "active",
			},
		},
		ResponsesLoadBalance: "failover",
		GeminiUpstream:       []config.UpstreamConfig{},
		GeminiLoadBalance:    "failover",
		ChatUpstream:         []config.UpstreamConfig{},
		ChatLoadBalance:      "failover",
	})
	sch := newTestScheduler(t, cfgManager)

	r := gin.New()
	r.PUT("/api/channels/:id", UpdateUpstream(cfgManager, sch))

	body, err := json.Marshal(map[string]any{
		"importFromResponsesChannelId": "resp-src",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/channels/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d, body=%s", w.Code, w.Body.String())
	}
}
