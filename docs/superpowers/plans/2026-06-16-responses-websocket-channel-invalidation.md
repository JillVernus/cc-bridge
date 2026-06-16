# Responses WebSocket Channel Invalidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop active Responses WebSocket conversations from continuing to use a channel after that channel is disabled, suspended, deleted, or has WebSocket support turned off.

**Architecture:** Add a small in-process active WebSocket connection manager owned by the `handlers` package. The Responses WebSocket handler registers live client/upstream socket pairs after upstream dial succeeds, and channel-management handlers close matching live sockets after successful administrative changes.

**Tech Stack:** Go 1.22, Gin handlers, Gorilla WebSocket, existing `config.ConfigManager`, existing handler tests with `httptest`.

---

## Problem Summary

Current behavior:

- `ResponsesWebSocketHandler` selects one upstream once during `GET /v1/responses`.
- It then dials one upstream WebSocket and `proxyResponsesWebSocketFrames` forwards all later frames through that same socket.
- Channel management clears trace affinity when a channel is disabled/suspended, but does not close already-open WebSocket connections.
- Switching to a non-WebSocket channel cannot affect an already-open WebSocket transport.

Required behavior:

- Existing WebSocket sessions using a taken-down channel must be closed promptly.
- The client must reconnect and let normal routing/transport negotiation happen again.
- The bridge must not try to hot-swap a live WebSocket to a non-WebSocket channel.

## File Structure

- Create: `backend-go/internal/handlers/responses_websocket_connections.go`
  - Defines `responsesWebSocketConnectionManager`.
  - Tracks active Responses WebSocket client/upstream connection pairs by channel index and stable channel ID.
  - Provides idempotent `Register` and `CloseByChannel` methods.

- Create: `backend-go/internal/handlers/responses_websocket_connections_test.go`
  - Unit tests for manager registration, unregister, close-by-stable-ID, close-by-index fallback, and idempotency.

- Modify: `backend-go/internal/handlers/responses_websocket.go`
  - Register active client/upstream sockets after upstream dial succeeds.
  - Unregister on handler exit.

- Modify: `backend-go/internal/handlers/config.go`
  - Close active Responses WebSocket sessions when a Responses channel is disabled/suspended.
  - Close active Responses WebSocket sessions when `responsesWebSocketEnabled` changes from usable to unusable.
  - Close active Responses WebSocket sessions when a Responses channel is deleted.

- Modify: `backend-go/internal/handlers/responses_websocket_test.go`
  - Add integration coverage for disabling a channel while a WebSocket is open.
  - Add integration coverage for turning off `responsesWebSocketEnabled` while a WebSocket is open.

- Optional modify: `backend-go/internal/handlers/channel_status_affinity_test.go`
  - Extend current status tests only if shared helpers make that simpler than adding cases to `responses_websocket_test.go`.

## Design Details

### Connection Key

Track both:

- `channelIndex int`
- `channelStableID string`

Stable ID is the primary identity when present. Index is retained as a fallback for legacy/malformed configs and for current handler APIs that are index-based.

Do not track only by index. Deleting/reordering channels can make index-only invalidation close the wrong live session or miss the intended one.

### Close Behavior

Use WebSocket close code `1008 Policy Violation` with a short reason such as:

```text
Responses WebSocket channel disabled
```

Reason:

- The channel is no longer administratively allowed for this transport.
- The client should reconnect or fall back to HTTP/SSE.

Implementation should send `WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(...), deadline)` to the client first, then close both client and upstream sockets. Ignore close/write errors because one side may already be closed.

### Concurrency Rules

- Manager methods must be safe for concurrent use.
- Do not hold the manager mutex while writing close frames or closing sockets.
- Unregister must be idempotent.
- Close must be idempotent.
- Closing due to admin action should remove entries from the manager before closing sockets to avoid double-close loops.

### Scope Boundaries

Do not:

- Change normal HTTP/SSE Responses routing.
- Try to switch an existing WebSocket to another channel.
- Add database state for active sockets.
- Add frontend UI changes.

---

## Task 1: Add Connection Manager Unit Tests

**Files:**

- Create: `backend-go/internal/handlers/responses_websocket_connections_test.go`

- [ ] **Step 1: Write fake connection type**

Add a small fake closer/control-writer in the test file:

```go
type fakeWSConn struct {
	mu            sync.Mutex
	closeCount    int
	controlFrames []int
	controlText   []string
}

func (f *fakeWSConn) WriteControl(messageType int, data []byte, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.controlFrames = append(f.controlFrames, messageType)
	f.controlText = append(f.controlText, string(data))
	return nil
}

func (f *fakeWSConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return nil
}

func (f *fakeWSConn) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCount
}
```

