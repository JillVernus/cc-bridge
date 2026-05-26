package handlers

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

const codexResponsesWebSocketBetaHeaderValue = "responses_websockets=2026-02-06"

var responsesWebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ResponsesWebSocketHandler handles optional WebSocket transport for /v1/responses.
func ResponsesWebSocketHandler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	reqLogManager *requestlog.Manager,
	apiKeyManager *apikey.Manager,
	failoverTracker *config.FailoverTracker,
	channelRateLimiter *middleware.ChannelRateLimiter,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfgManager == nil || !cfgManager.GetResponsesWebSocketConfig().Enabled {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Responses WebSocket disabled",
				"message": "Responses WebSocket transport is disabled in admin settings",
				"path":    c.Request.URL.Path,
			})
			return
		}

		if envCfg != nil {
			if _, exists := c.Get(middleware.ContextKeyAPIKeyID); !exists {
				middleware.ProxyAuthMiddlewareWithAPIKey(envCfg, apiKeyManager)(c)
				if c.IsAborted() {
					return
				}
			}
		}

		if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
			if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
				if !validatedKey.CheckEndpointPermission("responses") {
					c.JSON(http.StatusForbidden, gin.H{
						"error": "Endpoint /v1/responses not allowed for this API key",
						"code":  "ENDPOINT_NOT_ALLOWED",
					})
					return
				}
			}
		}

		upstream, selection, err := selectResponsesWebSocketUpstream(c, cfgManager, channelScheduler)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if upstream.ServiceType != "openai-oauth" {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error":   "Responses WebSocket only supports openai-oauth channels",
				"message": "Use HTTPS/SSE fallback for this channel type",
			})
			return
		}
		if upstream.OAuthTokens == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OAuth tokens not configured for this channel"})
			return
		}

		if channelRateLimiter != nil {
			result := channelRateLimiter.Acquire(c.Request.Context(), upstream, "responses")
			if !result.Allowed {
				status := http.StatusTooManyRequests
				if result.Error == c.Request.Context().Err() {
					status = http.StatusRequestTimeout
				}
				errText := "channel rate limit exceeded"
				if result.Error != nil {
					errText = result.Error.Error()
				}
				c.JSON(status, gin.H{"error": errText})
				return
			}
		}

		accessToken, accountID, updatedTokens, err := codexTokenManager.GetValidToken(upstream.OAuthTokens)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Failed to get valid OAuth token: %s", err.Error())})
			return
		}
		if updatedTokens != nil {
			if saveErr := cfgManager.UpdateResponsesOAuthTokensByName(upstream.Name, updatedTokens); saveErr != nil {
				// Do not fail the in-flight request when persistence fails; the
				// refreshed token is still valid for this connection.
				fmt.Printf("⚠️ [OAuth WebSocket] failed to save refreshed token: %v\n", saveErr)
			}
		}

		upstreamURL, err := buildResponsesWebSocketURL(codexOAuthResponsesEndpoint)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		upstreamHeaders := buildCodexOAuthWebSocketHeaders(c, cfgManager, accessToken, accountID)

		clientConn, err := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer clientConn.Close()

		upstreamConn, resp, err := dialResponsesWebSocket(c.Request.Context(), upstreamURL, upstreamHeaders, upstream)
		if err != nil {
			if resp != nil {
				_ = clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","status":%d,"error":{"message":%q}}`, resp.StatusCode, http.StatusText(resp.StatusCode))))
			} else {
				_ = clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","status":502,"error":{"message":%q}}`, err.Error())))
			}
			if channelScheduler != nil && selection != nil {
				channelScheduler.RecordFailure(selection.ChannelIndex, true)
			}
			return
		}
		defer upstreamConn.Close()

		logTracker := newResponsesWebSocketLogTracker(c, reqLogManager, upstream, selection)
		err = proxyResponsesWebSocketFrames(clientConn, upstreamConn, logTracker)
		logTracker.finish(err)
		if channelScheduler != nil && selection != nil {
			if err != nil {
				channelScheduler.RecordFailure(selection.ChannelIndex, true)
			} else {
				channelScheduler.RecordSuccess(selection.ChannelIndex, true)
			}
		}
	}
}

