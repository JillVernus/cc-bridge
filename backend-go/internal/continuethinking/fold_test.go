package continuethinking

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// sseStream builds an SSE byte stream from the given data payloads. Each `data:`
// line is terminated by a blank line.
func sseStream(datas ...string) io.ReadCloser {
	var b strings.Builder
	for _, d := range datas {
		b.WriteString("data: ")
		b.WriteString(d)
		b.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

// usageEvent builds a response.completed event with the given reasoning token
// count (and output_tokens = reasoning + extraOutput for non-reasoning output).
func usageEvent(reasoning, extraOutput int, model string) string {
	out := reasoning + extraOutput
	return `{"type":"response.completed","response":{"id":"resp_x","model":"` + model + `","usage":{"input_tokens":100,"output_tokens":` + itoa(out) + `,"total_tokens":` + itoa(100+out) + `,"output_tokens_details":{"reasoning_tokens":` + itoa(reasoning) + `}}}}`
}

// reasoningItemEvents returns a reasoning output_item.added+done pair carrying
// encrypted_content (so the round is replayable), as separate data events.
func reasoningItemEvents(oi int) []string {
	idx := itoa(oi)
	return []string{
		`{"type":"response.output_item.added","output_index":` + idx + `,"item":{"type":"reasoning","id":"rs_` + idx + `","encrypted_content":"ENC"}}`,
		`{"type":"response.output_item.done","output_index":` + idx + `,"item":{"type":"reasoning","id":"rs_` + idx + `","encrypted_content":"ENC"}}`,
	}
}

// messageItemEvents returns a buffered message item's added + delta + done.
func messageItemEvents(oi int, text string) []string {
	idx := itoa(oi)
	return []string{
		`{"type":"response.output_item.added","output_index":` + idx + `,"item":{"type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":` + idx + `,"delta":"` + text + `"}`,
		`{"type":"response.output_item.done","output_index":` + idx + `,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}}`,
	}
}

// flatten concatenates variadic string-slices into one data list for sseStream.
func flatten(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// truncStream returns a fresh round that is truncated (516) — reused across
// continuation rounds in the cap test.
func truncStream() io.ReadCloser {
	return sseStream(flatten(reasoningItemEvents(0), []string{usageEvent(516, 0, "gpt-5")})...)
}

// TestCleanFinish_NoTruncation: round 1 finishes cleanly (500 reasoning) ->
// flush buffered message, emit one terminal. No continuation.
func TestCleanFinish_NoTruncation(t *testing.T) {
	var openedRounds int
	deps := Deps{
		Round1Body:       sseStream(flatten(reasoningItemEvents(0), messageItemEvents(1, "hello"), []string{usageEvent(500, 50, "gpt-5")})...),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			openedRounds++
			return nil, nil
		},
	}
	out := FoldStream(context.Background(), deps)
	s := string(out)

	if openedRounds != 0 {
		t.Errorf("no continuation should open; got %d", openedRounds)
	}
	if !strings.Contains(s, `"delta":"hello"`) {
		t.Errorf("buffered message not flushed downstream")
	}
	if !strings.Contains(s, `"type":"response.completed"`) {
		t.Errorf("missing terminal response.completed")
	}
	if strings.Contains(s, "response.incomplete") {
		t.Errorf("should not be incomplete on clean finish")
	}
	// agent usage: input=100 (round1), reasoning=500, output=500+50=550
	if !strings.Contains(s, `"input_tokens":100`) || !strings.Contains(s, `"output_tokens":550`) {
		t.Errorf("agent usage wrong: %s", s)
	}
}

// TestSingleFold: round 1 truncated at 516 -> continue; round 2 clean.
// Round-1 buffered message must be DISCARDED; round-2 message flushed.
func TestSingleFold(t *testing.T) {
	round1 := sseStream(flatten(reasoningItemEvents(0), messageItemEvents(1, "BAD_TRUNCATED_ANSWER"), []string{usageEvent(516, 0, "gpt-5")})...)
	round2 := sseStream(flatten(messageItemEvents(0, "GOOD_FINAL_ANSWER"), []string{usageEvent(200, 100, "gpt-5")})...)

	var round int
	var gotPayload map[string]any
	deps := Deps{
		Round1Body:       round1,
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			round++
			gotPayload = payload
			return &http.Response{StatusCode: 200, Body: round2}, nil
		},
	}
	out := FoldStream(context.Background(), deps)
	s := string(out)

	if round != 1 {
		t.Errorf("expected 1 continuation round, got %d", round)
	}
	if strings.Contains(s, "BAD_TRUNCATED_ANSWER") {
		t.Errorf("round-1 tentative output should be discarded")
	}
	if !strings.Contains(s, "GOOD_FINAL_ANSWER") {
		t.Errorf("round-2 final answer should be flushed")
	}
	// Continuation payload must carry the replayed reasoning + commentary marker.
	if gotPayload != nil {
		input, _ := gotPayload["input"].([]any)
		// orig(1) + reasoning(1) + marker(1) = 3
		if len(input) != 3 {
			t.Errorf("replay tail not appended; input len=%d", len(input))
		}
		if len(input) > 0 {
			last, _ := input[len(input)-1].(map[string]any)
			if last["phase"] != "commentary" {
				t.Errorf("last replay item should be commentary marker: %+v", last)
			}
		}
		if _, ok := gotPayload["previous_response_id"]; ok {
			t.Errorf("previous_response_id should be dropped on continuation")
		}
		if gotPayload["stream"] != true {
			t.Errorf("continuation must force stream=true")
		}
	}
	if !strings.Contains(s, `"proxy_rounds"`) {
		t.Errorf("missing proxy_rounds metadata")
	}
}

// TestMaxContinueCap: every round truncated -> stop at MaxContinue+1.
func TestMaxContinueCap(t *testing.T) {
	roundsServed := 0
	deps := Deps{
		Round1Body:       truncStream(),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			roundsServed++
			return &http.Response{StatusCode: 200, Body: truncStream()}, nil
		},
	}
	out := FoldStream(context.Background(), deps)

	if roundsServed > MaxContinue {
		t.Errorf("continuation rounds %d exceeded cap %d", roundsServed, MaxContinue)
	}
	if !strings.Contains(string(out), "max_continue") {
		t.Errorf("expected max_continue stop reason after cap")
	}
}

