package requestlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDebugLog_RoundTripPreservesRawHeadersAndReturnsMaskedView(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_debug_headers.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	record := &RequestLog{
		ID:           "req_debug_headers",
		Status:       StatusCompleted,
		InitialTime:  time.Now().UTC(),
		CompleteTime: time.Now().UTC(),
		Type:         "responses",
		ProviderName: "responses-debug",
		Model:        "gpt-5",
		Endpoint:     "/v1/responses",
		ChannelName:  "responses-debug",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("Add request log failed: %v", err)
	}

	err = manager.AddDebugLog(&DebugLogEntry{
		RequestID:     record.ID,
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
		RequestHeaders: map[string]string{
			"Authorization": "Bearer super-secret-token",
			"Content-Type":  "application/json",
		},
		ResponseHeaders: map[string]string{
			"Set-Cookie":   "session=abc123",
			"Content-Type": "text/event-stream",
		},
	})
	if err != nil {
		t.Fatalf("AddDebugLog failed: %v", err)
	}

	entry, err := manager.GetDebugLog(record.ID)
	if err != nil {
		t.Fatalf("GetDebugLog failed: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected debug log entry")
	}

	if got := entry.RequestHeadersRaw["Authorization"]; got != "Bearer super-secret-token" {
		t.Fatalf("expected raw Authorization preserved, got %q", got)
	}
	if got := entry.RequestHeaders["Authorization"]; got == "" || got == "Bearer super-secret-token" {
		t.Fatalf("expected masked Authorization in default view, got %q", got)
	}
	if got := entry.RequestHeaders["Content-Type"]; got != "application/json" {
		t.Fatalf("expected Content-Type unchanged, got %q", got)
	}
	if got := entry.ResponseHeadersRaw["Set-Cookie"]; got != "session=abc123" {
		t.Fatalf("expected raw Set-Cookie preserved, got %q", got)
	}
	if got := entry.ResponseHeaders["Set-Cookie"]; got == "" || got == "session=abc123" {
		t.Fatalf("expected masked Set-Cookie in default view, got %q", got)
	}
}
