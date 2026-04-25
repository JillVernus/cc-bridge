package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/gin-gonic/gin"
)

func TestGetResponsesChannelOAuthStatusByChannelID_ReturnsMatchingOAuthChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-non-oauth",
				Name:        "Regular Responses",
				ServiceType: "responses",
				BaseURL:     "https://api.example.com/v1",
			},
			{
				ID:          "oauth-stable-a",
				Name:        "OAuth Stable A",
				ServiceType: "openai-oauth",
				BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
				OAuthTokens: &config.OAuthTokens{
					AccessToken:  "access-token",
					RefreshToken: "refresh-token",
					AccountID:    "acct-123",
				},
			},
		},
		ResponsesLoadBalance: "failover",
	})

	headers := http.Header{}
	headers.Set("X-Codex-Plan-Type", "plus")
	headers.Set("X-Codex-Primary-Used-Percent", "12")
	headers.Set("X-Codex-Secondary-Used-Percent", "44")
	quota.GetManager().UpdateFromHeadersForChannel(1, "oauth-stable-a", "OAuth Stable A", headers)

	router := gin.New()
	router.GET("/api/responses/channels/by-id/:channelID/oauth/status", GetResponsesChannelOAuthStatusByChannelID(cfgManager))

	req := httptest.NewRequest(http.MethodGet, "/api/responses/channels/by-id/oauth-stable-a/oauth/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["channelName"]; got != "OAuth Stable A" {
		t.Fatalf("channelName = %#v, want %q", got, "OAuth Stable A")
	}
	if got := payload["channelId"]; got != float64(1) {
		t.Fatalf("channelId = %#v, want 1", got)
	}

	quotaPayload, ok := payload["quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected quota payload, got %#v", payload["quota"])
	}
	codexQuota, ok := quotaPayload["codex_quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected codex_quota payload, got %#v", quotaPayload["codex_quota"])
	}
	if got := codexQuota["primary_used_percent"]; got != float64(12) {
		t.Fatalf("primary_used_percent = %#v, want 12", got)
	}
}

func TestGetResponsesChannelOAuthStatusByChannelID_RejectsUnknownChannelID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := createTestConfigManager(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "oauth-stable-a",
				Name:        "OAuth Stable A",
				ServiceType: "openai-oauth",
				BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
			},
		},
		ResponsesLoadBalance: "failover",
	})

	router := gin.New()
	router.GET("/api/responses/channels/by-id/:channelID/oauth/status", GetResponsesChannelOAuthStatusByChannelID(cfgManager))

	req := httptest.NewRequest(http.MethodGet, "/api/responses/channels/by-id/missing/oauth/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d, body=%s", rec.Code, rec.Body.String())
	}
}
