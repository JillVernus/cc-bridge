# Codex Priority Disable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the per-channel Codex service-tier override so eligible Responses channels can explicitly rewrite `service_tier: "priority"` to `default`, keep logging/pricing aligned with the effective tier, and show clear UI copy and request-log semantics for proxy-disabled priority.

**Architecture:** Reuse the existing `codexServiceTierOverride` field and add a third policy value, `force_default`. The existing effective-tier helper remains the single source of truth for request rewriting, fast-mode billing, and request-log metadata, while the WebUI updates its option copy and log-table override messaging to distinguish forced-fast from priority-disabled outcomes.

**Tech Stack:** Go 1.22, Gin, SQLite/PostgreSQL config storage, request-log SSE events, Vue 3, TypeScript, Vuetify, Bun

---

### Task 1: Extend Config And API Typing For `force_default`

**Files:**
- Modify: `backend-go/internal/config/config.go`
- Modify: `backend-go/internal/config/db_storage.go`
- Modify: `backend-go/internal/handlers/config.go`
- Modify: `frontend/src/services/api.ts`
- Test: `backend-go/internal/config/config_db_save_test.go`
- Test: `backend-go/internal/config/config_test.go` or equivalent file-backed config load test

- [ ] **Step 1: Add the new allowed channel type union**

Update the relevant backend/frontend type comments and TypeScript unions so
`codexServiceTierOverride` allows:

- `off`
- `force_priority`
- `force_default`

- [ ] **Step 2: Normalize create/update inputs**

In backend config create/update handling, implement the rule that:

- surrounding whitespace is trimmed
- comparisons are case-insensitive
- unknown or empty values become `off`
- persisted values use canonical lowercase strings

- [ ] **Step 3: Normalize loaded config values**

In backend config load paths, ensure stored values are normalized on read so
legacy casing/whitespace variants do not leak back into runtime behavior.

- [ ] **Step 4: Keep DB persistence unchanged but round-trippable**

Ensure `backend-go/internal/config/db_storage.go` continues to load/store the
text field correctly for `force_default` without introducing any new migration.

- [ ] **Step 5: Expose canonical values through config APIs**

Ensure config GET responses emit canonical values for eligible channels and do
not break existing create/update payload handling. Ineligible channel types
should effectively behave as `off`.

- [ ] **Step 6: Add normalization and persistence test coverage**

Extend `backend-go/internal/config/config_db_save_test.go` with cases that
verify:

- `force_default` survives save/load round-trip
- whitespace/casing variants normalize to canonical lowercase
- unknown or empty values behave as `off`

- [ ] **Step 7: Add a file-backed normalization regression**

Add one non-DB config test in `backend-go/internal/config/config_test.go` or an
equivalent file-load test that proves:

- mixed-case / whitespace legacy values normalize on read
- unknown or empty values surface as effective `off`
- canonical lowercase values are what runtime/config GET paths expose

### Task 2: Implement `force_default` In Effective Tier Resolution

**Files:**
- Modify: `backend-go/internal/handlers/service_tier.go`
- Test: `backend-go/internal/handlers/service_tier_test.go`

- [ ] **Step 1: Extend the effective-tier helper rules**

Update `resolveEffectiveResponsesServiceTier` to support:

- `force_default` rewriting explicit `service_tier: "priority"` to `"default"`

while preserving existing behavior for:

- `off`
- `force_priority`
- missing `service_tier`
- non-priority non-empty tiers

- [ ] **Step 2: Keep fast-mode state derived from the rewritten tier**

Ensure the helper returns:

- effective body bytes
- effective `serviceTier`
- effective `isFastMode`
- `serviceTierOverridden`

with downgraded requests returning:

- `serviceTier = "default"`
- `isFastMode = false`
- `serviceTierOverridden = true`

- [ ] **Step 3: Add focused helper tests**

Extend `backend-go/internal/handlers/service_tier_test.go` with cases for:

- `priority` + `force_default` => `default`, not fast, overridden
- missing tier + `force_default` => unchanged, not overridden
- `default` + `force_default` => unchanged, not overridden
- other non-empty tier + `force_default` => unchanged, not overridden

### Task 3: Apply The New Policy To Responses And OAuth Forwarding

**Files:**
- Modify: `backend-go/internal/handlers/responses.go`
- Test: `backend-go/internal/handlers/responses_oauth_test.go`

- [ ] **Step 1: Reuse the effective-tier helper for selected channel attempts**

Verify both standard Responses and `openai-oauth` attempt paths continue to use
the effective request body produced by the helper, now including
`force_default`.

- [ ] **Step 2: Keep bridge `speed = "fast"` behavior out of scope**

Confirm no new logic rewrites Claude bridge `speed = "fast"` traffic. The new
policy must only act on explicit Responses `service_tier: "priority"`.

