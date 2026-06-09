# Karpathy Review Follow-up Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` if this is implemented by an agent. Track steps with checkbox syntax and keep fixes scoped unless evidence proves the scope is too narrow.

**Goal:** Close the agreed Reviewer findings from the Karpathy review of the Responses WebSocket, quota refresh, and metrics work.

**Architecture:** Treat this as a verification-first follow-up. Add focused automated regression coverage where the repo already has harnesses, complete the remaining manual checks with evidence, and only change production code if those checks expose a real gap.

**Tech Stack:** Go 1.22, Gin, SQLite-backed config manager, Gorilla WebSocket tests, Vue 3, Bun test, `vue-tsc`.

---

## Status

READY

## Step 0: Existing Patterns To Reuse

- Config/WebSocket tests already exist in `backend-go/internal/config/responses_websocket_test.go` and `backend-go/internal/handlers/responses_websocket_test.go`.
- Frontend has a lightweight Bun source-level test at `frontend/src/components/ChannelOrchestration.test.ts`; there is no full Vue mount test harness in the current frontend dependencies.
- Current code already contains the follow-up implementation pieces from commit `65dbb01`:
  - `backend-go/internal/handlers/responses_websocket.go` records connection-level failure when no request was observed.
  - `frontend/src/components/ChannelOrchestration.vue` clears/restarts `usageQuotaRefreshTimer` on tab changes and clears it on unmount.

## Objective

Make the previous WebSocket/quota fixes auditable and regression-resistant without widening the product behavior.

## In Scope

- Complete and document the four open manual checks in `docs/20260609-01 - WebSocket persistence and quota update fixes.md` lines 70-74.
- Add focused config regression coverage proving new `openai-oauth` Responses channels persist `ResponsesWebSocketEnabled=true`.
- Add low-cost WebSocket metrics/fallback regression tests if the existing handler/tracker harness can cover the right branch without fragile sleeps or external services.
- Add an edge-case review/checklist gate for:
  - WebSocket connection fails before the first request.
  - Timer/listener lifecycle during tab changes and component unmount.
  - Error paths that could silently drop metrics or leak polling.
- Standardize the stale follow-up commit hash in `docs/20260609-01` from `2d55ff8` to `65dbb01`.
- Optional: optimize fixed 10s quota polling only if it remains a small `ChannelOrchestration.vue` change and does not require a new frontend test framework.

## Out of Scope

- Replacing frontend polling with backend WebSocket/SSE quota events.
- Refactoring scheduler, metrics, request log, or config storage architecture.
- Broad UI redesign of channel orchestration.
- Adding a new frontend component test framework just for timer lifecycle coverage.
- Touching files outside config, websocket handler, orchestration component, docs, and tests unless a failing check produces direct evidence.

## Priority / ROI

| Priority | Item | ROI | Risk | Verification Type |
| --- | --- | --- | --- | --- |
| P0 | Correct stale follow-up commit hash | High | Low | Docs diff |
| P0 | Document the four manual checks with environment evidence | High | Medium | Manual evidence, blocked if preflight unavailable |
| P0 | Config persistence regression for new `openai-oauth` Responses channels | High | Low | Automated Go test |
| P1 | WebSocket fallback/recent-call regression coverage | Medium-high | Medium | Automated Go test if harness stays simple |
| P1 | Edge-case checklist gate | Medium | Low | Review checklist + targeted test evidence |
| P2 | Quota polling backoff/poll-after-request optimization | Low-medium | Medium | Automated/source-level frontend test or manual only |

Recommendation: do P0 and P1. Keep P2 deferred unless P0/P1 are green and the change stays local to `ChannelOrchestration.vue` plus its existing Bun test.

## Acceptance Checks

- `docs/20260609-01` no longer references `2d55ff8`; it references `65dbb01`.
- `docs/20260609-01` Step 4 is marked complete only after each manual check has dated evidence, environment notes, and pass/fail result.
- A Go test proves `ConfigManager.AddResponsesUpstream(openai-oauth)` persists `ResponsesWebSocketEnabled=true` across manager reload.
- Existing non-OAuth behavior remains covered: a plain `responses` channel is not forced on unless explicitly enabled, and ineligible service types are normalized off.
- If added, WebSocket fallback tests prove:
  - the `65dbb01` fallback branch records a scheduler failure when the upstream WebSocket connects, `logTracker` is created, and proxying errors before the first `response.create`;
  - a pre-logTracker dial failure may be covered separately, but it does not count as proof of the `65dbb01` fallback branch;
  - a completed WebSocket request records success once and is not overwritten by a later connection close/error;
  - `hadAnyRequests()` distinguishes no-request failures from request-level failures.
- Frontend verification guards timer lifecycle behavior:
  - source-level assertions confirm the tab-change watcher clears the old timer before assigning a new interval;
  - interval is cleared on unmount;
  - polling respects `document.visibilityState === 'visible'`.
- If exact "only one active interval" proof is required, add a tiny helper-level Bun fake-timer test with no new framework; otherwise record this as source-level guard coverage, not runtime proof.
- Manual verification has a preflight result for runtime availability, browser availability, and usable OAuth channel/credentials; if any preflight item is unavailable, Step 4 remains unchecked and the blocker is recorded.
- Validation commands finish green or the doc records the exact blocker.

