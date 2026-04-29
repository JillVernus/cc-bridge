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
