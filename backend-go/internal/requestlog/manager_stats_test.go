package requestlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGetStats_ExcludesRetryWaitAuditRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_stats.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)

	mustAddLog(t, manager, &RequestLog{
		Status:                   StatusCompleted,
		InitialTime:              base,
		DurationMs:               1200,
		Type:                     "responses",
		ProviderName:             "messages-primary",
		Model:                    "gpt-5",
		InputTokens:              100,
		OutputTokens:             50,
		CacheCreationInputTokens: 20,
		CacheReadInputTokens:     10,
		Price:                    1.25,
		HTTPStatus:               200,
		ChannelID:                1,
		ChannelName:              "messages-primary",
		Endpoint:                 "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusError,
		InitialTime:  base.Add(1 * time.Hour),
		DurationMs:   2400,
		Type:         "responses",
		ProviderName: "messages-primary",
		Model:        "gpt-5",
		InputTokens:  30,
		OutputTokens: 5,
		Price:        0.75,
		HTTPStatus:   429,
		ChannelID:    1,
		ChannelName:  "messages-primary",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusRetryWait,
		InitialTime:  base.Add(2 * time.Hour),
		DurationMs:   9999,
		Type:         "responses",
		ProviderName: "messages-primary",
		Model:        "gpt-5",
		InputTokens:  999,
		OutputTokens: 999,
		Price:        9.99,
		HTTPStatus:   429,
		ChannelID:    1,
		ChannelName:  "messages-primary",
		Endpoint:     "/v1/messages",
	})

	stats, err := manager.GetStats(&RequestLogFilter{Endpoint: "/v1/messages"})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalRequests != 2 {
		t.Fatalf("expected 2 terminal requests, got %d", stats.TotalRequests)
	}
	if stats.TotalSuccess != 1 {
		t.Fatalf("expected 1 success, got %d", stats.TotalSuccess)
	}
	if stats.TotalFailure != 1 {
		t.Fatalf("expected 1 failure, got %d", stats.TotalFailure)
	}
	if stats.TotalTokens.InputTokens != 130 {
		t.Fatalf("expected input tokens to exclude retry_wait row, got %d", stats.TotalTokens.InputTokens)
	}
	if stats.TotalTokens.OutputTokens != 55 {
		t.Fatalf("expected output tokens to exclude retry_wait row, got %d", stats.TotalTokens.OutputTokens)
	}
	if stats.TotalCost != 2.0 {
		t.Fatalf("expected total cost 2.0, got %v", stats.TotalCost)
	}

	provider := stats.ByProvider["messages-primary"]
	if provider.Count != 2 || provider.Success != 1 || provider.Failure != 1 {
		t.Fatalf("unexpected provider stats: %+v", provider)
	}
}

func TestGetDailyStats_ExcludesRetryWaitAndRespectsEndpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_daily_stats.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)

	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base,
		DurationMs:   1000,
		Type:         "claude",
		ProviderName: "messages-primary",
		Model:        "claude-sonnet",
		InputTokens:  10,
		OutputTokens: 20,
		Price:        0.25,
		HTTPStatus:   200,
		ChannelID:    1,
		ChannelName:  "messages-primary",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusRetryWait,
		InitialTime:  base.Add(2 * time.Hour),
		DurationMs:   5000,
		Type:         "claude",
		ProviderName: "messages-primary",
		Model:        "claude-sonnet",
		InputTokens:  777,
		OutputTokens: 0,
		Price:        7.77,
		HTTPStatus:   429,
		ChannelID:    1,
		ChannelName:  "messages-primary",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusError,
		InitialTime:  base.Add(26 * time.Hour),
		DurationMs:   3000,
		Type:         "claude",
		ProviderName: "messages-primary",
		Model:        "claude-sonnet",
		InputTokens:  15,
		OutputTokens: 5,
		Price:        0.5,
		HTTPStatus:   500,
		ChannelID:    1,
		ChannelName:  "messages-primary",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base.Add(30 * time.Hour),
		DurationMs:   1500,
		Type:         "responses",
		ProviderName: "responses-primary",
		Model:        "gpt-5",
		InputTokens:  99,
		OutputTokens: 1,
		Price:        1.5,
		HTTPStatus:   200,
		ChannelID:    2,
		ChannelName:  "responses-primary",
		Endpoint:     "/v1/responses",
	})

	resp, err := manager.GetDailyStats(base.Add(-1*time.Hour), base.Add(48*time.Hour), "/v1/messages")
	if err != nil {
		t.Fatalf("GetDailyStats failed: %v", err)
	}

	if len(resp.DataPoints) != 2 {
		t.Fatalf("expected 2 daily data points for messages endpoint, got %d", len(resp.DataPoints))
	}

	day1 := resp.DataPoints[0]
	if day1.Date != "2026-03-10" || day1.Requests != 1 || day1.Success != 1 || day1.Failure != 0 {
		t.Fatalf("unexpected day1 aggregate: %+v", day1)
	}
	if day1.InputTokens != 10 || day1.Cost != 0.25 {
		t.Fatalf("expected day1 totals to exclude retry_wait row, got %+v", day1)
	}

	day2 := resp.DataPoints[1]
	if day2.Date != "2026-03-11" || day2.Requests != 1 || day2.Success != 0 || day2.Failure != 1 {
		t.Fatalf("unexpected day2 aggregate: %+v", day2)
	}
	if day2.InputTokens != 15 || day2.Cost != 0.5 {
		t.Fatalf("expected day2 totals for messages endpoint only, got %+v", day2)
	}
}
