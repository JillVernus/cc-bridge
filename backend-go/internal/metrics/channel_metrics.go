package metrics

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestRecord 带时间戳的请求记录
type RequestRecord struct {
	Timestamp time.Time
	Success   bool
}

// RecentCall 最近调用结果（用于 UI 快速可视化）
type RecentCall struct {
	Success           bool      `json:"success"`
	StatusCode        int       `json:"statusCode,omitempty"`
	Timestamp         time.Time `json:"timestamp,omitempty"`
	Model             string    `json:"model,omitempty"`
	ChannelName       string    `json:"channelName,omitempty"`
	RoutedChannelName string    `json:"routedChannelName,omitempty"`
}

// ChannelMetrics 渠道指标
type ChannelMetrics struct {
	ChannelIndex        int          `json:"channelIndex"`
	RequestCount        int64        `json:"requestCount"`
	SuccessCount        int64        `json:"successCount"`
	FailureCount        int64        `json:"failureCount"`
	ConsecutiveFailures int64        `json:"consecutiveFailures"`
	LastSuccessAt       *time.Time   `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time   `json:"lastFailureAt,omitempty"`
	CircuitBrokenAt     *time.Time   `json:"circuitBrokenAt,omitempty"` // 熔断开始时间
	RecentCalls         []RecentCall `json:"recentCalls,omitempty"`     // 最近调用（最多 20 条）
	// 滑动窗口记录（最近 N 次请求的结果）
	recentResults []bool // true=success, false=failure
	// 带时间戳的请求记录（用于分时段统计，保留24小时）
	requestHistory []RequestRecord
	// 渠道身份（用于检测索引复用导致的指标串位）
	boundChannelName string
	boundChannelID   string
}

// TimeWindowStats 分时段统计
type TimeWindowStats struct {
	RequestCount int64   `json:"requestCount"`
	SuccessCount int64   `json:"successCount"`
	FailureCount int64   `json:"failureCount"`
	SuccessRate  float64 `json:"successRate"`
}

// ChannelIdentityExpectation describes expected channel identity at a given index.
type ChannelIdentityExpectation struct {
	ChannelIndex int
	ChannelID    string
	ChannelName  string
}

// MetricsManager 指标管理器
type MetricsManager struct {
	mu                  sync.RWMutex
	metrics             map[int]*ChannelMetrics    // runtime view keyed by channelIndex
	metricsByIdentity   map[string]*ChannelMetrics // primary storage keyed by channel UID (fallback: legacy index key)
	windowSize          int                        // 滑动窗口大小
	failureThreshold    float64                    // 失败率阈值
	circuitRecoveryTime time.Duration              // 熔断恢复时间
	stopCh              chan struct{}              // 用于停止清理 goroutine
}

const recentCallsLimit = 20
const legacyIndexIdentityPrefix = "__idx__:"

// NewMetricsManager 创建指标管理器
func NewMetricsManager() *MetricsManager {
	m := &MetricsManager{
		metrics:             make(map[int]*ChannelMetrics),
		metricsByIdentity:   make(map[string]*ChannelMetrics),
		windowSize:          10,               // 默认基于最近 10 次请求计算失败率
		failureThreshold:    0.5,              // 默认 50% 失败率阈值
		circuitRecoveryTime: 15 * time.Minute, // 默认 15 分钟自动恢复
		stopCh:              make(chan struct{}),
	}
	// 启动后台熔断恢复任务
	go m.cleanupCircuitBreakers()
	return m
}

// NewMetricsManagerWithConfig 创建带配置的指标管理器
func NewMetricsManagerWithConfig(windowSize int, failureThreshold float64) *MetricsManager {
	if windowSize <= 0 {
		windowSize = 10
	}
	if failureThreshold <= 0 || failureThreshold > 1 {
		failureThreshold = 0.5
	}
	m := &MetricsManager{
		metrics:             make(map[int]*ChannelMetrics),
		metricsByIdentity:   make(map[string]*ChannelMetrics),
		windowSize:          windowSize,
		failureThreshold:    failureThreshold,
		circuitRecoveryTime: 15 * time.Minute,
		stopCh:              make(chan struct{}),
	}
	// 启动后台熔断恢复任务
	go m.cleanupCircuitBreakers()
	return m
}

func legacyIdentityKey(channelIndex int) string {
	return legacyIndexIdentityPrefix + strconv.Itoa(channelIndex)
}

func identityKey(channelIndex int, channelID string) string {
	normalizedID := strings.TrimSpace(channelID)
	if normalizedID != "" {
		return normalizedID
	}
	return legacyIdentityKey(channelIndex)
}

func (m *MetricsManager) getOrCreateByIdentityLocked(channelIndex int, channelID, channelName string) *ChannelMetrics {
	key := identityKey(channelIndex, channelID)
	normalizedID := strings.TrimSpace(channelID)
	normalizedName := strings.TrimSpace(channelName)

	metrics, exists := m.metricsByIdentity[key]
	if !exists && normalizedID != "" {
		// Migrate legacy index-keyed bucket to stable UID key once identity is available.
		legacyKey := legacyIdentityKey(channelIndex)
		if legacy, ok := m.metricsByIdentity[legacyKey]; ok {
			legacyBoundID := strings.TrimSpace(legacy.boundChannelID)
			// Guard against stale legacy aliases from previous index reuse.
			// Only migrate when legacy bucket is unbound or already bound to the same UID.
			if legacyBoundID == "" || legacyBoundID == normalizedID {
				delete(m.metricsByIdentity, legacyKey)
				m.metricsByIdentity[key] = legacy
				metrics = legacy
				exists = true
			}
		}
	}
	if !exists {
		metrics = &ChannelMetrics{
			ChannelIndex:  channelIndex,
			recentResults: make([]bool, 0, m.windowSize),
			RecentCalls:   make([]RecentCall, 0, recentCallsLimit),
		}
		m.metricsByIdentity[key] = metrics
	}

	previousIndex := metrics.ChannelIndex
	if previousIndex != channelIndex {
		if bound, ok := m.metrics[previousIndex]; ok && bound == metrics {
			delete(m.metrics, previousIndex)
		}
	}
	metrics.ChannelIndex = channelIndex
	if normalizedID != "" {
		metrics.boundChannelID = normalizedID
	}
	if normalizedName != "" {
		metrics.boundChannelName = normalizedName
	}
	m.metrics[channelIndex] = metrics
	return metrics
}

func (m *MetricsManager) getByIdentityLocked(channelIndex int, channelID string) *ChannelMetrics {
	if metrics, exists := m.metrics[channelIndex]; exists {
		if channelID == "" || strings.TrimSpace(metrics.boundChannelID) == strings.TrimSpace(channelID) {
			return metrics
		}
	}
	key := identityKey(channelIndex, channelID)
	if metrics, exists := m.metricsByIdentity[key]; exists {
		return metrics
	}
	if channelID != "" {
		if metrics, exists := m.metricsByIdentity[strings.TrimSpace(channelID)]; exists {
			return metrics
		}
	}
	return nil
}

// getOrCreate 获取或创建渠道指标（兼容旧索引调用）
func (m *MetricsManager) getOrCreate(channelIndex int) *ChannelMetrics {
	return m.getOrCreateByIdentityLocked(channelIndex, "", "")
}

// RecordSuccess 记录成功请求
func (m *MetricsManager) RecordSuccess(channelIndex int) {
	m.RecordSuccessWithStatus(channelIndex, 0)
}

// RecordSuccessWithStatus 记录成功请求（可选状态码）
func (m *MetricsManager) RecordSuccessWithStatus(channelIndex int, statusCode int) {
	m.RecordSuccessWithStatusDetail(channelIndex, statusCode, "", "")
}

// RecordSuccessWithStatusDetail 记录成功请求（可选状态码、模型和渠道名）
func (m *MetricsManager) RecordSuccessWithStatusDetail(channelIndex int, statusCode int, model, channelName string, routedChannelName ...string) {
	m.RecordSuccessWithStatusDetailByIdentity(channelIndex, "", statusCode, model, channelName, routedChannelName...)
}

// RecordSuccessWithStatusDetailByIdentity records success using stable channel identity.
func (m *MetricsManager) RecordSuccessWithStatusDetailByIdentity(channelIndex int, channelID string, statusCode int, model, channelName string, routedChannelName ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.getOrCreateByIdentityLocked(channelIndex, channelID, channelName)
	metrics.RequestCount++
	metrics.SuccessCount++
	metrics.ConsecutiveFailures = 0

	now := time.Now()
	metrics.LastSuccessAt = &now

	// 成功后清除熔断标记
	if metrics.CircuitBrokenAt != nil {
		metrics.CircuitBrokenAt = nil
		log.Printf("✅ 渠道 [%d] 因请求成功退出熔断状态", channelIndex)
	}

	// 更新滑动窗口
	m.appendToWindow(metrics, true)

	// 记录带时间戳的请求
	m.appendToHistory(metrics, now, true)

	// 记录最近调用
	routed := ""
	if len(routedChannelName) > 0 {
		routed = routedChannelName[0]
	}
	m.appendRecentCall(metrics, true, statusCode, model, channelName, routed)
}

// RecordFailure 记录失败请求
func (m *MetricsManager) RecordFailure(channelIndex int) {
	m.RecordFailureWithStatus(channelIndex, 0)
}

// RecordFailureWithStatus 记录失败请求（可选状态码）
func (m *MetricsManager) RecordFailureWithStatus(channelIndex int, statusCode int) {
	m.RecordFailureWithStatusDetail(channelIndex, statusCode, "", "")
}

// RecordFailureWithStatusDetail 记录失败请求（可选状态码、模型和渠道名）
func (m *MetricsManager) RecordFailureWithStatusDetail(channelIndex int, statusCode int, model, channelName string, routedChannelName ...string) {
	m.RecordFailureWithStatusDetailByIdentity(channelIndex, "", statusCode, model, channelName, routedChannelName...)
}

// RecordFailureWithStatusDetailByIdentity records failure using stable channel identity.
func (m *MetricsManager) RecordFailureWithStatusDetailByIdentity(channelIndex int, channelID string, statusCode int, model, channelName string, routedChannelName ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.getOrCreateByIdentityLocked(channelIndex, channelID, channelName)
	metrics.RequestCount++
	metrics.FailureCount++
	metrics.ConsecutiveFailures++

	now := time.Now()
	metrics.LastFailureAt = &now

	// 更新滑动窗口
	m.appendToWindow(metrics, false)

	// 检查是否刚进入熔断状态
	if metrics.CircuitBrokenAt == nil && m.isCircuitBroken(metrics) {
		metrics.CircuitBrokenAt = &now
		log.Printf("⚡ 渠道 [%d] 进入熔断状态（失败率: %.1f%%）", channelIndex, m.calculateFailureRateInternal(metrics)*100)
	}

	// 记录带时间戳的请求
	m.appendToHistory(metrics, now, false)

	// 记录最近调用
	routed := ""
	if len(routedChannelName) > 0 {
		routed = routedChannelName[0]
	}
	m.appendRecentCall(metrics, false, statusCode, model, channelName, routed)
}

// ReconcileChannelIdentity 将索引指标与当前配置中的渠道身份（ID/名称）对齐。
// 当同一索引绑定到不同渠道时，重置该索引指标以避免历史数据串位。
func (m *MetricsManager) ReconcileChannelIdentity(channelIndex int, expectedChannelID, expectedChannelName string) {
	// Keep backward compatibility for single-index reconcile callers (tests/legacy paths):
	// preserve existing index view expectations and only override the target index.
	m.mu.RLock()
	expectations := make([]ChannelIdentityExpectation, 0, len(m.metrics)+1)
	expectations = append(expectations, ChannelIdentityExpectation{
		ChannelIndex: channelIndex,
		ChannelID:    expectedChannelID,
		ChannelName:  expectedChannelName,
	})
	for idx, metrics := range m.metrics {
		if idx == channelIndex || metrics == nil {
			continue
		}
		channelName := strings.TrimSpace(metrics.boundChannelName)
		if channelName == "" {
			channelName = inferRecentChannelName(metrics.RecentCalls)
		}
		expectations = append(expectations, ChannelIdentityExpectation{
			ChannelIndex: idx,
			ChannelID:    strings.TrimSpace(metrics.boundChannelID),
			ChannelName:  channelName,
		})
	}
	m.mu.RUnlock()

	m.ReconcileChannelIdentities(expectations)
}

// ReconcileChannelIdentities performs a batch remap by stable channel identity.
// It preserves existing metrics on reorder/insert by remapping old index buckets
// to their new indices via channel ID first, then channel name fallback.
func (m *MetricsManager) ReconcileChannelIdentities(expected []ChannelIdentityExpectation) {
	if len(expected) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existingByID := make(map[string]*ChannelMetrics, len(m.metricsByIdentity))
	existingByName := make(map[string][]*ChannelMetrics, len(m.metricsByIdentity))
	pointerKey := make(map[*ChannelMetrics]string, len(m.metricsByIdentity))
	for key, metrics := range m.metricsByIdentity {
		if metrics == nil || key == "" {
			continue
		}
		pointerKey[metrics] = key
		channelID := strings.TrimSpace(metrics.boundChannelID)
		if channelID != "" {
			if _, exists := existingByID[channelID]; !exists {
				existingByID[channelID] = metrics
			}
		}

		channelName := strings.TrimSpace(metrics.boundChannelName)
		if channelName == "" {
			channelName = inferRecentChannelName(metrics.RecentCalls)
		}
		normalizedName := strings.ToLower(strings.TrimSpace(channelName))
		if normalizedName != "" {
			existingByName[normalizedName] = append(existingByName[normalizedName], metrics)
		}
	}

	assigned := make(map[*ChannelMetrics]bool, len(m.metricsByIdentity))
	indexView := make(map[int]*ChannelMetrics, len(expected))
	nextIdentity := make(map[string]*ChannelMetrics, len(expected))
	remappedByID := 0
	remappedByName := 0
	reboundLegacy := 0

	for _, exp := range expected {
		channelIndex := exp.ChannelIndex
		expectedID := strings.TrimSpace(exp.ChannelID)
		expectedName := strings.TrimSpace(exp.ChannelName)

		var candidate *ChannelMetrics
		candidateKey := ""

		// Fast path: existing index binding already matches expected identity.
		if current := m.metrics[channelIndex]; current != nil && !assigned[current] {
			currentID := strings.TrimSpace(current.boundChannelID)
			if expectedID != "" && currentID != "" && currentID == expectedID {
				candidate = current
				candidateKey = pointerKey[current]
			} else if expectedID == "" {
				currentName := strings.TrimSpace(current.boundChannelName)
				if currentName == "" {
					currentName = inferRecentChannelName(current.RecentCalls)
				}
				if strings.EqualFold(currentName, expectedName) {
					candidate = current
					candidateKey = pointerKey[current]
				}
			}
		}

		// Primary remap: stable channel ID.
		if candidate == nil && expectedID != "" {
			if byID, ok := m.metricsByIdentity[expectedID]; ok && !assigned[byID] {
				candidate = byID
				candidateKey = expectedID
				remappedByID++
			} else if byID, ok := existingByID[expectedID]; ok && !assigned[byID] {
				candidate = byID
				candidateKey = pointerKey[byID]
				remappedByID++
			}
		}

		// Secondary remap: unique name fallback (legacy/no-id buckets).
		if candidate == nil && expectedName != "" {
			normalizedName := strings.ToLower(expectedName)
			for _, byName := range existingByName[normalizedName] {
				if assigned[byName] {
					continue
				}
				candidate = byName
				candidateKey = pointerKey[byName]
				remappedByName++
				break
			}
		}

		// Legacy index-key fallback (only for channels without stable ID).
		if candidate == nil && expectedID == "" {
			legacyKey := legacyIdentityKey(channelIndex)
			if legacy, ok := m.metricsByIdentity[legacyKey]; ok && !assigned[legacy] {
				candidate = legacy
				candidateKey = legacyKey
			}
		}

		// No historical metrics for this channel yet.
		if candidate == nil {
			continue
		}

		candidate.ChannelIndex = channelIndex
		if expectedID != "" {
			candidate.boundChannelID = expectedID
		}
		if expectedName != "" {
			candidate.boundChannelName = expectedName
		}

		if expectedID != "" {
			if candidateKey != "" && candidateKey != expectedID {
				reboundLegacy++
			}
			nextIdentity[expectedID] = candidate
		} else {
			nextIdentity[legacyIdentityKey(channelIndex)] = candidate
		}

		indexView[channelIndex] = candidate
		assigned[candidate] = true
	}

	m.metrics = indexView
	m.metricsByIdentity = nextIdentity

	if remappedByID > 0 || remappedByName > 0 || reboundLegacy > 0 {
		log.Printf(
			"🔁 渠道指标重映射完成 (by channelID: %d, by channelName: %d, rebound: %d)",
			remappedByID,
			remappedByName,
			reboundLegacy,
		)
	}
}

// isCircuitBroken 判断是否达到熔断条件（内部方法，调用前需持有锁）
func (m *MetricsManager) isCircuitBroken(metrics *ChannelMetrics) bool {
	minRequests := m.windowSize / 2
	if len(metrics.recentResults) < minRequests {
		return false
	}
	return m.calculateFailureRateInternal(metrics) >= m.failureThreshold
}

// calculateFailureRateInternal 计算失败率（内部方法，调用前需持有锁）
func (m *MetricsManager) calculateFailureRateInternal(metrics *ChannelMetrics) float64 {
	if len(metrics.recentResults) == 0 {
		return 0
	}
	failures := 0
	for _, success := range metrics.recentResults {
		if !success {
			failures++
		}
	}
	return float64(failures) / float64(len(metrics.recentResults))
}

// appendToWindow 向滑动窗口添加记录
func (m *MetricsManager) appendToWindow(metrics *ChannelMetrics, success bool) {
	metrics.recentResults = append(metrics.recentResults, success)
	// 保持窗口大小
	if len(metrics.recentResults) > m.windowSize {
		metrics.recentResults = metrics.recentResults[1:]
	}
}

// appendToHistory 向历史记录添加请求（保留24小时）
func (m *MetricsManager) appendToHistory(metrics *ChannelMetrics, timestamp time.Time, success bool) {
	metrics.requestHistory = append(metrics.requestHistory, RequestRecord{
		Timestamp: timestamp,
		Success:   success,
	})

	// 清理超过24小时的记录
	cutoff := time.Now().Add(-24 * time.Hour)
	newStart := 0
	for i, record := range metrics.requestHistory {
		if record.Timestamp.After(cutoff) {
			newStart = i
			break
		}
	}
	if newStart > 0 {
		metrics.requestHistory = metrics.requestHistory[newStart:]
	}
}

// appendRecentCall 记录最近调用结果（最多保留 recentCallsLimit 条）
func (m *MetricsManager) appendRecentCall(metrics *ChannelMetrics, success bool, statusCode int, model, channelName, routedChannelName string) {
	if statusCode < 0 {
		statusCode = 0
	}
	normalizedChannelName := strings.TrimSpace(channelName)
	normalizedRoutedChannelName := strings.TrimSpace(routedChannelName)
	if strings.EqualFold(normalizedChannelName, normalizedRoutedChannelName) {
		normalizedRoutedChannelName = ""
	}
	if normalizedChannelName != "" {
		metrics.boundChannelName = normalizedChannelName
	}
	metrics.RecentCalls = append(metrics.RecentCalls, RecentCall{
		Success:           success,
		StatusCode:        statusCode,
		Timestamp:         time.Now(),
		Model:             model,
		ChannelName:       normalizedChannelName,
		RoutedChannelName: normalizedRoutedChannelName,
	})
	if len(metrics.RecentCalls) > recentCallsLimit {
		metrics.RecentCalls = metrics.RecentCalls[len(metrics.RecentCalls)-recentCallsLimit:]
	}
}

// SeedRecentCalls seeds recent calls for a channel (used for startup restore).
// This only restores visualization data and does not change scheduler counters.
func (m *MetricsManager) SeedRecentCalls(channelIndex int, calls []RecentCall) {
	m.SeedRecentCallsByIdentity(channelIndex, "", "", calls)
}

// SeedRecentCallsByIdentity seeds recent calls with stable channel identity.
func (m *MetricsManager) SeedRecentCallsByIdentity(channelIndex int, channelID, channelName string, calls []RecentCall) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.getOrCreateByIdentityLocked(channelIndex, channelID, channelName)

	if len(calls) == 0 {
		metrics.RecentCalls = make([]RecentCall, 0, recentCallsLimit)
		if strings.TrimSpace(channelName) != "" {
			metrics.boundChannelName = strings.TrimSpace(channelName)
		}
		metrics.boundChannelID = strings.TrimSpace(channelID)
		return
	}
	if len(calls) > recentCallsLimit {
		calls = calls[len(calls)-recentCallsLimit:]
	}

	seeded := make([]RecentCall, len(calls))
	copy(seeded, calls)
	for i := range seeded {
		if seeded[i].StatusCode < 0 {
			seeded[i].StatusCode = 0
		}
		seeded[i].ChannelName = strings.TrimSpace(seeded[i].ChannelName)
		seeded[i].RoutedChannelName = strings.TrimSpace(seeded[i].RoutedChannelName)
		if strings.EqualFold(seeded[i].ChannelName, seeded[i].RoutedChannelName) {
			seeded[i].RoutedChannelName = ""
		}
	}
	metrics.RecentCalls = seeded
	if strings.TrimSpace(channelName) != "" {
		metrics.boundChannelName = strings.TrimSpace(channelName)
	} else {
		metrics.boundChannelName = inferRecentChannelName(seeded)
	}
	metrics.boundChannelID = strings.TrimSpace(channelID)
}

func inferRecentChannelName(calls []RecentCall) string {
	for i := len(calls) - 1; i >= 0; i-- {
		channelName := strings.TrimSpace(calls[i].ChannelName)
		if channelName != "" {
			return channelName
		}
	}
	return ""
}

// GetTimeWindowStats 获取指定时间窗口内的统计
func (m *MetricsManager) GetTimeWindowStats(channelIndex int, duration time.Duration) TimeWindowStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.metrics[channelIndex]
	if !exists {
		return TimeWindowStats{SuccessRate: 100}
	}

	cutoff := time.Now().Add(-duration)
	var requestCount, successCount, failureCount int64

	for _, record := range metrics.requestHistory {
		if record.Timestamp.After(cutoff) {
			requestCount++
			if record.Success {
				successCount++
			} else {
				failureCount++
			}
		}
	}

	successRate := float64(100)
	if requestCount > 0 {
		successRate = float64(successCount) / float64(requestCount) * 100
	}

	return TimeWindowStats{
		RequestCount: requestCount,
		SuccessCount: successCount,
		FailureCount: failureCount,
		SuccessRate:  successRate,
	}
}

// GetAllTimeWindowStats 获取所有时间窗口的统计（15m, 1h, 6h, 24h）
func (m *MetricsManager) GetAllTimeWindowStats(channelIndex int) map[string]TimeWindowStats {
	return map[string]TimeWindowStats{
		"15m": m.GetTimeWindowStats(channelIndex, 15*time.Minute),
		"1h":  m.GetTimeWindowStats(channelIndex, 1*time.Hour),
		"6h":  m.GetTimeWindowStats(channelIndex, 6*time.Hour),
		"24h": m.GetTimeWindowStats(channelIndex, 24*time.Hour),
	}
}

// GetMetrics 获取渠道指标
func (m *MetricsManager) GetMetrics(channelIndex int) *ChannelMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if metrics, exists := m.metrics[channelIndex]; exists {
		recentCalls := make([]RecentCall, len(metrics.RecentCalls))
		copy(recentCalls, metrics.RecentCalls)
		// 返回副本
		return &ChannelMetrics{
			ChannelIndex:        metrics.ChannelIndex,
			RequestCount:        metrics.RequestCount,
			SuccessCount:        metrics.SuccessCount,
			FailureCount:        metrics.FailureCount,
			ConsecutiveFailures: metrics.ConsecutiveFailures,
			LastSuccessAt:       metrics.LastSuccessAt,
			LastFailureAt:       metrics.LastFailureAt,
			CircuitBrokenAt:     metrics.CircuitBrokenAt,
			RecentCalls:         recentCalls,
		}
	}
	return nil
}

// GetAllMetrics 获取所有渠道指标
func (m *MetricsManager) GetAllMetrics() []*ChannelMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ChannelMetrics, 0, len(m.metrics))
	for _, metrics := range m.metrics {
		recentCalls := make([]RecentCall, len(metrics.RecentCalls))
		copy(recentCalls, metrics.RecentCalls)
		result = append(result, &ChannelMetrics{
			ChannelIndex:        metrics.ChannelIndex,
			RequestCount:        metrics.RequestCount,
			SuccessCount:        metrics.SuccessCount,
			FailureCount:        metrics.FailureCount,
			ConsecutiveFailures: metrics.ConsecutiveFailures,
			LastSuccessAt:       metrics.LastSuccessAt,
			LastFailureAt:       metrics.LastFailureAt,
			CircuitBrokenAt:     metrics.CircuitBrokenAt,
			RecentCalls:         recentCalls,
		})
	}
	return result
}

// CalculateFailureRate 计算渠道失败率（基于滑动窗口）
func (m *MetricsManager) CalculateFailureRate(channelIndex int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.metrics[channelIndex]
	if !exists || len(metrics.recentResults) == 0 {
		return 0
	}

	failures := 0
	for _, success := range metrics.recentResults {
		if !success {
			failures++
		}
	}

	return float64(failures) / float64(len(metrics.recentResults))
}

// CalculateFailureRateByIdentity calculates failure rate using stable channel identity.
func (m *MetricsManager) CalculateFailureRateByIdentity(channelIndex int, channelID string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.getByIdentityLocked(channelIndex, channelID)
	if metrics == nil || len(metrics.recentResults) == 0 {
		return 0
	}

	// Keep current index view updated for compatibility endpoints.
	previousIndex := metrics.ChannelIndex
	if previousIndex != channelIndex {
		if bound, ok := m.metrics[previousIndex]; ok && bound == metrics {
			delete(m.metrics, previousIndex)
		}
	}
	m.metrics[channelIndex] = metrics
	metrics.ChannelIndex = channelIndex

	failures := 0
	for _, success := range metrics.recentResults {
		if !success {
			failures++
		}
	}
	return float64(failures) / float64(len(metrics.recentResults))
}

// CalculateSuccessRate 计算渠道成功率（基于滑动窗口）
func (m *MetricsManager) CalculateSuccessRate(channelIndex int) float64 {
	return 1 - m.CalculateFailureRate(channelIndex)
}

// IsChannelHealthy 判断渠道是否健康（失败率低于阈值）
func (m *MetricsManager) IsChannelHealthy(channelIndex int) bool {
	return m.CalculateFailureRate(channelIndex) < m.failureThreshold
}

// IsChannelHealthyByIdentity checks health by stable channel identity.
func (m *MetricsManager) IsChannelHealthyByIdentity(channelIndex int, channelID string) bool {
	return m.CalculateFailureRateByIdentity(channelIndex, channelID) < m.failureThreshold
}

// ShouldSuspend 判断是否应该熔断（失败率达到阈值）
func (m *MetricsManager) ShouldSuspend(channelIndex int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.metrics[channelIndex]
	if !exists {
		return false
	}

	// 至少有一定数量的请求才判断
	minRequests := m.windowSize / 2
	if len(metrics.recentResults) < minRequests {
		return false
	}

	return m.CalculateFailureRate(channelIndex) >= m.failureThreshold
}

// Reset 重置渠道指标（用于恢复熔断）
func (m *MetricsManager) Reset(channelIndex int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metrics, exists := m.metrics[channelIndex]; exists {
		metrics.ConsecutiveFailures = 0
		metrics.recentResults = make([]bool, 0, m.windowSize)
		metrics.CircuitBrokenAt = nil // 清除熔断时间
		// 保留历史统计，但清除滑动窗口
		log.Printf("🔄 渠道 [%d] 指标已手动重置", channelIndex)
	}
}

// ResetByIdentity resets channel metrics by stable identity.
func (m *MetricsManager) ResetByIdentity(channelIndex int, channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.getByIdentityLocked(channelIndex, channelID)
	if metrics == nil {
		return
	}
	metrics.ConsecutiveFailures = 0
	metrics.recentResults = make([]bool, 0, m.windowSize)
	metrics.CircuitBrokenAt = nil
	previousIndex := metrics.ChannelIndex
	if previousIndex != channelIndex {
		if bound, ok := m.metrics[previousIndex]; ok && bound == metrics {
			delete(m.metrics, previousIndex)
		}
	}
	metrics.ChannelIndex = channelIndex
	m.metrics[channelIndex] = metrics
	log.Printf("🔄 渠道 [%d] 指标已手动重置", channelIndex)
}

// ResetAll 重置所有渠道指标
func (m *MetricsManager) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics = make(map[int]*ChannelMetrics)
	m.metricsByIdentity = make(map[string]*ChannelMetrics)
}

// Stop 停止后台清理任务
func (m *MetricsManager) Stop() {
	close(m.stopCh)
}

// cleanupCircuitBreakers 后台任务：定期检查并恢复超时的熔断渠道
func (m *MetricsManager) cleanupCircuitBreakers() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.recoverExpiredCircuitBreakers()
		case <-m.stopCh:
			return
		}
	}
}

// recoverExpiredCircuitBreakers 恢复超时的熔断渠道
func (m *MetricsManager) recoverExpiredCircuitBreakers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, metrics := range m.metricsByIdentity {
		if metrics.CircuitBrokenAt != nil {
			elapsed := now.Sub(*metrics.CircuitBrokenAt)
			if elapsed > m.circuitRecoveryTime {
				// 重置熔断状态
				metrics.ConsecutiveFailures = 0
				metrics.recentResults = make([]bool, 0, m.windowSize)
				metrics.CircuitBrokenAt = nil
				log.Printf("✅ 渠道 [%d] 熔断自动恢复（已超过 %v）", metrics.ChannelIndex, m.circuitRecoveryTime)
			}
		}
	}
}

// GetCircuitRecoveryTime 获取熔断恢复时间
func (m *MetricsManager) GetCircuitRecoveryTime() time.Duration {
	return m.circuitRecoveryTime
}

// GetFailureThreshold 获取失败率阈值
func (m *MetricsManager) GetFailureThreshold() float64 {
	return m.failureThreshold
}

// GetWindowSize 获取滑动窗口大小
func (m *MetricsManager) GetWindowSize() int {
	return m.windowSize
}

// MetricsResponse API 响应结构
type MetricsResponse struct {
	ChannelIndex        int     `json:"channelIndex"`
	RequestCount        int64   `json:"requestCount"`
	SuccessCount        int64   `json:"successCount"`
	FailureCount        int64   `json:"failureCount"`
	SuccessRate         float64 `json:"successRate"`
	ErrorRate           float64 `json:"errorRate"`
	ConsecutiveFailures int64   `json:"consecutiveFailures"`
	Latency             int64   `json:"latency"` // 需要从其他地方获取
	LastSuccessAt       *string `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *string `json:"lastFailureAt,omitempty"`
	CircuitBrokenAt     *string `json:"circuitBrokenAt,omitempty"` // 熔断开始时间
}

