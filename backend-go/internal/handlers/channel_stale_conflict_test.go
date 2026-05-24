package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/database"
	"github.com/gin-gonic/gin"
)

func staleConflictSeedConfig() config.Config {
	return config.Config{
		Upstream:             []config.UpstreamConfig{staleConflictMessagesUpstream("messages-initial")},
		LoadBalance:          "failover",
		ResponsesUpstream:    []config.UpstreamConfig{staleConflictResponsesUpstream("responses-initial")},
		ResponsesLoadBalance: "failover",
		GeminiLoadBalance:    "failover",
		ChatLoadBalance:      "failover",
		UserAgent:            config.GetDefaultUserAgentConfig(),
		GeminiUpstream:       []config.UpstreamConfig{staleConflictGeminiUpstream("gemini-initial")},
		ChatUpstream:         []config.UpstreamConfig{staleConflictChatUpstream("chat-initial")},
	}
}

func staleConflictMessagesUpstream(name string) config.UpstreamConfig {
	return config.UpstreamConfig{
		Name:        name,
		BaseURL:     "https://messages.example.com",
		ServiceType: "openai",
		APIKeys:     []string{name + "-key"},
		Status:      "active",
	}
}

func staleConflictResponsesUpstream(name string) config.UpstreamConfig {
	return config.UpstreamConfig{
		Name:        name,
		BaseURL:     "https://responses.example.com",
		ServiceType: "responses",
		APIKeys:     []string{name + "-key"},
		Status:      "active",
	}
}

func staleConflictGeminiUpstream(name string) config.UpstreamConfig {
	return config.UpstreamConfig{
		Name:        name,
		BaseURL:     "https://gemini.example.com",
		ServiceType: "gemini",
		APIKeys:     []string{name + "-key"},
		Status:      "active",
	}
}

func staleConflictChatUpstream(name string) config.UpstreamConfig {
	return config.UpstreamConfig{
		Name:        name,
		BaseURL:     "https://chat.example.com",
		ServiceType: "openai",
		APIKeys:     []string{name + "-key"},
		Status:      "active",
	}
}

func newStaleConflictManagers(t *testing.T, seed config.Config) (*config.ConfigManager, *config.ConfigManager, database.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "handler-stale-conflict.db")
	db, err := database.New(database.Config{
		Type: database.DialectSQLite,
		URL:  dbPath,
	})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := database.RunMigrations(db); err != nil {
		_ = db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	seedStorage := config.NewDBConfigStorage(db, time.Second)
	if err := seedStorage.SaveConfigToDB(&seed); err != nil {
		_ = db.Close()
		t.Fatalf("seed SaveConfigToDB() failed: %v", err)
	}

	return newDBBackedStaleConflictManager(t, db), newDBBackedStaleConflictManager(t, db), db
}

func newDBBackedStaleConflictManager(t *testing.T, db database.DB) *config.ConfigManager {
	t.Helper()

	cfgManager, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })

	storage := config.NewDBConfigStorage(db, time.Second)
	storage.SetConfigManager(cfgManager)
	cfgManager.SetDBStorage(storage)

	return cfgManager
}

