package apikey

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func newRotateTestManager(t *testing.T) *Manager {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	manager, err := NewManager(db)
	if err != nil {
		t.Fatalf("new api key manager: %v", err)
	}
	return manager
}

func TestManagerRotateReplacesSecretOnSameRecordAndInvalidatesOldKey(t *testing.T) {
	manager := newRotateTestManager(t)

	created, err := manager.Create(&CreateAPIKeyRequest{
		Name:                "client-key",
		Description:         "used by client",
		IsAdmin:             true,
		RateLimitRPM:        42,
		AllowedEndpoints:    []string{"messages", "responses"},
		AllowedChannelsMsg:  []string{"msg-primary"},
		AllowedChannelsResp: []string{"resp-primary"},
		AllowedModels:       []string{"gpt-*"},
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	oldKey := created.Key

	rotated, err := manager.Rotate(created.ID)
	if err != nil {
		t.Fatalf("rotate key: %v", err)
	}

	if rotated.ID != created.ID {
		t.Fatalf("expected same id %d, got %d", created.ID, rotated.ID)
	}
	if rotated.Key == "" {
		t.Fatal("expected rotated response to include the new key")
	}
	if rotated.Key == oldKey {
		t.Fatal("expected rotate to generate a different key")
	}
	if rotated.KeyPrefix == created.KeyPrefix {
		t.Fatalf("expected display prefix to change from %q", created.KeyPrefix)
	}
	if rotated.Name != created.Name || rotated.Description != created.Description || !rotated.IsAdmin || rotated.RateLimitRPM != created.RateLimitRPM {
		t.Fatalf("expected metadata to be preserved, got %#v", rotated.APIKey)
	}

	if validatedOld := manager.Validate(oldKey); validatedOld != nil {
		t.Fatalf("expected old key to be invalid after rotation, got %#v", validatedOld)
	}

	validatedNew := manager.Validate(rotated.Key)
	if validatedNew == nil {
		t.Fatal("expected new key to validate")
	}
	if validatedNew.ID != created.ID || validatedNew.Name != created.Name || !validatedNew.IsAdmin || validatedNew.RateLimitRPM != created.RateLimitRPM {
		t.Fatalf("expected validation metadata to stay on the same key record, got %#v", validatedNew)
	}
	if len(validatedNew.AllowedEndpoints) != 2 || validatedNew.AllowedEndpoints[0] != "messages" || validatedNew.AllowedEndpoints[1] != "responses" {
		t.Fatalf("expected endpoint permissions to be preserved, got %#v", validatedNew.AllowedEndpoints)
	}
}

func TestManagerRotateRejectsRevokedKey(t *testing.T) {
	manager := newRotateTestManager(t)

	created, err := manager.Create(&CreateAPIKeyRequest{Name: "client-key"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := manager.Revoke(created.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	if _, err := manager.Rotate(created.ID); err == nil {
		t.Fatal("expected rotating a revoked key to fail")
	}
}
