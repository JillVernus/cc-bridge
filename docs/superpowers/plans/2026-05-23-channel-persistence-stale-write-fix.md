# Channel Persistence Stale-Write Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make channel create/edit writes visible immediately, prevent stale instances from overwriting newer channel state, and return stable channel identity to the frontend.

**Architecture:** Introduce one authoritative config revision per persisted config snapshot and use it as the concurrency contract for DB-backed deployments. Store the DB revision in a single metadata/settings row and update it in the same transaction as channel/config writes. In DB mode, save/update should be optimistic and atomic: compare the caller's expected revision with the persisted revision inside the write transaction, then write the full config snapshot only if it still matches. JSON mode remains single-process/file-backed and gets a local in-memory revision only for API response consistency; cross-process JSON CAS is explicitly out of scope. API mutations should return stable channel identity (`id` canonical, `index` compatibility only), and the frontend should lock save actions until the mutation completes and the returned identity is available.

**Tech Stack:** Go 1.22 backend, SQLite/PostgreSQL-backed config storage, Vue 3 + TypeScript frontend, Vuetify UI.

---

### Task 1: Define the Config Revision Contract

**Files:**
- Modify: `backend-go/internal/config/config.go`
- Modify: `backend-go/internal/config/db_storage.go`
- Create: `backend-go/internal/database/migrations/015_config_revision.sql`
- Test: `backend-go/internal/config/config_revision_test.go`

- [ ] **Step 1: Write failing tests**
  - Add tests that prove config load returns a revision, config save increments it, DB polling uses it instead of second-resolution timestamps, and rapid same-second saves are not missed.
  - Add a JSON-mode test that verifies the local revision increments after a successful file save for response/header consistency, without claiming cross-process CAS.

- [ ] **Step 2: Run the new tests to verify RED**
  - Run: `cd backend-go && go test ./internal/config -run 'Test.*Revision|Test.*Stale' -count=1`
  - Expected: fail because revision support and stale-write checks do not exist yet.

- [ ] **Step 3: Implement the revision source**
  - Add migration `015_config_revision.sql` to initialize a `settings` row such as `key='config_revision', category='config_meta', value='1'` for existing DB deployments.
  - Update `001_config_tables.sql` only so fresh installs also have the metadata row.
  - Make DB-backed saves increment the revision row in the same transaction as channel/settings writes.
  - Replace `MAX(updated_at)` polling with the authoritative revision row.
  - Make config loads return revision alongside config state inside the storage layer.
  - In JSON mode, keep the revision in `ConfigManager` memory and increment it only after `os.WriteFile` succeeds.

- [ ] **Step 4: Verify GREEN**
  - Run: `cd backend-go && go test ./internal/config -count=1`
  - Expected: pass.

### Task 2: Add Atomic Stale-Writer Protection

**Files:**
- Modify: `backend-go/internal/config/db_storage.go`
- Modify: `backend-go/internal/config/config.go`
- Modify: `backend-go/internal/handlers/config.go`
- Test: `backend-go/internal/config/config_stale_write_test.go`

- [ ] **Step 1: Write failing tests**
  - Add a same-DB, two-manager test where instance A creates a channel, instance B tries to save from an old revision, and the stale write is rejected.
  - Add a test that a stale write does not delete or overwrite a newer channel snapshot.
  - Add a test that a failed stale save leaves the stale instance's in-memory config unchanged or reloaded to the durable DB state.

- [ ] **Step 2: Run the tests to verify RED**
  - Run: `cd backend-go && go test ./internal/config -run 'Test.*Stale' -count=1`
  - Expected: fail until CAS-style revision checks exist.

- [ ] **Step 3: Implement CAS save behavior**
  - Require `expectedRevision == persistedRevision` inside the same DB transaction before sync/delete/update runs.
  - Return `409 Conflict` for stale channel mutations.
  - Keep the in-memory `ConfigManager.config` unchanged on failed persistence.
  - Reload the local manager only after durable save succeeds.
  - Continue using existing `channel_id`-based smart update/insert/delete behavior in `syncChannelsTx`; do not reintroduce full table wipe-and-reinsert.
  - Ensure DB polling `lastVersion` advances to the new revision after local successful writes so the writer instance does not immediately reprocess its own save as a stale external change.

- [ ] **Step 4: Verify GREEN**
  - Run: `cd backend-go && go test ./internal/config ./internal/handlers -count=1`
  - Expected: pass.

### Task 3: Stabilize Channel Identity on Mutation APIs

**Files:**
- Modify: `backend-go/internal/handlers/config.go`
- Modify: `backend-go/internal/handlers/upstream_models.go`
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/App.vue`
- Test: `backend-go/internal/handlers/config_identity_test.go`

- [ ] **Step 1: Write failing tests**
  - Add tests that create a channel and assert the mutation response returns the canonical stable `id` plus current `index`.
  - Add a test that model-fetch/update paths can resolve by stable identity when available.

- [ ] **Step 2: Run the tests to verify RED**
  - Run: `cd backend-go && go test ./internal/handlers -run 'Test.*Identity|Test.*Stable' -count=1`
  - Expected: fail because create/update responses do not expose stable identity yet.

- [ ] **Step 3: Implement the response contract**
  - Return created/updated channel identity from add/update endpoints.
  - Treat `id` as canonical for follow-up actions and `index` as compatibility metadata only.
  - Preserve existing index-based routes until callers are migrated.
  - Add stable-ID routes or request parameters for update/delete/model-fetch where the frontend needs immediate follow-up behavior; keep index routes as compatibility-only.
  - For ambiguous numeric path parameters, resolve explicit stable-ID routes before index routes.
  - Return `ETag` on config/channel reads and accept `If-Match` on mutation requests for the expected revision.

- [ ] **Step 4: Verify GREEN**
  - Run: `cd backend-go && go test ./internal/handlers -count=1`
  - Expected: pass.

### Task 4: Fix Frontend Save Sequencing

**Files:**
- Modify: `frontend/src/components/AddChannelModal.vue`
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/services/api.ts`
- Test: `frontend/src/App.channel-save.test.ts`

- [ ] **Step 1: Write the failing test**
  - Add a test that the modal disables repeat submit while a save is in flight.
  - Add a test that an immediate edit after create uses the returned stable identity instead of waiting for a refresh.
  - Add a test that a `409 Conflict` response shows a conflict toast and refreshes channel state.

- [ ] **Step 2: Run the test to verify RED**
  - Run: `cd frontend && bun test src/App.channel-save.test.ts`
  - Expected: current behavior should not satisfy the new test assertions.

- [ ] **Step 3: Implement frontend guards**
  - Add a saving lock/loading state to the modal.
  - Use returned stable channel identity for immediate follow-up actions.
  - Refresh after save only as a consistency update, not as the source of truth for the newly created channel.
  - Store and send the latest config revision using `ETag`/`If-Match` when available.
  - On `409 Conflict`, show a clear toast and refresh the affected channel list.

- [ ] **Step 4: Verify GREEN**
  - Run: `cd frontend && bun test src/App.channel-save.test.ts`
  - Run: `cd frontend && bun run type-check`
  - Expected: pass.

### Task 5: Final Verification

- [ ] Run backend targeted tests: `cd backend-go && go test ./internal/config ./internal/handlers -count=1`
- [ ] Run frontend targeted tests: `cd frontend && bun test src/App.channel-save.test.ts`
- [ ] Run frontend type validation: `cd frontend && bun run type-check`
- [ ] Confirm stale saves return `409 Conflict`, rapid writes no longer disappear, and immediate model fetch works after create.