func selectResponsesWebSocketUpstream(c *gin.Context, cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) (*config.UpstreamConfig, *scheduler.SelectionResult, error) {
	var allowedChannels []string
	if vk, exists := c.Get(middleware.ContextKeyValidatedKey); exists {
		if validatedKey, ok := vk.(*apikey.ValidatedKey); ok && validatedKey != nil {
			allowedChannels = validatedKey.GetAllowedChannelsByType("responses")
		}
	}

	if channelScheduler != nil {
		selection, err := channelScheduler.SelectChannel(c.Request.Context(), "codex-websocket", map[int]bool{}, true, allowedChannels, "")
		if err != nil {
			return nil, nil, err
		}
		if selection == nil || selection.Upstream == nil {
			return nil, nil, fmt.Errorf("no Responses channel selected")
		}
		if selection.CompositeUpstream != nil {
			return nil, nil, fmt.Errorf("Responses WebSocket does not support composite channels")
		}
		return selection.Upstream, selection, nil
	}

	cfg := cfgManager.GetConfig()
	for i := range cfg.ResponsesUpstream {
		upstream := cfg.ResponsesUpstream[i]
		if config.GetChannelStatus(&upstream) == "disabled" {
			continue
		}
		if len(allowedChannels) > 0 && !channelIDAllowed(upstream.ID, allowedChannels) {
			continue
		}
		upstream.Index = i
		return &upstream, &scheduler.SelectionResult{Upstream: &upstream, ChannelIndex: i}, nil
	}
	return nil, nil, fmt.Errorf("no active Responses channels available")
}

func channelIDAllowed(channelID string, allowedChannels []string) bool {
	channelID = strings.TrimSpace(channelID)
	for _, allowed := range allowedChannels {
		if channelID != "" && channelID == strings.TrimSpace(allowed) {
			return true
		}
	}
	return false
}

func buildResponsesWebSocketURL(httpURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(httpURL))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported responses websocket URL scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("responses websocket URL host is empty")
	}
	return parsed.String(), nil
}

func buildCodexOAuthWebSocketHeaders(c *gin.Context, cfgManager *config.ConfigManager, accessToken string, accountID string) http.Header {
	headers := http.Header{}
	incoming := c.Request.Header
	copyOptionalHeader(headers, incoming, "x-codex-turn-state")
	copyOptionalHeader(headers, incoming, "x-codex-turn-metadata")
	copyOptionalHeader(headers, incoming, "x-client-request-id")
	copyOptionalHeader(headers, incoming, "x-responsesapi-include-timing-metrics")
	copyOptionalHeader(headers, incoming, "Version")
	copyOptionalHeader(headers, incoming, "Conversation_id")
	copyOptionalHeader(headers, incoming, "Session_id")
	copyOptionalHeader(headers, incoming, "X-Claude-Code-Session-Id")

	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("ChatGPT-Account-ID", accountID)
	headers.Set("Originator", strings.TrimSpace(c.GetHeader("Originator")))
	if headers.Get("Originator") == "" {
		headers.Set("Originator", "codex_cli_rs")
	}
	headers.Set("User-Agent", resolveResponsesUserAgentForOAuth(c, cfgManager))

	beta := strings.TrimSpace(c.GetHeader("OpenAI-Beta"))
	if beta == "" || !strings.Contains(beta, "responses_websockets=") {
		beta = codexResponsesWebSocketBetaHeaderValue
	}
	headers.Set("OpenAI-Beta", beta)

	return headers
}

func copyOptionalHeader(dst http.Header, src http.Header, key string) {
	if value := strings.TrimSpace(src.Get(key)); value != "" {
		dst.Set(key, value)
	}
}

