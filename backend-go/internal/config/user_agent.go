package config

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMessagesUserAgent is the fallback UA used for /v1/messages (claude direct passthrough).
	DefaultMessagesUserAgent = "claude-cli/2.1.12 (external, cli)"
	// DefaultResponsesUserAgent is the fallback UA used for /v1/responses (responses/openai-oauth direct passthrough).
	DefaultResponsesUserAgent = "codex_cli_rs/0.73.0 (Linux; x86_64)"
)

var (
	messagesUserAgentPattern  = regexp.MustCompile(`(?i)^claude-cli/([0-9]+(?:\.[0-9]+)*)`)
	responsesUserAgentPattern = regexp.MustCompile(`(?i)^codex_cli_rs/([0-9]+(?:\.[0-9]+)*)`)
)

// UserAgentEndpointConfig stores the latest known UA and its capture timestamp.
type UserAgentEndpointConfig struct {
	Latest         string `json:"latest"`
	LastCapturedAt string `json:"lastCapturedAt,omitempty"`
}

// UserAgentConfig stores endpoint-specific UA preferences.
type UserAgentConfig struct {
	Messages  UserAgentEndpointConfig `json:"messages"`
	Responses UserAgentEndpointConfig `json:"responses"`
}

// GetDefaultUserAgentConfig returns the default UA configuration.
func GetDefaultUserAgentConfig() UserAgentConfig {
	return UserAgentConfig{
		Messages: UserAgentEndpointConfig{
			Latest: DefaultMessagesUserAgent,
		},
		Responses: UserAgentEndpointConfig{
			Latest: DefaultResponsesUserAgent,
		},
	}
}

func normalizeUserAgentConfig(cfg *UserAgentConfig) bool {
	changed := false

	cfg.Messages.Latest = strings.TrimSpace(cfg.Messages.Latest)
	cfg.Responses.Latest = strings.TrimSpace(cfg.Responses.Latest)
	cfg.Messages.LastCapturedAt = strings.TrimSpace(cfg.Messages.LastCapturedAt)
	cfg.Responses.LastCapturedAt = strings.TrimSpace(cfg.Responses.LastCapturedAt)

	if cfg.Messages.Latest == "" || !IsValidMessagesUserAgent(cfg.Messages.Latest) {
		cfg.Messages.Latest = DefaultMessagesUserAgent
		changed = true
	}
	if cfg.Responses.Latest == "" || !IsValidResponsesUserAgent(cfg.Responses.Latest) {
		cfg.Responses.Latest = DefaultResponsesUserAgent
		changed = true
	}

	return changed
}

func sanitizeUserAgentConfig(cfg UserAgentConfig) (UserAgentConfig, error) {
	cfg.Messages.Latest = strings.TrimSpace(cfg.Messages.Latest)
	cfg.Responses.Latest = strings.TrimSpace(cfg.Responses.Latest)
	cfg.Messages.LastCapturedAt = strings.TrimSpace(cfg.Messages.LastCapturedAt)
	cfg.Responses.LastCapturedAt = strings.TrimSpace(cfg.Responses.LastCapturedAt)

	if cfg.Messages.Latest == "" {
		cfg.Messages.Latest = DefaultMessagesUserAgent
	}
	if cfg.Responses.Latest == "" {
		cfg.Responses.Latest = DefaultResponsesUserAgent
	}

	if !IsValidMessagesUserAgent(cfg.Messages.Latest) {
		return cfg, fmt.Errorf("invalid messages user-agent: must start with claude-cli/ and include numeric version")
	}
	if !IsValidResponsesUserAgent(cfg.Responses.Latest) {
		return cfg, fmt.Errorf("invalid responses user-agent: must start with codex_cli_rs/ and include numeric version")
	}

	return cfg, nil
}

// IsValidMessagesUserAgent checks whether UA looks like Claude CLI.
func IsValidMessagesUserAgent(userAgent string) bool {
	_, ok := parseVersionFromUserAgent(userAgent, messagesUserAgentPattern)
	return ok
}

// IsValidResponsesUserAgent checks whether UA looks like Codex CLI.
func IsValidResponsesUserAgent(userAgent string) bool {
	_, ok := parseVersionFromUserAgent(userAgent, responsesUserAgentPattern)
	return ok
}

func parseVersionFromUserAgent(userAgent string, pattern *regexp.Regexp) ([]int, bool) {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return nil, false
	}

	matches := pattern.FindStringSubmatch(userAgent)
	if len(matches) < 2 {
		return nil, false
	}

	parts := strings.Split(matches[1], ".")
	version := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		version = append(version, n)
	}
	return version, true
}

