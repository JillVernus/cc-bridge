package handlers

import "testing"

func TestParseClaudeRequestForRouting_FallbackOnNullableFieldTypeMismatch(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":true,
		"temperature":null,
		"messages":[{"role":"user","content":"hi"}]
	}`)

	req := parseClaudeRequestForRouting(body)
	if req.Model != "claude-opus-4-6" {
		t.Fatalf("expected model to be extracted, got %q", req.Model)
	}
	if !req.Stream {
		t.Fatalf("expected stream=true to be preserved on fallback parse")
	}
}

func TestParseClaudeRequestForRouting_AcceptsStringStreamFlag(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":"true"
	}`)

	req := parseClaudeRequestForRouting(body)
	if req.Model != "claude-opus-4-6" {
		t.Fatalf("expected model to be extracted, got %q", req.Model)
	}
	if !req.Stream {
		t.Fatalf("expected stream=true when stream is string \"true\"")
	}
}

func TestParseClaudeRequestForRouting_AcceptsStringFalseStreamFlag(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":"false"
	}`)

	req := parseClaudeRequestForRouting(body)
	if req.Model != "claude-opus-4-6" {
		t.Fatalf("expected model to be extracted, got %q", req.Model)
	}
	if req.Stream {
		t.Fatalf("expected stream=false when stream is string \"false\"")
	}
}

func TestParseClaudeRequestForRouting_InvalidJSONReturnsZeroValue(t *testing.T) {
	req := parseClaudeRequestForRouting([]byte(`{not-json}`))
	if req.Model != "" {
		t.Fatalf("expected empty model for invalid JSON, got %q", req.Model)
	}
	if req.Stream {
		t.Fatalf("expected stream=false for invalid JSON")
	}
}
