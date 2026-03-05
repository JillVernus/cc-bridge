package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/httpclient"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// ProxyHandler 代理处理器
// 支持多渠道调度：当配置多个渠道时自动启用
func ProxyHandler(envCfg *config.EnvConfig, cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler, reqLogManager *requestlog.Manager) gin.HandlerFunc {
	return ProxyHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, nil, nil, nil, nil)
}

// ProxyHandlerWithAPIKey 代理处理器（支持 API Key 验证）
func ProxyHandlerWithAPIKey(envCfg *config.EnvConfig, cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler, reqLogManager *requestlog.Manager, apiKeyManager *apikey.Manager, usageManager *quota.UsageManager, failoverTracker *config.FailoverTracker, channelRateLimiter *middleware.ChannelRateLimiter) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// 先进行认证（如果上游中间件尚未完成认证）
		if _, exists := c.Get(middleware.ContextKeyAPIKeyID); !exists {
			middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager)(c)
			if c.IsAborted() {
				return
			}
		}

		// Check endpoint permission
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckEndpointPermission("messages") {
					c.JSON(403, gin.H{
						"error": "Endpoint /v1/messages not allowed for this API key",
						"code":  "ENDPOINT_NOT_ALLOWED",
					})
					return
				}
			}
		}

		startTime := time.Now()

		// 读取原始请求体
		maxBodyMB := envCfg.MaxRequestBodyMB
		if maxBodyMB <= 0 {
			maxBodyMB = 20
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBodyMB)*1024*1024)

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error":   "Request body too large",
					"message": fmt.Sprintf("Maximum allowed request size is %d MB", maxBodyMB),
				})
				return
			}
			c.JSON(400, gin.H{"error": "Failed to read request body"})
			return
		}
		// 恢复请求体供后续使用
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Store request data for debug logging
		StoreDebugRequestData(c, bodyBytes)

		// claudeReq 变量用于判断是否流式请求和提取 user_id
		var claudeReq types.ClaudeRequest
		if len(bodyBytes) > 0 {
			claudeReq = parseClaudeRequestForRouting(bodyBytes)
		}

		// Check model permission
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckModelPermission(claudeReq.Model) {
					c.JSON(403, gin.H{
						"error": fmt.Sprintf("Model %s not allowed for this API key", claudeReq.Model),
						"code":  "MODEL_NOT_ALLOWED",
					})
					return
				}
			}
		}

		// 提取 user_id 用于 Trace 亲和性
		compoundUserID := extractUserID(bodyBytes)
		userID, sessionID := parseClaudeCodeUserID(compoundUserID)

		// 提取 API Key ID 用于请求日志 (nil 表示未设置)
		var apiKeyID *int64
		if id, exists := c.Get(middleware.ContextKeyAPIKeyID); exists {
			if idVal, ok := id.(int64); ok {
				apiKeyID = &idVal
			}
		}

		// 创建 pending 请求日志记录
		var requestLogID string
		if reqLogManager != nil {
			pendingLog := &requestlog.RequestLog{
				Status:      requestlog.StatusPending,
				InitialTime: startTime,
				Model:       claudeReq.Model,
				Stream:      claudeReq.Stream,
				Endpoint:    "/v1/messages",
				ClientID:    userID,
				SessionID:   sessionID,
				APIKeyID:    apiKeyID,
			}
			if err := reqLogManager.Add(pendingLog); err != nil {
				log.Printf("⚠️ 创建 pending 请求日志失败: %v", err)
			} else {
				requestLogID = pendingLog.ID
			}
		}

		// 检查是否为多渠道模式
		isMultiChannel := channelScheduler.IsMultiChannelMode(false)

		// Get allowed channels from API key permissions
		var allowedChannelsMsg []string
		var allowedChannelsResp []string
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				allowedChannelsMsg = validatedKey.GetAllowedChannelsByType("messages")
				allowedChannelsResp = validatedKey.GetAllowedChannelsByType("responses")
			}
		}

		if isMultiChannel {
			// 多渠道模式：使用调度器
			handleMultiChannelProxy(c, envCfg, cfgManager, channelScheduler, bodyBytes, claudeReq, userID, sessionID, apiKeyID, startTime, reqLogManager, requestLogID, usageManager, allowedChannelsMsg, allowedChannelsResp, failoverTracker, channelRateLimiter)
		} else {
			// 单渠道模式：使用现有逻辑
			handleSingleChannelProxy(c, envCfg, cfgManager, channelScheduler, bodyBytes, claudeReq, startTime, reqLogManager, requestLogID, usageManager, allowedChannelsMsg, allowedChannelsResp, failoverTracker, userID, sessionID, apiKeyID, channelRateLimiter)
		}
	})
}

func parseClaudeRequestForRouting(bodyBytes []byte) types.ClaudeRequest {
	var req types.ClaudeRequest
	if len(bodyBytes) == 0 {
		return req
	}

	if err := json.Unmarshal(bodyBytes, &req); err == nil {
		return req
	}

	// Fallback for payloads that are valid JSON but not strictly compatible with
	// ClaudeRequest (for example nullable/non-standard fields). We only need
	// routing-critical fields here.
	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return req
	}

	if model, ok := raw["model"].(string); ok {
		req.Model = strings.TrimSpace(model)
	}

	switch stream := raw["stream"].(type) {
	case bool:
		req.Stream = stream
	case string:
		req.Stream = strings.EqualFold(strings.TrimSpace(stream), "true")
	}

	return req
}

// extractUserID 从请求体中提取 user_id
func extractUserID(bodyBytes []byte) string {
	var req struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err == nil {
		return req.Metadata.UserID
	}
	return ""
}

// parseClaudeCodeUserID 解析 Claude Code 的复合 user_id 格式
// 格式: user_<hash>_account__session_<session_uuid>
// 返回: (userID, sessionID)
func parseClaudeCodeUserID(compoundUserID string) (userID string, sessionID string) {
	compoundUserID = strings.TrimSpace(compoundUserID)
	if compoundUserID == "" {
		return "", ""
	}

	// 查找分隔符 "_account__session_"
	const delimiter = "_account__session_"
	idx := strings.Index(compoundUserID, delimiter)
	if idx == -1 {
		// 没有找到分隔符，整个字符串作为 userID
		return compoundUserID, ""
	}

	userID = strings.TrimSpace(compoundUserID[:idx])
	sessionID = strings.TrimSpace(compoundUserID[idx+len(delimiter):])
	return userID, sessionID
}

func resolveRequestLogChannelContext(selection *scheduler.SelectionResult, fallback *config.UpstreamConfig) (int, string) {
	if selection != nil && selection.CompositeUpstream != nil && selection.CompositeChannelIndex >= 0 {
		return selection.CompositeChannelIndex, strings.TrimSpace(selection.CompositeUpstream.Name)
	}
	if selection != nil && selection.Upstream != nil {
		return selection.ChannelIndex, strings.TrimSpace(selection.Upstream.Name)
	}
	if fallback != nil {
		return fallback.Index, strings.TrimSpace(fallback.Name)
	}
	return -1, ""
}

func isChannelAllowed(channelID string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, id := range allowed {
		if id == channelID {
			return true
		}
	}
	return false
}

func isResolvedTargetAllowedForPool(channelID string, pool string, allowedMessages []string, allowedResponses []string) bool {
	switch config.NormalizeCompositeTargetPool(pool) {
	case config.CompositeTargetPoolResponses:
		return isChannelAllowed(channelID, allowedResponses)
	default:
		return isChannelAllowed(channelID, allowedMessages)
	}
}

var getValidOAuthTokenForMessagesBridge = func(tokens *config.OAuthTokens) (string, string, *config.OAuthTokens, error) {
	return codexTokenManager.GetValidToken(tokens)
}

var refreshOAuthTokensForMessagesBridge = func(refreshToken string, retries int) (*config.OAuthTokens, error) {
	return codexTokenManager.RefreshTokensWithRetry(refreshToken, retries)
}

