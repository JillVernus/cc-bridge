package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// Context keys for debug logging
const (
	ContextKeyDebugRequestBody       = "debug_request_body"
	ContextKeyDebugReqHeaders        = "debug_request_headers"
	ContextKeyDebugRemovedReqHeaders = "debug_removed_request_headers"
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

func ApplyOutboundHeaderPolicy(c *gin.Context, cfgManager *config.ConfigManager, outboundReq *http.Request) {
	if c == nil || cfgManager == nil || outboundReq == nil {
		return
	}

	policy := cfgManager.GetOutboundHeaderPolicy()
	removed := utils.MatchHeaderStripRules(c.Request.Header, policy)
	c.Set(ContextKeyDebugRemovedReqHeaders, removed)
	_ = utils.ApplyHeaderStripRules(outboundReq.Header, policy)
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
	reqRemovedHeaders := make(map[string]string)
	if v, exists := c.Get(ContextKeyDebugRemovedReqHeaders); exists {
		if headers, ok := v.(map[string]string); ok {
			reqRemovedHeaders = headers
		}
	}

	// Create debug log entry
	entry := &requestlog.DebugLogEntry{
		RequestID:             requestLogID,
		RequestMethod:         c.Request.Method,
		RequestPath:           c.Request.URL.Path,
		RequestHeaders:        reqHeaders,
		RequestRemovedHeaders: reqRemovedHeaders,
		RequestBodySize:       len(reqBody),
		ResponseStatus:        respStatus,
		ResponseHeaders:       requestlog.HttpHeadersToMap(respHeaders),
		ResponseBodySize:      len(respBody),
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
			log.Printf("⚠️ Failed to save debug log: %v", err)
		}
	}()
}

// SaveErrorDebugLog saves debug log for error responses (convenience wrapper)
// Use this when you have the response body but may not have response headers
func SaveErrorDebugLog(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	respStatus int,
	respBody []byte,
) {
	SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, respStatus, nil, respBody)
}

// WriteJSONWithOptionalDebugLog writes a JSON response and persists the same body
// into debug logs when a request log ID is available.
func WriteJSONWithOptionalDebugLog(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	status int,
	payload interface{},
) {
	if c == nil {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		c.JSON(status, payload)
		SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, []byte(`{"error":"failed to marshal debug payload"}`))
		return
	}

	SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, body)
	c.Data(status, "application/json; charset=utf-8", body)
}
