package providers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/gin-gonic/gin"
)

func TestResponsesProviderAddsEncryptedReasoningIncludeForAutoMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{
		"model": "gpt-5",
		"input": "hello",
		"stream": true
	}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	provider := &ResponsesProvider{SessionManager: session.NewSessionManager(24*time.Hour, 100, 100000)}
	upstream := &config.UpstreamConfig{
		BaseURL:                         "https://api.openai.com/v1",
		ServiceType:                     "responses",
		ResponsesEncryptedReasoningMode: config.ResponsesEncryptedReasoningModeAuto,
	}

	providerReq, _, err := provider.ConvertToProviderRequest(ctx, upstream, "sk-test")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(providerReq.Body).Decode(&payload); err != nil {
		t.Fatalf("decode provider body: %v", err)
	}

	include, ok := payload["include"].([]interface{})
	if !ok {
		t.Fatalf("expected include array, got %#v", payload["include"])
	}
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v, want reasoning.encrypted_content only", include)
	}
	if payload["stream"] != true {
		t.Fatalf("stream was changed: %#v", payload["stream"])
	}
}

func TestResponsesProviderPreservesExistingIncludeAndAvoidsDuplicateEncryptedReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{
		"model": "gpt-5",
		"input": "hello",
		"include": ["file_search_call.results", "reasoning.encrypted_content"]
	}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	provider := &ResponsesProvider{SessionManager: session.NewSessionManager(24*time.Hour, 100, 100000)}
	upstream := &config.UpstreamConfig{
		BaseURL:                         "https://api.openai.com/v1",
		ServiceType:                     "responses",
		ResponsesEncryptedReasoningMode: config.ResponsesEncryptedReasoningModeAlways,
	}

	providerReq, _, err := provider.ConvertToProviderRequest(ctx, upstream, "sk-test")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(providerReq.Body).Decode(&payload); err != nil {
		t.Fatalf("decode provider body: %v", err)
	}

	include := payload["include"].([]interface{})
	if len(include) != 2 {
		t.Fatalf("include = %#v, want existing entry plus one encrypted reasoning entry", include)
	}
	if include[0] != "file_search_call.results" || include[1] != "reasoning.encrypted_content" {
		t.Fatalf("include order/content = %#v", include)
	}
}

func TestResponsesProviderDoesNotAddEncryptedReasoningIncludeWhenOff(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{
		"model": "gpt-5",
		"input": "hello",
		"stream": true
	}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	provider := &ResponsesProvider{SessionManager: session.NewSessionManager(24*time.Hour, 100, 100000)}
	upstream := &config.UpstreamConfig{
		BaseURL:                         "https://api.openai.com/v1",
		ServiceType:                     "responses",
		ResponsesEncryptedReasoningMode: config.ResponsesEncryptedReasoningModeOff,
	}

	providerReq, _, err := provider.ConvertToProviderRequest(ctx, upstream, "sk-test")
	if err != nil {
		t.Fatalf("ConvertToProviderRequest returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(providerReq.Body).Decode(&payload); err != nil {
		t.Fatalf("decode provider body: %v", err)
	}
	if _, ok := payload["include"]; ok {
		t.Fatalf("did not expect include when mode is off, got %#v", payload["include"])
	}
}
