package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/continuethinking"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

// wsFrameConn is the minimal websocket surface the fold driver needs. It lets
// tests substitute a fake and keeps the driver focused on frame I/O.
type wsFrameConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
}

// wsFoldDriver implements continuethinking.Driver over a single WebSocket
// transport: continuation rounds are `response.create` frames sent back over the
// same upstream socket, and folded downstream events are written as individual
// frames to the client socket.
//
// One driver instance serves exactly one client turn (one `response.create`).
// Round 1's request frame is dispatched by the orchestrator before Fold runs, so
// the first ReadRound simply reads the upstream response; OpenContinuation
// dispatches rounds 2..N.
type wsFoldDriver struct {
	client   wsFrameConn
	upstream wsFrameConn

	// Per-round request trace. Seeded with the round-1 frame by the orchestrator;
	// overwritten by each OpenContinuation.
	reqBody   []byte
	reqSentAt time.Time

	// readErr / writeErr capture the first fatal transport error so the
	// orchestrator can tear down the connection after Fold returns.
	readErr  error
	writeErr error
}

// ReadRound reads upstream frames until a terminal event or transport error.
func (d *wsFoldDriver) ReadRound(ctx context.Context) (continuethinking.RoundInput, bool) {
	var events []continuethinking.Event
	var firstPayloadAt, completeAt time.Time
	var respBuf bytes.Buffer
	sawTerminal := false

	for {
		msgType, payload, err := d.upstream.ReadMessage()
		if err != nil {
			d.readErr = err
			completeAt = time.Now()
			break
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}
		if firstPayloadAt.IsZero() {
			firstPayloadAt = time.Now()
		}
		if respBuf.Len() > 0 {
			respBuf.WriteByte('\n')
		}
		respBuf.Write(payload)

		var obj map[string]any
		if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
			continue
		}
		events = append(events, continuethinking.Event{Data: obj})
		if t, _ := obj["type"].(string); continuethinking.IsTerminalType(t) {
			sawTerminal = true
			completeAt = time.Now()
			break
		}
	}

	// A read error before any events were seen means the round could not be read
	// at all (treated as an upstream error by the fold).
	if len(events) == 0 && !sawTerminal {
		return continuethinking.RoundInput{}, false
	}

	return continuethinking.RoundInput{
		Events:         events,
		FirstPayloadAt: firstPayloadAt,
		RequestSentAt:  d.reqSentAt,
		CompleteAt:     completeAt,
		StatusCode:     200,
		RequestBody:    d.reqBody,
		ResponseBody:   respBuf.Bytes(),
	}, true
}

// OpenContinuation dispatches a continuation round as a `response.create` frame
// over the same upstream socket.
func (d *wsFoldDriver) OpenContinuation(ctx context.Context, payload map[string]any) bool {
	frame := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		frame[k] = v
	}
	frame["type"] = "response.create"
	// The WebSocket transport is inherently streaming; the `stream` flag belongs
	// to the HTTP/SSE body and is not part of a response.create frame.
	delete(frame, "stream")

	b, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	d.reqBody = b
	d.reqSentAt = time.Now()
	if err := d.upstream.WriteMessage(websocket.TextMessage, b); err != nil {
		d.writeErr = err
		return false
	}
	return true
}

// Emit writes one folded event downstream as a client frame.
func (d *wsFoldDriver) Emit(event map[string]any) {
	b, err := json.Marshal(event)
	if err != nil {
		return
	}
	if err := d.client.WriteMessage(websocket.TextMessage, b); err != nil && d.writeErr == nil {
		d.writeErr = err
	}
}

// EmitDone is a no-op: the WebSocket transport terminates a turn with a terminal
// event frame, not an SSE `[DONE]` sentinel.
func (d *wsFoldDriver) EmitDone() {}

// wsFoldContext carries the request-independent state a WebSocket continue-thinking
// turn needs for per-round logging.
type wsFoldContext struct {
	c           *gin.Context
	envCfg      *config.EnvConfig
	cfgManager  *config.ConfigManager
	reqLog      *requestlog.Manager
	scheduler   *scheduler.ChannelScheduler
	upstream    *config.UpstreamConfig
	channelID   int
	channelName string
}