// handleMultiChannelProxy handles multi-channel proxy requests with failover support.
// When a channel fails and there are more channels to try, it logs the failed attempt
// with StatusFailover and creates a new pending log for the next attempt.
func handleMultiChannelProxy(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	claudeReq types.ClaudeRequest,
	clientID string,
	sessionID string,
	apiKeyID *int64,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannelsMsg []string,
	allowedChannelsResp []string,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) {
	failedChannels := make(map[int]bool)
	var lastError error
	var lastResolvedTargetPermissionErr error
	var sawNonPermissionFailure bool
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}
	var lastFailedUpstream *config.UpstreamConfig

	// Get active channel count as max retry attempts
	maxChannelAttempts := channelScheduler.GetActiveChannelCount(false)
	shouldLogInfo := envCfg.ShouldLog("info")

	// Track current selection for composite failover chain
	var currentSelection *scheduler.SelectionResult

	// channelAttempt counts top-level channel selections.
	// Composite chain hops do not consume this budget.
	channelAttempt := 0
	for {
		var selection *scheduler.SelectionResult
		var err error

		// Check if we're continuing a composite failover chain.
		// Always delegate to GetNextCompositeFailover for sticky composite behavior;
		// it returns an error when the chain is exhausted.
		if currentSelection != nil && currentSelection.CompositeUpstream != nil {
			// Try next in composite failover chain
			selection, err = channelScheduler.GetNextCompositeFailover(currentSelection, false, failedChannels, nil, false)
			if err != nil {
				// If there were remaining failover entries but none could be selected,
				// treat it as non-permission exhaustion (e.g., missing credentials/unavailable targets).
				if len(currentSelection.FailoverChain) > 0 {
					sawNonPermissionFailure = true
				}
				if lastFailoverError == nil && lastResolvedTargetPermissionErr != nil && !sawNonPermissionFailure {
					httpStatus := http.StatusForbidden
					errMsg := lastResolvedTargetPermissionErr.Error()
					if reqLogManager != nil && requestLogID != "" {
						record := &requestlog.RequestLog{
							Status:       requestlog.StatusError,
							CompleteTime: time.Now(),
							DurationMs:   time.Since(startTime).Milliseconds(),
							Model:        claudeReq.Model,
							HTTPStatus:   httpStatus,
							Error:        errMsg,
						}
						logChannelIndex, logChannelName := resolveRequestLogChannelContext(currentSelection, lastFailedUpstream)
						if logChannelIndex >= 0 || logChannelName != "" {
							record.ChannelID = logChannelIndex
							record.ChannelName = logChannelName
						}
						if lastFailedUpstream != nil {
							record.Type = lastFailedUpstream.ServiceType
							record.ProviderName = lastFailedUpstream.Name
						}
						_ = reqLogManager.Update(requestLogID, record)
					}
					errJSON := fmt.Sprintf(`{"error":"channel not allowed","details":"%s","code":"CHANNEL_NOT_ALLOWED"}`, errMsg)
					SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, httpStatus, []byte(errJSON))
					c.JSON(httpStatus, gin.H{
						"error":   "channel not allowed",
						"details": errMsg,
						"code":    "CHANNEL_NOT_ALLOWED",
					})
					return
				}

				// Composite failover chain exhausted - do NOT try other channels
				// Return error to client immediately (sticky composite behavior)
				log.Printf("💥 [Composite] Failover chain exhausted for '%s': %v", currentSelection.CompositeUpstream.Name, err)

				// Update request log with final error status
				if reqLogManager != nil && requestLogID != "" {
					httpStatus := 503
					errMsg := fmt.Sprintf("composite channel '%s' failover chain exhausted", currentSelection.CompositeUpstream.Name)
					upstreamErr := ""
					failoverInfo := ""
					if lastFailoverError != nil && lastFailoverError.Status != 0 {
						httpStatus = lastFailoverError.Status
						upstreamErr = string(lastFailoverError.Body)
						failoverInfo = lastFailoverError.FailoverInfo
					}
					record := &requestlog.RequestLog{
						Status:        requestlog.StatusError,
						CompleteTime:  time.Now(),
						DurationMs:    time.Since(startTime).Milliseconds(),
						Model:         claudeReq.Model,
						HTTPStatus:    httpStatus,
						Error:         errMsg,
						UpstreamError: upstreamErr,
						FailoverInfo:  failoverInfo,
					}
					logChannelIndex, logChannelName := resolveRequestLogChannelContext(currentSelection, lastFailedUpstream)
					if logChannelIndex >= 0 || logChannelName != "" {
						record.ChannelID = logChannelIndex
						record.ChannelName = logChannelName
					}
					if lastFailedUpstream != nil {
						record.Type = lastFailedUpstream.ServiceType
						record.ProviderName = lastFailedUpstream.Name
					}
					_ = reqLogManager.Update(requestLogID, record)
				}

				// Return error response to client
				if lastFailoverError != nil {
					status := lastFailoverError.Status
					if status == 0 {
						status = 503
					}
					SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, lastFailoverError.Body)
					var errBody map[string]interface{}
					if jsonErr := json.Unmarshal(lastFailoverError.Body, &errBody); jsonErr == nil {
						c.JSON(status, errBody)
					} else {
						c.JSON(status, gin.H{"error": string(lastFailoverError.Body)})
					}
				} else {
					errMsg := fmt.Sprintf("composite channel '%s' failover chain exhausted", currentSelection.CompositeUpstream.Name)
					SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, 503, []byte(errMsg))
					c.JSON(503, gin.H{"type": "error", "error": gin.H{"type": "overloaded_error", "message": errMsg}})
				}
				return
			}
		} else {
			// Normal channel selection
			if channelAttempt >= maxChannelAttempts {
				break
			}
			selection, err = channelScheduler.SelectChannel(c.Request.Context(), clientID, failedChannels, false, allowedChannelsMsg, claudeReq.Model)
			if err != nil {
				if lastResolvedTargetPermissionErr != nil && !errors.Is(err, scheduler.ErrNoAllowedChannels) {
					lastError = fmt.Errorf("%w: %s", scheduler.ErrNoAllowedChannels, lastResolvedTargetPermissionErr.Error())
				} else {
					lastError = err
				}
				break
			}
			channelAttempt++
		}

		currentSelection = selection

		upstream := selection.Upstream
		if !isResolvedTargetAllowedForPool(strings.TrimSpace(upstream.ID), selection.TargetPool, allowedChannelsMsg, allowedChannelsResp) {
			failedKey := selection.ChannelIndex
			if selection.FailedChannelKey >= 0 {
				failedKey = selection.FailedChannelKey
			}
			failedChannels[failedKey] = true
			lastResolvedTargetPermissionErr = fmt.Errorf("resolved channel %s not allowed for this API key", strings.TrimSpace(upstream.Name))
			lastError = fmt.Errorf("%w: %s", scheduler.ErrNoAllowedChannels, lastResolvedTargetPermissionErr.Error())
			lastFailedUpstream = upstream
			continue
		}
		channelIndex := selection.ChannelIndex
		metricsModel := claudeReq.Model
		if selection.ResolvedModel != "" {
			metricsModel = selection.ResolvedModel
		}
		metricsChannelIndex := channelIndex
		metricsChannelName := upstream.Name
		metricsRoutedChannelName := ""
		if selection.CompositeUpstream != nil && selection.CompositeChannelIndex >= 0 {
			metricsChannelIndex = selection.CompositeChannelIndex
			metricsChannelName = selection.CompositeUpstream.Name
			metricsRoutedChannelName = upstream.Name
		}
		logChannelIndex := metricsChannelIndex
		logChannelName := strings.TrimSpace(metricsChannelName)

		if shouldLogInfo {
			attemptNum := channelAttempt
			if attemptNum <= 0 {
				attemptNum = 1
			}
			if selection.CompositeUpstream != nil {
				// Routed through a composite channel
				log.Printf("🎯 [Multi-Channel] [Composite: %d] %s → [Channel: %d] %s (reason: %s, model: %s, attempt %d/%d)",
					selection.CompositeChannelIndex, selection.CompositeUpstream.Name, channelIndex, upstream.Name, selection.Reason, selection.ResolvedModel, attemptNum, maxChannelAttempts)
			} else {
				log.Printf("🎯 [Multi-Channel] Selected channel: [%d] %s (reason: %s, attempt %d/%d)",
					channelIndex, upstream.Name, selection.Reason, attemptNum, maxChannelAttempts)
			}
		}

		// Check per-channel rate limit (if configured)
		if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
			rateLimitType := "messages"
			if config.NormalizeCompositeTargetPool(selection.TargetPool) == config.CompositeTargetPoolResponses {
				rateLimitType = "responses"
			}
			result := channelRateLimiter.Acquire(c.Request.Context(), upstream, rateLimitType)
			if !result.Allowed {
				if result.Queued {
					// Request was queued but timed out or client disconnected
					log.Printf("⏰ [Channel Rate Limit] Channel %d (%s): request failed after queue - %v",
						channelIndex, upstream.Name, result.Error)
				} else {
					// Rate limit exceeded, queue full or disabled
					log.Printf("🚫 [Channel Rate Limit] Channel %d (%s): %v",
						channelIndex, upstream.Name, result.Error)
				}
				channelScheduler.RecordFailureWithStatusDetail(metricsChannelIndex, false, 429, metricsModel, metricsChannelName, metricsRoutedChannelName)
				// Mark channel as failed for this request and try next channel.
				// Use scheduler-provided failure key to avoid cross-pool index collisions.
				failedKey := channelIndex
				if selection.FailedChannelKey >= 0 {
					failedKey = selection.FailedChannelKey
				}
				failedChannels[failedKey] = true
				sawNonPermissionFailure = true
				lastError = result.Error
				lastFailedUpstream = upstream
				continue
			}
			if result.Queued {
				log.Printf("✅ [Channel Rate Limit] Channel %d (%s): request released after %v queue wait",
					channelIndex, upstream.Name, result.WaitDuration)
			}
		}

		// Try all keys for this channel
		success, failoverErr, updatedLogID := tryChannelWithAllKeys(
			c,
			envCfg,
			cfgManager,
			upstream,
			bodyBytes,
			claudeReq,
			&startTime,
			reqLogManager,
			requestLogID,
			usageManager,
			failoverTracker,
			logChannelIndex,
			logChannelName,
			clientID,
			sessionID,
			apiKeyID,
		)
		requestLogID = updatedLogID // Update requestLogID in case it was changed during retry_wait
		sawNonPermissionFailure = true

		if c.Request.Context().Err() != nil {
			return
		}

		if success {
			// ActionNone/no-failover branches return handled=true with an error payload.
			// Classify by actual HTTP status as a guard against false-green metrics.
			handledStatus := c.Writer.Status()
			if handledStatus <= 0 {
				handledStatus = http.StatusOK
			}
			if failoverErr != nil || handledStatus >= http.StatusBadRequest {
				failureStatus := handledStatus
				if failoverErr != nil && failoverErr.Status > 0 {
					failureStatus = failoverErr.Status
				}
				if failureStatus < http.StatusBadRequest {
					failureStatus = http.StatusInternalServerError
				}
				channelScheduler.RecordFailureWithStatusDetail(metricsChannelIndex, false, failureStatus, metricsModel, metricsChannelName, metricsRoutedChannelName)
			} else {
				channelScheduler.RecordSuccessWithStatusDetail(metricsChannelIndex, false, handledStatus, metricsModel, metricsChannelName, metricsRoutedChannelName)
				// For composite channels, set affinity to the composite channel (not the resolved target)
				// This ensures subsequent requests still go through composite routing logic
				affinityIndex := channelIndex
				if selection.CompositeChannelIndex >= 0 {
					affinityIndex = selection.CompositeChannelIndex
				}
				channelScheduler.SetTraceAffinity(clientID, affinityIndex)
			}
			return
		}

		// Channel failed: record failure metrics
		failureStatus := 0
		if failoverErr != nil {
			failureStatus = failoverErr.Status
		}
		channelScheduler.RecordFailureWithStatusDetail(metricsChannelIndex, false, failureStatus, metricsModel, metricsChannelName, metricsRoutedChannelName)
		failedKey := channelIndex
		if selection.FailedChannelKey >= 0 {
			failedKey = selection.FailedChannelKey
		}
		failedChannels[failedKey] = true

		// For composite channels, check if there are more in the failover chain
		// If so, continue the loop (the composite failover will be handled at the top)
		if selection.CompositeUpstream != nil && len(selection.FailoverChain) > 0 {
			// Log the failover attempt for composite chain
			if reqLogManager != nil && requestLogID != "" {
				completeTime := time.Now()
				httpStatus := 0
				upstreamErr := ""
				failoverInfoStr := ""
				if failoverErr != nil {
					httpStatus = failoverErr.Status
					upstreamErr = string(failoverErr.Body)
					failoverInfoStr = failoverErr.FailoverInfo
				}

				errorMsg := fmt.Sprintf("composite failover to next in chain (%d remaining)", len(selection.FailoverChain))
				if httpStatus > 0 {
					errorMsg = fmt.Sprintf("%d: %s", httpStatus, errorMsg)
				}

				failoverRecord := &requestlog.RequestLog{
					Status:        requestlog.StatusFailover,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(startTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					Model:         claudeReq.Model,
					ChannelID:     logChannelIndex,
					ChannelName:   logChannelName,
					HTTPStatus:    httpStatus,
					Error:         errorMsg,
					UpstreamError: upstreamErr,
					FailoverInfo:  failoverInfoStr,
				}
				if err := reqLogManager.Update(requestLogID, failoverRecord); err != nil {
					log.Printf("⚠️ Failed to update failover log: %v", err)
				}

				// Create new pending log for next channel attempt
				newPendingLog := &requestlog.RequestLog{
					Status:      requestlog.StatusPending,
					InitialTime: time.Now(),
					Model:       claudeReq.Model,
					Stream:      claudeReq.Stream,
					Endpoint:    "/v1/messages",
					ChannelID:   logChannelIndex,
					ChannelName: logChannelName,
					ClientID:    clientID,
					SessionID:   sessionID,
					APIKeyID:    apiKeyID,
				}
				if err := reqLogManager.Add(newPendingLog); err != nil {
					log.Printf("⚠️ Failed to create failover pending log: %v", err)
				} else {
					requestLogID = newPendingLog.ID
					startTime = newPendingLog.InitialTime
				}
			}

			log.Printf("⚠️ [Composite] Channel [%d] %s failed, trying next in failover chain (%d remaining)",
				channelIndex, upstream.Name, len(selection.FailoverChain))

			if failoverErr != nil {
				lastFailoverError = failoverErr
			}
			lastError = fmt.Errorf("channel [%d] %s failed", channelIndex, upstream.Name)
			lastFailedUpstream = upstream
			continue // Continue loop - composite failover will be handled at top
		}

		// For composite channels with no more failover chain, the error handling
		// will be triggered at the top of the next iteration

		// Check if there are more channels to try (non-composite case)
		hasMoreChannels := channelAttempt < maxChannelAttempts

		if hasMoreChannels {
			// Failover case: log this failed attempt and create new pending log for next attempt
			if reqLogManager != nil && requestLogID != "" {
				completeTime := time.Now()
				httpStatus := 0
				upstreamErr := ""
				failoverInfo := ""
				if failoverErr != nil {
					httpStatus = failoverErr.Status
					upstreamErr = string(failoverErr.Body)
					failoverInfo = failoverErr.FailoverInfo
				}

				// Update current log as failover (keeping original error info)
				// Build error message with HTTP status for better visibility
				errorMsg := fmt.Sprintf("failover to next channel (%d/%d)", channelAttempt, maxChannelAttempts)
				if httpStatus > 0 {
					errorMsg = fmt.Sprintf("%d: %s", httpStatus, errorMsg)
				}

				failoverRecord := &requestlog.RequestLog{
					Status:        requestlog.StatusFailover,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(startTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					Model:         claudeReq.Model,
					ChannelID:     logChannelIndex,
					ChannelName:   logChannelName,
					HTTPStatus:    httpStatus,
					Error:         errorMsg,
					UpstreamError: upstreamErr,
					FailoverInfo:  failoverInfo,
				}
				if err := reqLogManager.Update(requestLogID, failoverRecord); err != nil {
					log.Printf("⚠️ Failed to update failover log: %v", err)
				}

				// Create new pending log for next channel attempt
				newPendingLog := &requestlog.RequestLog{
					Status:      requestlog.StatusPending,
					InitialTime: time.Now(),
					Model:       claudeReq.Model,
					Stream:      claudeReq.Stream,
					Endpoint:    "/v1/messages",
					ChannelID:   logChannelIndex,
					ChannelName: logChannelName,
					ClientID:    clientID,
					SessionID:   sessionID,
					APIKeyID:    apiKeyID,
				}
				if err := reqLogManager.Add(newPendingLog); err != nil {
					log.Printf("⚠️ Failed to create failover pending log: %v", err)
				} else {
					requestLogID = newPendingLog.ID
					startTime = newPendingLog.InitialTime
				}
			}

			log.Printf("⚠️ [Multi-Channel] Channel [%d] %s all keys failed, trying next channel", channelIndex, upstream.Name)
		}

		if failoverErr != nil {
			lastFailoverError = failoverErr
		}
		lastError = fmt.Errorf("channel [%d] %s failed", channelIndex, upstream.Name)
		lastFailedUpstream = upstream
	}

	// All channels failed
	log.Printf("💥 [Multi-Channel] All channels failed")

	// Update request log with final error status (this is the last attempt, no more failovers)
	if reqLogManager != nil && requestLogID != "" {
		httpStatus := 503
		errMsg := "all channels unavailable"
		upstreamErr := ""
		failoverInfo := ""
		if lastFailoverError != nil && lastFailoverError.Status != 0 {
			httpStatus = lastFailoverError.Status
			upstreamErr = string(lastFailoverError.Body)
			failoverInfo = lastFailoverError.FailoverInfo
		}
		if lastError != nil {
			errMsg = lastError.Error()
		}
		if lastResolvedTargetPermissionErr != nil && lastFailoverError == nil && !sawNonPermissionFailure {
			httpStatus = 403
			errMsg = lastResolvedTargetPermissionErr.Error()
		} else if lastError != nil && errors.Is(lastError, scheduler.ErrNoAllowedChannels) {
			httpStatus = 403
		}
		record := &requestlog.RequestLog{
			Status:        requestlog.StatusError,
			CompleteTime:  time.Now(),
			DurationMs:    time.Since(startTime).Milliseconds(),
			Model:         claudeReq.Model,
			HTTPStatus:    httpStatus,
			Error:         errMsg,
			UpstreamError: upstreamErr,
			FailoverInfo:  failoverInfo,
		}
		logChannelIndex, logChannelName := resolveRequestLogChannelContext(currentSelection, lastFailedUpstream)
		if logChannelIndex >= 0 || logChannelName != "" {
			record.ChannelID = logChannelIndex
			record.ChannelName = logChannelName
		}
		if lastFailedUpstream != nil {
			record.Type = lastFailedUpstream.ServiceType
			record.ProviderName = lastFailedUpstream.Name
		}
		_ = reqLogManager.Update(requestLogID, record)
	}

	// Return error response to client
	if lastFailoverError != nil {
		status := lastFailoverError.Status
		if status == 0 {
			status = 503
		}
		SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, lastFailoverError.Body)
		var errBody map[string]interface{}
		if err := json.Unmarshal(lastFailoverError.Body, &errBody); err == nil {
			c.JSON(status, errBody)
		} else {
			c.JSON(status, gin.H{"error": string(lastFailoverError.Body)})
		}
	} else {
		// Check if this is a permission error (no allowed channels)
		if lastResolvedTargetPermissionErr != nil && lastFailoverError == nil && !sawNonPermissionFailure {
			errMsg := lastResolvedTargetPermissionErr.Error()
			errJSON := fmt.Sprintf(`{"error":"channel not allowed","details":"%s","code":"CHANNEL_NOT_ALLOWED"}`, errMsg)
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, 403, []byte(errJSON))
			c.JSON(403, gin.H{
				"error":   "channel not allowed",
				"details": errMsg,
				"code":    "CHANNEL_NOT_ALLOWED",
			})
		} else if lastError != nil && errors.Is(lastError, scheduler.ErrNoAllowedChannels) {
			errMsg := lastError.Error()
			errJSON := fmt.Sprintf(`{"error":"channel not allowed","details":"%s","code":"CHANNEL_NOT_ALLOWED"}`, errMsg)
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, 403, []byte(errJSON))
			c.JSON(403, gin.H{
				"error":   "channel not allowed",
				"details": errMsg,
				"code":    "CHANNEL_NOT_ALLOWED",
			})
		} else {
			errMsg := "all channels unavailable"
			if lastError != nil {
				errMsg = lastError.Error()
			}
			errJSON := fmt.Sprintf(`{"error":"all channels unavailable","details":"%s"}`, errMsg)
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, 503, []byte(errJSON))
			c.JSON(503, gin.H{
				"error":   "all channels unavailable",
				"details": errMsg,
			})
		}
	}
}

