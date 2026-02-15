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

	got := m.GetStatusForChannel(2, "oauth-a")
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

func TestGetStatusForChannel_RemapByNameWithLatestTimestamp(t *testing.T) {
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

	got := m.GetStatusForChannel(9, "oauth-a")
	if got == nil {
		t.Fatalf("expected remapped status, got nil")
	}
	if got.ChannelID != 9 || got.ChannelName != "oauth-a" {
		t.Fatalf("unexpected remapped identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 12 {
		t.Fatalf("expected latest quota payload to be selected, got %+v", got.CodexQuota)
	}

	if _, exists := m.quotas[9]; !exists {
		t.Fatalf("expected remapped status to be stored at new channel index")
	}
	if _, exists := m.quotas[4]; exists {
		t.Fatalf("expected previous matched index to be cleaned up")
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

	got := m.GetStatusForChannel(3, "oauth-new")
	if got != nil {
		t.Fatalf("expected nil for stale mismatched entry, got %+v", got)
	}

	if _, exists := m.quotas[3]; exists {
		t.Fatalf("expected stale mismatched entry to be removed")
	}
}
