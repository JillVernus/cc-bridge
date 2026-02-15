package metrics

import (
	"testing"
	"time"
)

func TestSeedRecentCalls_TrimsAndSanitizes(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	calls := make([]RecentCall, 0, 25)
	for i := 0; i < 25; i++ {
		calls = append(calls, RecentCall{
			Success:           i%2 == 0,
			StatusCode:        i + 100,
			Timestamp:         time.Unix(int64(i), 0),
			Model:             "test-model",
			ChannelName:       "test-channel",
			RoutedChannelName: " test-route ",
		})
	}
	calls[10].StatusCode = -1
	calls[12].RoutedChannelName = "test-channel"

	m.SeedRecentCalls(3, calls)

	metrics := m.GetMetrics(3)
	if metrics == nil {
		t.Fatalf("expected metrics to be created")
	}
	if got := len(metrics.RecentCalls); got != 20 {
		t.Fatalf("expected 20 seeded calls, got %d", got)
	}

	// Keep latest 20 entries.
	if metrics.RecentCalls[0].StatusCode != calls[5].StatusCode {
		t.Fatalf("expected first seeded status code %d, got %d", calls[5].StatusCode, metrics.RecentCalls[0].StatusCode)
	}
	if metrics.RecentCalls[0].Model != calls[5].Model {
		t.Fatalf("expected seeded model %q, got %q", calls[5].Model, metrics.RecentCalls[0].Model)
	}
	if metrics.RecentCalls[0].ChannelName != calls[5].ChannelName {
		t.Fatalf("expected seeded channel name %q, got %q", calls[5].ChannelName, metrics.RecentCalls[0].ChannelName)
	}
	if metrics.RecentCalls[0].RoutedChannelName != "test-route" {
		t.Fatalf("expected seeded routed channel name to be trimmed, got %q", metrics.RecentCalls[0].RoutedChannelName)
	}
	if metrics.RecentCalls[7].RoutedChannelName != "" {
		t.Fatalf("expected duplicate routed channel name to be deduplicated, got %q", metrics.RecentCalls[7].RoutedChannelName)
	}

	// Negative status code must be sanitized to 0.
	if metrics.RecentCalls[5].StatusCode != 0 {
		t.Fatalf("expected sanitized status code 0, got %d", metrics.RecentCalls[5].StatusCode)
	}
	if metrics.RecentCalls[0].Timestamp.IsZero() {
		t.Fatalf("expected seeded timestamp to be preserved")
	}

	// Startup restore should not alter runtime counters.
	if metrics.RequestCount != 0 || metrics.SuccessCount != 0 || metrics.FailureCount != 0 {
		t.Fatalf("expected runtime counters untouched, got request=%d success=%d failure=%d", metrics.RequestCount, metrics.SuccessCount, metrics.FailureCount)
	}
}

func TestSeedRecentCalls_EmptyResetsSlots(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	m.RecordSuccessWithStatusDetail(1, 200, "model-a", "channel-a")
	if got := len(m.GetMetrics(1).RecentCalls); got != 1 {
		t.Fatalf("expected one recent call before reset, got %d", got)
	}
	if m.GetMetrics(1).RecentCalls[0].Timestamp.IsZero() {
		t.Fatalf("expected recorded call to include timestamp")
	}
	if m.GetMetrics(1).RecentCalls[0].Model != "model-a" || m.GetMetrics(1).RecentCalls[0].ChannelName != "channel-a" {
		t.Fatalf("expected recorded call to preserve model/channel metadata")
	}

	m.SeedRecentCalls(1, nil)

	metrics := m.GetMetrics(1)
	if metrics == nil {
		t.Fatalf("expected metrics to exist after reset")
	}
	if got := len(metrics.RecentCalls); got != 0 {
		t.Fatalf("expected recent slots reset to empty, got %d", got)
	}
}

