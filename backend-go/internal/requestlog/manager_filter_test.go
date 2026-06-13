package requestlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGetRecent_FilterByChannelMatchesChannelColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_filter.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base,
		Type:         "claude",
		ProviderName: "legacy-provider-a",
		Model:        "claude-sonnet-4",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    1,
		ChannelName:  "messages-channel-a",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base.Add(time.Second),
		Type:         "claude",
		ProviderName: "legacy-provider-b",
		Model:        "claude-sonnet-4",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    2,
		ChannelName:  "messages-channel-b",
		Endpoint:     "/v1/messages",
	})

	recent, err := manager.GetRecent(&RequestLogFilter{
		Channel: "messages-channel-a",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if recent.Total != 1 {
		t.Fatalf("filtered total = %d, want 1", recent.Total)
	}
	if len(recent.Requests) != 1 {
		t.Fatalf("filtered request count = %d, want 1", len(recent.Requests))
	}
	if got := recent.Requests[0].ChannelName; got != "messages-channel-a" {
		t.Fatalf("filtered channel = %q, want messages-channel-a", got)
	}
}
