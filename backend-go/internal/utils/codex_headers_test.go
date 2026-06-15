package utils

import (
	"net/http"
	"testing"
)

func TestSetCodexOAuthHeadersAcceptsCodexTUIUserAgent(t *testing.T) {
	headers := http.Header{}
	incoming := "codex-tui/0.139.0 (Ubuntu 24.4.0; aarch64) vscode/1.124.2 (codex-tui; 0.139.0)"

	SetCodexOAuthHeaders(headers, CodexOAuthHeadersInput{
		AccessToken: "access-token",
		AccountID:   "account-id",
		UserAgent:   incoming,
	})

	if got := headers.Get("User-Agent"); got != incoming {
		t.Fatalf("User-Agent = %q, want %q", got, incoming)
	}
}
