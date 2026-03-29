# X-Initiator Windowed Cost Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the per-domain `windowed_cost` X-Initiator override mode, expose its config in the forward-proxy settings UI, and extend the main log table status chip to show cost/time runtime state.

**Architecture:** Keep the existing `fixed_window`, `relative_countdown`, and `windowed_quota` paths intact. Add the smallest cost-specific backend state needed for `windowed_cost`, update the forward-proxy completion path to accumulate final request cost for active domain windows, then extend the Vue settings form and existing request-log toolbar chip to consume the new mode and fields.

**Tech Stack:** Go 1.22, Gin handlers, forward-proxy JSON config, SQLite-backed request logs, Vue 3 Composition API, Vuetify 3, TypeScript API models, i18n locale dictionaries.

---

### Task 1: Backend Config Shape And Defaults

**Files:**
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/handlers/forward_proxy.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`
- Modify: `backend-go/internal/handlers/forward_proxy_test.go`

- [ ] **Step 1: Write the failing config/default tests**

Add focused tests that expect:
- `validateXInitiatorOverrideConfig` accepts `windowed_cost` when `durationSeconds > 0` and `totalCost > 0`
- validation rejects `windowed_cost` when `totalCost <= 0`
- non-cost modes still validate when `totalCost` is omitted or zero-valued because the field is ignored outside `windowed_cost`
- `Server.GetConfig()` fills missing defaults with `mode = fixed_window`, `durationSeconds = 300`, `overrideTimes = 1`, and `totalCost = 1`
- handler GET/update responses include `totalCost` defaults in JSON
- a persisted cost config missing `totalCost` is normalized back to `totalCost = 1` in the returned API shape

- [ ] **Step 2: Run focused backend tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(ValidateXInitiatorOverrideConfig|GetConfig_XInitiatorOverrideDefaults)' && go test ./internal/handlers -run 'TestForwardProxyConfig_(GetDefaults|UpdateReturnsDefaults)'`
Expected: FAIL because `windowed_cost` and `totalCost` are not implemented yet.

- [ ] **Step 3: Implement minimal config/default support**

Update `XInitiatorOverrideConfig` to include `TotalCost float64`, add the `XInitiatorOverrideModeWindowedCost` constant, extend validation to require `totalCost > 0` only for `windowed_cost`, and normalize default values in `Server.GetConfig()` plus the nil/default handler response.

- [ ] **Step 4: Keep persisted config compatibility**

Mirror the existing legacy-defaulting logic used for `overrideTimes` so older persisted JSON that selects `windowed_cost` but omits `totalCost` is normalized safely during load without changing behavior for other modes.

- [ ] **Step 5: Re-run focused backend tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(ValidateXInitiatorOverrideConfig|GetConfig_XInitiatorOverrideDefaults)' && go test ./internal/handlers -run 'TestForwardProxyConfig_(GetDefaults|UpdateReturnsDefaults)'`
Expected: PASS.

### Task 2: Backend Window Start And Override Runtime Logic

**Files:**
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`

- [ ] **Step 1: Write the failing runtime-behavior tests**

Add focused tests for `windowed_cost` that expect:
- first `X-Initiator: user` request starts the window and does not rewrite
- later matching `user` requests rewrite while the window is active
- non-`user` requests do not start a new window
- expired state causes the next `user` request to become a fresh trigger
- per-domain isolation still holds

- [ ] **Step 2: Run the focused runtime tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestApplyXInitiatorOverride_(WindowedCostPerDomain|WindowedCostIgnoresNonUser|WindowedCostExpiryReset)'`
Expected: FAIL because `windowed_cost` state tracking does not exist yet.

- [ ] **Step 3: Add cost-specific runtime state**

In `server.go`, add the smallest additional per-domain state map or struct needed for `windowed_cost`, storing:
- `expiresAt`
- `accumulatedCost`
- `budgetCost`

Keep the existing expiry map for current modes and the quota-specific state for `windowed_quota`.

- [ ] **Step 4: Implement `windowed_cost` override behavior**

Update `applyXInitiatorOverride` so:
- a missing active state only starts when the incoming header is `user`
- the trigger request creates the state and does not rewrite
- later `user` requests rewrite from `user` to `agent` while the state is active
- non-`user` requests pass through unchanged
- expired state is replaced only by a fresh `user` trigger, not by arbitrary traffic

