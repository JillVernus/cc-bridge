package config

import (
	"path/filepath"
	"testing"
)

func TestResponsesWebSocketConfigDefaultsDisabled(t *testing.T) {
	cm, err := NewConfigManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cm.Close() })

	cfg := cm.GetResponsesWebSocketConfig()
	if cfg.Enabled {
		t.Fatalf("Responses WebSocket enabled = true, want false by default")
	}
}

func TestLegacyResponsesWebSocketConfigMigratesToOpenAIOAuthChannels(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cm.Close() })

	legacyCfg := cm.GetConfig()
	legacyCfg.ResponsesWebSocket.Enabled = true
	legacyCfg.ResponsesUpstream = []UpstreamConfig{
		{
			Name:        "oauth",
			BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
			ServiceType: "openai-oauth",
			Status:      "active",
		},
		{
			Name:        "responses",
			BaseURL:     "https://api.openai.com/v1",
			ServiceType: "responses",
			Status:      "active",
		},
	}
	if err := cm.RestoreConfig(legacyCfg); err != nil {
		t.Fatalf("RestoreConfig() failed: %v", err)
	}

	reloaded, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("reload NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })

	cfg := reloaded.GetConfig()
	if cfg.ResponsesWebSocket.Enabled {
		t.Fatalf("legacy global Responses WebSocket flag still enabled after migration")
	}
	if len(cfg.ResponsesUpstream) != 2 {
		t.Fatalf("responses channel count = %d, want 2", len(cfg.ResponsesUpstream))
	}
	if !cfg.ResponsesUpstream[0].ResponsesWebSocketEnabled {
		t.Fatalf("openai-oauth channel responsesWebSocketEnabled = false, want true")
	}
	if cfg.ResponsesUpstream[1].ResponsesWebSocketEnabled {
		t.Fatalf("responses channel responsesWebSocketEnabled = true, want false")
	}
}

func TestAddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}

	if err := cm.AddResponsesUpstream(UpstreamConfig{
		Name:        "oauth-default-ws",
		BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
		ServiceType: "openai-oauth",
		Status:      "active",
		OAuthTokens: &OAuthTokens{
			AccessToken: "access-token",
			AccountID:   "account-id",
		},
	}); err != nil {
		t.Fatalf("AddResponsesUpstream(openai-oauth) failed: %v", err)
	}
	if err := cm.AddResponsesUpstream(UpstreamConfig{
		Name:        "responses-default-http",
		BaseURL:     "https://api.openai.com/v1",
		ServiceType: "responses",
		Status:      "active",
		APIKeys:     []string{"key"},
	}); err != nil {
		t.Fatalf("AddResponsesUpstream(responses) failed: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	reloaded, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("reload NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })

	cfg := reloaded.GetConfig()
	if len(cfg.ResponsesUpstream) != 2 {
		t.Fatalf("responses channel count = %d, want 2", len(cfg.ResponsesUpstream))
	}
	if !cfg.ResponsesUpstream[0].ResponsesWebSocketEnabled {
		t.Fatalf("openai-oauth channel responsesWebSocketEnabled = false, want true after reload")
	}
	if cfg.ResponsesUpstream[1].ResponsesWebSocketEnabled {
		t.Fatalf("plain responses channel responsesWebSocketEnabled = true, want false by default")
	}
}

func TestResponsesWebSocketFlagPersistsForNonOAuthResponsesChannel(t *testing.T) {
	cm, err := NewConfigManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}

	if err := cm.AddResponsesUpstream(UpstreamConfig{
		Name:                      "responses-ws",
		BaseURL:                   "https://api.example.com",
		ServiceType:               "responses",
		Status:                    "active",
		APIKeys:                   []string{"key"},
		ResponsesWebSocketEnabled: true,
	}); err != nil {
		t.Fatalf("AddResponsesUpstream() failed: %v", err)
	}

	cfg := cm.GetConfig()
	if len(cfg.ResponsesUpstream) != 1 {
		t.Fatalf("responses channel count = %d, want 1", len(cfg.ResponsesUpstream))
	}
	if !cfg.ResponsesUpstream[0].ResponsesWebSocketEnabled {
		t.Fatalf("responses channel responsesWebSocketEnabled = false, want true")
	}

	serviceType := "composite"
	if _, err := cm.UpdateResponsesUpstream(0, UpstreamUpdate{ServiceType: &serviceType}); err != nil {
		t.Fatalf("UpdateResponsesUpstream() failed: %v", err)
	}
	cfg = cm.GetConfig()
	if cfg.ResponsesUpstream[0].ResponsesWebSocketEnabled {
		t.Fatalf("composite channel responsesWebSocketEnabled = true, want false")
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
}
