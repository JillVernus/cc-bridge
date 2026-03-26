package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func newConfigTestDBStorage(t *testing.T) (*DBConfigStorage, database.DB) {
	t.Helper()

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

	return NewDBConfigStorage(db, time.Second), db
}

func testChatConfig() Config {
	return Config{
		Upstream:                 []UpstreamConfig{},
		CurrentUpstream:          0,
		LoadBalance:              "failover",
		ResponsesUpstream:        []UpstreamConfig{},
		CurrentResponsesUpstream: 0,
		ResponsesLoadBalance:     "random",
		GeminiUpstream: []UpstreamConfig{{
			ID:          "gem-1",
			Name:        "Gemini Primary",
			BaseURL:     "https://gemini.example.com",
			ServiceType: "gemini",
			APIKeys:     []string{"gem-key"},
			Priority:    2,
		}},
		GeminiLoadBalance: "round-robin",
		ChatUpstream: []UpstreamConfig{{
			ID:                        "chat-1",
			Name:                      "Chat Primary",
			BaseURL:                   "https://chat.example.com/v1",
			ServiceType:               "openai",
			APIKeys:                   []string{"chat-key-1", "chat-key-2"},
			Description:               "chat upstream",
			Website:                   "https://chat.example.com",
			ResponseHeaderTimeoutSecs: 45,
			Priority:                  5,
			Status:                    "active",
			ModelMapping: map[string]string{
				"gpt-4.1": "gpt-4o-mini",
			},
			PriceMultipliers: map[string]TokenPriceMultipliers{
				"_default": {InputMultiplier: 1.25, OutputMultiplier: 1.5},
			},
			QuotaType:          "requests",
			QuotaLimit:         100,
			QuotaResetInterval: 1,
			QuotaResetUnit:     "days",
			QuotaModels:        []string{"gpt-4.1"},
			QuotaResetMode:     "fixed",
			RateLimitRpm:       60,
			QueueEnabled:       true,
			QueueTimeout:       30,
			KeyLoadBalance:     "random",
		}, {
			ID:          "chat-2",
			Name:        "Chat Secondary",
			BaseURL:     "https://chat2.example.com/v1",
			ServiceType: "openai",
			APIKeys:     []string{"chat-key-3"},
			Priority:    10,
			Status:      "disabled",
		}},
		ChatLoadBalance: "round-robin",
		UserAgent:       GetDefaultUserAgentConfig(),
	}
}

func TestSaveConfigLocked_ReturnsErrorWhenDBWriteFails(t *testing.T) {
	dbStorage, db := newConfigTestDBStorage(t)
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
		ChatUpstream:             []UpstreamConfig{},
		ChatLoadBalance:          "failover",
		UserAgent:                GetDefaultUserAgentConfig(),
	}

	if err := cm.saveConfigLocked(cfg); err == nil {
		t.Fatalf("expected saveConfigLocked to return database error when DB is closed")
	}
}

func TestDBConfigStorage_SaveAndLoadChatPersistence(t *testing.T) {
	dbStorage, db := newConfigTestDBStorage(t)
	defer db.Close()

	cfg := testChatConfig()
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("SaveConfigToDB() failed: %v", err)
	}

	loaded, err := dbStorage.LoadConfigFromDB()
	if err != nil {
		t.Fatalf("LoadConfigFromDB() failed: %v", err)
	}

	if loaded.ChatLoadBalance != cfg.ChatLoadBalance {
		t.Fatalf("ChatLoadBalance = %q, want %q", loaded.ChatLoadBalance, cfg.ChatLoadBalance)
	}
	if len(loaded.ChatUpstream) != len(cfg.ChatUpstream) {
		t.Fatalf("len(ChatUpstream) = %d, want %d", len(loaded.ChatUpstream), len(cfg.ChatUpstream))
	}
	if len(loaded.GeminiUpstream) != len(cfg.GeminiUpstream) {
		t.Fatalf("len(GeminiUpstream) = %d, want %d", len(loaded.GeminiUpstream), len(cfg.GeminiUpstream))
	}
	if loaded.GeminiLoadBalance != cfg.GeminiLoadBalance {
		t.Fatalf("GeminiLoadBalance = %q, want %q", loaded.GeminiLoadBalance, cfg.GeminiLoadBalance)
	}

	for i, want := range cfg.ChatUpstream {
		got := loaded.ChatUpstream[i]
		if got.Index != i {
			t.Fatalf("ChatUpstream[%d].Index = %d, want %d", i, got.Index, i)
		}
		got.Index = 0
		want.Index = 0
		gotJSON, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal loaded chat upstream %d: %v", i, err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal expected chat upstream %d: %v", i, err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("ChatUpstream[%d] mismatch:\n got: %s\nwant: %s", i, gotJSON, wantJSON)
		}
	}
}

