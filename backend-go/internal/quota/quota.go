// Package quota provides rate limit tracking for OpenAI/Codex channels
package quota

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Persister interface for quota persistence
type Persister interface {
	SaveChannelQuota(q *PersistedQuota) error
	GetChannelQuota(channelID int) (*PersistedQuota, error)
	GetAllChannelQuotas() ([]*PersistedQuota, error)
}

// PersistedQuota represents quota data for persistence
type PersistedQuota struct {
	ChannelID              int
	ChannelStableID        string
	ChannelName            string
	PlanType               string
	PrimaryUsedPercent     int
	PrimaryWindowMinutes   int
	PrimaryResetAt         *time.Time
	SecondaryUsedPercent   int
	SecondaryWindowMinutes int
	SecondaryResetAt       *time.Time
	CreditsHasCredits      bool
	CreditsUnlimited       bool
	CreditsBalance         string
	IsExceeded             bool
	ExceededAt             *time.Time
	RecoverAt              *time.Time
	ExceededReason         string
	UpdatedAt              time.Time
}

// CodexQuotaInfo contains Codex-specific quota information from response headers
type CodexQuotaInfo struct {
	PlanType string `json:"plan_type,omitempty"`

	ActiveLimit string `json:"active_limit,omitempty"`

	// Primary window (short-term, e.g., 5 hours)
	PrimaryUsedPercent      int       `json:"primary_used_percent"`
	PrimaryUsedPercentExact *float64  `json:"-"`
	PrimaryWindowMinutes    int       `json:"primary_window_minutes,omitempty"`
	PrimaryResetAt          time.Time `json:"primary_reset_at,omitempty"`

	// Secondary window (long-term, e.g., 7 days)
	SecondaryUsedPercent      int       `json:"secondary_used_percent"`
	SecondaryUsedPercentExact *float64  `json:"-"`
	SecondaryWindowMinutes    int       `json:"secondary_window_minutes,omitempty"`
	SecondaryResetAt          time.Time `json:"secondary_reset_at,omitempty"`

	// Over limit indicator
	PrimaryOverSecondaryLimitPercent int `json:"primary_over_secondary_limit_percent,omitempty"`

	// Credits info
	CreditsHasCredits bool   `json:"credits_has_credits"`
	CreditsUnlimited  bool   `json:"credits_unlimited"`
	CreditsBalance    string `json:"credits_balance,omitempty"`

	DetailedLimits []CodexQuotaLimitInfo `json:"detailed_limits,omitempty"`

	// Last updated timestamp
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexQuotaLimitInfo contains one named Codex metered limit family, such as
// x-codex-bengalfox-*.
type CodexQuotaLimitInfo struct {
	LimitID   string `json:"limit_id"`
	LimitName string `json:"limit_name,omitempty"`

	PrimaryUsedPercent         int       `json:"primary_used_percent"`
	PrimaryUsedPercentExact    *float64  `json:"-"`
	PrimaryWindowMinutes       int       `json:"primary_window_minutes,omitempty"`
	PrimaryResetAt             time.Time `json:"primary_reset_at,omitempty"`
	PrimaryResetAfterSeconds   int       `json:"primary_reset_after_seconds,omitempty"`
	SecondaryUsedPercent       int       `json:"secondary_used_percent"`
	SecondaryUsedPercentExact  *float64  `json:"-"`
	SecondaryWindowMinutes     int       `json:"secondary_window_minutes,omitempty"`
	SecondaryResetAt           time.Time `json:"secondary_reset_at,omitempty"`
	SecondaryResetAfterSeconds int       `json:"secondary_reset_after_seconds,omitempty"`

	PrimaryOverSecondaryLimitPercent int `json:"primary_over_secondary_limit_percent,omitempty"`
}

func (c *CodexQuotaLimitInfo) PrimaryUsedPercentValue() float64 {
	if c == nil {
		return 0
	}
	if c.PrimaryUsedPercentExact != nil {
		return *c.PrimaryUsedPercentExact
	}
	return float64(c.PrimaryUsedPercent)
}

func (c *CodexQuotaLimitInfo) SecondaryUsedPercentValue() float64 {
	if c == nil {
		return 0
	}
	if c.SecondaryUsedPercentExact != nil {
		return *c.SecondaryUsedPercentExact
	}
	return float64(c.SecondaryUsedPercent)
}

func (c *CodexQuotaInfo) PrimaryUsedPercentValue() float64 {
	if c == nil {
		return 0
	}
	if c.PrimaryUsedPercentExact != nil {
		return *c.PrimaryUsedPercentExact
	}
	return float64(c.PrimaryUsedPercent)
}

func (c *CodexQuotaInfo) SecondaryUsedPercentValue() float64 {
	if c == nil {
		return 0
	}
	if c.SecondaryUsedPercentExact != nil {
		return *c.SecondaryUsedPercentExact
	}
	return float64(c.SecondaryUsedPercent)
}

// RateLimitInfo contains rate limit information from OpenAI response headers (standard API)
type RateLimitInfo struct {
	// Request limits
	LimitRequests     int       `json:"limit_requests,omitempty"`
	RemainingRequests int       `json:"remaining_requests,omitempty"`
	ResetRequests     time.Time `json:"reset_requests,omitempty"`

	// Token limits
	LimitTokens     int       `json:"limit_tokens,omitempty"`
	RemainingTokens int       `json:"remaining_tokens,omitempty"`
	ResetTokens     time.Time `json:"reset_tokens,omitempty"`

	// Last updated timestamp
	UpdatedAt time.Time `json:"updated_at"`
}

// QuotaStatus represents the overall quota status for a channel
type QuotaStatus struct {
	ChannelID       int             `json:"channel_id"`
	ChannelStableID string          `json:"channel_stable_id,omitempty"`
	ChannelName     string          `json:"channel_name"`
	RateLimit       *RateLimitInfo  `json:"rate_limit,omitempty"`
	CodexQuota      *CodexQuotaInfo `json:"codex_quota,omitempty"`

	// Cooldown state (set when 429 is received)
	IsExceeded     bool      `json:"is_exceeded"`
	ExceededAt     time.Time `json:"exceeded_at,omitempty"`
	RecoverAt      time.Time `json:"recover_at,omitempty"`
	ExceededReason string    `json:"exceeded_reason,omitempty"`
}

// Manager manages quota tracking for channels
type Manager struct {
	mu        sync.RWMutex
	quotas    map[int]*QuotaStatus // keyed by channel index
	persister Persister
}

var (
	defaultManager *Manager
	once           sync.Once
)

// GetManager returns the singleton quota manager
func GetManager() *Manager {
	once.Do(func() {
		defaultManager = &Manager{
			quotas: make(map[int]*QuotaStatus),
		}
	})
	return defaultManager
}

// SetPersister sets the persistence layer and loads existing data
func (m *Manager) SetPersister(p Persister) {
	if m == nil || p == nil {
		return
	}

	m.mu.Lock()
	m.persister = p
	m.mu.Unlock()

	// Load existing quota data from persistence
	m.loadFromPersistence()
}

// loadFromPersistence loads all quota data from the persistence layer
func (m *Manager) loadFromPersistence() {
	if m.persister == nil {
		return
	}

	quotas, err := m.persister.GetAllChannelQuotas()
	if err != nil {
		log.Printf("⚠️ [Quota] Failed to load quota data from persistence: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	loaded := 0
	for _, pq := range quotas {
		status := &QuotaStatus{
			ChannelID:       pq.ChannelID,
			ChannelStableID: strings.TrimSpace(pq.ChannelStableID),
			ChannelName:     pq.ChannelName,
			IsExceeded:      pq.IsExceeded,
			ExceededReason:  pq.ExceededReason,
		}

		if pq.ExceededAt != nil {
			status.ExceededAt = *pq.ExceededAt
		}
		if pq.RecoverAt != nil {
			status.RecoverAt = *pq.RecoverAt
		}

		// Restore Codex quota if we have plan type or usage data
		if pq.PlanType != "" || pq.PrimaryUsedPercent > 0 || pq.SecondaryUsedPercent > 0 {
			status.CodexQuota = &CodexQuotaInfo{
				PlanType:               pq.PlanType,
				PrimaryUsedPercent:     pq.PrimaryUsedPercent,
				PrimaryWindowMinutes:   pq.PrimaryWindowMinutes,
				SecondaryUsedPercent:   pq.SecondaryUsedPercent,
				SecondaryWindowMinutes: pq.SecondaryWindowMinutes,
				CreditsHasCredits:      pq.CreditsHasCredits,
				CreditsUnlimited:       pq.CreditsUnlimited,
				CreditsBalance:         pq.CreditsBalance,
				UpdatedAt:              pq.UpdatedAt,
			}
			if pq.PrimaryResetAt != nil {
				status.CodexQuota.PrimaryResetAt = *pq.PrimaryResetAt
			}
			if pq.SecondaryResetAt != nil {
				status.CodexQuota.SecondaryResetAt = *pq.SecondaryResetAt
			}
		}

		m.quotas[pq.ChannelID] = status
		loaded++
	}

	if loaded > 0 {
		log.Printf("📊 [Quota] Loaded %d channel quota(s) from persistence", loaded)
	}
}

// persist saves the current quota status to persistence
func (m *Manager) persist(status *QuotaStatus) {
	if m.persister == nil || status == nil {
		return
	}

	pq := &PersistedQuota{
		ChannelID:       status.ChannelID,
		ChannelStableID: strings.TrimSpace(status.ChannelStableID),
		ChannelName:     status.ChannelName,
		IsExceeded:      status.IsExceeded,
		ExceededReason:  status.ExceededReason,
		UpdatedAt:       time.Now(),
	}

	if !status.ExceededAt.IsZero() {
		pq.ExceededAt = &status.ExceededAt
	}
	if !status.RecoverAt.IsZero() {
		pq.RecoverAt = &status.RecoverAt
	}

	if status.CodexQuota != nil {
		cq := status.CodexQuota
		pq.PlanType = cq.PlanType
		pq.PrimaryUsedPercent = cq.PrimaryUsedPercent
		pq.PrimaryWindowMinutes = cq.PrimaryWindowMinutes
		pq.SecondaryUsedPercent = cq.SecondaryUsedPercent
		pq.SecondaryWindowMinutes = cq.SecondaryWindowMinutes
		pq.CreditsHasCredits = cq.CreditsHasCredits
		pq.CreditsUnlimited = cq.CreditsUnlimited
		pq.CreditsBalance = cq.CreditsBalance

		if !cq.PrimaryResetAt.IsZero() {
			pq.PrimaryResetAt = &cq.PrimaryResetAt
		}
		if !cq.SecondaryResetAt.IsZero() {
			pq.SecondaryResetAt = &cq.SecondaryResetAt
		}
	}

	if err := m.persister.SaveChannelQuota(pq); err != nil {
		log.Printf("⚠️ [Quota] Failed to persist quota for channel %d: %v", status.ChannelID, err)
	}
}

// UpdateFromHeaders updates quota info from HTTP response headers
func (m *Manager) UpdateFromHeaders(channelID int, channelName string, headers http.Header) {
	m.UpdateFromHeadersForChannel(channelID, "", channelName, headers)
}

// UpdateFromHeadersForChannel updates quota info and records the stable channel
// ID when available, preventing stale index/name matches after channel changes.
func (m *Manager) UpdateFromHeadersForChannel(channelID int, channelStableID string, channelName string, headers http.Header) {
	if m == nil {
		return
	}

	codexInfo := parseCodexHeaders(headers)
	rateLimitInfo := parseRateLimitHeaders(headers)
	if codexInfo == nil && rateLimitInfo == nil {
		// No quota headers found - this is normal for non-OAuth channels.
		return
	}

	if codexInfo != nil {
		log.Printf("📊 [Quota] Updated Codex quota for channel %d (%s): primary=%.2f%%, secondary=%.2f%%, plan=%s",
			channelID, channelName,
			codexInfo.PrimaryUsedPercentValue(), codexInfo.SecondaryUsedPercentValue(), codexInfo.PlanType)
	}

	if rateLimitInfo != nil {
		log.Printf("📊 [Quota] Updated rate limits for channel %d (%s): requests=%d/%d, tokens=%d/%d",
			channelID, channelName,
			rateLimitInfo.RemainingRequests, rateLimitInfo.LimitRequests,
			rateLimitInfo.RemainingTokens, rateLimitInfo.LimitTokens)
	}

	m.mu.Lock()
	status := m.getOrCreateStatusForChannel(channelID, channelStableID, channelName)
	if codexInfo != nil {
		status.CodexQuota = codexInfo
	}
	if rateLimitInfo != nil {
		status.RateLimit = rateLimitInfo
	}
	status.ChannelStableID = strings.TrimSpace(channelStableID)
	status.ChannelName = channelName

	// Clear exceeded state if we got a successful response.
	if status.IsExceeded && time.Now().After(status.RecoverAt) {
		status.IsExceeded = false
		status.ExceededReason = ""
	}

	if codexInfo != nil {
		m.persist(status)
	}
	m.mu.Unlock()
}

// UpdateCodexQuotaForChannel stores Codex quota data obtained from a direct
// usage endpoint refresh.
func (m *Manager) UpdateCodexQuotaForChannel(channelID int, channelStableID string, channelName string, codexInfo *CodexQuotaInfo) {
	if m == nil || codexInfo == nil {
		return
	}
	if codexInfo.UpdatedAt.IsZero() {
		codexInfo.UpdatedAt = time.Now()
	}

	log.Printf("📊 [Quota] Refreshed Codex quota for channel %d (%s): primary=%.2f%%, secondary=%.2f%%, plan=%s",
		channelID, channelName,
		codexInfo.PrimaryUsedPercentValue(), codexInfo.SecondaryUsedPercentValue(), codexInfo.PlanType)

	m.mu.Lock()
	status := m.getOrCreateStatusForChannel(channelID, channelStableID, channelName)
	status.CodexQuota = codexInfo
	status.ChannelStableID = strings.TrimSpace(channelStableID)
	status.ChannelName = channelName

	if status.IsExceeded && time.Now().After(status.RecoverAt) {
		status.IsExceeded = false
		status.ExceededReason = ""
	}

	m.persist(status)
	m.mu.Unlock()
}

// getOrCreateStatus returns existing status or creates a new one (must be called with lock held)
func (m *Manager) getOrCreateStatus(channelID int, channelName string) *QuotaStatus {
	return m.getOrCreateStatusForChannel(channelID, "", channelName)
}

// getOrCreateStatusForChannel returns existing status or creates a new one
// (must be called with lock held).
func (m *Manager) getOrCreateStatusForChannel(channelID int, channelStableID string, channelName string) *QuotaStatus {
	stableID := strings.TrimSpace(channelStableID)
	name := strings.TrimSpace(channelName)

	if stableID != "" {
		if status := m.findStatusByStableIDLocked(stableID); status != nil {
			status.ChannelID = channelID
			status.ChannelStableID = stableID
			if name != "" {
				status.ChannelName = name
			}
			return status
		}
	}

	if status, exists := m.quotas[channelID]; exists {
		statusStableID := strings.TrimSpace(status.ChannelStableID)
		if stableID == "" || statusStableID == "" || statusStableID == stableID {
			status.ChannelStableID = stableID
			if name != "" {
				status.ChannelName = name
			}
			return status
		}
	}

	status := &QuotaStatus{
		ChannelID:       channelID,
		ChannelStableID: stableID,
		ChannelName:     channelName,
	}
	m.quotas[m.quotaMapKeyForNewStatusLocked(channelID)] = status
	return status
}

func (m *Manager) findStatusByStableIDLocked(stableID string) *QuotaStatus {
	stableID = strings.TrimSpace(stableID)
	if stableID == "" {
		return nil
	}

	var match *QuotaStatus
	var matchUpdatedAt time.Time
	for _, candidate := range m.quotas {
		if candidate == nil || strings.TrimSpace(candidate.ChannelStableID) != stableID {
			continue
		}
		candidateUpdatedAt := statusUpdatedAt(candidate)
		if match == nil || candidateUpdatedAt.After(matchUpdatedAt) {
			match = candidate
			matchUpdatedAt = candidateUpdatedAt
		}
	}
	return match
}

func (m *Manager) quotaMapKeyForNewStatusLocked(channelID int) int {
	if _, exists := m.quotas[channelID]; !exists {
		return channelID
	}

	for key := -1; ; key-- {
		if _, exists := m.quotas[key]; !exists {
			return key
		}
	}
}

// SetExceeded marks a channel as quota exceeded
func (m *Manager) SetExceeded(channelID int, channelName string, reason string, retryAfter time.Duration) {
	m.SetExceededForChannel(channelID, "", channelName, reason, retryAfter)
}

// SetExceededForChannel marks a stable channel identity as quota exceeded.
func (m *Manager) SetExceededForChannel(channelID int, channelStableID string, channelName string, reason string, retryAfter time.Duration) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.getOrCreateStatusForChannel(channelID, channelStableID, channelName)

	now := time.Now()
	status.IsExceeded = true
	status.ExceededAt = now
	status.ExceededReason = reason
	status.ChannelName = channelName

	if retryAfter > 0 {
		status.RecoverAt = now.Add(retryAfter)
	} else {
		// Default cooldown: 5 minutes
		status.RecoverAt = now.Add(5 * time.Minute)
	}

	// Persist the update
	m.persist(status)
}

// ClearExceeded clears the exceeded state for a channel
func (m *Manager) ClearExceeded(channelID int) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if status, exists := m.quotas[channelID]; exists {
		status.IsExceeded = false
		status.ExceededReason = ""
		m.persist(status)
	}
}

// GetStatus returns the quota status for a channel
func (m *Manager) GetStatus(channelID int) *QuotaStatus {
	if m == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if status, exists := m.quotas[channelID]; exists {
		return cloneStatus(status)
	}

	return nil
}

// GetStatusForChannel returns quota status for a channel identity.
// It protects against stale index remapping by validating stable channel ID
// first, then channel name for legacy entries without stable IDs.
//
// This method is intentionally read-only. Reorders and DB reloads can cause
// two channels to temporarily "swap" indices while in-flight requests still
// report quota updates using the previous index. Mutating the map during read
// lookups can destroy the other channel's cached quota state.
func (m *Manager) GetStatusForChannel(channelID int, channelStableID string, channelName string) *QuotaStatus {
	if m == nil {
		return nil
	}

	stableID := strings.TrimSpace(channelStableID)
	name := strings.TrimSpace(channelName)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if status, exists := m.quotas[channelID]; exists {
		if stableID != "" {
			statusStableID := strings.TrimSpace(status.ChannelStableID)
			if statusStableID == stableID {
				return cloneStatusForChannel(status, channelID, stableID, name)
			}
			if statusStableID == "" && name != "" && status.ChannelName == name {
				return cloneStatusForChannel(status, channelID, stableID, name)
			}
		} else if strings.TrimSpace(status.ChannelStableID) != "" {
			return nil
		}

		// Fast path: channel identity matches or cannot be validated.
		if stableID == "" && (name == "" || status.ChannelName == "" || status.ChannelName == name) {
			return cloneStatusForChannel(status, channelID, stableID, name)
		}
	}

	// Slow path: find best status with matching stable ID or legacy name.
	var match *QuotaStatus
	var matchUpdatedAt time.Time
	for _, candidate := range m.quotas {
		if candidate == nil {
			continue
		}

		candidateStableID := strings.TrimSpace(candidate.ChannelStableID)
		matchesStableID := stableID != "" && candidateStableID == stableID
		matchesLegacyName := stableID == "" && candidateStableID == "" && name != "" && candidate.ChannelName == name
		if !matchesStableID && !matchesLegacyName {
			continue
		}

		candidateUpdatedAt := statusUpdatedAt(candidate)
		if match == nil || candidateUpdatedAt.After(matchUpdatedAt) {
			match = candidate
			matchUpdatedAt = candidateUpdatedAt
		}
	}

	if match == nil {
		return nil
	}

	return cloneStatusForChannel(match, channelID, stableID, name)
}

func cloneStatusForChannel(status *QuotaStatus, channelID int, channelStableID string, channelName string) *QuotaStatus {
	cloned := cloneStatus(status)
	if cloned == nil {
		return nil
	}
	cloned.ChannelID = channelID
	if stableID := strings.TrimSpace(channelStableID); stableID != "" {
		cloned.ChannelStableID = stableID
	}
	if name := strings.TrimSpace(channelName); name != "" {
		cloned.ChannelName = name
	}
	return cloned
}

func cloneStatus(status *QuotaStatus) *QuotaStatus {
	if status == nil {
		return nil
	}
	cloned := *status
	if status.RateLimit != nil {
		rlCopy := *status.RateLimit
		cloned.RateLimit = &rlCopy
	}
	if status.CodexQuota != nil {
		cqCopy := *status.CodexQuota
		if status.CodexQuota.PrimaryUsedPercentExact != nil {
			primaryExact := *status.CodexQuota.PrimaryUsedPercentExact
			cqCopy.PrimaryUsedPercentExact = &primaryExact
		}
		if status.CodexQuota.SecondaryUsedPercentExact != nil {
			secondaryExact := *status.CodexQuota.SecondaryUsedPercentExact
			cqCopy.SecondaryUsedPercentExact = &secondaryExact
		}
		if status.CodexQuota.DetailedLimits != nil {
			cqCopy.DetailedLimits = make([]CodexQuotaLimitInfo, len(status.CodexQuota.DetailedLimits))
			copy(cqCopy.DetailedLimits, status.CodexQuota.DetailedLimits)
			for i := range cqCopy.DetailedLimits {
				if status.CodexQuota.DetailedLimits[i].PrimaryUsedPercentExact != nil {
					primaryExact := *status.CodexQuota.DetailedLimits[i].PrimaryUsedPercentExact
					cqCopy.DetailedLimits[i].PrimaryUsedPercentExact = &primaryExact
				}
				if status.CodexQuota.DetailedLimits[i].SecondaryUsedPercentExact != nil {
					secondaryExact := *status.CodexQuota.DetailedLimits[i].SecondaryUsedPercentExact
					cqCopy.DetailedLimits[i].SecondaryUsedPercentExact = &secondaryExact
				}
			}
		}
		cloned.CodexQuota = &cqCopy
	}
	return &cloned
}

func statusUpdatedAt(status *QuotaStatus) time.Time {
	if status == nil {
		return time.Time{}
	}
	updatedAt := status.ExceededAt
	if status.CodexQuota != nil && status.CodexQuota.UpdatedAt.After(updatedAt) {
		updatedAt = status.CodexQuota.UpdatedAt
	}
	if status.RateLimit != nil && status.RateLimit.UpdatedAt.After(updatedAt) {
		updatedAt = status.RateLimit.UpdatedAt
	}
	return updatedAt
}

// parseCodexHeaders extracts Codex quota info from response headers
func parseCodexHeaders(headers http.Header) *CodexQuotaInfo {
	if headers == nil {
		return nil
	}

	// Check if this is a Codex response by looking for Codex-specific headers
	planType := headers.Get("X-Codex-Plan-Type")
	primaryUsed := headers.Get("X-Codex-Primary-Used-Percent")
	secondaryUsed := headers.Get("X-Codex-Secondary-Used-Percent")
	activeLimit := strings.TrimSpace(headers.Get("X-Codex-Active-Limit"))
	detailedLimits := parseCodexDetailedLimits(headers)

	// Need at least one Codex header to proceed
	if planType == "" && primaryUsed == "" && secondaryUsed == "" && activeLimit == "" && len(detailedLimits) == 0 {
		return nil
	}

	info := &CodexQuotaInfo{
		PlanType:       planType,
		ActiveLimit:    activeLimit,
		DetailedLimits: detailedLimits,
		UpdatedAt:      time.Now(),
	}

	// Parse primary window info
	if v := primaryUsed; v != "" {
		if percent, ok := parseCodexPercentValue(v); ok {
			setCodexUsagePercent(&info.PrimaryUsedPercent, &info.PrimaryUsedPercentExact, percent)
		}
	}
	if v := headers.Get("X-Codex-Primary-Window-Minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.PrimaryWindowMinutes = n
		}
	}
	if v := headers.Get("X-Codex-Primary-Reset-At"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			info.PrimaryResetAt = time.Unix(n, 0)
		}
	}

	// Parse secondary window info
	if v := secondaryUsed; v != "" {
		if percent, ok := parseCodexPercentValue(v); ok {
			setCodexUsagePercent(&info.SecondaryUsedPercent, &info.SecondaryUsedPercentExact, percent)
		}
	}
	if v := headers.Get("X-Codex-Secondary-Window-Minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.SecondaryWindowMinutes = n
		}
	}
	if v := headers.Get("X-Codex-Secondary-Reset-At"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			info.SecondaryResetAt = time.Unix(n, 0)
		}
	}

	// Parse over limit indicator
	if v := headers.Get("X-Codex-Primary-Over-Secondary-Limit-Percent"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.PrimaryOverSecondaryLimitPercent = n
		}
	}

	// Parse credits info
	if v := headers.Get("X-Codex-Credits-Has-Credits"); v != "" {
		info.CreditsHasCredits = strings.EqualFold(v, "true")
	}
	if v := headers.Get("X-Codex-Credits-Unlimited"); v != "" {
		info.CreditsUnlimited = strings.EqualFold(v, "true")
	}
	info.CreditsBalance = headers.Get("X-Codex-Credits-Balance")

	return info
}