## Dependencies / Risks

- Manual checks need a runnable local or container setup, a browser, and at least one usable Responses `openai-oauth` channel with OAuth credentials.
- True quota refresh verification depends on upstream quota data changing or an observable `/usage` response. If quota data is static, record network-call evidence plus the displayed timestamp/state.
- WebSocket fallback coverage must distinguish the pre-logTracker dial-failure path from the post-upstream-connect proxy-error path. Keep it deterministic; skip handler-level coverage if it requires timing-sensitive sleeps.
- Frontend timer behavior is currently easiest to guard with source-level assertions. Exact active-interval count requires extracting a tiny helper or equivalent test seam and testing it with Bun fake timers.

## Unresolved Decisions

- Whether to add P2 quota polling optimization now. Default: no, unless P0/P1 are complete and the implementation is small enough to verify with existing Bun tests and `type-check`.
- Whether WebSocket fallback coverage should exercise the full handler's post-connect proxy-error path or stop at tracker-level evidence. Default: try the full handler with a local upstream that upgrades, then abruptly closes before any client request; if that is fragile, use tracker-level `hadAnyRequests()` coverage and document residual handler-path risk.

## Execution Steps

### Task 1: Correct Documentation Metadata

**Files:**
- Modify: `docs/20260609-01 - WebSocket persistence and quota update fixes.md`

- [ ] Replace follow-up commit hash `2d55ff8` with `65dbb01`.
- [ ] Add a short note that commit `65dbb01` contains the connection-level failure fallback and quota timer lifecycle cleanup.
- [ ] Do not mark Step 4 complete yet.

**Validation:**
- Run: `rg -n "2d55ff8|65dbb01" docs/20260609-01\ -\ WebSocket\ persistence\ and\ quota\ update\ fixes.md CHANGELOG.md docs/20260609-03\ -\ Karpathy\ Guidelines\ Review.md`
- Expected: no `2d55ff8`; `65dbb01` appears in the implementation doc and review/changelog references.

### Task 2: Add Config Persistence Regression Coverage

**Files:**
- Modify: `backend-go/internal/config/responses_websocket_test.go`

- [ ] Add `TestAddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists`.
- [ ] Use `t.TempDir()` and `NewConfigManager(filepath.Join(t.TempDir(), "config.json"))`.
- [ ] Add a Responses upstream with `ServiceType: "openai-oauth"` and do not set `ResponsesWebSocketEnabled`.
- [ ] Close/reload the config manager from the same path.
- [ ] Assert the reloaded channel has `ResponsesWebSocketEnabled == true`.
- [ ] Add or extend assertions that a plain `responses` channel is not auto-enabled unless explicitly set.

**Validation:**
- Run: `cd backend-go && go test ./internal/config -run 'Test(AddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists|ResponsesWebSocket)' -count=1`
- Expected: PASS.

### Task 3: Add WebSocket Metrics/Fallback Regression Coverage If Low-Cost

**Files:**
- Modify: `backend-go/internal/handlers/responses_websocket_test.go`

- [ ] Add a deterministic test for the `65dbb01` fallback branch: upstream WebSocket connects successfully, `logTracker` is created, then proxying returns an error before the client sends any `response.create`.
- [ ] Seed a Responses `openai-oauth` channel with WebSocket enabled and a scheduler.
- [ ] Point `codexOAuthResponsesEndpoint` or a `responses` upstream to a local WebSocket server that upgrades successfully, then abruptly closes the underlying connection before reading client messages.
- [ ] Dial the bridge WebSocket as a client, do not send `response.create`, and assert the bridge closes/errors cleanly.
- [ ] Assert `sch.GetResponsesMetricsManager().GetMetrics(index)` records one failure and zero successes.
- [ ] If adding a separate broken-upstream/dial-failure test, label it as pre-logTracker coverage only. Do not count it as proof of the `65dbb01` fallback branch.
- [ ] Add a second test only if cheap: complete one request, then close/error the connection, and assert recent calls/metrics contain a single success with no duplicate failure.

**Fallback if harness cost is too high:**
- Do not add fragile sleeps or broad scaffolding.
- Instead, document why full handler coverage was skipped, add a narrower tracker-level assertion around `hadAnyRequests()` and `completed` guarding, and record residual risk that the full post-connect handler fallback branch is not directly exercised.

**Validation:**
- Run: `cd backend-go && go test ./internal/handlers -run 'TestResponsesWebSocket.*(Fallback|Failure|Metrics|RecentCall|ResponseDone)' -count=1`
- Expected: PASS.

### Task 4: Frontend Timer Lifecycle Gate

**Files:**
- Modify: `frontend/src/components/ChannelOrchestration.test.ts`
- Modify only if a bug is found or P2 is accepted: `frontend/src/components/ChannelOrchestration.vue`

- [ ] First add lightweight source-level coverage requiring:
  - `clearUsageQuotaRefreshTimer()` exists.
  - `onUnmounted()` calls `clearUsageQuotaRefreshTimer()`.
  - the `props.channelType` watcher calls `clearUsageQuotaRefreshTimer()` before assigning a new `setInterval`.
  - interval polling checks `document.visibilityState === 'visible'`.
