package quota

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

type testQuotaPersister struct {
	quotas []*PersistedQuota
	err    error
}

func (p *testQuotaPersister) SaveChannelQuota(q *PersistedQuota) error {
	if p.err != nil {
		return p.err
	}
	return fmt.Errorf("unexpected SaveChannelQuota call: %+v", q)
}

func (p *testQuotaPersister) GetChannelQuota(channelID int) (*PersistedQuota, error) {
	for _, quota := range p.quotas {
		if quota.ChannelID == channelID {
			return quota, nil
		}
	}
	return nil, nil
}

func (p *testQuotaPersister) GetAllChannelQuotas() ([]*PersistedQuota, error) {
	return p.quotas, nil
}

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

func TestGetStatusForChannel_RejectsLegacyRowWhenStableIDRequestedAndNameDiffers(t *testing.T) {
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

	got := m.GetStatusForChannel(3, "new-stable-id", "different-name")
	if got != nil {
		t.Fatalf("expected nil for mismatched legacy quota row when stable id is available, got %+v", got)
	}
}

func TestGetStatusForChannel_StableIDFallsBackToCurrentLegacyRowWhenNameMatches(t *testing.T) {
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
	if got == nil {
		t.Fatalf("expected legacy quota row to be reused for same current index and name")
	}
	if got.ChannelID != 3 || got.ChannelStableID != "new-stable-id" || got.ChannelName != "shared-name" {
		t.Fatalf("unexpected fallback identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 77 {
		t.Fatalf("expected legacy quota payload, got %+v", got.CodexQuota)
	}
}

func TestSetPersister_LoadedLegacyQuotaCanBeReadByStableIDWhenCurrentNameMatches(t *testing.T) {
	now := time.Now()
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}
	m.SetPersister(&testQuotaPersister{
		quotas: []*PersistedQuota{
			{
				ChannelID:          3,
				ChannelName:        "shared-name",
				PlanType:           "plus",
				PrimaryUsedPercent: 77,
				UpdatedAt:          now,
			},
		},
	})

	got := m.GetStatusForChannel(3, "new-stable-id", "shared-name")
	if got == nil {
		t.Fatalf("expected loaded legacy quota row to be reused for same current index and name")
	}
	if got.ChannelID != 3 || got.ChannelStableID != "new-stable-id" || got.ChannelName != "shared-name" {
		t.Fatalf("unexpected loaded fallback identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PlanType != "plus" || got.CodexQuota.PrimaryUsedPercent != 77 {
		t.Fatalf("expected loaded legacy codex quota payload, got %+v", got.CodexQuota)
	}
}

func TestSetPersister_LoadsZeroPercentQuotaWhenWindowDataExists(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour)
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}
	m.SetPersister(&testQuotaPersister{
		quotas: []*PersistedQuota{
			{
				ChannelID:              3,
				ChannelStableID:        "oauth-stable-a",
				ChannelName:            "oauth-a",
				PrimaryUsedPercent:     0,
				PrimaryWindowMinutes:   300,
				PrimaryResetAt:         &resetAt,
				SecondaryUsedPercent:   0,
				SecondaryWindowMinutes: 10080,
				SecondaryResetAt:       &resetAt,
				UpdatedAt:              time.Now(),
			},
		},
	})

	got := m.GetStatusForChannel(3, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil {
		t.Fatalf("expected zero-percent persisted quota with window data to reload, got %+v", got)
	}
	if got.CodexQuota.PrimaryWindowMinutes != 300 || got.CodexQuota.SecondaryWindowMinutes != 10080 {
		t.Fatalf("unexpected reloaded quota windows: %+v", got.CodexQuota)
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

func TestGetStatusForChannel_StableIDScansPastConflictingCurrentIndex(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	now := time.Now()
	m.quotas[1] = &QuotaStatus{
		ChannelID:       1,
		ChannelStableID: "oauth-stable-b",
		ChannelName:     "oauth-b",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 88,
			UpdatedAt:          now,
		},
	}
	m.quotas[4] = &QuotaStatus{
		ChannelID:       4,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 22,
			UpdatedAt:          now.Add(-time.Minute),
		},
	}

	got := m.GetStatusForChannel(1, "oauth-stable-a", "oauth-a")
	if got == nil {
		t.Fatalf("expected stable-id lookup to scan past conflicting current index")
	}
	if got.ChannelID != 1 || got.ChannelStableID != "oauth-stable-a" || got.ChannelName != "oauth-a" {
		t.Fatalf("unexpected remapped identity: %+v", got)
	}
	if got.CodexQuota == nil || got.CodexQuota.PrimaryUsedPercent != 22 {
		t.Fatalf("expected oauth-stable-a quota payload, got %+v", got.CodexQuota)
	}
}

