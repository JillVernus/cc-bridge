package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/continuethinking"
	"github.com/JillVernus/cc-bridge/internal/middleware"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ctRequestContext is the request-scoped state a continue-thinking continuation
// round needs to rebuild and send one upstream request. It is captured by the
// opener closure passed into FoldStream.
type ctRequestContext struct {
	c            *gin.Context
	cfgManager   *config.ConfigManager
	upstream     *config.UpstreamConfig
	envCfg       *config.EnvConfig
	responsesReq types.ResponsesRequest
	// provider is the Responses provider used for API-key ("responses" serviceType)
	// continuation rounds (URL/header rebuild). nil for the OAuth path.
	provider *providers.ResponsesProvider
	// apiKey is the round-1 API key that succeeded; reused for all continuation
	// rounds of an API-key "responses" channel. Empty for the OAuth path.
	apiKey string
}

// maybeContinueThinkingOpener returns a RoundOpener for the given channel, or
// nil when continue-thinking is disabled, the channel is not a native Responses
// service type, or rebuild context is missing. This is the single gate;
// handleResponsesSuccess only folds when the returned opener is non-nil.
//
// Two opener paths exist, selected by serviceType:
//   - "openai-oauth" (Codex OAuth): rebuilds via buildCodexOAuthRequest with
//     preserveEncryptedReasoning=true, because the OAuth sanitizer would
//     otherwise strip the replayed encrypted reasoning the fold depends on.
//   - "responses" (API key): rebuilds directly with Authorization: Bearer,
//     preserving the replayed encrypted reasoning (no sanitizer runs on this
//     path). This is the path that hits the 516 reasoning-token cap when using
//     Codex through an API-key Responses endpoint.
func maybeContinueThinkingOpener(
	upstream *config.UpstreamConfig,
	ctCtx *ctRequestContext,
) continuethinking.RoundOpener {
	if upstream == nil || !upstream.ContinueThinkingEnabled {
		return nil
	}
	if ctCtx == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(upstream.ServiceType)) {
	case "openai-oauth":
		return openCodexOAuthContinueRound(ctCtx)
	case "responses":
		if ctCtx.provider == nil || ctCtx.apiKey == "" {
			return nil
		}
		return openAPIKeyResponsesContinueRound(ctCtx)
	default:
		return nil
	}
}

// openCodexOAuthContinueRound rebuilds a continuation round for a Codex OAuth
// channel, preserving replayed encrypted reasoning, and sends it.
func openCodexOAuthContinueRound(ctCtx *ctRequestContext) continuethinking.RoundOpener {
	return func(ctx context.Context, payload map[string]any) (*http.Response, error) {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		// Re-resolve a valid access token per continuation round (tokens may
		// refresh between rounds).
		accessToken, accountID, _, tokenErr := codexTokenManager.GetValidToken(ctCtx.upstream.OAuthTokens)
		if tokenErr != nil {
			return nil, tokenErr
		}

		req, _, reqErr := buildCodexOAuthRequest(
			ctCtx.c, ctCtx.cfgManager, ctCtx.upstream, bodyBytes, ctCtx.responsesReq,
			accessToken, accountID, true, true, // preserveEncryptedReasoning=true
		)
		if reqErr != nil {
			return nil, reqErr
		}
		return sendResponsesRequest(req, ctCtx.upstream, ctCtx.envCfg, ctCtx.responsesReq.Stream)
	}
}

