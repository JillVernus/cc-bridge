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
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/httpclient"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// GeminiHandlerWithAPIKey handles Gemini-native format requests (passthrough mode)
// Route: POST /v1/gemini/models/{model}:{action}
func GeminiHandlerWithAPIKey(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	apiKeyManager *apikey.Manager,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if envCfg.ShouldLog("debug") {
			log.Printf("🧭 [Gemini Incoming] %s %s", c.Request.Method, c.Request.URL.String())
		}

		// Authentication check (if not already done by upstream middleware)
		if _, exists := c.Get(middleware.ContextKeyAPIKeyID); !exists {
			middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager)(c)
			if c.IsAborted() {
				return
			}
		}

		// Check endpoint permission
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckEndpointPermission("gemini") {
					c.JSON(403, gin.H{
						"error": gin.H{
							"code":    403,
							"message": "Endpoint /v1/gemini not allowed for this API key",
							"status":  "PERMISSION_DENIED",
						},
					})
					return
				}
			}
		}

		startTime := time.Now()

		// Read original request body
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
					"error": gin.H{
						"code":    413,
						"message": fmt.Sprintf("Request body too large. Maximum allowed size is %d MB", maxBodyMB),
						"status":  "INVALID_ARGUMENT",
					},
				})
				return
			}
			c.JSON(400, gin.H{
				"error": gin.H{
					"code":    400,
					"message": "Failed to read request body",
					"status":  "INVALID_ARGUMENT",
				},
			})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Store request data for debug logging
		StoreDebugRequestData(c, bodyBytes)

		// Extract model from URL path
		actionParam := c.Param("action")
		actionParam = strings.TrimPrefix(actionParam, "/")
		model := extractGeminiModel(actionParam)

		// Check model permission
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckModelPermission(model) {
					c.JSON(403, gin.H{
						"error": gin.H{
							"code":    403,
							"message": fmt.Sprintf("Model %s not allowed for this API key", model),
							"status":  "PERMISSION_DENIED",
						},
					})
					return
				}
			}
		}

		// Determine if streaming (check query param alt=sse or action contains stream)
		isStreaming := c.Query("alt") == "sse" || strings.Contains(actionParam, "stream")

		// Extract API Key ID for request log
		var apiKeyID *int64
		if id, exists := c.Get(middleware.ContextKeyAPIKeyID); exists {
			if idVal, ok := id.(int64); ok {
				apiKeyID = &idVal
			}
		}

		// Create pending request log entry
		var requestLogID string
		if reqLogManager != nil {
			pendingLog := &requestlog.RequestLog{
				Status:      requestlog.StatusPending,
				InitialTime: startTime,
				Model:       model,
				Stream:      isStreaming,
				Endpoint:    "/v1/gemini",
				APIKeyID:    apiKeyID,
			}
			if err := reqLogManager.Add(pendingLog); err != nil {
				log.Printf("⚠️ 创建 pending 请求日志失败: %v", err)
			} else {
				requestLogID = pendingLog.ID
			}
		}

		// Get allowed channels from API key permissions (for Gemini channels)
		var allowedChannels []string
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				allowedChannels = validatedKey.GetAllowedChannelsByType("gemini")
			}
		}

		// Check if multi-channel mode for Gemini
		isMultiChannel := channelScheduler.IsGeminiMultiChannelMode()

		if isMultiChannel {
			handleMultiChannelGemini(c, envCfg, cfgManager, channelScheduler, bodyBytes, model, isStreaming, startTime, reqLogManager, requestLogID, usageManager, allowedChannels, failoverTracker, channelRateLimiter, apiKeyID)
		} else {
			handleSingleChannelGemini(c, envCfg, cfgManager, channelScheduler, bodyBytes, model, isStreaming, startTime, reqLogManager, requestLogID, usageManager, allowedChannels, failoverTracker, channelRateLimiter, apiKeyID)
		}
	}
}

// extractGeminiModel extracts model name from path like "gemini-2.0-flash:generateContent"
func extractGeminiModel(path string) string {
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		return path
	}
	return path[:idx]
}