func TestUpdateFromHeadersForChannel_StableIDUpdatesExistingEntryWithoutDestroyingCurrentIndex(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[1] = &QuotaStatus{
		ChannelID:       1,
		ChannelStableID: "oauth-stable-b",
		ChannelName:     "oauth-b",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 88,
			UpdatedAt:          time.Now().Add(-time.Minute),
		},
	}
	m.quotas[4] = &QuotaStatus{
		ChannelID:       4,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PrimaryUsedPercent: 22,
			UpdatedAt:          time.Now().Add(-time.Minute),
		},
	}

	headers := http.Header{}
	headers.Set("X-Codex-Plan-Type", "plus")
	headers.Set("X-Codex-Primary-Used-Percent", "35")
	headers.Set("X-Codex-Secondary-Used-Percent", "45")

	m.UpdateFromHeadersForChannel(1, "oauth-stable-a", "oauth-a", headers)

	currentIndexStatus := m.quotas[1]
	if currentIndexStatus == nil || currentIndexStatus.ChannelStableID != "oauth-stable-b" {
		t.Fatalf("expected current index quota to remain oauth-stable-b, got %+v", currentIndexStatus)
	}
	if currentIndexStatus.CodexQuota == nil || currentIndexStatus.CodexQuota.PrimaryUsedPercent != 88 {
		t.Fatalf("expected oauth-stable-b quota payload to remain unchanged, got %+v", currentIndexStatus.CodexQuota)
	}

	updatedStatus := m.quotas[4]
	if updatedStatus == nil || updatedStatus.ChannelStableID != "oauth-stable-a" {
		t.Fatalf("expected existing stable-id quota row to be updated, got %+v", updatedStatus)
	}
	if updatedStatus.ChannelID != 1 || updatedStatus.ChannelName != "oauth-a" {
		t.Fatalf("expected updated stable-id row to carry current identity, got %+v", updatedStatus)
	}
	if updatedStatus.CodexQuota == nil || updatedStatus.CodexQuota.PrimaryUsedPercent != 35 || updatedStatus.CodexQuota.SecondaryUsedPercent != 45 {
		t.Fatalf("expected new codex quota payload, got %+v", updatedStatus.CodexQuota)
	}
}

func TestUpdateFromHeadersForChannel_ParsesDecimalCodexUsedPercent(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	headers := http.Header{}
	headers.Set("X-Codex-Plan-Type", "plus")
	headers.Set("X-Codex-Primary-Used-Percent", "36.5")
	headers.Set("X-Codex-Secondary-Used-Percent", "27.4")

	m.UpdateFromHeadersForChannel(5, "oauth-stable-a", "oauth-a", headers)

	got := m.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil {
		t.Fatalf("expected codex quota payload, got %+v", got)
	}
	if got.CodexQuota.PrimaryUsedPercentValue() != 36.5 {
		t.Fatalf("primary used percent = %v, want 36.5", got.CodexQuota.PrimaryUsedPercentValue())
	}
	if got.CodexQuota.SecondaryUsedPercentValue() != 27.4 {
		t.Fatalf("secondary used percent = %v, want 27.4", got.CodexQuota.SecondaryUsedPercentValue())
	}
}

