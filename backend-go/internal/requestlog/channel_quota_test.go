package requestlog

import (
	"path/filepath"
	"testing"
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
