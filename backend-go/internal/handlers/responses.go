package handlers

import (
	"bufio"
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
	"github.com/JillVernus/cc-bridge/internal/auth/codex"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/converters"
	"github.com/JillVernus/cc-bridge/internal/httpclient"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// codexTokenManager is a shared token manager for OAuth token refresh
var codexTokenManager = codex.NewTokenManager()

// trackResponsesUsage tracks usage for Responses API channels based on quota type
func trackResponsesUsage(usageManager *quota.UsageManager, upstream *config.UpstreamConfig, model string, cost float64) {
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

	if err := usageManager.IncrementResponsesUsage(upstream.Index, amount); err != nil {
		log.Printf("⚠️ 配额使用量追踪失败 (Responses, channelIndex=%d): %v", upstream.Index, err)
	}
}

// ResponsesHandler Responses API 代理处理器
// 支持多渠道调度：当配置多个渠道时自动启用
func ResponsesHandler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
) gin.HandlerFunc {
	return ResponsesHandlerWithAPIKey(envCfg, cfgManager, sessionManager, channelScheduler, reqLogManager, nil, nil, nil, nil)
}

// ResponsesHandlerWithAPIKey Responses API 代理处理器（支持 API Key 验证）
func ResponsesHandlerWithAPIKey(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	apiKeyManager *apikey.Manager,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) gin.HandlerFunc {
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
				if !validatedKey.CheckEndpointPermission("responses") {
					c.JSON(403, gin.H{
						"error": "Endpoint /v1/responses not allowed for this API key",
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

		// 解析 Responses 请求
		var responsesReq types.ResponsesRequest
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &responsesReq)
		}

		// Check model permission
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckModelPermission(responsesReq.Model) {
					c.JSON(403, gin.H{
						"error": fmt.Sprintf("Model %s not allowed for this API key", responsesReq.Model),
						"code":  "MODEL_NOT_ALLOWED",
					})
					return
				}
			}
		}

		// 提取对话标识用于 Trace 亲和性 + 记录 user/session
		// - Codex：user_id 统一记录为 "codex"，session_id 使用 prompt_cache_key（会话级标识）
		// - Claude：使用复合 user_id 解析出 user_id + session_id
		promptCacheKey := strings.TrimSpace(gjson.GetBytes(bodyBytes, "prompt_cache_key").String())
		compoundUserID := ""
		userID := ""
		sessionID := ""
		if promptCacheKey != "" {
			userID = "codex"
			sessionID = promptCacheKey
			compoundUserID = promptCacheKey
		} else {
			// 优先级: Conversation_id Header > Session_id Header > prompt_cache_key > metadata.user_id
			compoundUserID = extractConversationID(c, bodyBytes)
			userID, sessionID = parseClaudeCodeUserID(compoundUserID)
		}

		// 提取 reasoning.effort 用于日志显示
		reasoningEffort := gjson.GetBytes(bodyBytes, "reasoning.effort").String()

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
				Status:          requestlog.StatusPending,
				InitialTime:     startTime,
				Model:           responsesReq.Model,
				ReasoningEffort: reasoningEffort,
				Stream:          responsesReq.Stream,
				Endpoint:        "/v1/responses",
				ClientID:        userID,
				SessionID:       sessionID,
				APIKeyID:        apiKeyID,
			}
			if err := reqLogManager.Add(pendingLog); err != nil {
				log.Printf("⚠️ 创建 pending 请求日志失败: %v", err)
			} else {
				requestLogID = pendingLog.ID
			}
		}

		// 检查是否为多渠道模式
		isMultiChannel := channelScheduler.IsMultiChannelMode(true) // true = isResponses

		// Get allowed channels from API key permissions
		var allowedChannels []int
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				allowedChannels = validatedKey.GetAllowedChannels(true) // true = Responses API
			}
		}

		if isMultiChannel {
			// Multi-channel mode: use scheduler with failover
			handleMultiChannelResponses(c, envCfg, cfgManager, channelScheduler, sessionManager, bodyBytes, responsesReq, userID, sessionID, apiKeyID, reasoningEffort, startTime, reqLogManager, requestLogID, usageManager, allowedChannels, failoverTracker, channelRateLimiter)
		} else {
			// 单渠道模式：使用现有逻辑
			handleSingleChannelResponses(c, envCfg, cfgManager, sessionManager, bodyBytes, responsesReq, startTime, reqLogManager, requestLogID, usageManager, allowedChannels, failoverTracker, userID, sessionID, apiKeyID, channelRateLimiter)
		}
	})
}

