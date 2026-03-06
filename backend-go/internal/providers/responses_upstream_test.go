package providers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

func TestConvertMessagesToInput_PreservesMixedOrder(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "before"},
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_1",
					"name":  "get_weather",
					"input": map[string]interface{}{"city": "NYC"},
				},
				map[string]interface{}{"type": "text", "text": "between"},
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "call_1",
					"content":     "sunny",
				},
			},
		},
	})

	if len(input) != 4 {
		t.Fatalf("expected 4 items, got %d", len(input))
	}

	if input[0]["type"] != "message" || input[0]["content"] != "before" {
		t.Fatalf("unexpected first item: %#v", input[0])
	}
	if input[1]["type"] != "function_call" || input[1]["call_id"] != "call_1" {
		t.Fatalf("unexpected second item: %#v", input[1])
	}
	if input[2]["type"] != "message" || input[2]["content"] != "between" {
		t.Fatalf("unexpected third item: %#v", input[2])
	}
	if input[3]["type"] != "function_call_output" || input[3]["call_id"] != "call_1" {
		t.Fatalf("unexpected fourth item: %#v", input[3])
	}
}

func TestConvertMessagesToInput_MultipleToolUseNoCollapse(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_1",
					"name":  "tool_a",
					"input": map[string]interface{}{"x": 1},
				},
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_2",
					"name":  "tool_b",
					"input": map[string]interface{}{"y": 2},
				},
			},
		},
	})

	if len(input) != 2 {
		t.Fatalf("expected 2 function_call items, got %d", len(input))
	}
	if input[0]["type"] != "function_call" || input[1]["type"] != "function_call" {
		t.Fatalf("expected two function_call items, got %#v", input)
	}
}

func TestConvertMessagesToInput_MultipleToolResultNoCollapse(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_1", "content": "r1"},
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_2", "content": "r2"},
			},
		},
	})

	if len(input) != 2 {
		t.Fatalf("expected 2 function_call_output items, got %d", len(input))
	}
	if input[0]["type"] != "function_call_output" || input[1]["type"] != "function_call_output" {
		t.Fatalf("expected two function_call_output items, got %#v", input)
	}
}

func TestConvertMessagesToInput_ToolResultErrorPreserved(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "call_err",
					"content":     "Exit code 128\nfatal: pathspec not found",
					"is_error":    true,
				},
			},
		},
	})

	if len(input) != 1 {
		t.Fatalf("expected 1 function_call_output item, got %d", len(input))
	}
	if input[0]["type"] != "function_call_output" || input[0]["call_id"] != "call_err" {
		t.Fatalf("unexpected item: %#v", input[0])
	}

	output, ok := input[0]["output"].(string)
	if !ok {
		t.Fatalf("expected output string, got %T", input[0]["output"])
	}

	var outputPayload map[string]interface{}
	if err := json.Unmarshal([]byte(output), &outputPayload); err != nil {
		t.Fatalf("expected JSON output payload for error tool_result, got %q: %v", output, err)
	}
	if outputPayload["is_error"] != true {
		t.Fatalf("expected is_error=true in output payload, got %#v", outputPayload)
	}
	if outputPayload["content"] != "Exit code 128\nfatal: pathspec not found" {
		t.Fatalf("unexpected output payload content: %#v", outputPayload["content"])
	}
}

func TestConvertMessagesToInput_RepeatedToolCallsPreserved(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_same",
					"name":  "tool_x",
					"input": map[string]interface{}{"v": 1},
				},
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_same",
					"name":  "tool_x",
					"input": map[string]interface{}{"v": 1},
				},
			},
		},
	})

	if len(input) != 2 {
		t.Fatalf("expected repeated tool calls preserved as 2 items, got %d", len(input))
	}
}

func TestConvertMessagesToInput_TextOnlyRegression(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	input := p.convertMessagesToInput([]types.ClaudeMessage{
		{Role: "user", Content: "hello"},
	})

	if len(input) != 1 {
		t.Fatalf("expected 1 item, got %d", len(input))
	}
	if input[0]["type"] != "message" || input[0]["role"] != "user" || input[0]["content"] != "hello" {
		t.Fatalf("unexpected text-only conversion: %#v", input[0])
	}
}

