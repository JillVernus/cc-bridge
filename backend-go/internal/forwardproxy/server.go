package forwardproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
	"github.com/JillVernus/cc-bridge/internal/utils"
)

// Config holds the forward proxy runtime configuration, persisted as JSON.
type Config struct {
	Enabled            bool                     `json:"enabled"`
	InterceptDomains   []string                 `json:"interceptDomains"`
	DomainAliases      map[string]string        `json:"domainAliases"`
	XInitiatorOverride XInitiatorOverrideConfig `json:"xInitiatorOverride"`
}

// ServerConfig is passed to NewServer at startup.
type ServerConfig struct {
	Port              int
	BindAddress       string // e.g. "0.0.0.0" or "127.0.0.1"
	CertDir           string
	ConfigDir         string // directory for forward-proxy.json (e.g. ".config")
	InterceptDomains  []string
	Enabled           bool
	RequestLogManager *requestlog.Manager
	ConfigManager     ConfigProvider // for debug log config
}

// ConfigProvider is the interface needed for checking debug log config.
type ConfigProvider interface {
	IsDebugLogEnabled() bool
	GetDebugLogMaxBodySize() int
}

// Server is the HTTPS forward proxy.
type Server struct {
	certManager       *CertManager
	requestLogManager *requestlog.Manager
	configProvider    ConfigProvider
	discoveryStore    *DiscoveryStore
	httpClient        *http.Client // dedicated client for upstream requests (no proxy env vars)
	port              int
	bindAddress       string
	configPath        string

	mu               sync.RWMutex
	interceptDomains map[string]bool
	domainAliases    map[string]string
	enabled          bool
	running          bool

	xInitiatorOverride         XInitiatorOverrideConfig
	xInitiatorDomainState      map[string]time.Time
	xInitiatorQuotaDomainState map[string]xInitiatorQuotaState
	xInitiatorCostDomainState  map[string]xInitiatorCostState
	nextXInitiatorCostWindowID uint64
	now                        func() time.Time
}

type ConfigSnapshot struct {
	Config  Config
	Runtime XInitiatorOverrideRuntimeStatus
	Running bool
	Port    int
}

// NewServer creates a new forward proxy server.
func NewServer(cfg ServerConfig) (*Server, error) {
	certMgr, err := NewCertManager(cfg.CertDir)
	if err != nil {
		return nil, fmt.Errorf("init cert manager: %w", err)
	}

	configDir := cfg.ConfigDir
	if configDir == "" {
		configDir = ".config"
	}
	configPath := filepath.Join(configDir, "forward-proxy.json")
	discoveryPath := filepath.Join(configDir, "forward-proxy-discovery.json")

	bindAddr := cfg.BindAddress
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	s := &Server{
		certManager:       certMgr,
		requestLogManager: cfg.RequestLogManager,
		configProvider:    cfg.ConfigManager,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				Proxy:                 nil, // never use proxy env vars (we ARE the proxy)
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				ForceAttemptHTTP2:     true,
			},
		},
		port:                       cfg.Port,
		bindAddress:                bindAddr,
		configPath:                 configPath,
		interceptDomains:           make(map[string]bool),
		domainAliases:              make(map[string]string),
		enabled:                    cfg.Enabled,
		xInitiatorDomainState:      make(map[string]time.Time),
		xInitiatorQuotaDomainState: make(map[string]xInitiatorQuotaState),
		xInitiatorCostDomainState:  make(map[string]xInitiatorCostState),
		now:                        time.Now,
	}

	discoveryStore, err := NewDiscoveryStore(discoveryPath)
	if err != nil {
		return nil, fmt.Errorf("init discovery store: %w", err)
	}
	s.discoveryStore = discoveryStore

	// Try to load persisted config, fall back to env defaults only when absent.
	if err := s.loadConfig(); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load persisted config: %w", err)
		}

		// No persisted config yet — use startup defaults
		for _, d := range cfg.InterceptDomains {
			s.interceptDomains[strings.ToLower(strings.TrimSpace(d))] = true
		}
		s.enabled = cfg.Enabled
		// Persist the initial config
		if err := s.saveConfig(); err != nil {
			log.Printf("[fwd-proxy] failed to persist initial config: %v", err)
		}
	}

	return s, nil
}

// ListenAndServe starts the forward proxy on the configured port.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.bindAddress, s.port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()
	err := srv.ListenAndServe()
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	return err
}

