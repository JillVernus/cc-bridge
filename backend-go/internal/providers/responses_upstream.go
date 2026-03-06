package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// ResponsesUpstreamProvider 将 Claude Messages 请求转换为 Responses API 格式
// 用于 /v1/messages 端点转发到 Responses API 上游
type ResponsesUpstreamProvider struct{}

// responsesOptionalFields captures optional/raw request shapes that are needed by
// higher-fidelity Claude -> Responses mapping, while keeping backward compatibility.
type responsesOptionalFields struct {
	TopP          float64
	HasTopP       bool
	ToolChoice    interface{}
	HasToolChoice bool
	RawTools      []map[string]interface{}
}

// ConvertToProviderRequest 将 Claude Messages 请求转换为 Responses API 格式
func (p *ResponsesUpstreamProvider) ConvertToProviderRequest(c *gin.Context, upstream *config.UpstreamConfig, apiKey string) (*http.Request, []byte, error) {
	// 读取原始请求体
	originalBodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read request body: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(originalBodyBytes))

	var claudeReq types.ClaudeRequest
	if err := json.Unmarshal(originalBodyBytes, &claudeReq); err != nil {
		return nil, originalBodyBytes, fmt.Errorf("failed to parse Claude request: %w", err)
	}

	// 同步保留原始 map 形态，供可选字段/tool shape 提取（不影响现有兼容行为）
	var rawRequest map[string]interface{}
	if len(originalBodyBytes) > 0 {
		_ = json.Unmarshal(originalBodyBytes, &rawRequest)
	}

	// 转换为 Responses API 格式
	responsesReq := p.convertToResponsesRequest(&claudeReq, upstream, rawRequest)
	if promptCacheKey := extractPromptCacheKeyForResponsesBridge(c, rawRequest); promptCacheKey != "" {
		if existing, _ := responsesReq["prompt_cache_key"].(string); strings.TrimSpace(existing) == "" {
			responsesReq["prompt_cache_key"] = promptCacheKey
		}
	}

	reqBodyBytes, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, originalBodyBytes, fmt.Errorf("failed to marshal Responses request body: %w", err)
	}

	// 构建目标 URL
	targetURL := p.buildTargetURL(upstream)

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, originalBodyBytes, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header = utils.PrepareUpstreamHeaders(c, req.URL.Host)
	utils.SetAuthenticationHeader(req.Header, apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, originalBodyBytes, nil
}

func extractPromptCacheKeyForResponsesBridge(c *gin.Context, rawRequest map[string]interface{}) string {
	if rawRequest != nil {
		if existingKey, ok := rawRequest["prompt_cache_key"].(string); ok {
			if trimmed := strings.TrimSpace(existingKey); trimmed != "" {
				return trimmed
			}
		}
	}

	if c != nil {
		if sessionID := strings.TrimSpace(c.GetHeader("Session_id")); sessionID != "" {
			return sessionID
		}
	}

	if rawRequest != nil {
		if metadata, ok := rawRequest["metadata"].(map[string]interface{}); ok {
			if userID, ok := metadata["user_id"].(string); ok {
				if parsedSession := extractSessionIDFromCompoundUserID(userID); parsedSession != "" {
					return parsedSession
				}
			}
		}
	}

	if c != nil {
		if conversationID := strings.TrimSpace(c.GetHeader("Conversation_id")); conversationID != "" {
			return conversationID
		}
	}

	return ""
}

func extractSessionIDFromCompoundUserID(compoundUserID string) string {
	compoundUserID = strings.TrimSpace(compoundUserID)
	if compoundUserID == "" {
		return ""
	}

	const delimiter = "_account__session_"
	idx := strings.Index(compoundUserID, delimiter)
	if idx == -1 {
		return ""
	}

	sessionID := strings.TrimSpace(compoundUserID[idx+len(delimiter):])
	if sessionID == "" {
		return ""
	}

	return sessionID
}

