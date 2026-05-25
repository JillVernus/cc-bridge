package handlers

import (
	"bytes"
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

func TestSetChannelStatus_TakenDownChannelClearsTraceAffinity(t *testing.T) {
	cfgManager, sch := newStatusAffinityTestScheduler(t)
	sch.SetTraceAffinity("client-1", 0)

	router := gin.New()
	router.PATCH("/api/channels/:id/status", SetChannelStatus(cfgManager, sch))

	body := bytes.NewBufferString(`{"status":"suspended"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/channels/0/status", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if preferred, ok := sch.GetTraceAffinityManager().GetPreferredChannel("client-1"); ok {
		t.Fatalf("expected affinity for suspended channel to be cleared, still preferred channel %d", preferred)
	}
}

func TestSetResponsesChannelStatus_TakenDownChannelClearsTraceAffinity(t *testing.T) {
	cfgManager, sch := newStatusAffinityTestScheduler(t)
	sch.SetTraceAffinity("client-1", 0)

	router := gin.New()
	router.PATCH("/api/responses/channels/:id/status", SetResponsesChannelStatus(cfgManager, sch))

	body := bytes.NewBufferString(`{"status":"disabled"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/responses/channels/0/status", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if preferred, ok := sch.GetTraceAffinityManager().GetPreferredChannel("client-1"); ok {
		t.Fatalf("expected affinity for disabled responses channel to be cleared, still preferred channel %d", preferred)
	}
}

func newStatusAffinityTestScheduler(t *testing.T) (*config.ConfigManager, *scheduler.ChannelScheduler) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := config.Config{
		LoadBalance:          "failover",
		ResponsesLoadBalance: "failover",
		Upstream: []config.UpstreamConfig{
			{
				ID:          "msg-0",
				Name:        "Messages 0",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-0"},
			},
			{
				ID:          "msg-1",
				Name:        "Messages 1",
				ServiceType: "claude",
				Status:      "active",
				APIKeys:     []string{"key-1"},
			},
		},
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-0",
				Name:        "Responses 0",
				ServiceType: "responses",
				Status:      "active",
				APIKeys:     []string{"resp-key-0"},
			},
			{
				ID:          "resp-1",
				Name:        "Responses 1",
				ServiceType: "responses",
				Status:      "active",
				APIKeys:     []string{"resp-key-1"},
			},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, b, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager, err := config.NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	traceAffinity := session.NewTraceAffinityManager()
	t.Cleanup(traceAffinity.Stop)

	sch := scheduler.NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)
	return cfgManager, sch
}
