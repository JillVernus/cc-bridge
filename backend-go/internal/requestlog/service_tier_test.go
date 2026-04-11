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
		Status:                StatusPending,
		InitialTime:           start,
		Type:                  "responses",
		ProviderName:          "responses-1",
		Model:                 "gpt-5",
		ServiceTier:           "priority",
		ServiceTierOverridden: true,
		Stream:                true,
		ChannelID:             1,
		ChannelName:           "responses-1",
		Endpoint:              "/v1/responses",
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
		Status:                StatusCompleted,
		CompleteTime:          completeTime,
		DurationMs:            2000,
		Type:                  "responses",
		ProviderName:          "responses-1",
		ResponseModel:         "gpt-5",
		ServiceTier:           "priority",
		ServiceTierOverridden: true,
		HTTPStatus:            200,
		ChannelID:             1,
		ChannelName:           "responses-1",
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
	if !got.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from GetByID")
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
	if !complete.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from complete SSE record")
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
	if !partial.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from partial SSE record")
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
	if !createdPayload.ServiceTierOverridden {
		t.Fatalf("expected created event serviceTierOverridden=true")
	}
	if !createdPayload.HasDebugData {
		t.Fatalf("expected created event hasDebugData=true")
	}

	complete.Domain = "api.example.com"
	updatedEvent := NewLogUpdatedEvent(record.ID, complete)
	updatedPayload, ok := updatedEvent.Data.(LogUpdatedPayload)
	if !ok {
		t.Fatalf("updated event payload type mismatch")
	}
	if updatedPayload.ServiceTier != "priority" {
		t.Fatalf("expected updated event serviceTier=priority, got %q", updatedPayload.ServiceTier)
	}
	if !updatedPayload.ServiceTierOverridden {
		t.Fatalf("expected updated event serviceTierOverridden=true")
	}
	if !updatedPayload.HasDebugData {
		t.Fatalf("expected updated event hasDebugData=true")
	}
	if updatedPayload.Domain != "api.example.com" {
		t.Fatalf("expected updated event domain=api.example.com, got %q", updatedPayload.Domain)
	}
}

func TestRequestLog_DowngradedServiceTier_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_service_tier_default.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	start := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:                StatusPending,
		InitialTime:           start,
		Type:                  "responses",
		ProviderName:          "responses-default",
		Model:                 "gpt-5",
		ServiceTier:           "default",
		ServiceTierOverridden: true,
		Stream:                true,
		ChannelID:             1,
		ChannelName:           "responses-default",
		Endpoint:              "/v1/responses",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending record: %v", err)
	}

	completeTime := start.Add(1500 * time.Millisecond)
	if err := manager.Update(record.ID, &RequestLog{
		Status:                StatusCompleted,
		CompleteTime:          completeTime,
		DurationMs:            1500,
		Type:                  "responses",
		ProviderName:          "responses-default",
		ResponseModel:         "gpt-5",
		ServiceTier:           "default",
		ServiceTierOverridden: true,
		HTTPStatus:            200,
		ChannelID:             1,
		ChannelName:           "responses-default",
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
	if got.ServiceTier != "default" {
		t.Fatalf("expected serviceTier=default from GetByID, got %q", got.ServiceTier)
	}
	if !got.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from GetByID")
	}

	recent, err := manager.GetRecent(&RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(recent.Requests) == 0 {
		t.Fatalf("expected at least one record from GetRecent")
	}
	if recent.Requests[0].ServiceTier != "default" {
		t.Fatalf("expected serviceTier=default from GetRecent, got %q", recent.Requests[0].ServiceTier)
	}
	if !recent.Requests[0].ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from GetRecent")
	}

	complete, err := manager.getCompleteRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getCompleteRecordForSSE failed: %v", err)
	}
	if complete.ServiceTier != "default" {
		t.Fatalf("expected serviceTier=default from complete SSE record, got %q", complete.ServiceTier)
	}
	if !complete.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from complete SSE record")
	}

	partial, err := manager.getPartialRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getPartialRecordForSSE failed: %v", err)
	}
	if partial.ServiceTier != "default" {
		t.Fatalf("expected serviceTier=default from partial SSE record, got %q", partial.ServiceTier)
	}
	if !partial.ServiceTierOverridden {
		t.Fatalf("expected serviceTierOverridden=true from partial SSE record")
	}

	createdEvent := NewLogCreatedEvent(partial)
	createdPayload, ok := createdEvent.Data.(LogCreatedPayload)
	if !ok {
		t.Fatalf("created event payload type mismatch")
	}
	if createdPayload.ServiceTier != "default" {
		t.Fatalf("expected created event serviceTier=default, got %q", createdPayload.ServiceTier)
	}
	if !createdPayload.ServiceTierOverridden {
		t.Fatalf("expected created event serviceTierOverridden=true")
	}

	updatedEvent := NewLogUpdatedEvent(record.ID, complete)
	updatedPayload, ok := updatedEvent.Data.(LogUpdatedPayload)
	if !ok {
		t.Fatalf("updated event payload type mismatch")
	}
	if updatedPayload.ServiceTier != "default" {
		t.Fatalf("expected updated event serviceTier=default, got %q", updatedPayload.ServiceTier)
	}
	if !updatedPayload.ServiceTierOverridden {
		t.Fatalf("expected updated event serviceTierOverridden=true")
	}
}