func TestRequestLogAdapter_PersistsCodexQuotaSnapshotAcrossReload(t *testing.T) {
	requestLogManager, err := requestlog.NewManager(filepath.Join(t.TempDir(), "request_logs.db"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		if err := requestLogManager.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	writer := &Manager{quotas: make(map[int]*QuotaStatus)}
	writer.SetPersister(NewRequestLogAdapter(requestLogManager))

	headers := http.Header{}
	headers.Set("X-Codex-Active-Limit", "premium")
	headers.Set("X-Codex-Plan-Type", "pro")
	headers.Set("X-Codex-Primary-Used-Percent", "36.5")
	headers.Set("X-Codex-Secondary-Used-Percent", "27.4")
	headers.Set("X-Codex-Bengalfox-Limit-Name", "GPT-5.3-Codex-Spark")
	headers.Set("X-Codex-Bengalfox-Primary-Used-Percent", "0.25")
	headers.Set("X-Codex-Bengalfox-Primary-Window-Minutes", "300")
	headers.Set("X-Codex-Bengalfox-Secondary-Used-Percent", "1.5")
	headers.Set("X-Codex-Bengalfox-Secondary-Window-Minutes", "10080")

	writer.UpdateFromHeadersForChannel(5, "oauth-stable-a", "oauth-a", headers)

	reader := &Manager{quotas: make(map[int]*QuotaStatus)}
	reader.SetPersister(NewRequestLogAdapter(requestLogManager))

	got := reader.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil {
		t.Fatalf("expected persisted codex quota after reload, got %+v", got)
	}
	if got.CodexQuota.ActiveLimit != "premium" {
		t.Fatalf("ActiveLimit = %q, want premium", got.CodexQuota.ActiveLimit)
	}
	if got.CodexQuota.PrimaryUsedPercentValue() != 36.5 {
		t.Fatalf("primary used percent = %v, want 36.5", got.CodexQuota.PrimaryUsedPercentValue())
	}
	if got.CodexQuota.SecondaryUsedPercentValue() != 27.4 {
		t.Fatalf("secondary used percent = %v, want 27.4", got.CodexQuota.SecondaryUsedPercentValue())
	}
	if len(got.CodexQuota.DetailedLimits) != 1 {
		t.Fatalf("DetailedLimits length = %d, want 1: %+v", len(got.CodexQuota.DetailedLimits), got.CodexQuota.DetailedLimits)
	}
	limit := got.CodexQuota.DetailedLimits[0]
	if limit.LimitID != "codex_bengalfox" || limit.LimitName != "GPT-5.3-Codex-Spark" {
		t.Fatalf("unexpected detailed limit identity: %+v", limit)
	}
	if limit.PrimaryUsedPercentValue() != 0.25 {
		t.Fatalf("detailed primary used percent = %v, want 0.25", limit.PrimaryUsedPercentValue())
	}
	if limit.SecondaryUsedPercentValue() != 1.5 {
		t.Fatalf("detailed secondary used percent = %v, want 1.5", limit.SecondaryUsedPercentValue())
	}
}

func TestUpdateFromHeadersForChannel_ParsesDetailedCodexQuotaFamilies(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	headers := http.Header{}
	headers.Set("X-Codex-Active-Limit", "premium")
	headers.Set("X-Codex-Plan-Type", "pro")
	headers.Set("X-Codex-Primary-Used-Percent", "58")
	headers.Set("X-Codex-Secondary-Used-Percent", "93")
	headers.Set("X-Codex-Bengalfox-Limit-Name", "GPT-5.3-Codex-Spark")
	headers.Set("X-Codex-Bengalfox-Primary-Used-Percent", "0")
	headers.Set("X-Codex-Bengalfox-Primary-Window-Minutes", "300")
	headers.Set("X-Codex-Bengalfox-Primary-Reset-At", "1779190352")
	headers.Set("X-Codex-Bengalfox-Primary-Reset-After-Seconds", "18000")
	headers.Set("X-Codex-Bengalfox-Secondary-Used-Percent", "1")
	headers.Set("X-Codex-Bengalfox-Secondary-Window-Minutes", "10080")
	headers.Set("X-Codex-Bengalfox-Secondary-Reset-At", "1779688894")
	headers.Set("X-Codex-Bengalfox-Secondary-Reset-After-Seconds", "516543")
	headers.Set("X-Ratelimit-Limit", "200")
	headers.Set("X-Ratelimit-Remaining", "199")
	headers.Set("X-Ratelimit-Reset", "1779172356")

	m.UpdateFromHeadersForChannel(5, "oauth-stable-a", "oauth-a", headers)

	got := m.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil {
		t.Fatalf("expected codex quota payload, got %+v", got)
	}
	if got.CodexQuota.ActiveLimit != "premium" {
		t.Fatalf("ActiveLimit = %q, want premium", got.CodexQuota.ActiveLimit)
	}
	if len(got.CodexQuota.DetailedLimits) != 1 {
		t.Fatalf("DetailedLimits length = %d, want 1: %+v", len(got.CodexQuota.DetailedLimits), got.CodexQuota.DetailedLimits)
	}
	limit := got.CodexQuota.DetailedLimits[0]
	if limit.LimitID != "codex_bengalfox" {
		t.Fatalf("LimitID = %q, want codex_bengalfox", limit.LimitID)
	}
	if limit.LimitName != "GPT-5.3-Codex-Spark" {
		t.Fatalf("LimitName = %q, want GPT-5.3-Codex-Spark", limit.LimitName)
	}
	if limit.PrimaryUsedPercent != 0 || limit.PrimaryWindowMinutes != 300 || limit.PrimaryResetAt.Unix() != 1779190352 || limit.PrimaryResetAfterSeconds != 18000 {
		t.Fatalf("unexpected primary detailed limit: %+v", limit)
	}
	if limit.SecondaryUsedPercent != 1 || limit.SecondaryWindowMinutes != 10080 || limit.SecondaryResetAt.Unix() != 1779688894 || limit.SecondaryResetAfterSeconds != 516543 {
		t.Fatalf("unexpected secondary detailed limit: %+v", limit)
	}
	if got.RateLimit == nil {
		t.Fatalf("expected generic rate limit payload")
	}
	if got.RateLimit.LimitRequests != 200 || got.RateLimit.RemainingRequests != 199 || got.RateLimit.ResetRequests.Unix() != 1779172356 {
		t.Fatalf("unexpected generic rate limit: %+v", got.RateLimit)
	}
}

func TestParseCodexUsagePayload_MapsUsageWindowsToQuotaInfo(t *testing.T) {
	payload := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window": {
				"used_percent": 36.5,
				"limit_window_seconds": 18000,
				"reset_at": 1893456000
			},
			"secondary_window": {
				"used_percent": 27.4,
				"limit_window_seconds": 604800,
				"reset_at": 1894060800
			}
		}
	}`)

	got, err := ParseCodexUsagePayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexUsagePayload returned error: %v", err)
	}
	if got.PlanType != "plus" {
		t.Fatalf("PlanType = %q, want plus", got.PlanType)
	}
	if got.PrimaryUsedPercentValue() != 36.5 {
		t.Fatalf("PrimaryUsedPercentValue = %v, want 36.5", got.PrimaryUsedPercentValue())
	}
	if got.PrimaryWindowMinutes != 300 {
		t.Fatalf("PrimaryWindowMinutes = %d, want 300", got.PrimaryWindowMinutes)
	}
	if got.PrimaryResetAt.Unix() != 1893456000 {
		t.Fatalf("PrimaryResetAt = %d, want 1893456000", got.PrimaryResetAt.Unix())
	}
	if got.SecondaryUsedPercentValue() != 27.4 {
		t.Fatalf("SecondaryUsedPercentValue = %v, want 27.4", got.SecondaryUsedPercentValue())
	}
	if got.SecondaryWindowMinutes != 10080 {
		t.Fatalf("SecondaryWindowMinutes = %d, want 10080", got.SecondaryWindowMinutes)
	}
	if got.SecondaryResetAt.Unix() != 1894060800 {
		t.Fatalf("SecondaryResetAt = %d, want 1894060800", got.SecondaryResetAt.Unix())
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("UpdatedAt should be set")
	}
}

func TestParseCodexUsagePayload_IdentifiesWindowsByDuration(t *testing.T) {
	payload := []byte(`{
		"rate_limit": {
			"primary_window": {
				"used_percent": 72,
				"limit_window_seconds": 604800
			},
			"secondary_window": {
				"used_percent": 19,
				"limit_window_seconds": 18000
			}
		},
		"credits": {
			"has_credits": true,
			"unlimited": false,
			"balance": "12.34"
		}
	}`)

	got, err := ParseCodexUsagePayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexUsagePayload returned error: %v", err)
	}
	if got.PrimaryUsedPercent != 19 {
		t.Fatalf("PrimaryUsedPercent = %d, want 19", got.PrimaryUsedPercent)
	}
	if got.SecondaryUsedPercent != 72 {
		t.Fatalf("SecondaryUsedPercent = %d, want 72", got.SecondaryUsedPercent)
	}
	if !got.CreditsHasCredits || got.CreditsUnlimited || got.CreditsBalance != "12.34" {
		t.Fatalf("unexpected credits: %+v", got)
	}
}

func TestParseCodexUsagePayload_ExtractsRateLimitResetCredits(t *testing.T) {
	payload := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window": {"used_percent": 99, "limit_window_seconds": 18000}
		},
		"rate_limit_reset_credits": {
			"available_count": 2,
			"credits": [
				{
					"id": "credit-a",
					"created_at": "2026-07-01T12:00:00Z",
					"expires_at": "2026-07-08T12:00:00Z"
				}
			]
		}
	}`)

	got, err := ParseCodexUsagePayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexUsagePayload returned error: %v", err)
	}
	if got.RateLimitResetCredits == nil {
		t.Fatalf("RateLimitResetCredits = nil, want summary")
	}
	if got.RateLimitResetCredits.AvailableCount != 2 {
		t.Fatalf("AvailableCount = %d, want 2", got.RateLimitResetCredits.AvailableCount)
	}
	if len(got.RateLimitResetCredits.Credits) != 1 {
		t.Fatalf("Credits length = %d, want 1", len(got.RateLimitResetCredits.Credits))
	}
	credit := got.RateLimitResetCredits.Credits[0]
	if credit.ID != "credit-a" {
		t.Fatalf("credit ID = %q, want credit-a", credit.ID)
	}
	if credit.CreatedAt == nil || credit.CreatedAt.Format(time.RFC3339) != "2026-07-01T12:00:00Z" {
		t.Fatalf("CreatedAt = %v, want 2026-07-01T12:00:00Z", credit.CreatedAt)
	}
	if credit.ExpiresAt == nil || credit.ExpiresAt.Format(time.RFC3339) != "2026-07-08T12:00:00Z" {
		t.Fatalf("ExpiresAt = %v, want 2026-07-08T12:00:00Z", credit.ExpiresAt)
	}
}

