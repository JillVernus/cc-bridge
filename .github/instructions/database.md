# Database Migrations Guide

**Specialist guide for managing database schema changes in cc-bridge.**

---

## Quick Facts

- **DB**: SQLite (local development) + PostgreSQL (optional production)
- **Tool**: Custom migration CLI (`backend-go/cmd/dbmigrate/`)
- **Storage**: Migrations live in `backend-go/cmd/dbmigrate/migrations/`
- **Format**: SQL files named `001_init.sql`, `002_add_users.sql`, etc.
- **Tracking**: Applied migrations stored in `migrations_applied` table
- **Rollbacks**: Reversible with down migrations

---

## Database Schema Overview

### SQLite (Development)

```
request_logs.db
├── migrations_applied (track applied migrations)
├── api_keys (API key management)
├── channels (upstream channel config)
├── request_logs (audit trail)
├── quota_usage (per-channel usage tracking)
└── circuit_breaker_state (channel failure counts)
```

### PostgreSQL (Production)

Same schema, different connection string:

```
postgresql://user:pass@localhost:5432/cc_bridge

Tables:
├── migrations_applied
├── api_keys
├── channels
├── request_logs
├── quota_usage
└── circuit_breaker_state
```

---

## Migration File Format

### Naming Convention

```
NNN_description.sql
```

- `NNN`: 3-digit sequence (001, 002, 003...)
- `description`: lowercase, hyphens, descriptive (e.g., `add_circuit_breaker_state`)

### Structure

Each migration file contains both **UP** and **DOWN** migrations:

```sql
-- 001_init.sql

-- UP: Create initial schema
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used TIMESTAMP
);

CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    service_type TEXT NOT NULL,
    priority INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT,
    channel_id TEXT NOT NULL,
    model TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost REAL,
    duration_ms INTEGER,
    success BOOLEAN DEFAULT 1,
    FOREIGN KEY (channel_id) REFERENCES channels(id)
);

-- DOWN: Drop tables (reverse order of creation)
DROP TABLE IF EXISTS request_logs;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS api_keys;
```

### Comments & Documentation

```sql
-- 003_add_quota_tracking.sql

-- UP: Add quota tracking table for per-channel usage limits
-- Purpose: Support per-channel monthly usage quotas
-- Related: handler for /api/quota, config schema update

CREATE TABLE quota_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id TEXT NOT NULL UNIQUE,
    month TEXT NOT NULL,  -- YYYY-MM format
    request_count INTEGER DEFAULT 0,
    input_tokens_total INTEGER DEFAULT 0,
    output_tokens_total INTEGER DEFAULT 0,
    cost_total REAL DEFAULT 0.0,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (channel_id) REFERENCES channels(id),
    UNIQUE(channel_id, month)
);

-- Create index for monthly lookups
CREATE INDEX idx_quota_usage_month ON quota_usage(month);

-- DOWN
DROP TABLE IF EXISTS quota_usage;
```

---

## Running Migrations

### CLI Tool

```bash
cd backend-go/cmd/dbmigrate

# Run all pending migrations
go run main.go --db-path ../../.config/request_logs.db apply

# Rollback last migration
go run main.go --db-path ../../.config/request_logs.db rollback

# Show applied migrations
go run main.go --db-path ../../.config/request_logs.db status

# Create fresh database (DEV ONLY)
go run main.go --db-path ../../.config/request_logs.db reset
```

### Makefile Shortcuts

```bash
cd backend-go

# Apply pending migrations
make migrate

# Rollback last migration
make migrate-down

# Show migration status
make migrate-status

# Reset database (DEV ONLY - loses all data)
make migrate-reset
```

### On Application Startup

The application auto-runs pending migrations on startup:

```go
// backend-go/main.go
if err := db.RunMigrations(dbPath); err != nil {
	log.Fatalf("failed to run migrations: %v", err)
}
```

---

## Creating New Migrations

### Step-by-Step

1. **Determine migration type**:
   - Schema change (create/alter table)?
   - Data migration (transform existing data)?
   - Index optimization?

2. **Find next sequence number**:
   ```bash
   ls backend-go/cmd/dbmigrate/migrations/ | tail -1
   # Output: 003_add_quota_tracking.sql → next is 004
   ```

3. **Create migration file**:
   ```bash
   touch backend-go/cmd/dbmigrate/migrations/004_your_migration_name.sql
   ```

