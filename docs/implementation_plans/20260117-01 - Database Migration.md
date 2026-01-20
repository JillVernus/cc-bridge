# Database Migration: JSON + SQLite → Unified Database

## Background
Currently cc-bridge uses hybrid storage (JSON files + SQLite) which makes it difficult to share data across devices. The goal is to consolidate all storage into a database layer that supports both SQLite (single-instance) and PostgreSQL (multi-instance HA).

## Approach
1. **Phase 1**: Migrate JSON files to SQLite (consolidate storage)
2. **Phase 2**: Add database abstraction layer + PostgreSQL driver support

---

## Phase 1: JSON Files → SQLite

### Current JSON Files:
| File | Manager | Hot-reload |
|------|---------|------------|
| `config.json` | ConfigManager | fsnotify |
| `pricing.json` | PricingManager | fsnotify |
| `model-aliases.json` | AliasesManager | fsnotify |
| `quota_usage.json` | UsageManager | None |

### Steps

- [x] Step 1.1: Create database abstraction package (`internal/database/`)
  - `database.go` - DB interface + factory
  - `sqlite.go` - SQLite implementation (refactor from requestlog)
  - `dialect.go` - SQL dialect helpers
  - `migration.go` - Schema migration runner
  - `postgres.go` - PostgreSQL stub for Phase 2

- [x] Step 1.2: Create new database tables
  - `settings` - Key-value store for simple configs
  - `channels` - Unified Messages + Responses channels
  - `model_pricing` - Model pricing data
  - `model_aliases` - Model alias mappings
  - `channel_usage` - Usage tracking (replaces quota_usage.json)

- [x] Step 1.3: Refactor ConfigManager
  - Added `STORAGE_BACKEND` env var (`json`|`database`)
  - Created `db_storage.go` adapter with DB sync capabilities
  - Implements JSON → DB auto-migration
  - DB polling for hot-reload (configurable interval)
  - Keeps existing JSON-based ConfigManager intact for backward compatibility

- [x] Step 1.4: Refactor PricingManager
  - Created `pricing/db_storage.go` adapter
  - DB-backed with polling support

- [x] Step 1.5: Refactor AliasesManager
  - Created `aliases/db_storage.go` adapter
  - DB-backed with polling support

- [x] Step 1.6: Refactor UsageManager
  - Created `quota/db_storage.go` adapter
  - Direct DB read/write for usage tracking

- [x] Step 1.7: Update main.go initialization
  - Created `db_storage_init.go` with `InitDBStorage()` helper
  - Conditional initialization based on `STORAGE_BACKEND` env var
  - Auto-migration from JSON files to database
  - Polling for config changes when using database storage

- [x] Step 1.8: Testing Phase 1
  - Verify auto-migration from existing JSON files ✓
  - Verify hot-reload via DB polling ✓
  - Verify all CRUD operations work ✓

- [x] Step 1.9: Consolidate request_logs.db into cc-bridge.db
  - Currently `request_logs.db` contains: `request_logs`, `user_aliases`, `api_keys`, `channel_quota`
  - Move all table schemas to unified `cc-bridge.db` migrations ✓
  - Refactor `RequestLogManager` to accept `database.DB` interface ✓
  - Refactor `APIKeyManager` to use shared database connection ✓
  - Update `main.go` initialization order ✓
  - Add data migration from existing `request_logs.db` ✓
  - Added `ensureAPIKeysColumns()` to handle existing DBs missing permission columns ✓
  - Verified: Old 84.5MB DB migrated to 29.6MB (SQLite fragmentation reclaimed) ✓

---

## Phase 2: Add PostgreSQL Support

### Steps

- [x] Step 2.1: Add database configuration to env.go
  ```
  DATABASE_TYPE=sqlite|postgresql
  DATABASE_URL=...
  DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD, DB_SSLMODE
  ```

- [x] Step 2.2: Implement PostgreSQL driver (`internal/database/postgres.go`)
  - Driver: `github.com/lib/pq`
  - Connection pooling: 25 max open, 5 max idle, 5min lifetime