// foldResponsesWebSocketFrames drives continue-thinking over a WebSocket
// connection. It is used instead of the raw bidirectional proxy for channels
// with continue-thinking enabled. Each client `response.create` frame starts one
// fold turn: the round-1 frame is forwarded upstream (with encrypted reasoning
// forced on), then the fold reads upstream rounds, silently continues on
// 518n-2 reasoning truncation, and folds all rounds into one downstream response.
//
// The loop is turn-serialized: while a turn folds it reads only from upstream.
// This matches the Codex request/response model over WebSocket (the client waits
// for a terminal before issuing the next request). Non-`response.create` client
// frames are forwarded to upstream as-is.
func foldResponsesWebSocketFrames(
	fc *wsFoldContext,
	clientConn *websocket.Conn,
	upstreamConn *websocket.Conn,
) error {
	for {
		msgType, payload, err := clientConn.ReadMessage()
		if err != nil {
			return normalizeResponsesWebSocketCloseError(err)
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}

		// Mirror the raw proxy: sanitize OAuth encrypted-reasoning state carried in
		// the client's own frame before it reaches upstream.
		payload = sanitizeCodexOAuthWebSocketClientPayload(fc.upstream, payload)

		if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
			// Pass-through non-request frames (e.g. control messages).
			if werr := upstreamConn.WriteMessage(msgType, payload); werr != nil {
				return werr
			}
			continue
		}

		if terr := runResponsesWebSocketFoldTurn(fc, clientConn, upstreamConn, payload); terr != nil {
			return terr
		}
	}
}

// runResponsesWebSocketFoldTurn folds one client turn. It forces the encrypted
// reasoning include on the round-1 frame, dispatches it upstream, runs the fold,
// and finalizes one request-log row per folded round.
func runResponsesWebSocketFoldTurn(
	fc *wsFoldContext,
	clientConn *websocket.Conn,
	upstreamConn *websocket.Conn,
	createFrame []byte,
) error {
	startTime := time.Now()

	// Force encrypted reasoning include so truncated rounds can be replayed.
	round1Frame := ensureContinueThinkingInclude(createFrame)

	// Parse the frame into the base body for continuation rebuilds.
	var baseBody map[string]any
	if err := json.Unmarshal(round1Frame, &baseBody); err != nil || baseBody == nil {
		baseBody = map[string]any{}
	}
	origInput, _ := baseBody["input"].([]any)

	model := strings.TrimSpace(gjson.GetBytes(round1Frame, "model").String())
	serviceTier := normalizeResponsesServiceTier(gjson.GetBytes(round1Frame, "service_tier").String())
	if serviceTier == "" && strings.EqualFold(gjson.GetBytes(round1Frame, "speed").String(), "fast") {
		serviceTier = "priority"
	}
	isFastMode := strings.EqualFold(serviceTier, "priority")
	originalReq := &types.ResponsesRequest{Model: model}

	// Derive the original request's client identity from the create frame + gin
	// context (the WS fold has no pre-created log row) so every folded row is
	// attributed to the same client as the original request.
	client := clientAttributionFromFrame(fc.c, createFrame)

	debugLogEnabled := fc.cfgManager != nil && fc.cfgManager.GetDebugLogConfig().Enabled

	driver := &wsFoldDriver{
		client:    clientConn,
		upstream:  upstreamConn,
		reqBody:   round1Frame,
		reqSentAt: startTime,
	}

	// Dispatch round 1 upstream before folding (the fold's first ReadRound reads
	// the response).
	if err := upstreamConn.WriteMessage(websocket.TextMessage, round1Frame); err != nil {
		return err
	}

	cfg := continuethinking.FoldConfig{
		BaseBody:  baseBody,
		OrigInput: origInput,
		OnRoundUsage: func(round int, u continuethinking.RoundUsage) {
			if fc.reqLog == nil {
				return
			}
			roundLogID := finalizeContinueThinkingRoundLog(
				fc.reqLog, "", round, u, fc.upstream, originalReq,
				fc.channelID, fc.channelName, isFastMode, serviceTier, false, startTime, "ws", client,
			)
			if debugLogEnabled && roundLogID != "" {
				saveContinueThinkingRoundDebug(fc.reqLog, fc.cfgManager, roundLogID, u.Trace)
			}
			if fc.scheduler != nil {
				if u.Status == requestlog.StatusCompleted {
					fc.scheduler.RecordSuccessWithStatusDetail(fc.channelID, true, u.HTTPStatus, model, fc.channelName)
				} else if u.Status == requestlog.StatusError {
					fc.scheduler.RecordFailureWithStatusDetail(fc.channelID, true, u.HTTPStatus, model, fc.channelName)
				}
			}
		},
		Log: func(format string, args ...any) {
			if fc.envCfg != nil && fc.envCfg.ShouldLog("info") {
				log.Printf(format, args...)
			}
		},
	}

	continuethinking.Fold(fc.c.Request.Context(), driver, cfg)

	if driver.writeErr != nil {
		return driver.writeErr
	}
	if driver.readErr != nil {
		return normalizeResponsesWebSocketCloseError(driver.readErr)
	}
	return nil
}

// normalizeResponsesWebSocketCloseError maps a normal WebSocket close to nil so
// clean disconnects are not treated as failures.
func normalizeResponsesWebSocketCloseError(err error) error {
	if err == nil {
		return nil
	}
	if websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived) {
		return nil
	}
	return err
}
