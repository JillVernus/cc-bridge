package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Dialect represents the database dialect
type Dialect string

const (
	DialectSQLite     Dialect = "sqlite"
	DialectPostgreSQL Dialect = "postgresql"
)

// Config holds database configuration
type Config struct {
	Type     Dialect // sqlite or postgresql
	URL      string  // Connection string or SQLite path
	Host     string  // PostgreSQL host
	Port     int     // PostgreSQL port
	Name     string  // PostgreSQL database name
	User     string  // PostgreSQL user
	Password string  // PostgreSQL password
	SSLMode  string  // PostgreSQL SSL mode
}

// ConfigFromEnv creates a Config from environment variables
func ConfigFromEnv() Config {
	dbType := os.Getenv("DATABASE_TYPE")
	if dbType == "" {
		dbType = "sqlite"
	}

	cfg := Config{
		Type:     Dialect(dbType),
		URL:      os.Getenv("DATABASE_URL"),
		Host:     getEnvOrDefault("DB_HOST", "localhost"),
		Port:     getEnvAsIntOrDefault("DB_PORT", 5432),
		Name:     getEnvOrDefault("DB_NAME", "ccbridge"),
		User:     getEnvOrDefault("DB_USER", "ccbridge"),
		Password: os.Getenv("DB_PASSWORD"),
		SSLMode:  getEnvOrDefault("DB_SSLMODE", "disable"),
	}

	// Default SQLite path
	if cfg.Type == DialectSQLite && cfg.URL == "" {
		cfg.URL = ".config/cc-bridge.db"
	}

	return cfg
}

// DB is the database interface that abstracts SQLite and PostgreSQL
type DB interface {
	// Core SQL operations
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row

	// Transaction support — returns a dialect-aware Tx that converts placeholders
	Begin() (*Tx, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error)

	// Dialect information
	Dialect() Dialect

	// Connection management
	Ping() error
	Close() error

	// Get underlying *sql.DB (for compatibility during migration)
	Raw() *sql.DB
}

// BaseDB wraps *sql.DB with dialect information
type BaseDB struct {
	*sql.DB
	dialect Dialect
	helper  *DialectHelper
}

// New creates a new database connection based on configuration
func New(cfg Config) (DB, error) {
	switch cfg.Type {
	case DialectSQLite:
		return NewSQLite(cfg)
	case DialectPostgreSQL:
		return NewPostgreSQL(cfg)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}
}

// Dialect returns the database dialect
func (db *BaseDB) Dialect() Dialect {
	return db.dialect
}

// Raw returns the underlying *sql.DB
func (db *BaseDB) Raw() *sql.DB {
	return db.DB
}

// Helper returns the dialect helper for SQL generation
func (db *BaseDB) Helper() *DialectHelper {
	return db.helper
}

// Exec executes a query with automatic placeholder conversion for PostgreSQL
func (db *BaseDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.DB.Exec(ConvertPlaceholders(query, db.dialect), args...)
}

// ExecContext executes a query with context and automatic placeholder conversion
func (db *BaseDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.DB.ExecContext(ctx, ConvertPlaceholders(query, db.dialect), args...)
}

// Query executes a query with automatic placeholder conversion for PostgreSQL
func (db *BaseDB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.DB.Query(ConvertPlaceholders(query, db.dialect), args...)
}

// QueryContext executes a query with context and automatic placeholder conversion
func (db *BaseDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return db.DB.QueryContext(ctx, ConvertPlaceholders(query, db.dialect), args...)
}

// QueryRow executes a query returning a single row with automatic placeholder conversion
func (db *BaseDB) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.DB.QueryRow(ConvertPlaceholders(query, db.dialect), args...)
}

// QueryRowContext executes a query with context returning a single row with automatic placeholder conversion
func (db *BaseDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return db.DB.QueryRowContext(ctx, ConvertPlaceholders(query, db.dialect), args...)
}

// Begin starts a transaction and returns a dialect-aware Tx wrapper
func (db *BaseDB) Begin() (*Tx, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, dialect: db.dialect}, nil
}

// BeginTx starts a transaction with options and returns a dialect-aware Tx wrapper
func (db *BaseDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, dialect: db.dialect}, nil
}

// Tx wraps *sql.Tx with automatic placeholder conversion for PostgreSQL
type Tx struct {
	*sql.Tx
	dialect Dialect
}

// Exec executes a query within the transaction with automatic placeholder conversion
func (tx *Tx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.Exec(ConvertPlaceholders(query, tx.dialect), args...)
}

// ExecContext executes a query with context within the transaction
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, ConvertPlaceholders(query, tx.dialect), args...)
}

// Query executes a query within the transaction with automatic placeholder conversion
func (tx *Tx) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return tx.Tx.Query(ConvertPlaceholders(query, tx.dialect), args...)
}

// QueryContext executes a query with context within the transaction
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, ConvertPlaceholders(query, tx.dialect), args...)
}

// QueryRow executes a query returning a single row within the transaction
func (tx *Tx) QueryRow(query string, args ...interface{}) *sql.Row {
	return tx.Tx.QueryRow(ConvertPlaceholders(query, tx.dialect), args...)
}

// QueryRowContext executes a query with context returning a single row within the transaction
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, ConvertPlaceholders(query, tx.dialect), args...)
}

// Prepare creates a prepared statement with automatic placeholder conversion
func (tx *Tx) Prepare(query string) (*sql.Stmt, error) {
	return tx.Tx.Prepare(ConvertPlaceholders(query, tx.dialect))
}

// Transaction executes a function within a transaction
func Transaction(db DB, fn func(tx *Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// TransactionContext executes a function within a transaction with context
func TransactionContext(ctx context.Context, db DB, fn func(tx *Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// WaitForDB waits for the database to be ready
func WaitForDB(db DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("database connection timeout after %v", timeout)
		case <-ticker.C:
			if err := db.Ping(); err == nil {
				return nil
			} else {
				log.Printf("Waiting for database connection: %v", err)
			}
		}
	}
}

// Helper functions

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		var result int
		if _, err := fmt.Sscanf(val, "%d", &result); err == nil {
			return result
		}
	}
	return defaultVal
}

// ConvertPlaceholders converts ? placeholders to dialect-specific format
func ConvertPlaceholders(query string, dialect Dialect) string {
	if dialect != DialectPostgreSQL {
		return query
	}

	// Convert ? to $1, $2, etc. for PostgreSQL
	var result strings.Builder
	paramIndex := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			paramIndex++
			result.WriteString(fmt.Sprintf("$%d", paramIndex))
		} else {
			result.WriteByte(query[i])
		}
	}
	return result.String()
}
