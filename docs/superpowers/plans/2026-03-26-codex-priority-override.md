# Codex Priority Override Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-channel Codex priority override that rewrites missing or `default` `service_tier` values to `priority` for eligible Responses channels, logs when the proxy performed the override, and shows an override icon in the WebUI log table.

**Architecture:** The change adds a new channel config field, computes the effective Responses `service_tier` per selected channel attempt, forwards the rewritten request body only when an eligible channel forces priority, and persists a dedicated request-log boolean so the WebUI can distinguish client-requested priority from proxy-forced priority. The implementation stays narrow to Responses and `openai-oauth` traffic and does not change Messages bridging semantics.

**Tech Stack:** Go 1.22, Gin, SQLite/PostgreSQL config storage, request-log SSE events, Vue 3, TypeScript, Vuetify, Bun

---

### Task 1: Add Channel Config Plumbing For Codex Override

**Files:**
- Modify: `backend-go/internal/config/config.go`
- Modify: `backend-go/internal/config/db_storage.go`
- Modify: `backend-go/internal/database/migrations/001_config_tables.sql`
- Create: `backend-go/internal/database/migrations/009_channels_codex_service_tier_override.sql`
- Modify: `backend-go/internal/handlers/config.go`
- Modify: `frontend/src/services/api.ts`
- Test: `backend-go/internal/config/config_db_save_test.go`

- [ ] **Step 1: Add the new channel field to backend config structs**

Add `CodexServiceTierOverride string` to `config.UpstreamConfig` and
`*string` support in `config.UpstreamUpdate`, with JSON key
`codexServiceTierOverride`.

- [ ] **Step 2: Persist the field in config DB storage**

Extend channel INSERT, UPDATE, and SELECT handling in
`backend-go/internal/config/db_storage.go` so the field round-trips for all
channel types without breaking older rows.

- [ ] **Step 3: Add a migration for the channels table**

Create `backend-go/internal/database/migrations/009_channels_codex_service_tier_override.sql`
to add a nullable `codex_service_tier_override` text column.

- [ ] **Step 4: Expose the field in config APIs**

Include `codexServiceTierOverride` in Responses and Messages channel GET
payloads in `backend-go/internal/handlers/config.go`, and ensure create/update
handlers bind and save it.

- [ ] **Step 5: Add frontend channel typing**

Extend the `Channel` interface in `frontend/src/services/api.ts` with
`codexServiceTierOverride?: 'off' | 'force_priority'`.

- [ ] **Step 6: Add persistence test coverage**

Extend `backend-go/internal/config/config_db_save_test.go` to save and reload a
channel with `codexServiceTierOverride: "force_priority"` and verify the value
survives DB round-trip.

### Task 2: Implement Effective Per-Attempt Codex Service Tier Resolution

**Files:**
- Modify: `backend-go/internal/handlers/service_tier.go`
- Modify: `backend-go/internal/handlers/responses.go`
- Modify: `backend-go/internal/providers/responses.go`
- Test: `backend-go/internal/handlers/service_tier_test.go`
- Test: `backend-go/internal/handlers/responses_oauth_test.go`

- [ ] **Step 1: Add a helper for effective Responses tier resolution**

In `backend-go/internal/handlers/service_tier.go`, add a helper that accepts:

- raw Responses request bytes
- parsed model name
- selected upstream

and returns:

- effective body bytes
- effective normalized `serviceTier`
- `isFastMode`
- `serviceTierOverridden`

- [ ] **Step 2: Implement the override rules**

The helper should:

- skip non-Codex models
- skip channels whose override mode is not `force_priority`
- rewrite missing `service_tier` to `"priority"`
- rewrite `"default"` to `"priority"`
- preserve `"priority"` and other non-empty values

- [ ] **Step 3: Use the effective request body for upstream forwarding**

Update the Responses forwarding paths so `responses` and `openai-oauth`
attempts send the effective request body for the selected channel, instead of
always forwarding the original raw request bytes unchanged.

- [ ] **Step 4: Keep fast-mode billing aligned with the effective tier**

Update the Responses handler attempt paths to use the effective `isFastMode`
value that comes from the selected channel decision, not only the original
incoming request body.

- [ ] **Step 5: Add helper tests**

Extend `backend-go/internal/handlers/service_tier_test.go` with cases for:

- missing tier + force override => `priority`, fast, overridden
- `default` tier + force override => `priority`, fast, overridden
- `priority` tier + force override => unchanged, fast, not overridden
- other non-empty tier + force override => unchanged, not overridden
- non-Codex model + force override => unchanged, not overridden
- override `off` => unchanged, not overridden

- [ ] **Step 6: Add OAuth request builder tests**

Extend `backend-go/internal/handlers/responses_oauth_test.go` to verify that
eligible `openai-oauth` requests are rewritten to `service_tier = "priority"`
for missing/default cases, while preserving existing priority and unrelated
fields.

