package handlers

import (
	"net/http" // 新增
	"strconv"
	"strings"
	"sync" // 新增
	"time" // 新增

	"github.com/JillVernus/cc-bridge/internal/auth/codex"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/httpclient" // 新增
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/gin-gonic/gin"
)

// maskAPIKeys 掩码 API 密钥列表（使用 proxy.go 中的 maskAPIKey）
func maskAPIKeys(keys []string) []string {
	masked := make([]string, len(keys))
	for i, key := range keys {
		masked[i] = maskAPIKey(key)
	}
	return masked
}

// GetUpstreams 获取上游列表 (兼容前端 channels 字段名)
func GetUpstreams(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		// 为每个upstream添加index字段
		upstreams := make([]gin.H, len(cfg.Upstream))
		for i, up := range cfg.Upstream {
			// 获取带默认值的 status 和 priority
			status := config.GetChannelStatus(&up)
			priority := config.GetChannelPriority(&up, i)

			upstreams[i] = gin.H{
				"index":                 i,
				"name":                  up.Name,
				"serviceType":           up.ServiceType,
				"baseUrl":               up.BaseURL,
				"apiKeyCount":           len(up.APIKeys),
				"description":           up.Description,
				"website":               up.Website,
				"insecureSkipVerify":    up.InsecureSkipVerify,
				"responseHeaderTimeout": up.ResponseHeaderTimeoutSecs,
				"modelMapping":          up.ModelMapping,
				"priceMultipliers":      up.PriceMultipliers,
				"latency":               nil,
				"status":                status,
				"priority":              priority,
			}
		}

		c.JSON(200, gin.H{
			"channels":    upstreams,
			"loadBalance": cfg.LoadBalance,
		})
	}
}

// AddUpstream 添加上游
func AddUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var upstream config.UpstreamConfig
		if err := c.ShouldBindJSON(&upstream); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.AddUpstream(upstream); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save config"})
			return
		}

		// 🔒 安全修复: 不返回 upstream 数据，防止 API 密钥泄露
		c.JSON(200, gin.H{
			"success": true,
			"message": "上游已添加",
		})
	}
}

// UpdateUpstream 更新上游
// sch 用于在单 key 更换时重置熔断状态
func UpdateUpstream(cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		var updates config.UpstreamUpdate
		if err := c.ShouldBindJSON(&updates); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		shouldResetMetrics, err := cfgManager.UpdateUpstream(id, updates)
		if err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		// 单 key 更换时重置熔断状态
		if shouldResetMetrics {
			sch.ResetChannelMetrics(id, false)
		}

		// 🔒 安全修复: 不返回 upstream 数据，防止 API 密钥泄露
		c.JSON(200, gin.H{
			"success": true,
			"message": "上游已更新",
		})
	}
}

// DeleteUpstream 删除上游
func DeleteUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		removed, err := cfgManager.RemoveUpstream(id)
		if err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		// 🔒 安全修复: 不返回 removed 数据，防止 API 密钥泄露
		_ = removed // 忽略返回值，仅用于确认删除成功
		c.JSON(200, gin.H{
			"success": true,
			"message": "上游已删除",
		})
	}
}

// AddApiKey 添加 API 密钥
func AddApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.AddAPIKey(id, req.APIKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥已存在") {
				c.JSON(400, gin.H{"error": "API密钥已存在"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message": "API密钥已添加",
			"success": true,
		})
	}
}

// DeleteApiKey 删除 API 密钥 (支持URL路径参数)
// Deprecated: Use DeleteApiKeyByIndex instead to avoid exposing keys in URLs
func DeleteApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		// 从URL路径参数获取apiKey
		apiKey := c.Param("apiKey")
		if apiKey == "" {
			c.JSON(400, gin.H{"error": "API key is required"})
			return
		}

		if err := cfgManager.RemoveAPIKey(id, apiKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥不存在") {
				c.JSON(404, gin.H{"error": "API key not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message": "API密钥已删除",
		})
	}
}

