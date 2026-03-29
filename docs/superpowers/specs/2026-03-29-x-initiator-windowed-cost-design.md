# X-Initiator Windowed Cost Design

## Goal

Add a fourth `X-Initiator` override mode named `windowed_cost` for forward
proxy interception.

The new mode should let operators define, per intercepted domain:

- a fixed active window length in seconds
- a maximum total cost budget for that active window

The window should begin only when the first intercepted request with
`X-Initiator: user` arrives for the domain. While the window is active, all
forward-proxied requests for that domain should count toward the total cost
budget.

## Current State

Forward proxy `X-Initiator` override currently supports three modes:

- `fixed_window`
- `relative_countdown`
- `windowed_quota`

Current behavior is per intercepted domain:

- `fixed_window`
  - the first detected `X-Initiator: user` starts a fixed-duration window
  - the trigger request is not rewritten
  - later matching `user` requests are rewritten to `agent` until expiry
- `relative_countdown`
  - same trigger behavior as `fixed_window`
  - each successful rewrite refreshes the expiry
- `windowed_quota`
  - the first detected `X-Initiator: user` starts a fixed-duration window
  - the trigger request is not rewritten
  - later matching `user` requests are rewritten to `agent`
  - each rewrite consumes one remaining override
  - the domain resets immediately when quota reaches zero

The WebUI already exposes these modes in forward proxy settings and shows the
active runtime state in the request log toolbar chip and tooltip.

## Requirements

### Functional

- Add a fourth mode value: `windowed_cost`.
- Add a new numeric config field: `totalCost`.
- Keep behavior scoped per intercepted domain.
- For `windowed_cost`:
  - the first detected `X-Initiator: user` starts the domain window
  - the trigger request is not rewritten
  - later `X-Initiator: user` requests for the same domain are rewritten to
    `agent` while the window is active
  - all forward-proxied requests for that domain count toward the active
    window's accumulated cost, regardless of whether they were rewritten
  - the domain resets when the duration expires
  - the domain resets immediately after a request causes the accumulated domain
    cost to exceed the configured `totalCost`
  - the request that crosses the cost threshold is still allowed and counted
  - after reset, the next detected `user` request becomes a fresh trigger
- Preserve existing semantics for `fixed_window`, `relative_countdown`, and
  `windowed_quota`.
- Extend the existing runtime status chip and tooltip to represent
  `windowed_cost`.

### Non-Functional

- Avoid changing the semantics of the existing modes.
- Preserve backward compatibility for existing saved forward-proxy configs.
- Reuse the existing request-log price as the source of truth for cost
  accumulation.
- Keep runtime status responses safe for clients that do not yet read the new
  fields.

## Proposed Change

### Config Shape

Extend `XInitiatorOverrideConfig` with:

- `TotalCost float64`

Rules:

- `durationSeconds` remains required for all enabled modes
- `overrideTimes` is required and must be greater than zero only when
  `mode == windowed_quota`
- `totalCost` is required and must be greater than zero only when
  `mode == windowed_cost`
- `overrideTimes` is ignored by other modes
- `totalCost` is ignored by other modes

Default behavior:

- existing configs without `totalCost` must still load successfully
- use a concrete default of `totalCost = 1`
- return that default consistently in:
  - nil/default forward-proxy handler responses
  - normal GET config responses when persisted config omitted the field
  - frontend fallback/default config objects

## Runtime State Model

Use the same mode-specific approach already adopted for `windowed_quota`.

Implementation intent:

- keep the existing `fixed_window` and `relative_countdown` execution path and
  semantics as intact as possible
- keep the existing `windowed_quota` state model intact
- add the smallest additional internal state needed to support
  `windowed_cost` cleanly

Recommended state direction:

- retain expiry-based tracking for `fixed_window` and `relative_countdown`
- retain quota-specific per-domain tracking for `windowed_quota`
- add cost-specific per-domain tracking only for `windowed_cost`

For `windowed_cost`, the runtime state needs:

- `expiresAt`
- `accumulatedCost`
- `budgetCost`

## Effective Override Rules

For `windowed_cost`, when a request reaches the override logic:

1. Confirm the incoming domain key is normalized exactly as current logic does.
2. Confirm whether the incoming `X-Initiator` value is `user`.
3. If no active state exists for that domain:
   - if the request is not `user`, do nothing
   - if the request is `user`, create a fresh state:
     - `expiresAt = now + durationSeconds`
     - `accumulatedCost = 0`
     - `budgetCost = totalCost`
     - return without rewriting the trigger request
4. If active state exists but is expired:
   - if the request is not `user`, treat the domain as idle and do nothing
   - if the request is `user`, replace it with a fresh state and do not rewrite
     the current trigger request
5. If active non-expired state exists:
   - later `user` requests are rewritten from `user` to `agent`
   - non-`user` requests are forwarded as-is
   - the state remains active until time expiry or cost reset

Requests with missing or non-`user` initiator values do not create state.

## Cost Accounting Rules

Cost accounting for `windowed_cost` happens after the upstream request
completes and the final request-log `price` is known.

Rules:

- only active, non-expired `windowed_cost` states participate in accounting
- once a domain window is active, every forward-proxied request for that domain
  contributes its final `price` to `accumulatedCost`
- this includes:
  - the trigger request
  - rewritten `user -> agent` requests
  - non-`user` requests routed through the same intercepted domain while the
    window is active
- if the request completes after the runtime state has already expired by time,
  skip accounting and let the next `user` request start a fresh window
- if adding the completed request's price causes `accumulatedCost > budgetCost`,
  clear the domain state immediately after counting that request

