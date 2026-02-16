package forwardproxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// handleMITMConnect performs TLS MITM interception for configured domains.
func (s *Server) handleMITMConnect(w http.ResponseWriter, _ *http.Request, targetAddr, hostOnly string) {
	// Hijack client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientRawConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[fwd-proxy] hijack error: %v", err)
		return
	}
	defer clientRawConn.Close()

	// Send 200 Connection Established directly to hijacked connection
	if _, err := clientRawConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("[fwd-proxy] failed to send 200 to client: %v", err)
		return
	}

	// Get TLS cert for the target host
	tlsCert, err := s.certManager.GetCertForHost(hostOnly)
	if err != nil {
		log.Printf("[fwd-proxy] cert generation error for %s: %v", hostOnly, err)
		return
	}

	// TLS handshake with client (we act as server)
	clientTLSConn := tls.Server(clientRawConn, &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
	})
	if err := clientTLSConn.Handshake(); err != nil {
		log.Printf("[fwd-proxy] client TLS handshake error for %s: %v", hostOnly, err)
		return
	}
	defer clientTLSConn.Close()

	// TLS dial to real upstream
	upstreamTLSConn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		targetAddr,
		&tls.Config{
			ServerName: hostOnly,
		},
	)
	if err != nil {
		log.Printf("[fwd-proxy] upstream TLS dial error for %s: %v", targetAddr, err)
		return
	}
	defer upstreamTLSConn.Close()

	// HTTP request loop (keep-alive) — break on any upstream error
	clientReader := bufio.NewReader(clientTLSConn)
	upstreamReader := bufio.NewReader(upstreamTLSConn)
	for {
		req, err := http.ReadRequest(clientReader)
		if err != nil {
			if err != io.EOF {
				log.Printf("[fwd-proxy] read request error: %v", err)
			}
			return
		}

		// Set the host for the upstream request
		req.URL.Scheme = "https"
		req.URL.Host = targetAddr
		req.RequestURI = "" // Must be empty for client requests

		// Remove hop-by-hop headers before forwarding to upstream
		removeHopByHopHeaders(req.Header)

		// Prevent compressed responses so metric parsing works on raw JSON/SSE
		req.Header.Set("Accept-Encoding", "identity")

		if err := s.proxyRequest(clientTLSConn, upstreamTLSConn, upstreamReader, req, hostOnly); err != nil {
			log.Printf("[fwd-proxy] proxy request error, closing tunnel: %v", err)
			return
		}
	}
}

// proxyRequest forwards a single HTTP request and its response, optionally parsing the response.
// Returns an error if the tunnel should be closed (upstream I/O failure).
// upstreamReader must be reused across the keep-alive connection lifecycle.
func (s *Server) proxyRequest(clientConn io.Writer, upstreamConn net.Conn, upstreamReader *bufio.Reader, req *http.Request, hostOnly string) error {
	startTime := time.Now()

	// Read request body eagerly for metadata extraction (matches normal proxy path).
	// metadata.user_id is at the end of the JSON body, after tool schemas and system
	// prompts that can exceed 64KB, so we must capture the full body.
	var reqBody []byte
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return err
		}
		reqBody = bodyBytes
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Create pending log entry before forwarding (makes request visible in UI immediately)
	var pendingLogID string
	if s.requestLogManager != nil && isAnthropicEndpoint(req.URL.Path) {
		pendingLog := createPendingLog(req, startTime, hostOnly, reqBody)
		if err := s.requestLogManager.Add(pendingLog); err != nil {
			log.Printf("[fwd-proxy] failed to create pending log: %v", err)
		} else {
			pendingLogID = pendingLog.ID
		}
	}

	if err := req.Write(upstreamConn); err != nil {
		return err
	}

	// Read response from upstream (reuse persistent reader)
	resp, err := http.ReadResponse(upstreamReader, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	s.mu.RLock()
	proxyEnabled := s.enabled
	s.mu.RUnlock()

	if isSSE && proxyEnabled {
		s.proxySSEResponse(clientConn, resp, req, hostOnly, startTime, reqBody, pendingLogID)
	} else if proxyEnabled && isAnthropicEndpoint(req.URL.Path) {
		s.proxyJSONResponse(clientConn, resp, req, hostOnly, startTime, reqBody, pendingLogID)
	} else {
		s.forwardResponse(clientConn, resp)
	}
	return nil
}

