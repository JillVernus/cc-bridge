package forwardproxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

func TestUpdateConfig_NormalizesDomainAliases(t *testing.T) {
	s := &Server{
		configPath:            filepath.Join(t.TempDir(), "forward-proxy.json"),
		interceptDomains:      make(map[string]bool),
		xInitiatorDomainState: make(map[string]time.Time),
	}

	err := s.UpdateConfig(Config{
		Enabled:          true,
		InterceptDomains: []string{" API.OPENAI.COM ", "api.openai.com"},
		DomainAliases: map[string]string{
			" API.OPENAI.COM ":  " OpenAI ",
			"files.example.com": "",
		},
		XInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         false,
			Mode:            XInitiatorOverrideModeFixedWindow,
			DurationSeconds: 300,
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	got := s.GetConfig()
	if len(got.DomainAliases) != 1 {
		t.Fatalf("expected exactly one normalized alias, got %#v", got.DomainAliases)
	}
	if got.DomainAliases["api.openai.com"] != "OpenAI" {
		t.Fatalf("expected normalized alias OpenAI, got %#v", got.DomainAliases)
	}
}

func TestHandleHTTPForward_InterceptedUnknownEndpointCreatesMainLogRow(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2]}]}`)
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
		domainAliases: map[string]string{
			hostOnly: "OpenAI",
		},
	}

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
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
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.Type != "forward-proxy" {
		t.Fatalf("expected generic forward-proxy type, got %q", got.Type)
	}
	if got.ProviderName != "OpenAI" {
		t.Fatalf("expected providerName alias %q, got %q", "OpenAI", got.ProviderName)
	}
	if got.Model != "text-embedding-3-small" {
		t.Fatalf("expected request model preserved, got %q", got.Model)
	}
	if got.Endpoint != "/v1/embeddings" {
		t.Fatalf("expected endpoint /v1/embeddings, got %q", got.Endpoint)
	}
	if got.HTTPStatus != http.StatusOK {
		t.Fatalf("expected HTTP status 200, got %d", got.HTTPStatus)
	}
}

func TestHandleHTTPForward_InterceptedResponsesJSONParsesUsage(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_123","model":"gpt-5","status":"completed","usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19},"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`)
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

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":false,"user":"user-1","prompt_cache_key":"sess-1","input":"hello"}`))
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
	if got.Type != "responses" {
		t.Fatalf("expected responses type, got %q", got.Type)
	}
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.InputTokens != 12 || got.OutputTokens != 7 {
		t.Fatalf("expected usage 12/7, got %d/%d", got.InputTokens, got.OutputTokens)
	}
	if got.ResponseModel != "gpt-5" {
		t.Fatalf("expected responseModel gpt-5, got %q", got.ResponseModel)
	}
	if got.ClientID != "user-1" {
		t.Fatalf("expected clientID user-1, got %q", got.ClientID)
	}
	if got.SessionID != "sess-1" {
		t.Fatalf("expected sessionID sess-1, got %q", got.SessionID)
	}
}

func TestHandleHTTPForward_InterceptedGeminiJSONParsesUsage(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"modelVersion":"gemini-2.5-pro","usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":5,"totalTokenCount":16},"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`)
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

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
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
	if got.Type != "gemini" {
		t.Fatalf("expected gemini type, got %q", got.Type)
	}
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.InputTokens != 11 || got.OutputTokens != 5 {
		t.Fatalf("expected usage 11/5, got %d/%d", got.InputTokens, got.OutputTokens)
	}
	if got.ResponseModel != "gemini-2.5-pro" {
		t.Fatalf("expected responseModel gemini-2.5-pro, got %q", got.ResponseModel)
	}
}

func TestHandleHTTPForward_PersistsXInitiatorMetadata(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Initiator"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_123","model":"gpt-5","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`)
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}
	hostOnly := strings.ToLower(upstreamURL.Hostname())

	current := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	s := &Server{
		requestLogManager: reqLogManager,
		httpClient:        upstream.Client(),
		enabled:           true,
		interceptDomains: map[string]bool{
			hostOnly: true,
		},
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeFixedWindow,
			DurationSeconds: 300,
		},
		xInitiatorDomainState: make(map[string]time.Time),
		now: func() time.Time {
			return current
		},
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Initiator", "user")

		rec := httptest.NewRecorder()
		s.handleHTTPForward(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		current = current.Add(5 * time.Second)
	}

	if len(seen) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(seen))
	}
	if seen[0] != "user" {
		t.Fatalf("expected first upstream header to stay user, got %q", seen[0])
	}
	if seen[1] != "agent" {
		t.Fatalf("expected second upstream header to be overridden to agent, got %q", seen[1])
	}

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 2 {
		t.Fatalf("expected exactly two request logs, got %d", len(recent.Requests))
	}

	first := recent.Requests[1]
	if first.OriginalXInitiator != "user" {
		t.Fatalf("expected first originalXInitiator=user, got %q", first.OriginalXInitiator)
	}
	if first.EffectiveXInitiator != "user" {
		t.Fatalf("expected first effectiveXInitiator=user, got %q", first.EffectiveXInitiator)
	}

	second := recent.Requests[0]
	if second.OriginalXInitiator != "user" {
		t.Fatalf("expected second originalXInitiator=user, got %q", second.OriginalXInitiator)
	}
	if second.EffectiveXInitiator != "agent" {
		t.Fatalf("expected second effectiveXInitiator=agent, got %q", second.EffectiveXInitiator)
	}
}

