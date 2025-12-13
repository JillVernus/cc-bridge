package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware adds security headers to responses
// These headers only affect browser-based access (Web UI)
// They do not affect API proxy requests or non-browser clients
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip security headers for API proxy endpoints
		// These are typically used by non-browser clients
		if strings.HasPrefix(path, "/v1/") {
			c.Next()
			return
		}

		// X-Content-Type-Options: Prevents MIME type sniffing
		// Stops browsers from trying to guess the content type
		c.Header("X-Content-Type-Options", "nosniff")

		// X-Frame-Options: Prevents clickjacking attacks
		// DENY = page cannot be displayed in a frame
		c.Header("X-Frame-Options", "DENY")

		// X-XSS-Protection: Enables browser's XSS filter
		// 1; mode=block = enable and block the page if attack detected
		c.Header("X-XSS-Protection", "1; mode=block")

		// Referrer-Policy: Controls how much referrer info is sent
		// strict-origin-when-cross-origin = send full URL for same-origin, only origin for cross-origin
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		c.Next()
	}
}
