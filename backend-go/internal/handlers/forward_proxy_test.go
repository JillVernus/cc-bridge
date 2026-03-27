package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/JillVernus/cc-bridge/internal/forwardproxy"
	"github.com/gin-gonic/gin"
)

type forwardProxyConfigResponse struct {
	Enabled            bool                                         `json:"enabled"`
	InterceptDomains   []string                                     `json:"interceptDomains"`
	DomainAliases      map[string]string                            `json:"domainAliases"`
	XInitiatorOverride forwardproxy.XInitiatorOverrideConfig        `json:"xInitiatorOverride"`
	Runtime            forwardproxy.XInitiatorOverrideRuntimeStatus `json:"xInitiatorOverrideRuntime"`
	Running            bool                                         `json:"running"`
	Port               int                                          `json:"port"`
	Message            string                                       `json:"message"`
}

func TestForwardProxyConfig_GetDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil server response includes x-initiator defaults", func(t *testing.T) {
		r := gin.New()
		r.GET("/forward-proxy", GetForwardProxyConfig(nil))

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/forward-proxy", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}

		var resp forwardProxyConfigResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.XInitiatorOverride.Mode != forwardproxy.XInitiatorOverrideModeFixedWindow {
			t.Fatalf("expected default mode %q, got %q", forwardproxy.XInitiatorOverrideModeFixedWindow, resp.XInitiatorOverride.Mode)
		}
		if resp.XInitiatorOverride.DurationSeconds != 300 {
			t.Fatalf("expected default durationSeconds 300, got %d", resp.XInitiatorOverride.DurationSeconds)
		}
		if resp.XInitiatorOverride.OverrideTimes != 1 {
			t.Fatalf("expected default overrideTimes 1, got %d", resp.XInitiatorOverride.OverrideTimes)
		}
	})

	t.Run("persisted quota config missing overrideTimes is normalized", func(t *testing.T) {
		fpServer := newTestForwardProxyServer(t, `{
			"enabled": true,
			"interceptDomains": ["api.example.com"],
			"domainAliases": {},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_quota",
				"durationSeconds": 120
			}
		}`)

		r := gin.New()
		r.GET("/forward-proxy", GetForwardProxyConfig(fpServer))

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/forward-proxy", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}

		var resp forwardProxyConfigResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.XInitiatorOverride.Mode != forwardproxy.XInitiatorOverrideModeWindowedQuota {
			t.Fatalf("expected mode %q, got %q", forwardproxy.XInitiatorOverrideModeWindowedQuota, resp.XInitiatorOverride.Mode)
		}
		if resp.XInitiatorOverride.DurationSeconds != 120 {
			t.Fatalf("expected durationSeconds 120, got %d", resp.XInitiatorOverride.DurationSeconds)
		}
		if resp.XInitiatorOverride.OverrideTimes != 1 {
			t.Fatalf("expected normalized overrideTimes 1, got %d", resp.XInitiatorOverride.OverrideTimes)
		}
	})
}

func TestForwardProxyConfig_UpdateReturnsDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fpServer := newTestForwardProxyServer(t, "")

	r := gin.New()
	r.PUT("/forward-proxy", UpdateForwardProxyConfig(fpServer))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/forward-proxy", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp forwardProxyConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.XInitiatorOverride.Mode != forwardproxy.XInitiatorOverrideModeFixedWindow {
		t.Fatalf("expected default mode %q, got %q", forwardproxy.XInitiatorOverrideModeFixedWindow, resp.XInitiatorOverride.Mode)
	}
	if resp.XInitiatorOverride.DurationSeconds != 300 {
		t.Fatalf("expected default durationSeconds 300, got %d", resp.XInitiatorOverride.DurationSeconds)
	}
	if resp.XInitiatorOverride.OverrideTimes != 1 {
		t.Fatalf("expected default overrideTimes 1, got %d", resp.XInitiatorOverride.OverrideTimes)
	}
}

func TestForwardProxyConfig_UpdateRejectsInvalidXInitiatorOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name: "windowed quota with zero override times returns bad request",
			body: `{
				"xInitiatorOverride": {
					"enabled": true,
					"mode": "windowed_quota",
					"durationSeconds": 300,
					"overrideTimes": 0
				}
			}`,
			wantError: "overrideTimes",
		},
		{
			name: "invalid mode returns bad request",
			body: `{
				"xInitiatorOverride": {
					"enabled": true,
					"mode": "not_a_mode",
					"durationSeconds": 300,
					"overrideTimes": 1
				}
			}`,
			wantError: "mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fpServer := newTestForwardProxyServer(t, "")

			r := gin.New()
			r.PUT("/forward-proxy", UpdateForwardProxyConfig(fpServer))

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/forward-proxy", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tt.wantError) {
				t.Fatalf("expected error body to mention %q, got %s", tt.wantError, w.Body.String())
			}
		})
	}
}