// trackGeminiUsage tracks usage for Gemini API channels based on quota type
func trackGeminiUsage(usageManager *quota.UsageManager, upstream *config.UpstreamConfig, model string, cost float64) {
	if usageManager == nil || upstream == nil || upstream.QuotaType == "" {
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

	if err := usageManager.IncrementGeminiUsage(upstream.Index, amount); err != nil {
		log.Printf("⚠️ 配额使用量追踪失败 (Gemini, channelIndex=%d): %v", upstream.Index, err)
	}
}

// handleMultiChannelGemini handles multi-channel Gemini requests with failover
func handleMultiChannelGemini(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	model string,
	isStreaming bool,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannels []string,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
	apiKeyID *int64,
) {
	failedChannels := make(map[int]bool)
	var lastError error
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}

	maxChannelAttempts := channelScheduler.GetActiveGeminiChannelCount()
	shouldLogInfo := envCfg.ShouldLog("info")

	for channelAttempt := 0; channelAttempt < maxChannelAttempts; channelAttempt++ {
		// Select channel from GeminiUpstream
		selection, err := channelScheduler.SelectGeminiChannel(c.Request.Context(), failedChannels, allowedChannels)
		if err != nil {
			lastError = err
			break
		}

		upstream := selection.Upstream
		channelIndex := selection.ChannelIndex

		if shouldLogInfo {
			log.Printf("🎯 [Multi-Channel/Gemini] Selected channel: [%d] %s (reason: %s, attempt %d/%d)",
				channelIndex, upstream.Name, selection.Reason, channelAttempt+1, maxChannelAttempts)
		}

		// Check per-channel rate limit
		if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
			result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "gemini")
			if !result.Allowed {
				log.Printf("🚫 [Channel Rate Limit/Gemini] Channel %d (%s): %v",
					channelIndex, upstream.Name, result.Error)
				failedChannels[channelIndex] = true
				lastError = result.Error
				continue
			}
		}

		success, failoverErr := tryGeminiChannel(c, envCfg, cfgManager, upstream, bodyBytes, model, isStreaming, &startTime, reqLogManager, &requestLogID, usageManager, failoverTracker, apiKeyID)

		// Client disconnected
		if c.Request.Context().Err() != nil {
			return
		}

		if success {
			// ActionNone/no-failover branches return handled=true with an error payload.
			// Treat any such handled payload as failure so recent success stats stay accurate.
			if failoverErr != nil {
				channelScheduler.RecordGeminiFailureWithStatus(channelIndex, failoverErr.Status)
			} else {
				channelScheduler.RecordGeminiSuccessWithStatus(channelIndex, 200)
			}
			return
		}

		// Channel failed
		failureStatus := 0
		if failoverErr != nil {
			failureStatus = failoverErr.Status
		}
		channelScheduler.RecordGeminiFailureWithStatus(channelIndex, failureStatus)
		failedChannels[channelIndex] = true

		// Check if there are more channels to try
		hasMoreChannels := channelAttempt < maxChannelAttempts-1 && len(failedChannels) < maxChannelAttempts

		if hasMoreChannels {
			// Log failover attempt
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

				failoverRecord := &requestlog.RequestLog{
					Status:        requestlog.StatusFailover,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(startTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					Model:         model,
					ChannelID:     channelIndex,
					ChannelName:   upstream.Name,
					HTTPStatus:    httpStatus,
					Error:         fmt.Sprintf("failover to next channel (%d/%d)", channelAttempt+1, maxChannelAttempts),
					UpstreamError: upstreamErr,
					FailoverInfo:  failoverInfo,
				}
				if err := reqLogManager.Update(requestLogID, failoverRecord); err != nil {
					log.Printf("⚠️ Failed to update failover log: %v", err)
				}

				// Create new pending log for next attempt
				newPendingLog := &requestlog.RequestLog{
					Status:      requestlog.StatusPending,
					InitialTime: time.Now(),
					Model:       model,
					Stream:      isStreaming,
					Endpoint:    "/v1/gemini",
					APIKeyID:    apiKeyID,
				}
				if err := reqLogManager.Add(newPendingLog); err != nil {
					log.Printf("⚠️ Failed to create failover pending log: %v", err)
				} else {
					requestLogID = newPendingLog.ID
					startTime = newPendingLog.InitialTime
				}
			}

			log.Printf("⚠️ [Multi-Channel/Gemini] Channel [%d] %s failed, trying next channel", channelIndex, upstream.Name)
		}

		if failoverErr != nil {
			lastFailoverError = failoverErr
			lastError = fmt.Errorf("channel [%d] %s failed", channelIndex, upstream.Name)
		}
	}

	// All channels failed
	log.Printf("💥 [Multi-Channel/Gemini] All channels failed")

	// Update request log with final error status
	if reqLogManager != nil && requestLogID != "" {
		httpStatus := 503
		errMsg := "all gemini channels unavailable"
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
			DurationMs:    time.Since(startTime).Milliseconds(),
			Model:         model,
			HTTPStatus:    httpStatus,
			Error:         errMsg,
			UpstreamError: upstreamErr,
			FailoverInfo:  failoverInfo,
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
			c.JSON(status, gin.H{"error": gin.H{"message": string(lastFailoverError.Body)}})
		}
	} else {
		// Check if this is a permission error (no allowed channels)
		if lastError != nil && errors.Is(lastError, scheduler.ErrNoAllowedChannels) {
			errMsg := lastError.Error()
			c.JSON(403, gin.H{
				"error": gin.H{
					"code":    403,
					"message": errMsg,
					"status":  "PERMISSION_DENIED",
				},
			})
		} else {
			errMsg := "all gemini channels unavailable"
			if lastError != nil {
				errMsg = lastError.Error()
			}
			c.JSON(503, gin.H{
				"error": gin.H{
					"code":    503,
					"message": errMsg,
					"status":  "UNAVAILABLE",
				},
			})
		}
	}
}