// tryChannelWithAllKeys tries all API keys for a channel.
// Returns (success bool, lastFailoverError *struct{Status int; Body []byte; FailoverInfo string}, updatedRequestLogID string)
func tryChannelWithAllKeys(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	claudeReq types.ClaudeRequest,
	startTime *time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	logChannelIndex int,
	logChannelName string,
	clientID string,
	sessionID string,
	apiKeyID *int64,
) (bool, *struct {
	Status       int
	Body         []byte
	FailoverInfo string
}, string) {
	if upstream.ServiceType == "openai-oauth" {
		success, failoverErr := tryMessagesChannelWithOAuth(c, envCfg, cfgManager, upstream, bodyBytes, claudeReq, *startTime, reqLogManager, requestLogID, usageManager, logChannelIndex, logChannelName)
		return success, failoverErr, requestLogID
	}

	if len(upstream.APIKeys) == 0 {
		return false, nil, requestLogID
	}

	provider := providers.GetProvider(upstream.ServiceType)
	if provider == nil {
		return false, nil, requestLogID
	}
	if strings.TrimSpace(logChannelName) == "" {
		logChannelName = strings.TrimSpace(upstream.Name)
	}
	if logChannelIndex < 0 {
		logChannelIndex = upstream.Index
	}

	maxRetries := len(upstream.APIKeys)
	failedKeys := make(map[string]bool)
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}
	deprioritizeCandidates := make(map[string]bool)
	var pinnedKey string      // For retry-same-key scenarios
	var retryWaitPending bool // Allows loop to continue for one retry after wait
	currentStartTime := *startTime
	defer func() {
		*startTime = currentStartTime
	}()
	currentRequestLogID := requestLogID

	for attempt := 0; attempt < maxRetries || retryWaitPending; {
		retryWaitPending = false // Clear at start of each iteration

		// 恢复请求体
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var apiKey string
		var err error

		// If we have a pinned key from a previous retry-same-key decision, use it
		if pinnedKey != "" {
			apiKey = pinnedKey
			pinnedKey = "" // Clear after use
			// Don't increment attempt for retry-same-key
		} else {
			apiKey, err = cfgManager.GetNextAPIKey(upstream, failedKeys)
			if err != nil {
				break
			}
			attempt++ // Only increment when trying a new key
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🔑 使用API密钥: %s (尝试 %d/%d)", maskAPIKey(apiKey), attempt+1, maxRetries)
		}

		// 转换请求
		providerReq, _, err := provider.ConvertToProviderRequest(c, upstream, apiKey)
		if err != nil {
			failedKeys[apiKey] = true
			continue
		}
		applyMessagesUserAgentPolicy(c, cfgManager, upstream, providerReq)

		// 发送请求
		resp, err := sendRequest(providerReq, upstream, envCfg, claudeReq.Stream)
		if err != nil {
			if c.Request.Context().Err() != nil {
				if reqLogManager != nil && currentRequestLogID != "" {
					completeTime := time.Now()
					record := &requestlog.RequestLog{
						Status:       requestlog.StatusError,
						CompleteTime: completeTime,
						DurationMs:   completeTime.Sub(currentStartTime).Milliseconds(),
						Type:         upstream.ServiceType,
						ProviderName: upstream.Name,
						HTTPStatus:   499,
						ChannelID:    logChannelIndex,
						ChannelName:  logChannelName,
						Error:        "client disconnected during upstream request",
					}
					_ = reqLogManager.Update(currentRequestLogID, record)
				}
				return false, nil, currentRequestLogID
			}
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			log.Printf("⚠️ API密钥失败: %v", err)
			continue
		}

		// Content filter: detect errors hidden in HTTP 200 response bodies.
		// On match, convert to a real error response and let the existing failover logic handle it.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && upstream.ContentFilter != nil && upstream.ContentFilter.Enabled {
			var filterResult contentFilterResult
			var bufferedBody []byte

			if claudeReq.Stream {
				var filterErr error
				filterResult, bufferedBody, filterErr = checkContentFilterOnStream(resp, upstream.ContentFilter)
				if filterErr != nil {
					log.Printf("⚠️ Content filter stream read error: %v", filterErr)
					failedKeys[apiKey] = true
					continue
				}
			} else {
				bodyData, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					log.Printf("⚠️ Content filter body read error: %v", readErr)
					failedKeys[apiKey] = true
					continue
				}
				bufferedBody = bodyData
				filterResult = checkContentFilterOnBody(bodyData, upstream.ContentFilter)
			}

			if filterResult.Matched {
				syntheticStatus := filterResult.MatchedStatusCode
				if syntheticStatus == 0 {
					syntheticStatus = 429
				}
				resp.StatusCode = syntheticStatus
				log.Printf("🚫 [Content Filter] Channel %d (%s): matched keyword %q, converting to HTTP %d for failover",
					upstream.Index, upstream.Name, filterResult.MatchedKeyword, syntheticStatus)

				errorBody, _ := json.Marshal(map[string]interface{}{
					"error": map[string]interface{}{
						"message": fmt.Sprintf("content_filter: %s", filterResult.AssembledText),
						"type":    "content_filter_match",
						"keyword": filterResult.MatchedKeyword,
					},
				})
				bufferedBody = errorBody
			}

			resp.Body = io.NopCloser(bytes.NewReader(bufferedBody))
		}

		// 检查响应状态
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

			// Handle 429 errors with smart subtype detection
			if resp.StatusCode == 429 && failoverTracker != nil {
				// Unified failover logic: always use admin failover settings
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key (tracker handles attempt counting)
					log.Printf("⏳ 429 %s: 等待 %v 后重试同一密钥 (max: %d)", decision.Reason, decision.Wait, decision.MaxAttempts)

					failoverInfo := requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds()))

					// AUDIT: Log this retry_wait attempt before waiting
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						retryWaitRecord := &requestlog.RequestLog{
							Status:        requestlog.StatusRetryWait,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("429 %s - retrying after %v", decision.Reason, decision.Wait),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  failoverInfo,
						}
						if err := reqLogManager.Update(currentRequestLogID, retryWaitRecord); err != nil {
							log.Printf("⚠️ Failed to update retry_wait log: %v", err)
						}

						// Save debug log for this 429 error response
						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       claudeReq.Model,
							Stream:      claudeReq.Stream,
							Endpoint:    "/v1/messages",
							ChannelID:   logChannelIndex,
							ChannelName: logChannelName,
							ClientID:    clientID,
							SessionID:   sessionID,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							currentStartTime = newPendingLog.InitialTime
						}
					}

					// Capture for last-resort error reporting
					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: failoverInfo,
					}

					select {
					case <-time.After(decision.Wait):
						currentStartTime = time.Now() // exclude on-hold wait from duration metrics
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						if reqLogManager != nil && currentRequestLogID != "" {
							completeTime := time.Now()
							record := &requestlog.RequestLog{
								Status:       requestlog.StatusError,
								CompleteTime: completeTime,
								DurationMs:   completeTime.Sub(currentStartTime).Milliseconds(),
								Type:         upstream.ServiceType,
								ProviderName: upstream.Name,
								HTTPStatus:   499,
								ChannelID:    logChannelIndex,
								ChannelName:  logChannelName,
								Error:        "client disconnected during retry wait",
							}
							_ = reqLogManager.Update(currentRequestLogID, record)
						}
						return false, nil, currentRequestLogID
					}

				case config.ActionFailoverKey:
					// Immediate failover to next key
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ 429 %s: 立即切换到下一个密钥", decision.Reason)

					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionFailover, "next key"),
					}

					if decision.DeprioritizeKey {
						deprioritizeCandidates[apiKey] = true
					}
					continue

				case config.ActionSuspendChan:
					// Suspend channel until quota resets
					if reqLogManager != nil && decision.SuspendChannel {
						// Calculate suspension duration: use quota reset time if available, default 5 min
						suspendedUntil := time.Now().Add(5 * time.Minute)
						if upstream.QuotaResetAt != nil && upstream.QuotaResetAt.After(time.Now()) {
							suspendedUntil = *upstream.QuotaResetAt
							log.Printf("⏸️ [Messages] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Messages] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						channelType := "messages" // Multi-channel proxy is always Messages API
						if err := reqLogManager.SuspendChannel(upstream.Index, channelType, suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (%s): %v", upstream.Index, channelType, err)
						}
					}
					log.Printf("⏸️ 429 %s: 渠道暂停，切换到下一个渠道", decision.Reason)

					// Return false to trigger channel failover (outer loop will try next channel)
					return false, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "next channel"),
					}, currentRequestLogID

				default:
					// ActionNone - return error to client
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("429 %s (returning error)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, decision.Reason),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return true, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, decision.Reason),
					}, currentRequestLogID
				}
			}

			// Non-429 errors: use unified failover logic with full decision switch
			if failoverTracker != nil {
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key
					log.Printf("⏳ %d %s: 等待 %v 后重试同一密钥 (max: %d)", resp.StatusCode, decision.Reason, decision.Wait, decision.MaxAttempts)

					failoverInfo := requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds()))

					// AUDIT: Log this retry_wait attempt before waiting
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						retryWaitRecord := &requestlog.RequestLog{
							Status:        requestlog.StatusRetryWait,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("%d %s - retrying after %v", resp.StatusCode, decision.Reason, decision.Wait),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  failoverInfo,
						}
						if err := reqLogManager.Update(currentRequestLogID, retryWaitRecord); err != nil {
							log.Printf("⚠️ Failed to update retry_wait log: %v", err)
						}

						// Save debug log for this error response
						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       claudeReq.Model,
							Stream:      claudeReq.Stream,
							Endpoint:    "/v1/messages",
							ChannelID:   logChannelIndex,
							ChannelName: logChannelName,
							ClientID:    clientID,
							SessionID:   sessionID,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							currentStartTime = newPendingLog.InitialTime
						}
					}

					// Capture for last-resort error reporting
					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: failoverInfo,
					}

					select {
					case <-time.After(decision.Wait):
						currentStartTime = time.Now() // exclude on-hold wait from duration metrics
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						if reqLogManager != nil && currentRequestLogID != "" {
							completeTime := time.Now()
							record := &requestlog.RequestLog{
								Status:       requestlog.StatusError,
								CompleteTime: completeTime,
								DurationMs:   completeTime.Sub(currentStartTime).Milliseconds(),
								Type:         upstream.ServiceType,
								ProviderName: upstream.Name,
								HTTPStatus:   499,
								ChannelID:    logChannelIndex,
								ChannelName:  logChannelName,
								Error:        "client disconnected during retry wait",
							}
							_ = reqLogManager.Update(currentRequestLogID, record)
						}
						return false, nil, currentRequestLogID
					}

				case config.ActionFailoverKey:
					// Failover to next key
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ %d %s: 切换到下一个密钥", resp.StatusCode, decision.Reason)

					// Determine the reason for failover
					failoverReason := requestlog.FailoverActionFailover
					if resp.StatusCode == 401 || resp.StatusCode == 403 {
						failoverReason = requestlog.FailoverActionAuthFailed
					}
					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, failoverReason, "next key"),
					}

					// Only deprioritize for 429 errors (quota-related), not transient 5xx
					if decision.DeprioritizeKey && resp.StatusCode == 429 {
						deprioritizeCandidates[apiKey] = true
					}
					continue

				case config.ActionSuspendChan:
					// Suspend channel
					if reqLogManager != nil && decision.SuspendChannel {
						suspendedUntil := time.Now().Add(5 * time.Minute)
						if upstream.QuotaResetAt != nil && upstream.QuotaResetAt.After(time.Now()) {
							suspendedUntil = *upstream.QuotaResetAt
							log.Printf("⏸️ [Messages] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Messages] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						channelType := "messages"
						if err := reqLogManager.SuspendChannel(upstream.Index, channelType, suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (%s): %v", upstream.Index, channelType, err)
						}
					}
					log.Printf("⏸️ %d %s: 渠道暂停，切换到下一个渠道", resp.StatusCode, decision.Reason)

					return false, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "next channel"),
					}, currentRequestLogID

				default:
					// ActionNone - return error to client
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, ""),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return true, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, ""),
					}, currentRequestLogID
				}
			} else {
				// No failover tracker - return error to client
				if reqLogManager != nil && currentRequestLogID != "" {
					completeTime := time.Now()
					record := &requestlog.RequestLog{
						Status:        requestlog.StatusError,
						CompleteTime:  completeTime,
						DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
						Type:          upstream.ServiceType,
						ProviderName:  upstream.Name,
						HTTPStatus:    resp.StatusCode,
						ChannelID:     logChannelIndex,
						ChannelName:   logChannelName,
						Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
						UpstreamError: string(respBodyBytes),
						FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
					}
					_ = reqLogManager.Update(currentRequestLogID, record)
				}
				SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
				c.Data(resp.StatusCode, "application/json", respBodyBytes)
				return true, &struct {
					Status       int
					Body         []byte
					FailoverInfo string
				}{
					Status:       resp.StatusCode,
					Body:         respBodyBytes,
					FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
				}, currentRequestLogID
			}
		}

		// 处理成功响应 - reset error counters on success
		if failoverTracker != nil {
			failoverTracker.ResetOnSuccess(upstream.Index, apiKey)
		}
		if len(deprioritizeCandidates) > 0 {
			for key := range deprioritizeCandidates {
				_ = cfgManager.DeprioritizeAPIKey(key)
			}
		}

		if claudeReq.Stream {
			handleStreamResponse(c, resp, provider, envCfg, cfgManager, currentStartTime, upstream, reqLogManager, currentRequestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
		} else {
			handleNormalResponse(c, resp, provider, envCfg, cfgManager, currentStartTime, upstream, reqLogManager, currentRequestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
		}
		return true, nil, currentRequestLogID
	}

	return false, lastFailoverError, currentRequestLogID
}

