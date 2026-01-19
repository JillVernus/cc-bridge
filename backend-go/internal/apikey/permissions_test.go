package apikey

import "testing"

func TestValidatedKey_CheckModelPermission_ThinkingSuffixCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		allowed     []string
		model       string
		expectAllow bool
	}{
		{
			name:        "base_allowed_allows_suffixed_model",
			allowed:     []string{"gpt-5.2"},
			model:       "gpt-5.2(high)",
			expectAllow: true,
		},
		{
			name:        "wildcard_allows_suffixed_model",
			allowed:     []string{"gpt-5.2*"},
			model:       "gpt-5.2(xhigh)",
			expectAllow: true,
		},
		{
			name:        "exact_suffixed_allowed",
			allowed:     []string{"gpt-5.2(high)"},
			model:       "gpt-5.2(high)",
			expectAllow: true,
		},
		{
			name:        "different_suffixed_not_allowed",
			allowed:     []string{"gpt-5.2(xhigh)"},
			model:       "gpt-5.2(high)",
			expectAllow: false,
		},
		{
			name:        "unknown_suffix_still_matches_base_allowlist",
			allowed:     []string{"gpt-5.2"},
			model:       "gpt-5.2(weird)",
			expectAllow: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vk := &ValidatedKey{AllowedModels: tc.allowed}
			got := vk.CheckModelPermission(tc.model)
			if got != tc.expectAllow {
				t.Fatalf("allowed=%v model=%q expected=%v got=%v", tc.allowed, tc.model, tc.expectAllow, got)
			}
		})
	}
}
