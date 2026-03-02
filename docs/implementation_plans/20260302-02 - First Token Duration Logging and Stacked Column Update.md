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

## Post-Release Hotfix Addendum (2026-03-02)

Additional fixes were shipped after the initial plan implementation to address
production-observed behavior:

1. `77865a8` - `fix(requestlog): fallback updated SSE payload fetch to avoid stuck pending logs`
   - Scope: `backend-go/internal/requestlog/pg_notify.go`
   - Fix: when cross-instance `log:updated` complete-record fetch fails, fallback
     to partial-record fetch and still broadcast update.
   - User-visible impact: reduced risk of rows staying `pending` on other
     instances due to dropped update events.

2. `693a31b` - `fix(requestlog): avoid postgres NULL typing failure in Update`
   - Scope: `backend-go/internal/requestlog/manager.go`
   - Fix: replaced PostgreSQL-fragile `? IS NOT NULL` usage in `Update()` with
     typed-safe `COALESCE(first_token_time, ?)` binding.
   - User-visible impact: prevents completed/error rows from remaining
     `pending` due to failed update writes.

3. `fcc0f11` - `fix(request-log): refine stacked duration spacing and header label`
   - Scope: `frontend/src/components/RequestLogTable.vue`
   - Fixes:
     - align stacked `First Token Duration + Duration` values to match duration
       alignment preference.
     - add right-side spacing after `ms` to reduce visual column sticking.
     - when stacked mode is active, primary stacked header displays
       `Duration`.

4. `c18122c` - `fix(first-token): detect responses fallback text events`
   - Scope:
     - `backend-go/internal/utils/first_token_detector.go`
     - `backend-go/internal/utils/first_token_detector_test.go`
   - Fix: expanded Responses SSE first-token detection beyond
     `response.output_text.delta/done` to include fallback text paths in:
     - `response.content_part.added/done`
     - `response.output_item.added/done`
     - `response.completed/done` output payload
   - Goal: reduce false negatives where streamed requests contain text but
     `firstTokenTime` was previously not captured.

5. `(this commit)` - `fix(first-token): fallback to first SSE payload for responses streams`
   - Scope:
     - `backend-go/internal/handlers/responses.go`
     - `backend-go/internal/handlers/first_token.go`
     - `backend-go/internal/handlers/first_token_test.go`
     - `backend-go/internal/handlers/first_token_stream_paths_test.go`
   - Fix: when `/v1/responses` stream rows do not emit detectable text-token
     events (for example tool-call-only streams), capture first-token timing from
     the first non-empty `data:` SSE payload as a best-effort fallback.
   - Guardrails:
     - detector-driven text-token timestamp still takes precedence when present.
     - fallback ignores empty payload and `data: [DONE]`.

6. `(this commit)` - `fix(first-token): complete fallback coverage + hook-ingest fields`
   - Scope:
     - `backend-go/internal/handlers/requestlog_handler.go`
     - `backend-go/internal/handlers/chat_completions.go`
     - `backend-go/internal/handlers/gemini.go`
     - `backend-go/internal/handlers/proxy.go`
     - `backend-go/internal/handlers/first_token.go`
     - `backend-go/internal/handlers/hook_log_ingest_test.go`
     - `backend-go/internal/handlers/first_token_stream_paths_test.go`
     - `backend-go/internal/handlers/first_token_test.go`
   - Fixes:
     - hook ingest now accepts and persists `firstTokenTime` and
       `firstTokenDurationMs` (with validation + pending normalization).
     - first-payload fallback now applies to stream paths in
       chat-completions, gemini, and messages/proxy (not only responses).
     - added regression tests for payload/chunk fallback paths and hook-ingest
       first-token fields.

### Hotfix Validation Snapshot

- Backend validation:
  - `cd backend-go && go test ./internal/requestlog ./internal/handlers ./internal/forwardproxy ./internal/database -count=1`
  - `cd backend-go && go test ./internal/utils ./internal/handlers ./internal/forwardproxy ./internal/requestlog -count=1`
- Frontend validation:
  - `cd frontend && bun run type-check`
  - `cd frontend && bun run build`
