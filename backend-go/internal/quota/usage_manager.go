package quota

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
)

// UsageManager handles channel usage quota tracking and persistence
type UsageManager struct {
	mu        sync.RWMutex
	usageFile string
	usage     UsageFile
	configMgr *config.ConfigManager
	stopChan  chan struct{}
}

// NewUsageManager creates a new usage quota manager
func NewUsageManager(configDir string, configMgr *config.ConfigManager) (*UsageManager, error) {
	m := &UsageManager{
		usageFile: filepath.Join(configDir, "quota_usage.json"),
		configMgr: configMgr,
		stopChan:  make(chan struct{}),
	}

	if err := m.load(); err != nil {
		return nil, err
	}

	// Start auto-reset checker
	go m.autoResetLoop()

	return m, nil
}

// load reads usage data from file
func (m *UsageManager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Initialize empty usage if file doesn't exist
	if _, err := os.Stat(m.usageFile); os.IsNotExist(err) {
		m.usage = UsageFile{
			Messages:  make(map[string]ChannelUsage),
			Responses: make(map[string]ChannelUsage),
		}
		return nil
	}

	data, err := os.ReadFile(m.usageFile)
	if err != nil {
		return fmt.Errorf("failed to read quota usage file: %w", err)
	}

	if err := json.Unmarshal(data, &m.usage); err != nil {
		return fmt.Errorf("failed to parse quota usage file: %w", err)
	}

	// Ensure maps are initialized
	if m.usage.Messages == nil {
		m.usage.Messages = make(map[string]ChannelUsage)
	}
	if m.usage.Responses == nil {
		m.usage.Responses = make(map[string]ChannelUsage)
	}

	return nil
}

// save persists usage data to file
func (m *UsageManager) save() error {
	data, err := json.MarshalIndent(m.usage, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quota usage: %w", err)
	}

	if err := os.WriteFile(m.usageFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write quota usage file: %w", err)
	}

	return nil
}

// GetUsage returns the current usage for a Messages channel
func (m *UsageManager) GetUsage(channelIndex int) *ChannelUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := strconv.Itoa(channelIndex)
	if usage, ok := m.usage.Messages[key]; ok {
		return &usage
	}
	return nil
}

// GetResponsesUsage returns the current usage for a Responses channel
func (m *UsageManager) GetResponsesUsage(channelIndex int) *ChannelUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := strconv.Itoa(channelIndex)
	if usage, ok := m.usage.Responses[key]; ok {
		return &usage
	}
	return nil
}

