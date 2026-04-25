package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteConnection(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database connection
	cfg := Config{
		Type: DialectSQLite,
		URL:  dbPath,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Verify dialect
	if db.Dialect() != DialectSQLite {
		t.Errorf("Expected dialect SQLite, got %s", db.Dialect())
	}
}

func TestMigrations(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database connection
	cfg := Config{
		Type: DialectSQLite,
		URL:  dbPath,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Verify tables were created
	tables := []string{"settings", "channels", "model_pricing", "model_aliases", "channel_usage", "schema_migrations"}
	for _, table := range tables {
		exists, err := TableExists(db, table)
		if err != nil {
			t.Errorf("Failed to check table %s: %v", table, err)
			continue
		}
		if !exists {
			t.Errorf("Table %s was not created", table)
		}
	}

	// Verify migration was recorded
	var version int
	err = db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version)
	if err != nil {
		t.Fatalf("Failed to get migration version: %v", err)
	}
	if version < 1 {
		t.Errorf("Expected migration version >= 1, got %d", version)
	}
}

func TestSettingsTable(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Type: DialectSQLite,
		URL:  dbPath,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Insert a setting
	_, err = db.Exec("INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
		"test_key", "test_value", "test_category")
	if err != nil {
		t.Fatalf("Failed to insert setting: %v", err)
	}

	// Read it back
	var value string
	err = db.QueryRow("SELECT value FROM settings WHERE key = ?", "test_key").Scan(&value)
	if err != nil {
		t.Fatalf("Failed to read setting: %v", err)
	}
	if value != "test_value" {
		t.Errorf("Expected value 'test_value', got '%s'", value)
	}

	// Update it
	_, err = db.Exec("UPDATE settings SET value = ? WHERE key = ?", "updated_value", "test_key")
	if err != nil {
		t.Fatalf("Failed to update setting: %v", err)
	}

	// Verify update
	err = db.QueryRow("SELECT value FROM settings WHERE key = ?", "test_key").Scan(&value)
	if err != nil {
		t.Fatalf("Failed to read updated setting: %v", err)
	}
	if value != "updated_value" {
		t.Errorf("Expected value 'updated_value', got '%s'", value)
	}

	// Delete it
	_, err = db.Exec("DELETE FROM settings WHERE key = ?", "test_key")
	if err != nil {
		t.Fatalf("Failed to delete setting: %v", err)
	}

	// Verify deletion
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM settings WHERE key = ?", "test_key").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count settings: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 settings, got %d", count)
	}
}

