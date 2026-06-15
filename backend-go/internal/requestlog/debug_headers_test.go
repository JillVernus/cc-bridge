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
			"Authorization":    "Bearer super-secret-token",
			"Content-Type":     "application/json",
			"CF-Connecting-IP": "203.0.113.10",
		},
		RequestRemovedHeaders: map[string]string{
			"CF-Connecting-IP": "Cf-*",
		},
		RequestModifiedHeaders: map[string]string{
			"User-Agent": "codex_cli_rs/0.80.0 (Linux; x86_64)",
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
	if got := entry.RequestRemovedHeaders["CF-Connecting-IP"]; got != "Cf-*" {
		t.Fatalf("expected removed header rule preserved, got %q", got)
	}
	if got := entry.RequestModifiedHeaders["User-Agent"]; got != "codex_cli_rs/0.80.0 (Linux; x86_64)" {
		t.Fatalf("expected modified User-Agent target preserved, got %q", got)
	}
	if got := entry.ResponseHeadersRaw["Set-Cookie"]; got != "session=abc123" {
		t.Fatalf("expected raw Set-Cookie preserved, got %q", got)
	}
	if got := entry.ResponseHeaders["Set-Cookie"]; got == "" || got == "session=abc123" {
		t.Fatalf("expected masked Set-Cookie in default view, got %q", got)
	}
}

func TestRequestLogCleanupKeepsParentRowsWhileDebugHeadersRetained(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_cleanup_keeps_debug_headers.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	oldTime := time.Now().AddDate(0, 0, -31)
	record := &RequestLog{
		ID:           "req_old_with_debug_headers",
		Status:       StatusCompleted,
		InitialTime:  oldTime,
		CompleteTime: oldTime.Add(time.Second),
		Type:         "responses",
		ProviderName: "responses-debug",
		Model:        "gpt-5",
		Endpoint:     "/v1/responses",
		ChannelName:  "responses-debug",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("Add request log failed: %v", err)
	}
	if err := manager.AddDebugLog(&DebugLogEntry{
		RequestID:     record.ID,
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
		RequestHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		ResponseStatus: httpStatusOKForDebugTest,
		ResponseHeaders: map[string]string{
			"Content-Type": "application/json",
		},
	}); err != nil {
		t.Fatalf("AddDebugLog failed: %v", err)
	}

	deleted, err := manager.Cleanup(30)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected cleanup to keep parent row while debug headers exist, deleted %d rows", deleted)
	}

	entry, err := manager.GetDebugLog(record.ID)
	if err != nil {
		t.Fatalf("GetDebugLog failed: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected debug headers to survive request-log cleanup")
	}
}

