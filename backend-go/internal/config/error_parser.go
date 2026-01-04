package config

import (
	"encoding/json"
	"strings"
	"time"
)

// Error subtype constants for rule matching
const (
	SubtypeQuotaExhausted    = "QUOTA_EXHAUSTED"    // 429 with QUOTA_EXHAUSTED reason
	SubtypeModelCooldown     = "model_cooldown"     // 429 with code=model_cooldown
	SubtypeResourceExhausted = "RESOURCE_EXHAUSTED" // 429 with generic RESOURCE_EXHAUSTED
	SubtypeCreditExhausted   = "CREDIT_EXHAUSTED"   // 403 with "quota is not enough" message
)

// Error429Type represents the detected 429 error subtype (legacy, kept for compatibility)
type Error429Type int

const (
	Error429Unknown          Error429Type = iota // Unknown or unparseable 429
	Error429QuotaExhausted                       // Type 1: QUOTA_EXHAUSTED in details
	Error429ModelCooldown                        // Type 2: code="model_cooldown" with reset_seconds
	Error429ResourceExhausted                    // Type 3: generic RESOURCE_EXHAUSTED without quota reason
)

// String returns a human-readable name for the error type
func (t Error429Type) String() string {
	switch t {
	case Error429QuotaExhausted:
		return "quota_exhausted"
	case Error429ModelCooldown:
		return "model_cooldown"
	case Error429ResourceExhausted:
		return "resource_exhausted"
	default:
		return "unknown"
	}
}

// ParsedError contains parsed error information for rule matching
type ParsedError struct {
	StatusCode   int           // HTTP status code (e.g., 429, 403)
	Subtype      string        // Error subtype (e.g., "QUOTA_EXHAUSTED", "model_cooldown")
	WaitDuration time.Duration // Suggested wait duration (for retry_wait action)
	ResetSeconds float64       // Raw reset_seconds from response (for model_cooldown)
}

// ErrorCodePattern returns the pattern for rule matching (e.g., "429:QUOTA_EXHAUSTED" or "429")
func (p *ParsedError) ErrorCodePattern() string {
	if p.Subtype != "" {
		return formatErrorPattern(p.StatusCode, p.Subtype)
	}
	return formatErrorPattern(p.StatusCode, "")
}

// formatErrorPattern creates an error pattern string
func formatErrorPattern(statusCode int, subtype string) string {
	if subtype != "" {
		return strings.Join([]string{intToStr(statusCode), subtype}, ":")
	}
	return intToStr(statusCode)
}

// intToStr converts int to string without importing strconv
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ParseError analyzes a response body and returns parsed error information
func ParseError(statusCode int, body []byte, cfg *FailoverConfig) *ParsedError {
	result := &ParsedError{StatusCode: statusCode}

	if len(body) == 0 {
		return result
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return result
	}

	// Navigate to error object if present
	errObj := raw
	if errField, ok := raw["error"].(map[string]any); ok {
		errObj = errField
	}

	switch statusCode {
	case 429:
		parseError429(errObj, cfg, result)
	case 403:
		parseError403(errObj, result)
	}

	return result
}

// parseError429 parses 429 error subtypes
func parseError429(errObj map[string]any, cfg *FailoverConfig, result *ParsedError) {
	// Priority 1: Check for RESOURCE_EXHAUSTED with QUOTA_EXHAUSTED (most specific)
	// Check this first because quota exhaustion should always trigger channel suspension,
	// even if model_cooldown is also present
	if status, ok := errObj["status"].(string); ok && status == "RESOURCE_EXHAUSTED" {
		if details, ok := errObj["details"].([]any); ok {
			for _, detail := range details {
				if detailMap, ok := detail.(map[string]any); ok {
					if reason, ok := detailMap["reason"].(string); ok && reason == "QUOTA_EXHAUSTED" {
						result.Subtype = SubtypeQuotaExhausted
						return
					}
				}
			}
		}
	}

	// Priority 2: Check for model_cooldown
	if code, ok := errObj["code"].(string); ok && code == "model_cooldown" {
		result.Subtype = SubtypeModelCooldown
		// Default wait time when reset_seconds is missing (prevents busy-loop)
		defaultWaitSeconds := 2
		extraSeconds := 1
		maxWaitSeconds := 60
		if cfg != nil {
			if cfg.ModelCooldownExtraSeconds > 0 {
				extraSeconds = cfg.ModelCooldownExtraSeconds
			}
			if cfg.ModelCooldownMaxWaitSeconds > 0 {
				maxWaitSeconds = cfg.ModelCooldownMaxWaitSeconds
			}
		}

		if resetSec, ok := errObj["reset_seconds"].(float64); ok {
			result.ResetSeconds = resetSec
			waitSeconds := int(resetSec+0.999) + extraSeconds
			if waitSeconds > maxWaitSeconds {
				waitSeconds = maxWaitSeconds
			}
			result.WaitDuration = time.Duration(waitSeconds) * time.Second
		} else {
			// reset_seconds missing - use default to prevent busy-loop
			result.WaitDuration = time.Duration(defaultWaitSeconds) * time.Second
		}
		return
	}

	// Priority 3: Generic RESOURCE_EXHAUSTED (without QUOTA_EXHAUSTED)
	if status, ok := errObj["status"].(string); ok && status == "RESOURCE_EXHAUSTED" {
		result.Subtype = SubtypeResourceExhausted
		waitSeconds := 20
		if cfg != nil && cfg.GenericResourceWaitSeconds > 0 {
			waitSeconds = cfg.GenericResourceWaitSeconds
		}
		result.WaitDuration = time.Duration(waitSeconds) * time.Second
	}
}

// parseError403 parses 403 error subtypes (credit exhaustion)
func parseError403(errObj map[string]any, result *ParsedError) {
	// Check for credit exhaustion pattern in message
	// Example: {"error":{"message":"token quota is not enough, token remain quota: $0.204628, need quota: $0.469752"}}
	if message, ok := errObj["message"].(string); ok {
		messageLower := strings.ToLower(message)
		if strings.Contains(messageLower, "quota is not enough") ||
			strings.Contains(messageLower, "insufficient") && strings.Contains(messageLower, "quota") {
			result.Subtype = SubtypeCreditExhausted
		}
	}
}

// Parse429Error analyzes a 429 response body and returns the subtype and suggested wait duration.
// LEGACY: Kept for backward compatibility. Use ParseError for new code.
func Parse429Error(body []byte, cfg *FailoverConfig) (Error429Type, time.Duration) {
	parsed := ParseError(429, body, cfg)

	switch parsed.Subtype {
	case SubtypeQuotaExhausted:
		return Error429QuotaExhausted, 0
	case SubtypeModelCooldown:
		return Error429ModelCooldown, parsed.WaitDuration
	case SubtypeResourceExhausted:
		return Error429ResourceExhausted, parsed.WaitDuration
	default:
		return Error429Unknown, 0
	}
}
