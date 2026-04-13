package requestlog

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestDebugLog_LegacySchemaWithoutRemovedHeadersColumn_Compatibility(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_debug_logs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	schema := `
	CREATE TABLE request_logs (
		id TEXT PRIMARY KEY
	);
	CREATE TABLE request_debug_logs (
		request_id TEXT PRIMARY KEY,
		request_method TEXT NOT NULL,
		request_path TEXT NOT NULL,
		request_headers BLOB,
		request_body BLOB,
		request_body_size INTEGER DEFAULT 0,
		response_status INTEGER DEFAULT 0,
		response_headers BLOB,
		response_body BLOB,
		response_body_size INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (request_id) REFERENCES request_logs(id) ON DELETE CASCADE
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO request_logs (id) VALUES (?)`, "req_legacy_debug"); err != nil {
		t.Fatalf("failed to seed request log row: %v", err)
	}

	manager := &Manager{
		db:          db,
		dialect:     database.DialectSQLite,
		broadcaster: NewBroadcaster(),
	}

	err = manager.AddDebugLog(&DebugLogEntry{
		RequestID:             "req_legacy_debug",
		RequestMethod:         "POST",
		RequestPath:           "/v1/messages",
		RequestHeaders:        map[string]string{"Content-Type": "application/json"},
		RequestRemovedHeaders: map[string]string{"Cf-Ray": "Cf-*"},
		RequestBody:           `{"model":"claude-sonnet-4"}`,
		RequestBodySize:       28,
		ResponseStatus:        503,
		ResponseHeaders:       map[string]string{"Content-Type": "application/json"},
		ResponseBody:          `{"error":"legacy schema"}`,
		ResponseBodySize:      25,
	})
	if err != nil {
		t.Fatalf("AddDebugLog failed for legacy schema: %v", err)
	}

	entry, err := manager.GetDebugLog("req_legacy_debug")
	if err != nil {
		t.Fatalf("GetDebugLog failed for legacy schema: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected debug log entry")
	}
	if entry.RequestBody != `{"model":"claude-sonnet-4"}` {
		t.Fatalf("unexpected request body: %q", entry.RequestBody)
	}
	if entry.ResponseBody != `{"error":"legacy schema"}` {
		t.Fatalf("unexpected response body: %q", entry.ResponseBody)
	}
	if len(entry.RequestRemovedHeaders) != 0 {
		t.Fatalf("expected removed headers to be empty on legacy schema, got %+v", entry.RequestRemovedHeaders)
	}
	if entry.CreatedAt.IsZero() || entry.CreatedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected createdAt to be populated, got %v", entry.CreatedAt)
	}
}
