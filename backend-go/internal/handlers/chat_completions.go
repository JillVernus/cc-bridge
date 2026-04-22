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
	"regexp"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/converters"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

type chatExecuteError struct {
	StatusCode     int
	Body           []byte
	FirstTokenTime *time.Time
	Err            error
}

func (e *chatExecuteError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if len(e.Body) > 0 {
		return string(e.Body)
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("upstream returned status %d", e.StatusCode)
	}
	return "chat upstream error"
}

// ChatCompletionsHandler handles /v1/chat/completions requests.
func ChatCompletionsHandler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
) gin.HandlerFunc {
	return ChatCompletionsHandlerWithAPIKey(envCfg, cfgManager, channelScheduler, reqLogManager, nil, nil, nil, nil)
}

// ChatCompletionsHandlerWithAPIKey handles /v1/chat/completions requests with API key auth.
func ChatCompletionsHandlerWithAPIKey(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	apiKeyManager *apikey.Manager,
	usageManager *quota.UsageManager,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		_ = failoverTracker

		if _, exists := c.Get(middleware.ContextKeyAPIKeyID); !exists {
			middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager)(c)
			if c.IsAborted() {
				return
			}
		}

		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckEndpointPermission("chat") {
					c.JSON(http.StatusForbidden, gin.H{
						"error": "Endpoint /v1/chat/completions not allowed for this API key",
						"code":  "ENDPOINT_NOT_ALLOWED",
					})
					return
				}
			}
		}

		startTime := time.Now()
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		StoreDebugRequestData(c, bodyBytes)

		var req types.ChatCompletionsRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid chat completions request body",
				"code":  "INVALID_REQUEST_BODY",
			})
			return
		}

		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckModelPermission(req.Model) {
					c.JSON(http.StatusForbidden, gin.H{
						"error": fmt.Sprintf("Model %s not allowed for this API key", req.Model),
						"code":  "MODEL_NOT_ALLOWED",
					})
					return
				}
			}
		}

		userID, sessionID := parseClaudeCodeUserID(strings.TrimSpace(req.User))
		if sessionID == "" {
			sessionID = utils.GetSessionIDHeader(c.Request.Header)
		}

		var apiKeyID *int64
		if id, exists := c.Get(middleware.ContextKeyAPIKeyID); exists {
			if idVal, ok := id.(int64); ok {
				apiKeyID = &idVal
			}
		}

		var requestLogID string
		if reqLogManager != nil {
			pendingLog := &requestlog.RequestLog{
				Status:      requestlog.StatusPending,
				InitialTime: startTime,
				Model:       req.Model,
				Stream:      req.Stream,
				Endpoint:    "/v1/chat/completions",
				ClientID:    userID,
				SessionID:   sessionID,
				APIKeyID:    apiKeyID,
			}
			if err := reqLogManager.Add(pendingLog); err != nil {
				log.Printf("⚠️ 创建 chat pending 请求日志失败: %v", err)
			} else {
				requestLogID = pendingLog.ID
			}
		}

		var allowedChannels []string
		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				allowedChannels = validatedKey.GetAllowedChannelsByType("chat")
			}
		}

		if channelScheduler.IsChatMultiChannelMode() {
			handleChatMultiChannel(c, envCfg, cfgManager, channelScheduler, reqLogManager, usageManager, channelRateLimiter, &req, startTime, requestLogID, allowedChannels)
			return
		}

		handleChatSingleChannel(c, envCfg, cfgManager, channelScheduler, reqLogManager, usageManager, channelRateLimiter, &req, startTime, requestLogID, allowedChannels)
	})
}

