package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/gin-gonic/gin"
)

// RequestLogHandler 请求日志处理器
type RequestLogHandler struct {
	manager *requestlog.Manager
}

// NewRequestLogHandler 创建请求日志处理器
func NewRequestLogHandler(manager *requestlog.Manager) *RequestLogHandler {
	return &RequestLogHandler{manager: manager}
}

// GetLogs 获取请求日志列表
func (h *RequestLogHandler) GetLogs(c *gin.Context) {
	filter := &requestlog.RequestLogFilter{}

	// 解析查询参数
	if provider := c.Query("provider"); provider != "" {
		filter.Provider = provider
	}
	if model := c.Query("model"); model != "" {
		filter.Model = model
	}
	if endpoint := c.Query("endpoint"); endpoint != "" {
		filter.Endpoint = endpoint
	}
	if clientID := c.Query("clientId"); clientID != "" {
		filter.ClientID = clientID
	}
	if sessionID := c.Query("sessionId"); sessionID != "" {
		filter.SessionID = sessionID
	}
	if status := c.Query("httpStatus"); status != "" {
		if s, err := strconv.Atoi(status); err == nil {
			filter.HTTPStatus = s
		}
	}
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if offset := c.Query("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &t
		}
	}

	result, err := h.manager.GetRecent(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetStats 获取统计信息
func (h *RequestLogHandler) GetStats(c *gin.Context) {
	filter := &requestlog.RequestLogFilter{}

	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &t
		}
	}
	if clientID := c.Query("clientId"); clientID != "" {
		filter.ClientID = clientID
	}
	if sessionID := c.Query("sessionId"); sessionID != "" {
		filter.SessionID = sessionID
	}

	stats, err := h.manager.GetStats(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// ClearLogs 清空所有日志
func (h *RequestLogHandler) ClearLogs(c *gin.Context) {
	if err := h.manager.ClearAll(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "All logs cleared"})
}

// GetLogByID 获取单条日志
func (h *RequestLogHandler) GetLogByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	record, err := h.manager.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	c.JSON(http.StatusOK, record)
}

// GetActiveSessions 获取活跃会话列表
func (h *RequestLogHandler) GetActiveSessions(c *gin.Context) {
	// Parse threshold parameter (default 30m)
	threshold := 30 * time.Minute
	if thresholdStr := c.Query("threshold"); thresholdStr != "" {
		if d, err := time.ParseDuration(thresholdStr); err == nil && d > 0 {
			threshold = d
		}
	}

	sessions, err := h.manager.GetActiveSessions(threshold)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return empty array instead of null
	if sessions == nil {
		sessions = []requestlog.ActiveSession{}
	}

	c.JSON(http.StatusOK, sessions)
}

// CleanupLogs 清理指定天数之前的日志
func (h *RequestLogHandler) CleanupLogs(c *gin.Context) {
	daysStr := c.Query("days")
	if daysStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days parameter is required"})
		return
	}

	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be a positive integer"})
		return
	}

	// Validate allowed retention periods
	allowedDays := map[int]bool{30: true, 60: true, 90: true, 180: true, 365: true}
	if !allowedDays[days] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be one of: 30, 60, 90, 180, 365"})
		return
	}

	deleted, err := h.manager.Cleanup(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Cleanup completed",
		"deletedCount":  deleted,
		"retentionDays": days,
	})
}

// ========== User Alias Handlers ==========

// GetAliases 获取所有用户别名
func (h *RequestLogHandler) GetAliases(c *gin.Context) {
	aliases, err := h.manager.GetAllAliases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, aliases)
}

