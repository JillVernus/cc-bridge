package converters

import (
	"bufio"
	"bytes"
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
// Claude Messages → Chat Completions Response Converter
// Converts Claude response format to OpenAI Chat Completions format
// ============================================================================

// ConvertClaudeToChatResponse converts Claude non-streaming response to Chat Completions format
func ConvertClaudeToChatResponse(claudeResp *types.ClaudeResponse, model string) *types.ChatCompletionsResponse {
	resp := &types.ChatCompletionsResponse{
		ID:      generateChatResponseID(claudeResp.ID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []types.ChatCompletionsChoice{},
	}

	// Convert content to message
	message := convertClaudeContentToMessage(claudeResp.Content)

	// Convert finish reason
	finishReason := convertClaudeStopReason(claudeResp.StopReason)

	resp.Choices = append(resp.Choices, types.ChatCompletionsChoice{
		Index:        0,
		Message:      message,
		FinishReason: finishReason,
	})

	// Convert usage
	if claudeResp.Usage != nil {
		resp.Usage = &types.ChatCompletionsUsage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		}
	}

	return resp
}

// generateChatResponseID generates a chat completion ID from Claude ID
func generateChatResponseID(claudeID string) string {
	if strings.HasPrefix(claudeID, "msg_") {
		return "chatcmpl-" + claudeID[4:]
	}
	return "chatcmpl-" + claudeID
}

// convertClaudeContentToMessage converts Claude content blocks to Chat message
func convertClaudeContentToMessage(content []types.ClaudeContent) types.ChatCompletionsMessage {
	msg := types.ChatCompletionsMessage{
		Role: "assistant",
	}

	var textParts []string
	var toolCalls []types.ChatCompletionsToolCall

	for _, block := range content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "thinking":
			// Include thinking in text if present
			if block.Thinking != "" {
				textParts = append(textParts, block.Thinking)
			}
		case "tool_use":
			// Convert to tool_call
			argsJSON, _ := json.Marshal(block.Input)
			toolCall := types.ChatCompletionsToolCall{
				ID:   block.ID,
				Type: "function",
				Function: types.ChatCompletionsToolCallFunction{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	// Set content
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "")
	}

	// Set tool_calls
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return msg
}

// convertClaudeStopReason converts Claude stop_reason to Chat finish_reason
func convertClaudeStopReason(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

// ============================================================================
// Streaming Conversion: Claude SSE → Chat Completions SSE
// ============================================================================

// claudeToChatStreamState maintains state during streaming conversion
type claudeToChatStreamState struct {
	ResponseID   string
	Model        string
	Created      int64
	CurrentText  strings.Builder
	ToolCalls    map[int]*toolCallAccumulator
	FinishReason string
	InputTokens  int
	OutputTokens int
	UsageSeen    bool
	FirstChunk   bool
	IncludeUsage bool
}

// toolCallAccumulator accumulates tool call data during streaming
type toolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// newClaudeToChatStreamState creates a new streaming state
func newClaudeToChatStreamState(model string, includeUsage bool) *claudeToChatStreamState {
	return &claudeToChatStreamState{
		ResponseID:   fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Model:        model,
		Created:      time.Now().Unix(),
		ToolCalls:    make(map[int]*toolCallAccumulator),
		FirstChunk:   true,
		IncludeUsage: includeUsage,
	}
}

// ConvertClaudeToChatStream converts Claude SSE stream to Chat Completions SSE stream
func ConvertClaudeToChatStream(
	ctx context.Context,
	reader io.Reader,
	model string,
	includeUsage bool,
) (<-chan string, <-chan error) {
	outCh := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		state := newClaudeToChatStreamState(model, includeUsage)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		var eventType string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()

			// Parse SSE format
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")

				chunks := state.processClaudeEvent(eventType, data)
				for _, chunk := range chunks {
					select {
					case outCh <- chunk:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}

		// Send final chunk with usage if needed
		if state.IncludeUsage && state.UsageSeen {
			finalChunk := state.buildFinalChunkWithUsage()
			select {
			case outCh <- finalChunk:
			case <-ctx.Done():
			}
		}

		// Send [DONE]
		select {
		case outCh <- "data: [DONE]\n\n":
		case <-ctx.Done():
		}
	}()

	return outCh, errCh
}

// processClaudeEvent processes a Claude SSE event and returns Chat chunks
func (s *claudeToChatStreamState) processClaudeEvent(eventType, data string) []string {
	var chunks []string

	switch eventType {
	case "message_start":
		// Extract message ID
		if id := gjson.Get(data, "message.id").String(); id != "" {
			s.ResponseID = generateChatResponseID(id)
		}
		// Send initial chunk with role
		chunks = append(chunks, s.buildChunk("", true, false))

	case "content_block_start":
		blockType := gjson.Get(data, "content_block.type").String()
		index := int(gjson.Get(data, "index").Int())

		if blockType == "tool_use" {
			// Start tool call
			id := gjson.Get(data, "content_block.id").String()
			name := gjson.Get(data, "content_block.name").String()
			s.ToolCalls[index] = &toolCallAccumulator{
				ID:   id,
				Name: name,
			}
			// Send tool call start chunk
			chunks = append(chunks, s.buildToolCallStartChunk(index, id, name))
		}

	case "content_block_delta":
		deltaType := gjson.Get(data, "delta.type").String()
		index := int(gjson.Get(data, "index").Int())

		switch deltaType {
		case "text_delta":
			text := gjson.Get(data, "delta.text").String()
			if text != "" {
				chunks = append(chunks, s.buildChunk(text, false, false))
			}

		case "thinking_delta":
			// Include thinking in content
			thinking := gjson.Get(data, "delta.thinking").String()
			if thinking != "" {
				chunks = append(chunks, s.buildChunk(thinking, false, false))
			}

		case "input_json_delta":
			// Accumulate tool call arguments
			partial := gjson.Get(data, "delta.partial_json").String()
			if tc, ok := s.ToolCalls[index]; ok && partial != "" {
				tc.Arguments.WriteString(partial)
				chunks = append(chunks, s.buildToolCallDeltaChunk(index, partial))
			}
		}

	case "content_block_stop":
		// Content block finished, no special handling needed

	case "message_delta":
		// Extract stop_reason
		stopReason := gjson.Get(data, "delta.stop_reason").String()
		if stopReason != "" {
			s.FinishReason = convertClaudeStopReason(stopReason)
		}
		// Extract usage
		s.InputTokens = int(gjson.Get(data, "usage.input_tokens").Int())
		s.OutputTokens = int(gjson.Get(data, "usage.output_tokens").Int())
		if s.InputTokens > 0 || s.OutputTokens > 0 {
			s.UsageSeen = true
		}

	case "message_stop":
		// Send final chunk with finish_reason
		chunks = append(chunks, s.buildFinalChunk())
	}

	return chunks
}

// buildChunk builds a Chat Completions chunk
func (s *claudeToChatStreamState) buildChunk(content string, includeRole bool, isFinal bool) string {
	chunk := types.ChatCompletionsChunk{
		ID:      s.ResponseID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []types.ChatCompletionsChunkChoice{
			{
				Index: 0,
				Delta: types.ChatCompletionsChunkDelta{},
			},
		},
	}

	if includeRole {
		chunk.Choices[0].Delta.Role = "assistant"
	}

	if content != "" {
		chunk.Choices[0].Delta.Content = &content
	}

	if isFinal && s.FinishReason != "" {
		chunk.Choices[0].FinishReason = &s.FinishReason
	}

	jsonBytes, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonBytes))
}

