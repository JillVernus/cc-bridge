package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/gin-gonic/gin"
)

// UsageQuotaHandler handles usage quota-related API endpoints
type UsageQuotaHandler struct {
	usageManager  *quota.UsageManager
	reqLogManager *requestlog.Manager
}

// NewUsageQuotaHandler creates a new usage quota handler
func NewUsageQuotaHandler(um *quota.UsageManager, reqLogManager *requestlog.Manager) *UsageQuotaHandler {
	return &UsageQuotaHandler{usageManager: um, reqLogManager: reqLogManager}
}

// GetChannelUsageQuota returns usage quota status for a Messages channel
// GET /api/channels/:id/usage
func (h *UsageQuotaHandler) GetChannelUsageQuota(c *gin.Context) {
	indexStr := c.Param("id")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel index"})
		return
	}

	status := h.usageManager.GetChannelUsageStatus(index)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// ResetChannelUsageQuota resets usage quota for a Messages channel
// POST /api/channels/:id/usage/reset
func (h *UsageQuotaHandler) ResetChannelUsageQuota(c *gin.Context) {
	indexStr := c.Param("id")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel index"})
		return
	}

	if err := h.usageManager.ResetUsage(index); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Also clear any channel suspension (quota channel was suspended due to 429 quota_exhausted)
	if h.reqLogManager != nil {
		if cleared, err := h.reqLogManager.ClearChannelSuspension(index, "messages"); err != nil {
			log.Printf("⚠️ Failed to clear channel suspension: %v", err)
		} else if cleared {
			log.Printf("✅ Cleared suspension for Messages channel [%d] (quota reset)", index)
		}
	}

	// Return updated status
	status := h.usageManager.GetChannelUsageStatus(index)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"usage":   status,
	})
}

// GetResponsesChannelUsageQuota returns usage quota status for a Responses channel
// GET /api/responses/channels/:id/usage
func (h *UsageQuotaHandler) GetResponsesChannelUsageQuota(c *gin.Context) {
	indexStr := c.Param("id")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel index"})
		return
	}

	status := h.usageManager.GetResponsesChannelUsageStatus(index)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// ResetResponsesChannelUsageQuota resets usage quota for a Responses channel
// POST /api/responses/channels/:id/usage/reset
func (h *UsageQuotaHandler) ResetResponsesChannelUsageQuota(c *gin.Context) {
	indexStr := c.Param("id")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel index"})
		return
	}

	if err := h.usageManager.ResetResponsesUsage(index); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Also clear any channel suspension (quota channel was suspended due to 429 quota_exhausted)
	if h.reqLogManager != nil {
		if cleared, err := h.reqLogManager.ClearChannelSuspension(index, "responses"); err != nil {
			log.Printf("⚠️ Failed to clear channel suspension: %v", err)
		} else if cleared {
			log.Printf("✅ Cleared suspension for Responses channel [%d] (quota reset)", index)
		}
	}

	status := h.usageManager.GetResponsesChannelUsageStatus(index)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"usage":   status,
	})
}

// GetAllChannelUsageQuotas returns usage quota statuses for all Messages channels
// GET /api/channels/usage
func (h *UsageQuotaHandler) GetAllChannelUsageQuotas(c *gin.Context) {
	statuses := h.usageManager.GetAllChannelUsageStatuses()
	c.JSON(http.StatusOK, statuses)
}

// GetAllResponsesChannelUsageQuotas returns usage quota statuses for all Responses channels
// GET /api/responses/channels/usage
func (h *UsageQuotaHandler) GetAllResponsesChannelUsageQuotas(c *gin.Context) {
	statuses := h.usageManager.GetAllResponsesChannelUsageStatuses()
	c.JSON(http.StatusOK, statuses)
}
