# Composite Channel Feature

## Background

Currently, CC-Bridge operates at channel-level granularity. When a request comes in, the scheduler selects a single channel, and ALL models in that request must be served by that channel. This creates limitations:

1. **Model Lock-in**: If using Channel A, you can only access models available in Channel A
2. **Suboptimal Routing**: Can't use haiku from cheap Channel A while using opus from premium Channel C
3. **Cost Inefficiency**: Forced to use expensive channels for cheap models
4. **Availability Issues**: One channel's downtime affects all model access

**Solution**: Introduce "Composite Channels" - a channel type that aggregates models from multiple real Claude channels, enabling model-level routing.

```
Request (opus)  ──▶ Composite Channel ──┬──▶ Channel A (haiku)
Request (haiku) ──▶                     ├──▶ Channel B (sonnet)
                                        └──▶ Channel C (opus)
```

## Scope & Constraints

| Constraint | Decision | Rationale |
|------------|----------|-----------|
| Service type | Claude (Messages API) only | Different protocols require different converters; Claude is primary use case |
| Cross-type mixing | Not allowed | Composite can only reference other `claude` type channels |
| Fallback chains | Deferred to v2 | Adds significant complexity; single target per pattern for v1 |

## Approach

Add a new `serviceType: "composite"` that:
- Has no baseUrl, apiKeys, or OAuth tokens of its own
- Contains model-to-channel mappings
- Routes each request to the appropriate real channel based on requested model
- Inherits quota/rate-limit tracking from the target channel

## Field Analysis

### Fields in Add/Edit Form (Composite Channel)

| Field | Type | Notes |
|-------|------|-------|
| `name` | text input | Required, channel identification |
| `description` | textarea | Optional, user notes |
| `serviceType` | dropdown | Select "Composite" to trigger form switch |
| `compositeMappings` | drag-drop UI | New mapping editor (replaces all other fields) |

### Fields NOT in Form (Managed Elsewhere)

| Field | How Managed | Same as Regular Channels |
|-------|-------------|--------------------------|
| `status` | Channel card toggle (active/suspended/disabled) | Yes |
| `priority` | Drag-and-drop reorder in channel list | Yes |

### Fields Ignored for Composite Channels

| Field | Reason |
|-------|--------|
| `baseUrl` | No direct API calls |
| `apiKeys` | Uses target channel's keys |
| `oauthTokens` | N/A for claude type |
| `priceMultipliers` | Inherited from target |
| `quotaType/quotaLimit/...` | Tracked per target channel |
| `rateLimitRpm/queueEnabled/...` | Inherited from target |
| `insecureSkipVerify` | No direct connections |
| `responseHeaderTimeout` | No direct connections |
| `modelMapping` | Replaced by compositeMappings |
| `website` | Not relevant |

### New Data Structure

```go
// CompositeMapping defines a model-to-channel mapping
type CompositeMapping struct {
    Pattern       string `json:"pattern"`                  // Model pattern: "haiku", "opus", or "*" (wildcard)
    TargetChannel int    `json:"targetChannel"`            // Target channel index
    TargetModel   string `json:"targetModel,omitempty"`    // Optional: override model name sent to target
}

// Add to UpstreamConfig
type UpstreamConfig struct {
    // ... existing fields ...

    // Composite channel mappings (only for serviceType="composite")
    CompositeMappings []CompositeMapping `json:"compositeMappings,omitempty"`
}
```

### Mapping Resolution Order

1. **Exact match**: `"claude-opus-4-20250514"` → Channel 2
2. **Contains match**: `"opus"` → Channel 2 (matches any model containing "opus")
3. **Wildcard**: `"*"` → Channel 0 (default fallback, must be last)

### Example Configuration

```json
{
  "name": "Premium Mix",
  "serviceType": "composite",
  "status": "active",
  "priority": 1,
  "compositeMappings": [
    { "pattern": "haiku", "targetChannel": 0 },
    { "pattern": "sonnet", "targetChannel": 1 },
    { "pattern": "opus", "targetChannel": 2 },
    { "pattern": "*", "targetChannel": 0 }
  ]
}
```

