package providers

import (
	"strings"
	"testing"
)

func TestOpenAIChatToolifyParser_ToolCall(t *testing.T) {
	trigger := "<<CALL_abc123>>"
	input := "hello " + trigger + "\n<invoke name=\"Write\">\n<parameter name=\"file_path\">/tmp/a</parameter>\n<parameter name=\"payload\">{\"a\":1}</parameter>\n</invoke>"

	p := newOpenAIChatToolifyParser(trigger, false)
	p.FeedString(input)
	p.Finish()
	events := p.ConsumeEvents()

	var textParts []string
	var toolCalls []openAIChatParsedInvokeCall
	for _, ev := range events {
		switch ev.Type {
		case "text":
			textParts = append(textParts, ev.Content)
		case "tool_call":
			if ev.Call != nil {
				toolCalls = append(toolCalls, *ev.Call)
			}
		}
	}

	if strings.Join(textParts, "") != "hello " {
		t.Fatalf("expected text before trigger, got %q", strings.Join(textParts, ""))
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "Write" {
		t.Fatalf("expected tool name Write, got %q", toolCalls[0].Name)
	}
	if toolCalls[0].Arguments["file_path"] != "/tmp/a" {
		t.Fatalf("expected file_path /tmp/a, got %#v", toolCalls[0].Arguments["file_path"])
	}
	if arg, ok := toolCalls[0].Arguments["payload"].(map[string]interface{}); !ok || arg["a"] != float64(1) {
		t.Fatalf("expected payload JSON to parse, got %#v", toolCalls[0].Arguments["payload"])
	}
}

func TestOpenAIChatToolifyParser_FlushesTextInToolModeWithoutTrigger(t *testing.T) {
	trigger := "<<CALL_zzzzzzzzzzzz>>"
	p := newOpenAIChatToolifyParser(trigger, false)

	long := strings.Repeat("a", 600)
	p.FeedString(long)

	events := p.ConsumeEvents()
	if len(events) == 0 {
		t.Fatalf("expected some events before Finish() due to streaming flush, got none")
	}

	p.Finish()
	events = append(events, p.ConsumeEvents()...)

	var out strings.Builder
	for _, ev := range events {
		if ev.Type == "text" {
			out.WriteString(ev.Content)
		}
	}

	if out.String() != long {
		t.Fatalf("expected roundtrip text, got len=%d", len(out.String()))
	}
}

func TestOpenAIChatToolifyParser_ThinkingBlocks(t *testing.T) {
	trigger := "<<CALL_abc123>>"
	tests := []struct {
		name  string
		input string
	}{
		{name: "ThinkingTag", input: "a<thinking>hi</thinking>b"},
		{name: "AntmlThinkingTagLiteralBackslashB", input: `a<antml\b:thinking>hi</antml\b:thinking>b`},
		{name: "AntmlThinkingTagBackspace", input: "a<antml\b:thinking>hi</antml\b:thinking>b"},
		{name: "AntmlThinkingTagNoBackspace", input: "a<antml:thinking>hi</antml:thinking>b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newOpenAIChatToolifyParser(trigger, true)
			p.FeedString(tt.input)
			p.Finish()
			events := p.ConsumeEvents()

			got := make([]string, 0, len(events))
			for _, ev := range events {
				switch ev.Type {
				case "text":
					got = append(got, "text:"+ev.Content)
				case "thinking":
					got = append(got, "thinking:"+ev.Content)
				}
			}

			want := []string{"text:a", "thinking:hi", "text:b"}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("expected %v, got %v", want, got)
			}
		})
	}
}