func convertClaudeBodyToResponsesRequest(c *gin.Context, upstream *config.UpstreamConfig, bodyBytes []byte) ([]byte, types.ResponsesRequest, error) {
	var responsesReq types.ResponsesRequest

	provider := &providers.ResponsesUpstreamProvider{}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// API key is irrelevant here: we only need the converted request payload.
	providerReq, _, err := provider.ConvertToProviderRequest(c, upstream, "oauth-placeholder")
	if err != nil {
		return nil, responsesReq, err
	}
	if providerReq.Body == nil {
		return nil, responsesReq, fmt.Errorf("converted responses request body is empty")
	}
	defer providerReq.Body.Close()

	responsesBody, err := io.ReadAll(providerReq.Body)
	if err != nil {
		return nil, responsesReq, err
	}
	if len(responsesBody) > 0 {
		if err := json.Unmarshal(responsesBody, &responsesReq); err != nil {
			return nil, responsesReq, err
		}
	}

	return responsesBody, responsesReq, nil
}

func tryMessagesChannelWithOAuth(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	claudeReq types.ClaudeRequest,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	logChannelIndex int,
	logChannelName string,
) (bool, *struct {
	Status       int
	Body         []byte
	FailoverInfo string
}) {
	if strings.TrimSpace(logChannelName) == "" {
		logChannelName = strings.TrimSpace(upstream.Name)
	}
	if logChannelIndex < 0 {
		logChannelIndex = upstream.Index
	}

	makeError := func(status int, msg string) *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	} {
		return &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: status,
			Body:   []byte(fmt.Sprintf(`{"error":"%s"}`, msg)),
		}
	}

	if upstream.OAuthTokens == nil {
		return false, makeError(503, "OAuth tokens not configured for this channel")
	}

	accessToken, accountID, updatedTokens, err := getValidOAuthTokenForMessagesBridge(upstream.OAuthTokens)
	if err != nil {
		return false, makeError(401, fmt.Sprintf("Failed to get valid OAuth token: %s", err.Error()))
	}

	if updatedTokens != nil {
		if err := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, updatedTokens); err != nil {
			log.Printf("⚠️ [OAuth] 保存刷新后的 token 失败: %v", err)
		}
	}

	responsesBody, responsesReq, err := convertClaudeBodyToResponsesRequest(c, upstream, bodyBytes)
	if err != nil {
		return false, makeError(500, fmt.Sprintf("Failed to convert Claude request to Responses format: %s", err.Error()))
	}
	// Keep stream intent aligned with the original Claude request.
	responsesReq.Stream = claudeReq.Stream

	providerReq, err := buildCodexOAuthRequest(c, cfgManager, upstream, responsesBody, responsesReq, accessToken, accountID, false)
	if err != nil {
		return false, makeError(500, fmt.Sprintf("Failed to build OAuth request: %s", err.Error()))
	}

	resp, err := sendResponsesRequest(providerReq, upstream, envCfg, responsesReq.Stream)
	if err != nil {
		return false, makeError(502, fmt.Sprintf("Request failed: %s", err.Error()))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

		if resp.StatusCode == 401 {
			log.Printf("🔄 [OAuth] 401 错误，尝试强制刷新 token...")
			newTokens, refreshErr := refreshOAuthTokensForMessagesBridge(upstream.OAuthTokens.RefreshToken, 2)
			if refreshErr == nil {
				if saveErr := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, newTokens); saveErr != nil {
					log.Printf("⚠️ [OAuth] 保存刷新后的 token 失败: %v", saveErr)
				}
			}
		}

		return false, &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: resp.StatusCode,
			Body:   respBodyBytes,
		}
	}

	// Keep quota/status bookkeeping behavior consistent with existing responses OAuth path.
	quota.GetManager().UpdateFromHeaders(upstream.Index, upstream.Name, resp.Header)

	provider := &providers.ResponsesUpstreamProvider{}
	if claudeReq.Stream {
		handleStreamResponse(c, resp, provider, envCfg, cfgManager, startTime, upstream, reqLogManager, requestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
	} else {
		handleNormalResponse(c, resp, provider, envCfg, cfgManager, startTime, upstream, reqLogManager, requestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
	}

	return true, nil
}

// handleSingleChannelProxy 处理单渠道代理请求（现有逻辑）
func handleSingleChannelProxy(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	claudeReq types.ClaudeRequest,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannelsMsg []string,
	allowedChannelsResp []string,
	failoverTracker *config.FailoverTracker,
	clientID string,
	sessionID string,
	apiKeyID *int64,
	channelRateLimiter *middleware.ChannelRateLimiter,
) {
	finalizePendingWithError := func(status int, errMsg string, channel *config.UpstreamConfig, channelIndex *int, channelName string) {
		if reqLogManager == nil || requestLogID == "" {
			return
		}
		completeTime := time.Now()
		record := &requestlog.RequestLog{
			Status:       requestlog.StatusError,
			CompleteTime: completeTime,
			DurationMs:   completeTime.Sub(startTime).Milliseconds(),
			Model:        claudeReq.Model,
			Endpoint:     "/v1/messages",
			HTTPStatus:   status,
			Error:        errMsg,
		}
		if channel != nil {
			record.Type = channel.ServiceType
			record.ProviderName = channel.Name
		}
		if channelIndex != nil {
			record.ChannelID = *channelIndex
		}
		if trimmed := strings.TrimSpace(channelName); trimmed != "" {
			record.ChannelName = trimmed
		} else if channel != nil {
			record.ChannelName = channel.Name
		}
		_ = reqLogManager.Update(requestLogID, record)
	}

	// 获取当前上游配置
	upstream, err := cfgManager.GetCurrentUpstream()
	if err != nil {
		finalizePendingWithError(503, "No channels configured. Please add a channel in the admin UI.", nil, nil, "")
		c.JSON(503, gin.H{
			"error": "No channels configured. Please add a channel in the admin UI.",
			"code":  "NO_UPSTREAM",
		})
		return
	}

	// Check if this channel is allowed by API key permissions
	if !isChannelAllowed(strings.TrimSpace(upstream.ID), allowedChannelsMsg) {
		channelIndex := upstream.Index
		finalizePendingWithError(403, fmt.Sprintf("Channel %s not allowed for this API key", upstream.Name), upstream, &channelIndex, upstream.Name)
		c.JSON(403, gin.H{
			"error": fmt.Sprintf("Channel %s not allowed for this API key", upstream.Name),
			"code":  "CHANNEL_NOT_ALLOWED",
		})
		return
	}

	// Composite channels don't have API keys - they route to other channels.
	// openai-oauth channels use OAuth tokens instead of API keys.
	if len(upstream.APIKeys) == 0 && !config.IsCompositeChannel(upstream) && upstream.ServiceType != "openai-oauth" {
		channelIndex := upstream.Index
		finalizePendingWithError(503, fmt.Sprintf("Current channel \"%s\" has no API keys configured", upstream.Name), upstream, &channelIndex, upstream.Name)
		c.JSON(503, gin.H{
			"error": fmt.Sprintf("Current channel \"%s\" has no API keys configured", upstream.Name),
			"code":  "NO_API_KEYS",
		})
		return
	}

	// Resolve composite channel to target channel
	metricsModel := claudeReq.Model
	metricsChannelIndex := upstream.Index
	metricsChannelName := upstream.Name
	metricsRoutedChannelName := ""
	resolvedTargetPool := config.CompositeTargetPoolMessages
	if config.IsCompositeChannel(upstream) {
		compositeName := upstream.Name
		compositeIndex := upstream.Index
		cfg := cfgManager.GetConfig()
		resolved, found := config.ResolveCompositeMappingWithPools(upstream, claudeReq.Model, cfg.Upstream, cfg.ResponsesUpstream)
		if !found {
			channelIndex := upstream.Index
			finalizePendingWithError(400, fmt.Sprintf("Composite channel '%s' has no mapping for model '%s'", upstream.Name, claudeReq.Model), upstream, &channelIndex, upstream.Name)
			c.JSON(400, gin.H{
				"error": fmt.Sprintf("Composite channel '%s' has no mapping for model '%s'", upstream.Name, claudeReq.Model),
				"code":  "NO_COMPOSITE_MAPPING",
			})
			return
		}
		targetIndex := resolved.TargetIndex
		resolvedTargetPool = config.NormalizeCompositeTargetPool(resolved.TargetPool)

		var targetUpstream config.UpstreamConfig
		switch resolvedTargetPool {
		case config.CompositeTargetPoolResponses:
			if targetIndex < 0 || targetIndex >= len(cfg.ResponsesUpstream) {
				channelIndex := upstream.Index
				finalizePendingWithError(503, fmt.Sprintf("Composite channel '%s' target channel ID '%s' not found", upstream.Name, resolved.TargetChannelID), upstream, &channelIndex, upstream.Name)
				c.JSON(503, gin.H{
					"error": fmt.Sprintf("Composite channel '%s' target channel ID '%s' not found", upstream.Name, resolved.TargetChannelID),
					"code":  "COMPOSITE_TARGET_NOT_FOUND",
				})
				return
			}
			targetUpstream = cfg.ResponsesUpstream[targetIndex]
		default:
			if targetIndex < 0 || targetIndex >= len(cfg.Upstream) {
				channelIndex := upstream.Index
				finalizePendingWithError(503, fmt.Sprintf("Composite channel '%s' target channel ID '%s' not found", upstream.Name, resolved.TargetChannelID), upstream, &channelIndex, upstream.Name)
				c.JSON(503, gin.H{
					"error": fmt.Sprintf("Composite channel '%s' target channel ID '%s' not found", upstream.Name, resolved.TargetChannelID),
					"code":  "COMPOSITE_TARGET_NOT_FOUND",
				})
				return
			}
			targetUpstream = cfg.Upstream[targetIndex]
		}

		if !isResolvedTargetAllowedForPool(strings.TrimSpace(targetUpstream.ID), resolvedTargetPool, allowedChannelsMsg, allowedChannelsResp) {
			channelIndex := upstream.Index
			finalizePendingWithError(403, fmt.Sprintf("Composite target channel %s not allowed for this API key", targetUpstream.Name), upstream, &channelIndex, upstream.Name)
			c.JSON(403, gin.H{
				"error": fmt.Sprintf("Composite target channel %s not allowed for this API key", targetUpstream.Name),
				"code":  "CHANNEL_NOT_ALLOWED",
			})
			return
		}
		log.Printf("🔀 [Single-Channel Composite] '%s' → target [%d] '%s' (pool: %s) for model '%s' (resolved: '%s')",
			upstream.Name, targetIndex, targetUpstream.Name, resolvedTargetPool, claudeReq.Model, resolved.TargetModel)
		metricsChannelIndex = compositeIndex
		metricsChannelName = compositeName
		metricsRoutedChannelName = targetUpstream.Name
		upstream = &targetUpstream
		if resolved.TargetModel != "" {
			metricsModel = resolved.TargetModel
		}
	}
	logChannelIndex := metricsChannelIndex
	logChannelName := strings.TrimSpace(metricsChannelName)

	recordSingleSuccess := func(statusCode int) {
		channelScheduler.RecordSuccessWithStatusDetail(metricsChannelIndex, false, statusCode, metricsModel, metricsChannelName, metricsRoutedChannelName)
	}
	recordSingleFailure := func(statusCode int) {
		channelScheduler.RecordFailureWithStatusDetail(metricsChannelIndex, false, statusCode, metricsModel, metricsChannelName, metricsRoutedChannelName)
	}

	// Check per-channel rate limit (if configured)
	if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
		rateLimitType := "messages"
		if resolvedTargetPool == config.CompositeTargetPoolResponses {
			rateLimitType = "responses"
		}
		result := channelRateLimiter.Acquire(c.Request.Context(), upstream, rateLimitType)
		if !result.Allowed {
			log.Printf("🚫 [Channel Rate Limit] Channel %d (%s): %v", upstream.Index, upstream.Name, result.Error)
			channelScheduler.RecordFailureWithStatusDetail(metricsChannelIndex, false, 429, metricsModel, metricsChannelName, metricsRoutedChannelName)
			if reqLogManager != nil && requestLogID != "" {
				completeTime := time.Now()
				record := &requestlog.RequestLog{
					Status:       requestlog.StatusError,
					CompleteTime: completeTime,
					DurationMs:   completeTime.Sub(startTime).Milliseconds(),
					Model:        claudeReq.Model,
					Type:         upstream.ServiceType,
					ProviderName: upstream.Name,
					HTTPStatus:   429,
					ChannelID:    logChannelIndex,
					ChannelName:  logChannelName,
					Endpoint:     "/v1/messages",
					Error:        fmt.Sprintf("channel rate limit exceeded (%d RPM)", upstream.RateLimitRpm),
				}
				_ = reqLogManager.Update(requestLogID, record)
			}
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": fmt.Sprintf("Channel rate limit exceeded (%d RPM)", upstream.RateLimitRpm),
			})
			return
		}
		if result.Queued {
			log.Printf("✅ [Channel Rate Limit] Channel %d (%s): request released after %v queue wait",
				upstream.Index, upstream.Name, result.WaitDuration)
		}
	}

	// Handle Responses OAuth channel through the messages->responses bridge.
	if upstream.ServiceType == "openai-oauth" {
		success, failoverErr := tryMessagesChannelWithOAuth(c, envCfg, cfgManager, upstream, bodyBytes, claudeReq, startTime, reqLogManager, requestLogID, usageManager, logChannelIndex, logChannelName)
		if !success && failoverErr != nil {
			status := failoverErr.Status
			if status == 0 {
				status = 500
			}
			channelIndex := logChannelIndex
			errMsg := strings.TrimSpace(string(failoverErr.Body))
			if errMsg == "" {
				errMsg = fmt.Sprintf("upstream returned status %d", status)
			}
			finalizePendingWithError(status, errMsg, upstream, &channelIndex, logChannelName)
			recordSingleFailure(status)
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, failoverErr.Body)
			var errBody map[string]interface{}
			if err := json.Unmarshal(failoverErr.Body, &errBody); err == nil {
				c.JSON(status, errBody)
			} else {
				c.JSON(status, gin.H{"error": string(failoverErr.Body)})
			}
		} else if success {
			handledStatus := c.Writer.Status()
			if handledStatus <= 0 {
				handledStatus = http.StatusOK
			}
			if handledStatus >= http.StatusBadRequest {
				recordSingleFailure(handledStatus)
			} else {
				recordSingleSuccess(handledStatus)
			}
		} else {
			channelIndex := logChannelIndex
			finalizePendingWithError(500, "oauth bridge request failed", upstream, &channelIndex, logChannelName)
			recordSingleFailure(500)
		}
		return
	}

	// 获取提供商
	provider := providers.GetProvider(upstream.ServiceType)
	if provider == nil {
		channelIndex := logChannelIndex
		finalizePendingWithError(400, "Unsupported service type", upstream, &channelIndex, logChannelName)
		c.JSON(400, gin.H{"error": "Unsupported service type"})
		return
	}

	// 实现 failover 重试逻辑
	maxRetries := len(upstream.APIKeys)
	failedKeys := make(map[string]bool)
	var lastError error
	var lastOriginalBodyBytes []byte
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}
	deprioritizeCandidates := make(map[string]bool)
	var pinnedKey string      // For retry-same-key scenarios
	var retryWaitPending bool // Allows loop to continue for one retry after wait
	currentStartTime := startTime
	currentRequestLogID := requestLogID

	for attempt := 0; attempt < maxRetries || retryWaitPending; {
		retryWaitPending = false // Clear at start of each iteration

		// 恢复请求体
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var apiKey string
		var err error

		// If we have a pinned key from a previous retry-same-key decision, use it
		if pinnedKey != "" {
			apiKey = pinnedKey
			pinnedKey = "" // Clear after use
			// Don't increment attempt for retry-same-key
		} else {
			apiKey, err = cfgManager.GetNextAPIKey(upstream, failedKeys)
			if err != nil {
				lastError = err
				break
			}
			attempt++ // Only increment when trying a new key
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🎯 使用上游: %s - %s (尝试 %d/%d)", upstream.Name, upstream.BaseURL, attempt+1, maxRetries)
			log.Printf("🔑 使用API密钥: %s", maskAPIKey(apiKey))
		}

		// 转换请求
		providerReq, originalBodyBytes, err := provider.ConvertToProviderRequest(c, upstream, apiKey)
		if err != nil {
			lastError = err
			failedKeys[apiKey] = true
			if originalBodyBytes != nil {
				lastOriginalBodyBytes = originalBodyBytes
			}
			continue
		}
		lastOriginalBodyBytes = originalBodyBytes
		applyMessagesUserAgentPolicy(c, cfgManager, upstream, providerReq)

		// 请求日志记录
		if envCfg.EnableRequestLogs {
			log.Printf("📥 收到请求: %s %s", c.Request.Method, c.Request.URL.Path)
			if envCfg.IsDevelopment() {
				logBody := lastOriginalBodyBytes
				if len(logBody) == 0 && c.Request.Body != nil {
					bodyFromContext, _ := io.ReadAll(c.Request.Body)
					c.Request.Body = io.NopCloser(bytes.NewReader(bodyFromContext))
					logBody = bodyFromContext
				}
				formattedBody := utils.FormatJSONBytesForLog(logBody, 500)
				log.Printf("📄 原始请求体:\n%s", formattedBody)

				sanitizedHeaders := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						sanitizedHeaders[key] = values[0]
					}
				}
				maskedHeaders := utils.MaskSensitiveHeaders(sanitizedHeaders)
				headersJSON, _ := json.MarshalIndent(maskedHeaders, "", "  ")
				log.Printf("📥 原始请求头:\n%s", string(headersJSON))
			}
		}

		// 发送请求
		resp, err := sendRequest(providerReq, upstream, envCfg, claudeReq.Stream)
		if err != nil {
			if c.Request.Context().Err() != nil {
				channelIndex := logChannelIndex
				finalizePendingWithError(499, "client disconnected during upstream request", upstream, &channelIndex, logChannelName)
				return
			}
			lastError = err
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			log.Printf("⚠️ API密钥失败: %v", err)
			continue
		}

		// Content filter: detect errors hidden in HTTP 200 response bodies.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && upstream.ContentFilter != nil && upstream.ContentFilter.Enabled {
			var filterResult contentFilterResult
			var bufferedBody []byte

			if claudeReq.Stream {
				var filterErr error
				filterResult, bufferedBody, filterErr = checkContentFilterOnStream(resp, upstream.ContentFilter)
				if filterErr != nil {
					log.Printf("⚠️ Content filter stream read error: %v", filterErr)
					failedKeys[apiKey] = true
					continue
				}
			} else {
				bodyData, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					log.Printf("⚠️ Content filter body read error: %v", readErr)
					failedKeys[apiKey] = true
					continue
				}
				bufferedBody = bodyData
				filterResult = checkContentFilterOnBody(bodyData, upstream.ContentFilter)
			}

			if filterResult.Matched {
				syntheticStatus := filterResult.MatchedStatusCode
				if syntheticStatus == 0 {
					syntheticStatus = 429
				}
				resp.StatusCode = syntheticStatus
				log.Printf("🚫 [Content Filter] Channel %d (%s): matched keyword %q, converting to HTTP %d for failover",
					upstream.Index, upstream.Name, filterResult.MatchedKeyword, syntheticStatus)

				errorBody, _ := json.Marshal(map[string]interface{}{
					"error": map[string]interface{}{
						"message": fmt.Sprintf("content_filter: %s", filterResult.AssembledText),
						"type":    "content_filter_match",
						"keyword": filterResult.MatchedKeyword,
					},
				})
				bufferedBody = errorBody
			}

			resp.Body = io.NopCloser(bytes.NewReader(bufferedBody))
		}

		// 检查响应状态
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

			// Handle 429 errors with smart subtype detection
			if resp.StatusCode == 429 && failoverTracker != nil {
				// Unified failover logic: always use admin failover settings
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key (tracker handles attempt counting)
					log.Printf("⏳ 429 %s: 等待 %v 后重试同一密钥 (max: %d)", decision.Reason, decision.Wait, decision.MaxAttempts)

					failoverInfo := requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds()))

					// AUDIT: Log this retry_wait attempt before waiting
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						retryWaitRecord := &requestlog.RequestLog{
							Status:        requestlog.StatusRetryWait,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("429 %s - retrying after %v", decision.Reason, decision.Wait),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  failoverInfo,
						}
						if err := reqLogManager.Update(currentRequestLogID, retryWaitRecord); err != nil {
							log.Printf("⚠️ Failed to update retry_wait log: %v", err)
						}

						// Save debug log for this 429 error response
						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       claudeReq.Model,
							Stream:      claudeReq.Stream,
							Endpoint:    "/v1/messages",
							ChannelID:   logChannelIndex,
							ChannelName: logChannelName,
							ClientID:    clientID,
							SessionID:   sessionID,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							currentStartTime = newPendingLog.InitialTime
						}
					}

					select {
					case <-time.After(decision.Wait):
						currentStartTime = time.Now() // exclude on-hold wait from duration metrics
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						channelIndex := logChannelIndex
						finalizePendingWithError(499, "client disconnected during retry wait", upstream, &channelIndex, logChannelName)
						return
					}

				case config.ActionFailoverKey:
					// Immediate failover to next key
					lastError = fmt.Errorf("429 %s", decision.Reason)
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ 429 %s: 立即切换到下一个密钥", decision.Reason)
					if envCfg.EnableResponseLogs && envCfg.IsDevelopment() {
						formattedBody := utils.FormatJSONBytesForLog(respBodyBytes, 500)
						log.Printf("📦 失败原因:\n%s", formattedBody)
					}

					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionFailover, "next key"),
					}

					if decision.DeprioritizeKey {
						deprioritizeCandidates[apiKey] = true
					}
					continue

				case config.ActionSuspendChan:
					// Suspend channel until quota resets (single-channel mode)
					// Record suspension for monitoring, but return error to client since no fallback
					if reqLogManager != nil && decision.SuspendChannel {
						suspendedUntil := time.Now().Add(5 * time.Minute)
						if upstream.QuotaResetAt != nil && upstream.QuotaResetAt.After(time.Now()) {
							suspendedUntil = *upstream.QuotaResetAt
						}
						if err := reqLogManager.SuspendChannel(upstream.Index, "messages", suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (messages): %v", upstream.Index, err)
						}
					}
					log.Printf("⏸️ 429 %s: 渠道暂停 (单渠道模式，无可用后备)", decision.Reason)

					// Update request log and return error to client
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("429 %s (channel suspended)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "no fallback"),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					recordSingleFailure(resp.StatusCode)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return

				default:
					// ActionNone - return error to client
					if envCfg.EnableResponseLogs {
						log.Printf("⚠️ 429 %s (returning error)", decision.Reason)
					}
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("429 %s (returning error)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, decision.Reason),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					recordSingleFailure(resp.StatusCode)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return
				}
			}

			// Non-429 errors: use unified failover logic with full decision switch
			if failoverTracker != nil {
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key
					log.Printf("⏳ %d %s: 等待 %v 后重试同一密钥 (max: %d)", resp.StatusCode, decision.Reason, decision.Wait, decision.MaxAttempts)

					failoverInfo := requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionRetryWait, fmt.Sprintf("%.0fs", decision.Wait.Seconds()))

					// AUDIT: Log this retry_wait attempt before waiting
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						retryWaitRecord := &requestlog.RequestLog{
							Status:        requestlog.StatusRetryWait,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("%d %s - retrying after %v", resp.StatusCode, decision.Reason, decision.Wait),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  failoverInfo,
						}
						if err := reqLogManager.Update(currentRequestLogID, retryWaitRecord); err != nil {
							log.Printf("⚠️ Failed to update retry_wait log: %v", err)
						}

						// Save debug log for this error response
						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       claudeReq.Model,
							Stream:      claudeReq.Stream,
							Endpoint:    "/v1/messages",
							ChannelID:   logChannelIndex,
							ChannelName: logChannelName,
							ClientID:    clientID,
							SessionID:   sessionID,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							currentStartTime = newPendingLog.InitialTime
						}
					}

					// Capture for last-resort error reporting
					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: failoverInfo,
					}

					select {
					case <-time.After(decision.Wait):
						currentStartTime = time.Now() // exclude on-hold wait from duration metrics
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						channelIndex := logChannelIndex
						finalizePendingWithError(499, "client disconnected during retry wait", upstream, &channelIndex, logChannelName)
						return
					}

				case config.ActionFailoverKey:
					// Failover to next key
					lastError = fmt.Errorf("upstream error: %d", resp.StatusCode)
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ %d %s: 切换到下一个密钥", resp.StatusCode, decision.Reason)

					if envCfg.EnableResponseLogs && envCfg.IsDevelopment() {
						formattedBody := utils.FormatJSONBytesForLog(respBodyBytes, 500)
						log.Printf("📦 失败原因:\n%s", formattedBody)
					} else if envCfg.EnableResponseLogs {
						log.Printf("失败原因: %s", string(respBodyBytes))
					}

					lastFailoverError = &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionFailover, "next key"),
					}

					// Only deprioritize for 429 errors (quota-related), not transient 5xx
					if decision.DeprioritizeKey && resp.StatusCode == 429 {
						deprioritizeCandidates[apiKey] = true
					}
					continue

				case config.ActionSuspendChan:
					// Suspend channel
					if reqLogManager != nil && decision.SuspendChannel {
						suspendedUntil := time.Now().Add(5 * time.Minute)
						if upstream.QuotaResetAt != nil && upstream.QuotaResetAt.After(time.Now()) {
							suspendedUntil = *upstream.QuotaResetAt
							log.Printf("⏸️ [Messages] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Messages] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						channelType := "messages"
						if err := reqLogManager.SuspendChannel(upstream.Index, channelType, suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (%s): %v", upstream.Index, channelType, err)
						}
					}
					log.Printf("⏸️ %d %s: 渠道暂停", resp.StatusCode, decision.Reason)

					// For single-channel, return error since there's no other channel to try
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("%d %s - channel suspended", resp.StatusCode, decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, ""),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					recordSingleFailure(resp.StatusCode)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return

				default:
					// ActionNone - return error to client
					if envCfg.EnableResponseLogs {
						log.Printf("⚠️ 上游返回错误: %d", resp.StatusCode)
						if envCfg.IsDevelopment() {
							formattedBody := utils.FormatJSONBytesForLog(respBodyBytes, 500)
							log.Printf("📦 错误响应体:\n%s", formattedBody)

							respHeaders := make(map[string]string)
							for key, values := range resp.Header {
								if len(values) > 0 {
									respHeaders[key] = values[0]
								}
							}
							respHeadersJSON, _ := json.MarshalIndent(respHeaders, "", "  ")
							log.Printf("📋 错误响应头:\n%s", string(respHeadersJSON))
						}
					}
					if reqLogManager != nil && currentRequestLogID != "" {
						completeTime := time.Now()
						record := &requestlog.RequestLog{
							Status:        requestlog.StatusError,
							CompleteTime:  completeTime,
							DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
							Type:          upstream.ServiceType,
							ProviderName:  upstream.Name,
							HTTPStatus:    resp.StatusCode,
							ChannelID:     logChannelIndex,
							ChannelName:   logChannelName,
							Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, ""),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					recordSingleFailure(resp.StatusCode)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return
				}
			} else {
				// No failover tracker - return error to client
				if envCfg.EnableResponseLogs {
					log.Printf("⚠️ 上游返回错误: %d", resp.StatusCode)
					if envCfg.IsDevelopment() {
						formattedBody := utils.FormatJSONBytesForLog(respBodyBytes, 500)
						log.Printf("📦 错误响应体:\n%s", formattedBody)
					}
				}
				if reqLogManager != nil && currentRequestLogID != "" {
					completeTime := time.Now()
					record := &requestlog.RequestLog{
						Status:        requestlog.StatusError,
						CompleteTime:  completeTime,
						DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
						Type:          upstream.ServiceType,
						ProviderName:  upstream.Name,
						HTTPStatus:    resp.StatusCode,
						ChannelID:     logChannelIndex,
						ChannelName:   logChannelName,
						Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
						UpstreamError: string(respBodyBytes),
						FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
					}
					_ = reqLogManager.Update(currentRequestLogID, record)
				}
				SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
				recordSingleFailure(resp.StatusCode)
				c.Data(resp.StatusCode, "application/json", respBodyBytes)
				return
			}
		}

		// 处理成功响应 - reset error counters on success
		if failoverTracker != nil {
			failoverTracker.ResetOnSuccess(upstream.Index, apiKey)
		}
		if len(deprioritizeCandidates) > 0 {
			for key := range deprioritizeCandidates {
				if err := cfgManager.DeprioritizeAPIKey(key); err != nil {
					log.Printf("⚠️ 密钥降级失败: %v", err)
				}
			}
		}

		if claudeReq.Stream {
			recordSingleSuccess(resp.StatusCode)
			handleStreamResponse(c, resp, provider, envCfg, cfgManager, currentStartTime, upstream, reqLogManager, currentRequestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
		} else {
			recordSingleSuccess(resp.StatusCode)
			handleNormalResponse(c, resp, provider, envCfg, cfgManager, currentStartTime, upstream, reqLogManager, currentRequestLogID, claudeReq.Model, usageManager, logChannelIndex, logChannelName)
		}
		return
	}

	// All keys failed
	log.Printf("💥 All API keys failed")

	// 更新请求日志为错误状态
	if reqLogManager != nil && currentRequestLogID != "" {
		httpStatus := 500
		errMsg := "all API keys are unavailable"
		upstreamErr := ""
		failoverInfo := ""
		if lastFailoverError != nil && lastFailoverError.Status != 0 {
			httpStatus = lastFailoverError.Status
			upstreamErr = string(lastFailoverError.Body)
			failoverInfo = lastFailoverError.FailoverInfo
		}
		if lastError != nil {
			errMsg = lastError.Error()
		}
		record := &requestlog.RequestLog{
			Status:        requestlog.StatusError,
			CompleteTime:  time.Now(),
			DurationMs:    time.Since(currentStartTime).Milliseconds(),
			Model:         claudeReq.Model,
			Type:          upstream.ServiceType,
			ProviderName:  upstream.Name,
			HTTPStatus:    httpStatus,
			ChannelID:     logChannelIndex,
			ChannelName:   logChannelName,
			Endpoint:      "/v1/messages",
			Error:         errMsg,
			UpstreamError: upstreamErr,
			FailoverInfo:  failoverInfo,
		}
		_ = reqLogManager.Update(currentRequestLogID, record)
	}

	if lastFailoverError != nil {
		status := lastFailoverError.Status
		if status == 0 {
			status = 500
		}
		recordSingleFailure(status)
		SaveErrorDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, status, lastFailoverError.Body)
		var errBody map[string]interface{}
		if err := json.Unmarshal(lastFailoverError.Body, &errBody); err == nil {
			c.JSON(status, errBody)
		} else {
			c.JSON(status, gin.H{"error": string(lastFailoverError.Body)})
		}
	} else {
		recordSingleFailure(500)
		errMsg := "unknown error"
		if lastError != nil {
			errMsg = lastError.Error()
		}
		errJSON := fmt.Sprintf(`{"error":"all upstream API keys are unavailable","details":"%s"}`, errMsg)
		SaveErrorDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, 500, []byte(errJSON))
		c.JSON(500, gin.H{
			"error":   "all upstream API keys are unavailable",
			"details": errMsg,
		})
	}
}