// SetAlias 设置用户别名
func (h *RequestLogHandler) SetAlias(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId is required"})
		return
	}

	var req struct {
		Alias string `json:"alias"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Alias == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alias is required"})
		return
	}

	if err := h.manager.SetAlias(userID, req.Alias); err != nil {
		if err.Error() == "alias already in use" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alias set successfully"})
}

// DeleteAlias 删除用户别名
func (h *RequestLogHandler) DeleteAlias(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId is required"})
		return
	}

	if err := h.manager.DeleteAlias(userID); err != nil {
		if err.Error() == "alias not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alias deleted successfully"})
}

// ImportAliases 批量导入别名（用于从 localStorage 迁移）
func (h *RequestLogHandler) ImportAliases(c *gin.Context) {
	var aliases map[string]string
	if err := c.ShouldBindJSON(&aliases); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body, expected object with userId: alias pairs"})
		return
	}

	imported, err := h.manager.ImportAliases(aliases)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Import completed",
		"imported": imported,
	})
}

// ========== Stats History Handlers (for Charts) ==========

	// GetStatsHistory 获取统计历史数据（用于图表）
	// GET /api/logs/stats/history?duration=1h&endpoint=/v1/messages
	func (h *RequestLogHandler) GetStatsHistory(c *gin.Context) {
		duration := c.DefaultQuery("duration", "1h")
		endpoint := c.Query("endpoint") // optional: "/v1/messages" or "/v1/responses"

		// Validate duration
		validDurations := map[string]bool{"1h": true, "6h": true, "24h": true, "today": true, "period": true}
		if !validDurations[duration] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid duration. Use: 1h, 6h, 24h, today, or period"})
			return
		}

		// Optional date range filter (RFC3339)
		fromStr := c.Query("from")
		toStr := c.Query("to")
		if fromStr != "" || toStr != "" {
			if fromStr == "" || toStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Both from and to are required when filtering by date range"})
				return
			}
			from, err := time.Parse(time.RFC3339, fromStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid from. Expected RFC3339 timestamp"})
				return
			}
			to, err := time.Parse(time.RFC3339, toStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid to. Expected RFC3339 timestamp"})
				return
			}

			result, err := h.manager.GetStatsHistoryRange(duration, from, to, endpoint)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, result)
			return
		}

		result, err := h.manager.GetStatsHistory(duration, endpoint)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

	c.JSON(http.StatusOK, result)
}

	// GetProviderStatsHistory 获取按 provider/channel 维度聚合的统计历史数据（用于图表）
	// GET /api/logs/providers/stats/history?duration=1h&endpoint=/v1/messages
	func (h *RequestLogHandler) GetProviderStatsHistory(c *gin.Context) {
		duration := c.DefaultQuery("duration", "1h")
		endpoint := c.Query("endpoint") // optional: "/v1/messages" or "/v1/responses"

		// Validate duration
		validDurations := map[string]bool{"1h": true, "6h": true, "24h": true, "today": true, "period": true}
		if !validDurations[duration] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid duration. Use: 1h, 6h, 24h, today, or period"})
			return
		}

		// Optional date range filter (RFC3339)
		fromStr := c.Query("from")
		toStr := c.Query("to")
		if fromStr != "" || toStr != "" {
			if fromStr == "" || toStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Both from and to are required when filtering by date range"})
				return
			}
			from, err := time.Parse(time.RFC3339, fromStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid from. Expected RFC3339 timestamp"})
				return
			}
			to, err := time.Parse(time.RFC3339, toStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid to. Expected RFC3339 timestamp"})
				return
			}

			result, err := h.manager.GetProviderStatsHistoryRange(duration, from, to, endpoint)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, result)
			return
		}

		result, err := h.manager.GetProviderStatsHistory(duration, endpoint)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

	c.JSON(http.StatusOK, result)
}

// GetChannelStatsHistory 获取渠道统计历史数据
// GET /api/logs/channels/:id/stats/history?duration=1h&endpoint=/v1/messages
func (h *RequestLogHandler) GetChannelStatsHistory(c *gin.Context) {
	idStr := c.Param("id")
	channelID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	duration := c.DefaultQuery("duration", "1h")
	endpoint := c.Query("endpoint")

	// Validate duration
	validDurations := map[string]bool{"1h": true, "6h": true, "24h": true, "today": true}
	if !validDurations[duration] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid duration. Use: 1h, 6h, 24h, or today"})
		return
	}

	result, err := h.manager.GetChannelStatsHistory(channelID, duration, endpoint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
