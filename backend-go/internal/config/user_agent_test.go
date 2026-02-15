package config

import (
	"path/filepath"
	"testing"
)

func newTestConfigManagerForUA(t *testing.T) *ConfigManager {
	t.Helper()
	return &ConfigManager{
		config: Config{
			UserAgent: GetDefaultUserAgentConfig(),
		},
		configFile: filepath.Join(t.TempDir(), "config.json"),
	}
}

func TestUserAgentValidation(t *testing.T) {
	if !IsValidMessagesUserAgent("claude-cli/2.1.12 (external, cli)") {
		t.Fatalf("expected valid messages user-agent")
	}
	if IsValidMessagesUserAgent("Mozilla/5.0") {
		t.Fatalf("expected invalid messages user-agent")
	}

	if !IsValidResponsesUserAgent("codex_cli_rs/0.73.0 (Linux; x86_64)") {
		t.Fatalf("expected valid responses user-agent")
	}
	if IsValidResponsesUserAgent("curl/8.6.0") {
		t.Fatalf("expected invalid responses user-agent")
	}
}

func TestResolveMessagesUserAgentCaptureNewerVersion(t *testing.T) {
	cm := newTestConfigManagerForUA(t)
	incoming := "claude-cli/2.2.0 (external, cli)"

	got := cm.ResolveMessagesUserAgent(incoming)
	if got != incoming {
		t.Fatalf("ResolveMessagesUserAgent() = %q, want %q", got, incoming)
	}

	cfg := cm.GetUserAgentConfig()
	if cfg.Messages.Latest != incoming {
		t.Fatalf("stored latest = %q, want %q", cfg.Messages.Latest, incoming)
	}
	if cfg.Messages.LastCapturedAt == "" {
		t.Fatalf("expected LastCapturedAt to be recorded")
	}
}

func TestResolveMessagesUserAgentNoCaptureForOlderVersion(t *testing.T) {
	cm := newTestConfigManagerForUA(t)
	cm.config.UserAgent.Messages.Latest = "claude-cli/2.3.0 (external, cli)"

	incoming := "claude-cli/2.2.0 (external, cli)"
	got := cm.ResolveMessagesUserAgent(incoming)
	if got != incoming {
		t.Fatalf("ResolveMessagesUserAgent() = %q, want direct-pass %q", got, incoming)
	}

	cfg := cm.GetUserAgentConfig()
	if cfg.Messages.Latest != "claude-cli/2.3.0 (external, cli)" {
		t.Fatalf("expected stored latest to remain newer value, got %q", cfg.Messages.Latest)
	}
}

func TestResolveMessagesUserAgentFallbackWhenInvalidIncoming(t *testing.T) {
	cm := newTestConfigManagerForUA(t)
	cm.config.UserAgent.Messages.Latest = "claude-cli/2.9.9 (external, cli)"

	got := cm.ResolveMessagesUserAgent("Mozilla/5.0")
	if got != "claude-cli/2.9.9 (external, cli)" {
		t.Fatalf("fallback = %q, want stored value", got)
	}
}

func TestResolveResponsesUserAgentCaptureNewerVersion(t *testing.T) {
	cm := newTestConfigManagerForUA(t)
	incoming := "codex_cli_rs/0.80.0 (Linux; x86_64)"

	got := cm.ResolveResponsesUserAgent(incoming)
	if got != incoming {
		t.Fatalf("ResolveResponsesUserAgent() = %q, want %q", got, incoming)
	}

	cfg := cm.GetUserAgentConfig()
	if cfg.Responses.Latest != incoming {
		t.Fatalf("stored latest = %q, want %q", cfg.Responses.Latest, incoming)
	}
	if cfg.Responses.LastCapturedAt == "" {
		t.Fatalf("expected LastCapturedAt to be recorded")
	}
}