// DeleteApiKeyByIndex 通过索引删除 API 密钥（安全：不在URL中暴露密钥）
func DeleteApiKeyByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.RemoveAPIKeyByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已删除", "success": true})
	}
}

// MoveApiKeyToTopByIndex 通过索引将 API 密钥移到最前面（安全：不在URL中暴露密钥）
func MoveApiKeyToTopByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.MoveAPIKeyToTopByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已置顶", "success": true})
	}
}

// MoveApiKeyToBottomByIndex 通过索引将 API 密钥移到最后面（安全：不在URL中暴露密钥）
func MoveApiKeyToBottomByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.MoveAPIKeyToBottomByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已置底", "success": true})
	}
}

// DeleteResponsesApiKeyByIndex 通过索引删除 Responses 渠道 API 密钥（安全：不在URL中暴露密钥）
func DeleteResponsesApiKeyByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.RemoveResponsesAPIKeyByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已删除", "success": true})
	}
}

// MoveResponsesApiKeyToTopByIndex 通过索引将 Responses 渠道 API 密钥移到最前面
func MoveResponsesApiKeyToTopByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.MoveResponsesAPIKeyToTopByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已置顶", "success": true})
	}
}

// MoveResponsesApiKeyToBottomByIndex 通过索引将 Responses 渠道 API 密钥移到最后面
func MoveResponsesApiKeyToBottomByIndex(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		keyIndex, err := strconv.Atoi(c.Param("keyIndex"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid key index"})
			return
		}

		if err := cfgManager.MoveResponsesAPIKeyToBottomByIndex(channelID, keyIndex); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else if strings.Contains(err.Error(), "无效的密钥索引") {
				c.JSON(404, gin.H{"error": "Key index not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{"message": "API密钥已置底", "success": true})
	}
}

// UpdateLoadBalance 更新负载均衡策略
func UpdateLoadBalance(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Strategy string `json:"strategy"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.SetLoadBalance(req.Strategy); err != nil {
			if strings.Contains(err.Error(), "无效的负载均衡策略") {
				c.JSON(400, gin.H{"error": err.Error()})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message":  "负载均衡策略已更新",
			"strategy": req.Strategy,
		})
	}
}

// UpdateResponsesLoadBalance 更新 Responses 负载均衡策略
func UpdateResponsesLoadBalance(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Strategy string `json:"strategy"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.SetResponsesLoadBalance(req.Strategy); err != nil {
			if strings.Contains(err.Error(), "无效的负载均衡策略") {
				c.JSON(400, gin.H{"error": err.Error()})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message":  "Responses 负载均衡策略已更新",
			"strategy": req.Strategy,
		})
	}
}

// PingChannel Ping单个渠道
func PingChannel(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
			return
		}

		config := cfgManager.GetConfig()
		if id < 0 || id >= len(config.Upstream) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		channel := config.Upstream[id]
		startTime := time.Now()

		// 简化测试：只检查连通性，不关心HTTP状态码
		testURL := strings.TrimSuffix(channel.BaseURL, "/")

		client := httpclient.GetManager().GetStandardClient(5*time.Second, channel.InsecureSkipVerify, 0)
		req, err := http.NewRequest("HEAD", testURL, nil)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "status": "error", "error": "Failed to create request"})
			return
		}

		resp, err := client.Do(req)
		latency := time.Since(startTime).Milliseconds()

		if err != nil {
			// 网络连接失败
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"latency": latency,
				"status":  "error",
				"error":   err.Error(),
			})
			return
		}
		defer resp.Body.Close()

		// 只要能完成请求就算成功，不检查HTTP状态码
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"latency": latency,
			"status":  "healthy",
		})
	}
}