// handleSingleChannelGemini handles single-channel Gemini requests
func handleSingleChannelGemini(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	model string,
	isStreaming bool,
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	allowedChannels []string,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
	apiKeyID *int64,
) {
	// Get Gemini upstream from GeminiUpstream channel list
	upstream, err := cfgManager.GetCurrentGeminiUpstream()
	if err != nil {
		c.JSON(503, gin.H{
			"error": gin.H{
				"code":    503,
				"message": "No Gemini channel configured. Please add a Gemini channel in the Gemini tab.",
				"status":  "UNAVAILABLE",
			},
		})
		return
	}

	// Check if this channel is allowed by API key permissions
	if len(allowedChannels) > 0 {
		allowed := false
		for _, id := range allowedChannels {
			if id == upstream.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(403, gin.H{
				"error": gin.H{
					"code":    403,
					"message": fmt.Sprintf("Channel %s not allowed for this API key", upstream.Name),
					"status":  "PERMISSION_DENIED",
				},
			})
			return
		}
	}

	// Check per-channel rate limit
	if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
		result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "gemini")
		if !result.Allowed {
			log.Printf("🚫 [Channel Rate Limit/Gemini] Channel %d (%s): %v", upstream.Index, upstream.Name, result.Error)
			c.JSON(429, gin.H{
				"error": gin.H{
					"code":    429,
					"message": fmt.Sprintf("Channel rate limit exceeded (%d RPM)", upstream.RateLimitRpm),
					"status":  "RESOURCE_EXHAUSTED",
				},
			})
			return
		}
	}

	if len(upstream.APIKeys) == 0 {
		c.JSON(503, gin.H{
			"error": gin.H{
				"code":    503,
				"message": fmt.Sprintf("Gemini channel \"%s\" has no API keys configured", upstream.Name),
				"status":  "UNAVAILABLE",
			},
		})
		return
	}

	success, failoverErr := tryGeminiChannel(c, envCfg, cfgManager, upstream, bodyBytes, model, isStreaming, &startTime, reqLogManager, &requestLogID, usageManager, failoverTracker, apiKeyID)

	// Client disconnected
	if c.Request.Context().Err() != nil {
		return
	}

	if !success {
		if failoverErr != nil {
			status := failoverErr.Status
			if status == 0 {
				status = 500
			}
			// Update request log for single-channel final failure
			if reqLogManager != nil && requestLogID != "" {
				completeTime := time.Now()
				record := &requestlog.RequestLog{
					Status:        requestlog.StatusError,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(startTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					Model:         model,
					ChannelID:     upstream.Index,
					ChannelName:   upstream.Name,
					HTTPStatus:    status,
					Error:         fmt.Sprintf("upstream returned status %d", status),
					UpstreamError: string(failoverErr.Body),
					FailoverInfo:  failoverErr.FailoverInfo,
				}
				_ = reqLogManager.Update(requestLogID, record)
			}
			SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, status, failoverErr.Body)
			var errBody map[string]interface{}
			if err := json.Unmarshal(failoverErr.Body, &errBody); err == nil {
				c.JSON(status, errBody)
			} else {
				c.JSON(status, gin.H{"error": gin.H{"message": string(failoverErr.Body)}})
			}
		} else {
			// No failover error but still failed (e.g., no API keys available)
			if reqLogManager != nil && requestLogID != "" {
				record := &requestlog.RequestLog{
					Status:       requestlog.StatusError,
					CompleteTime: time.Now(),
					DurationMs:   time.Since(startTime).Milliseconds(),
					Model:        model,
					HTTPStatus:   503,
					Error:        "all API keys exhausted or unavailable",
				}
				_ = reqLogManager.Update(requestLogID, record)
			}
			c.JSON(503, gin.H{
				"error": gin.H{
					"code":    503,
					"message": "Gemini channel unavailable: all API keys exhausted",
					"status":  "UNAVAILABLE",
				},
			})
		}
	}
}