---

## Implementation Plan

### Phase 1: Backend Schema & Validation ✅

- [x] **Step 1.1**: Add `CompositeMapping` struct to `backend-go/internal/config/config.go`
- [x] **Step 1.2**: Add `CompositeMappings []CompositeMapping` field to `UpstreamConfig`
- [x] **Step 1.3**: Add `IsCompositeChannel(upstream *UpstreamConfig) bool` helper
- [x] **Step 1.4**: Add `ResolveCompositeMapping(upstream *UpstreamConfig, model string) (targetIndex int, targetModel string, found bool)` method
- [x] **Step 1.5**: Add validation in `AddUpstream`:
  - Composite must have at least one mapping
  - Target channels must exist and be `claude` type (not composite - no recursion)
  - Composite should not require baseUrl/apiKeys
- [x] **Step 1.6**: Add same validation to `UpdateUpstream`

### Phase 2: Scheduler Integration ✅

- [x] **Step 2.1**: Update `ChannelScheduler.SelectChannel()`:
  - Add `model string` parameter
  - If selected channel is composite, call `ResolveCompositeMapping`
  - Return the resolved real channel
- [x] **Step 2.2**: Update scheduler response type to include both:
  - `selectedChannel` - the real channel to use
  - `compositeChannel` - the composite channel (if applicable, for logging)
- [x] **Step 2.3**: Handle edge case: if resolved target channel is unavailable:
  - Skip to next mapping that matches the model
  - If no available mappings, treat composite as unavailable

### Phase 3: Handler Integration ✅

- [x] **Step 3.1**: Update `handlers/proxy.go`:
  - Extract model from request before calling scheduler
  - Pass model to `SelectChannel()`
  - Log composite resolution if applicable
- [x] **Step 3.2**: Update request logging to show: `[Composite: Premium Mix] → [Channel: Claude Direct]`

### Phase 4: API Endpoints ✅

- [x] **Step 4.1**: Update `POST /api/channels` to accept composite channel creation
  - Validate compositeMappings field
  - Return error if target channels don't exist or wrong type
- [x] **Step 4.2**: Update `PUT /api/channels/:id` for composite channel updates
- [x] **Step 4.3**: Update `GET /api/channels` response to include compositeMappings
- [x] **Step 4.4**: Add helper endpoint `GET /api/channels/:id/test-mapping?model=xxx`:
  - Returns which target channel would be selected for given model
  - Useful for debugging/testing mappings

### Phase 5: Frontend - Form UI Switch ✅

- [x] **Step 5.1**: Add "Composite" option to service type dropdown in `AddChannelModal.vue`
  - Only show for Messages channels (not Responses)
- [x] **Step 5.2**: Add reactive computed `isCompositeChannel` based on `form.serviceType === 'composite'`
- [x] **Step 5.3**: When `composite` selected, conditionally render:
  - Hide: baseUrl input, apiKeys section, website input
  - Hide: Quota tab, Rate Limit tab (disable tab buttons)
  - Hide: Model mapping section (replaced by composite mappings)
  - Hide: Price multipliers section
  - Show: Composite mapping editor section
- [x] **Step 5.4**: Create basic mapping form (before drag-and-drop):
  - List of existing mappings (pattern → channel name)
  - Add mapping: pattern input + channel dropdown
  - Remove mapping button

### Phase 6: Frontend - Drag & Drop Mapping UI ✅

- [x] **Step 6.1**: Create `CompositeChannelEditor.vue` component
- [x] **Step 6.2**: Left panel: Available Claude channels
  - Fetch channels, filter to `serviceType === 'claude'` and `status !== 'disabled'`
  - Show channel name, status indicator
  - List common models (haiku, sonnet, opus) as clickable chips
- [x] **Step 6.3**: Right panel: Mapping slots
  - Drag-to-reorder mapping list with vuedraggable
  - Each slot shows: pattern, target channel, optional model override
  - Reorderable (affects matching priority)
