package utils

import (
	"net/http"
	"strings"
)

// DefaultCodexUserAgent is the fallback User-Agent when the incoming request
// doesn't have a valid Codex CLI User-Agent
const DefaultCodexUserAgent = "codex_cli_rs/0.73.0 (Linux; x86_64)"

// CodexOAuthHeadersInput contains the headers to forward from the incoming request
type CodexOAuthHeadersInput struct {
	AccessToken    string
	AccountID      string
	UserAgent      string // Original User-Agent from incoming request
	ConversationID string // Original Conversation_id from incoming request
	SessionID      string // Original Session_id from incoming request
	Originator     string // Original Originator from incoming request
}

// SetCodexOAuthHeaders sets the required headers for Codex OAuth API requests.
// These headers are required when authenticating with a ChatGPT subscription OAuth token
// instead of a standard API key.
func SetCodexOAuthHeaders(headers http.Header, input CodexOAuthHeadersInput) {
	// Authentication (OAuth specific)
	headers.Set("Authorization", "Bearer "+input.AccessToken)
	headers.Set("Chatgpt-Account-Id", input.AccountID)

	// Originator - forward from incoming request or use default
	originator := input.Originator
	if originator == "" {
		originator = "codex_cli_rs"
	}
	headers.Set("Originator", originator)

	// User-Agent - forward if it's a valid Codex CLI User-Agent, otherwise use fallback
	userAgent := input.UserAgent
	if !isValidCodexUserAgent(userAgent) {
		userAgent = DefaultCodexUserAgent
	}
	headers.Set("User-Agent", userAgent)

	// Session tracking headers - forward from incoming request
	if input.ConversationID != "" {
		headers.Set("Conversation_id", input.ConversationID)
	}
	if input.SessionID != "" {
		headers.Set("Session_id", input.SessionID)
	}

	// Content headers
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
}

// SetCodexOAuthStreamHeaders sets headers specifically for streaming Codex OAuth requests.
func SetCodexOAuthStreamHeaders(headers http.Header, input CodexOAuthHeadersInput) {
	SetCodexOAuthHeaders(headers, input)
	// Ensure streaming Accept header is set
	headers.Set("Accept", "text/event-stream")
}

// SetCodexOAuthNonStreamHeaders sets headers for non-streaming Codex OAuth requests.
func SetCodexOAuthNonStreamHeaders(headers http.Header, input CodexOAuthHeadersInput) {
	SetCodexOAuthHeaders(headers, input)
	// Override Accept for non-streaming
	headers.Set("Accept", "application/json")
}

// isValidCodexUserAgent checks if the User-Agent is from the official Codex CLI
func isValidCodexUserAgent(userAgent string) bool {
	return strings.HasPrefix(userAgent, "codex_cli_rs/")
}