// sendRequest 发送HTTP请求
func sendRequest(req *http.Request, upstream *config.UpstreamConfig, envCfg *config.EnvConfig, isStream bool) (*http.Response, error) {
	// 使用全局客户端管理器
	clientManager := httpclient.GetManager()

	var client *http.Client
	if isStream {
		// 流式请求：使用无超时的客户端，但有响应头超时
		client = clientManager.GetStreamClient(upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	} else {
		// 普通请求：使用有超时的客户端，同时应用渠道的响应头超时设置
		timeout := time.Duration(envCfg.RequestTimeout) * time.Millisecond
		client = clientManager.GetStandardClient(timeout, upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	}

	if upstream.InsecureSkipVerify && envCfg.EnableRequestLogs {
		log.Printf("⚠️ 正在跳过对 %s 的TLS证书验证", req.URL.String())
	}

	// Claude 流式请求必须显式声明 SSE 接受类型，避免部分网关返回非标准分段。
	if isStream && upstream.ServiceType == "claude" {
		req.Header.Set("Accept", "text/event-stream")
	}

	if envCfg.EnableRequestLogs {
		log.Printf("🌐 实际请求URL: %s", req.URL.String())
		log.Printf("📤 请求方法: %s", req.Method)
		if envCfg.IsDevelopment() {
			// 对请求头做敏感信息脱敏
			reqHeaders := make(map[string]string)
			for key, values := range req.Header {
				if len(values) > 0 {
					reqHeaders[key] = values[0]
				}
			}
			maskedReqHeaders := utils.MaskSensitiveHeaders(reqHeaders)
			reqHeadersJSON, _ := json.MarshalIndent(maskedReqHeaders, "", "  ")
			log.Printf("📋 实际请求头:\n%s", string(reqHeadersJSON))

			if req.Body != nil {
				// 读取请求体用于日志
				bodyBytes, err := io.ReadAll(req.Body)
				if err == nil {
					// 恢复请求体
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

					// 使用智能截断和简化函数（与TS版本对齐）
					formattedBody := utils.FormatJSONBytesForLog(bodyBytes, 500)
					log.Printf("📦 实际请求体:\n%s", formattedBody)
				}
			}
		}
	}

	return client.Do(req)
}

// trackMessagesUsage tracks usage for Messages API channels based on quota type
func trackMessagesUsage(usageManager *quota.UsageManager, upstream *config.UpstreamConfig, model string, cost float64) {
	if usageManager == nil || upstream.QuotaType == "" {
		return
	}

	// Check if this model should be counted for quota
	if !upstream.ShouldCountQuota(model) {
		return
	}

	var amount float64
	switch upstream.QuotaType {
	case "requests":
		amount = 1
	case "credit":
		amount = cost
	default:
		return
	}

	if err := usageManager.IncrementUsage(upstream.Index, amount); err != nil {
		log.Printf("⚠️ 配额使用量追踪失败 (Messages, channelIndex=%d): %v", upstream.Index, err)
	}
}

// handleNormalResponse 处理非流式响应
func handleNormalResponse(
	c *gin.Context,
	resp *http.Response,
	provider providers.Provider,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	startTime time.Time,
	upstream *config.UpstreamConfig,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	requestModel string,
	usageManager *quota.UsageManager,
	logChannelIndex int,
	logChannelName string,
) {
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to read response"})
		return
	}

	completeTime := time.Now()
	durationMs := completeTime.Sub(startTime).Milliseconds()

	if envCfg.EnableResponseLogs {
		log.Printf("⏱️ 响应完成: %dms, 状态: %d", durationMs, resp.StatusCode)
		if envCfg.IsDevelopment() {
			// 响应头(不需要脱敏)
			respHeaders := make(map[string]string)
			for key, values := range resp.Header {
				if len(values) > 0 {
					respHeaders[key] = values[0]
				}
			}
			respHeadersJSON, _ := json.MarshalIndent(respHeaders, "", "  ")
			log.Printf("📋 响应头:\n%s", string(respHeadersJSON))

			// 使用智能截断（与TS版本对齐）
			formattedBody := utils.FormatJSONBytesForLog(bodyBytes, 500)
			log.Printf("📦 响应体:\n%s", formattedBody)
		}
	}

	providerResp := &types.ProviderResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       bodyBytes,
		Stream:     false,
	}

	claudeResp, err := provider.ConvertToClaudeResponse(providerResp)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to convert response"})
		return
	}

	// 监听响应关闭事件(客户端断开连接)
	closeNotify := c.Writer.CloseNotify()
	go func() {
		select {
		case <-closeNotify:
			// 检查响应是否已完成
			if !c.Writer.Written() {
				if envCfg.EnableResponseLogs {
					responseTime := time.Since(startTime).Milliseconds()
					log.Printf("⏱️ 响应中断: %dms, 状态: %d", responseTime, resp.StatusCode)
				}
			}
		case <-time.After(10 * time.Second):
			// 超时退出goroutine,避免泄漏
			return
		}
	}()

	// 转发上游响应头到客户端（透明代理）
	utils.ForwardResponseHeaders(resp.Header, c.Writer)

	c.JSON(200, claudeResp)

	// 响应完成后记录
	if envCfg.EnableResponseLogs {
		responseTime := time.Since(startTime).Milliseconds()
		log.Printf("⏱️ 响应发送完成: %dms, 状态: %d", responseTime, resp.StatusCode)
	}

	// 更新请求日志（所有上游都更新；usage/成本仅在可提取时填充）
	if reqLogManager != nil && requestLogID != "" {
		var usage *types.Usage
		var responseModel string

		if claudeResp != nil {
			usage = claudeResp.Usage
		}

		// 从响应中提取实际使用的模型名（若有）
		var respMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &respMap); err == nil {
			if m, ok := respMap["model"].(string); ok {
				responseModel = m
			}
		}

		// 用于定价计算的模型名（优先响应模型，若无定价配置则回退到请求模型）
		pricingModel := responseModel
		if pricingModel == "" {
			pricingModel = requestModel
		} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && requestModel != "" {
			pricingModel = requestModel
		}

		record := &requestlog.RequestLog{
			Status:        requestlog.StatusCompleted,
			CompleteTime:  completeTime,
			DurationMs:    durationMs,
			Type:          upstream.ServiceType,
			ProviderName:  upstream.Name,
			ResponseModel: responseModel,
			HTTPStatus:    resp.StatusCode,
			ChannelID:     logChannelIndex,
			ChannelName:   logChannelName,
		}

		if usage != nil {
			record.InputTokens = usage.InputTokens
			record.OutputTokens = usage.OutputTokens
			record.CacheCreationInputTokens = usage.CacheCreationInputTokens
			record.CacheReadInputTokens = usage.CacheReadInputTokens
			record.TotalTokens = usage.TotalTokens

			if pm := pricing.GetManager(); pm != nil {
				var multipliers *pricing.PriceMultipliers
				if channelMult := upstream.GetPriceMultipliers(pricingModel); channelMult != nil {
					multipliers = &pricing.PriceMultipliers{
						InputMultiplier:         channelMult.GetEffectiveMultiplier("input"),
						OutputMultiplier:        channelMult.GetEffectiveMultiplier("output"),
						CacheCreationMultiplier: channelMult.GetEffectiveMultiplier("cacheCreation"),
						CacheReadMultiplier:     channelMult.GetEffectiveMultiplier("cacheRead"),
					}
				}
				breakdown := pm.CalculateCostWithBreakdown(
					pricingModel,
					usage.InputTokens,
					usage.OutputTokens,
					usage.CacheCreationInputTokens,
					usage.CacheReadInputTokens,
					multipliers,
				)
				record.Price = breakdown.TotalCost
				record.InputCost = breakdown.InputCost
				record.OutputCost = breakdown.OutputCost
				record.CacheCreationCost = breakdown.CacheCreationCost
				record.CacheReadCost = breakdown.CacheReadCost
			}
		}

		if err := reqLogManager.Update(requestLogID, record); err != nil {
			log.Printf("⚠️ 请求日志更新失败: %v", err)
		}

		// Save debug log if enabled
		SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, bodyBytes)

		// Track usage for quota (count 2xx and 400 as successful - 400 is client error but still counts as a request)
		if (resp.StatusCode >= 200 && resp.StatusCode < 300) || resp.StatusCode == 400 {
			trackMessagesUsage(usageManager, upstream, requestModel, record.Price)
		}
	}
}