func performJSONRequest(t *testing.T, router *gin.Engine, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestStalePromotionHandlersReturnConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		remoteWrite func(*config.ConfigManager) error
		register    func(*gin.Engine, *config.ConfigManager)
		path        string
	}{
		{
			name: "messages",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddUpstream(staleConflictMessagesUpstream("messages-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.PUT("/api/channels/:id/promotion", SetChannelPromotion(cm))
			},
			path: "/api/channels/0/promotion",
		},
		{
			name: "responses",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddResponsesUpstream(staleConflictResponsesUpstream("responses-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.PUT("/api/responses/channels/:id/promotion", SetResponsesChannelPromotion(cm))
			},
			path: "/api/responses/channels/0/promotion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
			defer db.Close()

			if err := tt.remoteWrite(managerA); err != nil {
				t.Fatalf("remote write failed: %v", err)
			}

			router := gin.New()
			tt.register(router, managerB)

			w := performJSONRequest(t, router, http.MethodPut, tt.path, map[string]any{"duration": 60})
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 Conflict, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGeminiStaleChannelMutationHandlersReturnConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		remoteWrite func(*config.ConfigManager) error
		register    func(*gin.Engine, *config.ConfigManager)
		method      string
		path        string
		body        any
	}{
		{
			name: "add",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddGeminiUpstream(staleConflictGeminiUpstream("gemini-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.POST("/api/gemini/channels", AddGeminiUpstream(cm))
			},
			method: http.MethodPost,
			path:   "/api/gemini/channels",
			body: map[string]any{
				"name":        "gemini-stale",
				"baseUrl":     "https://gemini-stale.example.com",
				"serviceType": "gemini",
				"apiKeys":     []string{"gemini-stale-key"},
			},
		},
		{
			name: "update",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddGeminiUpstream(staleConflictGeminiUpstream("gemini-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.PUT("/api/gemini/channels/:id", UpdateGeminiUpstream(cm, nil))
			},
			method: http.MethodPut,
			path:   "/api/gemini/channels/0",
			body:   map[string]any{"name": "gemini-stale-update"},
		},
		{
			name: "delete",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddGeminiUpstream(staleConflictGeminiUpstream("gemini-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.DELETE("/api/gemini/channels/:id", DeleteGeminiUpstream(cm, nil))
			},
			method: http.MethodDelete,
			path:   "/api/gemini/channels/0",
			body:   map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
			defer db.Close()

			if err := tt.remoteWrite(managerA); err != nil {
				t.Fatalf("remote write failed: %v", err)
			}

			router := gin.New()
			tt.register(router, managerB)

			w := performJSONRequest(t, router, tt.method, tt.path, tt.body)
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 Conflict, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestChatStaleChannelMutationHandlersReturnConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		remoteWrite func(*config.ConfigManager) error
		register    func(*gin.Engine, *config.ConfigManager)
		method      string
		path        string
		body        any
	}{
		{
			name: "add",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddChatUpstream(staleConflictChatUpstream("chat-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.POST("/api/chat/channels", AddChatUpstream(cm))
			},
			method: http.MethodPost,
			path:   "/api/chat/channels",
			body: map[string]any{
				"name":        "chat-stale",
				"baseUrl":     "https://chat-stale.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"chat-stale-key"},
			},
		},
		{
			name: "update",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddChatUpstream(staleConflictChatUpstream("chat-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.PUT("/api/chat/channels/:id", UpdateChatUpstream(cm, nil))
			},
			method: http.MethodPut,
			path:   "/api/chat/channels/0",
			body:   map[string]any{"name": "chat-stale-update"},
		},
		{
			name: "delete",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddChatUpstream(staleConflictChatUpstream("chat-remote"))
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.DELETE("/api/chat/channels/:id", DeleteChatUpstream(cm, nil))
			},
			method: http.MethodDelete,
			path:   "/api/chat/channels/0",
			body:   map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
			defer db.Close()

			if err := tt.remoteWrite(managerA); err != nil {
				t.Fatalf("remote write failed: %v", err)
			}

			router := gin.New()
			tt.register(router, managerB)

			w := performJSONRequest(t, router, tt.method, tt.path, tt.body)
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 Conflict, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestStableChannelReadThroughRefreshesStaleManagerForModelFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"fresh-model","object":"model","owned_by":"test"}]}`)
	}))
	t.Cleanup(upstreamServer.Close)

	managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
	defer db.Close()

	if err := managerA.AddUpstream(config.UpstreamConfig{
		ID:          "fresh-stable-id",
		Name:        "fresh-stable",
		BaseURL:     upstreamServer.URL,
		ServiceType: "openai",
		APIKeys:     []string{"fresh-key"},
		Status:      "active",
	}); err != nil {
		t.Fatalf("remote add failed: %v", err)
	}

	router := gin.New()
	router.GET("/api/channels/:id/models", FetchUpstreamModels(managerB))

	req := httptest.NewRequest(http.MethodGet, "/api/channels/0/models?channelId=fresh-stable-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	var payload UpstreamModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("success = false, want true, error=%s", payload.Error)
	}
	if len(payload.Models) != 1 || payload.Models[0].ID != "fresh-model" {
		t.Fatalf("models = %#v, want fresh-model", payload.Models)
	}
}

func TestStableChannelModelFetchRefreshesKnownStaleChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	staleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"stale-model","object":"model","owned_by":"test"}]}`)
	}))
	t.Cleanup(staleServer.Close)

	freshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"fresh-model","object":"model","owned_by":"test"}]}`)
	}))
	t.Cleanup(freshServer.Close)

	seed := staleConflictSeedConfig()
	seed.Upstream[0].ID = "known-stale-id"
	seed.Upstream[0].BaseURL = staleServer.URL
	seed.Upstream[0].ServiceType = "openai"
	managerA, managerB, db := newStaleConflictManagers(t, seed)
	defer db.Close()

	if _, err := managerA.UpdateUpstream(0, config.UpstreamUpdate{
		BaseURL: &freshServer.URL,
		APIKeys: []string{"fresh-key"},
	}); err != nil {
		t.Fatalf("remote update failed: %v", err)
	}

	router := gin.New()
	router.GET("/api/channels/:id/models", FetchUpstreamModels(managerB))

	req := httptest.NewRequest(http.MethodGet, "/api/channels/0/models?channelId=known-stale-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	var payload UpstreamModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("success = false, want true, error=%s", payload.Error)
	}
	if len(payload.Models) != 1 || payload.Models[0].ID != "fresh-model" {
		t.Fatalf("models = %#v, want fresh-model from refreshed channel", payload.Models)
	}
}

func TestStableChannelMutationRefreshesWhenIfMatchAheadOfLocalRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)

	managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
	defer db.Close()

	if err := managerA.AddUpstream(config.UpstreamConfig{
		ID:          "fresh-edit-id",
		Name:        "fresh-edit",
		BaseURL:     "https://fresh-edit.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"fresh-edit-key"},
		Status:      "active",
	}); err != nil {
		t.Fatalf("remote add failed: %v", err)
	}
	_, remoteRevision := managerA.GetConfigWithRevision()

	router := gin.New()
	router.PUT("/api/channels/:id", UpdateUpstream(managerB, nil))

	req := httptest.NewRequest(http.MethodPut, "/api/channels/0?channelId=fresh-edit-id", bytes.NewReader([]byte(`{"name":"fresh-edit-updated"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", strconv.Quote(strconv.FormatInt(remoteRevision, 10)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	cfg := managerB.GetConfig()
	index, ok := findChannelIndexByStableID(cfg.Upstream, "fresh-edit-id")
	if !ok {
		t.Fatalf("managerB did not refresh fresh-edit-id")
	}
	if cfg.Upstream[index].Name != "fresh-edit-updated" {
		t.Fatalf("channel name = %q, want fresh-edit-updated", cfg.Upstream[index].Name)
	}
}

