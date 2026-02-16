# Anthropic Hook Log Ingest API (Phase 1)

## Background

We want to ingest Claude Code hook-derived telemetry into cc-bridge request logs,
without proxying the original Anthropic API call path.

Phase 1 focuses only on backend ingest capability:

- Add a backend API endpoint that accepts hook-submitted log payloads
- Map payload into existing `request_logs` schema
- Keep compatibility with existing Request Log UI and SSE behavior

Phase 2 (pending) will implement the hook script/client-side sender.

## Goals

1. Provide a stable ingest endpoint for Anthropic OAuth hook telemetry
2. Reuse existing request log storage (`requestlog.Manager`)
3. Support idempotent inserts to avoid duplicate logs from hook retries
4. Preserve existing Request Log page behavior (list/stats/SSE)

## Progress (2026-02-16)

### Implementation Status

- [x] Added endpoint `POST /api/logs/hooks/anthropic`
- [x] Reused existing `/api` auth middleware (admin-level key path)
- [x] Implemented payload validation and default mapping for Anthropic OAuth mode
- [x] Implemented idempotent behavior with `requestId` as `request_logs.id`
- [x] Registered route in `backend-go/main.go`
- [x] Added focused handler tests in `backend-go/internal/handlers/hook_log_ingest_test.go`

### Review Findings & Fixes

1. **Pending lifecycle normalization**
   - Found issue: pending payloads could carry `completeTime`, `durationMs`, and `httpStatus`.
   - Fix: pending records are now normalized to:
     - `completeTime = zero`
     - `durationMs = 0`
     - `httpStatus = 0`

2. **UI-path verification coverage gap**
   - Found issue: initial tests only asserted via `GetByID`, while UI list uses `GetRecent`.
   - Fix: tests now assert `GetRecent` fields (`providerName`, `status`) after ingest.

3. **Re-review test assertion correction**
   - Found issue: asserting `status` from `GetByID` is invalid (that accessor does not read status).
   - Fix: pending status assertion moved to `GetRecent`.

4. **PostgreSQL placeholder compatibility in read paths**
   - Found issue: `requestlog.Manager.GetByID` and `GetAlias` used `?` placeholders without `convertQuery()`, causing `pq: syntax error at end of input`.
   - Fix: both methods now apply `m.convertQuery(query)` before `QueryRow`.

5. **Idempotency read-before-write hardening**
   - Found issue: hook ingest idempotency depended on a pre-insert `GetByID` read, adding unnecessary failure surface.
   - Fix: ingest now uses atomic insert + duplicate-key detection only (`created=false` on duplicate), preserving idempotency while reducing dependency on read path behavior.

6. **SSE created-event token visibility gap for final-state hook logs**
   - Found issue: hook payloads inserted as final `completed/error` records only emitted `log:created`, but created payload lacked token/cost fields and frontend initialized those fields to `0`.
   - Effect: newly ingested hook rows could appear without token metrics until manual refresh/poll reload.
   - Fix:
     - Expanded backend `LogCreatedPayload` to include duration/status/token/cost/error fields from stored record.
     - Updated PostgreSQL cross-instance partial fetch (`getPartialRecordForSSE`) to load the same fields (including `provider_name`).
     - Updated frontend `handleLogCreated` to hydrate row metrics from `log:created` payload when present.

7. **Hook log API key attribution and automatic pricing**
   - Found issue:
     - Hook-ingested logs did not store authenticated cc-bridge API key identity (`api_key_id` remained null).
     - Hook-ingested logs with token metrics but zeroed cost fields remained `price=0`.
   - Fix:
     - Hook ingest now reads `apiKeyID` from auth middleware context and stores it in `request_logs.api_key_id` (supports `0 => master`).
     - Hook ingest now auto-calculates cost breakdown via pricing manager when payload omits all cost fields (`price/inputCost/outputCost/cacheCreationCost/cacheReadCost` all zero).
     - Pricing model selection matches existing behavior: prefer `responseModel`, fallback to `model` when needed.
   - Tests added:
     - `TestIngestAnthropicHookLog_UsesAPIKeyIDFromContext`
     - `TestIngestAnthropicHookLog_AutoCalculatesPrice`

### Verification Executed

- [x] `cd backend-go && go test ./internal/handlers -run IngestAnthropicHookLog -count=1`
- [x] `cd backend-go && go test ./...`
- [x] `cd backend-go && go vet ./...`
- [x] `cd frontend && bun run build`

### Runtime Validation Note

- Live API calls to `http://172.17.0.7:3000/api/logs/hooks/anthropic` still returned `pq: syntax error at end of input` during this session.
- This indicates that runtime instance had not yet loaded the latest backend code when calls were made (deployment/restart pending).

