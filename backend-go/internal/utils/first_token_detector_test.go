package utils

import (
	"strings"
	"testing"
)

func TestFirstTokenDetector_ClaudeSSE_DetectsContentDelta(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolClaudeSSE)

	if d.ObserveLine("event: content_block_start") {
		t.Fatalf("should not detect on non-delta event")
	}
	if d.ObserveLine(`data: {"type":"content_block_start","index":0}`) {
		t.Fatalf("should not detect on non-delta payload")
	}
	if d.ObserveLine("event: content_block_delta") {
		t.Fatalf("should not detect on event line alone")
	}
	if !d.ObserveLine(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`) {
		t.Fatalf("expected detection on first content_block_delta payload")
	}
	if !d.Done() {
		t.Fatalf("detector should be marked done after first token")
	}
}

func TestFirstTokenDetector_OpenAIChat_IgnoresToolCallsAndWhitespace(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolOpenAIChatSSE)

	if d.ObserveLine(`data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1"}]}}]}`) {
		t.Fatalf("tool-call-only chunk must not count as first token")
	}
	if d.ObserveLine(`data: {"choices":[{"delta":{"content":"   "}}]}`) {
		t.Fatalf("whitespace-only content must not count as first token")
	}
	if !d.ObserveLine(`data: {"choices":[{"delta":{"content":"hi"}}]}`) {
		t.Fatalf("non-empty content should count as first token")
	}
}

func TestFirstTokenDetector_ResponsesSSE_DeltaAcrossChunkBoundaries(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if d.ObserveBytes([]byte("event: response.output_text.delta\n")) {
		t.Fatalf("event line alone must not detect")
	}
	if d.ObserveBytes([]byte(`data: {"type":"response.output_text.delta","delta":"Hel`)) {
		t.Fatalf("partial payload must not detect before newline completes line")
	}
	if !d.ObserveBytes([]byte("lo\"}\n\n")) {
		t.Fatalf("expected detection once split delta payload is completed")
	}
}

func TestFirstTokenDetector_ResponsesSSE_DoneFallback(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)
	if d.ObserveLine(`data: {"type":"response.output_text.done","text":"   "}`) {
		t.Fatalf("whitespace done text must not count as first token")
	}

	d = NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)
	if !d.ObserveLine(`data: {"type":"response.output_text.done","text":"final text"}`) {
		t.Fatalf("done text fallback should count when no delta was seen")
	}
}

func TestFirstTokenDetector_ResponsesSSE_PayloadTypeWinsOverStaleEvent(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)
	_ = d.ObserveLine("event: response.output_item.added")

	if !d.ObserveLine(`data: {"type":"response.output_text.delta","delta":"hello"}`) {
		t.Fatalf("expected detection from payload type even with stale/irrelevant last event")
	}
}

func TestFirstTokenDetector_ResponsesSSE_ContentPartAddedFallback(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if d.ObserveLine(`data: {"type":"response.content_part.added","part":{"type":"output_text","text":"   "}}`) {
		t.Fatalf("whitespace output_text in content_part.added must not count as first token")
	}
	if !d.ObserveLine(`data: {"type":"response.content_part.added","part":{"type":"output_text","text":"hello"}}`) {
		t.Fatalf("non-empty output_text in content_part.added should count as first token")
	}
}

func TestFirstTokenDetector_ResponsesSSE_ContentPartAdded_IgnoresNonOutputTextPart(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if d.ObserveLine(`data: {"type":"response.content_part.added","part":{"type":"refusal","text":"hello"}}`) {
		t.Fatalf("non-output_text content_part must not count as first token")
	}
}

func TestFirstTokenDetector_ResponsesSSE_CompletedOutputFallback(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if d.ObserveLine(`data: {"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"   "}]}]}}`) {
		t.Fatalf("whitespace-only completed output text must not count as first token")
	}
	if !d.ObserveLine(`data: {"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}}`) {
		t.Fatalf("non-empty completed output text should count as first token")
	}
}

func TestFirstTokenDetector_ResponsesSSE_OutputItemFallback(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if !d.ObserveLine(`data: {"type":"response.output_item.added","item":{"type":"message","content":[{"type":"output_text","text":"hello"}]}}`) {
		t.Fatalf("non-empty output_item output_text should count as first token")
	}
}

func TestFirstTokenDetector_ResponsesSSE_OutputItemAndCompleted_IgnoresNonTextShapes(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolResponsesSSE)

	if d.ObserveLine(`data: {"type":"response.output_item.added","item":{"type":"function_call","arguments":"{\"cmd\":\"echo hi\"}"}}`) {
		t.Fatalf("function_call output_item must not count as first token")
	}
	if d.ObserveLine(`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"refusal","text":"no"}]}}`) {
		t.Fatalf("non-output_text content block in output_item must not count as first token")
	}
	if d.ObserveLine(`data: {"type":"response.completed","response":{"output":[{"type":"function_call","name":"exec_command"}]}}`) {
		t.Fatalf("completed response without message output_text must not count as first token")
	}
}

func TestFirstTokenDetector_StopAfterDetect(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolClaudeSSE)

	_ = d.ObserveLine("event: content_block_delta")
	if !d.ObserveLine(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"first"}}`) {
		t.Fatalf("expected first detection")
	}
	if d.ObserveLine(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"second"}}`) {
		t.Fatalf("detector must stop parsing after first detection")
	}
}

func TestFirstTokenDetector_GeminiRaw_BoundedBufferAndDetection(t *testing.T) {
	d := NewFirstTokenDetector(FirstTokenProtocolGeminiRaw)

	if d.ObserveBytes([]byte(strings.Repeat("x", d.maxBufferLen*3))) {
		t.Fatalf("raw noise should not detect first token")
	}
	if len(d.pending) > d.maxBufferLen {
		t.Fatalf("pending buffer exceeded max: got=%d max=%d", len(d.pending), d.maxBufferLen)
	}

	if !d.ObserveBytes([]byte(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`)) {
		t.Fatalf("expected detection for gemini text field")
	}
}