// buildToolCallStartChunk builds a chunk for tool call start
func (s *claudeToChatStreamState) buildToolCallStartChunk(index int, id, name string) string {
	chunk := types.ChatCompletionsChunk{
		ID:      s.ResponseID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []types.ChatCompletionsChunkChoice{
			{
				Index: 0,
				Delta: types.ChatCompletionsChunkDelta{
					ToolCalls: []types.ChatCompletionsToolCall{
						{
							Index: &index,
							ID:    id,
							Type:  "function",
							Function: types.ChatCompletionsToolCallFunction{
								Name:      name,
								Arguments: "",
							},
						},
					},
				},
			},
		},
	}

	jsonBytes, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonBytes))
}

// buildToolCallDeltaChunk builds a chunk for tool call argument delta
func (s *claudeToChatStreamState) buildToolCallDeltaChunk(index int, args string) string {
	chunk := types.ChatCompletionsChunk{
		ID:      s.ResponseID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []types.ChatCompletionsChunkChoice{
			{
				Index: 0,
				Delta: types.ChatCompletionsChunkDelta{
					ToolCalls: []types.ChatCompletionsToolCall{
						{
							Index: &index,
							Function: types.ChatCompletionsToolCallFunction{
								Arguments: args,
							},
						},
					},
				},
			},
		},
	}

	jsonBytes, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonBytes))
}