### Phase 2 Abandoned (2026-02-16)

- Phase 2 hook sender work is abandoned (not only suspended).
- Decision reason: available Claude hook stdin data and local transcript JSONL
  do not provide enough reliable low-level API call information for this goal.
- Confirmed gaps:
  - internal upstream sub-calls are not fully observable from stdin/transcript
    in tested flows (for example missing intermediate Haiku routing calls)
  - per-call lifecycle linkage is not consistently reconstructable without
    noisy synthetic records
  - token snapshots can be transient during thinking->finalization timing
- Removed Phase 2 hook artifacts (`scripts/hooks/*`) from repository.
- Removed installed Phase 2 hooks from local Claude settings; restored original
  non-Phase2 hook (`check-api-stats.sh` on `UserPromptSubmit`).
- Phase 1 backend ingest endpoint remains in place.

## Non-Goals (Phase 1)

- No hook script implementation
- No frontend changes
- No new DB table/schema migration
- No full two-stage pending->completed lifecycle orchestration from hooks

## API Design

### Endpoint

- `POST /api/logs/hooks/anthropic`

### Auth

- Reuse existing `/api` auth middleware
- Requires valid admin-level access key (same as other `/api/logs/*` endpoints)

### Request Body (initial contract)

```json
{
  "requestId": "sessionId:assistantMessageId",
  "status": "completed",
  "initialTime": "2026-02-16T03:10:00Z",
  "completeTime": "2026-02-16T03:10:04Z",
  "durationMs": 4000,
  "model": "claude-opus-4-6",
  "responseModel": "claude-opus-4-6",
  "reasoningEffort": "",
  "stream": false,
  "inputTokens": 1200,
  "outputTokens": 220,
  "cacheCreationInputTokens": 0,
  "cacheReadInputTokens": 0,
  "price": 0,
  "inputCost": 0,
  "outputCost": 0,
  "cacheCreationCost": 0,
  "cacheReadCost": 0,
  "httpStatus": 200,
  "endpoint": "/v1/messages",
  "clientId": "client-abc",
  "sessionId": "session-xyz",
  "error": "",
  "upstreamError": ""
}
```

### Response

- `200 OK`

```json
{
  "id": "sessionId:assistantMessageId",
  "created": true
}
```

If already exists (idempotent replay):

```json
{
  "id": "sessionId:assistantMessageId",
  "created": false
}
```

## Mapping Rules

Default values for Anthropic OAuth-only ingestion:

- `type`: `claude`
- `providerName`: `anthropic-oauth`
- `endpoint`: `/v1/messages` (if omitted)
- `channelId`: `1`
- `channelUid`: `oauth:default`
- `channelName`: `Anthropic OAuth`
- `apiKeyId`: `null`
- `failoverInfo`: empty string
- `hasDebugData`: false

Status-based default HTTP status (when request omits `httpStatus`):

- `completed` -> `200`
- `error` -> `599`
- `timeout` -> `408`
- `pending` -> `0`

Time handling:

- If `initialTime` omitted: use server `now`
- If non-pending and `completeTime` omitted: use server `now`
- If `durationMs` omitted: derive from `completeTime - initialTime` (>= 0)

## Validation Rules

- `requestId` required, non-empty, length <= 200
- `status` must be one of requestlog status constants
- `model` required, non-empty, length <= 200
- RFC3339 parsing for `initialTime`/`completeTime` when provided
- Token and cost fields must be >= 0
- Reject invalid payload with `400`

## Idempotency Strategy

- Use `requestId` as `request_logs.id`
- Insert directly with `requestId` as primary key
- If insert succeeds, return `created=true`
- If duplicate-key conflict occurs, return idempotent success (`created=false`)

## Implementation Steps

### Step 1: Add Handler Method

File: `backend-go/internal/handlers/requestlog_handler.go`

- Add request DTO for hook ingest
- Add `IngestAnthropicHookLog` method
- Add payload validation, mapping, and idempotency behavior

### Step 2: Register Route

File: `backend-go/main.go`

Within existing `if reqLogManager != nil` logs route block:

- Register `POST /api/logs/hooks/anthropic`

### Step 3: Add Focused Tests

File: `backend-go/internal/handlers/hook_log_ingest_test.go`

Test cases:

- minimal valid payload insert
- duplicate `requestId` idempotent replay
- invalid status
- missing required fields
- invalid timestamps

## Risks

- Hook retries may still race across instances; duplicate-key fallback must be
  treated as success.