func TestConvertToResponsesRequest_ToolsAndToolChoice(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	req := &types.ClaudeRequest{
		Model:    "claude-test",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Tools: []types.ClaudeTool{
			{Name: "get_weather", Description: "weather", InputSchema: map[string]interface{}{"type": "object"}},
			{Type: "web_search_20250305"},
		},
	}
	raw := map[string]interface{}{
		"tools": []interface{}{
			map[string]interface{}{
				"name":        "get_weather",
				"description": "weather",
				"input_schema": map[string]interface{}{
					"type": "object",
				},
			},
			map[string]interface{}{
				"type": "web_search_20250305",
			},
		},
		"tool_choice": map[string]interface{}{
			"type": "tool",
			"name": "get_weather",
		},
	}

	out := p.convertToResponsesRequest(req, upstream, raw)
	tools, ok := out["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected tools as []map[string]interface{}, got %T", out["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 converted tools, got %d", len(tools))
	}
	if tools[0]["type"] != "function" || tools[0]["name"] != "get_weather" {
		t.Fatalf("unexpected first tool: %#v", tools[0])
	}
	if tools[1]["type"] != "web_search_preview" {
		t.Fatalf("unexpected second tool: %#v", tools[1])
	}

	toolChoice, ok := out["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_choice object, got %T", out["tool_choice"])
	}
	if toolChoice["type"] != "function" || toolChoice["name"] != "get_weather" {
		t.Fatalf("unexpected tool_choice mapping: %#v", toolChoice)
	}
}

func TestConvertToResponsesRequest_ToolChoiceStringMapping(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	tests := []struct {
		name  string
		input interface{}
		check func(t *testing.T, out map[string]interface{})
	}{
		{
			name:  "auto",
			input: "auto",
			check: func(t *testing.T, out map[string]interface{}) {
				if out["tool_choice"] != "auto" {
					t.Fatalf("expected auto, got %#v", out["tool_choice"])
				}
			},
		},
		{
			name:  "any_to_required",
			input: "any",
			check: func(t *testing.T, out map[string]interface{}) {
				if out["tool_choice"] != "required" {
					t.Fatalf("expected required, got %#v", out["tool_choice"])
				}
			},
		},
		{
			name:  "none",
			input: "none",
			check: func(t *testing.T, out map[string]interface{}) {
				if out["tool_choice"] != "none" {
					t.Fatalf("expected none, got %#v", out["tool_choice"])
				}
			},
		},
		{
			name:  "named_tool_string",
			input: "my_tool",
			check: func(t *testing.T, out map[string]interface{}) {
				obj, ok := out["tool_choice"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected object tool_choice, got %T", out["tool_choice"])
				}
				if obj["type"] != "function" || obj["name"] != "my_tool" {
					t.Fatalf("unexpected named tool mapping: %#v", obj)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &types.ClaudeRequest{
				Model:      "claude-test",
				Messages:   []types.ClaudeMessage{{Role: "user", Content: "hi"}},
				ToolChoice: tt.input,
			}
			out := p.convertToResponsesRequest(req, upstream, nil)
			tt.check(t, out)
		})
	}
}

func TestConvertToResponsesRequest_DefaultToolChoiceAutoWhenToolsPresent(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	req := &types.ClaudeRequest{
		Model:    "claude-test",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Tools: []types.ClaudeTool{
			{
				Name:        "Bash",
				Description: "run shell commands",
				InputSchema: map[string]interface{}{"type": "object"},
			},
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)
	if out["tool_choice"] != "auto" {
		t.Fatalf("expected default tool_choice=auto when tools are present, got %#v", out["tool_choice"])
	}
}

func TestConvertToResponsesRequest_TopPAndThinkingMapping(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	req := &types.ClaudeRequest{
		Model:    "claude-test",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type:         "enabled",
			BudgetTokens: 1000,
		},
	}
	raw := map[string]interface{}{
		"top_p": 0.85,
	}

	out := p.convertToResponsesRequest(req, upstream, raw)

	topP, ok := out["top_p"].(float64)
	if !ok {
		t.Fatalf("expected top_p in output, got %#v", out["top_p"])
	}
	if topP != 0.85 {
		t.Fatalf("expected top_p 0.85, got %v", topP)
	}

	reasoning, ok := out["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning object, got %#v", out["reasoning"])
	}
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("expected reasoning.effort=xhigh, got %#v", reasoning)
	}
}

