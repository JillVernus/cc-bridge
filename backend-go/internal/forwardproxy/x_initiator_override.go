package forwardproxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	XInitiatorOverrideModeFixedWindow       = "fixed_window"
	XInitiatorOverrideModeRelativeCountdown = "relative_countdown"
)

type XInitiatorOverrideConfig struct {
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"`
	DurationSeconds int    `json:"durationSeconds"`
}

func validateXInitiatorOverrideConfig(cfg XInitiatorOverrideConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.DurationSeconds <= 0 {
		return fmt.Errorf("xInitiatorOverride.durationSeconds must be greater than 0")
	}
	switch strings.TrimSpace(cfg.Mode) {
	case XInitiatorOverrideModeFixedWindow, XInitiatorOverrideModeRelativeCountdown:
		return nil
	default:
		return fmt.Errorf("xInitiatorOverride.mode must be one of %q or %q", XInitiatorOverrideModeFixedWindow, XInitiatorOverrideModeRelativeCountdown)
	}
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
