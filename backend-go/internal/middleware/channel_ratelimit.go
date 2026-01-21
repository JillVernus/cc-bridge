package middleware

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
)

// ChannelRateLimiter manages per-channel rate limiting with optional queue mode
type ChannelRateLimiter struct {
	mu       sync.Mutex
	channels map[string]*channelLimitState // keyed by "type:index" (e.g., "messages:0", "responses:1")
	stopChan chan struct{}
	stopped  bool // prevents double-close panic
}

// channelLimitState tracks rate limit state for a single channel
type channelLimitState struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
	queue     []*queuedRequest
	releasing bool // true when release goroutine is running
}

// queuedRequest represents a request waiting in the queue
type queuedRequest struct {
	done       chan struct{}
	err        error
	queuedAt   time.Time
	timeout    time.Duration
	channelKey string
}

// ChannelRateLimitResult contains the result of a rate limit check
type ChannelRateLimitResult struct {
	Allowed      bool
	Queued       bool
	QueueDepth   int
	WaitDuration time.Duration
	Error        error
}

// NewChannelRateLimiter creates a new channel rate limiter
func NewChannelRateLimiter() *ChannelRateLimiter {
	crl := &ChannelRateLimiter{
		channels: make(map[string]*channelLimitState),
		stopChan: make(chan struct{}),
	}
	go crl.cleanup()
	return crl
}

// cleanup periodically removes expired channel states
func (crl *ChannelRateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			crl.mu.Lock()
			now := time.Now()
			for channelKey, state := range crl.channels {
				state.mu.Lock()
				// Remove if window expired and no queued requests
				if now.After(state.windowEnd) && len(state.queue) == 0 {
					delete(crl.channels, channelKey)
				}
				state.mu.Unlock()
			}
			crl.mu.Unlock()
		case <-crl.stopChan:
			return
		}
	}
}

// Stop stops the channel rate limiter (safe to call multiple times)
func (crl *ChannelRateLimiter) Stop() {
	crl.mu.Lock()
	defer crl.mu.Unlock()
	if !crl.stopped {
		crl.stopped = true
		close(crl.stopChan)
	}
}

// getOrCreateState gets or creates a channel state
func (crl *ChannelRateLimiter) getOrCreateState(channelKey string) *channelLimitState {
	crl.mu.Lock()
	defer crl.mu.Unlock()

	state, exists := crl.channels[channelKey]
	if !exists {
		state = &channelLimitState{
			queue: make([]*queuedRequest, 0),
		}
		crl.channels[channelKey] = state
	}
	return state
}

// Acquire attempts to acquire a rate limit slot for the given channel
// If queue mode is enabled and limit is exceeded, the request will be queued
// Returns when the request can proceed or when it should be rejected
// channelType should be "messages" or "responses" to distinguish between the two channel pools
func (crl *ChannelRateLimiter) Acquire(ctx context.Context, upstream *config.UpstreamConfig, channelType string) ChannelRateLimitResult {
	// Skip if rate limiting is disabled for this channel
	if upstream.RateLimitRpm <= 0 {
		return ChannelRateLimitResult{Allowed: true}
	}

	// Use composite key to avoid collision between messages and responses channels
	channelKey := fmt.Sprintf("%s:%d", channelType, upstream.Index)
	state := crl.getOrCreateState(channelKey)

	state.mu.Lock()

	now := time.Now()
	rpm := upstream.RateLimitRpm

	// Check if window has expired (use >= to handle exact boundary)
	if !now.Before(state.windowEnd) {
		// Reset window
		state.count = 0
		state.windowEnd = now.Add(time.Minute)
	}

	// Check if under limit
	if state.count < rpm {
		state.count++
		state.mu.Unlock()
		return ChannelRateLimitResult{Allowed: true}
	}

	// Rate limit exceeded - check if queue mode is enabled
	if !upstream.QueueEnabled {
		state.mu.Unlock()
		return ChannelRateLimitResult{
			Allowed: false,
			Error:   fmt.Errorf("channel rate limit exceeded (%d RPM)", rpm),
		}
	}

	// Queue mode enabled - check if queue is full
	maxQueueDepth := rpm // Queue depth = rate limit
	if len(state.queue) >= maxQueueDepth {
		state.mu.Unlock()
		return ChannelRateLimitResult{
			Allowed:    false,
			QueueDepth: len(state.queue),
			Error:      fmt.Errorf("channel rate limit queue full (%d/%d)", len(state.queue), maxQueueDepth),
		}
	}

	// Add to queue
	queueTimeout := time.Duration(upstream.GetQueueTimeout()) * time.Second
	req := &queuedRequest{
		done:       make(chan struct{}),
		queuedAt:   now,
		timeout:    queueTimeout,
		channelKey: channelKey,
	}
	state.queue = append(state.queue, req)
	queueDepth := len(state.queue)

	log.Printf("⏳ [Channel Rate Limit] Channel %s (%s): request queued (%d/%d), timeout=%v",
		channelKey, upstream.Name, queueDepth, maxQueueDepth, queueTimeout)

	// Start release goroutine if not already running
	if !state.releasing {
		state.releasing = true
		go crl.releaseLoop(channelKey, upstream.Name, rpm)
	}

	state.mu.Unlock()

	// Wait for release or timeout
	select {
	case <-req.done:
		if req.err != nil {
			return ChannelRateLimitResult{
				Allowed:      false,
				Queued:       true,
				QueueDepth:   queueDepth,
				WaitDuration: time.Since(req.queuedAt),
				Error:        req.err,
			}
		}
		return ChannelRateLimitResult{
			Allowed:      true,
			Queued:       true,
			QueueDepth:   queueDepth,
			WaitDuration: time.Since(req.queuedAt),
		}
	case <-ctx.Done():
		// Client disconnected - remove from queue
		crl.removeFromQueue(channelKey, req)
		return ChannelRateLimitResult{
			Allowed:      false,
			Queued:       true,
			QueueDepth:   queueDepth,
			WaitDuration: time.Since(req.queuedAt),
			Error:        ctx.Err(),
		}
	}
}