// PingAllChannels Ping所有渠道
func PingAllChannels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()
		results := make(chan gin.H)
		var wg sync.WaitGroup

		for i, channel := range cfg.Upstream {
			wg.Add(1)
			go func(id int, ch config.UpstreamConfig) {
				defer wg.Done()

				startTime := time.Now()
				// 简化测试：只检查连通性，不关心HTTP状态码
				testURL := strings.TrimSuffix(ch.BaseURL, "/")

				client := httpclient.GetManager().GetStandardClient(5*time.Second, ch.InsecureSkipVerify, 0)
				req, err := http.NewRequest("HEAD", testURL, nil)
				if err != nil {
					results <- gin.H{"id": id, "name": ch.Name, "latency": 0, "status": "error", "error": "req_creation_failed"}
					return
				}

				resp, err := client.Do(req)
				latency := time.Since(startTime).Milliseconds()

				if err != nil {
					// 网络连接失败
					results <- gin.H{"id": id, "name": ch.Name, "latency": latency, "status": "error", "error": err.Error()}
					return
				}
				defer resp.Body.Close()

				// 只要能完成请求就算成功，不检查HTTP状态码
				results <- gin.H{"id": id, "name": ch.Name, "latency": latency, "status": "healthy"}
			}(i, channel)
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		var finalResults []gin.H
		for res := range results {
			finalResults = append(finalResults, res)
		}

		c.JSON(http.StatusOK, finalResults)
	}
}

// ============== Responses 渠道管理 API ==============

// GetResponsesUpstreams 获取 Responses 上游列表
func GetResponsesUpstreams(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := cfgManager.GetConfig()

		upstreams := make([]gin.H, len(cfg.ResponsesUpstream))
		for i, up := range cfg.ResponsesUpstream {
			// 获取带默认值的 status 和 priority
			status := config.GetChannelStatus(&up)
			priority := config.GetChannelPriority(&up, i)

			upstreams[i] = gin.H{
				"index":                 i,
				"name":                  up.Name,
				"serviceType":           up.ServiceType,
				"baseUrl":               up.BaseURL,
				"apiKeyCount":           len(up.APIKeys),
				"description":           up.Description,
				"website":               up.Website,
				"insecureSkipVerify":    up.InsecureSkipVerify,
				"responseHeaderTimeout": up.ResponseHeaderTimeoutSecs,
				"modelMapping":          up.ModelMapping,
				"priceMultipliers":      up.PriceMultipliers,
				"latency":               nil,
				"status":                status,
				"priority":              priority,
			}
		}

		c.JSON(200, gin.H{
			"channels":    upstreams,
			"loadBalance": cfg.ResponsesLoadBalance,
		})
	}
}

// AddResponsesUpstream 添加 Responses 上游
func AddResponsesUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var upstream config.UpstreamConfig
		if err := c.ShouldBindJSON(&upstream); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		if err := cfgManager.AddResponsesUpstream(upstream); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Responses upstream added successfully"})
	}
}

// UpdateResponsesUpstream 更新 Responses 上游
// sch 用于在单 key 更换时重置熔断状态
func UpdateResponsesUpstream(cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		var updates config.UpstreamUpdate
		if err := c.ShouldBindJSON(&updates); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		shouldResetMetrics, err := cfgManager.UpdateResponsesUpstream(id, updates)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// 单 key 更换时重置熔断状态
		if shouldResetMetrics {
			sch.ResetChannelMetrics(id, true)
		}

		c.JSON(200, gin.H{"message": "Responses upstream updated successfully"})
	}
}

// DeleteResponsesUpstream 删除 Responses 上游
func DeleteResponsesUpstream(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		if _, err := cfgManager.RemoveResponsesUpstream(id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Responses upstream deleted successfully"})
	}
}

// AddResponsesApiKey 添加 Responses 渠道 API 密钥
func AddResponsesApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.AddResponsesAPIKey(id, req.APIKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥已存在") {
				c.JSON(400, gin.H{"error": "API密钥已存在"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message": "API密钥已添加",
			"success": true,
		})
	}
}

// DeleteResponsesApiKey 删除 Responses 渠道 API 密钥
func DeleteResponsesApiKey(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid upstream ID"})
			return
		}

		apiKey := c.Param("apiKey")
		if apiKey == "" {
			c.JSON(400, gin.H{"error": "API key is required"})
			return
		}

		if err := cfgManager.RemoveResponsesAPIKey(id, apiKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥不存在") {
				c.JSON(404, gin.H{"error": "API key not found"})
			} else {
				c.JSON(500, gin.H{"error": "Failed to save config"})
			}
			return
		}

		c.JSON(200, gin.H{
			"message": "API密钥已删除",
		})
	}
}

