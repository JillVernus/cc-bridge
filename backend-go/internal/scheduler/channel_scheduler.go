package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/session"
)

// ErrNoAllowedChannels is returned when channel permission filtering removes all available channels.
// Handlers should return 403 Forbidden when they receive this error.
var ErrNoAllowedChannels = errors.New("no channels available after permission filtering")

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
	geminiMetricsManager    *metrics.MetricsManager // Gemini 渠道指标
	traceAffinity           *session.TraceAffinityManager
	suspensionChecker       SuspensionChecker // For checking quota channel suspension
	// Round-robin counters for channel-level load balancing
	messagesRoundRobinCounter  int
	responsesRoundRobinCounter int
	geminiRoundRobinCounter    int
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
		geminiMetricsManager:    metrics.NewMetricsManager(), // Create dedicated Gemini metrics
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

// upstreamHasCredentials reports whether the upstream can be used for requests.
// Composite channels are always usable (they route to other channels).
// Regular channels require API keys, except Responses channels of type "openai-oauth" which use OAuth tokens instead.
func (s *ChannelScheduler) upstreamHasCredentials(upstream *config.UpstreamConfig, isResponses bool) bool {
	if upstream == nil {
		return false
	}
	if config.IsCompositeChannel(upstream) {
		return true
	}
	if len(upstream.APIKeys) > 0 {
		return true
	}
	if isResponses && upstream.ServiceType == "openai-oauth" {
		return upstream.OAuthTokens != nil && strings.TrimSpace(upstream.OAuthTokens.AccessToken) != ""
	}
	return false
}

// SelectionResult 渠道选择结果
type SelectionResult struct {
	Upstream     *config.UpstreamConfig
	ChannelIndex int
	Reason       string // 选择原因（用于日志）

	// Composite channel fields (populated when routing through a composite channel)
	CompositeUpstream     *config.UpstreamConfig // The composite channel used for routing (nil if direct)
	CompositeChannelIndex int                    // Index of the composite channel (-1 if direct)
	ResolvedModel         string                 // The model name after composite resolution (may be overridden)
	FailoverChain         []string               // Remaining failover chain for sticky composite behavior
	FailoverChainIndex    int                    // Current position in failover chain (0 = primary, 1+ = failover)
}

