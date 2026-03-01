package main

import (
	"embed"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/aliases"
	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/forwardproxy"
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
	// 初始化随机数生成器（用于 random 负载均衡策略）
	rand.Seed(time.Now().UnixNano())

	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		log.Println("没有找到 .env 文件，使用环境变量或默认值")
	}

	// 设置版本信息到 handlers 包
	handlers.SetVersionInfo(Version, BuildTime, GitCommit)

	// 初始化配置管理器
	envCfg := config.NewEnvConfig()

	log.Printf(
		"🧭 Runtime storage profile: ENV=%s, STORAGE_BACKEND=%s, DATABASE_TYPE=%s",
		envCfg.Env,
		envCfg.StorageBackend,
		envCfg.DatabaseType,
	)
	if envCfg.IsDevelopment() && strings.EqualFold(envCfg.StorageBackend, "database") && strings.EqualFold(envCfg.DatabaseType, "postgresql") {
		log.Printf("🛠️ Development environment is using PostgreSQL")
	}

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

	// Initialize database storage if STORAGE_BACKEND=database
	// This is done early so we can use unified database for request logs and API keys
	dbStorageMgr := InitDBStorage(envCfg, cfgManager)
	if dbStorageMgr != nil {
		defer dbStorageMgr.Close()
	}

	// 初始化请求日志管理器
	// When using unified database, get the manager from dbStorageMgr
	var reqLogManager *requestlog.Manager
	if dbStorageMgr != nil && dbStorageMgr.GetRequestLogManager() != nil {
		reqLogManager = dbStorageMgr.GetRequestLogManager()
		log.Printf("✅ 请求日志管理器已初始化 (using unified database)")
	} else {
		// Fallback to separate database
		var err error
		reqLogManager, err = requestlog.NewManager(".config/request_logs.db")
		if err != nil {
			log.Printf("⚠️ 请求日志管理器初始化失败: %v (日志功能将被禁用)", err)
			reqLogManager = nil
		} else {
			log.Printf("✅ 请求日志管理器已初始化")
		}
	}

	if reqLogManager != nil {
		// Prefer stable channel UID linkage for request logs.
		// This ensures reorders do not break restore and analytics.
		reqLogManager.SetChannelUIDResolver(func(endpoint string, index int, channelName string) string {
			cfg := cfgManager.GetConfig()

			findByName := func(upstreams []config.UpstreamConfig, normalizedName string) string {
				matches := 0
				resolvedUID := ""
				for _, upstream := range upstreams {
					if strings.ToLower(strings.TrimSpace(upstream.Name)) == normalizedName {
						channelUID := strings.TrimSpace(upstream.ID)
						if channelUID == "" {
							continue
						}
						matches++
						resolvedUID = channelUID
					}
				}
				if matches == 1 {
					return resolvedUID
				}
				return ""
			}

			normalizedName := strings.ToLower(strings.TrimSpace(channelName))
			if normalizedName != "" {
				switch endpoint {
				case "/v1/messages":
					if uid := findByName(cfg.Upstream, normalizedName); uid != "" {
						return uid
					}
				case "/v1/responses":
					if uid := findByName(cfg.ResponsesUpstream, normalizedName); uid != "" {
						return uid
					}
				case "/v1/gemini":
					if uid := findByName(cfg.GeminiUpstream, normalizedName); uid != "" {
						return uid
					}
				default:
					if uid := findByName(cfg.Upstream, normalizedName); uid != "" {
						return uid
					}
					if uid := findByName(cfg.ResponsesUpstream, normalizedName); uid != "" {
						return uid
					}
					if uid := findByName(cfg.GeminiUpstream, normalizedName); uid != "" {
						return uid
					}
				}
			}

			var upstreams []config.UpstreamConfig
			switch endpoint {
			case "/v1/messages":
				upstreams = cfg.Upstream
			case "/v1/responses":
				upstreams = cfg.ResponsesUpstream
			case "/v1/gemini":
				upstreams = cfg.GeminiUpstream
			default:
				return ""
			}
			if index >= 0 && index < len(upstreams) {
				return strings.TrimSpace(upstreams[index].ID)
			}
			return ""
		})

		// Restore recent API call slots from persisted request logs (for channel metrics UI).
		// Run immediately, then run a second pass after startup settles (DB/config polling)
		// to correct any temporary index/name mismatches caused by stale pre-poll config.
		seedRecentCallMetricsFromLogs(reqLogManager, channelScheduler, cfgManager, false)
		go func() {
			time.Sleep(8 * time.Second)
			seedRecentCallMetricsFromLogs(reqLogManager, channelScheduler, cfgManager, false)
		}()

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

	// Enable database write-through for usage quota manager
	if dbStorageMgr != nil && usageQuotaManager != nil {
		usageQuotaManager.SetDBStorage(dbStorageMgr.GetUsageStorage())
		// Reload usage from database now that DB storage is connected
		usageQuotaManager.ReloadFromDB()
		log.Printf("✅ 用量配额管理器已连接数据库存储")
	}

	// 初始化 API Key 管理器
	// When using unified database, get the manager from dbStorageMgr
	var apiKeyManager *apikey.Manager
	if dbStorageMgr != nil && dbStorageMgr.GetAPIKeyManager() != nil {
		apiKeyManager = dbStorageMgr.GetAPIKeyManager()
		log.Printf("✅ API Key 管理器已初始化 (using unified database)")
	} else if reqLogManager != nil {
		// Fallback to using request log manager's database
		var err error
		apiKeyManager, err = apikey.NewManager(reqLogManager.GetDB())
		if err != nil {
			log.Printf("⚠️ API Key 管理器初始化失败: %v (API Key 功能将被禁用)", err)
			apiKeyManager = nil
		} else {
			log.Printf("✅ API Key 管理器已初始化")
		}
	}

	// Set up channel ID resolver for migrating legacy channel index permissions to stable IDs
	if apiKeyManager != nil && cfgManager != nil {
		apiKeyManager.SetChannelIDResolver(func(endpointType string, index int) string {
			cfg := cfgManager.GetConfig()
			var upstreams []config.UpstreamConfig
			switch endpointType {
			case "responses":
				upstreams = cfg.ResponsesUpstream
			case "gemini":
				upstreams = cfg.GeminiUpstream
			default: // "messages"
				upstreams = cfg.Upstream
			}
			if index >= 0 && index < len(upstreams) {
				return upstreams[index].ID
			}
			return ""
		})
		log.Printf("✅ Channel ID resolver configured for API key permission migration")
	}

	// 初始化定价管理器
	_, err = pricing.InitManager(".config/pricing.json")
	if err != nil {
		log.Printf("⚠️ 定价管理器初始化失败: %v (将使用默认定价)", err)
	} else {
		log.Printf("✅ 定价管理器已初始化")
	}

	// 初始化模型别名管理器
	_, err = aliases.InitManager(".config/model-aliases.json")
	if err != nil {
		log.Printf("⚠️ 模型别名管理器初始化失败: %v (将使用默认别名)", err)
	} else {
		log.Printf("✅ 模型别名管理器已初始化")
	}

	// Enable database write-through for pricing and aliases managers
	if dbStorageMgr != nil {
		if pricingMgr := pricing.GetManager(); pricingMgr != nil {
			dbStorageMgr.ConnectPricingManager(pricingMgr)
		}
		if aliasesMgr := aliases.GetManager(); aliasesMgr != nil {
			dbStorageMgr.ConnectAliasesManager(aliasesMgr)
		}
	}

	// 初始化速率限制配置管理器
	rateLimitCfgManager, err := ratelimit.InitManager(".config/ratelimit.json")
	if err != nil {
		log.Printf("⚠️ 速率限制配置管理器初始化失败: %v (将使用默认配置)", err)
	} else {
		log.Printf("✅ 速率限制配置管理器已初始化")
	}

	// 初始化正向代理 (Forward Proxy)
	var fpServer *forwardproxy.Server
	if envCfg.ForwardProxyEnabled {
		fpServer, err = forwardproxy.NewServer(forwardproxy.ServerConfig{
			Port:              envCfg.ForwardProxyPort,
			BindAddress:       envCfg.ForwardProxyBindAddress,
			CertDir:           envCfg.ForwardProxyCertDir,
			ConfigDir:         ".config",
			InterceptDomains:  envCfg.ForwardProxyInterceptDomains,
			Enabled:           true,
			RequestLogManager: reqLogManager,
			ConfigManager:     &fpConfigAdapter{cfgManager},
		})
		if err != nil {
			log.Printf("⚠️ Forward proxy initialization failed: %v", err)
			fpServer = nil
		} else {
			go func() {
				log.Printf("✅ Forward proxy listening on :%d", envCfg.ForwardProxyPort)
				if err := fpServer.ListenAndServe(); err != nil {
					log.Printf("⚠️ Forward proxy error: %v", err)
				}
			}()
		}
	}

	// 设置 Gin 模式
	if envCfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化速率限制器（使用配置管理器的配置）
	var apiRateLimiter, portalRateLimiter *middleware.RateLimiter
	var authFailureLimiter *middleware.AuthFailureRateLimiter
	channelRateLimiter := middleware.NewChannelRateLimiter()
	log.Printf("✅ 渠道速率限制器已初始化")

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
		apiGroup.GET("/messages/channels/current", handlers.GetCurrentMessagesChannel(cfgManager))
		apiGroup.POST("/channels", handlers.AddUpstream(cfgManager))
		apiGroup.PUT("/channels/:id", handlers.UpdateUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/channels/:id", handlers.DeleteUpstream(cfgManager, channelRateLimiter))
		apiGroup.POST("/channels/:id/keys", handlers.AddApiKey(cfgManager))
		if allowDeprecatedKeyPathEndpoints {
			apiGroup.DELETE("/channels/:id/keys/:apiKey", handlers.DeleteApiKey(cfgManager))            // Deprecated: use index-based endpoint
			apiGroup.POST("/channels/:id/keys/:apiKey/top", handlers.MoveApiKeyToTop(cfgManager))       // Deprecated: use index-based endpoint
			apiGroup.POST("/channels/:id/keys/:apiKey/bottom", handlers.MoveApiKeyToBottom(cfgManager)) // Deprecated: use index-based endpoint
		}
		apiGroup.DELETE("/channels/:id/keys/index/:keyIndex", handlers.DeleteApiKeyByIndex(cfgManager)) // Secure: uses key index
		apiGroup.POST("/channels/:id/keys/index/:keyIndex/top", handlers.MoveApiKeyToTopByIndex(cfgManager))
		apiGroup.POST("/channels/:id/keys/index/:keyIndex/bottom", handlers.MoveApiKeyToBottomByIndex(cfgManager))

		// 多渠道调度 API
		apiGroup.POST("/channels/reorder", handlers.ReorderChannels(cfgManager))
		apiGroup.PATCH("/channels/:id/status", handlers.SetChannelStatus(cfgManager))
		apiGroup.POST("/channels/:id/resume", handlers.ResumeChannel(channelScheduler, false))
		apiGroup.POST("/channels/:id/promotion", handlers.SetChannelPromotion(cfgManager))
		apiGroup.GET("/channels/:id/test-mapping", handlers.TestCompositeMapping(cfgManager))
		apiGroup.GET("/channels/:id/models", handlers.FetchUpstreamModels(cfgManager))
		apiGroup.GET("/channels/metrics", handlers.GetChannelMetrics(messagesMetricsManager, cfgManager))
		apiGroup.GET("/channels/scheduler/stats", handlers.GetSchedulerStats(channelScheduler))

		// Responses 渠道管理
		apiGroup.GET("/responses/channels", handlers.GetResponsesUpstreams(cfgManager))
		apiGroup.POST("/responses/channels", handlers.AddResponsesUpstream(cfgManager))
		apiGroup.PUT("/responses/channels/:id", handlers.UpdateResponsesUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/responses/channels/:id", handlers.DeleteResponsesUpstream(cfgManager, channelRateLimiter))
		apiGroup.POST("/responses/channels/:id/keys", handlers.AddResponsesApiKey(cfgManager))
		if allowDeprecatedKeyPathEndpoints {
			apiGroup.DELETE("/responses/channels/:id/keys/:apiKey", handlers.DeleteResponsesApiKey(cfgManager))            // Deprecated: use index-based endpoint
			apiGroup.POST("/responses/channels/:id/keys/:apiKey/top", handlers.MoveResponsesApiKeyToTop(cfgManager))       // Deprecated: use index-based endpoint
			apiGroup.POST("/responses/channels/:id/keys/:apiKey/bottom", handlers.MoveResponsesApiKeyToBottom(cfgManager)) // Deprecated: use index-based endpoint
		}
		apiGroup.DELETE("/responses/channels/:id/keys/index/:keyIndex", handlers.DeleteResponsesApiKeyByIndex(cfgManager)) // Secure: uses key index
		apiGroup.POST("/responses/channels/:id/keys/index/:keyIndex/top", handlers.MoveResponsesApiKeyToTopByIndex(cfgManager))
		apiGroup.POST("/responses/channels/:id/keys/index/:keyIndex/bottom", handlers.MoveResponsesApiKeyToBottomByIndex(cfgManager))
		apiGroup.PUT("/responses/loadbalance", handlers.UpdateResponsesLoadBalance(cfgManager))

		// Responses 多渠道调度 API
		apiGroup.POST("/responses/channels/reorder", handlers.ReorderResponsesChannels(cfgManager))
		apiGroup.PATCH("/responses/channels/:id/status", handlers.SetResponsesChannelStatus(cfgManager))
		apiGroup.POST("/responses/channels/:id/resume", handlers.ResumeChannel(channelScheduler, true))
		apiGroup.POST("/responses/channels/:id/promotion", handlers.SetResponsesChannelPromotion(cfgManager))
		apiGroup.GET("/responses/channels/metrics", handlers.GetResponsesChannelMetrics(responsesMetricsManager, cfgManager))
		apiGroup.GET("/responses/channels/:id/oauth/status", handlers.GetResponsesChannelOAuthStatus(cfgManager))
		apiGroup.GET("/responses/channels/:id/models", handlers.FetchResponsesUpstreamModels(cfgManager))

		// Gemini 渠道管理
		apiGroup.GET("/gemini/channels", handlers.GetGeminiUpstreams(cfgManager))
		apiGroup.POST("/gemini/channels", handlers.AddGeminiUpstream(cfgManager))
		apiGroup.PUT("/gemini/channels/:id", handlers.UpdateGeminiUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/gemini/channels/:id", handlers.DeleteGeminiUpstream(cfgManager, channelRateLimiter))
		apiGroup.POST("/gemini/channels/:id/keys", handlers.AddGeminiApiKey(cfgManager))
		apiGroup.DELETE("/gemini/channels/:id/keys/index/:keyIndex", handlers.DeleteGeminiApiKeyByIndex(cfgManager))
		apiGroup.POST("/gemini/channels/reorder", handlers.ReorderGeminiChannels(cfgManager))
		apiGroup.PATCH("/gemini/channels/:id/status", handlers.SetGeminiChannelStatus(cfgManager))
		apiGroup.GET("/gemini/channels/metrics", handlers.GetGeminiChannelMetrics(channelScheduler.GetGeminiMetricsManager(), cfgManager))
		apiGroup.GET("/gemini/channels/:id/models", handlers.FetchGeminiUpstreamModels(cfgManager))
		apiGroup.PUT("/gemini/loadbalance", handlers.UpdateGeminiLoadBalance(cfgManager))

		// Chat (OpenAI Chat Completions) 渠道管理
		apiGroup.GET("/chat/channels", handlers.GetChatUpstreams(cfgManager))
		apiGroup.POST("/chat/channels", handlers.AddChatUpstream(cfgManager))
		apiGroup.PUT("/chat/channels/:id", handlers.UpdateChatUpstream(cfgManager, channelScheduler))
		apiGroup.DELETE("/chat/channels/:id", handlers.DeleteChatUpstream(cfgManager, channelRateLimiter))
		apiGroup.POST("/chat/channels/:id/keys", handlers.AddChatApiKey(cfgManager))
		apiGroup.DELETE("/chat/channels/:id/keys/index/:keyIndex", handlers.DeleteChatApiKeyByIndex(cfgManager))
		apiGroup.POST("/chat/channels/reorder", handlers.ReorderChatChannels(cfgManager))
		apiGroup.PATCH("/chat/channels/:id/status", handlers.SetChatChannelStatus(cfgManager))
		apiGroup.GET("/chat/channels/metrics", handlers.GetChatChannelMetrics(cfgManager, channelScheduler))
		apiGroup.PUT("/chat/loadbalance", handlers.SetChatLoadBalance(cfgManager))

		// 负载均衡
		apiGroup.PUT("/loadbalance", handlers.UpdateLoadBalance(cfgManager))

		// Ping测试
		apiGroup.GET("/ping/:id", handlers.PingChannel(cfgManager))
		apiGroup.GET("/ping", handlers.PingAllChannels(cfgManager))

		// 请求日志 API
		if reqLogManager != nil {
			reqLogHandler := handlers.NewRequestLogHandler(reqLogManager)
			apiGroup.GET("/logs", reqLogHandler.GetLogs)
			apiGroup.POST("/logs/hooks/anthropic", reqLogHandler.IngestAnthropicHookLog)
			apiGroup.GET("/logs/stream", reqLogHandler.StreamLogs) // SSE real-time updates
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
			// Gemini 渠道配额
			apiGroup.GET("/gemini/channels/usage", usageQuotaHandler.GetAllGeminiChannelUsageQuotas)
			apiGroup.GET("/gemini/channels/:id/usage", usageQuotaHandler.GetGeminiChannelUsageQuota)
			apiGroup.POST("/gemini/channels/:id/usage/reset", usageQuotaHandler.ResetGeminiChannelUsageQuota)
		}

		// 定价配置 API
		apiGroup.GET("/pricing", handlers.GetPricing())
		apiGroup.PUT("/pricing", handlers.UpdatePricing())
		apiGroup.PUT("/pricing/models/:model", handlers.AddModelPricing())
		apiGroup.DELETE("/pricing/models/:model", handlers.DeleteModelPricing())
		apiGroup.POST("/pricing/reset", handlers.ResetPricingToDefault())

		// 模型别名配置 API
		apiGroup.GET("/model-aliases", handlers.GetAliases())
		apiGroup.PUT("/model-aliases", handlers.UpdateAliases())
		apiGroup.POST("/model-aliases/reset", handlers.ResetAliasesToDefault())

		// 速率限制配置 API
		apiGroup.GET("/ratelimit", handlers.GetRateLimitConfig())
		apiGroup.PUT("/ratelimit", handlers.UpdateRateLimitConfig())
		apiGroup.POST("/ratelimit/reset", handlers.ResetRateLimitConfig())

		// 调试日志配置 API
		apiGroup.GET("/config/debug-log", handlers.GetDebugLogConfig(cfgManager))
		apiGroup.PUT("/config/debug-log", handlers.UpdateDebugLogConfig(cfgManager))

		// User-Agent 配置 API
		apiGroup.GET("/config/user-agent", handlers.GetUserAgentConfig(cfgManager))
		apiGroup.PUT("/config/user-agent", handlers.UpdateUserAgentConfig(cfgManager))

		// 故障转移配置 API
		apiGroup.GET("/config/failover", handlers.GetFailoverConfig(cfgManager))
		apiGroup.PUT("/config/failover", handlers.UpdateFailoverConfig(cfgManager))
		apiGroup.POST("/config/failover/reset", handlers.ResetFailoverConfig(cfgManager))

		// 备份/恢复 API
		apiGroup.POST("/config/backup", handlers.CreateBackup(cfgManager))
		apiGroup.GET("/config/backups", handlers.ListBackups())
		apiGroup.POST("/config/restore/:filename", handlers.RestoreBackup(cfgManager))
		apiGroup.DELETE("/config/backups/:filename", handlers.DeleteBackup())

		// Forward Proxy 配置 API
		apiGroup.GET("/forward-proxy/config", handlers.GetForwardProxyConfig(fpServer))
		apiGroup.PUT("/forward-proxy/config", handlers.UpdateForwardProxyConfig(fpServer))
		apiGroup.GET("/forward-proxy/ca-cert", handlers.DownloadForwardProxyCACert(fpServer))
	}

	// 代理端点 - 统一入口（带 API 速率限制）
	v1Group := r.Group("/v1")
	// 先认证再限流：支持按 API Key 应用自定义 RPM
	v1Group.Use(middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager))
	v1Group.Use(middleware.APIRateLimitMiddleware(apiRateLimiter))
	{
		v1Group.GET("/models", handlers.GetModels())
		// Gemini-compatible base path (for clients expecting /v1/models/{model}:{action})
		// Examples:
		//   POST /v1/models/gemini-2.0-flash:generateContent
		//   POST /v1/models/gemini-2.0-flash:streamGenerateContent?alt=sse
		v1Group.POST("/models/*action", handlers.GeminiHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))
		v1Group.POST("/messages", handlers.ProxyHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))
		v1Group.POST("/responses", handlers.ResponsesHandlerWithAPIKey(envCfg, cfgManager, sessionManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))
		v1Group.POST("/chat/completions", handlers.ChatCompletionsHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))

		// Gemini incoming endpoint (passthrough mode)
		// Route: POST /v1/gemini/models/{model}:{action}
		// Examples:
		//   POST /v1/gemini/models/gemini-2.0-flash:generateContent
		//   POST /v1/gemini/models/gemini-2.0-flash:streamGenerateContent?alt=sse
		geminiGroup := v1Group.Group("/gemini")
		{
			geminiGroup.POST("/models/*action", handlers.GeminiHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))
		}
	}

	// Gemini-compatible base path (for clients expecting /v1beta/models/{model}:{action})
	v1betaGroup := r.Group("/v1beta")
	v1betaGroup.Use(middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager))
	v1betaGroup.Use(middleware.APIRateLimitMiddleware(apiRateLimiter))
	{
		v1betaGroup.POST("/models/*action", handlers.GeminiHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))
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
	if fpServer != nil {
		fmt.Printf("🔀 Forward Proxy: http://localhost:%d (MITM intercept enabled)\n", envCfg.ForwardProxyPort)
	}
	// 检查是否使用默认密码，给予提示
	if envCfg.ProxyAccessKey == "your-proxy-access-key" {
		fmt.Printf("🔑 访问密钥: your-proxy-access-key (默认值，建议通过 .env 文件修改)\n")
	}
	fmt.Printf("\n")

	if err := r.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// fpConfigAdapter adapts config.ConfigManager to forwardproxy.ConfigProvider.
