package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

func newIdentityTestConfigManager(t *testing.T) *config.ConfigManager {
	t.Helper()

	cfgManager, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewConfigManager() failed: %v", err)
	}
	t.Cleanup(func() { _ = cfgManager.Close() })
	return cfgManager
}

func performIdentityJSONRequest(t *testing.T, router *gin.Engine, method string, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeIdentityResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body %q: %v", w.Body.String(), err)
	}
	return payload
}

func TestAddUpstreamIdentityReturnsStableIDAndCurrentIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newIdentityTestConfigManager(t)
	router := gin.New()
	router.POST("/api/channels", AddUpstream(cfgManager))

	w := performIdentityJSONRequest(t, router, http.MethodPost, "/api/channels", map[string]any{
		"name":        "identity-created",
		"baseUrl":     "https://identity-created.example.com",
		"serviceType": "openai",
		"apiKeys":     []string{"identity-created-key"},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	payload := decodeIdentityResponse(t, w)
	id, ok := payload["id"].(string)
	if !ok || id == "" {
		t.Fatalf("id = %#v, want non-empty stable channel id", payload["id"])
	}
	if got := int(payload["index"].(float64)); got != 0 {
		t.Fatalf("index = %d, want 0", got)
	}

	cfg := cfgManager.GetConfig()
	if len(cfg.Upstream) != 1 {
		t.Fatalf("upstream count = %d, want 1", len(cfg.Upstream))
	}
	if cfg.Upstream[0].ID != id {
		t.Fatalf("response id = %q, persisted id = %q", id, cfg.Upstream[0].ID)
	}
}

func TestAddChannelRejectsDuplicateStableIDAndDoesNotReturnExistingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		path            string
		seed            func(*config.ConfigManager)
		register        func(*gin.Engine, *config.ConfigManager)
		body            map[string]any
		assertUnchanged func(*testing.T, config.Config)
	}{
		{
			name: "messages",
			path: "/api/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddUpstream(config.UpstreamConfig{ID: "duplicate-id", Name: "Existing Messages", BaseURL: "https://existing.example.com", ServiceType: "openai", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/channels", AddUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "duplicate-id",
				"name":        "New Messages",
				"baseUrl":     "https://new.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"new-key"},
			},
			assertUnchanged: func(t *testing.T, cfg config.Config) {
				if len(cfg.Upstream) != 1 || cfg.Upstream[0].Name != "Existing Messages" {
					t.Fatalf("messages pool changed unexpectedly: %#v", cfg.Upstream)
				}
			},
		},
		{
			name: "responses",
			path: "/api/responses/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{ID: "duplicate-id", Name: "Existing Responses", BaseURL: "https://existing.example.com", ServiceType: "responses", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddResponsesUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/responses/channels", AddResponsesUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "duplicate-id",
				"name":        "New Responses",
				"baseUrl":     "https://new.example.com",
				"serviceType": "responses",
				"apiKeys":     []string{"new-key"},
			},
			assertUnchanged: func(t *testing.T, cfg config.Config) {
				if len(cfg.ResponsesUpstream) != 1 || cfg.ResponsesUpstream[0].Name != "Existing Responses" {
					t.Fatalf("responses pool changed unexpectedly: %#v", cfg.ResponsesUpstream)
				}
			},
		},
		{
			name: "gemini",
			path: "/api/gemini/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "duplicate-id", Name: "Existing Gemini", BaseURL: "https://existing.example.com", ServiceType: "gemini", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddGeminiUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/gemini/channels", AddGeminiUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "duplicate-id",
				"name":        "New Gemini",
				"baseUrl":     "https://new.example.com",
				"serviceType": "gemini",
				"apiKeys":     []string{"new-key"},
			},
			assertUnchanged: func(t *testing.T, cfg config.Config) {
				if len(cfg.GeminiUpstream) != 1 || cfg.GeminiUpstream[0].Name != "Existing Gemini" {
					t.Fatalf("gemini pool changed unexpectedly: %#v", cfg.GeminiUpstream)
				}
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "duplicate-id", Name: "Existing Chat", BaseURL: "https://existing.example.com", ServiceType: "openai", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddChatUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/chat/channels", AddChatUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "duplicate-id",
				"name":        "New Chat",
				"baseUrl":     "https://new.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"new-key"},
			},
			assertUnchanged: func(t *testing.T, cfg config.Config) {
				if len(cfg.ChatUpstream) != 1 || cfg.ChatUpstream[0].Name != "Existing Chat" {
					t.Fatalf("chat pool changed unexpectedly: %#v", cfg.ChatUpstream)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			tt.seed(cfgManager)

			router := gin.New()
			tt.register(router, cfgManager)

			w := performIdentityJSONRequest(t, router, http.MethodPost, tt.path, tt.body, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 Bad Request, body=%s", w.Code, w.Body.String())
			}
			payload := decodeIdentityResponse(t, w)
			if _, ok := payload["id"]; ok {
				t.Fatalf("duplicate-id response must not return existing id identity: %s", w.Body.String())
			}
			if _, ok := payload["index"]; ok {
				t.Fatalf("duplicate-id response must not return existing index identity: %s", w.Body.String())
			}
			errText, _ := payload["error"].(string)
			if !strings.Contains(strings.ToLower(errText), "duplicate channel id") {
				t.Fatalf("error = %q, want duplicate channel ID error", errText)
			}
			tt.assertUnchanged(t, cfgManager.GetConfig())
		})
	}
}

