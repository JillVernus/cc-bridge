package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/handlers"
	"github.com/JillVernus/cc-bridge/internal/logger"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/ratelimit"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

func main() {
	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		log.Println("没有找到 .env 文件，使用环境变量或默认值")
	}

	// 设置版本信息到 handlers 包
	handlers.SetVersionInfo(Version, BuildTime, GitCommit)

	// 初始化配置管理器
	envCfg := config.NewEnvConfig()

	// 🔒 安全检查：禁止使用默认访问密钥（除非显式允许）
	// 防止因 ENV 配置错误导致生产环境暴露
	if envCfg.ProxyAccessKey == "your-proxy-access-key" {
		if os.Getenv("ALLOW_INSECURE_DEFAULT_KEY") == "true" && envCfg.IsDevelopment() {
			log.Println("⚠️ 警告: 使用默认 PROXY_ACCESS_KEY，仅限本地开发使用")
		} else {
			log.Fatal("🚨 安全错误: 禁止使用默认 PROXY_ACCESS_KEY。请在 .env 文件中设置强密钥，或在开发环境设置 ALLOW_INSECURE_DEFAULT_KEY=true")
		}
	}
	if len(envCfg.ProxyAccessKey) < 16 {
		log.Fatal("🚨 安全错误: PROXY_ACCESS_KEY 必须至少16个字符。当前长度:", len(envCfg.ProxyAccessKey))
	}

	// 初始化日志系统（必须在其他初始化之前）
	logCfg := &logger.Config{
		LogDir:     envCfg.LogDir,
		LogFile:    envCfg.LogFile,
		MaxSize:    envCfg.LogMaxSize,
		MaxBackups: envCfg.LogMaxBackups,
		MaxAge:     envCfg.LogMaxAge,
		Compress:   envCfg.LogCompress,
		Console:    envCfg.LogToConsole,
	}
	if err := logger.Setup(logCfg); err != nil {
		log.Fatalf("初始化日志系统失败: %v", err)
	}

	cfgManager, err := config.NewConfigManager(".config/config.json")
	if err != nil {
		log.Fatalf("初始化配置管理器失败: %v", err)
	}

	// 初始化会话管理器（Responses API 专用）
	sessionManager := session.NewSessionManager(
		24*time.Hour, // 24小时过期
		100,          // 最多100条消息
		100000,       // 最多100k tokens
	)
	log.Printf("✅ 会话管理器已初始化")

	// 初始化多渠道调度器（Messages 和 Responses 使用独立的指标管理器）
	messagesMetricsManager := metrics.NewMetricsManager()
	responsesMetricsManager := metrics.NewMetricsManager()
	traceAffinityManager := session.NewTraceAffinityManager()
	channelScheduler := scheduler.NewChannelScheduler(cfgManager, messagesMetricsManager, responsesMetricsManager, traceAffinityManager)
	log.Printf("✅ 多渠道调度器已初始化 (失败率阈值: %.0f%%, 滑动窗口: %d)",
		messagesMetricsManager.GetFailureThreshold()*100, messagesMetricsManager.GetWindowSize())

	// 初始化故障转移阈值跟踪器
	failoverTracker := config.NewFailoverTracker()
	log.Printf("✅ 故障转移阈值跟踪器已初始化")

	// 初始化请求日志管理器
	reqLogManager, err := requestlog.NewManager(".config/request_logs.db")
	if err != nil {
		log.Printf("⚠️ 请求日志管理器初始化失败: %v (日志功能将被禁用)", err)
		reqLogManager = nil
	} else {
		log.Printf("✅ 请求日志管理器已初始化")

		// 连接调度器与请求日志管理器（用于配额渠道暂停检查）
		channelScheduler.SetSuspensionChecker(reqLogManager)

		// 初始化配额持久化（使用请求日志数据库）
		quotaAdapter := quota.NewRequestLogAdapter(reqLogManager)
		quota.GetManager().SetPersister(quotaAdapter)

		// 启动定期清理 stale pending 请求的 goroutine
		go func() {
			// 立即执行一次清理（处理服务重启前遗留的 pending 请求）
			if updated, err := reqLogManager.CleanupStalePending(300); err != nil {
				log.Printf("⚠️ 清理 stale pending 请求失败: %v", err)
			} else if updated > 0 {
				log.Printf("✅ 启动时清理了 %d 个 stale pending 请求", updated)
			}

			// 清理过期的渠道暂停记录
			if cleared, err := reqLogManager.ClearExpiredSuspensions(); err != nil {
				log.Printf("⚠️ 清理过期渠道暂停记录失败: %v", err)
			} else if cleared > 0 {
				log.Printf("✅ 启动时清理了 %d 个过期渠道暂停记录", cleared)
			}

			// 每 60 秒检查一次
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := reqLogManager.CleanupStalePending(300); err != nil {
					log.Printf("⚠️ 清理 stale pending 请求失败: %v", err)
				}
				// 同时清理过期的渠道暂停记录
				if _, err := reqLogManager.ClearExpiredSuspensions(); err != nil {
					log.Printf("⚠️ 清理过期渠道暂停记录失败: %v", err)
				}
			}
		}()

		// 启动调试日志清理 goroutine（每小时执行一次）
		reqLogManager.StartDebugLogCleanup(func() int {
			cfg := cfgManager.GetDebugLogConfig()
			return cfg.GetRetentionHours()
		})
	}

	// 初始化用量配额管理器（用于渠道配额追踪）
	usageQuotaManager, err := quota.NewUsageManager(".config", cfgManager)
	if err != nil {
		log.Printf("⚠️ 用量配额管理器初始化失败: %v (配额追踪将被禁用)", err)
		usageQuotaManager = nil
	} else {
		log.Printf("✅ 用量配额管理器已初始化")
	}

	// 初始化 API Key 管理器（使用与请求日志相同的数据库）
	var apiKeyManager *apikey.Manager
	if reqLogManager != nil {
		apiKeyManager, err = apikey.NewManager(reqLogManager.GetDB())
		if err != nil {
			log.Printf("⚠️ API Key 管理器初始化失败: %v (API Key 功能将被禁用)", err)
			apiKeyManager = nil
		} else {
			log.Printf("✅ API Key 管理器已初始化")
		}
	}

	// 初始化定价管理器
	_, err = pricing.InitManager(".config/pricing.json")
	if err != nil {
		log.Printf("⚠️ 定价管理器初始化失败: %v (将使用默认定价)", err)
	} else {
		log.Printf("✅ 定价管理器已初始化")
	}

	// 初始化速率限制配置管理器
	rateLimitCfgManager, err := ratelimit.InitManager(".config/ratelimit.json")
	if err != nil {
		log.Printf("⚠️ 速率限制配置管理器初始化失败: %v (将使用默认配置)", err)
	} else {
		log.Printf("✅ 速率限制配置管理器已初始化")
	}

	// 设置 Gin 模式
	if envCfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化速率限制器（使用配置管理器的配置）
	var apiRateLimiter, portalRateLimiter *middleware.RateLimiter
	var authFailureLimiter *middleware.AuthFailureRateLimiter

	if rateLimitCfgManager != nil {
		cfg := rateLimitCfgManager.GetConfig()
		apiRateLimiter = middleware.NewRateLimiterWithConfig(cfg.API)
		portalRateLimiter = middleware.NewRateLimiterWithConfig(cfg.Portal)
		authFailureLimiter = middleware.NewAuthFailureRateLimiterWithConfig(cfg.AuthFailure)

		// 设置配置变更回调
		rateLimitCfgManager.SetOnChangeCallback(func(newCfg ratelimit.RateLimitConfig) {
			apiRateLimiter.UpdateConfig(newCfg.API)
			portalRateLimiter.UpdateConfig(newCfg.Portal)
			authFailureLimiter.UpdateConfig(newCfg.AuthFailure)
		})

		log.Printf("✅ 速率限制器已初始化 (API: %d rpm, Portal: %d rpm)",
			cfg.API.RequestsPerMinute, cfg.Portal.RequestsPerMinute)
	} else {
		// Fallback to default config
		defaultCfg := ratelimit.GetDefaultConfig()
		apiRateLimiter = middleware.NewRateLimiterWithConfig(defaultCfg.API)
		portalRateLimiter = middleware.NewRateLimiterWithConfig(defaultCfg.Portal)
		authFailureLimiter = middleware.NewAuthFailureRateLimiterWithConfig(defaultCfg.AuthFailure)
		log.Printf("✅ 速率限制器已初始化 (使用默认配置)")
	}

	// 创建路由器（不使用 gin.Default() 以避免默认的 Logger 中间件产生大量日志）
	r := gin.New()
	r.Use(gin.Recovery()) // 只添加 Recovery 中间件，不添加 Logger

	// 🔒 配置可信代理（防止 IP 欺骗攻击）
	// 如果设置了 TRUSTED_PROXIES 环境变量，只信任指定的代理 IP
	// 如果未设置，在生产环境默认不信任任何代理（使用直连 IP）
	if len(envCfg.TrustedProxies) > 0 {
		if err := r.SetTrustedProxies(envCfg.TrustedProxies); err != nil {
			log.Printf("⚠️ 设置可信代理失败: %v", err)
		} else {
			log.Printf("✅ 已配置可信代理: %v", envCfg.TrustedProxies)
		}
	} else if envCfg.IsProduction() {
		// 生产环境默认不信任任何代理，使用直连 IP
		if err := r.SetTrustedProxies(nil); err != nil {
			log.Printf("⚠️ 禁用可信代理失败: %v", err)
		} else {
			log.Printf("✅ 生产环境: 已禁用代理信任 (使用直连 IP)")
		}
	}
	// 开发环境保持 Gin 默认行为（信任所有代理）

	// 配置安全响应头（仅影响 Web UI）
	r.Use(middleware.SecurityHeadersMiddleware())

	// 配置 CORS
	r.Use(middleware.CORSMiddleware(envCfg))

	// 🔒 Portal 速率限制中间件（用于 /api/* 端点）
	r.Use(middleware.PortalRateLimitMiddleware(portalRateLimiter))

	// Web UI 访问控制中间件
	r.Use(middleware.WebAuthMiddlewareWithAPIKeyAndFailureLimiter(envCfg, cfgManager, apiKeyManager, authFailureLimiter))

	// 🔒 健康检查端点（最小化响应，无需认证）
	// 只返回 {"status": "healthy"}，不暴露系统信息
	r.GET(envCfg.HealthCheckPath, handlers.HealthCheck())

	// 配置重载端点
	r.POST("/admin/config/reload", handlers.ReloadConfig(cfgManager))

	// 详细健康检查端点（需要认证，返回完整系统信息）
	r.GET("/api/health/details", handlers.HealthCheckDetailed(envCfg, cfgManager))

	// 开发信息端点
	if envCfg.IsDevelopment() {
		r.GET("/admin/dev/info", handlers.DevInfo(envCfg, cfgManager))
	}

	// 🔒 Deprecated endpoints toggle (insecure: puts API keys in URL path)
	// Only enable for backwards compatibility with legacy clients.
	allowDeprecatedKeyPathEndpoints := os.Getenv("ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS") == "true"
	if allowDeprecatedKeyPathEndpoints {
		log.Printf("⚠️ 已启用不安全的旧版 API Key 路径端点 (keys in URL path) - 建议仅用于临时兼容旧客户端")
	}

	// Web 管理界面 API 路由
	apiGroup := r.Group("/api")
	{
		// 渠道管理 (兼容前端 /api/channels 路由)
		apiGroup.GET("/channels", handlers.GetUpstreams(cfgManager))
		apiGroup.POST("/channels", handlers.AddUpstream(cfgManager))
		apiGroup.PUT("/channels/:id", handlers.UpdateUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/channels/:id", handlers.DeleteUpstream(cfgManager))
		apiGroup.POST("/channels/:id/keys", handlers.AddApiKey(cfgManager))
		if allowDeprecatedKeyPathEndpoints {
			apiGroup.DELETE("/channels/:id/keys/:apiKey", handlers.DeleteApiKey(cfgManager))            // Deprecated: use index-based endpoint
			apiGroup.POST("/channels/:id/keys/:apiKey/top", handlers.MoveApiKeyToTop(cfgManager))       // Deprecated: use index-based endpoint
			apiGroup.POST("/channels/:id/keys/:apiKey/bottom", handlers.MoveApiKeyToBottom(cfgManager)) // Deprecated: use index-based endpoint
		}
		apiGroup.DELETE("/channels/:id/keys/index/:keyIndex", handlers.DeleteApiKeyByIndex(cfgManager))  // Secure: uses key index
		apiGroup.POST("/channels/:id/keys/index/:keyIndex/top", handlers.MoveApiKeyToTopByIndex(cfgManager))
		apiGroup.POST("/channels/:id/keys/index/:keyIndex/bottom", handlers.MoveApiKeyToBottomByIndex(cfgManager))

		// 多渠道调度 API
		apiGroup.POST("/channels/reorder", handlers.ReorderChannels(cfgManager))
		apiGroup.PATCH("/channels/:id/status", handlers.SetChannelStatus(cfgManager))
		apiGroup.POST("/channels/:id/resume", handlers.ResumeChannel(channelScheduler, false))
		apiGroup.POST("/channels/:id/promotion", handlers.SetChannelPromotion(cfgManager))
		apiGroup.GET("/channels/metrics", handlers.GetChannelMetrics(messagesMetricsManager))
		apiGroup.GET("/channels/scheduler/stats", handlers.GetSchedulerStats(channelScheduler))

		// Responses 渠道管理
		apiGroup.GET("/responses/channels", handlers.GetResponsesUpstreams(cfgManager))
		apiGroup.POST("/responses/channels", handlers.AddResponsesUpstream(cfgManager))
		apiGroup.PUT("/responses/channels/:id", handlers.UpdateResponsesUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/responses/channels/:id", handlers.DeleteResponsesUpstream(cfgManager))
		apiGroup.POST("/responses/channels/:id/keys", handlers.AddResponsesApiKey(cfgManager))
		if allowDeprecatedKeyPathEndpoints {
			apiGroup.DELETE("/responses/channels/:id/keys/:apiKey", handlers.DeleteResponsesApiKey(cfgManager))            // Deprecated: use index-based endpoint
			apiGroup.POST("/responses/channels/:id/keys/:apiKey/top", handlers.MoveResponsesApiKeyToTop(cfgManager))       // Deprecated: use index-based endpoint
			apiGroup.POST("/responses/channels/:id/keys/:apiKey/bottom", handlers.MoveResponsesApiKeyToBottom(cfgManager)) // Deprecated: use index-based endpoint
		}
		apiGroup.DELETE("/responses/channels/:id/keys/index/:keyIndex", handlers.DeleteResponsesApiKeyByIndex(cfgManager))  // Secure: uses key index
		apiGroup.POST("/responses/channels/:id/keys/index/:keyIndex/top", handlers.MoveResponsesApiKeyToTopByIndex(cfgManager))
		apiGroup.POST("/responses/channels/:id/keys/index/:keyIndex/bottom", handlers.MoveResponsesApiKeyToBottomByIndex(cfgManager))
		apiGroup.PUT("/responses/loadbalance", handlers.UpdateResponsesLoadBalance(cfgManager))

		// Responses 多渠道调度 API
		apiGroup.POST("/responses/channels/reorder", handlers.ReorderResponsesChannels(cfgManager))
		apiGroup.PATCH("/responses/channels/:id/status", handlers.SetResponsesChannelStatus(cfgManager))
		apiGroup.POST("/responses/channels/:id/resume", handlers.ResumeChannel(channelScheduler, true))
		apiGroup.POST("/responses/channels/:id/promotion", handlers.SetResponsesChannelPromotion(cfgManager))
		apiGroup.GET("/responses/channels/metrics", handlers.GetResponsesChannelMetrics(responsesMetricsManager))
		apiGroup.GET("/responses/channels/:id/oauth/status", handlers.GetResponsesChannelOAuthStatus(cfgManager))

		// 负载均衡
		apiGroup.PUT("/loadbalance", handlers.UpdateLoadBalance(cfgManager))

		// Ping测试
		apiGroup.GET("/ping/:id", handlers.PingChannel(cfgManager))
		apiGroup.GET("/ping", handlers.PingAllChannels(cfgManager))

		// 请求日志 API
		if reqLogManager != nil {
			reqLogHandler := handlers.NewRequestLogHandler(reqLogManager)
			apiGroup.GET("/logs", reqLogHandler.GetLogs)
			apiGroup.GET("/logs/stats", reqLogHandler.GetStats)
			apiGroup.GET("/logs/stats/history", reqLogHandler.GetStatsHistory)
			apiGroup.GET("/logs/providers/stats/history", reqLogHandler.GetProviderStatsHistory)
			apiGroup.GET("/logs/channels/:id/stats/history", reqLogHandler.GetChannelStatsHistory)
			apiGroup.GET("/logs/sessions/active", reqLogHandler.GetActiveSessions)
			apiGroup.GET("/logs/:id", reqLogHandler.GetLogByID)
			apiGroup.DELETE("/logs", reqLogHandler.ClearLogs)
			apiGroup.POST("/logs/cleanup", reqLogHandler.CleanupLogs)

			// 调试日志 API
			apiGroup.GET("/logs/:id/debug", reqLogHandler.GetDebugLog)
			apiGroup.DELETE("/logs/debug", reqLogHandler.PurgeDebugLogs)
			apiGroup.GET("/logs/debug/stats", reqLogHandler.GetDebugLogStats)

			// 用户别名 API
			apiGroup.GET("/aliases", reqLogHandler.GetAliases)
			apiGroup.PUT("/aliases/:userId", reqLogHandler.SetAlias)
			apiGroup.DELETE("/aliases/:userId", reqLogHandler.DeleteAlias)
			apiGroup.POST("/aliases/import", reqLogHandler.ImportAliases)
		}

		// API Key 管理 API (需要 admin 权限)
		if apiKeyManager != nil {
			apiKeyHandler := handlers.NewAPIKeyHandler(apiKeyManager)
			apiGroup.GET("/keys", apiKeyHandler.ListKeys)
			apiGroup.POST("/keys", apiKeyHandler.CreateKey)
			apiGroup.GET("/keys/:id", apiKeyHandler.GetKey)
			apiGroup.PUT("/keys/:id", apiKeyHandler.UpdateKey)
			apiGroup.DELETE("/keys/:id", apiKeyHandler.DeleteKey)
			apiGroup.POST("/keys/:id/enable", apiKeyHandler.EnableKey)
			apiGroup.POST("/keys/:id/disable", apiKeyHandler.DisableKey)
			apiGroup.POST("/keys/:id/revoke", apiKeyHandler.RevokeKey)
		}

		// 用量配额 API (渠道配额追踪)
		if usageQuotaManager != nil {
			usageQuotaHandler := handlers.NewUsageQuotaHandler(usageQuotaManager, reqLogManager)
			// Messages 渠道配额
			apiGroup.GET("/channels/usage", usageQuotaHandler.GetAllChannelUsageQuotas)
			apiGroup.GET("/channels/:id/usage", usageQuotaHandler.GetChannelUsageQuota)
			apiGroup.POST("/channels/:id/usage/reset", usageQuotaHandler.ResetChannelUsageQuota)
			// Responses 渠道配额
			apiGroup.GET("/responses/channels/usage", usageQuotaHandler.GetAllResponsesChannelUsageQuotas)
			apiGroup.GET("/responses/channels/:id/usage", usageQuotaHandler.GetResponsesChannelUsageQuota)
			apiGroup.POST("/responses/channels/:id/usage/reset", usageQuotaHandler.ResetResponsesChannelUsageQuota)
		}

		// 定价配置 API
		apiGroup.GET("/pricing", handlers.GetPricing())
		apiGroup.PUT("/pricing", handlers.UpdatePricing())
		apiGroup.PUT("/pricing/models/:model", handlers.AddModelPricing())
		apiGroup.DELETE("/pricing/models/:model", handlers.DeleteModelPricing())
		apiGroup.POST("/pricing/reset", handlers.ResetPricingToDefault())

		// 速率限制配置 API
		apiGroup.GET("/ratelimit", handlers.GetRateLimitConfig())
		apiGroup.PUT("/ratelimit", handlers.UpdateRateLimitConfig())
		apiGroup.POST("/ratelimit/reset", handlers.ResetRateLimitConfig())

		// 调试日志配置 API
		apiGroup.GET("/config/debug-log", handlers.GetDebugLogConfig(cfgManager))
		apiGroup.PUT("/config/debug-log", handlers.UpdateDebugLogConfig(cfgManager))

		// 故障转移配置 API
		apiGroup.GET("/config/failover", handlers.GetFailoverConfig(cfgManager))
		apiGroup.PUT("/config/failover", handlers.UpdateFailoverConfig(cfgManager))
		apiGroup.POST("/config/failover/reset", handlers.ResetFailoverConfig(cfgManager))

		// 备份/恢复 API
		apiGroup.POST("/config/backup", handlers.CreateBackup(cfgManager))
		apiGroup.GET("/config/backups", handlers.ListBackups())
		apiGroup.POST("/config/restore/:filename", handlers.RestoreBackup(cfgManager))
		apiGroup.DELETE("/config/backups/:filename", handlers.DeleteBackup())
	}

	// 代理端点 - 统一入口（带 API 速率限制）
	v1Group := r.Group("/v1")
	// 先认证再限流：支持按 API Key 应用自定义 RPM
	v1Group.Use(middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager))
	v1Group.Use(middleware.APIRateLimitMiddleware(apiRateLimiter))
	{
		v1Group.POST("/messages", handlers.ProxyHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker))
		v1Group.POST("/responses", handlers.ResponsesHandlerWithAPIKey(envCfg, cfgManager, sessionManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker))
	}

	// 静态文件服务 (嵌入的前端)
	if envCfg.EnableWebUI {
		handlers.ServeFrontend(r, frontendFS)
	} else {
		// 纯 API 模式
		r.GET("/", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"name":    "CC-Bridge",
				"mode":    "API Only",
				"version": "1.0.0",
				"endpoints": gin.H{
					"health": envCfg.HealthCheckPath,
					"proxy":  "/v1/messages",
					"config": "/admin/config/reload",
				},
				"message": "Web界面已禁用，此服务器运行在纯API模式下",
			})
		})
	}

	// 启动服务器
	addr := fmt.Sprintf(":%d", envCfg.Port)
	fmt.Printf("\n🚀 CC-Bridge 服务器已启动\n")
	fmt.Printf("📌 版本: %s\n", Version)
	if BuildTime != "unknown" {
		fmt.Printf("🕐 构建时间: %s\n", BuildTime)
	}
	if GitCommit != "unknown" {
		fmt.Printf("🔖 Git提交: %s\n", GitCommit)
	}
	fmt.Printf("🌐 管理界面: http://localhost:%d\n", envCfg.Port)
	fmt.Printf("📍 API 地址: http://localhost:%d/v1\n", envCfg.Port)
	fmt.Printf("📋 Claude Messages: POST /v1/messages\n")
	fmt.Printf("📋 Codex Responses: POST /v1/responses\n")
	fmt.Printf("💚 健康检查: GET %s\n", envCfg.HealthCheckPath)
	fmt.Printf("📊 环境: %s\n", envCfg.Env)
	// 检查是否使用默认密码，给予提示
	if envCfg.ProxyAccessKey == "your-proxy-access-key" {
		fmt.Printf("🔑 访问密钥: your-proxy-access-key (默认值，建议通过 .env 文件修改)\n")
	}
	fmt.Printf("\n")

	if err := r.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
