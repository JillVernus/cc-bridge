# X-Initiator Windowed Quota Design

## Goal

Add a third `X-Initiator` override mode named `windowed_quota` for forward
proxy interception.

The new mode should let operators define, per intercepted domain:

- a fixed active window length in seconds
- a maximum number of `user -> agent` rewrites allowed during that window

The WebUI must also surface the mode in settings and extend the existing
runtime status chip above the main request log table so operators can see quota
and time state at a glance.

## Current State

Forward proxy `X-Initiator` override currently supports two modes:

- `fixed_window`
- `relative_countdown`

Current behavior is per intercepted domain:

- the first detected `X-Initiator: user` starts a domain window and is not
  rewritten
- later matching requests during the active window are rewritten to
  `X-Initiator: agent`
- `fixed_window` keeps the original expiry time
- `relative_countdown` refreshes the expiry after each successful rewrite

Current runtime state only tracks per-domain expiry timestamps, and the main
request log toolbar chip only shows a compact mode label plus nearest expiry.

## Requirements

### Functional

- Add a third mode value: `windowed_quota`.
- Add a new numeric config field: `overrideTimes`.
- Keep behavior scoped per intercepted domain.
- For `windowed_quota`:
  - the first detected `X-Initiator: user` starts the domain window
  - the trigger request is not rewritten and does not consume quota
  - later `X-Initiator: user` requests for the same domain are rewritten to
    `agent` while the window is active
  - each successful rewrite consumes one remaining override
  - the domain resets immediately when quota reaches zero
  - the domain also resets when the window duration expires
  - after reset, the next detected `user` request becomes a fresh trigger
- Preserve existing semantics for `fixed_window` and `relative_countdown`.
- Extend the existing top-bar status chip above the main request log table to
  represent `windowed_quota`.
- Add tooltip details for all active domains when `windowed_quota` is active.

### Non-Functional

- Avoid refactoring the other two modes unless required for compatibility.
- Preserve backward compatibility for existing saved forward-proxy configs.
- Keep runtime status responses safe for clients that do not yet read the new
  fields.

## Proposed Change

### Config Shape

Extend `XInitiatorOverrideConfig` with:

- `overrideTimes int`

Rules:

- `durationSeconds` remains required for all enabled modes
- `overrideTimes` is required and must be greater than zero only when
  `mode == windowed_quota`
- `overrideTimes` is ignored by `fixed_window` and `relative_countdown`

Default behavior:

- existing configs without `overrideTimes` must still load successfully
- use a concrete default of `overrideTimes = 1`
- return that default consistently in:
  - nil/default forward-proxy handler responses
  - normal GET config responses when persisted config omitted the field
  - frontend fallback/default config objects

### Runtime State Model

Use the approved mode-specific approach instead of a broad internal refactor.

Implementation intent:

- keep the existing `fixed_window` and `relative_countdown` execution path and
  semantics as intact as possible
- add the smallest additional internal state needed to support
  `windowed_quota` cleanly
- do not introduce per-domain `mode` storage unless the implementation proves
  it is necessary

Recommended state direction:

- retain expiry-based tracking for the existing modes
- add quota-specific per-domain tracking only for `windowed_quota`

For `windowed_quota`, the runtime state needs:

- `expiresAt`
- `remainingOverrides`
- `totalOverrides`

Behavior by mode:

- `fixed_window`
  - first `user` request creates the active expiry window
  - later matching requests override until expiry
  - expiry does not move on override
- `relative_countdown`
  - first `user` request creates the active expiry window
  - later matching requests override and refresh expiry
- `windowed_quota`
  - first `user` request creates state with `expiresAt` and
    `remainingOverrides = totalOverrides = overrideTimes`
  - later matching requests override only while `remainingOverrides > 0`
  - each successful override decrements `remainingOverrides`
  - if `remainingOverrides` becomes zero after a rewrite, remove the domain
    state immediately
  - if `expiresAt` is reached first, treat the domain as idle and remove or
    ignore stale state on next access

This keeps `windowed_quota` explicit without rewriting the other two modes more
than necessary.

## Effective Override Rules

For `windowed_quota`, when a request reaches the override logic:

1. Confirm the incoming `X-Initiator` value is `user`.
2. Normalize the intercepted domain key exactly as current logic does.
3. If no active state exists for that domain, create a fresh state:
   - `expiresAt = now + durationSeconds`
   - `remainingOverrides = overrideTimes`
   - return without rewriting the trigger request
4. If active state exists but is expired, replace it with a fresh state and do
   not rewrite the current trigger request.
5. If active non-expired state exists and `remainingOverrides > 0`:
   - rewrite `X-Initiator` from `user` to `agent`
   - decrement `remainingOverrides`
6. If quota reaches zero after decrement, clear the domain state immediately.
7. Otherwise keep the state active until `expiresAt`.

Requests with missing or non-`user` initiator values do not create or consume
state.

## Runtime Status API

Preserve the current summary response fields:

- `enabled`
- `mode`
- `activeDomains`
- `nearestExpiryAt`
- `nearestRemainingSeconds`

