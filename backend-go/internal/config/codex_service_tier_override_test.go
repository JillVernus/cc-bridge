package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigManagerLoadConfig_NormalizesCodexServiceTierOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	raw := Config{
		Upstream: []UpstreamConfig{{
			ID:                       "msg-1",
			Name:                     "Messages Ineligible",
			BaseURL:                  "https://example.com/messages",
			ServiceType:              "responses",
			Status:                   "active",
			CodexServiceTierOverride: " Force_Default ",
		}},
		LoadBalance: "failover",
		ResponsesUpstream: []UpstreamConfig{
			{
				ID:                       "resp-1",
				Name:                     "Responses Default",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "openai-oauth",
				Status:                   "active",
				CodexServiceTierOverride: " Force_Default ",
			},
			{
				ID:                       "resp-2",
				Name:                     "Responses Unknown",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "responses",
				Status:                   "active",
				CodexServiceTierOverride: " unexpected ",
			},
			{
				ID:                       "resp-3",
				Name:                     "Responses Empty",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "responses",
				Status:                   "active",
				CodexServiceTierOverride: "   ",
			},
		},
		ResponsesLoadBalance: "failover",
		GeminiUpstream:       []UpstreamConfig{},
		GeminiLoadBalance:    "failover",
		ChatUpstream:         []UpstreamConfig{},
		ChatLoadBalance:      "failover",
	}

	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write raw config: %v", err)
	}

	cm := &ConfigManager{configFile: configPath}
	if err := cm.loadConfig(); err != nil {
		t.Fatalf("loadConfig() failed: %v", err)
	}

	cfg := cm.GetConfig()
	if got := cfg.Upstream[0].CodexServiceTierOverride; got != "off" {
		t.Fatalf("messages CodexServiceTierOverride = %q, want off", got)
	}

	wantResponses := map[string]string{
		"resp-1": "force_default",
		"resp-2": "off",
		"resp-3": "off",
	}
	for _, upstream := range cfg.ResponsesUpstream {
		want, ok := wantResponses[upstream.ID]
		if !ok {
			t.Fatalf("unexpected responses channel %q", upstream.ID)
		}
		if got := upstream.CodexServiceTierOverride; got != want {
			t.Fatalf("responses CodexServiceTierOverride for %s = %q, want %q", upstream.ID, got, want)
		}
	}
}

func TestConfigManagerAddAndUpdateResponsesUpstream_NormalizesCodexServiceTierOverride(t *testing.T) {
	cm := &ConfigManager{
		configFile: filepath.Join(t.TempDir(), "config.json"),
		config: Config{
			Upstream:                 []UpstreamConfig{},
			LoadBalance:              "failover",
			ResponsesUpstream:        []UpstreamConfig{},
			ResponsesLoadBalance:     "failover",
			GeminiUpstream:           []UpstreamConfig{},
			GeminiLoadBalance:        "failover",
			ChatUpstream:             []UpstreamConfig{},
			ChatLoadBalance:          "failover",
			UserAgent:                GetDefaultUserAgentConfig(),
			CurrentUpstream:          0,
			CurrentResponsesUpstream: 0,
		},
	}

	if err := cm.AddResponsesUpstream(UpstreamConfig{
		ID:                       "resp-1",
		Name:                     "Responses Add",
		BaseURL:                  "https://chatgpt.com/backend-api/codex",
		ServiceType:              "openai-oauth",
		CodexServiceTierOverride: " Force_Default ",
	}); err != nil {
		t.Fatalf("AddResponsesUpstream() failed: %v", err)
	}

	if got := cm.config.ResponsesUpstream[0].CodexServiceTierOverride; got != "force_default" {
		t.Fatalf("added CodexServiceTierOverride = %q, want force_default", got)
	}

	serviceType := "openai"
	if _, err := cm.UpdateResponsesUpstream(0, UpstreamUpdate{ServiceType: &serviceType}); err != nil {
		t.Fatalf("UpdateResponsesUpstream() failed: %v", err)
	}

	if got := cm.config.ResponsesUpstream[0].CodexServiceTierOverride; got != "off" {
		t.Fatalf("updated CodexServiceTierOverride = %q, want off after ineligible serviceType", got)
	}
}