// convertToResponsesRequest 将 Claude 请求转换为 Responses 请求
func (p *ResponsesUpstreamProvider) convertToResponsesRequest(
	claudeReq *types.ClaudeRequest,
	upstream *config.UpstreamConfig,
	rawRequest map[string]interface{},
) map[string]interface{} {
	optionalFields := p.extractOptionalFields(claudeReq, rawRequest)
	redirectedModel := config.RedirectModel(claudeReq.Model, upstream)
	reasoningFromModelSuffix := map[string]interface{}(nil)
	if baseModel, reasoning, changed := splitResponsesModelThinkingSuffix(redirectedModel); changed {
		redirectedModel = baseModel
		reasoningFromModelSuffix = reasoning
	}

	req := map[string]interface{}{
		"model":  redirectedModel,
		"stream": claudeReq.Stream,
	}

	// 转换 system 为 instructions
	if claudeReq.System != nil {
		systemText := extractSystemText(claudeReq.System)
		if systemText != "" {
			req["instructions"] = systemText
		}
	}

	// 转换 messages 为 input
	input := p.convertMessagesToInput(claudeReq.Messages)
	req["input"] = input

	// 复制其他参数
	if claudeReq.MaxTokens > 0 {
		req["max_output_tokens"] = claudeReq.MaxTokens
	}
	if claudeReq.Temperature > 0 {
		req["temperature"] = claudeReq.Temperature
	}
	if optionalFields.HasTopP && optionalFields.TopP > 0 {
		req["top_p"] = optionalFields.TopP
	}
	if reasoning, ok := p.convertThinkingToReasoning(claudeReq.Thinking); ok {
		req["reasoning"] = reasoning
	}
	// Thinking suffix from model mapping (e.g. gpt-5.3-codex(xhigh)) should override
	// per-request defaults for Responses upstream compatibility.
	if reasoningFromModelSuffix != nil {
		req["reasoning"] = reasoningFromModelSuffix
	}

	// 转换 tools
	convertedTools := []map[string]interface{}{}
	if len(claudeReq.Tools) > 0 {
		convertedTools = p.convertTools(claudeReq.Tools, optionalFields.RawTools)
		if len(convertedTools) > 0 {
			req["tools"] = convertedTools
		}
	}
	if toolChoice, ok := p.convertToolChoice(optionalFields); ok {
		req["tool_choice"] = toolChoice
	} else if len(convertedTools) > 0 {
		// Claude Messages defaults to automatic tool selection when tools are present.
		// Mirror that default explicitly for Responses to keep tool behavior consistent.
		req["tool_choice"] = "auto"
	}

	return req
}

func (p *ResponsesUpstreamProvider) extractOptionalFields(
	claudeReq *types.ClaudeRequest,
	rawRequest map[string]interface{},
) responsesOptionalFields {
	result := responsesOptionalFields{}

	if claudeReq.TopP > 0 {
		result.TopP = claudeReq.TopP
		result.HasTopP = true
	}

	if claudeReq.ToolChoice != nil {
		result.ToolChoice = claudeReq.ToolChoice
		result.HasToolChoice = true
	}

	if rawRequest == nil {
		return result
	}

	if !result.HasTopP {
		if topP, ok := parseJSONNumber(rawRequest["top_p"]); ok {
			result.TopP = topP
			result.HasTopP = true
		}
	}

	if !result.HasToolChoice {
		if toolChoice, ok := rawRequest["tool_choice"]; ok {
			result.ToolChoice = toolChoice
			result.HasToolChoice = true
		}
	}

	if rawTools, ok := rawRequest["tools"].([]interface{}); ok {
		for _, rawTool := range rawTools {
			toolMap, ok := rawTool.(map[string]interface{})
			if !ok {
				continue
			}
			result.RawTools = append(result.RawTools, toolMap)
		}
	}

	return result
}

func parseJSONNumber(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func splitResponsesModelThinkingSuffix(model string) (baseModel string, reasoning map[string]interface{}, changed bool) {
	base, suffix, ok := utils.SplitModelSuffix(model)
	if !ok {
		return model, nil, false
	}

	normalized := utils.NormalizeThinkingSuffix(suffix)
	if !utils.IsSupportedThinkingSuffix(normalized) || normalized == "" {
		// Keep unknown suffixes as part of model for backward compatibility.
		return model, nil, false
	}

	trimmedBase := strings.TrimSpace(base)
	if trimmedBase == "" {
		return model, nil, false
	}

	return trimmedBase, map[string]interface{}{
		"effort": normalized,
	}, true
}

func parseJSONInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}

func normalizeReasoningEffort(effort string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(effort)), true
	default:
		return "", false
	}
}

func formatResponsesDisplayModel(model string, reasoningEffort string) string {
	baseModel := strings.TrimSpace(model)
	if baseModel == "" {
		baseModel = "responses"
	}

	effort, ok := normalizeReasoningEffort(reasoningEffort)
	if !ok || effort == "" || effort == "none" || effort == "minimal" {
		return baseModel
	}

	// Keep display stable when upstream model already contains the same suffix.
	if strippedBase, suffix, hasSuffix := utils.SplitModelSuffix(baseModel); hasSuffix {
		normalizedSuffix := utils.NormalizeThinkingSuffix(suffix)
		if normalizedSuffix == effort {
			baseModel = strings.TrimSpace(strippedBase)
			if baseModel == "" {
				baseModel = "responses"
			}
		} else {
			return baseModel
		}
	}

	return fmt.Sprintf("%s (%s)", baseModel, effort)
}