- Hook payload schema may evolve; Phase 1 should keep strict-but-small
  validation and avoid overfitting.

## Rollout

1. Deploy backend with ingest endpoint
2. Manually test with curl
3. Confirm log appears in `/api/logs` and SSE stream
4. Proceed to Phase 2 hook sender implementation

## Phase 2 Discussion Snapshot (2026-02-16)

### Goal

Keep Anthropic OAuth request logging as close as possible to real Claude Code behavior,
while collecting transparent per-call metrics in cc-bridge logs.

### Agreed Direction

1. Use local Claude session JSONL as the source of truth for completed usage/model data.
2. Treat one logical API call as one assistant message identity:
   - key: `sessionId + ":" + message.id`
3. Use hooks as orchestration triggers, but not as the sole metric source.
4. Prefer completion-driven ingestion (`Stop` / `SubagentStop`) for accurate final metrics.
5. If needed for UI lifecycle, optionally synthesize a pending row before completed row
   using the same `requestId`.

### JSONL Findings (current environment)

1. `assistant` records contain `message.id`, `message.model`, and `message.usage.*`.
2. The same `message.id` appears multiple times in a session (thinking/text/tool-use
   progressive updates), so dedupe is required.
3. `message.id` can repeat across different session files, so `message.id` alone is
   not globally unique.
4. Subagents have separate transcript files under `subagents/*.jsonl`, and should be
   ingested as separate streams.

### Dedupe / Selection Rule (draft)

For each `(sessionId, message.id)` group:

1. `requestId`: `sessionId:message.id`
2. `initialTime`: earliest timestamp in group
3. `completeTime`: latest timestamp in group
4. usage/model: from latest record or max-usage record in the same group
5. ingest once (idempotent replay ignored by API)

### Field Mapping Strategy (Anthropic OAuth mode)

1. From JSONL:
   - `model` / `responseModel` -> `message.model`
   - token metrics -> `message.usage.input_tokens/output_tokens/cache_*`
   - `sessionId` -> top-level `sessionId`
2. Defaulted in hook sender / backend:
   - `endpoint=/v1/messages`
   - `status=completed` (for finalized assistant message groups)
   - `httpStatus=200` unless caller decides otherwise
   - missing upstream HTTP/error internals use safe defaults

### Known Limits

JSONL does not provide reliable raw upstream HTTP metadata per low-level retry/call
(e.g., exact upstream status chain, request IDs), so Phase 2 represents the
assistant-message level call lifecycle rather than every transport-layer attempt.

## Phase 2 Hybrid Plan (updated 2026-02-16)

### Objective

Build a local hook sender that reports Anthropic OAuth usage into
`POST /api/logs/hooks/anthropic`, while keeping upstream Anthropic request
behavior untouched.

### Hybrid Data Sources (priority order)

1. Transcript JSONL (`~/.claude/projects/.../*.jsonl`) for stable call identity
   and token/model usage.
2. Hook stdin JSON for event context (`session_id`, `transcript_path`, event,
   cwd, etc.) and trigger timing.
3. Statusline stdin cost snapshot (when available) for direct Claude-reported
   cost parity.
4. Safe defaults + backend auto-pricing fallback when sender-side cost is
   missing.

### Hook/Event Strategy

Required events (Phase 2A):

1. `Stop`
2. `SubagentStop`

Optional event (Phase 2B):

1. `UserPromptSubmit` for synthetic `pending` insertion (same `requestId`,
   later completed by `Stop`/`SubagentStop` replay semantics)

### End-to-End Flow

1. Hook executes on `Stop` or `SubagentStop`.
2. Script reads stdin JSON and resolves `transcript_path`.
3. Script loads cursor state for that transcript (byte offset + seen requestIds).
4. Script scans only newly appended JSONL lines from last cursor.
5. Script extracts assistant records with `message.id` and `message.usage`.
6. Script groups by `(sessionId, message.id)` and applies dedupe rule.
7. Script builds payload for each unseen group and POSTs to cc-bridge ingest API.
8. Script commits cursor + seen IDs only after successful send (or idempotent
   duplicate response).

### Identity and Dedupe Rules

1. `requestId = sessionId + ":" + message.id`
2. Group key: `(sessionId, message.id)`
3. `initialTime`: earliest timestamp in group
4. `completeTime`: latest timestamp in group
5. usage/model source: latest valid assistant record in group (or max-usage
   fallback if latest is partial)
6. Sender dedupe + backend idempotency are both enabled (defense in depth)

### Field Mapping Contract (Phase 2 sender -> Phase 1 ingest API)

Sender should populate:

