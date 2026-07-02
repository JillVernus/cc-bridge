package continuethinking

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Done is the sentinel yielded by IncrementalSSE for the `data: [DONE]` line.
const Done = "[DONE]"

// Event is a parsed SSE event (the decoded JSON object of a `data:` payload) or
// the Done sentinel string. Callers should type-assert: string == Done, else map.
type Event struct {
	// Done is true when this event is the [DONE] sentinel.
	Done bool
	// Data is the decoded JSON object (nil for [DONE] / malformed lines).
	Data map[string]any
}

// IncrementalSSE frames an io.Reader byte stream into SSE events, reassembling
// events that span arbitrary chunk boundaries. It mirrors CodexCont's
// incremental_sse: an event is terminated by a blank line; multiple `data:`
// lines within one event are joined with newlines; `event:` / comment (`:`)
// lines are ignored (the JSON carries its own type).
//
// Malformed JSON data lines are skipped (lenient).
func IncrementalSSE(r io.Reader) []Event {
	events, _ := incrementalSSE(r)
	return events
}

// IncrementalSSEWithTiming is like IncrementalSSE but also returns the wall-clock
// time the first `data:` payload line was read (the request's first-token proxy
// signal). Zero if no payload was read.
func IncrementalSSEWithTiming(r io.Reader) ([]Event, time.Time) {
	return incrementalSSE(r)
}

func incrementalSSE(r io.Reader) ([]Event, time.Time) {
	var events []Event
	var firstPayloadAt time.Time
	reader := bufio.NewReader(r)
	var dataLines []string
	var sawAny bool

	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if payload == Done {
			events = append(events, Event{Done: true})
			sawAny = true
			return
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err == nil {
			events = append(events, Event{Data: obj})
			sawAny = true
		}
	}

	for {
		line, err := reader.ReadString('\n')
		// Strip trailing \n and optional \r.
		if len(line) > 0 {
			line = strings.TrimRight(line, "\n")
			line = strings.TrimRight(line, "\r")
		}
		switch {
		case line == "":
			// blank line → event terminator (only flush if we had data on the
			// boundary; a bare newline at EOF with no data is a no-op)
			flush()
		case strings.HasPrefix(line, ":"):
			// comment
		case strings.HasPrefix(line, "data:"):
			val := strings.TrimPrefix(line, "data:")
			val = strings.TrimPrefix(val, " ")
			dataLines = append(dataLines, val)
			if firstPayloadAt.IsZero() {
				firstPayloadAt = time.Now()
			}
		default:
			// `event:` / `id:` / `retry:` lines: ignored (type lives in JSON).
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			// reader.ReadString only returns io.EOF or nil for non-fatal reads;
			// treat any other error as stream end.
			break
		}
	}
	// Flush a trailing event with no terminating blank line.
	flush()

	_ = sawAny
	return events, firstPayloadAt
}

// SerializeEvent renders one event downstream as
// `event: <type>\ndata: <json>\n\n`, mirroring the upstream framing.
func SerializeEvent(event map[string]any) []byte {
	etype, _ := event["type"].(string)
	if etype == "" {
		etype = "message"
	}
	data, _ := json.Marshal(event)
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(etype)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}

// SerializeDone renders the terminal `data: [DONE]\n\n`.
func SerializeDone() []byte {
	return []byte("data: [DONE]\n\n")
}
