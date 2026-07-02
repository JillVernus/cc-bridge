package handlers

import (
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/continuethinking"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/types"
)

// Regression test for two continue-thinking main-log bugs:
//  1. client attribution (ClientID/SessionID/APIKeyID) disappeared from Add-path
//     log rows (WebSocket turns and SSE continuation rounds).
//  2. the round-1 request's output-token metric must always be captured.
//
// The main log is the source of truth for per-client request count and cost, so
// every original request's metrics must be recorded regardless of whether
// continue-thinking is enabled.
func TestContinueThinkingRoundLogAttribution(t *testing.T) {
	upstream := &config.UpstreamConfig{
		Name:        "codex-oauth",
		ServiceType: "openai-oauth",
		Index:       7,
	}
	originalReq := &types.ResponsesRequest{Model: "gpt-5"}
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	round1Usage := continuethinking.RoundUsage{
		Round:           1,
		InputTokens:     100,
		OutputTokens:    862,
		CachedTokens:    0,
		TotalTokens:     962,
		ReasoningTokens: 516,
		ResponseModel:   "gpt-5",
		HTTPStatus:      200,
		Status:          requestlog.StatusCompleted,
		FirstEventAt:    start.Add(50 * time.Millisecond),
		RequestSentAt:   start,
		RoundCompleteAt: start.Add(800 * time.Millisecond),
	}
	apiKeyID := int64(42)
	client := clientAttribution{clientID: "codex", sessionID: "sess-abc", apiKeyID: &apiKeyID}

	// SSE round 1: the pre-created pending row already carries client attribution;
	// finalize updates it in place. Output tokens must be captured.
	t.Run("SSE_round1_updates_existing_log", func(t *testing.T) {
		mgr := newTestRequestLogManager(t)

		pending := &requestlog.RequestLog{
			Status:      requestlog.StatusPending,
			InitialTime: start,
			Model:       "gpt-5",
			Stream:      true,
			Transport:   "sse",
			Endpoint:    "/v1/responses",
			ClientID:    "codex",
			SessionID:   "sess-abc",
			APIKeyID:    &apiKeyID,
		}
		if err := mgr.Add(pending); err != nil {
			t.Fatalf("add pending: %v", err)
		}

		id := finalizeContinueThinkingRoundLog(
			mgr, pending.ID, 1, round1Usage, upstream, originalReq,
			7, "codex-oauth", false, "", false, start, "sse", client,
		)
		if id != pending.ID {
			t.Fatalf("round-1 should finalize existing log, got %q want %q", id, pending.ID)
		}

		got, err := mgr.GetByID(pending.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.OutputTokens != 862 {
			t.Errorf("output tokens not captured: got %d want 862", got.OutputTokens)
		}
		if got.ClientID != "codex" || got.SessionID != "sess-abc" {
			t.Errorf("client attribution lost: clientID=%q sessionID=%q", got.ClientID, got.SessionID)
		}
		if got.APIKeyID == nil || *got.APIKeyID != 42 {
			t.Errorf("apiKeyID lost: %v", got.APIKeyID)
		}
	})

	// WebSocket round 1: no pre-created row, so finalize Adds a new one. It must
	// carry both the output-token metric and the client attribution.
	t.Run("WS_round1_adds_attributed_log", func(t *testing.T) {
		mgr := newTestRequestLogManager(t)

		id := finalizeContinueThinkingRoundLog(
			mgr, "", 1, round1Usage, upstream, originalReq,
			7, "codex-oauth", false, "", false, start, "ws", client,
		)
		if id == "" {
			t.Fatalf("WS round-1 should create a log row")
		}

		got, err := mgr.GetByID(id)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.OutputTokens != 862 {
			t.Errorf("output tokens not captured: got %d want 862", got.OutputTokens)
		}
		if got.ClientID != "codex" || got.SessionID != "sess-abc" {
			t.Errorf("client attribution missing on WS row: clientID=%q sessionID=%q", got.ClientID, got.SessionID)
		}
		if got.APIKeyID == nil || *got.APIKeyID != 42 {
			t.Errorf("apiKeyID missing on WS row: %v", got.APIKeyID)
		}
	})

	// A continuation round (round > 1) also Adds a new row and must stay attributed
	// to the same client, tagged as a folded round.
	t.Run("continuation_round_stays_attributed", func(t *testing.T) {
		mgr := newTestRequestLogManager(t)

		round2Usage := round1Usage
		round2Usage.Round = 2
		id := finalizeContinueThinkingRoundLog(
			mgr, "prev-round-1-id", 2, round2Usage, upstream, originalReq,
			7, "codex-oauth", false, "", false, start, "sse", client,
		)
		if id == "" {
			t.Fatalf("continuation round should create a log row")
		}

		got, err := mgr.GetByID(id)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.ClientID != "codex" || got.SessionID != "sess-abc" {
			t.Errorf("client attribution lost on continuation row: clientID=%q sessionID=%q", got.ClientID, got.SessionID)
		}
		if got.FailoverInfo != "continue_thinking round 2" {
			t.Errorf("continuation row not tagged: %q", got.FailoverInfo)
		}
	})
}
