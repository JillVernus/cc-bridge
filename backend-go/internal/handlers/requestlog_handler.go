package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
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

const (
	hookRequestIDMaxLen     = 200
	hookModelMaxLen         = 200
	hookEndpointMaxLen      = 200
	hookClientSessionMaxLen = 200
	hookErrorMaxLen         = 2000
)

type anthropicHookLogIngestRequest struct {
	RequestID                string  `json:"requestId"`
	Status                   string  `json:"status"`
	InitialTime              string  `json:"initialTime"`
	CompleteTime             string  `json:"completeTime"`
	DurationMs               *int64  `json:"durationMs"`
	Model                    string  `json:"model"`
	ResponseModel            string  `json:"responseModel"`
	ReasoningEffort          string  `json:"reasoningEffort"`
	Stream                   *bool   `json:"stream"`
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	Price                    float64 `json:"price"`
	InputCost                float64 `json:"inputCost"`
	OutputCost               float64 `json:"outputCost"`
	CacheCreationCost        float64 `json:"cacheCreationCost"`
	CacheReadCost            float64 `json:"cacheReadCost"`
	HTTPStatus               *int    `json:"httpStatus"`
	Endpoint                 string  `json:"endpoint"`
	ClientID                 string  `json:"clientId"`
	SessionID                string  `json:"sessionId"`
	Error                    string  `json:"error"`
	UpstreamError            string  `json:"upstreamError"`
}

// IngestAnthropicHookLog ingests Anthropic OAuth hook log payload into request logs.
// POST /api/logs/hooks/anthropic
func (h *RequestLogHandler) IngestAnthropicHookLog(c *gin.Context) {
	var req anthropicHookLogIngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "requestId is required"})
		return
	}
	if len(requestID) > hookRequestIDMaxLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("requestId too long (max %d)", hookRequestIDMaxLen)})
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	if len(model) > hookModelMaxLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("model too long (max %d)", hookModelMaxLen)})
		return
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = requestlog.StatusCompleted
	}
	if !isValidHookRequestStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be one of: pending, completed, error, timeout, failover, retry_wait"})
		return
	}

	if req.InputTokens < 0 || req.OutputTokens < 0 || req.CacheCreationInputTokens < 0 || req.CacheReadInputTokens < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token fields must be >= 0"})
		return
	}
	if req.Price < 0 || req.InputCost < 0 || req.OutputCost < 0 || req.CacheCreationCost < 0 || req.CacheReadCost < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cost fields must be >= 0"})
		return
	}

	now := time.Now()
	initialTime := now
	if strings.TrimSpace(req.InitialTime) != "" {
		t, err := parseHookTime(req.InitialTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "initialTime must be RFC3339"})
			return
		}
		initialTime = t
	}

	completeTime := time.Time{}
	if strings.TrimSpace(req.CompleteTime) != "" {
		t, err := parseHookTime(req.CompleteTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "completeTime must be RFC3339"})
			return
		}
		completeTime = t
	}
	if status != requestlog.StatusPending && completeTime.IsZero() {
		completeTime = now
	}

	durationMs := int64(0)
	if req.DurationMs != nil {
		if *req.DurationMs < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "durationMs must be >= 0"})
			return
		}
		durationMs = *req.DurationMs
	} else if !completeTime.IsZero() {
		durationMs = completeTime.Sub(initialTime).Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
	}

	httpStatus := defaultHookHTTPStatus(status)
	if req.HTTPStatus != nil {
		if *req.HTTPStatus < 0 || *req.HTTPStatus > 999 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "httpStatus must be between 0 and 999"})
			return
		}
		httpStatus = *req.HTTPStatus
	}

	// Normalize pending records to keep lifecycle semantics consistent with native logs:
	// pending entries should not carry completion time, duration, or HTTP status.
	if status == requestlog.StatusPending {
		completeTime = time.Time{}
		durationMs = 0
		httpStatus = 0
	}

	stream := false
	if req.Stream != nil {
		stream = *req.Stream
	}

	endpoint := trimToMax(strings.TrimSpace(req.Endpoint), hookEndpointMaxLen)
	if endpoint == "" {
		endpoint = "/v1/messages"
	}

	clientID := trimToMax(strings.TrimSpace(req.ClientID), hookClientSessionMaxLen)
	sessionID := trimToMax(strings.TrimSpace(req.SessionID), hookClientSessionMaxLen)
	responseModel := trimToMax(strings.TrimSpace(req.ResponseModel), hookModelMaxLen)
	reasoningEffort := trimToMax(strings.TrimSpace(req.ReasoningEffort), 32)
	errMsg := trimToMax(strings.TrimSpace(req.Error), hookErrorMaxLen)
	upstreamErr := trimToMax(strings.TrimSpace(req.UpstreamError), hookErrorMaxLen)
	apiKeyID := getHookIngestAPIKeyID(c)

	price := req.Price
	inputCost := req.InputCost
	outputCost := req.OutputCost
	cacheCreationCost := req.CacheCreationCost
	cacheReadCost := req.CacheReadCost

	// Auto-calculate pricing when hook payload does not provide any cost values.
	if shouldAutoCalculateHookCost(req) {
		pricingModel := responseModel
		if pricingModel == "" {
			pricingModel = model
		} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && model != "" {
			pricingModel = model
		}

		if pm := pricing.GetManager(); pm != nil && pricingModel != "" {
			breakdown := pm.CalculateCostWithBreakdown(
				pricingModel,
				req.InputTokens,
				req.OutputTokens,
				req.CacheCreationInputTokens,
				req.CacheReadInputTokens,
				nil,
			)
			price = breakdown.TotalCost
			inputCost = breakdown.InputCost
			outputCost = breakdown.OutputCost
			cacheCreationCost = breakdown.CacheCreationCost
			cacheReadCost = breakdown.CacheReadCost
		}
	}

	record := &requestlog.RequestLog{
		ID:                       requestID,
		Status:                   status,
		InitialTime:              initialTime,
		CompleteTime:             completeTime,
		DurationMs:               durationMs,
		Type:                     "claude",
		ProviderName:             "anthropic-oauth",
		Model:                    model,
		ResponseModel:            responseModel,
		ReasoningEffort:          reasoningEffort,
		InputTokens:              req.InputTokens,
		OutputTokens:             req.OutputTokens,
		CacheCreationInputTokens: req.CacheCreationInputTokens,
		CacheReadInputTokens:     req.CacheReadInputTokens,
		Price:                    price,
		InputCost:                inputCost,
		OutputCost:               outputCost,
		CacheCreationCost:        cacheCreationCost,
		CacheReadCost:            cacheReadCost,
		HTTPStatus:               httpStatus,
		Stream:                   stream,
		ChannelID:                1,
		ChannelUID:               "oauth:default",
		ChannelName:              "Anthropic OAuth",
		Endpoint:                 endpoint,
		ClientID:                 clientID,
		SessionID:                sessionID,
		APIKeyID:                 apiKeyID,
		Error:                    errMsg,
		UpstreamError:            upstreamErr,
		FailoverInfo:             "",
		CreatedAt:                initialTime,
	}

	if err := h.manager.Add(record); err != nil {
		if isDuplicateRequestLogIDError(err) {
			c.JSON(http.StatusOK, gin.H{"id": requestID, "created": false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      record.ID,
		"created": true,
	})
}

func isValidHookRequestStatus(status string) bool {
	switch status {
	case requestlog.StatusPending,
		requestlog.StatusCompleted,
		requestlog.StatusError,
		requestlog.StatusTimeout,
		requestlog.StatusFailover,
		requestlog.StatusRetryWait:
		return true
	default:
		return false
	}
}

func parseHookTime(value string) (time.Time, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, v)
}

