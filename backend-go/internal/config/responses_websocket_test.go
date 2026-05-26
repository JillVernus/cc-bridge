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

func TestUpdateResponsesWebSocketConfigPersists(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cm.Close() })

	if err := cm.UpdateResponsesWebSocketConfig(ResponsesWebSocketConfig{Enabled: true}); err != nil {
		t.Fatalf("UpdateResponsesWebSocketConfig() failed: %v", err)
	}

	reloaded, err := NewConfigManager(cfgPath)
	if err != nil {
		t.Fatalf("reload NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })

	if !reloaded.GetResponsesWebSocketConfig().Enabled {
		t.Fatalf("Responses WebSocket enabled = false after reload, want true")
	}
}
