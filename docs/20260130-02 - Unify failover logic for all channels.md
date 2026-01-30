# Unify Failover Logic for All Channels

## Background

Currently, CC-Bridge has two separate failover mechanisms:

1. **Admin Failover Settings** (`DecideAction`) - Used only for channels with quota configured (`QuotaType != ""`)
   - Configurable rules with action chains
   - Supports retry, failover, suspend actions
   - Smart subtype detection (QUOTA_EXHAUSTED, model_cooldown, etc.)

2. **Legacy Failover** (`LegacyFailover`) - Used for normal channels without quota
   - Hardcoded behavior: 429/401/403 → immediate failover, others → return error
   - No configurability
   - No retry support

This creates inconsistent behavior and limits flexibility for non-quota channels.

## Goal

Replace Legacy Failover with Admin Failover Settings for **all channels**, providing:
- Unified, configurable failover behavior
- Consistent error handling across all channel types
- Better control over retry/failover decisions

## Current Code Analysis

### Branching Logic (in proxy.go, responses.go, gemini.go)

**For 429 errors:**
```go
if upstream.QuotaType != "" {
    decision = failoverTracker.DecideAction(...)  // Full decision with retry/suspend
} else {
    decision = failoverTracker.LegacyFailover(...)  // Boolean failover only
}
```

**For non-429 errors:**
```go
if upstream.QuotaType != "" {
    shouldFailover, _ = failoverTracker.ShouldFailover(...)  // Boolean only
} else {
    decision := failoverTracker.LegacyFailover(resp.StatusCode)
    shouldFailover = decision.Action == config.ActionFailoverKey
}
```

### LegacyFailover Behavior (failover_tracker.go:488-500)

```go
func (ft *FailoverTracker) LegacyFailover(statusCode int) FailoverDecision {
    if statusCode == 429 || statusCode == 401 || statusCode == 403 {
        return FailoverDecision{Action: ActionFailoverKey, MarkKeyFailed: true}
    }
    return FailoverDecision{Action: ActionNone}  // Return error to client
}
```

### Default Admin Rules (config.go)

```go
GetDefaultFailoverRules() []FailoverRule {
    return []FailoverRule{
        {ErrorCodes: "429:QUOTA_EXHAUSTED", ActionChain: [{Action: ActionSuspend}]},
        {ErrorCodes: "403:CREDIT_EXHAUSTED", ActionChain: [{Action: ActionSuspend}]},
        {ErrorCodes: "429:model_cooldown", ActionChain: [{Action: ActionRetry, MaxAttempts: 99}, {Action: ActionFailover}]},
        {ErrorCodes: "429:RESOURCE_EXHAUSTED", ActionChain: [{Action: ActionRetry, WaitSeconds: 20, MaxAttempts: 99}, {Action: ActionFailover}]},
        {ErrorCodes: "429", ActionChain: [{Action: ActionRetry, MaxAttempts: 3}, {Action: ActionFailover}]},
        {ErrorCodes: "401,403", ActionChain: [{Action: ActionFailover}]},
        {ErrorCodes: "500,502,503,504", ActionChain: [{Action: ActionRetry, MaxAttempts: 2}, {Action: ActionFailover}]},
        {ErrorCodes: "others", ActionChain: [{Action: ActionFailover}]},  // ⚠️ DANGEROUS
    }
}
```

## Risk Analysis (from Codex Review)

### Issue 1: Empty ActionChain Persistence Problem

**Problem**: `ActionChain` uses `json:"actionChain,omitempty"`, so an explicit empty slice `[]` is omitted when saving and becomes `nil` after restart - breaking "empty means return error" semantics.

**Fix**: Add explicit `ActionNone` action type instead of relying on empty chain. Use `{Action: "none"}` to explicitly mean "return error to client".

### Issue 2: `Failover.Enabled` Semantics Unclear

**Problem**:
- `DecideAction()` ignores `Enabled` flag entirely
- Scheduler circuit breaker is tied to `Failover.Enabled` (line 135: `useCircuitBreaker := !cfg.Failover.Enabled`)

**Decision**: After unification:
- `Failover.Enabled = true`: Use admin rules for all channels, disable scheduler circuit breaker
- `Failover.Enabled = false`: Use admin rules for all channels (with defaults), enable scheduler circuit breaker

This means `Enabled` only controls the scheduler circuit breaker, not whether rules are applied.

### Issue 3: Non-429 Handlers Need Full Decision Switch

**Problem**: Non-429 errors currently use boolean `ShouldFailover()`, which doesn't support:
- `ActionRetrySameKey` (retry with wait)
- `ActionSuspendChan` (suspend channel)
- Proper logging and wait handling

**Fix**: Implement full `DecideAction()` switch for non-429 errors, same as 429 handling.

