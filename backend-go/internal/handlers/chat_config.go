package handlers

import (
	"strconv"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// Chat Channel Configuration APIs
// CRUD operations for /v1/chat/completions channel management
// ============================================================================

// GetChatUpstreams 获取 Chat 渠道列表
func GetChatUpstreams(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		upstreams := make([]gin.H, len(cfg.ChatUpstream))
		for i, up := range cfg.ChatUpstream {
			status := config.GetChannelStatus(&up)
			priority := config.GetChannelPriority(&up, i)

			upstreams[i] = gin.H{
				"index":                 i,
				"id":                    up.ID,
				"name":                  up.Name,
				"serviceType":           up.ServiceType,
				"baseUrl":               up.BaseURL,
				"apiKeyCount":           len(up.APIKeys),
				"maskedKeys":            buildMaskedKeys(up.APIKeys),
				"description":           up.Description,
				"website":               up.Website,
				"insecureSkipVerify":    up.InsecureSkipVerify,
				"responseHeaderTimeout": up.ResponseHeaderTimeoutSecs,
				"modelMapping":          up.ModelMapping,
				"priceMultipliers":      up.PriceMultipliers,
				"latency":               nil,
				"status":                status,
				"priority":              priority,
				"quotaType":             up.QuotaType,
				"quotaLimit":            up.QuotaLimit,
				"quotaResetAt":          up.QuotaResetAt,
				"quotaResetInterval":    up.QuotaResetInterval,
				"quotaResetUnit":        up.QuotaResetUnit,
				"quotaModels":           up.QuotaModels,
				"quotaResetMode":        up.QuotaResetMode,
				"rateLimitRpm":          up.RateLimitRpm,
				"queueEnabled":          up.QueueEnabled,
				"queueTimeout":          up.QueueTimeout,
				"keyLoadBalance":        up.KeyLoadBalance,
			}
		}

		c.JSON(200, gin.H{
			"channels":    upstreams,
			"current":     -1, // Chat doesn't use current channel concept
			"loadBalance": cfg.ChatLoadBalance,
		})
	}
}

