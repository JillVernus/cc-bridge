// Package quota provides rate limit tracking for OpenAI/Codex channels
package quota

import (
	"log"
	"net/http"
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

	// Primary window (short-term, e.g., 5 hours)
	PrimaryUsedPercent   int       `json:"primary_used_percent"`
	PrimaryWindowMinutes int       `json:"primary_window_minutes,omitempty"`
	PrimaryResetAt       time.Time `json:"primary_reset_at,omitempty"`

	// Secondary window (long-term, e.g., 7 days)
	SecondaryUsedPercent   int       `json:"secondary_used_percent"`
	SecondaryWindowMinutes int       `json:"secondary_window_minutes,omitempty"`
	SecondaryResetAt       time.Time `json:"secondary_reset_at,omitempty"`

	// Over limit indicator
	PrimaryOverSecondaryLimitPercent int `json:"primary_over_secondary_limit_percent,omitempty"`

	// Credits info
	CreditsHasCredits bool   `json:"credits_has_credits"`
	CreditsUnlimited  bool   `json:"credits_unlimited"`
	CreditsBalance    string `json:"credits_balance,omitempty"`

	// Last updated timestamp
	UpdatedAt time.Time `json:"updated_at"`
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
	ChannelID   int             `json:"channel_id"`
	ChannelName string          `json:"channel_name"`
	RateLimit   *RateLimitInfo  `json:"rate_limit,omitempty"`
	CodexQuota  *CodexQuotaInfo `json:"codex_quota,omitempty"`

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
			ChannelID:      pq.ChannelID,
			ChannelName:    pq.ChannelName,
			IsExceeded:     pq.IsExceeded,
			ExceededReason: pq.ExceededReason,
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
		ChannelID:      status.ChannelID,
		ChannelName:    status.ChannelName,
		IsExceeded:     status.IsExceeded,
		ExceededReason: status.ExceededReason,
		UpdatedAt:      time.Now(),
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
	if m == nil {
		return
	}

	// Try Codex headers first
	codexInfo := parseCodexHeaders(headers)
	if codexInfo != nil {
		log.Printf("📊 [Quota] Updated Codex quota for channel %d (%s): primary=%d%%, secondary=%d%%, plan=%s",
			channelID, channelName,
			codexInfo.PrimaryUsedPercent, codexInfo.SecondaryUsedPercent, codexInfo.PlanType)

		m.mu.Lock()
		status := m.getOrCreateStatus(channelID, channelName)
		status.CodexQuota = codexInfo
		status.ChannelName = channelName

		// Clear exceeded state if we got a successful response
		if status.IsExceeded && time.Now().After(status.RecoverAt) {
			status.IsExceeded = false
			status.ExceededReason = ""
		}

		// Persist the update
		m.persist(status)
		m.mu.Unlock()
		return
	}

	// Try standard OpenAI rate limit headers
	info := parseRateLimitHeaders(headers)
	if info != nil {
		log.Printf("📊 [Quota] Updated rate limits for channel %d (%s): requests=%d/%d, tokens=%d/%d",
			channelID, channelName,
			info.RemainingRequests, info.LimitRequests,
			info.RemainingTokens, info.LimitTokens)

		m.mu.Lock()
		status := m.getOrCreateStatus(channelID, channelName)
		status.RateLimit = info
		status.ChannelName = channelName

		// Clear exceeded state if we got a successful response
		if status.IsExceeded && time.Now().After(status.RecoverAt) {
			status.IsExceeded = false
			status.ExceededReason = ""
		}
		m.mu.Unlock()
		return
	}

	// No quota headers found - this is normal for non-OAuth channels
}

// getOrCreateStatus returns existing status or creates a new one (must be called with lock held)
func (m *Manager) getOrCreateStatus(channelID int, channelName string) *QuotaStatus {
	status, exists := m.quotas[channelID]
	if !exists {
		status = &QuotaStatus{
			ChannelID:   channelID,
			ChannelName: channelName,
		}
		m.quotas[channelID] = status
	}
	return status
}

// SetExceeded marks a channel as quota exceeded
func (m *Manager) SetExceeded(channelID int, channelName string, reason string, retryAfter time.Duration) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.getOrCreateStatus(channelID, channelName)

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

// GetStatusForChannel returns quota status for a channel index/name pair.
// It protects against stale index remapping by validating channel name.
// If index lookup mismatches the expected name, it attempts to locate by name
// and remap the in-memory key to the current channel index.
func (m *Manager) GetStatusForChannel(channelID int, channelName string) *QuotaStatus {
	if m == nil {
		return nil
	}

	name := strings.TrimSpace(channelName)

	m.mu.Lock()
	defer m.mu.Unlock()

	if status, exists := m.quotas[channelID]; exists {
		// Fast path: channel identity matches or cannot be validated.
		if name == "" || status.ChannelName == "" || status.ChannelName == name {
			if status.ChannelName == "" && name != "" {
				status.ChannelName = name
				m.persist(status)
			}
			return cloneStatus(status)
		}
	}

	if name == "" {
		return nil
	}

	// Slow path: find best status with matching channel name and remap.
	matchID := -1
	var match *QuotaStatus
	var matchUpdatedAt time.Time
	for id, candidate := range m.quotas {
		if candidate == nil || candidate.ChannelName != name {
			continue
		}
		candidateUpdatedAt := statusUpdatedAt(candidate)
		if match == nil || candidateUpdatedAt.After(matchUpdatedAt) {
			match = candidate
			matchID = id
			matchUpdatedAt = candidateUpdatedAt
		}
	}

	if match == nil {
		// Avoid returning stale data for the wrong channel.
		delete(m.quotas, channelID)
		return nil
	}

	remapped := *match
	remapped.ChannelID = channelID
	remapped.ChannelName = name
	m.quotas[channelID] = &remapped
	if matchID >= 0 && matchID != channelID {
		delete(m.quotas, matchID)
	}
	m.persist(&remapped)

	return cloneStatus(&remapped)
}

func cloneStatus(status *QuotaStatus) *QuotaStatus {
	if status == nil {
		return nil
	}
	copy := *status
	if status.RateLimit != nil {
		rlCopy := *status.RateLimit
		copy.RateLimit = &rlCopy
	}
	if status.CodexQuota != nil {
		cqCopy := *status.CodexQuota
		copy.CodexQuota = &cqCopy
	}
	return &copy
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

	// Need at least one Codex header to proceed
	if planType == "" && primaryUsed == "" && secondaryUsed == "" {
		return nil
	}

	info := &CodexQuotaInfo{
		PlanType:  planType,
		UpdatedAt: time.Now(),
	}

	// Parse primary window info
	if v := primaryUsed; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.PrimaryUsedPercent = n
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
		if n, err := strconv.Atoi(v); err == nil {
			info.SecondaryUsedPercent = n
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
	if v := headers.Get("x-ratelimit-limit-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.LimitRequests = n
			hasData = true
		}
	}
	if v := headers.Get("x-ratelimit-remaining-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RemainingRequests = n
			hasData = true
		}
	}
	if v := headers.Get("x-ratelimit-reset-requests"); v != "" {
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