func TestForwardProxyConfig_RuntimeStatusDomains(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("get response returns runtime status domain details", func(t *testing.T) {
		fpServer := newTestForwardProxyServer(t, "")
		current := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
		if err := fpServer.UpdateConfig(forwardproxy.Config{
			Enabled:          true,
			InterceptDomains: []string{"api.a.com", "api.b.com"},
			DomainAliases: map[string]string{
				"api.b.com": "Beta API",
			},
			XInitiatorOverride: forwardproxy.XInitiatorOverrideConfig{
				Enabled:         true,
				Mode:            forwardproxy.XInitiatorOverrideModeWindowedQuota,
				DurationSeconds: 300,
				OverrideTimes:   3,
			},
		}); err != nil {
			t.Fatalf("failed to update config: %v", err)
		}
		setForwardProxyServerNow(t, fpServer, func() time.Time { return current })
		setForwardProxyServerQuotaState(t, fpServer, current)

		r := gin.New()
		r.GET("/forward-proxy", GetForwardProxyConfig(fpServer))

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/forward-proxy", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}

		var resp forwardProxyConfigResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		assertRuntimeStatusDomains(t, resp.Runtime)
	})

	t.Run("update response returns runtime status domain details", func(t *testing.T) {
		fpServer := newTestForwardProxyServer(t, "")

		r := gin.New()
		r.PUT("/forward-proxy", UpdateForwardProxyConfig(fpServer))

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/forward-proxy", strings.NewReader(`{
			"enabled": true,
			"interceptDomains": ["api.a.com", "api.b.com"],
			"domainAliases": {"api.b.com": "Beta API"},
			"xInitiatorOverride": {
				"enabled": true,
				"mode": "windowed_quota",
				"durationSeconds": 300,
				"overrideTimes": 3
			}
		}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}

		var resp forwardProxyConfigResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(resp.Runtime.Domains) != 0 {
			t.Fatalf("expected update to reset runtime domain state, got %#v", resp.Runtime.Domains)
		}
		if resp.Runtime.Mode != resp.XInitiatorOverride.Mode {
			t.Fatalf("expected runtime mode to match config mode, got runtime=%q config=%q", resp.Runtime.Mode, resp.XInitiatorOverride.Mode)
		}
	})
}

func setForwardProxyServerNow(t *testing.T, server *forwardproxy.Server, now func() time.Time) {
	t.Helper()
	setUnexportedField(t, server, "now", now)
}

func setForwardProxyServerQuotaState(t *testing.T, server *forwardproxy.Server, current time.Time) {
	t.Helper()

	serverValue := reflect.ValueOf(server).Elem()
	quotaStateField := serverValue.FieldByName("xInitiatorQuotaDomainState")
	stateType := quotaStateField.Type().Elem()
	quotaStateMap := reflect.MakeMap(quotaStateField.Type())
	makeState := func(expiresAt time.Time, remainingOverrides int, totalOverrides int) reflect.Value {
		state := reflect.New(stateType).Elem()
		setReflectValue(state.FieldByName("expiresAt"), reflect.ValueOf(expiresAt))
		setReflectValue(state.FieldByName("remainingOverrides"), reflect.ValueOf(remainingOverrides))
		setReflectValue(state.FieldByName("totalOverrides"), reflect.ValueOf(totalOverrides))
		return state
	}
	quotaStateMap.SetMapIndex(reflect.ValueOf("api.a.com"), makeState(current.Add(21*time.Second), 2, 3))
	quotaStateMap.SetMapIndex(reflect.ValueOf("api.b.com"), makeState(current.Add(9*time.Second), 1, 3))
	quotaStateMap.SetMapIndex(reflect.ValueOf("api.c.com"), makeState(current.Add(-5*time.Second), 3, 3))
	setUnexportedField(t, server, "xInitiatorQuotaDomainState", quotaStateMap.Interface())
}

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()

	v := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	if !v.IsValid() {
		t.Fatalf("missing field %q", fieldName)
	}
	setReflectValue(v, reflect.ValueOf(value))
}

func setReflectValue(field reflect.Value, value reflect.Value) {
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(value)
}

func assertRuntimeStatusDomains(t *testing.T, status forwardproxy.XInitiatorOverrideRuntimeStatus) {
	t.Helper()

	if status.ActiveDomains != 2 {
		t.Fatalf("expected 2 active domains, got %d", status.ActiveDomains)
	}
	if status.NearestRemainingSeconds != 9 {
		t.Fatalf("expected nearest remaining 9 seconds, got %d", status.NearestRemainingSeconds)
	}
	if len(status.Domains) != 2 {
		t.Fatalf("expected 2 runtime domains, got %d", len(status.Domains))
	}
	if status.Domains[0].Domain != "api.b.com" {
		t.Fatalf("expected nearest-expiry domain first, got %q", status.Domains[0].Domain)
	}
	if status.Domains[0].DisplayName != "Beta API" {
		t.Fatalf("expected aliased display name, got %q", status.Domains[0].DisplayName)
	}
	if status.Domains[0].RemainingOverrides == nil || *status.Domains[0].RemainingOverrides != 1 {
		t.Fatalf("expected remainingOverrides 1, got %#v", status.Domains[0].RemainingOverrides)
	}
	if status.Domains[0].TotalOverrides == nil || *status.Domains[0].TotalOverrides != 3 {
		t.Fatalf("expected totalOverrides 3, got %#v", status.Domains[0].TotalOverrides)
	}
}

func newTestForwardProxyServer(t *testing.T, persistedConfig string) *forwardproxy.Server {
	t.Helper()

	baseDir := t.TempDir()
	certDir := filepath.Join(baseDir, "certs")
	configDir := filepath.Join(baseDir, "config")

	if persistedConfig != "" {
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatalf("failed to create config dir: %v", err)
		}
		configPath := filepath.Join(configDir, "forward-proxy.json")
		if err := os.WriteFile(configPath, []byte(persistedConfig), 0o644); err != nil {
			t.Fatalf("failed to write persisted config: %v", err)
		}
	}

	fpServer, err := forwardproxy.NewServer(forwardproxy.ServerConfig{
		Port:        8443,
		BindAddress: "127.0.0.1",
		CertDir:     certDir,
		ConfigDir:   configDir,
	})
	if err != nil {
		t.Fatalf("failed to create forward proxy server: %v", err)
	}

	return fpServer
}