func parseCodexDetailedLimits(headers http.Header) []CodexQuotaLimitInfo {
	if headers == nil {
		return nil
	}

	limitParts := map[string]struct{}{}
	for key := range headers {
		if limitPart := codexDetailedLimitPartFromHeader(key); limitPart != "" {
			limitParts[limitPart] = struct{}{}
		}
	}
	if len(limitParts) == 0 {
		return nil
	}

	parts := make([]string, 0, len(limitParts))
	for part := range limitParts {
		parts = append(parts, part)
	}
	sort.Strings(parts)

	limits := make([]CodexQuotaLimitInfo, 0, len(parts))
	for _, part := range parts {
		prefix := "X-Codex-" + part
		limit := CodexQuotaLimitInfo{
			LimitID:   "codex_" + strings.ReplaceAll(strings.ToLower(part), "-", "_"),
			LimitName: strings.TrimSpace(headers.Get(prefix + "-Limit-Name")),
		}

		hasData := limit.LimitName != ""
		if v := headers.Get(prefix + "-Primary-Used-Percent"); v != "" {
			if percent, ok := parseCodexPercentValue(v); ok {
				setCodexUsagePercent(&limit.PrimaryUsedPercent, &limit.PrimaryUsedPercentExact, percent)
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Primary-Window-Minutes"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit.PrimaryWindowMinutes = n
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Primary-Reset-At"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				limit.PrimaryResetAt = time.Unix(n, 0)
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Primary-Reset-After-Seconds"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit.PrimaryResetAfterSeconds = n
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Secondary-Used-Percent"); v != "" {
			if percent, ok := parseCodexPercentValue(v); ok {
				setCodexUsagePercent(&limit.SecondaryUsedPercent, &limit.SecondaryUsedPercentExact, percent)
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Secondary-Window-Minutes"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit.SecondaryWindowMinutes = n
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Secondary-Reset-At"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				limit.SecondaryResetAt = time.Unix(n, 0)
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Secondary-Reset-After-Seconds"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit.SecondaryResetAfterSeconds = n
				hasData = true
			}
		}
		if v := headers.Get(prefix + "-Primary-Over-Secondary-Limit-Percent"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit.PrimaryOverSecondaryLimitPercent = n
				hasData = true
			}
		}

		if hasData {
			limits = append(limits, limit)
		}
	}

	return limits
}

