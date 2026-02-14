package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
)

func createTestConfigManager(t *testing.T, cfg config.Config) *config.ConfigManager {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	return cfgManager
}

func TestGetCurrentMessagesChannel_NormalChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "ch-normal",
				Name:        "Claude-Normal",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-1"},
			},
		},
		LoadBalance:              "failover",
		ResponsesUpstream:        []config.UpstreamConfig{},
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []config.UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfgManager := createTestConfigManager(t, cfg)

	r := gin.New()
	r.GET("/api/messages/channels/current", GetCurrentMessagesChannel(cfgManager, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ChannelName string `json:"channelName"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.ChannelName != "Claude-Normal" {
		t.Fatalf("expected channelName=Claude-Normal, got %q", resp.ChannelName)
	}
}

func TestGetCurrentMessagesChannel_CompositeResolvesOpusTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "ch-composite",
				Name:        "Composite-Primary",
				ServiceType: "composite",
				Status:      "active",
				CompositeMappings: []config.CompositeMapping{
					{Pattern: "haiku", TargetChannelID: "ch-haiku"},
					{Pattern: "sonnet", TargetChannelID: "ch-sonnet"},
					{Pattern: "opus", TargetChannelID: "ch-opus"},
				},
			},
			{
				ID:          "ch-haiku",
				Name:        "Haiku-Target",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-h"},
			},
			{
				ID:          "ch-sonnet",
				Name:        "Sonnet-Target",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-s"},
			},
			{
				ID:          "ch-opus",
				Name:        "Opus-Target",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-o"},
			},
		},
		LoadBalance:              "failover",
		ResponsesUpstream:        []config.UpstreamConfig{},
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []config.UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfgManager := createTestConfigManager(t, cfg)

	r := gin.New()
	r.GET("/api/messages/channels/current", GetCurrentMessagesChannel(cfgManager, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ChannelName string `json:"channelName"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.ChannelName != "Opus-Target" {
		t.Fatalf("expected channelName=Opus-Target, got %q", resp.ChannelName)
	}
}

func TestGetCurrentMessagesChannel_NoChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream:                 []config.UpstreamConfig{},
		LoadBalance:              "failover",
		ResponsesUpstream:        []config.UpstreamConfig{},
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []config.UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	})

	r := gin.New()
	r.GET("/api/messages/channels/current", GetCurrentMessagesChannel(cfgManager, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestGetCurrentMessagesChannel_MultiChannelMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "ch-1",
				Name:        "Channel-1",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-1"},
			},
			{
				ID:          "ch-2",
				Name:        "Channel-2",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-2"},
			},
		},
		LoadBalance:              "failover",
		ResponsesUpstream:        []config.UpstreamConfig{},
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []config.UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfgManager := createTestConfigManager(t, cfg)

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)
	sch := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	r := gin.New()
	r.GET("/api/messages/channels/current", GetCurrentMessagesChannel(cfgManager, sch))

	req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d, body=%s", w.Code, w.Body.String())
	}
}