1. `requestId`, `status`, `initialTime`, `completeTime`, `durationMs`
2. `model`, `responseModel`
3. `inputTokens`, `outputTokens`, `cacheCreationInputTokens`,
   `cacheReadInputTokens`
4. `sessionId`, `endpoint=/v1/messages`
5. `httpStatus=200` for finalized assistant results (unless sender explicitly
   detects local processing failure path)
6. `price/inputCost/outputCost/cache*Cost`:
   - prefer stdin-reported cost when available
   - otherwise send `0` and rely on backend auto-pricing

Fields safely defaulted by backend:

1. provider/channel defaults (`anthropic-oauth`, channel metadata)
2. API key attribution from auth context
3. final cost computation when sender omits cost

### Pending/Completed Policy

Phase 2A (default):

1. Completed-only ingestion from `Stop`/`SubagentStop` for correctness-first
   rollout.

Phase 2B (optional UX enhancement):

1. Emit synthetic `pending` on `UserPromptSubmit`.
2. Emit finalized `completed/error` with same `requestId` when assistant record
   is available.
3. Keep this behind a feature flag until stability is verified.

### Failure Handling

1. Transcript not yet flushed:
   - short retry window (e.g., 3 attempts with small backoff)
2. Network/API failure to cc-bridge:
   - local retry queue/spool file for later replay
3. Malformed JSONL line:
   - skip line, log diagnostic, continue
4. Duplicate send:
   - treat `created=false` as success and advance cursor

### Implementation Deliverables

1. Hook sender script/binary (reads stdin, parses transcript incrementally,
   sends ingest payload)
2. Local state store (cursor + dedupe IDs, per transcript/session)
3. Hook config template for `~/.claude/settings.json`
4. Test fixtures for duplicated message IDs and subagent transcripts
5. End-to-end validation checklist against cc-bridge WebUI Request Log table

### Validation Plan

1. Unit-level parser tests:
   - duplicate `message.id` updates
   - partial/invalid lines
   - subagent transcript separation
2. Sender integration tests:
   - idempotent replay
   - retry queue drain
   - zero-cost payload -> backend auto-pricing
3. Live manual validation:
   - one completed turn
   - one tool-heavy turn (multiple assistant updates)
   - one subagent turn
   - confirm model/tokens/cost/status/api-key visibility in WebUI

### Risks and Mitigations

1. Hook schema drift across Claude Code versions:
   - keep stdin parsing tolerant to unknown fields
2. Transcript write timing race:
   - bounded retry before declaring no-op
3. Local sender crash between parse and send:
   - commit cursor only after successful send
4. Missing direct upstream HTTP metadata:
   - explicitly scope logs to assistant-message level semantics

## Phase 2A Progress (debug sender prototype, 2026-02-16)

Implemented a debug-first local hook sender in repo:

1. `scripts/hooks/ccbridge-hook-debug.sh`
   - reads hook stdin JSON
   - attempts transcript-based assistant usage extraction
   - sends to `POST /api/logs/hooks/anthropic`
   - supports dry-run and verbose diagnostics
2. `scripts/hooks/claude-settings.debug.json`
   - wires the same script to multiple hook events for trigger-everywhere debug
3. `scripts/hooks/README.md`
   - install, env, and local dry-run instructions

Current behavior:

1. does not separate OAuth/API mode yet (intended for debug stage)
2. emits completed logs when assistant usage is available
3. can emit synthetic pending logs when no usage is found

Validation executed (prototype):

1. `bash -n scripts/hooks/ccbridge-hook-debug.sh`
2. dry run without transcript (pending payload)
3. dry run with transcript usage (completed payload)
4. live send through ingest API (completed + pending rows confirmed in `/api/logs`)

Review/fix pass (same day):

1. fixed invalid hook stdin JSON causing non-zero script exit
2. fixed network POST failures causing non-zero script exit
3. bounded transcript scan to tail lines (`CCBRIDGE_HOOK_TRANSCRIPT_TAIL_LINES`)
4. kept sender fail-safe so hook flow does not block Claude usage

Second review/fix pass (real turn validation):

1. fixed stuck synthetic pending rows by switching default to completed-only mode
   (`CCBRIDGE_HOOK_DEBUG_EMIT_EMPTY=0`)
2. added finalize retry for `Stop`/`SubagentStop` to avoid capturing transient
   thinking-only usage before final text usage is flushed
3. added sender event filter default (`CCBRIDGE_HOOK_SEND_EVENTS=Stop,SubagentStop`)
   so non-final hook events do not emit noisy logs by default
4. confirmed limitation: local transcript for tested turn only exposed final
   Sonnet assistant record, not intermediate internal Haiku routing calls