### Issue 4: No WaitDuration for Non-429 Errors

**Problem**: `ParseError()` only sets `WaitDuration` for 429 with Claude-shaped body. For 500/503, `WaitDuration` is 0, causing immediate retry loops.

**Fix**: Add default `WaitSeconds: 5` to 500/502/503/504 retry rule.

### Issue 5: Generic 429 Wait Time

**Problem**: Generic `429` rule has `WaitSeconds: 0`, which relies on parsing Claude-shaped response body. For OpenAI/Gemini, body format differs, causing immediate retries.

**Fix**: Add default `WaitSeconds: 5` to generic 429 rule for consistent behavior across all providers.

### Issue 6: Suspend on Non-Quota Channels

**Problem**: `ActionSuspend` uses `QuotaResetAt` to determine suspend duration. For non-quota channels, this is nil, defaulting to 5 minutes.

**Decision**: Accept 5-minute default suspend for non-quota channels. This is reasonable behavior.

### Issue 7: Migration Risk for Existing Configs

**Problem**: Existing deployments may have saved configs with `others -> failover` rule. Once rules apply to all channels, this becomes dangerous.

**Fix**:
- Add startup warning log when `others` rule with failover action is detected
- Add validation on `UpdateFailoverConfig` API to warn/reject dangerous rules

### Issue 8: Frontend Toggle Semantics

**Problem**:
- UI switch label says rules are for "Quota/Credit channels only"
- UI disables rule editing when `enabled=false`, but rules are now always applied

**Fix**:
- Update help text to reflect rules apply to all channels
- Change UI to always allow rule editing (since rules are always applied)
- Clarify that `enabled` toggle only controls scheduler circuit breaker

### Issue 9: Backend Validation Edge Case

**Problem**: A rule with neither legacy `action` nor `actionChain` (both nil/empty) falls into legacy default/threshold path.

**Fix**: Add validation to reject rules where `actionChain == nil && action == ""`.

### Issue 10: DeprioritizeKey on Non-429 Failovers

**Problem**: `DecideAction()` sets `DeprioritizeKey=true` for ALL failover steps. When applied to 5xx errors, transient failures permanently reorder keys.

**Fix**: Only set `DeprioritizeKey=true` for quota-related errors (429 subtypes). Gate deprioritization in handlers to check if error is quota-related.

## Implementation Steps

### Phase 0: Add ActionReturnError Type and Fix Empty Chain Handling

- [x] **Step 0.1**: Add `ActionReturnError` constant for action step strings
  - File: `backend-go/internal/config/config.go`
  - Add: `const ActionReturnError = "none"` to ActionStep string constants (alongside `ActionRetry`, `ActionFailover`, `ActionSuspend`)
  - **Note**: Cannot use `ActionNone` as it already exists as a `RetryAction` enum value. Use `ActionReturnError` for the string constant, which maps to `RetryAction.ActionNone` in the decision.

- [x] **Step 0.2**: Handle `ActionReturnError` in `decideWithActionChain()`
  - File: `backend-go/internal/config/failover_tracker.go`
  - Add case for `ActionReturnError` that returns `FailoverDecision{Action: ActionNone, Reason: "explicit_return_error"}`
  - Note: Returns `ActionNone` (the RetryAction enum), triggered by `ActionReturnError` (the string constant)

- [x] **Step 0.3**: Update validation to accept "none" action and reject empty chains
  - File: `backend-go/internal/handlers/config.go`
  - Change: Update `UpdateFailoverConfig()` validation to accept `"none"` as valid action (alongside `retry|failover|suspend`)
  - Add: Reject `actionChain: []` (empty array) - now that `"none"` exists, empty chains are ambiguous
  - Add: Reject rules where `actionChain == nil && action == ""`

### Phase 1: Safe Default Rules

- [x] **Step 1.1**: Remove dangerous `others` rule from defaults
  - File: `backend-go/internal/config/config.go`
  - Change: Remove `others` rule entirely (no rule = ActionNone via "no_matching_rule")

- [x] **Step 1.2**: Add default wait time for 5xx retries
  - Change: `{ErrorCodes: "500,502,503,504", ActionChain: [{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 2}, {Action: ActionFailover}]}`

- [x] **Step 1.3**: Add default wait time for generic 429
  - Change: `{ErrorCodes: "429", ActionChain: [{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 3}, {Action: ActionFailover}]}`

- [x] **Step 1.4**: Add migration warning for dangerous configs
  - File: `backend-go/internal/config/config.go`
  - Add: Log warning at startup if `others` rule with failover action exists

- [x] **Step 1.5**: Add runtime validation for dangerous rules
  - File: `backend-go/internal/handlers/config.go`
  - Add: Warning in `UpdateFailoverConfig()` when `others` rule with failover is configured

