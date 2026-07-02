package continuethinking

import (
	"context"
	"net/http"
	"time"
)

// Driver abstracts the transport a fold runs over. The fold engine
// (foldState.run) is transport-agnostic: it reads one round's parsed events,
// decides continue/stop, dispatches continuation rounds, and emits folded
// downstream events — all through this interface. Two implementations exist:
//   - sseDriver: HTTP/SSE transport (the /v1/responses streaming path). Each
//     round is a separate HTTP request; downstream events are accumulated as
//     SSE bytes and returned by FoldStream.
//   - a WebSocket driver (in the handlers package): a single persistent
//     bidirectional connection; continuation rounds are `response.create`
//     frames sent back over the same socket; downstream events are written as
//     individual frames to the client connection.
type Driver interface {
	// ReadRound reads the current round's events until a terminal event or
	// upstream EOF. ok is false when there was no readable body (treated as an
	// upstream error). The returned RoundInput carries the round's timing and
	// raw request/response artifacts for the debug trace.
	ReadRound(ctx context.Context) (RoundInput, bool)

	// OpenContinuation dispatches the next round upstream with the fold-shaped
	// payload and makes it the current round (the following ReadRound reads it).
	// ok is false on any dispatch failure or upstream >=400 status.
	OpenContinuation(ctx context.Context, payload map[string]any) bool

	// Emit writes one folded event downstream (SSE bytes or a WS frame).
	Emit(event map[string]any)

	// EmitDone writes the stream terminator (SSE `[DONE]`; no-op for WS).
	EmitDone()
}

// RoundInput is one round's parsed events plus the metadata the fold needs for
// per-round usage/timing/debug logging. A driver returns this from ReadRound.
type RoundInput struct {
	// Events are the round's parsed SSE/WS events in order.
	Events []Event
	// FirstPayloadAt is when the round's first payload was read (TTFT signal).
	FirstPayloadAt time.Time
	// RequestSentAt is when the round's upstream request was dispatched.
	RequestSentAt time.Time
	// CompleteAt is when the round's upstream stream finished.
	CompleteAt time.Time
	// StatusCode is the round's upstream HTTP status (200 for WS frames).
	StatusCode int
	// RequestBody is the serialized upstream request body for the round.
	RequestBody []byte
	// ResponseBody is the raw upstream response bytes (joined events for WS).
	ResponseBody []byte
	// ResponseHeaders mirror the upstream HTTP response (nil for WS).
	ResponseHeaders http.Header
}

// FoldConfig is the transport-independent input to the fold engine: the base
// request body used to rebuild continuation rounds, the original input items,
// and the logging callbacks.
type FoldConfig struct {
	// BaseBody is the parsed client request body (used to rebuild continuation
	// rounds via BuildRoundPayload). Not mutated.
	BaseBody map[string]any
	// OrigInput is the original `input` items from the client request; the
	// continuation replay tail is appended to a copy of this.
	OrigInput []any
	// OnRoundUsage reports each round's real upstream usage for logging. May be nil.
	OnRoundUsage func(round int, u RoundUsage)
	// Log, if non-nil, is used for the per-round decision log line.
	Log func(format string, args ...any)
}

// Fold drives the N-round fold over the given driver. It blocks until the fold
// completes (clean finish, cap hit, or error). Downstream output is delivered
// through driver.Emit / driver.EmitDone.
func Fold(ctx context.Context, driver Driver, cfg FoldConfig) {
	st := &foldState{cfg: cfg, driver: driver, ctx: ctx}
	st.run()
}
