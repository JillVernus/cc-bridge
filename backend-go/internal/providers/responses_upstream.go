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

// convertToResponsesRequest 将 Claude 请求转换为 Responses 请求
func (p *ResponsesUpstreamProvider) convertToResponsesRequest(
	claudeReq *types.ClaudeRequest,
	upstream *config.UpstreamConfig,
	rawRequest map[string]interface{},
) map[string]interface{} {
	optionalFields := p.extractOptionalFields(claudeReq, rawRequest)

	req := map[string]interface{}{
		"model":  config.RedirectModel(claudeReq.Model, upstream),
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

	// 转换 tools
	if len(claudeReq.Tools) > 0 {
		req["tools"] = p.convertTools(claudeReq.Tools, optionalFields.RawTools)
	}
	if toolChoice, ok := p.convertToolChoice(optionalFields); ok {
		req["tool_choice"] = toolChoice
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

			var output string
			if str, ok := resultContent.(string); ok {
				output = str
			} else {
				outputJSON, _ := json.Marshal(resultContent)
				output = string(outputJSON)
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

	return map[string]interface{}{
		"effort": "high",
	}, true
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
		usage := &types.Usage{}
		if promptTokens, ok := usageMap["prompt_tokens"].(float64); ok {
			usage.InputTokens = int(promptTokens)
		}
		if completionTokens, ok := usageMap["completion_tokens"].(float64); ok {
			usage.OutputTokens = int(completionTokens)
		}
		claudeResp.Usage = usage
	}

	return claudeResp, nil
}

// HandleStreamResponse 处理流式响应
func (p *ResponsesUpstreamProvider) HandleStreamResponse(body io.ReadCloser) (<-chan string, <-chan error, error) {
	eventChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		const maxCapacity = 4 * 1024 * 1024
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		textBlockStarted := false
		textBlockIndex := 0

		for scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimSpace(line)

			if line == "" || line == "data: [DONE]" {
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
			case "response.output_text.delta":
				// 文本增量
				if delta, ok := chunk["delta"].(string); ok {
					if !textBlockStarted {
						// 发送 content_block_start
						startEvent := map[string]interface{}{
							"type":  "content_block_start",
							"index": textBlockIndex,
							"content_block": map[string]string{
								"type": "text",
								"text": "",
							},
						}
						startJSON, _ := json.Marshal(startEvent)
						eventChan <- fmt.Sprintf("event: content_block_start\ndata: %s\n\n", startJSON)
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
					deltaJSON, _ := json.Marshal(deltaEvent)
					eventChan <- fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", deltaJSON)
				}

			case "response.completed", "response.done":
				// 流结束
				if textBlockStarted {
					stopEvent := map[string]interface{}{
						"type":  "content_block_stop",
						"index": textBlockIndex,
					}
					stopJSON, _ := json.Marshal(stopEvent)
					eventChan <- fmt.Sprintf("event: content_block_stop\ndata: %s\n\n", stopJSON)
				}

				// 发送 message_delta
				event := map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]string{
						"stop_reason": "end_turn",
					},
				}
				eventJSON, _ := json.Marshal(event)
				eventChan <- fmt.Sprintf("event: message_delta\ndata: %s\n\n", eventJSON)
			}
		}

		// 确保流结束时关闭任何未关闭的文本块
		if textBlockStarted {
			stopEvent := map[string]interface{}{
				"type":  "content_block_stop",
				"index": textBlockIndex,
			}
			stopJSON, _ := json.Marshal(stopEvent)
			eventChan <- fmt.Sprintf("event: content_block_stop\ndata: %s\n\n", stopJSON)
		}

		if err := scanner.Err(); err != nil {
			errChan <- err
		}
	}()

	return eventChan, errChan, nil
}