func TestRequestLog_XInitiatorMetadata_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_x_initiator.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	start := time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:              StatusPending,
		InitialTime:         start,
		Type:                "claude",
		ProviderName:        "Subscription (Forward Proxy)",
		Model:               "claude-sonnet-4",
		Stream:              true,
		ChannelID:           0,
		ChannelUID:          "subscription:forward-proxy",
		ChannelName:         "Subscription (Forward Proxy)",
		Endpoint:            "/v1/messages",
		OriginalXInitiator:  "user",
		EffectiveXInitiator: "agent",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending record: %v", err)
	}

	completeTime := start.Add(1500 * time.Millisecond)
	if err := manager.Update(record.ID, &RequestLog{
		Status:              StatusCompleted,
		CompleteTime:        completeTime,
		DurationMs:          1500,
		Type:                "claude",
		ProviderName:        "Subscription (Forward Proxy)",
		ResponseModel:       "claude-sonnet-4-20250514",
		HTTPStatus:          200,
		ChannelID:           0,
		ChannelUID:          "subscription:forward-proxy",
		ChannelName:         "Subscription (Forward Proxy)",
		OriginalXInitiator:  "user",
		EffectiveXInitiator: "agent",
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
	if got.OriginalXInitiator != "user" {
		t.Fatalf("expected originalXInitiator=user from GetByID, got %q", got.OriginalXInitiator)
	}
	if got.EffectiveXInitiator != "agent" {
		t.Fatalf("expected effectiveXInitiator=agent from GetByID, got %q", got.EffectiveXInitiator)
	}

	complete, err := manager.getCompleteRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getCompleteRecordForSSE failed: %v", err)
	}
	if complete.OriginalXInitiator != "user" {
		t.Fatalf("expected originalXInitiator=user from complete SSE record, got %q", complete.OriginalXInitiator)
	}
	if complete.EffectiveXInitiator != "agent" {
		t.Fatalf("expected effectiveXInitiator=agent from complete SSE record, got %q", complete.EffectiveXInitiator)
	}

	partial, err := manager.getPartialRecordForSSE(record.ID)
	if err != nil {
		t.Fatalf("getPartialRecordForSSE failed: %v", err)
	}
	if partial.OriginalXInitiator != "user" {
		t.Fatalf("expected originalXInitiator=user from partial SSE record, got %q", partial.OriginalXInitiator)
	}
	if partial.EffectiveXInitiator != "agent" {
		t.Fatalf("expected effectiveXInitiator=agent from partial SSE record, got %q", partial.EffectiveXInitiator)
	}

	createdEvent := NewLogCreatedEvent(partial)
	createdPayload, ok := createdEvent.Data.(LogCreatedPayload)
	if !ok {
		t.Fatalf("created event payload type mismatch")
	}
	if createdPayload.OriginalXInitiator != "user" {
		t.Fatalf("expected created event originalXInitiator=user, got %q", createdPayload.OriginalXInitiator)
	}
	if createdPayload.EffectiveXInitiator != "agent" {
		t.Fatalf("expected created event effectiveXInitiator=agent, got %q", createdPayload.EffectiveXInitiator)
	}

	updatedEvent := NewLogUpdatedEvent(record.ID, complete)
	updatedPayload, ok := updatedEvent.Data.(LogUpdatedPayload)
	if !ok {
		t.Fatalf("updated event payload type mismatch")
	}
	if updatedPayload.OriginalXInitiator != "user" {
		t.Fatalf("expected updated event originalXInitiator=user, got %q", updatedPayload.OriginalXInitiator)
	}
	if updatedPayload.EffectiveXInitiator != "agent" {
		t.Fatalf("expected updated event effectiveXInitiator=agent, got %q", updatedPayload.EffectiveXInitiator)
	}
}
