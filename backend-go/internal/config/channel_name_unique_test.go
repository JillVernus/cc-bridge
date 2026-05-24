package config

import (
	"strings"
	"testing"
)

func TestAddUpstream_RejectsDuplicateNameCaseInsensitive(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}}

	if err := cm.AddUpstream(UpstreamConfig{Name: " alpha ", BaseURL: "x", ServiceType: "openai"}); err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestUpdateUpstream_RejectsDuplicateNameCaseInsensitive(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}, {Name: "Beta", ID: "id-b"}}

	name := "ALPHA"
	_, err := cm.UpdateUpstream(1, UpstreamUpdate{Name: &name})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestAddResponsesUpstream_RejectsEmptyName(t *testing.T) {
	cm := &ConfigManager{}
	if err := cm.AddResponsesUpstream(UpstreamConfig{Name: "  ", BaseURL: "x", ServiceType: "openai"}); err == nil {
		t.Fatalf("expected empty name to be rejected")
	}
}

func TestEnsureUniqueChannelName_AllowsSameNameAcrossTypes(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}

	// Same name in a different type should be allowed
	if err := cm.ensureUniqueChannelNameLocked("Shared", "gemini", -1); err != nil {
		t.Fatalf("expected same name across types to be allowed, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("Shared", "responses", -1); err != nil {
		t.Fatalf("expected same name across types to be allowed, got: %v", err)
	}
}

func TestEnsureUniqueChannelName_AllowsSameNameAcrossAllTypes(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}
	cm.config.ResponsesUpstream = []UpstreamConfig{{Name: "Other", ID: "id-b"}}
	cm.config.GeminiUpstream = []UpstreamConfig{{Name: "Third", ID: "id-c"}}

	// "Shared" exists in messages — should be allowed in responses and gemini
	if err := cm.ensureUniqueChannelNameLocked("shared", "responses", -1); err != nil {
		t.Fatalf("expected same name from messages to be allowed in responses, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("shared", "gemini", -1); err != nil {
		t.Fatalf("expected same name from messages to be allowed in gemini, got: %v", err)
	}

	// "Other" exists in responses — should be allowed in messages and gemini
	if err := cm.ensureUniqueChannelNameLocked("other", "messages", -1); err != nil {
		t.Fatalf("expected same name from responses to be allowed in messages, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("other", "gemini", -1); err != nil {
		t.Fatalf("expected same name from responses to be allowed in gemini, got: %v", err)
	}
}

func TestEnsureUniqueChannelName_RejectsSameNameWithinType(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}}

	if err := cm.ensureUniqueChannelNameLocked("alpha", "messages", -1); err == nil {
		t.Fatalf("expected duplicate name within same type to be rejected")
	}
}

func newChannelIdentityConfigManager(t *testing.T) *ConfigManager {
	t.Helper()
	return &ConfigManager{configFile: t.TempDir() + "/config.json"}
}

func assertDuplicateChannelIDError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected duplicate channel ID error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate channel id") {
		t.Fatalf("error = %v, want duplicate channel ID error", err)
	}
}