// handleStreamResponse 处理流式响应
func handleStreamResponse(
	c *gin.Context,
	resp *http.Response,
	provider providers.Provider,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	startTime time.Time,
	upstream *config.UpstreamConfig,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	requestModel string,
	usageManager *quota.UsageManager,
	logChannelIndex int,
	logChannelName string,
) {
	defer resp.Body.Close()

	// Check if upstream returned a non-SSE response (e.g., JSON error with HTTP 200)
	// This can happen when upstream returns an error but with 200 status code
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "text/event-stream") {
		// Read the response body
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("⚠️ Failed to read non-SSE response body: %v", err)
			c.JSON(500, gin.H{"error": "Failed to read response"})
			return
		}

		completeTime := time.Now()
		durationMs := completeTime.Sub(startTime).Milliseconds()

		// Check if this is an error response
		var errResp map[string]interface{}
		isError := false
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
			if _, hasError := errResp["error"]; hasError {
				isError = true
			}
		}

		if envCfg.EnableResponseLogs {
			log.Printf("⚠️ Upstream returned non-SSE response (Content-Type: %s) for streaming request", contentType)
			if envCfg.IsDevelopment() {
				formattedBody := utils.FormatJSONBytesForLog(bodyBytes, 500)
				log.Printf("📦 Non-SSE response body:\n%s", formattedBody)
			}
		}

		// Update request log
		if reqLogManager != nil && requestLogID != "" {
			status := requestlog.StatusCompleted
			errorMsg := ""
			if isError {
				status = requestlog.StatusError
				errorMsg = "upstream returned error with HTTP 200"
			}
			record := &requestlog.RequestLog{
				Status:        status,
				CompleteTime:  completeTime,
				DurationMs:    durationMs,
				Type:          upstream.ServiceType,
				ProviderName:  upstream.Name,
				HTTPStatus:    resp.StatusCode,
				ChannelID:     logChannelIndex,
				ChannelName:   logChannelName,
				Error:         errorMsg,
				UpstreamError: string(bodyBytes),
			}
			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}

			// Save debug log
			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, bodyBytes)
		}

		// Forward the response to client
		c.Data(resp.StatusCode, contentType, bodyBytes)
		return
	}

	eventChan, errChan, err := provider.HandleStreamResponse(resp.Body)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to handle stream response"})
		return
	}

	// 先转发上游响应头（透明代理）
	utils.ForwardResponseHeaders(resp.Header, c.Writer)

	// 设置 SSE 响应头（可能覆盖上游的 Content-Type）
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Status(200)

	var logBuffer bytes.Buffer
	var synthesizer *utils.StreamSynthesizer
	// /v1/messages always emits Claude-style SSE to clients after provider conversion.
	firstTokenDetector := streamDetectorForServiceType("claude")
	var firstTokenTime *time.Time
	var firstStreamPayloadTime *time.Time

	// We need synthesizer to extract usage/model for request logs from streaming payloads.
	needsSynthesizer := (upstream.ServiceType == "claude" ||
		upstream.ServiceType == "openai_chat" ||
		upstream.ServiceType == "responses" ||
		upstream.ServiceType == "openai-oauth") && reqLogManager != nil
	streamLoggingEnabled := envCfg.IsDevelopment() && envCfg.EnableResponseLogs

	// Check if debug logging is enabled (need to capture response body)
	debugLogEnabled := cfgManager.GetDebugLogConfig().Enabled

	if streamLoggingEnabled || needsSynthesizer {
		synthesizer = utils.NewStreamSynthesizer(upstream.ServiceType)
	}

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("⚠️ ResponseWriter不支持Flush接口")
		return
	}
	flusher.Flush()

	finalizeStreamSuccess := func() {
		completeTime := time.Now()
		durationMs := completeTime.Sub(startTime).Milliseconds()

		if envCfg.EnableResponseLogs {
			log.Printf("⏱️ 流式响应完成: %dms", durationMs)

			// 打印完整的响应内容
			if envCfg.IsDevelopment() {
				if synthesizer != nil {
					synthesizedContent := synthesizer.GetSynthesizedContent()
					parseFailed := synthesizer.IsParseFailed()
					if synthesizedContent != "" && !parseFailed {
						log.Printf("🛰️  上游流式响应合成内容:\n%s", strings.TrimSpace(synthesizedContent))
					} else if logBuffer.Len() > 0 {
						log.Printf("🛰️  上游流式响应原始内容:\n%s", logBuffer.String())
					}
				} else if logBuffer.Len() > 0 {
					// synthesizer为nil时，直接打印原始内容
					log.Printf("🛰️  上游流式响应原始内容:\n%s", logBuffer.String())
				}
			}
		}

		// 更新请求日志（所有上游都更新；usage/成本仅在可提取时填充）
		if reqLogManager != nil && requestLogID != "" {
			var usage *utils.StreamUsage
			responseModel := ""

			if synthesizer != nil {
				usage = synthesizer.GetUsage()
				responseModel = synthesizer.GetModel()
			}

			pricingModel := responseModel
			if pricingModel == "" {
				pricingModel = requestModel
			} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && requestModel != "" {
				pricingModel = requestModel
			}
			if firstTokenTime == nil && firstStreamPayloadTime != nil {
				firstTokenTime = firstStreamPayloadTime
			}

			record := &requestlog.RequestLog{
				Status:               requestlog.StatusCompleted,
				CompleteTime:         completeTime,
				DurationMs:           durationMs,
				FirstTokenTime:       firstTokenTime,
				FirstTokenDurationMs: firstTokenDurationFromStart(startTime, firstTokenTime),
				Type:                 upstream.ServiceType,
				ProviderName:         upstream.Name,
				ResponseModel:        responseModel,
				HTTPStatus:           resp.StatusCode,
				ChannelID:            logChannelIndex,
				ChannelName:          logChannelName,
			}

			if usage != nil {
				record.InputTokens = usage.InputTokens
				record.OutputTokens = usage.OutputTokens
				record.CacheCreationInputTokens = usage.CacheCreationInputTokens
				record.CacheReadInputTokens = usage.CacheReadInputTokens
				record.TotalTokens = usage.TotalTokens

				if pm := pricing.GetManager(); pm != nil {
					var multipliers *pricing.PriceMultipliers
					if channelMult := upstream.GetPriceMultipliers(pricingModel); channelMult != nil {
						multipliers = &pricing.PriceMultipliers{
							InputMultiplier:         channelMult.GetEffectiveMultiplier("input"),
							OutputMultiplier:        channelMult.GetEffectiveMultiplier("output"),
							CacheCreationMultiplier: channelMult.GetEffectiveMultiplier("cacheCreation"),
							CacheReadMultiplier:     channelMult.GetEffectiveMultiplier("cacheRead"),
						}
					}
					breakdown := pm.CalculateCostWithBreakdown(
						pricingModel,
						usage.InputTokens,
						usage.OutputTokens,
						usage.CacheCreationInputTokens,
						usage.CacheReadInputTokens,
						multipliers,
					)
					record.Price = breakdown.TotalCost
					record.InputCost = breakdown.InputCost
					record.OutputCost = breakdown.OutputCost
					record.CacheCreationCost = breakdown.CacheCreationCost
					record.CacheReadCost = breakdown.CacheReadCost
				}
			}

			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}

			// Save debug log if enabled (use logBuffer for stream response body)
			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, logBuffer.Bytes())

			// Track usage for quota (stream responses are successful when channel closed)
			trackMessagesUsage(usageManager, upstream, requestModel, record.Price)
		}
	}

	finalizeStreamError := func(streamErr error) {
		// 真的有错误发生
		log.Printf("💥 流式传输错误: %v", streamErr)

		// 打印已接收到的部分响应
		if envCfg.EnableResponseLogs && envCfg.IsDevelopment() {
			if synthesizer != nil {
				synthesizedContent := synthesizer.GetSynthesizedContent()
				if synthesizedContent != "" && !synthesizer.IsParseFailed() {
					log.Printf("🛰️  上游流式响应合成内容 (部分):\n%s", strings.TrimSpace(synthesizedContent))
				} else if logBuffer.Len() > 0 {
					log.Printf("🛰️  上游流式响应原始内容 (部分):\n%s", logBuffer.String())
				}
			}
		}
		if reqLogManager != nil && requestLogID != "" {
			completeTime := time.Now()
			if firstTokenTime == nil && firstStreamPayloadTime != nil {
				firstTokenTime = firstStreamPayloadTime
			}
			record := &requestlog.RequestLog{
				Status:               requestlog.StatusError,
				CompleteTime:         completeTime,
				DurationMs:           completeTime.Sub(startTime).Milliseconds(),
				FirstTokenTime:       firstTokenTime,
				FirstTokenDurationMs: firstTokenDurationFromStart(startTime, firstTokenTime),
				Type:                 upstream.ServiceType,
				ProviderName:         upstream.Name,
				HTTPStatus:           resp.StatusCode,
				ChannelID:            logChannelIndex,
				ChannelName:          logChannelName,
				Error:                streamErr.Error(),
			}
			_ = reqLogManager.Update(requestLogID, record)
		}
	}

	clientGone := false
	eventStream := eventChan
	errorStream := errChan
	for {
		if eventStream == nil && errorStream == nil {
			finalizeStreamSuccess()
			return
		}

		select {
		case event, ok := <-eventStream:
			if !ok {
				eventStream = nil
				continue
			}

			// 缓存事件用于最后的日志输出和 usage 提取
			markFirstSSEPayloadInChunkIfPresent(event, &firstStreamPayloadTime)
			if firstTokenDetector != nil && firstTokenTime == nil {
				markFirstTokenIfDetected(firstTokenDetector.ObserveChunk(event), &firstTokenTime)
			}
			if streamLoggingEnabled || needsSynthesizer || debugLogEnabled {
				if streamLoggingEnabled || debugLogEnabled {
					logBuffer.WriteString(event)
				}
				if synthesizer != nil {
					lines := strings.Split(event, "\n")
					for _, line := range lines {
						synthesizer.ProcessLine(line)
					}
				}
			}

			// 实时转发给客户端（流式传输）
			if !clientGone {
				_, err := w.Write([]byte(event))
				if err != nil {
					clientGone = true // 标记客户端已断开，停止后续写入
					errMsg := err.Error()
					if strings.Contains(errMsg, "broken pipe") || strings.Contains(errMsg, "connection reset") {
						if envCfg.ShouldLog("info") {
							log.Printf("ℹ️ 客户端中断连接 (正常行为)，继续接收上游数据...")
						}
					} else {
						log.Printf("⚠️ 流式传输写入错误: %v", err)
					}
					// 注意：这里不再return，而是继续循环以耗尽eventChan
				} else {
					flusher.Flush()
				}
			}

		case streamErr, ok := <-errorStream:
			if !ok {
				// errChan 关闭，继续等待 eventChan 关闭，避免误判提前完成
				errorStream = nil
				continue
			}
			if streamErr != nil {
				finalizeStreamError(streamErr)
				return
			}
		}
	}
}