// releaseLoop releases queued requests at 1-second intervals
func (crl *ChannelRateLimiter) releaseLoop(channelKey string, channelName string, rpm int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			state := crl.getOrCreateState(channelKey)
			state.mu.Lock()

			now := time.Now()

			// Check if window has expired - reset counter (use >= to handle exact boundary)
			if !now.Before(state.windowEnd) {
				state.count = 0
				state.windowEnd = now.Add(time.Minute)
			}

			// Check if we can release a request
			if len(state.queue) == 0 {
				state.releasing = false
				state.mu.Unlock()
				return
			}

			// Check if under limit
			if state.count >= rpm {
				state.mu.Unlock()
				continue
			}

			// Get the first request (FIFO)
			req := state.queue[0]

			// Check if request has timed out
			if now.Sub(req.queuedAt) > req.timeout {
				// Remove from queue and signal timeout
				state.queue = state.queue[1:]
				req.err = fmt.Errorf("queue timeout exceeded (%v)", req.timeout)
				close(req.done)
				log.Printf("⏰ [Channel Rate Limit] Channel %s (%s): request timed out after %v",
					channelKey, channelName, now.Sub(req.queuedAt))
				state.mu.Unlock()
				continue
			}

			// Release the request (removed client disconnect check - ctx is no longer stored)
			state.queue = state.queue[1:]
			state.count++
			close(req.done)
			log.Printf("✅ [Channel Rate Limit] Channel %s (%s): request released after %v wait (%d remaining in queue)",
				channelKey, channelName, now.Sub(req.queuedAt), len(state.queue))

			state.mu.Unlock()

		case <-crl.stopChan:
			// Drain queue with errors
			state := crl.getOrCreateState(channelKey)
			state.mu.Lock()
			for _, req := range state.queue {
				req.err = fmt.Errorf("rate limiter stopped")
				close(req.done)
			}
			state.queue = nil
			state.releasing = false
			state.mu.Unlock()
			return
		}
	}
}

// removeFromQueue removes a request from the queue (called when client disconnects)
func (crl *ChannelRateLimiter) removeFromQueue(channelKey string, req *queuedRequest) {
	state := crl.getOrCreateState(channelKey)
	state.mu.Lock()
	defer state.mu.Unlock()

	for i, r := range state.queue {
		if r == req {
			state.queue = append(state.queue[:i], state.queue[i+1:]...)
			break
		}
	}
}

// GetQueueStatus returns the current queue status for a channel
func (crl *ChannelRateLimiter) GetQueueStatus(channelKey string, rpm int) (current int, max int, windowResetIn time.Duration) {
	crl.mu.Lock()
	state, exists := crl.channels[channelKey]
	crl.mu.Unlock()

	if !exists {
		return 0, rpm, 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	current = len(state.queue)
	max = rpm
	if time.Now().Before(state.windowEnd) {
		windowResetIn = time.Until(state.windowEnd)
	}
	return
}

// GetChannelStats returns rate limit statistics for a channel
func (crl *ChannelRateLimiter) GetChannelStats(channelKey string) (count int, windowEnd time.Time, queueLen int) {
	crl.mu.Lock()
	state, exists := crl.channels[channelKey]
	crl.mu.Unlock()

	if !exists {
		return 0, time.Time{}, 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	return state.count, state.windowEnd, len(state.queue)
}

// ClearChannel removes rate limit state for a specific channel index
// Call this when a channel is deleted to prevent state leakage to new channels at the same index
func (crl *ChannelRateLimiter) ClearChannel(channelIndex int) {
	crl.mu.Lock()
	defer crl.mu.Unlock()

	// Clear messages/responses/gemini channel states
	messagesKey := fmt.Sprintf("messages:%d", channelIndex)
	responsesKey := fmt.Sprintf("responses:%d", channelIndex)
	geminiKey := fmt.Sprintf("gemini:%d", channelIndex)

	if state, exists := crl.channels[messagesKey]; exists {
		state.mu.Lock()
		// Signal any queued requests to fail
		for _, req := range state.queue {
			req.err = fmt.Errorf("channel deleted")
			close(req.done)
		}
		state.queue = nil
		state.mu.Unlock()
		delete(crl.channels, messagesKey)
	}

	if state, exists := crl.channels[responsesKey]; exists {
		state.mu.Lock()
		// Signal any queued requests to fail
		for _, req := range state.queue {
			req.err = fmt.Errorf("channel deleted")
			close(req.done)
		}
		state.queue = nil
		state.mu.Unlock()
		delete(crl.channels, responsesKey)
	}

	if state, exists := crl.channels[geminiKey]; exists {
		state.mu.Lock()
		// Signal any queued requests to fail
		for _, req := range state.queue {
			req.err = fmt.Errorf("channel deleted")
			close(req.done)
		}
		state.queue = nil
		state.mu.Unlock()
		delete(crl.channels, geminiKey)
	}
}
