package handlers

import (
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// HealthCheck 健康检查处理器（最小化响应，无需认证）
// 🔒 安全修复: 只返回基本状态，不暴露系统信息
func HealthCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "healthy",
		})
	}
}

// HealthCheckDetailed 详细健康检查处理器（需要认证）
// 返回完整的系统信息，仅供管理员使用
func HealthCheckDetailed(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		healthData := gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"uptime":    time.Since(startTime).Seconds(),
			"mode":      envCfg.Env,
			"version":   getVersion(),
			"config": gin.H{
				"upstreamCount":          len(cfg.Upstream),
				"responsesUpstreamCount": len(cfg.ResponsesUpstream),
				"loadBalance":            cfg.LoadBalance,
				"responsesLoadBalance":   cfg.ResponsesLoadBalance,
			},
		}

		c.JSON(200, healthData)
	}
}

// getVersion 获取版本信息
func getVersion() gin.H {
	// 这些变量在编译时通过 -ldflags 注入
	// 从根目录 VERSION 文件读取
	return gin.H{
		"version":   getVersionString(),
		"buildTime": getBuildTime(),
		"gitCommit": getGitCommit(),
	}
}

// 以下函数用于从 main 包获取版本信息
// 由于无法直接导入 main 包，使用默认值
var (
	versionString = "v0.0.0-dev"
	buildTime     = "unknown"
	gitCommit     = "unknown"
)

func getVersionString() string { return versionString }
func getBuildTime() string     { return buildTime }
func getGitCommit() string     { return gitCommit }

// SetVersionInfo 设置版本信息（从 main 调用）
func SetVersionInfo(version, build, commit string) {
	versionString = version
	buildTime = build
	gitCommit = commit
}

// ReloadConfig 配置重载处理器
func ReloadConfig(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := cfgManager.ReloadConfig(); err != nil {
			c.JSON(500, gin.H{
				"status":    "error",
				"message":   "Config reload failed",
				"error":     err.Error(),
				"timestamp": time.Now().Format(time.RFC3339),
			})
			return
		}

		config := cfgManager.GetConfig()
		c.JSON(200, gin.H{
			"status":    "success",
			"message":   "Config reloaded",
			"timestamp": time.Now().Format(time.RFC3339),
			"config": gin.H{
				"upstreamCount":        len(config.Upstream),
				"loadBalance":          config.LoadBalance,
				"responsesLoadBalance": config.ResponsesLoadBalance,
			},
		})
	}
}

// DevInfo 开发信息处理器
// 🔒 安全修复: 不再返回完整配置和环境变量，防止密钥泄露
func DevInfo(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		// 返回脱敏的配置摘要，不包含 API 密钥
		c.JSON(200, gin.H{
			"status":    "development",
			"timestamp": time.Now().Format(time.RFC3339),
			"config": gin.H{
				"upstreamCount":          len(cfg.Upstream),
				"responsesUpstreamCount": len(cfg.ResponsesUpstream),
				"loadBalance":            cfg.LoadBalance,
				"responsesLoadBalance":   cfg.ResponsesLoadBalance,
			},
			"environment": gin.H{
				"env":             envCfg.Env,
				"port":            envCfg.Port,
				"enableWebUI":     envCfg.EnableWebUI,
				"enableCORS":      envCfg.EnableCORS,
				"enableRateLimit": envCfg.EnableRateLimit,
				"logLevel":        envCfg.LogLevel,
				// 🔒 不暴露: ProxyAccessKey, CORSOrigin 等敏感配置
			},
		})
	}
}

var startTime = time.Now()
