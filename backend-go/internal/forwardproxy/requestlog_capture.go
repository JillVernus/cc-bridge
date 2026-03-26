package forwardproxy

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/tidwall/gjson"
)

type interceptedRequestKind struct {
	logType            string
	streamServiceType  string
	firstTokenProtocol utils.FirstTokenProtocol
}

var geminiModelPathPattern = regexp.MustCompile(`/models/([^/:]+)`)

func detectInterceptedRequestKind(path string) interceptedRequestKind {
	lowerPath := strings.ToLower(strings.TrimSpace(path))

	switch {
	case isAnthropicEndpoint(lowerPath):
		return interceptedRequestKind{
			logType:            "claude",
			streamServiceType:  "claude",
			firstTokenProtocol: utils.FirstTokenProtocolClaudeSSE,
		}
	case strings.Contains(lowerPath, "/responses"):
		return interceptedRequestKind{
			logType:            "responses",
			streamServiceType:  "responses",
			firstTokenProtocol: utils.FirstTokenProtocolResponsesSSE,
		}
	case strings.Contains(lowerPath, "/chat/completions"):
		return interceptedRequestKind{
			logType:            "openai",
			streamServiceType:  "openai",
			firstTokenProtocol: utils.FirstTokenProtocolOpenAIChatSSE,
		}
	case isGeminiEndpoint(lowerPath):
		return interceptedRequestKind{
			logType:            "gemini",
			streamServiceType:  "gemini",
			firstTokenProtocol: utils.FirstTokenProtocolGeminiRaw,
		}
	default:
		return interceptedRequestKind{
			logType: "forward-proxy",
		}
	}
}

func isGeminiEndpoint(path string) bool {
	return strings.Contains(path, ":generatecontent") ||
		strings.Contains(path, ":streamgeneratecontent") ||
		strings.Contains(path, "/v1/gemini")
}

func createInterceptedPendingLog(req *http.Request, startTime time.Time, hostOnly string, reqBody []byte) *requestlog.RequestLog {
	kind := detectInterceptedRequestKind(req.URL.Path)
	clientID, sessionID := extractInterceptedClientSession(kind.logType, reqBody)
	model, stream := extractInterceptedRequestMeta(kind.logType, req.URL.Path, reqBody)

	return &requestlog.RequestLog{
		Status:       requestlog.StatusPending,
		InitialTime:  startTime,
		Type:         kind.logType,
		ProviderName: hostOnly,
		Model:        model,
		Stream:       stream,
		Endpoint:     req.URL.Path,
		ChannelID:    0,
		ChannelUID:   "subscription:forward-proxy",
		ChannelName:  "Subscription (Forward Proxy)",
		ClientID:     clientID,
		SessionID:    sessionID,
		CreatedAt:    time.Now(),
	}
}

func createInterceptedCompletionRecord(path string, usage *utils.StreamUsage, httpStatus int, startTime, endTime time.Time, hostOnly string, firstTokenTime *time.Time) *requestlog.RequestLog {
	kind := detectInterceptedRequestKind(path)
	if usage == nil {
		usage = &utils.StreamUsage{}
	}

	status := requestlog.StatusCompleted
	if httpStatus >= 400 {
		status = requestlog.StatusError
	}

	durationMs := endTime.Sub(startTime).Milliseconds()
	var firstTokenDurationMs int64
	if firstTokenTime != nil {
		firstTokenDurationMs = firstTokenTime.Sub(startTime).Milliseconds()
		if firstTokenDurationMs < 0 {
			firstTokenDurationMs = 0
		}
	}

	totalTokens := usage.InputTokens + usage.OutputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens

	var totalCost float64
	var inputCost, outputCost, cacheCreationCost, cacheReadCost float64
	if pm := pricing.GetManager(); pm != nil {
		breakdown := pm.CalculateCostWithBreakdown(
			usage.Model,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheCreationInputTokens,
			usage.CacheReadInputTokens,
			nil,
		)
		totalCost = breakdown.TotalCost
		inputCost = breakdown.InputCost
		outputCost = breakdown.OutputCost
		cacheCreationCost = breakdown.CacheCreationCost
		cacheReadCost = breakdown.CacheReadCost
	}

	return &requestlog.RequestLog{
		Status:                   status,
		CompleteTime:             endTime,
		DurationMs:               durationMs,
		FirstTokenTime:           firstTokenTime,
		FirstTokenDurationMs:     firstTokenDurationMs,
		Type:                     kind.logType,
		ProviderName:             hostOnly,
		ResponseModel:            usage.Model,
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		TotalTokens:              totalTokens,
		Price:                    totalCost,
		InputCost:                inputCost,
		OutputCost:               outputCost,
		CacheCreationCost:        cacheCreationCost,
		CacheReadCost:            cacheReadCost,
		HTTPStatus:               httpStatus,
		ChannelUID:               "subscription:forward-proxy",
		ChannelName:              "Subscription (Forward Proxy)",
	}
}

func parseInterceptedJSONResponse(path string, body []byte) *utils.StreamUsage {
	kind := detectInterceptedRequestKind(path)

	switch kind.logType {
	case "claude":
		_, usage := parseJSONResponse(body)
		return usage
	case "responses":
		return parseResponsesJSONUsage(body)
	case "openai":
		return parseOpenAIChatJSONUsage(body)
	case "gemini":
		return parseGeminiJSONUsage(body)
	default:
		return parseGenericJSONUsage(body)
	}
}