This ensures the threshold-crossing request is still allowed and counted,
exactly as requested.

## Runtime Status API

Preserve the current summary response fields:

- `enabled`
- `mode`
- `activeDomains`
- `nearestExpiryAt`
- `nearestRemainingSeconds`

Keep using:

- `domains []XInitiatorOverrideDomainStatus`

Per-domain status for `windowed_cost` should include:

- `domain`
- `displayName`
- `expiresAt`
- `remainingSeconds`
- optional `accumulatedCost`
- optional `budgetCost`

Response rules:

- include only active, non-expired domains
- sort by nearest expiry first
- for non-cost modes, omit cost-budget fields
- keep new fields optional to avoid forcing all clients to consume them

## WebUI

### Forward Proxy Settings

In `ForwardProxySettings.vue`:

- add `windowed_cost` to the mode selector
- keep `durationSeconds` visible for all modes
- keep `overrideTimes` visible only when `mode == windowed_quota`
- add a `totalCost` numeric field shown only when `mode == windowed_cost`
- update helper copy to describe all four modes clearly

Expected operator-facing meanings:

- `fixed_window`: first `user` starts the window; later `user` requests rewrite
  until fixed expiry
- `relative_countdown`: same as fixed, but each successful rewrite refreshes
  the countdown
- `windowed_quota`: first `user` starts the window; later `user` requests
  rewrite until either quota is consumed or time expires
- `windowed_cost`: first `user` starts the window; later `user` requests
  rewrite while active; all forward-proxied requests count toward total domain
  cost until either the budget is exceeded or time expires

### Main Log Table Top Bar

The existing chip in `RequestLogTable.vue` should be extended rather than
replaced.

Compact label behavior:

- disabled: existing off state label
- enabled but idle: short mode label plus idle state
- active `fixed_window` and `relative_countdown`: retain current compact time
  summary behavior
- active `windowed_quota`: keep existing quota/time summary behavior
- active `windowed_cost`: show nearest active-domain cost/time summary in the
  form `Cost 1.84/2.00 - 173s` or equivalent localized copy

The compact chip should continue to represent the nearest-expiring active
domain.

### Tooltip

When active domains exist, the tooltip should show per-domain detail rows.

Each row should include:

- alias when configured, otherwise raw domain
- remaining seconds
- for `windowed_quota`, `remainingOverrides/totalOverrides`
- for `windowed_cost`, `accumulatedCost/budgetCost`

Sort tooltip rows by nearest expiry first.

### Localization

Add localized strings for:

- `windowed_cost` full label
- `windowed_cost` short label for the status chip
- `totalCost` field label
- `totalCost` helper text if needed
- tooltip phrasing for cost usage and remaining time

Update both:

- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`

## Expected Touchpoints

Backend:

- `backend-go/internal/forwardproxy/x_initiator_override.go`
- `backend-go/internal/forwardproxy/x_initiator_override_test.go`
- `backend-go/internal/forwardproxy/server.go`
- `backend-go/internal/forwardproxy/requestlog_capture.go`
- `backend-go/internal/handlers/forward_proxy.go`

Frontend:

- `frontend/src/services/api.ts`
- `frontend/src/components/ForwardProxySettings.vue`
- `frontend/src/components/RequestLogTable.vue`
- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`

## Scope

In scope:

- new `windowed_cost` backend mode
- config validation and default response updates
- runtime state support for per-domain cost tracking
- post-completion cost accumulation using request-log price data
- runtime status API detail payload for active cost windows
- settings UI updates for the new mode and `totalCost`
- request log top-bar chip and tooltip enhancements
- targeted backend tests and frontend verification

Out of scope:

- changing the per-domain semantics of existing modes
- making budget global across domains
- pre-request price estimation
- historical analytics for consumed window cost
- redesigning the request log toolbar layout beyond the existing chip

## Risks

### Completion-Time Accounting

Because the budget is updated only after a request completes, the runtime state
reflects completed cost rather than in-flight estimated cost. This is desired
for correctness, but the badge may lag behind in-flight usage until each
request finishes.

### Expiry Versus Late Completion

If a long-running request completes after the window has already expired, the
implementation must not accidentally revive or mutate stale domain state. Cost
updates should apply only to still-active windows.

### Backward Compatibility

Older persisted configs will not contain `totalCost`. The backend must apply
defaults safely and only enforce `totalCost > 0` when the selected mode is
`windowed_cost`.

## Validation

Backend:

- `cd backend-go && make test`
- unit tests for `windowed_cost` validation
- unit tests for trigger-only first request behavior
- unit tests for rewriting later `user` requests during active windows
- unit tests showing non-`user` requests do not start a window
- unit tests showing all forward-proxied requests count toward the active
  window's cost
- unit tests showing the threshold-crossing request is counted and then resets
  the state
- unit tests showing expiry resets state even when the budget is not reached
- unit tests for per-domain isolation
- runtime status tests covering nearest expiry, active counts, and per-domain
  cost detail payloads

Frontend:

- `cd frontend && bun run type-check`
- `cd frontend && bun run build`
- manual verification of settings conditional fields
- manual verification of chip labels for off, idle, active quota, and active
  cost states
- manual verification of tooltip detail rows for multiple domains

Manual runtime scenarios:

- first `user` request starts the cost window and is not rewritten
- trigger request cost contributes to the active window after completion
- later `user` requests rewrite while the window is active
- non-`user` requests still contribute cost once the window is active
- the request that exceeds the configured cost budget is still counted
- the window resets immediately after the threshold-crossing request completes
- expiry clears state even when budget remains
- multiple active domains show the nearest-expiry domain in the chip and all
  active domain rows in the tooltip
