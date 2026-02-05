package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	// 转换为 Responses API 格式
	responsesReq := p.convertToResponsesRequest(&claudeReq, upstream)

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
func (p *ResponsesUpstreamProvider) convertToResponsesRequest(claudeReq *types.ClaudeRequest, upstream *config.UpstreamConfig) map[string]interface{} {
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

	// 转换 tools
	if len(claudeReq.Tools) > 0 {
		req["tools"] = p.convertTools(claudeReq.Tools)
	}

	return req
}

// convertMessagesToInput 将 Claude messages 转换为 Responses input
func (p *ResponsesUpstreamProvider) convertMessagesToInput(messages []types.ClaudeMessage) []map[string]interface{} {
	input := []map[string]interface{}{}

	for _, msg := range messages {
		item := p.convertMessageToInputItem(msg)
		if item != nil {
			input = append(input, item)
		}
	}

	return input
}

// convertMessageToInputItem 将单个 Claude message 转换为 Responses input item
func (p *ResponsesUpstreamProvider) convertMessageToInputItem(msg types.ClaudeMessage) map[string]interface{} {
	// 处理字符串内容
	if str, ok := msg.Content.(string); ok {
		return map[string]interface{}{
			"type":    "message",
			"role":    msg.Role,
			"content": str,
		}
	}

	// 处理内容数组
	contents, ok := msg.Content.([]interface{})
	if !ok {
		return nil
	}

	// 提取文本内容
	var textParts []string
	var toolCalls []map[string]interface{}
	var toolResults []map[string]interface{}

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
			id, _ := content["id"].(string)
			name, _ := content["name"].(string)
			input := content["input"]

			// 将 input 序列化为 JSON 字符串
			inputJSON, _ := json.Marshal(input)

			toolCalls = append(toolCalls, map[string]interface{}{
				"type":      "function_call",
				"call_id":   id,
				"name":      name,
				"arguments": string(inputJSON),
			})

		case "tool_result":
			toolUseID, _ := content["tool_use_id"].(string)
			resultContent := content["content"]

			var output string
			if str, ok := resultContent.(string); ok {
				output = str
			} else {
				outputJSON, _ := json.Marshal(resultContent)
				output = string(outputJSON)
			}

			toolResults = append(toolResults, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": toolUseID,
				"output":  output,
			})
		}
	}

	// 如果有文本内容，创建 message item
	if len(textParts) > 0 {
		return map[string]interface{}{
			"type":    "message",
			"role":    msg.Role,
			"content": strings.Join(textParts, "\n"),
		}
	}

	// 如果有 tool_use，返回 function_call items
	if len(toolCalls) > 0 {
		// 返回第一个 tool call（Responses API 通常一次处理一个）
		return toolCalls[0]
	}

	// 如果有 tool_result，返回 function_call_output items
	if len(toolResults) > 0 {
		return toolResults[0]
	}

	return nil
}

// convertTools 将 Claude tools 转换为 Responses tools
func (p *ResponsesUpstreamProvider) convertTools(claudeTools []types.ClaudeTool) []map[string]interface{} {
	tools := []map[string]interface{}{}

	for _, tool := range claudeTools {
		tools = append(tools, map[string]interface{}{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		})
	}

	return tools
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