4. **Write UP and DOWN migrations**:
   ```sql
   -- 004_add_circuit_breaker_table.sql
   
   -- UP
   CREATE TABLE circuit_breaker_state (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       channel_id TEXT NOT NULL UNIQUE,
       failure_count INTEGER DEFAULT 0,
       last_failure TIMESTAMP,
       cooldown_until TIMESTAMP,
       
       FOREIGN KEY (channel_id) REFERENCES channels(id)
   );
   
   -- DOWN
   DROP TABLE IF EXISTS circuit_breaker_state;
   ```

5. **Test UP migration**:
   ```bash
   cd backend-go
   go run cmd/dbmigrate/main.go --db-path .config/request_logs.db apply
   
   # Verify schema
   sqlite3 .config/request_logs.db ".schema circuit_breaker_state"
   ```

6. **Test DOWN migration**:
   ```bash
   go run cmd/dbmigrate/main.go --db-path .config/request_logs.db rollback
   
   # Verify table dropped
   sqlite3 .config/request_logs.db ".tables"
   ```

7. **Commit migration file**:
   ```bash
   git add backend-go/cmd/dbmigrate/migrations/004_*.sql
   git commit -m "feat(db): add circuit breaker state tracking"
   ```

---

## Common Migration Patterns

### Adding a Column

```sql
-- 005_add_request_id_to_logs.sql

-- UP
ALTER TABLE request_logs ADD COLUMN request_id TEXT UNIQUE;
CREATE INDEX idx_request_logs_request_id ON request_logs(request_id);

-- DOWN
DROP INDEX IF EXISTS idx_request_logs_request_id;
ALTER TABLE request_logs DROP COLUMN request_id;
```

### Renaming a Column

```sql
-- 006_rename_cost_to_usage_cost.sql

-- UP (SQLite doesn't have native RENAME, so use workaround)
ALTER TABLE request_logs RENAME TO request_logs_old;
CREATE TABLE request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT,
    channel_id TEXT NOT NULL,
    model TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    usage_cost REAL,  -- renamed from cost
    duration_ms INTEGER,
    success BOOLEAN DEFAULT 1,
    FOREIGN KEY (channel_id) REFERENCES channels(id)
);
INSERT INTO request_logs SELECT * FROM request_logs_old;
DROP TABLE request_logs_old;

-- DOWN (reverse the rename)
ALTER TABLE request_logs RENAME TO request_logs_old;
CREATE TABLE request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT,
    channel_id TEXT NOT NULL,
    model TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost REAL,  -- renamed back
    duration_ms INTEGER,
    success BOOLEAN DEFAULT 1,
    FOREIGN KEY (channel_id) REFERENCES channels(id)
);
INSERT INTO request_logs SELECT * FROM request_logs_old;
DROP TABLE request_logs_old;
```

### Adding a Table with Seed Data

```sql
-- 007_create_pricing_tiers.sql

-- UP
CREATE TABLE pricing_tiers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    input_price_per_mtok REAL NOT NULL,
    output_price_per_mtok REAL NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert default pricing
INSERT INTO pricing_tiers (id, name, input_price_per_mtok, output_price_per_mtok) VALUES
    ('claude-3-opus', 'Claude 3 Opus', 0.015, 0.075),
    ('claude-3-sonnet', 'Claude 3 Sonnet', 0.003, 0.015),
    ('gpt-4-turbo', 'GPT-4 Turbo', 0.01, 0.03);

-- DOWN
DROP TABLE IF EXISTS pricing_tiers;
```

### Data Migration (Transform Existing Data)

```sql
-- 008_migrate_legacy_keys_format.sql

-- UP
-- Old format: keys stored in plain text
-- New format: keys stored hashed + salted

-- Create new columns
ALTER TABLE api_keys ADD COLUMN key_salt TEXT;
ALTER TABLE api_keys ADD COLUMN key_hash TEXT;

-- Note: Actual data transformation happens in Go code
-- (SQL can't call hash functions easily)
-- See backend_migration.go for Go-based migration

-- DOWN
DROP COLUMN key_salt;
DROP COLUMN key_hash;
```

**For complex data migrations, use Go code:**