func TestAddUpstreams_RejectDuplicateStableIDWithinPool(t *testing.T) {
	tests := []struct {
		name string
		seed func(*ConfigManager)
		add  func(*ConfigManager) error
	}{
		{
			name: "messages",
			seed: func(cm *ConfigManager) {
				cm.config.Upstream = []UpstreamConfig{{Name: "Existing Messages", ID: "duplicate-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddUpstream(UpstreamConfig{Name: "New Messages", ID: "duplicate-id", BaseURL: "https://messages.example.com", ServiceType: "openai", APIKeys: []string{"key"}})
			},
		},
		{
			name: "responses",
			seed: func(cm *ConfigManager) {
				cm.config.ResponsesUpstream = []UpstreamConfig{{Name: "Existing Responses", ID: "duplicate-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddResponsesUpstream(UpstreamConfig{Name: "New Responses", ID: "duplicate-id", BaseURL: "https://responses.example.com", ServiceType: "responses", APIKeys: []string{"key"}})
			},
		},
		{
			name: "gemini",
			seed: func(cm *ConfigManager) {
				cm.config.GeminiUpstream = []UpstreamConfig{{Name: "Existing Gemini", ID: "duplicate-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddGeminiUpstream(UpstreamConfig{Name: "New Gemini", ID: "duplicate-id", BaseURL: "https://gemini.example.com", ServiceType: "gemini", APIKeys: []string{"key"}})
			},
		},
		{
			name: "chat",
			seed: func(cm *ConfigManager) {
				cm.config.ChatUpstream = []UpstreamConfig{{Name: "Existing Chat", ID: "duplicate-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddChatUpstream(UpstreamConfig{Name: "New Chat", ID: "duplicate-id", BaseURL: "https://chat.example.com", ServiceType: "openai", APIKeys: []string{"key"}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := newChannelIdentityConfigManager(t)
			tt.seed(cm)

			assertDuplicateChannelIDError(t, tt.add(cm))
		})
	}
}

func TestAddUpstreams_GenerateUniqueStableIDWithinPool(t *testing.T) {
	tests := []struct {
		name string
		seed func(*ConfigManager)
		add  func(*ConfigManager) error
		ids  func(Config) []string
	}{
		{
			name: "messages",
			seed: func(cm *ConfigManager) {
				cm.config.Upstream = []UpstreamConfig{{Name: "Existing Messages", ID: "existing-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddUpstream(UpstreamConfig{Name: "Generated Messages", BaseURL: "https://messages.example.com", ServiceType: "openai", APIKeys: []string{"key"}})
			},
			ids: func(cfg Config) []string {
				return []string{cfg.Upstream[0].ID, cfg.Upstream[1].ID}
			},
		},
		{
			name: "responses",
			seed: func(cm *ConfigManager) {
				cm.config.ResponsesUpstream = []UpstreamConfig{{Name: "Existing Responses", ID: "existing-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddResponsesUpstream(UpstreamConfig{Name: "Generated Responses", BaseURL: "https://responses.example.com", ServiceType: "responses", APIKeys: []string{"key"}})
			},
			ids: func(cfg Config) []string {
				return []string{cfg.ResponsesUpstream[0].ID, cfg.ResponsesUpstream[1].ID}
			},
		},
		{
			name: "gemini",
			seed: func(cm *ConfigManager) {
				cm.config.GeminiUpstream = []UpstreamConfig{{Name: "Existing Gemini", ID: "existing-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddGeminiUpstream(UpstreamConfig{Name: "Generated Gemini", BaseURL: "https://gemini.example.com", ServiceType: "gemini", APIKeys: []string{"key"}})
			},
			ids: func(cfg Config) []string {
				return []string{cfg.GeminiUpstream[0].ID, cfg.GeminiUpstream[1].ID}
			},
		},
		{
			name: "chat",
			seed: func(cm *ConfigManager) {
				cm.config.ChatUpstream = []UpstreamConfig{{Name: "Existing Chat", ID: "existing-id"}}
			},
			add: func(cm *ConfigManager) error {
				return cm.AddChatUpstream(UpstreamConfig{Name: "Generated Chat", BaseURL: "https://chat.example.com", ServiceType: "openai", APIKeys: []string{"key"}})
			},
			ids: func(cfg Config) []string {
				return []string{cfg.ChatUpstream[0].ID, cfg.ChatUpstream[1].ID}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := newChannelIdentityConfigManager(t)
			tt.seed(cm)

			if err := tt.add(cm); err != nil {
				t.Fatalf("add with generated ID failed: %v", err)
			}
			ids := tt.ids(cm.GetConfig())
			if ids[1] == "" {
				t.Fatalf("generated id is empty")
			}
			if ids[1] == ids[0] {
				t.Fatalf("generated id duplicated existing id %q", ids[0])
			}
		})
	}
}
