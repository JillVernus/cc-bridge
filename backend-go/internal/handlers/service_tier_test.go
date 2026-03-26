package handlers

import (
	"encoding/json"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func TestIsFastModeForMessagesBridge(t *testing.T) {
	tests := []struct {
		name        string
		speed       string
		serviceType string
		want        bool
	}{
		{name: "responses fast", speed: "fast", serviceType: "responses", want: true},
		{name: "oauth fast", speed: "fast", serviceType: "openai-oauth", want: true},
		{name: "claude fast stays false", speed: "fast", serviceType: "claude", want: false},
		{name: "responses non fast stays false", speed: "normal", serviceType: "responses", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFastModeForMessagesBridge(tc.speed, tc.serviceType); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestNormalizeResponsesServiceTier(t *testing.T) {
	if got := normalizeResponsesServiceTier(" Priority "); got != "priority" {
		t.Fatalf("expected priority, got %q", got)
	}
	if got := normalizeResponsesServiceTier("default"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestResolveEffectiveResponsesServiceTier(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		model           string
		upstream        *config.UpstreamConfig
		wantTier        string
		wantFast        bool
		wantOverridden  bool
		wantServiceTier *string
	}{
		{
			name:  "missing tier forced to priority",
			body:  `{"model":"gpt-5-codex","input":"hi"}`,
			model: "gpt-5-codex",
			upstream: &config.UpstreamConfig{
				ServiceType:              "responses",
				CodexServiceTierOverride: "force_priority",
			},
			wantTier:        "priority",
			wantFast:        true,
			wantOverridden:  true,
			wantServiceTier: strPtr("priority"),
		},
		{
			name:  "default tier forced to priority",
			body:  `{"model":"gpt-5-codex","service_tier":"default","input":"hi"}`,
			model: "gpt-5-codex",
			upstream: &config.UpstreamConfig{
				ServiceType:              "openai-oauth",
				CodexServiceTierOverride: "force_priority",
			},
			wantTier:        "priority",
			wantFast:        true,
			wantOverridden:  true,
			wantServiceTier: strPtr("priority"),
		},
		{
			name:  "priority tier remains unchanged",
			body:  `{"model":"gpt-5-codex","service_tier":"priority","input":"hi"}`,
			model: "gpt-5-codex",
			upstream: &config.UpstreamConfig{
				ServiceType:              "responses",
				CodexServiceTierOverride: "force_priority",
			},
			wantTier:        "priority",
			wantFast:        true,
			wantOverridden:  false,
			wantServiceTier: strPtr("priority"),
		},
		{
			name:  "other non-empty tier remains unchanged",
			body:  `{"model":"gpt-5-codex","service_tier":"flex","input":"hi"}`,
			model: "gpt-5-codex",
			upstream: &config.UpstreamConfig{
				ServiceType:              "responses",
				CodexServiceTierOverride: "force_priority",
			},
			wantTier:        "flex",
			wantFast:        false,
			wantOverridden:  false,
			wantServiceTier: strPtr("flex"),
		},
		{
			name:  "override applies to any request on enabled codex tab channel",
			body:  `{"model":"gpt-5","input":"hi"}`,
			model: "gpt-5",
			upstream: &config.UpstreamConfig{
				ServiceType:              "responses",
				CodexServiceTierOverride: "force_priority",
			},
			wantTier:        "priority",
			wantFast:        true,
			wantOverridden:  true,
			wantServiceTier: strPtr("priority"),
		},
		{
			name:  "override off leaves default unchanged",
			body:  `{"model":"gpt-5-codex","service_tier":"default","input":"hi"}`,
			model: "gpt-5-codex",
			upstream: &config.UpstreamConfig{
				ServiceType:              "responses",
				CodexServiceTierOverride: "off",
			},
			wantTier:        "default",
			wantFast:        false,
			wantOverridden:  false,
			wantServiceTier: strPtr("default"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			effectiveBody, serviceTier, isFastMode, overridden, err := resolveEffectiveResponsesServiceTier(
				[]byte(tc.body),
				tc.upstream,
			)
			if err != nil {
				t.Fatalf("resolveEffectiveResponsesServiceTier returned error: %v", err)
			}
			if serviceTier != tc.wantTier {
				t.Fatalf("serviceTier = %q, want %q", serviceTier, tc.wantTier)
			}
			if isFastMode != tc.wantFast {
				t.Fatalf("isFastMode = %v, want %v", isFastMode, tc.wantFast)
			}
			if overridden != tc.wantOverridden {
				t.Fatalf("overridden = %v, want %v", overridden, tc.wantOverridden)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(effectiveBody, &payload); err != nil {
				t.Fatalf("failed to unmarshal effective body: %v", err)
			}

			rawTier, hasTier := payload["service_tier"]
			if tc.wantServiceTier == nil {
				if hasTier {
					t.Fatalf("expected service_tier to be absent, got %#v", rawTier)
				}
				return
			}

			gotTier, ok := rawTier.(string)
			if !ok {
				t.Fatalf("expected string service_tier, got %#v", rawTier)
			}
			if gotTier != *tc.wantServiceTier {
				t.Fatalf("effective service_tier = %q, want %q", gotTier, *tc.wantServiceTier)
			}
		})
	}
}

func strPtr(v string) *string {
	return &v
}
