package handlers

import (
	"github.com/JillVernus/cc-bridge/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

// GetRateLimitConfig returns the current rate limit configuration
func GetRateLimitConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		rm := ratelimit.GetManager()
		if rm == nil {
			c.JSON(500, gin.H{"error": "Rate limit manager not initialized"})
			return
		}

		config := rm.GetConfig()
		c.JSON(200, config)
	}
}

// UpdateRateLimitConfig updates the rate limit configuration
func UpdateRateLimitConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		rm := ratelimit.GetManager()
		if rm == nil {
			c.JSON(500, gin.H{"error": "Rate limit manager not initialized"})
			return
		}

		var config ratelimit.RateLimitConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		// Validate configuration with reasonable bounds
		const maxRPM = 10000         // Max 10,000 requests per minute
		const maxBlockMinutes = 1440 // Max 24 hours (1440 minutes)

		if config.API.RequestsPerMinute < 0 {
			c.JSON(400, gin.H{"error": "API requests per minute must be non-negative"})
			return
		}
		if config.API.RequestsPerMinute > maxRPM {
			c.JSON(400, gin.H{"error": "API requests per minute cannot exceed 10,000"})
			return
		}
		if config.Portal.RequestsPerMinute < 0 {
			c.JSON(400, gin.H{"error": "Portal requests per minute must be non-negative"})
			return
		}
		if config.Portal.RequestsPerMinute > maxRPM {
			c.JSON(400, gin.H{"error": "Portal requests per minute cannot exceed 10,000"})
			return
		}

		// Validate auth failure thresholds
		for i, threshold := range config.AuthFailure.Thresholds {
			if threshold.Failures <= 0 {
				c.JSON(400, gin.H{"error": "Auth failure threshold failures must be positive"})
				return
			}
			if threshold.BlockMinutes <= 0 {
				c.JSON(400, gin.H{"error": "Auth failure threshold block minutes must be positive"})
				return
			}
			if threshold.BlockMinutes > maxBlockMinutes {
				c.JSON(400, gin.H{"error": "Auth failure block minutes cannot exceed 1440 (24 hours)"})
				return
			}
			// Ensure thresholds are in ascending order
			if i > 0 && threshold.Failures <= config.AuthFailure.Thresholds[i-1].Failures {
				c.JSON(400, gin.H{"error": "Auth failure thresholds must be in ascending order by failures"})
				return
			}
		}

		if err := rm.UpdateConfig(config); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save rate limit config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "Rate limit configuration updated",
			"config":  config,
		})
	}
}

// ResetRateLimitConfig resets the rate limit configuration to defaults
func ResetRateLimitConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		rm := ratelimit.GetManager()
		if rm == nil {
			c.JSON(500, gin.H{"error": "Rate limit manager not initialized"})
			return
		}

		defaultConfig := ratelimit.GetDefaultConfig()
		if err := rm.UpdateConfig(defaultConfig); err != nil {
			c.JSON(500, gin.H{"error": "Failed to reset rate limit config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "Rate limit configuration reset to defaults",
			"config":  defaultConfig,
		})
	}
}