func handleChatSingleChannel(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	usageManager *quota.UsageManager,
	channelRateLimiter *middleware.ChannelRateLimiter,
	req *types.ChatCompletionsRequest,
	startTime time.Time,
	requestLogID string,
	allowedChannels []string,
) {
	upstream, err := cfgManager.GetCurrentChatUpstream()
	if err != nil {
		finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, nil, http.StatusServiceUnavailable, "No Chat channels configured", nil)
		WriteJSONWithOptionalDebugLog(c, cfgManager, reqLogManager, requestLogID, http.StatusServiceUnavailable, gin.H{"error": "No Chat channels configured"})
		return
	}

	if len(allowedChannels) > 0 {
		allowed := false
		for _, id := range allowedChannels {
			if id == upstream.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, upstream, http.StatusForbidden, "Channel not allowed", nil)
			WriteJSONWithOptionalDebugLog(c, cfgManager, reqLogManager, requestLogID, http.StatusForbidden, gin.H{
				"error": "channel not allowed",
				"code":  "CHANNEL_NOT_ALLOWED",
			})
			return
		}
	}

	if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
		result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "chat")
		if !result.Allowed {
			channelScheduler.RecordChatFailureWithStatusDetail(upstream.Index, http.StatusTooManyRequests, req.Model, upstream.Name)
			finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, upstream, http.StatusTooManyRequests, result.Error.Error(), nil)
			WriteJSONWithOptionalDebugLog(c, cfgManager, reqLogManager, requestLogID, http.StatusTooManyRequests, gin.H{"error": "channel rate limit exceeded"})
			return
		}
	}

	statusCode, firstTokenTime, err := executeChatRequest(c, envCfg, cfgManager, upstream, req, requestLogID, reqLogManager)
	if err != nil {
		channelScheduler.RecordChatFailureWithStatusDetail(upstream.Index, extractChatStatus(err), req.Model, upstream.Name)
		finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, upstream, extractChatStatus(err), err.Error(), firstTokenTime)
		respondChatExecutionError(c, cfgManager, reqLogManager, requestLogID, err)
		return
	}

	channelScheduler.RecordChatSuccessWithStatusDetail(upstream.Index, statusCode, req.Model, upstream.Name)
	finalizeChatSuccessLog(reqLogManager, requestLogID, startTime, req.Model, upstream, statusCode, firstTokenTime)
	trackMessagesUsage(usageManager, upstream, req.Model, 0)
}

func handleChatMultiChannel(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	usageManager *quota.UsageManager,
	channelRateLimiter *middleware.ChannelRateLimiter,
	req *types.ChatCompletionsRequest,
	startTime time.Time,
	requestLogID string,
	allowedChannels []string,
) {
	failedChannels := make(map[int]bool)
	maxAttempts := channelScheduler.GetActiveChatChannelCount()
	if maxAttempts <= 0 {
		finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, nil, http.StatusServiceUnavailable, "No active Chat channels", nil)
		WriteJSONWithOptionalDebugLog(c, cfgManager, reqLogManager, requestLogID, http.StatusServiceUnavailable, gin.H{"error": "No active Chat channels"})
		return
	}

	var lastErr error
	var lastUpstream *config.UpstreamConfig

	for i := 0; i < maxAttempts; i++ {
		selection, err := channelScheduler.SelectChatChannel(c.Request.Context(), failedChannels, allowedChannels)
		if err != nil {
			lastErr = err
			break
		}

		upstream := selection.Upstream
		channelIndex := selection.ChannelIndex
		lastUpstream = upstream

		if channelRateLimiter != nil && upstream.RateLimitRpm > 0 {
			result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "chat")
			if !result.Allowed {
				channelScheduler.RecordChatFailureWithStatusDetail(channelIndex, http.StatusTooManyRequests, req.Model, upstream.Name)
				failedChannels[channelIndex] = true
				lastErr = result.Error
				continue
			}
		}

		statusCode, firstTokenTime, err := executeChatRequest(c, envCfg, cfgManager, upstream, req, requestLogID, reqLogManager)
		if err != nil {
			channelScheduler.RecordChatFailureWithStatusDetail(channelIndex, extractChatStatus(err), req.Model, upstream.Name)
			failedChannels[channelIndex] = true
			lastErr = err
			if reqLogManager != nil && requestLogID != "" {
				if ce, ok := err.(*chatExecuteError); ok && ce != nil && ce.FirstTokenTime == nil {
					ce.FirstTokenTime = firstTokenTime
				}
			}
			if c.Request.Context().Err() != nil {
				return
			}
			continue
		}

		channelScheduler.RecordChatSuccessWithStatusDetail(channelIndex, statusCode, req.Model, upstream.Name)
		finalizeChatSuccessLog(reqLogManager, requestLogID, startTime, req.Model, upstream, statusCode, firstTokenTime)
		trackMessagesUsage(usageManager, upstream, req.Model, 0)
		return
	}

	statusCode := http.StatusServiceUnavailable
	if errors.Is(lastErr, scheduler.ErrNoAllowedChannels) {
		statusCode = http.StatusForbidden
	}
	if ce, ok := lastErr.(*chatExecuteError); ok && ce.StatusCode > 0 {
		statusCode = ce.StatusCode
	}

	var lastFirstTokenTime *time.Time
	if ce, ok := lastErr.(*chatExecuteError); ok && ce != nil {
		lastFirstTokenTime = ce.FirstTokenTime
	}
	finalizeChatErrorLog(reqLogManager, requestLogID, startTime, req.Model, lastUpstream, statusCode, errStringOrDefault(lastErr, "all chat channels unavailable"), lastFirstTokenTime)
	respondChatExecutionErrorWithFallback(c, cfgManager, reqLogManager, requestLogID, lastErr, statusCode)
}

