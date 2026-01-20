# Per-Channel Rate Limit with Queue

**Created**: 2026-01-06
**Status**: Completed
**Scope**: Backend + Frontend

## Overview

Add per-channel rate limiting with optional queue mode. This allows CC-Bridge to respect upstream provider rate limits and provide smooth UX by queuing requests instead of rejecting them.

## Current Rate Limit Architecture

| Layer | Scope | Location | Behavior |
|-------|-------|----------|----------|
| Global API Limit | All `/v1/*` requests | Admin Settings | Reject when exceeded |
| Per-API-Key Limit | Per API key | API Key Settings | Reject when exceeded |

## New Rate Limit Layer

| Layer | Scope | Location | Behavior |
|-------|-------|----------|----------|
| Per-Channel Limit | Per channel | Channel Edit Form | Reject OR Queue (configurable) |

## Request Flow

```
Request → Global Limit → API Key Limit → Channel Limit → Upstream
              ↓              ↓               ↓
           Reject         Reject      Reject/Queue
```

## New Channel Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rateLimitRpm` | int | 0 | Requests per minute (0 = disabled) |
| `queueEnabled` | bool | false | Enable queue mode instead of reject |
| `queueTimeout` | int | 60 | Max seconds to wait in queue |

## Queue Mechanics

When `queueEnabled = true` and rate limit is exceeded:

1. **Queue Depth** = `rateLimitRpm` value (e.g., 20 RPM → max 20 queued)
2. **Release Order** = FIFO (first-in-first-out)
3. **Release Interval** = 1 second between releases
4. **Queue Timeout** = `queueTimeout` seconds, then reject with 429
5. **Processing Timeout** = Channel's existing timeout setting (separate from queue wait)

### Queue Full Behavior

When queue is full (depth = rateLimitRpm), new requests are rejected immediately with 429.

### Window Reset

The rate limit uses a sliding window (same as existing implementation):
- Window starts on first request
- Resets after 60 seconds
- Queue releases requests as slots become available

## Data Model Changes

### Backend (Go)

```go
// In config/types.go - Channel struct
type Channel struct {
    // ... existing fields ...

    // Per-channel rate limiting
    RateLimitRpm int  `json:"rateLimitRpm,omitempty"` // 0 = disabled
    QueueEnabled bool `json:"queueEnabled,omitempty"` // false = reject mode
    QueueTimeout int  `json:"queueTimeout,omitempty"` // seconds, default 60
}
```

### Frontend (TypeScript)

```typescript
// In services/api.ts - Channel interface
interface Channel {
    // ... existing fields ...

    rateLimitRpm?: number   // 0 = disabled
    queueEnabled?: boolean  // false = reject mode
    queueTimeout?: number   // seconds, default 60
}
```

## New Components

### ChannelRateLimiter (backend-go/internal/middleware/channel_ratelimit.go)

```go
type ChannelRateLimiter struct {
    mu       sync.Mutex
    channels map[int]*channelLimitState // keyed by channel index
}

type channelLimitState struct {
    count      int
    windowEnd  time.Time
    queue      chan *queuedRequest
    queueDepth int
}

type queuedRequest struct {
    done    chan struct{}
    err     error
    timeout time.Duration
}

func (cl *ChannelRateLimiter) Acquire(channelIdx int, cfg *Channel) error
func (cl *ChannelRateLimiter) Release(channelIdx int)
```

## Implementation Steps

### Phase 1: Backend Core
- [x] Step 1: Add new fields to Channel struct in `config/types.go`
- [x] Step 2: Create `ChannelRateLimiter` in `middleware/channel_ratelimit.go`
- [x] Step 3: Implement sliding window counter per channel
- [x] Step 4: Implement queue with FIFO release (1s interval)
- [x] Step 5: Implement queue timeout handling
- [x] Step 6: Integrate into proxy handlers (`proxy.go`, `responses.go`)

### Phase 2: Backend API
- [x] Step 7: Update channel CRUD endpoints to handle new fields
- [x] Step 8: Add channel rate limit status to `/api/channels` response
- [x] Step 9: Clear rate limit state on channel delete (prevent index reuse issues)

### Phase 3: Frontend
- [x] Step 10: Add rate limit fields to `AddChannelModal.vue`
- [x] Step 11: Add i18n translations (en.ts, zh-CN.ts)

### Phase 4: Testing & Documentation
- [x] Step 12: Test queue behavior under load
- [x] Step 13: Test timeout handling
- [x] Step 14: Codex code review and bug fixes

## API Response Examples

### 429 Response (Queue Full or Timeout)

```json
{
    "error": "Too Many Requests",
    "message": "Channel rate limit exceeded, queue is full",
    "retry_after": 5
}
```

### Queue Status in Channel Response

```json
{
    "id": 1,
    "name": "Claude API",
    "rateLimitRpm": 20,
    "queueEnabled": true,
    "queueTimeout": 60,
    "queueStatus": {
        "current": 5,
        "max": 20,
        "windowResetIn": 45
    }
}
```

## UI Mockup

```
┌─────────────────────────────────────────────────────────┐
│ Channel Settings                                         │
├─────────────────────────────────────────────────────────┤
│                                                          │
│ Rate Limit (RPM)     [    20    ]                       │
│ ○ Hint: 0 = no limit                                    │
│                                                          │
│ [✓] Enable Queue Mode                                   │
│     └─ Queue Timeout (s)  [    60    ]                  │
│        ○ Max wait time before rejecting                 │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| `rateLimitRpm = 0` | No rate limiting, queue disabled |
| `queueEnabled = false` | Reject immediately when limit exceeded |
| Client disconnects while queued | Remove from queue, no upstream call |
| Queue timeout reached | Return 429 to client |
| Channel disabled while requests queued | Drain queue with 503 errors |

## Commits

- `421d521` - feat: per-channel rate limit with queue mode v1.3.143

