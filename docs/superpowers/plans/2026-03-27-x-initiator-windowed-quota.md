# X-Initiator Windowed Quota Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the per-domain `windowed_quota` X-Initiator override mode, expose its config in the forward-proxy settings UI, and extend the main log table status chip to show quota/time runtime state.

**Architecture:** Keep the existing `fixed_window` and `relative_countdown` behavior paths as intact as possible. Add the smallest quota-specific backend state needed for `windowed_quota`, extend the runtime status payload with optional per-domain details, then update the Vue settings form and existing request-log toolbar chip to consume the new mode and fields.

**Tech Stack:** Go 1.22, Gin handlers, forward-proxy JSON config, Vue 3 Composition API, Vuetify 3, TypeScript API models, i18n locale dictionaries.

---

### Task 1: Backend Config Shape And Defaults

**Files:**
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/handlers/forward_proxy.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`
- Create: `backend-go/internal/handlers/forward_proxy_test.go`

- [ ] **Step 1: Write the failing config/default tests**

Add focused tests that expect:
- `validateXInitiatorOverrideConfig` accepts `windowed_quota` when `durationSeconds > 0` and `overrideTimes > 0`
- validation rejects `windowed_quota` when `overrideTimes <= 0`
- `Server.GetConfig()` fills missing defaults with `mode = fixed_window`, `durationSeconds = 300`, and `overrideTimes = 1`
- handler GET/update responses include `mode`, `durationSeconds`, and `overrideTimes` defaults in JSON
- non-quota modes still validate when `overrideTimes` is omitted or zero-valued because the field is ignored outside `windowed_quota`
- a persisted quota config missing `overrideTimes` is normalized back to `overrideTimes = 1` in the returned API shape

- [ ] **Step 2: Run focused backend tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(ValidateXInitiatorOverrideConfig|GetConfig_XInitiatorOverrideDefaults)' && go test ./internal/handlers -run 'TestForwardProxyConfig_(GetDefaults|UpdateReturnsDefaults)'`
Expected: FAIL because `overrideTimes` and `windowed_quota` are not implemented yet.

- [ ] **Step 3: Implement minimal config/default support**

Update `XInitiatorOverrideConfig` to include `OverrideTimes int`, add the `XInitiatorOverrideModeWindowedQuota` constant, extend validation to require `overrideTimes > 0` only for `windowed_quota`, and normalize default values in `Server.GetConfig()` plus the nil/default handler response.

- [ ] **Step 4: Reset in-memory state on config changes**

Keep the existing reset-on-update behavior in `server.go`, but make sure both normal load and update paths carry the new config field safely so older persisted JSON without `overrideTimes` still works through defaults.

- [ ] **Step 5: Re-run focused backend tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'Test(ValidateXInitiatorOverrideConfig|GetConfig_XInitiatorOverrideDefaults)' && go test ./internal/handlers -run 'TestForwardProxyConfig_(GetDefaults|UpdateReturnsDefaults)'`
Expected: PASS.

### Task 2: Backend Windowed Quota Runtime Logic

**Files:**
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/forwardproxy/x_initiator_override.go`
- Test: `backend-go/internal/forwardproxy/x_initiator_override_test.go`

- [ ] **Step 1: Write the failing quota-behavior tests**

Add table-driven or focused tests for:
- first `X-Initiator: user` starts the window and does not rewrite
- second and later matching requests rewrite and decrement quota
- quota exhaustion clears the active domain immediately
- expiry resets the domain even when quota remains
- per-domain isolation still holds
- non-`user` headers do not trigger or consume state

- [ ] **Step 2: Run the focused runtime tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestApplyXInitiatorOverride_(FixedWindowPerDomain|RelativeCountdownRefreshesPerDomain|WindowedQuotaPerDomain|WindowedQuotaIgnoresNonUser)'`
Expected: FAIL because `windowed_quota` state tracking does not exist yet.

- [ ] **Step 3: Add quota-specific runtime state**

In `server.go`, keep the existing expiry-based map for current modes if that stays simplest, and add the smallest additional per-domain state map or struct needed for `windowed_quota` (`expiresAt`, `remainingOverrides`, `totalOverrides`). Avoid changing the observable behavior of `fixed_window` and `relative_countdown`.

- [ ] **Step 4: Implement `windowed_quota` override behavior**

Update `applyXInitiatorOverride` so:
- the first matching `user` request starts state and returns without rewrite
- active `windowed_quota` state rewrites `user -> agent` only while quota remains
- each successful rewrite decrements remaining quota
- hitting zero removes the domain state immediately
- expiry recreates a fresh trigger state instead of rewriting the expired request

- [ ] **Step 5: Re-run the focused runtime tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestApplyXInitiatorOverride_(FixedWindowPerDomain|RelativeCountdownRefreshesPerDomain|WindowedQuotaPerDomain|WindowedQuotaIgnoresNonUser)'`
Expected: PASS.