func dialResponsesWebSocket(ctx interface{ Done() <-chan struct{} }, upstreamURL string, headers http.Header, upstream *config.UpstreamConfig) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
	}
	if upstream != nil && upstream.InsecureSkipVerify {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	conn, resp, err := dialer.Dial(upstreamURL, headers)
	if conn != nil {
		conn.EnableWriteCompression(false)
	}
	return conn, resp, err
}

type responsesWebSocketLogTracker struct {
	mu          sync.Mutex
	manager     *requestlog.Manager
	apiKeyID    *int64
	upstream    *config.UpstreamConfig
	channelID   int
	channelName string
	header      http.Header

	requestLogID string
	startTime    time.Time
	model        string
	completed    bool

	firstTokenDetector *utils.FirstTokenDetector
	firstTokenTime     *time.Time
	firstPayloadTime   *time.Time
}

func newResponsesWebSocketLogTracker(c *gin.Context, manager *requestlog.Manager, upstream *config.UpstreamConfig, selection *scheduler.SelectionResult) *responsesWebSocketLogTracker {
	tracker := &responsesWebSocketLogTracker{
		manager:  manager,
		upstream: upstream,
		header:   http.Header{},
	}
	if c != nil && c.Request != nil {
		tracker.header = c.Request.Header.Clone()
		if id, exists := c.Get(middleware.ContextKeyAPIKeyID); exists {
			if idVal, ok := id.(int64); ok {
				tracker.apiKeyID = &idVal
			}
		}
	}
	tracker.channelID, tracker.channelName = resolveResponsesRequestLogChannelContext(selection, upstream)
	return tracker
}

func (t *responsesWebSocketLogTracker) observeClientMessage(payload []byte) {
	if t == nil || t.manager == nil || strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.requestLogID != "" && !t.completed {
		return
	}

	t.startTime = time.Now()
	t.completed = false
	t.firstTokenTime = nil
	t.firstPayloadTime = nil
	t.firstTokenDetector = streamDetectorForServiceType(upstreamServiceType(t.upstream))
	t.model = strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	promptCacheKey := strings.TrimSpace(gjson.GetBytes(payload, "prompt_cache_key").String())
	compoundUserID := ""
	userID := ""
	sessionID := ""
	if promptCacheKey != "" {
		userID = "codex"
		sessionID = promptCacheKey
		compoundUserID = promptCacheKey
	} else {
		compoundUserID = extractConversationIDFromHeaders(t.header, payload)
		userID, sessionID = parseClaudeCodeUserID(compoundUserID)
	}

	serviceTier := normalizeResponsesServiceTier(gjson.GetBytes(payload, "service_tier").String())
	if serviceTier == "" && strings.EqualFold(gjson.GetBytes(payload, "speed").String(), "fast") {
		serviceTier = "priority"
	}

	record := &requestlog.RequestLog{
		Status:          requestlog.StatusPending,
		InitialTime:     t.startTime,
		Type:            upstreamServiceType(t.upstream),
		ProviderName:    upstreamName(t.upstream),
		Model:           t.model,
		ReasoningEffort: gjson.GetBytes(payload, "reasoning.effort").String(),
		ServiceTier:     serviceTier,
		Stream:          true,
		Transport:       "ws",
		Endpoint:        "/v1/responses",
		ChannelID:       t.channelID,
		ChannelName:     t.channelName,
		ClientID:        userID,
		SessionID:       sessionID,
		APIKeyID:        t.apiKeyID,
	}
	if err := t.manager.Add(record); err != nil {
		log.Printf("⚠️ failed to create Responses WebSocket request log: %v", err)
		t.requestLogID = ""
		return
	}
	t.requestLogID = record.ID
}

