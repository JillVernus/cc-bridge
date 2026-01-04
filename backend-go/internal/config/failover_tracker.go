package config

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// RetryAction defines what the handler should do after a failed request
type RetryAction int

const (
	ActionNone           RetryAction = iota // Return error to client (no retry)
	ActionFailoverKey                       // Switch to next key/channel
	ActionRetrySameKey                      // Wait and retry with same key
	ActionSuspendChan                       // Suspend channel until quota reset
)

// String returns a human-readable name for the action
func (a RetryAction) String() string {
	switch a {
	case ActionFailoverKey:
		return "failover_key"
	case ActionRetrySameKey:
		return "retry_same_key"
	case ActionSuspendChan:
		return "suspend_channel"
	default:
		return "none"
	}
}

// FailoverDecision contains the complete decision for handlers
type FailoverDecision struct {
	Action          RetryAction   // What action to take
	Wait            time.Duration // How long to wait (for RetrySameKey)
	MarkKeyFailed   bool          // Should mark key as failed in config manager
	DeprioritizeKey bool          // Should deprioritize key (quota-related)
	SuspendChannel  bool          // Should suspend the entire channel
	Reason          string        // For logging: "quota_exhausted", "model_cooldown", etc.
}

// FailoverTracker 跟踪每个渠道/API key 组合的连续错误计数
type FailoverTracker struct {
	mu       sync.RWMutex
	counters map[string]map[string]int // key: "channelIdx:apiKey", value: map[errorCodeGroup]count
}

// NewFailoverTracker 创建新的故障转移跟踪器
func NewFailoverTracker() *FailoverTracker {
	return &FailoverTracker{
		counters: make(map[string]map[string]int),
	}
}

// makeKey 生成渠道+apiKey的组合键
func makeKey(channelIdx int, apiKey string) string {
	return strconv.Itoa(channelIdx) + ":" + apiKey
}

// MatchRule finds the best matching rule for a given error pattern
// It tries specific patterns first (e.g., "429:QUOTA_EXHAUSTED") then falls back to general patterns (e.g., "429")
// Returns the matched rule and the error group key for counter tracking
func MatchRule(parsedError *ParsedError, rules []FailoverRule) (*FailoverRule, string) {
	if parsedError == nil {
		return nil, ""
	}

	statusStr := intToStr(parsedError.StatusCode)
	specificPattern := parsedError.ErrorCodePattern() // e.g., "429:QUOTA_EXHAUSTED"

	// Phase 1: Try to match specific pattern (e.g., "429:QUOTA_EXHAUSTED")
	if parsedError.Subtype != "" {
		for i := range rules {
			rule := &rules[i]
			if rule.ErrorCodes == "others" {
				continue
			}
			codes := strings.Split(rule.ErrorCodes, ",")
			for _, code := range codes {
				if strings.TrimSpace(code) == specificPattern {
					return rule, specificPattern
				}
			}
		}
	}

	// Phase 2: Try to match general pattern (e.g., "429")
	for i := range rules {
		rule := &rules[i]
		if rule.ErrorCodes == "others" {
			continue
		}
		codes := strings.Split(rule.ErrorCodes, ",")
		for _, code := range codes {
			if strings.TrimSpace(code) == statusStr {
				return rule, statusStr
			}
		}
	}

	// Phase 3: Fall back to "others" rule
	for i := range rules {
		rule := &rules[i]
		if rule.ErrorCodes == "others" {
			return rule, "others"
		}
	}

	return nil, ""
}

// DecideAction determines the action for any error based on the new rule system
// This is the main entry point for quota channels
func (ft *FailoverTracker) DecideAction(channelIdx int, apiKey string, statusCode int, respBody []byte, failoverConfig *FailoverConfig) FailoverDecision {
	// Parse the error to get subtype information
	parsed := ParseError(statusCode, respBody, failoverConfig)

	// Get rules (use defaults if not configured)
	rules := failoverConfig.Rules
	if len(rules) == 0 {
		rules = GetDefaultFailoverRules()
	}

	// Find matching rule
	rule, errorGroup := MatchRule(parsed, rules)
	if rule == nil {
		// No matching rule - don't failover
		return FailoverDecision{
			Action: ActionNone,
			Reason: "no_matching_rule",
		}
	}

	// Execute action based on rule type
	switch rule.Action {
	case ActionSuspendChannel:
		return FailoverDecision{
			Action:          ActionSuspendChan,
			Wait:            0,
			MarkKeyFailed:   false,
			DeprioritizeKey: false,
			SuspendChannel:  true,
			Reason:          parsed.Subtype,
		}

	case ActionFailoverImmediate:
		return FailoverDecision{
			Action:          ActionFailoverKey,
			Wait:            0,
			MarkKeyFailed:   true,
			DeprioritizeKey: false,
			Reason:          "immediate_failover",
		}

	case ActionRetryWait:
		waitDuration := parsed.WaitDuration
		if rule.WaitSeconds > 0 {
			// Use configured wait time instead of response-based
			waitDuration = time.Duration(rule.WaitSeconds) * time.Second
		}
		return FailoverDecision{
			Action:          ActionRetrySameKey,
			Wait:            waitDuration,
			MarkKeyFailed:   false,
			DeprioritizeKey: false,
			Reason:          parsed.Subtype,
		}

	case ActionFailoverThreshold:
		return ft.decideWithThreshold(channelIdx, apiKey, errorGroup, rule.Threshold, parsed.Subtype)

	default:
		// Unknown action type - use threshold behavior with default threshold
		return ft.decideWithThreshold(channelIdx, apiKey, errorGroup, 1, "unknown_action")
	}
}

