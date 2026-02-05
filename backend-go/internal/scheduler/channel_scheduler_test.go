package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/session"
)

func TestSelectChannel_AllowsOpenAIOAuthResponsesChannelWithoutAPIKeys(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := config.Config{
		Upstream:                 []config.UpstreamConfig{},
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		ResponsesUpstream:        []config.UpstreamConfig{},
		GeminiUpstream:           []config.UpstreamConfig{},
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-1",
		Name:        "Codex",
		ServiceType: "openai-oauth",
		Status:      "active",
		OAuthTokens: &config.OAuthTokens{
			AccessToken:  "access-token",
			AccountID:    "account-id",
			RefreshToken: "refresh-token",
		},
	})

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

	s := NewChannelScheduler(cfgManager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), traceAffinity)

	selection, err := s.SelectChannel(context.Background(), "u", make(map[int]bool), true, []string{"resp-1"}, "gpt-4o")
	if err != nil {
		t.Fatalf("SelectChannel returned error: %v", err)
	}
	if selection == nil || selection.Upstream == nil {
		t.Fatalf("SelectChannel returned nil selection")
	}
	if selection.Upstream.ServiceType != "openai-oauth" {
		t.Fatalf("expected serviceType=openai-oauth, got %q", selection.Upstream.ServiceType)
	}
	if selection.ChannelIndex != 0 {
		t.Fatalf("expected channelIndex=0, got %d", selection.ChannelIndex)
	}
}
