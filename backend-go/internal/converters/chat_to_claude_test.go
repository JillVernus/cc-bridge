package converters

import (
	"encoding/json"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/types"
)

// ============================================================================
// ConvertChatToClaudeRequest Tests
// ============================================================================

func TestConvertChatToClaudeRequest_BasicMessage(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model:  "claude-3-5-sonnet",
		Stream: false,
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello, Claude!"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model 'claude-3-5-sonnet', got '%s'", result.Model)
	}

	if result.Stream != false {
		t.Error("expected stream to be false")
	}

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", result.Messages[0].Role)
	}
}

func TestConvertChatToClaudeRequest_WithSystemMessage(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "claude-3-5-sonnet",
		Messages: []types.ChatCompletionsMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// System message should be extracted
	if result.System == nil {
		t.Fatal("expected system message to be set")
	}
	if result.System != "You are a helpful assistant." {
		t.Errorf("expected system 'You are a helpful assistant.', got '%v'", result.System)
	}

	// Only user message should remain in messages
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
}

func TestConvertChatToClaudeRequest_WithMaxTokens(t *testing.T) {
	maxTokens := 500
	req := &types.ChatCompletionsRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: &maxTokens,
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.MaxTokens != 500 {
		t.Errorf("expected max_tokens 500, got %d", result.MaxTokens)
	}
}

func TestConvertChatToClaudeRequest_WithMaxCompletionTokens(t *testing.T) {
	maxCompletionTokens := 1000
	req := &types.ChatCompletionsRequest{
		Model:               "claude-3-5-sonnet",
		MaxCompletionTokens: &maxCompletionTokens,
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.MaxTokens != 1000 {
		t.Errorf("expected max_tokens 1000, got %d", result.MaxTokens)
	}
}

func TestConvertChatToClaudeRequest_WithTemperature(t *testing.T) {
	temp := 0.7
	req := &types.ChatCompletionsRequest{
		Model:       "claude-3-5-sonnet",
		Temperature: &temp,
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", result.Temperature)
	}
}

func TestConvertChatToClaudeRequest_WithTools(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "claude-3-5-sonnet",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []types.ChatCompletionsTool{
			{
				Type: "function",
				Function: types.ChatCompletionsToolFunction{
					Name:        "get_weather",
					Description: "Get the current weather",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}

	if result.Tools[0].Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got '%s'", result.Tools[0].Name)
	}

	if result.Tools[0].Description != "Get the current weather" {
		t.Errorf("expected description 'Get the current weather', got '%s'", result.Tools[0].Description)
	}
}

func TestConvertChatToClaudeRequest_WithAssistantToolCalls(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "claude-3-5-sonnet",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What's the weather in Tokyo?"},
			{
				Role:    "assistant",
				Content: "I'll check the weather for you.",
				ToolCalls: []types.ChatCompletionsToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: types.ChatCompletionsToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"location": "Tokyo"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "Sunny, 25°C",
				ToolCallID: "call_123",
			},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// Should have 3 messages: user, assistant with tool_use, user with tool_result
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}

	// Check assistant message has tool_use content
	assistantMsg := result.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", assistantMsg.Role)
	}

	// The content should be an array with text and tool_use blocks
	contentBlocks, ok := assistantMsg.Content.([]types.ClaudeContent)
	if !ok {
		t.Fatalf("expected assistant content to be []ClaudeContent")
	}

	hasText := false
	hasToolUse := false
	for _, block := range contentBlocks {
		if block.Type == "text" && block.Text == "I'll check the weather for you." {
			hasText = true
		}
		if block.Type == "tool_use" && block.ID == "call_123" && block.Name == "get_weather" {
			hasToolUse = true
		}
	}

	if !hasText {
		t.Error("expected text block in assistant content")
	}
	if !hasToolUse {
		t.Error("expected tool_use block in assistant content")
	}

	// Check tool result is converted to user message with tool_result
	toolResultMsg := result.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Errorf("expected tool result role 'user', got '%s'", toolResultMsg.Role)
	}
}

// ============================================================================
// convertReasoningEffortToThinking Tests
// ============================================================================