// ServeHTTP handles incoming proxy requests.
// Supports both CONNECT tunneling (standard HTTPS proxy) and
// plain HTTP forwarding (for clients that send absolute-URL requests to the proxy).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
		return
	}
	// Plain HTTP forward proxy — client sends full URL (e.g. POST https://host/path)
	if r.URL.IsAbs() {
		s.handleHTTPForward(w, r)
		return
	}
	http.Error(w, "Not a proxy request", http.StatusBadRequest)
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	targetHost := r.Host
	// Ensure host:port format
	if !strings.Contains(targetHost, ":") {
		targetHost = targetHost + ":443"
	}

	hostOnly := targetHost
	if h, _, err := net.SplitHostPort(targetHost); err == nil {
		hostOnly = h
	}

	s.mu.RLock()
	intercept := s.enabled && s.interceptDomains[strings.ToLower(hostOnly)]
	enabled := s.enabled
	s.mu.RUnlock()

	if intercept {
		log.Printf("[fwd-proxy] MITM intercept for %s", hostOnly)
		s.recordDiscovery(DiscoveryEvent{
			Host:        hostOnly,
			Port:        extractPort(targetHost, "443"),
			Transport:   DiscoveryTransportConnect,
			Intercepted: true,
			Method:      http.MethodConnect,
		})
		s.handleMITMConnect(w, r, targetHost, hostOnly)
	} else {
		log.Printf("[fwd-proxy] blind tunnel for %s (enabled=%v, domainMatch=%v)", hostOnly, enabled, s.interceptDomains[strings.ToLower(hostOnly)])
		s.recordDiscovery(DiscoveryEvent{
			Host:        hostOnly,
			Port:        extractPort(targetHost, "443"),
			Transport:   DiscoveryTransportConnect,
			Intercepted: false,
			Method:      http.MethodConnect,
		})
		s.handleBlindTunnel(w, r, targetHost)
	}
}

func (s *Server) handleBlindTunnel(w http.ResponseWriter, _ *http.Request, targetAddr string) {
	upstreamConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to connect to %s: %v", targetAddr, err), http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	// Hijack client connection first to avoid buffer flushing issues
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[fwd-proxy] hijack error: %v", err)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established directly to hijacked connection
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("[fwd-proxy] failed to send 200 to client (blind tunnel): %v", err)
		return
	}

	// Bidirectional copy — wait for both directions to finish
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(upstreamConn, clientConn)
		// Signal upstream we're done writing
		if tc, ok := upstreamConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		io.Copy(clientConn, upstreamConn)
		// Signal client we're done writing
		if tc, ok := clientConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// handleHTTPForward handles plain HTTP proxy requests where the client sends an absolute URL.
