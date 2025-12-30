package middleware

import (
	"log"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// rateLimitEntry 记录单个客户端的请求计数
type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

// RateLimiter 速率限制器
type RateLimiter struct {
	mu       sync.RWMutex
	entries  map[string]*rateLimitEntry
	window   time.Duration
	maxReqs  int
	enabled  bool
	stopChan chan struct{}
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(envCfg *config.EnvConfig) *RateLimiter {
	rl := &RateLimiter{
		entries:  make(map[string]*rateLimitEntry),
		window:   time.Duration(envCfg.RateLimitWindow) * time.Millisecond,
		maxReqs:  envCfg.RateLimitMaxRequests,
		enabled:  envCfg.EnableRateLimit,
		stopChan: make(chan struct{}),
	}

	// 启动清理过期条目的 goroutine
	go rl.cleanup()

	return rl
}

// cleanup 定期清理过期的速率限制条目
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

// Stop 停止速率限制器
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// getClientKey 获取客户端标识
// 优先使用 API Key hash，其次使用 IP 地址
func getClientKey(c *gin.Context) string {
	// 优先使用 API Key（如果已验证）
	if keyName, exists := c.Get(ContextKeyAPIKeyName); exists {
		if name, ok := keyName.(string); ok && name != "" {
			return "key:" + name
		}
	}

	// 回退到 IP 地址
	return "ip:" + c.ClientIP()
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow(clientKey string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[clientKey]

	if !exists || now.After(entry.windowEnd) {
		// 新窗口
		rl.entries[clientKey] = &rateLimitEntry{
			count:     1,
			windowEnd: now.Add(rl.window),
		}
		return true
	}

	// 在当前窗口内
	if entry.count >= rl.maxReqs {
		return false
	}

	entry.count++
	return true
}

// RateLimitMiddleware 速率限制中间件
// 应用于所有需要保护的端点
func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl == nil || !rl.enabled {
			c.Next()
			return
		}

		clientKey := getClientKey(c)

		if !rl.Allow(clientKey) {
			log.Printf("🚫 [速率限制] 客户端 %s 超出请求限制", clientKey)
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "请求过于频繁，请稍后再试",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthFailureRateLimiter 认证失败专用速率限制器
// 对认证失败的请求进行更严格的限制，防止暴力破解
type AuthFailureRateLimiter struct {
	mu       sync.RWMutex
	failures map[string]*authFailureEntry
	stopChan chan struct{}
}

type authFailureEntry struct {
	count     int
	blockEnd  time.Time
	lastFail  time.Time
}

// NewAuthFailureRateLimiter 创建认证失败速率限制器
func NewAuthFailureRateLimiter() *AuthFailureRateLimiter {
	arl := &AuthFailureRateLimiter{
		failures: make(map[string]*authFailureEntry),
		stopChan: make(chan struct{}),
	}

	go arl.cleanup()
	return arl
}

// cleanup 清理过期条目
func (arl *AuthFailureRateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			arl.mu.Lock()
			now := time.Now()
			for key, entry := range arl.failures {
				// 清理超过 1 小时未活动的条目
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

// Stop 停止限制器
func (arl *AuthFailureRateLimiter) Stop() {
	close(arl.stopChan)
}

// RecordFailure 记录认证失败
func (arl *AuthFailureRateLimiter) RecordFailure(clientIP string) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

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

	// 阶梯式封禁：
	// 5 次失败 -> 封禁 1 分钟
	// 10 次失败 -> 封禁 5 分钟
	// 20 次失败 -> 封禁 30 分钟
	switch {
	case entry.count >= 20:
		entry.blockEnd = now.Add(30 * time.Minute)
		log.Printf("🔒 [暴力破解防护] IP %s 已被封禁 30 分钟 (失败 %d 次)", clientIP, entry.count)
	case entry.count >= 10:
		entry.blockEnd = now.Add(5 * time.Minute)
		log.Printf("🔒 [暴力破解防护] IP %s 已被封禁 5 分钟 (失败 %d 次)", clientIP, entry.count)
	case entry.count >= 5:
		entry.blockEnd = now.Add(1 * time.Minute)
		log.Printf("🔒 [暴力破解防护] IP %s 已被封禁 1 分钟 (失败 %d 次)", clientIP, entry.count)
	}
}

// IsBlocked 检查 IP 是否被封禁
func (arl *AuthFailureRateLimiter) IsBlocked(clientIP string) bool {
	arl.mu.RLock()
	defer arl.mu.RUnlock()

	entry, exists := arl.failures[clientIP]
	if !exists {
		return false
	}

	return time.Now().Before(entry.blockEnd)
}

// ClearFailures 清除某 IP 的失败记录（认证成功后调用）
func (arl *AuthFailureRateLimiter) ClearFailures(clientIP string) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	delete(arl.failures, clientIP)
}
