package middleware

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

// rateLimitEntry records request count for a single client
type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

// RateLimitInfo contains rate limit status information
type RateLimitInfo struct {
	Allowed   bool
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// RateLimiter is a dynamic rate limiter that supports hot-reload configuration
type RateLimiter struct {
	mu       sync.RWMutex
	entries  map[string]*rateLimitEntry
	window   time.Duration
	maxReqs  int
	enabled  bool
	stopChan chan struct{}
}

// NewRateLimiterWithConfig creates a rate limiter with the given configuration
func NewRateLimiterWithConfig(cfg ratelimit.EndpointRateLimit) *RateLimiter {
	rl := &RateLimiter{
		entries:  make(map[string]*rateLimitEntry),
		window:   time.Minute, // Fixed 1-minute window for RPM
		maxReqs:  cfg.RequestsPerMinute,
		enabled:  cfg.Enabled,
		stopChan: make(chan struct{}),
	}

	go rl.cleanup()
	return rl
}

// UpdateConfig updates the rate limiter configuration dynamically
func (rl *RateLimiter) UpdateConfig(cfg ratelimit.EndpointRateLimit) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.maxReqs = cfg.RequestsPerMinute
	rl.enabled = cfg.Enabled
	log.Printf("🔄 Rate limiter config updated: enabled=%v, rpm=%d", cfg.Enabled, cfg.RequestsPerMinute)
}

// cleanup periodically removes expired rate limit entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, entry := range rl.entries {
				if now.After(entry.windowEnd) {
					delete(rl.entries, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopChan:
			return
		}
	}
}

// Stop stops the rate limiter
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// getClientKey returns the client identifier
// Prioritizes API Key name, falls back to IP address
func getClientKey(c *gin.Context) string {
	if keyName, exists := c.Get(ContextKeyAPIKeyName); exists {
		if name, ok := keyName.(string); ok && name != "" {
			return "key:" + name
		}
	}
	return "ip:" + c.ClientIP()
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow(clientKey string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if !rl.enabled || rl.maxReqs <= 0 {
		return true
	}

	now := time.Now()
	entry, exists := rl.entries[clientKey]

	if !exists || now.After(entry.windowEnd) {
		rl.entries[clientKey] = &rateLimitEntry{
			count:     1,
			windowEnd: now.Add(rl.window),
		}
		return true
	}

	if entry.count >= rl.maxReqs {
		return false
	}

	entry.count++
	return true
}

// AllowWithCustomLimit checks if a request is allowed with a custom per-key limit
// If customRPM is 0, uses the default limit
func (rl *RateLimiter) AllowWithCustomLimit(clientKey string, customRPM int) bool {
	info := rl.CheckWithCustomLimit(clientKey, customRPM)
	return info.Allowed
}

// CheckWithCustomLimit checks rate limit and returns detailed info
func (rl *RateLimiter) CheckWithCustomLimit(clientKey string, customRPM int) RateLimitInfo {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if !rl.enabled {
		return RateLimitInfo{Allowed: true, Limit: 0, Remaining: 0}
	}

	// Determine the effective limit
	effectiveLimit := rl.maxReqs
	if customRPM > 0 {
		effectiveLimit = customRPM
	}
	if effectiveLimit <= 0 {
		return RateLimitInfo{Allowed: true, Limit: 0, Remaining: 0}
	}

	now := time.Now()
	entry, exists := rl.entries[clientKey]

	if !exists || now.After(entry.windowEnd) {
		windowEnd := now.Add(rl.window)
		rl.entries[clientKey] = &rateLimitEntry{
			count:     1,
			windowEnd: windowEnd,
		}
		return RateLimitInfo{
			Allowed:   true,
			Limit:     effectiveLimit,
			Remaining: effectiveLimit - 1,
			ResetAt:   windowEnd,
		}
	}

	if entry.count >= effectiveLimit {
		return RateLimitInfo{
			Allowed:   false,
			Limit:     effectiveLimit,
			Remaining: 0,
			ResetAt:   entry.windowEnd,
		}
	}

	entry.count++
	return RateLimitInfo{
		Allowed:   true,
		Limit:     effectiveLimit,
		Remaining: effectiveLimit - entry.count,
		ResetAt:   entry.windowEnd,
	}
}

// RateLimitMiddleware creates a rate limit middleware for the given limiter
func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl == nil {
			c.Next()
			return
		}

		clientKey := getClientKey(c)

		if !rl.Allow(clientKey) {
			log.Printf("🚫 [Rate Limit] Client %s exceeded request limit", clientKey)
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "Request rate limit exceeded, please try again later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// APIRateLimitMiddleware creates a rate limit middleware for API endpoints (/v1/*)
// Supports per-key rate limits from ValidatedKey and adds rate limit headers
func APIRateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl == nil {
			c.Next()
			return
		}

		clientKey := getClientKey(c)

		// Check for per-key rate limit (set by auth middleware)
		customRPM := 0
		if rpm, exists := c.Get(ContextKeyRateLimitRPM); exists {
			if rpmVal, ok := rpm.(int); ok {
				customRPM = rpmVal
			}
		}

		info := rl.CheckWithCustomLimit(clientKey, customRPM)

		// Add rate limit headers (RFC 6585 style)
		if info.Limit > 0 {
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", info.Limit))
			c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", info.Remaining))
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", info.ResetAt.Unix()))
		}

		if !info.Allowed {
			log.Printf("🚫 [API Rate Limit] Client %s exceeded request limit (custom=%d)", clientKey, customRPM)
			c.Header("Retry-After", fmt.Sprintf("%d", int(time.Until(info.ResetAt).Seconds())+1))
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "Request rate limit exceeded, please try again later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PortalRateLimitMiddleware creates a rate limit middleware for portal endpoints (/api/*)
func PortalRateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl == nil {
			c.Next()
			return
		}

		// Only apply to /api/* paths
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}

		clientKey := getClientKey(c)

		if !rl.Allow(clientKey) {
			log.Printf("🚫 [Portal Rate Limit] Client %s exceeded request limit", clientKey)
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "Request rate limit exceeded, please try again later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthFailureRateLimiter handles rate limiting for authentication failures
type AuthFailureRateLimiter struct {
	mu         sync.RWMutex
	failures   map[string]*authFailureEntry
	thresholds []ratelimit.AuthFailureThreshold
	enabled    bool
	stopChan   chan struct{}
}

type authFailureEntry struct {
	count    int
	blockEnd time.Time
	lastFail time.Time
}

// NewAuthFailureRateLimiterWithConfig creates an auth failure rate limiter with config
func NewAuthFailureRateLimiterWithConfig(cfg ratelimit.AuthFailureConfig) *AuthFailureRateLimiter {
	arl := &AuthFailureRateLimiter{
		failures:   make(map[string]*authFailureEntry),
		thresholds: cfg.Thresholds,
		enabled:    cfg.Enabled,
		stopChan:   make(chan struct{}),
	}

	go arl.cleanup()
	return arl
}

// UpdateConfig updates the auth failure limiter configuration
func (arl *AuthFailureRateLimiter) UpdateConfig(cfg ratelimit.AuthFailureConfig) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	arl.thresholds = cfg.Thresholds
	arl.enabled = cfg.Enabled
	log.Printf("🔄 Auth failure limiter config updated: enabled=%v, thresholds=%d", cfg.Enabled, len(cfg.Thresholds))
}

// cleanup removes expired entries
func (arl *AuthFailureRateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			arl.mu.Lock()
			now := time.Now()
			for key, entry := range arl.failures {
				if now.Sub(entry.lastFail) > 1*time.Hour {
					delete(arl.failures, key)
				}
			}
			arl.mu.Unlock()
		case <-arl.stopChan:
			return
		}
	}
}

