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
	dbStorage *DBUsageStorage // Database storage adapter (nil if using JSON-only mode)
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

// SetDBStorage sets the database storage adapter for write-through caching
func (m *UsageManager) SetDBStorage(dbStorage *DBUsageStorage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbStorage = dbStorage
}

// ReloadFromDB reloads usage data from the database (call after SetDBStorage)
func (m *UsageManager) ReloadFromDB() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.dbStorage == nil {
		return
	}

	usage, err := m.dbStorage.LoadAll()
	if err != nil {
		log.Printf("⚠️ Failed to reload quota usage from database: %v", err)
		return
	}
	m.usage = usage
	log.Printf("✅ Quota usage reloaded from database")
}

// load reads usage data from file or database
func (m *UsageManager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// When using database storage, load from DB
	if m.dbStorage != nil {
		usage, err := m.dbStorage.LoadAll()
		if err != nil {
			return fmt.Errorf("failed to load quota usage from database: %w", err)
		}
		m.usage = usage
		return nil
	}

	// JSON-only mode: read from file
	if _, err := os.Stat(m.usageFile); os.IsNotExist(err) {
		m.usage = UsageFile{
			Messages:  make(map[string]ChannelUsage),
			Responses: make(map[string]ChannelUsage),
			Gemini:    make(map[string]ChannelUsage),
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
	if m.usage.Gemini == nil {
		m.usage.Gemini = make(map[string]ChannelUsage)
	}

	return nil
}

// save persists usage data to file or database
func (m *UsageManager) save() error {
	// When using database storage, write to DB
	if m.dbStorage != nil {
		usageCopy := m.usage
		go func() {
			if err := m.dbStorage.SaveAll(usageCopy); err != nil {
				log.Printf("⚠️ Failed to sync quota usage to database: %v", err)
			}
		}()
		return nil
	}

	// JSON-only mode: write to file
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

// GetGeminiUsage returns the current usage for a Gemini channel
func (m *UsageManager) GetGeminiUsage(channelIndex int) *ChannelUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := strconv.Itoa(channelIndex)
	if usage, ok := m.usage.Gemini[key]; ok {
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

// IncrementGeminiUsage adds to the usage counter for a Gemini channel
func (m *UsageManager) IncrementGeminiUsage(channelIndex int, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	usage := m.usage.Gemini[key]
	usage.Used += amount
	if usage.LastResetAt.IsZero() {
		usage.LastResetAt = time.Now()
	}
	m.usage.Gemini[key] = usage

	// For rolling mode: update quotaResetAt if it's in the past
	cfg := m.configMgr.GetConfig()
	if channelIndex >= 0 && channelIndex < len(cfg.GeminiUpstream) {
		upstream := cfg.GeminiUpstream[channelIndex]
		if upstream.QuotaResetMode == "rolling" &&
			upstream.QuotaResetAt != nil &&
			upstream.QuotaResetAt.Before(time.Now()) &&
			upstream.QuotaResetInterval > 0 {
			newResetAt := m.calculateNextResetFromNow(upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if err := m.configMgr.UpdateGeminiChannelQuotaResetAt(channelIndex, newResetAt); err != nil {
				log.Printf("⚠️ Failed to update quotaResetAt for Gemini rolling mode: %v", err)
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

// ResetGeminiUsage resets the usage counter for a Gemini channel
func (m *UsageManager) ResetGeminiUsage(channelIndex int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strconv.Itoa(channelIndex)
	m.usage.Gemini[key] = ChannelUsage{
		Used:        0,
		LastResetAt: time.Now(),
	}

	log.Printf("🔄 Gemini 渠道 [%d] 配额已重置", channelIndex)
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

// GetGeminiChannelUsageStatus returns the full usage status for a Gemini channel
func (m *UsageManager) GetGeminiChannelUsageStatus(channelIndex int) *ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	if channelIndex < 0 || channelIndex >= len(cfg.GeminiUpstream) {
		return nil
	}

	upstream := cfg.GeminiUpstream[channelIndex]
	if upstream.QuotaType == "" {
		return &ChannelUsageStatus{QuotaType: ""}
	}

	usage := m.GetGeminiUsage(channelIndex)
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

// calculatePreviousReset returns the most recent scheduled reset time that is in the past (for fixed mode)
func (m *UsageManager) calculatePreviousReset(firstReset *time.Time, interval int, unit string) *time.Time {
	if firstReset == nil || interval <= 0 {
		return nil
	}

	now := time.Now()

	// If first reset is in the future, there's no previous reset yet
	if firstReset.After(now) {
		return nil
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
		duration = time.Duration(interval) * 30 * 24 * time.Hour
	default:
		duration = time.Duration(interval) * time.Hour
	}

	// Find the most recent reset time that is <= now
	prev := *firstReset
	for {
		next := prev.Add(duration)
		if next.After(now) {
			break
		}
		prev = next
	}

	return &prev
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

		usage := m.GetUsage(i)
		if usage == nil {
			continue
		}

		var shouldReset bool

		if upstream.QuotaResetMode == "rolling" {
			// Rolling mode: quotaResetAt is the exact next reset time (dynamically updated on first request)
			// Simply check if we've passed that time and haven't reset since
			shouldReset = upstream.QuotaResetAt.Before(now) && usage.LastResetAt.Before(*upstream.QuotaResetAt)
		} else {
			// Fixed mode: calculate the most recent scheduled reset time from the original time
			previousReset := m.calculatePreviousReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if previousReset != nil {
				shouldReset = usage.LastResetAt.Before(*previousReset)
			}
		}

		if shouldReset {
			if err := m.ResetUsage(i); err != nil {
				log.Printf("⚠️ 自动重置 Messages 渠道 [%d] 配额失败: %v", i, err)
			} else {
				log.Printf("⏰ Messages 渠道 [%d] %s 配额已自动重置 (模式: %s)", i, upstream.Name, upstream.QuotaResetMode)
			}
		}
	}

	// Check Responses channels
	for i, upstream := range cfg.ResponsesUpstream {
		if upstream.QuotaType == "" || upstream.QuotaResetAt == nil || upstream.QuotaResetInterval <= 0 {
			continue
		}

		usage := m.GetResponsesUsage(i)
		if usage == nil {
			continue
		}

		var shouldReset bool

		if upstream.QuotaResetMode == "rolling" {
			// Rolling mode: quotaResetAt is the exact next reset time
			shouldReset = upstream.QuotaResetAt.Before(now) && usage.LastResetAt.Before(*upstream.QuotaResetAt)
		} else {
			// Fixed mode: calculate the most recent scheduled reset time
			previousReset := m.calculatePreviousReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if previousReset != nil {
				shouldReset = usage.LastResetAt.Before(*previousReset)
			}
		}

		if shouldReset {
			if err := m.ResetResponsesUsage(i); err != nil {
				log.Printf("⚠️ 自动重置 Responses 渠道 [%d] 配额失败: %v", i, err)
			} else {
				log.Printf("⏰ Responses 渠道 [%d] %s 配额已自动重置 (模式: %s)", i, upstream.Name, upstream.QuotaResetMode)
			}
		}
	}

	// Check Gemini channels
	for i, upstream := range cfg.GeminiUpstream {
		if upstream.QuotaType == "" || upstream.QuotaResetAt == nil || upstream.QuotaResetInterval <= 0 {
			continue
		}

		usage := m.GetGeminiUsage(i)
		if usage == nil {
			continue
		}

		var shouldReset bool

		if upstream.QuotaResetMode == "rolling" {
			shouldReset = upstream.QuotaResetAt.Before(now) && usage.LastResetAt.Before(*upstream.QuotaResetAt)
		} else {
			previousReset := m.calculatePreviousReset(upstream.QuotaResetAt, upstream.QuotaResetInterval, upstream.QuotaResetUnit)
			if previousReset != nil {
				shouldReset = usage.LastResetAt.Before(*previousReset)
			}
		}

		if shouldReset {
			if err := m.ResetGeminiUsage(i); err != nil {
				log.Printf("⚠️ 自动重置 Gemini 渠道 [%d] 配额失败: %v", i, err)
			} else {
				log.Printf("⏰ Gemini 渠道 [%d] %s 配额已自动重置 (模式: %s)", i, upstream.Name, upstream.QuotaResetMode)
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

// GetAllGeminiChannelUsageStatuses returns usage statuses for all Gemini channels
func (m *UsageManager) GetAllGeminiChannelUsageStatuses() map[int]*ChannelUsageStatus {
	cfg := m.configMgr.GetConfig()
	result := make(map[int]*ChannelUsageStatus)

	for i := range cfg.GeminiUpstream {
		status := m.GetGeminiChannelUsageStatus(i)
		if status != nil && status.QuotaType != "" {
			result[i] = status
		}
	}

	return result
}