func executeChatRequest(
	c *gin.Context,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	upstream *config.UpstreamConfig,
	req *types.ChatCompletionsRequest,
	requestLogID string,
	reqLogManager *requestlog.Manager,
) (int, *time.Time, error) {
	if upstream == nil {
		return 0, nil, &chatExecuteError{StatusCode: http.StatusServiceUnavailable, Err: fmt.Errorf("chat upstream is nil")}
	}
	if len(upstream.APIKeys) == 0 {
		return 0, nil, &chatExecuteError{StatusCode: http.StatusServiceUnavailable, Err: fmt.Errorf("chat upstream has no API keys")}
	}

	failedKeys := make(map[string]bool)
	maxKeyAttempts := len(upstream.APIKeys)

	for keyAttempt := 0; keyAttempt < maxKeyAttempts; keyAttempt++ {
		apiKey, err := cfgManager.GetNextChatAPIKey(upstream, failedKeys)
		if err != nil {
			return 0, nil, &chatExecuteError{StatusCode: http.StatusServiceUnavailable, Err: err}
		}

		var providerReq *http.Request
		svcType := strings.ToLower(strings.TrimSpace(upstream.ServiceType))
		switch svcType {
		case "claude":
			providerReq, err = buildClaudeRequestForChat(c, upstream, apiKey, req)
		case "gemini":
			providerReq, err = buildGeminiRequestForChat(c, upstream, apiKey, req)
		case "openai", "openai_chat", "openaiold":
			providerReq, err = buildOpenAIRequestForChat(c, upstream, apiKey, req)
		default:
			providerReq, err = buildOpenAIRequestForChat(c, upstream, apiKey, req)
		}
		if err != nil {
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			continue
		}
		ApplyOutboundHeaderPolicy(c, cfgManager, providerReq)

		resp, err := sendRequest(providerReq, upstream, envCfg, req.Stream)
		if err != nil {
			if c.Request.Context().Err() != nil {
				return 0, nil, &chatExecuteError{StatusCode: 499, Err: c.Request.Context().Err()}
			}
			failedKeys[apiKey] = true
			cfgManager.MarkKeyAsFailed(apiKey)
			continue
		}

		statusCode, firstTokenTime, execErr := handleChatUpstreamResponse(c, cfgManager, upstream, req, resp, requestLogID, reqLogManager)
		if execErr != nil {
			var ce *chatExecuteError
			if errors.As(execErr, &ce) {
				if ce.FirstTokenTime == nil {
					ce.FirstTokenTime = firstTokenTime
				}
				if ce.StatusCode >= 400 && ce.StatusCode < 500 && ce.StatusCode != http.StatusTooManyRequests {
					return 0, ce.FirstTokenTime, ce
				}
			}
			failedKeys[apiKey] = true
			if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests {
				cfgManager.MarkKeyAsFailed(apiKey)
			}
			if keyAttempt == maxKeyAttempts-1 {
				return 0, firstTokenTime, execErr
			}
			continue
		}

		return statusCode, firstTokenTime, nil
	}

	return 0, nil, &chatExecuteError{StatusCode: http.StatusServiceUnavailable, Err: fmt.Errorf("all chat keys exhausted")}
}

