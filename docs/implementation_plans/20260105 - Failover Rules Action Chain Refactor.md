# Failover Rules Action Chain Refactor

**Created**: 2026-01-05
**Status**: Complete
**Scope**: Backend + Frontend

## Overview

Refactor the Failover Rules system to support action chains (IF-THEN-ELSE style) instead of single actions. This enables:
- Configurable retry attempts (not just 1)
- Chained actions (retry → failover → suspend)
- Unified action model (merge `threshold` and `retry_wait` into `retry`)

## Current Limitations

1. `retry_wait` action only retries **once** (hardcoded in handlers)
2. `suspend_channel` and `failover_immediate` don't support threshold
3. No "next action" concept - each action is terminal
4. UI shows different fields for different action types (inconsistent UX)

## New Data Model

### Backend (Go)

```go
// ActionStep represents a single step in the action chain
type ActionStep struct {
    Action      string `json:"action"`                // "retry", "failover", "suspend"
    WaitSeconds int    `json:"waitSeconds,omitempty"` // Wait before retry (0 = auto-detect from response)
    MaxAttempts int    `json:"maxAttempts,omitempty"` // Max retry attempts (for retry action)
}

// FailoverRule defines how to handle specific error patterns
type FailoverRule struct {
    ErrorCodes  string       `json:"errorCodes"`  // Error pattern: "401,403" or "429:QUOTA_EXHAUSTED"
    ActionChain []ActionStep `json:"actionChain"` // Sequential actions to execute
}
```

### Frontend (TypeScript)

```typescript
interface ActionStep {
    action: 'retry' | 'failover' | 'suspend'
    waitSeconds?: number
    maxAttempts?: number
}

interface FailoverRule {
    errorCodes: string
    actionChain: ActionStep[]
}
```

## Migration Strategy

Old format detection: if rule has `action` field (string) instead of `actionChain` (array)

| Old Format | New Format |
|------------|------------|
| `{action: "suspend_channel"}` | `{actionChain: [{action: "suspend"}]}` |
| `{action: "failover_immediate"}` | `{actionChain: [{action: "failover"}]}` |
| `{action: "failover_threshold", threshold: 3}` | `{actionChain: [{action: "retry", maxAttempts: 3}, {action: "failover"}]}` |
| `{action: "retry_wait", waitSeconds: 30}` | `{actionChain: [{action: "retry", waitSeconds: 30, maxAttempts: 99}, {action: "failover"}]}` |

## New Default Rules

```go
func GetDefaultFailoverRules() []FailoverRule {
    return []FailoverRule{
        // Quota exhausted - suspend channel
        {ErrorCodes: "429:QUOTA_EXHAUSTED", ActionChain: []ActionStep{{Action: "suspend"}}},
        {ErrorCodes: "403:CREDIT_EXHAUSTED", ActionChain: []ActionStep{{Action: "suspend"}}},

        // Model cooldown - retry with auto-detect wait, then failover
        {ErrorCodes: "429:model_cooldown", ActionChain: []ActionStep{
            {Action: "retry", WaitSeconds: 0, MaxAttempts: 99},
            {Action: "failover"},
        }},

        // Resource exhausted - retry 20s wait, then failover
        {ErrorCodes: "429:RESOURCE_EXHAUSTED", ActionChain: []ActionStep{
            {Action: "retry", WaitSeconds: 20, MaxAttempts: 99},
            {Action: "failover"},
        }},

        // Generic 429 - retry 3 times immediately, then failover
        {ErrorCodes: "429", ActionChain: []ActionStep{
            {Action: "retry", WaitSeconds: 0, MaxAttempts: 3},
            {Action: "failover"},
        }},

        // Auth errors - immediate failover
        {ErrorCodes: "401,403", ActionChain: []ActionStep{{Action: "failover"}}},

        // Server errors - retry 2 times, then failover
        {ErrorCodes: "500,502,503,504", ActionChain: []ActionStep{
            {Action: "retry", WaitSeconds: 0, MaxAttempts: 2},
            {Action: "failover"},
        }},

        // Others - immediate failover
        {ErrorCodes: "others", ActionChain: []ActionStep{{Action: "failover"}}},
    }
}
```

## UI Design

Expandable row design with action chain visualization:

```
┌───────────────────────────────────────────────────────────────────────┐
│ Failover Rules                                               [+ Add]  │
├───────────────────────────────────────────────────────────────────────┤
│ ▼ 429:QUOTA_EXHAUSTED                                         [Delete]│
│   └─ suspend                                                          │
│                                                                       │
│ ▼ 429:model_cooldown                                          [Delete]│
│   ├─ retry (wait: auto, max: 99)                                      │
│   └─ failover                                                         │
│                                                                       │
│ ▶ 401,403  →  failover                            (collapsed) [Delete]│
└───────────────────────────────────────────────────────────────────────┘

Edit Modal:
┌─────────────────────────────────────────────────────────────┐
│ Edit Rule                                          [Delete] │
├─────────────────────────────────────────────────────────────┤
│ Error Pattern: [  401, 403  ]                               │
│                                                             │
│ Action Chain:                                               │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Step 1: [Retry ▼]  Wait: [30]s  Max: [3] attempts  [×]  │ │
│ │    ↓                                                    │ │
│ │ Step 2: [Failover ▼]                               [×]  │ │
│ │                                            [+ Add Step] │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│                                    [Cancel]  [Save]         │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Steps

### Backend

- [x] **Step 1**: Update data model in `config.go`
  - Add `ActionStep` struct
  - Update `FailoverRule` struct with `ActionChain`
  - Keep old fields for migration detection
  - Update `GetDefaultFailoverRules()`
  - Add `MigrateRule()` and `MigrateRules()` helper functions

- [x] **Step 2**: Add migration logic in `config.go`
  - Detect old format on load
  - Convert to new format
  - Save migrated config

- [x] **Step 3**: Update `failover_tracker.go`
  - Update `FailoverDecision` to include chain step info (ChainStepIndex, ChainComplete, MaxAttempts)
  - Add `decideWithActionChain()` function for chain-aware decisions
  - Keep legacy action handling for backward compatibility

- [x] **Step 4**: Update handler retry loops
  - `proxy.go`: Removed `retryWaitUsed` tracking in both handlers
  - `responses.go`: Removed `retryWaitUsed` tracking in both handlers
  - Tracker now handles all attempt counting internally

- [x] **Step 5**: Update API handlers in `handlers/config.go`
  - Validate new rule format
  - Ensure backward compatibility in responses

### Frontend

- [x] **Step 6**: Update TypeScript types in `api.ts`
  - Add `ActionStep` interface
  - Update `FailoverRule` interface
  - Add `FailoverActionType` and `LegacyFailoverAction` types

- [x] **Step 7**: Update `FailoverSettings.vue` component
  - Expandable row design with collapsible rules
  - Action chain summary in collapsed view
  - Action chain editor in expanded view
  - Add/remove steps (max 5)
  - Client-side migration from legacy format
  - Validation for action chain

- [x] **Step 8**: Update translations
  - `locales/en.ts`: Add new keys for action chain UI
  - `locales/zh-CN.ts`: Add new keys for action chain UI

### Testing & Validation

- [x] **Step 9**: Build verification
  - TypeScript type-check: PASS
  - Frontend build: PASS
  - Backend Go build: PASS

- [x] **Step 10**: Codex code review
  - Part 1 (Backend Data Model): Added missing `default` case in `MigrateRule()` switch
  - Part 2 (Failover Tracker): No changes needed - implementation correct
  - Part 3 (Handler Retry Loops): No changes - architectural design intentional
  - Part 4 (Frontend): Fixed `getActionChainSummary`, `migrateRule`, and `removeRule` edge cases

## Files to Modify

| File | Changes |
|------|---------|
| `backend-go/internal/config/config.go` | Data model, defaults, migration |
| `backend-go/internal/config/failover_tracker.go` | Decision logic |
| `backend-go/internal/handlers/config.go` | API validation |
| `backend-go/internal/handlers/proxy.go` | Retry loop (2 places) |
| `backend-go/internal/handlers/responses.go` | Retry loop (2 places) |
| `frontend/src/services/api.ts` | TypeScript types |
| `frontend/src/components/FailoverSettings.vue` | UI component |
| `frontend/src/locales/en.ts` | English translations |
| `frontend/src/locales/zh-CN.ts` | Chinese translations |

## Commits

| Step | Commit Hash | Description |
|------|-------------|-------------|
| 1-10 | d167a5a | feat: Failover Rules Action Chain refactor v1.3.140 |

## Notes

- Action chain max length: 5 steps (to prevent infinite loops)
- `retry` action with `maxAttempts: 99` effectively means "retry indefinitely"
- `waitSeconds: 0` means auto-detect from API response (for 429 errors)
- Chain execution stops at first `suspend` or when all keys exhausted after `failover`
