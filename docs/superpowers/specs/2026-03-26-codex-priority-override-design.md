# Codex Priority Override Design

## Goal

Allow a specific Responses channel to force Codex `/v1/responses` traffic into
`service_tier = "priority"` when the incoming request omits `service_tier` or
sets it to `"default"`.

## Current State

`cc-bridge` already treats `service_tier = "priority"` as Codex fast mode:

- request logs store `service_tier`
- pricing doubles cost when fast mode is detected
- the WebUI shows a thunder icon in the log table when the stored tier is
  `priority`

Today that tier comes from the client request body itself, or from the
Messages-to-Responses bridge mapping `speed = "fast"` to `priority`.

There is no per-channel setting that can promote a Codex request to priority
mode on behalf of the client, and there is no UI signal that distinguishes
"client requested priority" from "proxy forced priority".

## Requirements

### Functional

- Add a per-channel setting for Responses channels that controls Codex service
  tier override behavior.
- Support at least:
  - `off`
  - `force_priority`
- When `force_priority` is enabled for a selected channel:
  - missing `service_tier` must be rewritten to `"priority"`
  - `service_tier = "default"` must be rewritten to `"priority"`
  - `service_tier = "priority"` must remain unchanged
  - other non-empty values must remain unchanged
- Apply the override only to Codex `/v1/responses` traffic.
- Store explicit evidence in the request log when the proxy performed the
  rewrite.
- Show a dedicated icon beside the existing thunder icon in the WebUI main log
  table model column when the request was proxy-overridden.

### Non-Functional

- Keep pricing, request logging, and upstream request bodies aligned.
- Preserve existing behavior for non-Codex traffic.
- Preserve existing behavior for Messages bridging unless we intentionally
  extend the feature later.

## Proposed Change

### Channel Config

Add a new channel field:

- `codexServiceTierOverride`

Allowed values:

- `off`
- `force_priority`

This field should be carried through:

- JSON config structs
- config DB persistence
- config REST responses and updates
- frontend channel types
- channel create/edit modal

The setting should only be shown for Responses-pool channels whose
`serviceType` is `responses` or `openai-oauth`.

## Effective Override Rules

For a selected channel with `codexServiceTierOverride = "force_priority"`:

1. Confirm the request is Codex traffic.
2. Read the current request `service_tier`.
3. If the value is empty or `"default"`, rewrite it to `"priority"`.
4. Mark the request as `service_tier_overridden = true`.
5. Recompute fast-mode state from the effective tier so logging and pricing use
   the same answer as the forwarded upstream request.

If the request already contains `service_tier = "priority"`, keep it and do not
mark it overridden.

## Architecture

### Effective Tier Must Be Channel-Aware

The override cannot be decided once at request ingress, because the setting is
owned by the selected channel.

That matters for:

- single-channel Responses routing
- multi-channel Responses routing with failover
- `openai-oauth` Codex routing

The effective tier therefore needs to be computed per selected channel attempt.

### Recommended Backend Shape

Introduce a small helper in the Responses handler layer that accepts:

- raw request body
- parsed request model
- selected upstream channel

And returns:

- effective request body bytes
- effective `serviceTier`
- effective `isFastMode`
- `serviceTierOverridden`

That helper should:

- detect Codex requests using the existing model-based Codex detection
- inspect the selected channel override setting
- rewrite the JSON body only when needed
- normalize the resulting effective tier for local billing and logging

### Request Forwarding

Use the effective body for forwarding on Responses and `openai-oauth` channel
attempts.

That keeps the upstream request body aligned with the request log entry and the
price multiplier logic.

### Request Logging

Add a boolean request-log field:

- `serviceTierOverridden`

Persist it in the request log table, include it in REST fetches, and include it
in SSE created/updated payloads.

This lets the WebUI show a factual "proxy override happened" indicator instead
of inferring it from channel config later.

## WebUI

### Channel Editor

In the channel modal, add a compact Codex-specific section for eligible
Responses channels:

- label: Codex service tier override
- options: Off, Force priority
- helper text: forcing priority rewrites missing/default Codex requests and may
  increase billed cost

### Request Log Table

Keep the existing thunder icon behavior for `serviceTier === 'priority'`.

Add a second icon immediately beside it when
`serviceTierOverridden === true`.

Tooltip text should explain that the proxy forced priority for this request.

The indicator should appear consistently in:

- stacked model display
- expanded model column
- existing reasoning/mapping tooltips where the fast-mode badge already appears

## Scope

In scope:

- Responses channel config field and persistence
- per-attempt Codex priority override for Responses and `openai-oauth`
- request-log persistence and SSE support for override evidence
- WebUI channel editor control
- WebUI log-table override icon
- targeted backend tests and frontend verification

Out of scope:

- extending the feature to `/v1/messages` bridging
- generic field-rewrite rules
- rewriting unknown future `service_tier` values
- changing upstream latency guarantees beyond opting into priority processing

## Risks

### Billing Surprise

Priority mode already maps to 2x local billing in `cc-bridge`, so the UI copy
must clearly warn operators that enabling the override can increase cost.

### Multi-Channel Consistency

If a failover chain mixes channels with and without the override, each attempt
must compute and store its own effective service tier so the final log reflects
the actual channel behavior.

### Live Log Timing

Pending logs may initially be created before the effective channel attempt is
known. The implementation should update the log with the effective tier and
override flag before or during the selected attempt so the live UI converges to
the correct state quickly.

## Validation

Backend:

- targeted unit tests for effective override decisions
- targeted request-builder tests for `openai-oauth`
- config persistence tests for the new channel field
- request-log round-trip tests for the new override flag

Frontend:

- `bun run type-check`
- `bun run build`