func (t *responsesWebSocketLogTracker) observeUpstreamMessage(payload []byte) {
	if t == nil || t.manager == nil {
		return
	}

	t.observeFirstTokenPayload(payload)

	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	switch eventType {
	case "response.completed":
		t.completeFromPayload(payload)
	case "error", "response.failed":
		errMsg := strings.TrimSpace(gjson.GetBytes(payload, "error.message").String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(gjson.GetBytes(payload, "response.error.message").String())
		}
		if errMsg == "" {
			errMsg = "Responses WebSocket upstream error"
		}
		t.completeError(http.StatusBadGateway, errMsg)
	}
}

func (t *responsesWebSocketLogTracker) observeFirstTokenPayload(payload []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.requestLogID == "" || t.completed || len(strings.TrimSpace(string(payload))) == 0 {
		return
	}
	if t.firstPayloadTime == nil {
		ts := time.Now()
		t.firstPayloadTime = &ts
	}
	if t.firstTokenDetector != nil && t.firstTokenTime == nil {
		markFirstTokenIfDetected(t.firstTokenDetector.ObserveLine("data: "+string(payload)), &t.firstTokenTime)
	}
}

func (t *responsesWebSocketLogTracker) completeFromPayload(payload []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.requestLogID == "" || t.completed {
		return
	}

	completeTime := time.Now()
	firstTokenTime := t.firstTokenTime
	if firstTokenTime == nil {
		firstTokenTime = t.firstPayloadTime
	}
	usage, responseModel := extractCodexUsageFromSSE("data: " + string(payload))
	if responseModel == "" {
		responseModel = strings.TrimSpace(gjson.GetBytes(payload, "response.model").String())
	}
	record := &requestlog.RequestLog{
		Status:               requestlog.StatusCompleted,
		CompleteTime:         completeTime,
		DurationMs:           completeTime.Sub(t.startTime).Milliseconds(),
		Type:                 upstreamServiceType(t.upstream),
		ProviderName:         upstreamName(t.upstream),
		Model:                t.model,
		ResponseModel:        responseModel,
		FirstTokenTime:       firstTokenTime,
		FirstTokenDurationMs: firstTokenDurationFromStart(t.startTime, firstTokenTime),
		HTTPStatus:           http.StatusOK,
		Stream:               true,
		Transport:            "ws",
		Endpoint:             "/v1/responses",
		ChannelID:            t.channelID,
		ChannelName:          t.channelName,
	}
	if usage != nil {
		actualInput := usage.InputTokens - usage.CachedTokens
		if actualInput < 0 {
			actualInput = 0
		}
		record.InputTokens = actualInput
		record.OutputTokens = usage.OutputTokens
		record.CacheReadInputTokens = usage.CachedTokens

		pricingModel := responseModel
		if pricingModel == "" {
			pricingModel = t.model
		}
		if pm := pricing.GetManager(); pm != nil && pricingModel != "" {
			var multipliers *pricing.PriceMultipliers
			if t.upstream != nil {
				if channelMult := t.upstream.GetPriceMultipliers(pricingModel); channelMult != nil {
					multipliers = &pricing.PriceMultipliers{
						InputMultiplier:         channelMult.GetEffectiveMultiplier("input"),
						OutputMultiplier:        channelMult.GetEffectiveMultiplier("output"),
						CacheCreationMultiplier: channelMult.GetEffectiveMultiplier("cacheCreation"),
						CacheReadMultiplier:     channelMult.GetEffectiveMultiplier("cacheRead"),
					}
				}
			}
			multipliers = pricing.ApplyFastModeMultiplier(multipliers, strings.EqualFold(strings.TrimSpace(record.ServiceTier), "priority"))
			breakdown := pm.CalculateCostWithBreakdown(pricingModel, record.InputTokens, record.OutputTokens, record.CacheCreationInputTokens, record.CacheReadInputTokens, multipliers)
			record.Price = breakdown.TotalCost
			record.InputCost = breakdown.InputCost
			record.OutputCost = breakdown.OutputCost
			record.CacheCreationCost = breakdown.CacheCreationCost
			record.CacheReadCost = breakdown.CacheReadCost
		}
	}
	if err := t.manager.Update(t.requestLogID, record); err != nil {
		log.Printf("⚠️ failed to complete Responses WebSocket request log: %v", err)
		return
	}
	t.completed = true
}