// handleMultiChannelResponses handles multi-channel Responses API requests with failover support.
// When a channel fails and there are more channels to try, it logs the failed attempt
// with StatusFailover and creates a new pending log for the next attempt.
func handleMultiChannelResponses(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	sessionManager *session.SessionManager,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	clientID string,
	sessionID string,
	apiKeyID *int64,
	reasoningEffort string,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannels []int,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) {
	failedChannels := make(map[int]bool)
	var lastError error
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}
	var lastFailedUpstream *config.UpstreamConfig

	maxChannelAttempts := channelScheduler.GetActiveChannelCount(true) // true = isResponses

	for channelAttempt := 0; channelAttempt < maxChannelAttempts; channelAttempt++ {
		selection, err := channelScheduler.SelectChannel(c.Request.Context(), clientID, failedChannels, true, allowedChannels, responsesReq.Model)
		if err != nil {
			lastError = err
			break
		}

		upstream := selection.Upstream
		channelIndex := selection.ChannelIndex

		if envCfg.ShouldLog("info") {
			log.Printf("🎯 [Multi-Channel/Responses] Selected channel: [%d] %s (reason: %s, attempt %d/%d)",
				channelIndex, upstream.Name, selection.Reason, channelAttempt+1, maxChannelAttempts)
		}

		// Check per-channel rate limit (if configured)
		if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
			result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "responses")
			if !result.Allowed {
				if result.Queued {
					// Request was queued but timed out or client disconnected
					log.Printf("⏰ [Channel Rate Limit/Responses] Channel %d (%s): request failed after queue - %v",
						channelIndex, upstream.Name, result.Error)
				} else {
					// Rate limit exceeded, queue full or disabled
					log.Printf("🚫 [Channel Rate Limit/Responses] Channel %d (%s): %v",
						channelIndex, upstream.Name, result.Error)
				}
				// Mark channel as failed for this request and try next channel
				failedChannels[channelIndex] = true
				lastError = result.Error
				continue
			}
			if result.Queued {
				log.Printf("✅ [Channel Rate Limit/Responses] Channel %d (%s): request released after %v queue wait",
					channelIndex, upstream.Name, result.WaitDuration)
			}
		}

		success, failoverErr, updatedLogID := tryResponsesChannelWithAllKeys(c, envCfg, cfgManager, sessionManager, upstream, bodyBytes, responsesReq, startTime, reqLogManager, requestLogID, usageManager, failoverTracker, clientID, sessionID, apiKeyID)
		requestLogID = updatedLogID // Update requestLogID in case it was changed during retry_wait

		if success {
			channelScheduler.RecordSuccess(channelIndex, true)
			channelScheduler.SetTraceAffinity(clientID, channelIndex)
			return
		}

		// Channel failed: record failure metrics
		channelScheduler.RecordFailure(channelIndex, true)
		failedChannels[channelIndex] = true

		// Check if there are more channels to try
		hasMoreChannels := channelAttempt < maxChannelAttempts-1 && len(failedChannels) < maxChannelAttempts

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
				errorMsg := fmt.Sprintf("failover to next channel (%d/%d)", channelAttempt+1, maxChannelAttempts)
				if httpStatus > 0 {
					errorMsg = fmt.Sprintf("%d: %s", httpStatus, errorMsg)
				}

				failoverRecord := &requestlog.RequestLog{
					Status:          requestlog.StatusFailover,
					CompleteTime:    completeTime,
					DurationMs:      completeTime.Sub(startTime).Milliseconds(),
					Type:            upstream.ServiceType,
					ProviderName:    upstream.Name,
					Model:           responsesReq.Model,
					ReasoningEffort: reasoningEffort,
					ChannelID:       channelIndex,
					ChannelName:     upstream.Name,
					HTTPStatus:      httpStatus,
					Error:           errorMsg,
					UpstreamError:   upstreamErr,
					FailoverInfo:    failoverInfo,
				}
				if err := reqLogManager.Update(requestLogID, failoverRecord); err != nil {
					log.Printf("⚠️ Failed to update failover log: %v", err)
				}

				// Create new pending log for next channel attempt
				newPendingLog := &requestlog.RequestLog{
					Status:          requestlog.StatusPending,
					InitialTime:     time.Now(),
					Model:           responsesReq.Model,
					ReasoningEffort: reasoningEffort,
					Stream:          responsesReq.Stream,
					Endpoint:        "/v1/responses",
					ClientID:        clientID,
					SessionID:       sessionID,
					APIKeyID:        apiKeyID,
				}
				if err := reqLogManager.Add(newPendingLog); err != nil {
					log.Printf("⚠️ Failed to create failover pending log: %v", err)
				} else {
					requestLogID = newPendingLog.ID
					startTime = newPendingLog.InitialTime
				}
			}

			log.Printf("⚠️ [Multi-Channel/Responses] Channel [%d] %s all keys failed, trying next channel", channelIndex, upstream.Name)
		}

		if failoverErr != nil {
			lastFailoverError = failoverErr
			lastError = fmt.Errorf("channel [%d] %s failed", channelIndex, upstream.Name)
			lastFailedUpstream = upstream
		}
	}

	// All channels failed
	log.Printf("💥 [Multi-Channel/Responses] All channels failed")

	// Update request log with final error status
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
		record := &requestlog.RequestLog{
			Status:          requestlog.StatusError,
			CompleteTime:    time.Now(),
			DurationMs:      time.Since(startTime).Milliseconds(),
			Model:           responsesReq.Model,
			ReasoningEffort: reasoningEffort,
			HTTPStatus:      httpStatus,
			Error:           errMsg,
			UpstreamError:   upstreamErr,
			FailoverInfo:    failoverInfo,
		}
		if lastFailedUpstream != nil {
			record.Type = lastFailedUpstream.ServiceType
			record.ProviderName = lastFailedUpstream.Name
			record.ChannelID = lastFailedUpstream.Index
			record.ChannelName = lastFailedUpstream.Name
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
		errMsg := "all channels unavailable"
		if lastError != nil {
			errMsg = lastError.Error()
		}
		errJSON := fmt.Sprintf(`{"error":"all Responses channels unavailable","details":"%s"}`, errMsg)
		SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, 503, []byte(errJSON))
		c.JSON(503, gin.H{
			"error":   "all Responses channels unavailable",
			"details": errMsg,
		})
	}
}