func TestConvertToResponsesRequest_ThinkingDisabledOmitted(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	req := &types.ClaudeRequest{
		Model:    "claude-test",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type:         "disabled",
			BudgetTokens: 1000,
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)
	if _, ok := out["reasoning"]; ok {
		t.Fatalf("expected reasoning omitted when thinking is disabled")
	}
}

func TestConvertToResponsesRequest_ThinkingEffortOverride(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{}

	req := &types.ClaudeRequest{
		Model:    "claude-test",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type:         "enabled",
			BudgetTokens: 1000,
			Effort:       "high",
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)
	reasoning, ok := out["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning object, got %#v", out["reasoning"])
	}
	if reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning.effort=high override, got %#v", reasoning)
	}
}

func TestConvertToResponsesRequest_ModelSuffixToReasoning(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		ModelMapping: map[string]string{
			"claude-opus-4-6": "gpt-5.3-codex(xhigh)",
		},
	}

	req := &types.ClaudeRequest{
		Model:    "claude-opus-4-6",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type: "adaptive",
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)

	model, _ := out["model"].(string)
	if model != "gpt-5.3-codex" {
		t.Fatalf("expected model stripped from suffix, got %q", model)
	}

	reasoning, ok := out["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning from model suffix, got %#v", out["reasoning"])
	}
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("expected reasoning.effort=xhigh from suffix, got %#v", reasoning)
	}
}

func TestConvertToResponsesRequest_ModelSuffixOverridesThinkingReasoning(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		ModelMapping: map[string]string{
			"claude-opus-4-6": "gpt-5.3-codex(low)",
		},
	}

	req := &types.ClaudeRequest{
		Model:    "claude-opus-4-6",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type:         "enabled",
			BudgetTokens: 1000,
			Effort:       "xhigh",
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)
	reasoning, ok := out["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning object, got %#v", out["reasoning"])
	}
	if reasoning["effort"] != "low" {
		t.Fatalf("expected model suffix to override thinking effort, got %#v", reasoning)
	}
}

func TestConvertToResponsesRequest_ModelSuffixAppliesWhenThinkingDisabled(t *testing.T) {
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		ModelMapping: map[string]string{
			"claude-opus-4-6": "gpt-5.3-codex(xhigh)",
		},
	}

	req := &types.ClaudeRequest{
		Model:    "claude-opus-4-6",
		Messages: []types.ClaudeMessage{{Role: "user", Content: "hi"}},
		Thinking: &types.ClaudeThinking{
			Type: "disabled",
		},
	}

	out := p.convertToResponsesRequest(req, upstream, nil)
	model, _ := out["model"].(string)
	if model != "gpt-5.3-codex" {
		t.Fatalf("expected model stripped from suffix, got %q", model)
	}

	reasoning, ok := out["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning from model suffix, got %#v", out["reasoning"])
	}
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("expected reasoning.effort=xhigh from suffix even when thinking disabled, got %#v", reasoning)
	}
}

