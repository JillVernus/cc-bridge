package continuethinking

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// RoundOpener issues one upstream streaming request with a payload already
// shaped by BuildRoundPayload. The handler owns transport (OAuth headers vs
// API-key passthrough) and must NOT sanitize the replayed encrypted reasoning.
// The returned response body is closed by the fold.
type RoundOpener func(ctx context.Context, payload map[string]any) (*http.Response, error)

// Deps is the HTTP/SSE input to FoldStream. It keeps the handler decoupled from
// the fold internals: handlers wire closures, the package stays pure.
type Deps struct {
	// Round1Body is the already-opened round-1 upstream response body. The fold
	// closes it when done with the round.
	Round1Body io.ReadCloser
	// Round1StatusCode is the HTTP status of the round-1 response.
	Round1StatusCode int
	// Round1Headers is the round-1 upstream response headers (for debug logging).
	Round1Headers http.Header
	// Round1RequestBody is the serialized round-1 upstream request body (debug).
	Round1RequestBody []byte
	// Round1RequestSentAt is when round 1's upstream request was dispatched.
	Round1RequestSentAt time.Time

	// BaseBody is the parsed client request body (used to rebuild continuation
	// rounds via BuildRoundPayload). Not mutated.
	BaseBody map[string]any
	// OrigInput is the original `input` items from the client request.
	OrigInput []any

	// OpenRound issues one upstream streaming continuation request.
	OpenRound RoundOpener

	// OnRoundUsage reports each round's real upstream usage for logging. May be nil.
	OnRoundUsage func(round int, u RoundUsage)

	// Log, if non-nil, is used for the per-round decision log line.
	Log func(format string, args ...any)
}

// FoldStream drives the N-round fold over the HTTP/SSE transport and returns the
// downstream SSE byte stream. It blocks until the fold completes. This is the
// entry point the /v1/responses streaming handler imports.
func FoldStream(ctx context.Context, deps Deps) []byte {
	d := &sseDriver{
		opener:      deps.OpenRound,
		body:        deps.Round1Body,
		statusCode:  deps.Round1StatusCode,
		reqBody:     deps.Round1RequestBody,
		reqSentAt:   deps.Round1RequestSentAt,
		respStatus:  deps.Round1StatusCode,
		respHeaders: deps.Round1Headers,
	}
	Fold(ctx, d, FoldConfig{
		BaseBody:     deps.BaseBody,
		OrigInput:    deps.OrigInput,
		OnRoundUsage: deps.OnRoundUsage,
		Log:          deps.Log,
	})
	return d.out
}

// sseDriver implements Driver over the HTTP/SSE transport: each round is its own
// HTTP request, and downstream events are accumulated as SSE bytes.
type sseDriver struct {
	opener RoundOpener

	// current round source + trace metadata.
	body        io.ReadCloser
	statusCode  int
	reqBody     []byte
	reqSentAt   time.Time
	respStatus  int
	respHeaders http.Header

	out []byte
}

func (d *sseDriver) ReadRound(ctx context.Context) (RoundInput, bool) {
	if d.body == nil {
		return RoundInput{}, false
	}
	var respBuf bytes.Buffer
	tee := io.TeeReader(d.body, &respBuf)
	events, firstPayloadAt := IncrementalSSEWithTiming(tee)
	completeAt := time.Now()
	_ = d.body.Close()
	d.body = nil
	return RoundInput{
		Events:          events,
		FirstPayloadAt:  firstPayloadAt,
		RequestSentAt:   d.reqSentAt,
		CompleteAt:      completeAt,
		StatusCode:      d.statusCode,
		RequestBody:     d.reqBody,
		ResponseBody:    respBuf.Bytes(),
		ResponseHeaders: d.respHeaders,
	}, true
}

func (d *sseDriver) OpenContinuation(ctx context.Context, payload map[string]any) bool {
	if b, err := json.Marshal(payload); err == nil {
		d.reqBody = b
	}
	d.reqSentAt = time.Now()
	resp, err := d.opener(ctx, payload)
	if err != nil || resp == nil {
		return false
	}
	d.respStatus = resp.StatusCode
	d.respHeaders = resp.Header
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return false
	}
	d.body = resp.Body
	d.statusCode = resp.StatusCode
	return true
}

func (d *sseDriver) Emit(event map[string]any) {
	d.out = append(d.out, SerializeEvent(event)...)
}

func (d *sseDriver) EmitDone() {
	d.out = append(d.out, SerializeDone()...)
}
