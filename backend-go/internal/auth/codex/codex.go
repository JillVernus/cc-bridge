// Package codex provides OAuth token management for OpenAI Codex (ChatGPT subscription) authentication.
// It handles token parsing, validation, and automatic refresh for the openai-oauth service type.
package codex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// CodexOAuthExport represents an external export wrapper that bundles one or
// more OAuth accounts. Only the fields we consume are modeled.
type CodexOAuthExport struct {
	Accounts []CodexOAuthExportAccount `json:"accounts"`
}

type CodexOAuthExportAccount struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Credentials struct {
		AccessToken      string `json:"access_token"`
		ChatGPTAccountID string `json:"chatgpt_account_id"`
		Email            string `json:"email"`
		ExpiresAt        string `json:"expires_at"`
		RefreshToken     string `json:"refresh_token"`
		IDToken          string `json:"id_token"`
	} `json:"credentials"`
	Extra struct {
		LastRefresh string `json:"last_refresh"`
	} `json:"extra"`
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
//
// It supports three shapes:
//  1. Codex CLI auth.json: {OPENAI_API_KEY, last_refresh, tokens: {...}}
//  2. Flat OAuth payload:  {access_token, account_id, refresh_token, ...}
//  3. External export wrapper: {accounts: [{platform, type, credentials, extra}, ...]}
//
// For (3), the first entry with platform == "openai" and type == "oauth" is used.
// The wrapper format may omit refresh_token; such tokens are accepted and will
// stop working once the access token expires.
func ParseAuthJSON(content string) (*config.OAuthTokens, error) {
	// First, try the export wrapper. We detect it by the presence of accounts[].
	var export CodexOAuthExport
	if err := json.Unmarshal([]byte(content), &export); err == nil && len(export.Accounts) > 0 {
		return parseExportWrapper(&export)
	}

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

// parseExportWrapper extracts the first eligible OpenAI OAuth account from an
// external export. refresh_token is optional in this format.
func parseExportWrapper(export *CodexOAuthExport) (*config.OAuthTokens, error) {
	for i := range export.Accounts {
		acct := &export.Accounts[i]
		if !strings.EqualFold(acct.Platform, "openai") {
			continue
		}
		if !strings.EqualFold(acct.Type, "oauth") {
			continue
		}
		if acct.Credentials.AccessToken == "" {
			return nil, fmt.Errorf("missing access_token in auth.json")
		}
		if acct.Credentials.ChatGPTAccountID == "" {
			return nil, fmt.Errorf("missing account_id in auth.json")
		}
		return &config.OAuthTokens{
			AccessToken:  acct.Credentials.AccessToken,
			AccountID:    acct.Credentials.ChatGPTAccountID,
			IDToken:      acct.Credentials.IDToken,
			RefreshToken: acct.Credentials.RefreshToken,
			LastRefresh:  acct.Extra.LastRefresh,
		}, nil
	}
	return nil, fmt.Errorf("no eligible openai oauth account in export")
}

// GetValidToken returns a valid access token and account ID.
// If the token is expired or near expiry, it will attempt to refresh it.
// When the tokens have no refresh_token (e.g. imported from an external export
// wrapper), the access token is returned as-is and no refresh is attempted;
// upstream will surface a 401 when the token eventually expires.
// Returns: accessToken, accountID, updatedTokens (if refreshed), error
func (tm *TokenManager) GetValidToken(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
	if tokens == nil {
		return "", "", nil, fmt.Errorf("OAuth tokens not configured")
	}

	if tokens.RefreshToken == "" {
		return tokens.AccessToken, tokens.AccountID, nil, nil
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

// IsTokenValid checks if the OAuth tokens are properly configured.
// RefreshToken is optional: imports from external export wrappers may lack one,
// in which case the channel runs until the access token expires.
func IsTokenValid(tokens *config.OAuthTokens) bool {
	if tokens == nil {
		return false
	}
	return tokens.AccessToken != "" && tokens.AccountID != ""
}