func TestParseCodexRateLimitResetCreditsPayload_ExtractsDetailedCreditMetadata(t *testing.T) {
	payload := []byte(`{
		"available_count": 1,
		"total_earned_count": 2,
		"credits": [
			{
				"id": "credit-a",
				"title": "Full reset (Weekly + 5 hours)",
				"granted_at": "2026-07-01T12:00:00Z",
				"expires_at": "2026-07-08T12:00:00Z"
			}
		]
	}`)

	got, err := ParseCodexRateLimitResetCreditsPayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexRateLimitResetCreditsPayload returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("reset credits = nil, want summary")
	}
	if got.AvailableCount != 1 {
		t.Fatalf("AvailableCount = %d, want 1", got.AvailableCount)
	}
	if got.TotalEarnedCount != 2 {
		t.Fatalf("TotalEarnedCount = %d, want 2", got.TotalEarnedCount)
	}
	if len(got.Credits) != 1 {
		t.Fatalf("Credits length = %d, want 1", len(got.Credits))
	}
	credit := got.Credits[0]
	if credit.Title != "Full reset (Weekly + 5 hours)" {
		t.Fatalf("Title = %q, want Full reset (Weekly + 5 hours)", credit.Title)
	}
	if credit.CreatedAt == nil || credit.CreatedAt.Format(time.RFC3339) != "2026-07-01T12:00:00Z" {
		t.Fatalf("CreatedAt = %v, want granted timestamp", credit.CreatedAt)
	}
	if credit.ExpiresAt == nil || credit.ExpiresAt.Format(time.RFC3339) != "2026-07-08T12:00:00Z" {
		t.Fatalf("ExpiresAt = %v, want expiry timestamp", credit.ExpiresAt)
	}
}