func TestProxyRequest_InterceptedResponsesEndpointCreatesCompletedLog(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	s := &Server{
		requestLogManager: reqLogManager,
		enabled:           true,
	}

	upstreamConn, upstreamPeer := net.Pipe()
	defer upstreamConn.Close()
	defer upstreamPeer.Close()

	go func() {
		reader := bufio.NewReader(upstreamPeer)
		req, err := http.ReadRequest(reader)
		if err != nil {
			t.Errorf("failed to read proxied request: %v", err)
			return
		}
		if req.URL.Path != "/v1/responses" {
			t.Errorf("expected proxied path /v1/responses, got %q", req.URL.Path)
			return
		}

		body := `{"id":"resp_mitm","model":"gpt-5","status":"completed","usage":{"input_tokens":9,"output_tokens":4,"total_tokens":13}}`
		_, err = io.WriteString(upstreamPeer, fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body))
		if err != nil {
			t.Errorf("failed to write upstream response: %v", err)
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":false}`))
	var clientSink bytes.Buffer

	err := s.proxyRequest(&clientSink, upstreamConn, bufio.NewReader(upstreamConn), req, "api.openai.com")
	if err != nil {
		t.Fatalf("proxyRequest failed: %v", err)
	}

	recent, err := reqLogManager.GetRecent(&requestlog.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("expected exactly one request log, got %d", len(recent.Requests))
	}

	got := recent.Requests[0]
	if got.Type != "responses" {
		t.Fatalf("expected responses type, got %q", got.Type)
	}
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.InputTokens != 9 || got.OutputTokens != 4 {
		t.Fatalf("expected usage 9/4, got %d/%d", got.InputTokens, got.OutputTokens)
	}
	if got.ResponseModel != "gpt-5" {
		t.Fatalf("expected responseModel gpt-5, got %q", got.ResponseModel)
	}
}

func TestHandleHTTPForward_UnknownSSEStreamStillParsesClaudeStyleUsage(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":65,"output_tokens":131,"cache_creation_input_tokens":13688,"cache_read_input_tokens":510}}`+"\n\n")
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

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/backend-api/custom-codex-stream", strings.NewReader(`{"stream":true}`))
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
	if got.Type != "forward-proxy" {
		t.Fatalf("expected forward-proxy type, got %q", got.Type)
	}
	if got.InputTokens != 65 {
		t.Fatalf("expected input tokens 65, got %d", got.InputTokens)
	}
	if got.OutputTokens != 131 {
		t.Fatalf("expected output tokens 131, got %d", got.OutputTokens)
	}
	if got.CacheCreationInputTokens != 13688 {
		t.Fatalf("expected cache creation tokens 13688, got %d", got.CacheCreationInputTokens)
	}
	if got.CacheReadInputTokens != 510 {
		t.Fatalf("expected cache read tokens 510, got %d", got.CacheReadInputTokens)
	}
}

func TestHandleHTTPForward_InterceptedOpenAIChatSSEParsesFinalUsageAndModel(t *testing.T) {
	reqLogManager := newForwardProxyTestRequestLogManager(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl-1","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"content":null}}],"usage":{"completion_tokens":279,"prompt_tokens":15666,"prompt_tokens_details":{"cached_tokens":15466},"total_tokens":15945},"model":"claude-haiku-4.5"}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
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

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/chat/completions", strings.NewReader(`{"model":"claude-haiku-4.5","stream":true}`))
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
	if got.Type != "openai" {
		t.Fatalf("expected openai type, got %q", got.Type)
	}
	if got.Status != requestlog.StatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.InputTokens != 200 {
		t.Fatalf("expected normalized input tokens 200, got %d", got.InputTokens)
	}
	if got.CacheReadInputTokens != 15466 {
		t.Fatalf("expected cache read tokens 15466, got %d", got.CacheReadInputTokens)
	}
	if got.OutputTokens != 279 {
		t.Fatalf("expected output tokens 279, got %d", got.OutputTokens)
	}
	if got.ResponseModel != "claude-haiku-4.5" {
		t.Fatalf("expected responseModel claude-haiku-4.5, got %q", got.ResponseModel)
	}
}