- [x] **Step 6.4**: Add manual pattern input for custom patterns
- [x] **Step 6.5**: Add wildcard `*` option for default fallback
- [x] **Step 6.6**: Visual validation:
  - Warning if no wildcard fallback (with one-click add button)
  - Warning if target channel is suspended/disabled
  - Error if duplicate exact patterns

### Phase 7: Testing & Documentation

- [ ] **Step 7.1**: Unit tests for `ResolveCompositeMapping`:
  - Exact match
  - Contains match
  - Wildcard match
  - No match (error case)
- [ ] **Step 7.2**: Unit tests for composite validation
- [ ] **Step 7.3**: Integration test: request through composite channel
- [ ] **Step 7.4**: Update backend-go/CLAUDE.md with composite channel docs

---

## UI Design

### Service Type Selection (Real-time Switch)

```
Before selecting composite:           After selecting composite:
┌─────────────────────────────┐      ┌─────────────────────────────┐
│ Name: [________________]    │      │ Name: [________________]    │
│ Service: [▼ Claude API  ]   │  →   │ Service: [▼ Composite   ]   │
│                             │      │                             │
│ Base URL: [_____________]   │      │ ┌─ Model Mappings ────────┐ │
│ API Keys: [_____________]   │      │ │                         │ │
│                             │      │ │  [Drag-drop UI here]    │ │
│ [Config] [Quota] [Rate]     │      │ │                         │ │
└─────────────────────────────┘      │ └─────────────────────────┘ │
                                     │                             │
                                     │ (No Quota/Rate tabs)        │
                                     └─────────────────────────────┘
```

### Composite Mapping Editor (Phase 6)

```
┌─────────────────────────────────────────────────────────────────┐
│ Composite Channel: Premium Mix                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─ Available Channels ──────┐  ┌─ Model Mappings ───────────┐  │
│  │                           │  │  (drag to reorder)         │  │
│  │  ▼ Claude Direct          │  │                            │  │
│  │    [haiku] [sonnet] [opus]│  │  ┌────────────────────────┐ │  │
│  │                           │  │  │ 1. haiku → Claude Direct│ │  │
│  │  ▼ Claude Reseller        │  │  └────────────────────────┘ │  │
│  │    [haiku] [sonnet]       │  │  ┌────────────────────────┐ │  │
│  │                           │  │  │ 2. sonnet → Reseller   │ │  │
│  │  ▼ Claude Premium         │  │  └────────────────────────┘ │  │
│  │    [opus]                 │  │  ┌────────────────────────┐ │  │
│  │                           │  │  │ 3. opus → Premium      │ │  │
│  │  ✗ OpenAI (incompatible)  │  │  └────────────────────────┘ │  │
│  │                           │  │  ┌────────────────────────┐ │  │
│  │  [+ Custom pattern]       │  │  │ 4. * → Claude Direct   │ │  │
│  │                           │  │  └────────────────────────┘ │  │
│  └───────────────────────────┘  └────────────────────────────┘  │
│                                                                 │
│                               [Cancel]  [Save Channel]          │
└─────────────────────────────────────────────────────────────────┘
```

---

## Future Enhancements (v2)

### Fallback Chain

> **Complexity Assessment**: Adding fallback would require:
> 1. Multiple targets per pattern with priority
> 2. Retry loop when resolved channel fails
> 3. State tracking to prevent infinite loops
>
> **Deferred**: Users can work around by having composite as first priority, with regular channels as fallback in the scheduler.

### Responses API Support

If needed, add composite support for Responses channels (Codex) with same pattern.

---

## Commits

_(To be filled as implementation progresses)_

- `_______` - Phase 1: Backend schema and validation
- `2fccaa4` - Phase 2: Scheduler integration
- `78ebf3b` - Phase 3: Handler integration
- `_______` - Phase 4: API endpoints
- `_______` - Phase 5: Frontend form UI switch
- `_______` - Phase 6: Drag-and-drop mapping UI
- `_______` - Phase 7: Tests and documentation