- [x] Step 2.3: Handle SQL dialect differences
  | Feature | SQLite | PostgreSQL |
  |---------|--------|------------|
  | Auto-increment | `AUTOINCREMENT` | `SERIAL` |
  | Placeholder | `?` | `$1, $2` |
  | BLOB | `BLOB` | `BYTEA` |
  | Datetime | `DATETIME` | `TIMESTAMPTZ` |
  | Upsert | `INSERT OR REPLACE` | `ON CONFLICT DO UPDATE` |
  | Epoch time | `strftime('%s', ts)` | `EXTRACT(EPOCH FROM ts)` |
  | Boolean default | `DEFAULT 0` | `DEFAULT FALSE` |

- [x] Step 2.4: Create migration SQL files
  - Dialect-aware templates via `convertMigrationSQL()`
  - `schema_migrations` tracking table

- [x] Step 2.5: Create data migration CLI tool (`cmd/dbmigrate/`)
  - Export SQLite to SQL dump (--dry-run)
  - Import to PostgreSQL with ON CONFLICT DO NOTHING
  - Successfully migrated 35,935 request logs

- [x] Step 2.6: PostgreSQL query compatibility fixes
  - Added `convertQuery()` helper to convert `?` to `$1, $2...`
  - Fixed all dynamic queries in `requestlog/manager.go`
  - Fixed queries in `requestlog/debug.go`
  - Fixed queries in `config/db_storage.go`
  - Fixed queries in `apikey/manager.go`
  - Fixed `LastInsertId()` → `RETURNING id` for PostgreSQL
  - Enabled error logging for debug log failures

- [x] Step 2.7: Testing Phase 2
  - Test with PostgreSQL in Docker ✓
  - Verify config loading/hot-reload ✓
  - Verify request logging ✓
  - Verify debug logging ✓
  - Verify API key management ✓

---

## Docker/Unraid Deployment

### Environment Variables for PostgreSQL
```env
STORAGE_BACKEND=database
DATABASE_TYPE=postgresql
DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=disable
```

### Migration Steps
1. Build Docker image with `dbmigrate` tool included
2. Start container normally
3. Exec into container: `docker exec -it <container> /bin/sh`
4. Run migration: `/app/dbmigrate -source /app/data/cc-bridge.db -target "postgres://..." -migrate`
5. Add PostgreSQL env vars in Unraid Docker template
6. Restart container

### Files Added/Modified for PostgreSQL
| File | Changes |
|------|---------|
| `internal/database/postgres.go` | Full PostgreSQL driver implementation |
| `internal/database/migration.go` | BLOB→BYTEA, BOOLEAN default fixes |
| `cmd/dbmigrate/main.go` | Data migration CLI tool |
| `docker-compose.postgres.yml` | PostgreSQL compose config |
| `scripts/migrate-to-postgres.sh` | Migration helper script |
| `Dockerfile` | Include dbmigrate in build |
| `backend-go/Makefile` | Build target for dbmigrate |

---

## Files to Modify

### Phase 1:
| File | Changes |
|------|---------|
| `internal/database/` (new) | Database abstraction layer |
| `internal/config/config.go` | Remove fsnotify, add DB backend |
| `internal/config/json_migration.go` (new) | JSON→DB migration |
| `internal/pricing/pricing.go` | DB backend |
| `internal/aliases/aliases.go` | DB backend |
| `internal/quota/usage_manager.go` | DB backend |
| `main.go` | New initialization order |
| `internal/requestlog/manager.go` | Accept `database.DB`, remove own connection |
| `internal/apikey/manager.go` | Use shared database connection |
| `internal/database/migration.go` | Add request_logs/api_keys table schemas |

### Phase 2:
| File | Changes |
|------|---------|
| `internal/config/env.go` | Add DB config vars |
| `internal/database/postgres.go` (new) | PostgreSQL driver |
| `cmd/dbmigrate/` (new) | Migration CLI tool |

