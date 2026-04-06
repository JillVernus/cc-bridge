package config

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/fsnotify/fsnotify"
)

// TokenPriceMultipliers 单个模型的价格乘数配置
type TokenPriceMultipliers struct {
	InputMultiplier         float64 `json:"inputMultiplier,omitempty"`         // 输入 token 价格乘数，默认 1.0
	OutputMultiplier        float64 `json:"outputMultiplier,omitempty"`        // 输出 token 价格乘数，默认 1.0
	CacheCreationMultiplier float64 `json:"cacheCreationMultiplier,omitempty"` // 缓存创建价格乘数，默认 1.0
	CacheReadMultiplier     float64 `json:"cacheReadMultiplier,omitempty"`     // 缓存读取价格乘数，默认 1.0
}

// OAuthTokens stores OAuth authentication data for OpenAI OAuth channels
// Matches the structure of Codex CLI's auth.json tokens field
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`           // OAuth2 access token for API calls
	AccountID    string `json:"account_id"`             // ChatGPT account ID (required for API headers)
	IDToken      string `json:"id_token,omitempty"`     // JWT ID token containing user info
	RefreshToken string `json:"refresh_token"`          // OAuth2 refresh token for token renewal
	LastRefresh  string `json:"last_refresh,omitempty"` // Timestamp of last token refresh
}

const (
	CodexServiceTierOverrideOff           = "off"
	CodexServiceTierOverrideForcePriority = "force_priority"
	CodexServiceTierOverrideForceDefault  = "force_default"
)

// NormalizeCodexServiceTierOverride returns the canonical override mode for the
// given channel pool and service type.
func NormalizeCodexServiceTierOverride(channelType string, serviceType string, raw string) string {
	if !isEligibleCodexServiceTierOverrideChannel(channelType, serviceType) {
		return CodexServiceTierOverrideOff
	}

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", CodexServiceTierOverrideOff:
		return CodexServiceTierOverrideOff
	case CodexServiceTierOverrideForcePriority:
		return CodexServiceTierOverrideForcePriority
	case CodexServiceTierOverrideForceDefault:
		return CodexServiceTierOverrideForceDefault
	default:
		return CodexServiceTierOverrideOff
	}
}

func isEligibleCodexServiceTierOverrideChannel(channelType string, serviceType string) bool {
	if strings.ToLower(strings.TrimSpace(channelType)) != "responses" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(serviceType)) {
	case "responses", "openai-oauth":
		return true
	default:
		return false
	}
}

func normalizeUpstreamCodexServiceTierOverride(channelType string, upstream *UpstreamConfig) {
	if upstream == nil {
		return
	}
	upstream.CodexServiceTierOverride = NormalizeCodexServiceTierOverride(
		channelType,
		upstream.ServiceType,
		upstream.CodexServiceTierOverride,
	)
}

func normalizeConfigCodexServiceTierOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	for i := range cfg.Upstream {
		normalizeUpstreamCodexServiceTierOverride("messages", &cfg.Upstream[i])
	}
	for i := range cfg.ResponsesUpstream {
		normalizeUpstreamCodexServiceTierOverride("responses", &cfg.ResponsesUpstream[i])
	}
	for i := range cfg.GeminiUpstream {
		normalizeUpstreamCodexServiceTierOverride("gemini", &cfg.GeminiUpstream[i])
	}
	for i := range cfg.ChatUpstream {
		normalizeUpstreamCodexServiceTierOverride("chat", &cfg.ChatUpstream[i])
	}
}

// GetEffectiveMultiplier 获取有效乘数（0 或未设置时返回 1.0）
func (t *TokenPriceMultipliers) GetEffectiveMultiplier(tokenType string) float64 {
	var m float64
	switch tokenType {
	case "input":
		m = t.InputMultiplier
	case "output":
		m = t.OutputMultiplier
	case "cacheCreation":
		m = t.CacheCreationMultiplier
	case "cacheRead":
		m = t.CacheReadMultiplier
	}
	if m == 0 {
		return 1.0
	}
	return m
}

// ContentFilterRule defines a single content filter matching rule.
// When keyword is matched (case-insensitive substring), response is converted to StatusCode.
type ContentFilterRule struct {
	Keyword    string `json:"keyword"`              // Keyword to match in response text (case-insensitive substring)
	StatusCode int    `json:"statusCode,omitempty"` // HTTP status code to use when matched (default 429)
}

// ContentFilter defines per-channel response content filtering to detect errors
// returned as HTTP 200 with error text in the response body.
type ContentFilter struct {
	Enabled bool `json:"enabled"` // Whether content filtering is active
	// Rules for keyword-to-status mapping. First matched rule wins.
	Rules []ContentFilterRule `json:"rules,omitempty"`
	// Legacy fields (backward compatibility): if Rules is empty, these are used.
	Keywords   []string `json:"keywords,omitempty"`   // Legacy: keyword list
	StatusCode int      `json:"statusCode,omitempty"` // Legacy: shared status code for all keywords (default 429)
}

// CompositeMapping defines a model-to-channel mapping for composite channels
type CompositeTargetRef struct {
	Pool      string `json:"pool,omitempty"` // messages | responses (default: messages)
	ChannelID string `json:"channelId"`      // Target channel ID in the selected pool
}

const (
	CompositeTargetPoolMessages  = "messages"
	CompositeTargetPoolResponses = "responses"
)

type CompositeMapping struct {
	Pattern         string               `json:"pattern"`                   // Model pattern: "haiku", "sonnet", "opus" (mandatory, no wildcard)
	TargetChannelID string               `json:"targetChannelId"`           // Primary target channel ID
	TargetPool      string               `json:"targetPool,omitempty"`      // Primary target pool: messages | responses (default messages)
	FailoverChain   []string             `json:"failoverChain,omitempty"`   // Ordered failover channel IDs (min 1 required)
	FailoverTargets []CompositeTargetRef `json:"failoverTargets,omitempty"` // Canonical failover targets (pool-aware)
	TargetModel     string               `json:"targetModel,omitempty"`     // Optional: override model name sent to target
	// Deprecated: use TargetChannelID instead. Kept for migration only.
	TargetChannel int `json:"targetChannel,omitempty"` // Legacy: target channel index
}

// UpstreamConfig 上游配置
type UpstreamConfig struct {
	Index                     int               `json:"-"`            // Internal index, set at runtime (not persisted to JSON)
	ID                        string            `json:"id,omitempty"` // Unique stable identifier for channel references
	BaseURL                   string            `json:"baseUrl"`
	APIKeys                   []string          `json:"apiKeys"`
	ServiceType               string            `json:"serviceType"` // gemini, openai, openai_chat, openaiold, claude, openai-oauth
	Name                      string            `json:"name,omitempty"`
	Description               string            `json:"description,omitempty"`
	Website                   string            `json:"website,omitempty"`
	InsecureSkipVerify        bool              `json:"insecureSkipVerify,omitempty"`
	ModelMapping              map[string]string `json:"modelMapping,omitempty"`
	ResponseHeaderTimeoutSecs int               `json:"responseHeaderTimeout,omitempty"` // 响应头超时（秒），默认30秒
	// 多渠道调度相关字段
	Priority       int        `json:"priority"`                 // 渠道优先级（数字越小优先级越高，默认按索引）
	Status         string     `json:"status"`                   // 渠道状态：active（正常）, suspended（暂停）, disabled（备用池）
	PromotionUntil *time.Time `json:"promotionUntil,omitempty"` // 促销期截止时间，在此期间内优先使用此渠道（忽略trace亲和）
	// 价格乘数配置：key 为模型名称（支持前缀匹配），"_default" 为默认乘数
	PriceMultipliers map[string]TokenPriceMultipliers `json:"priceMultipliers,omitempty"`
	// OpenAI OAuth configuration (for serviceType="openai-oauth")
	OAuthTokens *OAuthTokens `json:"oauthTokens,omitempty"`
	// Codex Responses service tier override for eligible channels.
	// Canonical values: off | force_priority | force_default.
	CodexServiceTierOverride string `json:"codexServiceTierOverride,omitempty"`
	// 配额设置（可选）
	QuotaType          string     `json:"quotaType,omitempty"`          // "requests" | "credit" | "" (无配额)
	QuotaLimit         float64    `json:"quotaLimit,omitempty"`         // 最大配额值（请求数或金额）
	QuotaResetAt       *time.Time `json:"quotaResetAt,omitempty"`       // 首次/下次重置时间
	QuotaResetInterval int        `json:"quotaResetInterval,omitempty"` // 重置间隔值
	QuotaResetUnit     string     `json:"quotaResetUnit,omitempty"`     // "hours" | "days" | "weeks" | "months"
	QuotaModels        []string   `json:"quotaModels,omitempty"`        // 配额计数模型过滤（子字符串匹配），空数组=全部模型
	QuotaResetMode     string     `json:"quotaResetMode,omitempty"`     // "fixed" | "rolling", 默认 "fixed"
	// Per-channel rate limiting (upstream protection)
	RateLimitRpm int  `json:"rateLimitRpm,omitempty"` // Requests per minute (0 = disabled)
	QueueEnabled bool `json:"queueEnabled,omitempty"` // Enable queue mode instead of reject
	QueueTimeout int  `json:"queueTimeout,omitempty"` // Max seconds to wait in queue (default 60)
	// Composite channel mappings (only for serviceType="composite")
	CompositeMappings []CompositeMapping `json:"compositeMappings,omitempty"`
	// Per-channel API key load balancing strategy (overrides global setting)
	// Valid values: "" (inherit global), "round-robin", "random", "failover"
	KeyLoadBalance string `json:"keyLoadBalance,omitempty"`
	// Content filter: detect errors returned as HTTP 200 with error text in body
	ContentFilter *ContentFilter `json:"contentFilter,omitempty"`
}

// GetResponseHeaderTimeout 获取响应头超时时间（秒），默认120秒
func (u *UpstreamConfig) GetResponseHeaderTimeout() int {
	if u.ResponseHeaderTimeoutSecs > 0 {
		return u.ResponseHeaderTimeoutSecs
	}
	return 120 // 默认120秒
}

// GetQueueTimeout 获取队列超时时间（秒），默认60秒
func (u *UpstreamConfig) GetQueueTimeout() int {
	if u.QueueTimeout > 0 {
		return u.QueueTimeout
	}
	return 60 // 默认60秒
}

// GetPriceMultipliers 获取指定模型的价格乘数（累积模式：特定模型乘数 × _default 乘数）
func (u *UpstreamConfig) GetPriceMultipliers(model string) *TokenPriceMultipliers {
	if u.PriceMultipliers == nil {
		return nil
	}

	// 获取 _default 乘数（如果存在）
	defaultMult, hasDefault := u.PriceMultipliers["_default"]

	// 查找特定模型乘数（精确匹配优先，然后前缀匹配）
	var modelMult *TokenPriceMultipliers

	// 精确匹配
	if m, ok := u.PriceMultipliers[model]; ok {
		modelMult = &m
	} else {
		// 前缀匹配
		for pattern, multipliers := range u.PriceMultipliers {
			if pattern != "_default" && strings.HasPrefix(model, pattern) {
				m := multipliers
				modelMult = &m
				break
			}
		}
	}

	// 如果没有任何匹配，返回 _default（如果存在）
	if modelMult == nil {
		if hasDefault {
			return &defaultMult
		}
		return nil
	}

	// 如果没有 _default，只返回模型乘数
	if !hasDefault {
		return modelMult
	}

	// 累积模式：特定模型乘数 × _default 乘数
	combined := TokenPriceMultipliers{
		InputMultiplier:         multiplyEffective(modelMult.InputMultiplier, defaultMult.InputMultiplier),
		OutputMultiplier:        multiplyEffective(modelMult.OutputMultiplier, defaultMult.OutputMultiplier),
		CacheCreationMultiplier: multiplyEffective(modelMult.CacheCreationMultiplier, defaultMult.CacheCreationMultiplier),
		CacheReadMultiplier:     multiplyEffective(modelMult.CacheReadMultiplier, defaultMult.CacheReadMultiplier),
	}
	return &combined
}

// multiplyEffective 计算两个乘数的累积值（0 视为 1.0）
func multiplyEffective(a, b float64) float64 {
	if a == 0 {
		a = 1.0
	}
	if b == 0 {
		b = 1.0
	}
	return a * b
}

// generateChannelID generates a unique channel ID (8 hex characters)
func generateChannelID() string {
	b := make([]byte, 4)
	cryptorand.Read(b)
	return hex.EncodeToString(b)
}

// ShouldCountQuota checks if the given model should be counted for quota
// Returns true if QuotaModels is empty (count all) or if model matches any pattern (substring match)
func (u *UpstreamConfig) ShouldCountQuota(model string) bool {
	if len(u.QuotaModels) == 0 {
		return true
	}
	modelLower := strings.ToLower(model)
	for _, pattern := range u.QuotaModels {
		if strings.Contains(modelLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// UpstreamUpdate 用于部分更新 UpstreamConfig
type UpstreamUpdate struct {
	Name                      *string           `json:"name"`
	ServiceType               *string           `json:"serviceType"`
	BaseURL                   *string           `json:"baseUrl"`
	APIKeys                   []string          `json:"apiKeys"`
	Description               *string           `json:"description"`
	Website                   *string           `json:"website"`
	InsecureSkipVerify        *bool             `json:"insecureSkipVerify"`
	ModelMapping              map[string]string `json:"modelMapping"`
	ResponseHeaderTimeoutSecs *int              `json:"responseHeaderTimeout"`
	// 多渠道调度相关字段
	Priority         *int                             `json:"priority"`
	Status           *string                          `json:"status"`
	PromotionUntil   *time.Time                       `json:"promotionUntil"`
	PriceMultipliers map[string]TokenPriceMultipliers `json:"priceMultipliers"`
	// OpenAI OAuth configuration
	OAuthTokens *OAuthTokens `json:"oauthTokens"`
	// Codex Responses service tier override for eligible channels.
	// Canonical values: off | force_priority | force_default.
	CodexServiceTierOverride *string `json:"codexServiceTierOverride"`
	// 配额设置
	QuotaType          *string    `json:"quotaType"`
	QuotaLimit         *float64   `json:"quotaLimit"`
	QuotaResetAt       *time.Time `json:"quotaResetAt"`
	QuotaResetInterval *int       `json:"quotaResetInterval"`
	QuotaResetUnit     *string    `json:"quotaResetUnit"`
	QuotaModels        []string   `json:"quotaModels"`
	QuotaResetMode     *string    `json:"quotaResetMode"`
	// Per-channel rate limiting
	RateLimitRpm *int  `json:"rateLimitRpm"`
	QueueEnabled *bool `json:"queueEnabled"`
	QueueTimeout *int  `json:"queueTimeout"`
	// Composite channel mappings
	CompositeMappings []CompositeMapping `json:"compositeMappings"`
	// Per-channel API key load balancing strategy (overrides global setting)
	KeyLoadBalance *string `json:"keyLoadBalance"`
	// Content filter
	ContentFilter *ContentFilter `json:"contentFilter"`
}

// Config 配置结构
// DebugLogConfig 调试日志配置
type DebugLogConfig struct {
	Enabled        bool `json:"enabled"`        // 是否启用调试日志，默认 false
	RetentionHours int  `json:"retentionHours"` // 保留时间（小时），默认 24
	MaxBodySize    int  `json:"maxBodySize"`    // 最大请求体大小（字节），默认 1MB
}

// GetRetentionHours 获取保留时间，默认 24 小时
func (d *DebugLogConfig) GetRetentionHours() int {
	if d.RetentionHours <= 0 {
		return 24
	}
	return d.RetentionHours
}

// GetMaxBodySize 获取最大请求体大小，默认 1MB
func (d *DebugLogConfig) GetMaxBodySize() int {
	if d.MaxBodySize <= 0 {
		return 1048576 // 1MB
	}
	return d.MaxBodySize
}

// FailoverAction 故障转移动作类型 (新版本)
const (
	ActionRetry       = "retry"    // 等待后重试（可配置次数）
	ActionFailover    = "failover" // 故障转移到下一个密钥
	ActionSuspend     = "suspend"  // 暂停渠道直到配额重置
	ActionReturnError = "none"     // 返回错误给客户端（不重试/不故障转移）
)

// FailoverAction 故障转移动作类型 (旧版本 - 用于迁移)
const (
	ActionFailoverImmediate = "failover_immediate" // 立即故障转移到下一个密钥
	ActionFailoverThreshold = "failover_threshold" // 达到阈值后故障转移
	ActionRetryWait         = "retry_wait"         // 等待后使用同一密钥重试
	ActionSuspendChannel    = "suspend_channel"    // 暂停渠道直到配额重置
)

// ActionStep 动作链中的单个步骤
type ActionStep struct {
	Action      string `json:"action"`                // 动作类型: "retry", "failover", "suspend"
	WaitSeconds int    `json:"waitSeconds,omitempty"` // 等待秒数 (0 = 使用响应中的 reset_seconds，仅用于 retry)
	MaxAttempts int    `json:"maxAttempts,omitempty"` // 最大重试次数 (仅用于 retry，99 = 无限重试)
}

// FailoverRule 单条故障转移规则
type FailoverRule struct {
	ErrorCodes  string       `json:"errorCodes"`            // 错误码模式: "401,403" 或 "429:QUOTA_EXHAUSTED" 或 "others"
	ActionChain []ActionStep `json:"actionChain,omitempty"` // 动作链: 按顺序执行的动作列表

	// 以下字段用于向后兼容迁移，新规则不应使用
	Action      string `json:"action,omitempty"`      // [已废弃] 动作类型
	Threshold   int    `json:"threshold,omitempty"`   // [已废弃] 用于 failover_threshold
	WaitSeconds int    `json:"waitSeconds,omitempty"` // [已废弃] 用于 retry_wait
}

// FailoverConfig 故障转移配置
type FailoverConfig struct {
	Enabled bool           `json:"enabled"` // 启用自定义故障转移规则（false=使用传统行为）
	Rules   []FailoverRule `json:"rules"`   // 规则列表

	// 429 智能处理配置 (仅适用于 Claude Messages API)
	GenericResourceWaitSeconds  int `json:"genericResourceWaitSeconds,omitempty"`  // Type 3: 通用资源耗尽等待时间（默认 20 秒）
	ModelCooldownExtraSeconds   int `json:"modelCooldownExtraSeconds,omitempty"`   // Type 2: 模型冷却额外等待时间（默认 1 秒）
	ModelCooldownMaxWaitSeconds int `json:"modelCooldownMaxWaitSeconds,omitempty"` // Type 2: 模型冷却最大等待时间（默认 60 秒）
}

// GetDefaultFailoverRules 获取默认故障转移规则（适用于所有渠道）
// 规则按优先级排序：更具体的模式（如 429:QUOTA_EXHAUSTED）优先于通用模式（如 429）
func GetDefaultFailoverRules() []FailoverRule {
	return []FailoverRule{
		// 配额耗尽 - 暂停渠道直到重置
		{ErrorCodes: "429:QUOTA_EXHAUSTED", ActionChain: []ActionStep{{Action: ActionSuspend}}},
		{ErrorCodes: "403:CREDIT_EXHAUSTED", ActionChain: []ActionStep{{Action: ActionSuspend}}},
		// 模型冷却 - 等待响应中的 reset_seconds 后重试（无限次），最终故障转移
		{ErrorCodes: "429:model_cooldown", ActionChain: []ActionStep{
			{Action: ActionRetry, WaitSeconds: 0, MaxAttempts: 99},
			{Action: ActionFailover},
		}},
		// 通用资源耗尽 - 等待 20 秒后重试（无限次），最终故障转移
		{ErrorCodes: "429:RESOURCE_EXHAUSTED", ActionChain: []ActionStep{
			{Action: ActionRetry, WaitSeconds: 20, MaxAttempts: 99},
			{Action: ActionFailover},
		}},
		// 通用 429 - 等待 5 秒后重试 3 次，然后故障转移（统一所有提供商行为）
		{ErrorCodes: "429", ActionChain: []ActionStep{
			{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 3},
			{Action: ActionFailover},
		}},
		// 认证错误 - 立即故障转移
		{ErrorCodes: "401,403", ActionChain: []ActionStep{{Action: ActionFailover}}},
		// 服务器错误 - 等待 5 秒后重试 2 次，然后故障转移
		{ErrorCodes: "500,502,503,504", ActionChain: []ActionStep{
			{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 2},
			{Action: ActionFailover},
		}},
		// 注意：不再包含 "others" 规则 - 未匹配的错误将返回给客户端（ActionNone）
	}
}

// MigrateRule 将旧格式规则迁移到新格式（动作链）
// 如果规则已经是新格式（有 ActionChain），则原样返回
func MigrateRule(rule FailoverRule) FailoverRule {
	// 如果已有 ActionChain，无需迁移
	if len(rule.ActionChain) > 0 {
		return rule
	}

	// 没有旧 Action 字段，返回原样
	if rule.Action == "" {
		return rule
	}

	// 根据旧 Action 类型转换为新的 ActionChain
	switch rule.Action {
	case ActionSuspendChannel:
		rule.ActionChain = []ActionStep{{Action: ActionSuspend}}

	case ActionFailoverImmediate:
		rule.ActionChain = []ActionStep{{Action: ActionFailover}}

	case ActionFailoverThreshold:
		threshold := rule.Threshold
		if threshold <= 0 {
			threshold = 1
		}
		rule.ActionChain = []ActionStep{
			{Action: ActionRetry, WaitSeconds: 0, MaxAttempts: threshold},
			{Action: ActionFailover},
		}

	case ActionRetryWait:
		rule.ActionChain = []ActionStep{
			{Action: ActionRetry, WaitSeconds: rule.WaitSeconds, MaxAttempts: 99},
			{Action: ActionFailover},
		}

	default:
		// Unknown legacy action type, default to immediate failover
		rule.ActionChain = []ActionStep{{Action: ActionFailover}}
	}

	// 清除旧字段
	rule.Action = ""
	rule.Threshold = 0
	rule.WaitSeconds = 0

	return rule
}

// MigrateRules 批量迁移规则列表
func MigrateRules(rules []FailoverRule) ([]FailoverRule, bool) {
	migrated := make([]FailoverRule, len(rules))
	needsMigration := false

	for i, rule := range rules {
		if rule.Action != "" && len(rule.ActionChain) == 0 {
			needsMigration = true
		}
		migrated[i] = MigrateRule(rule)
	}

	return migrated, needsMigration
}

// NormalizeDangerousOthersFailoverRules rewrites dangerous catch-all rules:
// errorCodes="others" with action "failover" will be converted to "none" (return error).
// Returns normalized rules and number of changed rules.
func NormalizeDangerousOthersFailoverRules(rules []FailoverRule) ([]FailoverRule, int) {
	if len(rules) == 0 {
		return rules, 0
	}

	normalized := make([]FailoverRule, len(rules))
	copy(normalized, rules)

	changedRules := 0
	for i := range normalized {
		if strings.ToLower(strings.TrimSpace(normalized[i].ErrorCodes)) != "others" {
			continue
		}

		// Legacy rules may still use Action; migrate first for consistent normalization.
		if len(normalized[i].ActionChain) == 0 && normalized[i].Action != "" {
			normalized[i] = MigrateRule(normalized[i])
		}

		ruleChanged := false
		for j := range normalized[i].ActionChain {
			if normalized[i].ActionChain[j].Action == ActionFailover {
				normalized[i].ActionChain[j].Action = ActionReturnError
				ruleChanged = true
			}
		}
		if ruleChanged {
			changedRules++
		}
	}

	return normalized, changedRules
}

type Config struct {
	Upstream        []UpstreamConfig `json:"upstream"`
	CurrentUpstream int              `json:"currentUpstream,omitempty"` // 已废弃：旧格式兼容用
	LoadBalance     string           `json:"loadBalance"`               // round-robin, random, failover

	// Responses 接口专用配置（独立于 /v1/messages）
	ResponsesUpstream        []UpstreamConfig `json:"responsesUpstream"`
	CurrentResponsesUpstream int              `json:"currentResponsesUpstream,omitempty"` // 已废弃：旧格式兼容用
	ResponsesLoadBalance     string           `json:"responsesLoadBalance"`

	// Gemini 接口专用配置（独立于 /v1/messages，用于 /v1/gemini/models/*）
	GeminiUpstream    []UpstreamConfig `json:"geminiUpstream"`
	GeminiLoadBalance string           `json:"geminiLoadBalance"`

	// Chat 接口专用配置（独立于 /v1/messages，用于 /v1/chat/completions）
	ChatUpstream    []UpstreamConfig `json:"chatUpstream"`
	ChatLoadBalance string           `json:"chatLoadBalance"`

	// 调试日志配置
	DebugLog DebugLogConfig `json:"debugLog,omitempty"`

	// 故障转移阈值配置
	Failover FailoverConfig `json:"failover,omitempty"`

	// User-Agent 回退与自动捕获配置
	UserAgent UserAgentConfig `json:"userAgent,omitempty"`
}

// FailedKey 失败密钥记录
type FailedKey struct {
	Timestamp    time.Time
	FailureCount int
}

// ConfigManager 配置管理器
type ConfigManager struct {
	mu                    sync.RWMutex
	config                Config
	configFile            string
	requestCount          int
	responsesRequestCount int
	watcher               *fsnotify.Watcher
	failedKeysCache       map[string]*FailedKey
	keyRecoveryTime       time.Duration
	maxFailureCount       int
	dbStorage             *DBConfigStorage // Database storage adapter (nil if using JSON-only mode)
	disableFileWatcher    bool             // Disable file watcher when using database polling
}

func (cm *ConfigManager) ensureUniqueChannelNameLocked(name string, channelType string, excludeIndex int) error {
	candidate := strings.ToLower(strings.TrimSpace(name))
	if candidate == "" {
		return fmt.Errorf("channel name is required")
	}

	// Only check within the same channel type — seed restore and migration
	// already scope name lookups by type, so cross-type uniqueness is unnecessary.
	var upstreams []UpstreamConfig
	switch channelType {
	case "messages":
		upstreams = cm.config.Upstream
	case "responses":
		upstreams = cm.config.ResponsesUpstream
	case "gemini":
		upstreams = cm.config.GeminiUpstream
	case "chat":
		upstreams = cm.config.ChatUpstream
	default:
		return fmt.Errorf("unknown channel type: %s", channelType)
	}

	for i := range upstreams {
		if i == excludeIndex {
			continue
		}
		other := strings.ToLower(strings.TrimSpace(upstreams[i].Name))
		if other != "" && other == candidate {
			return fmt.Errorf("duplicate channel name: %s", name)
		}
	}
	return nil
}

const (
	maxBackups      = 10
	keyRecoveryTime = 5 * time.Minute
	maxFailureCount = 3
)

// NewConfigManager 创建配置管理器
func NewConfigManager(configFile string) (*ConfigManager, error) {
	cm := &ConfigManager{
		configFile:      configFile,
		failedKeysCache: make(map[string]*FailedKey),
		keyRecoveryTime: keyRecoveryTime,
		maxFailureCount: maxFailureCount,
	}

	// 加载配置
	if err := cm.loadConfig(); err != nil {
		return nil, err
	}

	// 启动文件监听（仅在未禁用时）
	if !cm.disableFileWatcher {
		if err := cm.startWatcher(); err != nil {
			log.Printf("启动配置文件监听失败: %v", err)
		}
	} else {
		log.Printf("📁 File watcher disabled (using database polling)")
	}

	// 启动定期清理
	go cm.cleanupExpiredFailures()

	return cm, nil
}

// loadConfig 加载配置
func (cm *ConfigManager) loadConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 如果配置文件不存在，创建默认配置
	if _, err := os.Stat(cm.configFile); os.IsNotExist(err) {
		defaultConfig := Config{
			Upstream:                 []UpstreamConfig{},
			CurrentUpstream:          0,
			LoadBalance:              "failover",
			ResponsesUpstream:        []UpstreamConfig{},
			CurrentResponsesUpstream: 0,
			ResponsesLoadBalance:     "failover",
			GeminiUpstream:           []UpstreamConfig{},
			GeminiLoadBalance:        "failover",
			ChatUpstream:             []UpstreamConfig{},
			ChatLoadBalance:          "failover",
			UserAgent:                GetDefaultUserAgentConfig(),
		}

		// 确保目录存在
		if err := os.MkdirAll(filepath.Dir(cm.configFile), 0755); err != nil {
			return err
		}

		return cm.saveConfigLocked(defaultConfig)
	}

	// 读取配置文件
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &cm.config); err != nil {
		return err
	}
	normalizeConfigCodexServiceTierOverrides(&cm.config)

	// 兼容旧配置：如果 ResponsesLoadBalance 为空则回退到主配置
	if cm.config.LoadBalance == "" {
		cm.config.LoadBalance = "failover"
	}
	if cm.config.ResponsesLoadBalance == "" {
		cm.config.ResponsesLoadBalance = cm.config.LoadBalance
	}

	// 兼容旧格式：检测是否需要迁移（旧格式无 status 字段或 status 为空）
	needMigrationUpstream := false
	needMigrationResponses := false

	// 检查 Messages 渠道是否需要迁移
	if len(cm.config.Upstream) > 0 {
		hasStatusField := false
		for _, up := range cm.config.Upstream {
			if up.Status != "" {
				hasStatusField = true
				break
			}
		}
		if !hasStatusField {
			needMigrationUpstream = true
		}
	}

	// 检查 Responses 渠道是否需要迁移
	if len(cm.config.ResponsesUpstream) > 0 {
		hasStatusField := false
		for _, up := range cm.config.ResponsesUpstream {
			if up.Status != "" {
				hasStatusField = true
				break
			}
		}
		if !hasStatusField {
			needMigrationResponses = true
		}
	}

	if needMigrationUpstream || needMigrationResponses {
		log.Printf("检测到旧格式配置，正在迁移到新格式...")

		// Messages 渠道迁移
		if needMigrationUpstream && len(cm.config.Upstream) > 0 {
			currentIdx := cm.config.CurrentUpstream
			if currentIdx < 0 || currentIdx >= len(cm.config.Upstream) {
				currentIdx = 0
			}
			// 设置所有渠道的 status 状态
			for i := range cm.config.Upstream {
				if i == currentIdx {
					cm.config.Upstream[i].Status = "active"
				} else {
					cm.config.Upstream[i].Status = "disabled"
				}
			}
			log.Printf("Messages 渠道 [%d] %s 已设置为 active，其他 %d 个渠道已设为 disabled",
				currentIdx, cm.config.Upstream[currentIdx].Name, len(cm.config.Upstream)-1)
		}

		// Responses 渠道迁移
		if needMigrationResponses && len(cm.config.ResponsesUpstream) > 0 {
			currentIdx := cm.config.CurrentResponsesUpstream
			if currentIdx < 0 || currentIdx >= len(cm.config.ResponsesUpstream) {
				currentIdx = 0
			}
			// 设置所有渠道的 status 状态
			for i := range cm.config.ResponsesUpstream {
				if i == currentIdx {
					cm.config.ResponsesUpstream[i].Status = "active"
				} else {
					cm.config.ResponsesUpstream[i].Status = "disabled"
				}
			}
			log.Printf("Responses 渠道 [%d] %s 已设置为 active，其他 %d 个渠道已设为 disabled",
				currentIdx, cm.config.ResponsesUpstream[currentIdx].Name, len(cm.config.ResponsesUpstream)-1)
		}

		// 保存迁移后的配置（saveConfigLocked 会自动清理废弃字段）
		if err := cm.saveConfigLocked(cm.config); err != nil {
			log.Printf("保存迁移后的配置失败: %v", err)
			return err
		}
		log.Printf("配置迁移完成")
	}

	// 故障转移规则迁移：检测旧格式规则并转换为新的动作链格式
	if len(cm.config.Failover.Rules) > 0 {
		migratedRules, needsRuleMigration := MigrateRules(cm.config.Failover.Rules)
		normalizedRules, normalizedRuleCount := NormalizeDangerousOthersFailoverRules(migratedRules)
		if needsRuleMigration || normalizedRuleCount > 0 {
			if needsRuleMigration {
				log.Printf("检测到旧格式故障转移规则，正在迁移到动作链格式...")
			}
			if normalizedRuleCount > 0 {
				log.Printf("⚠️ 检测到 %d 条危险规则 'others + failover'，已自动转换为 'others + return_error'", normalizedRuleCount)
			}
			cm.config.Failover.Rules = normalizedRules
			if err := cm.saveConfigLocked(cm.config); err != nil {
				log.Printf("保存故障转移规则更新失败: %v", err)
				return err
			}
			if needsRuleMigration {
				log.Printf("故障转移规则迁移完成，共迁移 %d 条规则", len(migratedRules))
			}
		}
	}

	// User-Agent 配置迁移：为旧配置填充默认值
	userAgentChanged := normalizeUserAgentConfig(&cm.config.UserAgent)
	if userAgentChanged {
		log.Printf("检测到缺失/无效的 User-Agent 配置，正在写入默认值...")
		if err := cm.saveConfigLocked(cm.config); err != nil {
			log.Printf("保存 User-Agent 配置迁移失败: %v", err)
			return err
		}
	}

	// 自检：没有配置 key 的渠道自动暂停
	needSave := cm.validateChannelKeys()
	if needSave {
		if err := cm.saveConfigLocked(cm.config); err != nil {
			log.Printf("保存自检后的配置失败: %v", err)
			return err
		}
	}

	// 安全警告：检查 insecureSkipVerify 选项
	cm.warnInsecureChannels()

	// 设置每个渠道的 Index 字段（运行时字段，用于请求日志等）
	// 同时为没有 ID 的渠道生成 ID（迁移支持）
	idMigrationNeeded := false
	for i := range cm.config.Upstream {
		cm.config.Upstream[i].Index = i
		if cm.config.Upstream[i].ID == "" {
			cm.config.Upstream[i].ID = generateChannelID()
			idMigrationNeeded = true
			log.Printf("Generated ID for Messages channel [%d] %s: %s", i, cm.config.Upstream[i].Name, cm.config.Upstream[i].ID)
		}
	}
	for i := range cm.config.ResponsesUpstream {
		cm.config.ResponsesUpstream[i].Index = i
		if cm.config.ResponsesUpstream[i].ID == "" {
			cm.config.ResponsesUpstream[i].ID = generateChannelID()
			idMigrationNeeded = true
			log.Printf("Generated ID for Responses channel [%d] %s: %s", i, cm.config.ResponsesUpstream[i].Name, cm.config.ResponsesUpstream[i].ID)
		}
	}
	for i := range cm.config.GeminiUpstream {
		cm.config.GeminiUpstream[i].Index = i
		if cm.config.GeminiUpstream[i].ID == "" {
			cm.config.GeminiUpstream[i].ID = generateChannelID()
			idMigrationNeeded = true
			log.Printf("Generated ID for Gemini channel [%d] %s: %s", i, cm.config.GeminiUpstream[i].Name, cm.config.GeminiUpstream[i].ID)
		}
	}
	for i := range cm.config.ChatUpstream {
		cm.config.ChatUpstream[i].Index = i
		if cm.config.ChatUpstream[i].ID == "" {
			cm.config.ChatUpstream[i].ID = generateChannelID()
			idMigrationNeeded = true
			log.Printf("Generated ID for Chat channel [%d] %s: %s", i, cm.config.ChatUpstream[i].Name, cm.config.ChatUpstream[i].ID)
		}
	}
	if idMigrationNeeded {
		if err := cm.saveConfigLocked(cm.config); err != nil {
			log.Printf("Failed to save ID migration: %v", err)
			return err
		}
		log.Printf("Channel ID migration completed")
	}

	return nil
}

// validateChannelKeys 自检渠道密钥配置
// 没有配置 API key 的渠道，即使状态为 active 也应暂停
// 例外：openai-oauth 类型渠道使用 OAuthTokens 而非 APIKeys
// 例外：composite 类型渠道是虚拟路由，不需要 APIKeys
// 返回 true 表示有配置被修改，需要保存
func (cm *ConfigManager) validateChannelKeys() bool {
	modified := false

	// 检查 Messages 渠道
	for i := range cm.config.Upstream {
		upstream := &cm.config.Upstream[i]
		status := upstream.Status
		if status == "" {
			status = "active"
		}

		// Composite channels don't need API keys (they route to other channels)
		if IsCompositeChannel(upstream) {
			continue
		}

		// 如果是 active 状态但没有配置 key，自动设为 suspended
		if status == "active" && len(upstream.APIKeys) == 0 {
			upstream.Status = "suspended"
			modified = true
			log.Printf("⚠️ [自检] Messages 渠道 [%d] %s 没有配置 API key，已自动暂停", i, upstream.Name)
		}
	}

	// 检查 Responses 渠道
	for i := range cm.config.ResponsesUpstream {
		upstream := &cm.config.ResponsesUpstream[i]
		status := upstream.Status
		if status == "" {
			status = "active"
		}

		// openai-oauth 类型使用 OAuthTokens，不需要 APIKeys
		if upstream.ServiceType == "openai-oauth" {
			// 检查 OAuthTokens 是否配置
			if status == "active" && (upstream.OAuthTokens == nil || upstream.OAuthTokens.AccessToken == "") {
				upstream.Status = "suspended"
				modified = true
				log.Printf("⚠️ [自检] Responses 渠道 [%d] %s (openai-oauth) 没有配置 OAuth tokens，已自动暂停", i, upstream.Name)
			}
			continue
		}

		// 如果是 active 状态但没有配置 key，自动设为 suspended
		if status == "active" && len(upstream.APIKeys) == 0 {
			upstream.Status = "suspended"
			modified = true
			log.Printf("⚠️ [自检] Responses 渠道 [%d] %s 没有配置 API key，已自动暂停", i, upstream.Name)
		}
	}

	// 检查 Gemini 渠道
	for i := range cm.config.GeminiUpstream {
		upstream := &cm.config.GeminiUpstream[i]
		status := upstream.Status
		if status == "" {
			status = "active"
		}

		// 如果是 active 状态但没有配置 key，自动设为 suspended
		if status == "active" && len(upstream.APIKeys) == 0 {
			upstream.Status = "suspended"
			modified = true
			log.Printf("⚠️ [自检] Gemini 渠道 [%d] %s 没有配置 API key，已自动暂停", i, upstream.Name)
		}
	}

	// 检查 Chat 渠道
	for i := range cm.config.ChatUpstream {
		upstream := &cm.config.ChatUpstream[i]
		status := upstream.Status
		if status == "" {
			status = "active"
		}

		if status == "active" && len(upstream.APIKeys) == 0 {
			upstream.Status = "suspended"
			modified = true
			log.Printf("⚠️ [自检] Chat 渠道 [%d] %s 没有配置 API key，已自动暂停", i, upstream.Name)
		}
	}

	return modified
}

// warnInsecureChannels 警告使用了 insecureSkipVerify 的渠道
func (cm *ConfigManager) warnInsecureChannels() {
	// 检查 Messages 渠道
	for i, upstream := range cm.config.Upstream {
		if upstream.InsecureSkipVerify {
			log.Printf("⚠️ [安全警告] Messages 渠道 [%d] %s 已启用 insecureSkipVerify - TLS 证书验证已禁用，存在中间人攻击风险",
				i, upstream.Name)
		}
	}

	// 检查 Responses 渠道
	for i, upstream := range cm.config.ResponsesUpstream {
		if upstream.InsecureSkipVerify {
			log.Printf("⚠️ [安全警告] Responses 渠道 [%d] %s 已启用 insecureSkipVerify - TLS 证书验证已禁用，存在中间人攻击风险",
				i, upstream.Name)
		}
	}

	// 检查 Gemini 渠道
	for i, upstream := range cm.config.GeminiUpstream {
		if upstream.InsecureSkipVerify {
			log.Printf("⚠️ [安全警告] Gemini 渠道 [%d] %s 已启用 insecureSkipVerify - TLS 证书验证已禁用，存在中间人攻击风险",
				i, upstream.Name)
		}
	}

	// 检查 Chat 渠道
	for i, upstream := range cm.config.ChatUpstream {
		if upstream.InsecureSkipVerify {
			log.Printf("⚠️ [安全警告] Chat 渠道 [%d] %s 已启用 insecureSkipVerify - TLS 证书验证已禁用，存在中间人攻击风险",
				i, upstream.Name)
		}
	}
}

// saveConfigLocked 保存配置（已加锁）
func (cm *ConfigManager) saveConfigLocked(config Config) error {
	normalizeConfigCodexServiceTierOverrides(&config)

	// When using database storage, skip JSON file operations
	// Database is the source of truth in multi-instance deployments
	if cm.dbStorage != nil {
		// Ensure all channels have IDs before saving to prevent
		// duplicate ID generation in concurrent writes
		for i := range config.Upstream {
			if config.Upstream[i].ID == "" {
				config.Upstream[i].ID = generateChannelID()
			}
		}
		for i := range config.ResponsesUpstream {
			if config.ResponsesUpstream[i].ID == "" {
				config.ResponsesUpstream[i].ID = generateChannelID()
			}
		}
		for i := range config.GeminiUpstream {
			if config.GeminiUpstream[i].ID == "" {
				config.GeminiUpstream[i].ID = generateChannelID()
			}
		}
		for i := range config.ChatUpstream {
			if config.ChatUpstream[i].ID == "" {
				config.ChatUpstream[i].ID = generateChannelID()
			}
		}

		cm.config = config

		// Write to database synchronously to guarantee ordering and
		// return persistence errors to callers.
		start := time.Now()
		if err := cm.dbStorage.SaveConfigToDB(&config); err != nil {
			return err
		}
		elapsed := time.Since(start)
		if elapsed > 100*time.Millisecond {
			log.Printf("⏱️ Database sync took %v", elapsed)
		}
		return nil
	}

	// JSON-only mode: backup, write to file
	cm.backupConfig()

	// 清理已废弃字段，确保不会被序列化到 JSON
	config.CurrentUpstream = 0
	config.CurrentResponsesUpstream = 0

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	cm.config = config

	// Write to JSON file
	return os.WriteFile(cm.configFile, data, 0644)
}

// SaveConfig 保存配置
func (cm *ConfigManager) SaveConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveConfigLocked(cm.config)
}

// SetDBStorage sets the database storage adapter for write-through caching
func (cm *ConfigManager) SetDBStorage(dbStorage *DBConfigStorage) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.dbStorage = dbStorage
	// Disable file watcher when using database storage (polling handles sync)
	cm.disableFileWatcher = true
	// Close existing watcher if it's running
	if cm.watcher != nil {
		cm.watcher.Close()
		cm.watcher = nil
		log.Printf("📁 File watcher stopped (switched to database polling)")
	}
}

// ReloadConfig 从磁盘重新加载配置
func (cm *ConfigManager) ReloadConfig() error {
	return cm.loadConfig()
}

// RestoreConfig 从备份恢复配置
func (cm *ConfigManager) RestoreConfig(config Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Backup current config before restoring
	cm.backupConfig()

	// Save the restored config
	return cm.saveConfigLocked(config)
}

// backupConfig 备份配置
func (cm *ConfigManager) backupConfig() {
	if _, err := os.Stat(cm.configFile); os.IsNotExist(err) {
		return
	}

	backupDir := filepath.Join(filepath.Dir(cm.configFile), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		log.Printf("创建备份目录失败: %v", err)
		return
	}

	// 读取当前配置
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		log.Printf("读取配置文件失败: %v", err)
		return
	}

	// 创建备份文件
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("config-%s.json", timestamp))
	if err := os.WriteFile(backupFile, data, 0644); err != nil {
		log.Printf("写入备份文件失败: %v", err)
		return
	}

	// 清理旧备份
	cm.cleanupOldBackups(backupDir)
}

// cleanupOldBackups 清理旧备份
func (cm *ConfigManager) cleanupOldBackups(backupDir string) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	if len(entries) <= maxBackups {
		return
	}

	// 删除最旧的备份
	for i := 0; i < len(entries)-maxBackups; i++ {
		os.Remove(filepath.Join(backupDir, entries[i].Name()))
	}
}

// startWatcher 启动文件监听
func (cm *ConfigManager) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	cm.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("检测到配置文件变化，重载配置...")
					if err := cm.loadConfig(); err != nil {
						log.Printf("配置重载失败: %v", err)
					} else {
						log.Printf("配置已重载")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("文件监听错误: %v", err)
			}
		}
	}()

	return watcher.Add(cm.configFile)
}

// GetConfig 获取配置
func (cm *ConfigManager) GetConfig() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// GetCurrentUpstream 获取当前上游配置
// 优先选择第一个 active 状态的渠道，若无则回退到第一个渠道
func (cm *ConfigManager) GetCurrentUpstream() (*UpstreamConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.config.Upstream) == 0 {
		return nil, fmt.Errorf("no upstream channels configured")
	}

	// 优先选择第一个 active 状态的渠道
	for i := range cm.config.Upstream {
		status := cm.config.Upstream[i].Status
		if status == "" || status == "active" {
			upstream := cm.config.Upstream[i]
			return &upstream, nil
		}
	}

	// 没有 active 渠道，回退到第一个渠道
	upstream := cm.config.Upstream[0]
	return &upstream, nil
}

// GetNextAPIKey 获取下一个 API 密钥（Messages 负载均衡）
func (cm *ConfigManager) GetNextAPIKey(upstream *UpstreamConfig, failedKeys map[string]bool) (string, error) {
	// Use per-channel key load balance setting if set, otherwise fall back to global
	strategy := upstream.KeyLoadBalance
	if strategy == "" {
		strategy = cm.config.LoadBalance
	}
	return cm.getNextAPIKeyWithStrategy(upstream, failedKeys, strategy, &cm.requestCount)
}

// GetNextResponsesAPIKey 获取下一个 API 密钥（Responses 负载均衡）
func (cm *ConfigManager) GetNextResponsesAPIKey(upstream *UpstreamConfig, failedKeys map[string]bool) (string, error) {
	// Use per-channel key load balance setting if set, otherwise fall back to global
	strategy := upstream.KeyLoadBalance
	if strategy == "" {
		strategy = cm.config.ResponsesLoadBalance
	}
	return cm.getNextAPIKeyWithStrategy(upstream, failedKeys, strategy, &cm.responsesRequestCount)
}

func (cm *ConfigManager) getNextAPIKeyWithStrategy(upstream *UpstreamConfig, failedKeys map[string]bool, strategy string, requestCounter *int) (string, error) {
	if len(upstream.APIKeys) == 0 {
		return "", fmt.Errorf("upstream %s has no available API keys", upstream.Name)
	}

	// 综合考虑临时失败密钥和内存中的失败密钥
	availableKeys := []string{}
	for _, key := range upstream.APIKeys {
		if !failedKeys[key] && !cm.isKeyFailed(key) {
			availableKeys = append(availableKeys, key)
		}
	}

	if len(availableKeys) == 0 {
		// 如果所有密钥都失效,检查是否有可以恢复的密钥
		allFailedKeys := []string{}
		for _, key := range upstream.APIKeys {
			if failedKeys[key] || cm.isKeyFailed(key) {
				allFailedKeys = append(allFailedKeys, key)
			}
		}

		if len(allFailedKeys) == len(upstream.APIKeys) {
			// 如果所有密钥都在内存失败缓存中,尝试选择失败时间最早的密钥
			var oldestFailedKey string
			oldestTime := time.Now()

			cm.mu.RLock()
			for _, key := range upstream.APIKeys {
				if !failedKeys[key] { // 排除本次请求已经尝试过的密钥
					if failure, exists := cm.failedKeysCache[key]; exists {
						if failure.Timestamp.Before(oldestTime) {
							oldestTime = failure.Timestamp
							oldestFailedKey = key
						}
					}
				}
			}
			cm.mu.RUnlock()

			if oldestFailedKey != "" {
				log.Printf("⚠️ 所有密钥都失效,尝试最早失败的密钥: %s", maskAPIKey(oldestFailedKey))
				return oldestFailedKey, nil
			}
		}

		return "", fmt.Errorf("all API keys for upstream %s are temporarily unavailable", upstream.Name)
	}

	// 根据负载均衡策略选择密钥
	switch strategy {
	case "round-robin":
		cm.mu.Lock()
		*requestCounter++
		index := (*requestCounter - 1) % len(availableKeys)
		selectedKey := availableKeys[index]
		cm.mu.Unlock()
		log.Printf("轮询选择密钥 %s (%d/%d)", maskAPIKey(selectedKey), index+1, len(availableKeys))
		return selectedKey, nil

	case "random":
		index := rand.Intn(len(availableKeys))
		selectedKey := availableKeys[index]
		log.Printf("随机选择密钥 %s (%d/%d)", maskAPIKey(selectedKey), index+1, len(availableKeys))
		return selectedKey, nil

	case "failover":
		fallthrough
	default:
		selectedKey := availableKeys[0]
		// 获取该密钥在原始列表中的索引
		keyIndex := 0
		for i, key := range upstream.APIKeys {
			if key == selectedKey {
				keyIndex = i + 1
				break
			}
		}
		log.Printf("故障转移选择密钥 %s (%d/%d)", maskAPIKey(selectedKey), keyIndex, len(upstream.APIKeys))
		return selectedKey, nil
	}
}

// MarkKeyAsFailed 标记密钥失败
func (cm *ConfigManager) MarkKeyAsFailed(apiKey string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if failure, exists := cm.failedKeysCache[apiKey]; exists {
		failure.FailureCount++
		failure.Timestamp = time.Now()
	} else {
		cm.failedKeysCache[apiKey] = &FailedKey{
			Timestamp:    time.Now(),
			FailureCount: 1,
		}
	}

	failure := cm.failedKeysCache[apiKey]
	recoveryTime := cm.keyRecoveryTime
	if failure.FailureCount > cm.maxFailureCount {
		recoveryTime = cm.keyRecoveryTime * 2
	}

	log.Printf("标记API密钥失败: %s (失败次数: %d, 恢复时间: %v)",
		maskAPIKey(apiKey), failure.FailureCount, recoveryTime)
}

// isKeyFailed 检查密钥是否失败
func (cm *ConfigManager) isKeyFailed(apiKey string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	failure, exists := cm.failedKeysCache[apiKey]
	if !exists {
		return false
	}

	recoveryTime := cm.keyRecoveryTime
	if failure.FailureCount > cm.maxFailureCount {
		recoveryTime = cm.keyRecoveryTime * 2
	}

	return time.Since(failure.Timestamp) < recoveryTime
}

// cleanupExpiredFailures 清理过期的失败记录
func (cm *ConfigManager) cleanupExpiredFailures() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cm.mu.Lock()
		now := time.Now()
		for key, failure := range cm.failedKeysCache {
			recoveryTime := cm.keyRecoveryTime
			if failure.FailureCount > cm.maxFailureCount {
				recoveryTime = cm.keyRecoveryTime * 2
			}

			if now.Sub(failure.Timestamp) > recoveryTime {
				delete(cm.failedKeysCache, key)
				log.Printf("API密钥 %s 已从失败列表中恢复", maskAPIKey(key))
			}
		}
		cm.mu.Unlock()
	}
}

// clearFailedKeysForUpstream 清理指定渠道的所有失败 key 记录
// 当渠道被删除时调用，避免内存泄漏和冷却状态残留
func (cm *ConfigManager) clearFailedKeysForUpstream(upstream *UpstreamConfig) {
	for _, key := range upstream.APIKeys {
		if _, exists := cm.failedKeysCache[key]; exists {
			delete(cm.failedKeysCache, key)
			log.Printf("已清理被删除渠道 %s 的失败密钥记录: %s", upstream.Name, maskAPIKey(key))
		}
	}
}

// AddUpstream 添加上游
// ValidateCompositeChannel validates a messages composite channel configuration.
// messagesUpstreams and responsesUpstreams are used for cross-pool target validation.
func ValidateCompositeChannel(upstream *UpstreamConfig, messagesUpstreams []UpstreamConfig, responsesUpstreams []UpstreamConfig) error {
	if !IsCompositeChannel(upstream) {
		return nil // Not a composite channel, no validation needed
	}

	// Composite channels must have exactly 3 mappings (haiku, sonnet, opus)
	if len(upstream.CompositeMappings) != 3 {
		return fmt.Errorf("composite channel must have exactly 3 mappings (haiku, sonnet, opus), got %d", len(upstream.CompositeMappings))
	}

	// Composite channel invariants: should not have APIKeys or BaseURL
	if len(upstream.APIKeys) > 0 {
		return fmt.Errorf("composite channel should not have API keys (it routes to other channels)")
	}
	if upstream.BaseURL != "" {
		return fmt.Errorf("composite channel should not have baseUrl (it routes to other channels)")
	}

	// Required patterns
	requiredPatterns := map[string]bool{"haiku": false, "sonnet": false, "opus": false}

	// Messages-pool service types that can receive /v1/messages traffic
	messagesCompatibleTypes := map[string]bool{
		"claude":      true,
		"openai_chat": true,
		"openai":      true,
		"gemini":      true,
		"openaiold":   true,
		"responses":   true,
	}
	// Responses-pool target types allowed for messages composite bridging
	responsesCompatibleTypes := map[string]bool{
		"responses":    true,
		"openai-oauth": true,
	}

	validateTarget := func(target CompositeTargetRef, context string) (*UpstreamConfig, error) {
		pool := NormalizeCompositeTargetPool(target.Pool)
		channelID := strings.TrimSpace(target.ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("%s: channel ID is empty", context)
		}

		var upstreams []UpstreamConfig
		var allowedTypes map[string]bool
		switch pool {
		case CompositeTargetPoolResponses:
			upstreams = responsesUpstreams
			allowedTypes = responsesCompatibleTypes
		default:
			upstreams = messagesUpstreams
			allowedTypes = messagesCompatibleTypes
		}

		for idx := range upstreams {
			if upstreams[idx].ID == channelID {
				targetUpstream := &upstreams[idx]
				if !allowedTypes[targetUpstream.ServiceType] {
					return nil, fmt.Errorf("%s: channel '%s' in pool '%s' has incompatible type '%s'", context, targetUpstream.Name, pool, targetUpstream.ServiceType)
				}
				if IsCompositeChannel(targetUpstream) {
					return nil, fmt.Errorf("%s: channel '%s' cannot be another composite channel", context, targetUpstream.Name)
				}
				return targetUpstream, nil
			}
		}
		return nil, fmt.Errorf("%s: channel ID '%s' not found in pool '%s'", context, channelID, pool)
	}

	// Validate each mapping
	for i := range upstream.CompositeMappings {
		mapping := &upstream.CompositeMappings[i]

		// Normalize pattern: trim whitespace
		mapping.Pattern = strings.TrimSpace(mapping.Pattern)
		mapping.TargetModel = strings.TrimSpace(mapping.TargetModel)

		// Pattern must be one of the required patterns (no wildcard)
		if mapping.Pattern == "" {
			return fmt.Errorf("mapping %d: pattern cannot be empty", i)
		}
		if mapping.Pattern == "*" {
			return fmt.Errorf("mapping %d: wildcard (*) pattern is not allowed; all 3 models must be explicitly mapped", i)
		}
		if _, isRequired := requiredPatterns[mapping.Pattern]; !isRequired {
			return fmt.Errorf("mapping %d: pattern '%s' is not valid; must be one of: haiku, sonnet, opus", i, mapping.Pattern)
		}
		if requiredPatterns[mapping.Pattern] {
			return fmt.Errorf("mapping %d: duplicate pattern '%s'", i, mapping.Pattern)
		}
		requiredPatterns[mapping.Pattern] = true

		if mapping.TargetPool != "" && !IsValidCompositeTargetPool(mapping.TargetPool) {
			return fmt.Errorf("mapping %d (pattern '%s'): invalid targetPool '%s'", i, mapping.Pattern, mapping.TargetPool)
		}
		for j, target := range mapping.FailoverTargets {
			if target.Pool != "" && !IsValidCompositeTargetPool(target.Pool) {
				return fmt.Errorf("mapping %d (pattern '%s') failoverTargets[%d]: invalid pool '%s'", i, mapping.Pattern, j, target.Pool)
			}
		}

		// Legacy primary migration: index -> ID in messages pool
		if strings.TrimSpace(mapping.TargetChannelID) == "" && mapping.TargetChannel >= 0 && mapping.TargetChannel < len(messagesUpstreams) {
			target := &messagesUpstreams[mapping.TargetChannel]
			if target.ID != "" {
				mapping.TargetChannelID = target.ID
				mapping.TargetPool = CompositeTargetPoolMessages
				log.Printf("Auto-migrated composite mapping %d: index %d -> ID %s", i, mapping.TargetChannel, target.ID)
			} else {
				return fmt.Errorf("mapping %d: target channel at index %d has no ID", i, mapping.TargetChannel)
			}
		}

		normalizeCompositeMappingTargets(mapping)

		primary := getCompositePrimaryTargetRef(mapping)
		if _, err := validateTarget(primary, fmt.Sprintf("mapping %d (pattern '%s')", i, mapping.Pattern)); err != nil {
			return err
		}

		failoverTargets := getCompositeFailoverTargetRefs(mapping)
		if len(failoverTargets) == 0 {
			return fmt.Errorf("mapping %d (pattern '%s'): failover chain must have at least 1 channel", i, mapping.Pattern)
		}
		for j, failoverTarget := range failoverTargets {
			if _, err := validateTarget(failoverTarget, fmt.Sprintf("mapping %d (pattern '%s') failover[%d]", i, mapping.Pattern, j)); err != nil {
				return err
			}
		}
	}

	// Ensure all required patterns are present
	for pattern, found := range requiredPatterns {
		if !found {
			return fmt.Errorf("missing required pattern '%s'; all 3 models (haiku, sonnet, opus) must be mapped", pattern)
		}
	}

	return nil
}

func (cm *ConfigManager) AddUpstream(upstream UpstreamConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	upstream.Name = strings.TrimSpace(upstream.Name)
	if upstream.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if err := cm.ensureUniqueChannelNameLocked(upstream.Name, "messages", -1); err != nil {
		return err
	}

	// Generate unique ID if not provided
	if upstream.ID == "" {
		upstream.ID = generateChannelID()
	}

	// Validate channel ID doesn't use reserved prefix
	if strings.HasPrefix(upstream.ID, "__invalid_") {
		return fmt.Errorf("channel ID cannot start with reserved prefix '__invalid_'")
	}

	// Validate composite channel before adding
	if IsCompositeChannel(&upstream) {
		if err := ValidateCompositeChannel(&upstream, cm.config.Upstream, cm.config.ResponsesUpstream); err != nil {
			return fmt.Errorf("invalid composite channel: %w", err)
		}
	}

	// 新建渠道默认设为 active
	if upstream.Status == "" {
		upstream.Status = "active"
	}

	// Set the Index to the new position
	upstream.Index = len(cm.config.Upstream)

	cm.config.Upstream = append(cm.config.Upstream, upstream)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加上游: %s (ID: %s)", upstream.Name, upstream.ID)
	return nil
}

// UpdateUpstream 更新上游
// 返回值：shouldResetMetrics 表示是否需要重置渠道指标（熔断状态）
func (cm *ConfigManager) UpdateUpstream(index int, updates UpstreamUpdate) (shouldResetMetrics bool, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return false, fmt.Errorf("invalid upstream index: %d", index)
	}

	upstream := &cm.config.Upstream[index]

	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return false, fmt.Errorf("channel name is required")
		}
		if err := cm.ensureUniqueChannelNameLocked(name, "messages", index); err != nil {
			return false, err
		}
		upstream.Name = name
	}
	if updates.BaseURL != nil {
		upstream.BaseURL = *updates.BaseURL
	}
	if updates.ServiceType != nil {
		upstream.ServiceType = *updates.ServiceType
	}
	if updates.Description != nil {
		upstream.Description = *updates.Description
	}
	if updates.Website != nil {
		upstream.Website = *updates.Website
	}
	if updates.APIKeys != nil {
		// 只有单 key 场景且 key 被更换时，才自动激活并重置熔断
		if len(upstream.APIKeys) == 1 && len(updates.APIKeys) == 1 &&
			upstream.APIKeys[0] != updates.APIKeys[0] {
			shouldResetMetrics = true
			if upstream.Status == "suspended" {
				upstream.Status = "active"
				log.Printf("渠道 [%d] %s 已从暂停状态自动激活（单 key 更换）", index, upstream.Name)
			}
		}
		upstream.APIKeys = updates.APIKeys
	}
	if updates.ModelMapping != nil {
		upstream.ModelMapping = updates.ModelMapping
	}
	if updates.InsecureSkipVerify != nil {
		upstream.InsecureSkipVerify = *updates.InsecureSkipVerify
	}
	if updates.Priority != nil {
		upstream.Priority = *updates.Priority
	}
	if updates.Status != nil {
		upstream.Status = *updates.Status
	}
	if updates.PromotionUntil != nil {
		upstream.PromotionUntil = updates.PromotionUntil
	}
	if updates.ResponseHeaderTimeoutSecs != nil {
		upstream.ResponseHeaderTimeoutSecs = *updates.ResponseHeaderTimeoutSecs
	}
	if updates.PriceMultipliers != nil {
		upstream.PriceMultipliers = updates.PriceMultipliers
	}
	if updates.CodexServiceTierOverride != nil {
		upstream.CodexServiceTierOverride = strings.TrimSpace(*updates.CodexServiceTierOverride)
	}
	// 配额设置
	if updates.QuotaType != nil {
		upstream.QuotaType = *updates.QuotaType
		// 当 quotaType 设置为空时，清除所有其他配额字段
		if *updates.QuotaType == "" {
			upstream.QuotaLimit = 0
			upstream.QuotaResetAt = nil
			upstream.QuotaResetInterval = 0
			upstream.QuotaResetUnit = ""
			upstream.QuotaModels = nil
			upstream.QuotaResetMode = ""
		}
	}
	if updates.QuotaLimit != nil {
		upstream.QuotaLimit = *updates.QuotaLimit
	}
	if updates.QuotaResetAt != nil {
		upstream.QuotaResetAt = updates.QuotaResetAt
	}
	if updates.QuotaResetInterval != nil {
		upstream.QuotaResetInterval = *updates.QuotaResetInterval
	}
	if updates.QuotaResetUnit != nil {
		upstream.QuotaResetUnit = *updates.QuotaResetUnit
	}
	if updates.QuotaModels != nil {
		upstream.QuotaModels = updates.QuotaModels
	}
	if updates.QuotaResetMode != nil {
		upstream.QuotaResetMode = *updates.QuotaResetMode
	}
	// Per-channel rate limiting
	if updates.RateLimitRpm != nil {
		upstream.RateLimitRpm = *updates.RateLimitRpm
	}
	if updates.QueueEnabled != nil {
		upstream.QueueEnabled = *updates.QueueEnabled
	}
	if updates.QueueTimeout != nil {
		upstream.QueueTimeout = *updates.QueueTimeout
	}
	// Per-channel API key load balancing strategy
	if updates.KeyLoadBalance != nil {
		upstream.KeyLoadBalance = *updates.KeyLoadBalance
	}
	// Content filter
	if updates.ContentFilter != nil {
		upstream.ContentFilter = updates.ContentFilter
	}
	// Composite channel mappings
	if updates.CompositeMappings != nil {
		upstream.CompositeMappings = updates.CompositeMappings
	}

	// Validate composite channel after applying all updates
	if IsCompositeChannel(upstream) {
		if err := ValidateCompositeChannel(upstream, cm.config.Upstream, cm.config.ResponsesUpstream); err != nil {
			return false, fmt.Errorf("invalid composite channel: %w", err)
		}
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return false, err
	}

	log.Printf("已更新上游: [%d] %s", index, cm.config.Upstream[index].Name)
	return shouldResetMetrics, nil
}

// RemoveUpstream 删除上游
func (cm *ConfigManager) RemoveUpstream(index int) (*UpstreamConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return nil, fmt.Errorf("invalid upstream index: %d", index)
	}

	removed := cm.config.Upstream[index]

	// Update composite mappings that reference this channel
	cm.updateCompositeMappingsOnDelete(removed.ID, index, CompositeTargetPoolMessages)

	cm.config.Upstream = append(cm.config.Upstream[:index], cm.config.Upstream[index+1:]...)

	// Reindex remaining upstreams
	for i := range cm.config.Upstream {
		cm.config.Upstream[i].Index = i
	}

	// 清理被删除渠道的失败 key 冷却记录
	cm.clearFailedKeysForUpstream(&removed)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return nil, err
	}

	log.Printf("已删除上游: %s (ID: %s)", removed.Name, removed.ID)
	return &removed, nil
}

// updateCompositeMappingsOnDelete removes or updates messages composite mappings when a target channel is deleted.
func (cm *ConfigManager) updateCompositeMappingsOnDelete(deletedID string, deletedIndex int, deletedPool string) {
	normalizedDeletedPool := NormalizeCompositeTargetPool(deletedPool)
	for i := range cm.config.Upstream {
		upstream := &cm.config.Upstream[i]
		if !IsCompositeChannel(upstream) || len(upstream.CompositeMappings) == 0 {
			continue
		}

		// Filter out mappings that reference the deleted channel
		validMappings := make([]CompositeMapping, 0, len(upstream.CompositeMappings))
		channelChanged := false
		for _, mapping := range upstream.CompositeMappings {
			normalizeCompositeMappingTargets(&mapping)

			// Check if this mapping references the deleted channel
			referencesDeleted := false
			primary := getCompositePrimaryTargetRef(&mapping)
			if primary.Pool == normalizedDeletedPool && primary.ChannelID == deletedID {
				referencesDeleted = true
			} else if normalizedDeletedPool == CompositeTargetPoolMessages && mapping.TargetChannelID == "" && mapping.TargetChannel == deletedIndex {
				// Legacy index-based primary reference (messages-only)
				referencesDeleted = true
			}

			if referencesDeleted {
				channelChanged = true
				log.Printf("⚠️ Removed composite mapping '%s' from channel '%s' (target channel deleted)",
					mapping.Pattern, upstream.Name)
				continue
			}

			// For legacy index-based mappings, update indices > deleted
			if normalizedDeletedPool == CompositeTargetPoolMessages && mapping.TargetChannelID == "" && mapping.TargetChannel > deletedIndex {
				mapping.TargetChannel--
				channelChanged = true
			}

			failovers := getCompositeFailoverTargetRefs(&mapping)
			filteredFailovers := make([]CompositeTargetRef, 0, len(failovers))
			removedFromFailover := false
			for _, failover := range failovers {
				if failover.Pool == normalizedDeletedPool && failover.ChannelID == deletedID {
					removedFromFailover = true
					continue
				}
				filteredFailovers = append(filteredFailovers, failover)
			}
			if removedFromFailover {
				channelChanged = true
				log.Printf("⚠️ Removed deleted target from composite failover chain '%s' in channel '%s'",
					mapping.Pattern, upstream.Name)
			}
			mapping.FailoverTargets = filteredFailovers
			legacyChain := make([]string, 0, len(filteredFailovers))
			for _, target := range filteredFailovers {
				legacyChain = append(legacyChain, target.ChannelID)
			}
			mapping.FailoverChain = legacyChain

			validMappings = append(validMappings, mapping)
		}

		if channelChanged || len(validMappings) != len(upstream.CompositeMappings) {
			upstream.CompositeMappings = validMappings
			if len(validMappings) == 0 {
				log.Printf("⚠️ Composite channel '%s' has no remaining mappings after deletion", upstream.Name)
			}
		}
	}
}

// AddAPIKey 添加API密钥
func (cm *ConfigManager) AddAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 检查密钥是否已存在
	for _, key := range cm.config.Upstream[index].APIKeys {
		if key == apiKey {
			return fmt.Errorf("API key already exists")
		}
	}

	cm.config.Upstream[index].APIKeys = append(cm.config.Upstream[index].APIKeys, apiKey)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加API密钥到上游 [%d] %s", index, cm.config.Upstream[index].Name)
	return nil
}

// RemoveAPIKey 删除API密钥
func (cm *ConfigManager) RemoveAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 查找并删除密钥
	keys := cm.config.Upstream[index].APIKeys
	found := false
	for i, key := range keys {
		if key == apiKey {
			cm.config.Upstream[index].APIKeys = append(keys[:i], keys[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("API key not found")
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从上游 [%d] %s 删除API密钥", index, cm.config.Upstream[index].Name)
	return nil
}

// SetLoadBalance 设置 Messages 负载均衡策略
func (cm *ConfigManager) SetLoadBalance(strategy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := validateLoadBalanceStrategy(strategy); err != nil {
		return err
	}

	cm.config.LoadBalance = strategy

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置负载均衡策略: %s", strategy)
	return nil
}

// SetResponsesLoadBalance 设置 Responses 负载均衡策略
func (cm *ConfigManager) SetResponsesLoadBalance(strategy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := validateLoadBalanceStrategy(strategy); err != nil {
		return err
	}

	cm.config.ResponsesLoadBalance = strategy

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Responses 负载均衡策略: %s", strategy)
	return nil
}

// SetGeminiLoadBalance 设置 Gemini 负载均衡策略
func (cm *ConfigManager) SetGeminiLoadBalance(strategy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := validateLoadBalanceStrategy(strategy); err != nil {
		return err
	}

	cm.config.GeminiLoadBalance = strategy

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Gemini 负载均衡策略: %s", strategy)
	return nil
}

func validateLoadBalanceStrategy(strategy string) error {
	if strategy != "round-robin" && strategy != "random" && strategy != "failover" {
		return fmt.Errorf("invalid load balancing strategy: %s", strategy)
	}
	return nil
}

// DeprioritizeAPIKey 降低API密钥优先级（在所有渠道中查找）
func (cm *ConfigManager) DeprioritizeAPIKey(apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 遍历所有渠道查找该 API 密钥
	for upstreamIdx := range cm.config.Upstream {
		upstream := &cm.config.Upstream[upstreamIdx]
		index := -1
		for i, key := range upstream.APIKeys {
			if key == apiKey {
				index = i
				break
			}
		}

		if index != -1 && index != len(upstream.APIKeys)-1 {
			// 移动到末尾
			upstream.APIKeys = append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)
			upstream.APIKeys = append(upstream.APIKeys, apiKey)
			log.Printf("已将API密钥移动到末尾以降低优先级: %s (渠道: %s)", maskAPIKey(apiKey), upstream.Name)
			return cm.saveConfigLocked(cm.config)
		}
	}

	// 同样遍历 Responses 渠道
	for upstreamIdx := range cm.config.ResponsesUpstream {
		upstream := &cm.config.ResponsesUpstream[upstreamIdx]
		index := -1
		for i, key := range upstream.APIKeys {
			if key == apiKey {
				index = i
				break
			}
		}

		if index != -1 && index != len(upstream.APIKeys)-1 {
			// 移动到末尾
			upstream.APIKeys = append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)
			upstream.APIKeys = append(upstream.APIKeys, apiKey)
			log.Printf("已将API密钥移动到末尾以降低优先级: %s (Responses渠道: %s)", maskAPIKey(apiKey), upstream.Name)
			return cm.saveConfigLocked(cm.config)
		}
	}

	return nil
}

// MoveAPIKeyToTop 将指定渠道的 API 密钥移到最前面
func (cm *ConfigManager) MoveAPIKeyToTop(upstreamIndex int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.Upstream[upstreamIndex]
	index := -1
	for i, key := range upstream.APIKeys {
		if key == apiKey {
			index = i
			break
		}
	}

	if index <= 0 {
		return nil // 已经在最前面或未找到
	}

	// 移动到开头
	upstream.APIKeys = append([]string{apiKey}, append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)...)
	return cm.saveConfigLocked(cm.config)
}

// MoveAPIKeyToBottom 将指定渠道的 API 密钥移到最后面
func (cm *ConfigManager) MoveAPIKeyToBottom(upstreamIndex int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.Upstream[upstreamIndex]
	index := -1
	for i, key := range upstream.APIKeys {
		if key == apiKey {
			index = i
			break
		}
	}

	if index == -1 || index == len(upstream.APIKeys)-1 {
		return nil // 已经在最后面或未找到
	}

	// 移动到末尾
	upstream.APIKeys = append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)
	upstream.APIKeys = append(upstream.APIKeys, apiKey)
	return cm.saveConfigLocked(cm.config)
}

// MoveResponsesAPIKeyToTop 将指定 Responses 渠道的 API 密钥移到最前面
func (cm *ConfigManager) MoveResponsesAPIKeyToTop(upstreamIndex int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.ResponsesUpstream[upstreamIndex]
	index := -1
	for i, key := range upstream.APIKeys {
		if key == apiKey {
			index = i
			break
		}
	}

	if index <= 0 {
		return nil
	}

	upstream.APIKeys = append([]string{apiKey}, append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)...)
	return cm.saveConfigLocked(cm.config)
}

// MoveResponsesAPIKeyToBottom 将指定 Responses 渠道的 API 密钥移到最后面
func (cm *ConfigManager) MoveResponsesAPIKeyToBottom(upstreamIndex int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.ResponsesUpstream[upstreamIndex]
	index := -1
	for i, key := range upstream.APIKeys {
		if key == apiKey {
			index = i
			break
		}
	}

	if index == -1 || index == len(upstream.APIKeys)-1 {
		return nil
	}

	upstream.APIKeys = append(upstream.APIKeys[:index], upstream.APIKeys[index+1:]...)
	upstream.APIKeys = append(upstream.APIKeys, apiKey)
	return cm.saveConfigLocked(cm.config)
}

// RemoveAPIKeyByIndex 通过索引删除API密钥（安全：不在URL中暴露密钥）
func (cm *ConfigManager) RemoveAPIKeyByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	keys := cm.config.Upstream[upstreamIndex].APIKeys
	if keyIndex < 0 || keyIndex >= len(keys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	cm.config.Upstream[upstreamIndex].APIKeys = append(keys[:keyIndex], keys[keyIndex+1:]...)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从上游 [%d] %s 删除索引为 %d 的API密钥", upstreamIndex, cm.config.Upstream[upstreamIndex].Name, keyIndex)
	return nil
}

// MoveAPIKeyToTopByIndex 通过索引将API密钥移到最前面
func (cm *ConfigManager) MoveAPIKeyToTopByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.Upstream[upstreamIndex]
	if keyIndex < 0 || keyIndex >= len(upstream.APIKeys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	if keyIndex == 0 {
		return nil // 已经在最前面
	}

	key := upstream.APIKeys[keyIndex]
	upstream.APIKeys = append([]string{key}, append(upstream.APIKeys[:keyIndex], upstream.APIKeys[keyIndex+1:]...)...)
	return cm.saveConfigLocked(cm.config)
}

// MoveAPIKeyToBottomByIndex 通过索引将API密钥移到最后面
func (cm *ConfigManager) MoveAPIKeyToBottomByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.Upstream[upstreamIndex]
	if keyIndex < 0 || keyIndex >= len(upstream.APIKeys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	if keyIndex == len(upstream.APIKeys)-1 {
		return nil // 已经在最后面
	}

	key := upstream.APIKeys[keyIndex]
	upstream.APIKeys = append(upstream.APIKeys[:keyIndex], upstream.APIKeys[keyIndex+1:]...)
	upstream.APIKeys = append(upstream.APIKeys, key)
	return cm.saveConfigLocked(cm.config)
}

// RemoveResponsesAPIKeyByIndex 通过索引删除Responses渠道API密钥
func (cm *ConfigManager) RemoveResponsesAPIKeyByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	keys := cm.config.ResponsesUpstream[upstreamIndex].APIKeys
	if keyIndex < 0 || keyIndex >= len(keys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	cm.config.ResponsesUpstream[upstreamIndex].APIKeys = append(keys[:keyIndex], keys[keyIndex+1:]...)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从Responses上游 [%d] %s 删除索引为 %d 的API密钥", upstreamIndex, cm.config.ResponsesUpstream[upstreamIndex].Name, keyIndex)
	return nil
}

// MoveResponsesAPIKeyToTopByIndex 通过索引将Responses渠道API密钥移到最前面
func (cm *ConfigManager) MoveResponsesAPIKeyToTopByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.ResponsesUpstream[upstreamIndex]
	if keyIndex < 0 || keyIndex >= len(upstream.APIKeys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	if keyIndex == 0 {
		return nil
	}

	key := upstream.APIKeys[keyIndex]
	upstream.APIKeys = append([]string{key}, append(upstream.APIKeys[:keyIndex], upstream.APIKeys[keyIndex+1:]...)...)
	return cm.saveConfigLocked(cm.config)
}

// MoveResponsesAPIKeyToBottomByIndex 通过索引将Responses渠道API密钥移到最后面
func (cm *ConfigManager) MoveResponsesAPIKeyToBottomByIndex(upstreamIndex int, keyIndex int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upstreamIndex < 0 || upstreamIndex >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	upstream := &cm.config.ResponsesUpstream[upstreamIndex]
	if keyIndex < 0 || keyIndex >= len(upstream.APIKeys) {
		return fmt.Errorf("invalid key index: %d", keyIndex)
	}

	if keyIndex == len(upstream.APIKeys)-1 {
		return nil
	}

	key := upstream.APIKeys[keyIndex]
	upstream.APIKeys = append(upstream.APIKeys[:keyIndex], upstream.APIKeys[keyIndex+1:]...)
	upstream.APIKeys = append(upstream.APIKeys, key)
	return cm.saveConfigLocked(cm.config)
}

// RedirectModel 模型重定向
func RedirectModel(model string, upstream *UpstreamConfig) string {
	if upstream.ModelMapping == nil || len(upstream.ModelMapping) == 0 {
		return model
	}

	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return model
	}

	baseModel, suffix, hasSuffix := utils.SplitModelSuffix(trimmedModel)
	suffix = strings.TrimSpace(suffix)

	// 直接匹配（精确匹配优先）
	// If the mapping is explicitly defined for the full input model (including suffix),
	// return it as-is (do not auto-propagate suffix).
	if mapped, ok := upstream.ModelMapping[trimmedModel]; ok {
		return mapped
	}

	// If the request model has a suffix, prefer exact mapping on the base model next.
	if hasSuffix {
		if mapped, ok := upstream.ModelMapping[baseModel]; ok {
			// Preserve suffix unless the mapped target already has one (allows explicit override).
			if suffix != "" {
				if _, _, targetHasSuffix := utils.SplitModelSuffix(mapped); !targetHasSuffix {
					return utils.ApplyModelSuffix(mapped, suffix)
				}
			}
			return mapped
		}
	}

	// 模糊匹配：按源模型长度从长到短排序，确保最长匹配优先
	// 例如：同时配置 "codex" 和 "gpt-5.1-codex" 时，"gpt-5.1-codex" 应该先匹配
	type mapping struct {
		source string
		target string
	}
	mappings := make([]mapping, 0, len(upstream.ModelMapping))
	for source, target := range upstream.ModelMapping {
		mappings = append(mappings, mapping{source, target})
	}
	// 按源模型长度降序排序
	sort.Slice(mappings, func(i, j int) bool {
		return len(mappings[i].source) > len(mappings[j].source)
	})

	matchText := trimmedModel
	if hasSuffix {
		matchText = baseModel
	}

	// 按排序后的顺序进行模糊匹配
	for _, m := range mappings {
		// Try matching against base model first (when suffix exists), but also allow
		// matches against the full original string for maximum compatibility.
		matched := strings.Contains(matchText, m.source) || strings.Contains(m.source, matchText)
		if !matched && hasSuffix {
			matched = strings.Contains(trimmedModel, m.source) || strings.Contains(m.source, trimmedModel)
		}
		if matched {
			if hasSuffix && suffix != "" {
				// Preserve suffix unless the mapped target already has one (allows explicit override).
				if _, _, targetHasSuffix := utils.SplitModelSuffix(m.target); !targetHasSuffix {
					return utils.ApplyModelSuffix(m.target, suffix)
				}
			}
			return m.target
		}
	}

	return model
}

// maskAPIKey 掩码API密钥（与 TS 版本保持一致）
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}

	length := len(key)
	if length <= 10 {
		// 短密钥：保留前3位和后2位
		if length <= 5 {
			return "***"
		}
		return key[:3] + "***" + key[length-2:]
	}

	// 长密钥：保留前8位和后5位
	return key[:8] + "***" + key[length-5:]
}

// Close 关闭配置管理器
func (cm *ConfigManager) Close() error {
	if cm.watcher != nil {
		return cm.watcher.Close()
	}
	return nil
}

// ============== Responses 接口专用方法 ==============

// GetCurrentResponsesUpstream 获取当前 Responses 上游配置
// 优先选择第一个 active 状态的渠道，若无则回退到第一个渠道
func (cm *ConfigManager) GetCurrentResponsesUpstream() (*UpstreamConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.config.ResponsesUpstream) == 0 {
		return nil, fmt.Errorf("no Responses channels configured")
	}

	// 优先选择第一个 active 状态的渠道
	for i := range cm.config.ResponsesUpstream {
		status := cm.config.ResponsesUpstream[i].Status
		if status == "" || status == "active" {
			upstream := cm.config.ResponsesUpstream[i]
			return &upstream, nil
		}
	}

	// 没有 active 渠道，回退到第一个渠道
	upstream := cm.config.ResponsesUpstream[0]
	return &upstream, nil
}

// AddResponsesUpstream 添加 Responses 上游
func (cm *ConfigManager) AddResponsesUpstream(upstream UpstreamConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	upstream.Name = strings.TrimSpace(upstream.Name)
	if upstream.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if err := cm.ensureUniqueChannelNameLocked(upstream.Name, "responses", -1); err != nil {
		return err
	}

	// Generate unique ID if not provided
	if upstream.ID == "" {
		upstream.ID = generateChannelID()
	}

	// Validate channel ID doesn't use reserved prefix
	if strings.HasPrefix(upstream.ID, "__invalid_") {
		return fmt.Errorf("channel ID cannot start with reserved prefix '__invalid_'")
	}

	// 新建渠道默认设为 active
	if upstream.Status == "" {
		upstream.Status = "active"
	}

	// Set the Index to the new position
	upstream.Index = len(cm.config.ResponsesUpstream)

	cm.config.ResponsesUpstream = append(cm.config.ResponsesUpstream, upstream)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加 Responses 上游: %s (ID: %s)", upstream.Name, upstream.ID)
	return nil
}

// UpdateResponsesUpstream 更新 Responses 上游
// 返回值：shouldResetMetrics 表示是否需要重置渠道指标（熔断状态）
func (cm *ConfigManager) UpdateResponsesUpstream(index int, updates UpstreamUpdate) (shouldResetMetrics bool, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return false, fmt.Errorf("invalid Responses upstream index: %d", index)
	}

	upstream := &cm.config.ResponsesUpstream[index]

	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return false, fmt.Errorf("channel name is required")
		}
		if err := cm.ensureUniqueChannelNameLocked(name, "responses", index); err != nil {
			return false, err
		}
		upstream.Name = name
	}
	if updates.BaseURL != nil {
		upstream.BaseURL = *updates.BaseURL
	}
	if updates.ServiceType != nil {
		upstream.ServiceType = *updates.ServiceType
	}
	if updates.Description != nil {
		upstream.Description = *updates.Description
	}
	if updates.Website != nil {
		upstream.Website = *updates.Website
	}
	if updates.APIKeys != nil {
		// 只有单 key 场景且 key 被更换时，才自动激活并重置熔断
		if len(upstream.APIKeys) == 1 && len(updates.APIKeys) == 1 &&
			upstream.APIKeys[0] != updates.APIKeys[0] {
			shouldResetMetrics = true
			if upstream.Status == "suspended" {
				upstream.Status = "active"
				log.Printf("Responses 渠道 [%d] %s 已从暂停状态自动激活（单 key 更换）", index, upstream.Name)
			}
		}
		upstream.APIKeys = updates.APIKeys
	}
	if updates.ModelMapping != nil {
		upstream.ModelMapping = updates.ModelMapping
	}
	if updates.InsecureSkipVerify != nil {
		upstream.InsecureSkipVerify = *updates.InsecureSkipVerify
	}
	if updates.Priority != nil {
		upstream.Priority = *updates.Priority
	}
	if updates.Status != nil {
		upstream.Status = *updates.Status
	}
	if updates.PromotionUntil != nil {
		upstream.PromotionUntil = updates.PromotionUntil
	}
	if updates.ResponseHeaderTimeoutSecs != nil {
		upstream.ResponseHeaderTimeoutSecs = *updates.ResponseHeaderTimeoutSecs
	}
	if updates.PriceMultipliers != nil {
		upstream.PriceMultipliers = updates.PriceMultipliers
	}
	if updates.OAuthTokens != nil {
		// OAuth tokens updated - auto-activate if suspended
		upstream.OAuthTokens = updates.OAuthTokens
		if upstream.Status == "suspended" && upstream.OAuthTokens.AccessToken != "" {
			upstream.Status = "active"
			shouldResetMetrics = true
			log.Printf("Responses 渠道 [%d] %s 已从暂停状态自动激活（OAuth tokens 更新）", index, upstream.Name)
		}
	}
	if updates.CodexServiceTierOverride != nil {
		upstream.CodexServiceTierOverride = strings.TrimSpace(*updates.CodexServiceTierOverride)
	}
	// 配额设置
	if updates.QuotaType != nil {
		upstream.QuotaType = *updates.QuotaType
		// 当 quotaType 设置为空时，清除所有其他配额字段
		if *updates.QuotaType == "" {
			upstream.QuotaLimit = 0
			upstream.QuotaResetAt = nil
			upstream.QuotaResetInterval = 0
			upstream.QuotaResetUnit = ""
			upstream.QuotaModels = nil
			upstream.QuotaResetMode = ""
		}
	}
	if updates.QuotaLimit != nil {
		upstream.QuotaLimit = *updates.QuotaLimit
	}
	if updates.QuotaResetAt != nil {
		upstream.QuotaResetAt = updates.QuotaResetAt
	}
	if updates.QuotaResetInterval != nil {
		upstream.QuotaResetInterval = *updates.QuotaResetInterval
	}
	if updates.QuotaResetUnit != nil {
		upstream.QuotaResetUnit = *updates.QuotaResetUnit
	}
	if updates.QuotaModels != nil {
		upstream.QuotaModels = updates.QuotaModels
	}
	if updates.QuotaResetMode != nil {
		upstream.QuotaResetMode = *updates.QuotaResetMode
	}
	// Per-channel rate limiting
	if updates.RateLimitRpm != nil {
		upstream.RateLimitRpm = *updates.RateLimitRpm
	}
	if updates.QueueEnabled != nil {
		upstream.QueueEnabled = *updates.QueueEnabled
	}
	if updates.QueueTimeout != nil {
		upstream.QueueTimeout = *updates.QueueTimeout
	}
	// Per-channel API key load balancing strategy
	if updates.KeyLoadBalance != nil {
		upstream.KeyLoadBalance = *updates.KeyLoadBalance
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return false, err
	}

	log.Printf("已更新 Responses 上游: [%d] %s", index, cm.config.ResponsesUpstream[index].Name)
	return shouldResetMetrics, nil
}

// UpdateChannelQuotaResetAt updates the quotaResetAt for a channel (internal use for rolling mode)
func (cm *ConfigManager) UpdateChannelQuotaResetAt(index int, isResponses bool, newTime time.Time) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if isResponses {
		if index < 0 || index >= len(cm.config.ResponsesUpstream) {
			return fmt.Errorf("invalid responses channel index: %d", index)
		}
		cm.config.ResponsesUpstream[index].QuotaResetAt = &newTime
		log.Printf("🔄 Rolling mode: Updated quotaResetAt for Responses channel [%d] to %s", index, newTime.Format(time.RFC3339))
	} else {
		if index < 0 || index >= len(cm.config.Upstream) {
			return fmt.Errorf("invalid channel index: %d", index)
		}
		cm.config.Upstream[index].QuotaResetAt = &newTime
		log.Printf("🔄 Rolling mode: Updated quotaResetAt for Messages channel [%d] to %s", index, newTime.Format(time.RFC3339))
	}

	return cm.saveConfigLocked(cm.config)
}

// UpdateGeminiChannelQuotaResetAt updates the quotaResetAt for a Gemini channel (internal use for rolling mode)
func (cm *ConfigManager) UpdateGeminiChannelQuotaResetAt(index int, newTime time.Time) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return fmt.Errorf("invalid gemini channel index: %d", index)
	}
	cm.config.GeminiUpstream[index].QuotaResetAt = &newTime
	log.Printf("🔄 Rolling mode: Updated quotaResetAt for Gemini channel [%d] to %s", index, newTime.Format(time.RFC3339))

	return cm.saveConfigLocked(cm.config)
}

// RemoveResponsesUpstream 删除 Responses 上游
func (cm *ConfigManager) RemoveResponsesUpstream(index int) (*UpstreamConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return nil, fmt.Errorf("invalid Responses upstream index: %d", index)
	}

	removed := cm.config.ResponsesUpstream[index]

	// Update messages composite mappings that reference this responses channel
	cm.updateCompositeMappingsOnDelete(removed.ID, index, CompositeTargetPoolResponses)

	cm.config.ResponsesUpstream = append(cm.config.ResponsesUpstream[:index], cm.config.ResponsesUpstream[index+1:]...)

	// Reindex remaining upstreams
	for i := range cm.config.ResponsesUpstream {
		cm.config.ResponsesUpstream[i].Index = i
	}

	// 清理被删除渠道的失败 key 冷却记录
	cm.clearFailedKeysForUpstream(&removed)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return nil, err
	}

	log.Printf("已删除 Responses 上游: %s", removed.Name)
	return &removed, nil
}

// AddResponsesAPIKey 添加 Responses 上游的 API 密钥
func (cm *ConfigManager) AddResponsesAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 检查密钥是否已存在
	for _, key := range cm.config.ResponsesUpstream[index].APIKeys {
		if key == apiKey {
			return fmt.Errorf("API key already exists")
		}
	}

	cm.config.ResponsesUpstream[index].APIKeys = append(cm.config.ResponsesUpstream[index].APIKeys, apiKey)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加API密钥到 Responses 上游 [%d] %s", index, cm.config.ResponsesUpstream[index].Name)
	return nil
}

// RemoveResponsesAPIKey 删除 Responses 上游的 API 密钥
func (cm *ConfigManager) RemoveResponsesAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 查找并删除密钥
	keys := cm.config.ResponsesUpstream[index].APIKeys
	found := false
	for i, key := range keys {
		if key == apiKey {
			cm.config.ResponsesUpstream[index].APIKeys = append(keys[:i], keys[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("API key not found")
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从 Responses 上游 [%d] %s 删除API密钥", index, cm.config.ResponsesUpstream[index].Name)
	return nil
}

// UpdateResponsesOAuthTokens 更新 Responses 渠道的 OAuth tokens
// 用于自动刷新 token 后保存新的 tokens
func (cm *ConfigManager) UpdateResponsesOAuthTokens(index int, tokens *OAuthTokens) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid Responses upstream index: %d", index)
	}

	upstream := &cm.config.ResponsesUpstream[index]
	upstream.OAuthTokens = tokens

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已更新 Responses 上游 [%d] %s 的 OAuth tokens", index, upstream.Name)
	return nil
}

// UpdateResponsesOAuthTokensByName 根据渠道名称更新 Responses 渠道的 OAuth tokens
// 用于在不知道渠道索引的情况下更新 tokens（如单渠道模式）
func (cm *ConfigManager) UpdateResponsesOAuthTokensByName(name string, tokens *OAuthTokens) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i, upstream := range cm.config.ResponsesUpstream {
		if upstream.Name == name {
			cm.config.ResponsesUpstream[i].OAuthTokens = tokens

			if err := cm.saveConfigLocked(cm.config); err != nil {
				return err
			}

			log.Printf("已更新 Responses 上游 %s 的 OAuth tokens", name)
			return nil
		}
	}

	return fmt.Errorf("no Responses channel named %s found", name)
}

// ============== 多渠道调度相关方法 ==============

// ReorderUpstreams 重新排序 Messages 渠道优先级
// order 是渠道索引数组，按新的优先级顺序排列（只更新传入的渠道，支持部分排序）
func (cm *ConfigManager) ReorderUpstreams(order []int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(order) == 0 {
		return fmt.Errorf("order list cannot be empty")
	}

	// 验证所有索引都有效且不重复
	seen := make(map[int]bool)
	for _, idx := range order {
		if idx < 0 || idx >= len(cm.config.Upstream) {
			return fmt.Errorf("invalid channel index: %d", idx)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate channel index: %d", idx)
		}
		seen[idx] = true
	}

	// 更新传入渠道的优先级（未传入的渠道保持原优先级不变）
	// 注意：priority 从 1 开始，避免 omitempty 吞掉 0 值
	for i, idx := range order {
		cm.config.Upstream[idx].Priority = i + 1
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已更新 Messages 渠道优先级顺序 (%d 个渠道)", len(order))
	return nil
}

// ReorderResponsesUpstreams 重新排序 Responses 渠道优先级
// order 是渠道索引数组，按新的优先级顺序排列（只更新传入的渠道，支持部分排序）
func (cm *ConfigManager) ReorderResponsesUpstreams(order []int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(order) == 0 {
		return fmt.Errorf("order list cannot be empty")
	}

	seen := make(map[int]bool)
	for _, idx := range order {
		if idx < 0 || idx >= len(cm.config.ResponsesUpstream) {
			return fmt.Errorf("invalid channel index: %d", idx)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate channel index: %d", idx)
		}
		seen[idx] = true
	}

	// 更新传入渠道的优先级（未传入的渠道保持原优先级不变）
	// 注意：priority 从 1 开始，避免 omitempty 吞掉 0 值
	for i, idx := range order {
		cm.config.ResponsesUpstream[idx].Priority = i + 1
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已更新 Responses 渠道优先级顺序 (%d 个渠道)", len(order))
	return nil
}

// SetChannelStatus 设置 Messages 渠道状态
func (cm *ConfigManager) SetChannelStatus(index int, status string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 状态值转为小写，支持大小写不敏感
	status = strings.ToLower(status)
	if status != "active" && status != "suspended" && status != "disabled" {
		return fmt.Errorf("invalid status: %s (allowed: active, suspended, disabled)", status)
	}

	cm.config.Upstream[index].Status = status

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置渠道 [%d] %s 状态为: %s", index, cm.config.Upstream[index].Name, status)
	return nil
}

// SetResponsesChannelStatus 设置 Responses 渠道状态
func (cm *ConfigManager) SetResponsesChannelStatus(index int, status string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 状态值转为小写，支持大小写不敏感
	status = strings.ToLower(status)
	if status != "active" && status != "suspended" && status != "disabled" {
		return fmt.Errorf("invalid status: %s (allowed: active, suspended, disabled)", status)
	}

	cm.config.ResponsesUpstream[index].Status = status

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Responses 渠道 [%d] %s 状态为: %s", index, cm.config.ResponsesUpstream[index].Name, status)
	return nil
}

// GetChannelStatus 获取渠道状态（带默认值处理）
func GetChannelStatus(upstream *UpstreamConfig) string {
	if upstream.Status == "" {
		return "active"
	}
	return upstream.Status
}

// GetChannelPriority 获取渠道优先级（带默认值处理）
func GetChannelPriority(upstream *UpstreamConfig, index int) int {
	if upstream.Priority == 0 {
		return index
	}
	return upstream.Priority
}

// SetChannelPromotion 设置渠道促销期
// duration 为促销持续时间，传入 0 表示清除促销期
func (cm *ConfigManager) SetChannelPromotion(index int, duration time.Duration) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.Upstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	if duration <= 0 {
		cm.config.Upstream[index].PromotionUntil = nil
		log.Printf("已清除渠道 [%d] %s 的促销期", index, cm.config.Upstream[index].Name)
	} else {
		// 清除其他渠道的促销期（同一时间只允许一个促销渠道）
		for i := range cm.config.Upstream {
			if i != index && cm.config.Upstream[i].PromotionUntil != nil {
				cm.config.Upstream[i].PromotionUntil = nil
			}
		}
		promotionEnd := time.Now().Add(duration)
		cm.config.Upstream[index].PromotionUntil = &promotionEnd
		log.Printf("🎉 已设置渠道 [%d] %s 进入促销期，截止: %s", index, cm.config.Upstream[index].Name, promotionEnd.Format(time.RFC3339))
	}

	return cm.saveConfigLocked(cm.config)
}

// SetResponsesChannelPromotion 设置 Responses 渠道促销期
func (cm *ConfigManager) SetResponsesChannelPromotion(index int, duration time.Duration) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ResponsesUpstream) {
		return fmt.Errorf("invalid Responses upstream index: %d", index)
	}

	if duration <= 0 {
		cm.config.ResponsesUpstream[index].PromotionUntil = nil
		log.Printf("已清除 Responses 渠道 [%d] %s 的促销期", index, cm.config.ResponsesUpstream[index].Name)
	} else {
		// 清除其他渠道的促销期（同一时间只允许一个促销渠道）
		for i := range cm.config.ResponsesUpstream {
			if i != index && cm.config.ResponsesUpstream[i].PromotionUntil != nil {
				cm.config.ResponsesUpstream[i].PromotionUntil = nil
			}
		}
		promotionEnd := time.Now().Add(duration)
		cm.config.ResponsesUpstream[index].PromotionUntil = &promotionEnd
		log.Printf("🎉 已设置 Responses 渠道 [%d] %s 进入促销期，截止: %s", index, cm.config.ResponsesUpstream[index].Name, promotionEnd.Format(time.RFC3339))
	}

	return cm.saveConfigLocked(cm.config)
}

// IsChannelInPromotion 检查渠道是否处于促销期
func IsChannelInPromotion(upstream *UpstreamConfig) bool {
	if upstream.PromotionUntil == nil {
		return false
	}
	return time.Now().Before(*upstream.PromotionUntil)
}

// IsCompositeChannel checks if the channel is a composite channel
func IsCompositeChannel(upstream *UpstreamConfig) bool {
	return upstream.ServiceType == "composite"
}

func NormalizeCompositeTargetPool(pool string) string {
	switch strings.ToLower(strings.TrimSpace(pool)) {
	case CompositeTargetPoolResponses:
		return CompositeTargetPoolResponses
	default:
		return CompositeTargetPoolMessages
	}
}

func IsValidCompositeTargetPool(pool string) bool {
	normalized := strings.ToLower(strings.TrimSpace(pool))
	return normalized == CompositeTargetPoolMessages || normalized == CompositeTargetPoolResponses
}

func normalizeCompositeTargetRef(target CompositeTargetRef, defaultPool string) CompositeTargetRef {
	pool := NormalizeCompositeTargetPool(target.Pool)
	if strings.TrimSpace(target.Pool) == "" {
		pool = NormalizeCompositeTargetPool(defaultPool)
	}
	return CompositeTargetRef{
		Pool:      pool,
		ChannelID: strings.TrimSpace(target.ChannelID),
	}
}

func getCompositePrimaryTargetRef(mapping *CompositeMapping) CompositeTargetRef {
	if mapping == nil {
		return CompositeTargetRef{
			Pool: CompositeTargetPoolMessages,
		}
	}
	return normalizeCompositeTargetRef(CompositeTargetRef{
		Pool:      mapping.TargetPool,
		ChannelID: mapping.TargetChannelID,
	}, CompositeTargetPoolMessages)
}

// GetCompositePrimaryTargetRef returns the normalized primary target for a composite mapping.
func GetCompositePrimaryTargetRef(mapping *CompositeMapping) CompositeTargetRef {
	return getCompositePrimaryTargetRef(mapping)
}

func getCompositeFailoverTargetRefs(mapping *CompositeMapping) []CompositeTargetRef {
	if mapping == nil {
		return nil
	}
	primary := getCompositePrimaryTargetRef(mapping)
	if len(mapping.FailoverTargets) > 0 {
		refs := make([]CompositeTargetRef, 0, len(mapping.FailoverTargets))
		for _, target := range mapping.FailoverTargets {
			normalized := normalizeCompositeTargetRef(target, primary.Pool)
			if normalized.ChannelID == "" {
				continue
			}
			refs = append(refs, normalized)
		}
		return refs
	}
	if len(mapping.FailoverChain) == 0 {
		return nil
	}
	refs := make([]CompositeTargetRef, 0, len(mapping.FailoverChain))
	for _, channelID := range mapping.FailoverChain {
		normalized := normalizeCompositeTargetRef(CompositeTargetRef{
			Pool:      primary.Pool,
			ChannelID: channelID,
		}, primary.Pool)
		if normalized.ChannelID == "" {
			continue
		}
		refs = append(refs, normalized)
	}
	return refs
}

// GetCompositeFailoverTargetRefs returns normalized failover targets for a composite mapping.
func GetCompositeFailoverTargetRefs(mapping *CompositeMapping) []CompositeTargetRef {
	return getCompositeFailoverTargetRefs(mapping)
}

func normalizeCompositeMappingTargets(mapping *CompositeMapping) {
	if mapping == nil {
		return
	}

	primary := getCompositePrimaryTargetRef(mapping)
	mapping.TargetPool = primary.Pool
	mapping.TargetChannelID = primary.ChannelID

	failoverRefs := getCompositeFailoverTargetRefs(mapping)
	mapping.FailoverTargets = failoverRefs

	legacyChain := make([]string, 0, len(failoverRefs))
	for _, target := range failoverRefs {
		legacyChain = append(legacyChain, target.ChannelID)
	}
	mapping.FailoverChain = legacyChain
}

type ResolvedCompositeMapping struct {
	TargetPool      string
	TargetChannelID string
	TargetIndex     int
	TargetModel     string
}

// ResolveCompositeMappingWithPools resolves a composite mapping across messages/responses pools.
// Resolution order: exact match > contains match (longest pattern first) > wildcard "*".
func ResolveCompositeMappingWithPools(
	upstream *UpstreamConfig,
	model string,
	messagesUpstreams []UpstreamConfig,
	responsesUpstreams []UpstreamConfig,
) (ResolvedCompositeMapping, bool) {
	if upstream == nil || !IsCompositeChannel(upstream) || len(upstream.CompositeMappings) == 0 {
		return ResolvedCompositeMapping{}, false
	}

	resolve := func(mapping *CompositeMapping) (ResolvedCompositeMapping, bool) {
		if mapping == nil {
			return ResolvedCompositeMapping{}, false
		}
		mappingCopy := *mapping
		normalizeCompositeMappingTargets(&mappingCopy)
		target := getCompositePrimaryTargetRef(&mappingCopy)
		if target.ChannelID == "" && mapping.TargetChannel >= 0 && mapping.TargetChannel < len(messagesUpstreams) {
			target = CompositeTargetRef{
				Pool:      CompositeTargetPoolMessages,
				ChannelID: strings.TrimSpace(messagesUpstreams[mapping.TargetChannel].ID),
			}
		}
		idx := resolveTargetIndexByRef(target, messagesUpstreams, responsesUpstreams)
		if idx < 0 && target.ChannelID == "" && mapping.TargetChannel >= 0 && mapping.TargetChannel < len(messagesUpstreams) {
			idx = mapping.TargetChannel
			target = CompositeTargetRef{
				Pool:      CompositeTargetPoolMessages,
				ChannelID: strings.TrimSpace(messagesUpstreams[idx].ID),
			}
		}
		if idx < 0 {
			return ResolvedCompositeMapping{}, false
		}

		targetModel := model
		if mappingCopy.TargetModel != "" {
			targetModel = mappingCopy.TargetModel
		}

		return ResolvedCompositeMapping{
			TargetPool:      target.Pool,
			TargetChannelID: target.ChannelID,
			TargetIndex:     idx,
			TargetModel:     targetModel,
		}, true
	}

	var wildcardMapping *CompositeMapping
	var containsMatches []*CompositeMapping
	for i := range upstream.CompositeMappings {
		mapping := &upstream.CompositeMappings[i]
		if mapping.Pattern == "*" {
			wildcardMapping = mapping
			continue
		}
		if mapping.Pattern == model {
			return resolve(mapping)
		}
		if strings.Contains(model, mapping.Pattern) {
			containsMatches = append(containsMatches, mapping)
		}
	}

	if len(containsMatches) > 0 {
		sort.Slice(containsMatches, func(i, j int) bool {
			return len(containsMatches[i].Pattern) > len(containsMatches[j].Pattern)
		})
		return resolve(containsMatches[0])
	}

	if wildcardMapping != nil {
		return resolve(wildcardMapping)
	}

	return ResolvedCompositeMapping{}, false
}

// ResolveCompositeMapping is backward-compatible single-pool resolution helper.
// For compatibility, the provided upstream list is treated as both pool lookups.
func ResolveCompositeMapping(upstream *UpstreamConfig, model string, upstreams []UpstreamConfig) (string, int, string, bool) {
	resolved, found := ResolveCompositeMappingWithPools(upstream, model, upstreams, upstreams)
	if !found {
		return "", -1, "", false
	}
	return resolved.TargetChannelID, resolved.TargetIndex, resolved.TargetModel, true
}

func resolveTargetIndexByRef(target CompositeTargetRef, messagesUpstreams []UpstreamConfig, responsesUpstreams []UpstreamConfig) int {
	pool := NormalizeCompositeTargetPool(target.Pool)
	channelID := strings.TrimSpace(target.ChannelID)
	if channelID == "" {
		return -1
	}

	var upstreams []UpstreamConfig
	switch pool {
	case CompositeTargetPoolResponses:
		upstreams = responsesUpstreams
	default:
		upstreams = messagesUpstreams
	}

	for i := range upstreams {
		if upstreams[i].ID == channelID {
			return i
		}
	}
	return -1
}

// resolveTargetIndex finds the index of the primary target channel by ID or legacy index.
// Returns -1 if not found (bounds-safe).
func resolveTargetIndex(mapping *CompositeMapping, upstreams []UpstreamConfig) int {
	if mapping == nil {
		return -1
	}
	mappingCopy := *mapping
	normalizeCompositeMappingTargets(&mappingCopy)
	primary := getCompositePrimaryTargetRef(&mappingCopy)
	return resolveTargetIndexByRef(primary, upstreams, upstreams)
}

// GetPromotedChannel 获取当前处于促销期的渠道索引（返回优先级最高的）
func (cm *ConfigManager) GetPromotedChannel() (int, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for i, upstream := range cm.config.Upstream {
		if IsChannelInPromotion(&upstream) && GetChannelStatus(&upstream) == "active" {
			return i, true
		}
	}
	return -1, false
}

// GetPromotedResponsesChannel 获取当前处于促销期的 Responses 渠道索引
func (cm *ConfigManager) GetPromotedResponsesChannel() (int, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for i, upstream := range cm.config.ResponsesUpstream {
		if IsChannelInPromotion(&upstream) && GetChannelStatus(&upstream) == "active" {
			return i, true
		}
	}
	return -1, false
}

// GetDebugLogConfig 获取调试日志配置
func (cm *ConfigManager) GetDebugLogConfig() DebugLogConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.DebugLog
}

// UpdateDebugLogConfig 更新调试日志配置
func (cm *ConfigManager) UpdateDebugLogConfig(config DebugLogConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.config.DebugLog = config
	return cm.saveConfigLocked(cm.config)
}

// GetFailoverConfig 获取故障转移配置
func (cm *ConfigManager) GetFailoverConfig() FailoverConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.Failover
}

// UpdateFailoverConfig 更新故障转移配置
func (cm *ConfigManager) UpdateFailoverConfig(config FailoverConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	normalizedRules, normalizedRuleCount := NormalizeDangerousOthersFailoverRules(config.Rules)
	if normalizedRuleCount > 0 {
		log.Printf("⚠️ 检测到 %d 条危险规则 'others + failover'，已自动转换为 'others + return_error'", normalizedRuleCount)
	}
	config.Rules = normalizedRules

	cm.config.Failover = config
	return cm.saveConfigLocked(cm.config)
}

// ==================== Gemini Upstream Methods ====================

// GetCurrentGeminiUpstream 获取当前 Gemini 上游配置
// 优先选择第一个 active 状态的渠道，若无则回退到第一个渠道
func (cm *ConfigManager) GetCurrentGeminiUpstream() (*UpstreamConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.config.GeminiUpstream) == 0 {
		return nil, fmt.Errorf("no Gemini channels configured")
	}

	// 优先选择第一个 active 状态的渠道
	for i := range cm.config.GeminiUpstream {
		status := cm.config.GeminiUpstream[i].Status
		if status == "" || status == "active" {
			upstream := cm.config.GeminiUpstream[i]
			return &upstream, nil
		}
	}

	// 没有 active 渠道，回退到第一个渠道
	upstream := cm.config.GeminiUpstream[0]
	return &upstream, nil
}

// GetGeminiUpstreams 获取所有 Gemini 渠道配置
func (cm *ConfigManager) GetGeminiUpstreams() []UpstreamConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent concurrent modification
	result := make([]UpstreamConfig, len(cm.config.GeminiUpstream))
	copy(result, cm.config.GeminiUpstream)
	return result
}

// AddGeminiUpstream 添加 Gemini 上游
func (cm *ConfigManager) AddGeminiUpstream(upstream UpstreamConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	upstream.Name = strings.TrimSpace(upstream.Name)
	if upstream.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if err := cm.ensureUniqueChannelNameLocked(upstream.Name, "gemini", -1); err != nil {
		return err
	}

	// Generate unique ID if not provided
	if upstream.ID == "" {
		upstream.ID = generateChannelID()
	}

	// Validate channel ID doesn't use reserved prefix
	if strings.HasPrefix(upstream.ID, "__invalid_") {
		return fmt.Errorf("channel ID cannot start with reserved prefix '__invalid_'")
	}

	// 新建渠道默认设为 active
	if upstream.Status == "" {
		upstream.Status = "active"
	}

	// Set the Index to the new position
	upstream.Index = len(cm.config.GeminiUpstream)

	cm.config.GeminiUpstream = append(cm.config.GeminiUpstream, upstream)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加 Gemini 上游: %s (ID: %s)", upstream.Name, upstream.ID)
	return nil
}

// UpdateGeminiUpstream 更新 Gemini 上游
// 返回值：shouldResetMetrics 表示是否需要重置渠道指标（熔断状态）
func (cm *ConfigManager) UpdateGeminiUpstream(index int, updates UpstreamUpdate) (shouldResetMetrics bool, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return false, fmt.Errorf("invalid Gemini upstream index: %d", index)
	}

	upstream := &cm.config.GeminiUpstream[index]

	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return false, fmt.Errorf("channel name is required")
		}
		if err := cm.ensureUniqueChannelNameLocked(name, "gemini", index); err != nil {
			return false, err
		}
		upstream.Name = name
	}
	if updates.BaseURL != nil {
		upstream.BaseURL = *updates.BaseURL
	}
	if updates.ServiceType != nil {
		upstream.ServiceType = *updates.ServiceType
	}
	if updates.Description != nil {
		upstream.Description = *updates.Description
	}
	if updates.Website != nil {
		upstream.Website = *updates.Website
	}
	if updates.APIKeys != nil {
		// 只有单 key 场景且 key 被更换时，才自动激活并重置熔断
		if len(upstream.APIKeys) == 1 && len(updates.APIKeys) == 1 &&
			upstream.APIKeys[0] != updates.APIKeys[0] {
			shouldResetMetrics = true
			if upstream.Status == "suspended" {
				upstream.Status = "active"
				log.Printf("Gemini 渠道 [%d] %s 已从暂停状态自动激活（单 key 更换）", index, upstream.Name)
			}
		}
		upstream.APIKeys = updates.APIKeys
	}
	if updates.ModelMapping != nil {
		upstream.ModelMapping = updates.ModelMapping
	}
	if updates.InsecureSkipVerify != nil {
		upstream.InsecureSkipVerify = *updates.InsecureSkipVerify
	}
	if updates.Priority != nil {
		upstream.Priority = *updates.Priority
	}
	if updates.Status != nil {
		upstream.Status = *updates.Status
	}
	if updates.PromotionUntil != nil {
		upstream.PromotionUntil = updates.PromotionUntil
	}
	if updates.ResponseHeaderTimeoutSecs != nil {
		upstream.ResponseHeaderTimeoutSecs = *updates.ResponseHeaderTimeoutSecs
	}
	if updates.PriceMultipliers != nil {
		upstream.PriceMultipliers = updates.PriceMultipliers
	}
	if updates.CodexServiceTierOverride != nil {
		upstream.CodexServiceTierOverride = strings.TrimSpace(*updates.CodexServiceTierOverride)
	}
	// 配额设置
	if updates.QuotaType != nil {
		upstream.QuotaType = *updates.QuotaType
		// 当 quotaType 设置为空时，清除所有其他配额字段
		if *updates.QuotaType == "" {
			upstream.QuotaLimit = 0
			upstream.QuotaResetAt = nil
			upstream.QuotaResetInterval = 0
			upstream.QuotaResetUnit = ""
			upstream.QuotaModels = nil
			upstream.QuotaResetMode = ""
		}
	}
	if updates.QuotaLimit != nil {
		upstream.QuotaLimit = *updates.QuotaLimit
	}
	if updates.QuotaResetAt != nil {
		upstream.QuotaResetAt = updates.QuotaResetAt
	}
	if updates.QuotaResetInterval != nil {
		upstream.QuotaResetInterval = *updates.QuotaResetInterval
	}
	if updates.QuotaResetUnit != nil {
		upstream.QuotaResetUnit = *updates.QuotaResetUnit
	}
	if updates.QuotaModels != nil {
		upstream.QuotaModels = updates.QuotaModels
	}
	if updates.QuotaResetMode != nil {
		upstream.QuotaResetMode = *updates.QuotaResetMode
	}
	// Per-channel rate limiting
	if updates.RateLimitRpm != nil {
		upstream.RateLimitRpm = *updates.RateLimitRpm
	}
	if updates.QueueEnabled != nil {
		upstream.QueueEnabled = *updates.QueueEnabled
	}
	if updates.QueueTimeout != nil {
		upstream.QueueTimeout = *updates.QueueTimeout
	}
	// Per-channel API key load balancing strategy
	if updates.KeyLoadBalance != nil {
		upstream.KeyLoadBalance = *updates.KeyLoadBalance
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return false, err
	}

	log.Printf("已更新 Gemini 上游: [%d] %s", index, cm.config.GeminiUpstream[index].Name)
	return shouldResetMetrics, nil
}

// RemoveGeminiUpstream 删除 Gemini 上游
func (cm *ConfigManager) RemoveGeminiUpstream(index int) (*UpstreamConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return nil, fmt.Errorf("invalid Gemini upstream index: %d", index)
	}

	removed := cm.config.GeminiUpstream[index]
	cm.config.GeminiUpstream = append(cm.config.GeminiUpstream[:index], cm.config.GeminiUpstream[index+1:]...)

	// Reindex remaining upstreams
	for i := range cm.config.GeminiUpstream {
		cm.config.GeminiUpstream[i].Index = i
	}

	// 清理被删除渠道的失败 key 冷却记录
	cm.clearFailedKeysForUpstream(&removed)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return nil, err
	}

	log.Printf("已删除 Gemini 上游: %s", removed.Name)
	return &removed, nil
}

// AddGeminiAPIKey 添加 Gemini 上游的 API 密钥
func (cm *ConfigManager) AddGeminiAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 检查密钥是否已存在
	for _, key := range cm.config.GeminiUpstream[index].APIKeys {
		if key == apiKey {
			return fmt.Errorf("API key already exists")
		}
	}

	cm.config.GeminiUpstream[index].APIKeys = append(cm.config.GeminiUpstream[index].APIKeys, apiKey)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加API密钥到 Gemini 上游 [%d] %s", index, cm.config.GeminiUpstream[index].Name)
	return nil
}

// RemoveGeminiAPIKey 删除 Gemini 上游的 API 密钥
func (cm *ConfigManager) RemoveGeminiAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 查找并删除密钥
	keys := cm.config.GeminiUpstream[index].APIKeys
	found := false
	for i, key := range keys {
		if key == apiKey {
			cm.config.GeminiUpstream[index].APIKeys = append(keys[:i], keys[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("API key not found")
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从 Gemini 上游 [%d] %s 删除API密钥", index, cm.config.GeminiUpstream[index].Name)
	return nil
}

// SetGeminiChannelStatus 设置 Gemini 渠道状态
func (cm *ConfigManager) SetGeminiChannelStatus(index int, status string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.GeminiUpstream) {
		return fmt.Errorf("invalid Gemini channel index: %d", index)
	}

	cm.config.GeminiUpstream[index].Status = status

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Gemini 渠道 [%d] %s 状态为: %s", index, cm.config.GeminiUpstream[index].Name, status)
	return nil
}

// ReorderGeminiUpstreams 重新排序 Gemini 上游
func (cm *ConfigManager) ReorderGeminiUpstreams(order []int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(order) != len(cm.config.GeminiUpstream) {
		return fmt.Errorf("order list length mismatch: expected %d, got %d", len(cm.config.GeminiUpstream), len(order))
	}

	// Validate order indices
	seen := make(map[int]bool)
	for _, idx := range order {
		if idx < 0 || idx >= len(cm.config.GeminiUpstream) {
			return fmt.Errorf("invalid channel index: %d", idx)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate channel index: %d", idx)
		}
		seen[idx] = true
	}

	// Reorder
	newUpstreams := make([]UpstreamConfig, len(order))
	for newIndex, oldIndex := range order {
		newUpstreams[newIndex] = cm.config.GeminiUpstream[oldIndex]
		newUpstreams[newIndex].Index = newIndex
	}
	cm.config.GeminiUpstream = newUpstreams

	return cm.saveConfigLocked(cm.config)
}

// GetNextGeminiAPIKey 获取下一个 Gemini API 密钥（负载均衡）
func (cm *ConfigManager) GetNextGeminiAPIKey(upstream *UpstreamConfig, failedKeys map[string]bool) (string, error) {
	// Use per-channel key load balance setting if set, otherwise fall back to global Gemini setting
	strategy := upstream.KeyLoadBalance
	if strategy == "" {
		strategy = cm.config.GeminiLoadBalance
	}
	if strategy == "" {
		strategy = "failover" // Default
	}
	return cm.getNextAPIKeyWithStrategy(upstream, failedKeys, strategy, &cm.requestCount)
}

// ==================== Chat Upstream Methods ====================

// GetCurrentChatUpstream 获取当前 Chat 上游配置
// 优先选择第一个 active 状态的渠道，若无则回退到第一个渠道
func (cm *ConfigManager) GetCurrentChatUpstream() (*UpstreamConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.config.ChatUpstream) == 0 {
		return nil, fmt.Errorf("no Chat channels configured")
	}

	// 优先选择第一个 active 状态的渠道
	for i := range cm.config.ChatUpstream {
		status := cm.config.ChatUpstream[i].Status
		if status == "" || status == "active" {
			upstream := cm.config.ChatUpstream[i]
			return &upstream, nil
		}
	}

	// 没有 active 渠道，回退到第一个渠道
	upstream := cm.config.ChatUpstream[0]
	return &upstream, nil
}

// GetChatUpstreams 获取所有 Chat 渠道配置
func (cm *ConfigManager) GetChatUpstreams() []UpstreamConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent concurrent modification
	result := make([]UpstreamConfig, len(cm.config.ChatUpstream))
	copy(result, cm.config.ChatUpstream)
	return result
}

// AddChatUpstream 添加 Chat 上游
func (cm *ConfigManager) AddChatUpstream(upstream UpstreamConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	upstream.Name = strings.TrimSpace(upstream.Name)
	if upstream.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if err := cm.ensureUniqueChannelNameLocked(upstream.Name, "chat", -1); err != nil {
		return err
	}

	// Generate unique ID if not provided
	if upstream.ID == "" {
		upstream.ID = generateChannelID()
	}

	// Validate channel ID doesn't use reserved prefix
	if strings.HasPrefix(upstream.ID, "__invalid_") {
		return fmt.Errorf("channel ID cannot start with reserved prefix '__invalid_'")
	}

	// 新建渠道默认设为 active
	if upstream.Status == "" {
		upstream.Status = "active"
	}

	// Set the Index to the new position
	upstream.Index = len(cm.config.ChatUpstream)

	cm.config.ChatUpstream = append(cm.config.ChatUpstream, upstream)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加 Chat 上游: %s (ID: %s)", upstream.Name, upstream.ID)
	return nil
}

// UpdateChatUpstream 更新 Chat 上游
// 返回值：shouldResetMetrics 表示是否需要重置渠道指标（熔断状态）
func (cm *ConfigManager) UpdateChatUpstream(index int, updates UpstreamUpdate) (shouldResetMetrics bool, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ChatUpstream) {
		return false, fmt.Errorf("invalid Chat upstream index: %d", index)
	}

	upstream := &cm.config.ChatUpstream[index]

	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return false, fmt.Errorf("channel name is required")
		}
		if err := cm.ensureUniqueChannelNameLocked(name, "chat", index); err != nil {
			return false, err
		}
		upstream.Name = name
	}
	if updates.BaseURL != nil {
		upstream.BaseURL = *updates.BaseURL
	}
	if updates.ServiceType != nil {
		upstream.ServiceType = *updates.ServiceType
	}
	if updates.Description != nil {
		upstream.Description = *updates.Description
	}
	if updates.Website != nil {
		upstream.Website = *updates.Website
	}
	if updates.APIKeys != nil {
		// 只有单 key 场景且 key 被更换时，才自动激活并重置熔断
		if len(upstream.APIKeys) == 1 && len(updates.APIKeys) == 1 &&
			upstream.APIKeys[0] != updates.APIKeys[0] {
			shouldResetMetrics = true
			if upstream.Status == "suspended" {
				upstream.Status = "active"
				log.Printf("Chat 渠道 [%d] %s 已从暂停状态自动激活（单 key 更换）", index, upstream.Name)
			}
		}
		upstream.APIKeys = updates.APIKeys
	}
	if updates.ModelMapping != nil {
		upstream.ModelMapping = updates.ModelMapping
	}
	if updates.InsecureSkipVerify != nil {
		upstream.InsecureSkipVerify = *updates.InsecureSkipVerify
	}
	if updates.Priority != nil {
		upstream.Priority = *updates.Priority
	}
	if updates.Status != nil {
		upstream.Status = *updates.Status
	}
	if updates.PromotionUntil != nil {
		upstream.PromotionUntil = updates.PromotionUntil
	}
	if updates.ResponseHeaderTimeoutSecs != nil {
		upstream.ResponseHeaderTimeoutSecs = *updates.ResponseHeaderTimeoutSecs
	}
	if updates.PriceMultipliers != nil {
		upstream.PriceMultipliers = updates.PriceMultipliers
	}
	if updates.CodexServiceTierOverride != nil {
		upstream.CodexServiceTierOverride = strings.TrimSpace(*updates.CodexServiceTierOverride)
	}
	// 配额设置
	if updates.QuotaType != nil {
		upstream.QuotaType = *updates.QuotaType
		// 当 quotaType 设置为空时，清除所有其他配额字段
		if *updates.QuotaType == "" {
			upstream.QuotaLimit = 0
			upstream.QuotaResetAt = nil
			upstream.QuotaResetInterval = 0
			upstream.QuotaResetUnit = ""
			upstream.QuotaModels = nil
			upstream.QuotaResetMode = ""
		}
	}
	if updates.QuotaLimit != nil {
		upstream.QuotaLimit = *updates.QuotaLimit
	}
	if updates.QuotaResetAt != nil {
		upstream.QuotaResetAt = updates.QuotaResetAt
	}
	if updates.QuotaResetInterval != nil {
		upstream.QuotaResetInterval = *updates.QuotaResetInterval
	}
	if updates.QuotaResetUnit != nil {
		upstream.QuotaResetUnit = *updates.QuotaResetUnit
	}
	if updates.QuotaModels != nil {
		upstream.QuotaModels = updates.QuotaModels
	}
	if updates.QuotaResetMode != nil {
		upstream.QuotaResetMode = *updates.QuotaResetMode
	}
	// Per-channel rate limiting
	if updates.RateLimitRpm != nil {
		upstream.RateLimitRpm = *updates.RateLimitRpm
	}
	if updates.QueueEnabled != nil {
		upstream.QueueEnabled = *updates.QueueEnabled
	}
	if updates.QueueTimeout != nil {
		upstream.QueueTimeout = *updates.QueueTimeout
	}
	// Per-channel API key load balancing strategy
	if updates.KeyLoadBalance != nil {
		upstream.KeyLoadBalance = *updates.KeyLoadBalance
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return false, err
	}

	log.Printf("已更新 Chat 上游: [%d] %s", index, cm.config.ChatUpstream[index].Name)
	return shouldResetMetrics, nil
}

// RemoveChatUpstream 删除 Chat 上游
func (cm *ConfigManager) RemoveChatUpstream(index int) (*UpstreamConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ChatUpstream) {
		return nil, fmt.Errorf("invalid Chat upstream index: %d", index)
	}

	removed := cm.config.ChatUpstream[index]
	cm.config.ChatUpstream = append(cm.config.ChatUpstream[:index], cm.config.ChatUpstream[index+1:]...)

	// Reindex remaining upstreams
	for i := range cm.config.ChatUpstream {
		cm.config.ChatUpstream[i].Index = i
	}

	// 清理被删除渠道的失败 key 冷却记录
	cm.clearFailedKeysForUpstream(&removed)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return nil, err
	}

	log.Printf("已删除 Chat 上游: %s", removed.Name)
	return &removed, nil
}

// AddChatAPIKey 添加 Chat 上游的 API 密钥
func (cm *ConfigManager) AddChatAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ChatUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 检查密钥是否已存在
	for _, key := range cm.config.ChatUpstream[index].APIKeys {
		if key == apiKey {
			return fmt.Errorf("API key already exists")
		}
	}

	cm.config.ChatUpstream[index].APIKeys = append(cm.config.ChatUpstream[index].APIKeys, apiKey)

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已添加API密钥到 Chat 上游 [%d] %s", index, cm.config.ChatUpstream[index].Name)
	return nil
}

// RemoveChatAPIKey 删除 Chat 上游的 API 密钥
func (cm *ConfigManager) RemoveChatAPIKey(index int, apiKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ChatUpstream) {
		return fmt.Errorf("invalid upstream index: %d", index)
	}

	// 查找并删除密钥
	keys := cm.config.ChatUpstream[index].APIKeys
	found := false
	for i, key := range keys {
		if key == apiKey {
			cm.config.ChatUpstream[index].APIKeys = append(keys[:i], keys[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("API key not found")
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已从 Chat 上游 [%d] %s 删除API密钥", index, cm.config.ChatUpstream[index].Name)
	return nil
}

// SetChatChannelStatus 设置 Chat 渠道状态
func (cm *ConfigManager) SetChatChannelStatus(index int, status string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index < 0 || index >= len(cm.config.ChatUpstream) {
		return fmt.Errorf("invalid Chat channel index: %d", index)
	}

	cm.config.ChatUpstream[index].Status = status

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Chat 渠道 [%d] %s 状态为: %s", index, cm.config.ChatUpstream[index].Name, status)
	return nil
}

// ReorderChatUpstreams 重新排序 Chat 上游
func (cm *ConfigManager) ReorderChatUpstreams(order []int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(order) != len(cm.config.ChatUpstream) {
		return fmt.Errorf("order list length mismatch: expected %d, got %d", len(cm.config.ChatUpstream), len(order))
	}

	// Validate order indices
	seen := make(map[int]bool)
	for _, idx := range order {
		if idx < 0 || idx >= len(cm.config.ChatUpstream) {
			return fmt.Errorf("invalid channel index: %d", idx)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate channel index: %d", idx)
		}
		seen[idx] = true
	}

	// Reorder
	newUpstreams := make([]UpstreamConfig, len(order))
	for newIndex, oldIndex := range order {
		newUpstreams[newIndex] = cm.config.ChatUpstream[oldIndex]
		newUpstreams[newIndex].Index = newIndex
	}
	cm.config.ChatUpstream = newUpstreams

	return cm.saveConfigLocked(cm.config)
}

// GetNextChatAPIKey 获取下一个 Chat API 密钥（负载均衡）
func (cm *ConfigManager) GetNextChatAPIKey(upstream *UpstreamConfig, failedKeys map[string]bool) (string, error) {
	// Use per-channel key load balance setting if set, otherwise fall back to global Chat setting
	strategy := upstream.KeyLoadBalance
	if strategy == "" {
		strategy = cm.config.ChatLoadBalance
	}
	if strategy == "" {
		strategy = "failover" // Default
	}
	return cm.getNextAPIKeyWithStrategy(upstream, failedKeys, strategy, &cm.requestCount)
}

// SetChatLoadBalance 设置 Chat 负载均衡策略
func (cm *ConfigManager) SetChatLoadBalance(strategy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := validateLoadBalanceStrategy(strategy); err != nil {
		return err
	}

	cm.config.ChatLoadBalance = strategy

	if err := cm.saveConfigLocked(cm.config); err != nil {
		return err
	}

	log.Printf("已设置 Chat 负载均衡策略: %s", strategy)
	return nil
}
