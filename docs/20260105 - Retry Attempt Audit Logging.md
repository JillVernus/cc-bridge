# Implementation Plan: Retry Attempt Audit Logging

**Date**: 2026-01-05
**Priority**: High (Audit Requirement)
**Status**: Complete

## Problem Statement

When a request encounters a 429 error with `retry_wait` action (e.g., `429:RESOURCE_EXHAUSTED`), the system waits and retries. However, the intermediate 429 attempts are NOT logged to the database - only the final result (success or ultimate failure) is recorded.

**Current behavior:**
```
Request → 429:RESOURCE_EXHAUSTED → wait 30s → retry → 200 success
                    ↓
              NOT LOGGED                              ↓
                                               ONLY THIS IS LOGGED
```

**Required behavior (for audit):**
```
Request → 429:RESOURCE_EXHAUSTED → wait 30s → retry → 200 success
                    ↓                                      ↓
           LOG ENTRY 1:                              LOG ENTRY 2:
    "429:RESOURCE_EXHAUSTED >                     "200 success"
     retry_wait > 30s"
```

## Affected Files

| File | Changes Required |
|------|------------------|
| `backend-go/internal/handlers/proxy.go` | Multi-channel & single-channel retry_wait logging |
| `backend-go/internal/handlers/responses.go` | Responses API retry_wait logging (streaming & non-streaming) |
| `backend-go/internal/requestlog/types.go` | Add new status constant `StatusRetryWait` |

## Steps

### Backend

- [x] **Step 1**: Add `StatusRetryWait` constant in `types.go`
- [x] **Step 2**: Modify `proxy.go` - `tryChannelWithAllKeys()` to log retry_wait attempts
- [x] **Step 3**: Pass client context to `tryChannelWithAllKeys()`
- [x] **Step 4**: Update `handleMultiChannelProxy()` call site
- [x] **Step 5**: Modify `proxy.go` - `handleSingleChannelProxy()` for retry_wait logging
- [x] **Step 6**: Modify `responses.go` - Non-streaming handler for retry_wait logging
- [x] **Step 7**: Modify `responses.go` - Streaming handler for retry_wait logging

### Verification

- [x] **Step 8**: Build verification (Go build passes)
- [x] **Step 9**: Status updated to Complete

## Implementation Details

### Step 1: Add New Status Constant

**File:** `backend-go/internal/requestlog/types.go`

Add a new status constant for retry_wait attempts:

```go
const (
    StatusPending   = "pending"
    StatusCompleted = "completed"
    StatusError     = "error"
    StatusTimeout   = "timeout"
    StatusFailover  = "failover"
    StatusRetryWait = "retry_wait"  // NEW: Request hit 429, waiting to retry
)
```

### Step 2: Modify proxy.go - tryChannelWithAllKeys()

**File:** `backend-go/internal/handlers/proxy.go`
**Function:** `tryChannelWithAllKeys()` (around line 486)

**Current code (ActionRetrySameKey case):**
```go
case config.ActionRetrySameKey:
    // Wait and retry with same key
    log.Printf("⏳ 429 %s: 等待 %v 后重试同一密钥", decision.Reason, decision.Wait)
    // Capture the error in case this is the last attempt
    lastFailoverError = &struct {
        Status       int
        Body         []byte
        FailoverInfo string
    }{
        Status:       resp.StatusCode,
        Body:         respBodyBytes,
        FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds())),
    }
    select {
    case <-time.After(decision.Wait):
        pinnedKey = apiKey // Pin for next attempt
        continue
    case <-c.Request.Context().Done():
        return false, nil
    }
```

**New code:**
```go
case config.ActionRetrySameKey:
    // Wait and retry with same key
    log.Printf("⏳ 429 %s: 等待 %v 后重试同一密钥", decision.Reason, decision.Wait)

    failoverInfo := requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds()))

    // AUDIT: Log this retry_wait attempt before waiting
    if reqLogManager != nil && requestLogID != "" {
        completeTime := time.Now()
        retryWaitRecord := &requestlog.RequestLog{
            Status:        requestlog.StatusRetryWait,
            CompleteTime:  completeTime,
            DurationMs:    completeTime.Sub(startTime).Milliseconds(),
            Type:          upstream.ServiceType,
            ProviderName:  upstream.Name,
            HTTPStatus:    resp.StatusCode,
            ChannelID:     upstream.Index,
            ChannelName:   upstream.Name,
            Error:         fmt.Sprintf("429 %s - retrying after %v", decision.Reason, decision.Wait),
            UpstreamError: string(respBodyBytes),
            FailoverInfo:  failoverInfo,
        }
        if err := reqLogManager.Update(requestLogID, retryWaitRecord); err != nil {
            log.Printf("⚠️ Failed to update retry_wait log: %v", err)
        }

        // Create new pending log for the retry attempt
        newPendingLog := &requestlog.RequestLog{
            Status:      requestlog.StatusPending,
            InitialTime: time.Now().Add(decision.Wait), // Will start after wait
            Model:       claudeReq.Model,
            Stream:      claudeReq.Stream,
            Endpoint:    "/v1/messages",
            // Preserve client context from original request
            ClientID:    "", // Need to pass through from outer scope
            SessionID:   "", // Need to pass through from outer scope
            APIKeyID:    nil, // Need to pass through from outer scope
        }
        if err := reqLogManager.Add(newPendingLog); err != nil {
            log.Printf("⚠️ Failed to create retry pending log: %v", err)
        } else {
            requestLogID = newPendingLog.ID
            startTime = time.Now().Add(decision.Wait) // Reset for accurate duration
        }
    }

    // Capture for last-resort error reporting
    lastFailoverError = &struct {
        Status       int
        Body         []byte
        FailoverInfo string
    }{
        Status:       resp.StatusCode,
        Body:         respBodyBytes,
        FailoverInfo: failoverInfo,
    }

    select {
    case <-time.After(decision.Wait):
        pinnedKey = apiKey
        startTime = time.Now() // Reset startTime after wait completes
        continue
    case <-c.Request.Context().Done():
        return false, nil
    }
```

