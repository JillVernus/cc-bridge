# WebSocket Persistence and Quota Update Fixes

## Background

Multiple issues reported with the responses websocket feature and quota tracking:

1. **WebSocket persistence**: The `ResponsesWebSocketEnabled` flag doesn't persist after container restart
2. **Quota bar updates**: Usage quota bars only update on manual refresh, not after new request responses
3. **Recent calls display**: WebSocket requests sometimes show as failed incorrectly
4. **OAuth defaults**: All Codex OAuth channels should default to websocket enabled

## Root Causes

### 1. WebSocket Persistence
The `ResponsesWebSocketEnabled` field is stored in the database (`channels` table), but the migration logic in `migrateConfigResponsesWebSocket()` only enables it for OAuth channels during the migration phase. The flag is correctly persisted, but the issue is likely:
- Migration only runs once when `cfg.ResponsesWebSocket.Enabled` is true
- After migration completes, `cfg.ResponsesWebSocket.Enabled` is set to false (line 140)
- New OAuth channels created after migration don't get the default

### 2. Quota Bar Updates
Current implementation (`ChannelOrchestration.vue`):
- `fetchUsageQuotas()` only called manually via `refreshMetrics()` button (line 1406)
- No automatic refresh after request completion
- No websocket/event listener for quota changes

### 3. Recent Calls Display
The success/failure determination (line 1162-1168) relies on `call.success` from metrics, which is set in `channel_metrics.go`. For websocket requests:
- Success is recorded in `responses_websocket.go` via `RecordSuccess()`/`RecordFailure()`
- The log tracker completes via `completeFromPayload()` or `completeError()`
- Issue: timing/race condition between websocket completion and metrics update

### 4. OAuth Default
The `migrateConfigResponsesWebSocket()` function (config.go:126-166) only runs during migration when the old global flag exists. New channels created via API don't go through this migration path.

## Approach

### Fix 1: Ensure WebSocket Default for New OAuth Channels
Modify channel creation logic to set `ResponsesWebSocketEnabled = true` by default for `openai-oauth` channels.

### Fix 2: Auto-refresh Quota After Requests
Add event listener or polling mechanism to refresh usage quotas after request completion:
- Option A: Poll every N seconds when page is visible
- Option B: Add websocket event from backend when quota changes
- **Chosen**: Use frontend polling (simpler, no backend changes needed)

### Fix 3: Investigate Recent Calls WebSocket Timing
Review the flow from websocket completion → metrics update → frontend display to identify race conditions.

### Fix 4: Default OAuth Channels to WebSocket
Ensure all new OAuth channels get `ResponsesWebSocketEnabled = true` at creation time.

## Steps

- [x] **Step 1**: Fix OAuth channel websocket default at creation
  - Modified `backend-go/internal/config/config.go:addResponsesUpstream()`
  - Set `ResponsesWebSocketEnabled = true` for new `openai-oauth` channels in responses tab
  
- [x] **Step 2**: Add auto-refresh for usage quota bars
  - Added polling timer in `ChannelOrchestration.vue` (every 10 seconds)
  - Only polls when component is mounted and visible
  - Calls `fetchUsageQuotas()` periodically
  - Added cleanup in `onUnmounted()`
  
- [x] **Step 3**: Fix recent calls websocket display
  - Modified `responses_websocket.go` to record metrics when individual requests complete
  - Added scheduler reference to `responsesWebSocketLogTracker`
  - Call `RecordSuccess()`/`RecordFailure()` in `completeFromPayload()`/`completeError()`
  - Fixed issue where connection-level errors were incorrectly marking all requests as failed
  
- [ ] **Step 4**: Testing
  - Test: Create new OAuth channel → verify websocket enabled by default
  - Test: Restart container → verify websocket setting persists
  - Test: Make websocket requests → verify quota updates automatically
  - Test: Make websocket requests → verify recent calls shows correct status

### Manual Verification - 2026-06-09

Preflight result: **BLOCKED**. Step 4 remains unchecked because the local workspace has no running app and no usable OAuth Responses channel/credentials for browser/runtime verification.

| Preflight item | Result | Evidence | Notes |
| --- | --- | --- | --- |
| Runtime available | BLOCKED | Local listener check for ports 3000/5173 returned no backend/frontend listener | No local dev server, built binary, or container was already running for manual browser checks. |
| Browser available | PASS | `playwright --version` → `Version 1.58.0` | Browser automation entrypoint exists. |
| Usable OAuth channel/credentials available | BLOCKED | `.config/config.json` scan returned `[]` for Responses channels with `serviceType == "openai-oauth"` | No real/test OAuth Responses channel is configured. |

Manual check results:

Each manual check records `Result:`, `Evidence:`, and `Notes:` below so the runbook can be validated mechanically.

| Check | Result / Evidence / Notes |
| --- | --- |
| Create new OAuth Responses channel and verify WebSocket enabled by default | Result: BLOCKED<br>Evidence: Preflight lacks a running app and usable OAuth channel setup<br>Notes: Automated coverage added in `TestAddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists`. |
| Restart container/service and verify setting persists | Result: BLOCKED<br>Evidence: Preflight lacks a running app/container and usable OAuth channel setup<br>Notes: Automated reload persistence coverage added in `TestAddResponsesOpenAIOAuthDefaultsWebSocketEnabledAndPersists`. |
| Make WebSocket request and verify quota updates automatically | Result: BLOCKED<br>Evidence: Preflight lacks usable OAuth credentials and runtime<br>Notes: Source-level timer lifecycle guard added; runtime quota mutation still requires OAuth/browser evidence. |
| Make WebSocket request and verify recent calls shows correct status | Result: BLOCKED<br>Evidence: Preflight lacks usable OAuth credentials and runtime<br>Notes: Automated WebSocket recent-call success/fallback coverage exists, but browser-level manual evidence is still unavailable. |

## Implementation Notes

### Quota Polling Strategy
```typescript
let quotaRefreshTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  refreshMetrics()
  fetchOAuthQuotas()
  fetchUsageQuotas()
  
  // Auto-refresh quotas every 10 seconds
  quotaRefreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible') {
      fetchUsageQuotas()
    }
  }, 10000)
})

onUnmounted(() => {
  clearOAuthQuotaResetTimer()
  if (quotaRefreshTimer) {
    clearInterval(quotaRefreshTimer)
    quotaRefreshTimer = null
  }
})
```

### OAuth Channel Default
Location: `backend-go/internal/handlers/channel_handler.go` (or wherever channels are created)
```go
// When creating a new responses channel with serviceType "openai-oauth"
if channelType == "responses" && 
   strings.EqualFold(strings.TrimSpace(upstream.ServiceType), "openai-oauth") {
    upstream.ResponsesWebSocketEnabled = true
}
```

## Commits

- `e88b97b` - fix: websocket persistence, quota auto-refresh, and metrics recording
- `65dbb01` - fix: address review findings for websocket and quota features

## Review & Improvements

After initial implementation, a thorough review identified and fixed:

1. **Connection-level failure handling** - Added fallback to record failures when websocket connection fails before any requests are made
2. **Quota timer management** - Clear and restart timer on tab change to prevent memory leaks
3. **Request tracking** - Added `requestCount` and `hadAnyRequests()` to distinguish connection failures from request failures

Commit `65dbb01` contains the connection-level failure fallback and quota timer lifecycle cleanup.

See `docs/20260609-02 - Implementation review findings.md` for detailed analysis.

## Edge-Case Checklist - 2026-06-09

| Edge case | Result | Evidence | Residual risk |
| --- | --- | --- | --- |
| connection fails before first request | COVERED | `go test ./internal/handlers -run 'TestResponsesWebSocket.*(Fallback|Failure|Metrics|RecentCall|ResponseDone)' -count=1`; `TestResponsesWebSocketFallbackRecordsFailureAfterUpstreamConnectBeforeFirstRequest` covers post-upstream-connect proxy error before first `response.create` | Browser/manual OAuth path blocked by missing runtime and credentials. |
| connection fails after one completed request | COVERED | Same handler command; `TestResponsesWebSocketResponseDoneRestoresAsRecentCallSuccess` verifies completed request remains one recent-call success | Does not manually verify browser rendering without runtime/OAuth setup. |
| tab changes repeatedly while quota polling is active | COVERED_SOURCE_LEVEL | `bun test src/components/ChannelOrchestration.test.ts`; source guard verifies watcher calls `clearUsageQuotaRefreshTimer()` before assigning a new interval | Source-level guard only; it does not prove exact live interval cardinality at runtime. |
| component unmounts while polling is active | COVERED_SOURCE_LEVEL | `bun test src/components/ChannelOrchestration.test.ts`; source guard verifies `onUnmounted()` calls `clearUsageQuotaRefreshTimer()` | Source-level guard only; no Vue mount harness added. |
| quota API errors repeatedly | ACCEPTED_RESIDUAL_RISK | Existing `fetchUsageQuotas()` catches and logs errors; no new production behavior change in this follow-up | No automated retry/backoff change; P2 polling optimization deferred. |
| invisible/background tab | COVERED_SOURCE_LEVEL | `bun test src/components/ChannelOrchestration.test.ts`; source guard verifies polling checks `document.visibilityState === 'visible'` | Source-level guard only; browser runtime verification blocked by preflight. |