func TestParseCodexRateLimitResetCreditsPayload_DerivesTotalFromCreditsWhenMissing(t *testing.T) {
	payload := []byte(`{
		"available_count": 1,
		"credits": [
			{"id": "credit-a", "granted_at": "2026-07-01T12:00:00Z", "expires_at": "2026-07-08T12:00:00Z"},
			{"id": "credit-b", "granted_at": "2026-07-02T12:00:00Z", "expires_at": "2026-07-09T12:00:00Z"},
			{"id": "credit-c", "granted_at": "2026-07-03T12:00:00Z", "expires_at": "2026-07-10T12:00:00Z"}
		]
	}`)

	got, err := ParseCodexRateLimitResetCreditsPayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexRateLimitResetCreditsPayload returned error: %v", err)
	}
	if got.AvailableCount != 1 {
		t.Fatalf("AvailableCount = %d, want explicit available count 1", got.AvailableCount)
	}
	if got.TotalEarnedCount != 3 {
		t.Fatalf("TotalEarnedCount = %d, want derived total 3", got.TotalEarnedCount)
	}
	if len(got.Credits) != 3 {
		t.Fatalf("Credits length = %d, want 3", len(got.Credits))
	}
}

func TestParseCodexUsagePayload_ExtractsAdditionalRateLimits(t *testing.T) {
	payload := []byte(`{
		"plan_type": "pro",
		"rate_limit": {
			"primary_window": {"used_percent": 35, "limit_window_seconds": 18000, "reset_at": 1779203908},
			"secondary_window": {"used_percent": 11, "limit_window_seconds": 604800, "reset_at": 1779688894}
		},
		"additional_rate_limits": [
			{
				"limit_name": "GPT-5.3-Codex-Spark",
				"metered_feature": "codex_bengalfox",
				"rate_limit": {
					"allowed": true,
					"limit_reached": false,
					"primary_window": {
						"used_percent": 32,
						"limit_window_seconds": 18000,
						"reset_after_seconds": 8766,
						"reset_at": 1779203908
					},
					"secondary_window": {
						"used_percent": 11,
						"limit_window_seconds": 604800,
						"reset_after_seconds": 493753,
						"reset_at": 1779688895
					}
				}
			}
		]
	}`)

	got, err := ParseCodexUsagePayload(payload)
	if err != nil {
		t.Fatalf("ParseCodexUsagePayload returned error: %v", err)
	}
	if len(got.DetailedLimits) != 1 {
		t.Fatalf("DetailedLimits length = %d, want 1: %+v", len(got.DetailedLimits), got.DetailedLimits)
	}
	limit := got.DetailedLimits[0]
	if limit.LimitID != "codex_bengalfox" {
		t.Fatalf("LimitID = %q, want codex_bengalfox", limit.LimitID)
	}
	if limit.LimitName != "GPT-5.3-Codex-Spark" {
		t.Fatalf("LimitName = %q, want GPT-5.3-Codex-Spark", limit.LimitName)
	}
	if limit.PrimaryUsedPercent != 32 || limit.PrimaryWindowMinutes != 300 || limit.PrimaryResetAt.Unix() != 1779203908 || limit.PrimaryResetAfterSeconds != 8766 {
		t.Fatalf("unexpected primary detailed limit: %+v", limit)
	}
	if limit.SecondaryUsedPercent != 11 || limit.SecondaryWindowMinutes != 10080 || limit.SecondaryResetAt.Unix() != 1779688895 || limit.SecondaryResetAfterSeconds != 493753 {
		t.Fatalf("unexpected secondary detailed limit: %+v", limit)
	}
}