### Task 3: Runtime Status Payload And Handler Coverage

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
- include `remainingOverrides` and `totalOverrides` only for `windowed_quota`
- surface the same `domains` detail payload through handler GET/update responses when active

- [ ] **Step 2: Run the focused status tests to verify failure**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestGetXInitiatorOverrideRuntimeStatus' && go test ./internal/handlers -run 'TestForwardProxyConfig_RuntimeStatusDomains'`
Expected: FAIL because the detail payload is not returned yet.

- [ ] **Step 3: Implement the runtime status detail model**

Add a dedicated per-domain status struct, filter out expired domains, sort active domains by nearest expiry, and resolve `displayName` through the existing domain-alias helper so the tooltip can render per-domain rows without duplicating alias logic in the frontend.

- [ ] **Step 4: Verify handler responses use the richer status shape**

Keep the handler response shape backward-compatible, but ensure both GET and update responses return the new config defaults and runtime status fields via `fpServer.GetXInitiatorOverrideRuntimeStatus()`.

- [ ] **Step 5: Re-run the focused status tests**

Run: `cd backend-go && go test ./internal/forwardproxy -run 'TestGetXInitiatorOverrideRuntimeStatus' && go test ./internal/handlers -run 'TestForwardProxyConfig_RuntimeStatusDomains'`
Expected: PASS.

### Task 4: Frontend Types, Settings Form, And Locale Copy

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/ForwardProxySettings.vue`
- Modify: `frontend/src/components/ForwardProxyDiscoveryView.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Extend frontend API types first**

Add `windowed_quota` and `overrideTimes` to the forward-proxy config types, and add the optional per-domain runtime status array/type so Vue code can consume the backend payload without `any`-style shortcuts.

- [ ] **Step 2: Update frontend fallback/default objects**

Update the local default config objects in `ForwardProxySettings.vue` and `ForwardProxyDiscoveryView.vue` to include `overrideTimes: 1` and the new runtime detail array shape.

- [ ] **Step 3: Implement the settings form changes**

Add the `windowed_quota` select option, show the `overrideTimes` numeric field only for that mode, and clamp/save numeric values so the payload always sends positive integers for enabled quota mode.

- [ ] **Step 4: Update localized labels and helper text**

Add full and short mode labels plus the `overrideTimes` field label and chip/tooltip copy in both locale files. Keep the mode descriptions aligned with the approved semantics: trigger request does not consume quota, later requests do.

- [ ] **Step 5: Run frontend type-check**

Run: `cd frontend && bun run type-check`
Expected: PASS.

### Task 5: Request Log Top-Bar Chip And Tooltip

**Files:**
- Modify: `frontend/src/components/RequestLogTable.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Update computed helpers for the new mode**

Extend the mode-label and chip-label computed values so `windowed_quota` shows a short mode label, stays `idle` when no active domains exist, and uses the nearest-expiring domain summary when active.

- [ ] **Step 2: Render nearest-domain quota summary in the existing chip**

Use the first sorted runtime domain entry to render compact text in the form `Quota 2/3 - 184s` (localized equivalent), while keeping the existing fixed/countdown display behavior unchanged.

- [ ] **Step 3: Expand the tooltip to show per-domain rows**

Render each active domain row using `displayName`, remaining seconds, and, for `windowed_quota`, `remainingOverrides/totalOverrides`. Keep the tooltip compact but readable for multiple active domains.

- [ ] **Step 4: Run frontend build verification**

Run: `cd frontend && bun run build`
Expected: PASS.

### Task 6: Final Regression Verification

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
- choose `Windowed quota`
- set `Duration = 300` and `Override Times = 3`
- save, then watch the existing status chip above the main log table in `RequestLogTable.vue`

Exercise these request sequences against an intercepted domain A using requests that send `X-Initiator: user`:
1. first request to domain A -> upstream header remains `user`, chip becomes active, tooltip shows domain A with `3/3` remaining
2. second request to domain A -> upstream header becomes `agent`, chip updates to `2/3`
3. third request to domain A -> upstream header becomes `agent`, chip updates to `1/3`
4. fourth request to domain A -> upstream header becomes `agent`, quota is exhausted, domain A disappears from active runtime state immediately
5. next request after exhaustion to domain A -> treated as a fresh trigger again, no rewrite

Then verify:
- let a window expire before quota exhaustion and confirm the next request is a fresh trigger
- repeat the sequence on domain B while domain A is active and confirm per-domain isolation
- when both domains are active, confirm the chip shows the nearest-expiry domain and the tooltip lists both active domains

- [ ] **Step 5: Prepare commit summary for implementation session**

Use commit intent that matches the finished change set, for example: `feat: add windowed quota mode for X-Initiator override`