func TestAddChannelRejectsCrossPoolDuplicateStableID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		seed        func(*config.ConfigManager)
		register    func(*gin.Engine, *config.ConfigManager)
		body        map[string]any
		wantPoolLen func(config.Config) int
	}{
		{
			name: "messages rejects responses id",
			path: "/api/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddResponsesUpstream(config.UpstreamConfig{ID: "shared-cross-pool", Name: "Existing Responses", BaseURL: "https://existing-responses.example.com", ServiceType: "responses", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddResponsesUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/channels", AddUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "shared-cross-pool",
				"name":        "New Messages",
				"baseUrl":     "https://new-messages.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"new-key"},
			},
			wantPoolLen: func(cfg config.Config) int { return len(cfg.Upstream) },
		},
		{
			name: "responses rejects messages id",
			path: "/api/responses/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddUpstream(config.UpstreamConfig{ID: "shared-cross-pool", Name: "Existing Messages", BaseURL: "https://existing-messages.example.com", ServiceType: "openai", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/responses/channels", AddResponsesUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "shared-cross-pool",
				"name":        "New Responses",
				"baseUrl":     "https://new-responses.example.com",
				"serviceType": "responses",
				"apiKeys":     []string{"new-key"},
			},
			wantPoolLen: func(cfg config.Config) int { return len(cfg.ResponsesUpstream) },
		},
		{
			name: "gemini rejects chat id",
			path: "/api/gemini/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "shared-cross-pool", Name: "Existing Chat", BaseURL: "https://existing-chat.example.com", ServiceType: "openai", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddChatUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/gemini/channels", AddGeminiUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "shared-cross-pool",
				"name":        "New Gemini",
				"baseUrl":     "https://new-gemini.example.com",
				"serviceType": "gemini",
				"apiKeys":     []string{"new-key"},
			},
			wantPoolLen: func(cfg config.Config) int { return len(cfg.GeminiUpstream) },
		},
		{
			name: "chat rejects gemini id",
			path: "/api/chat/channels",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "shared-cross-pool", Name: "Existing Gemini", BaseURL: "https://existing-gemini.example.com", ServiceType: "gemini", APIKeys: []string{"existing-key"}}); err != nil {
					t.Fatalf("seed AddGeminiUpstream failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/chat/channels", AddChatUpstream(cfgManager))
			},
			body: map[string]any{
				"id":          "shared-cross-pool",
				"name":        "New Chat",
				"baseUrl":     "https://new-chat.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"new-key"},
			},
			wantPoolLen: func(cfg config.Config) int { return len(cfg.ChatUpstream) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			tt.seed(cfgManager)

			router := gin.New()
			tt.register(router, cfgManager)

			w := performIdentityJSONRequest(t, router, http.MethodPost, tt.path, tt.body, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 Bad Request, body=%s", w.Code, w.Body.String())
			}
			payload := decodeIdentityResponse(t, w)
			errText, _ := payload["error"].(string)
			if !strings.Contains(strings.ToLower(errText), "duplicate channel id") {
				t.Fatalf("error = %q, want duplicate channel ID error", errText)
			}
			if got := tt.wantPoolLen(cfgManager.GetConfig()); got != 0 {
				t.Fatalf("target pool count = %d, want 0", got)
			}
		})
	}
}