---

## New Table Schemas

### settings
```sql
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    category TEXT DEFAULT 'general',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### channels
```sql
CREATE TABLE channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id TEXT NOT NULL UNIQUE,
    channel_type TEXT NOT NULL,  -- 'messages' | 'responses'
    name TEXT NOT NULL,
    description TEXT,
    service_type TEXT NOT NULL,
    base_url TEXT,
    status TEXT DEFAULT 'active',
    priority INTEGER DEFAULT 0,
    promotion_until DATETIME,
    -- Quota settings
    quota_type TEXT,
    quota_limit REAL DEFAULT 0,
    quota_reset_at DATETIME,
    quota_reset_interval INTEGER,
    quota_reset_unit TEXT,
    quota_reset_mode TEXT DEFAULT 'fixed',
    -- Rate limiting
    rate_limit_rpm INTEGER DEFAULT 0,
    queue_enabled BOOLEAN DEFAULT 0,
    queue_timeout INTEGER DEFAULT 60,
    key_load_balance TEXT,
    -- Complex fields (JSON)
    api_keys TEXT,
    model_mapping TEXT,
    price_multipliers TEXT,
    oauth_tokens TEXT,
    quota_models TEXT,
    composite_mappings TEXT,
    -- Timestamps
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### model_pricing
```sql
CREATE TABLE model_pricing (
    model_id TEXT PRIMARY KEY,
    input_price REAL NOT NULL,
    output_price REAL NOT NULL,
    cache_creation_price REAL,
    cache_read_price REAL,
    description TEXT,
    export_to_models BOOLEAN DEFAULT 1,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### model_aliases
```sql
CREATE TABLE model_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias_type TEXT NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    sort_order INTEGER DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### channel_usage
```sql
CREATE TABLE channel_usage (
    channel_id INTEGER NOT NULL,
    channel_type TEXT NOT NULL,
    used REAL DEFAULT 0,
    last_reset_at DATETIME,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, channel_type)
);
```

### request_logs (consolidated from request_logs.db)
```sql
CREATE TABLE request_logs (
    id TEXT PRIMARY KEY,
    status TEXT DEFAULT 'completed',
    initial_time DATETIME NOT NULL,
    complete_time DATETIME,
    duration_ms INTEGER DEFAULT 0,
    provider TEXT NOT NULL,
    provider_name TEXT,
    model TEXT NOT NULL,
    response_model TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cache_creation_input_tokens INTEGER DEFAULT 0,
    cache_read_input_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    price REAL DEFAULT 0,
    input_cost REAL DEFAULT 0,
    output_cost REAL DEFAULT 0,
    cache_creation_cost REAL DEFAULT 0,
    cache_read_cost REAL DEFAULT 0,
    http_status INTEGER DEFAULT 0,
    stream BOOLEAN NOT NULL,
    channel_id INTEGER,
    channel_name TEXT,
    endpoint TEXT,
    client_id TEXT,
    session_id TEXT,
    reasoning_effort TEXT,
    error TEXT,
    upstream_error TEXT,
    failover_info TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_request_logs_initial_time ON request_logs(initial_time);
CREATE INDEX idx_request_logs_status ON request_logs(status);
CREATE INDEX idx_request_logs_session_id ON request_logs(session_id);
```

### user_aliases (consolidated from request_logs.db)
```sql
CREATE TABLE user_aliases (
    user_id TEXT PRIMARY KEY,
    alias TEXT NOT NULL UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_user_aliases_alias ON user_aliases(alias);
```

### api_keys (consolidated from request_logs.db)
```sql
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    is_admin BOOLEAN NOT NULL DEFAULT 0,
    rate_limit_rpm INTEGER NOT NULL DEFAULT 0,
    allowed_endpoints TEXT,
    allowed_channels_msg TEXT,
    allowed_channels_resp TEXT,
    allowed_models TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME
);
CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_status ON api_keys(status);
```

---

## Commits
<!-- Added after each commit -->

