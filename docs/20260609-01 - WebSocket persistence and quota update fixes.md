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