func TestRequestLogAdapter_PersistsCodexResetCreditsAcrossReload(t *testing.T) {
	requestLogManager, err := requestlog.NewManager(filepath.Join(t.TempDir(), "request_logs.db"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		if err := requestLogManager.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	createdAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	writer := &Manager{quotas: make(map[int]*QuotaStatus)}
	writer.SetPersister(NewRequestLogAdapter(requestLogManager))
	if err := writer.UpdateCodexQuotaForChannel(5, "oauth-stable-a", "oauth-a", &CodexQuotaInfo{
		PlanType:           "pro",
		PrimaryUsedPercent: 12,
		RateLimitResetCredits: &CodexRateLimitResetCreditsInfo{
			AvailableCount: 2,
			CreatedAt:      &createdAt,
			ExpiresAt:      &expiresAt,
			Credits: []CodexRateLimitResetCredit{
				{ID: "credit-a", CreatedAt: &createdAt, ExpiresAt: &expiresAt},
			},
		},
	}); err != nil {
		t.Fatalf("UpdateCodexQuotaForChannel failed: %v", err)
	}

	reader := &Manager{quotas: make(map[int]*QuotaStatus)}
	reader.SetPersister(NewRequestLogAdapter(requestLogManager))

	got := reader.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil || got.CodexQuota.RateLimitResetCredits == nil {
		t.Fatalf("expected persisted reset credits after reload, got %+v", got)
	}
	resetCredits := got.CodexQuota.RateLimitResetCredits
	if resetCredits.AvailableCount != 2 {
		t.Fatalf("AvailableCount = %d, want 2", resetCredits.AvailableCount)
	}
	if resetCredits.CreatedAt == nil || !resetCredits.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", resetCredits.CreatedAt, createdAt)
	}
	if resetCredits.ExpiresAt == nil || !resetCredits.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", resetCredits.ExpiresAt, expiresAt)
	}
	if len(resetCredits.Credits) != 1 || resetCredits.Credits[0].ID != "credit-a" {
		t.Fatalf("Credits = %+v, want credit-a", resetCredits.Credits)
	}
}