func TestChannelListReadThroughRefreshesStaleManager(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		remoteWrite func(*config.ConfigManager) error
		register    func(*gin.Engine, *config.ConfigManager)
		path        string
		wantID      string
	}{
		{
			name: "messages",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddUpstream(config.UpstreamConfig{
					ID:          "fresh-list-messages",
					Name:        "fresh-list-messages",
					BaseURL:     "https://fresh-list-messages.example.com",
					ServiceType: "openai",
					APIKeys:     []string{"fresh-list-key"},
					Status:      "active",
				})
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.GET("/api/channels", GetUpstreams(cm))
			},
			path:   "/api/channels",
			wantID: "fresh-list-messages",
		},
		{
			name: "responses",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddResponsesUpstream(config.UpstreamConfig{
					ID:          "fresh-list-responses",
					Name:        "fresh-list-responses",
					BaseURL:     "https://fresh-list-responses.example.com",
					ServiceType: "responses",
					APIKeys:     []string{"fresh-list-key"},
					Status:      "active",
				})
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.GET("/api/responses/channels", GetResponsesUpstreams(cm))
			},
			path:   "/api/responses/channels",
			wantID: "fresh-list-responses",
		},
		{
			name: "gemini",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddGeminiUpstream(config.UpstreamConfig{
					ID:          "fresh-list-gemini",
					Name:        "fresh-list-gemini",
					BaseURL:     "https://fresh-list-gemini.example.com",
					ServiceType: "gemini",
					APIKeys:     []string{"fresh-list-key"},
					Status:      "active",
				})
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.GET("/api/gemini/channels", GetGeminiUpstreams(cm))
			},
			path:   "/api/gemini/channels",
			wantID: "fresh-list-gemini",
		},
		{
			name: "chat",
			remoteWrite: func(cm *config.ConfigManager) error {
				return cm.AddChatUpstream(config.UpstreamConfig{
					ID:          "fresh-list-chat",
					Name:        "fresh-list-chat",
					BaseURL:     "https://fresh-list-chat.example.com",
					ServiceType: "openai",
					APIKeys:     []string{"fresh-list-key"},
					Status:      "active",
				})
			},
			register: func(router *gin.Engine, cm *config.ConfigManager) {
				router.GET("/api/chat/channels", GetChatUpstreams(cm))
			},
			path:   "/api/chat/channels",
			wantID: "fresh-list-chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managerA, managerB, db := newStaleConflictManagers(t, staleConflictSeedConfig())
			defer db.Close()

			if err := tt.remoteWrite(managerA); err != nil {
				t.Fatalf("remote write failed: %v", err)
			}

			router := gin.New()
			tt.register(router, managerB)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
			}

			var payload struct {
				Channels []struct {
					ID string `json:"id"`
				} `json:"channels"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			for _, channel := range payload.Channels {
				if channel.ID == tt.wantID {
					return
				}
			}
			t.Fatalf("channels = %#v, want id %q", payload.Channels, tt.wantID)
		})
	}
}
