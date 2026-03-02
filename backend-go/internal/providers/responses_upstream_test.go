package providers

import (
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
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
	if reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning.effort=high, got %#v", reasoning)
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
