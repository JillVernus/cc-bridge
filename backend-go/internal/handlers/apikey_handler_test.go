package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

func newAPIKeyHandlerTestManager(t *testing.T) *apikey.Manager {
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

func TestAPIKeyHandlerRotateKeyReturnsNewSecretAndInvalidatesOldSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := newAPIKeyHandlerTestManager(t)
	created, err := manager.Create(&apikey.CreateAPIKeyRequest{
		Name:             "client-key",
		AllowedEndpoints: []string{"messages"},
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	router := gin.New()
	handler := NewAPIKeyHandler(manager)
	router.POST("/api/keys/:id/rotate", handler.RotateKey)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/keys/%d/rotate", created.ID), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response apikey.CreateAPIKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != created.ID {
		t.Fatalf("expected same key id %d, got %d", created.ID, response.ID)
	}
	if response.Key == "" || response.Key == created.Key {
		t.Fatalf("expected a new one-time key in response")
	}
	if manager.Validate(created.Key) != nil {
		t.Fatal("expected old key to be invalidated immediately")
	}
	if manager.Validate(response.Key) == nil {
		t.Fatal("expected rotated key to validate")
	}
}