func codexDetailedLimitPartFromHeader(headerName string) string {
	normalized := strings.ToLower(strings.TrimSpace(headerName))
	if !strings.HasPrefix(normalized, "x-codex-") {
		return ""
	}

	suffixes := []string{
		"-primary-used-percent",
		"-primary-window-minutes",
		"-primary-reset-at",
		"-primary-reset-after-seconds",
		"-primary-over-secondary-limit-percent",
		"-secondary-used-percent",
		"-secondary-window-minutes",
		"-secondary-reset-at",
		"-secondary-reset-after-seconds",
		"-limit-name",
	}
	for _, suffix := range suffixes {
		prefix := strings.TrimSuffix(normalized, suffix)
		if prefix == normalized {
			continue
		}
		limitPart := strings.TrimPrefix(prefix, "x-codex-")
		if limitPart == prefix || limitPart == "" {
			continue
		}
		switch limitPart {
		case "primary", "secondary", "credits", "plan", "active":
			continue
		}
		return limitPart
	}

	return ""
}

func parseCodexPercentHeader(value string) (int, bool) {
	percent, ok := parseCodexPercentValue(value)
	if !ok {
		return 0, false
	}
	return int(math.Round(percent)), true
}

func parseCodexPercentValue(value string) (float64, bool) {
	percent, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0, false
	}
	return clampCodexUsagePercent(percent), true
}