func TestUpdateUpstreamStableIdentityWinsOverConflictingIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newIdentityTestConfigManager(t)
	if err := cfgManager.AddUpstream(config.UpstreamConfig{
		ID:          "0",
		Name:        "numeric-stable-id",
		BaseURL:     "https://stable.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"stable-key"},
	}); err != nil {
		t.Fatalf("AddUpstream stable channel failed: %v", err)
	}
	if err := cfgManager.AddUpstream(config.UpstreamConfig{
		ID:          "1",
		Name:        "index-conflict",
		BaseURL:     "https://index.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"index-key"},
	}); err != nil {
		t.Fatalf("AddUpstream index channel failed: %v", err)
	}

	router := gin.New()
	router.PUT("/api/channels/:id", UpdateUpstream(cfgManager, nil))

	w := performIdentityJSONRequest(t, router, http.MethodPut, "/api/channels/1?channelId=0", map[string]any{
		"name": "updated-by-stable-id",
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	payload := decodeIdentityResponse(t, w)
	if got := payload["id"]; got != "0" {
		t.Fatalf("id = %#v, want stable id %q", got, "0")
	}
	if got := int(payload["index"].(float64)); got != 0 {
		t.Fatalf("index = %d, want stable channel index 0", got)
	}

	cfg := cfgManager.GetConfig()
	if cfg.Upstream[0].Name != "updated-by-stable-id" {
		t.Fatalf("stable channel name = %q, want updated-by-stable-id", cfg.Upstream[0].Name)
	}
	if cfg.Upstream[1].Name != "index-conflict" {
		t.Fatalf("conflicting index channel name = %q, want index-conflict", cfg.Upstream[1].Name)
	}
}

func TestFetchUpstreamModelsStableIdentityWinsOverConflictingIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("upstream path = %q, want /v1/models", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "stable-model", "object": "model", "owned_by": "test"},
			},
		})
	}))
	t.Cleanup(upstreamServer.Close)

	cfgManager := newIdentityTestConfigManager(t)
	if err := cfgManager.AddUpstream(config.UpstreamConfig{
		ID:          "0",
		Name:        "stable-model-source",
		BaseURL:     upstreamServer.URL,
		ServiceType: "openai",
		APIKeys:     []string{"stable-key"},
	}); err != nil {
		t.Fatalf("AddUpstream stable channel failed: %v", err)
	}
	if err := cfgManager.AddUpstream(config.UpstreamConfig{
		ID:          "1",
		Name:        "conflicting-index-source",
		BaseURL:     "https://unused.example.com",
		ServiceType: "unsupported",
		APIKeys:     []string{"unused-key"},
	}); err != nil {
		t.Fatalf("AddUpstream index channel failed: %v", err)
	}

	router := gin.New()
	router.GET("/api/channels/:id/models", FetchUpstreamModels(cfgManager))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/channels/1/models?channelId=0", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
	}

	var payload UpstreamModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body %q: %v", w.Body.String(), err)
	}
	if !payload.Success {
		t.Fatalf("success = false, want true, error=%s", payload.Error)
	}
	if len(payload.Models) != 1 || payload.Models[0].ID != "stable-model" {
		t.Fatalf("models = %#v, want stable-model from stable channel", payload.Models)
	}
}

func TestStableChannelMutationIfMatchMismatchReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgManager := newIdentityTestConfigManager(t)
	router := gin.New()
	router.GET("/api/channels", GetUpstreams(cfgManager))
	router.POST("/api/channels", AddUpstream(cfgManager))

	read := httptest.NewRecorder()
	router.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/channels", nil))
	if read.Code != http.StatusOK {
		t.Fatalf("read status = %d, want 200 OK, body=%s", read.Code, read.Body.String())
	}
	if etag := read.Header().Get("ETag"); etag == "" {
		t.Fatalf("GET /api/channels ETag is empty")
	}

	w := performIdentityJSONRequest(t, router, http.MethodPost, "/api/channels", map[string]any{
		"name":        "stale-if-match",
		"baseUrl":     "https://stale-if-match.example.com",
		"serviceType": "openai",
		"apiKeys":     []string{"stale-key"},
	}, map[string]string{"If-Match": `"999"`})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 Conflict, body=%s", w.Code, w.Body.String())
	}
	if got := len(cfgManager.GetConfig().Upstream); got != 0 {
		t.Fatalf("upstream count after stale If-Match = %d, want 0", got)
	}
}

func TestGeminiAndChatChannelReadsReturnETag(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		register func(*gin.Engine, *config.ConfigManager)
	}{
		{
			name: "gemini",
			path: "/api/gemini/channels",
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.GET("/api/gemini/channels", GetGeminiUpstreams(cfgManager))
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels",
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.GET("/api/chat/channels", GetChatUpstreams(cfgManager))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			router := gin.New()
			tt.register(router, cfgManager)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
			}
			if etag := w.Header().Get("ETag"); etag == "" {
				t.Fatalf("%s ETag is empty", tt.path)
			}
		})
	}
}

func TestGeminiAndChatAddChannelIdentityReturnsStableIDAndCurrentIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		path      string
		poolCount func(config.Config) int
		poolID    func(config.Config) string
		register  func(*gin.Engine, *config.ConfigManager)
		body      map[string]any
	}{
		{
			name: "gemini",
			path: "/api/gemini/channels",
			poolCount: func(cfg config.Config) int {
				return len(cfg.GeminiUpstream)
			},
			poolID: func(cfg config.Config) string {
				return cfg.GeminiUpstream[0].ID
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/gemini/channels", AddGeminiUpstream(cfgManager))
			},
			body: map[string]any{
				"name":        "gemini-identity-created",
				"baseUrl":     "https://gemini-identity-created.example.com",
				"serviceType": "gemini",
				"apiKeys":     []string{"gemini-created-key"},
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels",
			poolCount: func(cfg config.Config) int {
				return len(cfg.ChatUpstream)
			},
			poolID: func(cfg config.Config) string {
				return cfg.ChatUpstream[0].ID
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/chat/channels", AddChatUpstream(cfgManager))
			},
			body: map[string]any{
				"name":        "chat-identity-created",
				"baseUrl":     "https://chat-identity-created.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"chat-created-key"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			router := gin.New()
			tt.register(router, cfgManager)

			w := performIdentityJSONRequest(t, router, http.MethodPost, tt.path, tt.body, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
			}

			payload := decodeIdentityResponse(t, w)
			id, ok := payload["id"].(string)
			if !ok || id == "" {
				t.Fatalf("id = %#v, want non-empty stable channel id", payload["id"])
			}
			if got := int(payload["index"].(float64)); got != 0 {
				t.Fatalf("index = %d, want 0", got)
			}
			cfg := cfgManager.GetConfig()
			if got := tt.poolCount(cfg); got != 1 {
				t.Fatalf("channel count = %d, want 1", got)
			}
			if got := tt.poolID(cfg); got != id {
				t.Fatalf("response id = %q, persisted id = %q", id, got)
			}
		})
	}
}