func TestDebugLog_TwoTierRetentionZeroKeepsHeadersForever(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_debug_retention.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	record := &RequestLog{
		ID:           "req_debug_retention_forever",
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
	if err := manager.AddDebugLog(&DebugLogEntry{
		RequestID:     record.ID,
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
		RequestHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		RequestBody:     `{"prompt":"old"}`,
		RequestBodySize: 16,
		ResponseStatus:  httpStatusOKForDebugTest,
		ResponseHeaders: map[string]string{
			"Content-Type": "text/event-stream",
		},
		ResponseBody:     `data: old`,
		ResponseBodySize: 9,
	}); err != nil {
		t.Fatalf("AddDebugLog failed: %v", err)
	}

	oldCreatedAt := time.Now().Add(-240 * time.Hour)
	if _, err := manager.db.Exec(`UPDATE request_debug_logs SET created_at = ? WHERE request_id = ?`, oldCreatedAt, record.ID); err != nil {
		t.Fatalf("failed to age debug log: %v", err)
	}

	bodiesDeleted, rowsDeleted, err := manager.PurgeExpiredDebugLogsTwoTier(1, 0)
	if err != nil {
		t.Fatalf("PurgeExpiredDebugLogsTwoTier failed: %v", err)
	}
	if bodiesDeleted != 1 {
		t.Fatalf("expected one body purge, got %d", bodiesDeleted)
	}
	if rowsDeleted != 0 {
		t.Fatalf("expected zero rows deleted when header retention is forever, got %d", rowsDeleted)
	}

	entry, err := manager.GetDebugLog(record.ID)
	if err != nil {
		t.Fatalf("GetDebugLog failed: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected header row to remain when header retention is forever")
	}
	if got := entry.RequestHeadersRaw["Content-Type"]; got != "application/json" {
		t.Fatalf("expected request headers preserved, got %q", got)
	}
	if got := entry.ResponseHeadersRaw["Content-Type"]; got != "text/event-stream" {
		t.Fatalf("expected response headers preserved, got %q", got)
	}
	if entry.RequestBody != "" || entry.ResponseBody != "" {
		t.Fatalf("expected bodies purged, got request=%q response=%q", entry.RequestBody, entry.ResponseBody)
	}
}

func TestDebugLog_PurgeAllHeadersClearsHeadersAndRemovesEmptyRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_debug_header_purge.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	addRecord := func(id string) {
		t.Helper()
		record := &RequestLog{
			ID:           id,
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
			t.Fatalf("Add request log %s failed: %v", id, err)
		}
	}
	addRecord("req_header_only")
	addRecord("req_full_debug")

	if err := manager.AddDebugLog(&DebugLogEntry{
		RequestID:     "req_header_only",
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
		RequestHeaders: map[string]string{
			"X-Header-Only": "yes",
		},
		ResponseStatus: httpStatusOKForDebugTest,
		ResponseHeaders: map[string]string{
			"X-Response-Header": "yes",
		},
	}); err != nil {
		t.Fatalf("AddDebugLog header-only failed: %v", err)
	}
	if err := manager.AddDebugLog(&DebugLogEntry{
		RequestID:     "req_full_debug",
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
		RequestHeaders: map[string]string{
			"X-Full-Debug": "yes",
		},
		RequestBody:     `{"prompt":"keep body"}`,
		RequestBodySize: 22,
		ResponseStatus:  httpStatusOKForDebugTest,
		ResponseHeaders: map[string]string{
			"X-Response-Header": "yes",
		},
		ResponseBody:     `data: keep body`,
		ResponseBodySize: 15,
	}); err != nil {
		t.Fatalf("AddDebugLog full failed: %v", err)
	}

	headersCleared, rowsDeleted, err := manager.PurgeAllDebugLogHeaders()
	if err != nil {
		t.Fatalf("PurgeAllDebugLogHeaders failed: %v", err)
	}
	if headersCleared != 2 {
		t.Fatalf("expected headers cleared from 2 rows, got %d", headersCleared)
	}
	if rowsDeleted != 1 {
		t.Fatalf("expected 1 now-empty header-only row deleted, got %d", rowsDeleted)
	}

	headerOnly, err := manager.GetDebugLog("req_header_only")
	if err != nil {
		t.Fatalf("GetDebugLog header-only failed: %v", err)
	}
	if headerOnly != nil {
		t.Fatalf("expected header-only row to be removed after header purge")
	}

	fullEntry, err := manager.GetDebugLog("req_full_debug")
	if err != nil {
		t.Fatalf("GetDebugLog full failed: %v", err)
	}
	if fullEntry == nil {
		t.Fatalf("expected full debug row with body to remain")
	}
	if len(fullEntry.RequestHeadersRaw) != 0 || len(fullEntry.ResponseHeadersRaw) != 0 {
		t.Fatalf("expected headers cleared, got request=%v response=%v", fullEntry.RequestHeadersRaw, fullEntry.ResponseHeadersRaw)
	}
	if fullEntry.RequestBody == "" || fullEntry.ResponseBody == "" {
		t.Fatalf("expected bodies to remain after header purge")
	}
}

const httpStatusOKForDebugTest = 200
