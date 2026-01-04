package scheduler

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/session"
)

// SuspensionChecker interface for checking channel suspension status
type SuspensionChecker interface {
	IsChannelSuspended(channelID int, channelType string) (bool, time.Time, string)
}

// ChannelScheduler 多渠道调度器
type ChannelScheduler struct {
	mu                      sync.RWMutex
	configManager           *config.ConfigManager
	messagesMetricsManager  *metrics.MetricsManager // Messages 渠道指标
	responsesMetricsManager *metrics.MetricsManager // Responses 渠道指标
	traceAffinity           *session.TraceAffinityManager
	suspensionChecker       SuspensionChecker // For checking quota channel suspension
}

// NewChannelScheduler 创建多渠道调度器
func NewChannelScheduler(
	cfgManager *config.ConfigManager,
	messagesMetrics *metrics.MetricsManager,
	responsesMetrics *metrics.MetricsManager,
	traceAffinity *session.TraceAffinityManager,
) *ChannelScheduler {
	return &ChannelScheduler{
		configManager:           cfgManager,
		messagesMetricsManager:  messagesMetrics,
		responsesMetricsManager: responsesMetrics,
		traceAffinity:           traceAffinity,
	}
}

// SetSuspensionChecker sets the suspension checker (called after requestLogManager is initialized)
func (s *ChannelScheduler) SetSuspensionChecker(checker SuspensionChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suspensionChecker = checker
}

// getMetricsManager 根据类型获取对应的指标管理器
func (s *ChannelScheduler) getMetricsManager(isResponses bool) *metrics.MetricsManager {
	if isResponses {
		return s.responsesMetricsManager
	}
	return s.messagesMetricsManager
}

// isChannelSuspended checks if a quota channel is suspended
// Returns (isSuspended, suspendedUntil, reason)
func (s *ChannelScheduler) isChannelSuspended(channelIndex int, isResponses bool) (bool, time.Time, string) {
	if s.suspensionChecker == nil {
		return false, time.Time{}, ""
	}
	channelType := "messages"
	if isResponses {
		channelType = "responses"
	}
	return s.suspensionChecker.IsChannelSuspended(channelIndex, channelType)
}

// SelectionResult 渠道选择结果
type SelectionResult struct {
	Upstream     *config.UpstreamConfig
	ChannelIndex int
	Reason       string // 选择原因（用于日志）
}