// ToResponse 转换为 API 响应格式
func (m *MetricsManager) ToResponse(channelIndex int, latency int64) *MetricsResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.metrics[channelIndex]
	if !exists {
		return &MetricsResponse{
			ChannelIndex: channelIndex,
			SuccessRate:  100,
			ErrorRate:    0,
			Latency:      latency,
		}
	}

	failureRate := m.CalculateFailureRate(channelIndex)
	successRate := (1 - failureRate) * 100

	resp := &MetricsResponse{
		ChannelIndex:        channelIndex,
		RequestCount:        metrics.RequestCount,
		SuccessCount:        metrics.SuccessCount,
		FailureCount:        metrics.FailureCount,
		SuccessRate:         successRate,
		ErrorRate:           failureRate * 100,
		ConsecutiveFailures: metrics.ConsecutiveFailures,
		Latency:             latency,
	}

	if metrics.LastSuccessAt != nil {
		t := metrics.LastSuccessAt.Format(time.RFC3339)
		resp.LastSuccessAt = &t
	}
	if metrics.LastFailureAt != nil {
		t := metrics.LastFailureAt.Format(time.RFC3339)
		resp.LastFailureAt = &t
	}
	if metrics.CircuitBrokenAt != nil {
		t := metrics.CircuitBrokenAt.Format(time.RFC3339)
		resp.CircuitBrokenAt = &t
	}

	return resp
}
