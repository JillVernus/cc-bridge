package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/httpclient"
	"github.com/gin-gonic/gin"
)

// UpstreamModel represents a model from upstream provider
type UpstreamModel struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// UpstreamModelsResponse is the response for fetching upstream models
type UpstreamModelsResponse struct {
	Success bool            `json:"success"`
	Models  []UpstreamModel `json:"models,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// OpenAI-style model list response
type openAIModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// Gemini model list response
type geminiModelListResponse struct {
	Models []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"models"`
}

// FetchUpstreamModels fetches the model list from an upstream channel
// GET /api/channels/:id/models
func FetchUpstreamModels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, UpstreamModelsResponse{
				Success: false,
				Error:   "Invalid channel ID",
			})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.Upstream) {
			c.JSON(http.StatusNotFound, UpstreamModelsResponse{
				Success: false,
				Error:   "Channel not found",
			})
			return
		}

		channel := cfg.Upstream[id]
		models, err := fetchModelsFromUpstream(&channel)
		if err != nil {
			c.JSON(http.StatusOK, UpstreamModelsResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, UpstreamModelsResponse{
			Success: true,
			Models:  models,
		})
	}
}

// FetchResponsesUpstreamModels fetches the model list from a Responses upstream channel
// GET /api/responses/channels/:id/models
func FetchResponsesUpstreamModels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, UpstreamModelsResponse{
				Success: false,
				Error:   "Invalid channel ID",
			})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.ResponsesUpstream) {
			c.JSON(http.StatusNotFound, UpstreamModelsResponse{
				Success: false,
				Error:   "Channel not found",
			})
			return
		}

		channel := cfg.ResponsesUpstream[id]
		models, err := fetchModelsFromUpstream(&channel)
		if err != nil {
			c.JSON(http.StatusOK, UpstreamModelsResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, UpstreamModelsResponse{
			Success: true,
			Models:  models,
		})
	}
}

// FetchGeminiUpstreamModels fetches the model list from a Gemini upstream channel
// GET /api/gemini/channels/:id/models
func FetchGeminiUpstreamModels(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, UpstreamModelsResponse{
				Success: false,
				Error:   "Invalid channel ID",
			})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.GeminiUpstream) {
			c.JSON(http.StatusNotFound, UpstreamModelsResponse{
				Success: false,
				Error:   "Channel not found",
			})
			return
		}

		channel := cfg.GeminiUpstream[id]
		models, err := fetchModelsFromUpstream(&channel)
		if err != nil {
			c.JSON(http.StatusOK, UpstreamModelsResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, UpstreamModelsResponse{
			Success: true,
			Models:  models,
		})
	}
}

// fetchModelsFromUpstream fetches models from an upstream provider
func fetchModelsFromUpstream(channel *config.UpstreamConfig) ([]UpstreamModel, error) {
	// Check if channel has API keys
	if len(channel.APIKeys) == 0 {
		return nil, fmt.Errorf("no API key configured for this channel")
	}

	serviceType := channel.ServiceType
	baseURL := strings.TrimSuffix(channel.BaseURL, "/")
	apiKey := channel.APIKeys[0] // Use first API key

	// Composite channels don't have their own models
	if serviceType == "composite" {
		return nil, fmt.Errorf("composite channels do not have upstream models")
	}

	// Build the models endpoint URL based on service type
	var modelsURL string
	var authHeader string
	var authValue string

	switch serviceType {
	case "gemini":
		// Gemini (Generative Language API) model list endpoint:
		// https://generativelanguage.googleapis.com/v1beta/models
		// Prefer x-goog-api-key header (avoid putting secrets in URL).
		// If baseURL doesn't include a version segment, default to /v1beta.
		versioned := baseURL
		if !strings.HasSuffix(versioned, "/v1beta") && !strings.HasSuffix(versioned, "/v1alpha") && !strings.HasSuffix(versioned, "/v1") {
			// Also handle ".../v1beta/..." cases by checking for any "/v1*/" occurrence.
			if !strings.Contains(versioned, "/v1beta/") && !strings.Contains(versioned, "/v1alpha/") && !strings.Contains(versioned, "/v1/") {
				versioned = versioned + "/v1beta"
			}
		}
		modelsURL = strings.TrimSuffix(versioned, "/") + "/models"
		authHeader = "x-goog-api-key"
		authValue = apiKey
	case "openai", "openai_chat", "openaiold", "responses", "openai-oauth", "claude":
		// OpenAI-compatible APIs use Bearer token
		// Note: "claude" type channels may actually be OpenAI-compatible proxies
		// Try to determine if baseURL already includes /v1
		if strings.HasSuffix(baseURL, "/v1") {
			modelsURL = baseURL + "/models"
		} else if strings.Contains(baseURL, "/v1/") {
			// baseURL might be like https://api.openai.com/v1/chat/completions
			// Extract up to /v1
			idx := strings.Index(baseURL, "/v1/")
			modelsURL = baseURL[:idx+3] + "/models"
		} else {
			// Assume we need to add /v1/models
			modelsURL = baseURL + "/v1/models"
		}
		authHeader = "Authorization"
		authValue = "Bearer " + apiKey
	default:
		return nil, fmt.Errorf("unsupported service type: %s", serviceType)
	}

	// Create HTTP request
	client := httpclient.GetManager().GetStandardClient(10*time.Second, channel.InsecureSkipVerify, 0)
	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	if authHeader != "" {
		req.Header.Set(authHeader, authValue)
	}
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, truncateString(string(body), 200))
	}

	// Parse response based on service type
	var models []UpstreamModel

	if serviceType == "gemini" {
		var geminiResp geminiModelListResponse
		if err := json.Unmarshal(body, &geminiResp); err != nil {
			return nil, fmt.Errorf("failed to parse Gemini response: %v", err)
		}
		for _, m := range geminiResp.Models {
			// Gemini model names are like "models/gemini-pro", extract just the model name
			modelID := strings.TrimPrefix(m.Name, "models/")
			models = append(models, UpstreamModel{
				ID:      modelID,
				Object:  "model",
				OwnedBy: "google",
			})
		}
	} else {
		// OpenAI-compatible response
		var openAIResp openAIModelListResponse
		if err := json.Unmarshal(body, &openAIResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %v", err)
		}
		for _, m := range openAIResp.Data {
			models = append(models, UpstreamModel{
				ID:      m.ID,
				Object:  m.Object,
				OwnedBy: m.OwnedBy,
			})
		}
	}

	// Sort models by ID
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