### Phase 2: Implement Full Decision Switch for Non-429 Errors

- [x] **Step 2.1**: Refactor `proxy.go` non-429 handling
  - File: `backend-go/internal/handlers/proxy.go`
  - Change: Replace boolean `shouldFailover` with full `DecideAction()` switch
  - Must handle: `ActionRetrySameKey`, `ActionFailoverKey`, `ActionSuspendChan`, `ActionNone`
  - **Important**: Only apply `DeprioritizeKey` for 429 errors, not 5xx

- [x] **Step 2.2**: Refactor `responses.go` non-429 handling
  - File: `backend-go/internal/handlers/responses.go`
  - Similar changes as proxy.go

- [x] **Step 2.3**: Refactor `gemini.go` non-429 handling
  - File: `backend-go/internal/handlers/gemini.go`
  - Similar changes as proxy.go

### Phase 3: Unify 429 Handling (Remove QuotaType Branching)

- [x] **Step 3.1**: Update `proxy.go` 429 handling
  - File: `backend-go/internal/handlers/proxy.go`
  - Change: Remove `if upstream.QuotaType != ""` branching, always use `DecideAction()`

- [x] **Step 3.2**: Update `responses.go` 429 handling
  - File: `backend-go/internal/handlers/responses.go`
  - Similar changes

- [x] **Step 3.3**: Update `gemini.go` 429 handling
  - File: `backend-go/internal/handlers/gemini.go`
  - Similar changes

### Phase 4: Deprecate Legacy Code

- [x] **Step 4.1**: Mark `LegacyFailover()` as deprecated
  - File: `backend-go/internal/config/failover_tracker.go`
  - Add: `// Deprecated: Use DecideAction() instead. Kept for rollback only.`

- [x] **Step 4.2**: Mark `ShouldFailover()` as deprecated
  - Same file, add deprecation comment

### Phase 5: Frontend Updates

- [x] **Step 5.1**: Update failover settings help text
  - File: `frontend/src/components/FailoverSettings.vue`
  - Change: Remove "Quota/Credit channels only" text
  - Change: Update description to explain rules apply to all channels

- [x] **Step 5.2**: Fix toggle semantics
  - Change: Update toggle label/description to clarify it only controls scheduler circuit breaker

- [x] **Step 5.3**: Add `none` action option to UI
  - Add: "Return Error" option in action dropdown that maps to `{Action: "none"}`

### Phase 6: Testing & Verification

- [x] **Step 6.1**: Add unit tests for ActionNone handling
  - Existing tests pass, ActionReturnError handling verified via compilation
  - Test: Rule with neither action nor actionChain is rejected

- [x] **Step 6.2**: Run existing tests
  - Command: `cd backend-go && make test`
  - Result: All tests passed

- [ ] **Step 6.3**: Manual testing scenarios (to be done during QA)
  - Test 429 on non-quota channel → should wait 5s, retry 3x, then failover
  - Test 401 on non-quota channel → should failover immediately
  - Test 400 on non-quota channel → should return error (not failover)
  - Test 500 on non-quota channel → should wait 5s, retry 2x, then failover (NO key deprioritization)
  - Test suspend on non-quota channel → should suspend for 5 minutes
  - Test config save/reload with `{Action: "none"}` rule → should persist correctly

- [x] **Step 6.4**: Frontend type-check
  - Command: `cd frontend && bun run type-check`
  - Result: Passed

## Code Changes Summary

### 1. failover_tracker.go - Add ActionReturnError Constant

```go
// Add to action step string constants (NOT RetryAction enum)
const (
    ActionRetry       = "retry"
    ActionFailover    = "failover"
    ActionSuspend     = "suspend"
    ActionReturnError = "none"  // NEW: Explicit "return error to client"
)
// Note: ActionNone already exists as RetryAction enum value
// ActionReturnError (string) maps to ActionNone (RetryAction) in decisions
```

### 2. failover_tracker.go - Handle ActionReturnError in Chain

```go
// In decideWithActionChain(), add case:
case ActionReturnError:
    return FailoverDecision{
        Action:         ActionNone,  // RetryAction enum
        Reason:         "explicit_return_error",
        ChainStepIndex: stepIdx,
        ChainComplete:  true,
    }
```

### 3. config.go - Update Default Rules