// SelectChannel 选择最佳渠道
// 优先级: 促销期渠道 > Trace亲和（促销渠道失败时回退） > 渠道优先级顺序
// allowedChannels: API key 允许的渠道ID列表，nil 表示允许所有渠道
// model: The requested model name (used for composite channel routing)
func (s *ChannelScheduler) SelectChannel(
	ctx context.Context,
	userID string,
	failedChannels map[int]bool,
	isResponses bool,
	allowedChannels []string,
	model string,
) (*SelectionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 获取活跃渠道列表
	activeChannels := s.getActiveChannels(isResponses)
	if len(activeChannels) == 0 {
		return nil, fmt.Errorf("no active channels available")
	}

	// Filter by allowed channels if specified
	if len(allowedChannels) > 0 {
		activeChannels = s.filterByAllowedChannels(activeChannels, allowedChannels)
		if len(activeChannels) == 0 {
			return nil, fmt.Errorf("%w (allowed channels: %v)", ErrNoAllowedChannels, allowedChannels)
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
			if s.upstreamHasCredentials(upstream, isResponses) {
				log.Printf("🎉 促销期优先选择渠道: [%d] %s (user: %s)", promotedChannel.Index, upstream.Name, maskUserID(userID))
				result := &SelectionResult{
					Upstream:     upstream,
					ChannelIndex: promotedChannel.Index,
					Reason:       "promotion_priority",
				}
				resolved, err := s.resolveCompositeChannel(result, model, isResponses, failedChannels, metricsManager, useCircuitBreaker)
				if err == nil {
					return resolved, nil
				}
				log.Printf("⚠️ [Composite] Failed to resolve promoted channel [%d] %s: %v", promotedChannel.Index, upstream.Name, err)
				failedChannels[promotedChannel.Index] = true
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
							result := &SelectionResult{
								Upstream:     upstream,
								ChannelIndex: preferredIdx,
								Reason:       "trace_affinity",
							}
							resolved, err := s.resolveCompositeChannel(result, model, isResponses, failedChannels, metricsManager, useCircuitBreaker)
							if err == nil {
								return resolved, nil
							}
							log.Printf("⚠️ [Composite] Failed to resolve trace affinity channel [%d] %s: %v", preferredIdx, upstream.Name, err)
							failedChannels[preferredIdx] = true
						}
					}
				}
			}
		}
	}

	// 2. Build list of healthy candidate channels
	var healthyCandidates []ChannelInfo
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
		if s.upstreamHasCredentials(upstream, isResponses) {
			healthyCandidates = append(healthyCandidates, ch)
		}
	}

	if len(healthyCandidates) == 0 {
		// 3. 所有健康渠道都失败，选择失败率最低的作为降级
		return s.selectFallbackChannel(activeChannels, failedChannels, isResponses, model, metricsManager, useCircuitBreaker)
	}

	// 3. Apply channel selection strategy
	loadBalanceStrategy := cfg.LoadBalance
	if isResponses {
		loadBalanceStrategy = cfg.ResponsesLoadBalance
	}

	// Order candidates based on strategy
	orderedCandidates := s.orderChannelsByStrategy(healthyCandidates, loadBalanceStrategy, isResponses)

	// Try each candidate in order
	for _, ch := range orderedCandidates {
		upstream := s.getUpstreamByIndex(ch.Index, isResponses)
		if upstream == nil {
			continue
		}

		reason := "priority_order"
		switch loadBalanceStrategy {
		case "round-robin":
			reason = "round_robin"
		case "random":
			reason = "random"
		}

		log.Printf("✅ 选择渠道: [%d] %s (策略: %s)", ch.Index, upstream.Name, reason)
		result := &SelectionResult{
			Upstream:     upstream,
			ChannelIndex: ch.Index,
			Reason:       reason,
		}
		resolved, err := s.resolveCompositeChannel(result, model, isResponses, failedChannels, metricsManager, useCircuitBreaker)
		if err == nil {
			return resolved, nil
		}
		log.Printf("⚠️ [Composite] Failed to resolve channel [%d] %s: %v", ch.Index, upstream.Name, err)
		failedChannels[ch.Index] = true
	}

	// 4. 所有健康渠道都失败，选择失败率最低的作为降级
	return s.selectFallbackChannel(activeChannels, failedChannels, isResponses, model, metricsManager, useCircuitBreaker)
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

// orderChannelsByStrategy orders channels based on the load balancing strategy
// - failover: keep priority order (already sorted)
// - round-robin: rotate starting position based on counter
// - random: shuffle the channels
func (s *ChannelScheduler) orderChannelsByStrategy(channels []ChannelInfo, strategy string, isResponses bool) []ChannelInfo {
	if len(channels) <= 1 {
		return channels
	}

	// Make a copy to avoid modifying the original
	result := make([]ChannelInfo, len(channels))
	copy(result, channels)

	switch strategy {
	case "round-robin":
		// Get and increment the counter (need write lock for this)
		// Note: We're already holding RLock, so we need to be careful
		// For simplicity, use atomic-like behavior by reading counter value
		var counter int
		if isResponses {
			counter = s.responsesRoundRobinCounter
			s.responsesRoundRobinCounter++
		} else {
			counter = s.messagesRoundRobinCounter
			s.messagesRoundRobinCounter++
		}
		// Rotate the slice: start from counter % len position
		startIdx := counter % len(result)
		if startIdx > 0 {
			rotated := make([]ChannelInfo, len(result))
			copy(rotated, result[startIdx:])
			copy(rotated[len(result)-startIdx:], result[:startIdx])
			result = rotated
		}
		log.Printf("🔄 Round-robin: counter=%d, startIdx=%d, first=[%d] %s",
			counter, startIdx, result[0].Index, result[0].Name)

	case "random":
		// Shuffle using Fisher-Yates algorithm
		for i := len(result) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			result[i], result[j] = result[j], result[i]
		}
		log.Printf("🎲 Random: first=[%d] %s", result[0].Index, result[0].Name)

	default: // "failover" or any other value - keep priority order
		log.Printf("🔀 Failover: first=[%d] %s (priority: %d)", result[0].Index, result[0].Name, result[0].Priority)
	}

	return result
}

