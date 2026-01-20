# Composite Channel Failover Chain Enhancement

## Background

The composite channel feature (v1.3.149) introduced model-level routing, allowing a virtual channel to route different models (haiku, sonnet, opus) to different real channels. However, the current implementation has limitations:

1. **External Failover**: When a target channel fails, the system fails over to OTHER channels outside the composite, bypassing the composite routing logic entirely
2. **ServiceType Restriction**: Only `claude` type channels can be selected as targets; other Claude-compatible types (`openai_chat`, `openai`, `gemini`) are excluded
3. **No Per-Model Failover**: No way to define ordered failover chains per model pattern
4. **Wildcard Dependency**: Relies on wildcard `*` pattern for fallback, but all 3 Claude models should be explicitly mapped

**Goal**: Implement per-model failover chains with sticky composite behavior (no external failover).

## Current vs New Behavior

### Current Flow
```
Request (opus) → Channel-A
                    ↓ fails
                → Try other composite mappings for "opus"
                    ↓ all fail
                → Try external channels (scheduler fallback)
```

### New Flow
```
Request (opus) → Channel-A (primary)
                    ↓ fails (obey A's failover rules)
                → Channel-B (failover 1)
                    ↓ fails (obey B's failover rules)
                → Channel-D (failover 2)
                    ↓ fails
                → Return error to client (NO external failover)
```

## Data Structure Changes

### Backend (`config.go`)

```go
// CompositeMapping defines a model-to-channel mapping for composite channels
type CompositeMapping struct {
    Pattern         string   `json:"pattern"`                  // "haiku", "sonnet", "opus" (mandatory)
    TargetChannelID string   `json:"targetChannelId"`          // Primary target channel ID
    FailoverChain   []string `json:"failoverChain"`            // Ordered failover channel IDs (min 1 required)
    TargetModel     string   `json:"targetModel,omitempty"`    // Optional: override model name
    // Deprecated: use TargetChannelID instead
    TargetChannel int `json:"targetChannel,omitempty"`
}
```

### Frontend (`api.ts`)

```typescript
interface CompositeMapping {
  pattern: string           // "haiku", "sonnet", "opus"
  targetChannelId: string   // Primary target
  failoverChain: string[]   // Ordered failover list (min 1)
  targetModel?: string      // Optional model override
}
```

## Validation Rules

| Rule | Description |
|------|-------------|
| 3 mandatory patterns | `haiku`, `sonnet`, `opus` all required |
| Min 1 failover | Each pattern needs ≥1 channel in `failoverChain` |
| Compatible serviceTypes | `claude`, `openai_chat`, `openai`, `gemini`, `openaiold` allowed as targets |
| No status filter | Disabled/suspended channels can be selected and used |
| No wildcard | Remove `*` pattern option (all models explicitly mapped) |
| No self-reference | Target channels cannot be composite type |

## Example Configuration

```json
{
  "name": "Premium Composite",
  "serviceType": "composite",
  "status": "active",
  "compositeMappings": [
    {
      "pattern": "haiku",
      "targetChannelId": "ch-001",
      "failoverChain": ["ch-002", "ch-003"]
    },
    {
      "pattern": "sonnet",
      "targetChannelId": "ch-002",
      "failoverChain": ["ch-004", "ch-005"]
    },
    {
      "pattern": "opus",
      "targetChannelId": "ch-003",
      "failoverChain": ["ch-002", "ch-004"]
    }
  ]
}
```

## UI Design

```
┌─────────────────────────────────────────────────────────────────┐
│ Model Routing                                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│ haiku   [Channel A ▼]  →  [Channel B ▼]  →  [Channel C ▼]  [+] │
│                           └── failover chain ──┘                │
│                                                                  │
│ sonnet  [Channel B ▼]  →  [Channel D ▼]  →  [        ▼]  [+]   │
│                                                                  │
│ opus    [Channel C ▼]  →  [Channel B ▼]  →  [Channel D ▼]  [+] │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Steps

- [x] Step 1: Update `CompositeMapping` struct in `backend-go/internal/config/config.go`
  - Add `FailoverChain []string` field
  - Keep backward compatibility with existing mappings

- [x] Step 2: Update `ValidateCompositeChannel` in `config.go`
  - Require all 3 patterns: haiku, sonnet, opus
  - Require min 1 failover per pattern
  - Allow all Claude-compatible serviceTypes
  - Remove wildcard validation (no longer allowed)

- [x] Step 3: Update `isTargetChannelAvailable` in `channel_scheduler.go`
  - Add parameter to skip status check for composite targets
  - Allow disabled/suspended channels when called from composite resolution

- [x] Step 4: Update `ResolveCompositeMapping` in `config.go`
  - Return full chain (primary + failovers) for iteration
  - Remove wildcard fallback logic

- [x] Step 5: Update `tryResolveCompositeWithFallback` in `channel_scheduler.go`
  - Iterate through failover chain in order
  - Each target obeys its own channel failover rules
  - Return error when chain exhausted (no external fallback)

- [x] Step 6: Update `proxy.go` handlers
  - Detect when composite failover chain is exhausted
  - Return error to client instead of trying external channels
  - Update both Messages and Responses handlers

- [x] Step 7: Update frontend `api.ts`
  - Add `failoverChain: string[]` to `CompositeMapping` type

- [x] Step 8: Update `CompositeChannelEditor.vue`
  - Change filter: include all Claude-compatible serviceTypes
  - Remove wildcard option
  - Add failover chain UI for each pattern
  - Validate: 3 patterns required, min 1 failover each

- [x] Step 9: Update locales (`en.ts`, `zh-CN.ts`)
  - Add new labels for failover chain UI
  - Update validation messages

- [x] Step 10: Test implementation
  - Unit tests for validation
  - Integration tests for failover chain behavior
  - UI testing

- [x] Step 11: Fix single-channel mode support
  - Add composite channel resolution to single-channel proxy handler (Messages API)
  - Add composite channel resolution to single-channel responses handler (Responses API)
  - Fix API key check to skip validation for composite channels
  - Fix CompositeChannelEditor to reinitialize state on modal reopen

## Commits

- `5f47bd1` - fix: composite channel trace affinity binding to wrong channel v1.3.151
- `0baebf0` - fix: composite channel UI issues and API response v1.3.150
- `e28da03` - feat: add composite channel for model-level routing v1.3.149
- `78ebf3b` - feat: add composite channel logging in handlers (Phase 3)
- `2fccaa4` - feat: add composite channel scheduler integration (Phase 2)
- `9a594b3` - fix: enable composite channel routing in single-channel mode v1.3.152

