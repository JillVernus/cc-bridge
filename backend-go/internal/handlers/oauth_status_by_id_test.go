package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestRefreshResponsesChannelOAuthQuotaByChannelID_QueriesUsageAndUpdatesStoredQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var observedPath string
	var observedAuth string
	var observedAccount string
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
		observedAuth = r.Header.Get("Authorization")
		observedAccount = r.Header.Get("Chatgpt-Account-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"plan_type": "pro",
			"rate_limit": {
				"primary_window": {
					"used_percent": 15,
					"limit_window_seconds": 18000,
					"reset_at": 1893456000
				},
				"secondary_window": {
					"used_percent": 48,
					"limit_window_seconds": 604800,
					"reset_at": 1894060800
				}
			}
		}`)
	}))
	defer usageServer.Close()

	oldEndpoints := codexOAuthUsageEndpoints
	oldHTTPClient := codexOAuthUsageHTTPClient
	codexOAuthUsageEndpoints = []string{usageServer.URL + "/backend-api/codex/usage"}
	codexOAuthUsageHTTPClient = usageServer.Client()
	t.Cleanup(func() {
		codexOAuthUsageEndpoints = oldEndpoints
		codexOAuthUsageHTTPClient = oldHTTPClient
	})

	cfgManager := createTestConfigManager(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "oauth-refresh-stable-a",
				Name:        "OAuth Refresh A",
				ServiceType: "openai-oauth",
				BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
				OAuthTokens: &config.OAuthTokens{
					AccessToken:  makeFutureJWT(t),
					RefreshToken: "refresh-token",
					AccountID:    "acct-refresh-123",
				},
			},
		},
		ResponsesLoadBalance: "failover",
	})

	router := gin.New()
	router.POST("/api/responses/channels/by-id/:channelID/oauth/quota/refresh", RefreshResponsesChannelOAuthQuotaByChannelID(cfgManager))

	req := httptest.NewRequest(http.MethodPost, "/api/responses/channels/by-id/oauth-refresh-stable-a/oauth/quota/refresh", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if observedPath != "/backend-api/codex/usage" {
		t.Fatalf("usage path = %q, want /backend-api/codex/usage", observedPath)
	}
	if observedAuth == "" || observedAuth == "Bearer " {
		t.Fatalf("expected Authorization bearer token, got %q", observedAuth)
	}
	if observedAccount != "acct-refresh-123" {
		t.Fatalf("Chatgpt-Account-Id = %q, want acct-refresh-123", observedAccount)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	quotaPayload, ok := payload["quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected quota payload, got %#v", payload["quota"])
	}
	codexQuota, ok := quotaPayload["codex_quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected codex_quota payload, got %#v", quotaPayload["codex_quota"])
	}
	if got := codexQuota["primary_used_percent"]; got != float64(15) {
		t.Fatalf("primary_used_percent = %#v, want 15", got)
	}
	if got := codexQuota["secondary_used_percent"]; got != float64(48) {
		t.Fatalf("secondary_used_percent = %#v, want 48", got)
	}

	stored := quota.GetManager().GetStatusForChannel(0, "oauth-refresh-stable-a", "OAuth Refresh A")
	if stored == nil || stored.CodexQuota == nil {
		t.Fatalf("expected stored quota, got %+v", stored)
	}
	if stored.CodexQuota.PlanType != "pro" || stored.CodexQuota.PrimaryUsedPercent != 15 {
		t.Fatalf("unexpected stored quota: %+v", stored.CodexQuota)
	}
}

func makeFutureJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix())))
	return header + "." + payload + ".sig"
}