// convertMessagesToInput 将 Claude messages 转换为 Responses input
func (p *ResponsesUpstreamProvider) convertMessagesToInput(messages []types.ClaudeMessage) []map[string]interface{} {
	input := []map[string]interface{}{}

	for _, msg := range messages {
		items := p.convertMessageToInputItems(msg)
		if len(items) > 0 {
			input = append(input, items...)
		}
	}

	return input
}

// convertMessageToInputItems 将单个 Claude message 转换为一个或多个 Responses input items
// 规则：文本块按原顺序聚合；遇到 tool_use/tool_result 先 flush 文本，再追加工具项。
func (p *ResponsesUpstreamProvider) convertMessageToInputItems(msg types.ClaudeMessage) []map[string]interface{} {
	// 处理字符串内容
	if str, ok := msg.Content.(string); ok {
		return []map[string]interface{}{{
			"type":    "message",
			"role":    msg.Role,
			"content": str,
		}}
	}

	// 处理内容数组
	contents, ok := msg.Content.([]interface{})
	if !ok {
		return nil
	}

	items := []map[string]interface{}{}
	var textParts []string
	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		items = append(items, map[string]interface{}{
			"type":    "message",
			"role":    msg.Role,
			"content": strings.Join(textParts, "\n"),
		})
		textParts = textParts[:0]
	}

	for _, c := range contents {
		content, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		contentType, _ := content["type"].(string)

		switch contentType {
		case "text":
			if text, ok := content["text"].(string); ok {
				textParts = append(textParts, text)
			}

		case "tool_use":
			flushText()
			id, _ := content["id"].(string)
			name, _ := content["name"].(string)
			input := content["input"]

			// 将 input 序列化为 JSON 字符串
			inputJSON, _ := json.Marshal(input)

			items = append(items, map[string]interface{}{
				"type":      "function_call",
				"call_id":   id,
				"name":      name,
				"arguments": string(inputJSON),
			})

		case "tool_result":
			flushText()
			toolUseID, _ := content["tool_use_id"].(string)
			resultContent := content["content"]
			isError, _ := content["is_error"].(bool)

			var output string
			if isError {
				// Preserve Claude tool_result error semantics in a structured way so
				// upstream models can distinguish failures from normal tool outputs.
				errorPayload := map[string]interface{}{
					"is_error": true,
					"content":  resultContent,
				}
				outputJSON, _ := json.Marshal(errorPayload)
				output = string(outputJSON)
			} else {
				if str, ok := resultContent.(string); ok {
					output = str
				} else {
					outputJSON, _ := json.Marshal(resultContent)
					output = string(outputJSON)
				}
			}

			items = append(items, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": toolUseID,
				"output":  output,
			})
		}
	}

	flushText()
	return items
}

// convertTools 将 Claude tools 转换为 Responses tools
func (p *ResponsesUpstreamProvider) convertTools(claudeTools []types.ClaudeTool, rawTools []map[string]interface{}) []map[string]interface{} {
	tools := []map[string]interface{}{}

	// 优先使用 raw tools：可保留 struct 无法表达的内置工具形态
	if len(rawTools) > 0 {
		for _, rawTool := range rawTools {
			converted, ok := p.convertRawTool(rawTool)
			if ok {
				tools = append(tools, converted)
			}
		}
		return tools
	}

	for _, tool := range claudeTools {
		converted, ok := p.convertTypedTool(tool)
		if ok {
			tools = append(tools, converted)
		}
	}

	return tools
}

func (p *ResponsesUpstreamProvider) convertTypedTool(tool types.ClaudeTool) (map[string]interface{}, bool) {
	toolType := normalizeToolType(tool.Type)
	if isWebSearchToolType(toolType) {
		return map[string]interface{}{
			"type": "web_search_preview",
		}, true
	}

	if toolType != "" && toolType != "function" {
		log.Printf("⚠️ [Messages->Responses] unsupported typed tool dropped: type=%q", tool.Type)
		return nil, false
	}

	name := strings.TrimSpace(tool.Name)
	if name == "" || tool.InputSchema == nil {
		log.Printf("⚠️ [Messages->Responses] malformed function tool dropped (typed): name=%q has_input_schema=%t", name, tool.InputSchema != nil)
		return nil, false
	}

	converted := map[string]interface{}{
		"type":       "function",
		"name":       name,
		"parameters": tool.InputSchema,
	}
	if description := strings.TrimSpace(tool.Description); description != "" {
		converted["description"] = description
	}

	return converted, true
}