func compareVersion(a, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	for i := 0; i < maxLen; i++ {
		av := 0
		bv := 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func isNewerMessagesUserAgent(candidate, current string) bool {
	candidateVer, ok := parseVersionFromUserAgent(candidate, messagesUserAgentPattern)
	if !ok {
		return false
	}
	currentVer, ok := parseVersionFromUserAgent(current, messagesUserAgentPattern)
	if !ok {
		return true
	}
	return compareVersion(candidateVer, currentVer) > 0
}

func isNewerResponsesUserAgent(candidate, current string) bool {
	candidateVer, ok := parseVersionFromUserAgent(candidate, responsesUserAgentPattern)
	if !ok {
		return false
	}
	currentVer, ok := parseVersionFromUserAgent(current, responsesUserAgentPattern)
	if !ok {
		return true
	}
	return compareVersion(candidateVer, currentVer) > 0
}

// GetUserAgentConfig returns current UA config with defaults applied.
func (cm *ConfigManager) GetUserAgentConfig() UserAgentConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cfg := cm.config.UserAgent
	normalizeUserAgentConfig(&cfg)
	return cfg
}

// UpdateUserAgentConfig updates admin-editable UA fallback config.
func (cm *ConfigManager) UpdateUserAgentConfig(cfg UserAgentConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	sanitized, err := sanitizeUserAgentConfig(cfg)
	if err != nil {
		return err
	}

	// Keep capture timestamp unless explicitly provided.
	if sanitized.Messages.LastCapturedAt == "" {
		sanitized.Messages.LastCapturedAt = cm.config.UserAgent.Messages.LastCapturedAt
	}
	if sanitized.Responses.LastCapturedAt == "" {
		sanitized.Responses.LastCapturedAt = cm.config.UserAgent.Responses.LastCapturedAt
	}

	cm.config.UserAgent = sanitized
	return cm.saveConfigLocked(cm.config)
}

// ResolveMessagesUserAgent resolves outgoing UA for /v1/messages direct passthrough.
func (cm *ConfigManager) ResolveMessagesUserAgent(incoming string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.resolveMessagesUserAgentLocked(incoming)
}

// ResolveResponsesUserAgent resolves outgoing UA for /v1/responses direct passthrough.
func (cm *ConfigManager) ResolveResponsesUserAgent(incoming string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.resolveResponsesUserAgentLocked(incoming)
}

func (cm *ConfigManager) resolveMessagesUserAgentLocked(incoming string) string {
	changed := normalizeUserAgentConfig(&cm.config.UserAgent)
	incoming = strings.TrimSpace(incoming)

	if IsValidMessagesUserAgent(incoming) {
		if isNewerMessagesUserAgent(incoming, cm.config.UserAgent.Messages.Latest) {
			cm.config.UserAgent.Messages.Latest = incoming
			cm.config.UserAgent.Messages.LastCapturedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
		}
		if changed {
			cm.saveUserAgentLocked()
		}
		return incoming
	}

	fallback := strings.TrimSpace(cm.config.UserAgent.Messages.Latest)
	if !IsValidMessagesUserAgent(fallback) {
		fallback = DefaultMessagesUserAgent
		if cm.config.UserAgent.Messages.Latest != fallback {
			cm.config.UserAgent.Messages.Latest = fallback
			changed = true
		}
	}

	if changed {
		cm.saveUserAgentLocked()
	}
	return fallback
}

func (cm *ConfigManager) resolveResponsesUserAgentLocked(incoming string) string {
	changed := normalizeUserAgentConfig(&cm.config.UserAgent)
	incoming = strings.TrimSpace(incoming)

	if IsValidResponsesUserAgent(incoming) {
		if isNewerResponsesUserAgent(incoming, cm.config.UserAgent.Responses.Latest) {
			cm.config.UserAgent.Responses.Latest = incoming
			cm.config.UserAgent.Responses.LastCapturedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
		}
		if changed {
			cm.saveUserAgentLocked()
		}
		return incoming
	}

	fallback := strings.TrimSpace(cm.config.UserAgent.Responses.Latest)
	if !IsValidResponsesUserAgent(fallback) {
		fallback = DefaultResponsesUserAgent
		if cm.config.UserAgent.Responses.Latest != fallback {
			cm.config.UserAgent.Responses.Latest = fallback
			changed = true
		}
	}

	if changed {
		cm.saveUserAgentLocked()
	}
	return fallback
}

func (cm *ConfigManager) saveUserAgentLocked() {
	if err := cm.saveConfigLocked(cm.config); err != nil {
		log.Printf("⚠️ 保存 User-Agent 配置失败: %v", err)
	}
}