func TestConvertToClaudeResponse_UsageFromResponsesShape(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	providerResp := &types.ProviderResponse{
		StatusCode: 200,
		Body: []byte(`{
			"id":"resp_123",
			"status":"completed",
			"model":"gpt-5-codex",
			"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],
			"usage":{
				"input_tokens":120,
				"input_tokens_details":{"cached_tokens":20},
				"output_tokens":30,
				"total_tokens":150
			}
		}`),
	}

	claudeResp, err := p.ConvertToClaudeResponse(providerResp)
	if err != nil {
		t.Fatalf("ConvertToClaudeResponse returned error: %v", err)
	}
	if claudeResp.Usage == nil {
		t.Fatal("expected usage in Claude response")
	}
	if claudeResp.Usage.InputTokens != 100 {
		t.Fatalf("expected input_tokens=100 (input-cached), got %d", claudeResp.Usage.InputTokens)
	}
	if claudeResp.Usage.OutputTokens != 30 {
		t.Fatalf("expected output_tokens=30, got %d", claudeResp.Usage.OutputTokens)
	}
	if claudeResp.Usage.CacheReadInputTokens != 20 {
		t.Fatalf("expected cache_read_input_tokens=20, got %d", claudeResp.Usage.CacheReadInputTokens)
	}
	if claudeResp.Usage.TotalTokens != 150 {
		t.Fatalf("expected total_tokens=150, got %d", claudeResp.Usage.TotalTokens)
	}
}

func TestConvertToClaudeResponse_UsageLegacyPromptCompletionFallback(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	providerResp := &types.ProviderResponse{
		StatusCode: 200,
		Body: []byte(`{
			"id":"resp_legacy",
			"status":"completed",
			"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"prompt_tokens":12,"completion_tokens":8}
		}`),
	}

	claudeResp, err := p.ConvertToClaudeResponse(providerResp)
	if err != nil {
		t.Fatalf("ConvertToClaudeResponse returned error: %v", err)
	}
	if claudeResp.Usage == nil {
		t.Fatal("expected usage in Claude response")
	}
	if claudeResp.Usage.InputTokens != 12 {
		t.Fatalf("expected input_tokens=12 from prompt_tokens, got %d", claudeResp.Usage.InputTokens)
	}
	if claudeResp.Usage.OutputTokens != 8 {
		t.Fatalf("expected output_tokens=8 from completion_tokens, got %d", claudeResp.Usage.OutputTokens)
	}
	if claudeResp.Usage.TotalTokens != 20 {
		t.Fatalf("expected total_tokens derived as 20, got %d", claudeResp.Usage.TotalTokens)
	}
}

func collectStreamOutput(t *testing.T, p *ResponsesUpstreamProvider, sse string) string {
	t.Helper()

	eventChan, errChan, err := p.HandleStreamResponse(io.NopCloser(strings.NewReader(sse)))
	if err != nil {
		t.Fatalf("HandleStreamResponse returned error: %v", err)
	}

	var out strings.Builder
	for ev := range eventChan {
		out.WriteString(ev)
	}

	select {
	case streamErr := <-errChan:
		if streamErr != nil {
			t.Fatalf("stream returned error: %v", streamErr)
		}
	default:
	}

	return out.String()
}

func TestHandleStreamResponse_ClaudeSSESequenceSingleStopAndMessageStop(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"!"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi!"}]}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3},"output_tokens":2,"total_tokens":12}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_start") != 1 {
		t.Fatalf("expected exactly one content_block_start, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_stop") != 1 {
		t.Fatalf("expected exactly one content_block_stop, got output: %s", out)
	}
	if strings.Count(out, "event: message_delta") != 1 {
		t.Fatalf("expected exactly one message_delta, got output: %s", out)
	}
	if strings.Count(out, "event: message_stop") != 1 {
		t.Fatalf("expected exactly one message_stop, got output: %s", out)
	}
	if !strings.Contains(out, `"cache_read_input_tokens":3`) {
		t.Fatalf("expected message_delta usage to include cache_read_input_tokens=3, got output: %s", out)
	}
	if !strings.Contains(out, `"input_tokens":7`) {
		t.Fatalf("expected message_delta usage input_tokens adjusted by cache (7), got output: %s", out)
	}
}

