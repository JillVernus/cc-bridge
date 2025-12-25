package providers

import (
	"strings"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/types"
)

func TestSanitizeUserText_RemovesToolProtocolTags(t *testing.T) {
	in := "hello <invoke name=\"X\">x</invoke> world <tool_result id=\"y\">z</tool_result>!"
	out := sanitizeUserText(in)
	if strings.Contains(strings.ToLower(out), "<invoke") || strings.Contains(strings.ToLower(out), "<tool_result") {
		t.Fatalf("expected tool protocol tags stripped, got %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("expected surrounding text preserved, got %q", out)
	}
}

func TestNormalizeClaudeBlocks_ToolUseAddsTriggerAndInvoke(t *testing.T) {
	trigger := "<<CALL_abc123>>"
	content := []interface{}{
		map[string]interface{}{
			"type": "tool_use",
			"id":   "toolu_test",
			"name": "Write",
			"input": map[string]interface{}{
				"file_path": "/tmp/a",
			},
		},
	}
	out := normalizeClaudeBlocks(content, trigger, false)
	if !strings.HasPrefix(out, trigger) {
		t.Fatalf("expected trigger prefix, got %q", out)
	}
	if !strings.Contains(out, "<invoke") || !strings.Contains(out, `name="Write"`) {
		t.Fatalf("expected invoke tag, got %q", out)
	}
	if !strings.Contains(out, `<parameter name="file_path">/tmp/a</parameter>`) {
		t.Fatalf("expected parameter, got %q", out)
	}
}

func TestBuildToolsXML_IsDeterministic(t *testing.T) {
	tools := []types.ClaudeTool{
		{
			Name:        "ToolA",
			Description: "desc",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"b": map[string]interface{}{"type": "string"},
					"a": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"a"},
			},
		},
	}
	out1 := buildToolsXML(tools)
	out2 := buildToolsXML(tools)
	if out1 != out2 {
		t.Fatalf("expected deterministic output")
	}
	if strings.Index(out1, `name="a"`) > strings.Index(out1, `name="b"`) {
		t.Fatalf("expected parameters sorted by name, got %q", out1)
	}
}

func TestInjectToolPrompt_UsesBackspaceInAntmlTags(t *testing.T) {
	out := injectToolPrompt([]types.ClaudeTool{{Name: "ToolA", Description: "desc"}}, "<<CALL_abc123>>")
	if strings.Contains(out, `\\b:tools`) || strings.Contains(out, `<antml\\b:`) {
		t.Fatalf("expected no literal \\\\b sequences in injected prompt, got %q", out)
	}
	if !strings.Contains(out, "\b:tools") || !strings.Contains(out, "<antml\b:tools>") {
		t.Fatalf("expected backspace control character in antml tags, got %q", out)
	}
	if strings.Contains(openAIChatThinkingHint, "\\b") || !strings.Contains(openAIChatThinkingHint, "\b") {
		t.Fatalf("expected thinking hint to include backspace control character")
	}
}
