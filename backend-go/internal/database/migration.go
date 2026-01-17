package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a database migration
type Migration struct {
	Version int
	Name    string
	Up      string
}

// RunMigrations runs all pending migrations
func RunMigrations(db DB) error {
	// Ensure migrations table exists
	if err := createMigrationsTable(db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	currentVersion := getCurrentVersion(db)

	// Load and apply pending migrations
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Apply pending migrations
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		log.Printf("📦 Applying migration %03d: %s", m.Version, m.Name)

		// Convert placeholders for PostgreSQL if needed
		upSQL := m.Up
		if db.Dialect() == DialectPostgreSQL {
			upSQL = convertMigrationSQL(upSQL, db.Dialect())
		}

		// Execute migration
		if _, err := db.Exec(upSQL); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Name, err)
		}

		// Record migration
		query := "INSERT INTO schema_migrations (version, name) VALUES (?, ?)"
		if db.Dialect() == DialectPostgreSQL {
			query = "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)"
		}
		if _, err := db.Exec(query, m.Version, m.Name); err != nil {
			return fmt.Errorf("failed to record migration %d: %w", m.Version, err)
		}

		log.Printf("✅ Migration %03d applied successfully", m.Version)
	}

	return nil
}

// createMigrationsTable creates the schema_migrations table if it doesn't exist
func createMigrationsTable(db DB) error {
	var createSQL string
	switch db.Dialect() {
	case DialectPostgreSQL:
		createSQL = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`
	default:
		createSQL = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`
	}

	_, err := db.Exec(createSQL)
	return err
}

// getCurrentVersion returns the current migration version
func getCurrentVersion(db DB) int {
	var version int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0
	}
	return version
}

// loadMigrations loads all migration files from the embedded filesystem
func loadMigrations() ([]Migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		// No migrations directory yet
		return nil, nil
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
		}

		// Parse filename: 001_initial.sql -> version=1, name=initial
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		name := strings.TrimSuffix(parts[1], ".sql")

		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			Up:      string(content),
		})
	}

	return migrations, nil
}

// convertMigrationSQL converts SQLite-style SQL to PostgreSQL
func convertMigrationSQL(sql string, dialect Dialect) string {
	if dialect != DialectPostgreSQL {
		return sql
	}

	// Convert AUTOINCREMENT to SERIAL
	sql = strings.ReplaceAll(sql, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")

	// Convert DATETIME to TIMESTAMP WITH TIME ZONE
	sql = strings.ReplaceAll(sql, "DATETIME", "TIMESTAMP WITH TIME ZONE")

	// Convert BLOB to BYTEA
	sql = strings.ReplaceAll(sql, "BLOB", "BYTEA")

	// Convert BOOLEAN defaults (handle NOT NULL variants too)
	sql = strings.ReplaceAll(sql, "BOOLEAN NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT FALSE")
	sql = strings.ReplaceAll(sql, "BOOLEAN NOT NULL DEFAULT 1", "BOOLEAN NOT NULL DEFAULT TRUE")
	sql = strings.ReplaceAll(sql, "BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT FALSE")
	sql = strings.ReplaceAll(sql, "BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT TRUE")

	return sql
}

// GetMigrationStatus returns the current migration status
func GetMigrationStatus(db DB) (current int, available int, pending []Migration, err error) {
	current = getCurrentVersion(db)

	migrations, err := loadMigrations()
	if err != nil {
		return 0, 0, nil, err
	}

	available = len(migrations)
	for _, m := range migrations {
		if m.Version > current {
			pending = append(pending, m)
		}
	}

	return current, available, pending, nil
}

// MigrationRunner is a helper for running migrations with hooks
type MigrationRunner struct {
	db         DB
	beforeHook func(m Migration)
	afterHook  func(m Migration, err error)
}

// NewMigrationRunner creates a new migration runner
func NewMigrationRunner(db DB) *MigrationRunner {
	return &MigrationRunner{db: db}
}

// OnBefore sets a hook to run before each migration
func (r *MigrationRunner) OnBefore(fn func(m Migration)) *MigrationRunner {
	r.beforeHook = fn
	return r
}

// OnAfter sets a hook to run after each migration
func (r *MigrationRunner) OnAfter(fn func(m Migration, err error)) *MigrationRunner {
	r.afterHook = fn
	return r
}

// Run executes all pending migrations
func (r *MigrationRunner) Run() error {
	return RunMigrations(r.db)
}

// RollbackTo rolls back migrations to a specific version
// Note: This requires down migrations which are not yet implemented
func RollbackTo(db DB, version int) error {
	return fmt.Errorf("rollback not yet implemented")
}

// ColumnExists is a helper function to check if a column exists
func ColumnExists(db DB, table, column string) (bool, error) {
	switch d := db.(type) {
	case *SQLiteDB:
		return d.ColumnExists(table, column)
	case *PostgreSQLDB:
		return d.ColumnExists(table, column)
	default:
		return false, fmt.Errorf("unsupported database type")
	}
}

// TableExists is a helper function to check if a table exists
func TableExists(db DB, table string) (bool, error) {
	switch d := db.(type) {
	case *SQLiteDB:
		return d.TableExists(table)
	case *PostgreSQLDB:
		return d.TableExists(table)
	default:
		return false, fmt.Errorf("unsupported database type")
	}
}

// AddColumnIfNotExists adds a column if it doesn't exist
func AddColumnIfNotExists(db DB, table, column, definition string) error {
	exists, err := ColumnExists(db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)
	_, err = db.Exec(query)
	return err
}

// RunInTransaction executes a function within a transaction
func RunInTransaction(db DB, fn func(tx *sql.Tx) error) error {
	return Transaction(db, fn)
}
