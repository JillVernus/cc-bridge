// Package codex provides OAuth token management for OpenAI Codex (ChatGPT subscription) authentication.
// It handles token parsing, validation, and automatic refresh for the openai-oauth service type.
package codex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
)

// CodexAuthJSON represents the structure of the official Codex CLI's auth.json file
// This is what users copy/paste from ~/.codex/auth.json
type CodexAuthJSON struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	LastRefresh  string `json:"last_refresh"`
	Tokens       struct {
		AccessToken  string `json:"access_token"`
		AccountID    string `json:"account_id"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

// TokenManager handles OAuth token validation and refresh for Codex channels.
// It is thread-safe and can be shared across multiple goroutines.
type TokenManager struct {
	mu         sync.RWMutex
	httpClient *http.Client
}

// NewTokenManager creates a new TokenManager instance
func NewTokenManager() *TokenManager {
	return &TokenManager{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ParseAuthJSON parses the content of a Codex auth.json file and extracts the OAuth tokens.
// This is used when users paste their auth.json content into the cc-bridge UI.
func ParseAuthJSON(content string) (*config.OAuthTokens, error) {
	var authJSON CodexAuthJSON
	if err := json.Unmarshal([]byte(content), &authJSON); err != nil {
		return nil, fmt.Errorf("failed to parse auth.json: %w", err)
	}

	// Validate required fields
	if authJSON.Tokens.AccessToken == "" {
		return nil, fmt.Errorf("missing access_token in auth.json")
	}
	if authJSON.Tokens.AccountID == "" {
		return nil, fmt.Errorf("missing account_id in auth.json")
	}
	if authJSON.Tokens.RefreshToken == "" {
		return nil, fmt.Errorf("missing refresh_token in auth.json")
	}

	return &config.OAuthTokens{
		AccessToken:  authJSON.Tokens.AccessToken,
		AccountID:    authJSON.Tokens.AccountID,
		IDToken:      authJSON.Tokens.IDToken,
		RefreshToken: authJSON.Tokens.RefreshToken,
		LastRefresh:  authJSON.LastRefresh,
	}, nil
}

// GetValidToken returns a valid access token and account ID.
// If the token is expired or near expiry, it will attempt to refresh it.
// Returns: accessToken, accountID, updatedTokens (if refreshed), error
func (tm *TokenManager) GetValidToken(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
	if tokens == nil {
		return "", "", nil, fmt.Errorf("OAuth tokens not configured")
	}

	tm.mu.RLock()
	expired := tm.isTokenExpired(tokens)
	tm.mu.RUnlock()

	if !expired {
		return tokens.AccessToken, tokens.AccountID, nil, nil
	}

	// Token is expired or near expiry, attempt refresh
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check after acquiring write lock
	if !tm.isTokenExpired(tokens) {
		return tokens.AccessToken, tokens.AccountID, nil, nil
	}

	// Refresh tokens
	newTokens, err := tm.RefreshTokens(tokens.RefreshToken)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to refresh tokens: %w", err)
	}

	return newTokens.AccessToken, newTokens.AccountID, newTokens, nil
}

// isTokenExpired checks if the access token is expired or will expire within 5 minutes.
// It parses the JWT access token to extract the expiration time.
func (tm *TokenManager) isTokenExpired(tokens *config.OAuthTokens) bool {
	if tokens == nil || tokens.AccessToken == "" {
		return true
	}

	// Parse expiry from the access token (JWT)
	expiry, err := ParseJWTExpiry(tokens.AccessToken)
	if err != nil {
		// If we can't parse expiry, check last refresh time
		// Assume tokens expire after 1 hour
		if tokens.LastRefresh != "" {
			lastRefresh, err := time.Parse(time.RFC3339, tokens.LastRefresh)
			if err == nil {
				// Refresh if last refresh was more than 55 minutes ago
				return time.Since(lastRefresh) > 55*time.Minute
			}
		}
		// Can't determine expiry, assume expired for safety
		return true
	}

	// Refresh if token expires within 5 minutes
	return time.Until(expiry) < 5*time.Minute
}

// IsTokenValid checks if the OAuth tokens are properly configured
func IsTokenValid(tokens *config.OAuthTokens) bool {
	if tokens == nil {
		return false
	}
	return tokens.AccessToken != "" && tokens.AccountID != "" && tokens.RefreshToken != ""
}