func (p *ResponsesUpstreamProvider) convertRawTool(rawTool map[string]interface{}) (map[string]interface{}, bool) {
	rawType, _ := rawTool["type"].(string)
	toolType := normalizeToolType(rawType)

	if isWebSearchToolType(toolType) {
		return map[string]interface{}{
			"type": "web_search_preview",
		}, true
	}

	if toolType != "" && toolType != "function" {
		log.Printf("⚠️ [Messages->Responses] unsupported raw tool dropped: type=%q", rawType)
		return nil, false
	}

	name, _ := rawTool["name"].(string)
	name = strings.TrimSpace(name)

	parameters := rawTool["input_schema"]
	if parameters == nil {
		parameters = rawTool["parameters"]
	}

	if name == "" || parameters == nil {
		log.Printf("⚠️ [Messages->Responses] malformed function tool dropped (raw): name=%q has_parameters=%t", name, parameters != nil)
		return nil, false
	}

	converted := map[string]interface{}{
		"type":       "function",
		"name":       name,
		"parameters": parameters,
	}
	if description, _ := rawTool["description"].(string); strings.TrimSpace(description) != "" {
		converted["description"] = strings.TrimSpace(description)
	}

	return converted, true
}

func (p *ResponsesUpstreamProvider) convertToolChoice(optionalFields responsesOptionalFields) (interface{}, bool) {
	if !optionalFields.HasToolChoice {
		return nil, false
	}

	switch value := optionalFields.ToolChoice.(type) {
	case string:
		return normalizeStringToolChoice(value)
	case map[string]interface{}:
		return convertObjectToolChoice(value)
	default:
		log.Printf("⚠️ [Messages->Responses] unsupported tool_choice dropped: %T", optionalFields.ToolChoice)
		return nil, false
	}
}

func normalizeStringToolChoice(choice string) (interface{}, bool) {
	normalized := strings.ToLower(strings.TrimSpace(choice))
	if normalized == "" {
		return nil, false
	}

	switch normalized {
	case "auto", "none":
		return normalized, true
	case "any", "required":
		return "required", true
	default:
		return map[string]interface{}{
			"type": "function",
			"name": choice,
		}, true
	}
}

func convertObjectToolChoice(choice map[string]interface{}) (interface{}, bool) {
	if functionObj, ok := choice["function"].(map[string]interface{}); ok {
		if name, _ := functionObj["name"].(string); strings.TrimSpace(name) != "" {
			return map[string]interface{}{
				"type": "function",
				"name": strings.TrimSpace(name),
			}, true
		}
	}

	if name, _ := choice["name"].(string); strings.TrimSpace(name) != "" {
		return map[string]interface{}{
			"type": "function",
			"name": strings.TrimSpace(name),
		}, true
	}

	typeValue, _ := choice["type"].(string)
	normalizedType := normalizeToolType(typeValue)
	switch normalizedType {
	case "auto", "none":
		return normalizedType, true
	case "any", "required":
		return "required", true
	case "tool", "function":
		log.Printf("⚠️ [Messages->Responses] tool_choice object missing function name, dropped: type=%q", typeValue)
		return nil, false
	case "":
		log.Printf("⚠️ [Messages->Responses] tool_choice object missing type/name, dropped")
		return nil, false
	default:
		log.Printf("⚠️ [Messages->Responses] unsupported tool_choice object type dropped: %q", typeValue)
		return nil, false
	}
}

