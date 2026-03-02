package requestlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestLog_FirstTokenFields_RoundTripAndSSEFetches(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_first_token.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	start := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	pending := &RequestLog{
		Status:       StatusPending,
		InitialTime:  start,
		Type:         "claude",
		ProviderName: "anthropic",
		Model:        "claude-sonnet-4",
		Stream:       true,
		ChannelID:    1,
		ChannelName:  "messages-1",
		Endpoint:     "/v1/messages",
	}
	if err := manager.Add(pending); err != nil {
		t.Fatalf("failed to add pending record: %v", err)
	}

	firstTokenTime := start.Add(120 * time.Millisecond)
	update := &RequestLog{
		Status:               StatusCompleted,
		FirstTokenTime:       &firstTokenTime,
		FirstTokenDurationMs: 120,
		CompleteTime:         start.Add(2 * time.Second),
		DurationMs:           2000,
		Type:                 "claude",
		ProviderName:         "anthropic",
		ResponseModel:        "claude-sonnet-4",
		InputTokens:          10,
		OutputTokens:         20,
		HTTPStatus:           200,
		ChannelID:            1,
		ChannelName:          "messages-1",
	}
	if err := manager.Update(pending.ID, update); err != nil {
		t.Fatalf("failed to update record: %v", err)
	}

	got, err := manager.GetByID(pending.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected record from GetByID")
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime in GetByID result")
	}
	if got.FirstTokenDurationMs != 120 {
		t.Fatalf("expected firstTokenDurationMs=120, got %d", got.FirstTokenDurationMs)
	}

	list, err := manager.GetRecent(&RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(list.Requests) == 0 {
		t.Fatalf("expected at least one record from GetRecent")
	}
	if list.Requests[0].FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime in GetRecent result")
	}
	if list.Requests[0].FirstTokenDurationMs != 120 {
		t.Fatalf("expected firstTokenDurationMs=120 in GetRecent, got %d", list.Requests[0].FirstTokenDurationMs)
	}

	complete, err := manager.getCompleteRecordForSSE(pending.ID)
	if err != nil {
		t.Fatalf("getCompleteRecordForSSE failed: %v", err)
	}
	if complete.FirstTokenTime == nil || complete.FirstTokenDurationMs != 120 {
		t.Fatalf("complete SSE record missing first-token fields: %+v", complete)
	}

	partial, err := manager.getPartialRecordForSSE(pending.ID)
	if err != nil {
		t.Fatalf("getPartialRecordForSSE failed: %v", err)
	}
	if partial.FirstTokenTime == nil || partial.FirstTokenDurationMs != 120 {
		t.Fatalf("partial SSE record missing first-token fields: %+v", partial)
	}

	createdEvent := NewLogCreatedEvent(partial)
	createdPayload, ok := createdEvent.Data.(LogCreatedPayload)
	if !ok {
		t.Fatalf("created event payload type mismatch")
	}
	if createdPayload.FirstTokenTime == nil || createdPayload.FirstTokenDurationMs != 120 {
		t.Fatalf("created event payload missing first-token fields: %+v", createdPayload)
	}

	updatedEvent := NewLogUpdatedEvent(pending.ID, complete)
	updatedPayload, ok := updatedEvent.Data.(LogUpdatedPayload)
	if !ok {
		t.Fatalf("updated event payload type mismatch")
	}
	if updatedPayload.FirstTokenTime == nil || updatedPayload.FirstTokenDurationMs != 120 {
		t.Fatalf("updated event payload missing first-token fields: %+v", updatedPayload)
	}
}

func TestRequestLog_UpdatePreservesExistingFirstTokenFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_first_token_preserve.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	start := time.Date(2026, 3, 2, 13, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:       StatusPending,
		InitialTime:  start,
		Type:         "responses",
		ProviderName: "openai",
		Model:        "gpt-5",
		Stream:       true,
		ChannelID:    2,
		ChannelName:  "responses-2",
		Endpoint:     "/v1/responses",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending record: %v", err)
	}

	firstTokenTime := start.Add(333 * time.Millisecond)
	if err := manager.Update(record.ID, &RequestLog{
		Status:               StatusCompleted,
		FirstTokenTime:       &firstTokenTime,
		FirstTokenDurationMs: 333,
		CompleteTime:         start.Add(2 * time.Second),
		DurationMs:           2000,
		Type:                 "responses",
		ProviderName:         "openai",
		ResponseModel:        "gpt-5",
		HTTPStatus:           200,
		ChannelID:            2,
		ChannelName:          "responses-2",
	}); err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	// Later updates may not have first-token fields (e.g. error detail enrichment).
	// Existing first-token values must remain unchanged.
	if err := manager.Update(record.ID, &RequestLog{
		Status:               StatusCompleted,
		FirstTokenTime:       nil,
		FirstTokenDurationMs: 0,
		CompleteTime:         start.Add(3 * time.Second),
		DurationMs:           3000,
		Type:                 "responses",
		ProviderName:         "openai",
		ResponseModel:        "gpt-5",
		HTTPStatus:           200,
		ChannelID:            2,
		ChannelName:          "responses-2",
	}); err != nil {
		t.Fatalf("second update failed: %v", err)
	}

	got, err := manager.GetByID(record.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.FirstTokenTime == nil {
		t.Fatalf("expected firstTokenTime to be preserved")
	}
	if got.FirstTokenDurationMs != 333 {
		t.Fatalf("expected firstTokenDurationMs=333 to be preserved, got %d", got.FirstTokenDurationMs)
	}
}

func TestLogEvents_FirstTokenTimeOmittedWhenNil(t *testing.T) {
	record := &RequestLog{
		ID:                   "req_test_nil_first_token",
		Status:               StatusCompleted,
		InitialTime:          time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC),
		CompleteTime:         time.Date(2026, 3, 2, 14, 0, 1, 0, time.UTC),
		FirstTokenTime:       nil,
		FirstTokenDurationMs: 0,
		DurationMs:           1000,
		Type:                 "gemini",
		ProviderName:         "gemini",
		Model:                "gemini-2.5-pro",
		ChannelID:            3,
		ChannelName:          "gemini-3",
		Endpoint:             "/v1/gemini",
	}

	createdJSON, err := json.Marshal(NewLogCreatedEvent(record))
	if err != nil {
		t.Fatalf("failed to marshal created event: %v", err)
	}
	if strings.Contains(string(createdJSON), `"firstTokenTime"`) {
		t.Fatalf("created event should omit firstTokenTime when nil")
	}
	if !strings.Contains(string(createdJSON), `"firstTokenDurationMs":0`) {
		t.Fatalf("created event should include firstTokenDurationMs=0")
	}

	updatedJSON, err := json.Marshal(NewLogUpdatedEvent(record.ID, record))
	if err != nil {
		t.Fatalf("failed to marshal updated event: %v", err)
	}
	if strings.Contains(string(updatedJSON), `"firstTokenTime"`) {
		t.Fatalf("updated event should omit firstTokenTime when nil")
	}
	if !strings.Contains(string(updatedJSON), `"firstTokenDurationMs":0`) {
		t.Fatalf("updated event should include firstTokenDurationMs=0")
	}
}
