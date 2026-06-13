package requestlog

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestGetStats_IncludesAverageTPSPerSummaryGroup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_stats.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	apiKeyID := int64(7)
	base := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	records := []*RequestLog{
		{
			Status:       StatusCompleted,
			InitialTime:  base,
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 100,
			DurationMs:   10000,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
			ClientID:     "client-a",
			SessionID:    "session-a",
			APIKeyID:     &apiKeyID,
		},
		{
			Status:       StatusCompleted,
			InitialTime:  base.Add(time.Second),
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 10,
			DurationMs:   100,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
			ClientID:     "client-a",
			SessionID:    "session-a",
			APIKeyID:     &apiKeyID,
		},
		{
			Status:       StatusCompleted,
			InitialTime:  base.Add(2 * time.Second),
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 999,
			DurationMs:   0,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
			ClientID:     "client-a",
			SessionID:    "session-a",
			APIKeyID:     &apiKeyID,
		},
	}
	for _, record := range records {
		mustAddLog(t, manager, record)
	}

	stats, err := manager.GetStats(&RequestLogFilter{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	payload, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats failed: %v", err)
	}
	var decoded struct {
		AvgTPS            float64                       `json:"avgTps"`
		AvgTPSSampleCount int64                         `json:"avgTpsSampleCount"`
		ByProvider        map[string]map[string]float64 `json:"byProvider"`
		ByModel           map[string]map[string]float64 `json:"byModel"`
		ByClient          map[string]map[string]float64 `json:"byClient"`
		BySession         map[string]map[string]float64 `json:"bySession"`
		ByAPIKey          map[string]map[string]float64 `json:"byApiKey"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal stats failed: %v", err)
	}

	wantAvgTPS := 55.0
	if math.Abs(decoded.AvgTPS-wantAvgTPS) > 0.0001 {
		t.Fatalf("avgTps = %v, want %v", decoded.AvgTPS, wantAvgTPS)
	}
	if decoded.AvgTPSSampleCount != 2 {
		t.Fatalf("avgTpsSampleCount = %d, want 2", decoded.AvgTPSSampleCount)
	}

	assertAvgTPS := func(name string, group map[string]map[string]float64, key string) {
		t.Helper()
		got, ok := group[key]["avgTps"]
		if !ok {
			t.Fatalf("%s[%q] missing avgTps in JSON payload: %#v", name, key, group[key])
		}
		if math.Abs(got-wantAvgTPS) > 0.0001 {
			t.Fatalf("%s[%q].avgTps = %v, want %v", name, key, got, wantAvgTPS)
		}
	}

	assertAvgTPS("byProvider", decoded.ByProvider, "channel-a")
	assertAvgTPS("byModel", decoded.ByModel, "model-a")
	assertAvgTPS("byClient", decoded.ByClient, "client-a")
	assertAvgTPS("bySession", decoded.BySession, "session-a")
	assertAvgTPS("byApiKey", decoded.ByAPIKey, "7")
}

func TestGetDailyStats_IncludesAverageTPSPerDay(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_daily_tps.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	records := []*RequestLog{
		{
			Status:       StatusCompleted,
			InitialTime:  base,
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 100,
			DurationMs:   10000,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
		},
		{
			Status:       StatusCompleted,
			InitialTime:  base.Add(time.Hour),
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 10,
			DurationMs:   100,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
		},
		{
			Status:       StatusCompleted,
			InitialTime:  base.Add(2 * time.Hour),
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 999,
			DurationMs:   0,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
		},
		{
			Status:       StatusCompleted,
			InitialTime:  base.AddDate(0, 0, 1),
			Type:         "claude",
			ProviderName: "channel-a",
			Model:        "model-a",
			OutputTokens: 20,
			DurationMs:   1000,
			HTTPStatus:   200,
			ChannelID:    1,
			ChannelName:  "channel-a",
			Endpoint:     "/v1/messages",
		},
	}
	for _, record := range records {
		mustAddLog(t, manager, record)
	}

	resp, err := manager.GetDailyStats(base.Add(-time.Minute), base.AddDate(0, 0, 2), "/v1/messages")
	if err != nil {
		t.Fatalf("GetDailyStats failed: %v", err)
	}
	if len(resp.DataPoints) != 2 {
		t.Fatalf("expected 2 daily data points, got %d", len(resp.DataPoints))
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal daily stats failed: %v", err)
	}
	var decoded struct {
		DataPoints []struct {
			Date              string  `json:"date"`
			AvgTPS            float64 `json:"avgTps"`
			AvgTPSSampleCount int64   `json:"avgTpsSampleCount"`
		} `json:"dataPoints"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal daily stats failed: %v", err)
	}

	if math.Abs(decoded.DataPoints[0].AvgTPS-55.0) > 0.0001 {
		t.Fatalf("%s avgTps = %v, want 55", decoded.DataPoints[0].Date, decoded.DataPoints[0].AvgTPS)
	}
	if decoded.DataPoints[0].AvgTPSSampleCount != 2 {
		t.Fatalf("%s avgTpsSampleCount = %d, want 2", decoded.DataPoints[0].Date, decoded.DataPoints[0].AvgTPSSampleCount)
	}
	if math.Abs(decoded.DataPoints[1].AvgTPS-20.0) > 0.0001 {
		t.Fatalf("%s avgTps = %v, want 20", decoded.DataPoints[1].Date, decoded.DataPoints[1].AvgTPS)
	}
	if decoded.DataPoints[1].AvgTPSSampleCount != 1 {
		t.Fatalf("%s avgTpsSampleCount = %d, want 1", decoded.DataPoints[1].Date, decoded.DataPoints[1].AvgTPSSampleCount)
	}
}

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
