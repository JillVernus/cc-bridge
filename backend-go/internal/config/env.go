package config

import (
	"os"
	"strconv"
	"strings"
)

// EnvConfig 环境变量配置
type EnvConfig struct {
	Port                 int
	Env                  string
	EnableWebUI          bool
	ProxyAccessKey       string
	LoadBalanceStrategy  string
	LogLevel             string
	EnableRequestLogs    bool
	EnableResponseLogs   bool
	RequestTimeout       int
	MaxConcurrentReqs    int
	MaxRequestBodyMB     int
	EnableCORS           bool
	CORSOrigin           string
	EnableRateLimit      bool
	RateLimitWindow      int
	RateLimitMaxRequests int
	HealthCheckEnabled   bool
	HealthCheckPath      string
	// 日志文件相关配置
	LogDir        string
	LogFile       string
	LogMaxSize    int  // 单个日志文件最大大小 (MB)
	LogMaxBackups int  // 保留的旧日志文件最大数量
	LogMaxAge     int  // 保留的旧日志文件最大天数
	LogCompress   bool // 是否压缩旧日志文件
	LogToConsole  bool // 是否同时输出到控制台
	// 安全相关配置
	TrustedProxies []string // 可信代理 IP/CIDR 列表（用于正确获取客户端 IP）
	// 存储后端配置
	StorageBackend     string // "json" (default) or "database"
	DatabaseType       string // "sqlite" or "postgresql"
	DatabaseURL        string // SQLite path or PostgreSQL connection string
	ConfigPollInterval int    // Config polling interval in seconds (for database backend)
	SkipMigration      bool   // Skip data migration checks on startup (for faster restarts)
	// Forward proxy configuration
	ForwardProxyEnabled          bool     // FORWARD_PROXY_ENABLED, default: true (set "false" to disable)
	ForwardProxyPort             int      // FORWARD_PROXY_PORT, default: 3001
	ForwardProxyBindAddress      string   // FORWARD_PROXY_BIND, default: "0.0.0.0"
	ForwardProxyInterceptDomains []string // FORWARD_PROXY_INTERCEPT_DOMAINS, default: "api.anthropic.com"
	ForwardProxyCertDir          string   // FORWARD_PROXY_CERT_DIR, default: ".config/certs"
}

// NewEnvConfig 创建环境配置
func NewEnvConfig() *EnvConfig {
	// 支持 ENV 和 NODE_ENV（向后兼容）
	env := getEnv("ENV", "")
	if env == "" {
		env = getEnv("NODE_ENV", "development")
	}

	// 解析可信代理列表
	trustedProxies := parseTrustedProxies(getEnv("TRUSTED_PROXIES", ""))

	return &EnvConfig{
		Port:                 getEnvAsInt("PORT", 3000),
		Env:                  env,
		EnableWebUI:          getEnv("ENABLE_WEB_UI", "true") != "false",
		ProxyAccessKey:       getEnv("PROXY_ACCESS_KEY", "your-proxy-access-key"),
		LoadBalanceStrategy:  getEnv("LOAD_BALANCE_STRATEGY", "failover"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		EnableRequestLogs:    getEnv("ENABLE_REQUEST_LOGS", "true") != "false",
		EnableResponseLogs:   getEnv("ENABLE_RESPONSE_LOGS", "true") != "false",
		RequestTimeout:       getEnvAsInt("REQUEST_TIMEOUT", 300000),
		MaxConcurrentReqs:    getEnvAsInt("MAX_CONCURRENT_REQUESTS", 100),
		MaxRequestBodyMB:     getEnvAsInt("MAX_REQUEST_BODY_MB", 20),
		EnableCORS:           getEnv("ENABLE_CORS", "true") != "false",
		CORSOrigin:           getEnv("CORS_ORIGIN", ""),
		EnableRateLimit:      getEnv("ENABLE_RATE_LIMIT", "true") != "false",
		RateLimitWindow:      getEnvAsInt("RATE_LIMIT_WINDOW", 60000),
		RateLimitMaxRequests: getEnvAsInt("RATE_LIMIT_MAX_REQUESTS", 100),
		HealthCheckEnabled:   getEnv("HEALTH_CHECK_ENABLED", "true") != "false",
		HealthCheckPath:      getEnv("HEALTH_CHECK_PATH", "/health"),
		// 日志文件配置
		LogDir:        getEnv("LOG_DIR", "logs"),
		LogFile:       getEnv("LOG_FILE", "app.log"),
		LogMaxSize:    getEnvAsInt("LOG_MAX_SIZE", 100),   // 默认 100MB
		LogMaxBackups: getEnvAsInt("LOG_MAX_BACKUPS", 10), // 默认保留 10 个
		LogMaxAge:     getEnvAsInt("LOG_MAX_AGE", 30),     // 默认保留 30 天
		LogCompress:   getEnv("LOG_COMPRESS", "true") != "false",
		LogToConsole:  getEnv("LOG_TO_CONSOLE", "true") != "false",
		// 安全配置
		TrustedProxies: trustedProxies,
		// 存储后端配置
		StorageBackend:     getEnv("STORAGE_BACKEND", "json"),           // "json" or "database"
		DatabaseType:       getEnv("DATABASE_TYPE", "sqlite"),           // "sqlite" or "postgresql"
		DatabaseURL:        getEnv("DATABASE_URL", ""),                  // Connection string
		ConfigPollInterval: getEnvAsInt("CONFIG_POLL_INTERVAL", 5),      // seconds
		SkipMigration:      getEnv("SKIP_MIGRATION", "false") == "true", // Skip migration checks
		// Forward proxy configuration
		ForwardProxyEnabled:          getEnv("FORWARD_PROXY_ENABLED", "true") != "false",
		ForwardProxyPort:             getEnvAsInt("FORWARD_PROXY_PORT", 3001),
		ForwardProxyBindAddress:      getEnv("FORWARD_PROXY_BIND", "0.0.0.0"),
		ForwardProxyInterceptDomains: parseCommaSeparated(getEnv("FORWARD_PROXY_INTERCEPT_DOMAINS", "api.anthropic.com")),
		ForwardProxyCertDir:          getEnv("FORWARD_PROXY_CERT_DIR", ".config/certs"),
	}
}

// IsDevelopment 是否为开发环境
func (c *EnvConfig) IsDevelopment() bool {
	return c.Env == "development"
}

// IsProduction 是否为生产环境
func (c *EnvConfig) IsProduction() bool {
	return c.Env == "production"
}

// UseDatabaseStorage returns true if database storage backend is enabled
func (c *EnvConfig) UseDatabaseStorage() bool {
	return c.StorageBackend == "database"
}

// UseJSONStorage returns true if JSON file storage backend is enabled (default)
func (c *EnvConfig) UseJSONStorage() bool {
	return c.StorageBackend != "database"
}

// ShouldLog 是否应该记录日志
func (c *EnvConfig) ShouldLog(level string) bool {
	levels := map[string]int{
		"error": 0,
		"warn":  1,
		"info":  2,
		"debug": 3,
	}

	currentLevel, ok := levels[c.LogLevel]
	if !ok {
		currentLevel = 2 // 默认 info
	}

	requestLevel, ok := levels[level]
	if !ok {
		return false
	}

	return requestLevel <= currentLevel
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量并转换为整数
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// parseTrustedProxies 解析可信代理列表
// 支持逗号分隔的 IP 地址或 CIDR 格式
// 例如: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16" 或 "127.0.0.1,::1"
func parseTrustedProxies(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// parseCommaSeparated parses a comma-separated string into a slice of trimmed non-empty strings.
func parseCommaSeparated(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
