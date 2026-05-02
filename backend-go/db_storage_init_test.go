package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestOpenDatabaseWithRetry_RetriesTransientOpenFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-bridge.db")
	cfg := database.Config{
		Type: database.DialectSQLite,
		URL:  dbPath,
	}

	attempts := 0
	migrations := 0

	db, err := openDatabaseWithRetry(
		cfg,
		2,
		0,
		func(cfg database.Config) (database.DB, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("database is starting")
			}
			return database.New(cfg)
		},
		func(database.DB) error {
			migrations++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("openDatabaseWithRetry returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	if attempts != 2 {
		t.Fatalf("expected 2 open attempts, got %d", attempts)
	}
	if migrations != 1 {
		t.Fatalf("expected migrations to run once after successful open, got %d", migrations)
	}
}

func TestOpenDatabaseWithRetry_RetriesTransientMigrationFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-bridge.db")
	cfg := database.Config{
		Type: database.DialectSQLite,
		URL:  dbPath,
	}

	migrations := 0

	db, err := openDatabaseWithRetry(
		cfg,
		2,
		0,
		database.New,
		func(database.DB) error {
			migrations++
			if migrations == 1 {
				return errors.New("database is recovering")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("openDatabaseWithRetry returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	if migrations != 2 {
		t.Fatalf("expected migrations to retry once, got %d runs", migrations)
	}
}

func TestDatabaseStartupRetryConfigDefaults(t *testing.T) {
	t.Setenv("DATABASE_STARTUP_RETRIES", "")
	t.Setenv("DATABASE_STARTUP_RETRY_DELAY_SECONDS", "")

	attempts, delay := databaseStartupRetryConfig()

	if attempts < 2 {
		t.Fatalf("expected retry config to make at least 2 attempts, got %d", attempts)
	}
	if delay <= 0*time.Second {
		t.Fatalf("expected positive retry delay, got %v", delay)
	}
}

func TestInitDBStorage_FailsClosedWhenDatabaseBackendCannotInitialize(t *testing.T) {
	oldOpenDatabase := openDatabaseForStorage
	oldRunMigrations := runDatabaseMigrationsForStorage
	oldFatalf := fatalf
	t.Cleanup(func() {
		openDatabaseForStorage = oldOpenDatabase
		runDatabaseMigrationsForStorage = oldRunMigrations
		fatalf = oldFatalf
	})

	t.Setenv("DATABASE_STARTUP_RETRIES", "1")
	t.Setenv("DATABASE_STARTUP_RETRY_DELAY_SECONDS", "1")

	openDatabaseForStorage = func(database.Config) (database.DB, error) {
		return nil, errors.New("postgres unavailable")
	}
	runDatabaseMigrationsForStorage = func(database.DB) error {
		t.Fatalf("migrations should not run when database open fails")
		return nil
	}

	var fatalMessage string
	fatalf = func(format string, args ...interface{}) {
		fatalMessage = fmt.Sprintf(format, args...)
		panic("fatal called")
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected InitDBStorage to stop startup")
		}
		if recovered != "fatal called" {
			t.Fatalf("unexpected panic: %v", recovered)
		}
		if !strings.Contains(fatalMessage, "STORAGE_BACKEND=database") {
			t.Fatalf("fatal message should mention explicit database backend, got %q", fatalMessage)
		}
		if strings.Contains(strings.ToLower(fatalMessage), "falling back") {
			t.Fatalf("fatal message should not mention fallback, got %q", fatalMessage)
		}
	}()

	InitDBStorage(&config.EnvConfig{
		StorageBackend: "database",
		DatabaseType:   "postgresql",
		DatabaseURL:    "postgres://ccbridge.invalid/db",
	}, nil)
}
