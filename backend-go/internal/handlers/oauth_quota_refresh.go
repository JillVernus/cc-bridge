package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/gin-gonic/gin"
)

var (
	codexOAuthUsageEndpoints = []string{
		"https://chatgpt.com/backend-api/codex/usage",
		"https://chatgpt.com/backend-api/wham/usage",
	}
	codexOAuthResetCreditStatusEndpoints = []string{
		"https://chatgpt.com/backend-api/wham/rate-limit-reset-credits",
		"https://chatgpt.com/backend-api/codex/rate-limit-reset-credits",
	}
	codexOAuthResetCreditEndpoints = []string{
		"https://chatgpt.com/backend-api/codex/rate-limit-reset-credits/consume",
		"https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume",
	}
	codexOAuthUsageHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

// RefreshResponsesChannelOAuthQuota refreshes Codex OAuth quota for a Responses
// channel using the current mutable index.
func RefreshResponsesChannelOAuthQuota(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.ResponsesUpstream) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		refreshResponsesChannelOAuthQuota(c, cfgManager, id, &cfg.ResponsesUpstream[id])
	}
}

// RefreshResponsesChannelOAuthQuotaByChannelID refreshes Codex OAuth quota for a
// Responses channel using its stable channel ID.
func RefreshResponsesChannelOAuthQuotaByChannelID(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID := c.Param("channelID")
		upstream, index, ok := findResponsesUpstreamByIDWithIndex(cfgManager, channelID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		refreshResponsesChannelOAuthQuota(c, cfgManager, index, upstream)
	}
}

// ResetResponsesChannelOAuthQuota consumes one earned OpenAI reset credit for a
// Responses channel using the current mutable index.
func ResetResponsesChannelOAuthQuota(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
			return
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.ResponsesUpstream) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		resetResponsesChannelOAuthQuota(c, cfgManager, id, &cfg.ResponsesUpstream[id])
	}
}

// ResetResponsesChannelOAuthQuotaByChannelID consumes one earned OpenAI reset
// credit for a Responses channel using its stable channel ID.
func ResetResponsesChannelOAuthQuotaByChannelID(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID := c.Param("channelID")
		upstream, index, ok := findResponsesUpstreamByIDWithIndex(cfgManager, channelID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}

		resetResponsesChannelOAuthQuota(c, cfgManager, index, upstream)
	}
}

func refreshResponsesChannelOAuthQuota(c *gin.Context, cfgManager *config.ConfigManager, channelIndex int, upstream *config.UpstreamConfig) {
	if upstream == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}
	if upstream.ServiceType != "openai-oauth" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "Channel is not an OAuth channel",
			"serviceType": upstream.ServiceType,
		})
		return
	}
	if upstream.OAuthTokens == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OAuth tokens not configured"})
		return
	}

	accessToken, accountID, updatedTokens, err := codexTokenManager.GetValidToken(upstream.OAuthTokens)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to get valid OAuth token"})
		return
	}
	if updatedTokens != nil {
		if saveErr := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, updatedTokens); saveErr != nil {
			log.Printf("⚠️ [OAuth] 保存刷新后的 token 失败: %v", saveErr)
		}
	}

	codexInfo, err := fetchCodexOAuthUsageQuota(c.Request.Context(), accessToken, accountID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to refresh Codex quota", "message": err.Error()})
		return
	}

	if err := quota.GetManager().UpdateCodexQuotaForChannel(channelIndex, upstream.ID, upstream.Name, codexInfo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to persist Codex quota", "message": err.Error()})
		return
	}
	writeResponsesChannelOAuthStatus(c, channelIndex, upstream)
}

func resetResponsesChannelOAuthQuota(c *gin.Context, cfgManager *config.ConfigManager, channelIndex int, upstream *config.UpstreamConfig) {
	if upstream == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}
	if upstream.ServiceType != "openai-oauth" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "Channel is not an OAuth channel",
			"serviceType": upstream.ServiceType,
		})
		return
	}
	if upstream.OAuthTokens == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OAuth tokens not configured"})
		return
	}

	accessToken, accountID, updatedTokens, err := codexTokenManager.GetValidToken(upstream.OAuthTokens)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Failed to get valid OAuth token"})
		return
	}
	if updatedTokens != nil {
		if saveErr := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, updatedTokens); saveErr != nil {
			log.Printf("⚠️ [OAuth] 保存刷新后的 token 失败: %v", saveErr)
		}
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		var req struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		if errBindJSON := c.ShouldBindJSON(&req); errBindJSON == nil {
			idempotencyKey = strings.TrimSpace(req.IdempotencyKey)
		}
	}
	if idempotencyKey == "" {
		idempotencyKey = newResetCreditRedeemRequestID()
	}

	resetResult, err := consumeCodexOAuthResetCredit(c.Request.Context(), accessToken, accountID, idempotencyKey)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to reset Codex quota", "message": err.Error()})
		return
	}

	codexInfo, err := fetchCodexOAuthUsageQuota(c.Request.Context(), accessToken, accountID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Reset consumed, but failed to refresh Codex quota", "message": err.Error(), "reset": resetResult})
		return
	}

	if err := quota.GetManager().UpdateCodexQuotaForChannel(channelIndex, upstream.ID, upstream.Name, codexInfo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to persist Codex quota", "message": err.Error(), "reset": resetResult})
		return
	}

	response, statusCode := buildResponsesChannelOAuthStatusPayload(channelIndex, upstream)
	response["reset"] = resetResult
	c.JSON(statusCode, response)
}