// IncrementUsage adds to the usage counter for a Messages channel
func (m *UsageManager) IncrementUsage(channelIndex int, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	usage := m.usage.Messages[key]
	usage.Used += amount
	if usage.LastResetAt.IsZero() {
		usage.LastResetAt = time.Now()
	}
	m.usage.Messages[key] = usage

	// For rolling mode: update quotaResetAt if it's in the past
	cfg := m.configMgr.GetConfig()
	if channelIndex >= 0 && channelIndex < len(cfg.Upstream) {
		upstream := cfg.Upstream[channelIndex]
		if upstream.QuotaResetMode == "rolling" &&
			upstream.QuotaResetAt != nil &&
			upstream.QuotaResetAt.Before(time.Now()) &&
			upstream.QuotaResetInterval > 0 {
			newResetAt := m.calculateNextResetFromNow(upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if err := m.configMgr.UpdateChannelQuotaResetAt(channelIndex, false, newResetAt); err != nil {
				log.Printf("⚠️ Failed to update quotaResetAt for rolling mode: %v", err)
			}
		}
	}

	return m.save()
}

// IncrementResponsesUsage adds to the usage counter for a Responses channel
func (m *UsageManager) IncrementResponsesUsage(channelIndex int, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	usage := m.usage.Responses[key]
	usage.Used += amount
	if usage.LastResetAt.IsZero() {
		usage.LastResetAt = time.Now()
	}
	m.usage.Responses[key] = usage

	// For rolling mode: update quotaResetAt if it's in the past
	cfg := m.configMgr.GetConfig()
	if channelIndex >= 0 && channelIndex < len(cfg.ResponsesUpstream) {
		upstream := cfg.ResponsesUpstream[channelIndex]
		if upstream.QuotaResetMode == "rolling" &&
			upstream.QuotaResetAt != nil &&
			upstream.QuotaResetAt.Before(time.Now()) &&
			upstream.QuotaResetInterval > 0 {
			newResetAt := m.calculateNextResetFromNow(upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if err := m.configMgr.UpdateChannelQuotaResetAt(channelIndex, true, newResetAt); err != nil {
				log.Printf("⚠️ Failed to update quotaResetAt for Responses rolling mode: %v", err)
			}
		}
	}

	return m.save()
}

// ResetUsage resets the usage counter for a Messages channel
func (m *UsageManager) ResetUsage(channelIndex int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	m.usage.Messages[key] = ChannelUsage{
		Used:        0,
		LastResetAt: time.Now(),
	}

	log.Printf("🔄 Messages 渠道 [%d] 配额已重置", channelIndex)
	return m.save()
}

// ResetResponsesUsage resets the usage counter for a Responses channel
func (m *UsageManager) ResetResponsesUsage(channelIndex int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	m.usage.Responses[key] = ChannelUsage{
		Used:        0,
		LastResetAt: time.Now(),
	}

	log.Printf("🔄 Responses 渠道 [%d] 配额已重置", channelIndex)
	return m.save()
}

// GetChannelUsageStatus returns the full usage status for a Messages channel
func (m *UsageManager) GetChannelUsageStatus(channelIndex int) *ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	if channelIndex < 0 || channelIndex >= len(cfg.Upstream) {
		return nil
	}

	upstream := cfg.Upstream[channelIndex]
	if upstream.QuotaType == "" {
		return &ChannelUsageStatus{QuotaType: ""}
	}

	usage := m.GetUsage(channelIndex)
	var used float64
	var lastResetAt *string
	if usage != nil {
		used = usage.Used
		if !usage.LastResetAt.IsZero() {
			t := usage.LastResetAt.Format(time.RFC3339)
			lastResetAt = &t
		}
	}

	remaining := upstream.QuotaLimit - used
	if remaining < 0 {
		remaining = 0
	}

	var remainingPct float64
	if upstream.QuotaLimit > 0 {
		remainingPct = (remaining / upstream.QuotaLimit) * 100
	}

	status := &ChannelUsageStatus{
		QuotaType:    upstream.QuotaType,
		Limit:        upstream.QuotaLimit,
		Used:         used,
		Remaining:    remaining,
		RemainingPct: remainingPct,
		LastResetAt:  lastResetAt,
	}

	// Calculate next reset time if auto-reset is configured
	if upstream.QuotaResetAt != nil && upstream.QuotaResetInterval > 0 {
		nextReset := m.calculateNextReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
		if nextReset != nil {
			t := nextReset.Format(time.RFC3339)
			status.NextResetAt = &t
		}
	}

	return status
}

// GetResponsesChannelUsageStatus returns the full usage status for a Responses channel
func (m *UsageManager) GetResponsesChannelUsageStatus(channelIndex int) *ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	if channelIndex < 0 || channelIndex >= len(cfg.ResponsesUpstream) {
		return nil
	}

	upstream := cfg.ResponsesUpstream[channelIndex]
	if upstream.QuotaType == "" {
		return &ChannelUsageStatus{QuotaType: ""}
	}

	usage := m.GetResponsesUsage(channelIndex)
	var used float64
	var lastResetAt *string
	if usage != nil {
		used = usage.Used
		if !usage.LastResetAt.IsZero() {
			t := usage.LastResetAt.Format(time.RFC3339)
			lastResetAt = &t
		}
	}

	remaining := upstream.QuotaLimit - used
	if remaining < 0 {
		remaining = 0
	}

	var remainingPct float64
	if upstream.QuotaLimit > 0 {
		remainingPct = (remaining / upstream.QuotaLimit) * 100
	}

	status := &ChannelUsageStatus{
		QuotaType:    upstream.QuotaType,
		Limit:        upstream.QuotaLimit,
		Used:         used,
		Remaining:    remaining,
		RemainingPct: remainingPct,
		LastResetAt:  lastResetAt,
	}

	if upstream.QuotaResetAt != nil && upstream.QuotaResetInterval > 0 {
		nextReset := m.calculateNextReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
		if nextReset != nil {
			t := nextReset.Format(time.RFC3339)
			status.NextResetAt = &t
		}
	}

	return status
}