// tryGeminiChannel tries a single Gemini channel with all its API keys
func tryGeminiChannel(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	upstream *config.UpstreamConfig,
	bodyBytes []byte,
	model string,
	isStreaming bool,
	startTime *time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID *string,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	apiKeyID *int64,
) (bool, *struct {
	Status       int
	Body         []byte
	FailoverInfo string
}) {
	if len(upstream.APIKeys) == 0 {
		return false, nil
	}

	provider := &providers.GeminiPassthroughProvider{}
	failedKeys := make(map[string]bool)
	maxRetries := len(upstream.APIKeys)
	var lastFailoverError *struct {
		Status       int
		Body         []byte
		FailoverInfo string
	}

	deprioritizeCandidates := make(map[string]bool)
	var pinnedKey string      // For retry-same-key scenarios
	var retryWaitPending bool // Allows loop to continue for one retry after wait
	currentStartTime := *startTime
	currentRequestLogID := *requestLogID

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
			apiKey, err = cfgManager.GetNextGeminiAPIKey(upstream, failedKeys)
			if err != nil {
				break
			}
			attempt++ // Only increment when trying a new key
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🔑 [Gemini] 使用API密钥: %s (尝试 %d/%d)", maskAPIKey(apiKey), attempt, maxRetries)
		}

		providerReq, _, err := provider.ConvertToProviderRequest(c, upstream, apiKey)
		if err != nil {
			failedKeys[apiKey] = true
			continue
		}

		resp, err := sendGeminiRequest(providerReq, upstream, envCfg, isStreaming)
		if err != nil {
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			log.Printf("⚠️ [Gemini] API密钥失败: %v", err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			respBodyBytes = utils.DecompressGzipIfNeeded(resp, respBodyBytes)

			// Handle 429 errors with smart subtype detection (quota channels)
			if resp.StatusCode == 429 && failoverTracker != nil {
				// Unified failover logic: always use admin failover settings
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key (tracker handles attempt counting)
					log.Printf("⏳ [Gemini] 429 %s: 等待 %v 后重试同一密钥 (max: %d)", decision.Reason, decision.Wait, decision.MaxAttempts)

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

						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       model,
							Stream:      isStreaming,
							Endpoint:    "/v1/gemini",
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							*requestLogID = newPendingLog.ID
							currentStartTime = newPendingLog.InitialTime
							*startTime = newPendingLog.InitialTime
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
						pinnedKey = apiKey
						retryWaitPending = true
						// Reset startTime after wait completes for the next attempt's duration
						currentStartTime = time.Now()
						continue
					case <-c.Request.Context().Done():
						// Client disconnected
						return false, nil
					}

				case config.ActionFailoverKey:
					// Immediate failover to next key
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ [Gemini] 429 %s: 立即切换到下一个密钥", decision.Reason)

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
							log.Printf("⏸️ [Gemini] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Gemini] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						if err := reqLogManager.SuspendChannel(upstream.Index, "gemini", suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ Failed to suspend channel [%d] (gemini): %v", upstream.Index, err)
						}
					}
					log.Printf("⏸️ [Gemini] 429 %s: 渠道暂停，切换到下一个渠道", decision.Reason)

					// Return false to trigger channel failover (multi-channel) or final error (single-channel)
					return false, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "next channel"),
					}

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
					}
				}
			}

			// Non-429 errors: use unified failover logic with full decision switch
			if failoverTracker != nil {
				failoverConfig := cfgManager.GetFailoverConfig()
				decision := failoverTracker.DecideAction(upstream.Index, apiKey, resp.StatusCode, respBodyBytes, &failoverConfig)

				switch decision.Action {
				case config.ActionRetrySameKey:
					// Wait and retry with same key
					log.Printf("⏳ [Gemini] %d %s: 等待 %v 后重试同一密钥 (max: %d)", resp.StatusCode, decision.Reason, decision.Wait, decision.MaxAttempts)

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
							Error:         fmt.Sprintf("%d %s - retrying after %v", resp.StatusCode, decision.Reason, decision.Wait),
							UpstreamError: string(respBodyBytes),
							FailoverInfo:  failoverInfo,
						}
						if err := reqLogManager.Update(currentRequestLogID, retryWaitRecord); err != nil {
							log.Printf("⚠️ [Gemini] Failed to update retry_wait log: %v", err)
						}

						// Save debug log for this error response
						SaveDebugLog(c, cfgManager, reqLogManager, currentRequestLogID, resp.StatusCode, resp.Header, respBodyBytes)

						// Create new pending log for the retry attempt
						newPendingLog := &requestlog.RequestLog{
							Status:      requestlog.StatusPending,
							InitialTime: time.Now(),
							Model:       model,
							Stream:      isStreaming,
							Endpoint:    c.Request.URL.Path,
							APIKeyID:    apiKeyID,
						}
						if err := reqLogManager.Add(newPendingLog); err != nil {
							log.Printf("⚠️ [Gemini] Failed to create retry pending log: %v", err)
						} else {
							currentRequestLogID = newPendingLog.ID
							*requestLogID = newPendingLog.ID
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
						return false, nil
					}

				case config.ActionFailoverKey:
					// Failover to next key
					failedKeys[apiKey] = true
					if decision.MarkKeyFailed {
						cfgManager.MarkKeyAsFailed(apiKey)
					}
					log.Printf("⚠️ [Gemini] %d %s: 切换到下一个密钥", resp.StatusCode, decision.Reason)

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
							log.Printf("⏸️ [Gemini] Channel [%d] %s: using QuotaResetAt %s for suspension",
								upstream.Index, upstream.Name, suspendedUntil.Format(time.RFC3339))
						} else {
							log.Printf("⏸️ [Gemini] Channel [%d] %s: using default 5min suspension (QuotaResetAt: %v)",
								upstream.Index, upstream.Name, upstream.QuotaResetAt)
						}
						channelType := "gemini"
						if err := reqLogManager.SuspendChannel(upstream.Index, channelType, suspendedUntil, decision.Reason); err != nil {
							log.Printf("⚠️ [Gemini] Failed to suspend channel [%d] (%s): %v", upstream.Index, channelType, err)
						}
					}
					log.Printf("⏸️ [Gemini] %d %s: 渠道暂停，切换到下一个渠道", resp.StatusCode, decision.Reason)

					// Return false to trigger channel failover (outer loop will try next channel)
					return false, &struct {
						Status       int
						Body         []byte
						FailoverInfo string
					}{
						Status:       resp.StatusCode,
						Body:         respBodyBytes,
						FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, decision.Reason, requestlog.FailoverActionSuspended, "next channel"),
					}

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
					}
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
				return true, &struct {
					Status       int
					Body         []byte
					FailoverInfo string
				}{
					Status:       resp.StatusCode,
					Body:         respBodyBytes,
					FailoverInfo: requestlog.FormatFailoverInfo(resp.StatusCode, "", requestlog.FailoverActionReturnErr, ""),
				}
			}
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

		// Success - handle response
		handleGeminiSuccess(c, resp, upstream, envCfg, cfgManager, isStreaming, currentStartTime, model, reqLogManager, currentRequestLogID, usageManager)
		return true, nil
	}

	return false, lastFailoverError
}