- [ ] **Step 5: Re-run the focused runtime tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestApplyXInitiatorOverride_(WindowedCostPerDomain|WindowedCostIgnoresNonUser|WindowedCostExpiryReset)'`
Expected: PASS.

### Task 3: Post-Completion Cost Accounting For Active Windows

**Files:**
- Modify: `backend-go/internal/forwardproxy/interceptor.go`
- Modify: `backend-go/internal/forwardproxy/requestlog_capture.go`
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Test: `backend-go/internal/forwardproxy/requestlog_capture_test.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`

- [ ] **Step 1: Write the failing cost-accounting tests**

Add focused tests that expect:
- the trigger request cost contributes to the active domain window after completion
- once a window is active, non-`user` requests still contribute cost for that domain
- rewritten `user -> agent` requests also contribute cost
- the request that causes accumulated cost to exceed `totalCost` is still counted
- state resets immediately after the threshold-crossing request completes
- if a request completes after expiry, it does not revive or mutate stale state

- [ ] **Step 2: Run the focused cost tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(HandleHTTPForward_WindowedCostAccounting|ApplyWindowedCostCompletion)'`
Expected: FAIL because no completion-time cost accumulation exists yet.

- [ ] **Step 3: Add a narrow completion hook for `windowed_cost`**

Implement a dedicated helper in `x_initiator_override.go` or `server.go` that updates active `windowed_cost` state only after final request `price` is known. Keep this separate from `applyXInitiatorOverride` so request-start behavior and completion-time accounting stay easy to reason about.

- [ ] **Step 4: Call the helper from both streaming and JSON completion paths**

In `interceptor.go`, after the final request log completion record is created and its `Price` is known, update the active domain cost window using:
- normalized `hostOnly`
- final request completion time
- final `record.Price`

Make sure both `proxySSEResponse` and `proxyJSONResponse` use the same helper so cost accounting stays consistent.

- [ ] **Step 5: Re-run the focused cost tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(HandleHTTPForward_WindowedCostAccounting|ApplyWindowedCostCompletion)'`
Expected: PASS.

### Task 4: Runtime Status Payload And Handler Coverage

**Files:**
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/handlers/forward_proxy.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`
- Modify: `backend-go/internal/handlers/forward_proxy_test.go`

- [ ] **Step 1: Write the failing runtime-status tests**

Add tests that expect `GetXInitiatorOverrideRuntimeStatus()` to:
- keep `enabled`, `mode`, `activeDomains`, `nearestExpiryAt`, and `nearestRemainingSeconds`
- include a `domains` array sorted by nearest expiry
- include alias-backed `displayName` when available
- include `accumulatedCost` and `budgetCost` only for `windowed_cost`
- surface the same `domains` detail payload through handler GET/update responses when active

- [ ] **Step 2: Run the focused status tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestGetXInitiatorOverrideRuntimeStatus' && go test ./internal/handlers -run 'TestForwardProxyConfig_RuntimeStatusDomains'`
Expected: FAIL because cost detail fields are not returned yet.

- [ ] **Step 3: Implement the runtime status detail model**

Extend the per-domain runtime status struct with optional cost fields, filter out expired domains, sort active domains by nearest expiry, and resolve `displayName` through the existing domain-alias helper so the frontend tooltip can render cost rows without duplicating alias logic.

- [ ] **Step 4: Verify handler responses use the richer status shape**

Keep the handler response shape backward-compatible, but ensure both GET and update responses return the new config defaults and runtime status fields through the existing snapshot/status helpers.

- [ ] **Step 5: Re-run the focused status tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestGetXInitiatorOverrideRuntimeStatus' && go test ./internal/handlers -run 'TestForwardProxyConfig_RuntimeStatusDomains'`
Expected: PASS.

### Task 5: Frontend Types, Settings Form, And Locale Copy

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/ForwardProxySettings.vue`
- Modify: `frontend/src/components/ForwardProxyDiscoveryView.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Extend frontend API types first**

Add `windowed_cost` and `totalCost` to the forward-proxy config types, and add optional per-domain runtime status cost fields so Vue code can consume the backend payload without `any`-style shortcuts.