func TestConvertReasoningEffortToThinking(t *testing.T) {
	tests := []struct {
		name     string
		effort   string
		expected *types.ClaudeThinking
	}{
		{
			name:     "none",
			effort:   "none",
			expected: &types.ClaudeThinking{Type: "disabled"},
		},
		{
			name:     "low",
			effort:   "low",
			expected: &types.ClaudeThinking{Type: "enabled", BudgetTokens: 5000},
		},
		{
			name:     "medium",
			effort:   "medium",
			expected: &types.ClaudeThinking{Type: "enabled", BudgetTokens: 10000},
		},
		{
			name:     "high",
			effort:   "high",
			expected: &types.ClaudeThinking{Type: "enabled", BudgetTokens: 20000},
		},
		{
			name:     "unknown",
			effort:   "unknown",
			expected: nil,
		},
		{
			name:     "case insensitive - HIGH",
			effort:   "HIGH",
			expected: &types.ClaudeThinking{Type: "enabled", BudgetTokens: 20000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertReasoningEffortToThinking(tt.effort)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Type != tt.expected.Type {
				t.Errorf("expected type '%s', got '%s'", tt.expected.Type, result.Type)
			}

			if result.BudgetTokens != tt.expected.BudgetTokens {
				t.Errorf("expected budget tokens %d, got %d", tt.expected.BudgetTokens, result.BudgetTokens)
			}
		})
	}
}

func TestConvertChatToClaudeRequest_WithReasoningEffort(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model:           "claude-3-5-sonnet",
		ReasoningEffort: "high",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Think through this problem."},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.Thinking == nil {
		t.Fatal("expected thinking to be set")
	}

	if result.Thinking.Type != "enabled" {
		t.Errorf("expected thinking type 'enabled', got '%s'", result.Thinking.Type)
	}

	if result.Thinking.BudgetTokens != 20000 {
		t.Errorf("expected budget tokens 20000, got %d", result.Thinking.BudgetTokens)
	}
}

// ============================================================================
// ConvertToolChoice Tests
// ============================================================================

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "none string",
			input:    "none",
			expected: map[string]string{"type": "none"},
		},
		{
			name:     "auto string",
			input:    "auto",
			expected: map[string]string{"type": "auto"},
		},
		{
			name:     "required string",
			input:    "required",
			expected: map[string]string{"type": "any"},
		},
		{
			name:  "specific function",
			input: map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}},
			expected: map[string]interface{}{
				"type": "tool",
				"name": "get_weather",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToolChoice(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			// Compare JSON representations for complex types
			expectedJSON, _ := json.Marshal(tt.expected)
			resultJSON, _ := json.Marshal(result)

			if string(expectedJSON) != string(resultJSON) {
				t.Errorf("expected %s, got %s", string(expectedJSON), string(resultJSON))
			}
		})
	}
}

// ============================================================================
// extractTextContent Tests
// ============================================================================

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: "",
		},
		{
			name:     "string input",
			input:    "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name: "array with text parts",
			input: []interface{}{
				map[string]interface{}{"type": "text", "text": "First"},
				map[string]interface{}{"type": "text", "text": "Second"},
			},
			expected: "First\nSecond",
		},
		{
			name: "array with mixed types (text only extracted)",
			input: []interface{}{
				map[string]interface{}{"type": "text", "text": "Text part"},
				map[string]interface{}{"type": "image_url", "url": "http://example.com"},
			},
			expected: "Text part",
		},
		{
			name:     "empty array",
			input:    []interface{}{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTextContent(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%v', got '%v'", tt.expected, result)
			}
		})
	}
}

// ============================================================================
// ConvertChatToClaudeJSON Tests
// ============================================================================

func TestConvertChatToClaudeJSON(t *testing.T) {
	chatJSON := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello!"}
		],
		"max_tokens": 100
	}`)

	result, err := ConvertChatToClaudeJSON(chatJSON, "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Model should be overridden
	if claudeReq["model"] != "claude-3-5-sonnet" {
		t.Errorf("expected model 'claude-3-5-sonnet', got '%v'", claudeReq["model"])
	}

	// System should be extracted
	if claudeReq["system"] != "You are helpful." {
		t.Errorf("expected system 'You are helpful.', got '%v'", claudeReq["system"])
	}
}

func TestConvertChatToClaudeJSON_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)

	_, err := ConvertChatToClaudeJSON(invalidJSON, "claude-3-5-sonnet")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ============================================================================
// Multi-turn Conversation Test
// ============================================================================

func TestConvertChatToClaudeRequest_MultiTurnConversation(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "claude-3-5-sonnet",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What is 2+2?"},
			{Role: "assistant", Content: "2+2 equals 4."},
			{Role: "user", Content: "And what is 3+3?"},
		},
	}

	result, err := ConvertChatToClaudeRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}

	// Verify message order and roles
	expectedRoles := []string{"user", "assistant", "user"}
	for i, msg := range result.Messages {
		if msg.Role != expectedRoles[i] {
			t.Errorf("message %d: expected role '%s', got '%s'", i, expectedRoles[i], msg.Role)
		}
	}
}