// MoveApiKeyToTop 将 API 密钥移到最前面
func MoveApiKeyToTop(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}
		apiKey := c.Param("apiKey")

		if err := cfgManager.MoveAPIKeyToTop(id, apiKey); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"message": "API密钥已置顶"})
	}
}

// MoveApiKeyToBottom 将 API 密钥移到最后面
func MoveApiKeyToBottom(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}
		apiKey := c.Param("apiKey")

		if err := cfgManager.MoveAPIKeyToBottom(id, apiKey); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"message": "API密钥已置底"})
	}
}

// MoveResponsesApiKeyToTop 将 Responses 渠道 API 密钥移到最前面
func MoveResponsesApiKeyToTop(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}
		apiKey := c.Param("apiKey")

		if err := cfgManager.MoveResponsesAPIKeyToTop(id, apiKey); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"message": "API密钥已置顶"})
	}
}

// MoveResponsesApiKeyToBottom 将 Responses 渠道 API 密钥移到最后面
func MoveResponsesApiKeyToBottom(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}
		apiKey := c.Param("apiKey")

		if err := cfgManager.MoveResponsesAPIKeyToBottom(id, apiKey); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"message": "API密钥已置底"})
	}
}

// ============== 多渠道调度 API ==============

// ReorderChannels 重新排序渠道优先级
func ReorderChannels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Order []int `json:"order"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.ReorderUpstreams(req.Order); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"success": true,
			"message": "渠道优先级已更新",
		})
	}
}

// ReorderResponsesChannels 重新排序 Responses 渠道优先级
func ReorderResponsesChannels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Order []int `json:"order"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.ReorderResponsesUpstreams(req.Order); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"success": true,
			"message": "Responses 渠道优先级已更新",
		})
	}
}

// SetChannelStatus 设置渠道状态
func SetChannelStatus(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		var req struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.SetChannelStatus(id, req.Status); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else {
				c.JSON(400, gin.H{"error": err.Error()})
			}
			return
		}

		c.JSON(200, gin.H{
			"success": true,
			"message": "渠道状态已更新",
			"status":  req.Status,
		})
	}
}

// SetResponsesChannelStatus 设置 Responses 渠道状态
func SetResponsesChannelStatus(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid channel ID"})
			return
		}

		var req struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body"})
			return
		}

		if err := cfgManager.SetResponsesChannelStatus(id, req.Status); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				c.JSON(404, gin.H{"error": "Channel not found"})
			} else {
				c.JSON(400, gin.H{"error": err.Error()})
			}
			return
		}

		c.JSON(200, gin.H{
			"success": true,
			"message": "Responses 渠道状态已更新",
			"status":  req.Status,
		})
	}
}

