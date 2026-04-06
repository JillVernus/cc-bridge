# Codex Priority Disable Design

## Goal

Allow a Responses channel to explicitly disable fast mode for requests that
arrive with `service_tier = "priority"`.

This extends the existing per-channel Codex priority override so a channel can
either:

- leave requests unchanged
- force missing/default traffic to `priority`
- force explicit `priority` traffic back to `default`

## Scope

This change applies only to Responses request bodies that explicitly include:

- `service_tier = "priority"`

It does not change:

- requests that omit `service_tier`
- requests with non-priority non-empty `service_tier`
- Claude bridge `speed = "fast"` behavior

## Current State

`cc-bridge` already supports a per-channel field:

- `codexServiceTierOverride`

Current supported values:

- `off`
- `force_priority`

Current behavior:

- `force_priority` rewrites missing or `default` `service_tier` to `priority`
- logs store the effective `serviceTier`
- logs store `serviceTierOverridden` when proxy rewrite occurs
- pricing treats effective `priority` as fast mode

## Proposed Change

### Channel Policy

Keep the existing field and add one more allowed value:

- `off`
- `force_priority`
- `force_default`

Recommended meaning:

- `off`
  - no rewrite
- `force_priority`
  - existing behavior
  - rewrite missing `service_tier` to `priority`
  - rewrite `service_tier = "default"` to `priority`
- `force_default`
  - new behavior
  - rewrite explicit `service_tier = "priority"` to `default`
  - do not modify missing `service_tier`
  - do not modify other non-empty values

## Effective Tier Rules

For a selected Responses or `openai-oauth` channel:

1. Read incoming `service_tier`.
2. Normalize the value using existing Responses tier normalization.
3. Apply channel override:
   - `force_priority`: empty/default -> `priority`
   - `force_default`: `priority` -> `default`
4. Mark `serviceTierOverridden = true` only when the proxy actually rewrote the
   request.
5. Recompute effective fast-mode state from the rewritten tier.

Result:

- forced downgrade to `default` must disable fast-mode pricing and fast-mode log
  indicators
- forwarded request body, request log metadata, and cost calculation must all
  agree

## Backend Design

### Config

Reuse `codexServiceTierOverride` as a string field.

No schema migration is required because the DB already persists this column as
text.

Accepted normalized values:

- `off`
- `force_priority`
- `force_default`

Config/API rules:

- trim surrounding whitespace before evaluating or persisting the value
- treat unknown or empty values as `off`
- treat the comparison as case-insensitive at read time, but persist canonical
  lowercase values
- for ineligible channel types, backend logic must ignore the field and behave
  as `off`
- frontend select inputs must only emit canonical values

### Request Rewriting

Extend the existing effective Responses service-tier helper in the handlers
layer.

Current helper returns:

- effective request body
- effective `serviceTier`
- effective `isFastMode`
- `serviceTierOverridden`

It should additionally support:

- `force_default` rewriting explicit `priority` to `default`

The helper remains the single source of truth for:

- upstream forwarded request body
- fast-mode billing decision
- request-log service-tier metadata

### Logging Invariant

For every selected Responses attempt, the exact effective tuple must stay
aligned across the whole pipeline:

- effective request body
- effective `serviceTier`
- effective `isFastMode`
- effective `serviceTierOverridden`

This tuple must flow through:

- successful completion
- retry-wait records
- failover records
- recreated pending records
- final error records
- request-log SSE payloads
- request-log list/detail fetches

Requirement:

- `serviceTierOverridden = true` must never be emitted with empty
  `serviceTier`
- a successful downgraded request must persist `serviceTier = "default"` and
  `serviceTierOverridden = true`

### Logging

Continue using:

- `serviceTier`
- `serviceTierOverridden`

No new DB field is required.

Interpretation:

- `serviceTier = "priority"` + `serviceTierOverridden = true`
  - proxy forced fast mode
- `serviceTier = "default"` + `serviceTierOverridden = true`
  - proxy disabled fast mode

Retry, failover, pending recreation, and final error paths must preserve the
effective service-tier metadata for the selected channel attempt.

## Frontend Design

### Channel Editor

Extend the existing select options in `AddChannelModal.vue`:

- Off
- Force priority for missing/default
- Force default for explicit priority

Update type definitions accordingly.

Frontend copy requirements:

- update the channel-setting label so it no longer implies one-way
  “priority-only” override semantics
- update the hint/help text in both English and Chinese to explain both
  `force_priority` and `force_default`
- the new option text must clearly state that only explicit `priority` is
  downgraded

### Request Log UI

Keep the existing override indicator, but distinguish the two outcomes:

- if `serviceTier === 'priority'` and `serviceTierOverridden`
  - show forced-fast meaning
- if `serviceTier !== 'priority'` and `serviceTierOverridden`
  - show forced-default / priority-disabled meaning

This can be done with:

- different tooltip text
- different label text
- optionally a different icon, if the existing UI would otherwise be ambiguous

Recommended minimal UI behavior:

- keep current override icon
- change tooltip/label based on final `serviceTier`

UI behavior requirements:

- downgraded rows must not display fast-mode wording or fast-mode explanatory
  text
- downgraded rows must display explicit “priority disabled by proxy” semantics
- forced-fast rows must keep the existing fast-mode semantics

## API / Type Changes

Frontend and backend channel types should allow:

- `'off' | 'force_priority' | 'force_default'`

No REST shape changes beyond the new string value are needed.

## Testing

### Backend Tests

Extend service-tier helper tests for:

- `priority` + `force_default` => `default`, not fast, overridden
- missing tier + `force_default` => unchanged, not overridden
- `default` + `force_default` => unchanged, not overridden
- non-empty non-priority tier + `force_default` => unchanged, not overridden

Extend OAuth builder tests for:

- explicit `priority` becomes `default`
- override flag is set when rewrite happens

Extend retry/error log regression coverage so downgraded attempts preserve:

- `serviceTier = "default"`
- `serviceTierOverridden = true`

Extend success-path coverage so completed downgraded requests preserve:

- `serviceTier = "default"`
- `serviceTierOverridden = true`

Extend config persistence coverage for:

- `force_default` save/load round-trip

Extend request-log fetch coverage for downgraded completed records across:

- direct DB fetch
- recent list fetch
- SSE created/updated payloads

### Frontend Checks

Verify:

- channel modal shows the third option
- channel modal copy explains both force-up and force-down behavior
- saved channel round-trips with `force_default`
- log table shows correct text/indicator for downgraded requests
- downgraded rows do not show fast-mode wording/icon semantics

## Risks

- UI wording may be confusing if “override” only implies forcing priority.
  The label and hint text should explain both directions clearly.
- Reusing `serviceTierOverridden` avoids schema churn, but the log UI must infer
  meaning from both `serviceTier` and the override flag.

## Recommendation

Implement this as a third value on the existing override field:

- `force_default`

This is the smallest coherent extension, keeps data model churn low, and fits
the current request-rewrite and logging pipeline cleanly.