// SelectChannel 选择最佳渠道
// 优先级: 促销期渠道 > Trace亲和（促销渠道失败时回退） > 渠道优先级顺序
// allowedChannels: API key 允许的渠道索引列表，nil 表示允许所有渠道
func (s *ChannelScheduler) SelectChannel(
	ctx context.Context,
	userID string,
	failedChannels map[int]bool,
	isResponses bool,
	allowedChannels []int,
) (*SelectionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 获取活跃渠道列表
	activeChannels := s.getActiveChannels(isResponses)
	if len(activeChannels) == 0 {
		return nil, fmt.Errorf("没有可用的活跃渠道")
	}

	// Filter by allowed channels if specified
	if len(allowedChannels) > 0 {
		activeChannels = s.filterByAllowedChannels(activeChannels, allowedChannels)
		if len(activeChannels) == 0 {
			return nil, fmt.Errorf("no available channel (allowed channels: %v)", allowedChannels)
		}
	}

	// 获取对应类型的指标管理器
	metricsManager := s.getMetricsManager(isResponses)

	// 检查是否启用了管理员故障转移设置（如果启用则禁用电路断路器）
	cfg := s.configManager.GetConfig()
	useCircuitBreaker := !cfg.Failover.Enabled // Failover.Enabled=true 时禁用电路断路器

	// 0. 检查促销期渠道（最高优先级）
	promotedChannel := s.findPromotedChannel(activeChannels, isResponses)
	if promotedChannel != nil && !failedChannels[promotedChannel.Index] {
		// 检查是否被暂停
		if suspended, until, reason := s.isChannelSuspended(promotedChannel.Index, isResponses); suspended {
			log.Printf("⏸️ 促销渠道 [%d] %s 被暂停 (原因: %s, 恢复: %s)", promotedChannel.Index, promotedChannel.Name, reason, until.Format(time.RFC3339))
		} else if !useCircuitBreaker || metricsManager.IsChannelHealthy(promotedChannel.Index) {
			// 促销渠道存在且未失败，检查是否健康（仅当电路断路器启用时）
			upstream := s.getUpstreamByIndex(promotedChannel.Index, isResponses)
			if upstream != nil && len(upstream.APIKeys) > 0 {
				log.Printf("🎉 促销期优先选择渠道: [%d] %s (user: %s)", promotedChannel.Index, upstream.Name, maskUserID(userID))
				return &SelectionResult{
					Upstream:     upstream,
					ChannelIndex: promotedChannel.Index,
					Reason:       "promotion_priority",
				}, nil
			} else if upstream != nil {
				log.Printf("⚠️ 促销渠道 [%d] %s 无可用密钥，跳过", promotedChannel.Index, upstream.Name)
			}
		} else {
			log.Printf("⚠️ 促销渠道 [%d] %s 不健康，跳过", promotedChannel.Index, promotedChannel.Name)
		}
	} else if promotedChannel != nil {
		log.Printf("⚠️ 促销渠道 [%d] %s 已在本次请求中失败，跳过", promotedChannel.Index, promotedChannel.Name)
	}

	// 1. 检查 Trace 亲和性（促销渠道失败时或无促销渠道时）
	if userID != "" {
		if preferredIdx, ok := s.traceAffinity.GetPreferredChannel(userID); ok {
			for _, ch := range activeChannels {
				if ch.Index == preferredIdx && !failedChannels[preferredIdx] {
					// 检查渠道状态：只有 active 状态才使用亲和性
					if ch.Status != "active" {
						log.Printf("⏸️ 跳过亲和渠道 [%d] %s: 状态为 %s (user: %s)", preferredIdx, ch.Name, ch.Status, maskUserID(userID))
						continue
					}
					// 检查是否被暂停
					if suspended, until, reason := s.isChannelSuspended(preferredIdx, isResponses); suspended {
						log.Printf("⏸️ 跳过亲和渠道 [%d] %s: 被暂停 (原因: %s, 恢复: %s, user: %s)", preferredIdx, ch.Name, reason, until.Format(time.RFC3339), maskUserID(userID))
						continue
					}
					// 检查渠道是否健康（仅当电路断路器启用时）
					if !useCircuitBreaker || metricsManager.IsChannelHealthy(preferredIdx) {
						upstream := s.getUpstreamByIndex(preferredIdx, isResponses)
						if upstream != nil {
							log.Printf("🎯 Trace亲和选择渠道: [%d] %s (user: %s)", preferredIdx, upstream.Name, maskUserID(userID))
							return &SelectionResult{
								Upstream:     upstream,
								ChannelIndex: preferredIdx,
								Reason:       "trace_affinity",
							}, nil
						}
					}
				}
			}
		}
	}

	// 2. 按优先级遍历活跃渠道
	for _, ch := range activeChannels {
		// 跳过本次请求已经失败的渠道
		if failedChannels[ch.Index] {
			continue
		}

		// 跳过非 active 状态的渠道（suspended 等）
		if ch.Status != "active" {
			log.Printf("⏸️ 跳过非活跃渠道: [%d] %s (状态: %s)", ch.Index, ch.Name, ch.Status)
			continue
		}

		// 跳过失败率过高的渠道（已熔断或即将熔断）- 仅当电路断路器启用时
		if useCircuitBreaker && !metricsManager.IsChannelHealthy(ch.Index) {
			log.Printf("⚠️ 跳过不健康渠道: [%d] %s (失败率: %.1f%%)",
				ch.Index, ch.Name, metricsManager.CalculateFailureRate(ch.Index)*100)
			continue
		}

		// 跳过被暂停的配额渠道（因配额耗尽等原因）
		if suspended, until, reason := s.isChannelSuspended(ch.Index, isResponses); suspended {
			log.Printf("⏸️ 跳过暂停渠道: [%d] %s (原因: %s, 恢复时间: %s)",
				ch.Index, ch.Name, reason, until.Format(time.RFC3339))
			continue
		}

		upstream := s.getUpstreamByIndex(ch.Index, isResponses)
		if upstream != nil && len(upstream.APIKeys) > 0 {
			log.Printf("✅ 选择渠道: [%d] %s (优先级: %d)", ch.Index, upstream.Name, ch.Priority)
			return &SelectionResult{
				Upstream:     upstream,
				ChannelIndex: ch.Index,
				Reason:       "priority_order",
			}, nil
		}
	}

	// 3. 所有健康渠道都失败，选择失败率最低的作为降级
	return s.selectFallbackChannel(activeChannels, failedChannels, isResponses)
}

