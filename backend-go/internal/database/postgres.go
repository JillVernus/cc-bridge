package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// PostgreSQLDB is the PostgreSQL implementation of DB
type PostgreSQLDB struct {
	*BaseDB
}

// NewPostgreSQL creates a new PostgreSQL database connection
func NewPostgreSQL(cfg Config) (*PostgreSQLDB, error) {
	connStr := cfg.URL
	if connStr == "" {
		connStr = buildPostgresConnString(cfg)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open PostgreSQL database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	pgDB := &PostgreSQLDB{
		BaseDB: &BaseDB{
			DB:      db,
			dialect: DialectPostgreSQL,
			helper:  NewDialectHelper(DialectPostgreSQL),
		},
	}

	log.Printf("📦 PostgreSQL database connected: %s", maskConnString(connStr))
	return pgDB, nil
}

// buildPostgresConnString builds a connection string from individual config fields
func buildPostgresConnString(cfg Config) string {
	connStr := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.Name, cfg.User, cfg.SSLMode,
	)
	if cfg.Password != "" {
		connStr += fmt.Sprintf(" password=%s", cfg.Password)
	}
	return connStr
}

// maskConnString masks sensitive parts of a connection string for logging
func maskConnString(connStr string) string {
	// Simple masking - just show host info
	if len(connStr) > 50 {
		return connStr[:30] + "..."
	}
	return connStr
}

// ColumnExists checks if a column exists in a table (PostgreSQL-specific)
func (db *PostgreSQLDB) ColumnExists(table, column string) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`
	err := db.QueryRow(query, table, column).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TableExists checks if a table exists (PostgreSQL-specific)
func (db *PostgreSQLDB) TableExists(table string) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name = $1
	`
	err := db.QueryRow(query, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