// The client sends "POST https://host/path HTTP/1.1" directly to the proxy.
// We forward the request over HTTPS to the upstream and stream the response back.
func (s *Server) handleHTTPForward(w http.ResponseWriter, r *http.Request) {
	hostOnly := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = "443"
	}

	s.mu.RLock()
	intercept := s.enabled && s.interceptDomains[strings.ToLower(hostOnly)]
	s.mu.RUnlock()

	log.Printf("[fwd-proxy] HTTP forward (absolute URL) for %s (intercept=%v)", hostOnly, intercept)
	s.recordDiscovery(DiscoveryEvent{
		Host:           hostOnly,
		Port:           port,
		Transport:      DiscoveryTransportHTTPForward,
		Intercepted:    intercept,
		Method:         r.Method,
		Path:           r.URL.Path,
		RequestHeaders: r.Header.Clone(),
		SeenAt:         time.Now(),
	})

	startTime := time.Now()

	// Read request body eagerly for metadata extraction (same as normal proxy path).
	// This ensures the body is fully captured before forwarding.
	var reqBody []byte
	var bodyReader io.Reader
	if r.Body != nil && intercept {
		bodyBytes, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			log.Printf("[fwd-proxy] read request body error: %v", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		reqBody = bodyBytes
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = r.Body
	}

	// Prevent compressed responses so metric parsing works on raw data
	if intercept {
		r.Header.Set("Accept-Encoding", "identity")
	}

	// Remove hop-by-hop headers from incoming request (RFC 2616 §13.5.1).
	// Done on r.Header directly so both the forwarded clone and debug logs are clean.
	removeHopByHopHeaders(r.Header)

	originalXInitiator := strings.TrimSpace(r.Header.Get("X-Initiator"))
	var windowedCostWindow xInitiatorCostWindowRef
	if intercept {
		_, windowedCostWindow = s.applyXInitiatorOverrideWithWindow(hostOnly, r.Header)
	}
	effectiveXInitiator := strings.TrimSpace(r.Header.Get("X-Initiator"))

	// Create pending log entry before forwarding (makes request visible in UI immediately)
	var pendingLogID string
	if s.requestLogManager != nil && intercept {
		providerDisplayName := s.resolveInterceptedProviderName(hostOnly)
		pendingLog := createInterceptedPendingLog(r, startTime, providerDisplayName, reqBody, originalXInitiator, effectiveXInitiator)
		if err := s.requestLogManager.Add(pendingLog); err != nil {
			log.Printf("[fwd-proxy] failed to create pending log: %v", err)
		} else {
			pendingLogID = pendingLog.ID
		}
	}

	// Build upstream request
	upstreamURL := r.URL.String()
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bodyReader)
	if err != nil {
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	upstreamReq.Header = r.Header.Clone()
	upstreamReq.ContentLength = r.ContentLength

	// Forward to upstream (use dedicated client — no proxy env vars, HTTP/2 enabled)
	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, "Upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers (excluding hop-by-hop)
	for key, values := range resp.Header {
		if isHopByHopHeader(key) {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	if !intercept {
		// Not intercepted — forward as-is
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// Intercepted — tee the response for metric extraction
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	// Capture response body for debug logging
	var respCapture bytes.Buffer
	const maxCapture = 10 * 1024 * 1024

	if isSSE {
		parser := newInterceptedStreamParser(r.URL.Path)

		w.WriteHeader(resp.StatusCode)

		// Tee to parser for metrics, and capture for debug
		var respWriter io.Writer
		if parser != nil && s.isDebugEnabled() {
			respWriter = io.MultiWriter(parser, &limitedWriter{w: &respCapture, remaining: maxCapture})
		} else if parser != nil {
			respWriter = parser
		} else if s.isDebugEnabled() {
			respWriter = &limitedWriter{w: &respCapture, remaining: maxCapture}
		}
		tee := io.Reader(resp.Body)
		if respWriter != nil {
			tee = io.TeeReader(resp.Body, respWriter)
		}

		// Flush SSE data as it arrives if the writer supports it
		if flusher, ok := w.(http.Flusher); ok {
			buf := make([]byte, 4096)
			for {
				n, readErr := tee.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					flusher.Flush()
				}
				if readErr != nil {
					break
				}
			}
		} else {
			io.Copy(w, tee)
		}

		endTime := time.Now()
		var usage *utils.StreamUsage
		var firstTokenTime *time.Time
		if parser != nil {
			usage = parser.GetUsage()
			firstTokenTime = parser.GetFirstTokenTime()
		}

		record := createInterceptedCompletionRecord(r.URL.Path, usage, resp.StatusCode, startTime, endTime, s.resolveInterceptedProviderName(hostOnly), firstTokenTime)
		s.finalizeInterceptedCompletionRecord(pendingLogID, hostOnly, windowedCostWindow, record)
		if s.requestLogManager != nil && pendingLogID != "" {
			s.saveDebugLog(pendingLogID, r, reqBody, resp.StatusCode, resp.Header, respCapture.Bytes())
		}
	} else {
		tee := io.TeeReader(resp.Body, &limitedWriter{w: &respCapture, remaining: maxCapture})

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, tee)

		endTime := time.Now()
		usage := parseInterceptedJSONResponse(r.URL.Path, respCapture.Bytes())

		record := createInterceptedCompletionRecord(r.URL.Path, usage, resp.StatusCode, startTime, endTime, s.resolveInterceptedProviderName(hostOnly), nil)
		record.Stream = false
		s.finalizeInterceptedCompletionRecord(pendingLogID, hostOnly, windowedCostWindow, record)
		if s.requestLogManager != nil && pendingLogID != "" {
			s.saveDebugLog(pendingLogID, r, reqBody, resp.StatusCode, resp.Header, respCapture.Bytes())
		}
	}
}

// GetConfig returns the current forward proxy configuration.
func (s *Server) GetConfig() Config {
	if s == nil {
		return Config{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getConfigLocked()
}

func (s *Server) GetConfigSnapshot() ConfigSnapshot {
	if s == nil {
		return ConfigSnapshot{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return ConfigSnapshot{
		Config:  s.getConfigLocked(),
		Runtime: s.getXInitiatorOverrideRuntimeStatusLocked(),
		Running: s.running,
		Port:    s.port,
	}
}

func (s *Server) getConfigLocked() Config {
	domains := make([]string, 0, len(s.interceptDomains))
	for d := range s.interceptDomains {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	overrideCfg := normalizeXInitiatorOverrideConfig(s.xInitiatorOverride)
	return Config{
		Enabled:            s.enabled,
		InterceptDomains:   domains,
		DomainAliases:      cloneDomainAliases(s.domainAliases),
		XInitiatorOverride: overrideCfg,
	}
}

func (s *Server) GetDiscoveryEntries() []DiscoveryEntry {
	if s == nil || s.discoveryStore == nil {
		return nil
	}
	return s.discoveryStore.List()
}

func (s *Server) ClearDiscoveryEntries() error {
	if s == nil || s.discoveryStore == nil {
		return nil
	}
	return s.discoveryStore.Clear()
}

// UpdateConfig updates the forward proxy configuration and persists it.
// Persists first from the new config; only applies in-memory changes on successful save.
func (s *Server) UpdateConfig(cfg Config) error {
	// Build the new domain set
	newDomains := make(map[string]bool)
	cleanedDomains := make([]string, 0, len(cfg.InterceptDomains))
	for _, d := range cfg.InterceptDomains {
		trimmed := strings.ToLower(strings.TrimSpace(d))
		if trimmed != "" {
			newDomains[trimmed] = true
			cleanedDomains = append(cleanedDomains, trimmed)
		}
	}
	sort.Strings(cleanedDomains)
	normalizedAliases := normalizeDomainAliases(cfg.DomainAliases)

	// Persist the new config to disk first (from immutable snapshot, no lock needed)
	persistCfg := Config{
		Enabled:            cfg.Enabled,
		InterceptDomains:   cleanedDomains,
		DomainAliases:      normalizedAliases,
		XInitiatorOverride: cfg.XInitiatorOverride,
	}
	if err := validateXInitiatorOverrideConfig(persistCfg.XInitiatorOverride); err != nil {
		return err
	}
	persistCfg.XInitiatorOverride = normalizeXInitiatorOverrideConfig(persistCfg.XInitiatorOverride)
	if err := s.persistConfig(persistCfg); err != nil {
		return err
	}

	// Persist succeeded — now swap in-memory state
	s.mu.Lock()
	s.enabled = cfg.Enabled
	s.interceptDomains = newDomains
	s.domainAliases = cloneDomainAliases(normalizedAliases)
	s.xInitiatorOverride = persistCfg.XInitiatorOverride
	s.xInitiatorDomainState = make(map[string]time.Time)
	s.xInitiatorQuotaDomainState = make(map[string]xInitiatorQuotaState)
	s.xInitiatorCostDomainState = make(map[string]xInitiatorCostState)
	s.mu.Unlock()
	return nil
}

func (s *Server) recordDiscovery(event DiscoveryEvent) {
	if s == nil || s.discoveryStore == nil {
		return
	}
	s.discoveryStore.Record(event)
}

func extractPort(hostport string, fallback string) string {
	if _, port, err := net.SplitHostPort(hostport); err == nil && strings.TrimSpace(port) != "" {
		return port
	}
	return fallback
}

func normalizeForwardProxyHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func normalizeDomainAliases(aliases map[string]string) map[string]string {
	normalized := make(map[string]string)
	for host, alias := range aliases {
		normalizedHost := normalizeForwardProxyHost(host)
		normalizedAlias := strings.TrimSpace(alias)
		if normalizedHost == "" || normalizedAlias == "" {
			continue
		}
		normalized[normalizedHost] = normalizedAlias
	}
	return normalized
}

func cloneDomainAliases(aliases map[string]string) map[string]string {
	if len(aliases) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(aliases))
	for host, alias := range aliases {
		cloned[host] = alias
	}
	return cloned
}

func (s *Server) resolveInterceptedProviderName(hostOnly string) string {
	s.mu.RLock()
	resolved := resolveInterceptedProviderNameFromAliases(s.domainAliases, hostOnly)
	s.mu.RUnlock()
	return resolved
}

func resolveInterceptedProviderNameFromAliases(domainAliases map[string]string, hostOnly string) string {
	normalizedHost := normalizeForwardProxyHost(hostOnly)
	if normalizedHost == "" {
		return ""
	}
	alias := strings.TrimSpace(domainAliases[normalizedHost])
	if alias != "" {
		return alias
	}
	return normalizedHost
}

// GetCertManager returns the certificate manager for CA cert download.
func (s *Server) GetCertManager() *CertManager {
	return s.certManager
}

// isDebugEnabled checks if debug logging is enabled via the config provider.
func (s *Server) isDebugEnabled() bool {
	if s.configProvider == nil {
		return false
	}
	return s.configProvider.IsDebugLogEnabled()
}

// saveDebugLog saves request/response data for debug inspection.
func (s *Server) saveDebugLog(requestID string, req *http.Request, reqBody []byte, respStatus int, respHeaders http.Header, respBody []byte) {
	if s.requestLogManager == nil || !s.isDebugEnabled() {
		return
	}

	maxBodySize := 1024 * 1024 // 1MB default
	if s.configProvider != nil {
		if size := s.configProvider.GetDebugLogMaxBodySize(); size > 0 {
			maxBodySize = size
		}
	}

	entry := requestlog.CreateDebugLogEntry(requestID, req, reqBody, respStatus, respHeaders, respBody, maxBodySize)
	if err := s.requestLogManager.AddDebugLog(entry); err != nil {
		log.Printf("[fwd-proxy] failed to save debug log: %v", err)
	}
}

// IsEnabled returns whether the proxy intercept is currently enabled.
func (s *Server) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// IsRunning returns whether the proxy listener is actually running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetPort returns the configured proxy port.
func (s *Server) GetPort() int {
	return s.port
}

func (s *Server) loadConfig() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}
	var raw struct {
		XInitiatorOverride json.RawMessage `json:"xInitiatorOverride"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	s.enabled = cfg.Enabled
	s.interceptDomains = make(map[string]bool)
	for _, d := range cfg.InterceptDomains {
		trimmed := normalizeForwardProxyHost(d)
		if trimmed != "" {
			s.interceptDomains[trimmed] = true
		}
	}
	s.domainAliases = normalizeDomainAliases(cfg.DomainAliases)
	if shouldDefaultLegacyQuotaOverrideTimes(cfg.XInitiatorOverride, raw.XInitiatorOverride) {
		cfg.XInitiatorOverride.OverrideTimes = 1
	}
	if shouldDefaultLegacyCostTotalCost(cfg.XInitiatorOverride, raw.XInitiatorOverride) {
		cfg.XInitiatorOverride.TotalCost = 1
	}
	if err := validateXInitiatorOverrideConfig(cfg.XInitiatorOverride); err != nil {
		return err
	}
	s.xInitiatorOverride = cfg.XInitiatorOverride
	s.xInitiatorDomainState = make(map[string]time.Time)
	s.xInitiatorQuotaDomainState = make(map[string]xInitiatorQuotaState)
	s.xInitiatorCostDomainState = make(map[string]xInitiatorCostState)
	return nil
}

func shouldDefaultLegacyQuotaOverrideTimes(cfg XInitiatorOverrideConfig, raw json.RawMessage) bool {
	if cfg.Mode != XInitiatorOverrideModeWindowedQuota {
		return false
	}
	return isMissingXInitiatorOverrideField(raw, "overrideTimes")
}

func shouldDefaultLegacyCostTotalCost(cfg XInitiatorOverrideConfig, raw json.RawMessage) bool {
	if cfg.Mode != XInitiatorOverrideModeWindowedCost {
		return false
	}
	return isMissingXInitiatorOverrideField(raw, "totalCost")
}

func isMissingXInitiatorOverrideField(raw json.RawMessage, fieldName string) bool {
	if len(raw) == 0 {
		return false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, exists := fields[fieldName]
	return !exists
}

func (s *Server) saveConfig() error {
	cfg := s.GetConfig()
	return s.persistConfig(cfg)
}

// persistConfig writes the given config to disk without touching in-memory state.
func (s *Server) persistConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(s.configPath, data, 0644)
}

// hopByHopHeaders are HTTP/1.1 hop-by-hop headers that must not be forwarded by proxies (RFC 2616 §13.5.1).
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// removeHopByHopHeaders removes hop-by-hop headers from the header set in place.
func removeHopByHopHeaders(h http.Header) {
	for header := range hopByHopHeaders {
		h.Del(header)
	}
}

// isHopByHopHeader returns true if the header name is a hop-by-hop header.
func isHopByHopHeader(name string) bool {
	return hopByHopHeaders[http.CanonicalHeaderKey(name)]
}
