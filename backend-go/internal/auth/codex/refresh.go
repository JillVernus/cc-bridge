package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
)

const (
	// OpenAI OAuth endpoints
	openaiTokenURL = "https://auth.openai.com/oauth/token"
	// Client ID used by official Codex CLI
	openaiClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// tokenResponse represents the response from OpenAI's token endpoint
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// RefreshTokens exchanges a refresh token for new OAuth tokens.
// It returns the new tokens including access_token, refresh_token, and id_token.
func (tm *TokenManager) RefreshTokens(refreshToken string) (*config.OAuthTokens, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is empty")
	}

	// Build the request body (form-encoded)
	data := url.Values{
		"client_id":     {openaiClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {"openid profile email"},
	}

	req, err := http.NewRequest("POST", openaiTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Extract account_id from the new id_token
	accountID, err := ParseJWTAccountID(tokenResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to parse account_id from id_token: %w", err)
	}

	tokens := &config.OAuthTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		AccountID:    accountID,
		LastRefresh:  time.Now().UTC().Format(time.RFC3339),
	}

	log.Printf("✅ OAuth tokens refreshed successfully")

	return tokens, nil
}

// RefreshTokensWithRetry attempts to refresh tokens with exponential backoff retry.
// maxRetries specifies the maximum number of retry attempts.
func (tm *TokenManager) RefreshTokensWithRetry(refreshToken string, maxRetries int) (*config.OAuthTokens, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("Token refresh attempt %d failed, retrying in %v...", attempt, backoff)
			time.Sleep(backoff)
		}

		tokens, err := tm.RefreshTokens(refreshToken)
		if err == nil {
			return tokens, nil
		}

		lastErr = err
		log.Printf("Token refresh attempt %d failed: %v", attempt+1, err)
	}

	return nil, fmt.Errorf("token refresh failed after %d attempts: %w", maxRetries, lastErr)
}
