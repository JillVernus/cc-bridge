package middleware

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// setupRouterWithAuth builds a minimal router with the auth middleware wired.
func setupRouterWithAuth(envCfg *config.EnvConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(WebAuthMiddleware(envCfg, nil))

	// Protected management API
	r.GET("/api/channels", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// SPA routes should pass through without access key
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "home")
	})
	r.GET("/dashboard", func(c *gin.Context) {
		c.String(http.StatusOK, "dashboard")
	})

	return r
}

func setupRouterWithAuthAndAPIKeyManager(envCfg *config.EnvConfig, apiKeyManager *apikey.Manager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(WebAuthMiddlewareWithAPIKey(envCfg, nil, apiKeyManager))

	// Protected management APIs
	r.GET("/api/channels", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/messages/channels/current", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return r
}

func newTestAPIKeyManager(t *testing.T) *apikey.Manager {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	manager, err := apikey.NewManager(db)
	if err != nil {
		t.Fatalf("new api key manager: %v", err)
	}
	return manager
}

func createTestAPIKey(t *testing.T, manager *apikey.Manager, name string, isAdmin bool, allowedEndpoints []string) string {
	t.Helper()

	resp, err := manager.Create(&apikey.CreateAPIKeyRequest{
		Name:             name,
		IsAdmin:          isAdmin,
		AllowedEndpoints: allowedEndpoints,
	})
	if err != nil {
		t.Fatalf("create api key %s: %v", name, err)
	}
	return resp.Key
}

func TestWebAuthMiddleware_APIRequiresKey(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:  "secret-key",
		EnableWebUI:     true,
		HealthCheckPath: "/health",
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("missing key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("wrong key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", "wrong")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("correct key allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", envCfg.ProxyAccessKey)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestWebAuthMiddleware_SPAPassesThrough(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:  "secret-key",
		EnableWebUI:     true,
		HealthCheckPath: "/health",
	}
	router := setupRouterWithAuth(envCfg)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWebAuthMiddleware_MessagesCurrentChannelPermission(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:  "secret-bootstrap-key",
		EnableWebUI:     true,
		HealthCheckPath: "/health",
	}
	apiKeyManager := newTestAPIKeyManager(t)
	router := setupRouterWithAuthAndAPIKeyManager(envCfg, apiKeyManager)

	nonAdminNoPerm := createTestAPIKey(t, apiKeyManager, "no-perm", false, nil)
	nonAdminMessagesOnly := createTestAPIKey(t, apiKeyManager, "messages-only", false, []string{"messages"})
	nonAdminCurrentOnly := createTestAPIKey(t, apiKeyManager, "messages-current-only", false, []string{EndpointPermissionMessagesChannelCurrent})
	adminNoPerm := createTestAPIKey(t, apiKeyManager, "admin", true, nil)

	t.Run("non-admin key without explicit permission denied", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
		req.Header.Set("x-api-key", nonAdminNoPerm)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("non-admin key with messages permission only denied", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
		req.Header.Set("x-api-key", nonAdminMessagesOnly)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("non-admin key with messages_current_channel permission allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
		req.Header.Set("x-api-key", nonAdminCurrentOnly)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("admin key allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/channels/current", nil)
		req.Header.Set("x-api-key", adminNoPerm)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("messages_current_channel key cannot access other admin API", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", nonAdminCurrentOnly)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}
