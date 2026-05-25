package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/session"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

func TestSanitizeCodexResponsesCompatibilityRequest_DropsProviderSpecificState(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5.5",
		"include": ["reasoning.encrypted_content"],
		"client_metadata": {"x": "y"},
		"text": {"verbosity": "low"},
		"reasoning": {"effort": "xhigh"},
		"tool_choice": "auto",
		"parallel_tool_calls": true,
		"input": [
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"reasoning","encrypted_content":"secret"},
			{"type":"custom_tool_call","call_id":"call_custom","name":"apply_patch","input":"*** Begin Patch"},
			{"type":"custom_tool_call_output","call_id":"call_custom","output":"ok"},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","arguments":{"query":"x"}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","tools":[]},
			{"type":"function_call","name":"exec_command","namespace":"mcp__ace_tool__","arguments":"{\"cmd\":\"ls\"}","call_id":"call_fn"},
			{"type":"function_call_output","call_id":"call_fn","output":"ok"}
		],
		"tools": [
			{"type":"function","name":"exec_command","parameters":{"type":"object"}},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
			{"type":"tool_search","execution":"client"},
			{"type":"web_search"}
		]
	}`)

	sanitized, changed, err := sanitizeCodexResponsesCompatibilityRequest(body)
	if err != nil {
		t.Fatalf("sanitizeCodexResponsesCompatibilityRequest returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected sanitizer to report changed=true")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(sanitized, &payload); err != nil {
		t.Fatalf("sanitized request is not JSON: %v", err)
	}

	for _, key := range []string{"include", "client_metadata", "text", "reasoning"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected %s to be removed, got payload: %#v", key, payload)
		}
	}

	input := payload["input"].([]interface{})
	if len(input) != 7 {
		t.Fatalf("expected provider-specific history to be converted into messages, got %d: %#v", len(input), input)
	}
	for _, item := range input {
		itemMap := item.(map[string]interface{})
		if itemMap["type"] != "message" {
			t.Fatalf("expected sanitized input item to be a message, got %#v", itemMap)
		}
		if _, ok := itemMap["namespace"]; ok {
			t.Fatalf("expected provider-specific namespace to be removed, got %#v", itemMap)
		}
	}

	tools := payload["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected only function tools to remain, got %#v", tools)
	}
	if tools[0].(map[string]interface{})["type"] != "function" {
		t.Fatalf("expected function tool, got %#v", tools[0])
	}
}

func TestTryResponsesChannelWithAllKeys_RetriesDecodeFailureWithCompatibilityRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var callCount int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(reqBody, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}

		if atomic.AddInt32(&callCount, 1) == 1 {
			if _, ok := payload["include"]; !ok {
				t.Fatalf("expected first attempt to preserve original Codex-specific include")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid request: failed to decode responses api request: invalid input: invalid request","type":"invalid_request_error"}}`)
			return
		}

		if _, ok := payload["include"]; ok {
			t.Fatalf("expected compatibility retry to remove include, got %#v", payload)
		}
		input := payload["input"].([]interface{})
		if len(input) != 1 {
			t.Fatalf("expected compatibility retry to remove provider-specific input items, got %#v", input)
		}
		tools := payload["tools"].([]interface{})
		if len(tools) != 1 {
			t.Fatalf("expected compatibility retry to keep only function tools, got %#v", tools)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_compat",
			"object":"response",
			"model":"gpt-5.5",
			"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"compat success"}]}],
			"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}
		}`)
	}))
	defer upstreamServer.Close()

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-compat",
				Name:        "responses-compat",
				BaseURL:     upstreamServer.URL,
				ServiceType: "responses",
				Status:      "active",
				APIKeys:     []string{"sk-compat"},
				Index:       0,
			},
		},
		ResponsesLoadBalance: "failover",
	})

	upstream, err := cfgManager.GetCurrentResponsesUpstream()
	if err != nil {
		t.Fatalf("GetCurrentResponsesUpstream failed: %v", err)
	}

	bodyBytes := []byte(`{
		"model":"gpt-5.5",
		"stream":false,
		"include":["reasoning.encrypted_content"],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"reasoning","encrypted_content":"secret"}
		],
		"tools":[
			{"type":"function","name":"exec_command","parameters":{"type":"object"}},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
		]
	}`)
	var responsesReq types.ResponsesRequest
	if err := json.Unmarshal(bodyBytes, &responsesReq); err != nil {
		t.Fatalf("unmarshal responses request: %v", err)
	}

	reqLogManager := newTestRequestLogManager(t)
	initialStart := time.Now().Add(-300 * time.Millisecond)
	initialLogID := addPendingLogForTest(t, reqLogManager, initialStart, "/v1/responses", "responses", "gpt-5.5", false, 0, "responses-compat")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	envCfg := &config.EnvConfig{
		LogLevel:       "error",
		RequestTimeout: 30000,
	}
	sessionManager := session.NewSessionManager(24*time.Hour, 100, 100000)
	startTime := initialStart

	success, failoverErr, finalLogID := tryResponsesChannelWithAllKeys(
		c,
		envCfg,
		cfgManager,
		sessionManager,
		upstream,
		bodyBytes,
		responsesReq,
		&startTime,
		reqLogManager,
		initialLogID,
		nil,
		nil,
		"user-test",
		"",
		nil,
		0,
		"responses-compat",
		false,
	)

	if !success {
		t.Fatalf("expected success after compatibility retry, got failoverErr=%+v", failoverErr)
	}
	if finalLogID != initialLogID {
		t.Fatalf("expected compatibility retry to keep same request log, got %s", finalLogID)
	}
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Fatalf("expected exactly 2 upstream calls, got %d", got)
	}
	recent := mustGetRecentLogByID(t, reqLogManager, finalLogID)
	if recent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed request log, got %s", recent.Status)
	}
}

func TestHandleSingleChannelResponses_RetriesDecodeFailureWithCompatibilityRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var callCount int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(reqBody, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}

		if atomic.AddInt32(&callCount, 1) == 1 {
			if _, ok := payload["include"]; !ok {
				t.Fatalf("expected first attempt to preserve original Codex-specific include")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid request: failed to decode responses api request: invalid input: invalid request","type":"invalid_request_error"}}`)
			return
		}

		if _, ok := payload["include"]; ok {
			t.Fatalf("expected compatibility retry to remove include, got %#v", payload)
		}
		input := payload["input"].([]interface{})
		if len(input) != 2 {
			t.Fatalf("expected compatibility retry to drop reasoning and convert custom tool history, got %#v", input)
		}
		for _, item := range input {
			itemMap := item.(map[string]interface{})
			if itemMap["type"] != "message" {
				t.Fatalf("expected compatibility input to contain only messages, got %#v", input)
			}
		}
		tools := payload["tools"].([]interface{})
		if len(tools) != 1 {
			t.Fatalf("expected compatibility retry to keep only function tools, got %#v", tools)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_compat_single",
			"object":"response",
			"model":"gpt-5.5",
			"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"compat success"}]}],
			"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}
		}`)
	}))
	defer upstreamServer.Close()

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:          "resp-compat-single",
				Name:        "responses-compat-single",
				BaseURL:     upstreamServer.URL,
				ServiceType: "responses",
				Status:      "active",
				APIKeys:     []string{"sk-compat"},
				Index:       0,
			},
		},
		ResponsesLoadBalance: "failover",
	})

	bodyBytes := []byte(`{
		"model":"gpt-5.5",
		"stream":false,
		"include":["reasoning.encrypted_content"],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"reasoning","encrypted_content":"secret"},
			{"type":"custom_tool_call","call_id":"call_custom","name":"apply_patch","input":"*** Begin Patch"}
		],
		"tools":[
			{"type":"function","name":"exec_command","parameters":{"type":"object"}},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
		]
	}`)
	var responsesReq types.ResponsesRequest
	if err := json.Unmarshal(bodyBytes, &responsesReq); err != nil {
		t.Fatalf("unmarshal responses request: %v", err)
	}

	reqLogManager := newTestRequestLogManager(t)
	initialStart := time.Now().Add(-300 * time.Millisecond)
	initialLogID := addPendingLogForTest(t, reqLogManager, initialStart, "/v1/responses", "responses", "gpt-5.5", false, 0, "responses-compat-single")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	handleSingleChannelResponses(
		c,
		&config.EnvConfig{LogLevel: "error", RequestTimeout: 30000},
		cfgManager,
		newTestScheduler(t, cfgManager),
		session.NewSessionManager(24*time.Hour, 100, 100000),
		bodyBytes,
		responsesReq,
		initialStart,
		reqLogManager,
		initialLogID,
		nil,
		nil,
		nil,
		"user-test",
		"",
		nil,
		nil,
		"",
		false,
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected success after compatibility retry, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Fatalf("expected exactly 2 upstream calls, got %d", got)
	}
	recent := mustGetRecentLogByID(t, reqLogManager, initialLogID)
	if recent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed request log, got %s", recent.Status)
	}
}
