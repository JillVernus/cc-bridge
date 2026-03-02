# Messages Composite to Responses Channels Bridge

## Date: 2026-03-02

## Scope (Strict)

This implementation is strictly limited to:

- Incoming endpoint: `/v1/messages` (Claude Messages format)
- Composite channel behavior in **Messages** channels
- Composite targets allowed from:
  - Messages pool (`config.Upstream`)
  - Responses pool (`config.ResponsesUpstream`) for Codex channels (`responses`, `openai-oauth`)
- Runtime bridge:
  - Claude Messages request -> Responses upstream request (for Responses pool targets)
  - Responses upstream response -> Claude Messages response (including stream passthrough conversion)

Out of scope:

- New API endpoints
- Changes to non-composite channel selection behavior
- Breaking existing composite configs
- Removing existing `importFromResponsesChannelId` feature in this phase

## Progress Tracker

Last updated: 2026-03-02 (Step 6 reviewer approved; final post-merge review approved)

- [x] Plan document created
- [x] Plan review approved
- [x] Step 1 implemented: pool-aware composite schema + normalization helpers
- [x] Step 1 reviewer approved
- [x] Step 2 implemented: backend validation/resolve/delete across messages + responses pools
- [x] Step 2 reviewer approved
- [x] Step 3 implemented: scheduler mixed-pool composite failover chain support
- [x] Step 3 reviewer approved
- [x] Step 4 implemented: `/v1/messages` runtime bridge to Responses targets (API + OAuth)
- [x] Step 4 reviewer approved
- [x] Step 5 implemented: frontend composite editor supports selecting Responses Codex channels
- [x] Step 5 reviewer approved
- [x] Step 6 implemented: targeted tests + regression checks
- [x] Step 6 reviewer approved
- [x] Final post-merge review approved

## Background

Current composite mappings in Messages channels only reference Messages pool channel IDs.
That prevents reusing existing Codex Responses channels directly, especially OAuth channels,
and causes duplicated config when users copy base URL/API keys into Messages channels.

The target architecture is reuse-first:

1. User configures Codex channels once in Responses tab (API or OAuth).
2. User selects those channels in Messages composite mappings/failover chains.
3. Runtime bridges request/response formats at `/v1/messages`.

## Target Contract

### A) Composite Target Schema (Backward Compatible + Canonicalization)

Each mapping supports:

- Primary target:
  - `targetChannelId` (existing)
  - `targetPool` (new; `messages` or `responses`, default `messages`)
- Failover targets:
  - `failoverTargets` (new array of `{ pool, channelId }`)
  - `failoverChain` remains for backward compatibility (legacy IDs)

Backward compatibility and canonicalization rules:

1. Canonical read precedence:
   - `targetPool` + `targetChannelId` define primary target.
   - If `targetPool` missing, default to `messages`.
   - `failoverTargets` is the source of truth for failover when present and non-empty.
   - If `failoverTargets` is empty/missing, derive failover from legacy `failoverChain`.
2. Legacy derivation:
   - Legacy `failoverChain` IDs are interpreted using:
     - `targetPool` when set
     - otherwise default `messages`.
3. Conflict policy:
   - If both `failoverTargets` and `failoverChain` are present and disagree,
     runtime uses `failoverTargets` only.
4. Canonical write policy:
   - On create/update, backend normalizes and persists:
     - canonical `targetPool`
     - canonical `failoverTargets`
   - `failoverChain` may be emitted for backward compatibility snapshots only,
     but must not drive runtime when `failoverTargets` exists.

### B) Validation Rules

For Messages composite channels:

- Required patterns: `haiku`, `sonnet`, `opus` (exactly 3 mappings)
- Primary + failover targets must exist
- Messages pool allowed service types:
  - `claude`, `openai_chat`, `openai`, `openaiold`, `gemini`, `responses`
- Responses pool allowed service types:
  - `responses`, `openai-oauth`
- Composite recursion forbidden

### C) Permission Model (`/v1/messages` -> Responses Targets)

1. Endpoint permission remains unchanged:
   - Request still enters `/v1/messages`, so API key must have `messages` endpoint permission.
2. Channel permission checks:
   - Top-level selected Messages channel/composite must be allowed by current API key channel ACL.
   - For composite resolution, each resolved target channel ID (including Responses pool targets)
     must also be in allowed channel IDs when channel ACL is enabled.
3. Failure behavior:
   - If resolved target channel is not allowed, treat as no eligible channel in chain and continue failover.
   - If all targets are denied/unavailable, return existing channel-not-allowed/unavailable semantics.

### D) Runtime Routing Rules (`/v1/messages`)

When composite target pool = `messages`:

- Keep existing behavior.

When composite target pool = `responses`:

- Convert Claude request to Responses request format (existing translator).
- Execute against selected Responses channel:
  - API key channel (`responses`)
  - OAuth channel (`openai-oauth`) with existing token refresh flow.
- Convert upstream Responses response/stream back to Claude Messages format.

### E) OAuth Parity Checklist (Required)

For `openai-oauth` targets in Messages composite bridge, implementation must preserve:

1. Shared token refresh path:
   - Use existing token manager validity check/refresh flow.
2. Token persistence:
   - Persist refreshed tokens back to Responses channel config.
3. 401 recovery:
   - Trigger forced refresh-on-401 behavior consistent with existing Responses path.
4. Header parity:
   - Preserve Codex OAuth required headers and stream/non-stream header variants.
5. User-Agent parity:
   - Follow existing OAuth UA policy used by Responses path.

