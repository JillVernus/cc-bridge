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
