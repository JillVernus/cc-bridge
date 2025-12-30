package handlers

import (
	"net/http"
	"strconv"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/gin-gonic/gin"
)

// APIKeyHandler handles API key management endpoints
type APIKeyHandler struct {
	manager *apikey.Manager
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(manager *apikey.Manager) *APIKeyHandler {
	return &APIKeyHandler{manager: manager}
}

// ListKeys returns all API keys
// GET /api/keys
func (h *APIKeyHandler) ListKeys(c *gin.Context) {
	filter := &apikey.APIKeyFilter{}

	if status := c.Query("status"); status != "" {
		filter.Status = status
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

	result, err := h.manager.List(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return empty array instead of null
	if result.Keys == nil {
		result.Keys = []apikey.APIKey{}
	}

	c.JSON(http.StatusOK, result)
}

// CreateKey creates a new API key
// POST /api/keys
func (h *APIKeyHandler) CreateKey(c *gin.Context) {
	var req apikey.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	const maxRateLimitRPM = 10000
	if req.RateLimitRPM < 0 || req.RateLimitRPM > maxRateLimitRPM {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rateLimitRpm must be between 0 and 10000"})
		return
	}

	result, err := h.manager.Create(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// GetKey returns a single API key by ID
// GET /api/keys/:id
func (h *APIKeyHandler) GetKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	key, err := h.manager.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if key == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	c.JSON(http.StatusOK, key)
}

// UpdateKey updates an API key
// PUT /api/keys/:id
func (h *APIKeyHandler) UpdateKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req apikey.UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.RateLimitRPM != nil {
		const maxRateLimitRPM = 10000
		if *req.RateLimitRPM < 0 || *req.RateLimitRPM > maxRateLimitRPM {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rateLimitRpm must be between 0 and 10000"})
			return
		}
	}

	key, err := h.manager.Update(id, &req)
	if err != nil {
		if err.Error() == "API key not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, key)
}

// DeleteKey deletes an API key
// DELETE /api/keys/:id
func (h *APIKeyHandler) DeleteKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.manager.Delete(id); err != nil {
		if err.Error() == "API key not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key deleted"})
}

// EnableKey enables a disabled API key
// POST /api/keys/:id/enable
func (h *APIKeyHandler) EnableKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.manager.Enable(id); err != nil {
		if err.Error() == "API key not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "cannot enable a revoked key" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key enabled"})
}

// DisableKey disables an API key
// POST /api/keys/:id/disable
func (h *APIKeyHandler) DisableKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.manager.Disable(id); err != nil {
		if err.Error() == "API key not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "cannot disable a revoked key" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key disabled"})
}

// RevokeKey permanently revokes an API key
// POST /api/keys/:id/revoke
func (h *APIKeyHandler) RevokeKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.manager.Revoke(id); err != nil {
		if err.Error() == "API key not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
}
