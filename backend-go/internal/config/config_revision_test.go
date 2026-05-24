package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func newRevisionTestDBStorage(t *testing.T) (*DBConfigStorage, database.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "config-revision-test.db")
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

	return NewDBConfigStorage(db, time.Second), db
}

func revisionTestConfig(name string) Config {
	return Config{
		Upstream: []UpstreamConfig{{
			ID:          "msg-1",
			Name:        name,
			BaseURL:     "https://messages.example.com",
			ServiceType: "openai",
			APIKeys:     []string{"msg-key"},
			Priority:    1,
			Status:      "active",
		}},
		LoadBalance:              "failover",
		ResponsesUpstream:        []UpstreamConfig{},
		CurrentResponsesUpstream: 0,
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		ChatUpstream:             []UpstreamConfig{},
		ChatLoadBalance:          "failover",
		UserAgent:                GetDefaultUserAgentConfig(),
	}
}

func TestDBConfigStorage_LoadConfigWithRevisionReturnsSeedRevision(t *testing.T) {
	dbStorage, db := newRevisionTestDBStorage(t)
	defer db.Close()

	cfg, revision, err := dbStorage.LoadConfigFromDBWithRevision()
	if err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() failed: %v", err)
	}
	if cfg == nil {
		t.Fatalf("LoadConfigFromDBWithRevision() returned nil config")
	}
	if revision != 1 {
		t.Fatalf("revision = %d, want 1", revision)
	}
}

func TestDBConfigStorage_SaveConfigIncrementsRevision(t *testing.T) {
	dbStorage, db := newRevisionTestDBStorage(t)
	defer db.Close()

	if _, revisionBefore, err := dbStorage.LoadConfigFromDBWithRevision(); err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() before save failed: %v", err)
	} else if revisionBefore != 1 {
		t.Fatalf("revision before save = %d, want 1", revisionBefore)
	}

	cfg := revisionTestConfig("Revision Primary")
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("SaveConfigToDB() failed: %v", err)
	}

	loaded, revisionAfter, err := dbStorage.LoadConfigFromDBWithRevision()
	if err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() after save failed: %v", err)
	}
	if revisionAfter != 2 {
		t.Fatalf("revision after first save = %d, want 2", revisionAfter)
	}
	if len(loaded.Upstream) != 1 || loaded.Upstream[0].Name != "Revision Primary" {
		t.Fatalf("loaded upstream = %#v, want saved config", loaded.Upstream)
	}

	cfg.Upstream[0].Name = "Revision Secondary"
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("second SaveConfigToDB() failed: %v", err)
	}

	_, revisionSecondSave, err := dbStorage.LoadConfigFromDBWithRevision()
	if err != nil {
		t.Fatalf("LoadConfigFromDBWithRevision() after second save failed: %v", err)
	}
	if revisionSecondSave != 3 {
		t.Fatalf("revision after second save = %d, want 3", revisionSecondSave)
	}
}

func TestDBConfigStorage_CheckForChangesUsesRevisionInsteadOfSecondResolutionTimestamps(t *testing.T) {
	dbStorage, db := newRevisionTestDBStorage(t)
	defer db.Close()

	cm := &ConfigManager{
		configFile:      filepath.Join(t.TempDir(), "config.json"),
		failedKeysCache: make(map[string]*FailedKey),
		config:          revisionTestConfig("Local Initial"),
	}
	dbStorage.SetConfigManager(cm)

	initial := revisionTestConfig("Remote Initial")
	if err := dbStorage.SaveConfigToDB(&initial); err != nil {
		t.Fatalf("initial SaveConfigToDB() failed: %v", err)
	}
	dbStorage.checkForChanges()
	if got := cm.GetConfig().Upstream[0].Name; got != "Remote Initial" {
		t.Fatalf("after initial reload upstream name = %q, want %q", got, "Remote Initial")
	}

	firstPolledVersion := dbStorage.loadLastVersion()
	rapid := revisionTestConfig("Remote Same Second")
	if err := dbStorage.SaveConfigToDB(&rapid); err != nil {
		t.Fatalf("rapid SaveConfigToDB() failed: %v", err)
	}

	dbStorage.checkForChanges()
	if got := cm.GetConfig().Upstream[0].Name; got != "Remote Same Second" {
		t.Fatalf("after same-second reload upstream name = %q, want %q", got, "Remote Same Second")
	}
	if lastVersion := dbStorage.loadLastVersion(); lastVersion <= firstPolledVersion {
		t.Fatalf("lastVersion = %d, want greater than %d", lastVersion, firstPolledVersion)
	}
}

