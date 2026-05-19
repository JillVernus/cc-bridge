# Codex Detailed Quota Headers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture all Codex quota-related response header families and related generic rate-limit headers, then show named quota buckets in the Responses OAuth channel UI.

**Architecture:** Extend the existing `quota.CodexQuotaInfo` model with `active_limit` and `detailed_limits`. Parse generic `X-Codex-<limit-id>-Primary-*`, `X-Codex-<limit-id>-Secondary-*`, and `X-Codex-<limit-id>-Limit-Name` header families without hard-coding `Bengalfox`. Keep the existing top-level `X-Codex-Primary/Secondary` fields as the main account quota for backward compatibility. Capture generic `X-Ratelimit-Limit`, `X-Ratelimit-Remaining`, and `X-Ratelimit-Reset` as request rate-limit data when present.

**Tech Stack:** Go 1.22 backend, Vue 3 + TypeScript frontend, Vuetify UI.

---

### Task 1: Backend Quota Parsing

**Files:**
- Modify: `backend-go/internal/quota/quota.go`
- Test: `backend-go/internal/quota/quota_test.go`

- [x] **Step 1: Write failing test**
  - Add a test that sends top-level Codex quota headers plus `X-Codex-Bengalfox-*` headers.
  - Assert `active_limit`, generic detailed limit ID/name, window percentages, window minutes, reset timestamps, and reset-after seconds are present.

- [x] **Step 2: Run test to verify RED**
  - Run: `cd backend-go && go test ./internal/quota -run TestUpdateFromHeadersForChannel_ParsesDetailedCodexQuotaFamilies -count=1`
  - Expected: fail because `CodexQuotaInfo` has no detailed limit fields.

- [x] **Step 3: Implement parser**
  - Add `CodexQuotaLimitInfo`.
  - Add `ActiveLimit` and `DetailedLimits` to `CodexQuotaInfo`.
  - Discover limit families by scanning response header names.
  - Parse each family using the same field names as top-level quota.
  - Preserve generic `X-Ratelimit-*` request-limit data alongside Codex quota data when both are present.

- [x] **Step 4: Verify GREEN**
  - Run: `cd backend-go && go test ./internal/quota -count=1`
  - Expected: pass.

### Task 2: Frontend Channel UI

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/OAuthStatusDialog.vue`
- Modify: `frontend/src/components/ChannelOrchestration.vue`
- Modify: `backend-go/internal/handlers/config.go`
- Test: `backend-go/internal/handlers/oauth_status_by_id_test.go`

- [x] **Step 1: Extend TypeScript API types**
  - Add `CodexQuotaLimitInfo`.
  - Add `active_limit` and `detailed_limits` to `CodexQuotaInfo`.

- [x] **Step 2: Render detailed limits**
  - OAuth status dialog shows named limits below the account quota.
  - Channel tooltip includes each detailed limit with primary/secondary remaining percentages.
  - Inline dual bar remains top-level account quota for compactness.

- [x] **Step 3: Expose fields through OAuth status API**
  - Return `active_limit` and `detailed_limits` from the channel OAuth status endpoint.
  - Cover the endpoint response with a handler test.

- [x] **Step 4: Type-check**
  - Run: `cd frontend && bun run type-check`
  - Expected: pass.

### Task 3: Final Verification

- [x] Run backend targeted tests: `cd backend-go && go test ./internal/quota ./internal/handlers -count=1`
- [x] Run frontend type-check: `cd frontend && bun run type-check`
- [x] Summarize parsed headers and UI behavior.