func normalizeToolType(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

func isWebSearchToolType(toolType string) bool {
	if toolType == "web_search" || toolType == "web_search_preview" {
		return true
	}
	return strings.HasPrefix(toolType, "web_search_")
}

func (p *ResponsesUpstreamProvider) convertThinkingToReasoning(thinking *types.ClaudeThinking) (map[string]interface{}, bool) {
	if thinking == nil {
		return nil, false
	}

	if normalizeToolType(thinking.Type) != "enabled" || thinking.BudgetTokens <= 0 {
		return nil, false
	}

	effort := "xhigh"
	if normalized, ok := normalizeReasoningEffort(thinking.Effort); ok {
		effort = normalized
	}

	return map[string]interface{}{
		"effort": effort,
	}, true
}

func convertResponsesUsageToClaudeUsage(usageMap map[string]interface{}) *types.Usage {
	usage := &types.Usage{}
	hasUsage := false

	rawInput, hasInput := parseJSONInt(usageMap["input_tokens"])
	if !hasInput {
		rawInput, hasInput = parseJSONInt(usageMap["prompt_tokens"])
	}

	outputTokens, hasOutput := parseJSONInt(usageMap["output_tokens"])
	if !hasOutput {
		outputTokens, hasOutput = parseJSONInt(usageMap["completion_tokens"])
	}

	cacheCreation, hasCacheCreation := parseJSONInt(usageMap["cache_creation_input_tokens"])
	cacheRead, hasCacheRead := parseJSONInt(usageMap["cache_read_input_tokens"])

	if details, ok := usageMap["input_tokens_details"].(map[string]interface{}); ok {
		if cachedTokens, ok := parseJSONInt(details["cached_tokens"]); ok {
			cacheRead = cachedTokens
			hasCacheRead = true
		}
	}

	if hasInput {
		usage.InputTokens = rawInput
		if hasCacheRead && cacheRead > 0 {
			usage.InputTokens = rawInput - cacheRead
			if usage.InputTokens < 0 {
				usage.InputTokens = 0
			}
		}
		hasUsage = true
	}

	if hasOutput {
		usage.OutputTokens = outputTokens
		hasUsage = true
	}

	if hasCacheCreation && cacheCreation > 0 {
		usage.CacheCreationInputTokens = cacheCreation
		hasUsage = true
	}
	if hasCacheRead && cacheRead > 0 {
		usage.CacheReadInputTokens = cacheRead
		hasUsage = true
	}

	if totalTokens, ok := parseJSONInt(usageMap["total_tokens"]); ok {
		usage.TotalTokens = totalTokens
		hasUsage = true
	} else if hasUsage {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	}

	if !hasUsage {
		return nil
	}

	return usage
}

// buildTargetURL 构建目标 URL
func (p *ResponsesUpstreamProvider) buildTargetURL(upstream *config.UpstreamConfig) string {
	baseURL := strings.TrimSuffix(upstream.BaseURL, "/")

	// 检测 baseURL 是否以版本号结尾
	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)
	hasVersionSuffix := versionPattern.MatchString(baseURL)

	if hasVersionSuffix {
		return baseURL + "/responses"
	}
	return baseURL + "/v1/responses"
}

// ConvertToClaudeResponse 将 Responses 响应转换为 Claude 响应
func (p *ResponsesUpstreamProvider) ConvertToClaudeResponse(providerResp *types.ProviderResponse) (*types.ClaudeResponse, error) {
	var responsesResp map[string]interface{}
	if err := json.Unmarshal(providerResp.Body, &responsesResp); err != nil {
		return nil, err
	}

	claudeResp := &types.ClaudeResponse{
		ID:      generateID(),
		Type:    "message",
		Role:    "assistant",
		Content: []types.ClaudeContent{},
	}

	// 转换 output 为 content
	if output, ok := responsesResp["output"].([]interface{}); ok {
		for _, item := range output {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)

			switch itemType {
			case "message":
				// 提取 content
				if content, ok := itemMap["content"].([]interface{}); ok {
					for _, c := range content {
						if cMap, ok := c.(map[string]interface{}); ok {
							if cType, _ := cMap["type"].(string); cType == "output_text" {
								if text, ok := cMap["text"].(string); ok {
									claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
										Type: "text",
										Text: text,
									})
								}
							}
						}
					}
				} else if contentStr, ok := itemMap["content"].(string); ok {
					claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
						Type: "text",
						Text: contentStr,
					})
				}

			case "text":
				if content, ok := itemMap["content"].(string); ok {
					claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
						Type: "text",
						Text: content,
					})
				}

			case "function_call":
				name, _ := itemMap["name"].(string)
				callID, _ := itemMap["call_id"].(string)
				arguments, _ := itemMap["arguments"].(string)

				var input interface{}
				if err := json.Unmarshal([]byte(arguments), &input); err != nil {
					input = arguments
				}

				claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
					Type:  "tool_use",
					ID:    callID,
					Name:  name,
					Input: input,
				})
			}
		}
	}

	// 设置 stop_reason
	if status, ok := responsesResp["status"].(string); ok {
		if status == "completed" {
			// 检查是否有 tool_use
			hasToolUse := false
			for _, c := range claudeResp.Content {
				if c.Type == "tool_use" {
					hasToolUse = true
					break
				}
			}
			if hasToolUse {
				claudeResp.StopReason = "tool_use"
			} else {
				claudeResp.StopReason = "end_turn"
			}
		}
	}

	// 转换 usage
	if usageMap, ok := responsesResp["usage"].(map[string]interface{}); ok {
		if usage := convertResponsesUsageToClaudeUsage(usageMap); usage != nil {
			claudeResp.Usage = usage
		}
	}

	return claudeResp, nil
}

