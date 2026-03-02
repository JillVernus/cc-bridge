package utils

import "testing"

func TestStreamSynthesizer_ResponsesUsageFromResponseCompleted(t *testing.T) {
	synth := NewStreamSynthesizer("responses")

	synth.ProcessLine(`data: {"type":"response.completed","response":{"model":"gpt-5-codex","usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":20},"output_tokens":30,"total_tokens":150}}}`)

	usage := synth.GetUsage()
	if usage.InputTokens != 100 {
		t.Fatalf("expected input_tokens=100 (input-cached), got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 30 {
		t.Fatalf("expected output_tokens=30, got %d", usage.OutputTokens)
	}
	if usage.CacheReadInputTokens != 20 {
		t.Fatalf("expected cache_read_input_tokens=20, got %d", usage.CacheReadInputTokens)
	}
	if usage.TotalTokens != 150 {
		t.Fatalf("expected total_tokens=150, got %d", usage.TotalTokens)
	}
	if usage.Model != "gpt-5-codex" {
		t.Fatalf("expected model=gpt-5-codex, got %q", usage.Model)
	}
}

func TestStreamSynthesizer_OpenAIOAuthUsesResponsesParser(t *testing.T) {
	synth := NewStreamSynthesizer("openai-oauth")

	synth.ProcessLine(`data: {"type":"response.completed","response":{"model":"gpt-5-codex","usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}}`)

	usage := synth.GetUsage()
	if usage.InputTokens != 12 {
		t.Fatalf("expected input_tokens=12 from prompt_tokens fallback, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 8 {
		t.Fatalf("expected output_tokens=8 from completion_tokens fallback, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 20 {
		t.Fatalf("expected total_tokens=20, got %d", usage.TotalTokens)
	}
	if usage.Model != "gpt-5-codex" {
		t.Fatalf("expected model=gpt-5-codex, got %q", usage.Model)
	}
}

func TestStreamSynthesizer_OpenAIOAuthSynthesizesResponsesText(t *testing.T) {
	synth := NewStreamSynthesizer("openai-oauth")

	synth.ProcessLine(`data: {"type":"response.output_text.delta","output_index":0,"delta":"hello "}`)
	synth.ProcessLine(`data: {"type":"response.output_text.delta","output_index":0,"delta":"world"}`)

	content := synth.GetSynthesizedContent()
	if content != "hello world" {
		t.Fatalf("expected synthesized content 'hello world', got %q", content)
	}
}

func TestStreamSynthesizer_ResponsesServiceParsesConvertedClaudeEvents(t *testing.T) {
	synth := NewStreamSynthesizer("responses")

	synth.ProcessLine(`data: {"type":"message_start","message":{"model":"gpt-5.3-codex","usage":{"input_tokens":120,"cache_read_input_tokens":20,"output_tokens":0}}}`)
	synth.ProcessLine(`data: {"type":"message_delta","usage":{"output_tokens":30}}`)

	usage := synth.GetUsage()
	if usage.Model != "gpt-5.3-codex" {
		t.Fatalf("expected model=gpt-5.3-codex, got %q", usage.Model)
	}
	if usage.InputTokens != 120 {
		t.Fatalf("expected input_tokens=120 from converted Claude event, got %d", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 20 {
		t.Fatalf("expected cache_read_input_tokens=20, got %d", usage.CacheReadInputTokens)
	}
	if usage.OutputTokens != 30 {
		t.Fatalf("expected output_tokens=30, got %d", usage.OutputTokens)
	}
}