func TestHandleStreamResponse_CompletedWithoutDeltaStillHasMessageStop(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":1,"total_tokens":9}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_start") != 0 {
		t.Fatalf("expected no content_block_start when no text deltas, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_stop") != 0 {
		t.Fatalf("expected no content_block_stop when no text deltas, got output: %s", out)
	}
	if strings.Count(out, "event: message_delta") != 1 {
		t.Fatalf("expected exactly one message_delta, got output: %s", out)
	}
	if strings.Count(out, "event: message_stop") != 1 {
		t.Fatalf("expected exactly one message_stop, got output: %s", out)
	}
}

func TestHandleStreamResponse_ClosesAfterCompletedWithoutWaitingForEOF(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	reader, writer := io.Pipe()
	eventChan, errChan, err := p.HandleStreamResponse(reader)
	if err != nil {
		t.Fatalf("HandleStreamResponse returned error: %v", err)
	}

	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		for ev := range eventChan {
			out.WriteString(ev)
		}
		done <- out.String()
	}()

	_, _ = io.WriteString(writer, strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
	}, "\n"))

	select {
	case out := <-done:
		if strings.Count(out, "event: message_stop") != 1 {
			t.Fatalf("expected exactly one message_stop, got output: %s", out)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected stream to close after response.completed without waiting for EOF")
	}

	select {
	case streamErr, ok := <-errChan:
		if ok && streamErr != nil {
			t.Fatalf("unexpected stream error: %v", streamErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected err channel to close after response.completed")
	}

	_ = writer.Close()
}

func TestHandleStreamResponse_MessageStartUsesUpstreamModel(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.3-codex","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if !strings.Contains(out, `"model":"gpt-5.3-codex"`) {
		t.Fatalf("expected message_start model to be upstream model, got output: %s", out)
	}
	if strings.Contains(out, `"model":"responses"`) {
		t.Fatalf("unexpected fallback model 'responses' in output: %s", out)
	}
}

func TestHandleStreamResponse_MessageStartDefersUntilModelAvailable(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.in_progress","response":{"id":"resp_1","model":"gpt-5.3-codex","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if !strings.Contains(out, `"model":"gpt-5.3-codex"`) {
		t.Fatalf("expected message_start model to come from in_progress event, got output: %s", out)
	}
	if strings.Contains(out, `"model":"responses"`) {
		t.Fatalf("unexpected fallback model 'responses' in output: %s", out)
	}
}

func TestHandleStreamResponse_MessageStartIncludesReasoningEffortDisplay(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if !strings.Contains(out, `"model":"gpt-5.3-codex (xhigh)"`) {
		t.Fatalf("expected message_start model to include reasoning effort display, got output: %s", out)
	}
}

func TestHandleStreamResponse_MessageStartIncludesDelayedReasoningEffortDisplay(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.3-codex","status":"in_progress"}}`,
		``,
		`data: {"type":"response.in_progress","response":{"id":"resp_1","model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if !strings.Contains(out, `"model":"gpt-5.3-codex (xhigh)"`) {
		t.Fatalf("expected delayed reasoning effort in message_start model, got output: %s", out)
	}
}

func TestHandleStreamResponse_TranslatesFunctionCallToClaudeToolUseStream(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","status":"in_progress","arguments":"","call_id":"call_1","name":"exec_command"},"output_index":2}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"cmd\":\"echo hi\"}","output_index":2}`,
		``,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"cmd\":\"echo hi\"}","output_index":2}`,
		``,
		`data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","status":"completed","arguments":"{\"cmd\":\"echo hi\"}","call_id":"call_1","name":"exec_command"},"output_index":2}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"echo hi\"}"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if !strings.Contains(out, `"model":"gpt-5.3-codex (xhigh)"`) {
		t.Fatalf("expected message_start model to include reasoning effort display, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_start") != 1 {
		t.Fatalf("expected exactly one content_block_start for tool_use, got output: %s", out)
	}
	if !strings.Contains(out, `"type":"tool_use"`) || !strings.Contains(out, `"name":"exec_command"`) || !strings.Contains(out, `"id":"call_1"`) {
		t.Fatalf("expected tool_use start block with id/name, got output: %s", out)
	}
	if !strings.Contains(out, `"type":"input_json_delta"`) || !strings.Contains(out, `\"cmd\":\"echo hi\"`) {
		t.Fatalf("expected tool arguments to stream as input_json_delta, got output: %s", out)
	}
	if strings.Count(out, "event: content_block_stop") != 1 {
		t.Fatalf("expected exactly one content_block_stop for tool_use, got output: %s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected stop_reason=tool_use, got output: %s", out)
	}
	if strings.Count(out, "event: message_stop") != 1 {
		t.Fatalf("expected exactly one message_stop, got output: %s", out)
	}
}

func TestHandleStreamResponse_ToolUseIncludesDelayedReasoningEffortDisplay(t *testing.T) {
	p := &ResponsesUpstreamProvider{}

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.3-codex","status":"in_progress"}}`,
		``,
		`data: {"type":"response.in_progress","response":{"id":"resp_1","model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","status":"in_progress","arguments":"","call_id":"call_1","name":"exec_command"},"output_index":2}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"cmd\":\"echo hi\"}","output_index":2}`,
		``,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"cmd\":\"echo hi\"}","output_index":2}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","reasoning":{"effort":"xhigh"},"status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"echo hi\"}"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`,
		``,
	}, "\n")

	out := collectStreamOutput(t, p, sse)

	if strings.Count(out, "event: message_start") != 1 {
		t.Fatalf("expected exactly one message_start, got output: %s", out)
	}
	if !strings.Contains(out, `"model":"gpt-5.3-codex (xhigh)"`) {
		t.Fatalf("expected delayed reasoning effort in message_start model for tool_use path, got output: %s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected stop_reason=tool_use, got output: %s", out)
	}
}

func TestConvertToProviderRequest_AutoSetsPromptCacheKeyFromSessionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://example.com",
	}

	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":true,
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session_id", "sess-bridge-123")
	c.Request = req

	providerReq, _, err := p.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}
	defer providerReq.Body.Close()

	outBytes, err := io.ReadAll(providerReq.Body)
	if err != nil {
		t.Fatalf("failed to read converted provider body: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(outBytes, &out); err != nil {
		t.Fatalf("failed to parse converted provider body: %v", err)
	}

	if out["prompt_cache_key"] != "sess-bridge-123" {
		t.Fatalf("expected prompt_cache_key from Session_id header, got %#v", out["prompt_cache_key"])
	}
}

func TestConvertToProviderRequest_AutoSetsPromptCacheKeyFromMetadataSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://example.com",
	}

	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":true,
		"max_tokens":64,
		"metadata":{"user_id":"user_abc_account__session_sess-meta-456"},
		"messages":[{"role":"user","content":"hello"}]
	}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	providerReq, _, err := p.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}
	defer providerReq.Body.Close()

	outBytes, err := io.ReadAll(providerReq.Body)
	if err != nil {
		t.Fatalf("failed to read converted provider body: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(outBytes, &out); err != nil {
		t.Fatalf("failed to parse converted provider body: %v", err)
	}

	if out["prompt_cache_key"] != "sess-meta-456" {
		t.Fatalf("expected prompt_cache_key parsed from metadata.user_id session suffix, got %#v", out["prompt_cache_key"])
	}
}

func TestConvertToProviderRequest_DoesNotUsePlainMetadataUserIDAsPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &ResponsesUpstreamProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://example.com",
	}

	body := []byte(`{
		"model":"claude-opus-4-6",
		"stream":true,
		"max_tokens":64,
		"metadata":{"user_id":"plain-user-id"},
		"messages":[{"role":"user","content":"hello"}]
	}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	providerReq, _, err := p.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}
	defer providerReq.Body.Close()

	outBytes, err := io.ReadAll(providerReq.Body)
	if err != nil {
		t.Fatalf("failed to read converted provider body: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(outBytes, &out); err != nil {
		t.Fatalf("failed to parse converted provider body: %v", err)
	}

	if _, ok := out["prompt_cache_key"]; ok {
		t.Fatalf("expected no prompt_cache_key for plain metadata.user_id, got %#v", out["prompt_cache_key"])
	}
}
