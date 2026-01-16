package converters

import (
	"fmt"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/JillVernus/cc-bridge/internal/types"
)

// ============== Gemini API 转换器 ==============

// GeminiConverter 实现 Responses → Gemini API 转换
type GeminiConverter struct{}

// ToProviderRequest 将 Responses 请求转换为 Gemini 格式
func (c *GeminiConverter) ToProviderRequest(sess *session.Session, req *types.ResponsesRequest) (interface{}, error) {
	// 转换 messages
	contents, systemInstruction, err := ResponsesToGeminiContents(sess, req.Input, req.Instructions)
	if err != nil {
		return nil, err
	}

	// 构建 Gemini 请求
	geminiReq := map[string]interface{}{
		"contents": contents,
	}

	// Gemini 使用独立的 systemInstruction 参数
	if systemInstruction != "" {
		geminiReq["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{
				{"text": systemInstruction},
			},
		}
	}

	// 生成配置
	genConfig := map[string]interface{}{}

	if req.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		genConfig["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		genConfig["topP"] = req.TopP
	}
	if req.Stop != nil {
		genConfig["stopSequences"] = req.Stop
	}

	if len(genConfig) > 0 {
		geminiReq["generationConfig"] = genConfig
	}

	return geminiReq, nil
}

// FromProviderResponse 将 Gemini 响应转换为 Responses 格式
func (c *GeminiConverter) FromProviderResponse(resp map[string]interface{}, sessionID string) (*types.ResponsesResponse, error) {
	return GeminiResponseToResponses(resp, sessionID)
}

// GetProviderName 获取上游服务名称
func (c *GeminiConverter) GetProviderName() string {
	return "Gemini API"
}

// ============== Responses → Gemini Contents ==============

// ResponsesToGeminiContents 将 Responses 格式转换为 Gemini contents 格式
func ResponsesToGeminiContents(sess *session.Session, newInput interface{}, instructions string) ([]map[string]interface{}, string, error) {
	contents := []map[string]interface{}{}

	// 1. 处理历史消息
	for _, item := range sess.Messages {
		content := responsesItemToGeminiContent(item)
		if content != nil {
			contents = append(contents, content)
		}
	}

	// 2. 处理新输入
	newItems, err := parseResponsesInput(newInput)
	if err != nil {
		return nil, "", err
	}

	for _, item := range newItems {
		content := responsesItemToGeminiContent(item)
		if content != nil {
			contents = append(contents, content)
		}
	}

	return contents, instructions, nil
}

// responsesItemToGeminiContent 单个 ResponsesItem 转换为 Gemini content
func responsesItemToGeminiContent(item types.ResponsesItem) map[string]interface{} {
	switch item.Type {
	case "message":
		role := item.Role
		if role == "" {
			role = "user"
		}
		// Gemini uses "model" instead of "assistant"
		if role == "assistant" {
			role = "model"
		}

		contentText := extractTextFromContent(item.Content)
		if contentText == "" {
			return nil
		}

		return map[string]interface{}{
			"role": role,
			"parts": []map[string]string{
				{"text": contentText},
			},
		}

	case "text":
		contentStr := extractTextFromContent(item.Content)
		if contentStr == "" {
			return nil
		}

		role := "user"
		if item.Role != "" {
			role = item.Role
		}
		if role == "assistant" {
			role = "model"
		}

		return map[string]interface{}{
			"role": role,
			"parts": []map[string]string{
				{"text": contentStr},
			},
		}
	}

	return nil
}

// ============== Gemini Response → Responses ==============

// GeminiResponseToResponses 将 Gemini 响应转换为 Responses 格式
func GeminiResponseToResponses(geminiResp map[string]interface{}, sessionID string) (*types.ResponsesResponse, error) {
	// 提取 model（Gemini 响应中可能没有 model 字段）
	model, _ := geminiResp["model"].(string)

	// 提取 candidates
	candidates, _ := geminiResp["candidates"].([]interface{})

	output := []types.ResponsesItem{}
	if len(candidates) > 0 {
		candidate, ok := candidates[0].(map[string]interface{})
		if ok {
			content, _ := candidate["content"].(map[string]interface{})
			parts, _ := content["parts"].([]interface{})

			for _, p := range parts {
				part, ok := p.(map[string]interface{})
				if !ok {
					continue
				}

				// 文本内容
				if text, ok := part["text"].(string); ok {
					output = append(output, types.ResponsesItem{
						Type:    "text",
						Content: text,
					})
				}

				// 函数调用
				if fc, ok := part["functionCall"].(map[string]interface{}); ok {
					name, _ := fc["name"].(string)
					args := fc["args"]

					output = append(output, types.ResponsesItem{
						Type: "function_call",
						Content: map[string]interface{}{
							"name":      name,
							"arguments": args,
						},
					})
				}
			}
		}
	}

	// 提取 usage
	usage := types.ResponsesUsage{}
	if usageMetadata, ok := geminiResp["usageMetadata"].(map[string]interface{}); ok {
		if promptTokens, ok := usageMetadata["promptTokenCount"].(float64); ok {
			usage.PromptTokens = int(promptTokens)
		}
		if candidatesTokens, ok := usageMetadata["candidatesTokenCount"].(float64); ok {
			usage.CompletionTokens = int(candidatesTokens)
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	// 确定状态
	status := "completed"
	if len(candidates) > 0 {
		candidate, _ := candidates[0].(map[string]interface{})
		finishReason, _ := candidate["finishReason"].(string)
		if strings.Contains(strings.ToLower(finishReason), "error") {
			status = "failed"
		}
	}

	responseID := fmt.Sprintf("resp_%d", getCurrentTimestamp())

	return &types.ResponsesResponse{
		ID:         responseID,
		Model:      model,
		Output:     output,
		Status:     status,
		PreviousID: "",
		Usage:      usage,
	}, nil
}
