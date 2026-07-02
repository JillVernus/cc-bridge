# Continue-Thinking (Codex reasoning-truncation fold) — Design

## Background

OpenAI Codex recently hard-caps the reasoning token budget, forcing a cut at
`reasoning_tokens == 518*n - 2` (516, 1034, 1552, ...). This produces low-quality
output because the model's chain of thought is truncated mid-stream.

The reference project `sample_source/CodexCont` solves this with a middleware
that detects the truncation fingerprint, silently asks the model to continue
thinking, and folds multiple upstream streaming rounds into one downstream SSE
response. cc-bridge already proxies `/v1/responses`, so the feature overlaps. We
migrate the logic into cc-bridge behind a per-channel toggle.

## Requirements (from user)

1. Recreate the logic in cc-bridge, **separated** so it can be removed later in
   a fast, safe way.
2. Channel-level toggle on **Codex channels only**.
3. The main request log must still show **each upstream round** as a separate
   entry (3 rounds folded into 1 client response → 3 log rows), not one merged
   row.

## Decisions (locked during brainstorm)

| Decision | Choice |
|----------|--------|
| Continuation method | `commentary` (clean hidden `phase:"commentary"` assistant message) — hardcoded |
| Tuning knobs | hardcoded to CodexCont defaults |
| Truncation step | hardcoded const `518` (fingerprint `518n-2`: 516/1034/1552/...) |
| Code placement | separate package `internal/continuethinking/`, `FoldStream` returns SSE bytes |
| Logging | round 1 = existing client-request log; continuation rounds = new pending logs (mirrors existing failover pattern) |
| Eligible channels | both `responses` and `openai-oauth` serviceType (existing `isNativeResponsesPoolChannel` gate) |
| UI | one channel-level toggle, conditionally shown for native Responses-pool channels |

Hardcoded constants (package-level):
```
TruncationStep     = 518
MaxContinue        = 8   // hard round cap after round 1
MinN               = 1   // continue only when tier n >= min_n
MaxN               = 6   // stop forcing once n > max_n
MaxTotalTokens     = 0   // off (no cumulative cost cap)
ForwardMarker      = false
RechunkFinalAnswer = false
MarkerText         = "Continue thinking..."
Method             = "commentary"
```

## Architecture

New isolated package `internal/continuethinking/`. No reverse imports into
handlers — `FoldStream` is the single entry point.

```
internal/continuethinking/
  detect.go    // IsTruncation(tokens) bool, TierN(tokens) int, ShouldContinue(tokens) bool
  payload.go   // BuildRoundPayload(baseBody, inputItems) map + CommentaryMessage(text)
  usage.go     // ParseReasoningTokens + RoundUsage helpers
  sse.go       // incremental SSE parser + serializer
  fold.go      // FoldStream(ctx, deps) (<-chan []byte, error)
  *_test.go
```

### Deps contract

```go
type Deps struct {
    Round1Body       io.ReadCloser   // already-open round-1 upstream stream
    Round1StatusCode int
    BaseBody         map[string]any  // parsed client request body (for rebuilding rounds 2+)
    OrigInput        []any           // original input items from round 1
    OpenRound        func(ctx context.Context, payload map[string]any) (*http.Response, error)
    OnRoundUsage     func(round int, u RoundUsage) string // returns logID; optional (nil = no logging)
    Log              func(format string, args ...any)    // optional
}
```

The handler owns transport (OAuth headers vs API-key passthrough) and logging.
The package owns the fold algorithm.

### Removal path

Delete `internal/continuethinking/` package + remove the gating `if` block +
drop the toggle field (struct, DTO, normalizer, migration, frontend) + drop the
`OpenRound`/`OnRoundUsage` closures. ~2 touch sites in `responses.go`; all else
isolated.

## Fold state machine

Direct port of CodexCont `fold_stream` (proxy.py:289).

Per round:
1. Read upstream SSE line-by-line (incremental, chunk-boundary safe).
2. `response.created`/`response.in_progress`: forward on round 1 only (rewrite
   `sequence_number`), capture `baseResponse`.
3. Reasoning items: forward live, rewriting `output_index` (via per-round
   `oiMap`) and `sequence_number`. On `output_item.done`, stash item in
   `roundReasoning` + `finalOutput`.
4. Message / function_call items: **buffer** as tentative output (do NOT
   forward). On terminal decision either discard (continue) or flush (clean).
5. Terminal event (`response.completed`/`failed`/`incomplete`): capture
   `usage`, break inner loop.

Round decision:
```
hasEnc       = last roundReasoning item has encrypted_content
withinCap    = true (MaxTotalTokens=0, off)
doContinue   = sawTerminal && ShouldContinue(rt) && hasEnc && round <= MaxContinue
```

- If `doContinue`: discard buffered output, append
  `[*roundReasoning, CommentaryMessage(marker)]` to `replayTail`, build next
  payload (`origInput + replayTail`), `OpenRound`, repeat.
- If upstream EOF without terminal: emit `response.incomplete`
  (`proxy_stopped_reason="upstream_eof"`), do NOT flush tentative output.
- If continuation round returns >= 400: emit `response.incomplete`
  (`"upstream_error"`).
- Clean finish: flush buffered output as the real answer, emit one
  reconstructed terminal event.

### agentUsage (single-response usage the client sees)

Direct port of `_agent_usage`:
- input / cached = round-1 only (NOT summed — avoids faking a blown context window)
- reasoning = summed across rounds
- output = reasoning + final round's non-reasoning part (0 if nothing flushed)
- total = input + output