// findPromotedChannel 查找处于促销期的渠道
func (s *ChannelScheduler) findPromotedChannel(activeChannels []ChannelInfo, isResponses bool) *ChannelInfo {
	for i := range activeChannels {
		ch := &activeChannels[i]
		if ch.Status != "active" {
			continue
		}
		upstream := s.getUpstreamByIndex(ch.Index, isResponses)
		if upstream != nil {
			if config.IsChannelInPromotion(upstream) {
				log.Printf("🎉 找到促销渠道: [%d] %s (promotionUntil: %v)", ch.Index, upstream.Name, upstream.PromotionUntil)
				return ch
			}
		}
	}
	return nil
}

// filterByAllowedChannels filters channels to only those in the allowed list
func (s *ChannelScheduler) filterByAllowedChannels(channels []ChannelInfo, allowed []int) []ChannelInfo {
	if len(allowed) == 0 {
		return channels
	}
	allowedSet := make(map[int]bool)
	for _, idx := range allowed {
		allowedSet[idx] = true
	}
	var filtered []ChannelInfo
	for _, ch := range channels {
		if allowedSet[ch.Index] {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

// selectFallbackChannel 选择降级渠道（失败率最低的）
func (s *ChannelScheduler) selectFallbackChannel(
	activeChannels []ChannelInfo,
	failedChannels map[int]bool,
	isResponses bool,
) (*SelectionResult, error) {
	metricsManager := s.getMetricsManager(isResponses)
	var bestChannel *ChannelInfo
	bestFailureRate := float64(2) // 初始化为不可能的值

	for i := range activeChannels {
		ch := &activeChannels[i]
		if failedChannels[ch.Index] {
			continue
		}
		// 跳过非 active 状态的渠道
		if ch.Status != "active" {
			continue
		}

		failureRate := metricsManager.CalculateFailureRate(ch.Index)
		if failureRate < bestFailureRate {
			bestFailureRate = failureRate
			bestChannel = ch
		}
	}

	if bestChannel != nil {
		upstream := s.getUpstreamByIndex(bestChannel.Index, isResponses)
		if upstream != nil {
			log.Printf("⚠️ 降级选择渠道: [%d] %s (失败率: %.1f%%)",
				bestChannel.Index, upstream.Name, bestFailureRate*100)
			return &SelectionResult{
				Upstream:     upstream,
				ChannelIndex: bestChannel.Index,
				Reason:       "fallback",
			}, nil
		}
	}

	return nil, fmt.Errorf("所有渠道都不可用")
}

// ChannelInfo 渠道信息（用于排序）
type ChannelInfo struct {
	Index    int
	Name     string
	Priority int
	Status   string
}

// getActiveChannels 获取活跃渠道列表（按优先级排序）
func (s *ChannelScheduler) getActiveChannels(isResponses bool) []ChannelInfo {
	cfg := s.configManager.GetConfig()

	var upstreams []config.UpstreamConfig
	if isResponses {
		upstreams = cfg.ResponsesUpstream
	} else {
		upstreams = cfg.Upstream
	}

	// 筛选活跃渠道
	var activeChannels []ChannelInfo
	for i, upstream := range upstreams {
		status := upstream.Status
		if status == "" {
			status = "active" // 默认为活跃
		}

		// 只选择 active 状态的渠道（suspended 也算在活跃序列中，但会被健康检查过滤）
		if status != "disabled" {
			priority := upstream.Priority
			if priority == 0 {
				priority = i // 默认优先级为索引
			}

			activeChannels = append(activeChannels, ChannelInfo{
				Index:    i,
				Name:     upstream.Name,
				Priority: priority,
				Status:   status,
			})
		}
	}

	// 按优先级排序（数字越小优先级越高）
	sort.Slice(activeChannels, func(i, j int) bool {
		return activeChannels[i].Priority < activeChannels[j].Priority
	})

	return activeChannels
}

// getUpstreamByIndex 根据索引获取上游配置
// 注意：返回的是副本，避免指向 slice 元素的指针在 slice 重分配后失效
func (s *ChannelScheduler) getUpstreamByIndex(index int, isResponses bool) *config.UpstreamConfig {
	cfg := s.configManager.GetConfig()

	var upstreams []config.UpstreamConfig
	if isResponses {
		upstreams = cfg.ResponsesUpstream
	} else {
		upstreams = cfg.Upstream
	}

	if index >= 0 && index < len(upstreams) {
		// 返回副本，避免返回指向 slice 元素的指针
		upstream := upstreams[index]
		return &upstream
	}
	return nil
}

// RecordSuccess 记录渠道成功
func (s *ChannelScheduler) RecordSuccess(channelIndex int, isResponses bool) {
	s.getMetricsManager(isResponses).RecordSuccess(channelIndex)
}

// RecordFailure 记录渠道失败
func (s *ChannelScheduler) RecordFailure(channelIndex int, isResponses bool) {
	s.getMetricsManager(isResponses).RecordFailure(channelIndex)
}

// SetTraceAffinity 设置 Trace 亲和
func (s *ChannelScheduler) SetTraceAffinity(userID string, channelIndex int) {
	if userID != "" {
		s.traceAffinity.SetPreferredChannel(userID, channelIndex)
	}
}

// UpdateTraceAffinity 更新 Trace 亲和时间（续期）
func (s *ChannelScheduler) UpdateTraceAffinity(userID string) {
	if userID != "" {
		s.traceAffinity.UpdateLastUsed(userID)
	}
}

// GetMessagesMetricsManager 获取 Messages 渠道指标管理器
func (s *ChannelScheduler) GetMessagesMetricsManager() *metrics.MetricsManager {
	return s.messagesMetricsManager
}

// GetResponsesMetricsManager 获取 Responses 渠道指标管理器
func (s *ChannelScheduler) GetResponsesMetricsManager() *metrics.MetricsManager {
	return s.responsesMetricsManager
}

// GetTraceAffinityManager 获取 Trace 亲和性管理器
func (s *ChannelScheduler) GetTraceAffinityManager() *session.TraceAffinityManager {
	return s.traceAffinity
}

// ResetChannelMetrics 重置渠道指标（用于恢复熔断）
func (s *ChannelScheduler) ResetChannelMetrics(channelIndex int, isResponses bool) {
	s.getMetricsManager(isResponses).Reset(channelIndex)
}

// GetActiveChannelCount 获取活跃渠道数量
func (s *ChannelScheduler) GetActiveChannelCount(isResponses bool) int {
	return len(s.getActiveChannels(isResponses))
}

// IsMultiChannelMode 判断是否为多渠道模式
func (s *ChannelScheduler) IsMultiChannelMode(isResponses bool) bool {
	return s.GetActiveChannelCount(isResponses) > 1
}

// maskUserID 掩码 user_id（保护隐私）
func maskUserID(userID string) string {
	if len(userID) <= 16 {
		return "***"
	}
	return userID[:8] + "***" + userID[len(userID)-4:]
}