func handleChatUpstreamResponse(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	upstream *config.UpstreamConfig,
	req *types.ChatCompletionsRequest,
	resp *http.Response,
	requestLogID string,
	reqLogManager *requestlog.Manager,
) (int, *time.Time, error) {
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyBytes = utils.DecompressGzipIfNeeded(resp, bodyBytes)
		SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, bodyBytes)
		return resp.StatusCode, nil, &chatExecuteError{StatusCode: resp.StatusCode, Body: bodyBytes, Err: fmt.Errorf("upstream returned status %d", resp.StatusCode)}
	}

	svcType := strings.ToLower(strings.TrimSpace(upstream.ServiceType))
	if req.Stream {
		utils.ForwardResponseHeaders(resp.Header, c.Writer)
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(resp.StatusCode)

		capture := &bytes.Buffer{}
		detector := utils.NewFirstTokenDetector(utils.FirstTokenProtocolOpenAIChatSSE)
		var err error
		var firstTokenTime *time.Time
		switch svcType {
		case "claude":
			firstTokenTime, err = streamClaudeToChat(c, resp.Body, req, capture, detector)
		case "gemini":
			firstTokenTime, err = streamGeminiToChat(c, resp.Body, req, capture, detector)
		case "openai", "openai_chat", "openaiold":
			firstTokenTime, err = streamOpenAIToChat(c, resp.Body, capture, detector)
		default:
			firstTokenTime, err = streamOpenAIToChat(c, resp.Body, capture, detector)
		}
		SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, capture.Bytes())
		if err != nil {
			return resp.StatusCode, firstTokenTime, &chatExecuteError{StatusCode: http.StatusInternalServerError, FirstTokenTime: firstTokenTime, Err: err}
		}
		return resp.StatusCode, firstTokenTime, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return http.StatusInternalServerError, nil, &chatExecuteError{StatusCode: http.StatusInternalServerError, Err: err}
	}
	bodyBytes = utils.DecompressGzipIfNeeded(resp, bodyBytes)

	var outBytes []byte
	contentType := "application/json"

	switch svcType {
	case "claude":
		outBytes, err = converters.ConvertClaudeJSONToChatJSON(bodyBytes, req.Model)
		if err != nil {
			return http.StatusInternalServerError, nil, &chatExecuteError{StatusCode: http.StatusInternalServerError, Err: err}
		}
	case "gemini":
		chatResp, convErr := converters.ConvertGeminiToChatResponse(bodyBytes, req.Model)
		if convErr != nil {
			return http.StatusInternalServerError, nil, &chatExecuteError{StatusCode: http.StatusInternalServerError, Err: convErr}
		}
		outBytes, err = json.Marshal(chatResp)
		if err != nil {
			return http.StatusInternalServerError, nil, &chatExecuteError{StatusCode: http.StatusInternalServerError, Err: err}
		}
	case "openai", "openai_chat", "openaiold":
		outBytes = bodyBytes
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			contentType = ct
		}
	default:
		outBytes = bodyBytes
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			contentType = ct
		}
	}

	utils.ForwardResponseHeaders(resp.Header, c.Writer)
	c.Data(resp.StatusCode, contentType, outBytes)
	SaveDebugLog(c, cfgManager, reqLogManager, requestLogID, resp.StatusCode, resp.Header, outBytes)

	return resp.StatusCode, nil, nil
}

func buildClaudeRequestForChat(c *gin.Context, upstream *config.UpstreamConfig, apiKey string, req *types.ChatCompletionsRequest) (*http.Request, error) {
	redirected := *req
	redirected.Model = config.RedirectModel(req.Model, upstream)

	claudeReq, err := converters.ConvertChatToClaudeRequest(&redirected)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, err
	}

	targetURL := joinClaudeMessagesURL(upstream.BaseURL)
	httpReq, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header = utils.PrepareUpstreamHeaders(c, httpReq.URL.Host)
	utils.SetAuthenticationHeader(httpReq.Header, apiKey)
	utils.EnsureCompatibleUserAgent(httpReq.Header, "claude")
	return httpReq, nil
}

func buildGeminiRequestForChat(c *gin.Context, upstream *config.UpstreamConfig, apiKey string, req *types.ChatCompletionsRequest) (*http.Request, error) {
	redirected := *req
	redirected.Model = config.RedirectModel(req.Model, upstream)

	geminiReq, err := converters.ConvertChatToGeminiRequest(&redirected)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	action := "generateContent"
	if req.Stream {
		action = "streamGenerateContent?alt=sse"
	}
	targetURL := fmt.Sprintf("%s/models/%s:%s", strings.TrimSuffix(upstream.BaseURL, "/"), redirected.Model, action)

	httpReq, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header = utils.PrepareMinimalHeaders(httpReq.URL.Host)
	utils.SetGeminiAuthenticationHeader(httpReq.Header, apiKey)
	_ = c
	return httpReq, nil
}

