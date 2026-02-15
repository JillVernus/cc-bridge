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
			Success:     i%2 == 0,
			StatusCode:  i + 100,
			Timestamp:   time.Unix(int64(i), 0),
			Model:       "test-model",
			ChannelName: "test-channel",
		})
	}
	calls[10].StatusCode = -1

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