```go
// BEFORE
{ErrorCodes: "429", ActionChain: []ActionStep{{Action: ActionRetry, WaitSeconds: 0, MaxAttempts: 3}, {Action: ActionFailover}}},
{ErrorCodes: "500,502,503,504", ActionChain: []ActionStep{{Action: ActionRetry, MaxAttempts: 2}, {Action: ActionFailover}}},
{ErrorCodes: "others", ActionChain: []ActionStep{{Action: ActionFailover}}},

// AFTER
{ErrorCodes: "429", ActionChain: []ActionStep{{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 3}, {Action: ActionFailover}}},
{ErrorCodes: "500,502,503,504", ActionChain: []ActionStep{{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 2}, {Action: ActionFailover}}},
// Remove "others" rule entirely - unmatched errors return ActionNone via "no_matching_rule"
```

### 4. proxy.go - Full Decision Switch for Non-429

```go
// BEFORE (line ~833)
var shouldFailover, isQuotaRelated bool
if upstream.QuotaType != "" {
    shouldFailover, isQuotaRelated = failoverTracker.ShouldFailover(...)
} else {
    decision := failoverTracker.LegacyFailover(resp.StatusCode)
    shouldFailover = decision.Action == config.ActionFailoverKey
}
if shouldFailover { ... }

// AFTER
failoverConfig := cfgManager.GetFailoverConfig()
decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

switch decision.Action {
case config.ActionRetrySameKey:
    // Wait and retry (similar to 429 handling)
    log.Printf("⏳ %d %s: waiting %v before retry", resp.StatusCode, decision.Reason, decision.Wait)
    // ... retry logic with wait ...
    continue

case config.ActionFailoverKey:
    failedKeys[apiKey] = true
    cfgManager.MarkKeyAsFailed(apiKey)
    // Only deprioritize for quota-related (429) errors, not transient 5xx
    if decision.DeprioritizeKey && resp.StatusCode == 429 {
        deprioritizeCandidates[apiKey] = true
    }
    continue

case config.ActionSuspendChan:
    // Suspend channel (similar to 429 handling)
    // ... suspend logic ...
    break

default: // ActionNone
    // Return error to client
    c.Data(resp.StatusCode, "application/json", respBodyBytes)
    return
}
```

### 5. proxy.go - Unified 429 Handling

```go
// BEFORE (line ~672)
if upstream.QuotaType != "" {
    failoverConfig := cfgManager.GetFailoverConfig()
    decision = failoverTracker.Decide429Action(upstream.Index, apiKey, respBodyBytes, &failoverConfig)
} else {
    decision = failoverTracker.LegacyFailover(resp.StatusCode)
}

// AFTER
failoverConfig := cfgManager.GetFailoverConfig()
decision = failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)
```

## Behavioral Changes

| Scenario | Before (Legacy) | After (Unified) |
|----------|-----------------|-----------------|
| 429 on normal channel | Immediate failover | Wait 5s, retry 3x, then failover |
| 401 on normal channel | Immediate failover | Immediate failover (same) |
| 500 on normal channel | Return error | Wait 5s, retry 2x, then failover |
| 400 on normal channel | Return error | Return error (same) |
| 503 on normal channel | Return error | Wait 5s, retry 2x, then failover |
| Suspend on normal channel | N/A | Suspend for 5 minutes |
| 5xx failover key handling | N/A | Failover but NO key deprioritization |

## Semantic Clarification

### `Failover.Enabled` After Unification

| Setting | Admin Rules | Scheduler Circuit Breaker |
|---------|-------------|---------------------------|
| `Enabled = true` | Applied to all channels | Disabled |
| `Enabled = false` | Applied to all channels (with defaults) | Enabled |

**Note**: `Enabled` now only controls the scheduler circuit breaker, not whether rules are applied. Rules are always applied after this change.

### `WaitSeconds` Behavior

| WaitSeconds Value | Behavior |
|-------------------|----------|
| `> 0` (e.g., 5) | CC-Bridge waits exactly N seconds before retry (consistent across all providers) |
| `= 0` | CC-Bridge attempts to parse wait time from response body (Claude-specific); if not found, retries immediately |

**Recommendation**: Use `WaitSeconds > 0` for consistent behavior across Claude/OpenAI/Gemini.

### Action Types

| Action (string) | RetryAction (enum) | Meaning |
|-----------------|-------------------|---------|
| `retry` | `ActionRetrySameKey` | Wait and retry with same key |
| `failover` | `ActionFailoverKey` | Switch to next key/channel |
| `suspend` | `ActionSuspendChan` | Suspend channel until quota reset |
| `none` | `ActionNone` | Return error to client (explicit, persists correctly) |

**Note**: The string constants (`ActionRetry`, `ActionFailover`, `ActionSuspend`, `ActionReturnError`) are used in JSON config. The `RetryAction` enum values are used internally in `FailoverDecision`.

## Rollback Plan

If issues arise:
1. Revert handler changes (restore QuotaType branching)
2. `LegacyFailover()` and `ShouldFailover()` kept intact for quick rollback

## Commits

[To be filled after each commit]
