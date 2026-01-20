package handlers

import (
	"strconv"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/metrics"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/gin-gonic/gin"
)

// GetGeminiUpstreams 获取 Gemini 渠道列表
func GetGeminiUpstreams(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		upstreams := make([]gin.H, len(cfg.GeminiUpstream))
		for i, up := range cfg.GeminiUpstream {
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
				"rateLimitRpm":          up.RateLimitRpm,
				"queueEnabled":          up.QueueEnabled,
				"queueTimeout":          up.QueueTimeout,
			}
		}

		c.JSON(200, gin.H{
			"channels":    upstreams,
			"current":     -1, // Gemini doesn't use current channel concept
			"loadBalance": cfg.GeminiLoadBalance,
		})
	}
}

// AddGeminiUpstream 添加 Gemini 渠道
func AddGeminiUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
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

		// Default service type to "gemini" for Gemini channels
		if req.ServiceType == "" {
			req.ServiceType = "gemini"
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
			RateLimitRpm:              req.RateLimitRpm,
			QueueEnabled:              req.QueueEnabled,
			QueueTimeout:              req.QueueTimeout,
		}

		if err := cfgManager.AddGeminiUpstream(upstream); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Gemini channel added successfully"})
	}
}

// UpdateGeminiUpstream 更新 Gemini 渠道
func UpdateGeminiUpstream(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) gin.HandlerFunc {
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

		shouldResetMetrics, err := cfgManager.UpdateGeminiUpstream(index, updates)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Reset metrics if needed
		if shouldResetMetrics && channelScheduler != nil {
			channelScheduler.ResetGeminiChannelMetrics(index)
		}

		c.JSON(200, gin.H{"message": "Gemini channel updated successfully"})
	}
}

// DeleteGeminiUpstream 删除 Gemini 渠道
func DeleteGeminiUpstream(cfgManager *config.ConfigManager, channelRateLimiter *middleware.ChannelRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		indexStr := c.Param("id")
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		removed, err := cfgManager.RemoveGeminiUpstream(index)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Cleanup rate limiter for removed channel
		if channelRateLimiter != nil && removed != nil {
			channelRateLimiter.ClearChannel(removed.Index)
		}

		c.JSON(200, gin.H{"message": "Gemini channel deleted successfully"})
	}
}

// AddGeminiApiKey 添加 Gemini 渠道的 API 密钥
func AddGeminiApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
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

		if err := cfgManager.AddGeminiAPIKey(index, req.APIKey); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "API key added successfully"})
	}
}

// DeleteGeminiApiKeyByIndex 删除 Gemini 渠道的 API 密钥（按索引）
func DeleteGeminiApiKeyByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
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
		upstreams := cfgManager.GetGeminiUpstreams()
		if index < 0 || index >= len(upstreams) {
			c.JSON(400, gin.H{"error": "Invalid channel index"})
			return
		}

		if keyIndex < 0 || keyIndex >= len(upstreams[index].APIKeys) {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		apiKey := upstreams[index].APIKeys[keyIndex]

		if err := cfgManager.RemoveGeminiAPIKey(index, apiKey); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "API key deleted successfully"})
	}
}

// SetGeminiChannelStatus 设置 Gemini 渠道状态
func SetGeminiChannelStatus(cfgManager *config.ConfigManager) gin.HandlerFunc {
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

		if err := cfgManager.SetGeminiChannelStatus(index, req.Status); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Channel status updated successfully"})
	}
}

// ReorderGeminiChannels 重新排序 Gemini 渠道
func ReorderGeminiChannels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Order []int `json:"order"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.ReorderGeminiUpstreams(req.Order); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Channels reordered successfully"})
	}
}

// GetGeminiChannelMetrics 获取 Gemini 渠道指标
func GetGeminiChannelMetrics(metricsManager *metrics.MetricsManager) gin.HandlerFunc {
	return GetChannelMetrics(metricsManager)
}
