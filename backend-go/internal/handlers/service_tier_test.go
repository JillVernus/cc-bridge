package handlers

import "testing"

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
