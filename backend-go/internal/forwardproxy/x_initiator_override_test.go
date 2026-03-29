package forwardproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateXInitiatorOverrideConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     XInitiatorOverrideConfig
		wantErr bool
	}{
		{
			name: "accepts windowed cost with positive duration and total cost",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedCost,
				DurationSeconds: 300,
				TotalCost:       2.5,
			},
		},
		{
			name: "rejects windowed cost when total cost is zero",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedCost,
				DurationSeconds: 300,
				TotalCost:       0,
			},
			wantErr: true,
		},
		{
			name: "accepts windowed quota with positive duration and override times",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedQuota,
				DurationSeconds: 300,
				OverrideTimes:   3,
			},
		},
		{
			name: "rejects windowed quota when override times is zero",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedQuota,
				DurationSeconds: 300,
				OverrideTimes:   0,
			},
			wantErr: true,
		},
		{
			name: "ignores zero override times for fixed window",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeFixedWindow,
				DurationSeconds: 300,
				OverrideTimes:   0,
				TotalCost:       0,
			},
		},
		{
			name: "ignores omitted override times and total cost for relative countdown",
			cfg: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeRelativeCountdown,
				DurationSeconds: 300,
				TotalCost:       0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateXInitiatorOverrideConfig(tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}

func TestGetConfig_XInitiatorOverrideDefaults(t *testing.T) {
	t.Run("fills missing defaults", func(t *testing.T) {
		s := &Server{}

		cfg := s.GetConfig()

		if cfg.XInitiatorOverride.Mode != XInitiatorOverrideModeFixedWindow {
			t.Fatalf("expected default mode %q, got %q", XInitiatorOverrideModeFixedWindow, cfg.XInitiatorOverride.Mode)
		}
		if cfg.XInitiatorOverride.DurationSeconds != 300 {
			t.Fatalf("expected default durationSeconds 300, got %d", cfg.XInitiatorOverride.DurationSeconds)
		}
		if cfg.XInitiatorOverride.OverrideTimes != 1 {
			t.Fatalf("expected default overrideTimes 1, got %d", cfg.XInitiatorOverride.OverrideTimes)
		}
		if cfg.XInitiatorOverride.TotalCost != 1 {
			t.Fatalf("expected default totalCost 1, got %v", cfg.XInitiatorOverride.TotalCost)
		}
	})

	t.Run("normalizes missing quota override times", func(t *testing.T) {
		s := &Server{
			xInitiatorOverride: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedQuota,
				DurationSeconds: 120,
			},
		}

		cfg := s.GetConfig()

		if cfg.XInitiatorOverride.OverrideTimes != 1 {
			t.Fatalf("expected normalized overrideTimes 1, got %d", cfg.XInitiatorOverride.OverrideTimes)
		}
		if cfg.XInitiatorOverride.TotalCost != 1 {
			t.Fatalf("expected normalized totalCost 1, got %v", cfg.XInitiatorOverride.TotalCost)
		}
	})

	t.Run("normalizes missing windowed cost total cost", func(t *testing.T) {
		s := &Server{
			xInitiatorOverride: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedCost,
				DurationSeconds: 120,
			},
		}

		cfg := s.GetConfig()

		if cfg.XInitiatorOverride.TotalCost != 1 {
			t.Fatalf("expected normalized totalCost 1, got %v", cfg.XInitiatorOverride.TotalCost)
		}
	})
}