- [ ] **Step 2: Write failing manager tests**

Add tests:

```go
func TestResponsesWebSocketConnectionManager_CloseByStableIDClosesMatchingConnections(t *testing.T)
func TestResponsesWebSocketConnectionManager_UnregisterPreventsAdminClose(t *testing.T)
func TestResponsesWebSocketConnectionManager_CloseByChannelIsIdempotent(t *testing.T)
func TestResponsesWebSocketConnectionManager_StableIDDoesNotCloseDifferentChannelAtSameIndex(t *testing.T)
func TestResponsesWebSocketConnectionManager_EmptyStableIDFallsBackToIndex(t *testing.T)
```

Expected assertions:

- Matching client and upstream both close once.
- Non-matching connections remain open.
- Calling returned unregister twice is safe.
- Calling `CloseByChannel` twice is safe.
- Close frame type includes `websocket.CloseMessage`.

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
cd backend-go && go test ./internal/handlers -run TestResponsesWebSocketConnectionManager -count=1
```

Expected: FAIL because manager types do not exist.

---

## Task 2: Implement Connection Manager

**Files:**

- Create: `backend-go/internal/handlers/responses_websocket_connections.go`

- [ ] **Step 1: Add connection interfaces and key types**

Implement:

```go
type responsesWebSocketControlCloser interface {
	WriteControl(messageType int, data []byte, deadline time.Time) error
	Close() error
}

type responsesWebSocketConnectionKey struct {
	channelIndex    int
	channelStableID string
}
```

- [ ] **Step 2: Add manager and entry structs**

Implement:

```go
type responsesWebSocketConnectionManager struct {
	mu      sync.Mutex
	entries map[*responsesWebSocketConnection]struct{}
}

type responsesWebSocketConnection struct {
	key      responsesWebSocketConnectionKey
	client   responsesWebSocketControlCloser
	upstream responsesWebSocketControlCloser
	removeOnce sync.Once
	closeOnce  sync.Once
	manager *responsesWebSocketConnectionManager
}
```

- [ ] **Step 3: Add constructor and package-level manager**

Implement:

```go
func newResponsesWebSocketConnectionManager() *responsesWebSocketConnectionManager {
	return &responsesWebSocketConnectionManager{
		entries: make(map[*responsesWebSocketConnection]struct{}),
	}
}

var activeResponsesWebSockets = newResponsesWebSocketConnectionManager()
```

- [ ] **Step 4: Implement `Register`**

Behavior:

- Ignore nil client/upstream by returning a no-op unregister.
- Trim stable ID.
- Store entry under lock.
- Return an unregister function that removes the entry once.

Signature:

```go
func (m *responsesWebSocketConnectionManager) Register(channelIndex int, channelStableID string, client, upstream responsesWebSocketControlCloser) func()
```

- [ ] **Step 5: Implement `CloseByChannel`**

Behavior:

- Collect matching entries under lock.
- Remove matching entries under the same lock.
- Close collected entries outside the lock.
- Return number of matched entries.

Signature:

```go
func (m *responsesWebSocketConnectionManager) CloseByChannel(channelIndex int, channelStableID string, reason string) int
```

Matching rule:

- If `channelStableID` is non-empty, match by stable ID.
- If `channelStableID` is empty, match by index.

- [ ] **Step 6: Implement close helper**

Use:

```go
const responsesWebSocketAdminCloseCode = websocket.ClosePolicyViolation

func (e *responsesWebSocketConnection) close(reason string) {
	e.closeOnce.Do(func() {
		if strings.TrimSpace(reason) == "" {
			reason = "Responses WebSocket channel disabled"
		}
		deadline := time.Now().Add(time.Second)
		payload := websocket.FormatCloseMessage(responsesWebSocketAdminCloseCode, reason)
		_ = e.client.WriteControl(websocket.CloseMessage, payload, deadline)
		_ = e.client.Close()
		_ = e.upstream.Close()
	})
}
```

- [ ] **Step 7: Run manager tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run TestResponsesWebSocketConnectionManager -count=1
```

Expected: PASS.

---

## Task 3: Register Live Sockets In Responses WebSocket Handler

**Files:**

- Modify: `backend-go/internal/handlers/responses_websocket.go`

- [ ] **Step 1: Register after upstream dial succeeds**

After:

```go
defer upstreamConn.Close()
```

Add:

```go
unregisterWebSocket := activeResponsesWebSockets.Register(
	selection.ChannelIndex,
	strings.TrimSpace(upstream.ID),
	clientConn,
	upstreamConn,
)
defer unregisterWebSocket()
```

Guard for nil `selection` if needed, although the existing flow should always have a selection after successful upstream selection:

```go
channelIndex := -1
if selection != nil {
	channelIndex = selection.ChannelIndex
}
```

- [ ] **Step 2: Run existing WebSocket tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run TestResponsesWebSocket -count=1
```

Expected: PASS.

---

## Task 4: Close Live Sockets On Status Disable/Suspend

**Files:**

- Modify: `backend-go/internal/handlers/config.go`
- Modify: `backend-go/internal/handlers/responses_websocket_test.go`

- [ ] **Step 1: Add helper to close active Responses WebSockets**

Near `clearTraceAffinityForTakenDownChannel`, add:

```go
func closeResponsesWebSocketsForTakenDownChannel(channelIndex int, channelStableID string, status string) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "suspended", "disabled":
		activeResponsesWebSockets.CloseByChannel(
			channelIndex,
			channelStableID,
			"Responses WebSocket channel disabled",
		)
	}
}
```

- [ ] **Step 2: Capture stable ID in `SetResponsesChannelStatus` before update**

Before `cfgManager.SetResponsesChannelStatus(id, req.Status)`, read:

```go
cfg := cfgManager.GetConfig()
channelStableID := ""
if id >= 0 && id < len(cfg.ResponsesUpstream) {
	channelStableID = strings.TrimSpace(cfg.ResponsesUpstream[id].ID)
}
```

After successful `SetResponsesChannelStatus`, call:

```go
closeResponsesWebSocketsForTakenDownChannel(id, channelStableID, req.Status)
```

Keep the existing trace-affinity clearing call.

- [ ] **Step 3: Add integration test for disabling live channel**

Add test:

```go
func TestResponsesWebSocketHandlerClosesActiveConnectionWhenChannelDisabled(t *testing.T)
```

Test shape:

- Start upstream `httptest.Server` that upgrades to WebSocket.
- Configure one active Responses channel with `ResponsesWebSocketEnabled: true`.
- Start bridge router with:
  - `GET /v1/responses` using `ResponsesWebSocketHandler`
  - `PATCH /api/responses/channels/:id/status` using `SetResponsesChannelStatus`
- Dial bridge WebSocket.
- Confirm upstream receives first `response.create`.
- PATCH channel status to `disabled`.
- Assert client receives close or read returns close error within one second.
- Assert a second message does not arrive at upstream.

- [ ] **Step 4: Run targeted tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run 'TestResponsesWebSocketHandlerClosesActiveConnectionWhenChannelDisabled|TestSetResponsesChannelStatus_TakenDownChannelClearsTraceAffinity' -count=1
```

Expected: PASS.

---

## Task 5: Close Live Sockets When WebSocket Support Is Disabled By Update

**Files:**

- Modify: `backend-go/internal/handlers/config.go`
- Modify: `backend-go/internal/handlers/responses_websocket_test.go`

- [ ] **Step 1: Capture previous WebSocket eligibility before update**

In `UpdateResponsesUpstream`, before applying updates:

```go
beforeCfg := cfgManager.GetConfig()
previousStableID := ""
previousEligible := false
if id >= 0 && id < len(beforeCfg.ResponsesUpstream) {
	previous := beforeCfg.ResponsesUpstream[id]
	previousStableID = strings.TrimSpace(previous.ID)
	previousEligible = isResponsesWebSocketEligible(&previous)
}
```

- [ ] **Step 2: Compare saved channel after update**

After successful update and after `cfg, revision := cfgManager.GetConfigWithRevision()`:

```go
currentEligible := false
if id >= 0 && id < len(cfg.ResponsesUpstream) {
	current := cfg.ResponsesUpstream[id]
	currentEligible = isResponsesWebSocketEligible(&current)
}
if previousEligible && !currentEligible {
	activeResponsesWebSockets.CloseByChannel(
		id,
		previousStableID,
		"Responses WebSocket channel disabled",
	)
}
```

This covers:

- `responsesWebSocketEnabled: false`
- service type changes to `composite`
- status updates that make the channel ineligible through full update

- [ ] **Step 3: Add integration test for disabling WebSocket flag**

Add test:

```go
func TestResponsesWebSocketHandlerClosesActiveConnectionWhenWebSocketFlagDisabled(t *testing.T)
```

Test shape:

- Same setup as Task 4.
- Use `PUT /api/responses/channels/0` with body:

```json
{"responsesWebSocketEnabled":false}
```

- Assert active WebSocket closes.