// tryResponsesChannelWithAllKeys 尝试使用 Responses 渠道的所有密钥
func tryResponsesChannelWithAllKeys(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	clientID string,
	sessionID string,
	apiKeyID *int64,
) (bool, *struct {
	Status       int
	Body         []byte
	FailoverInfo string
}, string) {
	// 处理 OpenAI OAuth 渠道（Codex）
	if upstream.ServiceType == "openai-oauth" {
		success, failoverErr := tryResponsesChannelWithOAuth(c, envCfg, cfgManager, sessionManager, upstream, bodyBytes, responsesReq, startTime, reqLogManager, requestLogID, usageManager)
		return success, failoverErr, requestLogID
	}

	if len(upstream.APIKeys) == 0 {
		return false, nil, requestLogID
	}

	provider := &providers.ResponsesProvider{SessionManager: sessionManager}

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
	currentStartTime := startTime
	currentRequestLogID := requestLogID

	for attempt := 0; attempt < maxRetries || retryWaitPending; {
		retryWaitPending = false // Clear at start of each iteration

		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var apiKey string
		var err error

		// If we have a pinned key from a previous retry-same-key decision, use it
		if pinnedKey != "" {
			apiKey = pinnedKey
			pinnedKey = "" // Clear after use
			// Don't increment attempt for retry-same-key
		} else {
			apiKey, err = cfgManager.GetNextResponsesAPIKey(upstream, failedKeys)
			if err != nil {
				break
			}
			attempt++ // Only increment when trying a new key
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🔑 [Responses] 使用API密钥: %s (尝试 %d/%d)", maskAPIKey(apiKey), attempt, maxRetries)
		}

		providerReq, _, err := provider.ConvertToProviderRequest(c, upstream, apiKey)
		if err != nil {
			failedKeys[apiKey] = true
			continue
		}

		resp, err := sendResponsesRequest(providerReq, upstream, envCfg, responsesReq.Stream)
		if err != nil {
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			log.Printf("⚠️ [Responses] API密钥失败: %v", err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

			// Handle 429 errors with smart subtype detection
			if resp.StatusCode == 429 && failoverTracker != nil {
				// Choose failover logic based on quota type
				var decision config.FailoverDecision
				if upstream.QuotaType != "" {
					// Quota channel: use admin failover settings
					failoverConfig := cfgManager.GetFailoverConfig()
					decision = failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)
				} else {
					// Normal channel: use legacy circuit breaker (immediate failover on 429)
					decision = failoverTracker.LegacyFailover(resp.StatusCode)
				}

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key (tracker handles attempt counting)
					log.Printf("⏳ [Responses] 429 %s: 等待 %v 后重试同一密钥 (max: %d)", decision.Reason, decision.Wait, decision.MaxAttempts)

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
							ChannelID:     upstream.Index,
							ChannelName:   upstream.Name,
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
							Model:       responsesReq.Model,
							Stream:      responsesReq.Stream,
							Endpoint:    "/v1/responses",
							ClientID:    clientID,
							SessionID:   sessionID,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
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
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						currentStartTime = time.Now() // Reset startTime after wait completes
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						return false, nil, currentRequestLogID
					}

				case config.ActionFailoverKey:
					// Immediate failover to next key
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ [Responses] 429 %s: 立即切换到下一个密钥", decision.Reason)

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
						suspendedUntil := time.Now().Add(5 * time.Minute)
						if upstream.QuotaResetAt != nil && upstream.QuotaResetAt.After(time.Now()) {
							suspendedUntil = *upstream.QuotaResetAt
							log.Printf("⏸️ [Responses] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Responses] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						if err := reqLogManager.SuspendChannel(upstream.Index, "responses", suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (responses): %v", upstream.Index, err)
						}
					}
					log.Printf("⏸️ [Responses] 429 %s: 渠道暂停，切换到下一个渠道", decision.Reason)

					// Return false to trigger channel failover
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
							ChannelID:     upstream.Index,
							ChannelName:   upstream.Name,
							Error:         fmt.Sprintf("429 %s (threshold not reached)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, "threshold not reached"),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return true, nil, currentRequestLogID
				}
			}

			// Non-429 errors: choose failover logic based on quota type
			var shouldFailover, isQuotaRelated bool
			if failoverTracker != nil {
				if upstream.QuotaType != "" {
					// Quota channel: use admin failover settings
					failoverConfig := cfgManager.GetFailoverConfig()
					shouldFailover, isQuotaRelated = failoverTracker.ShouldFailover(upstream.Index, apiKey, resp.StatusCode, &failoverConfig)
				} else {
					// Normal channel: use legacy circuit breaker
					decision := failoverTracker.LegacyFailover(resp.StatusCode)
					shouldFailover = decision.Action == config.ActionFailoverKey
					isQuotaRelated = false
				}
			} else {
				shouldFailover, isQuotaRelated = shouldRetryWithNextKey(resp.StatusCode, respBodyBytes)
			}
			if shouldFailover {
				failedKeys[apiKey] = true
				cfgManager.MarkKeyAsFailed(apiKey)
				log.Printf("⚠️ [Responses] API密钥失败 (状态: %d)，尝试下一个密钥", resp.StatusCode)

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
					FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, "", failoverReason, "next key"),
				}

				if isQuotaRelated {
					deprioritizeCandidates[apiKey] = true
				}
				continue
			}

			// 非 failover 错误，更新请求日志并返回
			if reqLogManager != nil && currentRequestLogID != "" {
				completeTime := time.Now()
				record := &requestlog.RequestLog{
					Status:        requestlog.StatusError,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					HTTPStatus:    resp.StatusCode,
					ChannelID:     upstream.Index,
					ChannelName:   upstream.Name,
					Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
					UpstreamError: string(respBodyBytes),
					FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
				}
				_ = reqLogManager.Update(currentRequestLogID, record)
			}
			SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
			c.Data(resp.StatusCode, "application/json", respBodyBytes)
			return true, nil, currentRequestLogID
		}

		if len(deprioritizeCandidates) > 0 {
			for key := range deprioritizeCandidates {
				_ = cfgManager.DeprioritizeAPIKey(key)
			}
		}

		// Reset error counters on success
		if failoverTracker != nil {
			failoverTracker.ResetOnSuccess(upstream.Index, apiKey)
		}

		handleResponsesSuccess(c, resp, provider, upstream, envCfg, cfgManager, sessionManager, startTime, &responsesReq, bodyBytes, reqLogManager, currentRequestLogID, usageManager)
		return true, nil, currentRequestLogID
	}

	return false, lastFailoverError, currentRequestLogID
}

