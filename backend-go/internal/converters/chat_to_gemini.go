package converters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/tidwall/gjson"
)

// ============================================================================
// Chat Completions → Gemini Converter
// Converts OpenAI Chat Completions format to Gemini generateContent format
// ============================================================================

// GeminiRequest represents Gemini generateContent request
type GeminiRequest struct {
	Contents          []GeminiContent         `json:"contents"`
	SystemInstruction *GeminiContent          `json:"systemInstruction,omitempty"`
	Tools             []GeminiTool            `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

// GeminiContent represents a content item
type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart represents a content part
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

// GeminiFunctionCall represents a function call
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// GeminiFunctionResponse represents a function response
type GeminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// GeminiTool represents a tool definition
type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// GeminiFunctionDeclaration represents a function declaration
type GeminiFunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// GeminiToolConfig represents tool configuration
type GeminiToolConfig struct {
	FunctionCallingConfig *GeminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// GeminiFunctionCallingConfig represents function calling config
type GeminiFunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"` // AUTO, ANY, NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GeminiGenerationConfig represents generation parameters
type GeminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

// ConvertChatToGeminiRequest converts OpenAI Chat request to Gemini format
func ConvertChatToGeminiRequest(req *types.ChatCompletionsRequest) (*GeminiRequest, error) {
	geminiReq := &GeminiRequest{}

	// Convert messages
	contents, systemInstruction := convertChatMessagesToGemini(req.Messages)
	geminiReq.Contents = contents
	geminiReq.SystemInstruction = systemInstruction

	// Convert tools
	if len(req.Tools) > 0 {
		geminiReq.Tools = convertChatToolsToGemini(req.Tools)
		geminiReq.ToolConfig = convertToolChoiceToGemini(req.ToolChoice)
	}

	// Set generation config
	geminiReq.GenerationConfig = &GeminiGenerationConfig{}
	if req.Temperature != nil {
		geminiReq.GenerationConfig.Temperature = req.Temperature
	}
	if req.TopP != nil {
		geminiReq.GenerationConfig.TopP = req.TopP
	}
	if maxTokens := req.GetMaxTokens(); maxTokens > 0 {
		geminiReq.GenerationConfig.MaxOutputTokens = &maxTokens
	}

	return geminiReq, nil
}

// convertChatMessagesToGemini converts chat messages to Gemini format
func convertChatMessagesToGemini(messages []types.ChatCompletionsMessage) ([]GeminiContent, *GeminiContent) {
	var contents []GeminiContent
	var systemInstruction *GeminiContent

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// System message becomes systemInstruction
			text := extractChatTextContent(msg.Content)
			if text != "" {
				systemInstruction = &GeminiContent{
					Parts: []GeminiPart{{Text: text}},
				}
			}

		case "user":
			text := extractChatTextContent(msg.Content)
			if text != "" {
				contents = append(contents, GeminiContent{
					Role:  "user",
					Parts: []GeminiPart{{Text: text}},
				})
			}

		case "assistant":
			content := convertAssistantToGemini(msg)
			if content != nil {
				contents = append(contents, *content)
			}

		case "tool":
			// Tool result
			content := convertToolResultToGemini(msg)
			if content != nil {
				contents = append(contents, *content)
			}
		}
	}

	return contents, systemInstruction
}