func fetchCodexOAuthUsageQuota(ctx context.Context, accessToken string, accountID string) (*quota.CodexQuotaInfo, error) {
	accessToken = strings.TrimSpace(accessToken)
	accountID = strings.TrimSpace(accountID)
	if accessToken == "" {
		return nil, fmt.Errorf("missing access token")
	}
	if accountID == "" {
		return nil, fmt.Errorf("missing account id")
	}

	var lastErr error
	for _, endpoint := range codexOAuthUsageEndpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		info, err := fetchCodexOAuthUsageQuotaFromEndpoint(ctx, endpoint, accessToken, accountID)
		if err == nil {
			enrichCodexOAuthResetCreditDetails(ctx, accessToken, accountID, info)
			return info, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no usage endpoints configured")
	}
	return nil, lastErr
}

func fetchCodexOAuthUsageQuotaFromEndpoint(ctx context.Context, endpoint string, accessToken string, accountID string) (*quota.CodexQuotaInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.118.0")

	resp, err := codexOAuthUsageHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("failed to close codex usage response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("usage endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return quota.ParseCodexUsagePayload(body)
}

func enrichCodexOAuthResetCreditDetails(ctx context.Context, accessToken string, accountID string, info *quota.CodexQuotaInfo) {
	if info == nil {
		return
	}
	details, err := fetchCodexOAuthResetCreditDetails(ctx, accessToken, accountID)
	if err != nil {
		log.Printf("failed to refresh codex reset credit details: %v", err)
		return
	}
	if details == nil {
		return
	}
	if existing := info.RateLimitResetCredits; existing != nil {
		if details.CreatedAt == nil {
			details.CreatedAt = existing.CreatedAt
		}
		if details.ExpiresAt == nil {
			details.ExpiresAt = existing.ExpiresAt
		}
		if details.TotalEarnedCount == 0 {
			details.TotalEarnedCount = existing.TotalEarnedCount
		}
	}
	info.RateLimitResetCredits = details
}

func fetchCodexOAuthResetCreditDetails(ctx context.Context, accessToken string, accountID string) (*quota.CodexRateLimitResetCreditsInfo, error) {
	accessToken = strings.TrimSpace(accessToken)
	accountID = strings.TrimSpace(accountID)
	if accessToken == "" {
		return nil, fmt.Errorf("missing access token")
	}

	var lastErr error
	for _, endpoint := range codexOAuthResetCreditStatusEndpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		info, err := fetchCodexOAuthResetCreditDetailsFromEndpoint(ctx, endpoint, accessToken, accountID)
		if err == nil {
			return info, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no reset credit status endpoints configured")
	}
	return nil, lastErr
}

func fetchCodexOAuthResetCreditDetailsFromEndpoint(ctx context.Context, endpoint string, accessToken string, accountID string) (*quota.CodexRateLimitResetCreditsInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if strings.TrimSpace(accountID) != "" {
		req.Header.Set("Chatgpt-Account-Id", accountID)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "codex_cli_rs/0.118.0")

	resp, err := codexOAuthUsageHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("failed to close codex reset credit details response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("reset credit details endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return quota.ParseCodexRateLimitResetCreditsPayload(body)
}

func consumeCodexOAuthResetCredit(ctx context.Context, accessToken string, accountID string, idempotencyKey string) (gin.H, error) {
	accessToken = strings.TrimSpace(accessToken)
	accountID = strings.TrimSpace(accountID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if accessToken == "" {
		return nil, fmt.Errorf("missing access token")
	}
	if accountID == "" {
		return nil, fmt.Errorf("missing account id")
	}
	if idempotencyKey == "" {
		return nil, fmt.Errorf("missing idempotency key")
	}

	var lastErr error
	for _, endpoint := range codexOAuthResetCreditEndpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		result, err := consumeCodexOAuthResetCreditFromEndpoint(ctx, endpoint, accessToken, accountID, idempotencyKey)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no reset credit endpoints configured")
	}
	return nil, lastErr
}

func consumeCodexOAuthResetCreditFromEndpoint(ctx context.Context, endpoint string, accessToken string, accountID string, idempotencyKey string) (gin.H, error) {
	body, err := json.Marshal(gin.H{"redeem_request_id": idempotencyKey})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.118.0")

	resp, err := codexOAuthUsageHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("failed to close codex reset credit response body: %v", closeErr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("reset credit endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var raw map[string]any
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("parse reset credit response: %w", err)
	}
	outcome := strings.TrimSpace(stringFromMap(raw, "code", "outcome"))
	if outcome == "" {
		return nil, fmt.Errorf("reset credit response missing code")
	}
	result := gin.H{
		"outcome":         outcome,
		"idempotency_key": idempotencyKey,
	}
	if windowsReset, ok := numericFromMap(raw, "windows_reset", "windowsReset"); ok {
		result["windows_reset"] = int(windowsReset)
	}
	return result, nil
}

func newResetCreditRedeemRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("cc-bridge-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func numericFromMap(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case json.Number:
			f, err := v.Float64()
			return f, err == nil
		}
	}
	return 0, false
}