Add optional detail fields so the WebUI can render richer status for
`windowed_quota`.

Recommended extension:

- `domains []XInitiatorOverrideDomainStatus`

Per-domain status should include:

- `domain`
- `displayName` (resolved alias when available, else raw domain)
- `expiresAt`
- `remainingSeconds`
- optional `remainingOverrides`
- optional `totalOverrides`

Response rules:

- include only active, non-expired domains
- sort by nearest expiry first so the most urgent domain appears first in the
  UI tooltip
- for non-quota modes, omit override-count fields
- keep new fields optional to avoid forcing all clients to consume them

## WebUI

### Forward Proxy Settings

In `ForwardProxySettings.vue`:

- add `windowed_quota` to the mode selector
- keep `durationSeconds` visible for all modes
- add an `overrideTimes` numeric field shown only when
  `mode == windowed_quota`
- update helper copy to describe all three modes clearly

Expected operator-facing meanings:

- `fixed_window`: first `user` starts the window; later `user` requests rewrite
  until fixed expiry
- `relative_countdown`: same as fixed, but each successful rewrite refreshes the
  countdown
- `windowed_quota`: first `user` starts the window; later `user` requests
  rewrite until either quota is consumed or time expires

### Main Log Table Top Bar

The existing chip in `RequestLogTable.vue` should be extended rather than
replaced.

Compact label behavior:

- disabled: existing off state label
- enabled but idle: short mode label plus idle state
- active `fixed_window` and `relative_countdown`: retain current compact time
  summary behavior
- active `windowed_quota`: show nearest active-domain quota/time summary in the
  form `Quota 2/3 - 184s` or equivalent localized copy

The compact chip should represent the nearest-expiring active domain so the top
bar stays readable.

### Tooltip

When active domains exist, the tooltip should show per-domain detail rows.

Each row should include:

- alias when configured, otherwise raw domain
- remaining seconds
- for `windowed_quota`, `remainingOverrides/totalOverrides`

Sort tooltip rows by nearest expiry first.

This keeps the chip compact while preserving debuggability for multiple active
domains.

### Localization

Add localized strings for:

- `windowed_quota` full label
- `windowed_quota` short label for the status chip
- `overrideTimes` field label
- `overrideTimes` helper text if needed
- tooltip phrasing for quota usage and remaining time

Update both:

- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`

## Expected Touchpoints

Backend:

- `backend-go/internal/forwardproxy/x_initiator_override.go`
- `backend-go/internal/forwardproxy/x_initiator_override_test.go`
- `backend-go/internal/forwardproxy/server.go`
- `backend-go/internal/handlers/forward_proxy.go`

Frontend:

- `frontend/src/services/api.ts`
- `frontend/src/components/ForwardProxySettings.vue`
- `frontend/src/components/RequestLogTable.vue`
- `frontend/src/components/ForwardProxyDiscoveryView.vue`
- `frontend/src/locales/en.ts`
- `frontend/src/locales/zh-CN.ts`

## Scope

In scope:

- new `windowed_quota` backend mode
- config validation and default response updates
- runtime state support for per-domain quota tracking
- runtime status API detail payload for active domains
- settings UI updates for the new mode and `overrideTimes`
- request log top-bar chip and tooltip enhancements
- targeted backend tests and frontend verification

Out of scope:

- changing the per-domain semantics of existing modes
- making quota global across domains
- introducing cross-domain shared counters
- adding historical analytics for consumed overrides
- redesigning the request log toolbar layout beyond the existing chip

## Risks

### Ambiguous State Cleanup

If expired or exhausted domain state is not cleaned consistently, the chip may
show stale active domains or incorrect quota values. The implementation should
filter inactive entries in runtime status and aggressively clear exhausted quota
entries.

### Backward Compatibility

Older persisted configs will not contain `overrideTimes`. The backend must
apply defaults safely and only enforce `overrideTimes > 0` when the selected
mode is `windowed_quota`.

### UI Compression

Multiple active domains can make the top bar noisy. The chip should remain a
single nearest-domain summary, with full detail moved into the tooltip.

## Validation

Backend:

- `cd backend-go && make test`
- unit tests for trigger-only first request behavior
- unit tests for quota consumption and immediate reset on exhaustion
- unit tests for expiry reset with remaining quota still available
- unit tests for per-domain isolation
- regression coverage for `fixed_window` and `relative_countdown`
- runtime status tests covering nearest expiry, active counts, and per-domain
  detail payloads

Frontend:

- `cd frontend && bun run type-check`
- `cd frontend && bun run build`
- manual verification of settings conditional fields
- manual verification of chip labels for off, idle, and active quota states
- manual verification of tooltip detail rows for multiple domains

Manual runtime scenarios:

- first `user` request starts the window and does not consume quota
- second matching `user` request consumes one override and updates chip summary
- final quota-consuming request clears active state immediately
- expiry clears state even when quota remains
- multiple active domains show nearest-expiry domain in the chip and all active
  domain rows in the tooltip