func TestUpdateCodexQuotaForChannel_PreservesDetailedLimitsAndActiveLimitWhenRefreshLacksThem(t *testing.T) {
	m := &Manager{quotas: make(map[int]*QuotaStatus)}

	exact32 := 32.0
	m.quotas[5] = &QuotaStatus{
		ChannelID:       5,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			PlanType:           "pro",
			ActiveLimit:        "premium",
			PrimaryUsedPercent: 35,
			DetailedLimits: []CodexQuotaLimitInfo{
				{
					LimitID:                 "codex_bengalfox",
					LimitName:               "GPT-5.3-Codex-Spark",
					PrimaryUsedPercent:      32,
					PrimaryUsedPercentExact: &exact32,
				},
			},
			UpdatedAt: time.Now().Add(-time.Minute),
		},
	}

	refreshed := &CodexQuotaInfo{
		PlanType:           "pro",
		PrimaryUsedPercent: 40,
	}

	m.UpdateCodexQuotaForChannel(5, "oauth-stable-a", "oauth-a", refreshed)

	got := m.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil {
		t.Fatalf("expected codex quota payload, got %+v", got)
	}
	if got.CodexQuota.PrimaryUsedPercent != 40 {
		t.Fatalf("PrimaryUsedPercent = %d, want refreshed 40", got.CodexQuota.PrimaryUsedPercent)
	}
	if got.CodexQuota.ActiveLimit != "premium" {
		t.Fatalf("ActiveLimit = %q, want preserved premium", got.CodexQuota.ActiveLimit)
	}
	if len(got.CodexQuota.DetailedLimits) != 1 || got.CodexQuota.DetailedLimits[0].LimitID != "codex_bengalfox" {
		t.Fatalf("DetailedLimits = %+v, want preserved bengalfox", got.CodexQuota.DetailedLimits)
	}
}

