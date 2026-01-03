package config

import (
	"strconv"
	"strings"
	"sync"
)

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

// findMatchingRule 找到匹配给定状态码的规则
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
			if strings.TrimSpace(code) == statusStr {
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

// ShouldFailover 检查是否应该触发故障转移
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

	// 更新计数器
	key := makeKey(channelIdx, apiKey)

	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.counters[key] == nil {
		ft.counters[key] = make(map[string]int)
	}

	ft.counters[key][errorGroup]++
	count := ft.counters[key][errorGroup]

	// 检查是否达到阈值
	if count >= rule.Threshold {
		// 达到阈值，清除该组计数并返回 true
		delete(ft.counters[key], errorGroup)
		return true, isQuotaRelated
	}

	// 未达到阈值
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
