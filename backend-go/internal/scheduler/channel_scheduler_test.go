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

func TestCompositeMixedPoolFailover_AvoidsCrossPoolIndexCollision(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}

	cfg.Upstream = append(cfg.Upstream,
		config.UpstreamConfig{
			ID:          "msg-0",
			Name:        "Messages Target",
			ServiceType: "claude",
			Status:      "active",
			Priority:    10,
			APIKeys:     []string{"msg-key"},
		},
		config.UpstreamConfig{
			ID:          "cmp-1",
			Name:        "Composite",
			ServiceType: "composite",
			Status:      "active",
			Priority:    0,
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetPool:      config.CompositeTargetPoolResponses,
					TargetChannelID: "resp-0",
					FailoverTargets: []config.CompositeTargetRef{
						{
							Pool:      config.CompositeTargetPoolMessages,
							ChannelID: "msg-0",
						},
					},
				},
			},
		},
	)
	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream, config.UpstreamConfig{
		ID:          "resp-0",
		Name:        "Responses Target",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"resp-key"},
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
	failedChannels := make(map[int]bool)

	firstSelection, err := s.SelectChannel(context.Background(), "u", failedChannels, false, []string{"cmp-1", "msg-0"}, "claude-3-haiku")
	if err != nil {
		t.Fatalf("SelectChannel returned error: %v", err)
	}
	if firstSelection == nil || firstSelection.Upstream == nil {
		t.Fatalf("SelectChannel returned nil selection")
	}
	if firstSelection.TargetPool != config.CompositeTargetPoolResponses {
		t.Fatalf("expected primary targetPool=responses, got %q", firstSelection.TargetPool)
	}
	if firstSelection.ChannelIndex != 0 {
		t.Fatalf("expected primary channelIndex=0 in responses pool, got %d", firstSelection.ChannelIndex)
	}
	if firstSelection.FailedChannelKey == firstSelection.ChannelIndex {
		t.Fatalf("expected pool-safe FailedChannelKey for cross-pool target, got %d", firstSelection.FailedChannelKey)
	}

	// Simulate primary target failure. If bookkeeping were index-only, this would
	// incorrectly poison messages channel index 0 and block failover.
	failedChannels[firstSelection.FailedChannelKey] = true

	nextSelection, err := s.GetNextCompositeFailover(firstSelection, false, failedChannels, nil, false)
	if err != nil {
		t.Fatalf("GetNextCompositeFailover returned error: %v", err)
	}
	if nextSelection == nil || nextSelection.Upstream == nil {
		t.Fatalf("GetNextCompositeFailover returned nil selection")
	}
	if nextSelection.TargetPool != config.CompositeTargetPoolMessages {
		t.Fatalf("expected failover targetPool=messages, got %q", nextSelection.TargetPool)
	}
	if nextSelection.Upstream.ID != "msg-0" {
		t.Fatalf("expected failover upstream msg-0, got %q", nextSelection.Upstream.ID)
	}
	if nextSelection.ChannelIndex != 0 {
		t.Fatalf("expected failover channelIndex=0 in messages pool, got %d", nextSelection.ChannelIndex)
	}
}

func TestCompositeMixedPoolFailover_AvoidsCrossPoolIndexCollision_ResponsesEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := config.Config{
		LoadBalance:              "failover",
		ResponsesLoadBalance:     "failover",
		GeminiLoadBalance:        "failover",
		CurrentUpstream:          0,
		CurrentResponsesUpstream: 0,
	}

	cfg.Upstream = append(cfg.Upstream, config.UpstreamConfig{
		ID:          "msg-0",
		Name:        "Messages Target",
		ServiceType: "claude",
		Status:      "active",
		APIKeys:     []string{"msg-key"},
	})

	cfg.ResponsesUpstream = append(cfg.ResponsesUpstream,
		config.UpstreamConfig{
			ID:          "resp-0",
			Name:        "Responses Target",
			ServiceType: "responses",
			Status:      "active",
			Priority:    10,
			APIKeys:     []string{"resp-key"},
		},
		config.UpstreamConfig{
			ID:          "cmp-resp",
			Name:        "Composite Responses",
			ServiceType: "composite",
			Status:      "active",
			Priority:    0,
			APIKeys:     []string{"composite-dummy"},
			CompositeMappings: []config.CompositeMapping{
				{
					Pattern:         "haiku",
					TargetPool:      config.CompositeTargetPoolMessages,
					TargetChannelID: "msg-0",
					FailoverTargets: []config.CompositeTargetRef{
						{
							Pool:      config.CompositeTargetPoolResponses,
							ChannelID: "resp-0",
						},
					},
				},
			},
		},
	)

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
	failedChannels := make(map[int]bool)

	firstSelection, err := s.SelectChannel(context.Background(), "u", failedChannels, true, []string{"cmp-resp", "resp-0"}, "gpt-5-haiku")
	if err != nil {
		t.Fatalf("SelectChannel returned error: %v", err)
	}
	if firstSelection == nil || firstSelection.Upstream == nil {
		t.Fatalf("SelectChannel returned nil selection")
	}
	if firstSelection.TargetPool != config.CompositeTargetPoolMessages {
		t.Fatalf("expected primary targetPool=messages, got %q", firstSelection.TargetPool)
	}
	if firstSelection.ChannelIndex != 0 {
		t.Fatalf("expected primary channelIndex=0 in messages pool, got %d", firstSelection.ChannelIndex)
	}
	if firstSelection.FailedChannelKey == firstSelection.ChannelIndex {
		t.Fatalf("expected pool-safe FailedChannelKey for cross-pool target, got %d", firstSelection.FailedChannelKey)
	}

	failedChannels[firstSelection.FailedChannelKey] = true

	nextSelection, err := s.GetNextCompositeFailover(firstSelection, true, failedChannels, nil, false)
	if err != nil {
		t.Fatalf("GetNextCompositeFailover returned error: %v", err)
	}
	if nextSelection == nil || nextSelection.Upstream == nil {
		t.Fatalf("GetNextCompositeFailover returned nil selection")
	}
	if nextSelection.TargetPool != config.CompositeTargetPoolResponses {
		t.Fatalf("expected failover targetPool=responses, got %q", nextSelection.TargetPool)
	}
	if nextSelection.Upstream.ID != "resp-0" {
		t.Fatalf("expected failover upstream resp-0, got %q", nextSelection.Upstream.ID)
	}
	if nextSelection.ChannelIndex != 0 {
		t.Fatalf("expected failover channelIndex=0 in responses pool, got %d", nextSelection.ChannelIndex)
	}
}
