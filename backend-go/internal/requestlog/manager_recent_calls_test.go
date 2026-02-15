package requestlog

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestGetRecentChannelCalls(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)

	// Messages channel 1: 25 calls -> should keep latest 20 (6..25)
	for i := 1; i <= 25; i++ {
		status := StatusCompleted
		httpStatus := 200 + i
		if i%2 == 1 {
			status = StatusError
			httpStatus = 500 + i
		}
		mustAddLog(t, manager, &RequestLog{
			Status:       status,
			InitialTime:  base.Add(time.Duration(i) * time.Second),
			Type:         "claude",
			ProviderName: "messages-ch1",
			Model:        "test-model",
			HTTPStatus:   httpStatus,
			Stream:       false,
			ChannelID:    1,
			ChannelName:  "messages-1",
			Endpoint:     "/v1/messages",
		})
	}

	// Messages channel 2: 3 calls
	for i := 1; i <= 3; i++ {
		mustAddLog(t, manager, &RequestLog{
			Status:       StatusCompleted,
			InitialTime:  base.Add(time.Duration(100+i) * time.Second),
			Type:         "claude",
			ProviderName: "messages-ch2",
			Model:        "test-model",
			HTTPStatus:   200,
			Stream:       false,
			ChannelID:    2,
			ChannelName:  "messages-2",
			Endpoint:     "/v1/messages",
		})
	}

	// Responses channel 7: 2 calls
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base.Add(200 * time.Second),
		Type:         "responses",
		ProviderName: "responses-ch7",
		Model:        "test-model",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    7,
		ChannelName:  "responses-7",
		Endpoint:     "/v1/responses",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusError,
		InitialTime:  base.Add(201 * time.Second),
		Type:         "responses",
		ProviderName: "responses-ch7",
		Model:        "test-model",
		HTTPStatus:   503,
		Stream:       false,
		ChannelID:    7,
		ChannelName:  "responses-7",
		Endpoint:     "/v1/responses",
	})

	// Gemini channel 9: 1 call
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base.Add(300 * time.Second),
		Type:         "gemini",
		ProviderName: "gemini-ch9",
		Model:        "test-model",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    9,
		ChannelName:  "gemini-9",
		Endpoint:     "/v1/gemini",
	})

	// Should be ignored by query (pending status)
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusPending,
		InitialTime:  base.Add(400 * time.Second),
		Type:         "claude",
		ProviderName: "ignored-pending",
		Model:        "test-model",
		HTTPStatus:   0,
		Stream:       false,
		ChannelID:    1,
		ChannelName:  "messages-1",
		Endpoint:     "/v1/messages",
	})

	calls, err := manager.GetRecentChannelCalls(20)
	if err != nil {
		t.Fatalf("GetRecentChannelCalls failed: %v", err)
	}

	grouped := make(map[string][]ChannelRecentCall)
	for _, call := range calls {
		key := fmt.Sprintf("%s#%d", call.Endpoint, call.ChannelID)
		grouped[key] = append(grouped[key], call)
	}

	msgCh1 := grouped["/v1/messages#1"]
	if len(msgCh1) != 20 {
		t.Fatalf("expected 20 calls for messages#1, got %d", len(msgCh1))
	}
	for idx, call := range msgCh1 {
		i := idx + 6 // latest 20 from 1..25
		wantSuccess := i%2 == 0
		wantStatus := 200 + i
		if !wantSuccess {
			wantStatus = 500 + i
		}
		if call.Timestamp.IsZero() {
			t.Fatalf("messages#1[%d] timestamp should not be zero", idx)
		}
		if idx > 0 && call.Timestamp.Before(msgCh1[idx-1].Timestamp) {
			t.Fatalf("messages#1 timestamps are not ordered ascending")
		}
		if call.Success != wantSuccess {
			t.Fatalf("messages#1[%d] success mismatch: got=%v want=%v", idx, call.Success, wantSuccess)
		}
		if call.HTTPStatus != wantStatus {
			t.Fatalf("messages#1[%d] status mismatch: got=%d want=%d", idx, call.HTTPStatus, wantStatus)
		}
		if call.Model != "test-model" {
			t.Fatalf("messages#1[%d] model mismatch: got=%q", idx, call.Model)
		}
		if call.ChannelName != "messages-1" {
			t.Fatalf("messages#1[%d] channel name mismatch: got=%q", idx, call.ChannelName)
		}
	}

	if got := len(grouped["/v1/messages#2"]); got != 3 {
		t.Fatalf("expected 3 calls for messages#2, got %d", got)
	}

	respCh7 := grouped["/v1/responses#7"]
	if len(respCh7) != 2 {
		t.Fatalf("expected 2 calls for responses#7, got %d", len(respCh7))
	}
	if !respCh7[0].Success || respCh7[0].HTTPStatus != 200 {
		t.Fatalf("unexpected first responses#7 call: %+v", respCh7[0])
	}
	if respCh7[1].Success || respCh7[1].HTTPStatus != 503 {
		t.Fatalf("unexpected second responses#7 call: %+v", respCh7[1])
	}
	if respCh7[0].Model != "test-model" || respCh7[0].ChannelName != "responses-7" {
		t.Fatalf("unexpected responses#7 metadata: %+v", respCh7[0])
	}

	geminiCh9 := grouped["/v1/gemini#9"]
	if len(geminiCh9) != 1 || !geminiCh9[0].Success || geminiCh9[0].HTTPStatus != 200 {
		t.Fatalf("unexpected gemini#9 calls: %+v", geminiCh9)
	}
	if geminiCh9[0].Model != "test-model" || geminiCh9[0].ChannelName != "gemini-9" {
		t.Fatalf("unexpected gemini#9 metadata: %+v", geminiCh9[0])
	}
}

func mustAddLog(t *testing.T, manager *Manager, record *RequestLog) {
	t.Helper()

	if record.Status != StatusPending {
		record.CompleteTime = record.InitialTime
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add log record: %v", err)
	}
}
