package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/providers"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/gin-gonic/gin"
)

type delayedStreamErrorProvider struct {
	delay time.Duration
	err   error
}

var _ providers.Provider = (*delayedStreamErrorProvider)(nil)

type staticStreamProvider struct {
	events []string
	err    error
}

var _ providers.Provider = (*staticStreamProvider)(nil)

func (p *delayedStreamErrorProvider) ConvertToProviderRequest(_ *gin.Context, _ *config.UpstreamConfig, _ string) (*http.Request, []byte, error) {
	return nil, nil, errors.New("not implemented in test provider")
}

func (p *delayedStreamErrorProvider) ConvertToClaudeResponse(_ *types.ProviderResponse) (*types.ClaudeResponse, error) {
	return nil, errors.New("not implemented in test provider")
}

func (p *delayedStreamErrorProvider) HandleStreamResponse(_ io.ReadCloser) (<-chan string, <-chan error, error) {
	eventCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		eventCh <- "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
		time.Sleep(p.delay)
		if p.err != nil {
			errCh <- p.err
		}
	}()

	return eventCh, errCh, nil
}

func (p *staticStreamProvider) ConvertToProviderRequest(_ *gin.Context, _ *config.UpstreamConfig, _ string) (*http.Request, []byte, error) {
	return nil, nil, errors.New("not implemented in test provider")
}

func (p *staticStreamProvider) ConvertToClaudeResponse(_ *types.ProviderResponse) (*types.ClaudeResponse, error) {
	return nil, errors.New("not implemented in test provider")
}

func (p *staticStreamProvider) HandleStreamResponse(_ io.ReadCloser) (<-chan string, <-chan error, error) {
	eventCh := make(chan string, len(p.events))
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		for _, event := range p.events {
			eventCh <- event
		}
		if p.err != nil {
			errCh <- p.err
		}
	}()

	return eventCh, errCh, nil
}

func TestHandleStreamResponse_TreatsClaudeSSEErrorBeforeContentAsUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-120 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "claude", "claude-opus-4-7", true, 26, "100xlabs")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":true}`))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}

	upstream := &config.UpstreamConfig{
		Index:       26,
		Name:        "100xlabs",
		ServiceType: "claude",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	handleStreamResponse(
		c,
		resp,
		&staticStreamProvider{
			events: []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4.7\",\"usage\":{\"input_tokens\":595,\"output_tokens\":0}}}\n\n",
				"data: {\"type\":\"error\",\"error\":{\"type\":\"upstream_error\",\"message\":\"Upstream access forbidden, please contact administrator\"}}\n\n",
			},
		},
		envCfg,
		cfgManager,
		startTime,
		upstream,
		reqLogManager,
		requestLogID,
		"claude-opus-4-7",
		nil,
		26,
		"100xlabs",
		false,
	)

	if strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("expected upstream SSE error frame not to be forwarded, got body: %s", rec.Body.String())
	}

	recent := mustGetRecentLogByID(t, reqLogManager, requestLogID)
	if recent.Status != requestlog.StatusError {
		t.Fatalf("expected status %q, got %q", requestlog.StatusError, recent.Status)
	}
	if !strings.Contains(recent.Error, "Upstream access forbidden") {
		t.Fatalf("expected upstream error message to be recorded, got: %q", recent.Error)
	}
}

func TestHandleStreamResponse_RecordsClaudeSSEErrorAfterContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-120 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "claude", "claude-sonnet-4", true, 1, "messages-1")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":true}`))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}

	upstream := &config.UpstreamConfig{
		Index:       1,
		Name:        "messages-1",
		ServiceType: "claude",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	handleStreamResponse(
		c,
		resp,
		&staticStreamProvider{
			events: []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n",
				"data: {\"type\":\"error\",\"error\":{\"type\":\"upstream_error\",\"message\":\"stream failed after content\"}}\n\n",
			},
		},
		envCfg,
		cfgManager,
		startTime,
		upstream,
		reqLogManager,
		requestLogID,
		"claude-sonnet-4",
		nil,
		1,
		"messages-1",
		false,
	)

	if !strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("expected post-content SSE error frame to be forwarded, got body: %s", rec.Body.String())
	}

	recent := mustGetRecentLogByID(t, reqLogManager, requestLogID)
	if recent.Status != requestlog.StatusError {
		t.Fatalf("expected status %q, got %q", requestlog.StatusError, recent.Status)
	}
	if !strings.Contains(recent.Error, "stream failed after content") {
		t.Fatalf("expected stream error message to be recorded, got: %q", recent.Error)
	}
}