// shouldRetryWithNextKey 判断是否应该使用下一个密钥重试
// 返回: (shouldFailover bool, isQuotaRelated bool)
func shouldRetryWithNextKey(statusCode int, bodyBytes []byte) (bool, bool) {
	// 401/403 通常是认证问题
	if statusCode == 401 || statusCode == 403 {
		return true, false
	}

	// 429 速率限制，切换下一个密钥
	if statusCode == 429 {
		return true, true
	}

	isQuotaRelated := false

	// 检查错误消息
	var errResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
		if errObj, ok := errResp["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok {
				msgLower := strings.ToLower(msg)
				if strings.Contains(msgLower, "insufficient") ||
					strings.Contains(msgLower, "invalid") ||
					strings.Contains(msgLower, "unauthorized") ||
					strings.Contains(msgLower, "quota") ||
					strings.Contains(msgLower, "rate limit") ||
					strings.Contains(msg, "请求数限制") ||
					strings.Contains(msgLower, "credit") ||
					strings.Contains(msgLower, "balance") {

					// 判断是否为额度/余额相关
					if strings.Contains(msgLower, "积分不足") ||
						strings.Contains(msgLower, "insufficient") ||
						strings.Contains(msgLower, "credit") ||
						strings.Contains(msgLower, "balance") ||
						strings.Contains(msgLower, "quota") ||
						strings.Contains(msg, "请求数限制") {
						isQuotaRelated = true
					}
					return true, isQuotaRelated
				}
			}

			if errType, ok := errObj["type"].(string); ok {
				errTypeLower := strings.ToLower(errType)
				if strings.Contains(errTypeLower, "permission") ||
					strings.Contains(errTypeLower, "insufficient") ||
					strings.Contains(errTypeLower, "over_quota") ||
					strings.Contains(errTypeLower, "billing") {

					// 判断是否为额度/余额相关
					if strings.Contains(errTypeLower, "over_quota") ||
						strings.Contains(errTypeLower, "billing") ||
						strings.Contains(errTypeLower, "insufficient") {
						isQuotaRelated = true
					}
					return true, isQuotaRelated
				}
			}
		}
	}

	// 500+ 错误也可以尝试 failover
	if statusCode >= 500 {
		return true, false
	}

	return false, false
}

// maskAPIKey 掩码API密钥（与 TS 版本保持一致）
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}

	length := len(key)
	if length <= 10 {
		// 短密钥：保留前3位和后2位
		if length <= 5 {
			return "***"
		}
		return key[:3] + "***" + key[length-2:]
	}

	// 长密钥：保留前8位和后5位
	return key[:8] + "***" + key[length-5:]
}