// sendGeminiRequest sends the request to Gemini upstream
func sendGeminiRequest(req *http.Request, upstream *config.UpstreamConfig, envCfg *config.EnvConfig, isStream bool) (*http.Response, error) {
	clientManager := httpclient.GetManager()

	var client *http.Client
	if isStream {
		client = clientManager.GetStreamClient(upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	} else {
		timeout := time.Duration(envCfg.RequestTimeout) * time.Millisecond
		client = clientManager.GetStandardClient(timeout, upstream.InsecureSkipVerify, upstream.GetResponseHeaderTimeout())
	}

	if upstream.InsecureSkipVerify && envCfg.EnableRequestLogs {
		log.Printf("⚠️ 正在跳过对 %s 的TLS证书验证", req.URL.String())
	}

	if envCfg.EnableRequestLogs {
		log.Printf("🌐 [Gemini] 实际请求URL: %s", req.URL.String())
	}

	return client.Do(req)
}

// handleGeminiSuccess handles successful Gemini response
func handleGeminiSuccess(
	c *gin.Context,
	resp *http.Response,
	upstream *config.UpstreamConfig,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	isStreaming bool,
	startTime time.Time,
	model string,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
) {
	defer resp.Body.Close()

	if isStreaming {
		// Streaming response - passthrough upstream bytes directly
		if envCfg.EnableResponseLogs {
			responseTime := time.Since(startTime).Milliseconds()
			log.Printf("⏱️ Gemini 流式响应开始: %dms, 状态: %d", responseTime, resp.StatusCode)
		}

		// Forward upstream response headers
		utils.ForwardResponseHeaders(resp.Header, c.Writer)

		// Streaming-friendly headers (do not override upstream Content-Type)
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")

		c.Status(resp.StatusCode)
		c.Writer.WriteHeaderNow()

		flusher, _ := c.Writer.(http.Flusher)
		fw := &flushWriter{w: c.Writer, flusher: flusher}

		debugCfg := cfgManager.GetDebugLogConfig()
		debugLogEnabled := debugCfg.Enabled

		// Capture only the tail for usage extraction; optionally capture a bounded prefix for debug logs.
		tail := &tailBuffer{max: 512 * 1024} // 512KB tail is enough to catch usage metadata near the end

		var debugCapture *cappedBuffer
		if debugLogEnabled {
			// Keep capture bounded even if debug config has no max, to avoid unbounded memory for long streams.
			maxCapture := debugCfg.GetMaxBodySize()
			if maxCapture <= 0 {
				maxCapture = 4 * 1024 * 1024 // 4MB safety cap
			}
			debugCapture = &cappedBuffer{max: maxCapture}
		}

		var streamReader io.Reader = resp.Body
		if debugCapture != nil {
			streamReader = io.TeeReader(resp.Body, io.MultiWriter(tail, debugCapture))
		} else {
			streamReader = io.TeeReader(resp.Body, tail)
		}

		copyBuf := make([]byte, 32*1024)
		if _, err := io.CopyBuffer(fw, streamReader, copyBuf); err != nil {
			log.Printf("⚠️ Gemini 流式响应传输错误: %v", err)
		}

		if flusher != nil {
			flusher.Flush()
		}

		usage := extractGeminiUsageFromStreamBytes(tail.Bytes())

		completeTime := time.Now()
		durationMs := completeTime.Sub(startTime).Milliseconds()

		if envCfg.EnableResponseLogs {
			log.Printf("✅ Gemini 流式响应完成: %dms", durationMs)
		}

		// Update request log
		if reqLogManager != nil && requestLogID != "" {
			record := &requestlog.RequestLog{
				Status:       requestlog.StatusCompleted,
				CompleteTime: completeTime,
				DurationMs:   durationMs,
				Type:         upstream.ServiceType,
				ProviderName: upstream.Name,
				HTTPStatus:   resp.StatusCode,
				ChannelID:    upstream.Index,
				ChannelName:  upstream.Name,
			}

			if usage != nil {
				record.InputTokens = usage.PromptTokenCount
				outputTokens := usage.CandidatesTokenCount + usage.ThoughtsTokenCount
				if outputTokens == 0 && usage.TotalTokenCount > 0 && usage.PromptTokenCount > 0 {
					outputTokens = usage.TotalTokenCount - usage.PromptTokenCount
					if outputTokens < 0 {
						outputTokens = 0
					}
				}
				record.OutputTokens = outputTokens
				if usage.ModelVersion != "" {
					record.ResponseModel = usage.ModelVersion
				}
			}

			// Pricing (prefer response model if priced, otherwise fall back to request model)
			pricingModel := record.ResponseModel
			if pricingModel == "" {
				pricingModel = model
			} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && model != "" {
				pricingModel = model
			}

			if pm := pricing.GetManager(); pm != nil && pricingModel != "" {
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
					record.InputTokens,
					record.OutputTokens,
					0,
					0,
					multipliers,
				)
				record.Price = breakdown.TotalCost
				record.InputCost = breakdown.InputCost
				record.OutputCost = breakdown.OutputCost
				record.CacheCreationCost = breakdown.CacheCreationCost
				record.CacheReadCost = breakdown.CacheReadCost
			}

			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}

			trackGeminiUsage(usageManager, upstream, model, record.Price)

			var debugBody []byte
			if debugCapture != nil {
				debugBody = debugCapture.Bytes()
			}
			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, debugBody)
		}
		return
	}

	// Non-streaming response
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		// Update request log with error status
		if reqLogManager != nil && requestLogID != "" {
			record := &requestlog.RequestLog{
				Status:       requestlog.StatusError,
				CompleteTime: time.Now(),
				DurationMs:   time.Since(startTime).Milliseconds(),
				Type:         upstream.ServiceType,
				ProviderName: upstream.Name,
				HTTPStatus:   500,
				ChannelID:    upstream.Index,
				ChannelName:  upstream.Name,
				Error:        fmt.Sprintf("failed to read response body: %v", err),
			}
			_ = reqLogManager.Update(requestLogID, record)
		}
		c.JSON(500, gin.H{"error": gin.H{"message": "Failed to read response"}})
		return
	}

	completeTime := time.Now()
	durationMs := completeTime.Sub(startTime).Milliseconds()

	if envCfg.EnableResponseLogs {
		log.Printf("⏱️ Gemini 响应完成: %dms, 状态: %d", durationMs, resp.StatusCode)
	}

	// Extract usage from response
	usage := extractGeminiUsageFromJSON(bodyBytes)

	// Update request log
	if reqLogManager != nil && requestLogID != "" {
		record := &requestlog.RequestLog{
			Status:       requestlog.StatusCompleted,
			CompleteTime: completeTime,
			DurationMs:   durationMs,
			Type:         upstream.ServiceType,
			ProviderName: upstream.Name,
			HTTPStatus:   resp.StatusCode,
			ChannelID:    upstream.Index,
			ChannelName:  upstream.Name,
		}

		if usage != nil {
			record.InputTokens = usage.PromptTokenCount
			outputTokens := usage.CandidatesTokenCount + usage.ThoughtsTokenCount
			if outputTokens == 0 && usage.TotalTokenCount > 0 && usage.PromptTokenCount > 0 {
				outputTokens = usage.TotalTokenCount - usage.PromptTokenCount
				if outputTokens < 0 {
					outputTokens = 0
				}
			}
			record.OutputTokens = outputTokens
			if usage.ModelVersion != "" {
				record.ResponseModel = usage.ModelVersion
			}
		}

		// Pricing (prefer response model if priced, otherwise fall back to request model)
		pricingModel := record.ResponseModel
		if pricingModel == "" {
			pricingModel = model
		} else if pm := pricing.GetManager(); pm != nil && !pm.HasPricing(pricingModel) && model != "" {
			pricingModel = model
		}

		if pm := pricing.GetManager(); pm != nil && pricingModel != "" {
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
				record.InputTokens,
				record.OutputTokens,
				0,
				0,
				multipliers,
			)
			record.Price = breakdown.TotalCost
			record.InputCost = breakdown.InputCost
			record.OutputCost = breakdown.OutputCost
			record.CacheCreationCost = breakdown.CacheCreationCost
			record.CacheReadCost = breakdown.CacheReadCost
		}

		if err := reqLogManager.Update(requestLogID, record); err != nil {
			log.Printf("⚠️ 请求日志更新失败: %v", err)
		}

		trackGeminiUsage(usageManager, upstream, model, record.Price)

		SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, bodyBytes)
	}

	// Forward upstream response headers
	utils.ForwardResponseHeaders(resp.Header, c.Writer)

	// Return response directly (passthrough)
	c.Data(resp.StatusCode, "application/json", bodyBytes)
}