func TestDBConfigStorage_RapidSameSecondSavesAreNotMissedByPolling(t *testing.T) {
	dbStorage, db := newRevisionTestDBStorage(t)
	defer db.Close()

	cm := &ConfigManager{
		configFile:      filepath.Join(t.TempDir(), "config.json"),
		failedKeysCache: make(map[string]*FailedKey),
		config:          revisionTestConfig("Local Initial"),
	}
	dbStorage.SetConfigManager(cm)

	cfg := revisionTestConfig("Rapid 1")
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("first SaveConfigToDB() failed: %v", err)
	}
	dbStorage.checkForChanges()

	cfg.Upstream[0].Name = "Rapid 2"
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("second SaveConfigToDB() failed: %v", err)
	}
	dbStorage.checkForChanges()

	cfg.Upstream[0].Name = "Rapid 3"
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("third SaveConfigToDB() failed: %v", err)
	}
	dbStorage.checkForChanges()

	if got := cm.GetConfig().Upstream[0].Name; got != "Rapid 3" {
		t.Fatalf("final upstream name = %q, want %q", got, "Rapid 3")
	}
	if lastVersion := dbStorage.loadLastVersion(); lastVersion != 4 {
		t.Fatalf("lastVersion = %d, want 4 after seed plus three saves", lastVersion)
	}
}

func TestConfigManager_JSONModeLocalRevisionIncrementsAfterSuccessfulSave(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cm := &ConfigManager{
		configFile:      configPath,
		failedKeysCache: make(map[string]*FailedKey),
		config:          revisionTestConfig("JSON Initial"),
	}

	_, revisionBefore := cm.GetConfigWithRevision()
	if revisionBefore != 0 {
		t.Fatalf("initial revision = %d, want 0 for unsaved in-memory config", revisionBefore)
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		t.Fatalf("saveConfigLocked() failed: %v", err)
	}

	_, revisionAfter := cm.GetConfigWithRevision()
	if revisionAfter != 1 {
		t.Fatalf("revision after successful file save = %d, want 1", revisionAfter)
	}

	if err := cm.saveConfigLocked(cm.config); err != nil {
		t.Fatalf("second saveConfigLocked() failed: %v", err)
	}

	_, revisionSecondSave := cm.GetConfigWithRevision()
	if revisionSecondSave != 2 {
		t.Fatalf("revision after second successful file save = %d, want 2", revisionSecondSave)
	}
}

func TestConfigManager_JSONModeLocalRevisionDoesNotIncrementAfterFailedSave(t *testing.T) {
	configDir := t.TempDir()
	cm := &ConfigManager{
		configFile:      configDir,
		failedKeysCache: make(map[string]*FailedKey),
		config:          revisionTestConfig("JSON Initial"),
		revision:        7,
	}

	updated := revisionTestConfig("JSON Unsaved")
	err := cm.saveConfigLocked(updated)
	if err == nil {
		t.Fatalf("saveConfigLocked() succeeded for directory path, want write failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "directory") {
		t.Fatalf("saveConfigLocked() error = %v, want directory write failure", err)
	}

	loaded, revisionAfter := cm.GetConfigWithRevision()
	if revisionAfter != 7 {
		t.Fatalf("revision after failed file save = %d, want 7", revisionAfter)
	}
	if got := loaded.Upstream[0].Name; got != "JSON Initial" {
		t.Fatalf("config after failed file save = %q, want original JSON Initial", got)
	}
}
