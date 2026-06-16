package handlers

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeResponsesWebSocketConn struct {
	mu            sync.Mutex
	closeCount    int
	controlFrames []int
	controlText   []string
}

func (f *fakeResponsesWebSocketConn) WriteControl(messageType int, data []byte, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.controlFrames = append(f.controlFrames, messageType)
	f.controlText = append(f.controlText, string(data))
	return nil
}

func (f *fakeResponsesWebSocketConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return nil
}

func (f *fakeResponsesWebSocketConn) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCount
}

func (f *fakeResponsesWebSocketConn) closeFrameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, frame := range f.controlFrames {
		if frame == websocket.CloseMessage {
			count++
		}
	}
	return count
}

func (f *fakeResponsesWebSocketConn) closeReasonContains(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, payload := range f.controlText {
		if strings.Contains(payload, text) {
			return true
		}
	}
	return false
}

func TestResponsesWebSocketConnectionManager_CloseByStableIDClosesMatchingConnections(t *testing.T) {
	manager := newResponsesWebSocketConnectionManager()
	client := &fakeResponsesWebSocketConn{}
	upstream := &fakeResponsesWebSocketConn{}
	otherClient := &fakeResponsesWebSocketConn{}
	otherUpstream := &fakeResponsesWebSocketConn{}

	manager.Register(0, "resp-a", client, upstream)
	manager.Register(1, "resp-b", otherClient, otherUpstream)

	closed := manager.CloseByChannel(9, "resp-a", "Responses WebSocket channel disabled")

	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if client.closedCount() != 1 || upstream.closedCount() != 1 {
		t.Fatalf("matching connection closes = client:%d upstream:%d, want 1/1", client.closedCount(), upstream.closedCount())
	}
	if otherClient.closedCount() != 0 || otherUpstream.closedCount() != 0 {
		t.Fatalf("non-matching connection closes = client:%d upstream:%d, want 0/0", otherClient.closedCount(), otherUpstream.closedCount())
	}
	if client.closeFrameCount() != 1 {
		t.Fatalf("client close frames = %d, want 1", client.closeFrameCount())
	}
	if !client.closeReasonContains("channel disabled") {
		t.Fatalf("client close frame does not contain expected reason")
	}
}

func TestResponsesWebSocketConnectionManager_UnregisterPreventsAdminClose(t *testing.T) {
	manager := newResponsesWebSocketConnectionManager()
	client := &fakeResponsesWebSocketConn{}
	upstream := &fakeResponsesWebSocketConn{}

	unregister := manager.Register(0, "resp-a", client, upstream)
	unregister()
	unregister()

	closed := manager.CloseByChannel(0, "resp-a", "Responses WebSocket channel disabled")

	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
	if client.closedCount() != 0 || upstream.closedCount() != 0 {
		t.Fatalf("connection closes = client:%d upstream:%d, want 0/0", client.closedCount(), upstream.closedCount())
	}
}

func TestResponsesWebSocketConnectionManager_CloseByChannelIsIdempotent(t *testing.T) {
	manager := newResponsesWebSocketConnectionManager()
	client := &fakeResponsesWebSocketConn{}
	upstream := &fakeResponsesWebSocketConn{}

	manager.Register(0, "resp-a", client, upstream)

	first := manager.CloseByChannel(0, "resp-a", "Responses WebSocket channel disabled")
	second := manager.CloseByChannel(0, "resp-a", "Responses WebSocket channel disabled")

	if first != 1 || second != 0 {
		t.Fatalf("closed counts = first:%d second:%d, want 1/0", first, second)
	}
	if client.closedCount() != 1 || upstream.closedCount() != 1 {
		t.Fatalf("connection closes = client:%d upstream:%d, want 1/1", client.closedCount(), upstream.closedCount())
	}
}

func TestResponsesWebSocketConnectionManager_StableIDDoesNotCloseDifferentChannelAtSameIndex(t *testing.T) {
	manager := newResponsesWebSocketConnectionManager()
	client := &fakeResponsesWebSocketConn{}
	upstream := &fakeResponsesWebSocketConn{}

	manager.Register(0, "resp-current", client, upstream)

	closed := manager.CloseByChannel(0, "resp-deleted", "Responses WebSocket channel disabled")

	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
	if client.closedCount() != 0 || upstream.closedCount() != 0 {
		t.Fatalf("connection closes = client:%d upstream:%d, want 0/0", client.closedCount(), upstream.closedCount())
	}
}

func TestResponsesWebSocketConnectionManager_EmptyStableIDFallsBackToIndex(t *testing.T) {
	manager := newResponsesWebSocketConnectionManager()
	client := &fakeResponsesWebSocketConn{}
	upstream := &fakeResponsesWebSocketConn{}
	otherClient := &fakeResponsesWebSocketConn{}
	otherUpstream := &fakeResponsesWebSocketConn{}

	manager.Register(0, "", client, upstream)
	manager.Register(1, "", otherClient, otherUpstream)

	closed := manager.CloseByChannel(0, "", "Responses WebSocket channel disabled")

	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if client.closedCount() != 1 || upstream.closedCount() != 1 {
		t.Fatalf("matching connection closes = client:%d upstream:%d, want 1/1", client.closedCount(), upstream.closedCount())
	}
	if otherClient.closedCount() != 0 || otherUpstream.closedCount() != 0 {
		t.Fatalf("non-matching connection closes = client:%d upstream:%d, want 0/0", otherClient.closedCount(), otherUpstream.closedCount())
	}
}
