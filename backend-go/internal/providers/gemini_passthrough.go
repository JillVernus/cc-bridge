package providers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// Package-level compiled regex for version pattern matching
var geminiVersionPattern = regexp.MustCompile(`/v\d+[a-z]*$`)

// Allowed characters for model names and actions (alphanumeric, dash, underscore, dot)
var validModelActionPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// GeminiPassthroughProvider handles Gemini -> Gemini passthrough (no protocol conversion)
type GeminiPassthroughProvider struct{}

// ConvertToProviderRequest builds the upstream Gemini request (passthrough mode)
// The model and action are extracted from the URL path, not the request body.
func (p *GeminiPassthroughProvider) ConvertToProviderRequest(c *gin.Context, upstream *config.UpstreamConfig, apiKey string) (*http.Request, []byte, error) {
	// Read original request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Extract model and action from URL path
	// The path format is: /v1/gemini/models/{model}:{action}
	// The gin param "action" captures everything after /models/
	actionParam := c.Param("action")
	actionParam = strings.TrimPrefix(actionParam, "/") // Remove leading slash

	// Parse model:action from the path
	model, action, err := parseAndValidateModelAction(actionParam)
	if err != nil {
		return nil, bodyBytes, err
	}

	// Apply model mapping if configured
	if upstream.ModelMapping != nil && len(upstream.ModelMapping) > 0 {
		model = config.RedirectModel(model, upstream)
	}

	// Build target URL: {baseURL}/models/{model}:{action}
	baseURL := strings.TrimSuffix(upstream.BaseURL, "/")

	// Gemini API URLs don't have version suffixes in the typical sense
	// They use formats like: https://generativelanguage.googleapis.com/v1beta/models/...
	// Check if baseURL already ends with a version pattern
	var targetURL string
	if geminiVersionPattern.MatchString(baseURL) {
		targetURL = baseURL + "/models/" + model + ":" + action
	} else {
		// Default: append /v1beta for Gemini
		targetURL = baseURL + "/v1beta/models/" + model + ":" + action
	}

	// Preserve query string but strip sensitive parameters
	queryString := sanitizeQueryString(c.Request.URL.RawQuery)
	if queryString != "" {
		targetURL += "?" + queryString
	}

	// Create request
	req, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, bodyBytes, err
	}

	// Use unified header handling (transparent proxy)
	req.Header = utils.PrepareUpstreamHeaders(c, req.URL.Host)
	utils.SetGeminiAuthenticationHeader(req.Header, apiKey)

	return req, bodyBytes, nil
}

// parseAndValidateModelAction parses "model:action" from the URL path segment
// and validates that they don't contain path traversal or invalid characters
// e.g., "gemini-2.0-flash:generateContent" -> ("gemini-2.0-flash", "generateContent", nil)
func parseAndValidateModelAction(path string) (model, action string, err error) {
	// Reject path traversal attempts
	if strings.Contains(path, "..") || strings.Contains(path, "/") {
		return "", "", fmt.Errorf("invalid model/action path: contains forbidden characters")
	}

	// Find the last colon (action separator)
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		// No action specified, default to generateContent
		model = path
		action = "generateContent"
	} else {
		model = path[:idx]
		action = path[idx+1:]
	}

	// Validate model name
	if model == "" || !validModelActionPattern.MatchString(model) {
		return "", "", fmt.Errorf("invalid model name: %q", model)
	}

	// Validate action name
	if action == "" || !validModelActionPattern.MatchString(action) {
		return "", "", fmt.Errorf("invalid action name: %q", action)
	}

	return model, action, nil
}

// sanitizeQueryString removes sensitive query parameters like "key"
// to prevent bring-your-own-key attacks
func sanitizeQueryString(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// Drop query entirely on parse error to prevent key leakage
		return ""
	}

	// Remove sensitive parameters
	values.Del("key")
	values.Del("api_key")
	values.Del("apiKey")

	return values.Encode()
}