func newInterceptedStreamParser(path string) *StreamParserWriter {
	kind := detectInterceptedRequestKind(path)
	if kind.streamServiceType == "" {
		return NewTypedStreamParserWriter("auto", "")
	}
	return NewTypedStreamParserWriter(kind.streamServiceType, kind.firstTokenProtocol)
}

func extractInterceptedClientSession(logType string, body []byte) (clientID, sessionID string) {
	if len(body) == 0 {
		return "", ""
	}

	metadataUserID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if metadataUserID != "" {
		if logType == "claude" {
			return extractClientSession(body)
		}
		clientID, sessionID = parseClaudeCodeUserID(metadataUserID)
	}

	if clientID == "" {
		clientID = strings.TrimSpace(gjson.GetBytes(body, "user").String())
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	}

	return clientID, sessionID
}

func extractInterceptedRequestMeta(logType string, path string, body []byte) (model string, stream bool) {
	if len(body) > 0 {
		model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
		stream = gjson.GetBytes(body, "stream").Bool()
	}

	if model == "" && logType == "gemini" {
		model = extractGeminiModelFromPath(path)
	}

	return model, stream
}

func extractGeminiModelFromPath(path string) string {
	matches := geminiModelPathPattern.FindStringSubmatch(path)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func parseResponsesJSONUsage(body []byte) *utils.StreamUsage {
	usage := &utils.StreamUsage{
		Model: firstNonEmptyString(
			strings.TrimSpace(gjson.GetBytes(body, "model").String()),
			strings.TrimSpace(gjson.GetBytes(body, "response.model").String()),
		),
	}

	rawInputTokens := int(gjson.GetBytes(body, "usage.input_tokens").Int())
	if rawInputTokens == 0 {
		rawInputTokens = int(gjson.GetBytes(body, "usage.prompt_tokens").Int())
	}
	outputTokens := int(gjson.GetBytes(body, "usage.output_tokens").Int())
	if outputTokens == 0 {
		outputTokens = int(gjson.GetBytes(body, "usage.completion_tokens").Int())
	}
	cacheReadTokens := int(gjson.GetBytes(body, "usage.cache_read_input_tokens").Int())
	if cacheReadTokens == 0 {
		cacheReadTokens = int(gjson.GetBytes(body, "usage.input_tokens_details.cached_tokens").Int())
	}
	if cacheReadTokens == 0 {
		cacheReadTokens = int(gjson.GetBytes(body, "usage.prompt_tokens_details.cached_tokens").Int())
	}

	usage.InputTokens = rawInputTokens
	if cacheReadTokens > 0 {
		usage.InputTokens -= cacheReadTokens
		if usage.InputTokens < 0 {
			usage.InputTokens = 0
		}
		usage.CacheReadInputTokens = cacheReadTokens
	}

	usage.OutputTokens = outputTokens
	usage.TotalTokens = int(gjson.GetBytes(body, "usage.total_tokens").Int())
	return usage
}

func parseOpenAIChatJSONUsage(body []byte) *utils.StreamUsage {
	rawInputTokens := int(gjson.GetBytes(body, "usage.prompt_tokens").Int())
	cacheReadTokens := int(gjson.GetBytes(body, "usage.prompt_tokens_details.cached_tokens").Int())
	inputTokens := rawInputTokens
	if cacheReadTokens > 0 {
		inputTokens -= cacheReadTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
	}

	return &utils.StreamUsage{
		Model:                strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		InputTokens:          inputTokens,
		OutputTokens:         int(gjson.GetBytes(body, "usage.completion_tokens").Int()),
		CacheReadInputTokens: cacheReadTokens,
		TotalTokens:          int(gjson.GetBytes(body, "usage.total_tokens").Int()),
	}
}

func parseGeminiJSONUsage(body []byte) *utils.StreamUsage {
	inputTokens := int(gjson.GetBytes(body, "usageMetadata.promptTokenCount").Int())
	outputTokens := int(gjson.GetBytes(body, "usageMetadata.candidatesTokenCount").Int())
	totalTokens := int(gjson.GetBytes(body, "usageMetadata.totalTokenCount").Int())

	if inputTokens == 0 {
		inputTokens = int(gjson.GetBytes(body, "cpaUsageMetadata.promptTokenCount").Int())
	}
	if outputTokens == 0 {
		outputTokens = int(gjson.GetBytes(body, "cpaUsageMetadata.candidatesTokenCount").Int())
	}
	if totalTokens == 0 {
		totalTokens = int(gjson.GetBytes(body, "cpaUsageMetadata.totalTokenCount").Int())
	}

	return &utils.StreamUsage{
		Model:        firstNonEmptyString(strings.TrimSpace(gjson.GetBytes(body, "modelVersion").String()), strings.TrimSpace(gjson.GetBytes(body, "model").String())),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
}

func parseGenericJSONUsage(body []byte) *utils.StreamUsage {
	return &utils.StreamUsage{
		Model: firstNonEmptyString(
			strings.TrimSpace(gjson.GetBytes(body, "model").String()),
			strings.TrimSpace(gjson.GetBytes(body, "modelVersion").String()),
		),
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
