package converters

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/types"
)

// ============================================================================
// ConvertClaudeToChatResponse Tests
// ============================================================================

func TestConvertClaudeToChatResponse_BasicText(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID: "msg_12345",
		Content: []types.ClaudeContent{
			{Type: "text", Text: "Hello! How can I help you?"},
		},
		StopReason: "end_turn",
		Usage: &types.Usage{
			InputTokens:  10,
			OutputTokens: 20,
		},
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

	// Check ID conversion
	if !strings.HasPrefix(result.ID, "chatcmpl-") {
		t.Errorf("expected ID to start with 'chatcmpl-', got '%s'", result.ID)
	}

	if result.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got '%s'", result.Object)
	}

	if result.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model 'claude-3-5-sonnet', got '%s'", result.Model)
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
	if content != "Hello! How can I help you?" {
		t.Errorf("expected content 'Hello! How can I help you?', got '%s'", content)
	}

	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", choice.FinishReason)
	}
}

func TestConvertClaudeToChatResponse_WithToolUse(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID: "msg_12345",
		Content: []types.ClaudeContent{
			{Type: "text", Text: "I'll check the weather for you."},
			{
				Type:  "tool_use",
				ID:    "toolu_123",
				Name:  "get_weather",
				Input: map[string]interface{}{"location": "Tokyo"},
			},
		},
		StopReason: "tool_use",
		Usage: &types.Usage{
			InputTokens:  15,
			OutputTokens: 30,
		},
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	choice := result.Choices[0]

	// Check text content
	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatalf("expected content to be string")
	}
	if content != "I'll check the weather for you." {
		t.Errorf("expected text content, got '%s'", content)
	}

	// Check tool calls
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}

	tc := choice.Message.ToolCalls[0]
	if tc.ID != "toolu_123" {
		t.Errorf("expected tool call ID 'toolu_123', got '%s'", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected type 'function', got '%s'", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", tc.Function.Name)
	}

	// Check finish reason
	if choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got '%s'", choice.FinishReason)
	}
}

func TestConvertClaudeToChatResponse_WithThinking(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID: "msg_12345",
		Content: []types.ClaudeContent{
			{Type: "thinking", Thinking: "Let me think about this..."},
			{Type: "text", Text: "The answer is 42."},
		},
		StopReason: "end_turn",
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

	choice := result.Choices[0]
	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatalf("expected content to be string")
	}

	// Thinking should be included in content
	if !strings.Contains(content, "Let me think about this...") {
		t.Error("expected thinking content to be included")
	}
	if !strings.Contains(content, "The answer is 42.") {
		t.Error("expected text content to be included")
	}
}

func TestConvertClaudeToChatResponse_Usage(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID:         "msg_12345",
		Content:    []types.ClaudeContent{{Type: "text", Text: "Hi"}},
		StopReason: "end_turn",
		Usage: &types.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

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

// ============================================================================
// generateChatResponseID Tests
// ============================================================================

func TestGenerateChatResponseID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with msg_ prefix",
			input:    "msg_12345abcde",
			expected: "chatcmpl-12345abcde",
		},
		{
			name:     "without prefix",
			input:    "custom_id_123",
			expected: "chatcmpl-custom_id_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateChatResponseID(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// ============================================================================
// convertClaudeStopReason Tests
// ============================================================================

func TestConvertClaudeStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"stop_sequence", "stop"},
		{"unknown", "stop"},
		{"", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertClaudeStopReason(tt.input)
			if result != tt.expected {
				t.Errorf("input '%s': expected '%s', got '%s'", tt.input, tt.expected, result)
			}
		})
	}
}

// ============================================================================
// ConvertClaudeJSONToChatJSON Tests
// ============================================================================

