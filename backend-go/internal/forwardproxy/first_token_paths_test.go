package forwardproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

func TestProxySSEResponse_RecordsFirstTokenForMITMPath(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	startTime := time.Now().Add(-150 * time.Millisecond)
	pending := &requestlog.RequestLog{
		Status:       requestlog.StatusPending,
		InitialTime:  startTime,
		Type:         "claude",
		ProviderName: "api.anthropic.com",
		Model:        "claude-sonnet-4",
		Stream:       true,
		Endpoint:     "/v1/messages",
		ChannelUID:   "subscription:forward-proxy",
		ChannelName:  "Subscription (Forward Proxy)",
	}
	if err := reqLogManager.Add(pending); err != nil {
		t.Fatalf("failed to add pending log: %v", err)
	}

	s := &Server{
		requestLogManager: reqLogManager,
	}

	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{"stream":true}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: message_start",
			`data: {"type":"message_start","message":{"id":"msg_mitm","model":"claude-sonnet-4-20250514","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
			"event: content_block_delta",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			"",
			"event: message_stop",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n"))),
	}

	var clientSink bytes.Buffer
	s.proxySSEResponse(&clientSink, resp, req, "api.anthropic.com", startTime, nil, pending.ID)

	got, err := reqLogManager.GetByID(pending.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected request log")
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime captured in MITM SSE path")
	}
	if got.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
	}
	if got.HTTPStatus != http.StatusOK {
		t.Fatalf("expected HTTP status 200 in stored record, got %d", got.HTTPStatus)
	}
}

func TestHandleHTTPForward_RecordsFirstTokenForSSEPath(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: content_block_delta\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello from forward"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}
	hostOnly := strings.ToLower(upstreamURL.Hostname())

	s := &Server{
		requestLogManager: reqLogManager,
		httpClient:        upstream.Client(),
		enabled:           true,
		interceptDomains: map[string]bool{
			hostOnly: true,
		},
	}

	body := `{"model":"claude-sonnet-4","stream":true,"metadata":{"user_id":"user_abc"}}`
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.handleHTTPForward(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("expected exactly one request log, got %d", len(recent.Requests))
	}

	got := recent.Requests[0]
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime captured in HTTP forward SSE path")
	}
	if got.FirstTokenDurationMs <= 0 {
		t.Fatalf("expected positive firstTokenDurationMs, got %d", got.FirstTokenDurationMs)
	}
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
}

func newForwardProxyTestRequestLogManager(t *testing.T) *requestlog.Manager {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "forward_proxy_request_logs.db")
	manager, err := requestlog.NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create request log manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})
	return manager
}
