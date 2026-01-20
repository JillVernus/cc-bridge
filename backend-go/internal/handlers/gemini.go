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
		var allowedChannels []int
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				// Note: May need separate Gemini channel allowlist in future
				allowedChannels = validatedKey.GetAllowedChannels(false)
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
	allowedChannels []int,
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

		success, failoverErr := tryGeminiChannel(c, envCfg, cfgManager, upstream, bodyBytes, model, isStreaming, startTime, reqLogManager, requestLogID, usageManager)

		if success {
			channelScheduler.RecordGeminiSuccess(channelIndex)
			return
		}

		// Channel failed
		channelScheduler.RecordGeminiFailure(channelIndex)
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
					Status:       requestlog.StatusFailover,
					CompleteTime: completeTime,
					DurationMs:   completeTime.Sub(startTime).Milliseconds(),
					Type:         upstream.ServiceType,
					ProviderName: upstream.Name,
					Model:        model,
					ChannelID:    channelIndex,
					ChannelName:  upstream.Name,
					HTTPStatus:   httpStatus,
					Error:        fmt.Sprintf("failover to next channel (%d/%d)", channelAttempt+1, maxChannelAttempts),
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
	allowedChannels []int,
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
		for _, idx := range allowedChannels {
			if idx == upstream.Index {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(403, gin.H{
				"error": gin.H{
					"code":    403,
					"message": fmt.Sprintf("Channel %s (index %d) not allowed for this API key", upstream.Name, upstream.Index),
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

	success, failoverErr := tryGeminiChannel(c, envCfg, cfgManager, upstream, bodyBytes, model, isStreaming, startTime, reqLogManager, requestLogID, usageManager)

	if !success {
		if failoverErr != nil {
			status := failoverErr.Status
			if status == 0 {
				status = 500
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
	startTime time.Time,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
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

	for attempt := 0; attempt < maxRetries; attempt++ {
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		apiKey, err := cfgManager.GetNextGeminiAPIKey(upstream, failedKeys)
		if err != nil {
			break
		}

		if envCfg.ShouldLog("info") {
			log.Printf("🔑 [Gemini] 使用API密钥: %s (尝试 %d/%d)", maskAPIKey(apiKey), attempt+1, maxRetries)
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

			// Check if we should failover
			shouldFailover := resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode >= 500

			if shouldFailover {
				failedKeys[apiKey] = true
				cfgManager.MarkKeyAsFailed(apiKey)
				log.Printf("⚠️ [Gemini] API密钥失败 (状态: %d)，尝试下一个密钥", resp.StatusCode)

				lastFailoverError = &struct {
					Status       int
					Body         []byte
					FailoverInfo string
				}{
					Status: resp.StatusCode,
					Body:   respBodyBytes,
				}
				continue
			}

			// Non-failover error - update request log and return
			if reqLogManager != nil && requestLogID != "" {
				completeTime := time.Now()
				record := &requestlog.RequestLog{
					Status:        requestlog.StatusError,
					CompleteTime:  completeTime,
					DurationMs:    completeTime.Sub(startTime).Milliseconds(),
					Type:          upstream.ServiceType,
					ProviderName:  upstream.Name,
					HTTPStatus:    resp.StatusCode,
					ChannelID:     upstream.Index,
					ChannelName:   upstream.Name,
					Error:         fmt.Sprintf("upstream returned status %d", resp.StatusCode),
					UpstreamError: string(respBodyBytes),
				}
				_ = reqLogManager.Update(requestLogID, record)
			}
			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, respBodyBytes)
			c.Data(resp.StatusCode, "application/json", respBodyBytes)
			return true, nil
		}

		// Success - handle response
		handleGeminiSuccess(c, resp, upstream, envCfg, cfgManager, isStreaming, startTime, model, reqLogManager, requestLogID, usageManager)
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
		// Streaming response - forward SSE directly
		if envCfg.EnableResponseLogs {
			responseTime := time.Since(startTime).Milliseconds()
			log.Printf("⏱️ Gemini 流式响应开始: %dms, 状态: %d", responseTime, resp.StatusCode)
		}

		// Forward upstream response headers
		utils.ForwardResponseHeaders(resp.Header, c.Writer)

		// Set SSE response headers
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")

		c.Status(resp.StatusCode)
		flusher, _ := c.Writer.(http.Flusher)

		// Use io.Copy for efficient streaming (avoid bufio.Scanner 64KB limit)
		var logBuffer bytes.Buffer
		debugLogEnabled := cfgManager.GetDebugLogConfig().Enabled

		scanner := bufio.NewScanner(resp.Body)
		const maxCapacity = 4 * 1024 * 1024 // 4MB
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		var usage *GeminiUsage

		for scanner.Scan() {
			line := scanner.Text()

			if debugLogEnabled {
				logBuffer.WriteString(line + "\n")
			}

			// Try to extract usage from streaming response
			if u := extractGeminiUsageFromSSE(line); u != nil {
				usage = u
			}

			// Forward line directly (passthrough)
			_, err := c.Writer.Write([]byte(line + "\n"))
			if err != nil {
				log.Printf("⚠️ Gemini 流式响应传输错误: %v", err)
				break
			}

			if flusher != nil {
				flusher.Flush()
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("⚠️ Gemini 流式响应读取错误: %v", err)
		}

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
				record.OutputTokens = usage.CandidatesTokenCount
			}

			if err := reqLogManager.Update(requestLogID, record); err != nil {
				log.Printf("⚠️ 请求日志更新失败: %v", err)
			}

			SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, logBuffer.Bytes())
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
			record.OutputTokens = usage.CandidatesTokenCount
		}

		if err := reqLogManager.Update(requestLogID, record); err != nil {
			log.Printf("⚠️ 请求日志更新失败: %v", err)
		}

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
	TotalTokenCount      int
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

	// Parse usageMetadata from response
	promptTokens := gjson.Get(jsonStr, "usageMetadata.promptTokenCount").Int()
	candidatesTokens := gjson.Get(jsonStr, "usageMetadata.candidatesTokenCount").Int()
	totalTokens := gjson.Get(jsonStr, "usageMetadata.totalTokenCount").Int()

	if promptTokens > 0 || candidatesTokens > 0 {
		return &GeminiUsage{
			PromptTokenCount:     int(promptTokens),
			CandidatesTokenCount: int(candidatesTokens),
			TotalTokenCount:      int(totalTokens),
		}
	}

	return nil
}

// extractGeminiUsageFromJSON extracts usage from non-streaming Gemini response
func extractGeminiUsageFromJSON(body []byte) *GeminiUsage {
	promptTokens := gjson.GetBytes(body, "usageMetadata.promptTokenCount").Int()
	candidatesTokens := gjson.GetBytes(body, "usageMetadata.candidatesTokenCount").Int()
	totalTokens := gjson.GetBytes(body, "usageMetadata.totalTokenCount").Int()

	if promptTokens > 0 || candidatesTokens > 0 {
		return &GeminiUsage{
			PromptTokenCount:     int(promptTokens),
			CandidatesTokenCount: int(candidatesTokens),
			TotalTokenCount:      int(totalTokens),
		}
	}

	return nil
}
