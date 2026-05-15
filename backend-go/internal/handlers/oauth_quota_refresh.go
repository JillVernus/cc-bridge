package handlers

import (
	"context"
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

	quota.GetManager().UpdateCodexQuotaForChannel(channelIndex, upstream.ID, upstream.Name, codexInfo)
	writeResponsesChannelOAuthStatus(c, channelIndex, upstream)
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
