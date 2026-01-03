package handlers

import (
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/gin-gonic/gin"
)

// Context keys for debug logging
const (
	ContextKeyDebugRequestBody = "debug_request_body"
	ContextKeyDebugReqHeaders  = "debug_request_headers"
)

// StoreDebugRequestData stores request data in context for later debug logging
func StoreDebugRequestData(c *gin.Context, bodyBytes []byte) {
	c.Set(ContextKeyDebugRequestBody, bodyBytes)
	// Store a copy of request headers
	headers := make(map[string]string)
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	c.Set(ContextKeyDebugReqHeaders, headers)
}

// SaveDebugLog saves debug log entry if debug logging is enabled
func SaveDebugLog(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	respStatus int,
	respHeaders http.Header,
	respBody []byte,
) {
	if reqLogManager == nil || requestLogID == "" {
		return
	}

	// Check if debug logging is enabled
	debugCfg := cfgManager.GetDebugLogConfig()
	if !debugCfg.Enabled {
		return
	}

	// Get request body from context
	var reqBody []byte
	if v, exists := c.Get(ContextKeyDebugRequestBody); exists {
		if body, ok := v.([]byte); ok {
			reqBody = body
		}
	}

	// Get request headers from context
	reqHeaders := make(map[string]string)
	if v, exists := c.Get(ContextKeyDebugReqHeaders); exists {
		if headers, ok := v.(map[string]string); ok {
			reqHeaders = headers
		}
	}

	// Create debug log entry
	entry := &requestlog.DebugLogEntry{
		RequestID:        requestLogID,
		RequestMethod:    c.Request.Method,
		RequestPath:      c.Request.URL.Path,
		RequestHeaders:   reqHeaders,
		RequestBodySize:  len(reqBody),
		ResponseStatus:   respStatus,
		ResponseHeaders:  requestlog.HttpHeadersToMap(respHeaders),
		ResponseBodySize: len(respBody),
	}

	// Apply body size limits
	maxBodySize := debugCfg.GetMaxBodySize()
	if maxBodySize > 0 && len(reqBody) > maxBodySize {
		entry.RequestBody = string(reqBody[:maxBodySize]) + "\n... [truncated]"
	} else {
		entry.RequestBody = string(reqBody)
	}

	if maxBodySize > 0 && len(respBody) > maxBodySize {
		entry.ResponseBody = string(respBody[:maxBodySize]) + "\n... [truncated]"
	} else {
		entry.ResponseBody = string(respBody)
	}

	// Save asynchronously to avoid blocking the response
	go func() {
		if err := reqLogManager.AddDebugLog(entry); err != nil {
			// Log error but don't fail the request
			// log.Printf("⚠️ Failed to save debug log: %v", err)
		}
	}()
}
