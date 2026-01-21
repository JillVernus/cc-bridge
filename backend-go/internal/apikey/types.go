package apikey

import (
	"time"
)

// API Key status constants
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"
)

// APIKey represents an API key record
type APIKey struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	KeyPrefix    string     `json:"keyPrefix"`   // First 8 chars for display "sk-abc1..."
	Description  string     `json:"description"` // Optional description
	Status       string     `json:"status"`      // active, disabled, revoked
	IsAdmin      bool       `json:"isAdmin"`
	RateLimitRPM int        `json:"rateLimitRpm"` // Requests per minute (0 = use global)
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`

	// Permission fields (nil/empty = unrestricted)
	AllowedEndpoints      []string `json:"allowedEndpoints,omitempty"`      // ["messages"], ["responses"], ["gemini"], or any combination
	AllowedChannelsMsg    []int    `json:"allowedChannelsMsg,omitempty"`    // channel indices for /v1/messages
	AllowedChannelsResp   []int    `json:"allowedChannelsResp,omitempty"`   // channel indices for /v1/responses
	AllowedChannelsGemini []int    `json:"allowedChannelsGemini,omitempty"` // channel indices for /v1/gemini (GeminiUpstream)
	AllowedModels         []string `json:"allowedModels,omitempty"`         // glob patterns: ["claude-sonnet-*"]
}

// CreateAPIKeyRequest represents a request to create a new API key
type CreateAPIKeyRequest struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	IsAdmin      bool   `json:"isAdmin"`
	RateLimitRPM int    `json:"rateLimitRpm"`

	// Permission fields (nil/empty = unrestricted)
	AllowedEndpoints      []string `json:"allowedEndpoints,omitempty"`
	AllowedChannelsMsg    []int    `json:"allowedChannelsMsg,omitempty"`
	AllowedChannelsResp   []int    `json:"allowedChannelsResp,omitempty"`
	AllowedChannelsGemini []int    `json:"allowedChannelsGemini,omitempty"`
	AllowedModels         []string `json:"allowedModels,omitempty"`
}

// CreateAPIKeyResponse represents the response after creating a new API key
// This is the only time the full key is returned
type CreateAPIKeyResponse struct {
	APIKey
	Key string `json:"key"` // Full key, only returned once on creation
}

// UpdateAPIKeyRequest represents a request to update an API key
type UpdateAPIKeyRequest struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	RateLimitRPM *int    `json:"rateLimitRpm"`

	// Permission fields (nil = no change, empty slice = clear/unrestrict)
	AllowedEndpoints      *[]string `json:"allowedEndpoints,omitempty"`
	AllowedChannelsMsg    *[]int    `json:"allowedChannelsMsg,omitempty"`
	AllowedChannelsResp   *[]int    `json:"allowedChannelsResp,omitempty"`
	AllowedChannelsGemini *[]int    `json:"allowedChannelsGemini,omitempty"`
	AllowedModels         *[]string `json:"allowedModels,omitempty"`
}

// APIKeyListResponse represents the response for listing API keys
type APIKeyListResponse struct {
	Keys    []APIKey `json:"keys"`
	Total   int64    `json:"total"`
	HasMore bool     `json:"hasMore"`
}

// APIKeyFilter represents filter options for querying API keys
type APIKeyFilter struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// ValidatedKey represents a validated API key with its metadata
// Used internally after authentication
type ValidatedKey struct {
	ID           int64
	Name         string
	IsAdmin      bool
	RateLimitRPM int

	// Permission fields
	AllowedEndpoints      []string
	AllowedChannelsMsg    []int
	AllowedChannelsResp   []int
	AllowedChannelsGemini []int
	AllowedModels         []string
}