// ParseCodexUsagePayload converts ChatGPT Codex usage endpoint JSON into the
// same quota structure used by response-header tracking.
func ParseCodexUsagePayload(payload []byte) (*CodexQuotaInfo, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, fmt.Errorf("parse codex usage payload: %w", err)
	}

	info := &CodexQuotaInfo{
		PlanType:  strings.TrimSpace(stringFromAny(firstCodexUsageValue(root, "plan_type", "planType"))),
		UpdatedAt: time.Now(),
	}

	rateLimit, _ := firstCodexUsageValue(root, "rate_limit", "rateLimit").(map[string]any)
	if rateLimit == nil {
		return nil, fmt.Errorf("codex usage payload missing rate_limit")
	}

	primary, secondary := findCodexUsageWindows(rateLimit)
	if primary == nil && secondary == nil {
		return nil, fmt.Errorf("codex usage payload missing quota windows")
	}

	applyCodexUsageWindow(primary, &info.PrimaryUsedPercent, &info.PrimaryUsedPercentExact, &info.PrimaryWindowMinutes, &info.PrimaryResetAt)
	applyCodexUsageWindow(secondary, &info.SecondaryUsedPercent, &info.SecondaryUsedPercentExact, &info.SecondaryWindowMinutes, &info.SecondaryResetAt)
	applyCodexUsageCredits(firstCodexUsageValue(root, "credits"), info)

	return info, nil
}

