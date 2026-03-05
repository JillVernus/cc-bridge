package providers

import (
	"io"
	"strings"
	"testing"
)

func collectClaudeStreamOutput(eventCh <-chan string, errCh <-chan error) (string, error) {
	var b strings.Builder
	for event := range eventCh {
		b.WriteString(event)
	}
	for err := range errCh {
		if err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

func TestClaudeProviderHandleStreamResponse_NormalizesBareJSONDataLine(t *testing.T) {
	p := &ClaudeProvider{}
	input := strings.Join([]string{
		"event: message_start",
		`{"type":"message_start","message":{"id":"msg_1","stop_reason":"tool_use"}}`,
		"",
	}, "\n")

	eventCh, errCh, err := p.HandleStreamResponse(io.NopCloser(strings.NewReader(input)))
	if err != nil {
		t.Fatalf("HandleStreamResponse returned error: %v", err)
	}

	out, streamErr := collectClaudeStreamOutput(eventCh, errCh)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}

	if !strings.Contains(out, "event: message_start\n") {
		t.Fatalf("expected event line to be preserved, got: %q", out)
	}
	if !strings.Contains(out, `data: {"type":"message_start","message":{"id":"msg_1","stop_reason":"tool_use"}}`+"\n") {
		t.Fatalf("expected bare JSON line to be normalized as data line, got: %q", out)
	}
}

func TestClaudeProviderHandleStreamResponse_PreservesStandardSSELines(t *testing.T) {
	p := &ClaudeProvider{}
	input := strings.Join([]string{
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}`,
		"",
	}, "\n")

	eventCh, errCh, err := p.HandleStreamResponse(io.NopCloser(strings.NewReader(input)))
	if err != nil {
		t.Fatalf("HandleStreamResponse returned error: %v", err)
	}

	out, streamErr := collectClaudeStreamOutput(eventCh, errCh)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}

	if !strings.Contains(out, "event: content_block_delta\n") {
		t.Fatalf("expected event line in output, got: %q", out)
	}
	if !strings.Contains(out, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}`+"\n") {
		t.Fatalf("expected data line in output, got: %q", out)
	}
}

func TestClaudeProviderHandleStreamResponse_HandlesVeryLargeDataLine(t *testing.T) {
	p := &ClaudeProvider{}

	// Larger than the previous scanner cap (4MB) to guard against token-limit truncation.
	hugePayload := strings.Repeat("x", 5*1024*1024)
	input := "event: message_start\n" + "data: " + hugePayload + "\n\n"

	eventCh, errCh, err := p.HandleStreamResponse(io.NopCloser(strings.NewReader(input)))
	if err != nil {
		t.Fatalf("HandleStreamResponse returned error: %v", err)
	}

	out, streamErr := collectClaudeStreamOutput(eventCh, errCh)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}

	if !strings.HasPrefix(out, "event: message_start\n") {
		t.Fatalf("expected message_start prefix, got first bytes: %q", out[:min(60, len(out))])
	}
	if !strings.Contains(out, "data: "+strings.Repeat("x", 64)) {
		t.Fatalf("expected large data line to be forwarded")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