// buildFinalChunk builds the final chunk with finish_reason
func (s *claudeToChatStreamState) buildFinalChunk() string {
	finishReason := s.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	chunk := types.ChatCompletionsChunk{
		ID:      s.ResponseID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []types.ChatCompletionsChunkChoice{
			{
				Index:        0,
				Delta:        types.ChatCompletionsChunkDelta{},
				FinishReason: &finishReason,
			},
		},
	}

	jsonBytes, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonBytes))
}

// buildFinalChunkWithUsage builds final chunk with usage (when include_usage=true)
func (s *claudeToChatStreamState) buildFinalChunkWithUsage() string {
	finishReason := s.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	chunk := types.ChatCompletionsChunk{
		ID:      s.ResponseID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []types.ChatCompletionsChunkChoice{
			{
				Index:        0,
				Delta:        types.ChatCompletionsChunkDelta{},
				FinishReason: &finishReason,
			},
		},
		Usage: &types.ChatCompletionsUsage{
			PromptTokens:     s.InputTokens,
			CompletionTokens: s.OutputTokens,
			TotalTokens:      s.InputTokens + s.OutputTokens,
		},
	}

	jsonBytes, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonBytes))
}

// ============================================================================
// JSON Helper for Non-Streaming Response
// ============================================================================

// ConvertClaudeJSONToChatJSON converts Claude JSON response to Chat format
func ConvertClaudeJSONToChatJSON(claudeJSON []byte, model string) ([]byte, error) {
	// Parse the raw JSON to extract fields
	result := gjson.ParseBytes(claudeJSON)

	id := result.Get("id").String()
	stopReason := result.Get("stop_reason").String()

	// Build content array
	var content []types.ClaudeContent
	result.Get("content").ForEach(func(_, value gjson.Result) bool {
		block := types.ClaudeContent{
			Type:      value.Get("type").String(),
			Text:      value.Get("text").String(),
			ID:        value.Get("id").String(),
			Name:      value.Get("name").String(),
			ToolUseID: value.Get("tool_use_id").String(),
		}
		if value.Get("input").Exists() {
			block.Input = value.Get("input").Value()
		}
		content = append(content, block)
		return true
	})

	claudeResp := &types.ClaudeResponse{
		ID:         id,
		Content:    content,
		StopReason: stopReason,
	}

	// Parse usage
	if result.Get("usage").Exists() {
		claudeResp.Usage = &types.Usage{
			InputTokens:  int(result.Get("usage.input_tokens").Int()),
			OutputTokens: int(result.Get("usage.output_tokens").Int()),
		}
	}

	chatResp := ConvertClaudeToChatResponse(claudeResp, model)
	return json.Marshal(chatResp)
}

// ConvertClaudeStreamToChat wraps the streaming conversion for io.Reader input
func ConvertClaudeStreamToChat(ctx context.Context, body io.ReadCloser, model string, includeUsage bool) (io.ReadCloser, error) {
	outCh, errCh := ConvertClaudeToChatStream(ctx, body, model, includeUsage)

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		defer body.Close()

		for {
			select {
			case chunk, ok := <-outCh:
				if !ok {
					return
				}
				pw.Write([]byte(chunk))
			case err := <-errCh:
				if err != nil {
					pw.CloseWithError(err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return io.NopCloser(pr), nil
}

// ============================================================================
// OpenAI Stream Passthrough Conversion (for OpenAI upstream)
// ============================================================================

// ConvertOpenAIStreamToChat passes through OpenAI stream with minimal modifications
// Used when upstream is OpenAI (passthrough case)
func ConvertOpenAIStreamToChat(ctx context.Context, reader io.Reader) (<-chan string, <-chan error) {
	outCh := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				// Pass through as-is (OpenAI format)
				select {
				case outCh <- line + "\n\n":
				case <-ctx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return outCh, errCh
}

// Ensure bytes import is used
var _ = bytes.Buffer{}
