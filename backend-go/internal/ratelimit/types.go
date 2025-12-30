package ratelimit

// EndpointRateLimit defines rate limit settings for an endpoint type
type EndpointRateLimit struct {
	Enabled           bool `json:"enabled"`
	RequestsPerMinute int  `json:"requestsPerMinute"`
}

// AuthFailureThreshold defines a single threshold for auth failure blocking
type AuthFailureThreshold struct {
	Failures     int `json:"failures"`
	BlockMinutes int `json:"blockMinutes"`
}

// AuthFailureConfig defines auth failure rate limiting settings
type AuthFailureConfig struct {
	Enabled    bool                   `json:"enabled"`
	Thresholds []AuthFailureThreshold `json:"thresholds"`
}

// RateLimitConfig is the root configuration structure
type RateLimitConfig struct {
	API         EndpointRateLimit `json:"api"`
	Portal      EndpointRateLimit `json:"portal"`
	AuthFailure AuthFailureConfig `json:"authFailure"`
}

// GetDefaultConfig returns the default rate limit configuration
func GetDefaultConfig() RateLimitConfig {
	return RateLimitConfig{
		API: EndpointRateLimit{
			Enabled:           true,
			RequestsPerMinute: 100,
		},
		Portal: EndpointRateLimit{
			Enabled:           true,
			RequestsPerMinute: 60,
		},
		AuthFailure: AuthFailureConfig{
			Enabled: true,
			Thresholds: []AuthFailureThreshold{
				{Failures: 5, BlockMinutes: 1},
				{Failures: 10, BlockMinutes: 5},
				{Failures: 20, BlockMinutes: 30},
			},
		},
	}
}