type fpConfigAdapter struct {
	cm *config.ConfigManager
}

func (a *fpConfigAdapter) IsDebugLogEnabled() bool {
	return a.cm.GetDebugLogConfig().Enabled
}

func (a *fpConfigAdapter) GetDebugLogMaxBodySize() int {
	cfg := a.cm.GetDebugLogConfig()
	return cfg.GetMaxBodySize()
}

func seedRecentCallMetricsFromLogs(reqLogManager *requestlog.Manager, channelScheduler *scheduler.ChannelScheduler, cfgManager *config.ConfigManager, onlyFillEmpty bool) {
	if reqLogManager == nil || channelScheduler == nil {
		return
	}

	recentCalls, err := reqLogManager.GetRecentChannelCalls(20)
	if err != nil {
		log.Printf("⚠️ 恢复最近 API 调用槽位失败: %v", err)
		return
	}
	if len(recentCalls) == 0 {
		return
	}

	messagesSeed := make(map[int][]metrics.RecentCall)
	responsesSeed := make(map[int][]metrics.RecentCall)
	geminiSeed := make(map[int][]metrics.RecentCall)
	chatSeed := make(map[int][]metrics.RecentCall)
	uidRemapped := 0
	nameRemapped := 0

	var cfg config.Config
	var messagesUpstreams []config.UpstreamConfig
	var responsesUpstreams []config.UpstreamConfig
	var geminiUpstreams []config.UpstreamConfig
	var chatUpstreams []config.UpstreamConfig
	var messagesIDToIndex map[string]int
	var responsesIDToIndex map[string]int
	var geminiIDToIndex map[string]int
	var chatIDToIndex map[string]int
	var messagesNameToIndex map[string]int
	var responsesNameToIndex map[string]int
	var geminiNameToIndex map[string]int
	var chatNameToIndex map[string]int
	messagesCount := 0
	responsesCount := 0
	geminiCount := 0
	chatCount := 0
	if cfgManager != nil {
		cfg = cfgManager.GetConfig()
		messagesUpstreams = cfg.Upstream
		responsesUpstreams = cfg.ResponsesUpstream
		geminiUpstreams = cfg.GeminiUpstream
		chatUpstreams = cfg.ChatUpstream
		messagesIDToIndex = buildChannelIDIndex(messagesUpstreams)
		responsesIDToIndex = buildChannelIDIndex(responsesUpstreams)
		geminiIDToIndex = buildChannelIDIndex(geminiUpstreams)
		chatIDToIndex = buildChannelIDIndex(chatUpstreams)
		messagesNameToIndex = buildUniqueChannelNameIndex(messagesUpstreams)
		responsesNameToIndex = buildUniqueChannelNameIndex(responsesUpstreams)
		geminiNameToIndex = buildUniqueChannelNameIndex(geminiUpstreams)
		chatNameToIndex = buildUniqueChannelNameIndex(chatUpstreams)
		messagesCount = len(messagesUpstreams)
		responsesCount = len(responsesUpstreams)
		geminiCount = len(geminiUpstreams)
		chatCount = len(chatUpstreams)
	}

	for _, call := range recentCalls {
		targetIndex := -1
		mappedBy := ""
		switch call.Endpoint {
		case "/v1/messages":
			targetIndex, mappedBy = resolveSeedTargetIndex(call.ChannelUID, call.ChannelID, call.ChannelName, messagesIDToIndex, messagesNameToIndex, messagesCount)
		case "/v1/responses":
			targetIndex, mappedBy = resolveSeedTargetIndex(call.ChannelUID, call.ChannelID, call.ChannelName, responsesIDToIndex, responsesNameToIndex, responsesCount)
		case "/v1/gemini":
			targetIndex, mappedBy = resolveSeedTargetIndex(call.ChannelUID, call.ChannelID, call.ChannelName, geminiIDToIndex, geminiNameToIndex, geminiCount)
		case "/v1/chat/completions":
			targetIndex, mappedBy = resolveSeedTargetIndex(call.ChannelUID, call.ChannelID, call.ChannelName, chatIDToIndex, chatNameToIndex, chatCount)
		}
		if targetIndex < 0 {
			continue
		}
		if targetIndex != call.ChannelID {
			if mappedBy == "uid" {
				uidRemapped++
			} else if mappedBy == "name" {
				nameRemapped++
			}
		}

		restored := metrics.RecentCall{
			Success:     call.Success,
			StatusCode:  call.HTTPStatus,
			Timestamp:   call.Timestamp,
			Model:       call.Model,
			ChannelName: call.ChannelName,
		}
		if mappedBy == "uid" || mappedBy == "name" {
			switch call.Endpoint {
			case "/v1/messages":
				if targetIndex >= 0 && targetIndex < len(messagesUpstreams) {
					restored.ChannelName = strings.TrimSpace(messagesUpstreams[targetIndex].Name)
				}
			case "/v1/responses":
				if targetIndex >= 0 && targetIndex < len(responsesUpstreams) {
					restored.ChannelName = strings.TrimSpace(responsesUpstreams[targetIndex].Name)
				}
			case "/v1/gemini":
				if targetIndex >= 0 && targetIndex < len(geminiUpstreams) {
					restored.ChannelName = strings.TrimSpace(geminiUpstreams[targetIndex].Name)
				}
			case "/v1/chat/completions":
				if targetIndex >= 0 && targetIndex < len(chatUpstreams) {
					restored.ChannelName = strings.TrimSpace(chatUpstreams[targetIndex].Name)
				}
			}
		}
		switch call.Endpoint {
		case "/v1/messages":
			messagesSeed[targetIndex] = append(messagesSeed[targetIndex], restored)
		case "/v1/responses":
			responsesSeed[targetIndex] = append(responsesSeed[targetIndex], restored)
		case "/v1/gemini":
			geminiSeed[targetIndex] = append(geminiSeed[targetIndex], restored)
		case "/v1/chat/completions":
			chatSeed[targetIndex] = append(chatSeed[targetIndex], restored)
		}
	}

	seedMetrics := func(manager *metrics.MetricsManager, upstreams []config.UpstreamConfig, data map[int][]metrics.RecentCall) int {
		seeded := 0
		for channelIndex, calls := range data {
			if onlyFillEmpty {
				existing := manager.GetMetrics(channelIndex)
				if existing != nil && len(existing.RecentCalls) > 0 {
					continue
				}
			}
			channelUID := ""
			channelName := ""
			if channelIndex >= 0 && channelIndex < len(upstreams) {
				channelUID = strings.TrimSpace(upstreams[channelIndex].ID)
				channelName = strings.TrimSpace(upstreams[channelIndex].Name)
			}
			manager.SeedRecentCallsByIdentity(channelIndex, channelUID, channelName, calls)
			seeded++
		}
		return seeded
	}

	messagesSeeded := seedMetrics(channelScheduler.GetMessagesMetricsManager(), messagesUpstreams, messagesSeed)
	responsesSeeded := seedMetrics(channelScheduler.GetResponsesMetricsManager(), responsesUpstreams, responsesSeed)
	geminiSeeded := seedMetrics(channelScheduler.GetGeminiMetricsManager(), geminiUpstreams, geminiSeed)
	chatSeeded := seedMetrics(channelScheduler.GetChatMetricsManager(), chatUpstreams, chatSeed)

	if messagesSeeded > 0 || responsesSeeded > 0 || geminiSeeded > 0 || chatSeeded > 0 {
		log.Printf("✅ 已恢复最近 API 调用槽位 (messages: %d, responses: %d, gemini: %d, chat: %d)",
			messagesSeeded, responsesSeeded, geminiSeeded, chatSeeded)
		if uidRemapped > 0 || nameRemapped > 0 {
			log.Printf("🔁 最近 API 调用槽位重映射完成 (by channelUid: %d, by channelName: %d)", uidRemapped, nameRemapped)
		}
	}
}

