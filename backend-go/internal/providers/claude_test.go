package providers

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

func newClaudeProviderTestContext(path string, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c
}

func TestClaudeProviderConvertToProviderRequest_RedirectsModelByAlias(t *testing.T) {
	provider := &ClaudeProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://api.anthropic.com",
		ModelMapping: map[string]string{
			"opus": "claude-opus-4-5-20251101",
		},
	}

	c := newClaudeProviderTestContext("/v1/messages", `{
		"model":"claude-opus-4-6",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}]
	}`)

	req, _, err := provider.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	got := string(bodyBytes)
	if !strings.Contains(got, `"model":"claude-opus-4-5-20251101"`) {
		t.Fatalf("expected model redirect in body, got: %s", got)
	}
}

func TestClaudeProviderConvertToProviderRequest_PreservesUnknownFields(t *testing.T) {
	provider := &ClaudeProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://api.anthropic.com",
		ModelMapping: map[string]string{
			"opus": "claude-opus-4-5-20251101",
		},
	}

	c := newClaudeProviderTestContext("/v1/complete", `{
		"model":"claude-opus-4-6",
		"prompt":"hello from complete api",
		"max_tokens_to_sample":128,
		"temperature":null
	}`)

	req, _, err := provider.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	got := string(bodyBytes)
	if !strings.Contains(got, `"model":"claude-opus-4-5-20251101"`) {
		t.Fatalf("expected model redirect in body, got: %s", got)
	}
	if !strings.Contains(got, `"prompt":"hello from complete api"`) {
		t.Fatalf("expected unknown field prompt to be preserved, got: %s", got)
	}
	if !strings.Contains(got, `"max_tokens_to_sample":128`) {
		t.Fatalf("expected unknown field max_tokens_to_sample to be preserved, got: %s", got)
	}
	if !strings.Contains(got, `"temperature":null`) {
		t.Fatalf("expected nullable field to be preserved, got: %s", got)
	}
}

func TestClaudeProviderConvertToProviderRequest_PassesThroughNonJSONBody(t *testing.T) {
	provider := &ClaudeProvider{}
	upstream := &config.UpstreamConfig{
		BaseURL: "https://api.anthropic.com",
		ModelMapping: map[string]string{
			"opus": "claude-opus-4-5-20251101",
		},
	}

	c := newClaudeProviderTestContext("/v1/messages", "not-json")
	req, _, err := provider.ConvertToProviderRequest(c, upstream, "test-key")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	if string(bodyBytes) != "not-json" {
		t.Fatalf("expected non-JSON body to pass through unchanged, got %q", string(bodyBytes))
	}
}

func TestClaudeProviderConvertToClaudeResponse_StripsRawThinkingTagsFromText(t *testing.T) {
	provider := &ClaudeProvider{}
	providerResp := &types.ProviderResponse{
		StatusCode: 200,
		Body: []byte(`{
			"id":"msg_1",
			"type":"message",
			"role":"assistant",
			"model":"claude-opus-4-7",
			"content":[{"type":"text","text":"\n<thinking>\ninternal notes\n</thinking>\n\nHey! What can I help you with?"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`),
	}

	claudeResp, err := provider.ConvertToClaudeResponse(providerResp)
	if err != nil {
		t.Fatalf("ConvertToClaudeResponse returned error: %v", err)
	}
	if len(claudeResp.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(claudeResp.Content))
	}
	if strings.Contains(claudeResp.Content[0].Text, "<thinking>") || strings.Contains(claudeResp.Content[0].Text, "</thinking>") {
		t.Fatalf("expected raw thinking tags to be stripped, got %q", claudeResp.Content[0].Text)
	}
	if claudeResp.Content[0].Text != "Hey! What can I help you with?" {
		t.Fatalf("unexpected sanitized text: %q", claudeResp.Content[0].Text)
	}
}

func TestClaudeProviderConvertToClaudeResponse_PreservesProperThinkingContentBlock(t *testing.T) {
	provider := &ClaudeProvider{}
	providerResp := &types.ProviderResponse{
		StatusCode: 200,
		Body: []byte(`{
			"id":"msg_1",
			"type":"message",
			"role":"assistant",
			"model":"claude-opus-4-7",
			"content":[
				{"type":"thinking","thinking":"valid reasoning block"},
				{"type":"text","text":"Thinking works."}
			],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`),
	}

	claudeResp, err := provider.ConvertToClaudeResponse(providerResp)
	if err != nil {
		t.Fatalf("ConvertToClaudeResponse returned error: %v", err)
	}
	if len(claudeResp.Content) != 2 {
		t.Fatalf("expected two content blocks, got %d", len(claudeResp.Content))
	}
	if claudeResp.Content[0].Type != "thinking" || claudeResp.Content[0].Thinking != "valid reasoning block" {
		t.Fatalf("expected proper thinking block to be preserved, got %+v", claudeResp.Content[0])
	}
	if claudeResp.Content[1].Text != "Thinking works." {
		t.Fatalf("expected text block to be preserved, got %+v", claudeResp.Content[1])
	}
}