func TestLoadConfig_XInitiatorOverrideCompatibility(t *testing.T) {
	t.Run("loads legacy windowed quota missing override times", func(t *testing.T) {
		configPath := writeForwardProxyConfigFile(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_quota",
				"durationSeconds": 120
			}
		}`)

		s := &Server{configPath: configPath}
		if err := s.loadConfig(); err != nil {
			t.Fatalf("expected legacy config to load, got %v", err)
		}

		if s.xInitiatorOverride.OverrideTimes != 1 {
			t.Fatalf("expected missing overrideTimes to normalize to 1, got %d", s.xInitiatorOverride.OverrideTimes)
		}
	})

	t.Run("rejects persisted windowed quota with explicit zero override times", func(t *testing.T) {
		configPath := writeForwardProxyConfigFile(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_quota",
				"durationSeconds": 120,
				"overrideTimes": 0
			}
		}`)

		s := &Server{configPath: configPath}
		if err := s.loadConfig(); err == nil {
			t.Fatalf("expected persisted config with explicit zero overrideTimes to fail")
		}
	})

	t.Run("rejects persisted config with explicit zero duration", func(t *testing.T) {
		configPath := writeForwardProxyConfigFile(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "fixed_window",
				"durationSeconds": 0
			}
		}`)

		s := &Server{configPath: configPath}
		if err := s.loadConfig(); err == nil {
			t.Fatalf("expected persisted config with explicit zero durationSeconds to fail")
		}
	})

	t.Run("loads legacy windowed cost missing total cost", func(t *testing.T) {
		configPath := writeForwardProxyConfigFile(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_cost",
				"durationSeconds": 120
			}
		}`)

		s := &Server{configPath: configPath}
		if err := s.loadConfig(); err != nil {
			t.Fatalf("expected legacy cost config to load, got %v", err)
		}

		if s.xInitiatorOverride.TotalCost != 1 {
			t.Fatalf("expected missing totalCost to normalize to 1, got %v", s.xInitiatorOverride.TotalCost)
		}
	})

	t.Run("rejects persisted windowed cost with explicit zero total cost", func(t *testing.T) {
		configPath := writeForwardProxyConfigFile(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_cost",
				"durationSeconds": 120,
				"totalCost": 0
			}
		}`)

		s := &Server{configPath: configPath}
		if err := s.loadConfig(); err == nil {
			t.Fatalf("expected persisted config with explicit zero totalCost to fail")
		}
	})
}

func TestNewServer_ConfigLoadBehavior(t *testing.T) {
	t.Run("seeds defaults when persisted config does not exist", func(t *testing.T) {
		configDir := t.TempDir()
		certDir := t.TempDir()

		s, err := NewServer(ServerConfig{
			ConfigDir:        configDir,
			CertDir:          certDir,
			Enabled:          true,
			InterceptDomains: []string{"api.example.com"},
		})
		if err != nil {
			t.Fatalf("expected missing persisted config to fall back to defaults, got %v", err)
		}

		cfg := s.GetConfig()
		if !cfg.Enabled {
			t.Fatalf("expected fallback config to preserve enabled default")
		}
		if len(cfg.InterceptDomains) != 1 || cfg.InterceptDomains[0] != "api.example.com" {
			t.Fatalf("expected fallback config to persist intercept domains, got %#v", cfg.InterceptDomains)
		}
	})

	t.Run("returns error when persisted config is invalid", func(t *testing.T) {
		configDir := t.TempDir()
		certDir := t.TempDir()
		configPath := filepath.Join(configDir, "forward-proxy.json")
		if err := os.WriteFile(configPath, []byte(`{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_quota",
				"durationSeconds": 120,
				"overrideTimes": 0
			}
		}`), 0o644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		_, err := NewServer(ServerConfig{
			ConfigDir: configDir,
			CertDir:   certDir,
		})
		if err == nil {
			t.Fatalf("expected invalid persisted config to return an error")
		}
		if !strings.Contains(err.Error(), "load persisted config") {
			t.Fatalf("expected wrapped load persisted config error, got %v", err)
		}
	})
}

func writeForwardProxyConfigFile(t *testing.T, contents string) string {
	t.Helper()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "forward-proxy.json")
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return configPath
}

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

func TestApplyXInitiatorOverride_WindowedQuotaPerDomain(t *testing.T) {
	current := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedQuota,
			DurationSeconds: 300,
			OverrideTimes:   3,
		},
		xInitiatorDomainState:      make(map[string]time.Time),
		xInitiatorQuotaDomainState: make(map[string]xInitiatorQuotaState),
		now: func() time.Time {
			return current
		},
	}

	t.Run("first request starts window without rewrite", func(t *testing.T) {
		headers := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", headers); overridden {
			t.Fatalf("expected first windowed quota request not to rewrite")
		}
		if got := headers.Get("X-Initiator"); got != "user" {
			t.Fatalf("expected header to remain user, got %q", got)
		}

		state, ok := s.xInitiatorQuotaDomainState["api.example.com"]
		if !ok {
			t.Fatalf("expected first request to create quota state")
		}
		if !state.expiresAt.Equal(current.Add(300 * time.Second)) {
			t.Fatalf("expected expiresAt %s, got %s", current.Add(300*time.Second).Format(time.RFC3339), state.expiresAt.Format(time.RFC3339))
		}
		if state.remainingOverrides != 3 {
			t.Fatalf("expected remainingOverrides 3, got %d", state.remainingOverrides)
		}
		if state.totalOverrides != 3 {
			t.Fatalf("expected totalOverrides 3, got %d", state.totalOverrides)
		}
	})

	t.Run("second and later requests rewrite and decrement quota", func(t *testing.T) {
		current = current.Add(10 * time.Second)

		second := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", second); !overridden {
			t.Fatalf("expected second request to rewrite")
		}
		if got := second.Get("X-Initiator"); got != "agent" {
			t.Fatalf("expected second request header agent, got %q", got)
		}
		if remaining := s.xInitiatorQuotaDomainState["api.example.com"].remainingOverrides; remaining != 2 {
			t.Fatalf("expected remainingOverrides 2 after second request, got %d", remaining)
		}

		current = current.Add(10 * time.Second)
		third := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", third); !overridden {
			t.Fatalf("expected third request to rewrite")
		}
		if got := third.Get("X-Initiator"); got != "agent" {
			t.Fatalf("expected third request header agent, got %q", got)
		}
		if remaining := s.xInitiatorQuotaDomainState["api.example.com"].remainingOverrides; remaining != 1 {
			t.Fatalf("expected remainingOverrides 1 after third request, got %d", remaining)
		}
	})

	t.Run("quota exhaustion clears active domain immediately", func(t *testing.T) {
		current = current.Add(10 * time.Second)

		fourth := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", fourth); !overridden {
			t.Fatalf("expected quota-consuming request to rewrite")
		}
		if got := fourth.Get("X-Initiator"); got != "agent" {
			t.Fatalf("expected exhausted request header agent, got %q", got)
		}
		if _, ok := s.xInitiatorQuotaDomainState["api.example.com"]; ok {
			t.Fatalf("expected exhausted quota state to be cleared immediately")
		}

		current = current.Add(10 * time.Second)
		reset := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", reset); overridden {
			t.Fatalf("expected request after exhaustion to become fresh trigger")
		}
		if got := reset.Get("X-Initiator"); got != "user" {
			t.Fatalf("expected fresh trigger after exhaustion to stay user, got %q", got)
		}
	})

	t.Run("expiry resets remaining quota without rewrite", func(t *testing.T) {
		current = current.Add(301 * time.Second)

		expired := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", expired); overridden {
			t.Fatalf("expected expired quota state to reset instead of rewrite")
		}
		if got := expired.Get("X-Initiator"); got != "user" {
			t.Fatalf("expected expired trigger to stay user, got %q", got)
		}

		state, ok := s.xInitiatorQuotaDomainState["api.example.com"]
		if !ok {
			t.Fatalf("expected expired request to recreate quota state")
		}
		if state.remainingOverrides != 3 {
			t.Fatalf("expected expiry reset remainingOverrides to 3, got %d", state.remainingOverrides)
		}
		if !state.expiresAt.Equal(current.Add(300 * time.Second)) {
			t.Fatalf("expected refreshed expiresAt %s, got %s", current.Add(300*time.Second).Format(time.RFC3339), state.expiresAt.Format(time.RFC3339))
		}
	})

	t.Run("per-domain isolation still holds", func(t *testing.T) {
		otherDomainFirst := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.other.com", otherDomainFirst); overridden {
			t.Fatalf("expected first request for other domain not to rewrite")
		}
		if got := otherDomainFirst.Get("X-Initiator"); got != "user" {
			t.Fatalf("expected other domain trigger to stay user, got %q", got)
		}

		current = current.Add(10 * time.Second)
		otherDomainSecond := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.other.com", otherDomainSecond); !overridden {
			t.Fatalf("expected second request for other domain to rewrite independently")
		}
		if got := otherDomainSecond.Get("X-Initiator"); got != "agent" {
			t.Fatalf("expected other domain second request header agent, got %q", got)
		}
		if remaining := s.xInitiatorQuotaDomainState["api.other.com"].remainingOverrides; remaining != 2 {
			t.Fatalf("expected other domain remainingOverrides 2, got %d", remaining)
		}

		if remaining := s.xInitiatorQuotaDomainState["api.example.com"].remainingOverrides; remaining != 3 {
			t.Fatalf("expected api.example.com state to remain independent with 3 remaining, got %d", remaining)
		}
	})
}

func TestApplyXInitiatorOverride_WindowedQuotaIgnoresNonUser(t *testing.T) {
	current := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedQuota,
			DurationSeconds: 300,
			OverrideTimes:   2,
		},
		xInitiatorDomainState:      make(map[string]time.Time),
		xInitiatorQuotaDomainState: make(map[string]xInitiatorQuotaState),
		now: func() time.Time {
			return current
		},
	}

	for _, initiator := range []string{"agent", "system", ""} {
		headers := http.Header{}
		if initiator != "" {
			headers.Set("X-Initiator", initiator)
		}
		if overridden := s.applyXInitiatorOverride("api.example.com", headers); overridden {
			t.Fatalf("expected initiator %q not to rewrite", initiator)
		}
		if len(s.xInitiatorQuotaDomainState) != 0 {
			t.Fatalf("expected initiator %q not to create quota state", initiator)
		}
	}

	trigger := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", trigger); overridden {
		t.Fatalf("expected first user request to remain trigger request")
	}
	if remaining := s.xInitiatorQuotaDomainState["api.example.com"].remainingOverrides; remaining != 2 {
		t.Fatalf("expected first user request not to consume quota, got %d remaining", remaining)
	}

	nonUser := http.Header{"X-Initiator": []string{"agent"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", nonUser); overridden {
		t.Fatalf("expected active quota state not to rewrite non-user request")
	}
	if remaining := s.xInitiatorQuotaDomainState["api.example.com"].remainingOverrides; remaining != 2 {
		t.Fatalf("expected non-user request not to consume quota, got %d remaining", remaining)
	}
}

func TestApplyXInitiatorOverride_WindowedCostPerDomain(t *testing.T) {
	current := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedCost,
			DurationSeconds: 300,
			TotalCost:       2.5,
		},
		now: func() time.Time {
			return current
		},
	}

	first := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", first); overridden {
		t.Fatalf("expected first windowed_cost request to start the window without rewrite")
	}
	if got := first.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected first request header to stay user, got %q", got)
	}

	current = current.Add(10 * time.Second)
	second := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", second); !overridden {
		t.Fatalf("expected later request in active window to rewrite")
	}
	if got := second.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected later request header to be rewritten to agent, got %q", got)
	}

	otherDomainFirst := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", otherDomainFirst); overridden {
		t.Fatalf("expected first request for different domain not to rewrite")
	}
	if got := otherDomainFirst.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected other domain trigger request header to stay user, got %q", got)
	}

	current = current.Add(10 * time.Second)
	otherDomainSecond := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", otherDomainSecond); !overridden {
		t.Fatalf("expected second request for different domain to rewrite independently")
	}
	if got := otherDomainSecond.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected other domain second request header agent, got %q", got)
	}
}

func TestApplyXInitiatorOverride_WindowedCostIgnoresNonUser(t *testing.T) {
	current := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedCost,
			DurationSeconds: 300,
			TotalCost:       2.5,
		},
		now: func() time.Time {
			return current
		},
	}

	for _, initiator := range []string{"agent", "system", ""} {
		headers := http.Header{}
		if initiator != "" {
			headers.Set("X-Initiator", initiator)
		}
		if overridden := s.applyXInitiatorOverride("api.example.com", headers); overridden {
			t.Fatalf("expected initiator %q not to rewrite", initiator)
		}
		if got := headers.Get("X-Initiator"); got != initiator {
			t.Fatalf("expected initiator %q to pass through unchanged, got %q", initiator, got)
		}
	}

	trigger := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", trigger); overridden {
		t.Fatalf("expected first user request to stay the trigger request")
	}

	nonUser := http.Header{"X-Initiator": []string{"agent"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", nonUser); overridden {
		t.Fatalf("expected active window not to rewrite non-user requests")
	}
	if got := nonUser.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected non-user request during active window to stay agent, got %q", got)
	}

	otherDomain := http.Header{"X-Initiator": []string{"agent"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", otherDomain); overridden {
		t.Fatalf("expected non-user request for idle domain not to rewrite")
	}

	firstUserOtherDomain := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.other.com", firstUserOtherDomain); overridden {
		t.Fatalf("expected non-user traffic not to start a new window for another domain")
	}
	if got := firstUserOtherDomain.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected first user request after non-user traffic to stay user, got %q", got)
	}
}

func TestApplyXInitiatorOverride_WindowedCostExpiryReset(t *testing.T) {
	current := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedCost,
			DurationSeconds: 30,
			TotalCost:       2.5,
		},
		now: func() time.Time {
			return current
		},
	}

	first := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", first); overridden {
		t.Fatalf("expected first request to start the window without rewrite")
	}

	current = current.Add(10 * time.Second)
	second := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", second); !overridden {
		t.Fatalf("expected later request before expiry to rewrite")
	}

	current = current.Add(21 * time.Second)
	nonUserAfterExpiry := http.Header{"X-Initiator": []string{"agent"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", nonUserAfterExpiry); overridden {
		t.Fatalf("expected expired state not to rewrite non-user request")
	}
	if got := nonUserAfterExpiry.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected non-user request after expiry to stay agent, got %q", got)
	}

	freshTrigger := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", freshTrigger); overridden {
		t.Fatalf("expected next user request after expiry to become a fresh trigger")
	}
	if got := freshTrigger.Get("X-Initiator"); got != "user" {
		t.Fatalf("expected fresh trigger after expiry to stay user, got %q", got)
	}

	current = current.Add(10 * time.Second)
	rewrittenAgain := http.Header{"X-Initiator": []string{"user"}}
	if overridden := s.applyXInitiatorOverride("api.example.com", rewrittenAgain); !overridden {
		t.Fatalf("expected request after fresh trigger to rewrite in the new window")
	}
	if got := rewrittenAgain.Get("X-Initiator"); got != "agent" {
		t.Fatalf("expected rewritten request in renewed window to be agent, got %q", got)
	}
}

func TestApplyWindowedCostCompletion(t *testing.T) {
	t.Run("counts trigger non-user and rewritten requests then resets on threshold crossing", func(t *testing.T) {
		current := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
		s := &Server{
			xInitiatorOverride: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedCost,
				DurationSeconds: 300,
				TotalCost:       2.5,
			},
			xInitiatorCostDomainState: make(map[string]xInitiatorCostState),
			now: func() time.Time {
				return current
			},
		}

		trigger := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("API.EXAMPLE.COM", trigger); overridden {
			t.Fatalf("expected trigger request not to rewrite")
		}

		state, ok := s.xInitiatorCostDomainState["api.example.com"]
		if !ok {
			t.Fatalf("expected trigger request to start cost state")
		}
		if !state.expiresAt.Equal(current.Add(300 * time.Second)) {
			t.Fatalf("expected expiresAt %s, got %s", current.Add(300*time.Second).Format(time.RFC3339), state.expiresAt.Format(time.RFC3339))
		}
		if state.accumulatedCost != 0 {
			t.Fatalf("expected trigger request to start with zero accumulated cost, got %v", state.accumulatedCost)
		}
		if state.budgetCost != 2.5 {
			t.Fatalf("expected trigger request budget cost 2.5, got %v", state.budgetCost)
		}

		current = current.Add(5 * time.Second)
		s.applyWindowedCostCompletion(" API.EXAMPLE.COM ", current, 1.0)

		state = s.xInitiatorCostDomainState["api.example.com"]
		if state.accumulatedCost != 1.0 {
			t.Fatalf("expected trigger completion to accumulate 1.0, got %v", state.accumulatedCost)
		}
		if !state.expiresAt.Equal(time.Date(2026, 3, 29, 0, 5, 0, 0, time.UTC)) {
			t.Fatalf("expected expiry to remain unchanged after trigger completion, got %s", state.expiresAt.Format(time.RFC3339))
		}

		nonUser := http.Header{"X-Initiator": []string{"agent"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", nonUser); overridden {
			t.Fatalf("expected active window not to rewrite non-user request")
		}

		current = current.Add(5 * time.Second)
		s.applyWindowedCostCompletion("api.example.com", current, 0.6)

		state = s.xInitiatorCostDomainState["api.example.com"]
		if state.accumulatedCost != 1.6 {
			t.Fatalf("expected non-user completion to accumulate 1.6, got %v", state.accumulatedCost)
		}
		if state.budgetCost != 2.5 {
			t.Fatalf("expected budget cost to remain 2.5, got %v", state.budgetCost)
		}

		rewritten := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", rewritten); !overridden {
			t.Fatalf("expected active window to rewrite later user request")
		}
		if got := rewritten.Get("X-Initiator"); got != "agent" {
			t.Fatalf("expected rewritten request header agent, got %q", got)
		}

		current = current.Add(5 * time.Second)
		s.applyWindowedCostCompletion("api.example.com", current, 1.0)

		if _, ok := s.xInitiatorCostDomainState["api.example.com"]; ok {
			t.Fatalf("expected threshold-crossing completion to clear active state immediately")
		}

		freshTrigger := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", freshTrigger); overridden {
			t.Fatalf("expected next user request after reset to become a fresh trigger")
		}
		freshState, ok := s.xInitiatorCostDomainState["api.example.com"]
		if !ok {
			t.Fatalf("expected reset domain to start a fresh state")
		}
		if freshState.accumulatedCost != 0 {
			t.Fatalf("expected fresh trigger to restart accumulated cost at 0, got %v", freshState.accumulatedCost)
		}
		if freshState.budgetCost != 2.5 {
			t.Fatalf("expected fresh trigger budget cost 2.5, got %v", freshState.budgetCost)
		}
		if !freshState.expiresAt.Equal(current.Add(300 * time.Second)) {
			t.Fatalf("expected fresh trigger expiresAt %s, got %s", current.Add(300*time.Second).Format(time.RFC3339), freshState.expiresAt.Format(time.RFC3339))
		}
	})

	t.Run("late completion after expiry does not revive stale state", func(t *testing.T) {
		current := time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)
		s := &Server{
			xInitiatorOverride: XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            XInitiatorOverrideModeWindowedCost,
				DurationSeconds: 30,
				TotalCost:       2.5,
			},
			xInitiatorCostDomainState: make(map[string]xInitiatorCostState),
			now: func() time.Time {
				return current
			},
		}

		trigger := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", trigger); overridden {
			t.Fatalf("expected first request to start the window without rewrite")
		}

		current = current.Add(31 * time.Second)
		s.applyWindowedCostCompletion("api.example.com", current, 1.0)

		if _, ok := s.xInitiatorCostDomainState["api.example.com"]; ok {
			t.Fatalf("expected expired completion not to keep or revive stale state")
		}

		freshTrigger := http.Header{"X-Initiator": []string{"user"}}
		if overridden := s.applyXInitiatorOverride("api.example.com", freshTrigger); overridden {
			t.Fatalf("expected next user request after expiry to become a fresh trigger")
		}
		freshState, ok := s.xInitiatorCostDomainState["api.example.com"]
		if !ok {
			t.Fatalf("expected fresh trigger to recreate state after expiry")
		}
		if freshState.accumulatedCost != 0 {
			t.Fatalf("expected fresh state to restart accumulated cost at 0, got %v", freshState.accumulatedCost)
		}
		if freshState.budgetCost != 2.5 {
			t.Fatalf("expected fresh state budget cost 2.5, got %v", freshState.budgetCost)
		}
		if !freshState.expiresAt.Equal(current.Add(30 * time.Second)) {
			t.Fatalf("expected fresh state expiresAt %s, got %s", current.Add(30*time.Second).Format(time.RFC3339), freshState.expiresAt.Format(time.RFC3339))
		}
	})
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
		domainAliases: map[string]string{
			"api.b.com": "Beta API",
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
	if len(status.Domains) != 2 {
		t.Fatalf("expected 2 domain details, got %d", len(status.Domains))
	}
	if status.Domains[0].Domain != "api.b.com" {
		t.Fatalf("expected nearest-expiry domain first, got %q", status.Domains[0].Domain)
	}
	if status.Domains[0].DisplayName != "Beta API" {
		t.Fatalf("expected aliased display name, got %q", status.Domains[0].DisplayName)
	}
	if status.Domains[0].RemainingSeconds != 9 {
		t.Fatalf("expected first domain remainingSeconds 9, got %d", status.Domains[0].RemainingSeconds)
	}
	if status.Domains[0].RemainingOverrides != nil || status.Domains[0].TotalOverrides != nil {
		t.Fatalf("expected non-quota mode not to expose override counts")
	}
	if status.Domains[1].Domain != "api.a.com" {
		t.Fatalf("expected second domain detail for api.a.com, got %q", status.Domains[1].Domain)
	}
}

func TestGetXInitiatorOverrideRuntimeStatus_WindowedQuotaSummary(t *testing.T) {
	current := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedQuota,
			DurationSeconds: 300,
			OverrideTimes:   3,
		},
		xInitiatorQuotaDomainState: map[string]xInitiatorQuotaState{
			"api.a.com": {expiresAt: current.Add(21 * time.Second), remainingOverrides: 2, totalOverrides: 3},
			"api.b.com": {expiresAt: current.Add(9 * time.Second), remainingOverrides: 1, totalOverrides: 3},
			"api.c.com": {expiresAt: current.Add(-5 * time.Second), remainingOverrides: 3, totalOverrides: 3},
		},
		domainAliases: map[string]string{
			"api.b.com": "Beta API",
		},
		now: func() time.Time {
			return current
		},
	}

	status := s.GetXInitiatorOverrideRuntimeStatus()
	if !status.Enabled {
		t.Fatalf("expected enabled runtime status")
	}
	if status.Mode != XInitiatorOverrideModeWindowedQuota {
		t.Fatalf("expected mode %q, got %q", XInitiatorOverrideModeWindowedQuota, status.Mode)
	}
	if status.ActiveDomains != 2 {
		t.Fatalf("expected 2 active quota domains, got %d", status.ActiveDomains)
	}
	if status.NearestRemainingSeconds != 9 {
		t.Fatalf("expected nearest remaining 9 seconds, got %d", status.NearestRemainingSeconds)
	}
	if status.NearestExpiryAt == nil || !status.NearestExpiryAt.Equal(current.Add(9*time.Second)) {
		t.Fatalf("expected nearest expiry at %s, got %#v", current.Add(9*time.Second).Format(time.RFC3339), status.NearestExpiryAt)
	}
	if len(status.Domains) != 2 {
		t.Fatalf("expected 2 domain details, got %d", len(status.Domains))
	}
	if status.Domains[0].Domain != "api.b.com" {
		t.Fatalf("expected nearest-expiry domain first, got %q", status.Domains[0].Domain)
	}
	if status.Domains[0].DisplayName != "Beta API" {
		t.Fatalf("expected aliased display name, got %q", status.Domains[0].DisplayName)
	}
	if status.Domains[0].RemainingSeconds != 9 {
		t.Fatalf("expected first domain remainingSeconds 9, got %d", status.Domains[0].RemainingSeconds)
	}
	if status.Domains[0].RemainingOverrides == nil || *status.Domains[0].RemainingOverrides != 1 {
		t.Fatalf("expected first domain remainingOverrides 1, got %#v", status.Domains[0].RemainingOverrides)
	}
	if status.Domains[0].TotalOverrides == nil || *status.Domains[0].TotalOverrides != 3 {
		t.Fatalf("expected first domain totalOverrides 3, got %#v", status.Domains[0].TotalOverrides)
	}
	if status.Domains[1].Domain != "api.a.com" {
		t.Fatalf("expected second domain detail for api.a.com, got %q", status.Domains[1].Domain)
	}
}

func TestGetXInitiatorOverrideRuntimeStatus_WindowedCostRemainsIdleForTask1(t *testing.T) {
	current := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	s := &Server{
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedCost,
			DurationSeconds: 300,
			TotalCost:       2.5,
		},
		xInitiatorDomainState: map[string]time.Time{
			"api.example.com": current.Add(30 * time.Second),
		},
		now: func() time.Time {
			return current
		},
	}

	status := s.GetXInitiatorOverrideRuntimeStatus()
	if !status.Enabled {
		t.Fatalf("expected enabled runtime status")
	}
	if status.Mode != XInitiatorOverrideModeWindowedCost {
		t.Fatalf("expected mode %q, got %q", XInitiatorOverrideModeWindowedCost, status.Mode)
	}
	if status.ActiveDomains != 0 {
		t.Fatalf("expected windowed_cost to report no active runtime domains during Task 1, got %d", status.ActiveDomains)
	}
	if status.NearestExpiryAt != nil {
		t.Fatalf("expected no nearest expiry for inactive windowed_cost runtime, got %#v", status.NearestExpiryAt)
	}
	if status.NearestRemainingSeconds != 0 {
		t.Fatalf("expected no remaining seconds for inactive windowed_cost runtime, got %d", status.NearestRemainingSeconds)
	}
	if len(status.Domains) != 0 {
		t.Fatalf("expected no domain details for inactive windowed_cost runtime, got %#v", status.Domains)
	}
}

func TestGetForwardProxyConfigSnapshot_RuntimeMatchesConfig(t *testing.T) {
	current := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	s := &Server{
		enabled: true,
		interceptDomains: map[string]bool{
			"api.a.com": true,
			"api.b.com": true,
		},
		domainAliases: map[string]string{
			"api.b.com": "Beta API",
		},
		xInitiatorOverride: XInitiatorOverrideConfig{
			Enabled:         true,
			Mode:            XInitiatorOverrideModeWindowedQuota,
			DurationSeconds: 300,
			OverrideTimes:   3,
		},
		xInitiatorQuotaDomainState: map[string]xInitiatorQuotaState{
			"api.a.com": {expiresAt: current.Add(21 * time.Second), remainingOverrides: 2, totalOverrides: 3},
			"api.b.com": {expiresAt: current.Add(9 * time.Second), remainingOverrides: 1, totalOverrides: 3},
		},
		now: func() time.Time {
			return current
		},
	}

	snapshot := s.GetConfigSnapshot()

	if !snapshot.Config.XInitiatorOverride.Enabled {
		t.Fatalf("expected snapshot config override to be enabled")
	}
	if snapshot.Config.XInitiatorOverride.Mode != XInitiatorOverrideModeWindowedQuota {
		t.Fatalf("expected snapshot mode %q, got %q", XInitiatorOverrideModeWindowedQuota, snapshot.Config.XInitiatorOverride.Mode)
	}
	if snapshot.Runtime.Mode != snapshot.Config.XInitiatorOverride.Mode {
		t.Fatalf("expected runtime mode to match config mode, got runtime=%q config=%q", snapshot.Runtime.Mode, snapshot.Config.XInitiatorOverride.Mode)
	}
	if snapshot.Runtime.ActiveDomains != 2 {
		t.Fatalf("expected 2 active domains, got %d", snapshot.Runtime.ActiveDomains)
	}
	if len(snapshot.Runtime.Domains) != 2 {
		t.Fatalf("expected 2 runtime domains, got %d", len(snapshot.Runtime.Domains))
	}
	if snapshot.Runtime.Domains[0].DisplayName != "Beta API" {
		t.Fatalf("expected aliased display name, got %q", snapshot.Runtime.Domains[0].DisplayName)
	}
}