func buildChannelIDIndex(upstreams []config.UpstreamConfig) map[string]int {
	idToIndex := make(map[string]int, len(upstreams))
	for i, up := range upstreams {
		channelID := strings.TrimSpace(up.ID)
		if channelID == "" {
			continue
		}
		idToIndex[channelID] = i
	}
	return idToIndex
}

func buildUniqueChannelNameIndex(upstreams []config.UpstreamConfig) map[string]int {
	nameCount := make(map[string]int, len(upstreams))
	for _, up := range upstreams {
		name := strings.ToLower(strings.TrimSpace(up.Name))
		if name == "" {
			continue
		}
		nameCount[name]++
	}

	nameToIndex := make(map[string]int, len(upstreams))
	for i, up := range upstreams {
		name := strings.ToLower(strings.TrimSpace(up.Name))
		if name == "" || nameCount[name] != 1 {
			continue
		}
		nameToIndex[name] = i
	}
	return nameToIndex
}

// resolveSeedTargetIndex resolves historical channel index to current config index.
// It prefers stable channel UID, then unique channel name.
// Index fallback is intentionally disabled to avoid cross-channel pollution after reorder/rename.
// Returns (index, mappedBy) where mappedBy is one of: "uid", "name", "".
func resolveSeedTargetIndex(channelUID string, channelID int, channelName string, idToIndex map[string]int, nameToIndex map[string]int, channelCount int) (int, string) {
	uid := strings.TrimSpace(channelUID)
	if uid != "" {
		if idx, ok := idToIndex[uid]; ok {
			return idx, "uid"
		}
	}

	name := strings.ToLower(strings.TrimSpace(channelName))
	if name != "" {
		if idx, ok := nameToIndex[name]; ok {
			return idx, "name"
		}
	}
	return -1, ""
}