func TestDBConfigStorage_CheckForChangesReloadsChatAndGeminiIndexes(t *testing.T) {
	dbStorage, db := newConfigTestDBStorage(t)
	defer db.Close()

	initial := Config{UserAgent: GetDefaultUserAgentConfig()}
	cm := &ConfigManager{
		configFile:      filepath.Join(t.TempDir(), "config.json"),
		failedKeysCache: make(map[string]*FailedKey),
		config:          initial,
	}
	dbStorage.SetConfigManager(cm)

	cfg := testChatConfig()
	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("SaveConfigToDB() failed: %v", err)
	}

	dbStorage.checkForChanges()

	reloaded := cm.GetConfig()
	if reloaded.ChatLoadBalance != cfg.ChatLoadBalance {
		t.Fatalf("reloaded ChatLoadBalance = %q, want %q", reloaded.ChatLoadBalance, cfg.ChatLoadBalance)
	}
	if len(reloaded.ChatUpstream) != len(cfg.ChatUpstream) {
		t.Fatalf("reloaded len(ChatUpstream) = %d, want %d", len(reloaded.ChatUpstream), len(cfg.ChatUpstream))
	}
	if len(reloaded.GeminiUpstream) != len(cfg.GeminiUpstream) {
		t.Fatalf("reloaded len(GeminiUpstream) = %d, want %d", len(reloaded.GeminiUpstream), len(cfg.GeminiUpstream))
	}
	for i := range reloaded.ChatUpstream {
		if reloaded.ChatUpstream[i].Index != i {
			t.Fatalf("reloaded ChatUpstream[%d].Index = %d, want %d", i, reloaded.ChatUpstream[i].Index, i)
		}
	}
	for i := range reloaded.GeminiUpstream {
		if reloaded.GeminiUpstream[i].Index != i {
			t.Fatalf("reloaded GeminiUpstream[%d].Index = %d, want %d", i, reloaded.GeminiUpstream[i].Index, i)
		}
	}
}

func TestDBConfigStorage_SaveAndLoadCodexServiceTierOverride(t *testing.T) {
	dbStorage, db := newConfigTestDBStorage(t)
	defer db.Close()

	cfg := Config{
		Upstream:        []UpstreamConfig{},
		CurrentUpstream: 0,
		LoadBalance:     "failover",
		ResponsesUpstream: []UpstreamConfig{{
			ID:                       "resp-1",
			Name:                     "Codex OAuth",
			BaseURL:                  "https://chatgpt.com/backend-api/codex",
			ServiceType:              "openai-oauth",
			Priority:                 1,
			Status:                   "active",
			CodexServiceTierOverride: "force_priority",
		}},
		CurrentResponsesUpstream: 0,
		ResponsesLoadBalance:     "failover",
		GeminiUpstream:           []UpstreamConfig{},
		GeminiLoadBalance:        "failover",
		ChatUpstream:             []UpstreamConfig{},
		ChatLoadBalance:          "failover",
		UserAgent:                GetDefaultUserAgentConfig(),
	}

	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("SaveConfigToDB() failed: %v", err)
	}

	loaded, err := dbStorage.LoadConfigFromDB()
	if err != nil {
		t.Fatalf("LoadConfigFromDB() failed: %v", err)
	}

	if len(loaded.ResponsesUpstream) != 1 {
		t.Fatalf("len(ResponsesUpstream) = %d, want 1", len(loaded.ResponsesUpstream))
	}
	if got := loaded.ResponsesUpstream[0].CodexServiceTierOverride; got != "force_priority" {
		t.Fatalf("CodexServiceTierOverride = %q, want force_priority", got)
	}
}
