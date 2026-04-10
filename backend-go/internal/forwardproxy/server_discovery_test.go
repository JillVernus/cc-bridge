package forwardproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleHTTPForward_RecordsDiscoveryForBlindTunnelCandidate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}

	discoveryStore, err := NewDiscoveryStore(filepath.Join(t.TempDir(), "forward-proxy-discovery.json"))
	if err != nil {
		t.Fatalf("failed to create discovery store: %v", err)
	}

	s := &Server{
		httpClient:       upstream.Client(),
		enabled:          true,
		discoveryEnabled: true,
		interceptDomains: map[string]bool{},
		discoveryStore:   discoveryStore,
	}

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/health", nil)
	rec := httptest.NewRecorder()

	s.handleHTTPForward(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	entries := discoveryStore.List()
	if len(entries) != 1 {
		t.Fatalf("expected one discovery entry, got %d", len(entries))
	}
	if entries[0].Host != strings.ToLower(upstreamURL.Hostname()) {
		t.Fatalf("expected host %q, got %q", upstreamURL.Hostname(), entries[0].Host)
	}
	if entries[0].Transport != DiscoveryTransportHTTPForward {
		t.Fatalf("expected transport %q, got %q", DiscoveryTransportHTTPForward, entries[0].Transport)
	}
	if entries[0].Intercepted {
		t.Fatalf("expected intercepted=false for non-intercepted forward request")
	}
	if entries[0].LastMethod != http.MethodGet {
		t.Fatalf("expected lastMethod GET, got %q", entries[0].LastMethod)
	}
	if entries[0].LastPath != "/health" {
		t.Fatalf("expected lastPath /health, got %q", entries[0].LastPath)
	}
}