// AddChatUpstream 添加 Chat 渠道
func AddChatUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name                      string            `json:"name"`
			BaseURL                   string            `json:"baseUrl"`
			ServiceType               string            `json:"serviceType"`
			APIKeys                   []string          `json:"apiKeys"`
			Description               string            `json:"description"`
			Website                   string            `json:"website"`
			InsecureSkipVerify        bool              `json:"insecureSkipVerify"`
			ResponseHeaderTimeoutSecs int               `json:"responseHeaderTimeout"`
			ModelMapping              map[string]string `json:"modelMapping"`
			Priority                  int               `json:"priority"`
			RateLimitRpm              int               `json:"rateLimitRpm"`
			QueueEnabled              bool              `json:"queueEnabled"`
			QueueTimeout              int               `json:"queueTimeout"`
			// Usage quota settings
			QuotaType          string     `json:"quotaType"`
			QuotaLimit         float64    `json:"quotaLimit"`
			QuotaResetAt       *time.Time `json:"quotaResetAt"`
			QuotaResetInterval int        `json:"quotaResetInterval"`
			QuotaResetUnit     string     `json:"quotaResetUnit"`
			QuotaModels        []string   `json:"quotaModels"`
			QuotaResetMode     string     `json:"quotaResetMode"`
			// Per-channel API key load balancing
			KeyLoadBalance string `json:"keyLoadBalance"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		// Validate required fields
		if req.Name == "" {
			c.JSON(400, gin.H{"error": "Channel name is required"})
			return
		}
		if req.BaseURL == "" {
			c.JSON(400, gin.H{"error": "Base URL is required"})
			return
		}

		// Default service type to "openai" for Chat channels (passthrough)
		if req.ServiceType == "" {
			req.ServiceType = "openai"
		}

		upstream := config.UpstreamConfig{
			Name:                      req.Name,
			BaseURL:                   req.BaseURL,
			ServiceType:               req.ServiceType,
			APIKeys:                   req.APIKeys,
			Description:               req.Description,
			Website:                   req.Website,
			InsecureSkipVerify:        req.InsecureSkipVerify,
			ResponseHeaderTimeoutSecs: req.ResponseHeaderTimeoutSecs,
			ModelMapping:              req.ModelMapping,
			Priority:                  req.Priority,
			QuotaType:                 req.QuotaType,
			QuotaLimit:                req.QuotaLimit,
			QuotaResetAt:              req.QuotaResetAt,
			QuotaResetInterval:        req.QuotaResetInterval,
			QuotaResetUnit:            req.QuotaResetUnit,
			QuotaModels:               req.QuotaModels,
			QuotaResetMode:            req.QuotaResetMode,
			RateLimitRpm:              req.RateLimitRpm,
			QueueEnabled:              req.QueueEnabled,
			QueueTimeout:              req.QueueTimeout,
			KeyLoadBalance:            req.KeyLoadBalance,
		}

		if err := cfgManager.AddChatUpstream(upstream); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Chat channel added successfully"})
	}
}

// UpdateChatUpstream 更新 Chat 渠道
func UpdateChatUpstream(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		var updates config.UpstreamUpdate
		if err := c.ShouldBindJSON(&updates); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		shouldResetMetrics, err := cfgManager.UpdateChatUpstream(index, updates)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Reset metrics if needed
		if shouldResetMetrics && channelScheduler != nil {
			channelScheduler.ResetChatChannelMetrics(index)
		}

		c.JSON(200, gin.H{"message": "Chat channel updated successfully"})
	}
}

// DeleteChatUpstream 删除 Chat 渠道
func DeleteChatUpstream(cfgManager *config.ConfigManager, channelRateLimiter *middleware.ChannelRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		removed, err := cfgManager.RemoveChatUpstream(index)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Cleanup rate limiter for removed channel
		if channelRateLimiter != nil && removed != nil {
			channelRateLimiter.ClearChannel(removed.Index)
		}

		c.JSON(200, gin.H{"message": "Chat channel deleted successfully"})
	}
}

// AddChatApiKey 添加 Chat 渠道的 API 密钥
func AddChatApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if req.APIKey == "" {
			c.JSON(400, gin.H{"error": "API key is required"})
			return
		}

		if err := cfgManager.AddChatAPIKey(index, req.APIKey); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "API key added successfully"})
	}
}

// DeleteChatApiKeyByIndex 删除 Chat 渠道的 API 密钥（按索引）
func DeleteChatApiKeyByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		keyIndexStr := c.Param("keyIndex")
		keyIndex, err := strconv.Atoi(keyIndexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		// Get the API key by index
		upstreams := cfgManager.GetChatUpstreams()
		if index < 0 || index >= len(upstreams) {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		if keyIndex < 0 || keyIndex >= len(upstreams[index].APIKeys) {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		apiKey := upstreams[index].APIKeys[keyIndex]

		if err := cfgManager.RemoveChatAPIKey(index, apiKey); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "API key deleted successfully"})
	}
}

// SetChatChannelStatus 设置 Chat 渠道状态
func SetChatChannelStatus(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		var req struct {
			Status string `json:"status"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if req.Status == "" {
			c.JSON(400, gin.H{"error": "Status is required"})
			return
		}

		if err := cfgManager.SetChatChannelStatus(index, req.Status); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Channel status updated successfully"})
	}
}

// ReorderChatChannels 重新排序 Chat 渠道
func ReorderChatChannels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Order []int `json:"order"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.ReorderChatUpstreams(req.Order); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Channels reordered successfully"})
	}
}

// GetChatChannelMetrics 获取 Chat 渠道指标
func GetChatChannelMetrics(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) gin.HandlerFunc {
	return getChannelMetricsByType(channelScheduler.GetChatMetricsManager(), cfgManager, metricsChannelTypeChat)
}

// SetChatLoadBalance 设置 Chat 负载均衡策略
func SetChatLoadBalance(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Strategy string `json:"strategy"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if req.Strategy == "" {
			c.JSON(400, gin.H{"error": "Strategy is required"})
			return
		}

		if err := cfgManager.SetChatLoadBalance(req.Strategy); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Load balance strategy updated successfully"})
	}
}
