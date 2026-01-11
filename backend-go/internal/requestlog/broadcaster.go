package requestlog

import (
	"log"
	"sync"
	"sync/atomic"
)

const (
	maxClients     = 100
	channelBuffer  = 100
)

// Broadcaster manages SSE client connections and event broadcasting
type Broadcaster struct {
	clients   map[string]chan *LogEvent
	mu        sync.RWMutex
	clientSeq atomic.Uint64
}

// NewBroadcaster creates a new broadcaster instance
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[string]chan *LogEvent),
	}
}

// Subscribe adds a new client and returns an event channel
// Returns (clientID, eventChannel) or ("", nil) if at capacity
func (b *Broadcaster) Subscribe() (string, <-chan *LogEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.clients) >= maxClients {
		log.Printf("⚠️ SSE broadcaster at capacity (%d clients)", maxClients)
		return "", nil
	}

	clientID := b.generateClientID()
	ch := make(chan *LogEvent, channelBuffer)
	b.clients[clientID] = ch

	log.Printf("📡 SSE client connected: %s (total: %d)", clientID, len(b.clients))
	return clientID, ch
}

// Unsubscribe removes a client from the broadcaster
func (b *Broadcaster) Unsubscribe(clientID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.clients[clientID]; ok {
		close(ch)
		delete(b.clients, clientID)
		log.Printf("📡 SSE client disconnected: %s (total: %d)", clientID, len(b.clients))
	}
}

// Broadcast sends an event to all connected clients (non-blocking)
func (b *Broadcaster) Broadcast(event *LogEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for clientID, ch := range b.clients {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel full, skip this client
			log.Printf("⚠️ SSE channel full for client %s, dropping event", clientID)
		}
	}
}

// ClientCount returns the current number of connected clients
func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

func (b *Broadcaster) generateClientID() string {
	seq := b.clientSeq.Add(1)
	return "sse_" + string(rune('A'+seq%26)) + string(rune('0'+seq/26%10))
}
