package converters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/types"
)

// ============================================================================
// ConvertChatToGeminiRequest Tests
// ============================================================================

func TestConvertChatToGeminiRequest_BasicMessage(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "gemini-pro",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello, Gemini!"},
		},
	}

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}

	content := result.Contents[0]
	if content.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", content.Role)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}

	if content.Parts[0].Text != "Hello, Gemini!" {
		t.Errorf("expected text 'Hello, Gemini!', got '%s'", content.Parts[0].Text)
	}
}

func TestConvertChatToGeminiRequest_WithSystemMessage(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "gemini-pro",
		Messages: []types.ChatCompletionsMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// System should become systemInstruction
	if result.SystemInstruction == nil {
		t.Fatal("expected systemInstruction to be set")
	}

	if len(result.SystemInstruction.Parts) != 1 {
		t.Fatalf("expected 1 part in systemInstruction, got %d", len(result.SystemInstruction.Parts))
	}

	if result.SystemInstruction.Parts[0].Text != "You are a helpful assistant." {
		t.Errorf("expected systemInstruction text 'You are a helpful assistant.'")
	}

	// Only user message in contents
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content (user only), got %d", len(result.Contents))
	}
}

func TestConvertChatToGeminiRequest_WithGenerationConfig(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 500
	req := &types.ChatCompletionsRequest{
		Model:       "gemini-pro",
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "Hello!"},
		},
	}

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.GenerationConfig == nil {
		t.Fatal("expected generationConfig to be set")
	}

	if *result.GenerationConfig.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", *result.GenerationConfig.Temperature)
	}

	if *result.GenerationConfig.TopP != 0.9 {
		t.Errorf("expected topP 0.9, got %f", *result.GenerationConfig.TopP)
	}

	if *result.GenerationConfig.MaxOutputTokens != 500 {
		t.Errorf("expected maxOutputTokens 500, got %d", *result.GenerationConfig.MaxOutputTokens)
	}
}

func TestConvertChatToGeminiRequest_WithTools(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "gemini-pro",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []types.ChatCompletionsTool{
			{
				Type: "function",
				Function: types.ChatCompletionsToolFunction{
					Name:        "get_weather",
					Description: "Get current weather",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	}

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}

	if len(result.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(result.Tools[0].FunctionDeclarations))
	}

	fn := result.Tools[0].FunctionDeclarations[0]
	if fn.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", fn.Name)
	}

	if fn.Description != "Get current weather" {
		t.Errorf("expected description 'Get current weather', got '%s'", fn.Description)
	}
}

func TestConvertChatToGeminiRequest_WithToolChoice(t *testing.T) {
	tests := []struct {
		name          string
		toolChoice    interface{}
		expectedMode  string
		expectedFuncs []string
	}{
		{
			name:         "none",
			toolChoice:   "none",
			expectedMode: "NONE",
		},
		{
			name:         "auto",
			toolChoice:   "auto",
			expectedMode: "AUTO",
		},
		{
			name:         "required",
			toolChoice:   "required",
			expectedMode: "ANY",
		},
		{
			name:          "specific function",
			toolChoice:    map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}},
			expectedMode:  "ANY",
			expectedFuncs: []string{"get_weather"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &types.ChatCompletionsRequest{
				Model: "gemini-pro",
				Messages: []types.ChatCompletionsMessage{
					{Role: "user", Content: "Test"},
				},
				Tools: []types.ChatCompletionsTool{
					{
						Type: "function",
						Function: types.ChatCompletionsToolFunction{
							Name: "get_weather",
						},
					},
				},
				ToolChoice: tt.toolChoice,
			}

			result, err := ConvertChatToGeminiRequest(req)
			if err != nil {
				t.Fatalf("conversion failed: %v", err)
			}

			if result.ToolConfig == nil || result.ToolConfig.FunctionCallingConfig == nil {
				t.Fatal("expected toolConfig to be set")
			}

			config := result.ToolConfig.FunctionCallingConfig
			if config.Mode != tt.expectedMode {
				t.Errorf("expected mode '%s', got '%s'", tt.expectedMode, config.Mode)
			}

			if tt.expectedFuncs != nil {
				if len(config.AllowedFunctionNames) != len(tt.expectedFuncs) {
					t.Errorf("expected %d allowed functions, got %d", len(tt.expectedFuncs), len(config.AllowedFunctionNames))
				}
			}
		})
	}
}