### Task 3: Persist Override Evidence In Request Logs And SSE

**Files:**
- Modify: `backend-go/internal/requestlog/types.go`
- Modify: `backend-go/internal/requestlog/manager.go`
- Modify: `backend-go/internal/requestlog/events.go`
- Modify: `backend-go/internal/requestlog/pg_notify.go`
- Create: `backend-go/internal/database/migrations/010_request_logs_service_tier_overridden.sql`
- Test: `backend-go/internal/requestlog/service_tier_test.go`
- Modify: `frontend/src/composables/useLogStream.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add the request-log field**

Add `ServiceTierOverridden bool` to `requestlog.RequestLog` with JSON key
`serviceTierOverridden`.

- [ ] **Step 2: Add request-log schema support**

Add a request-log migration and manager compatibility path for a new boolean
column such as `service_tier_overridden`.

- [ ] **Step 3: Include the field in request-log queries and updates**

Update `manager.go` and `pg_notify.go` so add/update/fetch/SSE code carries the
new flag through complete records, recent list results, and partial event fetch.

- [ ] **Step 4: Populate the field from effective tier decisions**

In `backend-go/internal/handlers/responses.go`, set
`ServiceTierOverridden: true` on log records only when the proxy actually
rewrote the request for the selected attempt.

- [ ] **Step 5: Extend frontend log payload types**

Add `serviceTierOverridden?: boolean` to:

- `RequestLog` in `frontend/src/services/api.ts`
- `LogCreatedPayload` and `LogUpdatedPayload` in
  `frontend/src/composables/useLogStream.ts`

- [ ] **Step 6: Add round-trip test coverage**

Extend `backend-go/internal/requestlog/service_tier_test.go` so a stored record
with `serviceTier = "priority"` and `serviceTierOverridden = true` survives DB
round-trip and SSE fetch paths.

### Task 4: Add Channel Editor Support In The WebUI

**Files:**
- Modify: `frontend/src/components/AddChannelModal.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Add form state for the override field**

Extend the modal form model with `codexServiceTierOverride`, defaulting to
`'off'`.

- [ ] **Step 2: Show the control only for eligible channels**

Render the new control only when:

- `props.channelType === 'responses'`
- `form.serviceType === 'responses' || form.serviceType === 'openai-oauth'`

- [ ] **Step 3: Add a clear warning in helper text**

Use localized copy that explains:

- missing/default Codex service tiers will be forced to priority
- priority billing may increase cost

- [ ] **Step 4: Wire the field into create/edit payloads**

Ensure modal reset, edit-load, and submit paths preserve the override value for
both create and update requests.

- [ ] **Step 5: Add i18n strings**

Add localized labels, descriptions, and option text in `en.ts` and
`zh-CN.ts`.

### Task 5: Add The Override Icon To The Main Log Table

**Files:**
- Modify: `frontend/src/components/RequestLogTable.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Reuse the existing fast-mode icon placement**

Identify every place in `RequestLogTable.vue` where `mdi-flash` is shown for
`serviceTier === 'priority'`.

- [ ] **Step 2: Add a second icon for proxy override**

Render a second icon immediately beside the thunder icon when
`item.serviceTierOverridden` is true.

- [ ] **Step 3: Add matching tooltip text**

Update the stacked and expanded tooltips so they explain both:

- fast mode is active
- the proxy forced priority for this request

- [ ] **Step 4: Keep the visual language consistent**

Use a distinct icon that reads as "proxy override" without replacing the
existing thunder icon. Keep sizing and spacing aligned with the existing model
cell icon stack.

### Task 6: Verify End To End Behavior

**Files:**
- Modify: `backend-go/internal/handlers/responses.go`
- Validation: `backend-go`
- Validation: `frontend`

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
cd backend-go && go test ./internal/handlers ./internal/requestlog ./internal/config
```

Expected:

- new override behavior tests pass
- existing related tests still pass

- [ ] **Step 2: Run full backend formatting**

Run:

```bash
cd backend-go && go fmt ./...
```

Expected: formatting succeeds with no errors.

- [ ] **Step 3: Run frontend type-check**

Run:

```bash
cd frontend && bun run type-check
```

Expected: no TypeScript errors after adding the new channel/log fields.

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd frontend && bun run build
```

Expected: production build succeeds and the request log table compiles with the
new icon logic.

- [ ] **Step 5: Review the final behavior**

Confirm:

- enabling `force_priority` on an eligible Responses channel rewrites missing
  and `default` Codex tiers to `priority`
- request logs store both `serviceTier = "priority"` and
  `serviceTierOverridden = true` when the proxy performed the rewrite
- the WebUI shows the thunder icon plus the override icon in the model column
- non-Codex and non-override traffic remain unchanged
