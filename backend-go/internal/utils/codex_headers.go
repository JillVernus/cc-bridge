package utils

import (
	"net/http"
)

// SetCodexOAuthHeaders sets the required headers for Codex OAuth API requests.
// These headers are required when authenticating with a ChatGPT subscription OAuth token
// instead of a standard API key.
func SetCodexOAuthHeaders(headers http.Header, accessToken, accountID string) {
	// Authentication
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("Chatgpt-Account-Id", accountID)

	// Codex CLI identification
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("Version", "0.21.0")

	// OpenAI beta flag for Responses API
	headers.Set("Openai-Beta", "responses=experimental")

	// User-Agent mimicking official Codex CLI
	headers.Set("User-Agent", "codex_cli_rs/0.50.0 (Linux; x86_64)")

	// Content headers
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")

	// Connection
	headers.Set("Connection", "Keep-Alive")
}

// SetCodexOAuthStreamHeaders sets headers specifically for streaming Codex OAuth requests.
// This is a convenience wrapper that includes stream-specific Accept header.
func SetCodexOAuthStreamHeaders(headers http.Header, accessToken, accountID string) {
	SetCodexOAuthHeaders(headers, accessToken, accountID)
	// Ensure streaming Accept header is set
	headers.Set("Accept", "text/event-stream")
}

// SetCodexOAuthNonStreamHeaders sets headers for non-streaming Codex OAuth requests.
func SetCodexOAuthNonStreamHeaders(headers http.Header, accessToken, accountID string) {
	SetCodexOAuthHeaders(headers, accessToken, accountID)
	// Override Accept for non-streaming
	headers.Set("Accept", "application/json")
}