// Stop stops the limiter
func (arl *AuthFailureRateLimiter) Stop() {
	close(arl.stopChan)
}

// RecordFailure records an authentication failure
func (arl *AuthFailureRateLimiter) RecordFailure(clientIP string) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	if !arl.enabled {
		return
	}

	now := time.Now()
	entry, exists := arl.failures[clientIP]

	if !exists {
		arl.failures[clientIP] = &authFailureEntry{
			count:    1,
			lastFail: now,
		}
		return
	}

	entry.count++
	entry.lastFail = now

	// Apply thresholds (sorted by failures descending for proper matching)
	for i := len(arl.thresholds) - 1; i >= 0; i-- {
		threshold := arl.thresholds[i]
		if entry.count >= threshold.Failures {
			entry.blockEnd = now.Add(time.Duration(threshold.BlockMinutes) * time.Minute)
			log.Printf("🔒 [Brute Force Protection] IP %s blocked for %d minutes (failures: %d)",
				clientIP, threshold.BlockMinutes, entry.count)
			break
		}
	}
}

// IsBlocked checks if an IP is blocked
func (arl *AuthFailureRateLimiter) IsBlocked(clientIP string) bool {
	arl.mu.RLock()
	defer arl.mu.RUnlock()

	if !arl.enabled {
		return false
	}

	entry, exists := arl.failures[clientIP]
	if !exists {
		return false
	}

	return time.Now().Before(entry.blockEnd)
}

// ClearFailures clears failure records for an IP (called on successful auth)
func (arl *AuthFailureRateLimiter) ClearFailures(clientIP string) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	delete(arl.failures, clientIP)
}