func shouldAutoCalculateHookCost(req anthropicHookLogIngestRequest) bool {
	return req.Price == 0 &&
		req.InputCost == 0 &&
		req.OutputCost == 0 &&
		req.CacheCreationCost == 0 &&
		req.CacheReadCost == 0
}

func getHookIngestAPIKeyID(c *gin.Context) *int64 {
	if c == nil {
		return nil
	}

	raw, exists := c.Get(middleware.ContextKeyAPIKeyID)
	if !exists || raw == nil {
		return nil
	}

	var id int64
	switch v := raw.(type) {
	case int64:
		id = v
	case int32:
		id = int64(v)
	case int:
		id = int64(v)
	case uint64:
		if v > uint64(^uint64(0)>>1) {
			return nil
		}
		id = int64(v)
	case uint32:
		id = int64(v)
	case uint:
		if uint64(v) > uint64(^uint64(0)>>1) {
			return nil
		}
		id = int64(v)
	default:
		return nil
	}

	return &id
}

func defaultHookHTTPStatus(status string) int {
	switch status {
	case requestlog.StatusCompleted:
		return http.StatusOK
	case requestlog.StatusTimeout:
		return http.StatusRequestTimeout
	case requestlog.StatusError:
		return 599
	case requestlog.StatusPending:
		return 0
	default:
		return 0
	}
}

func isDuplicateRequestLogIDError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "request_logs_pkey")
}

func trimToMax(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
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

// GetDebugLog 获取请求的调试日志
func (h *RequestLogHandler) GetDebugLog(c *gin.Context) {
	requestID := c.Param("id")
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Request ID is required"})
		return
	}

	entry, err := h.manager.GetDebugLog(requestID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Debug log not found"})
		return
	}

	c.JSON(http.StatusOK, entry)
}

// PurgeDebugLogs 清除所有调试日志
func (h *RequestLogHandler) PurgeDebugLogs(c *gin.Context) {
	deleted, err := h.manager.PurgeAllDebugLogs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Debug logs purged",
		"deleted": deleted,
	})
}

// GetDebugLogStats 获取调试日志统计信息
func (h *RequestLogHandler) GetDebugLogStats(c *gin.Context) {
	count, err := h.manager.GetDebugLogCount()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count": count,
	})
}

// StreamLogs 提供 SSE 实时日志流
// GET /api/logs/stream
func (h *RequestLogHandler) StreamLogs(c *gin.Context) {
	broadcaster := h.manager.GetBroadcaster()
	if broadcaster == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSE not available"})
		return
	}

	clientID, eventChan := broadcaster.Subscribe()
	if clientID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many SSE connections"})
		return
	}
	defer broadcaster.Unsubscribe(clientID)

	// Set all headers before writing anything
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "0")
	// For SSE, also set CORS if origin is present (EventSource doesn't send preflight)
	if origin := c.GetHeader("Origin"); origin != "" {
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Allow-Credentials", "true")
	}

	// Flush headers immediately by writing status
	c.Status(http.StatusOK)
	c.Writer.Flush()

	// Get context for cancellation
	ctx := c.Request.Context()

	// Send initial connection event
	_, _ = c.Writer.WriteString("event: connected\ndata: {\"clientId\":\"" + clientID + "\"}\n\n")
	c.Writer.Flush()

	// Stream events
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			sseData, err := event.ToSSE()
			if err != nil {
				continue
			}
			_, writeErr := c.Writer.WriteString(sseData)
			if writeErr != nil {
				return // Client disconnected
			}
			c.Writer.Flush()
		}
	}
}
