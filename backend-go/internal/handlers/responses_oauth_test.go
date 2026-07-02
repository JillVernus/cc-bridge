package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

func TestBuildCodexOAuthRequest_ForcesStoreFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name string
		body string
	}{
		{
			name: "strips max_output_tokens",
			body: `{
				"model":"gpt-5.4",
				"stream":true,
				"store":true,
				"max_output_tokens":123,
				"metadata":{"source":"test"}
			}`,
		},
		{
			name: "strips max_tokens",
			body: `{
				"model":"gpt-5.4",
				"stream":true,
				"store":true,
				"max_tokens":456,
				"metadata":{"source":"test"}
			}`,
		},
		{
			name: "strips both max token fields",
			body: `{
				"model":"gpt-5.4",
				"stream":true,
				"store":true,
				"max_output_tokens":123,
				"max_tokens":456,
				"metadata":{"source":"test"}
			}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(nil))

			responsesReq := types.ResponsesRequest{
				Model:  "gpt-5.4",
				Stream: true,
			}

			req, _, err := buildCodexOAuthRequest(
				c,
				nil,
				&config.UpstreamConfig{ServiceType: "openai-oauth"},
				[]byte(tc.body),
				responsesReq,
				"access-token",
				"account-1",
				false,
				false,
			)
			if err != nil {
				t.Fatalf("buildCodexOAuthRequest returned error: %v", err)
			}

			payloadBytes, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("failed to read built request body: %v", err)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				t.Fatalf("failed to parse built request body: %v", err)
			}

			store, ok := payload["store"].(bool)
			if !ok {
				t.Fatalf("expected built request to include boolean store, got %#v", payload["store"])
			}
			if store {
				t.Fatalf("expected built request store=false, got true")
			}
			if maxOutputTokens, exists := payload["max_output_tokens"]; exists {
				t.Fatalf("expected max_output_tokens to be stripped, got %#v", maxOutputTokens)
			}
			if maxTokens, exists := payload["max_tokens"]; exists {
				t.Fatalf("expected max_tokens to be stripped, got %#v", maxTokens)
			}
			if model, _ := payload["model"].(string); model != "gpt-5.4" {
				t.Fatalf("expected model to be preserved, got %q", model)
			}
			if metadata, ok := payload["metadata"].(map[string]interface{}); !ok || metadata["source"] != "test" {
				t.Fatalf("expected metadata to be preserved, got %#v", payload["metadata"])
			}
		})
	}
}

func TestBuildCodexOAuthRequest_StripsEncryptedReasoningState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(nil))

	body := []byte(`{
		"model":"gpt-5.4",
		"stream":true,
		"include":["reasoning.encrypted_content","file_search_call.results"],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"QVhO...fQ=="},
			{"type":"reasoning","id":"rs_2","summary":[{"type":"summary_text","text":"kept summary"}]},
			{"type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{}"}
		]
	}`)

	req, _, err := buildCodexOAuthRequest(
		c,
		nil,
		&config.UpstreamConfig{ServiceType: "openai-oauth"},
		body,
		types.ResponsesRequest{Model: "gpt-5.4", Stream: true},
		"access-token",
		"account-1",
		false,
		false,
	)
	if err != nil {
		t.Fatalf("buildCodexOAuthRequest returned error: %v", err)
	}

	payloadBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read built request body: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("failed to parse built request body: %v", err)
	}

	include, ok := payload["include"].([]interface{})
	if !ok {
		t.Fatalf("expected include to preserve non-encrypted entries, got %#v", payload["include"])
	}
	if len(include) != 1 || include[0] != "file_search_call.results" {
		t.Fatalf("include = %#v, want file_search_call.results only", include)
	}

	input := payload["input"].([]interface{})
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3 after dropping encrypted reasoning item: %#v", len(input), input)
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, exists := itemMap["encrypted_content"]; exists {
			t.Fatalf("encrypted_content was not stripped: %#v", itemMap)
		}
	}
	if input[1].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("expected non-encrypted reasoning summary to remain, got %#v", input[1])
	}
}

func TestSanitizeCodexOAuthEncryptedReasoningState_StripsNestedEncryptedContent(t *testing.T) {
	reqMap := map[string]interface{}{
		"type": "response.create",
		"response": map[string]interface{}{
			"include": []interface{}{"reasoning.encrypted_content", "file_search_call.results"},
			"input": []interface{}{
				map[string]interface{}{
					"type":              "reasoning",
					"encrypted_content": "QVhO...fQ==",
				},
				map[string]interface{}{
					"type": "message",
					"content": []interface{}{
						map[string]interface{}{
							"type":              "input_text",
							"text":              "hello",
							"encrypted_content": "nested-secret",
						},
					},
				},
			},
		},
	}

	sanitizeCodexOAuthEncryptedReasoningState(reqMap)

	response := reqMap["response"].(map[string]interface{})
	include := response["include"].([]interface{})
	if len(include) != 1 || include[0] != "file_search_call.results" {
		t.Fatalf("include = %#v, want file_search_call.results only", include)
	}
	input := response["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input length = %d, want encrypted reasoning item removed", len(input))
	}
	message := input[0].(map[string]interface{})
	content := message["content"].([]interface{})
	contentPart := content[0].(map[string]interface{})
	if _, exists := contentPart["encrypted_content"]; exists {
		t.Fatalf("nested encrypted_content was not stripped: %#v", contentPart)
	}
}

func TestBuildCodexOAuthRequest_AppliesCodexPriorityOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		body           string
		overrideMode   string
		wantTier       string
		wantOverridden bool
		wantSource     string
	}{
		{
			name:           "missing tier forced to priority",
			body:           `{"model":"gpt-5-codex","stream":true,"store":true}`,
			overrideMode:   config.CodexServiceTierOverrideForcePriority,
			wantTier:       "priority",
			wantOverridden: true,
		},
		{
			name:           "default tier forced to priority",
			body:           `{"model":"gpt-5-codex","stream":true,"store":true,"service_tier":"default"}`,
			overrideMode:   config.CodexServiceTierOverrideForcePriority,
			wantTier:       "priority",
			wantOverridden: true,
		},
		{
			name:           "priority tier preserved without override flag",
			body:           `{"model":"gpt-5-codex","stream":true,"store":true,"service_tier":"priority"}`,
			overrideMode:   config.CodexServiceTierOverrideForcePriority,
			wantTier:       "priority",
			wantOverridden: false,
		},
		{
			name:           "override off preserves default tier",
			body:           `{"model":"gpt-5-codex","stream":true,"store":true,"service_tier":"default"}`,
			overrideMode:   config.CodexServiceTierOverrideOff,
			wantTier:       "default",
			wantOverridden: false,
		},
		{
			name:           "priority tier removed when forced default and preserves unrelated fields",
			body:           `{"model":"gpt-5-codex","stream":true,"store":true,"service_tier":"priority","metadata":{"source":"task3"}}`,
			overrideMode:   config.CodexServiceTierOverrideForceDefault,
			wantTier:       "",
			wantOverridden: true,
			wantSource:     "task3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(nil))

			responsesReq := types.ResponsesRequest{
				Model:  "gpt-5-codex",
				Stream: true,
			}

			req, overridden, err := buildCodexOAuthRequest(
				c,
				nil,
				&config.UpstreamConfig{
					ServiceType:              "openai-oauth",
					CodexServiceTierOverride: tc.overrideMode,
				},
				[]byte(tc.body),
				responsesReq,
				"access-token",
				"account-1",
				false,
				false,
			)
			if err != nil {
				t.Fatalf("buildCodexOAuthRequest returned error: %v", err)
			}
			if overridden != tc.wantOverridden {
				t.Fatalf("overridden = %v, want %v", overridden, tc.wantOverridden)
			}

			payloadBytes, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("failed to read built request body: %v", err)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				t.Fatalf("failed to parse built request body: %v", err)
			}

			rawTier, hasTier := payload["service_tier"]
			if tc.wantTier == "" {
				if hasTier {
					t.Fatalf("expected service_tier to be absent, got %#v", rawTier)
				}
			} else {
				gotTier, _ := rawTier.(string)
				if gotTier != tc.wantTier {
					t.Fatalf("service_tier = %q, want %q", gotTier, tc.wantTier)
				}
			}
			if tc.wantSource != "" {
				metadata, ok := payload["metadata"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected metadata to be preserved, got %#v", payload["metadata"])
				}
				if gotSource, _ := metadata["source"].(string); gotSource != tc.wantSource {
					t.Fatalf("metadata.source = %q, want %q", gotSource, tc.wantSource)
				}
			}
		})
	}
}