// proxySSEResponse tees an SSE response through the Anthropic parser while forwarding to the client.
// Uses resp.Write() to preserve correct HTTP transfer framing (chunked encoding, etc.).
func (s *Server) proxySSEResponse(clientConn io.Writer, resp *http.Response, req *http.Request, hostOnly string, startTime time.Time, reqBody []byte, pendingLogID string) {
	// Use StreamSynthesizer (same as normal proxy) for metric extraction
	parser := NewStreamParserWriter()

	// Tee the response body: resp.Write reads and forwards to client,
	// TeeReader copies bytes to parser for metric extraction.
	// Also capture response bytes for debug logging.
	var respCapture bytes.Buffer
	const maxCapture = 10 * 1024 * 1024
	var teeWriter io.Writer = parser
	if s.isDebugEnabled() {
		teeWriter = io.MultiWriter(parser, &limitedWriter{w: &respCapture, remaining: maxCapture})
	}
	resp.Body = &teeReadCloser{Reader: io.TeeReader(resp.Body, teeWriter), Closer: resp.Body}

	if err := resp.Write(clientConn); err != nil {
		log.Printf("[fwd-proxy] write SSE response error: %v", err)
	}

	endTime := time.Now()
	usage := parser.GetUsage()

	if s.requestLogManager != nil && pendingLogID != "" {
		record := createCompletionRecord(usage, resp.StatusCode, startTime, endTime, hostOnly)
		if err := s.requestLogManager.Update(pendingLogID, record); err != nil {
			log.Printf("[fwd-proxy] failed to update request log: %v", err)
		}
		s.saveDebugLog(pendingLogID, req, reqBody, resp.StatusCode, resp.Header, respCapture.Bytes())
	}
}

// proxyJSONResponse handles non-streaming Anthropic responses.
// Forwards the complete response to the client, captures up to 10MB for metric parsing.
func (s *Server) proxyJSONResponse(clientConn io.Writer, resp *http.Response, req *http.Request, hostOnly string, startTime time.Time, reqBody []byte, pendingLogID string) {
	// Tee the response body: resp.Write reads and forwards to client,
	// TeeReader copies bytes to limitedWriter for metric extraction
	var captureBuf bytes.Buffer
	const maxCapture = 10 * 1024 * 1024
	resp.Body = &teeReadCloser{Reader: io.TeeReader(resp.Body, &limitedWriter{w: &captureBuf, remaining: maxCapture}), Closer: resp.Body}

	if err := resp.Write(clientConn); err != nil {
		log.Printf("[fwd-proxy] write JSON response error: %v", err)
	}

	endTime := time.Now()
	_, usage := parseJSONResponse(captureBuf.Bytes())

	if s.requestLogManager != nil && pendingLogID != "" {
		record := createCompletionRecord(usage, resp.StatusCode, startTime, endTime, hostOnly)
		record.Stream = false
		if err := s.requestLogManager.Update(pendingLogID, record); err != nil {
			log.Printf("[fwd-proxy] failed to update request log: %v", err)
		}
		s.saveDebugLog(pendingLogID, req, reqBody, resp.StatusCode, resp.Header, captureBuf.Bytes())
	}
}

// forwardResponse writes a response as-is to the client without parsing.
// Uses resp.Write() to preserve correct HTTP transfer framing.
func (s *Server) forwardResponse(clientConn io.Writer, resp *http.Response) {
	if err := resp.Write(clientConn); err != nil {
		log.Printf("[fwd-proxy] write response error: %v", err)
	}
}

func isAnthropicEndpoint(path string) bool {
	return strings.HasPrefix(path, "/v1/messages") || strings.HasPrefix(path, "/v1/complete")
}

// teeReadCloser wraps an io.Reader (typically a TeeReader) with the original body's Close method.
// This ensures the original response body is properly closed/drained after reading.
type teeReadCloser struct {
	io.Reader
	io.Closer
}

// limitedWriter writes up to `remaining` bytes to the underlying writer, then silently discards.
type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil // silently discard
	}
	n := int64(len(p))
	if n > lw.remaining {
		n = lw.remaining
	}
	written, err := lw.w.Write(p[:n])
	lw.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return len(p), nil // report full length consumed (discard overflow)
}