// tryResponsesChannelWithOAuth 使用 OAuth 认证尝试 Responses 请求（Codex）
func tryResponsesChannelWithOAuth(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
) (bool, *struct {
	Status       int
	Body         []byte
	FailoverInfo string
}) {
	// 辅助函数：更新请求日志为错误状态
	updateErrorLog := func(httpStatus int, errMsg string) {
		if reqLogManager != nil && requestLogID != "" {
			completeTime := time.Now()
			record := &requestlog.RequestLog{
				Status:        requestlog.StatusError,
				CompleteTime:  completeTime,
				DurationMs:    completeTime.Sub(startTime).Milliseconds(),
				Type:          "openai-oauth",
				ProviderName:  upstream.Name,
				HTTPStatus:    httpStatus,
				ChannelID:     upstream.Index,
				ChannelName:   upstream.Name,
				UpstreamError: errMsg,
			}
			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}
		}
	}

	if upstream.OAuthTokens == nil {
		errMsg := "OAuth tokens not configured for this channel"
		log.Printf("⚠️ [OAuth] 渠道 %s 未配置 OAuth tokens", upstream.Name)
		updateErrorLog(503, errMsg)
		return false, &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: 503,
			Body:   []byte(fmt.Sprintf(`{"error":"%s"}`, errMsg)),
		}
	}

	// 获取有效的 OAuth token（如果过期会自动刷新）
	accessToken, accountID, updatedTokens, err := codexTokenManager.GetValidToken(upstream.OAuthTokens)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to get valid OAuth token: %s", err.Error())
		log.Printf("⚠️ [OAuth] 获取有效 token 失败: %v", err)
		updateErrorLog(401, errMsg)
		return false, &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: 401,
			Body:   []byte(fmt.Sprintf(`{"error":"%s"}`, errMsg)),
		}
	}

	// 如果 token 被刷新了，保存到配置中
	if updatedTokens != nil {
		if err := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, updatedTokens); err != nil {
			log.Printf("⚠️ [OAuth] 保存刷新后的 token 失败: %v", err)
		} else {
			log.Printf("✅ [OAuth] Token 已刷新并保存")
		}
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if envCfg.ShouldLog("info") {
		log.Printf("🔐 [OAuth] 使用 Codex OAuth 认证 (Account: %s...)", accountID[:12])
	}

	// 构建 OAuth 请求
	providerReq, err := buildCodexOAuthRequest(c, upstream, bodyBytes, responsesReq, accessToken, accountID)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to build OAuth request: %s", err.Error())
		log.Printf("⚠️ [OAuth] 构建请求失败: %v", err)
		updateErrorLog(500, errMsg)
		return false, &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: 500,
			Body:   []byte(fmt.Sprintf(`{"error":"%s"}`, errMsg)),
		}
	}

	resp, err := sendResponsesRequest(providerReq, upstream, envCfg, responsesReq.Stream)
	if err != nil {
		errMsg := fmt.Sprintf("Request failed: %s", err.Error())
		log.Printf("⚠️ [OAuth] 请求失败: %v", err)
		updateErrorLog(502, errMsg)
		return false, &struct {
			Status       int
			Body         []byte
			FailoverInfo string
		}{
			Status: 502,
			Body:   []byte(fmt.Sprintf(`{"error":"%s"}`, errMsg)),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

		log.Printf("⚠️ [OAuth] Codex API 返回错误: %d - %s", resp.StatusCode, string(respBodyBytes))

		// 更新请求日志为错误状态
		updateErrorLog(resp.StatusCode, string(respBodyBytes))

		// 对于 429 错误，记录配额超限状态
		if resp.StatusCode == 429 {
			retryAfter := quota.ParseRetryAfter(resp.Header.Get("Retry-After"))
			quota.GetManager().SetExceeded(upstream.Index, upstream.Name, "rate_limit_exceeded", retryAfter)
		}

		// 对于 401 错误，尝试强制刷新 token
		if resp.StatusCode == 401 {
			log.Printf("🔄 [OAuth] 401 错误，尝试强制刷新 token...")
			newTokens, refreshErr := codexTokenManager.RefreshTokensWithRetry(upstream.OAuthTokens.RefreshToken, 2)
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

	// 更新配额信息从响应头
	quota.GetManager().UpdateFromHeaders(upstream.Index, upstream.Name, resp.Header)

	provider := &providers.ResponsesProvider{SessionManager: sessionManager}
	handleResponsesSuccess(c, resp, provider, upstream, envCfg, cfgManager, sessionManager, startTime, &responsesReq, bodyBytes, reqLogManager, requestLogID, usageManager)
	return true, nil
}

// buildCodexOAuthRequest 构建 Codex OAuth API 请求
func buildCodexOAuthRequest(
	c *gin.Context,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	accessToken string,
	accountID string,
) (*http.Request, error) {
	// 解析请求体为 map 以保留所有字段
	var reqMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
		return nil, fmt.Errorf("解析请求失败: %w", err)
	}

	// 模型重定向
	if model, ok := reqMap["model"].(string); ok {
		reqMap["model"] = config.RedirectModel(model, upstream)
	}

	// 序列化请求体
	reqBody, err := json.Marshal(reqMap)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// Codex OAuth 使用固定的 API 端点
	targetURL := "https://chatgpt.com/backend-api/codex/responses"

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	// 构建 OAuth 请求头输入，转发原始请求的关键头部
	headerInput := utils.CodexOAuthHeadersInput{
		AccessToken:    accessToken,
		AccountID:      accountID,
		UserAgent:      c.GetHeader("User-Agent"),
		ConversationID: c.GetHeader("Conversation_id"),
		SessionID:      c.GetHeader("Session_id"),
		Originator:     c.GetHeader("Originator"),
	}

	// 如果是流式请求，确保正确的 Accept 头
	if responsesReq.Stream {
		utils.SetCodexOAuthStreamHeaders(req.Header, headerInput)
	} else {
		utils.SetCodexOAuthNonStreamHeaders(req.Header, headerInput)
	}

	return req, nil
}

// handleSingleChannelResponses 处理单渠道 Responses 请求（现有逻辑）
func handleSingleChannelResponses(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannels []int,
	failoverTracker *config.FailoverTracker,
	clientID string,
	sessionID string,
	apiKeyID *int64,
	channelRateLimiter *middleware.ChannelRateLimiter,
) {
	// 获取当前 Responses 上游配置
	upstream, err := cfgManager.GetCurrentResponsesUpstream()
	if err != nil {
		c.JSON(503, gin.H{
			"error": "未配置任何 Responses 渠道，请先在管理界面添加渠道",
			"code":  "NO_RESPONSES_UPSTREAM",
		})
		return
	}

	// Check if this channel is allowed by API key permissions
	if len(allowedChannels) > 0 {
		allowed := false
		for _, idx := range allowedChannels {
			if idx == upstream.Index {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(403, gin.H{
				"error": fmt.Sprintf("Channel %s (index %d) not allowed for this API key", upstream.Name, upstream.Index),
				"code":  "CHANNEL_NOT_ALLOWED",
			})
			return
		}
	}

	// Check per-channel rate limit (if configured)
	if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
		result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "responses")
		if !result.Allowed {
			log.Printf("🚫 [Channel Rate Limit/Responses] Channel %d (%s): %v", upstream.Index, upstream.Name, result.Error)
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": fmt.Sprintf("Channel rate limit exceeded (%d RPM)", upstream.RateLimitRpm),
			})
			return
		}
		if result.Queued {
			log.Printf("✅ [Channel Rate Limit/Responses] Channel %d (%s): request released after %v queue wait",
				upstream.Index, upstream.Name, result.WaitDuration)
		}
	}

	// 处理 OpenAI OAuth 渠道（Codex）
	if upstream.ServiceType == "openai-oauth" {
		success, failoverErr := tryResponsesChannelWithOAuth(c, envCfg, cfgManager, sessionManager, upstream, bodyBytes, responsesReq, startTime, reqLogManager, requestLogID, usageManager)
		if !success && failoverErr != nil {
			status := failoverErr.Status
			if status == 0 {
				status = 500
			}
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, failoverErr.Body)
			var errBody map[string]interface{}
			if err := json.Unmarshal(failoverErr.Body, &errBody); err == nil {
				c.JSON(status, errBody)
			} else {
				c.JSON(status, gin.H{"error": string(failoverErr.Body)})
			}
		}
		return
	}

	if len(upstream.APIKeys) == 0 {
		c.JSON(503, gin.H{
			"error": fmt.Sprintf("当前 Responses 渠道 \"%s\" 未配置API密钥", upstream.Name),
			"code":  "NO_API_KEYS",
		})
		return
	}

	provider := &providers.ResponsesProvider{SessionManager: sessionManager}

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

		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var apiKey string
		var err error

		// If we have a pinned key from a previous retry-same-key decision, use it
		if pinnedKey != "" {
			apiKey = pinnedKey
			pinnedKey = "" // Clear after use
			// Don't increment attempt for retry-same-key
		} else {
			apiKey, err = cfgManager.GetNextResponsesAPIKey(upstream, failedKeys)
			if err != nil {
				lastError = err
				break
			}
			attempt++ // Only increment when trying a new key
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🎯 使用 Responses 上游: %s - %s (尝试 %d/%d)", upstream.Name, upstream.BaseURL, attempt, maxRetries)
			log.Printf("🔑 使用API密钥: %s", maskAPIKey(apiKey))
		}

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

		if envCfg.EnableRequestLogs {
			log.Printf("📥 收到 Responses 请求: %s %s", c.Request.Method, c.Request.URL.Path)
			if envCfg.IsDevelopment() {
				formattedBody := utils.FormatJSONBytesForLog(lastOriginalBodyBytes, 500)
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

		resp, err := sendResponsesRequest(providerReq, upstream, envCfg, responsesReq.Stream)
		if err != nil {
			lastError = err
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			log.Printf("⚠️ API密钥失败: %v", err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

			// Handle 429 errors with smart subtype detection (single-channel mode)
			if resp.StatusCode == 429 && failoverTracker != nil {
				// Choose failover logic based on quota type
				var decision config.FailoverDecision
				if upstream.QuotaType != "" {
					// Quota channel: use admin failover settings
					failoverConfig := cfgManager.GetFailoverConfig()
					decision = failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)
				} else {
					// Normal channel: use legacy circuit breaker (immediate failover on 429)
					decision = failoverTracker.LegacyFailover(resp.StatusCode)
				}

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key (tracker handles attempt counting)
					log.Printf("⏳ [Responses] 429 %s: 等待 %v 后重试同一密钥 (max: %d)", decision.Reason, decision.Wait, decision.MaxAttempts)

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
							ChannelID:     upstream.Index,
							ChannelName:   upstream.Name,
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
							Model:       responsesReq.Model,
							Stream:      responsesReq.Stream,
							Endpoint:    "/v1/responses",
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
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
						pinnedKey = apiKey            // Pin for next attempt
						retryWaitPending = true       // Allow loop to continue
						currentStartTime = time.Now() // Reset startTime after wait completes
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						return
					}

				case config.ActionFailoverKey:
					// Immediate failover to next key
					lastError = fmt.Errorf("429 %s", decision.Reason)
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ [Responses] 429 %s: 立即切换到下一个密钥", decision.Reason)
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
						if err := reqLogManager.SuspendChannel(upstream.Index, "responses", suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (responses): %v", upstream.Index, err)
						}
					}
					log.Printf("⏸️ [Responses] 429 %s: 渠道暂停 (单渠道模式，无可用后备)", decision.Reason)

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
							ChannelID:     upstream.Index,
							ChannelName:   upstream.Name,
							Error:         fmt.Sprintf("429 %s (channel suspended)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "no fallback"),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return

				default:
					// ActionNone - return error to client
					if envCfg.EnableResponseLogs {
						log.Printf("⚠️ [Responses] 429 %s (threshold not reached)", decision.Reason)
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
							ChannelID:     upstream.Index,
							ChannelName:   upstream.Name,
							Error:         fmt.Sprintf("429 %s (threshold not reached)", decision.Reason),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionReturnErr, "threshold not reached"),
						}
						_ = reqLogManager.Update(currentRequestLogID, record)
					}
					SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
					c.Data(resp.StatusCode, "application/json", respBodyBytes)
					return
				}
			}

			// Non-429 errors: choose failover logic based on quota type
			var shouldFailover, isQuotaRelated bool
			if failoverTracker != nil {
				if upstream.QuotaType != "" {
					// Quota channel: use admin failover settings
					failoverConfig := cfgManager.GetFailoverConfig()
					shouldFailover, isQuotaRelated = failoverTracker.ShouldFailover(upstream.Index, apiKey, resp.StatusCode, &failoverConfig)
				} else {
					// Normal channel: use legacy circuit breaker
					decision := failoverTracker.LegacyFailover(resp.StatusCode)
					shouldFailover = decision.Action == config.ActionFailoverKey
					isQuotaRelated = false
				}
			} else {
				shouldFailover, isQuotaRelated = shouldRetryWithNextKey(resp.StatusCode, respBodyBytes)
			}
			if shouldFailover {
				lastError = fmt.Errorf("上游错误: %d", resp.StatusCode)
				failedKeys[apiKey] = true
				cfgManager.MarkKeyAsFailed(apiKey)

				log.Printf("⚠️ Responses API密钥失败 (状态: %d)，尝试下一个密钥", resp.StatusCode)
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
					FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionFailover, "next key"),
				}

				if isQuotaRelated {
					deprioritizeCandidates[apiKey] = true
				}
				continue
			}

			// Non-failover error - update request log and return
			if reqLogManager != nil && currentRequestLogID != "" {
				completeTime := time.Now()
				record := &requestlog.RequestLog{
					Status:        requestlog.StatusError,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(currentStartTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					HTTPStatus:    resp.StatusCode,
					ChannelID:     upstream.Index,
					ChannelName:   upstream.Name,
					Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
					UpstreamError: string(respBodyBytes),
					FailoverInfo:  requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
				}
				_ = reqLogManager.Update(currentRequestLogID, record)
			}

			if envCfg.EnableResponseLogs {
				log.Printf("⚠️ Responses 上游返回错误: %d", resp.StatusCode)
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
			SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)
			c.Data(resp.StatusCode, "application/json", respBodyBytes)
			return
		}

		if len(deprioritizeCandidates) > 0 {
			for key := range deprioritizeCandidates {
				if err := cfgManager.DeprioritizeAPIKey(key); err != nil {
					log.Printf("⚠️ 密钥降级失败: %v", err)
				}
			}
		}

		// Reset error counters on success
		if failoverTracker != nil {
			failoverTracker.ResetOnSuccess(upstream.Index, apiKey)
		}

		handleResponsesSuccess(c, resp, provider, upstream, envCfg, cfgManager, sessionManager, currentStartTime, &responsesReq, bodyBytes, reqLogManager, currentRequestLogID, usageManager)
		return
	}

	log.Printf("💥 所有 Responses API密钥都失败了")

	if lastFailoverError != nil {
		status := lastFailoverError.Status
		if status == 0 {
			status = 500
		}
		SaveErrorDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, status, lastFailoverError.Body)
		var errBody map[string]interface{}
		if err := json.Unmarshal(lastFailoverError.Body, &errBody); err == nil {
			c.JSON(status, errBody)
		} else {
			c.JSON(status, gin.H{"error": string(lastFailoverError.Body)})
		}
	} else {
		errMsg := "未知错误"
		if lastError != nil {
			errMsg = lastError.Error()
		}
		errJSON := fmt.Sprintf(`{"error":"所有上游 Responses API密钥都不可用","details":"%s"}`, errMsg)
		SaveErrorDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, 500, []byte(errJSON))
		c.JSON(500, gin.H{
			"error":   "所有上游 Responses API密钥都不可用",
			"details": errMsg,
		})
	}
}