func TestReconcileChannelIdentity_ResetsOnIndexReuse(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	m.RecordFailureWithStatusDetail(2, 500, "model-old", "old-channel")
	before := m.GetMetrics(2)
	if before == nil {
		t.Fatalf("expected metrics before reconcile")
	}
	if before.RequestCount != 1 || before.FailureCount != 1 || len(before.RecentCalls) != 1 {
		t.Fatalf("expected precondition data to exist before reconcile")
	}

	m.ReconcileChannelIdentity(2, "channel-new-id", "new-channel")

	after := m.GetMetrics(2)
	if after == nil {
		t.Fatalf("expected metrics after reconcile")
	}
	if after.RequestCount != 0 || after.SuccessCount != 0 || after.FailureCount != 0 {
		t.Fatalf("expected counters reset after identity mismatch, got request=%d success=%d failure=%d", after.RequestCount, after.SuccessCount, after.FailureCount)
	}
	if len(after.RecentCalls) != 0 {
		t.Fatalf("expected recent slots reset after identity mismatch, got %d", len(after.RecentCalls))
	}
}

func TestReconcileChannelIdentity_InfersSeededChannelName(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	m.SeedRecentCalls(7, []RecentCall{
		{Success: true, StatusCode: 200, Timestamp: time.Unix(1, 0), ChannelName: "old-seeded"},
	})
	if got := len(m.GetMetrics(7).RecentCalls); got != 1 {
		t.Fatalf("expected seeded slot before reconcile, got %d", got)
	}

	m.ReconcileChannelIdentity(7, "channel-new-id", "new-seeded")

	after := m.GetMetrics(7)
	if after == nil {
		t.Fatalf("expected metrics after reconcile")
	}
	if len(after.RecentCalls) != 0 {
		t.Fatalf("expected seeded slots reset after identity mismatch, got %d", len(after.RecentCalls))
	}

	m.SeedRecentCalls(7, []RecentCall{
		{Success: true, StatusCode: 200, Timestamp: time.Unix(2, 0), ChannelName: "  New-Seeded  "},
	})
	m.ReconcileChannelIdentity(7, "channel-new-id", "new-seeded")

	matched := m.GetMetrics(7)
	if matched == nil {
		t.Fatalf("expected metrics after matching reconcile")
	}
	if len(matched.RecentCalls) != 1 {
		t.Fatalf("expected slots preserved when identity matches, got %d", len(matched.RecentCalls))
	}
}

func TestReconcileChannelIdentity_PrefersStableIDOverName(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	m.RecordSuccessWithStatusDetail(9, 200, "model-a", "old-name")
	m.ReconcileChannelIdentity(9, "stable-id", "old-name")

	before := m.GetMetrics(9)
	if before == nil || len(before.RecentCalls) != 1 {
		t.Fatalf("expected one slot before rename reconcile")
	}

	// Name changed but ID remains stable: metrics should be preserved.
	m.ReconcileChannelIdentity(9, "stable-id", "new-name")

	after := m.GetMetrics(9)
	if after == nil {
		t.Fatalf("expected metrics after reconcile")
	}
	if after.RequestCount != 1 || after.SuccessCount != 1 {
		t.Fatalf("expected counters preserved when stable ID matches")
	}
	if len(after.RecentCalls) != 1 {
		t.Fatalf("expected slots preserved when stable ID matches, got %d", len(after.RecentCalls))
	}
}

func TestRecordRecentCall_RoutedChannelMetadata(t *testing.T) {
	m := NewMetricsManager()
	t.Cleanup(m.Stop)

	m.RecordSuccessWithStatusDetail(4, 200, "model-x", "composite-a", "target-a")

	metrics := m.GetMetrics(4)
	if metrics == nil || len(metrics.RecentCalls) != 1 {
		t.Fatalf("expected one recent call, got %+v", metrics)
	}
	call := metrics.RecentCalls[0]
	if call.ChannelName != "composite-a" {
		t.Fatalf("expected owner channel name %q, got %q", "composite-a", call.ChannelName)
	}
	if call.RoutedChannelName != "target-a" {
		t.Fatalf("expected routed channel name %q, got %q", "target-a", call.RoutedChannelName)
	}

	m.RecordFailureWithStatusDetail(4, 503, "model-x", "channel-a", "channel-a")
	metrics = m.GetMetrics(4)
	if metrics == nil || len(metrics.RecentCalls) != 2 {
		t.Fatalf("expected two recent calls, got %+v", metrics)
	}
	if metrics.RecentCalls[1].RoutedChannelName != "" {
		t.Fatalf("expected duplicate routed channel name to be omitted, got %q", metrics.RecentCalls[1].RoutedChannelName)
	}
}