- [ ] Phrase the test/doc evidence as source-level lifecycle guard coverage. Do not claim it proves exactly one live runtime interval.
- [ ] If exact interval-count proof is desired, extract a tiny local timer helper and cover it with a Bun fake-timer test; do this only if the extraction stays local and avoids new dependencies.
- [ ] Do not introduce Vue Test Utils, jsdom, or a new frontend test framework for this follow-up.

**Validation:**
- Run: `cd frontend && bun test src/components/ChannelOrchestration.test.ts`
- Run: `cd frontend && bun run type-check`
- Expected: PASS.

### Task 5: Manual Verification Runbook And Evidence

**Files:**
- Modify: `docs/20260609-01 - WebSocket persistence and quota update fixes.md`

- [ ] Add a `Manual Verification - 2026-06-09` subsection under Step 4.
- [ ] Add a preflight table before the four checks:
  - Runtime available: local dev server, built binary, or container.
  - Browser available: browser name/version or Playwright/browser automation path.
  - Usable OAuth channel/credentials available: channel name/ID and whether credentials are real, test, or unavailable.
- [ ] If any preflight item is unavailable, record `Result: BLOCKED`, explain the blocker, and do not mark Step 4 complete.
- [ ] If preflight passes, record environment: commit hash, build/run command, browser, channel type, and whether OAuth credentials were real or test.
- [ ] Complete the four checks:
  - Create new OAuth Responses channel and verify WebSocket is enabled by default.
  - Restart container/service and verify the setting persists.
  - Make WebSocket request and verify quota updates automatically.
  - Make WebSocket request and verify recent calls shows the correct status.
- [ ] For each check, record `Result`, `Evidence`, and `Notes`.
- [ ] Only change Step 4 from `[ ]` to `[x]` if preflight passes and all four checks pass.

**Validation:**
- Run: `rg -n "Manual Verification|Preflight|Runtime available|Browser available|Usable OAuth|Result:|Evidence:|Notes:" docs/20260609-01\ -\ WebSocket\ persistence\ and\ quota\ update\ fixes.md`
- Expected: preflight and each manual check have evidence. Step 4 is checked only when preflight and all four checks pass; otherwise the blocker is recorded and Step 4 remains unchecked.

### Task 6: Edge-Case Checklist Gate

**Files:**
- Modify: `docs/20260609-01 - WebSocket persistence and quota update fixes.md`
- Or create if the section would clutter the original: `docs/20260609-05 - WebSocket quota edge-case checklist.md`

- [ ] Add checklist results for:
  - connection fails before first request;
  - connection fails after one completed request;
  - tab changes repeatedly while quota polling is active;
  - component unmounts while polling is active;
  - quota API errors repeatedly;
  - invisible/background tab.
- [ ] Link each item to automated test evidence, manual evidence, or an explicit accepted residual risk.

**Validation:**
- If the checklist is added to `docs/20260609-01`, run: `rg -n "connection fails before first request|tab changes|unmount|quota API errors|background tab|residual risk" docs/20260609-01\ -\ WebSocket\ persistence\ and\ quota\ update\ fixes.md`
- If a separate checklist doc is created, run the same `rg` against that new path.
- Expected: checklist exists and every item has evidence or a residual-risk note.

### Task 7: Optional Quota Polling Optimization

**Files, only if accepted:**
- Modify: `frontend/src/components/ChannelOrchestration.vue`
- Modify: `frontend/src/components/ChannelOrchestration.test.ts`

- [ ] Prefer backoff after repeated `fetchUsageQuotas()` failures, because it preserves current simple polling model.
- [ ] Keep immediate/manual refresh behavior unchanged.
- [ ] Reset backoff after a successful quota fetch or tab/channel mapping change.
- [ ] Avoid request-aware polling unless there is already a reliable frontend event for request completion.
- [ ] Defer this task unless P0/P1 are green and the implementation remains local to `ChannelOrchestration.vue` plus its existing Bun test.

**Validation:**
- Run: `cd frontend && bun test src/components/ChannelOrchestration.test.ts`
- Run: `cd frontend && bun run type-check`
- Expected: PASS.

## Final Verification

Run the focused checks first:

```bash
cd backend-go && go test ./internal/config -run 'Test(AddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists|ResponsesWebSocket)' -count=1
cd backend-go && go test ./internal/handlers -run 'TestResponsesWebSocket' -count=1
cd frontend && bun test src/components/ChannelOrchestration.test.ts
cd frontend && bun run type-check
```

Then run broader verification if the focused checks changed production code:

```bash
cd backend-go && go test ./... -count=1
cd frontend && bun test
```

## Done Criteria

- Manual verification is documented with evidence.
- Config persistence has automated coverage.
- WebSocket metrics/fallback and frontend timer lifecycle have either automated coverage or a documented, accepted reason for manual-only verification.
- Commit hash mismatch is fixed.
- Optional polling optimization is either implemented with verification or explicitly deferred.
- No unrelated files are modified.