// sendResponsesRequest 发送 Responses 请求
func sendResponsesRequest(req *http.Request, upstream *config.UpstreamConfig, envCfg *config.EnvConfig, isStream bool) (*http.Response, error) {
	clientManager := httpclient.GetManager()

	var client *http.Client
	if isStream {
		// 流式请求：使用无超时的流式客户端，但有响应头超时
		client = clientManager.GetStreamClient(upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	} else {
		// 非流式请求：使用环境变量配置的超时时间，同时应用渠道的响应头超时设置
		timeout := time.Duration(envCfg.RequestTimeout) * time.Millisecond
		client = clientManager.GetStandardClient(timeout, upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	}

	if upstream.InsecureSkipVerify && envCfg.EnableRequestLogs {
		log.Printf("⚠️ 正在跳过对 %s 的TLS证书验证", req.URL.String())
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

// handleResponsesSuccess 处理成功的 Responses 响应
func handleResponsesSuccess(
	c *gin.Context,
	resp *http.Response,
	provider *providers.ResponsesProvider,
	upstream *config.UpstreamConfig,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	startTime time.Time,
	originalReq *types.ResponsesRequest,
	originalRequestJSON []byte, // 原始请求 JSON，用于 Chat → Responses 转换
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
) {
	defer resp.Body.Close()

	upstreamType := upstream.ServiceType

	// 检查是否为流式响应
	isStream := originalReq != nil && originalReq.Stream

	if isStream {
		// 流式响应处理
		if envCfg.EnableResponseLogs {
			responseTime := time.Since(startTime).Milliseconds()
			log.Printf("⏱️ Responses 流式响应开始: %dms, 状态: %d", responseTime, resp.StatusCode)
		}

		// 先转发上游响应头（透明代理）
		utils.ForwardResponseHeaders(resp.Header, c.Writer)

		// 设置SSE响应头（可能覆盖上游的 Content-Type）
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")

		// 创建流式内容合成器（仅在开发模式并开启响应日志时）
		var synthesizer *utils.StreamSynthesizer
		var logBuffer bytes.Buffer
		streamLoggingEnabled := envCfg.IsDevelopment() && envCfg.EnableResponseLogs

		// Check if debug logging is enabled (need to capture response body)
		debugLogEnabled := cfgManager.GetDebugLogConfig().Enabled

		// 对于 responses 类型（包括 openai-oauth），我们需要 synthesizer 来提取 usage，不论日志是否启用
		needsSynthesizer := (upstreamType == "responses" || upstreamType == "openai-oauth") && reqLogManager != nil
		if streamLoggingEnabled || needsSynthesizer {
			synthesizer = utils.NewStreamSynthesizer(upstreamType)
		}

		// 判断是否需要转换：非 responses 类型的上游需要从 Chat Completions 转换为 Responses 格式
		// openai-oauth 使用 Responses API 格式，不需要转换
		needConvert := upstreamType != "responses" && upstreamType != "openai-oauth"
		var converterState any

		// 转发流式响应并记录内容
		c.Status(resp.StatusCode)
		flusher, _ := c.Writer.(http.Flusher)

		scanner := bufio.NewScanner(resp.Body)
		// 增加缓冲区大小：初始64KB，最大1MB
		const maxCapacity = 1024 * 1024 // 1MB
		buf := make([]byte, 0, 64*1024) // 初始64KB
		scanner.Buffer(buf, maxCapacity)

		// 用于提取 Codex usage 的变量
		var codexUsage *CodexUsage
		var responseModel string

		for scanner.Scan() {
			line := scanner.Text()

			// 记录日志（仅在开发模式下或调试日志启用时）
			if streamLoggingEnabled || debugLogEnabled {
				logBuffer.WriteString(line + "\n")
			}
			if synthesizer != nil {
				synthesizer.ProcessLine(line)
			}

			// 对于 responses/openai-oauth 类型，尝试从 response.completed 事件中提取 usage
			if (upstreamType == "responses" || upstreamType == "openai-oauth") && reqLogManager != nil {
				if usage, model := extractCodexUsageFromSSE(line); usage != nil {
					codexUsage = usage
					if model != "" {
						responseModel = model
					}
				}
			}

			if needConvert {
				// 调用转换器将 Chat Completions SSE 转换为 Responses SSE
				events := converters.ConvertOpenAIChatToResponses(
					c.Request.Context(),
					originalReq.Model,
					originalRequestJSON,
					nil,
					[]byte(line),
					&converterState,
				)
				for _, event := range events {
					_, err := c.Writer.Write([]byte(event))
					if err != nil {
						log.Printf("⚠️ 流式响应传输错误: %v", err)
						break
					}
				}
			} else {
				// 直接透传 Responses 格式的流
				_, err := c.Writer.Write([]byte(line + "\n"))
				if err != nil {
					log.Printf("⚠️ 流式响应传输错误: %v", err)
					break
				}
			}

			if flusher != nil {
				flusher.Flush()
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("⚠️ 流式响应读取错误: %v", err)
		}

		completeTime := time.Now()
		durationMs := completeTime.Sub(startTime).Milliseconds()

		if envCfg.EnableResponseLogs {
			log.Printf("✅ Responses 流式响应完成: %dms", durationMs)

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

		// 更新请求日志（Responses API）
		if reqLogManager != nil && requestLogID != "" {
			// 用于定价计算的模型名（优先响应模型，若无定价配置则回退到请求模型）
			pricingModel := responseModel
			if pricingModel == "" {
				pricingModel = originalReq.Model
			} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && originalReq.Model != "" {
				// 响应模型无定价配置，回退到请求模型
				pricingModel = originalReq.Model
			}

			record := &requestlog.RequestLog{
				Status:        requestlog.StatusCompleted,
				CompleteTime:  completeTime,
				DurationMs:    durationMs,
				Type:          upstreamType,
				ProviderName:  upstream.Name,
				ResponseModel: responseModel,
				HTTPStatus:    resp.StatusCode,
				ChannelID:     upstream.Index,
				ChannelName:   upstream.Name,
			}

			if codexUsage != nil {
				// Codex 的 input_tokens 已包含 cached_tokens，需要减去得到实际新输入
				actualInput := codexUsage.InputTokens - codexUsage.CachedTokens
				if actualInput < 0 {
					actualInput = 0
				}
				record.InputTokens = actualInput
				record.OutputTokens = codexUsage.OutputTokens
				record.CacheReadInputTokens = codexUsage.CachedTokens
				record.CacheCreationInputTokens = 0

				// 计算成本：使用 pricingModel（优先响应模型，无定价配置则回退到请求模型）
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
						actualInput,
						codexUsage.OutputTokens,
						0,
						codexUsage.CachedTokens,
						multipliers,
					)
					record.Price = breakdown.TotalCost
					record.InputCost = breakdown.InputCost
					record.OutputCost = breakdown.OutputCost
					record.CacheCreationCost = 0
					record.CacheReadCost = breakdown.CacheReadCost
				}
			}

			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}

			// Save debug log if enabled (use logBuffer for stream response body)
			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, logBuffer.Bytes())

			// Track usage for quota (streaming response completed)
			trackResponsesUsage(usageManager, upstream, originalReq.Model, record.Price)
		}
		return
	}

	// 非流式响应处理(原有逻辑)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to read response"})
		return
	}

	completeTime := time.Now()
	durationMs := completeTime.Sub(startTime).Milliseconds()

	if envCfg.EnableResponseLogs {
		log.Printf("⏱️ Responses 响应完成: %dms, 状态: %d", durationMs, resp.StatusCode)
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

	// 转换为 Responses 格式
	responsesResp, err := provider.ConvertToResponsesResponse(providerResp, upstreamType, "")
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to convert response"})
		return
	}

	// 更新请求日志（非流式 Responses API，包括 openai-oauth）
	if reqLogManager != nil && requestLogID != "" && (upstreamType == "responses" || upstreamType == "openai-oauth") {
		// 从非流式响应中提取 usage
		codexUsage, responseModel := extractCodexUsageFromJSON(bodyBytes)

		// 用于定价计算的模型名（优先响应模型，若无定价配置则回退到请求模型）
		pricingModel := responseModel
		if pricingModel == "" {
			pricingModel = originalReq.Model
		} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && originalReq.Model != "" {
			// 响应模型无定价配置，回退到请求模型
			pricingModel = originalReq.Model
		}

		record := &requestlog.RequestLog{
			Status:        requestlog.StatusCompleted,
			CompleteTime:  completeTime,
			DurationMs:    durationMs,
			Type:          upstreamType,
			ProviderName:  upstream.Name,
			ResponseModel: responseModel,
			HTTPStatus:    resp.StatusCode,
			ChannelID:     upstream.Index,
			ChannelName:   upstream.Name,
		}

		if codexUsage != nil {
			// Codex 的 input_tokens 已包含 cached_tokens，需要减去得到实际新输入
			actualInput := codexUsage.InputTokens - codexUsage.CachedTokens
			if actualInput < 0 {
				actualInput = 0
			}
			record.InputTokens = actualInput
			record.OutputTokens = codexUsage.OutputTokens
			record.CacheReadInputTokens = codexUsage.CachedTokens
			record.CacheCreationInputTokens = 0

			// 计算成本：使用 pricingModel（优先响应模型，无定价配置则回退到请求模型）
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
					actualInput,
					codexUsage.OutputTokens,
					0,
					codexUsage.CachedTokens,
					multipliers,
				)
				record.Price = breakdown.TotalCost
				record.InputCost = breakdown.InputCost
				record.OutputCost = breakdown.OutputCost
				record.CacheCreationCost = 0
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
			trackResponsesUsage(usageManager, upstream, originalReq.Model, record.Price)
		}
	}

	// 更新会话（如果需要）
	if originalReq.Store == nil || *originalReq.Store {
		// 获取会话
		sess, err := sessionManager.GetOrCreateSession(originalReq.PreviousResponseID)
		if err == nil {
			previousID := sess.LastResponseID

			// 追加用户输入
			inputItems, _ := parseInputToItems(originalReq.Input)
			for _, item := range inputItems {
				sessionManager.AppendMessage(sess.ID, item, 0)
			}

			// 追加助手响应
			for _, item := range responsesResp.Output {
				sessionManager.AppendMessage(sess.ID, item, 0)
			}

			// 仅在每次响应完成后累计一次 token（避免按输出项重复累计）
			sessionManager.AddTokens(sess.ID, responsesResp.Usage.TotalTokens)

			// 更新 last response ID
			sessionManager.UpdateLastResponseID(sess.ID, responsesResp.ID)

			// 记录映射
			sessionManager.RecordResponseMapping(responsesResp.ID, sess.ID)

			// 设置 previous_id
			if previousID != "" {
				responsesResp.PreviousID = previousID
			}
		}
	}

	// 转发上游响应头到客户端（透明代理）
	utils.ForwardResponseHeaders(resp.Header, c.Writer)

	c.JSON(200, responsesResp)
}