func buildOpenAIRequestForChat(c *gin.Context, upstream *config.UpstreamConfig, apiKey string, req *types.ChatCompletionsRequest) (*http.Request, error) {
	redirected := *req
	redirected.Model = config.RedirectModel(req.Model, upstream)
	bodyBytes, err := json.Marshal(&redirected)
	if err != nil {
		return nil, err
	}

	targetURL := joinOpenAIChatCompletionsURL(upstream.BaseURL)
	httpReq, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header = utils.PrepareMinimalHeaders(httpReq.URL.Host)
	utils.SetAuthenticationHeader(httpReq.Header, apiKey)
	_ = c
	return httpReq, nil
}

func streamClaudeToChat(c *gin.Context, body io.ReadCloser, req *types.ChatCompletionsRequest, capture *bytes.Buffer, detector *utils.FirstTokenDetector) (*time.Time, error) {
	converted, err := converters.ConvertClaudeStreamToChat(c.Request.Context(), body, req.Model, req.ShouldIncludeUsage())
	if err != nil {
		return nil, err
	}
	defer converted.Close()
	return streamReaderToClient(c, converted, capture, detector)
}

func streamGeminiToChat(c *gin.Context, body io.ReadCloser, req *types.ChatCompletionsRequest, capture *bytes.Buffer, detector *utils.FirstTokenDetector) (*time.Time, error) {
	outCh, errCh := converters.ConvertGeminiStreamToChat(c.Request.Context(), body, req.Model, req.ShouldIncludeUsage())
	return streamChunkChannelToClient(c, outCh, errCh, capture, detector)
}

func streamOpenAIToChat(c *gin.Context, body io.ReadCloser, capture *bytes.Buffer, detector *utils.FirstTokenDetector) (*time.Time, error) {
	outCh, errCh := converters.ConvertOpenAIStreamToChat(c.Request.Context(), body)
	return streamChunkChannelToClient(c, outCh, errCh, capture, detector)
}

func streamReaderToClient(c *gin.Context, reader io.Reader, capture *bytes.Buffer, detector *utils.FirstTokenDetector) (*time.Time, error) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	var firstTokenTime *time.Time
	var firstStreamPayloadTime *time.Time
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		markFirstSSEPayloadInChunkIfPresent(line, &firstStreamPayloadTime)
		if detector != nil && firstTokenTime == nil {
			markFirstTokenIfDetected(detector.ObserveChunk(line), &firstTokenTime)
		}
		if _, err := c.Writer.Write([]byte(line)); err != nil {
			if firstTokenTime == nil && firstStreamPayloadTime != nil {
				firstTokenTime = firstStreamPayloadTime
			}
			return firstTokenTime, err
		}
		if capture != nil {
			capture.WriteString(line)
		}
		flusher.Flush()
	}
	if firstTokenTime == nil && firstStreamPayloadTime != nil {
		firstTokenTime = firstStreamPayloadTime
	}
	return firstTokenTime, scanner.Err()
}

func streamChunkChannelToClient(c *gin.Context, outCh <-chan string, errCh <-chan error, capture *bytes.Buffer, detector *utils.FirstTokenDetector) (*time.Time, error) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	var firstTokenTime *time.Time
	var firstStreamPayloadTime *time.Time
	for {
		select {
		case chunk, ok := <-outCh:
			if !ok {
				if firstTokenTime == nil && firstStreamPayloadTime != nil {
					firstTokenTime = firstStreamPayloadTime
				}
				return firstTokenTime, nil
			}
			markFirstSSEPayloadInChunkIfPresent(chunk, &firstStreamPayloadTime)
			if detector != nil && firstTokenTime == nil {
				markFirstTokenIfDetected(detector.ObserveChunk(chunk), &firstTokenTime)
			}
			if _, err := c.Writer.Write([]byte(chunk)); err != nil {
				if firstTokenTime == nil && firstStreamPayloadTime != nil {
					firstTokenTime = firstStreamPayloadTime
				}
				return firstTokenTime, err
			}
			if capture != nil {
				capture.WriteString(chunk)
			}
			flusher.Flush()
		case err, ok := <-errCh:
			if ok && err != nil {
				if firstTokenTime == nil && firstStreamPayloadTime != nil {
					firstTokenTime = firstStreamPayloadTime
				}
				return firstTokenTime, err
			}
		case <-c.Request.Context().Done():
			if firstTokenTime == nil && firstStreamPayloadTime != nil {
				firstTokenTime = firstStreamPayloadTime
			}
			return firstTokenTime, c.Request.Context().Err()
		}
	}
}

