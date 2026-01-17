package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

// SQLiteDB is the SQLite implementation of DB
type SQLiteDB struct {
	*BaseDB
}

// NewSQLite creates a new SQLite database connection
func NewSQLite(cfg Config) (*SQLiteDB, error) {
	dbPath := cfg.URL
	if dbPath == "" {
		dbPath = ".config/cc-bridge.db"
	}

	// Add connection parameters for busy timeout and other settings
	// _busy_timeout=5000 - wait up to 5 seconds when database is locked
	// _txlock=immediate - acquire write lock immediately in transactions
	connStr := dbPath + "?_busy_timeout=5000&_txlock=immediate"
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Limit to single connection to avoid lock contention
	// SQLite doesn't benefit from multiple write connections
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("⚠️ Failed to enable WAL mode: %v", err)
	}

	// Set busy timeout to wait up to 5 seconds when database is locked
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		log.Printf("⚠️ Failed to set busy timeout: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		log.Printf("⚠️ Failed to enable foreign keys: %v", err)
	}

	sqliteDB := &SQLiteDB{
		BaseDB: &BaseDB{
			DB:      db,
			dialect: DialectSQLite,
			helper:  NewDialectHelper(DialectSQLite),
		},
	}

	log.Printf("📦 SQLite database initialized: %s", dbPath)
	return sqliteDB, nil
}

// ColumnExists checks if a column exists in a table (SQLite-specific)
func (db *SQLiteDB) ColumnExists(table, column string) (bool, error) {
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", table, column)
	err := db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TableExists checks if a table exists (SQLite-specific)
func (db *SQLiteDB) TableExists(table string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	err := db.QueryRow(query, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
