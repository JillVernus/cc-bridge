# Gemini Incoming Parity Fixes (Permissions, Quota, Charts, Failover)

**Date:** 2026-01-21  
**Status:** In Progress

## Background

We added a Gemini-native incoming passthrough endpoint (Gemini CLI compatible) and a dedicated Gemini channel list. Several parity gaps remained vs. the existing Messages/Responses flows:

- API key permission model doesn’t support Gemini endpoints/channels cleanly.
- Usage quota tracking/storage is Messages/Responses-only.
- Charts can’t filter/show Gemini-only data.
- Failover rules (retry-wait/suspend) aren’t applied for Gemini.
- Minor infra gaps (CORS allow-headers, rate-limiter cleanup).
- UI/Logs parity gaps (icons, modelVersion tooltip, pricing) for Gemini-specific logs/channels.
- Model aliases + upstream model fetching parity for Gemini channel edit form.

## Goals

- Bring Gemini incoming (`/v1/models/*`, `/v1beta/models/*`, `/v1/gemini/models/*`) to feature parity with existing endpoints:
  - [x] Correct API key endpoint/channel restrictions
  - [x] Quota tracking + reset endpoints for Gemini channels
  - [x] Charts can filter Gemini endpoint
  - [x] Failover rules action-chain works for Gemini (retry-wait/suspend)
  - [x] CORS + rate-limit cleanup parity
  - [x] UI/Logs parity (icons, modelVersion tooltip, pricing)

## Non-Goals

- Reworking Messages/Responses failover engine beyond Gemini parity.
- Rewriting historical charts storage/query logic.

## Implementation Plan

### Phase 1: Small correctness fixes
- [x] **CORS**: allow `x-goog-api-key` in `Access-Control-Allow-Headers`.
- [x] **Per-channel rate limiter cleanup**: include `gemini:<index>` when deleting a Gemini channel.

### Phase 2: API key permissions for Gemini
- [x] **Backend schema/types**
  - Add `allowedChannelsGemini` to API key model and persistence.
  - Allow endpoint selector value `gemini` in `allowedEndpoints`.
- [x] **DB migration**
  - Add `allowed_channels_gemini` column to `api_keys`.
- [x] **Backend enforcement**
  - Gemini handler must validate endpoint permission (`gemini`) and apply Gemini channel allowlist.
- [x] **Frontend UI**
  - API Key permissions UI: add Gemini endpoint option + Gemini channel allowlist selector.

### Phase 2.5: UI/logs/model parity for Gemini
- [x] **Channel icons**
  - Gemini service type should render Gemini icon (not Codex fallback).
- [x] **Channel edit form spacing**
  - Fix overlapping labels for “Channel Name” and “Service Type” under the tab row.
- [x] **Log table icons**
  - Gemini requests should use `frontend/src/assets/gemini.svg` (via `custom:gemini`).
- [x] **Token usage**
  - Count Gemini output tokens as `candidatesTokenCount + thoughtsTokenCount`.
- [x] **Response model tooltip**
  - Capture Gemini `modelVersion` and surface as response model mapping tooltip (request → response).
- [x] **Pricing**
  - Calculate Gemini request price using pricing config (prefer response model if priced).
- [x] **Fetch upstream models**
  - Ensure “Fetch Models” in Gemini channel edit form uses the channel base URL correctly.

### Phase 3: Gemini usage quota tracking
- [x] **Quota storage**
  - Extend quota usage file structure to include `gemini`.
- [x] **Quota manager**
  - Add `Get/Increment/Reset` for Gemini channels.
  - Ensure auto-reset supports Gemini channels.
  - Rolling reset-at update support for Gemini.
- [x] **Admin API endpoints**
  - Add `/api/gemini/channels/usage`, `/api/gemini/channels/:id/usage`, `/api/gemini/channels/:id/usage/reset`.
  - Reset should also clear `channel_suspensions` for `(channel_type=gemini)`.
- [x] **Gemini handler integration**
  - Successful Gemini requests should increment quota usage for Gemini channels.

### Phase 4: Charts & frontend wiring
- [x] **Global charts**
  - Add “Gemini” endpoint filter (`endpoint=/v1/gemini`).
- [x] **Channel charts**
  - Channel stats chart should support `gemini` endpoint filtering (not only messages/responses).
- [x] **Channel orchestration**
  - Gemini tab should call Gemini usage quota APIs (not Responses).

### Phase 5: Gemini failover rules parity
- [x] Apply `FailoverTracker` handling for Gemini (aligned with Responses behavior):
  - 429: `DecideAction` supports retry-wait + suspend.
  - Non-429: existing threshold/legacy behavior.
  - Suspend should use `channel_type=gemini` so scheduler skips suspended channels.
  - Audit `retry_wait` log entries for Gemini too.

### Phase 6: Quality + release
- [x] Run formatters (`gofmt`, frontend formatting if needed).
- [x] Run tests (`backend-go` unit tests).
- [x] Bump `VERSION`.
- [ ] Commit changes.

## Progress Log

- 2026-01-21: Plan created.
- 2026-01-21: Phase 1 completed (CORS + rate-limit cleanup).
- 2026-01-21: Phase 2 completed (API key Gemini permissions).
- 2026-01-21: Phase 2.5 completed (UI/logs/model parity incl. pricing + upstream model fetching).
- 2026-01-21: Phase 3 completed (Gemini quota tracking + reset APIs + handler integration).
- 2026-01-21: Phase 4 completed (Charts + Gemini quota UI wiring).
- 2026-01-21: Phase 5 completed (Failover rules parity for Gemini).