func TestConvertChatToGeminiRequest_WithAssistantToolCall(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "gemini-pro",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What's the weather in Tokyo?"},
			{
				Role:    "assistant",
				Content: "",
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

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// Should have 3 contents: user, model (assistant with function call), user (function response)
	if len(result.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(result.Contents))
	}

	// Check assistant message becomes model with function call
	modelContent := result.Contents[1]
	if modelContent.Role != "model" {
		t.Errorf("expected role 'model', got '%s'", modelContent.Role)
	}

	hasFunctionCall := false
	for _, part := range modelContent.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "get_weather" {
			hasFunctionCall = true
			break
		}
	}
	if !hasFunctionCall {
		t.Error("expected function call in model content")
	}

	// Check tool result becomes function response
	toolContent := result.Contents[2]
	if toolContent.Role != "user" {
		t.Errorf("expected tool result role 'user', got '%s'", toolContent.Role)
	}

	hasFunctionResponse := false
	for _, part := range toolContent.Parts {
		if part.FunctionResponse != nil {
			hasFunctionResponse = true
			break
		}
	}
	if !hasFunctionResponse {
		t.Error("expected function response in tool result content")
	}
}

// ============================================================================
// ConvertGeminiToChatResponse Tests
// ============================================================================

func TestConvertGeminiToChatResponse_BasicText(t *testing.T) {
	geminiJSON := []byte(`{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{"text": "Hello! How can I help?"}]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 20,
			"totalTokenCount": 30
		}
	}`)

	result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if !strings.HasPrefix(result.ID, "chatcmpl-") {
		t.Errorf("expected ID to start with 'chatcmpl-', got '%s'", result.ID)
	}

	if result.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got '%s'", result.Object)
	}

	if result.Model != "gemini-pro" {
		t.Errorf("expected model 'gemini-pro', got '%s'", result.Model)
	}

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	choice := result.Choices[0]
	if choice.Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", choice.Message.Role)
	}

	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatalf("expected content to be string")
	}
	if content != "Hello! How can I help?" {
		t.Errorf("expected content 'Hello! How can I help?', got '%s'", content)
	}

	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", choice.FinishReason)
	}
}

func TestConvertGeminiToChatResponse_WithFunctionCall(t *testing.T) {
	geminiJSON := []byte(`{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{
					"functionCall": {
						"name": "get_weather",
						"args": {"location": "Tokyo"}
					}
				}]
			},
			"finishReason": "STOP"
		}]
	}`)

	result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	choice := result.Choices[0]

	// Should have tool calls
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}

	tc := choice.Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", tc.Function.Name)
	}

	// Finish reason should be tool_calls
	if choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got '%s'", choice.FinishReason)
	}
}

func TestConvertGeminiToChatResponse_Usage(t *testing.T) {
	geminiJSON := []byte(`{
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "Hi"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 100,
			"candidatesTokenCount": 50,
			"totalTokenCount": 150
		}
	}`)

	result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if result.Usage == nil {
		t.Fatal("expected usage to be set")
	}

	if result.Usage.PromptTokens != 100 {
		t.Errorf("expected prompt_tokens 100, got %d", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 50 {
		t.Errorf("expected completion_tokens 50, got %d", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 150 {
		t.Errorf("expected total_tokens 150, got %d", result.Usage.TotalTokens)
	}
}

func TestConvertGeminiToChatResponse_FinishReasons(t *testing.T) {
	tests := []struct {
		geminiReason string
		expected     string
	}{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
	}

	for _, tt := range tests {
		t.Run(tt.geminiReason, func(t *testing.T) {
			geminiJSON := []byte(`{
				"candidates": [{
					"content": {"role": "model", "parts": [{"text": "test"}]},
					"finishReason": "` + tt.geminiReason + `"
				}]
			}`)

			result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
			if err != nil {
				t.Fatalf("conversion failed: %v", err)
			}

			if result.Choices[0].FinishReason != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result.Choices[0].FinishReason)
			}
		})
	}
}

// ============================================================================
// ConvertGeminiStreamToChat Tests
// ============================================================================

func TestConvertGeminiStreamToChat_BasicText(t *testing.T) {
	geminiStream := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]},"finishReason":""}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]},"finishReason":""}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"!"}]},"finishReason":"STOP"}]}

