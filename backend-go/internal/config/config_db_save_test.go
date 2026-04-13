package config

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
	"github.com/JillVernus/cc-bridge/internal/utils"
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
		ResponsesUpstream: []UpstreamConfig{
			{
				ID:                       "resp-force-default",
				Name:                     "Codex OAuth Default",
				BaseURL:                  "https://chatgpt.com/backend-api/codex",
				ServiceType:              "openai-oauth",
				Priority:                 1,
				Status:                   "active",
				CodexServiceTierOverride: " Force_Default ",
			},
			{
				ID:                       "resp-force-priority",
				Name:                     "Codex Responses Priority",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "responses",
				Priority:                 2,
				Status:                   "active",
				CodexServiceTierOverride: " Force_Priority ",
			},
			{
				ID:                       "resp-unknown",
				Name:                     "Codex Unknown",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "responses",
				Priority:                 3,
				Status:                   "active",
				CodexServiceTierOverride: "  sometimes  ",
			},
			{
				ID:                       "resp-empty",
				Name:                     "Codex Empty",
				BaseURL:                  "https://api.openai.com/v1/responses",
				ServiceType:              "responses",
				Priority:                 4,
				Status:                   "active",
				CodexServiceTierOverride: "   ",
			},
			{
				ID:                       "resp-ineligible",
				Name:                     "Codex Ineligible",
				BaseURL:                  "https://api.openai.com/v1",
				ServiceType:              "openai",
				Priority:                 5,
				Status:                   "active",
				CodexServiceTierOverride: "force_default",
			},
		},
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

	stored := map[string]string{}
	rows, err := db.Query(`
		SELECT channel_id, codex_service_tier_override
		FROM channels
		WHERE channel_type = ?
		ORDER BY priority, id
	`, "responses")
	if err != nil {
		t.Fatalf("query stored codex_service_tier_override values: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			channelID string
			override  sql.NullString
		)
		if err := rows.Scan(&channelID, &override); err != nil {
			t.Fatalf("scan stored codex_service_tier_override: %v", err)
		}
		stored[channelID] = override.String
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stored codex_service_tier_override rows: %v", err)
	}

	wantStored := map[string]string{
		"resp-force-default":  "force_default",
		"resp-force-priority": "force_priority",
		"resp-unknown":        "off",
		"resp-empty":          "off",
		"resp-ineligible":     "off",
	}
	if len(stored) != len(wantStored) {
		t.Fatalf("stored row count = %d, want %d", len(stored), len(wantStored))
	}
	for channelID, want := range wantStored {
		if got := stored[channelID]; got != want {
			t.Fatalf("stored codex_service_tier_override for %s = %q, want %q", channelID, got, want)
		}
	}

	loaded, err := dbStorage.LoadConfigFromDB()
	if err != nil {
		t.Fatalf("LoadConfigFromDB() failed: %v", err)
	}

	if len(loaded.ResponsesUpstream) != len(wantStored) {
		t.Fatalf("len(ResponsesUpstream) = %d, want %d", len(loaded.ResponsesUpstream), len(wantStored))
	}

	for _, upstream := range loaded.ResponsesUpstream {
		want, ok := wantStored[upstream.ID]
		if !ok {
			t.Fatalf("unexpected loaded channel ID %q", upstream.ID)
		}
		if got := upstream.CodexServiceTierOverride; got != want {
			t.Fatalf("loaded CodexServiceTierOverride for %s = %q, want %q", upstream.ID, got, want)
		}
	}
}

func TestDBConfigStorage_SaveAndLoadOutboundHeaderPolicy(t *testing.T) {
	dbStorage, db := newConfigTestDBStorage(t)
	defer db.Close()

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
		OutboundHeaderPolicy: utils.OutboundHeaderPolicy{
			Enabled:     true,
			StripRules:  []string{"Cf-*", "X-Forwarded-*", "True-Client-IP"},
			Initialized: true,
		},
	}

	if err := dbStorage.SaveConfigToDB(&cfg); err != nil {
		t.Fatalf("SaveConfigToDB() failed: %v", err)
	}

	loaded, err := dbStorage.LoadConfigFromDB()
	if err != nil {
		t.Fatalf("LoadConfigFromDB() failed: %v", err)
	}

	if loaded.OutboundHeaderPolicy.Enabled != cfg.OutboundHeaderPolicy.Enabled {
		t.Fatalf("OutboundHeaderPolicy.Enabled = %v, want %v", loaded.OutboundHeaderPolicy.Enabled, cfg.OutboundHeaderPolicy.Enabled)
	}

	gotJSON, err := json.Marshal(loaded.OutboundHeaderPolicy)
	if err != nil {
		t.Fatalf("marshal loaded outbound header policy: %v", err)
	}
	wantJSON, err := json.Marshal(cfg.OutboundHeaderPolicy)
	if err != nil {
		t.Fatalf("marshal expected outbound header policy: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("OutboundHeaderPolicy mismatch:\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}
