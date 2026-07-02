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
	headers.Set("X-Codex-Active-Limit", "premium")
	headers.Set("X-Codex-Primary-Used-Percent", "12")
	headers.Set("X-Codex-Secondary-Used-Percent", "44")
	headers.Set("X-Codex-Bengalfox-Limit-Name", "GPT-5.3-Codex-Spark")
	headers.Set("X-Codex-Bengalfox-Primary-Used-Percent", "1.5")
	headers.Set("X-Codex-Bengalfox-Primary-Window-Minutes", "300")
	headers.Set("X-Codex-Bengalfox-Primary-Reset-After-Seconds", "18000")
	headers.Set("X-Codex-Bengalfox-Secondary-Used-Percent", "82.25")
	headers.Set("X-Codex-Bengalfox-Secondary-Window-Minutes", "10080")
	headers.Set("X-Codex-Bengalfox-Secondary-Reset-After-Seconds", "604800")
	headers.Set("X-Ratelimit-Limit", "200")
	headers.Set("X-Ratelimit-Remaining", "199")
	headers.Set("X-Ratelimit-Reset", "1779172356")
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
	if got := codexQuota["active_limit"]; got != "premium" {
		t.Fatalf("active_limit = %#v, want premium", got)
	}
	detailedLimits, ok := codexQuota["detailed_limits"].([]any)
	if !ok || len(detailedLimits) != 1 {
		t.Fatalf("expected one detailed_limits entry, got %#v", codexQuota["detailed_limits"])
	}
	bengalfox, ok := detailedLimits[0].(map[string]any)
	if !ok {
		t.Fatalf("expected detailed limit object, got %#v", detailedLimits[0])
	}
	if got := bengalfox["limit_id"]; got != "codex_bengalfox" {
		t.Fatalf("limit_id = %#v, want codex_bengalfox", got)
	}
	if got := bengalfox["limit_name"]; got != "GPT-5.3-Codex-Spark" {
		t.Fatalf("limit_name = %#v, want GPT-5.3-Codex-Spark", got)
	}
	if got := bengalfox["primary_used_percent"]; got != 1.5 {
		t.Fatalf("primary_used_percent = %#v, want 1.5", got)
	}
	if got := bengalfox["secondary_used_percent"]; got != 82.25 {
		t.Fatalf("secondary_used_percent = %#v, want 82.25", got)
	}
	if got := bengalfox["primary_reset_after_seconds"]; got != float64(18000) {
		t.Fatalf("primary_reset_after_seconds = %#v, want 18000", got)
	}
	rateLimit, ok := quotaPayload["rate_limit"].(map[string]any)
	if !ok {
		t.Fatalf("expected rate_limit payload, got %#v", quotaPayload["rate_limit"])
	}
	if got := rateLimit["limit_requests"]; got != float64(200) {
		t.Fatalf("limit_requests = %#v, want 200", got)
	}
	if got := rateLimit["remaining_requests"]; got != float64(199) {
		t.Fatalf("remaining_requests = %#v, want 199", got)
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
					"used_percent": 0.64,
					"limit_window_seconds": 18000,
					"reset_at": 1893456000
				},
				"secondary_window": {
					"used_percent": 48.25,
					"limit_window_seconds": 604800,
					"reset_at": 1894060800
				}
			}
		}`)
	}))
	defer usageServer.Close()

	oldEndpoints := codexOAuthUsageEndpoints
	oldResetStatusEndpoints := codexOAuthResetCreditStatusEndpoints
	oldHTTPClient := codexOAuthUsageHTTPClient
	codexOAuthUsageEndpoints = []string{usageServer.URL + "/backend-api/codex/usage"}
	codexOAuthResetCreditStatusEndpoints = nil
	codexOAuthUsageHTTPClient = usageServer.Client()
	t.Cleanup(func() {
		codexOAuthUsageEndpoints = oldEndpoints
		codexOAuthResetCreditStatusEndpoints = oldResetStatusEndpoints
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
	if got := codexQuota["primary_used_percent"]; got != 0.64 {
		t.Fatalf("primary_used_percent = %#v, want 0.64", got)
	}
	if got := codexQuota["secondary_used_percent"]; got != 48.25 {
		t.Fatalf("secondary_used_percent = %#v, want 48.25", got)
	}

	stored := quota.GetManager().GetStatusForChannel(0, "oauth-refresh-stable-a", "OAuth Refresh A")
	if stored == nil || stored.CodexQuota == nil {
		t.Fatalf("expected stored quota, got %+v", stored)
	}
	if stored.CodexQuota.PlanType != "pro" || stored.CodexQuota.PrimaryUsedPercentValue() != 0.64 {
		t.Fatalf("unexpected stored quota: %+v", stored.CodexQuota)
	}
}

func TestResetResponsesChannelOAuthQuotaByChannelID_ConsumesResetCreditAndRefreshesQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var observedResetPath string
	var observedResetAuth string
	var observedResetAccount string
	var observedRedeemRequestID string
	var observedDetailsPath string
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/wham/rate-limit-reset-credits/consume":
			observedResetPath = r.URL.Path
			observedResetAuth = r.Header.Get("Authorization")
			observedResetAccount = r.Header.Get("Chatgpt-Account-Id")
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode reset body: %v", err)
			}
			observedRedeemRequestID = body["redeem_request_id"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":"reset","windows_reset":2}`)
		case "/backend-api/wham/usage":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"plan_type": "pro",
				"rate_limit": {
					"primary_window": {"used_percent": 0, "limit_window_seconds": 18000, "reset_at": 1893456000},
					"secondary_window": {"used_percent": 40, "limit_window_seconds": 604800, "reset_at": 1894060800}
				},
				"rate_limit_reset_credits": {
					"available_count": 1,
					"created_at": "2026-07-01T12:00:00Z",
					"expires_at": "2026-07-08T12:00:00Z"
				}
			}`)
		case "/backend-api/wham/rate-limit-reset-credits":
			observedDetailsPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"available_count": 1,
				"total_earned_count": 2,
				"credits": [
					{
						"id": "credit-a",
						"title": "Full reset (Weekly + 5 hours)",
						"granted_at": "2026-07-02T08:30:00Z",
						"expires_at": "2026-07-09T08:30:00Z"
					}
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer usageServer.Close()

	oldUsageEndpoints := codexOAuthUsageEndpoints
	oldResetStatusEndpoints := codexOAuthResetCreditStatusEndpoints
	oldResetEndpoints := codexOAuthResetCreditEndpoints
	oldHTTPClient := codexOAuthUsageHTTPClient
	codexOAuthUsageEndpoints = []string{usageServer.URL + "/backend-api/wham/usage"}
	codexOAuthResetCreditStatusEndpoints = []string{usageServer.URL + "/backend-api/wham/rate-limit-reset-credits"}
	codexOAuthResetCreditEndpoints = []string{usageServer.URL + "/backend-api/wham/rate-limit-reset-credits/consume"}
	codexOAuthUsageHTTPClient = usageServer.Client()
	t.Cleanup(func() {
		codexOAuthUsageEndpoints = oldUsageEndpoints
		codexOAuthResetCreditStatusEndpoints = oldResetStatusEndpoints
		codexOAuthResetCreditEndpoints = oldResetEndpoints
		codexOAuthUsageHTTPClient = oldHTTPClient
	})

	cfgManager := createTestConfigManager(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "oauth-reset-stable-a",
				Name:        "OAuth Reset A",
				ServiceType: "openai-oauth",
				BaseURL:     "https://chatgpt.com/backend-api/codex/responses",
				OAuthTokens: &config.OAuthTokens{
					AccessToken:  makeFutureJWT(t),
					RefreshToken: "refresh-token",
					AccountID:    "acct-reset-123",
				},
			},
		},
		ResponsesLoadBalance: "failover",
	})

	router := gin.New()
	router.POST("/api/responses/channels/by-id/:channelID/oauth/quota/reset-credit", ResetResponsesChannelOAuthQuotaByChannelID(cfgManager))

	req := httptest.NewRequest(http.MethodPost, "/api/responses/channels/by-id/oauth-reset-stable-a/oauth/quota/reset-credit", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if observedResetPath != "/backend-api/wham/rate-limit-reset-credits/consume" {
		t.Fatalf("reset path = %q, want /backend-api/wham/rate-limit-reset-credits/consume", observedResetPath)
	}
	if observedResetAuth == "" || observedResetAuth == "Bearer " {
		t.Fatalf("expected Authorization bearer token, got %q", observedResetAuth)
	}
	if observedResetAccount != "acct-reset-123" {
		t.Fatalf("Chatgpt-Account-Id = %q, want acct-reset-123", observedResetAccount)
	}
	if observedRedeemRequestID == "" {
		t.Fatalf("redeem_request_id should be generated")
	}
	if observedDetailsPath != "/backend-api/wham/rate-limit-reset-credits" {
		t.Fatalf("details path = %q, want /backend-api/wham/rate-limit-reset-credits", observedDetailsPath)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	resetPayload, ok := payload["reset"].(map[string]any)
	if !ok {
		t.Fatalf("expected reset payload, got %#v", payload["reset"])
	}
	if got := resetPayload["outcome"]; got != "reset" {
		t.Fatalf("reset outcome = %#v, want reset", got)
	}
	quotaPayload, ok := payload["quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected quota payload, got %#v", payload["quota"])
	}
	codexQuota, ok := quotaPayload["codex_quota"].(map[string]any)
	if !ok {
		t.Fatalf("expected codex_quota payload, got %#v", quotaPayload["codex_quota"])
	}
	resetCredits, ok := codexQuota["rate_limit_reset_credits"].(map[string]any)
	if !ok {
		t.Fatalf("expected rate_limit_reset_credits payload, got %#v", codexQuota["rate_limit_reset_credits"])
	}
	if got := resetCredits["available_count"]; got != float64(1) {
		t.Fatalf("available_count = %#v, want 1", got)
	}
	if got := resetCredits["total_earned_count"]; got != float64(2) {
		t.Fatalf("total_earned_count = %#v, want 2", got)
	}
	credits, ok := resetCredits["credits"].([]any)
	if !ok || len(credits) != 1 {
		t.Fatalf("credits = %#v, want one credit", resetCredits["credits"])
	}
	credit, ok := credits[0].(map[string]any)
	if !ok {
		t.Fatalf("credit = %#v, want object", credits[0])
	}
	if got := credit["title"]; got != "Full reset (Weekly + 5 hours)" {
		t.Fatalf("title = %#v, want title", got)
	}
	if got := credit["granted_at"]; got != "2026-07-02T08:30:00Z" {
		t.Fatalf("granted_at = %#v, want timestamp", got)
	}
	if got := credit["expires_at"]; got != "2026-07-09T08:30:00Z" {
		t.Fatalf("expires_at = %#v, want timestamp", got)
	}
}

func makeFutureJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix())))
	return header + "." + payload + ".sig"
}
