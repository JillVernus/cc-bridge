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
