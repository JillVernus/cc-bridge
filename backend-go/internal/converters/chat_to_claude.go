package converters

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/types"
)

// ============================================================================
// Chat Completions → Claude Messages Converter
// Converts OpenAI Chat Completions format to Claude Messages API format
// ============================================================================

// ConvertChatToClaudeRequest converts OpenAI Chat Completions request to Claude Messages request
func ConvertChatToClaudeRequest(req *types.ChatCompletionsRequest) (*types.ClaudeRequest, error) {
	claudeReq := &types.ClaudeRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}

	// Set max_tokens
	if maxTokens := req.GetMaxTokens(); maxTokens > 0 {
		claudeReq.MaxTokens = maxTokens
	}

	// Set temperature
	if req.Temperature != nil {
		claudeReq.Temperature = *req.Temperature
	}

	// Convert reasoning_effort to thinking
	if req.ReasoningEffort != "" {
		claudeReq.Thinking = convertReasoningEffortToThinking(req.ReasoningEffort)
	}

	// Extract system message and convert messages
	system, messages, err := convertChatMessagesToClaude(req.Messages)
	if err != nil {
		return nil, err
	}
	claudeReq.System = system
	claudeReq.Messages = messages

	// Convert tools
	if len(req.Tools) > 0 {
		claudeReq.Tools = convertChatToolsToClaude(req.Tools)
	}

	return claudeReq, nil
}

// convertReasoningEffortToThinking converts OpenAI reasoning_effort to Claude thinking config
func convertReasoningEffortToThinking(effort string) *types.ClaudeThinking {
	switch strings.ToLower(effort) {
	case "none":
		return &types.ClaudeThinking{Type: "disabled"}
	case "low":
		return &types.ClaudeThinking{Type: "enabled", BudgetTokens: 5000}
	case "medium":
		return &types.ClaudeThinking{Type: "enabled", BudgetTokens: 10000}
	case "high":
		return &types.ClaudeThinking{Type: "enabled", BudgetTokens: 20000}
	default:
		return nil
	}
}

// convertChatMessagesToClaude extracts system message and converts chat messages to Claude format
func convertChatMessagesToClaude(messages []types.ChatCompletionsMessage) (interface{}, []types.ClaudeMessage, error) {
	var systemContent interface{}
	var claudeMessages []types.ClaudeMessage

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// Extract system message content
			systemContent = extractTextContent(msg.Content)

		case "user":
			claudeMsg := types.ClaudeMessage{
				Role:    "user",
				Content: extractTextContent(msg.Content),
			}
			claudeMessages = append(claudeMessages, claudeMsg)

		case "assistant":
			claudeMsg, err := convertAssistantMessageToClaude(msg)
			if err != nil {
				return nil, nil, err
			}
			claudeMessages = append(claudeMessages, claudeMsg)

		case "tool":
			// Tool results become user messages with tool_result content
			claudeMsg := convertToolResultToClaude(msg)
			claudeMessages = append(claudeMessages, claudeMsg)
		}
	}

	return systemContent, claudeMessages, nil
}

// extractTextContent extracts text from content (handles string or array)
func extractTextContent(content interface{}) interface{} {
	if content == nil {
		return ""
	}

	// If it's a string, return as-is
	if s, ok := content.(string); ok {
		return s
	}

	// If it's an array (multimodal), extract text parts
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

// convertAssistantMessageToClaude converts assistant message (may include tool_calls)
func convertAssistantMessageToClaude(msg types.ChatCompletionsMessage) (types.ClaudeMessage, error) {
	claudeMsg := types.ClaudeMessage{
		Role: "assistant",
	}

	// If there are tool_calls, convert to content array with text and tool_use blocks
	if len(msg.ToolCalls) > 0 {
		var contentBlocks []types.ClaudeContent

		// Add text content if present
		textContent := extractTextContent(msg.Content)
		if text, ok := textContent.(string); ok && text != "" {
			contentBlocks = append(contentBlocks, types.ClaudeContent{
				Type: "text",
				Text: text,
			})
		}

		// Add tool_use blocks
		for _, tc := range msg.ToolCalls {
			// Parse arguments JSON
			var input interface{}
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					// If not valid JSON, use as string
					input = tc.Function.Arguments
				}
			}

			contentBlocks = append(contentBlocks, types.ClaudeContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		claudeMsg.Content = contentBlocks
	} else {
		// No tool calls, just text content
		claudeMsg.Content = extractTextContent(msg.Content)
	}

	return claudeMsg, nil
}

// convertToolResultToClaude converts tool role message to Claude tool_result
func convertToolResultToClaude(msg types.ChatCompletionsMessage) types.ClaudeMessage {
	// Get result content
	resultContent := ""
	if s, ok := msg.Content.(string); ok {
		resultContent = s
	}

	return types.ClaudeMessage{
		Role: "user",
		Content: []types.ClaudeContent{
			{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Text:      resultContent,
			},
		},
	}
}

// convertChatToolsToClaude converts OpenAI tools to Claude tools
func convertChatToolsToClaude(tools []types.ChatCompletionsTool) []types.ClaudeTool {
	var claudeTools []types.ClaudeTool

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}

		claudeTool := types.ClaudeTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
		claudeTools = append(claudeTools, claudeTool)
	}

	return claudeTools
}

// ============================================================================
// Tool Choice Conversion
// ============================================================================

// ConvertToolChoice converts OpenAI tool_choice to Claude format
// Returns: tool_choice value for Claude API
func ConvertToolChoice(toolChoice interface{}) interface{} {
	if toolChoice == nil {
		return nil // Claude default (auto)
	}

	// String values
	if s, ok := toolChoice.(string); ok {
		switch s {
		case "none":
			return map[string]string{"type": "none"}
		case "auto":
			return map[string]string{"type": "auto"}
		case "required":
			return map[string]string{"type": "any"}
		}
	}

	// Object value: {"type": "function", "function": {"name": "..."}}
	if m, ok := toolChoice.(map[string]interface{}); ok {
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return map[string]interface{}{
					"type": "tool",
					"name": name,
				}
			}
		}
	}

	return nil
}

// ============================================================================
// JSON Conversion Helpers
// ============================================================================

// ConvertChatToClaudeJSON converts raw JSON request from Chat to Claude format
func ConvertChatToClaudeJSON(chatJSON []byte, modelName string) ([]byte, error) {
	var req types.ChatCompletionsRequest
	if err := json.Unmarshal(chatJSON, &req); err != nil {
		return nil, fmt.Errorf("failed to parse chat request: %w", err)
	}

	// Override model if specified
	if modelName != "" {
		req.Model = modelName
	}

	claudeReq, err := ConvertChatToClaudeRequest(&req)
	if err != nil {
		return nil, err
	}

	return json.Marshal(claudeReq)
}
