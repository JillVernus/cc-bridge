package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestSaveConfigLocked_ReturnsErrorWhenDBWriteFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config-save-test.db")
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

	dbStorage := NewDBConfigStorage(db, time.Second)
	cm := &ConfigManager{
		configFile:      filepath.Join(t.TempDir(), "config.json"),
		failedKeysCache: make(map[string]*FailedKey),
		dbStorage:       dbStorage,
	}

	if err := db.Close(); err != nil {
		t.Fatalf("failed to close test database: %v", err)
	}

	cfg := Config{
		Upstream:                 []UpstreamConfig{},
		CurrentUpstream:          0,
		LoadBalance:              "failover",
		ResponsesUpstream:        []UpstreamConfig{},
		CurrentResponsesUpstream: 0,
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		UserAgent:                GetDefaultUserAgentConfig(),
	}

	if err := cm.saveConfigLocked(cfg); err == nil {
		t.Fatalf("expected saveConfigLocked to return database error when DB is closed")
	}
}