func TestHandleStreamResponse_WaitsForErrorChannelBeforeCompleting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-120 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "claude", "claude-sonnet-4", true, 1, "messages-1")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":true}`))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}

	upstream := &config.UpstreamConfig{
		Index:       1,
		Name:        "messages-1",
		ServiceType: "claude",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	handleStreamResponse(
		c,
		resp,
		&delayedStreamErrorProvider{
			delay: 20 * time.Millisecond,
			err:   errors.New("unexpected EOF"),
		},
		envCfg,
		cfgManager,
		startTime,
		upstream,
		reqLogManager,
		requestLogID,
		"claude-sonnet-4",
		nil,
		1,
		"messages-1",
		false,
	)

	recent := mustGetRecentLogByID(t, reqLogManager, requestLogID)
	if recent.Status != requestlog.StatusError {
		t.Fatalf("expected status %q, got %q", requestlog.StatusError, recent.Status)
	}
	if !strings.Contains(recent.Error, "unexpected EOF") {
		t.Fatalf("expected stream error to be recorded, got: %q", recent.Error)
	}
}

func TestHandleStreamResponse_CompletesResponsesBridgeRequestLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newTestConfigManager(t)
	reqLogManager := newTestRequestLogManager(t)

	startTime := time.Now().Add(-120 * time.Millisecond)
	requestLogID := addPendingLogForTest(t, reqLogManager, startTime, "/v1/messages", "openai-oauth", "gpt-5.3-codex", true, 7, "codex-oauth")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"stream":true}`))

	sse := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hi"}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(sse)),
	}

	upstream := &config.UpstreamConfig{
		Index:       7,
		Name:        "codex-oauth",
		ServiceType: "openai-oauth",
	}
	envCfg := &config.EnvConfig{LogLevel: "error"}

	done := make(chan struct{})
	go func() {
		handleStreamResponse(
			c,
			resp,
			&providers.ResponsesUpstreamProvider{},
			envCfg,
			cfgManager,
			startTime,
			upstream,
			reqLogManager,
			requestLogID,
			"gpt-5.3-codex",
			nil,
			7,
			"codex-oauth",
			true,
		)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleStreamResponse did not return after Responses bridge stream completed")
	}

	recent := mustGetRecentLogByID(t, reqLogManager, requestLogID)
	if recent.Status != requestlog.StatusCompleted {
		t.Fatalf("expected status %q, got %q", requestlog.StatusCompleted, recent.Status)
	}
	if recent.HTTPStatus != http.StatusOK {
		t.Fatalf("expected http status %d, got %d", http.StatusOK, recent.HTTPStatus)
	}
	if recent.OutputTokens != 1 {
		t.Fatalf("expected output tokens 1, got %d", recent.OutputTokens)
	}
	if recent.ServiceTier != "priority" {
		t.Fatalf("expected serviceTier priority, got %q", recent.ServiceTier)
	}
}

func TestSendRequest_ClaudeStreamForcesSSEAcceptHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		serviceType string
		wantAccept  string
	}{
		{
			name:        "claude stream forces sse",
			serviceType: "claude",
			wantAccept:  "text/event-stream",
		},
		{
			name:        "non-claude stream keeps original accept",
			serviceType: "openai",
			wantAccept:  "application/json",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var receivedAccept string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAccept = r.Header.Get("Accept")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"stream":true}`))
			if err != nil {
				t.Fatalf("NewRequest failed: %v", err)
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")

			envCfg := &config.EnvConfig{RequestTimeout: 5000}
			upstream := &config.UpstreamConfig{
				Name:        "test-upstream",
				ServiceType: tc.serviceType,
			}

			resp, err := sendRequest(req, upstream, envCfg, true)
			if err != nil {
				t.Fatalf("sendRequest failed: %v", err)
			}
			_ = resp.Body.Close()

			if receivedAccept != tc.wantAccept {
				t.Fatalf("received Accept = %q, want %q", receivedAccept, tc.wantAccept)
			}
		})
	}
}