```go
// backend-go/main_seed_restore_test.go (or dedicated migration file)
func migrateKeysToHash(db *sql.DB) error {
	rows, err := db.Query("SELECT id, key FROM api_keys WHERE key_hash IS NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return err
		}
		
		salt := generateSalt()
		hash := hashKey(key, salt)
		
		_, err := db.Exec(
			"UPDATE api_keys SET key_salt = ?, key_hash = ? WHERE id = ?",
			salt, hash, id,
		)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}
```

---

## Testing Migrations

### Unit Testing

```go
// cmd/dbmigrate/migrator_test.go

func TestMigrations(t *testing.T) {
	// Use temp database for testing
	tmpDB, _ := os.CreateTemp("", "test.db")
	defer os.Remove(tmpDB.Name())
	
	db := migrator.NewMigrator(tmpDB.Name())
	
	// Test UP migration
	if err := db.Apply(); err != nil {
		t.Fatalf("failed to apply: %v", err)
	}
	
	// Verify table exists
	var tableName string
	err := db.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='channels'",
	).Scan(&tableName)
	if err != nil {
		t.Error("channels table not created")
	}
	
	// Test DOWN migration
	if err := db.Rollback(); err != nil {
		t.Fatalf("failed to rollback: %v", err)
	}
	
	// Verify table dropped
	err = db.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='channels'",
	).Scan(&tableName)
	if err == nil {
		t.Error("channels table not dropped after rollback")
	}
}
```

### Integration Testing

```bash
# Test fresh migration from scratch
make migrate-reset
make test

# Test incremental migrations
git stash  # Undo pending changes
make migrate
git stash pop
make migrate
make test
```

---

## Troubleshooting

### "Migration X already applied"

If a migration fails midway and leaves the DB in bad state:

```bash
# Manually check applied migrations
sqlite3 .config/request_logs.db "SELECT * FROM migrations_applied;"

# If migration is marked applied but failed, manually remove it
sqlite3 .config/request_logs.db "DELETE FROM migrations_applied WHERE id = '004';"

# Re-run
make migrate
```

### "Constraint violation" or "Unique key exists"

Migration is trying to insert duplicate or violate constraint:

```sql
-- Before inserting duplicate primary key, check what exists
-- 009_fix_duplicate_channels.sql

-- DOWN (Previous state)
-- INSERT INTO channels VALUES ('ch-1', 'OpenAI', 'openai');

-- UP (Fix)
-- Skip duplicate inserts by checking existence first
INSERT OR IGNORE INTO channels VALUES ('ch-1', 'OpenAI', 'openai');
```

### Data Loss on Rollback

Always test DOWN migration in development first:

```bash
# Bad: Run on production without testing
make migrate-down  # Oops, lost data

# Good: Test locally first
rm .config/request_logs.db  # Fresh copy
make migrate-reset
make migrate  # Apply all migrations
make migrate-down  # Test rollback
# Verify data integrity
```

---

## Database Hygiene

### Regular Maintenance

```bash
# Vacuum database (reclaim space, optimize indices)
sqlite3 .config/request_logs.db "VACUUM;"

# Check database integrity
sqlite3 .config/request_logs.db "PRAGMA integrity_check;"

# View database stats
sqlite3 .config/request_logs.db "SELECT page_count * page_size / 1024.0 AS size_kb FROM pragma_page_count(), pragma_page_size();"
```

### Archiving Old Logs

```sql
-- 010_archive_old_request_logs.sql

-- Create archive table
CREATE TABLE request_logs_archive AS SELECT * FROM request_logs WHERE 0;

-- UP: Move logs older than 1 year to archive
INSERT INTO request_logs_archive 
SELECT * FROM request_logs 
WHERE timestamp < date('now', '-1 year');

DELETE FROM request_logs 
WHERE timestamp < date('now', '-1 year');

-- DOWN: Restore from archive
INSERT INTO request_logs SELECT * FROM request_logs_archive;
DELETE FROM request_logs_archive;
```

---

## See Also

- **DB initialization**: [backend-go/db_storage_init.go](../../backend-go/db_storage_init.go)
- **Migration CLI**: [backend-go/cmd/dbmigrate/main.go](../../backend-go/cmd/dbmigrate/main.go)
- **Config storage**: [backend-go/internal/config/](../../backend-go/internal/config/)
- **Request logging**: [backend-go/internal/requestlog/](../../backend-go/internal/requestlog/)

---

*Last updated: April 2026*