// CodexUsage Codex API 的 usage 结构
type CodexUsage struct {
	InputTokens  int
	OutputTokens int
	CachedTokens int
	TotalTokens  int
}

// extractCodexUsageFromSSE 从 SSE 事件行中提取 Codex usage 数据
// 返回 usage 和 model，如果不是 response.completed 事件则返回 nil
func extractCodexUsageFromSSE(line string) (*CodexUsage, string) {
	// SSE 格式: "data: {...}" 或 "data:{...}"
	var jsonStr string
	if strings.HasPrefix(line, "data: ") {
		jsonStr = strings.TrimPrefix(line, "data: ")
	} else if strings.HasPrefix(line, "data:") {
		jsonStr = strings.TrimPrefix(line, "data:")
	} else {
		return nil, ""
	}
	if jsonStr == "[DONE]" {
		return nil, ""
	}

	var event struct {
		Type     string `json:"type"`
		Response struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens        int `json:"input_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
		return nil, ""
	}

	// 只处理 response.completed 事件
	if event.Type != "response.completed" {
		return nil, ""
	}

	return &CodexUsage{
		InputTokens:  event.Response.Usage.InputTokens,
		OutputTokens: event.Response.Usage.OutputTokens,
		CachedTokens: event.Response.Usage.InputTokensDetails.CachedTokens,
		TotalTokens:  event.Response.Usage.TotalTokens,
	}, event.Response.Model
}

// extractCodexUsageFromJSON 从非流式 JSON 响应中提取 Codex usage 数据
func extractCodexUsageFromJSON(body []byte) (*CodexUsage, string) {
	var resp struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens        int `json:"input_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ""
	}

	return &CodexUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		CachedTokens: resp.Usage.InputTokensDetails.CachedTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}, resp.Model
}