### Reconstructed terminal + proxy metadata

Keep round-1's response `id`/`created_at`, take `status` from the upstream
terminal, supply reconstructed `output` + single-response `usage` + metadata:
- `proxy_rounds`: per-round `{round, reasoning_tokens, n}`
- `proxy_billed_usage`: summed upstream usage across hidden rounds
- `proxy_stopped_reason`: present when a guard stopped continuation

### SSE framing

Incremental byte framer over an `io.Reader`: buffer bytes, split on `\n`,
accumulate `data:` lines, flush on blank line. Handles events split across TCP
chunks (mirrors CodexCont `incremental_sse`). `serializeEvent` emits
`event: <type>\ndata: <json>\n\n`. `[DONE]` handled as a sentinel.

## Logging integration

`FoldStream` is logging-agnostic. It calls `OnRoundUsage(round, usage)` once per
round with that round's real upstream usage. The handler's closure does the
`reqLogManager` work:

- `round == 1`: finalize the existing `requestLogID` (the client-request log)
  with round-1 usage.
- `round >= 2`: create a NEW pending log via `reqLogManager.Add(...)` (mirrors
  the existing failover new-pending-log pattern at `responses.go:551/966`), then
  finalize it with that round's usage.

Each continuation entry's `FailoverInfo` field is set to
`"continue_thinking round N"` so folded rounds are distinguishable in the main
log view (no new DB column — reuses an existing text field; removal = stop
writing the string).

`finalizeLog` reuses the exact cost/usage logic already in
`handleResponsesSuccess` (`responses.go:2911-2970`): `pricing.GetManager()`,
`upstream.GetPriceMultipliers`, `trackResponsesUsage`. Each round's tokens and
cost are accurate.

Result (req #3): 3 fold rounds → 3 main-log rows, each with real per-round
tokens/cost, while the client sees 1 merged SSE stream.

## Channel toggle + config stack

Full-stack field `ContinueThinkingEnabled bool`, mirroring the proven
`ResponsesWebSocketEnabled` path. Eligibility gate = existing
`isNativeResponsesPoolChannel` (true for `responses` + `openai-oauth`).

| Layer | File | Change |
|-------|------|--------|
| Go struct | `config/config.go` (UpstreamConfig) | add `ContinueThinkingEnabled bool \`json:"continueThinkingEnabled,omitempty"\`` |
| Update DTO | `config/config.go` (UpstreamUpdate) | add `ContinueThinkingEnabled *bool \`json:"continueThinkingEnabled"\`` |
| Apply (responses) | `config/config.go` `updateResponsesUpstream` | nil-check apply block |
| Normalizer | `config/config.go` `normalizeUpstreamContinueThinking` | zero to false when NOT `isNativeResponsesPoolChannel`; call from `normalizeConfigCodexServiceTierOverrides` loop |
| GET handler | `handlers/config.go` `GetResponsesUpstreams` | add `"continueThinkingEnabled"` to gin.H |
| DB migration | `migrations/022_channels_continue_thinking_enabled.sql` | `ALTER TABLE channels ADD COLUMN continue_thinking_enabled BOOLEAN DEFAULT 0;` |
| DB INSERT | `db_storage.go` `insertChannelTx` | add column + value |
| DB UPDATE | `db_storage.go` `updateChannelTx` | add to SET clause + value |
| DB SELECT/Scan | `db_storage.go` `loadChannelsFrom` | add to SELECT + Scan + struct |
| Frontend TS | `api.ts` Channel interface | add `continueThinkingEnabled?: boolean` |
| Frontend form | `AddChannelModal.vue` | form state + `showContinueThinkingToggle` computed + v-switch + reset + edit-load + submit |
| i18n | locale files (en/zh) | `addChannel.continueThinking` + hint |

Default `false` (opt-in — feature is cost-incurring).

Runtime read: `handleResponsesSuccess` gating check on `upstream.ContinueThinkingEnabled`.
Scheduler reads config live every request, so toggling propagates after the next
DB poll (sub-second).

## OAuth rebuild caveat (critical)

The existing `buildCodexOAuthRequest` calls
`sanitizeCodexOAuthEncryptedReasoningState`, which **strips reasoning items
carrying `encrypted_content`** (`responses.go:1670-1672`). That is exactly the
replay data the fold needs. Therefore the `OpenRound` closure for continuation
rounds must NOT reuse `buildCodexOAuthRequest` blindly; it rebuilds the request
(carries OAuth headers, `store:false`, strips `max_output_tokens`/`max_tokens`)
but **preserves the replayed encrypted reasoning**. This mirrors CodexCont,
which never sanitizes encrypted content on continuation rounds.

## Testing

- `detect_test.go`: truncation math (516/1034/500/-1/nil), tier window, ShouldContinue boundaries.
- `sse_test.go`: framing across chunk boundaries, multi-line events, [DONE].
- `payload_test.go`: BuildRoundPayload (stream forced, include merged, previous_response_id dropped, input set), CommentaryMessage shape.
- `fold_test.go`: fixture-driven fake SSE streams via injectable `OpenRound` —
  clean-finish flush, single-fold (round 1 truncated → round 2 clean), multi-fold,
  max_continue cap, upstream-EOF incomplete, upstream-error incomplete,
  `agentUsage` reconstruction, `proxy_rounds` metadata.