func TestConvertClaudeJSONToChatJSON(t *testing.T) {
	claudeJSON := []byte(`{
		"id": "msg_test123",
		"content": [{"type": "text", "text": "Hello!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)

	result, err := ConvertClaudeJSONToChatJSON(claudeJSON, "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	var chatResp types.ChatCompletionsResponse
	if err := json.Unmarshal(result, &chatResp); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if chatResp.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got '%s'", chatResp.Object)
	}

	if chatResp.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model 'claude-3-5-sonnet', got '%s'", chatResp.Model)
	}

	if len(chatResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chatResp.Choices))
	}
}

// ============================================================================
// Streaming Conversion Tests
// ============================================================================

func TestConvertClaudeToChatStream_BasicText(t *testing.T) {
	// Simulate Claude SSE stream
	claudeStream := `event: message_start
data: {"type":"message_start","message":{"id":"msg_test123","type":"message","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outCh, errCh := ConvertClaudeToChatStream(ctx, strings.NewReader(claudeStream), "claude-3-5-sonnet", true)

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

	// Check first chunk has role
	if !strings.Contains(chunks[0], `"role":"assistant"`) {
		t.Error("first chunk should contain role")
	}

	// Check for [DONE] marker
	lastChunk := chunks[len(chunks)-1]
	if !strings.Contains(lastChunk, "[DONE]") {
		t.Error("last chunk should be [DONE]")
	}

	// Verify chunks are in proper SSE format
	for _, chunk := range chunks {
		if !strings.HasPrefix(chunk, "data: ") {
			t.Errorf("chunk should start with 'data: ', got: %s", chunk[:min(50, len(chunk))])
		}
	}
}

func TestConvertClaudeToChatStream_WithToolCall(t *testing.T) {
	claudeStream := `event: message_start
data: {"type":"message_start","message":{"id":"msg_test456","type":"message","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\": \"Tokyo\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":15,"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}
`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outCh, errCh := ConvertClaudeToChatStream(ctx, strings.NewReader(claudeStream), "claude-3-5-sonnet", false)

	var chunks []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range outCh {
			chunks = append(chunks, chunk)
		}
		select {
		case <-errCh:
		default:
		}
	}()

	<-done

	// Should have tool call chunks
	hasToolCall := false
	for _, chunk := range chunks {
		if strings.Contains(chunk, "tool_calls") && strings.Contains(chunk, "get_weather") {
			hasToolCall = true
			break
		}
	}

	if !hasToolCall {
		t.Error("expected tool call chunk in stream")
	}
}

// ============================================================================
// ConvertClaudeStreamToChat (io.ReadCloser wrapper) Tests
// ============================================================================

func TestConvertClaudeStreamToChat(t *testing.T) {
	claudeStream := `event: message_start
data: {"type":"message_start","message":{"id":"msg_wrapper","type":"message","role":"assistant"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Test"}}

event: message_stop
data: {"type":"message_stop"}
`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body := io.NopCloser(strings.NewReader(claudeStream))
	reader, err := ConvertClaudeStreamToChat(ctx, body, "claude-3-5-sonnet", false)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	defer reader.Close()

	// Read all content
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "data: ") {
		t.Error("expected SSE data format")
	}
}

// ============================================================================
// OpenAI Passthrough Tests
// ============================================================================

func TestConvertOpenAIStreamToChat(t *testing.T) {
	openaiStream := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hi"}}]}

data: [DONE]
`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outCh, errCh := ConvertOpenAIStreamToChat(ctx, strings.NewReader(openaiStream))

	var chunks []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range outCh {
			chunks = append(chunks, chunk)
		}
		select {
		case <-errCh:
		default:
		}
	}()

	<-done

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks (2 data + DONE), got %d", len(chunks))
	}

	// Verify passthrough (chunks should be unchanged)
	for _, chunk := range chunks {
		if !strings.HasPrefix(chunk, "data: ") {
			t.Errorf("chunk should start with 'data: ', got: %s", chunk)
		}
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestConvertClaudeToChatResponse_EmptyContent(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID:         "msg_empty",
		Content:    []types.ClaudeContent{},
		StopReason: "end_turn",
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	// Empty content should result in empty message content
	choice := result.Choices[0]
	if choice.Message.Content != nil && choice.Message.Content != "" {
		t.Errorf("expected empty content, got '%v'", choice.Message.Content)
	}
}

func TestConvertClaudeToChatResponse_NoUsage(t *testing.T) {
	claudeResp := &types.ClaudeResponse{
		ID:         "msg_nousage",
		Content:    []types.ClaudeContent{{Type: "text", Text: "Hi"}},
		StopReason: "end_turn",
		Usage:      nil,
	}

	result := ConvertClaudeToChatResponse(claudeResp, "claude-3-5-sonnet")

	if result.Usage != nil {
		t.Error("expected nil usage")
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