### Step 3: Pass Client Context to tryChannelWithAllKeys()

**File:** `backend-go/internal/handlers/proxy.go`

The function signature needs to include client context for proper log creation:

**Current signature:**
```go
func tryChannelWithAllKeys(
    c *gin.Context,
    envCfg *config.EnvConfig,
    cfgManager *config.ConfigManager,
    upstream *config.UpstreamConfig,
    bodyBytes []byte,
    claudeReq types.ClaudeRequest,
    startTime time.Time,
    reqLogManager *requestlog.Manager,
    requestLogID string,
    usageManager *quota.UsageManager,
    failoverTracker *config.FailoverTracker,
) (bool, *struct {...})
```

**New signature:**
```go
func tryChannelWithAllKeys(
    c *gin.Context,
    envCfg *config.EnvConfig,
    cfgManager *config.ConfigManager,
    upstream *config.UpstreamConfig,
    bodyBytes []byte,
    claudeReq types.ClaudeRequest,
    startTime time.Time,
    reqLogManager *requestlog.Manager,
    requestLogID string,
    usageManager *quota.UsageManager,
    failoverTracker *config.FailoverTracker,
    clientID string,      // NEW
    sessionID string,     // NEW
    apiKeyID *int64,      // NEW
) (bool, *struct {...}, string) // NEW: return updated requestLogID
```

### Step 4: Update handleMultiChannelProxy() Call Site

Update the call to `tryChannelWithAllKeys()` to pass client context and receive updated requestLogID.

### Step 5: Modify proxy.go - handleSingleChannelProxy()

Apply the same retry_wait logging pattern to the single-channel handler (around line 837).

### Step 6: Modify responses.go - Non-Streaming Handler

**File:** `backend-go/internal/handlers/responses.go`
**Location:** Around line 516 (ActionRetrySameKey case in non-streaming)

Apply the same pattern: log the retry_wait attempt before waiting, create new pending log for retry.

### Step 7: Modify responses.go - Streaming Handler

**File:** `backend-go/internal/handlers/responses.go`
**Location:** Around line 1078 (ActionRetrySameKey case in streaming)

Apply the same pattern.

## Data Model Considerations

### Log Entry Fields for retry_wait

| Field | Value |
|-------|-------|
| `status` | `"retry_wait"` |
| `httpStatus` | `429` |
| `failoverInfo` | `"429:RESOURCE_EXHAUSTED > retry_wait > 30s"` |
| `error` | `"429 RESOURCE_EXHAUSTED - retrying after 30s"` |
| `upstreamError` | Raw response body from upstream |
| `durationMs` | Time from request start to 429 received |

### Log Sequence Example

For a request that encounters 429 twice before succeeding:

| ID | Status | HTTPStatus | FailoverInfo | DurationMs |
|----|--------|------------|--------------|------------|
| `log-001` | `retry_wait` | 429 | `429:RESOURCE_EXHAUSTED > retry_wait > 30s` | 1500 |
| `log-002` | `retry_wait` | 429 | `429:RESOURCE_EXHAUSTED > retry_wait > 30s` | 1200 |
| `log-003` | `completed` | 200 | - | 2300 |

## Testing Strategy

### Manual Testing

1. Configure a channel with `429:RESOURCE_EXHAUSTED` → `retry_wait` (30s)
2. Trigger a request that will hit 429
3. Verify in request log table:
   - First entry: `status=retry_wait`, `httpStatus=429`, `failoverInfo` shows wait time
   - After wait + retry: Second entry with final result

### Edge Cases

1. **Client disconnect during wait**: Should log the retry_wait attempt, no retry log
2. **Multiple consecutive 429s**: Each should create a separate log entry
3. **429 then channel failover**: retry_wait log + failover log + next channel logs
4. **Streaming requests**: Same behavior as non-streaming

## Migration Notes

- No database schema changes required
- New `retry_wait` status value is backward compatible
- Frontend may need update to display `retry_wait` status distinctly (optional)

## Rollback Plan

If issues arise, revert the changes to the three affected files. Existing logs are not affected.

## Estimated Effort

| Task | Complexity |
|------|------------|
| Add StatusRetryWait constant | Trivial |
| Modify proxy.go tryChannelWithAllKeys | Medium |
| Modify proxy.go handleSingleChannelProxy | Medium |
| Modify responses.go (2 locations) | Medium |
| Update function signatures | Low |
| Testing | Medium |

**Total**: ~2-3 hours of implementation + testing