// HandleStreamResponse 处理流式响应
func (p *ResponsesUpstreamProvider) HandleStreamResponse(body io.ReadCloser) (<-chan string, <-chan error, error) {
	eventChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		const maxCapacity = 4 * 1024 * 1024
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		textBlockStarted := false
		textBlockIndex := -1
		nextBlockIndex := 0
		messageStarted := false
		messageEnded := false
		messageID := generateID()
		messageModel := "responses"
		messageReasoningEffort := ""
		toolUseSeen := false
		toolBlockOrder := make([]string, 0, 4)
		toolBlockIndexByItemID := map[string]int{}
		toolBlockClosedByItemID := map[string]bool{}
		toolBlockHasDeltaByItemID := map[string]bool{}
		toolCallNameByItemID := map[string]string{}
		toolCallIDByItemID := map[string]string{}

		emit := func(event string, data interface{}) {
			eventJSON, _ := json.Marshal(data)
			eventChan <- fmt.Sprintf("event: %s\ndata: %s\n\n", event, eventJSON)
		}

		extractReasoningEffort := func(payload map[string]interface{}) string {
			if payload == nil {
				return ""
			}
			reasoning, ok := payload["reasoning"].(map[string]interface{})
			if !ok {
				return ""
			}
			rawEffort, ok := reasoning["effort"].(string)
			if !ok {
				return ""
			}
			if effort, ok := normalizeReasoningEffort(rawEffort); ok {
				return effort
			}
			return ""
		}

		emitMessageStart := func() {
			if messageStarted {
				return
			}

			startEvent := map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            messageID,
					"type":          "message",
					"role":          "assistant",
					"model":         formatResponsesDisplayModel(messageModel, messageReasoningEffort),
					"content":       []interface{}{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]int{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			}
			emit("message_start", startEvent)
			messageStarted = true
		}

		closeTextBlock := func() {
			if !textBlockStarted {
				return
			}
			stopEvent := map[string]interface{}{
				"type":  "content_block_stop",
				"index": textBlockIndex,
			}
			emit("content_block_stop", stopEvent)
			textBlockStarted = false
			textBlockIndex = -1
		}

		emitToolStart := func(itemID string, callID string, name string) string {
			itemID = strings.TrimSpace(itemID)
			callID = strings.TrimSpace(callID)
			name = strings.TrimSpace(name)

			if itemID == "" {
				if callID != "" {
					itemID = callID
				} else {
					itemID = fmt.Sprintf("fc_%s", generateID())
				}
			}
			if callID == "" {
				callID = itemID
			}
			if name == "" {
				name = "tool"
			}

			toolCallNameByItemID[itemID] = name
			toolCallIDByItemID[itemID] = callID
			if _, exists := toolBlockIndexByItemID[itemID]; exists {
				return itemID
			}

			closeTextBlock()

			blockIndex := nextBlockIndex
			nextBlockIndex++
			startEvent := map[string]interface{}{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    callID,
					"name":  name,
					"input": map[string]interface{}{},
				},
			}
			emit("content_block_start", startEvent)
			toolBlockOrder = append(toolBlockOrder, itemID)
			toolBlockIndexByItemID[itemID] = blockIndex
			toolUseSeen = true
			return itemID
		}

		emitToolDelta := func(itemID string, partialJSON string) {
			itemID = strings.TrimSpace(itemID)
			if itemID == "" {
				return
			}
			blockIndex, exists := toolBlockIndexByItemID[itemID]
			if !exists {
				itemID = emitToolStart(itemID, toolCallIDByItemID[itemID], toolCallNameByItemID[itemID])
				blockIndex = toolBlockIndexByItemID[itemID]
			}
			deltaEvent := map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]string{
					"type":         "input_json_delta",
					"partial_json": partialJSON,
				},
			}
			emit("content_block_delta", deltaEvent)
			toolBlockHasDeltaByItemID[itemID] = true
			toolUseSeen = true
		}

		closeToolBlock := func(itemID string) {
			itemID = strings.TrimSpace(itemID)
			if itemID == "" {
				return
			}
			blockIndex, exists := toolBlockIndexByItemID[itemID]
			if !exists || toolBlockClosedByItemID[itemID] {
				return
			}
			stopEvent := map[string]interface{}{
				"type":  "content_block_stop",
				"index": blockIndex,
			}
			emit("content_block_stop", stopEvent)
			toolBlockClosedByItemID[itemID] = true
		}

		closeAllToolBlocks := func() {
			for _, itemID := range toolBlockOrder {
				closeToolBlock(itemID)
			}
		}

		emitMessageStop := func(stopReason string, usage *types.Usage) {
			if messageEnded {
				return
			}
			if stopReason == "" {
				stopReason = "end_turn"
			}
			if stopReason != "tool_use" && toolUseSeen {
				stopReason = "tool_use"
			}

			// Ensure block/message envelope is always well-formed for Claude clients.
			closeTextBlock()
			closeAllToolBlocks()

			deltaEvent := map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   stopReason,
					"stop_sequence": nil,
				},
			}
			if usage != nil {
				usageMap := map[string]int{}
				if usage.InputTokens > 0 {
					usageMap["input_tokens"] = usage.InputTokens
				}
				if usage.OutputTokens > 0 {
					usageMap["output_tokens"] = usage.OutputTokens
				}
				if usage.CacheCreationInputTokens > 0 {
					usageMap["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
				}
				if usage.CacheReadInputTokens > 0 {
					usageMap["cache_read_input_tokens"] = usage.CacheReadInputTokens
				}
				if usage.TotalTokens > 0 {
					usageMap["total_tokens"] = usage.TotalTokens
				}
				if len(usageMap) > 0 {
					deltaEvent["usage"] = usageMap
				}
			}
			emit("message_delta", deltaEvent)
			emit("message_stop", map[string]interface{}{"type": "message_stop"})
			messageEnded = true
		}

		extractCompletedMeta := func(chunk map[string]interface{}) (string, *types.Usage, string, string) {
			stopReason := "end_turn"
			var usage *types.Usage
			model := ""
			reasoningEffort := extractReasoningEffort(chunk)

			if m, ok := chunk["model"].(string); ok && strings.TrimSpace(m) != "" {
				model = strings.TrimSpace(m)
			}

			responseObj, _ := chunk["response"].(map[string]interface{})
			if responseObj != nil {
				if m, ok := responseObj["model"].(string); ok && strings.TrimSpace(m) != "" {
					model = strings.TrimSpace(m)
				}
				if effort := extractReasoningEffort(responseObj); effort != "" {
					reasoningEffort = effort
				}
				if usageMap, ok := responseObj["usage"].(map[string]interface{}); ok {
					usage = convertResponsesUsageToClaudeUsage(usageMap)
				}

				if output, ok := responseObj["output"].([]interface{}); ok {
					for _, item := range output {
						itemMap, ok := item.(map[string]interface{})
						if !ok {
							continue
						}
						if itemType, _ := itemMap["type"].(string); itemType == "function_call" {
							stopReason = "tool_use"
							break
						}
					}
				}
			} else if usageMap, ok := chunk["usage"].(map[string]interface{}); ok {
				usage = convertResponsesUsageToClaudeUsage(usageMap)
			}

			return model, usage, stopReason, reasoningEffort
		}

		extractEventModel := func(chunk map[string]interface{}) string {
			if m, ok := chunk["model"].(string); ok && strings.TrimSpace(m) != "" {
				return strings.TrimSpace(m)
			}
			if responseObj, ok := chunk["response"].(map[string]interface{}); ok {
				if m, ok := responseObj["model"].(string); ok && strings.TrimSpace(m) != "" {
					return strings.TrimSpace(m)
				}
			}
			return ""
		}

		extractEventReasoningEffort := func(chunk map[string]interface{}) string {
			if effort := extractReasoningEffort(chunk); effort != "" {
				return effort
			}
			if responseObj, ok := chunk["response"].(map[string]interface{}); ok {
				if effort := extractReasoningEffort(responseObj); effort != "" {
					return effort
				}
			}
			return ""
		}

		for scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimSpace(line)

			if line == "" {
				continue
			}
			if line == "data: [DONE]" {
				if messageStarted {
					stopReason := "end_turn"
					if toolUseSeen {
						stopReason = "tool_use"
					}
					emitMessageStop(stopReason, nil)
				}
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			jsonStr := strings.TrimPrefix(line, "data: ")

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
				continue
			}

			// 处理 Responses API 流式事件
			eventType, _ := chunk["type"].(string)

			switch eventType {
			case "response.created", "response.in_progress":
				model := extractEventModel(chunk)
				effort := extractEventReasoningEffort(chunk)
				if model != "" {
					messageModel = model
				}
				if effort != "" {
					messageReasoningEffort = effort
				}

			case "response.output_text.delta":
				// 文本增量
				if delta, ok := chunk["delta"].(string); ok {
					if model := extractEventModel(chunk); model != "" {
						messageModel = model
					}
					if effort := extractEventReasoningEffort(chunk); effort != "" {
						messageReasoningEffort = effort
					}
					emitMessageStart()
					if !textBlockStarted {
						// 发送 content_block_start
						textBlockIndex = nextBlockIndex
						nextBlockIndex++
						startEvent := map[string]interface{}{
							"type":  "content_block_start",
							"index": textBlockIndex,
							"content_block": map[string]string{
								"type": "text",
								"text": "",
							},
						}
						emit("content_block_start", startEvent)
						textBlockStarted = true
					}

					// 发送 content_block_delta
					deltaEvent := map[string]interface{}{
						"type":  "content_block_delta",
						"index": textBlockIndex,
						"delta": map[string]string{
							"type": "text_delta",
							"text": delta,
						},
					}
					emit("content_block_delta", deltaEvent)
				}

			case "response.output_item.added":
				itemMap, _ := chunk["item"].(map[string]interface{})
				itemType, _ := itemMap["type"].(string)
				if itemType == "function_call" {
					itemID, _ := itemMap["id"].(string)
					callID, _ := itemMap["call_id"].(string)
					name, _ := itemMap["name"].(string)
					arguments, _ := itemMap["arguments"].(string)
					emitMessageStart()
					itemID = emitToolStart(itemID, callID, name)
					if arguments != "" {
						emitToolDelta(itemID, arguments)
					}
				}

			case "response.function_call_arguments.delta":
				itemID, _ := chunk["item_id"].(string)
				delta, _ := chunk["delta"].(string)
				emitMessageStart()
				emitToolDelta(itemID, delta)

			case "response.function_call_arguments.done":
				itemID, _ := chunk["item_id"].(string)
				arguments, _ := chunk["arguments"].(string)
				emitMessageStart()
				itemID = emitToolStart(itemID, toolCallIDByItemID[itemID], toolCallNameByItemID[itemID])
				if arguments != "" && !toolBlockHasDeltaByItemID[itemID] {
					emitToolDelta(itemID, arguments)
				}
				closeToolBlock(itemID)

			case "response.output_item.done":
				itemMap, _ := chunk["item"].(map[string]interface{})
				itemType, _ := itemMap["type"].(string)
				if itemType == "function_call" {
					itemID, _ := itemMap["id"].(string)
					callID, _ := itemMap["call_id"].(string)
					name, _ := itemMap["name"].(string)
					arguments, _ := itemMap["arguments"].(string)
					emitMessageStart()
					itemID = emitToolStart(itemID, callID, name)
					if arguments != "" && !toolBlockHasDeltaByItemID[itemID] {
						emitToolDelta(itemID, arguments)
					}
					closeToolBlock(itemID)
				}

			case "response.completed", "response.done":
				model, usage, stopReason, reasoningEffort := extractCompletedMeta(chunk)
				if strings.TrimSpace(model) != "" {
					messageModel = strings.TrimSpace(model)
				}
				if reasoningEffort != "" {
					messageReasoningEffort = reasoningEffort
				}
				emitMessageStart()
				if usage != nil && usage.OutputTokens == 0 && (textBlockStarted || toolUseSeen) {
					// Keep streaming clients happy when output token count isn't provided.
					usage.OutputTokens = 1
				}
				emitMessageStop(stopReason, usage)
				// Some Responses-compatible upstreams do not close the SSE connection
				// promptly after the terminal event. Once we have emitted Claude's
				// terminal envelope, close this bridge stream immediately so
				// downstream handlers can finalize request logs instead of leaving
				// rows stuck in pending until timeout/EOF.
				return
			}
		}

		if messageStarted && !messageEnded {
			stopReason := "end_turn"
			if toolUseSeen {
				stopReason = "tool_use"
			}
			emitMessageStop(stopReason, nil)
		}

		if err := scanner.Err(); err != nil {
			errChan <- err
		}
	}()

	return eventChan, errChan, nil
}
