package quota

import "time"

// ChannelUsage represents the current usage state for a channel
type ChannelUsage struct {
	Used        float64   `json:"used"`        // Current usage (requests count or credit amount)
	LastResetAt time.Time `json:"lastResetAt"` // When the quota was last reset
}

// ChannelUsageStatus represents the full usage status for API response
type ChannelUsageStatus struct {
	QuotaType    string  `json:"quotaType"`             // "requests" | "credit" | ""
	Limit        float64 `json:"limit"`                 // Max quota value
	Used         float64 `json:"used"`                  // Current usage
	Remaining    float64 `json:"remaining"`             // Limit - Used
	RemainingPct float64 `json:"remainingPercent"`      // (Remaining / Limit) * 100
	LastResetAt  *string `json:"lastResetAt,omitempty"` // ISO timestamp
	NextResetAt  *string `json:"nextResetAt,omitempty"` // ISO timestamp (if auto-reset configured)
}

// UsageFile represents the structure persisted to quota_usage.json
type UsageFile struct {
	Messages  map[string]ChannelUsage `json:"messages"`  // key = channel index as string
	Responses map[string]ChannelUsage `json:"responses"` // key = channel index as string
	Gemini    map[string]ChannelUsage `json:"gemini"`    // key = channel index as string
}
