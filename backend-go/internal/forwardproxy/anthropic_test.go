package forwardproxy

import (
	"strings"
	"testing"
)

func TestStreamParserWriter_SSEParsing(t *testing.T) {
	// Simulated SSE stream from Anthropic API
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_01XFDUDYJgAACzvnptvVoYEL","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":25,"cache_creation_input_tokens":0,"cache_read_input_tokens":100,"output_tokens":1}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	parser := NewStreamParserWriter()
	_, err := parser.Write([]byte(sseData))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	usage := parser.GetUsage()

	// Verify message ID
	if parser.GetMessageID() != "msg_01XFDUDYJgAACzvnptvVoYEL" {
		t.Errorf("MessageID = %q, want %q", parser.GetMessageID(), "msg_01XFDUDYJgAACzvnptvVoYEL")
	}

	// Verify model
	if usage.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", usage.Model, "claude-sonnet-4-20250514")
	}

	// Verify input tokens (from message_start)
	if usage.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want %d", usage.InputTokens, 25)
	}

	// Verify output tokens from message_delta (correct final count, not the initial 1)
	if usage.OutputTokens != 12 {
		t.Errorf("OutputTokens = %d, want %d", usage.OutputTokens, 12)
	}

	// Verify cache tokens
	if usage.CacheReadInputTokens != 100 {
		t.Errorf("CacheReadInputTokens = %d, want %d", usage.CacheReadInputTokens, 100)
	}
}

func TestStreamParserWriter_OutputTokensFromDelta(t *testing.T) {
	// Verify that output_tokens from message_delta overrides the initial value
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_test","type":"message","model":"claude-sonnet-4-20250514","usage":{"input_tokens":50,"output_tokens":1}}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":42}}`,
		"",
	}, "\n")

	parser := NewStreamParserWriter()
	parser.Write([]byte(sseData))

	usage := parser.GetUsage()

	// Output tokens should be 42 (from delta), not 1 (from start)
	if usage.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want %d (from message_delta, not message_start)", usage.OutputTokens, 42)
	}
}

func TestStreamParserWriter_FlushOnGetUsage(t *testing.T) {
	// Simulate a stream that ends without a trailing newline after the last data line
	sseData := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_notrail","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":1}}}` +
		"\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":99}}` // no trailing newline

	parser := NewStreamParserWriter()
	parser.Write([]byte(sseData))

	usage := parser.GetUsage()

	if parser.GetMessageID() != "msg_notrail" {
		t.Errorf("MessageID = %q, want %q", parser.GetMessageID(), "msg_notrail")
	}
	if usage.OutputTokens != 99 {
		t.Errorf("OutputTokens = %d, want %d (should be flushed from unterminated last line)", usage.OutputTokens, 99)
	}
}

func TestParseJSONResponse(t *testing.T) {
	body := []byte(`{
		"id": "msg_json_test",
		"type": "message",
		"model": "claude-sonnet-4-20250514",
		"content": [{"type": "text", "text": "Hello!"}],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 10,
			"cache_read_input_tokens": 200
		}
	}`)

	messageID, usage := parseJSONResponse(body)

	if messageID != "msg_json_test" {
		t.Errorf("MessageID = %q, want %q", messageID, "msg_json_test")
	}
	if usage.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", usage.Model, "claude-sonnet-4-20250514")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want %d", usage.InputTokens, 100)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want %d", usage.OutputTokens, 50)
	}
	if usage.CacheCreationInputTokens != 10 {
		t.Errorf("CacheCreationInputTokens = %d, want %d", usage.CacheCreationInputTokens, 10)
	}
	if usage.CacheReadInputTokens != 200 {
		t.Errorf("CacheReadInputTokens = %d, want %d", usage.CacheReadInputTokens, 200)
	}
}

func TestParseJSONResponse_InvalidJSON(t *testing.T) {
	_, usage := parseJSONResponse([]byte("not json"))

	if usage.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", usage.InputTokens)
	}
}

func TestStreamUsage_FieldMapping(t *testing.T) {
	// Verify that StreamUsage fields are accessible for completion record creation
	parser := NewStreamParserWriter()
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_123","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"cache_creation_input_tokens":10,"cache_read_input_tokens":200,"output_tokens":1}}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":50}}`,
		"",
	}, "\n")
	parser.Write([]byte(sseData))

	usage := parser.GetUsage()

	if usage.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", usage.Model, "claude-sonnet-4-20250514")
	}
	totalTokens := usage.InputTokens + usage.OutputTokens +
		usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	if totalTokens != 360 {
		t.Errorf("TotalTokens = %d, want %d", totalTokens, 360)
	}
}

func TestExtractClientSession(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantUser string
		wantSess string
	}{
		{
			name:     "compound user_id with session",
			body:     []byte(`{"metadata":{"user_id":"user_abc123_account__session_550e8400-e29b-41d4-a716-446655440000"}}`),
			wantUser: "user_abc123",
			wantSess: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "user_id without session",
			body:     []byte(`{"metadata":{"user_id":"user_abc123"}}`),
			wantUser: "user_abc123",
			wantSess: "",
		},
		{
			name:     "empty body",
			body:     nil,
			wantUser: "",
			wantSess: "",
		},
		{
			name:     "no metadata field",
			body:     []byte(`{"model":"claude-sonnet-4-20250514"}`),
			wantUser: "",
			wantSess: "",
		},
		{
			name:     "invalid JSON",
			body:     []byte(`not json`),
			wantUser: "",
			wantSess: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotSess := extractClientSession(tt.body)
			if gotUser != tt.wantUser {
				t.Errorf("clientID = %q, want %q", gotUser, tt.wantUser)
			}
			if gotSess != tt.wantSess {
				t.Errorf("sessionID = %q, want %q", gotSess, tt.wantSess)
			}
		})
	}
}

func TestParseClaudeCodeUserID(t *testing.T) {
	tests := []struct {
		input    string
		wantUser string
		wantSess string
	}{
		{"user_abc_account__session_uuid-123", "user_abc", "uuid-123"},
		{"user_abc", "user_abc", ""},
		{"", "", ""},
		{"  user_abc_account__session_uuid-123  ", "user_abc", "uuid-123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotUser, gotSess := parseClaudeCodeUserID(tt.input)
			if gotUser != tt.wantUser {
				t.Errorf("userID = %q, want %q", gotUser, tt.wantUser)
			}
			if gotSess != tt.wantSess {
				t.Errorf("sessionID = %q, want %q", gotSess, tt.wantSess)
			}
		})
	}
}
