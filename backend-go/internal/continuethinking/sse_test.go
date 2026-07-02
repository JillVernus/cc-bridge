package continuethinking

import (
	"strings"
	"testing"
)

func TestIncrementalSSE_BasicEvents(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"r1"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`data: [DONE]`,
		``,
		"", // terminating newline
	}, "\n"))
	events := IncrementalSSE(in)

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	if events[0].Data["type"] != "response.created" {
		t.Errorf("event0 type = %v", events[0].Data["type"])
	}
	if events[1].Data["delta"] != "hi" {
		t.Errorf("event1 delta = %v", events[1].Data["delta"])
	}
	if !events[2].Done {
		t.Errorf("event2 should be Done sentinel")
	}
}

func TestIncrementalSSE_MultiLineData(t *testing.T) {
	// Two `data:` lines within one event join with newline per SSE spec.
	in := strings.NewReader("data: {\"a\":1\ndata: ,\"b\":2}\n\n")
	events := IncrementalSSE(in)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Data["a"] != float64(1) || events[0].Data["b"] != float64(2) {
		t.Errorf("joined payload wrong: %+v", events[0].Data)
	}
}

func TestIncrementalSSE_ChunkBoundaryInsensitive(t *testing.T) {
	// bufio.NewReader already buffers; the contract is line-oriented parsing
	// regardless of how bytes arrive. Re-feed a single stream split mid-line.
	in := strings.NewReader("data: {\"type\":\"x\"}\n\n")
	events := IncrementalSSE(in)
	if len(events) != 1 || events[0].Data["type"] != "x" {
		t.Fatalf("unexpected: %+v", events)
	}
}

func TestIncrementalSSE_SkipsMalformedJSON(t *testing.T) {
	in := strings.NewReader("data: not-json\n\ndata: {\"ok\":true}\n\n")
	events := IncrementalSSE(in)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (malformed skipped)", len(events))
	}
	if events[0].Data["ok"] != true {
		t.Errorf("expected ok=true, got %+v", events[0].Data)
	}
}

func TestIncrementalSSE_TrailingEventNoBlankLine(t *testing.T) {
	in := strings.NewReader("data: {\"type\":\"tail\"}") // no trailing newline
	events := IncrementalSSE(in)
	if len(events) != 1 || events[0].Data["type"] != "tail" {
		t.Fatalf("trailing event not flushed: %+v", events)
	}
}

func TestIncrementalSSE_IgnoresEventAndCommentLines(t *testing.T) {
	in := strings.NewReader(": keepalive\n\nevent: response.created\ndata: {\"type\":\"response.created\"}\n\n")
	events := IncrementalSSE(in)
	if len(events) != 1 || events[0].Data["type"] != "response.created" {
		t.Fatalf("comment/event lines should be ignored: %+v", events)
	}
}

func TestSerializeEvent(t *testing.T) {
	out := SerializeEvent(map[string]any{"type": "response.completed", "x": 1})
	s := string(out)
	if !strings.HasPrefix(s, "event: response.completed\n") {
		t.Errorf("missing event header: %q", s)
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Errorf("missing terminator: %q", s)
	}
	if !strings.Contains(s, `"type":"response.completed"`) {
		t.Errorf("missing data payload: %q", s)
	}
}

func TestSerializeDone(t *testing.T) {
	if got := string(SerializeDone()); got != "data: [DONE]\n\n" {
		t.Errorf("SerializeDone = %q", got)
	}
}
