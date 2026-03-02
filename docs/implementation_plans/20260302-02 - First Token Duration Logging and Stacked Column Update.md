# First Token Duration Logging and Stacked Column Update

## Date

2026-03-02

## Goal

Add first-token timing to request logs for streaming requests, expose it in API/SSE/UI,
and replace Request Log stacking option from `Time + Duration` to
`First Token Duration + Duration` while keeping the existing duration column.

## Reviewer-Approved Plan (v4)

Approval status: **approved** (reviewer gate passed)

### Phase 0: Semantics + Shared Detector

Implement one shared helper:

- `backend-go/internal/utils/first_token_detector.go`

Protocol-specific first-token semantics:

1. Claude-style stream:
- first token = first `event: content_block_delta` with non-empty `data:` payload.

2. OpenAI Chat SSE:
- first token = first `data:` JSON chunk where `choices[].delta.content` is non-empty.

3. OpenAI Responses SSE:
- first token = first `response.output_text.delta` with non-empty text.
- fallback = `response.output_text.done` with non-empty text if no delta arrived.

4. Gemini raw/SSE:
- first token = first streamed JSON fragment containing non-empty text part (`"text":"..."`).

Memory safety constraints:

- detector uses bounded sliding window only (fixed max bytes).
- detector stops parsing once first token is detected.

Non-stream semantics:

- `firstTokenTime` is null/absent.
- `firstTokenDurationMs` is `0`.

### Phase 1: Schema and Contracts (Both Storage Backends)

Add request log columns:

- `first_token_time` (nullable)
- `first_token_duration_ms` (`INTEGER DEFAULT 0`)

Apply in both storage modes:

1. SQLite requestlog manager:
- base schema in `requestlog.Manager.initSchema`
- additive migration checks in `initSchema`

2. Unified DB migrations:
- add migration file:
  - `backend-go/internal/database/migrations/007_request_logs_first_token_timing.sql`

Contract and query updates:

- `backend-go/internal/requestlog/types.go`
  - `RequestLog.FirstTokenTime *time.Time`
  - `RequestLog.FirstTokenDurationMs int64`
- `backend-go/internal/requestlog/manager.go`
  - Add/Update writes
  - `GetRecent`
  - `getCompleteRecordForSSE`
  - `GetByID`
- `backend-go/internal/requestlog/pg_notify.go`
  - partial record fetch for cross-instance SSE
- `backend-go/internal/requestlog/events.go`
  - `log:created` and `log:updated` payloads
- forward-proxy completion record builders

Frontend contract updates:

- `frontend/src/services/api.ts`
- `frontend/src/composables/useLogStream.ts`

Use optional `firstTokenTime` and numeric `firstTokenDurationMs`.

### Phase 2: Streaming Capture Implementation (All Paths)

Use shared detector in all streaming paths:

- `backend-go/internal/handlers/proxy.go`
- `backend-go/internal/handlers/responses.go`
- `backend-go/internal/handlers/gemini.go`
- `backend-go/internal/handlers/chat_completions.go`
- `backend-go/internal/forwardproxy/interceptor.go`
- `backend-go/internal/forwardproxy/anthropic.go`
- `backend-go/internal/forwardproxy/server.go` (HTTP-forward SSE path)

Persistence safety rule:

- capture first-token timing in-memory during stream.
- persist only in existing final completion/error `Update(...)` for that row.
- no separate mid-stream full-row update (avoids pending-row clobbering).

Retry/failover rule:

- keep per-attempt semantics aligned with current pending-log lifecycle/start time.

### Phase 3: Frontend UI + Stacking

RequestLog table/UI:

- add rendering for `firstTokenDurationMs`.
- replace stacking pair label/content from:
  - `Time + Duration`
  - to `First Token Duration + Duration`.
- keep current duration column separate.

Display rules:

- pending: spinner/placeholder.
- missing first-token metric: `—`.

i18n updates:

- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`

### Phase 4: Tests, Validation, Changelog

Persistence/contract tests:

- requestlog manager read/write/select coverage for new fields.
- nullable `firstTokenTime` behavior coverage.
- SSE payload field coverage for created/updated events.

Explicit per-path streaming tests:

- proxy/messages: first content delta capture.
- responses: `output_text.delta` + `output_text.done` fallback capture.
- gemini: raw fragment text detection capture with bounded detector.
- chat-completions: streaming helper capture and final log write propagation.
- forward-proxy MITM + HTTP-forward SSE branches capture.
- failover/retry per-attempt isolation.
- detector unit tests for:
  - chunk-boundary parsing
  - empty/whitespace deltas
  - tool-call-only streams
  - stop-after-detect behavior

Validation commands:

- `cd backend-go && go test ./internal/requestlog ./internal/handlers ./internal/forwardproxy ./internal/database`
- `cd frontend && bun run type-check`

Changelog update:

- add behavior notes for first-token fields and non-stream nullability.

## Execution Gates (Mandatory)

For each phase:

1. Implement
2. Reviewer pass
3. Fix feedback
4. Reviewer approval before moving to next phase

After all phases:

- run post-merge reviewer pass on merged state
- close only after explicit post-merge reviewer approval
