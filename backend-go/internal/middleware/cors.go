package middleware

import (
	"net/url"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// CORSMiddleware CORS 中间件
func CORSMiddleware(envCfg *config.EnvConfig) gin.HandlerFunc {
	corsOrigin := strings.TrimSpace(envCfg.CORSOrigin)
	allowAll := corsOrigin == "*"
	allowedOrigins := make(map[string]struct{})
	if !allowAll && corsOrigin != "" {
		for _, part := range strings.Split(corsOrigin, ",") {
			origin := strings.TrimSpace(part)
			if origin == "" {
				continue
			}
			allowedOrigins[origin] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		if !envCfg.EnableCORS {
			// 仍然快速处理预检请求；由于未返回 Allow-Origin，浏览器会阻止跨域访问。
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(204)
				return
			}
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")

		if origin != "" {
			allowedOrigin := ""
			if envCfg.IsDevelopment() {
				if isAllowedDevOrigin(origin) {
					allowedOrigin = origin
				}
			} else {
				if allowAll {
					allowedOrigin = "*"
				} else if _, ok := allowedOrigins[origin]; ok {
					allowedOrigin = origin
				}
			}

			if allowedOrigin != "" {
				c.Header("Access-Control-Allow-Origin", allowedOrigin)
				if allowedOrigin != "*" {
					c.Header("Vary", "Origin")
				}
			}
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")

		// 处理预检请求
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func isAllowedDevOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
