package requestlog

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// NewManagerWithDB creates a new request log manager using a shared database connection.
// This is used when STORAGE_BACKEND=database to consolidate all storage into one DB.
// Unlike NewManager, this version does NOT initialize schemas (handled by migrations).
// connStr is the PostgreSQL connection string (empty for SQLite)
func NewManagerWithDB(db database.DB, connStr string) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		db:           db.Raw(),
		dbPath:       "", // No path when using shared DB
		broadcaster:  NewBroadcaster(),
		dialect:      db.Dialect(),
		connStr:      connStr,
		listenerCtx:  ctx,
		listenerStop: cancel,
	}

	// Start PostgreSQL LISTEN for cross-instance SSE (only for PostgreSQL)
	if m.isPostgres() && connStr != "" {
		if err := m.startPGListener(ctx); err != nil {
			log.Printf("⚠️ Failed to start PostgreSQL LISTEN: %v", err)
			log.Printf("📡 SSE will work for current instance only")
		}
	}

	log.Printf("📊 Request log manager initialized (using shared database)")
	return m, nil
}

// GetDBInterface returns the database as an interface for type checking.
// Returns nil if the manager was created with NewManager (owns its own connection).
func (m *Manager) GetDBInterface() database.DB {
	// When using shared DB, we don't have a database.DB wrapper
	// This is a compatibility method for the transition period
	return nil
}

// IsUsingSharedDB returns true if this manager is using a shared database connection
func (m *Manager) IsUsingSharedDB() bool {
	return m.dbPath == ""
}

// MigrateFromLegacyDB migrates data from an existing request_logs.db to the unified database.
// This is called during the transition from separate databases to unified storage.
func MigrateFromLegacyDB(legacyPath string, targetDB database.DB) error {
	// Open legacy database
	legacyDB, err := sql.Open("sqlite", legacyPath+"?_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer legacyDB.Close()

	// Check if legacy DB has data
	var count int
	err = legacyDB.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&count)
	if err != nil {
		// Table doesn't exist or other error - nothing to migrate
		log.Printf("📊 No legacy request_logs to migrate: %v", err)
		return nil
	}

	if count == 0 {
		log.Printf("📊 Legacy request_logs is empty, skipping migration")
		return nil
	}

	log.Printf("📊 Migrating %d request logs from legacy database...", count)

	// Migrate request_logs
	if err := migrateTable(legacyDB, targetDB, "request_logs"); err != nil {
		return err
	}

	// Migrate user_aliases
	if err := migrateTable(legacyDB, targetDB, "user_aliases"); err != nil {
		log.Printf("⚠️ Failed to migrate user_aliases: %v", err)
	}

	// Migrate api_keys
	if err := migrateTable(legacyDB, targetDB, "api_keys"); err != nil {
		log.Printf("⚠️ Failed to migrate api_keys: %v", err)
	}

	// Migrate channel_quota
	if err := migrateTable(legacyDB, targetDB, "channel_quota"); err != nil {
		log.Printf("⚠️ Failed to migrate channel_quota: %v", err)
	}

	// Migrate channel_suspensions
	if err := migrateTable(legacyDB, targetDB, "channel_suspensions"); err != nil {
		log.Printf("⚠️ Failed to migrate channel_suspensions: %v", err)
	}

	log.Printf("✅ Migration from legacy database completed")
	return nil
}

func migrateTable(source *sql.DB, target database.DB, tableName string) error {
	// Check if source table exists
	var tableExists int
	err := source.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return nil // Table doesn't exist, skip
	}

	// Get column names from source
	rows, err := source.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			continue
		}
		columns = append(columns, name)
	}

	if len(columns) == 0 {
		return nil
	}

	// Build column list for INSERT
	columnList := ""
	placeholders := ""
	for i, col := range columns {
		if i > 0 {
			columnList += ", "
			placeholders += ", "
		}
		columnList += col
		if target.Dialect() == database.DialectPostgreSQL {
			placeholders += fmt.Sprintf("$%d", i+1)
		} else {
			placeholders += "?"
		}
	}

	// Count rows to migrate
	var sourceCount int
	source.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&sourceCount)
	if sourceCount == 0 {
		return nil
	}

	// Check if target already has data (avoid duplicate migration)
	var targetCount int
	target.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&targetCount)
	if targetCount > 0 {
		log.Printf("📊 Table %s already has %d rows in target, skipping migration", tableName, targetCount)
		return nil
	}

	// Copy data in batches
	batchSize := 1000
	offset := 0
	migrated := 0

	for {
		dataRows, err := source.Query("SELECT " + columnList + " FROM " + tableName + " LIMIT ? OFFSET ?", batchSize, offset)
		if err != nil {
			return err
		}

		hasRows := false
		for dataRows.Next() {
			hasRows = true
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := dataRows.Scan(valuePtrs...); err != nil {
				continue
			}

			var insertQuery string
			if target.Dialect() == database.DialectPostgreSQL {
				insertQuery = "INSERT INTO " + tableName + " (" + columnList + ") VALUES (" + placeholders + ") ON CONFLICT DO NOTHING"
			} else {
				insertQuery = "INSERT OR IGNORE INTO " + tableName + " (" + columnList + ") VALUES (" + placeholders + ")"
			}
			_, err := target.Exec(insertQuery, values...)
			if err != nil {
				log.Printf("⚠️ Failed to insert row into %s: %v", tableName, err)
				continue
			}
			migrated++
		}
		dataRows.Close()

		if !hasRows {
			break
		}
		offset += batchSize
	}

	log.Printf("📊 Migrated %d rows to %s", migrated, tableName)
	return nil
}