func TestChannelsTable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Type: DialectSQLite,
		URL:  dbPath,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Insert a channel
	_, err = db.Exec(`
		INSERT INTO channels (channel_id, channel_type, name, service_type, base_url, status, api_keys)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"ch001", "messages", "Test Channel", "claude", "https://api.anthropic.com", "active", `["sk-test-key"]`)
	if err != nil {
		t.Fatalf("Failed to insert channel: %v", err)
	}

	// Read it back
	var name, apiKeys string
	err = db.QueryRow("SELECT name, api_keys FROM channels WHERE channel_id = ?", "ch001").Scan(&name, &apiKeys)
	if err != nil {
		t.Fatalf("Failed to read channel: %v", err)
	}
	if name != "Test Channel" {
		t.Errorf("Expected name 'Test Channel', got '%s'", name)
	}
	if apiKeys != `["sk-test-key"]` {
		t.Errorf("Expected api_keys '[\"sk-test-key\"]', got '%s'", apiKeys)
	}
}

func TestTransaction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Type: DialectSQLite,
		URL:  dbPath,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Test successful transaction
	err = Transaction(db, func(tx *Tx) error {
		_, err := tx.Exec("INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
			"tx_key1", "tx_value1", "test")
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
			"tx_key2", "tx_value2", "test")
		return err
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	// Verify both inserts were committed
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM settings WHERE category = ?", "test").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count settings: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 settings, got %d", count)
	}

	// Test failed transaction (rollback)
	err = Transaction(db, func(tx *Tx) error {
		_, err := tx.Exec("INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
			"tx_key3", "tx_value3", "test")
		if err != nil {
			return err
		}
		// This should fail due to duplicate key
		_, err = tx.Exec("INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
			"tx_key1", "duplicate", "test")
		return err
	})
	if err == nil {
		t.Error("Expected transaction to fail due to duplicate key")
	}

	// Verify tx_key3 was NOT inserted (rollback worked)
	err = db.QueryRow("SELECT COUNT(*) FROM settings WHERE key = ?", "tx_key3").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count settings: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 (rollback), got %d", count)
	}
}

func TestDialectHelper(t *testing.T) {
	sqliteHelper := NewDialectHelper(DialectSQLite)
	pgHelper := NewDialectHelper(DialectPostgreSQL)

	// Test placeholder
	if sqliteHelper.Placeholder(1) != "?" {
		t.Errorf("SQLite placeholder should be ?, got %s", sqliteHelper.Placeholder(1))
	}
	if pgHelper.Placeholder(1) != "$1" {
		t.Errorf("PostgreSQL placeholder should be $1, got %s", pgHelper.Placeholder(1))
	}
	if pgHelper.Placeholder(3) != "$3" {
		t.Errorf("PostgreSQL placeholder should be $3, got %s", pgHelper.Placeholder(3))
	}

	// Test auto-increment
	if sqliteHelper.AutoIncrementPK() != "INTEGER PRIMARY KEY AUTOINCREMENT" {
		t.Errorf("SQLite auto-increment mismatch")
	}
	if pgHelper.AutoIncrementPK() != "SERIAL PRIMARY KEY" {
		t.Errorf("PostgreSQL auto-increment mismatch")
	}

	// Test datetime type
	if sqliteHelper.DatetimeType() != "DATETIME" {
		t.Errorf("SQLite datetime mismatch")
	}
	if pgHelper.DatetimeType() != "TIMESTAMP WITH TIME ZONE" {
		t.Errorf("PostgreSQL datetime mismatch")
	}
}

func TestRunMigrations_AddsRequestRemovedHeadersToExistingDebugLogsTable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(Config{
		Type: DialectSQLite,
		URL:  dbPath,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_migrations (version, name) VALUES (12, 'request_logs_priced_by_target_model');

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE channel_quota (
			channel_id INTEGER PRIMARY KEY,
			channel_name TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("Failed to set up legacy schema: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_debug_logs') WHERE name = 'request_removed_headers'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to inspect request_debug_logs schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected request_removed_headers column to exist after migration, got %d", count)
	}
}

func TestRunMigrations_AddsChannelStableIDToExistingChannelQuotaTable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ccbridge-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(Config{
		Type: DialectSQLite,
		URL:  dbPath,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_migrations (version, name) VALUES (13, 'request_debug_logs_removed_headers');

		CREATE TABLE channel_quota (
			channel_id INTEGER PRIMARY KEY,
			channel_name TEXT NOT NULL,
			plan_type TEXT,
			primary_used_percent INTEGER DEFAULT 0,
			primary_window_minutes INTEGER DEFAULT 0,
			primary_reset_at DATETIME,
			secondary_used_percent INTEGER DEFAULT 0,
			secondary_window_minutes INTEGER DEFAULT 0,
			secondary_reset_at DATETIME,
			credits_has_credits BOOLEAN DEFAULT 0,
			credits_unlimited BOOLEAN DEFAULT 0,
			credits_balance TEXT,
			is_exceeded BOOLEAN DEFAULT 0,
			exceeded_at DATETIME,
			recover_at DATETIME,
			exceeded_reason TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("Failed to set up legacy schema: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('channel_quota') WHERE name = 'channel_stable_id'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to inspect channel_quota schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected channel_stable_id column to exist after migration, got %d", count)
	}
}

func TestConvertPlaceholders(t *testing.T) {
	query := "SELECT * FROM users WHERE id = ? AND name = ? AND active = ?"

	// SQLite should remain unchanged
	sqliteResult := ConvertPlaceholders(query, DialectSQLite)
	if sqliteResult != query {
		t.Errorf("SQLite query should not change, got: %s", sqliteResult)
	}

	// PostgreSQL should convert to $1, $2, $3
	expected := "SELECT * FROM users WHERE id = $1 AND name = $2 AND active = $3"
	pgResult := ConvertPlaceholders(query, DialectPostgreSQL)
	if pgResult != expected {
		t.Errorf("PostgreSQL conversion failed.\nExpected: %s\nGot: %s", expected, pgResult)
	}
}
