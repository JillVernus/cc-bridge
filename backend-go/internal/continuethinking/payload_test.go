package continuethinking

import "testing"

func TestCommentaryMessage(t *testing.T) {
	m := CommentaryMessage("keep going")
	if m["type"] != "message" || m["role"] != "assistant" || m["phase"] != "commentary" {
		t.Errorf("bad marker shape: %+v", m)
	}
	content, ok := m["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("bad content: %+v", m["content"])
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "output_text" || part["text"] != "keep going" {
		t.Errorf("bad part: %+v", part)
	}
}

func TestBuildRoundPayload_ForceEncrypted(t *testing.T) {
	base := map[string]any{"model": "gpt-5", "input": []any{"orig"}, "previous_response_id": "resp_1"}
	payload := BuildRoundPayload(base, []any{"a", "b"}, true)

	if payload["model"] != "gpt-5" {
		t.Errorf("model not preserved")
	}
	if payload["stream"] != true {
		t.Errorf("stream not forced true")
	}
	if input, ok := payload["input"].([]any); !ok || len(input) != 2 || input[0] != "a" {
		t.Errorf("input not set to passed items: %+v", payload["input"])
	}
	if _, exists := payload["previous_response_id"]; exists {
		t.Errorf("previous_response_id should be dropped")
	}
	inc, ok := payload["include"].([]any)
	if !ok {
		t.Fatalf("include not a slice")
	}
	found := false
	for _, it := range inc {
		if it == ReasoningTokensInclude {
			found = true
		}
	}
	if !found {
		t.Errorf("encrypted reasoning not added to include: %+v", inc)
	}
	// base must not be mutated
	if _, exists := base["previous_response_id"]; !exists {
		t.Errorf("base body was mutated (previous_response_id removed)")
	}
}

func TestBuildRoundPayload_MergesExistingInclude(t *testing.T) {
	base := map[string]any{"include": []any{"reasoning.encrypted_content"}}
	payload := BuildRoundPayload(base, nil, false)
	inc, _ := payload["include"].([]any)
	if len(inc) != 1 || inc[0] != ReasoningTokensInclude {
		t.Errorf("existing include not preserved: %+v", inc)
	}
}

func TestBuildRoundPayload_NoIncludeWhenNotForcedAndAbsent(t *testing.T) {
	base := map[string]any{"model": "gpt-5"}
	payload := BuildRoundPayload(base, nil, false)
	if _, exists := payload["include"]; exists {
		t.Errorf("include should be absent when not forced and not present")
	}
}
