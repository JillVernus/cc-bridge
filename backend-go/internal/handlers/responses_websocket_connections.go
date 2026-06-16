package handlers

import (
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const responsesWebSocketAdminCloseCode = websocket.ClosePolicyViolation

type responsesWebSocketControlCloser interface {
	WriteControl(messageType int, data []byte, deadline time.Time) error
	Close() error
}

type responsesWebSocketConnectionKey struct {
	channelIndex    int
	channelStableID string
}

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
	manager    *responsesWebSocketConnectionManager
}

func newResponsesWebSocketConnectionManager() *responsesWebSocketConnectionManager {
	return &responsesWebSocketConnectionManager{
		entries: make(map[*responsesWebSocketConnection]struct{}),
	}
}

var activeResponsesWebSockets = newResponsesWebSocketConnectionManager()

func (m *responsesWebSocketConnectionManager) Register(channelIndex int, channelStableID string, client, upstream responsesWebSocketControlCloser) func() {
	if m == nil || client == nil || upstream == nil {
		return func() {}
	}

	entry := &responsesWebSocketConnection{
		key: responsesWebSocketConnectionKey{
			channelIndex:    channelIndex,
			channelStableID: strings.TrimSpace(channelStableID),
		},
		client:   client,
		upstream: upstream,
		manager:  m,
	}

	m.mu.Lock()
	m.entries[entry] = struct{}{}
	m.mu.Unlock()

	return entry.unregister
}

func (m *responsesWebSocketConnectionManager) CloseByChannel(channelIndex int, channelStableID string, reason string) int {
	if m == nil {
		return 0
	}

	channelStableID = strings.TrimSpace(channelStableID)
	var matches []*responsesWebSocketConnection

	m.mu.Lock()
	for entry := range m.entries {
		if responsesWebSocketConnectionMatches(entry.key, channelIndex, channelStableID) {
			delete(m.entries, entry)
			matches = append(matches, entry)
		}
	}
	m.mu.Unlock()

	for _, entry := range matches {
		entry.close(reason)
	}

	return len(matches)
}

func responsesWebSocketConnectionMatches(key responsesWebSocketConnectionKey, channelIndex int, channelStableID string) bool {
	if channelStableID != "" {
		return key.channelStableID == channelStableID
	}
	return key.channelIndex == channelIndex
}

func (e *responsesWebSocketConnection) unregister() {
	if e == nil || e.manager == nil {
		return
	}

	e.removeOnce.Do(func() {
		e.manager.mu.Lock()
		delete(e.manager.entries, e)
		e.manager.mu.Unlock()
	})
}

func (e *responsesWebSocketConnection) close(reason string) {
	if e == nil {
		return
	}

	e.closeOnce.Do(func() {
		e.unregister()

		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "Responses WebSocket channel disabled"
		}

		deadline := time.Now().Add(time.Second)
		payload := websocket.FormatCloseMessage(responsesWebSocketAdminCloseCode, reason)
		_ = e.client.WriteControl(websocket.CloseMessage, payload, deadline)
		_ = e.client.Close()
		_ = e.upstream.Close()
	})
}