- [ ] **Step 2: Update frontend fallback/default objects**

Update the local default config objects in `ForwardProxySettings.vue` and `ForwardProxyDiscoveryView.vue` to include `totalCost: 1` and the new runtime detail field shape.

- [ ] **Step 3: Implement the settings form changes**

Add the `windowed_cost` select option, show the `totalCost` numeric field only for that mode, keep `overrideTimes` exclusive to `windowed_quota`, and clamp/save numeric values so the payload always sends a positive cost for enabled cost mode.

- [ ] **Step 4: Update localized labels and helper text**

Add full and short mode labels plus the `totalCost` field label and chip/tooltip copy in both locale files. Keep the mode descriptions aligned with the approved semantics:
- first `user` request starts the window
- later `user` requests rewrite while active
- all forward-proxied requests count toward the active domain cost budget

- [ ] **Step 5: Run frontend type-check**

Run: `cd frontend && bun run type-check`
Expected: PASS.

### Task 6: Request Log Top-Bar Chip And Tooltip

**Files:**
- Modify: `frontend/src/components/RequestLogTable.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Update computed helpers for the new mode**

Extend the mode-label and chip-label computed values so `windowed_cost` shows a short mode label, stays `idle` when no active domains exist, and uses the nearest-expiring domain summary when active.

- [ ] **Step 2: Render nearest-domain cost summary in the existing chip**

Use the first sorted runtime domain entry to render compact text in the form `Cost 1.84/2.00 - 173s` (localized equivalent), while keeping the existing fixed/countdown/quota display behavior unchanged.

- [ ] **Step 3: Expand the tooltip to show per-domain cost rows**

Render each active domain row using `displayName`, remaining seconds, and:
- `remainingOverrides/totalOverrides` for `windowed_quota`
- `accumulatedCost/budgetCost` for `windowed_cost`

Keep the tooltip compact but readable for multiple active domains.

- [ ] **Step 4: Run frontend build verification**

Run: `cd frontend && bun run build`
Expected: PASS.

### Task 7: Final Regression Verification

**Files:**
- Modify: `backend-go/internal/forwardproxy/x_initiator_override_test.go` (only if assertion cleanup is still needed)
- Modify: `frontend/src/components/RequestLogTable.vue` (only if final polish is needed)

- [ ] **Step 1: Run the full forward-proxy backend package tests**

Run: `cd backend-go && go test ./internal/forwardproxy`
Expected: PASS.

- [ ] **Step 2: Run the full backend test suite**

Run: `cd backend-go && make test`
Expected: PASS.

- [ ] **Step 3: Re-run frontend checks after review polish**

Run: `cd frontend && bun run type-check && bun run build`
Expected: PASS.

- [ ] **Step 4: Manually verify the runtime scenarios**

Use the running app with forward proxy enabled.

In the WebUI:
- open forward proxy settings in `ForwardProxySettings.vue`
- enable `X-Initiator Override`
- choose `Windowed cost`
- set `Duration = 300` and `Total Cost = 2.00`
- save, then watch the existing status chip above the main log table in `RequestLogTable.vue`

Exercise these request sequences against an intercepted domain A:
1. first request to domain A with `X-Initiator: user` -> upstream header remains `user`, chip becomes active
2. let that request complete -> tooltip shows domain A with accumulated cost reflecting the trigger request
3. second request to domain A with `X-Initiator: user` -> upstream header becomes `agent`, accumulated cost increases after completion
4. send a non-`user` request to domain A while the window is active -> header stays as sent, accumulated cost still increases after completion
5. send another request that causes accumulated cost to exceed `2.00` -> request is still allowed, counted, then domain A disappears from active runtime state immediately after completion
6. next request after reset to domain A with `X-Initiator: user` -> treated as a fresh trigger again, no rewrite

Then verify:
- let a window expire before cost exhaustion and confirm the next `user` request is a fresh trigger
- repeat the sequence on domain B while domain A is active and confirm per-domain isolation
- when both domains are active, confirm the chip shows the nearest-expiry domain and the tooltip lists both active domains

- [ ] **Step 5: Prepare commit summary for implementation session**

Use commit intent that matches the finished change set, for example: `feat: add windowed cost mode for X-Initiator override`