// GeminiUsage represents Gemini API usage metadata
type GeminiUsage struct {
	PromptTokenCount     int
	CandidatesTokenCount int
	ThoughtsTokenCount   int
	TotalTokenCount      int
	ModelVersion         string
}

// extractGeminiUsageFromSSE extracts usage from Gemini SSE event
func extractGeminiUsageFromSSE(line string) *GeminiUsage {
	// SSE format: "data: {...}"
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}

	jsonStr := strings.TrimPrefix(line, "data: ")
	if jsonStr == "[DONE]" {
		return nil
	}

	// Parse usage metadata from response
	// Gemini may emit either usageMetadata or cpaUsageMetadata in SSE segments.
	modelVersion := gjson.Get(jsonStr, "modelVersion").String()
	promptTokens := gjson.Get(jsonStr, "usageMetadata.promptTokenCount").Int()
	candidatesTokens := gjson.Get(jsonStr, "usageMetadata.candidatesTokenCount").Int()
	thoughtsTokens := gjson.Get(jsonStr, "usageMetadata.thoughtsTokenCount").Int()
	totalTokens := gjson.Get(jsonStr, "usageMetadata.totalTokenCount").Int()

	if promptTokens == 0 {
		promptTokens = gjson.Get(jsonStr, "cpaUsageMetadata.promptTokenCount").Int()
	}
	if candidatesTokens == 0 {
		candidatesTokens = gjson.Get(jsonStr, "cpaUsageMetadata.candidatesTokenCount").Int()
	}
	if thoughtsTokens == 0 {
		thoughtsTokens = gjson.Get(jsonStr, "cpaUsageMetadata.thoughtsTokenCount").Int()
	}
	if totalTokens == 0 {
		totalTokens = gjson.Get(jsonStr, "cpaUsageMetadata.totalTokenCount").Int()
	}

	if promptTokens > 0 || candidatesTokens > 0 || thoughtsTokens > 0 || modelVersion != "" {
		return &GeminiUsage{
			PromptTokenCount:     int(promptTokens),
			CandidatesTokenCount: int(candidatesTokens),
			ThoughtsTokenCount:   int(thoughtsTokens),
			TotalTokenCount:      int(totalTokens),
			ModelVersion:         modelVersion,
		}
	}

	return nil
}

