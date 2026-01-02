package middleware

import (
	"crypto/subtle"
	"log"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// Context keys for storing validated API key info
const (
	ContextKeyAPIKeyID      = "apiKeyID"
	ContextKeyAPIKeyName    = "apiKeyName"
	ContextKeyAPIKeyIsAdmin = "apiKeyIsAdmin"
	ContextKeyIsBootstrap   = "isBootstrapAdmin"
	ContextKeyRateLimitRPM  = "rateLimitRPM"
	ContextKeyValidatedKey  = "validatedKey" // Full ValidatedKey struct for permission checks
)

// secureCompare performs a constant-time comparison of two strings
// to prevent timing attacks
func secureCompare(a, b string) bool {
	// Both strings must be non-empty and equal length for constant-time comparison
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WebAuthMiddleware Web 访问控制中间件
func WebAuthMiddleware(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) gin.HandlerFunc {
	return WebAuthMiddlewareWithAPIKey(envCfg, cfgManager, nil)
}

// WebAuthMiddlewareWithAPIKey Web 访问控制中间件（支持 API Key 验证）
func WebAuthMiddlewareWithAPIKey(envCfg *config.EnvConfig, cfgManager *config.ConfigManager, apiKeyManager *apikey.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 公开端点直接放行
		if path == envCfg.HealthCheckPath {
			c.Next()
			return
		}

		// 管理端点需要访问密钥（即使 Web UI 被禁用）
		if envCfg.IsDevelopment() && path == "/admin/dev/info" {
			if !validateAndSetContext(c, envCfg, apiKeyManager, true) {
				return
			}
			c.Next()
			return
		}

		if path == "/admin/config/reload" {
			if !validateAndSetContext(c, envCfg, apiKeyManager, true) {
				return
			}
			c.Next()
			return
		}

		// 静态资源文件直接放行
		if isStaticResource(path) {
			c.Next()
			return
		}

		// API 代理端点后续处理
		if strings.HasPrefix(path, "/v1/") {
			c.Next()
			return
		}

		// 如果禁用了 Web UI，返回 404
		if !envCfg.EnableWebUI {
			c.JSON(404, gin.H{
				"error":   "Web界面已禁用",
				"message": "此服务器运行在纯API模式下，请通过API端点访问服务",
			})
			c.Abort()
			return
		}

		// SPA 页面路由直接交给前端处理，但需要排除 /api* 路径
		if path == "/" || path == "/index.html" || (!strings.Contains(path, ".") && !strings.HasPrefix(path, "/api")) {
			c.Next()
			return
		}

		// 检查访问密钥（仅对管理 API 请求）
		if strings.HasPrefix(path, "/api") {
			// 🔒 安全修复: 所有管理 API 端点都需要 admin 权限
			// 管理操作包括: 渠道配置、日志查看、定价设置、备份恢复等
			if !validateAndSetContext(c, envCfg, apiKeyManager, true) {
				return
			}
		}

		c.Next()
	}
}

// validateAndSetContext validates the API key and sets context values
// Returns true if validation passed, false if request was aborted
func validateAndSetContext(c *gin.Context, envCfg *config.EnvConfig, apiKeyManager *apikey.Manager, requireAdmin bool) bool {
	providedKey := getAPIKey(c)
	clientIP := c.ClientIP()
	timestamp := time.Now().Format(time.RFC3339)
	logPath := sanitizePathForLogs(c.Request.URL.Path)

	// Try SQLite API keys first (if manager is available)
	if apiKeyManager != nil && providedKey != "" {
		if vk := apiKeyManager.Validate(providedKey); vk != nil {
			// Check admin requirement
			if requireAdmin && !vk.IsAdmin {
				log.Printf("🔒 [权限不足] IP: %s | Path: %s | Time: %s | Key: %s",
					clientIP, logPath, timestamp, vk.Name)
				c.JSON(403, gin.H{
					"error":   "Forbidden",
					"message": "Admin privileges required",
				})
				c.Abort()
				return false
			}

			// Set context values
			c.Set(ContextKeyAPIKeyID, vk.ID)
			c.Set(ContextKeyAPIKeyName, vk.Name)
			c.Set(ContextKeyAPIKeyIsAdmin, vk.IsAdmin)
			c.Set(ContextKeyIsBootstrap, false)
			c.Set(ContextKeyRateLimitRPM, vk.RateLimitRPM)

			if envCfg.ShouldLog("info") {
				log.Printf("✅ [认证成功] IP: %s | Path: %s | Time: %s | Key: %s",
					clientIP, logPath, timestamp, vk.Name)
			}
			return true
		}
	}

	// Fallback to bootstrap admin key (PROXY_ACCESS_KEY)
	if secureCompare(providedKey, envCfg.ProxyAccessKey) {
		// Bootstrap admin has full admin privileges
		c.Set(ContextKeyAPIKeyID, int64(0))
		c.Set(ContextKeyAPIKeyName, "master")
		c.Set(ContextKeyAPIKeyIsAdmin, true)
		c.Set(ContextKeyIsBootstrap, true)

		if envCfg.ShouldLog("info") {
			log.Printf("✅ [认证成功] IP: %s | Path: %s | Time: %s | Key: master",
				clientIP, logPath, timestamp)
		}
		return true
	}

	// Authentication failed
	reason := "密钥无效"
	if providedKey == "" {
		reason = "密钥缺失"
	}
	log.Printf("🔒 [认证失败] IP: %s | Path: %s | Time: %s | Reason: %s",
		clientIP, logPath, timestamp, reason)

	c.JSON(401, gin.H{
		"error":   "Unauthorized",
		"message": "Invalid or missing access key",
	})
	c.Abort()
	return false
}

// isStaticResource 判断是否为静态资源
func isStaticResource(path string) bool {
	staticExtensions := []string{
		"/assets/", ".css", ".js", ".ico", ".png", ".jpg",
		".gif", ".svg", ".woff", ".woff2", ".ttf", ".eot",
	}

	for _, ext := range staticExtensions {
		if strings.HasPrefix(path, ext) || strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}

// getAPIKey 获取 API 密钥
func getAPIKey(c *gin.Context) string {
	// 从 header 获取
	if key := c.GetHeader("x-api-key"); key != "" {
		return key
	}

	if auth := c.GetHeader("Authorization"); auth != "" {
		// 移除 Bearer 前缀
		return strings.TrimPrefix(auth, "Bearer ")
	}

	return ""
}

// ProxyAuthMiddleware 代理访问控制中间件
func ProxyAuthMiddleware(envCfg *config.EnvConfig) gin.HandlerFunc {
	return ProxyAuthMiddlewareWithAPIKey(envCfg, nil)
}

// ProxyAuthMiddlewareWithAPIKey 代理访问控制中间件（支持 API Key 验证）
func ProxyAuthMiddlewareWithAPIKey(envCfg *config.EnvConfig, apiKeyManager *apikey.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		providedKey := getAPIKey(c)

		// Try SQLite API keys first (if manager is available)
		if apiKeyManager != nil && providedKey != "" {
			if vk := apiKeyManager.Validate(providedKey); vk != nil {
				// Set context values for request logging
				c.Set(ContextKeyAPIKeyID, vk.ID)
				c.Set(ContextKeyAPIKeyName, vk.Name)
				c.Set(ContextKeyAPIKeyIsAdmin, vk.IsAdmin)
				c.Set(ContextKeyIsBootstrap, false)
				c.Set(ContextKeyRateLimitRPM, vk.RateLimitRPM)
				c.Set(ContextKeyValidatedKey, vk) // Store full ValidatedKey for permission checks
				c.Next()
				return
			}
		}

		// Fallback to bootstrap admin key (PROXY_ACCESS_KEY)
		if secureCompare(providedKey, envCfg.ProxyAccessKey) {
			c.Set(ContextKeyAPIKeyID, int64(0))
			c.Set(ContextKeyAPIKeyName, "master")
			c.Set(ContextKeyAPIKeyIsAdmin, true)
			c.Set(ContextKeyIsBootstrap, true)
			c.Set(ContextKeyRateLimitRPM, 0) // Master key uses global limit
			c.Set(ContextKeyValidatedKey, (*apikey.ValidatedKey)(nil)) // Bootstrap key has no restrictions
			c.Next()
			return
		}

		if envCfg.ShouldLog("warn") {
			log.Printf("🔒 代理访问密钥验证失败 - IP: %s", c.ClientIP())
		}

		c.JSON(401, gin.H{
			"error": "Invalid proxy access key",
		})
		c.Abort()
	}
}

// WebAuthMiddlewareWithAPIKeyAndFailureLimiter Web 访问控制中间件（支持认证失败限制）
func WebAuthMiddlewareWithAPIKeyAndFailureLimiter(envCfg *config.EnvConfig, cfgManager *config.ConfigManager, apiKeyManager *apikey.Manager, failureLimiter *AuthFailureRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		clientIP := c.ClientIP()

		// 检查 IP 是否被封禁
		if failureLimiter != nil && failureLimiter.IsBlocked(clientIP) {
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "由于多次认证失败，您的 IP 已被临时封禁",
			})
			c.Abort()
			return
		}

		// 公开端点直接放行
		if path == envCfg.HealthCheckPath {
			c.Next()
			return
		}

		// 管理端点需要访问密钥（即使 Web UI 被禁用）
		if envCfg.IsDevelopment() && path == "/admin/dev/info" {
			if !validateAndSetContextWithFailureLimiter(c, envCfg, apiKeyManager, true, failureLimiter) {
				return
			}
			c.Next()
			return
		}

		if path == "/admin/config/reload" {
			if !validateAndSetContextWithFailureLimiter(c, envCfg, apiKeyManager, true, failureLimiter) {
				return
			}
			c.Next()
			return
		}

		// 静态资源文件直接放行
		if isStaticResource(path) {
			c.Next()
			return
		}

		// API 代理端点后续处理
		if strings.HasPrefix(path, "/v1/") {
			c.Next()
			return
		}

		// 如果禁用了 Web UI，返回 404
		if !envCfg.EnableWebUI {
			c.JSON(404, gin.H{
				"error":   "Web界面已禁用",
				"message": "此服务器运行在纯API模式下，请通过API端点访问服务",
			})
			c.Abort()
			return
		}

		// SPA 页面路由直接交给前端处理，但需要排除 /api* 路径
		if path == "/" || path == "/index.html" || (!strings.Contains(path, ".") && !strings.HasPrefix(path, "/api")) {
			c.Next()
			return
		}

		// 检查访问密钥（仅对管理 API 请求）
		if strings.HasPrefix(path, "/api") {
			// 🔒 安全修复: 所有管理 API 端点都需要 admin 权限
			// 管理操作包括: 渠道配置、日志查看、定价设置、备份恢复等
			if !validateAndSetContextWithFailureLimiter(c, envCfg, apiKeyManager, true, failureLimiter) {
				return
			}
		}

		c.Next()
	}
}