// parseInputToItems 解析 input 为 ResponsesItem 数组
func parseInputToItems(input interface{}) ([]types.ResponsesItem, error) {
	switch v := input.(type) {
	case string:
		return []types.ResponsesItem{{Type: "text", Content: v}}, nil
	case []interface{}:
		items := []types.ResponsesItem{}
		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			itemType, _ := itemMap["type"].(string)
			content := itemMap["content"]
			items = append(items, types.ResponsesItem{Type: itemType, Content: content})
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported input type")
	}
}

// extractConversationID 从请求中提取对话标识（用于 Responses API 渠道亲和）
// 优先级: Conversation_id Header > Session_id Header > prompt_cache_key > metadata.user_id
func extractConversationID(c *gin.Context, bodyBytes []byte) string {
	// 1. HTTP Header: Conversation_id
	if convID := c.GetHeader("Conversation_id"); convID != "" {
		return convID
	}

	// 2. HTTP Header: Session_id
	if sessID := c.GetHeader("Session_id"); sessID != "" {
		return sessID
	}

	// 3. Request Body: prompt_cache_key 或 metadata.user_id
	var req struct {
		PromptCacheKey string `json:"prompt_cache_key"`
		Metadata       struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err == nil {
		if req.PromptCacheKey != "" {
			return req.PromptCacheKey
		}
		// 4. Fallback: metadata.user_id
		if req.Metadata.UserID != "" {
			return req.Metadata.UserID
		}
	}

	return ""
}

func isCodexResponsesModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "codex")
}
