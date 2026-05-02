package quota

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestDBUsageStorageLoadAllHandlesTimestampValues(t *testing.T) {
	db, err := database.New(database.Config{
		Type: database.DialectSQLite,
		URL:  filepath.Join(t.TempDir(), "cc-bridge.db"),
	})
	if err != nil {
		t.Fatalf("New database failed: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	lastResetAt := time.Date(2026, 5, 2, 10, 11, 12, 0, time.UTC)
	if _, err := db.Exec(
		`INSERT INTO channel_usage (channel_id, channel_type, used, last_reset_at) VALUES (?, ?, ?, ?)`,
		7,
		"responses",
		42.5,
		lastResetAt,
	); err != nil {
		t.Fatalf("insert channel_usage failed: %v", err)
	}

	usage, err := NewDBUsageStorage(db).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	got := usage.Responses["7"]
	if got.Used != 42.5 {
		t.Fatalf("expected used=42.5, got %v", got.Used)
	}
	if !got.LastResetAt.Equal(lastResetAt) {
		t.Fatalf("expected lastResetAt=%s, got %s", lastResetAt, got.LastResetAt)
	}
}
