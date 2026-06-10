package requestlog

import (
	"path/filepath"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestSaveChannelQuota_StableIDUpsertsAcrossIndexChanges(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "request_logs.db"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          5,
		ChannelStableID:    "oauth-stable-a",
		ChannelName:        "OAuth A",
		PrimaryUsedPercent: 20,
	}); err != nil {
		t.Fatalf("SaveChannelQuota stable-a initial failed: %v", err)
	}
	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          9,
		ChannelStableID:    "oauth-stable-b",
		ChannelName:        "OAuth B",
		PrimaryUsedPercent: 70,
	}); err != nil {
		t.Fatalf("SaveChannelQuota stable-b failed: %v", err)
	}
	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          1,
		ChannelStableID:    "oauth-stable-a",
		ChannelName:        "OAuth A Renamed",
		PrimaryUsedPercent: 35,
	}); err != nil {
		t.Fatalf("SaveChannelQuota stable-a moved failed: %v", err)
	}

	quotas, err := manager.GetAllChannelQuotas()
	if err != nil {
		t.Fatalf("GetAllChannelQuotas failed: %v", err)
	}

	if len(quotas) != 2 {
		t.Fatalf("expected 2 stable quota rows, got %d: %+v", len(quotas), quotas)
	}

	byStableID := make(map[string]*ChannelQuota, len(quotas))
	for _, quota := range quotas {
		byStableID[quota.ChannelStableID] = quota
	}

	stableA := byStableID["oauth-stable-a"]
	if stableA == nil {
		t.Fatalf("expected oauth-stable-a row, got %+v", byStableID)
	}
	if stableA.ChannelID != 1 || stableA.ChannelName != "OAuth A Renamed" || stableA.PrimaryUsedPercent != 35 {
		t.Fatalf("expected moved oauth-stable-a row to be updated, got %+v", stableA)
	}

	stableB := byStableID["oauth-stable-b"]
	if stableB == nil {
		t.Fatalf("expected oauth-stable-b row, got %+v", byStableID)
	}
	if stableB.ChannelID != 9 || stableB.ChannelName != "OAuth B" || stableB.PrimaryUsedPercent != 70 {
		t.Fatalf("expected oauth-stable-b row to remain unchanged, got %+v", stableB)
	}
}

func TestSaveChannelQuota_StableIDMoveDoesNotCollideWithLegacyCurrentIndexRow(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "request_logs.db"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          5,
		ChannelStableID:    "oauth-stable-a",
		ChannelName:        "OAuth A",
		PrimaryUsedPercent: 20,
	}); err != nil {
		t.Fatalf("SaveChannelQuota stable row failed: %v", err)
	}

	if _, err := manager.db.Exec(`
		INSERT INTO channel_quota (
			channel_id, channel_stable_id, channel_name, primary_used_percent
		) VALUES (?, NULL, ?, ?)
	`, 1, "Legacy OAuth A", 100); err != nil {
		t.Fatalf("insert legacy current-index row failed: %v", err)
	}

	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          1,
		ChannelStableID:    "oauth-stable-a",
		ChannelName:        "OAuth A",
		PrimaryUsedPercent: 35,
	}); err != nil {
		t.Fatalf("SaveChannelQuota stable row moved onto legacy index failed: %v", err)
	}

	quotas, err := manager.GetAllChannelQuotas()
	if err != nil {
		t.Fatalf("GetAllChannelQuotas failed: %v", err)
	}
	var got *ChannelQuota
	for _, quota := range quotas {
		if quota.ChannelStableID == "oauth-stable-a" {
			got = quota
			break
		}
	}
	if got == nil || got.PrimaryUsedPercent != 35 {
		t.Fatalf("expected stable row to update without colliding, got %+v in %+v", got, quotas)
	}
}

func TestGetAllChannelQuotas_LoadsLegacyRowsWithNullTextFields(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "request_logs.db"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	_, err = manager.db.Exec(`
		INSERT INTO channel_quota (
			channel_id, channel_stable_id, channel_name, plan_type,
			primary_used_percent, credits_balance, exceeded_reason
		) VALUES (?, NULL, ?, NULL, ?, NULL, NULL)
	`, 9, "Plus:jj-vpn-2023", 64)
	if err != nil {
		t.Fatalf("insert legacy quota row failed: %v", err)
	}

	quotas, err := manager.GetAllChannelQuotas()
	if err != nil {
		t.Fatalf("GetAllChannelQuotas failed: %v", err)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota row, got %d: %+v", len(quotas), quotas)
	}

	got := quotas[0]
	if got.ChannelID != 9 || got.ChannelStableID != "" || got.ChannelName != "Plus:jj-vpn-2023" {
		t.Fatalf("unexpected legacy quota identity: %+v", got)
	}
	if got.PlanType != "" || got.CreditsBalance != "" || got.ExceededReason != "" {
		t.Fatalf("expected NULL text fields to load as empty strings, got %+v", got)
	}
	if got.PrimaryUsedPercent != 64 {
		t.Fatalf("expected primary_used_percent=64, got %+v", got)
	}
}

func TestNewManagerWithDB_RepairsMissingCodexQuotaSnapshotColumn(t *testing.T) {
	db, err := database.New(database.Config{
		Type: database.DialectSQLite,
		URL:  filepath.Join(t.TempDir(), "cc-bridge.db"),
	})
	if err != nil {
		t.Fatalf("New database failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
		CREATE TABLE channel_quota (
			channel_id INTEGER PRIMARY KEY,
			channel_stable_id TEXT,
			channel_name TEXT NOT NULL,
			plan_type TEXT,
			primary_used_percent INTEGER DEFAULT 0,
			primary_window_minutes INTEGER DEFAULT 0,
			primary_reset_at DATETIME,
			secondary_used_percent INTEGER DEFAULT 0,
			secondary_window_minutes INTEGER DEFAULT 0,
			secondary_reset_at DATETIME,
			credits_has_credits BOOLEAN DEFAULT 0,
			credits_unlimited BOOLEAN DEFAULT 0,
			credits_balance TEXT,
			is_exceeded BOOLEAN DEFAULT 0,
			exceeded_at DATETIME,
			recover_at DATETIME,
			exceeded_reason TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("create legacy channel_quota schema failed: %v", err)
	}

	manager, err := NewManagerWithDB(db, "")
	if err != nil {
		t.Fatalf("NewManagerWithDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	if err := manager.SaveChannelQuota(&ChannelQuota{
		ChannelID:          1,
		ChannelStableID:    "oauth-stable-a",
		ChannelName:        "OAuth A",
		PrimaryUsedPercent: 12,
		CodexQuotaSnapshot: `{"plan_type":"plus","primary_used_percent":12}`,
	}); err != nil {
		t.Fatalf("SaveChannelQuota failed after schema repair: %v", err)
	}

	got, err := manager.GetChannelQuota(1)
	if err != nil {
		t.Fatalf("GetChannelQuota failed: %v", err)
	}
	if got == nil || got.CodexQuotaSnapshot == "" {
		t.Fatalf("expected codex quota snapshot to round-trip, got %+v", got)
	}
}