- [ ] **Step 3: Update OAuth request-builder test coverage**

Extend `backend-go/internal/handlers/responses_oauth_test.go` so explicit
`service_tier: "priority"` becomes `default` under `force_default`, while the
override flag is set and unrelated fields remain preserved.

### Task 4: Preserve Effective Tier Metadata Across Success, Retry, Failover, And Errors

**Files:**
- Modify: `backend-go/internal/handlers/responses.go`
- Modify: `backend-go/internal/requestlog/service_tier_test.go`
- Test: `backend-go/internal/handlers/responses_retry_wait_test.go`
- Test: `backend-go/internal/handlers/first_token_stream_paths_test.go`

- [ ] **Step 1: Audit every Responses log-write path**

Ensure the exact effective tuple:

- effective request body
- effective `serviceTier`
- effective `isFastMode`
- effective `serviceTierOverridden`

flows through:

- success records
- retry_wait records
- recreated pending logs
- failover records
- final error records

- [ ] **Step 2: Fix successful downgraded request logging**

Update the Responses success path so a completed downgraded request persists:

- `serviceTier = "default"`
- `serviceTierOverridden = true`

instead of silently dropping the non-fast effective tier.

- [ ] **Step 3: Add a completed-success regression test**

Extend `backend-go/internal/handlers/first_token_stream_paths_test.go` or an
equivalent Responses success-path test file with a case that proves a completed
`force_default` request persists:

- `serviceTier = "default"`
- `serviceTierOverridden = true`

- [ ] **Step 4: Add retry/error regression coverage**

Extend `backend-go/internal/handlers/responses_retry_wait_test.go` with a
`force_default` retry scenario that proves retry/failure log records preserve
the downgraded metadata.

- [ ] **Step 5: Add request-log fetch/SSE coverage**

Extend `backend-go/internal/requestlog/service_tier_test.go` so a completed
record with:

- `serviceTier = "default"`
- `serviceTierOverridden = true`

survives direct fetch, recent list fetch, and SSE created/updated payloads.

### Task 5: Update Channel Modal Copy And Option Set

**Files:**
- Modify: `frontend/src/components/AddChannelModal.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add the third select option**

Extend the modal control to show:

- Off
- Force priority for missing/default
- Force default for explicit priority

- [ ] **Step 2: Update form state typing**

Extend the modal form model and API interface union to include
`'force_default'`.

- [ ] **Step 3: Rewrite helper copy in both locales**

Update the label and hint text so they clearly describe both directions:

- force-up to `priority`
- force-down to `default`

The new wording must not imply the feature is only for forcing priority.

- [ ] **Step 4: Verify create/edit round-trip behavior**

Ensure modal reset, edit-load, and submit paths preserve `force_default` for
both create and update requests.

### Task 6: Distinguish Forced-Fast From Priority-Disabled In The Request Log UI

**Files:**
- Modify: `frontend/src/components/RequestLogTable.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Locate every override-specific label/tooltip branch**

Find the current places where the table explains:

- fast mode
- proxy override

for `serviceTier === 'priority'`.

- [ ] **Step 2: Add downgraded override semantics**

Update the table logic so when:

- `serviceTierOverridden === true`
- `serviceTier !== 'priority'`

the UI shows explicit “priority disabled by proxy” semantics.

- [ ] **Step 3: Prevent fast-mode wording on downgraded rows**

Ensure downgraded rows do not show fast-mode wording or the fast-mode
explanation text. The override icon may remain, but the copy must clearly
communicate disablement rather than acceleration.

- [ ] **Step 4: Add localized text for both outcomes**

Add or update locale strings so forced-fast and priority-disabled outcomes are
both clearly expressed in English and Chinese.

### Task 7: Verify End-To-End Behavior

**Files:**
- Validation: `backend-go`
- Validation: `frontend`

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
cd backend-go && go test ./internal/config ./internal/handlers ./internal/requestlog -count=1
```

Expected:

- new `force_default` coverage passes
- existing `force_priority` behavior remains green

- [ ] **Step 2: Run backend formatting**

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

Expected: no TypeScript errors after extending the override union and UI logic.

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd frontend && bun run build
```

Expected: production build succeeds and the request log table compiles with the
new downgraded-override display logic.

- [ ] **Step 5: Review final behavior**

Confirm:

- `force_default` rewrites explicit `service_tier: "priority"` to `default`
- missing `service_tier` remains unchanged under `force_default`
- pricing does not treat downgraded requests as fast mode
- completed/retried/failed downgraded logs keep `serviceTier = "default"` and
  `serviceTierOverridden = true`
- the WebUI clearly distinguishes forced-fast from priority-disabled requests
