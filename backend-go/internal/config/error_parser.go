package config

import (
	"encoding/json"
	"time"
)

// Error429Type represents the detected 429 error subtype
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

// Parse429Error analyzes a 429 response body and returns the subtype and suggested wait duration.
// The wait duration is only meaningful for ModelCooldown (from reset_seconds) and ResourceExhausted (default 20s).
// For QuotaExhausted and Unknown, wait duration is 0 (immediate failover or threshold-based).
func Parse429Error(body []byte, cfg *FailoverConfig) (Error429Type, time.Duration) {
	if len(body) == 0 {
		return Error429Unknown, 0
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return Error429Unknown, 0
	}

	// Navigate to error object if present
	errObj := raw
	if errField, ok := raw["error"].(map[string]any); ok {
		errObj = errField
	}

	// Priority 1: Check for model_cooldown (Type 2)
	if cooldownType, wait := parseModelCooldown(errObj, cfg); cooldownType == Error429ModelCooldown {
		return cooldownType, wait
	}

	// Priority 2 & 3: Check for RESOURCE_EXHAUSTED variants
	if resType, wait := parseResourceExhausted(errObj, cfg); resType != Error429Unknown {
		return resType, wait
	}

	return Error429Unknown, 0
}

// parseModelCooldown checks for model_cooldown error type
// Example: {"error":{"code":"model_cooldown","reset_seconds":16,...}}
func parseModelCooldown(errObj map[string]any, cfg *FailoverConfig) (Error429Type, time.Duration) {
	code, ok := errObj["code"].(string)
	if !ok || code != "model_cooldown" {
		return Error429Unknown, 0
	}

	// Extract reset_seconds (JSON numbers are always float64)
	var resetSeconds float64
	switch v := errObj["reset_seconds"].(type) {
	case float64:
		resetSeconds = v
	default:
		// No valid reset_seconds found, treat as unknown
		return Error429Unknown, 0
	}

	// Calculate wait time with configurable extra seconds
	// Use ceiling to avoid under-waiting for fractional seconds
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

	waitSeconds := int(resetSeconds+0.999) + extraSeconds // Ceiling for fractional seconds
	if waitSeconds > maxWaitSeconds {
		waitSeconds = maxWaitSeconds
	}

	return Error429ModelCooldown, time.Duration(waitSeconds) * time.Second
}

// parseResourceExhausted checks for RESOURCE_EXHAUSTED status and distinguishes quota vs generic
// Type 1: {"error":{"status":"RESOURCE_EXHAUSTED","details":[{"reason":"QUOTA_EXHAUSTED",...}]}}
// Type 3: {"error":{"status":"RESOURCE_EXHAUSTED"}} without QUOTA_EXHAUSTED reason
func parseResourceExhausted(errObj map[string]any, cfg *FailoverConfig) (Error429Type, time.Duration) {
	status, ok := errObj["status"].(string)
	if !ok || status != "RESOURCE_EXHAUSTED" {
		return Error429Unknown, 0
	}

	// Check details array for QUOTA_EXHAUSTED reason
	if details, ok := errObj["details"].([]any); ok {
		for _, detail := range details {
			if detailMap, ok := detail.(map[string]any); ok {
				if reason, ok := detailMap["reason"].(string); ok && reason == "QUOTA_EXHAUSTED" {
					// Type 1: Quota exhausted - immediate failover, no wait
					return Error429QuotaExhausted, 0
				}
			}
		}
	}

	// Type 3: Generic resource exhausted - wait and retry
	waitSeconds := 20
	if cfg != nil && cfg.GenericResourceWaitSeconds > 0 {
		waitSeconds = cfg.GenericResourceWaitSeconds
	}

	return Error429ResourceExhausted, time.Duration(waitSeconds) * time.Second
}
