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

	// Transaction support
	Begin() (*sql.Tx, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)

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

// Transaction executes a function within a transaction
func Transaction(db DB, fn func(tx *sql.Tx) error) error {
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
func TransactionContext(ctx context.Context, db DB, fn func(tx *sql.Tx) error) error {
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
