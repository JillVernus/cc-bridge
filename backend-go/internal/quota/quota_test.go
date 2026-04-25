package quota

import (
	"testing"
	"time"
)

func TestGetStatusForChannel_MatchByIDAndName(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	now := time.Now()
	m.quotas[2] = &QuotaStatus{
		ChannelID:   2,
		ChannelName: "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 30,
			UpdatedAt:          now,
		},
	}

	got := m.GetStatusForChannel(2, "", "oauth-a")
	if got == nil {
		t.Fatalf("expected quota status, got nil")
	}
	if got.ChannelID != 2 || got.ChannelName != "oauth-a" {
		t.Fatalf("unexpected status identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 30 {
		t.Fatalf("unexpected codex quota payload: %+v", got.CodexQuota)
	}
}

func TestGetStatusForChannel_LookupByNameWithLatestTimestamp(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	oldest := time.Now().Add(-2 * time.Hour)
	latest := time.Now().Add(-5 * time.Minute)

	// Same channel name appears at multiple legacy indices; newest one should win.
	m.quotas[1] = &QuotaStatus{
		ChannelID:   1,
		ChannelName: "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 80,
			UpdatedAt:          oldest,
		},
	}
	m.quotas[4] = &QuotaStatus{
		ChannelID:   4,
		ChannelName: "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 12,
			UpdatedAt:          latest,
		},
	}

	got := m.GetStatusForChannel(9, "", "oauth-a")
	if got == nil {
		t.Fatalf("expected remapped status, got nil")
	}
	if got.ChannelID != 9 || got.ChannelName != "oauth-a" {
		t.Fatalf("unexpected remapped identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 12 {
		t.Fatalf("expected latest quota payload to be selected, got %+v", got.CodexQuota)
	}
}

func TestGetStatusForChannel_RejectsMismatchedStaleEntry(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[3] = &QuotaStatus{
		ChannelID:   3,
		ChannelName: "oauth-old",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 77,
			UpdatedAt:          time.Now().Add(-24 * time.Hour),
		},
	}

	got := m.GetStatusForChannel(3, "", "oauth-new")
	if got != nil {
		t.Fatalf("expected nil for stale mismatched entry, got %+v", got)
	}
}

func TestGetStatusForChannel_RejectsSameNameWithDifferentStableID(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[3] = &QuotaStatus{
		ChannelID:       3,
		ChannelStableID: "old-stable-id",
		ChannelName:     "shared-name",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 77,
			UpdatedAt:          time.Now(),
		},
	}

	got := m.GetStatusForChannel(3, "new-stable-id", "shared-name")
	if got != nil {
		t.Fatalf("expected nil for stable-id mismatch even with same name, got %+v", got)
	}
}

func TestGetStatusForChannel_RejectsLegacyRowWhenStableIDRequested(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[3] = &QuotaStatus{
		ChannelID:   3,
		ChannelName: "shared-name",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 77,
			UpdatedAt:          time.Now(),
		},
	}

	got := m.GetStatusForChannel(3, "new-stable-id", "shared-name")
	if got != nil {
		t.Fatalf("expected nil for legacy quota row when stable id is available, got %+v", got)
	}
}

func TestGetStatusForChannel_LookupByStableIDSurvivesRename(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[1] = &QuotaStatus{
		ChannelID:       1,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "old-name",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 42,
			UpdatedAt:          time.Now(),
		},
	}

	got := m.GetStatusForChannel(9, "oauth-stable-a", "new-name")
	if got == nil {
		t.Fatalf("expected stable-id lookup after rename, got nil")
	}
	if got.ChannelID != 9 || got.ChannelStableID != "oauth-stable-a" || got.ChannelName != "new-name" {
		t.Fatalf("unexpected remapped identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 42 {
		t.Fatalf("expected stable-id quota payload, got %+v", got.CodexQuota)
	}
}

func TestGetStatusForChannel_SwappedIndicesDoNotDestroyOtherChannelState(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	now := time.Now()
	m.quotas[4] = &QuotaStatus{
		ChannelID:   4,
		ChannelName: "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 25,
			UpdatedAt:          now.Add(-1 * time.Minute),
		},
	}
	m.quotas[7] = &QuotaStatus{
		ChannelID:   7,
		ChannelName: "oauth-b",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 65,
			UpdatedAt:          now,
		},
	}

	// Simulate a reorder or DB reload that swaps the current indices.
	gotA := m.GetStatusForChannel(7, "", "oauth-a")
	if gotA == nil || gotA.CodexQuota == nil || gotA.CodexQuota.PrimaryUsedPercent != 25 {
		t.Fatalf("expected oauth-a quota after swap, got %+v", gotA)
	}

	gotB := m.GetStatusForChannel(4, "", "oauth-b")
	if gotB == nil || gotB.CodexQuota == nil || gotB.CodexQuota.PrimaryUsedPercent != 65 {
		t.Fatalf("expected oauth-b quota after swap, got %+v", gotB)
	}
}
