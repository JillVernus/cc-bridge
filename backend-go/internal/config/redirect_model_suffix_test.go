package config

import "testing"

func TestRedirectModel_PreservesSuffix_WhenMappedByBaseModel(t *testing.T) {
	up := &UpstreamConfig{
		ModelMapping: map[string]string{
			"gpt-5.2": "gpt-5.2",
		},
	}

	got := RedirectModel("gpt-5.2(high)", up)
	if got != "gpt-5.2(high)" {
		t.Fatalf("expected suffix to be preserved, got %q", got)
	}
}

func TestRedirectModel_PropagatesSuffix_ToMappedTarget(t *testing.T) {
	up := &UpstreamConfig{
		ModelMapping: map[string]string{
			"o3": "gpt-5.2",
		},
	}

	got := RedirectModel("o3(xhigh)", up)
	if got != "gpt-5.2(xhigh)" {
		t.Fatalf("expected suffix to be propagated, got %q", got)
	}
}

func TestRedirectModel_DoesNotPropagateSuffix_ForExplicitFullModelMapping(t *testing.T) {
	up := &UpstreamConfig{
		ModelMapping: map[string]string{
			"o3(xhigh)": "gpt-5.2",
		},
	}

	got := RedirectModel("o3(xhigh)", up)
	if got != "gpt-5.2" {
		t.Fatalf("expected explicit full mapping to be returned as-is, got %q", got)
	}
}

func TestRedirectModel_DoesNotOverrideTargetSuffix(t *testing.T) {
	up := &UpstreamConfig{
		ModelMapping: map[string]string{
			"o3": "gpt-5.2(high)",
		},
	}

	got := RedirectModel("o3(xhigh)", up)
	if got != "gpt-5.2(high)" {
		t.Fatalf("expected mapped target suffix to win, got %q", got)
	}
}

func TestRedirectModel_AllowsUnknownSuffix_PassThrough(t *testing.T) {
	up := &UpstreamConfig{
		ModelMapping: map[string]string{
			"o3": "gpt-5.2",
		},
	}

	got := RedirectModel("o3(wild)", up)
	if got != "gpt-5.2(wild)" {
		t.Fatalf("expected unknown suffix to be propagated, got %q", got)
	}
}

// TestRedirectModel_FuzzyMatchOpus tests the exact user-reported scenario:
// mapping "opus" -> "claude-opus-4-5" should match "claude-opus-4.6"
func TestRedirectModel_FuzzyMatchOpus(t *testing.T) {
	testCases := []struct {
		name     string
		mapping  map[string]string
		model    string
		expected string
	}{
		{
			name:     "opus matches claude-opus-4.6",
			mapping:  map[string]string{"opus": "claude-opus-4-5"},
			model:    "claude-opus-4.6",
			expected: "claude-opus-4-5",
		},
		{
			name:     "opus matches claude-opus-4-6 (dashes)",
			mapping:  map[string]string{"opus": "claude-opus-4-5"},
			model:    "claude-opus-4-6",
			expected: "claude-opus-4-5",
		},
		{
			name:     "opus matches claude-3-opus-20240229",
			mapping:  map[string]string{"opus": "claude-opus-4-5"},
			model:    "claude-3-opus-20240229",
			expected: "claude-opus-4-5",
		},
		{
			name:     "sonnet should not match opus model",
			mapping:  map[string]string{"opus": "claude-opus-4-5"},
			model:    "claude-3-sonnet-20240229",
			expected: "claude-3-sonnet-20240229", // unchanged
		},
		{
			name:     "exact opus matches",
			mapping:  map[string]string{"opus": "claude-opus-4-5"},
			model:    "opus",
			expected: "claude-opus-4-5",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			up := &UpstreamConfig{ModelMapping: tc.mapping}
			got := RedirectModel(tc.model, up)
			if got != tc.expected {
				t.Errorf("RedirectModel(%q) = %q, want %q", tc.model, got, tc.expected)
			}
		})
	}
}