func (t *responsesWebSocketLogTracker) completeError(status int, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completeErrorLocked(status, message)
}

func (t *responsesWebSocketLogTracker) completeErrorLocked(status int, message string) {
	if t.requestLogID == "" || t.completed {
		return
	}
	completeTime := time.Now()
	record := &requestlog.RequestLog{
		Status:               requestlog.StatusError,
		CompleteTime:         completeTime,
		DurationMs:           completeTime.Sub(t.startTime).Milliseconds(),
		Type:                 upstreamServiceType(t.upstream),
		ProviderName:         upstreamName(t.upstream),
		Model:                t.model,
		HTTPStatus:           status,
		FirstTokenTime:       t.firstTokenTime,
		FirstTokenDurationMs: firstTokenDurationFromStart(t.startTime, t.firstTokenTime),
		Stream:               true,
		Transport:            "ws",
		Endpoint:             "/v1/responses",
		ChannelID:            t.channelID,
		ChannelName:          t.channelName,
		Error:                message,
		UpstreamError:        message,
	}
	if err := t.manager.Update(t.requestLogID, record); err != nil {
		log.Printf("⚠️ failed to mark Responses WebSocket request log error: %v", err)
		return
	}
	t.completed = true
}

func (t *responsesWebSocketLogTracker) finish(proxyErr error) {
	if t == nil || t.manager == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.requestLogID == "" || t.completed {
		return
	}
	if proxyErr == nil {
		t.completeErrorLocked(499, "websocket closed before response.completed")
		return
	}
	t.completeErrorLocked(http.StatusBadGateway, proxyErr.Error())
}

func extractConversationIDFromHeaders(headers http.Header, payload []byte) string {
	if convID := strings.TrimSpace(headers.Get("Conversation_id")); convID != "" {
		return convID
	}
	if sessID := strings.TrimSpace(headers.Get("X-Claude-Code-Session-Id")); sessID != "" {
		return sessID
	}
	if sessID := strings.TrimSpace(headers.Get("Session_id")); sessID != "" {
		return sessID
	}
	if promptCacheKey := strings.TrimSpace(gjson.GetBytes(payload, "prompt_cache_key").String()); promptCacheKey != "" {
		return promptCacheKey
	}
	if userID := strings.TrimSpace(gjson.GetBytes(payload, "metadata.user_id").String()); userID != "" {
		return userID
	}
	return ""
}

func upstreamServiceType(upstream *config.UpstreamConfig) string {
	if upstream == nil {
		return ""
	}
	return upstream.ServiceType
}

func upstreamName(upstream *config.UpstreamConfig) string {
	if upstream == nil {
		return ""
	}
	return upstream.Name
}

func proxyResponsesWebSocketFrames(clientConn *websocket.Conn, upstreamConn *websocket.Conn, logTracker *responsesWebSocketLogTracker) error {
	errCh := make(chan error, 2)
	closeOnce := sync.Once{}
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}

	copyFrames := func(dst *websocket.Conn, src *websocket.Conn) {
		defer closeBoth()
		for {
			msgType, payload, err := src.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
					errCh <- nil
				} else {
					errCh <- err
				}
				return
			}
			if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
				continue
			}
			if logTracker != nil {
				if src == clientConn {
					logTracker.observeClientMessage(payload)
				} else {
					logTracker.observeUpstreamMessage(payload)
				}
			}
			if err := dst.WriteMessage(msgType, payload); err != nil {
				errCh <- err
				return
			}
		}
	}

	go copyFrames(upstreamConn, clientConn)
	go copyFrames(clientConn, upstreamConn)
	return <-errCh
}
