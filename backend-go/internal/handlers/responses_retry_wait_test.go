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

func TestTryResponsesChannelWithAllKeys_RetryWaitPreservesEffectiveServiceTierMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var callCount int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/responses" {
			t.Fatalf("expected upstream path /v1/responses, got %s", got)
		}

		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(reqBody, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if got, _ := payload["service_tier"].(string); got != "priority" {
			t.Fatalf("expected forced service_tier=priority, got %#v", payload["service_tier"])
		}

		if atomic.AddInt32(&callCount, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"retry later"}}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_retry",
			"object":"response",
			"model":"gpt-5",
			"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"retry success"}]}],
			"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}
		}`)
	}))
	defer upstreamServer.Close()

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		Failover: config.FailoverConfig{
			Rules: []config.FailoverRule{
				{
					ErrorCodes: "429",
					ActionChain: []config.ActionStep{
						{Action: config.ActionRetry, WaitSeconds: 0, MaxAttempts: 1},
					},
				},
			},
		},
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:                       "resp-retry",
				Name:                     "responses-retry",
				BaseURL:                  upstreamServer.URL,
				ServiceType:              "responses",
				Status:                   "active",
				APIKeys:                  []string{"sk-retry"},
				CodexServiceTierOverride: "force_priority",
				Index:                    0,
			},
		},
		ResponsesLoadBalance: "failover",
	})

	upstream, err := cfgManager.GetCurrentResponsesUpstream()
	if err != nil {
		t.Fatalf("GetCurrentResponsesUpstream failed: %v", err)
	}

	reqLogManager := newTestRequestLogManager(t)
	initialStart := time.Now().Add(-300 * time.Millisecond)
	initialLogID := addPendingLogForTest(t, reqLogManager, initialStart, "/v1/responses", "responses", "gpt-5", false, 0, "responses-retry")

	bodyBytes := []byte(`{"model":"gpt-5","input":"hello"}`)
	var responsesReq types.ResponsesRequest
	if err := json.Unmarshal(bodyBytes, &responsesReq); err != nil {
		t.Fatalf("unmarshal responses request: %v", err)
	}

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
	failoverTracker := config.NewFailoverTracker()

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
		failoverTracker,
		"user-test",
		"",
		nil,
		0,
		"responses-retry",
		false,
	)

	if !success {
		t.Fatalf("expected success after retry_wait, got failoverErr=%+v", failoverErr)
	}
	if finalLogID == initialLogID {
		t.Fatalf("expected retry_wait flow to create a new pending log id")
	}

	initialRecent := mustGetRecentLogByID(t, reqLogManager, initialLogID)
	finalRecent := mustGetRecentLogByID(t, reqLogManager, finalLogID)

	if initialRecent.Status != requestlog.StatusRetryWait {
		t.Fatalf("expected initial log status retry_wait, got %s", initialRecent.Status)
	}
	if initialRecent.ServiceTier != "priority" {
		t.Fatalf("expected initial retry_wait log serviceTier=priority, got %q", initialRecent.ServiceTier)
	}
	if !initialRecent.ServiceTierOverridden {
		t.Fatalf("expected initial retry_wait log serviceTierOverridden=true")
	}

	if finalRecent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected final log status completed, got %s", finalRecent.Status)
	}
	if finalRecent.ServiceTier != "priority" {
		t.Fatalf("expected final log serviceTier=priority, got %q", finalRecent.ServiceTier)
	}
	if !finalRecent.ServiceTierOverridden {
		t.Fatalf("expected final log serviceTierOverridden=true")
	}
}

func TestTryResponsesChannelWithAllKeys_RetryWaitPreservesDowngradedServiceTierMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var callCount int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/responses" {
			t.Fatalf("expected upstream path /v1/responses, got %s", got)
		}

		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(reqBody, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if got, _ := payload["service_tier"].(string); got != "default" {
			t.Fatalf("expected downgraded service_tier=default, got %#v", payload["service_tier"])
		}

		if atomic.AddInt32(&callCount, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"retry later"}}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_retry_default",
			"object":"response",
			"model":"gpt-5",
			"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"retry success"}]}],
			"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}
		}`)
	}))
	defer upstreamServer.Close()

	cfgManager := newTestConfigManagerWithConfig(t, config.Config{
		Failover: config.FailoverConfig{
			Rules: []config.FailoverRule{
				{
					ErrorCodes: "429",
					ActionChain: []config.ActionStep{
						{Action: config.ActionRetry, WaitSeconds: 0, MaxAttempts: 1},
					},
				},
			},
		},
		ResponsesUpstream: []config.UpstreamConfig{
			{
				ID:                       "resp-retry-default",
				Name:                     "responses-retry-default",
				BaseURL:                  upstreamServer.URL,
				ServiceType:              "responses",
				Status:                   "active",
				APIKeys:                  []string{"sk-retry"},
				CodexServiceTierOverride: config.CodexServiceTierOverrideForceDefault,
				Index:                    0,
			},
		},
		ResponsesLoadBalance: "failover",
	})

	upstream, err := cfgManager.GetCurrentResponsesUpstream()
	if err != nil {
		t.Fatalf("GetCurrentResponsesUpstream failed: %v", err)
	}

	reqLogManager := newTestRequestLogManager(t)
	initialStart := time.Now().Add(-300 * time.Millisecond)
	initialLogID := addPendingLogForTest(t, reqLogManager, initialStart, "/v1/responses", "responses", "gpt-5", false, 0, "responses-retry-default")

	bodyBytes := []byte(`{"model":"gpt-5","service_tier":"priority","input":"hello"}`)
	var responsesReq types.ResponsesRequest
	if err := json.Unmarshal(bodyBytes, &responsesReq); err != nil {
		t.Fatalf("unmarshal responses request: %v", err)
	}

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
	failoverTracker := config.NewFailoverTracker()

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
		failoverTracker,
		"user-test",
		"",
		nil,
		0,
		"responses-retry-default",
		false,
	)

	if !success {
		t.Fatalf("expected success after retry_wait, got failoverErr=%+v", failoverErr)
	}
	if finalLogID == initialLogID {
		t.Fatalf("expected retry_wait flow to create a new pending log id")
	}

	initialRecent := mustGetRecentLogByID(t, reqLogManager, initialLogID)
	finalRecent := mustGetRecentLogByID(t, reqLogManager, finalLogID)

	if initialRecent.Status != requestlog.StatusRetryWait {
		t.Fatalf("expected initial log status retry_wait, got %s", initialRecent.Status)
	}
	if initialRecent.ServiceTier != "default" {
		t.Fatalf("expected initial retry_wait log serviceTier=default, got %q", initialRecent.ServiceTier)
	}
	if !initialRecent.ServiceTierOverridden {
		t.Fatalf("expected initial retry_wait log serviceTierOverridden=true")
	}

	if finalRecent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected final log status completed, got %s", finalRecent.Status)
	}
	if finalRecent.ServiceTier != "default" {
		t.Fatalf("expected final log serviceTier=default, got %q", finalRecent.ServiceTier)
	}
	if !finalRecent.ServiceTierOverridden {
		t.Fatalf("expected final log serviceTierOverridden=true")
	}
}