func findCodexUsageWindows(rateLimit map[string]any) (map[string]any, map[string]any) {
	primary, _ := firstCodexUsageValue(rateLimit, "primary_window", "primaryWindow").(map[string]any)
	secondary, _ := firstCodexUsageValue(rateLimit, "secondary_window", "secondaryWindow").(map[string]any)

	var fiveHour map[string]any
	var weekly map[string]any
	for _, candidate := range []map[string]any{primary, secondary} {
		if candidate == nil {
			continue
		}
		seconds, ok := numericCodexUsageValue(firstCodexUsageValue(candidate, "limit_window_seconds", "limitWindowSeconds"))
		if !ok {
			continue
		}
		switch int(math.Round(seconds)) {
		case 5 * 60 * 60:
			fiveHour = candidate
		case 7 * 24 * 60 * 60:
			weekly = candidate
		}
	}
	if fiveHour == nil {
		fiveHour = primary
	}
	if weekly == nil {
		weekly = secondary
	}
	return fiveHour, weekly
}

func applyCodexUsageWindow(window map[string]any, usedPercent *int, usedPercentExact **float64, windowMinutes *int, resetAt *time.Time) {
	if window == nil {
		return
	}

	if used, ok := numericCodexUsageValue(firstCodexUsageValue(window, "used_percent", "usedPercent")); ok {
		setCodexUsagePercent(usedPercent, usedPercentExact, used)
	}

	if seconds, ok := numericCodexUsageValue(firstCodexUsageValue(window, "limit_window_seconds", "limitWindowSeconds")); ok && seconds > 0 {
		*windowMinutes = int(math.Round(seconds / 60))
	}

	if unixSeconds, ok := numericCodexUsageValue(firstCodexUsageValue(window, "reset_at", "resetAt")); ok && unixSeconds > 0 {
		*resetAt = time.Unix(int64(unixSeconds), 0)
		return
	}

	if resetAfterSeconds, ok := numericCodexUsageValue(firstCodexUsageValue(window, "reset_after_seconds", "resetAfterSeconds")); ok && resetAfterSeconds > 0 {
		*resetAt = time.Now().Add(time.Duration(resetAfterSeconds) * time.Second)
	}
}