func TestUpdateCodexQuotaForChannel_ReturnsPersistError(t *testing.T) {
	persistErr := fmt.Errorf("persist failed")
	m := &Manager{quotas: make(map[int]*QuotaStatus)}
	m.SetPersister(&testQuotaPersister{err: persistErr})

	err := m.UpdateCodexQuotaForChannel(5, "oauth-stable-a", "oauth-a", &CodexQuotaInfo{
		PlanType:           "plus",
		PrimaryUsedPercent: 12,
	})
	if err == nil {
		t.Fatalf("expected persist error, got nil")
	}
	if err.Error() != persistErr.Error() {
		t.Fatalf("error = %v, want %v", err, persistErr)
	}
}

func TestUpdateCodexQuotaForChannel_RefreshDetailedLimitsOverrideExisting(t *testing.T) {
	m := &Manager{quotas: make(map[int]*QuotaStatus)}

	m.quotas[5] = &QuotaStatus{
		ChannelID:       5,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "oauth-a",
		CodexQuota: &CodexQuotaInfo{
			DetailedLimits: []CodexQuotaLimitInfo{
				{LimitID: "codex_bengalfox", PrimaryUsedPercent: 32},
			},
			UpdatedAt: time.Now().Add(-time.Minute),
		},
	}

	refreshed := &CodexQuotaInfo{
		DetailedLimits: []CodexQuotaLimitInfo{
			{LimitID: "codex_bengalfox", PrimaryUsedPercent: 55},
		},
	}

	m.UpdateCodexQuotaForChannel(5, "oauth-stable-a", "oauth-a", refreshed)

	got := m.GetStatusForChannel(5, "oauth-stable-a", "oauth-a")
	if got == nil || got.CodexQuota == nil || len(got.CodexQuota.DetailedLimits) != 1 {
		t.Fatalf("unexpected status: %+v", got)
	}
	if got.CodexQuota.DetailedLimits[0].PrimaryUsedPercent != 55 {
		t.Fatalf("DetailedLimits[0].PrimaryUsedPercent = %d, want refreshed 55", got.CodexQuota.DetailedLimits[0].PrimaryUsedPercent)
	}
}

func TestSetExceededForChannel_StableIDUpdatesExistingEntryWithoutDestroyingCurrentIndex(t *testing.T) {
	m := &Manager{
		quotas: make(map[int]*QuotaStatus),
	}

	m.quotas[1] = &QuotaStatus{
		ChannelID:       1,
		ChannelStableID: "oauth-stable-b",
		ChannelName:     "oauth-b",
	}
	m.quotas[4] = &QuotaStatus{
		ChannelID:       4,
		ChannelStableID: "oauth-stable-a",
		ChannelName:     "oauth-a",
	}

	m.SetExceededForChannel(1, "oauth-stable-a", "oauth-a", "rate_limit_exceeded", time.Minute)

	currentIndexStatus := m.quotas[1]
	if currentIndexStatus == nil || currentIndexStatus.ChannelStableID != "oauth-stable-b" {
		t.Fatalf("expected current index quota to remain oauth-stable-b, got %+v", currentIndexStatus)
	}
	if currentIndexStatus.IsExceeded {
		t.Fatalf("expected oauth-stable-b not to be marked exceeded")
	}

	exceededStatus := m.quotas[4]
	if exceededStatus == nil || exceededStatus.ChannelStableID != "oauth-stable-a" {
		t.Fatalf("expected existing stable-id quota row to be updated, got %+v", exceededStatus)
	}
	if !exceededStatus.IsExceeded || exceededStatus.ExceededReason != "rate_limit_exceeded" {
		t.Fatalf("expected oauth-stable-a to be marked exceeded, got %+v", exceededStatus)
	}
	if exceededStatus.ChannelID != 1 || exceededStatus.ChannelName != "oauth-a" {
		t.Fatalf("expected exceeded stable-id row to carry current identity, got %+v", exceededStatus)
	}
}