- [ ] **Step 4: Run targeted tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run 'TestResponsesWebSocketHandlerClosesActiveConnectionWhenWebSocketFlagDisabled|TestResponsesWebSocketHandlerClosesActiveConnectionWhenChannelDisabled' -count=1
```

Expected: PASS.

---

## Task 6: Close Live Sockets On Responses Channel Delete

**Files:**

- Modify: `backend-go/internal/handlers/config.go`
- Modify: `backend-go/internal/handlers/responses_websocket_connections_test.go`

- [ ] **Step 1: Close deleted channel sockets after successful removal**

In `DeleteResponsesUpstream`, after successful `RemoveResponsesUpstream`:

```go
removedStableID := ""
if removed != nil {
	removedStableID = strings.TrimSpace(removed.ID)
}
activeResponsesWebSockets.CloseByChannel(
	id,
	removedStableID,
	"Responses WebSocket channel disabled",
)
```

Do this before writing the JSON response.

- [ ] **Step 2: Add delete-path test**

Prefer a manager-level test if full HTTP integration becomes noisy:

```go
func TestResponsesWebSocketConnectionManager_CloseDeletedChannelByStableIDAfterIndexShift(t *testing.T)
```

If integration is straightforward, add:

```go
func TestDeleteResponsesUpstreamClosesActiveWebSocketConnection(t *testing.T)
```

Minimum acceptable coverage:

- A connection registered with stable ID `resp-0` closes when delete path calls `CloseByChannel(0, "resp-0", ...)`.
- A connection registered with a different stable ID at index `0` does not close when stable ID does not match.

- [ ] **Step 3: Run targeted tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run 'TestResponsesWebSocketConnectionManager|TestDeleteResponsesUpstreamClosesActiveWebSocketConnection' -count=1
```

Expected: PASS. If the integration delete test is not added, run only the manager tests and document why.

---

## Task 7: Guard Against Global WebSocket Affinity

**Files:**

- Modify: `backend-go/internal/handlers/responses_websocket.go`
- Modify: `backend-go/internal/handlers/responses_websocket_test.go`

- [ ] **Step 1: Replace hard-coded websocket client ID**

Change:

```go
selection, err := channelScheduler.SelectChannel(c.Request.Context(), "codex-websocket", excludedChannels, true, allowedChannels, "")
```

To:

```go
selection, err := channelScheduler.SelectChannel(c.Request.Context(), "", excludedChannels, true, allowedChannels, "")
```

Reason:

- WebSocket selection occurs before a `response.create` payload is available.
- The literal `"codex-websocket"` is not a real conversation identity.
- Avoid accidental global trace affinity if future code sets affinity for this key.

- [ ] **Step 2: Run focused tests**

Run:

```bash
cd backend-go && go test ./internal/handlers -run TestResponsesWebSocket -count=1
```

Expected: PASS.

---

## Task 8: Full Verification

**Files:**

- All modified files.

- [ ] **Step 1: Format Go code**

Run:

```bash
cd backend-go && gofmt -w internal/handlers/responses_websocket.go internal/handlers/responses_websocket_connections.go internal/handlers/responses_websocket_connections_test.go internal/handlers/responses_websocket_test.go internal/handlers/config.go
```

- [ ] **Step 2: Run handler test package**

Run:

```bash
cd backend-go && go test ./internal/handlers -count=1
```

Expected: PASS.

- [ ] **Step 3: Run broader backend tests if handler package passes**

Run:

```bash
cd backend-go && go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Manual smoke test if local config has usable channels**

Scenario:

1. Enable `responsesWebSocketEnabled` on an OAuth Codex Responses channel.
2. Start a Codex websocket conversation.
3. Disable that channel from channel management.
4. Confirm the client socket closes/reconnects and no further traffic is logged against the disabled OAuth channel.
5. Select a non-WebSocket channel and confirm future traffic uses normal Responses HTTP/SSE behavior.

Expected:

- Existing WebSocket does not continue on the disabled OAuth channel.
- New WebSocket connection returns `404` if no websocket-enabled channels remain.
- Request logs do not show new `transport=ws` records for the disabled channel after the close.

---

## Acceptance Criteria

- Disabling or suspending a Responses channel closes active WebSocket sessions using that channel.
- Turning off `responsesWebSocketEnabled` closes active WebSocket sessions using that channel.
- Deleting a Responses channel closes active WebSocket sessions using that channel.
- Existing trace affinity behavior remains intact for future non-WebSocket scheduling.
- No live WebSocket is hot-swapped to a non-WebSocket channel.
- Tests prove the previously sticky OAuth WebSocket cannot keep receiving new frames after channel takedown.

## Notes For Reviewer

Pay special attention to:

- Whether the manager ever holds a mutex while closing sockets.
- Whether stable ID is used so channel deletion/reordering cannot target the wrong socket.
- Whether unregister and admin close are both idempotent.
- Whether tests avoid sleeps where channel/event synchronization would be more reliable.
- Whether the package-level manager causes test contamination; tests may need a helper to replace/reset `activeResponsesWebSockets` during a test.