// openAPIKeyResponsesContinueRound rebuilds a continuation round for an API-key
// "responses" channel: same target URL + Authorization: Bearer as round 1, with
// the fold payload (which carries the replayed encrypted reasoning) as the body.
// No sanitizer runs on this path, so the encrypted reasoning is preserved.
func openAPIKeyResponsesContinueRound(ctCtx *ctRequestContext) continuethinking.RoundOpener {
	return func(ctx context.Context, payload map[string]any) (*http.Response, error) {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		stream := false
		if s, ok := payload["stream"].(bool); ok {
			stream = s
		}
		model := ""
		if m, ok := payload["model"].(string); ok {
			model = m
		}

		targetURL := ctCtx.provider.BuildTargetURL(ctCtx.upstream, model, stream)
		req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}

		// Mirror round-1 header handling: transparent proxy headers minus client
		// auth, then the channel's own Authorization: Bearer.
		req.Header = utils.PrepareUpstreamHeaders(ctCtx.c, req.URL.Host)
		req.Header.Del("authorization")
		req.Header.Del("x-api-key")
		req.Header.Del("x-goog-api-key")
		utils.SetAuthenticationHeader(req.Header, ctCtx.apiKey)
		req.Header.Set("Content-Type", "application/json")

		applyResponsesUserAgentPolicy(ctCtx.c, ctCtx.cfgManager, ctCtx.upstream, req)
		ApplyOutboundHeaderPolicy(ctCtx.c, ctCtx.cfgManager, req)

		return sendResponsesRequest(req, ctCtx.upstream, ctCtx.envCfg, stream)
	}
}

// ensureContinueThinkingInclude parses a request body, forces the
// `reasoning.encrypted_content` include flag into it, and returns the re-serialized
// bytes. Used on round 1 so the truncation round carries replayable encrypted
// reasoning. Falls back to the original bytes on any parse/marshal error.
func ensureContinueThinkingInclude(bodyBytes []byte) []byte {
	var reqMap map[string]any
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil || reqMap == nil {
		return bodyBytes
	}
	if !utils.EnsureResponsesEncryptedReasoningInclude(reqMap) {
		return bodyBytes // already present
	}
	out, err := json.Marshal(reqMap)
	if err != nil {
		return bodyBytes
	}
	return out
}

// continueThinkingApplies reports whether the fold should run for this upstream.
// It gates on the native Responses service types (no Chat→Responses conversion
// is involved — the fold operates on native Responses SSE).
func continueThinkingApplies(upstream *config.UpstreamConfig) bool {
	if upstream == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(upstream.ServiceType)) {
	case "responses", "openai-oauth":
		return true
	default:
		return false
	}
}