func TestGeminiAndChatUpdateStableIdentityWinsOverConflictingIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		path       string
		seed       func(*config.ConfigManager)
		register   func(*gin.Engine, *config.ConfigManager)
		assertPool func(*testing.T, config.Config)
	}{
		{
			name: "gemini",
			path: "/api/gemini/channels/1?channelId=0",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "0", Name: "gemini-stable", BaseURL: "https://stable.example.com", ServiceType: "gemini", APIKeys: []string{"stable-key"}}); err != nil {
					t.Fatalf("AddGeminiUpstream stable failed: %v", err)
				}
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "1", Name: "gemini-index", BaseURL: "https://index.example.com", ServiceType: "gemini", APIKeys: []string{"index-key"}}); err != nil {
					t.Fatalf("AddGeminiUpstream index failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.PUT("/api/gemini/channels/:id", UpdateGeminiUpstream(cfgManager, nil))
			},
			assertPool: func(t *testing.T, cfg config.Config) {
				if cfg.GeminiUpstream[0].Name != "updated-by-stable-id" {
					t.Fatalf("stable gemini name = %q, want updated-by-stable-id", cfg.GeminiUpstream[0].Name)
				}
				if cfg.GeminiUpstream[1].Name != "gemini-index" {
					t.Fatalf("conflicting gemini name = %q, want gemini-index", cfg.GeminiUpstream[1].Name)
				}
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels/1?channelId=0",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "0", Name: "chat-stable", BaseURL: "https://stable.example.com", ServiceType: "openai", APIKeys: []string{"stable-key"}}); err != nil {
					t.Fatalf("AddChatUpstream stable failed: %v", err)
				}
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "1", Name: "chat-index", BaseURL: "https://index.example.com", ServiceType: "openai", APIKeys: []string{"index-key"}}); err != nil {
					t.Fatalf("AddChatUpstream index failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.PUT("/api/chat/channels/:id", UpdateChatUpstream(cfgManager, nil))
			},
			assertPool: func(t *testing.T, cfg config.Config) {
				if cfg.ChatUpstream[0].Name != "updated-by-stable-id" {
					t.Fatalf("stable chat name = %q, want updated-by-stable-id", cfg.ChatUpstream[0].Name)
				}
				if cfg.ChatUpstream[1].Name != "chat-index" {
					t.Fatalf("conflicting chat name = %q, want chat-index", cfg.ChatUpstream[1].Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			tt.seed(cfgManager)
			router := gin.New()
			tt.register(router, cfgManager)

			w := performIdentityJSONRequest(t, router, http.MethodPut, tt.path, map[string]any{
				"name": "updated-by-stable-id",
			}, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
			}
			payload := decodeIdentityResponse(t, w)
			if got := payload["id"]; got != "0" {
				t.Fatalf("id = %#v, want stable id %q", got, "0")
			}
			if got := int(payload["index"].(float64)); got != 0 {
				t.Fatalf("index = %d, want stable channel index 0", got)
			}
			tt.assertPool(t, cfgManager.GetConfig())
		})
	}
}

func TestGeminiAndChatFetchModelsStableIdentityWinsOverConflictingIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("upstream path = %q, want /v1/models", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "stable-model", "object": "model", "owned_by": "test"},
			},
		})
	}))
	t.Cleanup(upstreamServer.Close)

	tests := []struct {
		name     string
		path     string
		seed     func(*config.ConfigManager)
		register func(*gin.Engine, *config.ConfigManager)
	}{
		{
			name: "gemini",
			path: "/api/gemini/channels/1/models?channelId=0",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "0", Name: "gemini-stable-models", BaseURL: upstreamServer.URL, ServiceType: "openai", APIKeys: []string{"stable-key"}}); err != nil {
					t.Fatalf("AddGeminiUpstream stable failed: %v", err)
				}
				if err := cfgManager.AddGeminiUpstream(config.UpstreamConfig{ID: "1", Name: "gemini-conflict-models", BaseURL: "https://unused.example.com", ServiceType: "unsupported", APIKeys: []string{"unused-key"}}); err != nil {
					t.Fatalf("AddGeminiUpstream conflict failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.GET("/api/gemini/channels/:id/models", FetchGeminiUpstreamModels(cfgManager))
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels/1/models?channelId=0",
			seed: func(cfgManager *config.ConfigManager) {
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "0", Name: "chat-stable-models", BaseURL: upstreamServer.URL, ServiceType: "openai", APIKeys: []string{"stable-key"}}); err != nil {
					t.Fatalf("AddChatUpstream stable failed: %v", err)
				}
				if err := cfgManager.AddChatUpstream(config.UpstreamConfig{ID: "1", Name: "chat-conflict-models", BaseURL: "https://unused.example.com", ServiceType: "unsupported", APIKeys: []string{"unused-key"}}); err != nil {
					t.Fatalf("AddChatUpstream conflict failed: %v", err)
				}
			},
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.GET("/api/chat/channels/:id/models", FetchChatUpstreamModels(cfgManager))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			tt.seed(cfgManager)
			router := gin.New()
			tt.register(router, cfgManager)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 OK, body=%s", w.Code, w.Body.String())
			}
			var payload UpstreamModelsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response body %q: %v", w.Body.String(), err)
			}
			if !payload.Success {
				t.Fatalf("success = false, want true, error=%s", payload.Error)
			}
			if len(payload.Models) != 1 || payload.Models[0].ID != "stable-model" {
				t.Fatalf("models = %#v, want stable-model from stable channel", payload.Models)
			}
		})
	}
}

func TestGeminiAndChatIfMatchMismatchReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		path      string
		register  func(*gin.Engine, *config.ConfigManager)
		poolCount func(config.Config) int
		body      map[string]any
	}{
		{
			name: "gemini",
			path: "/api/gemini/channels",
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/gemini/channels", AddGeminiUpstream(cfgManager))
			},
			poolCount: func(cfg config.Config) int {
				return len(cfg.GeminiUpstream)
			},
			body: map[string]any{
				"name":        "gemini-stale-if-match",
				"baseUrl":     "https://gemini-stale-if-match.example.com",
				"serviceType": "gemini",
				"apiKeys":     []string{"gemini-stale-key"},
			},
		},
		{
			name: "chat",
			path: "/api/chat/channels",
			register: func(router *gin.Engine, cfgManager *config.ConfigManager) {
				router.POST("/api/chat/channels", AddChatUpstream(cfgManager))
			},
			poolCount: func(cfg config.Config) int {
				return len(cfg.ChatUpstream)
			},
			body: map[string]any{
				"name":        "chat-stale-if-match",
				"baseUrl":     "https://chat-stale-if-match.example.com",
				"serviceType": "openai",
				"apiKeys":     []string{"chat-stale-key"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgManager := newIdentityTestConfigManager(t)
			router := gin.New()
			tt.register(router, cfgManager)

			w := performIdentityJSONRequest(t, router, http.MethodPost, tt.path, tt.body, map[string]string{"If-Match": `"999"`})
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 Conflict, body=%s", w.Code, w.Body.String())
			}
			if got := tt.poolCount(cfgManager.GetConfig()); got != 0 {
				t.Fatalf("channel count after stale If-Match = %d, want 0", got)
			}
		})
	}
}

type addUpstreamWithExpectedRevision interface {
	AddUpstreamWithExpectedRevision(config.UpstreamConfig, int64) error
}

func TestIfMatchExpectedRevisionIsEnforcedInsideConfigManagerMutation(t *testing.T) {
	cfgManager := newIdentityTestConfigManager(t)
	mutator, ok := any(cfgManager).(addUpstreamWithExpectedRevision)
	if !ok {
		t.Fatalf("ConfigManager does not expose expected-revision channel mutation API")
	}

	_, revision := cfgManager.GetConfigWithRevision()
	if !ifMatchRevisionMatches(configRevisionETag(revision), revision) {
		t.Fatalf("test setup expected ETag %s to match revision %d", configRevisionETag(revision), revision)
	}

	if err := cfgManager.AddUpstream(config.UpstreamConfig{
		Name:        "intervening-write",
		BaseURL:     "https://intervening-write.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"intervening-key"},
	}); err != nil {
		t.Fatalf("intervening AddUpstream failed: %v", err)
	}

	err := mutator.AddUpstreamWithExpectedRevision(config.UpstreamConfig{
		Name:        "stale-expected-revision",
		BaseURL:     "https://stale-expected-revision.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"stale-key"},
	}, revision)
	if !errors.Is(err, config.ErrStaleConfigWrite) {
		t.Fatalf("err = %v, want ErrStaleConfigWrite", err)
	}

	cfg := cfgManager.GetConfig()
	if len(cfg.Upstream) != 1 {
		t.Fatalf("upstream count = %d, want only intervening write", len(cfg.Upstream))
	}
	if cfg.Upstream[0].Name != "intervening-write" {
		t.Fatalf("remaining channel = %q, want intervening-write", cfg.Upstream[0].Name)
	}
}
