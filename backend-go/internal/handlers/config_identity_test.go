package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