// runContinueThinkingFold drives the continue-thinking fold for one client
// request. It consumes the round-1 upstream response body, writes the folded
// downstream SSE stream to the client, and finalizes a request log entry per
// upstream round (round 1 = the existing client-request log; continuation
// rounds = new pending logs marked with FailoverInfo), satisfying the
// "N rounds folded -> N log entries" requirement.
func runContinueThinkingFold(
	c *gin.Context,
	resp *http.Response,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	originalReq *types.ResponsesRequest,
	originalRequestJSON []byte,
	reqLogManager *requestlog.Manager,
	requestLogID string,
	usageManager *quota.UsageManager,
	upstream *config.UpstreamConfig,
	logChannelIndex int,
	logChannelName string,
	isFastMode bool,
	effectiveServiceTier string,
	serviceTierOverridden bool,
	startTime time.Time,
	opener continuethinking.RoundOpener,
) {
	flusher, _ := c.Writer.(http.Flusher)
	c.Status(resp.StatusCode)

	// Parse the client request body into a map for continuation-round rebuilds.
	var baseBody map[string]any
	if err := json.Unmarshal(originalRequestJSON, &baseBody); err != nil || baseBody == nil {
		baseBody = map[string]any{}
	}
	if _, ok := baseBody["model"].(string); !ok && originalReq != nil {
		baseBody["model"] = originalReq.Model
	}
	origInput, _ := baseBody["input"].([]any)

	channelName := strings.TrimSpace(logChannelName)
	if channelName == "" {
		channelName = strings.TrimSpace(upstream.Name)
	}
	channelIndex := logChannelIndex
	if channelIndex < 0 {
		channelIndex = upstream.Index
	}
	debugLogEnabled := cfgManager.GetDebugLogConfig().Enabled

	// Resolve the original request's client identity once from the pre-created
	// round-1 log row so continuation rounds (Add path) are attributed to the
	// same client as the original request.
	client := clientAttributionFromLog(reqLogManager, requestLogID)

	// Round-1 upstream request body for the per-round debug trace: prefer the
	// body captured in the gin context (set by the debug middleware), else fall
	// back to the original client request JSON.
	round1ReqBody := originalRequestJSON
	if rb, ok := c.Get(ContextKeyDebugRequestBody); ok {
		if b, ok := rb.([]byte); ok && len(b) > 0 {
			round1ReqBody = b
		}
	}

	deps := continuethinking.Deps{
		Round1Body:          resp.Body,
		Round1StatusCode:    resp.StatusCode,
		Round1Headers:       resp.Header,
		Round1RequestBody:   round1ReqBody,
		Round1RequestSentAt: startTime,
		BaseBody:            baseBody,
		OrigInput:           origInput,
		OpenRound:           opener,
		OnRoundUsage: func(round int, u continuethinking.RoundUsage) {
			if reqLogManager == nil || requestLogID == "" {
				return
			}
			roundLogID := finalizeContinueThinkingRoundLog(
				reqLogManager, requestLogID, round, u, upstream, originalReq,
				channelIndex, channelName, isFastMode, effectiveServiceTier, serviceTierOverridden, startTime, "sse", client,
			)
			if debugLogEnabled && roundLogID != "" {
				saveContinueThinkingRoundDebug(reqLogManager, cfgManager, c, roundLogID, u.Trace)
			}
			if u.Status == requestlog.StatusCompleted {
				trackResponsesUsage(usageManager, upstream, modelOf(originalReq), 0)
			}
		},
		Log: func(format string, args ...any) {
			if envCfg != nil && envCfg.ShouldLog("info") {
				log.Printf(format, args...)
			}
		},
	}

	out := continuethinking.FoldStream(c.Request.Context(), deps)

	if _, err := c.Writer.Write(out); err != nil {
		if envCfg != nil && envCfg.ShouldLog("warn") {
			log.Printf("⚠️ continue-thinking: downstream write error: %v", err)
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// clientAttribution carries the original request's client identity so every
// continue-thinking log row (including Add-path rows: WebSocket turns and SSE
// continuation rounds) records the same ClientID/SessionID/APIKeyID as the
// original request. The main log is the source of truth for per-client request
// counts and cost, so attribution must never be dropped on the fold path.
type clientAttribution struct {
	clientID  string
	sessionID string
	apiKeyID  *int64
}

// clientAttributionFromLog reads the client identity off an already-created
// request-log row (the SSE path pre-creates the round-1 row with attribution via
// responses.go). Used so SSE continuation rounds (Add path) inherit the same
// client identity as the original request. Returns zero-value attribution when
// the row cannot be read.
func clientAttributionFromLog(reqLogManager *requestlog.Manager, requestLogID string) clientAttribution {
	if reqLogManager == nil || requestLogID == "" {
		return clientAttribution{}
	}
	row, err := reqLogManager.GetByID(requestLogID)
	if err != nil || row == nil {
		return clientAttribution{}
	}
	return clientAttribution{
		clientID:  row.ClientID,
		sessionID: row.SessionID,
		apiKeyID:  row.APIKeyID,
	}
}

// clientAttributionFromFrame parses the client identity from a `response.create`
// frame plus the gin request context, mirroring observeClientMessage on the raw
// WebSocket proxy path (prompt_cache_key -> codex/sessionID; else the
// conversation id from headers; apiKeyID from the auth middleware context). The
// WebSocket fold has no pre-created log row, so it derives attribution here to
// keep every folded row attributed to the original client.
func clientAttributionFromFrame(c *gin.Context, createFrame []byte) clientAttribution {
	var attr clientAttribution
	var header http.Header
	if c != nil && c.Request != nil {
		header = c.Request.Header
		if id, exists := c.Get(middleware.ContextKeyAPIKeyID); exists {
			if idVal, ok := id.(int64); ok {
				attr.apiKeyID = &idVal
			}
		}
	}

	promptCacheKey := strings.TrimSpace(gjson.GetBytes(createFrame, "prompt_cache_key").String())
	if promptCacheKey != "" {
		attr.clientID = "codex"
		attr.sessionID = promptCacheKey
		return attr
	}
	compoundUserID := extractConversationIDFromHeaders(header, createFrame)
	attr.clientID, attr.sessionID = parseClaudeCodeUserID(compoundUserID)
	return attr
}

// finalizeContinueThinkingRoundLog writes/finalizes one request log entry for a
// continue-thinking round. Round 1 updates the existing requestLogID with
// round-1 usage; continuation rounds create a new pending log (mirroring the
// existing failover new-pending-log pattern) marked via FailoverInfo so folded
// rounds are distinguishable in the main log.
func finalizeContinueThinkingRoundLog(
	reqLogManager *requestlog.Manager,
	requestLogID string,
	round int,
	u continuethinking.RoundUsage,
	upstream *config.UpstreamConfig,
	originalReq *types.ResponsesRequest,
	logChannelIndex int,
	logChannelName string,
	isFastMode bool,
	effectiveServiceTier string,
	serviceTierOverridden bool,
	startTime time.Time,
	transport string,
	client clientAttribution,
) string {
	completeTime := time.Now()
	actualInput := max0(u.InputTokens - u.CachedTokens)
	costBreakdown := computeContinueThinkingCost(upstream, originalReq, u, actualInput, isFastMode)

	// Per-round timing: TTFT = first upstream payload − request dispatch;
	// duration = request dispatch → round complete. Round 1's dispatch base is
	// the client request start; continuation rounds carry their own dispatch
	// stamp. All three timing fields are populated for every round.
	var firstTokenTime *time.Time
	var firstTokenDurationMs int64
	var durationMs int64
	if !u.FirstEventAt.IsZero() {
		ft := u.FirstEventAt
		firstTokenTime = &ft
		base := u.RequestSentAt
		if base.IsZero() {
			base = startTime
		}
		firstTokenDurationMs = ft.Sub(base).Milliseconds()
		if firstTokenDurationMs < 0 {
			firstTokenDurationMs = 0
		}
	}
	durationBase := u.RequestSentAt
	if durationBase.IsZero() {
		durationBase = startTime
	}
	if !u.RoundCompleteAt.IsZero() {
		durationMs = u.RoundCompleteAt.Sub(durationBase).Milliseconds()
	} else {
		durationMs = completeTime.Sub(durationBase).Milliseconds()
	}
	if durationMs < 0 {
		durationMs = 0
	}

	record := &requestlog.RequestLog{
		Status:               orLogStatus(u.Status, requestlog.StatusCompleted),
		CompleteTime:         completeTime,
		DurationMs:           durationMs,
		FirstTokenTime:       firstTokenTime,
		FirstTokenDurationMs: firstTokenDurationMs,
		Type:                 upstream.ServiceType,
		ProviderName:         upstream.Name,
		Model:                modelOf(originalReq),
		ResponseModel:        u.ResponseModel,
		HTTPStatus:           u.HTTPStatus,
		ReasoningEffort:      u.Trace.ReasoningEffort,
		Transport:            transport,
		Stream:               true,
		ChannelID:            logChannelIndex,
		ChannelName:          logChannelName,
		Endpoint:             "/v1/responses",
		InputTokens:          actualInput,
		OutputTokens:         u.OutputTokens,
		CacheReadInputTokens: u.CachedTokens,
	}
	if costBreakdown != nil {
		record.Price = costBreakdown.TotalCost
		record.InputCost = costBreakdown.InputCost
		record.OutputCost = costBreakdown.OutputCost
		record.CacheReadCost = costBreakdown.CacheReadCost
	}
	applyResponsesServiceTierMetadata(record, effectiveServiceTier, serviceTierOverridden)

	// Round 1 with an existing log row (the HTTP/SSE path pre-creates one):
	// finalize it in place. Otherwise (round > 1, or a transport like WebSocket
	// that has no pre-created row) append a new log entry.
	if round == 1 && requestLogID != "" {
		if err := reqLogManager.Update(requestLogID, record); err != nil {
			log.Printf("⚠️ continue-thinking round-1 log update failed: %v", err)
			return ""
		}
		return requestLogID
	}

	// New log entry. Continuation rounds (round > 1) are marked via FailoverInfo
	// so folded rounds are distinguishable in the main log.
	failoverInfo := ""
	if round > 1 {
		failoverInfo = "continue_thinking round " + itoaInt(round)
	}
	// Row start time: round 1 (e.g. a WebSocket turn with no pre-created row) begins
	// at the client request start; continuation rounds begin when that round was
	// dispatched upstream. Falling back to completeTime keeps the row well-formed if
	// no dispatch stamp is available.
	initialTime := u.RequestSentAt
	if round == 1 {
		initialTime = startTime
	}
	if initialTime.IsZero() {
		initialTime = completeTime
	}
	pending := &requestlog.RequestLog{
		Status:               record.Status,
		InitialTime:          initialTime,
		CompleteTime:         completeTime,
		DurationMs:           record.DurationMs,
		FirstTokenTime:       record.FirstTokenTime,
		FirstTokenDurationMs: record.FirstTokenDurationMs,
		Type:                 record.Type,
		ProviderName:         record.ProviderName,
		Model:                record.Model,
		ResponseModel:        record.ResponseModel,
		HTTPStatus:           record.HTTPStatus,
		ReasoningEffort:      record.ReasoningEffort,
		Stream:               true,
		Transport:            transport,
		ChannelID:            record.ChannelID,
		ChannelName:          record.ChannelName,
		Endpoint:             "/v1/responses",
		InputTokens:          record.InputTokens,
		OutputTokens:         record.OutputTokens,
		CacheReadInputTokens: record.CacheReadInputTokens,
		Price:                record.Price,
		InputCost:            record.InputCost,
		OutputCost:           record.OutputCost,
		CacheReadCost:        record.CacheReadCost,
		FailoverInfo:         failoverInfo,
		// Carry the original request's client identity so Add-path rows (WS turns
		// and SSE continuation rounds) are attributed to the same client as the
		// original request. The main log aggregates request count and cost per
		// client, so every original metric must be recorded regardless of whether
		// continue-thinking is enabled.
		ClientID:  client.clientID,
		SessionID: client.sessionID,
		APIKeyID:  client.apiKeyID,
	}
	applyResponsesServiceTierMetadata(pending, effectiveServiceTier, serviceTierOverridden)
	if err := reqLogManager.Add(pending); err != nil {
		log.Printf("⚠️ continue-thinking round-%d log create failed: %v", round, err)
		return ""
	}
	return pending.ID
}

// saveContinueThinkingRoundDebug persists a per-round debug log entry (request
// body + raw response bytes) for a folded continue-thinking round. Mirrors
// SaveDebugLog but sources the bodies from the fold's per-round trace rather
// than the gin context (continuation rounds are built inside the fold). Request
// method/path/headers come from the client's gin context so every folded row
// carries request headers (matching non-fold rows); response headers come from
// the round trace, falling back to a WebSocket-appropriate default when the
// transport has no per-round HTTP response headers.
func saveContinueThinkingRoundDebug(
	reqLogManager *requestlog.Manager,
	cfgManager *config.ConfigManager,
	c *gin.Context,
	roundLogID string,
	trace continuethinking.RoundTrace,
) {
	if reqLogManager == nil || roundLogID == "" {
		return
	}
	maxBodySize := 1024 * 1024
	if cfgManager != nil {
		debugCfg := cfgManager.GetDebugLogConfig()
		maxBodySize = debugCfg.GetMaxBodySize()
	}

	// Request method/path/headers from the client request context so every folded
	// row records request headers just like non-fold rows.
	requestMethod := ""
	requestPath := "/v1/responses"
	requestHeaders := map[string]string{}
	if c != nil && c.Request != nil {
		requestMethod = c.Request.Method
		if c.Request.URL != nil {
			requestPath = c.Request.URL.Path
		}
		requestHeaders = requestlog.HttpHeadersToMap(c.Request.Header)
	}

	// Response headers: use the round's upstream response headers when present
	// (SSE); WebSocket rounds have no per-round HTTP response, so fall back to the
	// same synthetic header the raw WebSocket proxy records.
	responseHeaders := requestlog.HttpHeadersToMap(trace.ResponseHeaders)
	if len(responseHeaders) == 0 {
		responseHeaders = map[string]string{"Content-Type": "application/jsonl"}
	}

	entry := &requestlog.DebugLogEntry{
		RequestID:        roundLogID,
		RequestMethod:    requestMethod,
		RequestPath:      requestPath,
		RequestHeaders:   requestHeaders,
		RequestBodySize:  len(trace.RequestBody),
		ResponseStatus:   trace.ResponseStatus,
		ResponseHeaders:  responseHeaders,
		ResponseBodySize: len(trace.ResponseBody),
	}
	if maxBodySize > 0 && len(trace.RequestBody) > maxBodySize {
		entry.RequestBody = string(trace.RequestBody[:maxBodySize]) + "\n... [truncated]"
	} else {
		entry.RequestBody = string(trace.RequestBody)
	}
	if maxBodySize > 0 && len(trace.ResponseBody) > maxBodySize {
		entry.ResponseBody = string(trace.ResponseBody[:maxBodySize]) + "\n... [truncated]"
	} else {
		entry.ResponseBody = string(trace.ResponseBody)
	}
	go func() {
		if err := reqLogManager.AddDebugLog(entry); err != nil {
			log.Printf("⚠️ continue-thinking: failed to save round debug log: %v", err)
		}
	}()
}

// computeContinueThinkingCost mirrors the pricing logic in handleResponsesSuccess:
// prefer the response model, fall back to the request model when it has no
// pricing config. Returns nil when no pricing manager is available.
func computeContinueThinkingCost(
	upstream *config.UpstreamConfig,
	originalReq *types.ResponsesRequest,
	u continuethinking.RoundUsage,
	actualInput int,
	isFastMode bool,
) *pricing.CostBreakdown {
	pm := pricing.GetManager()
	if pm == nil {
		return nil
	}
	pricingModel := u.ResponseModel
	if pricingModel == "" && originalReq != nil {
		pricingModel = originalReq.Model
	}
	if pricingModel != "" && !pm.HasPricing(pricingModel) && originalReq != nil && originalReq.Model != "" {
		pricingModel = originalReq.Model
	}
	if pricingModel == "" {
		return nil
	}
	var multipliers *pricing.PriceMultipliers
	if channelMult := upstream.GetPriceMultipliers(pricingModel); channelMult != nil {
		multipliers = &pricing.PriceMultipliers{
			InputMultiplier:         channelMult.GetEffectiveMultiplier("input"),
			OutputMultiplier:        channelMult.GetEffectiveMultiplier("output"),
			CacheCreationMultiplier: channelMult.GetEffectiveMultiplier("cacheCreation"),
			CacheReadMultiplier:     channelMult.GetEffectiveMultiplier("cacheRead"),
		}
	}
	multipliers = pricing.ApplyFastModeMultiplier(multipliers, isFastMode)
	b := pm.CalculateCostWithBreakdown(pricingModel, actualInput, u.OutputTokens, 0, u.CachedTokens, multipliers)
	return &b
}

func modelOf(req *types.ResponsesRequest) string {
	if req == nil {
		return ""
	}
	return req.Model
}

func orLogStatus(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func max0(i int) int {
	if i < 0 {
		return 0
	}
	return i
}

func itoaInt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
