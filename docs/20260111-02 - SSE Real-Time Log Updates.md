# SSE Real-Time Log Updates Implementation Plan

## Background

The current log table uses polling (3 API calls every 3 seconds). This plan implements Server-Sent Events (SSE) for real-time updates, reducing server load and providing instant feedback.

## Architecture Overview

```
┌─────────────┐     SSE      ┌─────────────────────────────────┐
│   Browser   │◄────────────│  /api/logs/stream               │
│  (Vue App)  │              │  ┌───────────────────────────┐  │
└─────────────┘              │  │      Broadcaster          │  │
                             │  │  ┌─────┐ ┌─────┐ ┌─────┐  │  │
                             │  │  │ ch1 │ │ ch2 │ │ ch3 │  │  │
                             │  │  └─────┘ └─────┘ └─────┘  │  │
                             │  └───────────▲───────────────┘  │
                             │              │                  │
                             │  ┌───────────┴───────────────┐  │
                             │  │   RequestLogManager       │  │
                             │  │   Add() / Update()        │  │
                             │  └───────────────────────────┘  │
                             └─────────────────────────────────┘
```

## Event Types

| Event | Trigger | Payload |
|-------|---------|---------|
| `log:created` | New request starts | Partial log (id, status, model, provider) |
| `log:updated` | Request completes/errors | Updated log fields |
| `log:stats` | Every 5 seconds | Stats summary + active sessions |
| `heartbeat` | Every 30 seconds | Keep-alive |

## Implementation Steps

### Phase 1: Backend - Broadcaster Core

- [x] **Step 1.1**: Create `backend-go/internal/requestlog/broadcaster.go`
  - `Broadcaster` struct with client map and mutex
  - `Subscribe(clientID)` / `Unsubscribe(clientID)` methods
  - `Broadcast(event)` with non-blocking send
  - Max 100 clients, channel buffer 100

- [x] **Step 1.2**: Create `backend-go/internal/requestlog/events.go`
  - `LogEvent` struct (Type, Data, Timestamp)
  - `LogEventPayload` for created/updated events
  - `StatsEventPayload` for periodic stats

- [x] **Step 1.3**: Modify `backend-go/internal/requestlog/manager.go`
  - Add `broadcaster *Broadcaster` field
  - Initialize in `NewManager()`
  - Call `BroadcastLogCreated()` after `Add()`
  - Call `BroadcastLogUpdated()` after `Update()`
  - Add `GetBroadcaster()` accessor

### Phase 2: Backend - SSE Handler

- [x] **Step 2.1**: Add `StreamLogs()` to `backend-go/internal/handlers/requestlog_handler.go`
  - Set SSE headers (text/event-stream, no-cache, keep-alive)
  - Subscribe to broadcaster on connect
  - Loop: select event channel + context.Done()
  - Format: `event: type\ndata: json\n\n`
  - Unsubscribe on disconnect

- [x] **Step 2.2**: Add route in `backend-go/main.go`
  - `apiGroup.GET("/logs/stream", reqLogHandler.StreamLogs)`
  - Updated auth middleware to support `?key=` query param for SSE

### Phase 3: Frontend - EventSource Composable

- [x] **Step 3.1**: Create `frontend/src/composables/useLogStream.ts`
  - EventSource wrapper with auto-reconnect
  - Exponential backoff (1s, 2s, 4s, 8s, max 5 attempts)
  - Event handler registration (on/off)
  - Connection state tracking
  - Falls back to polling after max retries

- [x] **Step 3.2**: Add `getLogStreamURL()` to `frontend/src/services/api.ts`

### Phase 4: Frontend - Integration

- [x] **Step 4.1**: Integrate SSE into `frontend/src/components/RequestLogTable.vue`
  - Initialize `useLogStream()` in onMounted
  - Handle `log:created`: prepend to logs array, highlight
  - Handle `log:updated`: update in-place, highlight
  - Handle `log:stats`: update stats + active sessions
  - Disable polling when SSE connected
  - Enable polling as fallback when disconnected

- [x] **Step 4.2**: Update UI indicators
  - Show "Live" badge when SSE connected
  - Show polling toggle when SSE unavailable

### Phase 5: Testing & Polish

- [ ] **Step 5.1**: Manual testing checklist
  - Single tab: logs appear instantly
  - Multiple tabs: all receive updates
  - Network disconnect: falls back to polling
  - Network reconnect: SSE resumes
  - Server restart: graceful reconnection

- [x] **Step 5.2**: Version bump and commit

## Key Files

| File | Action |
|------|--------|
| `backend-go/internal/requestlog/broadcaster.go` | Create |
| `backend-go/internal/requestlog/events.go` | Create |
| `backend-go/internal/requestlog/manager.go` | Modify |
| `backend-go/internal/handlers/requestlog_handler.go` | Modify |
| `backend-go/cmd/main.go` | Modify |
| `frontend/src/composables/useLogStream.ts` | Create |
| `frontend/src/services/api.ts` | Modify |
| `frontend/src/components/RequestLogTable.vue` | Modify |

## Event Payload Examples

```json
// log:created
{"type":"log:created","data":{"id":"req_abc","status":"pending","providerName":"Claude","model":"claude-sonnet-4-20250514","channelName":"Primary","initialTime":"2026-01-11T15:00:00Z"}}

// log:updated
{"type":"log:updated","data":{"id":"req_abc","status":"completed","durationMs":1234,"inputTokens":500,"outputTokens":200,"price":0.005}}

// log:stats (every 5s)
{"type":"log:stats","data":{"totalRequests":100,"totalCost":5.50,"byProvider":{...},"activeSessions":[...]}}
```

## Fallback Strategy

```
SSE Connect → Success → Disable polling, use SSE
           → Fail → Retry with backoff (1s, 2s, 4s, 8s, 16s)
                  → Max retries (5) → Enable polling permanently

SSE Disconnect → Enable polling → Attempt SSE reconnect in background
```

## Verification

1. **Backend**: `curl -N http://localhost:8080/api/logs/stream` should stream events
2. **Frontend**: Open DevTools Network tab, filter EventStream, verify events arrive
3. **Multi-tab**: Open 2+ tabs, make request, both update simultaneously
4. **Failover**: Kill server, verify polling activates, restart server, verify SSE resumes

## Commits