// extractGeminiUsageFromJSON extracts usage from non-streaming Gemini response
func extractGeminiUsageFromJSON(body []byte) *GeminiUsage {
	modelVersion := gjson.GetBytes(body, "modelVersion").String()
	promptTokens := gjson.GetBytes(body, "usageMetadata.promptTokenCount").Int()
	candidatesTokens := gjson.GetBytes(body, "usageMetadata.candidatesTokenCount").Int()
	thoughtsTokens := gjson.GetBytes(body, "usageMetadata.thoughtsTokenCount").Int()
	totalTokens := gjson.GetBytes(body, "usageMetadata.totalTokenCount").Int()

	if promptTokens == 0 {
		promptTokens = gjson.GetBytes(body, "cpaUsageMetadata.promptTokenCount").Int()
	}
	if candidatesTokens == 0 {
		candidatesTokens = gjson.GetBytes(body, "cpaUsageMetadata.candidatesTokenCount").Int()
	}
	if thoughtsTokens == 0 {
		thoughtsTokens = gjson.GetBytes(body, "cpaUsageMetadata.thoughtsTokenCount").Int()
	}
	if totalTokens == 0 {
		totalTokens = gjson.GetBytes(body, "cpaUsageMetadata.totalTokenCount").Int()
	}

	if promptTokens > 0 || candidatesTokens > 0 || thoughtsTokens > 0 || modelVersion != "" {
		return &GeminiUsage{
			PromptTokenCount:     int(promptTokens),
			CandidatesTokenCount: int(candidatesTokens),
			ThoughtsTokenCount:   int(thoughtsTokens),
			TotalTokenCount:      int(totalTokens),
			ModelVersion:         modelVersion,
		}
	}

	return nil
}

type flushWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}

type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if t.max <= 0 {
		return len(p), nil
	}
	if len(p) >= t.max {
		t.buf = append([]byte(nil), p[len(p)-t.max:]...)
		return len(p), nil
	}
	need := len(t.buf) + len(p) - t.max
	if need > 0 {
		t.buf = t.buf[need:]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte {
	return t.buf
}

type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.max <= 0 {
		return len(p), nil
	}
	if c.buf.Len() >= c.max {
		return len(p), nil
	}
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = c.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte {
	return c.buf.Bytes()
}

func extractGeminiUsageFromStreamBytes(streamTail []byte) *GeminiUsage {
	if len(streamTail) == 0 {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(streamTail))
	// Tail buffer is bounded; allow scanning large-ish lines safely within that bound.
	scanner.Buffer(make([]byte, 0, 64*1024), len(streamTail))

	var usage *GeminiUsage
	for scanner.Scan() {
		line := scanner.Text()
		if u := extractGeminiUsageFromSSE(line); u != nil {
			usage = u
		}
	}
	return usage
}