func setCodexUsagePercent(target *int, exactTarget **float64, value float64) {
	percent := clampCodexUsagePercent(value)
	*target = int(math.Round(percent))
	*exactTarget = &percent
}

func applyCodexUsageCredits(credits any, info *CodexQuotaInfo) {
	values, _ := credits.(map[string]any)
	if values == nil || info == nil {
		return
	}
	if hasCredits, ok := boolCodexUsageValue(firstCodexUsageValue(values, "has_credits", "hasCredits")); ok {
		info.CreditsHasCredits = hasCredits
	}
	if unlimited, ok := boolCodexUsageValue(firstCodexUsageValue(values, "unlimited")); ok {
		info.CreditsUnlimited = unlimited
	}
	if balance := strings.TrimSpace(stringFromAny(firstCodexUsageValue(values, "balance"))); balance != "" {
		info.CreditsBalance = balance
	}
}

func firstCodexUsageValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func numericCodexUsageValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func boolCodexUsageValue(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		switch normalized {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func stringFromAny(value any) string {
	if v, ok := value.(string); ok {
		return v
	}
	return ""
}

func clampCodexUsagePercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

// parseRateLimitHeaders extracts rate limit info from standard OpenAI response headers
func parseRateLimitHeaders(headers http.Header) *RateLimitInfo {
	if headers == nil {
		return nil
	}

	info := &RateLimitInfo{
		UpdatedAt: time.Now(),
	}

	hasData := false

	// Parse request limits
	if v := firstHeaderValue(headers, "x-ratelimit-limit-requests", "x-ratelimit-limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.LimitRequests = n
			hasData = true
		}
	}
	if v := firstHeaderValue(headers, "x-ratelimit-remaining-requests", "x-ratelimit-remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RemainingRequests = n
			hasData = true
		}
	}
	if v := firstHeaderValue(headers, "x-ratelimit-reset-requests", "x-ratelimit-reset"); v != "" {
		if t := parseResetTime(v); !t.IsZero() {
			info.ResetRequests = t
			hasData = true
		}
	}

	// Parse token limits
	if v := headers.Get("x-ratelimit-limit-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.LimitTokens = n
			hasData = true
		}
	}
	if v := headers.Get("x-ratelimit-remaining-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RemainingTokens = n
			hasData = true
		}
	}
	if v := headers.Get("x-ratelimit-reset-tokens"); v != "" {
		if t := parseResetTime(v); !t.IsZero() {
			info.ResetTokens = t
			hasData = true
		}
	}

	if !hasData {
		return nil
	}

	return info
}

func firstHeaderValue(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := headers.Get(name); value != "" {
			return value
		}
	}
	return ""
}

// parseResetTime parses the reset time from header value
// OpenAI uses formats like "1s", "1m", "1h", or ISO 8601 timestamps
func parseResetTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	// Try parsing as duration (e.g., "1s", "1m30s", "1h")
	if d, err := time.ParseDuration(value); err == nil {
		return time.Now().Add(d)
	}

	// Try parsing as ISO 8601 timestamp
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}

	// Try parsing as Unix timestamp
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(n, 0)
	}

	return time.Time{}
}

// ParseRetryAfter parses the Retry-After header value
func ParseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	// Try parsing as seconds
	if n, err := strconv.Atoi(value); err == nil {
		return time.Duration(n) * time.Second
	}

	// Try parsing as duration
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}

	// Try parsing as HTTP date
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		return time.Until(t)
	}

	return 0
}