func finalizeChatSuccessLog(reqLogManager *requestlog.Manager, requestLogID string, startTime time.Time, requestModel string, upstream *config.UpstreamConfig, statusCode int, firstTokenTime *time.Time) {
	if reqLogManager == nil || requestLogID == "" {
		return
	}
	record := &requestlog.RequestLog{
		Status:               requestlog.StatusCompleted,
		CompleteTime:         time.Now(),
		DurationMs:           time.Since(startTime).Milliseconds(),
		FirstTokenTime:       firstTokenTime,
		FirstTokenDurationMs: firstTokenDurationFromStart(startTime, firstTokenTime),
		Model:                requestModel,
		Endpoint:             "/v1/chat/completions",
		HTTPStatus:           statusCode,
	}
	if upstream != nil {
		record.Type = upstream.ServiceType
		record.ProviderName = upstream.Name
		record.ChannelID = upstream.Index
		record.ChannelName = upstream.Name
	}
	_ = reqLogManager.Update(requestLogID, record)
}

func finalizeChatErrorLog(reqLogManager *requestlog.Manager, requestLogID string, startTime time.Time, requestModel string, upstream *config.UpstreamConfig, statusCode int, errMsg string, firstTokenTime *time.Time) {
	if reqLogManager == nil || requestLogID == "" {
		return
	}
	record := &requestlog.RequestLog{
		Status:               requestlog.StatusError,
		CompleteTime:         time.Now(),
		DurationMs:           time.Since(startTime).Milliseconds(),
		FirstTokenTime:       firstTokenTime,
		FirstTokenDurationMs: firstTokenDurationFromStart(startTime, firstTokenTime),
		Model:                requestModel,
		Endpoint:             "/v1/chat/completions",
		HTTPStatus:           statusCode,
		Error:                errMsg,
	}
	if upstream != nil {
		record.Type = upstream.ServiceType
		record.ProviderName = upstream.Name
		record.ChannelID = upstream.Index
		record.ChannelName = upstream.Name
	}
	_ = reqLogManager.Update(requestLogID, record)
}

func respondChatExecutionError(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	err error,
) {
	respondChatExecutionErrorWithFallback(c, cfgManager, reqLogManager, requestLogID, err, http.StatusInternalServerError)
}

func respondChatExecutionErrorWithFallback(
	c *gin.Context,
	cfgManager *config.ConfigManager,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	err error,
	fallbackStatus int,
) {
	statusCode := fallbackStatus
	body := []byte(errStringOrDefault(err, "chat request failed"))

	if ce, ok := err.(*chatExecuteError); ok {
		if ce.StatusCode > 0 {
			statusCode = ce.StatusCode
		}
		if len(ce.Body) > 0 {
			body = ce.Body
		}
	}

	var errObj map[string]interface{}
	if json.Unmarshal(body, &errObj) == nil {
		SaveErrorDebugLog(c, cfgManager, reqLogManager, requestLogID, statusCode, body)
		c.Data(statusCode, "application/json; charset=utf-8", body)
		return
	}

	WriteJSONWithOptionalDebugLog(c, cfgManager, reqLogManager, requestLogID, statusCode, gin.H{"error": string(body)})
}

func extractChatStatus(err error) int {
	if ce, ok := err.(*chatExecuteError); ok && ce.StatusCode > 0 {
		return ce.StatusCode
	}
	if errors.Is(err, scheduler.ErrNoAllowedChannels) {
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func errStringOrDefault(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return fallback
	}
	return msg
}

func joinClaudeMessagesURL(baseURL string) string {
	base := strings.TrimSuffix(baseURL, "/")
	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)
	if versionPattern.MatchString(base) {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func joinOpenAIChatCompletionsURL(baseURL string) string {
	base := strings.TrimSuffix(baseURL, "/")
	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)
	if versionPattern.MatchString(base) {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}
