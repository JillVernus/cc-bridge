package forwardproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestApplyXInitiatorOverride_FixedWindowPerDomain(t *testing.T) {
	current := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	s := &Server{
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

	first := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", first); overridden {
		t.Fatalf("expected first request to start the window without override")
	}
	if got := first.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected first request header to stay user, got %q", got)
	}

	current = current.Add(10 * time.Second)
	second := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", second); !overridden {
		t.Fatalf("expected second request inside active fixed window to override")
	}
	if got := second.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected overridden header agent, got %q", got)
	}

	current = current.Add(291 * time.Second)
	expired := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", expired); overridden {
		t.Fatalf("expected fixed window not to extend on override")
	}
	if got := expired.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected post-expiry header to stay user, got %q", got)
	}

	otherDomain := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", otherDomain); overridden {
		t.Fatalf("expected per-domain tracking to avoid overriding a different domain")
	}
}

func TestApplyXInitiatorOverride_RelativeCountdownRefreshesPerDomain(t *testing.T) {
	current := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeRelativeCountdown,
			DurationSeconds: 30,
		},
		xInitiatorDomainState: make(map[string]time.Time),
		now: func() time.Time {
			return current
		},
	}

	first := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", first); overridden {
		t.Fatalf("expected first request to start the countdown without override")
	}

	current = current.Add(10 * time.Second)
	second := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", second); !overridden {
		t.Fatalf("expected second request before expiry to override")
	}
	if got := second.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected overridden header agent, got %q", got)
	}

	current = current.Add(25 * time.Second)
	third := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", third); !overridden {
		t.Fatalf("expected refreshed countdown to still be active")
	}

	current = current.Add(31 * time.Second)
	expired := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", expired); overridden {
		t.Fatalf("expected request after refreshed countdown expiry not to override")
	}
	if got := expired.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected post-expiry header to stay user, got %q", got)
	}

	otherDomain := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", otherDomain); overridden {
		t.Fatalf("expected relative countdown to stay scoped per domain")
	}
}

func TestHandleHTTPForward_XInitiatorOverride_OverridesLaterRequests(t *testing.T) {
	current := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	seen := make([]string, 0, 2)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Initiator"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}
	hostOnly := strings.ToLower(upstreamURL.Hostname())

	s := &Server{
		httpClient: upstream.Client(),
		enabled:    true,
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
		req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/messages", strings.NewReader(`{"stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Initiator", "user")

		rec := httptest.NewRecorder()
		s.handleHTTPForward(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		current = current.Add(10 * time.Second)
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
}

func TestGetXInitiatorOverrideRuntimeStatus_ReportsNearestExpiryAndActiveDomains(t *testing.T) {
	current := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeRelativeCountdown,
			DurationSeconds: 30,
		},
		xInitiatorDomainState: map[string]time.Time{
			"api.a.com": current.Add(21 * time.Second),
			"api.b.com": current.Add(9 * time.Second),
			"api.c.com": current.Add(-5 * time.Second),
		},
		now: func() time.Time {
			return current
		},
	}

	status := s.GetXInitiatorOverrideRuntimeStatus()
	if !status.Enabled {
		t.Fatalf("expected enabled runtime status")
	}
	if status.Mode != XInitiatorOverrideModeRelativeCountdown {
		t.Fatalf("expected mode %q, got %q", XInitiatorOverrideModeRelativeCountdown, status.Mode)
	}
	if status.ActiveDomains != 2 {
		t.Fatalf("expected 2 active domains, got %d", status.ActiveDomains)
	}
	if status.NearestRemainingSeconds != 9 {
		t.Fatalf("expected nearest remaining 9 seconds, got %d", status.NearestRemainingSeconds)
	}
	if status.NearestExpiryAt == nil || !status.NearestExpiryAt.Equal(current.Add(9*time.Second)) {
		t.Fatalf("expected nearest expiry at %s, got %#v", current.Add(9*time.Second).Format(time.RFC3339), status.NearestExpiryAt)
	}
}
