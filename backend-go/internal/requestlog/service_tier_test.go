package requestlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRequestLog_ServiceTierAndDebugData_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_service_tier.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	start := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:       StatusPending,
		InitialTime:  start,
		Type:         "responses",
		ProviderName: "responses-1",
		Model:        "gpt-5",
		ServiceTier:  "priority",
		Stream:       true,
		ChannelID:    1,
		ChannelName:  "responses-1",
		Endpoint:     "/v1/responses",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending record: %v", err)
	}

	if err := manager.AddDebugLog(&DebugLogEntry{
		RequestID:      record.ID,
		RequestMethod:  "POST",
		RequestPath:    "/v1/responses",
		ResponseStatus: 200,
	}); err != nil {
		t.Fatalf("failed to add debug log: %v", err)
	}

	completeTime := start.Add(2 * time.Second)
	if err := manager.Update(record.ID, &RequestLog{
		Status:        StatusCompleted,
		CompleteTime:  completeTime,
		DurationMs:    2000,
		Type:          "responses",
		ProviderName:  "responses-1",
		ResponseModel: "gpt-5",
		ServiceTier:   "priority",
		HTTPStatus:    200,
		ChannelID:     1,
		ChannelName:   "responses-1",
	}); err != nil {
		t.Fatalf("failed to update record: %v", err)
	}

	got, err := manager.GetByID(record.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected record from GetByID")
	}
	if got.ServiceTier != "priority" {
		t.Fatalf("expected serviceTier=priority from GetByID, got %q", got.ServiceTier)
	}
	if !got.HasDebugData {
		t.Fatalf("expected hasDebugData=true from GetByID")
	}

	complete, err := manager.getCompleteRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getCompleteRecordForSSE failed: %v", err)
	}
	if complete.ServiceTier != "priority" {
		t.Fatalf("expected serviceTier=priority from complete SSE record, got %q", complete.ServiceTier)
	}
	if !complete.HasDebugData {
		t.Fatalf("expected hasDebugData=true from complete SSE record")
	}

	partial, err := manager.getPartialRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getPartialRecordForSSE failed: %v", err)
	}
	if partial.ServiceTier != "priority" {
		t.Fatalf("expected serviceTier=priority from partial SSE record, got %q", partial.ServiceTier)
	}
	if !partial.HasDebugData {
		t.Fatalf("expected hasDebugData=true from partial SSE record")
	}

	createdEvent := NewLogCreatedEvent(partial)
	createdPayload, ok := createdEvent.Data.(LogCreatedPayload)
	if !ok {
		t.Fatalf("created event payload type mismatch")
	}
	if createdPayload.ServiceTier != "priority" {
		t.Fatalf("expected created event serviceTier=priority, got %q", createdPayload.ServiceTier)
	}
	if !createdPayload.HasDebugData {
		t.Fatalf("expected created event hasDebugData=true")
	}

	updatedEvent := NewLogUpdatedEvent(record.ID, complete)
	updatedPayload, ok := updatedEvent.Data.(LogUpdatedPayload)
	if !ok {
		t.Fatalf("updated event payload type mismatch")
	}
	if updatedPayload.ServiceTier != "priority" {
		t.Fatalf("expected updated event serviceTier=priority, got %q", updatedPayload.ServiceTier)
	}
	if !updatedPayload.HasDebugData {
		t.Fatalf("expected updated event hasDebugData=true")
	}
}
