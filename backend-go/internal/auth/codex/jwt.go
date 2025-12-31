package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JWTClaims represents the claims in an OpenAI JWT token
type JWTClaims struct {
	// Standard claims
	Exp   int64  `json:"exp"`   // Expiration time
	Iat   int64  `json:"iat"`   // Issued at
	Sub   string `json:"sub"`   // Subject (user ID)
	Email string `json:"email"` // User email

	// OpenAI-specific claims
	CodexAuthInfo *CodexAuthInfo `json:"https://api.openai.com/auth"`
}

// CodexAuthInfo contains OpenAI/Codex-specific authentication information
type CodexAuthInfo struct {
	ChatGPTAccountID               string `json:"chatgpt_account_id"`
	ChatGPTUserID                  string `json:"chatgpt_user_id"`
	ChatGPTPlanType                string `json:"chatgpt_plan_type"`
	ChatGPTSubscriptionActiveStart any    `json:"chatgpt_subscription_active_start,omitempty"`
	ChatGPTSubscriptionActiveUntil any    `json:"chatgpt_subscription_active_until,omitempty"`
	ChatGPTSubscriptionLastChecked any    `json:"chatgpt_subscription_last_checked,omitempty"`
}

// ParseJWTExpiry extracts the expiration time from a JWT token.
// It does NOT validate the signature - only parses the claims.
func ParseJWTExpiry(token string) (time.Time, error) {
	claims, err := parseJWTClaims(token)
	if err != nil {
		return time.Time{}, err
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no expiration claim in token")
	}

	return time.Unix(claims.Exp, 0), nil
}

// ParseJWTAccountID extracts the ChatGPT account ID from an id_token.
// This is required for the Chatgpt-Account-Id header in API requests.
func ParseJWTAccountID(idToken string) (string, error) {
	claims, err := parseJWTClaims(idToken)
	if err != nil {
		return "", err
	}

	if claims.CodexAuthInfo != nil && claims.CodexAuthInfo.ChatGPTAccountID != "" {
		return claims.CodexAuthInfo.ChatGPTAccountID, nil
	}

	return "", fmt.Errorf("no chatgpt_account_id found in id_token")
}

// ParseJWTEmail extracts the user email from a JWT token.
func ParseJWTEmail(token string) (string, error) {
	claims, err := parseJWTClaims(token)
	if err != nil {
		return "", err
	}

	if claims.Email == "" {
		return "", fmt.Errorf("no email claim in token")
	}

	return claims.Email, nil
}

// parseJWTClaims parses the claims from a JWT token without signature validation.
// JWT format: header.payload.signature
func parseJWTClaims(token string) (*JWTClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second part)
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &claims, nil
}

// base64URLDecode decodes a base64url-encoded string (no padding)
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if necessary
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	return base64.URLEncoding.DecodeString(s)
}

// ParseJWTClaims parses a JWT token and returns the full claims.
// This is useful for extracting subscription/plan metadata from id_token.
func ParseJWTClaims(token string) (*JWTClaims, error) {
	return parseJWTClaims(token)
}

// CodexOAuthStatus represents the derived status of a Codex OAuth channel
type CodexOAuthStatus struct {
	AccountID               string     `json:"account_id,omitempty"`
	MaskedAccountID         string     `json:"masked_account_id,omitempty"`
	Email                   string     `json:"email,omitempty"`
	PlanType                string     `json:"plan_type,omitempty"`
	SubscriptionActiveStart *time.Time `json:"subscription_active_start,omitempty"`
	SubscriptionActiveUntil *time.Time `json:"subscription_active_until,omitempty"`
	SubscriptionLastChecked *time.Time `json:"subscription_last_checked,omitempty"`
	TokenExpiresAt          *time.Time `json:"token_expires_at,omitempty"`
	LastRefresh             string     `json:"last_refresh,omitempty"`
}

// ParseOAuthStatus extracts OAuth status from id_token and access_token.
// Returns a CodexOAuthStatus with redacted sensitive information.
func ParseOAuthStatus(idToken, accessToken, lastRefresh string) (*CodexOAuthStatus, error) {
	status := &CodexOAuthStatus{
		LastRefresh: lastRefresh,
	}

	// Parse id_token for subscription/plan info
	if idToken != "" {
		claims, err := parseJWTClaims(idToken)
		if err == nil {
			status.Email = claims.Email
			if claims.CodexAuthInfo != nil {
				status.AccountID = claims.CodexAuthInfo.ChatGPTAccountID
				status.MaskedAccountID = maskAccountID(claims.CodexAuthInfo.ChatGPTAccountID)
				status.PlanType = claims.CodexAuthInfo.ChatGPTPlanType

				// Parse subscription timestamps
				if t := parseTimestamp(claims.CodexAuthInfo.ChatGPTSubscriptionActiveStart); t != nil {
					status.SubscriptionActiveStart = t
				}
				if t := parseTimestamp(claims.CodexAuthInfo.ChatGPTSubscriptionActiveUntil); t != nil {
					status.SubscriptionActiveUntil = t
				}
				if t := parseTimestamp(claims.CodexAuthInfo.ChatGPTSubscriptionLastChecked); t != nil {
					status.SubscriptionLastChecked = t
				}
			}
		}
	}

	// Parse access_token for expiry
	if accessToken != "" {
		expiry, err := ParseJWTExpiry(accessToken)
		if err == nil {
			status.TokenExpiresAt = &expiry
		}
	}

	return status, nil
}

// maskAccountID masks an account ID for display (shows first 8 and last 4 chars)
func maskAccountID(accountID string) string {
	if len(accountID) <= 12 {
		return accountID
	}
	return accountID[:8] + "..." + accountID[len(accountID)-4:]
}

// parseTimestamp attempts to parse various timestamp formats
func parseTimestamp(v any) *time.Time {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		// Try RFC3339 first
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return &t
		}
		// Try RFC3339Nano
		if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
			return &t
		}
		// Try common date format
		if t, err := time.Parse("2006-01-02", val); err == nil {
			return &t
		}
	case float64:
		if val <= 0 {
			return nil
		}
		t := time.Unix(int64(val), 0)
		return &t
	case int64:
		if val <= 0 {
			return nil
		}
		t := time.Unix(val, 0)
		return &t
	case int:
		if val <= 0 {
			return nil
		}
		t := time.Unix(int64(val), 0)
		return &t
	case json.Number:
		if i, err := val.Int64(); err == nil && i > 0 {
			t := time.Unix(i, 0)
			return &t
		}
	}
	return nil
}
