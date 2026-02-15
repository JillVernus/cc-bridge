package requestlog

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
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

func TestGetRecentChannelCalls_ChannelUIDGroupingAcrossIndexChanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_uid.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	base := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	uid := "stable-channel-uid"

	// Same stable channel UID but different historical channel indexes.
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base.Add(1 * time.Second),
		Type:         "claude",
		ProviderName: "messages-changed",
		Model:        "test-model",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    1,
		ChannelUID:   uid,
		ChannelName:  "messages-stable",
		Endpoint:     "/v1/messages",
	})
	mustAddLog(t, manager, &RequestLog{
		Status:       StatusError,
		InitialTime:  base.Add(2 * time.Second),
		Type:         "claude",
		ProviderName: "messages-changed",
		Model:        "test-model",
		HTTPStatus:   503,
		Stream:       false,
		ChannelID:    5, // index changed after reorder
		ChannelUID:   uid,
		ChannelName:  "messages-stable",
		Endpoint:     "/v1/messages",
	})

	calls, err := manager.GetRecentChannelCalls(20)
	if err != nil {
		t.Fatalf("GetRecentChannelCalls failed: %v", err)
	}

	var matched []ChannelRecentCall
	for _, call := range calls {
		if call.Endpoint == "/v1/messages" && call.ChannelUID == uid {
			matched = append(matched, call)
		}
	}

	if len(matched) != 2 {
		t.Fatalf("expected both historical indices grouped under same uid, got %d records", len(matched))
	}
	if !matched[0].Success || matched[0].HTTPStatus != 200 {
		t.Fatalf("unexpected first uid call: %+v", matched[0])
	}
	if matched[1].Success || matched[1].HTTPStatus != 503 {
		t.Fatalf("unexpected second uid call: %+v", matched[1])
	}
}

func TestAdd_ResolvesChannelUIDViaResolver(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_resolver.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	manager.SetChannelUIDResolver(func(endpoint string, channelIndex int, channelName string) string {
		if endpoint == "/v1/messages" && channelIndex == 2 && channelName == "messages-2" {
			return "msg-uid-2"
		}
		return ""
	})

	mustAddLog(t, manager, &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  time.Date(2026, 2, 16, 9, 0, 0, 0, time.UTC),
		Type:         "claude",
		ProviderName: "messages-ch2",
		Model:        "test-model",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    2,
		ChannelName:  "messages-2",
		Endpoint:     "/v1/messages",
	})

	calls, err := manager.GetRecentChannelCalls(20)
	if err != nil {
		t.Fatalf("GetRecentChannelCalls failed: %v", err)
	}

	found := false
	for _, call := range calls {
		if call.Endpoint == "/v1/messages" && call.ChannelID == 2 {
			found = true
			if call.ChannelUID != "msg-uid-2" {
				t.Fatalf("expected resolved channel uid msg-uid-2, got %q", call.ChannelUID)
			}
		}
	}
	if !found {
		t.Fatalf("expected restored call for messages#2")
	}
}

func TestUpdate_PreservesResolvedChannelUIDWhenEndpointMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "request_logs_update_uid.db")
	manager, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
	})

	manager.SetChannelUIDResolver(func(endpoint string, channelIndex int, channelName string) string {
		if channelName == "messages-a" {
			return "uid-messages-a"
		}
		return ""
	})

	base := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:       StatusPending,
		InitialTime:  base,
		Type:         "claude",
		ProviderName: "messages-a",
		Model:        "test-model",
		Stream:       false,
		Endpoint:     "/v1/messages",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("failed to add pending log: %v", err)
	}

	// Complete request with channel metadata but without endpoint.
	// This matches real handler update paths and must still keep UID linkage.
	update := &RequestLog{
		Status:       StatusCompleted,
		CompleteTime: base.Add(2 * time.Second),
		DurationMs:   2000,
		Type:         "claude",
		ProviderName: "messages-a",
		Model:        "test-model",
		HTTPStatus:   200,
		ChannelID:    3,
		ChannelName:  "messages-a",
	}
	if err := manager.Update(record.ID, update); err != nil {
		t.Fatalf("failed to update log: %v", err)
	}

	got, err := manager.GetByID(record.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.ChannelUID != "uid-messages-a" {
		t.Fatalf("expected channel uid to be resolved and preserved, got %q", got.ChannelUID)
	}
	if got.ChannelID != 3 {
		t.Fatalf("expected channel id=3, got %d", got.ChannelID)
	}
}

func TestLegacySchemaWithoutChannelUID_Compatibility(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_request_logs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open legacy sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	schema := `
	CREATE TABLE request_logs (
		id TEXT PRIMARY KEY,
		status TEXT DEFAULT 'completed',
		initial_time DATETIME NOT NULL,
		complete_time DATETIME,
		duration_ms INTEGER DEFAULT 0,
		provider TEXT NOT NULL,
		provider_name TEXT,
		model TEXT NOT NULL,
		response_model TEXT,
		reasoning_effort TEXT,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_creation_input_tokens INTEGER DEFAULT 0,
		cache_read_input_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		price REAL DEFAULT 0,
		input_cost REAL DEFAULT 0,
		output_cost REAL DEFAULT 0,
		cache_creation_cost REAL DEFAULT 0,
		cache_read_cost REAL DEFAULT 0,
		http_status INTEGER DEFAULT 0,
		stream BOOLEAN NOT NULL,
		channel_id INTEGER,
		channel_name TEXT,
		endpoint TEXT,
		client_id TEXT,
		session_id TEXT,
		api_key_id INTEGER,
		error TEXT,
		upstream_error TEXT,
		failover_info TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create legacy request_logs schema: %v", err)
	}

	manager := &Manager{
		db:          db,
		dialect:     database.DialectSQLite,
		broadcaster: NewBroadcaster(),
	}

	base := time.Date(2026, 2, 16, 11, 0, 0, 0, time.UTC)
	record := &RequestLog{
		Status:       StatusCompleted,
		InitialTime:  base,
		CompleteTime: base.Add(1 * time.Second),
		DurationMs:   1000,
		Type:         "responses",
		ProviderName: "responses-legacy",
		Model:        "gpt-5",
		HTTPStatus:   200,
		Stream:       false,
		ChannelID:    2,
		ChannelName:  "responses-2",
		Endpoint:     "/v1/responses",
	}
	if err := manager.Add(record); err != nil {
		t.Fatalf("expected legacy Add fallback to succeed, got error: %v", err)
	}

	calls, err := manager.GetRecentChannelCalls(20)
	if err != nil {
		t.Fatalf("expected legacy GetRecentChannelCalls fallback to succeed, got error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 recent call, got %d", len(calls))
	}
	if calls[0].Endpoint != "/v1/responses" || calls[0].ChannelID != 2 || !calls[0].Success {
		t.Fatalf("unexpected restored legacy call: %+v", calls[0])
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