// TestUpstreamEOF_NoTerminal: upstream stream ends without a terminal event.
func TestUpstreamEOF_NoTerminal(t *testing.T) {
	deps := Deps{
		Round1Body:       sseStream(flatten(reasoningItemEvents(0), messageItemEvents(1, "tentative"))...),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			t.Fatal("should not open continuation on EOF")
			return nil, nil
		},
	}
	out := FoldStream(context.Background(), deps)
	s := string(out)
	if !strings.Contains(s, "upstream_eof") {
		t.Errorf("expected upstream_eof incomplete, got: %s", s)
	}
	if strings.Contains(s, "tentative") {
		t.Errorf("tentative output should not be flushed on EOF: %s", s)
	}
}

// TestContinuationUpstreamError: continuation round returns >= 400.
func TestContinuationUpstreamError(t *testing.T) {
	deps := Deps{
		Round1Body:       sseStream(flatten(reasoningItemEvents(0), []string{usageEvent(516, 0, "gpt-5")})...),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("err"))}, nil
		},
	}
	out := FoldStream(context.Background(), deps)
	if !strings.Contains(string(out), "upstream_error") {
		t.Errorf("expected upstream_error incomplete")
	}
}

// TestBareErrorFrame_ForwardedAndStops: a bare upstream `error` frame (credit
// exhausted / rate limited mid-connection) must be forwarded downstream, reported
// as an error round, and stop the fold — never block waiting for a terminal.
func TestBareErrorFrame_ForwardedAndStops(t *testing.T) {
	errFrame := `{"type":"error","status":429,"error":{"message":"insufficient_quota","code":"insufficient_quota"}}`
	var reports []RoundUsage
	var openedRounds int
	deps := Deps{
		Round1Body:       sseStream(flatten(reasoningItemEvents(0), []string{errFrame})...),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			openedRounds++
			return nil, nil
		},
		OnRoundUsage: func(round int, u RoundUsage) {
			reports = append(reports, u)
		},
	}
	out := FoldStream(context.Background(), deps)
	s := string(out)

	if openedRounds != 0 {
		t.Errorf("no continuation should open on error frame; got %d", openedRounds)
	}
	if !strings.Contains(s, `"type":"error"`) || !strings.Contains(s, "insufficient_quota") {
		t.Errorf("error frame not forwarded downstream: %s", s)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 usage report for the errored round, got %d", len(reports))
	}
	if reports[0].Status != "error" {
		t.Errorf("round status should be error, got %q", reports[0].Status)
	}
	if reports[0].HTTPStatus != 429 {
		t.Errorf("error status should be 429, got %d", reports[0].HTTPStatus)
	}
}

// TestOnRoundUsageCalledPerRound: logging callback fires once per round.
func TestOnRoundUsageCalledPerRound(t *testing.T) {
	round2 := sseStream(flatten(messageItemEvents(0, "final"), []string{usageEvent(200, 100, "gpt-5")})...)
	var reports []RoundUsage
	deps := Deps{
		Round1Body:       sseStream(flatten(reasoningItemEvents(0), []string{usageEvent(516, 0, "gpt-5")})...),
		Round1StatusCode: 200,
		BaseBody:         map[string]any{"model": "gpt-5"},
		OrigInput:        []any{"orig"},
		OpenRound: func(ctx context.Context, payload map[string]any) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: round2}, nil
		},
		OnRoundUsage: func(round int, u RoundUsage) {
			reports = append(reports, u)
		},
	}
	FoldStream(context.Background(), deps)
	if len(reports) != 2 {
		t.Fatalf("expected 2 usage reports (one per round), got %d", len(reports))
	}
	if reports[0].Round != 1 || reports[0].ReasoningTokens != 516 {
		t.Errorf("round1 report wrong: %+v", reports[0])
	}
	if reports[1].Round != 2 || reports[1].ReasoningTokens != 200 {
		t.Errorf("round2 report wrong: %+v", reports[1])
	}
}