// decideWithThreshold handles threshold-based failover decisions
func (ft *FailoverTracker) decideWithThreshold(channelIdx int, apiKey string, errorGroup string, threshold int, reason string) FailoverDecision {
	if threshold <= 0 {
		threshold = 1
	}

	key := makeKey(channelIdx, apiKey)

	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.counters[key] == nil {
		ft.counters[key] = make(map[string]int)
	}

	ft.counters[key][errorGroup]++
	count := ft.counters[key][errorGroup]

	if count >= threshold {
		// Threshold reached - failover
		delete(ft.counters[key], errorGroup)
		return FailoverDecision{
			Action:          ActionFailoverKey,
			Wait:            0,
			MarkKeyFailed:   true,
			DeprioritizeKey: true,
			Reason:          reason + "_threshold_reached",
		}
	}

	// Threshold not reached - return error to client
	return FailoverDecision{
		Action: ActionNone,
		Wait:   0,
		Reason: reason + "_threshold_not_reached",
	}
}

// findMatchingRule 找到匹配给定状态码的规则 (LEGACY - kept for ShouldFailover compatibility)
// 返回匹配的规则和错误码组标识（用于计数器key）
func findMatchingRule(statusCode int, rules []FailoverRule) (*FailoverRule, string) {
	statusStr := strconv.Itoa(statusCode)

	for i := range rules {
		rule := &rules[i]
		if rule.ErrorCodes == "others" {
			continue // 最后处理 others
		}

		codes := strings.Split(rule.ErrorCodes, ",")
		for _, code := range codes {
			trimmed := strings.TrimSpace(code)
			// Only match plain status codes (not patterns with subtypes)
			if !strings.Contains(trimmed, ":") && trimmed == statusStr {
				return rule, rule.ErrorCodes
			}
		}
	}

	// 没有精确匹配，查找 "others" 规则
	for i := range rules {
		rule := &rules[i]
		if rule.ErrorCodes == "others" {
			return rule, "others"
		}
	}

	return nil, ""
}

// ShouldFailover 检查是否应该触发故障转移 (LEGACY - for backward compatibility)
// 返回 (shouldFailover, isQuotaRelated)
// 如果返回 true，表示已达到阈值，应该切换到下一个 key/channel
func (ft *FailoverTracker) ShouldFailover(channelIdx int, apiKey string, statusCode int, failoverConfig *FailoverConfig) (shouldFailover bool, isQuotaRelated bool) {
	// 检查是否为配额相关错误（用于特殊处理）
	isQuotaRelated = statusCode == 429

	// 如果未启用自定义故障转移规则，使用传统行为
	if failoverConfig == nil || !failoverConfig.Enabled {
		// 传统行为：429/401/403 立即触发故障转移
		if statusCode == 429 || statusCode == 401 || statusCode == 403 {
			return true, isQuotaRelated
		}
		return false, isQuotaRelated
	}

	rules := failoverConfig.Rules
	if len(rules) == 0 {
		// 没有规则，使用默认规则
		rules = GetDefaultFailoverRules()
	}

	// 找到匹配的规则
	rule, errorGroup := findMatchingRule(statusCode, rules)
	if rule == nil {
		// 没有匹配的规则，不触发故障转移
		return false, isQuotaRelated
	}

	// Handle different action types
	switch rule.Action {
	case ActionSuspendChannel, ActionFailoverImmediate:
		return true, isQuotaRelated
	case ActionRetryWait:
		return false, isQuotaRelated
	case ActionFailoverThreshold, "":
		// Threshold-based - use counter logic
		threshold := rule.Threshold
		if threshold <= 0 {
			threshold = 1
		}

		key := makeKey(channelIdx, apiKey)

		ft.mu.Lock()
		defer ft.mu.Unlock()

		if ft.counters[key] == nil {
			ft.counters[key] = make(map[string]int)
		}

		ft.counters[key][errorGroup]++
		count := ft.counters[key][errorGroup]

		if count >= threshold {
			delete(ft.counters[key], errorGroup)
			return true, isQuotaRelated
		}
		return false, isQuotaRelated
	}

	return false, isQuotaRelated
}

// ResetOnSuccess 成功响应时重置所有计数器
func (ft *FailoverTracker) ResetOnSuccess(channelIdx int, apiKey string) {
	key := makeKey(channelIdx, apiKey)

	ft.mu.Lock()
	defer ft.mu.Unlock()

	delete(ft.counters, key)
}

// ResetChannel 重置整个渠道的所有计数器（用于渠道被删除或重置时）
func (ft *FailoverTracker) ResetChannel(channelIdx int) {
	prefix := strconv.Itoa(channelIdx) + ":"

	ft.mu.Lock()
	defer ft.mu.Unlock()

	for key := range ft.counters {
		if strings.HasPrefix(key, prefix) {
			delete(ft.counters, key)
		}
	}
}

// GetErrorCount 获取当前错误计数（用于调试/监控）
func (ft *FailoverTracker) GetErrorCount(channelIdx int, apiKey string, errorGroup string) int {
	key := makeKey(channelIdx, apiKey)

	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if counts, ok := ft.counters[key]; ok {
		return counts[errorGroup]
	}
	return 0
}

// Decide429Action determines the action for a 429 error based on response body analysis.
// LEGACY: Kept for backward compatibility. Use DecideAction for new code.
func (ft *FailoverTracker) Decide429Action(channelIdx int, apiKey string, respBody []byte, failoverConfig *FailoverConfig) FailoverDecision {
	return ft.DecideAction(channelIdx, apiKey, 429, respBody, failoverConfig)
}
