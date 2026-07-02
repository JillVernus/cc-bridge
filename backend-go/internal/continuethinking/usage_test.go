package continuethinking

import "testing"

func TestExtractUsage(t *testing.T) {
	ev := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"model": "gpt-5",
			"usage": map[string]any{
				"input_tokens":  float64(100),
				"output_tokens": float64(516),
				"total_tokens":  float64(616),
				"input_tokens_details": map[string]any{
					"cached_tokens": float64(40),
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": float64(516),
				},
			},
		},
	}
	u, model, ok := extractUsage(ev)
	if !ok {
		t.Fatal("expected ok")
	}
	if model != "gpt-5" {
		t.Errorf("model = %q", model)
	}
	if u.InputTokens != 100 || u.OutputTokens != 516 || u.TotalTokens != 616 {
		t.Errorf("token counts wrong: %+v", u)
	}
	if u.CachedTokens != 40 {
		t.Errorf("cached = %d", u.CachedTokens)
	}
	if u.ReasoningTokens != 516 {
		t.Errorf("reasoning = %d", u.ReasoningTokens)
	}
	if !u.Truncated || u.N != 1 {
		t.Errorf("truncation detection wrong: truncated=%v n=%d", u.Truncated, u.N)
	}
}

func TestExtractUsage_NoUsageBlock(t *testing.T) {
	ev := map[string]any{"type": "response.completed", "response": map[string]any{"model": "x"}}
	_, model, ok := extractUsage(ev)
	if ok {
		t.Error("expected ok=false when no usage")
	}
	if model != "x" {
		t.Errorf("model should still be extracted: %q", model)
	}
}

func TestExtractUsage_CleanFinishNotTruncated(t *testing.T) {
	ev := map[string]any{
		"response": map[string]any{
			"usage": map[string]any{
				"output_tokens_details": map[string]any{"reasoning_tokens": float64(500)},
			},
		},
	}
	u, _, ok := extractUsage(ev)
	if !ok {
		t.Fatal("expected ok")
	}
	if u.Truncated {
		t.Errorf("500 reasoning tokens should not be truncated")
	}
}