// filterByAllowedChannels filters channels to only those in the allowed list (by channel ID)
func (s *ChannelScheduler) filterByAllowedChannels(channels []ChannelInfo, allowed []string) []ChannelInfo {
	if len(allowed) == 0 {
		return channels
	}
	allowedSet := make(map[string]bool)
	for _, id := range allowed {
		allowedSet[id] = true
	}
	var filtered []ChannelInfo
	for _, ch := range channels {
		if allowedSet[ch.ID] {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

// resolveCompositeChannel resolves a composite channel to its target channel based on the model.
// If the selected channel is not composite, it returns the original result unchanged.
// If the selected channel is composite but the target is unavailable, returns an error.
// For composite channels, this finds the first available channel in the failover chain.
func (s *ChannelScheduler) resolveCompositeChannel(
	result *SelectionResult,
	model string,
	isResponses bool,
	failedChannels map[int]bool,
	metricsManager *metrics.MetricsManager,
	useCircuitBreaker bool,
) (*SelectionResult, error) {
	if result == nil || result.Upstream == nil {
		return result, nil
	}

	// Check if this is a composite channel
	if !config.IsCompositeChannel(result.Upstream) {
		// Not composite - return as-is with default composite fields
		result.CompositeChannelIndex = -1
		result.ResolvedModel = model
		return result, nil
	}

	// Get all upstreams for resolution
	cfg := s.configManager.GetConfig()
	var upstreams []config.UpstreamConfig
	if isResponses {
		upstreams = cfg.ResponsesUpstream
	} else {
		upstreams = cfg.Upstream
	}

	composite := result.Upstream
	compositeIndex := result.ChannelIndex

	// Find the mapping for this model pattern
	var matchedMapping *config.CompositeMapping
	for i := range composite.CompositeMappings {
		mapping := &composite.CompositeMappings[i]
		// Check if model contains the pattern (haiku, sonnet, opus)
		if strings.Contains(strings.ToLower(model), strings.ToLower(mapping.Pattern)) {
			matchedMapping = mapping
			break
		}
	}

	if matchedMapping == nil {
		return nil, fmt.Errorf("composite channel '%s' has no mapping for model '%s'", composite.Name, model)
	}

	// Build full channel chain: primary + failoverChain
	fullChain := append([]string{matchedMapping.TargetChannelID}, matchedMapping.FailoverChain...)

	// Determine resolved model (may be overridden by mapping)
	resolvedModel := model
	if matchedMapping.TargetModel != "" {
		resolvedModel = matchedMapping.TargetModel
	}

	// Try each channel in the chain until we find an available one
	// For composite targets, we skip status checks (composite decides routing)
	for chainIdx, channelID := range fullChain {
		targetIndex := -1
		for j := range upstreams {
			if upstreams[j].ID == channelID {
				targetIndex = j
				break
			}
		}

		if targetIndex < 0 || targetIndex >= len(upstreams) {
			log.Printf("⚠️ [Composite] '%s' → channel ID '%s' not found, skipping", composite.Name, channelID)
			continue
		}

		targetCopy := upstreams[targetIndex]
		target := &targetCopy

		// For composite channel targets, skip status/suspension/circuit-breaker checks
		// The composite channel decides routing, not the target's status
		if s.isTargetChannelAvailable(target, targetIndex, isResponses, failedChannels, metricsManager, useCircuitBreaker, true) {
			reason := result.Reason + "_via_composite"
			if chainIdx > 0 {
				reason = result.Reason + "_via_composite_failover"
			}
			log.Printf("🔀 [Composite] '%s' → target [%d] '%s' for model '%s' (chain pos: %d, resolved: '%s')",
				composite.Name, targetIndex, target.Name, model, chainIdx, resolvedModel)
			return &SelectionResult{
				Upstream:              target,
				ChannelIndex:          targetIndex,
				Reason:                reason,
				CompositeUpstream:     composite,
				CompositeChannelIndex: compositeIndex,
				ResolvedModel:         resolvedModel,
				FailoverChain:         fullChain[chainIdx+1:], // Remaining chain for retry
				FailoverChainIndex:    chainIdx,
			}, nil
		}
		log.Printf("⚠️ [Composite] '%s' → target [%d] '%s' unavailable (no API keys or already failed), trying next in chain",
			composite.Name, targetIndex, target.Name)
	}

	// All channels in chain exhausted
	return nil, fmt.Errorf("composite channel '%s' exhausted all failover channels for model '%s'", composite.Name, model)
}

// GetNextCompositeFailover returns the next channel in the composite failover chain
// This is called when the current target fails and we need to try the next one
func (s *ChannelScheduler) GetNextCompositeFailover(
	prevResult *SelectionResult,
	isResponses bool,
	failedChannels map[int]bool,
	metricsManager *metrics.MetricsManager,
	useCircuitBreaker bool,
) (*SelectionResult, error) {
	if prevResult == nil || prevResult.CompositeUpstream == nil {
		return nil, fmt.Errorf("not a composite channel result")
	}

	if len(prevResult.FailoverChain) == 0 {
		return nil, fmt.Errorf("composite failover chain exhausted")
	}

	// Get all upstreams
	cfg := s.configManager.GetConfig()
	var upstreams []config.UpstreamConfig
	if isResponses {
		upstreams = cfg.ResponsesUpstream
	} else {
		upstreams = cfg.Upstream
	}

	composite := prevResult.CompositeUpstream
	compositeIndex := prevResult.CompositeChannelIndex

	// Try remaining channels in the failover chain
	for i, channelID := range prevResult.FailoverChain {
		targetIndex := -1
		for j := range upstreams {
			if upstreams[j].ID == channelID {
				targetIndex = j
				break
			}
		}

		if targetIndex < 0 || targetIndex >= len(upstreams) {
			continue
		}

		targetCopy := upstreams[targetIndex]
		target := &targetCopy

		// Skip status checks for composite targets
		if s.isTargetChannelAvailable(target, targetIndex, isResponses, failedChannels, metricsManager, useCircuitBreaker, true) {
			chainPosition := prevResult.FailoverChainIndex + 1 + i
			log.Printf("🔀 [Composite Failover] '%s' → target [%d] '%s' (chain pos: %d)",
				composite.Name, targetIndex, target.Name, chainPosition)
			return &SelectionResult{
				Upstream:              target,
				ChannelIndex:          targetIndex,
				Reason:                "composite_failover",
				CompositeUpstream:     composite,
				CompositeChannelIndex: compositeIndex,
				ResolvedModel:         prevResult.ResolvedModel,
				FailoverChain:         prevResult.FailoverChain[i+1:],
				FailoverChainIndex:    chainPosition,
			}, nil
		}
	}

	return nil, fmt.Errorf("composite failover chain exhausted for '%s'", composite.Name)
}

// isTargetChannelAvailable checks if a target channel is available for use
// skipStatusCheck: when true, allows disabled/suspended channels (for composite failover chain)
func (s *ChannelScheduler) isTargetChannelAvailable(
	target *config.UpstreamConfig,
	targetIndex int,
	isResponses bool,
	failedChannels map[int]bool,
	metricsManager *metrics.MetricsManager,
	useCircuitBreaker bool,
	skipStatusCheck bool,
) bool {
	// Check if already failed in this request
	if failedChannels[targetIndex] {
		return false
	}

	// Check status (skip for composite failover chain targets)
	if !skipStatusCheck {
		status := config.GetChannelStatus(target)
		if status != "active" {
			return false
		}
	}

	// Check if suspended (skip for composite failover chain targets)
	if !skipStatusCheck {
		if suspended, _, _ := s.isChannelSuspended(targetIndex, isResponses); suspended {
			return false
		}
	}

	// Check circuit breaker health (skip for composite failover chain targets)
	if !skipStatusCheck && useCircuitBreaker && !metricsManager.IsChannelHealthy(targetIndex) {
		return false
	}

	// Check if has usable credentials
	if !s.upstreamHasCredentials(target, isResponses) {
		return false
	}

	return true
}

// selectFallbackChannel 选择降级渠道（失败率最低的）
func (s *ChannelScheduler) selectFallbackChannel(
	activeChannels []ChannelInfo,
	failedChannels map[int]bool,
	isResponses bool,
	model string,
	metricsManager *metrics.MetricsManager,
	useCircuitBreaker bool,
) (*SelectionResult, error) {
	var bestChannel *ChannelInfo
	var bestUpstream *config.UpstreamConfig
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
		// 跳过被暂停的配额渠道
		if suspended, _, _ := s.isChannelSuspended(ch.Index, isResponses); suspended {
			continue
		}

		// 获取上游配置并检查是否有可用的 API Key（复合渠道除外）
		upstream := s.getUpstreamByIndex(ch.Index, isResponses)
		if upstream == nil {
			continue
		}
		if !s.upstreamHasCredentials(upstream, isResponses) {
			continue
		}

		failureRate := metricsManager.CalculateFailureRate(ch.Index)
		if failureRate < bestFailureRate {
			bestFailureRate = failureRate
			bestChannel = ch
			bestUpstream = upstream
		}
	}

	if bestChannel != nil && bestUpstream != nil {
		log.Printf("⚠️ 降级选择渠道: [%d] %s (失败率: %.1f%%)",
			bestChannel.Index, bestUpstream.Name, bestFailureRate*100)
		result := &SelectionResult{
			Upstream:     bestUpstream,
			ChannelIndex: bestChannel.Index,
			Reason:       "fallback",
		}
		resolved, err := s.resolveCompositeChannel(result, model, isResponses, failedChannels, metricsManager, useCircuitBreaker)
		if err == nil {
			return resolved, nil
		}
		log.Printf("⚠️ [Composite] Failed to resolve fallback channel [%d] %s: %v", bestChannel.Index, bestUpstream.Name, err)
		failedChannels[bestChannel.Index] = true
		// Recursively try to find another fallback
		return s.selectFallbackChannel(activeChannels, failedChannels, isResponses, model, metricsManager, useCircuitBreaker)
	}

	return nil, fmt.Errorf("all channels are unavailable")
}

// ChannelInfo 渠道信息（用于排序）
type ChannelInfo struct {
	Index    int
	ID       string // Stable channel ID for permission matching
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
				ID:       upstream.ID,
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

// ==================== Gemini Channel Methods ====================

// ChannelInfo for Gemini channels
type GeminiChannelInfo struct {
	Index    int
	Name     string
	Status   string
	Priority int
}

// SelectGeminiChannel 选择 Gemini 渠道
func (s *ChannelScheduler) SelectGeminiChannel(
	ctx context.Context,
	failedChannels map[int]bool,
	allowedChannels []string,
) (*SelectionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get active Gemini channels
	activeChannels := s.getActiveGeminiChannels()
	if len(activeChannels) == 0 {
		return nil, fmt.Errorf("no active Gemini channels available")
	}

	// Filter by allowed channels if specified (by channel ID)
	if len(allowedChannels) > 0 {
		allowedSet := make(map[string]bool)
		for _, id := range allowedChannels {
			allowedSet[id] = true
		}
		var filtered []ChannelInfo
		for _, ch := range activeChannels {
			if allowedSet[ch.ID] {
				filtered = append(filtered, ch)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("%w (allowed channels: %v)", ErrNoAllowedChannels, allowedChannels)
		}
		activeChannels = filtered
	}

	// Remove failed channels
	var availableChannels []ChannelInfo
	for _, ch := range activeChannels {
		if !failedChannels[ch.Index] {
			availableChannels = append(availableChannels, ch)
		}
	}
	if len(availableChannels) == 0 {
		return nil, fmt.Errorf("all Gemini channels have failed")
	}

	// Use priority-based selection
	cfg := s.configManager.GetConfig()
	strategy := cfg.GeminiLoadBalance
	if strategy == "" {
		strategy = "failover"
	}
	orderedChannels := s.orderGeminiChannelsByStrategy(availableChannels, strategy)

	// Select first available healthy channel
	for _, ch := range orderedChannels {
		// Check if suspended
		if suspended, _, _ := s.isGeminiChannelSuspended(ch.Index); suspended {
			continue
		}
		// Check if healthy (circuit breaker)
		if !s.geminiMetricsManager.IsChannelHealthy(ch.Index) {
			continue
		}
		upstream := s.getGeminiUpstreamByIndex(ch.Index)
		if upstream != nil && len(upstream.APIKeys) > 0 {
			return &SelectionResult{
				Upstream:     upstream,
				ChannelIndex: ch.Index,
				Reason:       "gemini_priority",
			}, nil
		}
	}

	// Fallback: return first available even if unhealthy
	for _, ch := range orderedChannels {
		upstream := s.getGeminiUpstreamByIndex(ch.Index)
		if upstream != nil && len(upstream.APIKeys) > 0 {
			return &SelectionResult{
				Upstream:     upstream,
				ChannelIndex: ch.Index,
				Reason:       "gemini_fallback",
			}, nil
		}
	}

	return nil, fmt.Errorf("no usable Gemini channel found")
}

// getActiveGeminiChannels returns active Gemini channels
func (s *ChannelScheduler) getActiveGeminiChannels() []ChannelInfo {
	cfg := s.configManager.GetConfig()
	var channels []ChannelInfo

	for i, upstream := range cfg.GeminiUpstream {
		status := config.GetChannelStatus(&upstream)
		if status == "active" || status == "" {
			channels = append(channels, ChannelInfo{
				Index:    i,
				ID:       upstream.ID,
				Name:     upstream.Name,
				Status:   status,
				Priority: config.GetChannelPriority(&upstream, i),
			})
		}
	}

	return channels
}

// getGeminiUpstreamByIndex gets Gemini upstream by index
func (s *ChannelScheduler) getGeminiUpstreamByIndex(index int) *config.UpstreamConfig {
	cfg := s.configManager.GetConfig()
	if index < 0 || index >= len(cfg.GeminiUpstream) {
		return nil
	}
	upstream := cfg.GeminiUpstream[index]
	return &upstream
}

// orderGeminiChannelsByStrategy orders Gemini channels by strategy
func (s *ChannelScheduler) orderGeminiChannelsByStrategy(channels []ChannelInfo, strategy string) []ChannelInfo {
	result := make([]ChannelInfo, len(channels))
	copy(result, channels)

	switch strategy {
	case "round-robin":
		// Rotate based on counter
		if len(result) > 0 {
			s.geminiRoundRobinCounter = (s.geminiRoundRobinCounter + 1) % len(result)
			rotated := make([]ChannelInfo, len(result))
			for i := range result {
				rotated[i] = result[(i+s.geminiRoundRobinCounter)%len(result)]
			}
			result = rotated
		}
	case "random":
		// Shuffle
		rand.Shuffle(len(result), func(i, j int) {
			result[i], result[j] = result[j], result[i]
		})
	default: // "failover" or priority-based
		// Sort by priority (lower = higher priority)
		sort.Slice(result, func(i, j int) bool {
			return result[i].Priority < result[j].Priority
		})
	}

	return result
}

// isGeminiChannelSuspended checks if a Gemini channel is suspended
func (s *ChannelScheduler) isGeminiChannelSuspended(channelIndex int) (bool, time.Time, string) {
	if s.suspensionChecker == nil {
		return false, time.Time{}, ""
	}
	return s.suspensionChecker.IsChannelSuspended(channelIndex, "gemini")
}

// RecordGeminiSuccess records success for Gemini channel
func (s *ChannelScheduler) RecordGeminiSuccess(channelIndex int) {
	s.geminiMetricsManager.RecordSuccess(channelIndex)
}

// RecordGeminiFailure records failure for Gemini channel
func (s *ChannelScheduler) RecordGeminiFailure(channelIndex int) {
	s.geminiMetricsManager.RecordFailure(channelIndex)
}

// ResetGeminiChannelMetrics resets metrics for a Gemini channel
func (s *ChannelScheduler) ResetGeminiChannelMetrics(channelIndex int) {
	s.geminiMetricsManager.Reset(channelIndex)
}

// GetGeminiMetricsManager returns the Gemini metrics manager
func (s *ChannelScheduler) GetGeminiMetricsManager() *metrics.MetricsManager {
	return s.geminiMetricsManager
}

// GetActiveGeminiChannelCount returns the number of active Gemini channels
func (s *ChannelScheduler) GetActiveGeminiChannelCount() int {
	return len(s.getActiveGeminiChannels())
}

// IsGeminiMultiChannelMode returns true if there are multiple active Gemini channels
func (s *ChannelScheduler) IsGeminiMultiChannelMode() bool {
	return s.GetActiveGeminiChannelCount() > 1
}
