package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

func TestFetchChatUpstreamModels_UsesChatChannelPool(t *testing.T) {
	gin.SetMode(gin.TestMode)

	messagesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"messages-model","object":"model","owned_by":"messages"}]}`)
	}))
	defer messagesServer.Close()

	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"chat-model","object":"model","owned_by":"chat"}]}`)
	}))
	defer chatServer.Close()

	cfgManager := createTestConfigManager(t, config.Config{
		Upstream: []config.UpstreamConfig{
			{
				ID:          "messages-0",
				Name:        "Messages 0",
				BaseURL:     messagesServer.URL,
				ServiceType: "openai",
				APIKeys:     []string{"messages-key"},
			},
		},
		ChatUpstream: []config.UpstreamConfig{
			{
				ID:          "chat-0",
				Name:        "Chat 0",
				BaseURL:     chatServer.URL,
				ServiceType: "openai",
				APIKeys:     []string{"chat-key"},
			},
		},
	})

	r := gin.New()
	r.GET("/api/chat/channels/:id/models", FetchChatUpstreamModels(cfgManager))

	req := httptest.NewRequest(http.MethodGet, "/api/chat/channels/0/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp UpstreamModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d body=%s", len(resp.Models), w.Body.String())
	}
	if resp.Models[0].ID != "chat-model" {
		t.Fatalf("expected chat model from chat pool, got %q", resp.Models[0].ID)
	}
	if resp.Models[0].OwnedBy != "chat" {
		t.Fatalf("expected chat-owned model, got %q", resp.Models[0].OwnedBy)
	}
}
