package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func newStaleWriteTestManagerPair(t *testing.T) (*ConfigManager, *ConfigManager, *DBConfigStorage, *DBConfigStorage, database.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "config-stale-write-test.db")
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

	storageA := NewDBConfigStorage(db, time.Second)
	storageB := NewDBConfigStorage(db, time.Second)

	managerA := newStaleWriteTestManager(t, storageA)
	managerB := newStaleWriteTestManager(t, storageB)

	storageA.SetConfigManager(managerA)
	storageB.SetConfigManager(managerB)

	return managerA, managerB, storageA, storageB, db
}

func newStaleWriteTestManager(t *testing.T, storage *DBConfigStorage) *ConfigManager {
	t.Helper()

	cfg, revision, err := storage.LoadConfigFromDBWithRevision()
	if err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() failed: %v", err)
	}

	return &ConfigManager{
		configFile:      filepath.Join(t.TempDir(), "config.json"),
		failedKeysCache: make(map[string]*FailedKey),
		config:          *cfg,
		dbStorage:       storage,
		revision:        revision,
	}
}

func staleWriteTestUpstream(name string) UpstreamConfig {
	return UpstreamConfig{
		Name:        name,
		BaseURL:     "https://stale-write.example.com",
		ServiceType: "openai",
		APIKeys:     []string{"stale-write-key"},
		Status:      "active",
	}
}

func assertStaleWriteError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("got nil error, want stale write error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Fatalf("error = %v, want stale write error", err)
	}
}

func TestConfigManager_StaleAddUpstreamIsRejectedWithSameDatabaseTwoManagers(t *testing.T) {
	managerA, managerB, _, _, db := newStaleWriteTestManagerPair(t)
	defer db.Close()

	if err := managerA.AddUpstream(staleWriteTestUpstream("writer-a")); err != nil {
		t.Fatalf("managerA AddUpstream() failed: %v", err)
	}

	err := managerB.AddUpstream(staleWriteTestUpstream("writer-b"))
	assertStaleWriteError(t, err)
}

func TestConfigManager_StaleWriteDoesNotDeleteOrOverwriteNewerChannelSnapshot(t *testing.T) {
	managerA, managerB, storageA, _, db := newStaleWriteTestManagerPair(t)
	defer db.Close()

	if err := managerA.AddUpstream(staleWriteTestUpstream("durable-newer")); err != nil {
		t.Fatalf("managerA AddUpstream() failed: %v", err)
	}
	assertStaleWriteError(t, managerB.AddUpstream(staleWriteTestUpstream("stale-overwrite")))

	loaded, _, err := storageA.LoadConfigFromDBWithRevision()
	if err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() after stale save failed: %v", err)
	}
	if len(loaded.Upstream) != 1 {
		t.Fatalf("loaded upstream count = %d, want 1 newer channel", len(loaded.Upstream))
	}
	if got := loaded.Upstream[0].Name; got != "durable-newer" {
		t.Fatalf("loaded upstream name = %q, want durable-newer", got)
	}
}

func TestConfigManager_FailedStaleSaveKeepsLocalConfigUnchangedOrReloadedToDurableState(t *testing.T) {
	managerA, managerB, _, _, db := newStaleWriteTestManagerPair(t)
	defer db.Close()

	beforeConfig, beforeRevision := managerB.GetConfigWithRevision()
	if len(beforeConfig.Upstream) != 0 {
		t.Fatalf("managerB initial upstream count = %d, want 0", len(beforeConfig.Upstream))
	}

	if err := managerA.AddUpstream(staleWriteTestUpstream("durable-after-stale")); err != nil {
		t.Fatalf("managerA AddUpstream() failed: %v", err)
	}
	assertStaleWriteError(t, managerB.AddUpstream(staleWriteTestUpstream("stale-local")))

	afterConfig, afterRevision := managerB.GetConfigWithRevision()
	_, durableRevision := managerA.GetConfigWithRevision()
	if afterRevision != beforeRevision && afterRevision != durableRevision {
		t.Fatalf("managerB revision after failed stale save = %d, want unchanged %d or durable %d", afterRevision, beforeRevision, durableRevision)
	}
	if len(afterConfig.Upstream) == 1 && afterConfig.Upstream[0].Name == "stale-local" {
		t.Fatalf("managerB kept unsaved stale channel in memory after failed save")
	}
	if len(afterConfig.Upstream) > 1 {
		t.Fatalf("managerB upstream count after failed stale save = %d, want unchanged or durable state", len(afterConfig.Upstream))
	}
}