// extractChatTextContent extracts text from chat content
func extractChatTextContent(content interface{}) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		var texts []string
		for _, part := range arr {
			if m, ok := part.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// convertAssistantToGemini converts assistant message to Gemini format
func convertAssistantToGemini(msg types.ChatCompletionsMessage) *GeminiContent {
	var parts []GeminiPart

	// Add text content
	text := extractChatTextContent(msg.Content)
	if text != "" {
		parts = append(parts, GeminiPart{Text: text})
	}

	// Add function calls
	for _, tc := range msg.ToolCalls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		parts = append(parts, GeminiPart{
			FunctionCall: &GeminiFunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	if len(parts) == 0 {
		return nil
	}

	return &GeminiContent{
		Role:  "model",
		Parts: parts,
	}
}

// convertToolResultToGemini converts tool result to Gemini format
func convertToolResultToGemini(msg types.ChatCompletionsMessage) *GeminiContent {
	result := extractChatTextContent(msg.Content)

	return &GeminiContent{
		Role: "user",
		Parts: []GeminiPart{
			{
				FunctionResponse: &GeminiFunctionResponse{
					Name: "", // Gemini doesn't require name in response
					Response: map[string]interface{}{
						"result": result,
					},
				},
			},
		},
	}
}

// convertChatToolsToGemini converts chat tools to Gemini format
func convertChatToolsToGemini(tools []types.ChatCompletionsTool) []GeminiTool {
	var declarations []GeminiFunctionDeclaration

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		declarations = append(declarations, GeminiFunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}

	if len(declarations) == 0 {
		return nil
	}

	return []GeminiTool{{FunctionDeclarations: declarations}}
}

// convertToolChoiceToGemini converts tool_choice to Gemini toolConfig
func convertToolChoiceToGemini(toolChoice interface{}) *GeminiToolConfig {
	if toolChoice == nil {
		return nil
	}

	if s, ok := toolChoice.(string); ok {
		switch s {
		case "none":
			return &GeminiToolConfig{
				FunctionCallingConfig: &GeminiFunctionCallingConfig{Mode: "NONE"},
			}
		case "auto":
			return &GeminiToolConfig{
				FunctionCallingConfig: &GeminiFunctionCallingConfig{Mode: "AUTO"},
			}
		case "required":
			return &GeminiToolConfig{
				FunctionCallingConfig: &GeminiFunctionCallingConfig{Mode: "ANY"},
			}
		}
	}

	// Specific function
	if m, ok := toolChoice.(map[string]interface{}); ok {
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return &GeminiToolConfig{
					FunctionCallingConfig: &GeminiFunctionCallingConfig{
						Mode:                 "ANY",
						AllowedFunctionNames: []string{name},
					},
				}
			}
		}
	}

	return nil
}

// ============================================================================
// Gemini → Chat Completions Response Converter
// ============================================================================

// ConvertGeminiToChatResponse converts Gemini response to Chat format
func ConvertGeminiToChatResponse(geminiJSON []byte, model string) (*types.ChatCompletionsResponse, error) {
	result := gjson.ParseBytes(geminiJSON)

	resp := &types.ChatCompletionsResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []types.ChatCompletionsChoice{},
	}

	// Extract content from first candidate
	candidate := result.Get("candidates.0")
	if !candidate.Exists() {
		return resp, nil
	}

	message := types.ChatCompletionsMessage{
		Role: "assistant",
	}

	var textParts []string
	var toolCalls []types.ChatCompletionsToolCall
	toolCallIndex := 0

	// Process parts
	candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
		if text := part.Get("text").String(); text != "" {
			textParts = append(textParts, text)
		}
		if fc := part.Get("functionCall"); fc.Exists() {
			argsJSON, _ := json.Marshal(fc.Get("args").Value())
			idx := toolCallIndex
			toolCalls = append(toolCalls, types.ChatCompletionsToolCall{
				ID:    fmt.Sprintf("call_%d", idx),
				Type:  "function",
				Index: &idx,
				Function: types.ChatCompletionsToolCallFunction{
					Name:      fc.Get("name").String(),
					Arguments: string(argsJSON),
				},
			})
			toolCallIndex++
		}
		return true
	})

	if len(textParts) > 0 {
		message.Content = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	// Determine finish reason
	finishReason := "stop"
	geminiFinishReason := candidate.Get("finishReason").String()
	switch geminiFinishReason {
	case "STOP":
		finishReason = "stop"
	case "MAX_TOKENS":
		finishReason = "length"
	case "SAFETY":
		finishReason = "content_filter"
	}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	resp.Choices = append(resp.Choices, types.ChatCompletionsChoice{
		Index:        0,
		Message:      message,
		FinishReason: finishReason,
	})

	// Extract usage
	if usage := result.Get("usageMetadata"); usage.Exists() {
		resp.Usage = &types.ChatCompletionsUsage{
			PromptTokens:     int(usage.Get("promptTokenCount").Int()),
			CompletionTokens: int(usage.Get("candidatesTokenCount").Int()),
			TotalTokens:      int(usage.Get("totalTokenCount").Int()),
		}
	}

	return resp, nil
}

// ConvertGeminiStreamToChat converts Gemini streaming response to Chat format
func ConvertGeminiStreamToChat(ctx context.Context, reader io.Reader, model string, includeUsage bool) (<-chan string, <-chan error) {
	outCh := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		firstChunk := true

		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			result := gjson.Parse(data)
			candidate := result.Get("candidates.0")
			if !candidate.Exists() {
				continue
			}

			// Build chunk
			chunk := types.ChatCompletionsChunk{
				ID:      responseID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []types.ChatCompletionsChunkChoice{
					{
						Index: 0,
						Delta: types.ChatCompletionsChunkDelta{},
					},
				},
			}

			// First chunk includes role
			if firstChunk {
				chunk.Choices[0].Delta.Role = "assistant"
				firstChunk = false
			}

			// Extract text
			text := candidate.Get("content.parts.0.text").String()
			if text != "" {
				chunk.Choices[0].Delta.Content = &text
			}

			// Check finish reason
			if finishReason := candidate.Get("finishReason").String(); finishReason != "" {
				fr := "stop"
				switch finishReason {
				case "STOP":
					fr = "stop"
				case "MAX_TOKENS":
					fr = "length"
				}
				chunk.Choices[0].FinishReason = &fr
			}

			jsonBytes, _ := json.Marshal(chunk)
			select {
			case outCh <- fmt.Sprintf("data: %s\n\n", string(jsonBytes)):
			case <-ctx.Done():
				return
			}
		}

		// Send [DONE]
		select {
		case outCh <- "data: [DONE]\n\n":
		case <-ctx.Done():
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return outCh, errCh
}
