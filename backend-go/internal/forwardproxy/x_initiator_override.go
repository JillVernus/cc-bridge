package forwardproxy

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

const (
	XInitiatorOverrideModeFixedWindow       = "fixed_window"
	XInitiatorOverrideModeRelativeCountdown = "relative_countdown"
	XInitiatorOverrideModeWindowedQuota     = "windowed_quota"
	XInitiatorOverrideModeWindowedCost      = "windowed_cost"
)

type XInitiatorOverrideConfig struct {
	Enabled         bool    `json:"enabled"`
	Mode            string  `json:"mode"`
	DurationSeconds int     `json:"durationSeconds"`
	OverrideTimes   int     `json:"overrideTimes"`
	TotalCost       float64 `json:"totalCost"`
}

type XInitiatorOverrideRuntimeStatus struct {
	Enabled                 bool                             `json:"enabled"`
	Mode                    string                           `json:"mode"`
	ActiveDomains           int                              `json:"activeDomains"`
	NearestExpiryAt         *time.Time                       `json:"nearestExpiryAt,omitempty"`
	NearestRemainingSeconds int                              `json:"nearestRemainingSeconds"`
	Domains                 []XInitiatorOverrideDomainStatus `json:"domains,omitempty"`
}

type XInitiatorOverrideDomainStatus struct {
	Domain             string    `json:"domain"`
	DisplayName        string    `json:"displayName"`
	ExpiresAt          time.Time `json:"expiresAt"`
	RemainingSeconds   int       `json:"remainingSeconds"`
	RemainingOverrides *int      `json:"remainingOverrides,omitempty"`
	TotalOverrides     *int      `json:"totalOverrides,omitempty"`
}

type xInitiatorQuotaState struct {
	expiresAt          time.Time
	remainingOverrides int
	totalOverrides     int
}

type xInitiatorCostState struct {
	expiresAt       time.Time
	accumulatedCost float64
	budgetCost      float64
}

func validateXInitiatorOverrideConfig(cfg XInitiatorOverrideConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.DurationSeconds <= 0 {
		return &ValidationError{Message: "xInitiatorOverride.durationSeconds must be greater than 0"}
	}
	switch strings.TrimSpace(cfg.Mode) {
	case XInitiatorOverrideModeFixedWindow, XInitiatorOverrideModeRelativeCountdown:
		return nil
	case XInitiatorOverrideModeWindowedQuota:
		if cfg.OverrideTimes <= 0 {
			return &ValidationError{Message: fmt.Sprintf("xInitiatorOverride.overrideTimes must be greater than 0 for %q", XInitiatorOverrideModeWindowedQuota)}
		}
		return nil
	case XInitiatorOverrideModeWindowedCost:
		if cfg.TotalCost <= 0 {
			return &ValidationError{Message: fmt.Sprintf("xInitiatorOverride.totalCost must be greater than 0 for %q", XInitiatorOverrideModeWindowedCost)}
		}
		return nil
	default:
		return &ValidationError{Message: fmt.Sprintf(
			"xInitiatorOverride.mode must be one of %q, %q, %q, or %q",
			XInitiatorOverrideModeFixedWindow,
			XInitiatorOverrideModeRelativeCountdown,
			XInitiatorOverrideModeWindowedQuota,
			XInitiatorOverrideModeWindowedCost,
		)}
	}
}

func normalizeXInitiatorOverrideConfig(cfg XInitiatorOverrideConfig) XInitiatorOverrideConfig {
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = XInitiatorOverrideModeFixedWindow
	}
	if cfg.DurationSeconds <= 0 {
		cfg.DurationSeconds = 300
	}
	if cfg.OverrideTimes <= 0 {
		cfg.OverrideTimes = 1
	}
	if cfg.TotalCost <= 0 {
		cfg.TotalCost = 1
	}
	return cfg
}

