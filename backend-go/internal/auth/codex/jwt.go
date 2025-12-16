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
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	ChatGPTUserID    string `json:"chatgpt_user_id"`
	ChatGPTPlanType  string `json:"chatgpt_plan_type"`
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