### F) Metrics/Logging Semantics

- Metrics owner channel: composite channel (unchanged).
- Routed channel name: actual resolved target channel (messages/responses).
- Request log channel context remains composite channel for continuity.

## Implementation Steps

### Step 0 - Plan + Review

Files:

- `docs/implementation_plans/20260302-01 - Messages Composite to Responses Channels Bridge.md`

Work:

- [x] Define scope and non-goals
- [x] Define step-by-step tracker with reviewer gates
- [ ] Reviewer approval

Gate:

- Reviewer must explicitly approve before Step 1.

### Step 1 - Pool-Aware Composite Schema

Files:

- `backend-go/internal/config/config.go`
- `frontend/src/services/api.ts`

Work:

- [ ] Add pool-aware mapping fields (`targetPool`, `failoverTargets`) with backward compatibility.
- [ ] Add normalization helpers for legacy mappings.

Gate:

- Reviewer approval required before Step 2.

### Step 2 - Validation/Resolve/Delete Across Pools

Files:

- `backend-go/internal/config/config.go`
- `backend-go/internal/handlers/config.go`
- `backend-go/internal/handlers/current_channel.go`

Work:

- [ ] Validate composite targets across both pools.
- [ ] Resolve mapping with pool-aware result.
- [ ] Clean composite references when Messages/Responses channels are deleted.
- [ ] Update current-channel resolution to support Responses-pool targets.
- [ ] Update test-mapping/admin debug output contract to include target pool when resolved.

Gate:

- Reviewer approval required before Step 3.

### Step 3 - Scheduler + Runtime Bookkeeping (Mixed Pool)

Files:

- `backend-go/internal/scheduler/channel_scheduler.go`
- `backend-go/internal/handlers/proxy.go`

Work:

- [ ] Support mixed pool failover chain entries in composite routing.
- [ ] Preserve sticky composite behavior.
- [ ] Make failover bookkeeping pool-safe (prevent index collisions across pools).
- [ ] Add gate check for mixed-pool chain traversal without cross-pool index contamination.

Gate:

- Reviewer approval required before Step 4.

### Step 4 - `/v1/messages` Runtime Bridge to Responses Targets

Files:

- `backend-go/internal/handlers/proxy.go`
- `backend-go/internal/handlers/responses.go` (shared helper reuse if needed)

Work:

- [ ] Route composite-resolved responses targets through request/response translation.
- [ ] Support both responses API-key and openai-oauth targets.
- [ ] Keep existing non-composite and messages-target paths unchanged.

Gate:

- Reviewer approval required before Step 5.

### Step 5 - Frontend Composite Editor: Include Codex Channels

Files:

- `frontend/src/components/CompositeChannelEditor.vue`
- `frontend/src/components/AddChannelModal.vue`
- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`
- `frontend/src/App.vue` (if data wiring changes needed)

Work:

- [x] Show selectable composite targets from Messages + Responses(Codex) pools.
- [x] Persist pool-aware mapping shape.
- [x] Keep old mapping edit/load behavior compatible.

Gate:

- Reviewer approval required before Step 6.

### Step 6 - Tests + Validation

Files:

- `backend-go/internal/config/*_test.go` (new/updated)
- `backend-go/internal/scheduler/*_test.go` (new/updated)
- `backend-go/internal/handlers/*_test.go` (new/updated)

Work:

- [x] Add targeted tests for validation/resolve/scheduler/runtime behavior.
- [x] Run targeted backend tests and frontend build/type-check.

Mandatory test matrix:

- [x] Legacy mapping migration defaults (`targetPool` missing, `failoverTargets` missing).
- [x] Mixed-pool mapping resolution (primary in responses, failover in messages, and vice versa).
- [x] Conflict precedence (`failoverTargets` vs `failoverChain`) uses canonical field.
- [x] Delete cleanup: deleting Messages target updates composites.
- [x] Delete cleanup: deleting Responses target updates composites.
- [x] Scheduler sticky failover through mixed-pool chain.
- [x] `/v1/messages` -> Responses API-key target (stream + non-stream).
- [x] `/v1/messages` -> Responses OAuth target (stream + non-stream).
- [x] OAuth parity checks (refresh persistence, 401 forced refresh path, headers/UA expectations).
- [x] Permission-denied target in composite chain is rejected safely.
- [x] Regression: Messages-only composite routing unchanged.

Suggested commands:

- [x] `cd backend-go && go test ./internal/config ./internal/scheduler ./internal/handlers -count=1`
- [x] `cd backend-go && go test ./... -count=1` (best effort)
- [x] `cd frontend && bun run build`

Gate:

- Reviewer approval required before final sign-off.

## Risks and Mitigations

1. Mixed-pool index collision in failover bookkeeping
   - Mitigation: pool-aware target refs and pool-aware resolution path.
2. Regression on existing Messages composite behavior
   - Mitigation: default pool fallback to `messages`; preserve legacy fields.
3. OAuth bridge path complexity
   - Mitigation: reuse existing token refresh/header logic; add targeted tests.

## Acceptance Criteria

Implementation is complete only when all are true:

1. Messages composite editor can select Responses Codex channels (API + OAuth).
2. Saved mappings persist pool-aware targets with backward compatibility.
3. `/v1/messages` can route through composite to Responses targets successfully.
4. OAuth Responses target works through `/v1/messages` path.
5. Permission checks are preserved for both top-level composite and resolved targets.
6. Existing Messages-only composite behavior is not regressed.
7. Tests pass and reviewer gives final approval.