// calculateNextReset determines when the next reset should occur
func (m *UsageManager) calculateNextReset(firstReset *time.Time, interval int, unit string) *time.Time {
	if firstReset == nil || interval <= 0 {
		return nil
	}

	now := time.Now()
	next := *firstReset

	// If first reset is in the future, that's the next reset
	if next.After(now) {
		return &next
	}

	// Calculate interval duration
	var duration time.Duration
	switch unit {
	case "hours":
		duration = time.Duration(interval) * time.Hour
	case "days":
		duration = time.Duration(interval) * 24 * time.Hour
	case "weeks":
		duration = time.Duration(interval) * 7 * 24 * time.Hour
	case "months":
		// Approximate month as 30 days
		duration = time.Duration(interval) * 30 * 24 * time.Hour
	default:
		duration = time.Duration(interval) * time.Hour
	}

	// Find the next reset time after now
	for next.Before(now) {
		next = next.Add(duration)
	}

	return &next
}

// calculateNextResetFromNow calculates the next reset time from now based on interval and unit
func (m *UsageManager) calculateNextResetFromNow(interval int, unit string) time.Time {
	now := time.Now()
	switch unit {
	case "hours":
		return now.Add(time.Duration(interval) * time.Hour)
	case "days":
		return now.AddDate(0, 0, interval)
	case "weeks":
		return now.AddDate(0, 0, interval*7)
	case "months":
		return now.AddDate(0, interval, 0)
	default:
		return now.AddDate(0, 0, interval) // default to days
	}
}

// autoResetLoop periodically checks if any channels need auto-reset
func (m *UsageManager) autoResetLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAndAutoReset()
		case <-m.stopChan:
			return
		}
	}
}

// checkAndAutoReset resets quotas that have passed their reset time
func (m *UsageManager) checkAndAutoReset() {
	cfg := m.configMgr.GetConfig()
	now := time.Now()

	// Check Messages channels
	for i, upstream := range cfg.Upstream {
		if upstream.QuotaType == "" || upstream.QuotaResetAt == nil || upstream.QuotaResetInterval <= 0 {
			continue
		}

		nextReset := m.calculateNextReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
		if nextReset == nil {
			continue
		}

		usage := m.GetUsage(i)
		if usage == nil {
			continue
		}

		// Check if we've passed the reset time since last reset
		// Only reset if: nextReset time has passed AND last reset was before nextReset
		if nextReset.Before(now) && usage.LastResetAt.Before(*nextReset) {
			if err := m.ResetUsage(i); err != nil {
				log.Printf("⚠️ 自动重置 Messages 渠道 [%d] 配额失败: %v", i, err)
			} else {
				log.Printf("⏰ Messages 渠道 [%d] %s 配额已自动重置", i, upstream.Name)
			}
		}
	}

	// Check Responses channels
	for i, upstream := range cfg.ResponsesUpstream {
		if upstream.QuotaType == "" || upstream.QuotaResetAt == nil || upstream.QuotaResetInterval <= 0 {
			continue
		}

		nextReset := m.calculateNextReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
		if nextReset == nil {
			continue
		}

		usage := m.GetResponsesUsage(i)
		if usage == nil {
			continue
		}

		// Only reset if: nextReset time has passed AND last reset was before nextReset
		if nextReset.Before(now) && usage.LastResetAt.Before(*nextReset) {
			if err := m.ResetResponsesUsage(i); err != nil {
				log.Printf("⚠️ 自动重置 Responses 渠道 [%d] 配额失败: %v", i, err)
			} else {
				log.Printf("⏰ Responses 渠道 [%d] %s 配额已自动重置", i, upstream.Name)
			}
		}
	}
}

// Close stops the auto-reset loop
func (m *UsageManager) Close() {
	close(m.stopChan)
}

// GetAllChannelUsageStatuses returns usage statuses for all Messages channels
func (m *UsageManager) GetAllChannelUsageStatuses() map[int]*ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	result := make(map[int]*ChannelUsageStatus)

	for i := range cfg.Upstream {
		status := m.GetChannelUsageStatus(i)
		if status != nil && status.QuotaType != "" {
			result[i] = status
		}
	}

	return result
}

// GetAllResponsesChannelUsageStatuses returns usage statuses for all Responses channels
func (m *UsageManager) GetAllResponsesChannelUsageStatuses() map[int]*ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	result := make(map[int]*ChannelUsageStatus)

	for i := range cfg.ResponsesUpstream {
		status := m.GetResponsesChannelUsageStatus(i)
		if status != nil && status.QuotaType != "" {
			result[i] = status
		}
	}

	return result
}