// GetResponsesChannelOAuthStatus returns the OAuth status for a Responses channel
// This endpoint extracts subscription/plan metadata from the id_token without exposing raw tokens
func GetResponsesChannelOAuthStatus(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.ResponsesUpstream) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		upstream := cfg.ResponsesUpstream[id]

		// Only openai-oauth channels have OAuth tokens
		if upstream.ServiceType != "openai-oauth" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       "Channel is not an OAuth channel",
				"serviceType": upstream.ServiceType,
			})
			return
		}

		// Check if OAuth tokens are configured
		if upstream.OAuthTokens == nil {
			c.JSON(http.StatusOK, gin.H{
				"channelId":   id,
				"channelName": upstream.Name,
				"serviceType": upstream.ServiceType,
				"configured":  false,
				"message":     "OAuth tokens not configured",
			})
			return
		}

		// Parse OAuth status from tokens
		status, err := codex.ParseOAuthStatus(
			upstream.OAuthTokens.IDToken,
			upstream.OAuthTokens.AccessToken,
			upstream.OAuthTokens.LastRefresh,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to parse OAuth status",
			})
			return
		}

		// Build response with channel info and OAuth status
		response := gin.H{
			"channelId":   id,
			"channelName": upstream.Name,
			"serviceType": upstream.ServiceType,
			"configured":  true,
			"status":      status,
		}

		// Add token health indicators
		if status.TokenExpiresAt != nil {
			now := time.Now()
			if status.TokenExpiresAt.Before(now) {
				response["tokenStatus"] = "expired"
			} else if status.TokenExpiresAt.Before(now.Add(5 * time.Minute)) {
				response["tokenStatus"] = "expiring_soon"
			} else {
				response["tokenStatus"] = "valid"
			}
			response["tokenExpiresIn"] = int(status.TokenExpiresAt.Sub(now).Seconds())
		}

		// Add quota/rate limit information
		if quotaStatus := quota.GetManager().GetStatus(id); quotaStatus != nil {
			quotaInfo := gin.H{}

			// Codex-specific quota (primary/secondary windows)
			if quotaStatus.CodexQuota != nil {
				cq := quotaStatus.CodexQuota
				codexQuotaInfo := gin.H{
					"plan_type":              cq.PlanType,
					"primary_used_percent":   cq.PrimaryUsedPercent,
					"secondary_used_percent": cq.SecondaryUsedPercent,
					"updated_at":             cq.UpdatedAt,
				}
				if cq.PrimaryWindowMinutes > 0 {
					codexQuotaInfo["primary_window_minutes"] = cq.PrimaryWindowMinutes
				}
				if !cq.PrimaryResetAt.IsZero() {
					codexQuotaInfo["primary_reset_at"] = cq.PrimaryResetAt
				}
				if cq.SecondaryWindowMinutes > 0 {
					codexQuotaInfo["secondary_window_minutes"] = cq.SecondaryWindowMinutes
				}
				if !cq.SecondaryResetAt.IsZero() {
					codexQuotaInfo["secondary_reset_at"] = cq.SecondaryResetAt
				}
				if cq.PrimaryOverSecondaryLimitPercent > 0 {
					codexQuotaInfo["primary_over_secondary_limit_percent"] = cq.PrimaryOverSecondaryLimitPercent
				}
				codexQuotaInfo["credits_has_credits"] = cq.CreditsHasCredits
				codexQuotaInfo["credits_unlimited"] = cq.CreditsUnlimited
				if cq.CreditsBalance != "" {
					codexQuotaInfo["credits_balance"] = cq.CreditsBalance
				}
				quotaInfo["codex_quota"] = codexQuotaInfo
			}

			// Standard OpenAI rate limits
			if quotaStatus.RateLimit != nil {
				rl := quotaStatus.RateLimit
				rateLimitInfo := gin.H{
					"updated_at": rl.UpdatedAt,
				}
				if rl.LimitRequests > 0 {
					rateLimitInfo["limit_requests"] = rl.LimitRequests
					rateLimitInfo["remaining_requests"] = rl.RemainingRequests
					if !rl.ResetRequests.IsZero() {
						rateLimitInfo["reset_requests"] = rl.ResetRequests
					}
				}
				if rl.LimitTokens > 0 {
					rateLimitInfo["limit_tokens"] = rl.LimitTokens
					rateLimitInfo["remaining_tokens"] = rl.RemainingTokens
					if !rl.ResetTokens.IsZero() {
						rateLimitInfo["reset_tokens"] = rl.ResetTokens
					}
				}
				quotaInfo["rate_limit"] = rateLimitInfo
			}

			if quotaStatus.IsExceeded {
				quotaInfo["is_exceeded"] = true
				quotaInfo["exceeded_at"] = quotaStatus.ExceededAt
				quotaInfo["recover_at"] = quotaStatus.RecoverAt
				if quotaStatus.ExceededReason != "" {
					quotaInfo["exceeded_reason"] = quotaStatus.ExceededReason
				}
			}

			if len(quotaInfo) > 0 {
				response["quota"] = quotaInfo
			}
		}

		c.JSON(http.StatusOK, response)
	}
}