data: [DONE]
`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outCh, errCh := ConvertGeminiStreamToChat(ctx, strings.NewReader(geminiStream), "gemini-pro", false)

	var chunks []string
	var streamErr error

	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range outCh {
			chunks = append(chunks, chunk)
		}
		select {
		case err := <-errCh:
			streamErr = err
		default:
		}
	}()

	<-done

	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// First chunk should have role
	if !strings.Contains(chunks[0], `"role":"assistant"`) {
		t.Error("first chunk should contain role")
	}

	// Last chunk should be [DONE]
	lastChunk := chunks[len(chunks)-1]
	if !strings.Contains(lastChunk, "[DONE]") {
		t.Error("last chunk should be [DONE]")
	}

	// All chunks should be in SSE format
	for _, chunk := range chunks {
		if !strings.HasPrefix(chunk, "data: ") {
			t.Errorf("chunk should start with 'data: ', got: %s", chunk[:min(50, len(chunk))])
		}
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestConvertGeminiToChatResponse_EmptyCandidates(t *testing.T) {
	geminiJSON := []byte(`{"candidates": []}`)

	result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// Should return empty choices
	if len(result.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(result.Choices))
	}
}

func TestConvertGeminiToChatResponse_NoCandidates(t *testing.T) {
	geminiJSON := []byte(`{}`)

	result, err := ConvertGeminiToChatResponse(geminiJSON, "gemini-pro")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// Should return empty choices
	if len(result.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(result.Choices))
	}
}

func TestConvertChatToGeminiRequest_MultiTurn(t *testing.T) {
	req := &types.ChatCompletionsRequest{
		Model: "gemini-pro",
		Messages: []types.ChatCompletionsMessage{
			{Role: "user", Content: "What is 2+2?"},
			{Role: "assistant", Content: "2+2 equals 4."},
			{Role: "user", Content: "And 3+3?"},
		},
	}

	result, err := ConvertChatToGeminiRequest(req)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(result.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(result.Contents))
	}

	// Check roles are converted correctly
	expectedRoles := []string{"user", "model", "user"}
	for i, content := range result.Contents {
		if content.Role != expectedRoles[i] {
			t.Errorf("content %d: expected role '%s', got '%s'", i, expectedRoles[i], content.Role)
		}
	}
}

// ============================================================================
// JSON Serialization Tests
// ============================================================================

func TestGeminiRequestJSON(t *testing.T) {
	temp := 0.7
	maxTokens := 100
	req := GeminiRequest{
		Contents: []GeminiContent{
			{
				Role:  "user",
				Parts: []GeminiPart{{Text: "Hello"}},
			},
		},
		GenerationConfig: &GeminiGenerationConfig{
			Temperature:     &temp,
			MaxOutputTokens: &maxTokens,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify structure
	contents, ok := parsed["contents"].([]interface{})
	if !ok || len(contents) != 1 {
		t.Error("expected 1 content")
	}

	genConfig, ok := parsed["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("expected generationConfig")
	}

	if genConfig["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", genConfig["temperature"])
	}
}

func TestExtractChatTextContent(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil",
			input:    nil,
			expected: "",
		},
		{
			name:     "string",
			input:    "Hello",
			expected: "Hello",
		},
		{
			name: "array with text",
			input: []interface{}{
				map[string]interface{}{"type": "text", "text": "First"},
				map[string]interface{}{"type": "text", "text": "Second"},
			},
			expected: "First\nSecond",
		},
		{
			name:     "empty array",
			input:    []interface{}{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractChatTextContent(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