func (s *Server) applyXInitiatorOverride(host string, headers http.Header) bool {
	if s == nil || headers == nil {
		return false
	}

	value := strings.TrimSpace(headers.Get("X-Initiator"))
	if !strings.EqualFold(value, "user") {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := s.xInitiatorOverride
	if !cfg.Enabled || cfg.DurationSeconds <= 0 {
		return false
	}

	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	hostKey := strings.ToLower(strings.TrimSpace(host))
	if hostKey == "" {
		return false
	}

	if cfg.Mode == XInitiatorOverrideModeWindowedQuota {
		if cfg.OverrideTimes <= 0 {
			return false
		}
		if s.xInitiatorQuotaDomainState == nil {
			s.xInitiatorQuotaDomainState = make(map[string]xInitiatorQuotaState)
		}

		state, active := s.xInitiatorQuotaDomainState[hostKey]
		if !active || !state.expiresAt.After(now) {
			s.xInitiatorQuotaDomainState[hostKey] = xInitiatorQuotaState{
				expiresAt:          now.Add(time.Duration(cfg.DurationSeconds) * time.Second),
				remainingOverrides: cfg.OverrideTimes,
				totalOverrides:     cfg.OverrideTimes,
			}
			return false
		}
		if state.remainingOverrides <= 0 {
			delete(s.xInitiatorQuotaDomainState, hostKey)
			s.xInitiatorQuotaDomainState[hostKey] = xInitiatorQuotaState{
				expiresAt:          now.Add(time.Duration(cfg.DurationSeconds) * time.Second),
				remainingOverrides: cfg.OverrideTimes,
				totalOverrides:     cfg.OverrideTimes,
			}
			return false
		}

		headers.Set("X-Initiator", "agent")
		state.remainingOverrides--
		if state.remainingOverrides <= 0 {
			delete(s.xInitiatorQuotaDomainState, hostKey)
			return true
		}
		s.xInitiatorQuotaDomainState[hostKey] = state
		return true
	}

	if cfg.Mode == XInitiatorOverrideModeWindowedCost {
		if cfg.TotalCost <= 0 {
			return false
		}
		if s.xInitiatorCostDomainState == nil {
			s.xInitiatorCostDomainState = make(map[string]xInitiatorCostState)
		}

		state, active := s.xInitiatorCostDomainState[hostKey]
		if !active || !state.expiresAt.After(now) {
			s.xInitiatorCostDomainState[hostKey] = xInitiatorCostState{
				expiresAt:       now.Add(time.Duration(cfg.DurationSeconds) * time.Second),
				accumulatedCost: 0,
				budgetCost:      cfg.TotalCost,
			}
			return false
		}

		headers.Set("X-Initiator", "agent")
		return true
	}

	if s.xInitiatorDomainState == nil {
		s.xInitiatorDomainState = make(map[string]time.Time)
	}

	expiresAt, active := s.xInitiatorDomainState[hostKey]
	if !active || !expiresAt.After(now) {
		s.xInitiatorDomainState[hostKey] = now.Add(time.Duration(cfg.DurationSeconds) * time.Second)
		return false
	}

	headers.Set("X-Initiator", "agent")
	if cfg.Mode == XInitiatorOverrideModeRelativeCountdown {
		s.xInitiatorDomainState[hostKey] = now.Add(time.Duration(cfg.DurationSeconds) * time.Second)
	}

	return true
}

func (s *Server) activeWindowedCostWindowExpiry(host string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}

	hostKey := strings.ToLower(strings.TrimSpace(host))
	if hostKey == "" {
		return time.Time{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := s.xInitiatorOverride
	if !cfg.Enabled || cfg.Mode != XInitiatorOverrideModeWindowedCost || cfg.DurationSeconds <= 0 || cfg.TotalCost <= 0 {
		return time.Time{}, false
	}
	if s.xInitiatorCostDomainState == nil {
		return time.Time{}, false
	}

	state, active := s.xInitiatorCostDomainState[hostKey]
	if !active {
		return time.Time{}, false
	}

	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	if !state.expiresAt.After(nowFn()) {
		return time.Time{}, false
	}

	return state.expiresAt, true
}

func (s *Server) applyWindowedCostCompletion(host string, windowExpiresAt, completedAt time.Time, price float64) {
	if s == nil {
		return
	}

	hostKey := strings.ToLower(strings.TrimSpace(host))
	if hostKey == "" || windowExpiresAt.IsZero() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := s.xInitiatorOverride
	if !cfg.Enabled || cfg.Mode != XInitiatorOverrideModeWindowedCost || cfg.DurationSeconds <= 0 || cfg.TotalCost <= 0 {
		return
	}
	if s.xInitiatorCostDomainState == nil {
		return
	}

	state, active := s.xInitiatorCostDomainState[hostKey]
	if !active {
		return
	}
	if !state.expiresAt.Equal(windowExpiresAt) {
		return
	}
	if !state.expiresAt.After(completedAt) {
		delete(s.xInitiatorCostDomainState, hostKey)
		return
	}

	if state.budgetCost <= 0 {
		state.budgetCost = cfg.TotalCost
	}
	state.accumulatedCost += price
	if state.accumulatedCost > state.budgetCost {
		delete(s.xInitiatorCostDomainState, hostKey)
		return
	}

	s.xInitiatorCostDomainState[hostKey] = state
}

func (s *Server) GetXInitiatorOverrideRuntimeStatus() XInitiatorOverrideRuntimeStatus {
	if s == nil {
		return XInitiatorOverrideRuntimeStatus{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getXInitiatorOverrideRuntimeStatusLocked()
}

func (s *Server) getXInitiatorOverrideRuntimeStatusLocked() XInitiatorOverrideRuntimeStatus {
	if s == nil {
		return XInitiatorOverrideRuntimeStatus{}
	}

	cfg := s.xInitiatorOverride
	cfg = normalizeXInitiatorOverrideConfig(cfg)
	status := XInitiatorOverrideRuntimeStatus{
		Enabled: cfg.Enabled,
		Mode:    cfg.Mode,
	}
	if cfg.Mode == XInitiatorOverrideModeWindowedCost {
		return status
	}

	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()

	if cfg.Mode == XInitiatorOverrideModeWindowedQuota {
		for domain, state := range s.xInitiatorQuotaDomainState {
			if !state.expiresAt.After(now) {
				continue
			}
			remainingSeconds := int(state.expiresAt.Sub(now).Seconds())
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
			remainingOverrides := state.remainingOverrides
			totalOverrides := state.totalOverrides
			status.Domains = append(status.Domains, XInitiatorOverrideDomainStatus{
				Domain:             domain,
				DisplayName:        resolveInterceptedProviderNameFromAliases(s.domainAliases, domain),
				ExpiresAt:          state.expiresAt,
				RemainingSeconds:   remainingSeconds,
				RemainingOverrides: &remainingOverrides,
				TotalOverrides:     &totalOverrides,
			})
			status.ActiveDomains++
			if status.NearestExpiryAt == nil || state.expiresAt.Before(*status.NearestExpiryAt) {
				exp := state.expiresAt
				status.NearestExpiryAt = &exp
			}
		}

		sort.Slice(status.Domains, func(i, j int) bool {
			return status.Domains[i].ExpiresAt.Before(status.Domains[j].ExpiresAt)
		})

		if status.NearestExpiryAt != nil {
			status.NearestRemainingSeconds = int(status.NearestExpiryAt.Sub(now).Seconds())
			if status.NearestRemainingSeconds < 0 {
				status.NearestRemainingSeconds = 0
			}
		}

		return status
	}

	for domain, expiresAt := range s.xInitiatorDomainState {
		if !expiresAt.After(now) {
			continue
		}
		remainingSeconds := int(expiresAt.Sub(now).Seconds())
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		status.Domains = append(status.Domains, XInitiatorOverrideDomainStatus{
			Domain:           domain,
			DisplayName:      resolveInterceptedProviderNameFromAliases(s.domainAliases, domain),
			ExpiresAt:        expiresAt,
			RemainingSeconds: remainingSeconds,
		})
		status.ActiveDomains++
		if status.NearestExpiryAt == nil || expiresAt.Before(*status.NearestExpiryAt) {
			exp := expiresAt
			status.NearestExpiryAt = &exp
		}
	}

	sort.Slice(status.Domains, func(i, j int) bool {
		return status.Domains[i].ExpiresAt.Before(status.Domains[j].ExpiresAt)
	})

	if status.NearestExpiryAt != nil {
		status.NearestRemainingSeconds = int(status.NearestExpiryAt.Sub(now).Seconds())
		if status.NearestRemainingSeconds < 0 {
			status.NearestRemainingSeconds = 0
		}
	}

	return status
}