// validateAndSetContextWithFailureLimiter 验证 API 密钥并记录失败（支持暴力破解防护）
func validateAndSetContextWithFailureLimiter(c *gin.Context, envCfg *config.EnvConfig, apiKeyManager *apikey.Manager, requireAdmin bool, failureLimiter *AuthFailureRateLimiter) bool {
	providedKey := getAPIKey(c)
	clientIP := c.ClientIP()
	timestamp := time.Now().Format(time.RFC3339)
	logPath := sanitizePathForLogs(c.Request.URL.Path)

	// Try SQLite API keys first (if manager is available)
	if apiKeyManager != nil && providedKey != "" {
		if vk := apiKeyManager.Validate(providedKey); vk != nil {
			// Check admin requirement
			if requireAdmin && !vk.IsAdmin {
				log.Printf("🔒 [权限不足] IP: %s | Path: %s | Time: %s | Key: %s",
					clientIP, logPath, timestamp, vk.Name)
				c.JSON(403, gin.H{
					"error":   "Forbidden",
					"message": "Admin privileges required",
				})
				c.Abort()
				return false
			}

			// 认证成功，清除失败记录
			if failureLimiter != nil {
				failureLimiter.ClearFailures(clientIP)
			}

			// Set context values
			c.Set(ContextKeyAPIKeyID, vk.ID)
			c.Set(ContextKeyAPIKeyName, vk.Name)
			c.Set(ContextKeyAPIKeyIsAdmin, vk.IsAdmin)
			c.Set(ContextKeyIsBootstrap, false)
			c.Set(ContextKeyRateLimitRPM, vk.RateLimitRPM)

			if envCfg.ShouldLog("info") {
				log.Printf("✅ [认证成功] IP: %s | Path: %s | Time: %s | Key: %s",
					clientIP, logPath, timestamp, vk.Name)
			}
			return true
		}
	}

	// Fallback to bootstrap admin key (PROXY_ACCESS_KEY)
	if secureCompare(providedKey, envCfg.ProxyAccessKey) {
		// 认证成功，清除失败记录
		if failureLimiter != nil {
			failureLimiter.ClearFailures(clientIP)
		}

		// Bootstrap admin has full admin privileges
		c.Set(ContextKeyAPIKeyID, int64(0))
		c.Set(ContextKeyAPIKeyName, "master")
		c.Set(ContextKeyAPIKeyIsAdmin, true)
		c.Set(ContextKeyIsBootstrap, true)
		c.Set(ContextKeyRateLimitRPM, 0) // Master key uses global limit

		if envCfg.ShouldLog("info") {
			log.Printf("✅ [认证成功] IP: %s | Path: %s | Time: %s | Key: master",
				clientIP, logPath, timestamp)
		}
		return true
	}

	// 认证失败，记录失败次数
	if failureLimiter != nil {
		failureLimiter.RecordFailure(clientIP)
	}

	reason := "密钥无效"
	if providedKey == "" {
		reason = "密钥缺失"
	}
	log.Printf("🔒 [认证失败] IP: %s | Path: %s | Time: %s | Reason: %s",
		clientIP, logPath, timestamp, reason)

	c.JSON(401, gin.H{
		"error":   "Unauthorized",
		"message": "Invalid or missing access key",
	})
	c.Abort()
	return false
}

func sanitizePathForLogs(path string) string {
	if !strings.HasPrefix(path, "/api/channels/") && !strings.HasPrefix(path, "/api/responses/channels/") {
		return path
	}

	keyMarker := "/keys/"
	i := strings.Index(path, keyMarker)
	if i == -1 {
		return path
	}

	after := path[i+len(keyMarker):]
	if after == "" || strings.HasPrefix(after, "index/") {
		return path
	}

	parts := strings.Split(after, "/")
	if len(parts) == 0 || parts[0] == "" {
		return path
	}

	parts[0] = "<redacted>"
	return path[:i+len(keyMarker)] + strings.Join(parts, "/")
}
