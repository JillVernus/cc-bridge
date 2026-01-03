package requestlog

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Manager manages request log storage using SQLite
type Manager struct {
	db     *sql.DB
	mu     sync.RWMutex
	dbPath string
}

// NewManager creates a new request log manager with SQLite storage
func NewManager(dbPath string) (*Manager, error) {
	if dbPath == "" {
		dbPath = ".config/request_logs.db"
	}

	// Add connection parameters for busy timeout and other settings
	// _busy_timeout=5000 - wait up to 5 seconds when database is locked
	// _txlock=immediate - acquire write lock immediately in transactions
	connStr := dbPath + "?_busy_timeout=5000&_txlock=immediate"
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Limit to single connection to avoid lock contention
	// SQLite doesn't benefit from multiple write connections
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("⚠️ Failed to enable WAL mode: %v", err)
	}

	// Set busy timeout to wait up to 5 seconds when database is locked (backup for connection string)
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		log.Printf("⚠️ Failed to set busy timeout: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		log.Printf("⚠️ Failed to enable foreign keys: %v", err)
	}

	m := &Manager{
		db:     db,
		dbPath: dbPath,
	}

	if err := m.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Printf("📊 Request log manager initialized: %s", dbPath)
	return m, nil
}

// initSchema creates the necessary tables and indexes
func (m *Manager) initSchema() error {
	// Create user_aliases table for storing user ID to alias mappings
	aliasSchema := `
	CREATE TABLE IF NOT EXISTS user_aliases (
		user_id TEXT PRIMARY KEY,
		alias TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_user_aliases_alias ON user_aliases(alias);
	`
	if _, err := m.db.Exec(aliasSchema); err != nil {
		return fmt.Errorf("failed to create user_aliases table: %w", err)
	}

	// Create table without status index first (for backward compatibility)
	// Note: For existing databases with user_id column, migration will rename it to client_id
	schema := `
	CREATE TABLE IF NOT EXISTS request_logs (
		id TEXT PRIMARY KEY,
		status TEXT DEFAULT 'completed',
		initial_time DATETIME NOT NULL,
		complete_time DATETIME,
		duration_ms INTEGER DEFAULT 0,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_creation_input_tokens INTEGER DEFAULT 0,
		cache_read_input_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		price REAL DEFAULT 0,
		http_status INTEGER DEFAULT 0,
		stream BOOLEAN NOT NULL,
		channel_id INTEGER,
		channel_name TEXT,
		endpoint TEXT,
		client_id TEXT,
		error TEXT,
		upstream_error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_request_logs_initial_time ON request_logs(initial_time);
	CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider);
	CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model);
	CREATE INDEX IF NOT EXISTS idx_request_logs_http_status ON request_logs(http_status);
	CREATE INDEX IF NOT EXISTS idx_request_logs_endpoint ON request_logs(endpoint);
	`

	_, err := m.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: Add status column if it doesn't exist (for existing databases)
	var count int
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='status'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check status column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN status TEXT DEFAULT 'completed'`)
		if err != nil {
			log.Printf("⚠️ Failed to add status column: %v", err)
		} else {
			log.Printf("✅ Added status column to request_logs table")
		}
	}

	// Create status index after ensuring column exists
	_, err = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status)`)
	if err != nil {
		log.Printf("⚠️ Failed to create status index: %v", err)
	}

	// Migration: Add provider_name column if it doesn't exist
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='provider_name'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check provider_name column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN provider_name TEXT`)
		if err != nil {
			log.Printf("⚠️ Failed to add provider_name column: %v", err)
		} else {
			log.Printf("✅ Added provider_name column to request_logs table")
		}
	}

	// Migration: Add upstream_error column if it doesn't exist
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='upstream_error'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check upstream_error column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN upstream_error TEXT`)
		if err != nil {
			log.Printf("⚠️ Failed to add upstream_error column: %v", err)
		} else {
			log.Printf("✅ Added upstream_error column to request_logs table")
		}
	}

	// Migration: Add cost breakdown columns if they don't exist
	costColumns := []string{"input_cost", "output_cost", "cache_creation_cost", "cache_read_cost"}
	for _, col := range costColumns {
		err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name=?`, col).Scan(&count)
		if err != nil {
			log.Printf("⚠️ Failed to check %s column: %v", col, err)
		} else if count == 0 {
			_, err = m.db.Exec(fmt.Sprintf(`ALTER TABLE request_logs ADD COLUMN %s REAL DEFAULT 0`, col))
			if err != nil {
				log.Printf("⚠️ Failed to add %s column: %v", col, err)
			} else {
				log.Printf("✅ Added %s column to request_logs table", col)
			}
		}
	}

	// Migration: Add response_model column if it doesn't exist
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='response_model'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check response_model column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN response_model TEXT`)
		if err != nil {
			log.Printf("⚠️ Failed to add response_model column: %v", err)
		} else {
			log.Printf("✅ Added response_model column to request_logs table")
		}
	}

	// Migration: Add reasoning_effort column if it doesn't exist
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='reasoning_effort'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check reasoning_effort column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN reasoning_effort TEXT`)
		if err != nil {
			log.Printf("⚠️ Failed to add reasoning_effort column: %v", err)
		} else {
			log.Printf("✅ Added reasoning_effort column to request_logs table")
		}
	}

	// Migration: Add session_id column if it doesn't exist (for Claude Code conversation tracking)
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='session_id'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check session_id column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN session_id TEXT`)
		if err != nil {
			log.Printf("⚠️ Failed to add session_id column: %v", err)
		} else {
			log.Printf("✅ Added session_id column to request_logs table")
		}
	}

	// Create index for session_id to improve query performance
	_, err = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_logs_session_id ON request_logs(session_id)`)
	if err != nil {
		log.Printf("⚠️ Failed to create session_id index: %v", err)
	}

	// Migration: Add api_key_id column if it doesn't exist (for API key tracking)
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='api_key_id'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check api_key_id column: %v", err)
	} else if count == 0 {
		_, err = m.db.Exec(`ALTER TABLE request_logs ADD COLUMN api_key_id INTEGER`)
		if err != nil {
			log.Printf("⚠️ Failed to add api_key_id column: %v", err)
		} else {
			log.Printf("✅ Added api_key_id column to request_logs table")
		}
	}

	// Create index for api_key_id to improve query performance
	_, err = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_logs_api_key_id ON request_logs(api_key_id)`)
	if err != nil {
		log.Printf("⚠️ Failed to create api_key_id index: %v", err)
	}

	// Migration: Rename user_id to client_id (user_id was actually client/machine identifier)
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='user_id'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check user_id column: %v", err)
	} else if count > 0 {
		// Column exists, rename it
		_, err = m.db.Exec(`ALTER TABLE request_logs RENAME COLUMN user_id TO client_id`)
		if err != nil {
			log.Printf("⚠️ Failed to rename user_id to client_id: %v", err)
			// If rename fails, return error to prevent further issues
			return fmt.Errorf("failed to rename user_id to client_id: %w", err)
		}
		log.Printf("✅ Renamed user_id column to client_id in request_logs table")
		// Drop old index and create new one
		m.db.Exec(`DROP INDEX IF EXISTS idx_request_logs_user_id`)
	}

	// Ensure client_id column exists (check after potential migration)
	err = m.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='client_id'`).Scan(&count)
	if err != nil {
		log.Printf("⚠️ Failed to check client_id column: %v", err)
	} else if count > 0 {
		// Column exists, ensure index exists
		_, err = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_logs_client_id ON request_logs(client_id)`)
		if err != nil {
			log.Printf("⚠️ Failed to create client_id index: %v", err)
		}
	}

	// Migration: Update old error records with timeout error message to use timeout status
	// This fixes records created before the StatusTimeout constant was added
	result, err := m.db.Exec(`UPDATE request_logs SET status = ? WHERE status = ? AND (error = 'request timed out' OR error = 'timeout')`, StatusTimeout, StatusError)
	if err != nil {
		log.Printf("⚠️ Failed to migrate timeout records: %v", err)
	} else if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
		log.Printf("✅ Migrated %d old timeout records to new status", rowsAffected)
	}

	// Create channel_quota table for persisting Codex quota data
	quotaSchema := `
	CREATE TABLE IF NOT EXISTS channel_quota (
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
	`
	if _, err := m.db.Exec(quotaSchema); err != nil {
		log.Printf("⚠️ Failed to create channel_quota table: %v", err)
	}

	// Create request_debug_logs table for storing full request/response data
	debugSchema := `
	CREATE TABLE IF NOT EXISTS request_debug_logs (
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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (request_id) REFERENCES request_logs(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_debug_logs_created ON request_debug_logs(created_at);
	`
	if _, err := m.db.Exec(debugSchema); err != nil {
		log.Printf("⚠️ Failed to create request_debug_logs table: %v", err)
	}

	return nil
}

// Add inserts a new request log record
func (m *Manager) Add(record *RequestLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if record.ID == "" {
		record.ID = generateID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.Status == "" {
		record.Status = StatusCompleted
	}

	// Calculate total tokens
	record.TotalTokens = record.InputTokens + record.OutputTokens

	query := `
	INSERT INTO request_logs (
		id, status, initial_time, complete_time, duration_ms,
		provider, provider_name, model, response_model, reasoning_effort, input_tokens, output_tokens,
		cache_creation_input_tokens, cache_read_input_tokens, total_tokens,
		price, input_cost, output_cost, cache_creation_cost, cache_read_cost,
		http_status, stream, channel_id, channel_name,
		endpoint, client_id, session_id, api_key_id, error, upstream_error, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Convert api_key_id to nullable value (nil = not set, 0 = master key)
	var apiKeyID interface{}
	if record.APIKeyID != nil {
		apiKeyID = *record.APIKeyID
	} else {
		apiKeyID = nil
	}

	_, err := m.db.Exec(query,
		record.ID,
		record.Status,
		record.InitialTime,
		record.CompleteTime,
		record.DurationMs,
		record.Type,
		record.ProviderName,
		record.Model,
		record.ResponseModel,
		record.ReasoningEffort,
		record.InputTokens,
		record.OutputTokens,
		record.CacheCreationInputTokens,
		record.CacheReadInputTokens,
		record.TotalTokens,
		record.Price,
		record.InputCost,
		record.OutputCost,
		record.CacheCreationCost,
		record.CacheReadCost,
		record.HTTPStatus,
		record.Stream,
		record.ChannelID,
		record.ChannelName,
		record.Endpoint,
		record.ClientID,
		record.SessionID,
		apiKeyID,
		record.Error,
		record.UpstreamError,
		record.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert request log: %w", err)
	}

	return nil
}

// Update updates an existing request log record (used to complete a pending request)
func (m *Manager) Update(id string, record *RequestLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Calculate total tokens
	record.TotalTokens = record.InputTokens + record.OutputTokens

	query := `
	UPDATE request_logs SET
		status = ?,
		complete_time = ?,
		duration_ms = ?,
		provider = ?,
		provider_name = ?,
		response_model = ?,
		input_tokens = ?,
		output_tokens = ?,
		cache_creation_input_tokens = ?,
		cache_read_input_tokens = ?,
		total_tokens = ?,
		price = ?,
		input_cost = ?,
		output_cost = ?,
		cache_creation_cost = ?,
		cache_read_cost = ?,
		http_status = ?,
		channel_id = ?,
		channel_name = ?,
		error = ?,
		upstream_error = ?
	WHERE id = ?
	`

	result, err := m.db.Exec(query,
		record.Status,
		record.CompleteTime,
		record.DurationMs,
		record.Type,
		record.ProviderName,
		record.ResponseModel,
		record.InputTokens,
		record.OutputTokens,
		record.CacheCreationInputTokens,
		record.CacheReadInputTokens,
		record.TotalTokens,
		record.Price,
		record.InputCost,
		record.OutputCost,
		record.CacheCreationCost,
		record.CacheReadCost,
		record.HTTPStatus,
		record.ChannelID,
		record.ChannelName,
		record.Error,
		record.UpstreamError,
		id,
	)

	if err != nil {
		return fmt.Errorf("failed to update request log: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("request log not found: %s", id)
	}

	return nil
}

// GetRecent retrieves the most recent request logs
func (m *Manager) GetRecent(filter *RequestLogFilter) (*RequestLogListResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if filter == nil {
		filter = &RequestLogFilter{}
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	// Build query with filters
	var conditions []string
	var args []interface{}

	if filter.Provider != "" {
		conditions = append(conditions, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.HTTPStatus != 0 {
		conditions = append(conditions, "http_status = ?")
		args = append(args, filter.HTTPStatus)
	}
	if filter.Endpoint != "" {
		conditions = append(conditions, "endpoint = ?")
		args = append(args, filter.Endpoint)
	}
	if filter.ClientID != "" {
		conditions = append(conditions, "client_id = ?")
		args = append(args, filter.ClientID)
	}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.From != nil {
		conditions = append(conditions, "initial_time >= ?")
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		conditions = append(conditions, "initial_time <= ?")
		args = append(args, *filter.To)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs %s", whereClause)
	var total int64
	if err := m.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count request logs: %w", err)
	}

	// Get records
	query := fmt.Sprintf(`
		SELECT id, status, initial_time, complete_time, duration_ms,
			   provider, provider_name, model, response_model, reasoning_effort, input_tokens, output_tokens,
			   cache_creation_input_tokens, cache_read_input_tokens, total_tokens,
			   price, input_cost, output_cost, cache_creation_cost, cache_read_cost,
			   http_status, stream, channel_id, channel_name,
			   endpoint, client_id, session_id, api_key_id, error, upstream_error, created_at
		FROM request_logs
		%s
		ORDER BY initial_time DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query request logs: %w", err)
	}
	defer rows.Close()

	var records []RequestLog
	for rows.Next() {
		var r RequestLog
		var channelID, apiKeyID sql.NullInt64
		var channelName, endpoint, clientID, sessionID, errorStr, upstreamErrorStr, status, providerName, responseModel, reasoningEffort sql.NullString
		var initialTime, completeTime, createdAt sql.NullString

		err := rows.Scan(
			&r.ID, &status, &initialTime, &completeTime, &r.DurationMs,
			&r.Type, &providerName, &r.Model, &responseModel, &reasoningEffort, &r.InputTokens, &r.OutputTokens,
			&r.CacheCreationInputTokens, &r.CacheReadInputTokens, &r.TotalTokens,
			&r.Price, &r.InputCost, &r.OutputCost, &r.CacheCreationCost, &r.CacheReadCost,
			&r.HTTPStatus, &r.Stream, &channelID, &channelName,
			&endpoint, &clientID, &sessionID, &apiKeyID, &errorStr, &upstreamErrorStr, &createdAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request log: %w", err)
		}

		if status.Valid {
			r.Status = status.String
		} else {
			r.Status = StatusCompleted
		}
		if providerName.Valid {
			r.ProviderName = providerName.String
		}
		if responseModel.Valid {
			r.ResponseModel = responseModel.String
		}
		if reasoningEffort.Valid {
			r.ReasoningEffort = reasoningEffort.String
		}
		if initialTime.Valid && initialTime.String != "" {
			r.InitialTime = parseTimeString(initialTime.String)
		}
		if completeTime.Valid && completeTime.String != "" {
			r.CompleteTime = parseTimeString(completeTime.String)
		}
		if createdAt.Valid && createdAt.String != "" {
			r.CreatedAt = parseTimeString(createdAt.String)
		}
		if channelID.Valid {
			r.ChannelID = int(channelID.Int64)
		}
		if channelName.Valid {
			r.ChannelName = channelName.String
		}
		if endpoint.Valid {
			r.Endpoint = endpoint.String
		}
		if clientID.Valid {
			r.ClientID = clientID.String
		}
		if sessionID.Valid {
			r.SessionID = sessionID.String
		}
		if apiKeyID.Valid {
			r.APIKeyID = &apiKeyID.Int64
		}
		if errorStr.Valid {
			r.Error = errorStr.String
		}
		if upstreamErrorStr.Valid {
			r.UpstreamError = upstreamErrorStr.String
		}

		records = append(records, r)
	}

	return &RequestLogListResponse{
		Requests: records,
		Total:    total,
		HasMore:  int64(filter.Offset+len(records)) < total,
	}, nil
}

// GetStats retrieves aggregated statistics
func (m *Manager) GetStats(filter *RequestLogFilter) (*RequestLogStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if filter == nil {
		filter = &RequestLogFilter{}
	}

	// Build where clause - exclude pending, timeout, and failover requests from statistics
	var conditions []string
	var args []interface{}

	// Only count completed/error requests in statistics (exclude pending/timeout/failover)
	conditions = append(conditions, "status NOT IN (?, ?, ?)")
	args = append(args, StatusPending, StatusTimeout, StatusFailover)

	if filter.From != nil {
		conditions = append(conditions, "initial_time >= ?")
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		conditions = append(conditions, "initial_time <= ?")
		args = append(args, *filter.To)
	}
	if filter.ClientID != "" {
		conditions = append(conditions, "client_id = ?")
		args = append(args, filter.ClientID)
	}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	stats := &RequestLogStats{
		ByProvider: make(map[string]ProviderStats),
		ByModel:    make(map[string]ModelStats),
		ByClient:   make(map[string]GroupStats),
		BySession:  make(map[string]GroupStats),
		ByAPIKey:   make(map[string]GroupStats),
	}

	// Get total stats
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(price), 0),
			MIN(initial_time),
			MAX(initial_time)
		FROM request_logs %s
	`, whereClause)

	var minTimeStr, maxTimeStr sql.NullString
	err := m.db.QueryRow(query, args...).Scan(
		&stats.TotalRequests,
		&stats.TotalTokens.InputTokens,
		&stats.TotalTokens.OutputTokens,
		&stats.TotalTokens.CacheCreationInputTokens,
		&stats.TotalTokens.CacheReadInputTokens,
		&stats.TotalTokens.TotalTokens,
		&stats.TotalCost,
		&minTimeStr,
		&maxTimeStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get total stats: %w", err)
	}

	// Parse time strings from SQLite
	if minTimeStr.Valid && minTimeStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, minTimeStr.String); err == nil {
			stats.TimeRange.From = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", minTimeStr.String); err == nil {
			stats.TimeRange.From = t
		}
	}
	if maxTimeStr.Valid && maxTimeStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, maxTimeStr.String); err == nil {
			stats.TimeRange.To = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", maxTimeStr.String); err == nil {
			stats.TimeRange.To = t
		}
	}

	// Get stats by provider (group by provider_name, the actual channel name)
	providerQuery := fmt.Sprintf(`
		SELECT COALESCE(provider_name, provider), COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(price), 0)
		FROM request_logs %s
		GROUP BY COALESCE(provider_name, provider)
	`, whereClause)

	providerRows, err := m.db.Query(providerQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider stats: %w", err)
	}
	defer providerRows.Close()

	for providerRows.Next() {
		var provider string
		var ps ProviderStats
		if err := providerRows.Scan(&provider, &ps.Count, &ps.InputTokens, &ps.OutputTokens, &ps.CacheCreationInputTokens, &ps.CacheReadInputTokens, &ps.Cost); err != nil {
			return nil, fmt.Errorf("failed to scan provider stats: %w", err)
		}
		stats.ByProvider[provider] = ps
	}

	// Get stats by model
	modelQuery := fmt.Sprintf(`
		SELECT model, COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(price), 0)
		FROM request_logs %s
		GROUP BY model
	`, whereClause)

	modelRows, err := m.db.Query(modelQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get model stats: %w", err)
	}
	defer modelRows.Close()

	for modelRows.Next() {
		var model string
		var ms ModelStats
		if err := modelRows.Scan(&model, &ms.Count, &ms.InputTokens, &ms.OutputTokens, &ms.CacheCreationInputTokens, &ms.CacheReadInputTokens, &ms.Cost); err != nil {
			return nil, fmt.Errorf("failed to scan model stats: %w", err)
		}
		stats.ByModel[model] = ms
	}

	// Get stats by user
	userQuery := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(TRIM(client_id), ''), '<unknown>'), COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(price), 0)
		FROM request_logs %s
		GROUP BY COALESCE(NULLIF(TRIM(client_id), ''), '<unknown>')
	`, whereClause)

	userRows, err := m.db.Query(userQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get user stats: %w", err)
	}
	defer userRows.Close()

	for userRows.Next() {
		var client string
		var us GroupStats
		if err := userRows.Scan(&client, &us.Count, &us.InputTokens, &us.OutputTokens, &us.CacheCreationInputTokens, &us.CacheReadInputTokens, &us.Cost); err != nil {
			return nil, fmt.Errorf("failed to scan client stats: %w", err)
		}
		stats.ByClient[client] = us
	}

	// Get stats by session
	sessionQuery := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(TRIM(session_id), ''), '<unknown>'), COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(price), 0)
		FROM request_logs %s
		GROUP BY COALESCE(NULLIF(TRIM(session_id), ''), '<unknown>')
	`, whereClause)

	sessionRows, err := m.db.Query(sessionQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get session stats: %w", err)
	}
	defer sessionRows.Close()

	for sessionRows.Next() {
		var session string
		var ss GroupStats
		if err := sessionRows.Scan(&session, &ss.Count, &ss.InputTokens, &ss.OutputTokens, &ss.CacheCreationInputTokens, &ss.CacheReadInputTokens, &ss.Cost); err != nil {
			return nil, fmt.Errorf("failed to scan session stats: %w", err)
		}
		stats.BySession[session] = ss
	}

	// Get stats by API key (0 = master, NULL = unknown)
	apiKeyQuery := fmt.Sprintf(`
		SELECT CASE WHEN api_key_id IS NULL THEN '<unknown>' WHEN api_key_id = 0 THEN 'master' ELSE CAST(api_key_id AS TEXT) END, COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens), 0),
			COALESCE(SUM(cache_read_input_tokens), 0),
			COALESCE(SUM(price), 0)
		FROM request_logs %s
		GROUP BY CASE WHEN api_key_id IS NULL THEN '<unknown>' WHEN api_key_id = 0 THEN 'master' ELSE CAST(api_key_id AS TEXT) END
	`, whereClause)

	apiKeyRows, err := m.db.Query(apiKeyQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get api key stats: %w", err)
	}
	defer apiKeyRows.Close()

	for apiKeyRows.Next() {
		var apiKey string
		var aks GroupStats
		if err := apiKeyRows.Scan(&apiKey, &aks.Count, &aks.InputTokens, &aks.OutputTokens, &aks.CacheCreationInputTokens, &aks.CacheReadInputTokens, &aks.Cost); err != nil {
			return nil, fmt.Errorf("failed to scan api key stats: %w", err)
		}
		stats.ByAPIKey[apiKey] = aks
	}

	return stats, nil
}

// ClearAll deletes all request logs
func (m *Manager) ClearAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec("DELETE FROM request_logs")
	if err != nil {
		return fmt.Errorf("failed to clear request logs: %w", err)
	}
	return nil
}

// GetByID retrieves a single request log by ID
func (m *Manager) GetByID(id string) (*RequestLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `
		SELECT id, initial_time, complete_time, duration_ms,
			   provider, model, input_tokens, output_tokens,
			   cache_creation_input_tokens, cache_read_input_tokens, total_tokens,
			   price, http_status, stream, channel_id, channel_name,
			   endpoint, client_id, session_id, error, created_at
		FROM request_logs
		WHERE id = ?
	`

	var r RequestLog
	var channelID sql.NullInt64
	var channelName, endpoint, clientID, sessionID, errorStr sql.NullString

	err := m.db.QueryRow(query, id).Scan(
		&r.ID, &r.InitialTime, &r.CompleteTime, &r.DurationMs,
		&r.Type, &r.Model, &r.InputTokens, &r.OutputTokens,
		&r.CacheCreationInputTokens, &r.CacheReadInputTokens, &r.TotalTokens,
		&r.Price, &r.HTTPStatus, &r.Stream, &channelID, &channelName,
		&endpoint, &clientID, &sessionID, &errorStr, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get request log: %w", err)
	}

	if channelID.Valid {
		r.ChannelID = int(channelID.Int64)
	}
	if channelName.Valid {
		r.ChannelName = channelName.String
	}
	if endpoint.Valid {
		r.Endpoint = endpoint.String
	}
	if clientID.Valid {
		r.ClientID = clientID.String
	}
	if sessionID.Valid {
		r.SessionID = sessionID.String
	}
	if errorStr.Valid {
		r.Error = errorStr.String
	}

	return &r, nil
}

// Cleanup removes old records (older than retention days)
func (m *Manager) Cleanup(retentionDays int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if retentionDays <= 0 {
		retentionDays = 30 // Default 30 days
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	result, err := m.db.Exec("DELETE FROM request_logs WHERE initial_time < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old records: %w", err)
	}

	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		log.Printf("🧹 Cleaned up %d old request log records", deleted)
	}

	return deleted, nil
}

// CleanupStalePending marks pending requests older than timeoutSeconds as timeout
func (m *Manager) CleanupStalePending(timeoutSeconds int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if timeoutSeconds <= 0 {
		timeoutSeconds = 300 // Default 300 seconds (5 minutes)
	}

	cutoff := time.Now().Add(-time.Duration(timeoutSeconds) * time.Second)
	now := time.Now()

	query := `
	UPDATE request_logs SET
		status = ?,
		complete_time = ?,
		error = ?
	WHERE status = ? AND initial_time < ?
	`

	result, err := m.db.Exec(query,
		StatusTimeout,
		now,
		"request timed out",
		StatusPending,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stale pending requests: %w", err)
	}

	updated, _ := result.RowsAffected()
	if updated > 0 {
		log.Printf("⏰ Marked %d stale pending requests as timeout (older than %d seconds)", updated, timeoutSeconds)
	}

	return updated, nil
}

// GetActiveSessions returns sessions with recent activity (within threshold duration)
func (m *Manager) GetActiveSessions(threshold time.Duration) ([]ActiveSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if threshold <= 0 {
		threshold = 30 * time.Minute // Default 30 minutes
	}

	cutoff := time.Now().Add(-threshold)

	// Query to get active sessions with aggregated stats
	// Uses a subquery to get the provider (type) from the most recent COMPLETED request in each session
	query := `
		SELECT
			session_id,
			(SELECT provider FROM request_logs r2
			 WHERE r2.session_id = r1.session_id AND r2.status = 'completed'
			 ORDER BY initial_time DESC LIMIT 1) as type,
			MIN(initial_time) as first_request_time,
			MAX(initial_time) as last_request_time,
			COUNT(*) as count,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_input_tokens), 0) as cache_creation_input_tokens,
			COALESCE(SUM(cache_read_input_tokens), 0) as cache_read_input_tokens,
			COALESCE(SUM(price), 0) as cost
		FROM request_logs r1
		WHERE session_id != ''
		  AND session_id IS NOT NULL
		  AND TRIM(session_id) != ''
		  AND status NOT IN (?, ?, ?)
		GROUP BY session_id
		HAVING MAX(initial_time) > ?
		ORDER BY last_request_time DESC
	`

	rows, err := m.db.Query(query, StatusPending, StatusTimeout, StatusFailover, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ActiveSession
	for rows.Next() {
		var s ActiveSession
		var sessionType sql.NullString
		var firstRequestTimeStr, lastRequestTimeStr string

		err := rows.Scan(
			&s.SessionID,
			&sessionType,
			&firstRequestTimeStr,
			&lastRequestTimeStr,
			&s.Count,
			&s.InputTokens,
			&s.OutputTokens,
			&s.CacheCreationInputTokens,
			&s.CacheReadInputTokens,
			&s.Cost,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan active session: %w", err)
		}

		s.FirstRequestTime = parseTimeString(firstRequestTimeStr)
		s.LastRequestTime = parseTimeString(lastRequestTimeStr)

		if sessionType.Valid {
			s.Type = sessionType.String
		}

		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active sessions: %w", err)
	}

	return sessions, nil
}

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// GetDB returns the underlying database connection
// Used by other managers that share the same database
func (m *Manager) GetDB() *sql.DB {
	return m.db
}

// ========== User Alias Methods ==========

// UserAlias represents a user ID to alias mapping
type UserAlias struct {
	UserID    string    `json:"userId"`
	Alias     string    `json:"alias"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GetAllAliases retrieves all user aliases
func (m *Manager) GetAllAliases() (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `SELECT user_id, alias FROM user_aliases`
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query user aliases: %w", err)
	}
	defer rows.Close()

	aliases := make(map[string]string)
	for rows.Next() {
		var userID, alias string
		if err := rows.Scan(&userID, &alias); err != nil {
			return nil, fmt.Errorf("failed to scan user alias: %w", err)
		}
		aliases[userID] = alias
	}

	return aliases, nil
}

// GetAlias retrieves a single alias by user ID
func (m *Manager) GetAlias(userID string) (*UserAlias, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `SELECT user_id, alias, created_at, updated_at FROM user_aliases WHERE user_id = ?`
	var ua UserAlias
	var createdAt, updatedAt string

	err := m.db.QueryRow(query, userID).Scan(&ua.UserID, &ua.Alias, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user alias: %w", err)
	}

	ua.CreatedAt = parseTimeString(createdAt)
	ua.UpdatedAt = parseTimeString(updatedAt)

	return &ua, nil
}

// SetAlias creates or updates a user alias
func (m *Manager) SetAlias(userID, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if userID == "" || alias == "" {
		return fmt.Errorf("user_id and alias are required")
	}

	// Check if alias is unique (excluding current user)
	var count int
	err := m.db.QueryRow(`SELECT COUNT(*) FROM user_aliases WHERE alias = ? AND user_id != ?`, alias, userID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check alias uniqueness: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("alias already in use")
	}

	// Upsert the alias
	query := `
	INSERT INTO user_aliases (user_id, alias, created_at, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(user_id) DO UPDATE SET
		alias = excluded.alias,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err = m.db.Exec(query, userID, alias)
	if err != nil {
		return fmt.Errorf("failed to set user alias: %w", err)
	}

	return nil
}

// DeleteAlias removes a user alias
func (m *Manager) DeleteAlias(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.Exec(`DELETE FROM user_aliases WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user alias: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("alias not found")
	}

	return nil
}

// ImportAliases imports multiple aliases (used for migration from localStorage)
func (m *Manager) ImportAliases(aliases map[string]string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO user_aliases (user_id, alias, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			alias = excluded.alias,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	imported := 0
	for userID, alias := range aliases {
		if userID == "" || alias == "" {
			continue
		}
		if _, err := stmt.Exec(userID, alias); err != nil {
			log.Printf("⚠️ Failed to import alias for %s: %v", userID, err)
			continue
		}
		imported++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return imported, nil
}

type statsHistoryBucket struct {
	dp            *StatsHistoryDataPoint
	durationsMs   []int64
	durationSumMs int64
}

func intervalForStatsHistoryWindow(windowLen time.Duration) time.Duration {
	switch {
	case windowLen <= time.Hour:
		return 1 * time.Minute
	case windowLen <= 6*time.Hour:
		return 5 * time.Minute
	case windowLen <= 24*time.Hour:
		return 15 * time.Minute
	case windowLen <= 7*24*time.Hour:
		return 1 * time.Hour
	default:
		return 4 * time.Hour
	}
}

// GetStatsHistory returns time-series statistics for charts
// duration: time range to query (e.g., "1h", "6h", "24h", or "today")
// endpoint: optional filter for endpoint ("/v1/messages" or "/v1/responses")
func (m *Manager) GetStatsHistory(duration string, endpoint string) (*StatsHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate time range and interval
	now := time.Now()
	var since time.Time
	var interval time.Duration
	var durationLabel string

	switch duration {
	case "1h":
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	case "6h":
		since = now.Add(-6 * time.Hour)
		interval = 5 * time.Minute
		durationLabel = "6h"
	case "24h":
		since = now.Add(-24 * time.Hour)
		interval = 15 * time.Minute
		durationLabel = "24h"
	case "today", "period":
		// Start of today in local timezone
		year, month, day := now.Date()
		since = time.Date(year, month, day, 0, 0, 0, 0, now.Location())
		// Calculate interval based on elapsed time today
		elapsed := now.Sub(since)
		if elapsed < time.Hour {
			interval = 1 * time.Minute
		} else if elapsed < 6*time.Hour {
			interval = 5 * time.Minute
		} else {
			interval = 15 * time.Minute
		}
		if duration == "today" {
			durationLabel = "today"
		} else {
			durationLabel = "period"
		}
	default:
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	}

	// Build query with optional endpoint filter
	query := `
		SELECT
			initial_time,
			status,
			COALESCE(duration_ms, 0) as duration_ms,
			COALESCE(input_tokens, 0) as input_tokens,
			COALESCE(output_tokens, 0) as output_tokens,
			COALESCE(cache_creation_input_tokens, 0) as cache_creation,
			COALESCE(cache_read_input_tokens, 0) as cache_read,
			COALESCE(price, 0) as price
		FROM request_logs
		WHERE initial_time >= ?
	`
	args := []interface{}{since}

	if endpoint != "" {
		query += ` AND endpoint = ?`
		args = append(args, endpoint)
	}

	query += ` ORDER BY initial_time ASC`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats history: %w", err)
	}
	defer rows.Close()

	// Create time buckets
	buckets := make(map[int64]*statsHistoryBucket)
	var summary StatsHistorySummary
	summary.Duration = durationLabel

	var latencyDurations []int64
	var latencySumMs int64

	for rows.Next() {
		var initialTimeStr string
		var status string
		var durationMs int64
		var inputTokens, outputTokens, cacheCreation, cacheRead int64
		var price float64

		if err := rows.Scan(&initialTimeStr, &status, &durationMs, &inputTokens, &outputTokens, &cacheCreation, &cacheRead, &price); err != nil {
			continue
		}

		initialTime := parseTimeString(initialTimeStr)
		if initialTime.IsZero() {
			continue
		}

		// Calculate bucket key (truncate to interval)
		bucketTime := initialTime.Truncate(interval)
		bucketKey := bucketTime.Unix()

		// Get or create bucket
		if _, exists := buckets[bucketKey]; !exists {
			buckets[bucketKey] = &statsHistoryBucket{
				dp: &StatsHistoryDataPoint{
					Timestamp: bucketTime,
				},
			}
		}

		bucket := buckets[bucketKey]
		bucket.dp.Requests++
		bucket.dp.InputTokens += inputTokens
		bucket.dp.OutputTokens += outputTokens
		bucket.dp.CacheCreationInputTokens += cacheCreation
		bucket.dp.CacheReadInputTokens += cacheRead
		bucket.dp.Cost += price

		// Count success/failure
		if status == StatusCompleted {
			bucket.dp.Success++
			summary.TotalSuccess++
		} else if status == StatusError || status == StatusTimeout {
			bucket.dp.Failure++
			summary.TotalFailure++
		}

		// Aggregate latency (exclude empty/unknown durations)
		if durationMs > 0 {
			bucket.durationsMs = append(bucket.durationsMs, durationMs)
			bucket.durationSumMs += durationMs
			latencyDurations = append(latencyDurations, durationMs)
			latencySumMs += durationMs
		}

		// Update summary
		summary.TotalRequests++
		summary.TotalInputTokens += inputTokens
		summary.TotalOutputTokens += outputTokens
		summary.TotalCacheCreationTokens += cacheCreation
		summary.TotalCacheReadTokens += cacheRead
		summary.TotalCost += price
	}

	// Calculate success rate
	if summary.TotalRequests > 0 {
		summary.AvgSuccessRate = float64(summary.TotalSuccess) / float64(summary.TotalRequests) * 100
	}

	// Calculate latency summary
	if len(latencyDurations) > 0 {
		summary.AvgDurationMs = float64(latencySumMs) / float64(len(latencyDurations))
		sort.Slice(latencyDurations, func(i, j int) bool { return latencyDurations[i] < latencyDurations[j] })
		summary.P50DurationMs = percentileMs(latencyDurations, 50)
		summary.P95DurationMs = percentileMs(latencyDurations, 95)
	}

	// Convert buckets map to sorted slice
	dataPoints := make([]StatsHistoryDataPoint, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.durationsMs) > 0 {
			bucket.dp.AvgDurationMs = float64(bucket.durationSumMs) / float64(len(bucket.durationsMs))
			sort.Slice(bucket.durationsMs, func(i, j int) bool { return bucket.durationsMs[i] < bucket.durationsMs[j] })
			bucket.dp.P50DurationMs = percentileMs(bucket.durationsMs, 50)
			bucket.dp.P95DurationMs = percentileMs(bucket.durationsMs, 95)
		}
		dataPoints = append(dataPoints, *bucket.dp)
	}

	// Sort by timestamp
	sort.Slice(dataPoints, func(i, j int) bool { return dataPoints[i].Timestamp.Before(dataPoints[j].Timestamp) })

	return &StatsHistoryResponse{
		DataPoints: dataPoints,
		Summary:    summary,
	}, nil
}

// GetStatsHistoryRange returns time-series statistics for charts within a custom date range.
// duration: window size to display (e.g., "1h", "6h", "24h", or "period" for the whole range)
// from/to: RFC3339 timestamps defining the selected period (typically from the logs page)
// endpoint: optional filter for endpoint ("/v1/messages" or "/v1/responses")
func (m *Manager) GetStatsHistoryRange(duration string, from, to time.Time, endpoint string) (*StatsHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	rangeStart := from
	rangeEnd := to
	if rangeEnd.After(now) {
		rangeEnd = now
	}
	if rangeEnd.Before(rangeStart) {
		return &StatsHistoryResponse{
			DataPoints: []StatsHistoryDataPoint{},
			Summary:    StatsHistorySummary{Duration: duration},
		}, nil
	}

	windowEnd := rangeEnd
	windowStart := rangeStart
	durationLabel := duration

	switch duration {
	case "period", "today":
		windowStart = rangeStart
		if duration == "today" {
			durationLabel = "today"
		} else {
			durationLabel = "period"
		}
	case "1h":
		windowStart = windowEnd.Add(-1 * time.Hour)
		durationLabel = "1h"
	case "6h":
		windowStart = windowEnd.Add(-6 * time.Hour)
		durationLabel = "6h"
	case "24h":
		windowStart = windowEnd.Add(-24 * time.Hour)
		durationLabel = "24h"
	default:
		windowStart = windowEnd.Add(-1 * time.Hour)
		durationLabel = "1h"
	}

	if windowStart.Before(rangeStart) {
		windowStart = rangeStart
	}

	interval := intervalForStatsHistoryWindow(windowEnd.Sub(windowStart))

	query := `
		SELECT
			initial_time,
			status,
			COALESCE(duration_ms, 0) as duration_ms,
			COALESCE(input_tokens, 0) as input_tokens,
			COALESCE(output_tokens, 0) as output_tokens,
			COALESCE(cache_creation_input_tokens, 0) as cache_creation,
			COALESCE(cache_read_input_tokens, 0) as cache_read,
			COALESCE(price, 0) as price
		FROM request_logs
		WHERE initial_time >= ? AND initial_time <= ?
	`
	args := []interface{}{windowStart, windowEnd}
	if endpoint != "" {
		query += ` AND endpoint = ?`
		args = append(args, endpoint)
	}
	query += ` ORDER BY initial_time ASC`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats history (range): %w", err)
	}
	defer rows.Close()

	buckets := make(map[int64]*statsHistoryBucket)
	var summary StatsHistorySummary
	summary.Duration = durationLabel

	var latencyDurations []int64
	var latencySumMs int64

	for rows.Next() {
		var initialTimeStr string
		var status string
		var durationMs int64
		var inputTokens, outputTokens, cacheCreation, cacheRead int64
		var price float64

		if err := rows.Scan(&initialTimeStr, &status, &durationMs, &inputTokens, &outputTokens, &cacheCreation, &cacheRead, &price); err != nil {
			continue
		}

		initialTime := parseTimeString(initialTimeStr)
		if initialTime.IsZero() {
			continue
		}

		bucketTime := initialTime.Truncate(interval)
		bucketKey := bucketTime.Unix()

		if _, exists := buckets[bucketKey]; !exists {
			buckets[bucketKey] = &statsHistoryBucket{
				dp: &StatsHistoryDataPoint{
					Timestamp: bucketTime,
				},
			}
		}

		bucket := buckets[bucketKey]
		bucket.dp.Requests++
		bucket.dp.InputTokens += inputTokens
		bucket.dp.OutputTokens += outputTokens
		bucket.dp.CacheCreationInputTokens += cacheCreation
		bucket.dp.CacheReadInputTokens += cacheRead
		bucket.dp.Cost += price

		if status == StatusCompleted {
			bucket.dp.Success++
			summary.TotalSuccess++
		} else if status == StatusError || status == StatusTimeout {
			bucket.dp.Failure++
			summary.TotalFailure++
		}

		if durationMs > 0 {
			bucket.durationsMs = append(bucket.durationsMs, durationMs)
			bucket.durationSumMs += durationMs
			latencyDurations = append(latencyDurations, durationMs)
			latencySumMs += durationMs
		}

		summary.TotalRequests++
		summary.TotalInputTokens += inputTokens
		summary.TotalOutputTokens += outputTokens
		summary.TotalCacheCreationTokens += cacheCreation
		summary.TotalCacheReadTokens += cacheRead
		summary.TotalCost += price
	}

	if summary.TotalRequests > 0 {
		summary.AvgSuccessRate = float64(summary.TotalSuccess) / float64(summary.TotalRequests) * 100
	}

	if len(latencyDurations) > 0 {
		summary.AvgDurationMs = float64(latencySumMs) / float64(len(latencyDurations))
		sort.Slice(latencyDurations, func(i, j int) bool { return latencyDurations[i] < latencyDurations[j] })
		summary.P50DurationMs = percentileMs(latencyDurations, 50)
		summary.P95DurationMs = percentileMs(latencyDurations, 95)
	}

	dataPoints := make([]StatsHistoryDataPoint, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.durationsMs) > 0 {
			bucket.dp.AvgDurationMs = float64(bucket.durationSumMs) / float64(len(bucket.durationsMs))
			sort.Slice(bucket.durationsMs, func(i, j int) bool { return bucket.durationsMs[i] < bucket.durationsMs[j] })
			bucket.dp.P50DurationMs = percentileMs(bucket.durationsMs, 50)
			bucket.dp.P95DurationMs = percentileMs(bucket.durationsMs, 95)
		}
		dataPoints = append(dataPoints, *bucket.dp)
	}
	sort.Slice(dataPoints, func(i, j int) bool { return dataPoints[i].Timestamp.Before(dataPoints[j].Timestamp) })

	return &StatsHistoryResponse{
		DataPoints: dataPoints,
		Summary:    summary,
	}, nil
}

// GetProviderStatsHistory returns time-series statistics grouped by provider/channel name.
// duration: time range to query (e.g., "1h", "6h", "24h", or "today")
// endpoint: optional filter for endpoint ("/v1/messages" or "/v1/responses")
func (m *Manager) GetProviderStatsHistory(duration string, endpoint string) (*ProviderStatsHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate time range and interval (same logic as GetStatsHistory)
	now := time.Now()
	year, month, day := now.Date()
	periodStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	var since time.Time
	var interval time.Duration
	var durationLabel string

	switch duration {
	case "1h":
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	case "6h":
		since = now.Add(-6 * time.Hour)
		interval = 5 * time.Minute
		durationLabel = "6h"
	case "24h":
		since = now.Add(-24 * time.Hour)
		interval = 15 * time.Minute
		durationLabel = "24h"
	case "today", "period":
		since = periodStart
		elapsed := now.Sub(since)
		if elapsed < time.Hour {
			interval = 1 * time.Minute
		} else if elapsed < 6*time.Hour {
			interval = 5 * time.Minute
		} else {
			interval = 15 * time.Minute
		}
		if duration == "today" {
			durationLabel = "today"
		} else {
			durationLabel = "period"
		}
	default:
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	}
	if since.Before(periodStart) {
		since = periodStart
	}

	// Precompute all bucket timestamps to keep series aligned (fill missing with 0).
	bucketStart := since.Truncate(interval)
	bucketEnd := now.Truncate(interval)
	bucketTimes := make([]time.Time, 0, int(bucketEnd.Sub(bucketStart)/interval)+1)
	for t := bucketStart; !t.After(bucketEnd); t = t.Add(interval) {
		bucketTimes = append(bucketTimes, t)
	}

	// Baseline cost per provider before the selected time range (within today's period)
	baselineCost := make(map[string]float64)
	baselineQuery := `
		SELECT
			COALESCE(provider_name, provider) as provider,
			COALESCE(SUM(price), 0) as cost
		FROM request_logs
		WHERE initial_time >= ? AND initial_time < ? AND status NOT IN (?, ?, ?)
	`
	baselineArgs := []interface{}{periodStart, since, StatusPending, StatusTimeout, StatusFailover}
	if endpoint != "" {
		baselineQuery += ` AND endpoint = ?`
		baselineArgs = append(baselineArgs, endpoint)
	}
	baselineQuery += ` GROUP BY COALESCE(provider_name, provider)`

	baselineRows, err := m.db.Query(baselineQuery, baselineArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query provider baseline cost: %w", err)
	}
	for baselineRows.Next() {
		var provider string
		var cost float64
		if err := baselineRows.Scan(&provider, &cost); err != nil {
			continue
		}
		baselineCost[provider] = cost
	}
	baselineRows.Close()

		// Build query with optional endpoint filter
		query := `
			SELECT
				initial_time,
				status,
				COALESCE(provider_name, provider) as provider,
				COALESCE(duration_ms, 0) as duration_ms,
				COALESCE(input_tokens, 0) as input_tokens,
				COALESCE(output_tokens, 0) as output_tokens,
				COALESCE(cache_creation_input_tokens, 0) as cache_creation,
				COALESCE(cache_read_input_tokens, 0) as cache_read,
				COALESCE(price, 0) as price
			FROM request_logs
			WHERE initial_time >= ? AND initial_time <= ?
		`
		args := []interface{}{since, now}

	if endpoint != "" {
		query += ` AND endpoint = ?`
		args = append(args, endpoint)
	}

	query += ` ORDER BY initial_time ASC`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query provider stats history: %w", err)
	}
	defer rows.Close()

	providerBuckets := make(map[string]map[int64]*statsHistoryBucket)
	providerSummary := make(map[string]*StatsHistorySummary)
	providerLatency := make(map[string][]int64)
	providerLatencySum := make(map[string]int64)

	var summary StatsHistorySummary
	summary.Duration = durationLabel
	var latencyDurations []int64
	var latencySumMs int64

	for rows.Next() {
		var initialTimeStr string
		var status string
		var provider string
		var durationMs int64
		var inputTokens, outputTokens, cacheCreation, cacheRead int64
		var price float64

		if err := rows.Scan(&initialTimeStr, &status, &provider, &durationMs, &inputTokens, &outputTokens, &cacheCreation, &cacheRead, &price); err != nil {
			continue
		}

		initialTime := parseTimeString(initialTimeStr)
		if initialTime.IsZero() {
			continue
		}

		bucketTime := initialTime.Truncate(interval)
		bucketKey := bucketTime.Unix()

		if _, ok := providerBuckets[provider]; !ok {
			providerBuckets[provider] = make(map[int64]*statsHistoryBucket)
		}
		if _, ok := providerSummary[provider]; !ok {
			providerSummary[provider] = &StatsHistorySummary{Duration: durationLabel}
		}

			// Create bucket if needed
			if _, ok := providerBuckets[provider][bucketKey]; !ok {
				providerBuckets[provider][bucketKey] = &statsHistoryBucket{
					dp: &StatsHistoryDataPoint{
						Timestamp: bucketTime,
					},
				}
			}

			bucket := providerBuckets[provider][bucketKey]
			isCostEligible := status != StatusPending && status != StatusTimeout && status != StatusFailover
			if isCostEligible {
				bucket.dp.Requests++
				bucket.dp.InputTokens += inputTokens
				bucket.dp.OutputTokens += outputTokens
				bucket.dp.CacheCreationInputTokens += cacheCreation
				bucket.dp.CacheReadInputTokens += cacheRead
				bucket.dp.Cost += price

				if status == StatusCompleted {
					bucket.dp.Success++
					providerSummary[provider].TotalSuccess++
					summary.TotalSuccess++
				} else if status == StatusError {
					bucket.dp.Failure++
					providerSummary[provider].TotalFailure++
					summary.TotalFailure++
				}

				if durationMs > 0 {
					bucket.durationsMs = append(bucket.durationsMs, durationMs)
					bucket.durationSumMs += durationMs

					providerLatency[provider] = append(providerLatency[provider], durationMs)
					providerLatencySum[provider] += durationMs

					latencyDurations = append(latencyDurations, durationMs)
					latencySumMs += durationMs
				}

				providerSummary[provider].TotalRequests++
				providerSummary[provider].TotalInputTokens += inputTokens
				providerSummary[provider].TotalOutputTokens += outputTokens
				providerSummary[provider].TotalCacheCreationTokens += cacheCreation
				providerSummary[provider].TotalCacheReadTokens += cacheRead
				providerSummary[provider].TotalCost += price

				summary.TotalRequests++
				summary.TotalInputTokens += inputTokens
				summary.TotalOutputTokens += outputTokens
				summary.TotalCacheCreationTokens += cacheCreation
				summary.TotalCacheReadTokens += cacheRead
				summary.TotalCost += price
			}
		}

	// Finalize summaries
	if summary.TotalRequests > 0 {
		summary.AvgSuccessRate = float64(summary.TotalSuccess) / float64(summary.TotalRequests) * 100
	}
	if len(latencyDurations) > 0 {
		summary.AvgDurationMs = float64(latencySumMs) / float64(len(latencyDurations))
		sort.Slice(latencyDurations, func(i, j int) bool { return latencyDurations[i] < latencyDurations[j] })
		summary.P50DurationMs = percentileMs(latencyDurations, 50)
		summary.P95DurationMs = percentileMs(latencyDurations, 95)
	}

	providerSet := make(map[string]struct{}, len(providerBuckets)+len(baselineCost))
	for p := range providerBuckets {
		providerSet[p] = struct{}{}
	}
	for p, cost := range baselineCost {
		if cost > 0 {
			providerSet[p] = struct{}{}
		}
	}

	providers := make([]string, 0, len(providerSet))
	for p := range providerSet {
		providers = append(providers, p)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })

	series := make([]ProviderStatsHistorySeries, 0, len(providers))
	for _, provider := range providers {
		if _, ok := providerBuckets[provider]; !ok {
			providerBuckets[provider] = make(map[int64]*statsHistoryBucket)
		}
		if _, ok := providerSummary[provider]; !ok {
			providerSummary[provider] = &StatsHistorySummary{Duration: durationLabel}
		}

		// Finalize bucket latency stats
		for _, bucket := range providerBuckets[provider] {
			if len(bucket.durationsMs) > 0 {
				bucket.dp.AvgDurationMs = float64(bucket.durationSumMs) / float64(len(bucket.durationsMs))
				sort.Slice(bucket.durationsMs, func(i, j int) bool { return bucket.durationsMs[i] < bucket.durationsMs[j] })
				bucket.dp.P50DurationMs = percentileMs(bucket.durationsMs, 50)
				bucket.dp.P95DurationMs = percentileMs(bucket.durationsMs, 95)
			}
		}

		// Finalize provider latency summary
		if ps := providerSummary[provider]; ps != nil {
			if ps.TotalRequests > 0 {
				ps.AvgSuccessRate = float64(ps.TotalSuccess) / float64(ps.TotalRequests) * 100
			}
			if durations := providerLatency[provider]; len(durations) > 0 {
				ps.AvgDurationMs = float64(providerLatencySum[provider]) / float64(len(durations))
				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				ps.P50DurationMs = percentileMs(durations, 50)
				ps.P95DurationMs = percentileMs(durations, 95)
			}
		}

		dataPoints := make([]StatsHistoryDataPoint, 0, len(bucketTimes))
		for _, bt := range bucketTimes {
			key := bt.Unix()
			if bucket, ok := providerBuckets[provider][key]; ok && bucket.dp != nil {
				dataPoints = append(dataPoints, *bucket.dp)
			} else {
				dataPoints = append(dataPoints, StatsHistoryDataPoint{Timestamp: bt})
			}
		}

		series = append(series, ProviderStatsHistorySeries{
			Provider:     provider,
			BaselineCost: baselineCost[provider],
			DataPoints:   dataPoints,
			Summary:      *providerSummary[provider],
		})
	}

	return &ProviderStatsHistoryResponse{
		Providers: series,
		Summary:   summary,
	}, nil
}

// GetProviderStatsHistoryRange returns provider/channel grouped time-series statistics within a custom date range.
// duration: window size to display (e.g., "1h", "6h", "24h", or "period" for the whole range)
// from/to: RFC3339 timestamps defining the selected period (typically from the logs page)
// endpoint: optional filter for endpoint ("/v1/messages" or "/v1/responses")
func (m *Manager) GetProviderStatsHistoryRange(duration string, from, to time.Time, endpoint string) (*ProviderStatsHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	rangeStart := from
	rangeEnd := to
	if rangeEnd.After(now) {
		rangeEnd = now
	}
	if rangeEnd.Before(rangeStart) {
		return &ProviderStatsHistoryResponse{
			Providers: []ProviderStatsHistorySeries{},
			Summary:   StatsHistorySummary{Duration: duration},
		}, nil
	}

	windowEnd := rangeEnd
	windowStart := rangeStart
	durationLabel := duration

	switch duration {
	case "period", "today":
		windowStart = rangeStart
		if duration == "today" {
			durationLabel = "today"
		} else {
			durationLabel = "period"
		}
	case "1h":
		windowStart = windowEnd.Add(-1 * time.Hour)
		durationLabel = "1h"
	case "6h":
		windowStart = windowEnd.Add(-6 * time.Hour)
		durationLabel = "6h"
	case "24h":
		windowStart = windowEnd.Add(-24 * time.Hour)
		durationLabel = "24h"
	default:
		windowStart = windowEnd.Add(-1 * time.Hour)
		durationLabel = "1h"
	}

	if windowStart.Before(rangeStart) {
		windowStart = rangeStart
	}

	interval := intervalForStatsHistoryWindow(windowEnd.Sub(windowStart))

	bucketStart := windowStart.Truncate(interval)
	bucketEnd := windowEnd.Truncate(interval)
	bucketTimes := make([]time.Time, 0, int(bucketEnd.Sub(bucketStart)/interval)+1)
	for t := bucketStart; !t.After(bucketEnd); t = t.Add(interval) {
		bucketTimes = append(bucketTimes, t)
	}

	// Baseline cost per provider before the selected time range (within the selected period)
	baselineCost := make(map[string]float64)
	if windowStart.After(rangeStart) {
		baselineQuery := `
			SELECT
				COALESCE(provider_name, provider) as provider,
				COALESCE(SUM(price), 0) as cost
			FROM request_logs
			WHERE initial_time >= ? AND initial_time < ? AND status NOT IN (?, ?, ?)
		`
		baselineArgs := []interface{}{rangeStart, windowStart, StatusPending, StatusTimeout, StatusFailover}
		if endpoint != "" {
			baselineQuery += ` AND endpoint = ?`
			baselineArgs = append(baselineArgs, endpoint)
		}
		baselineQuery += ` GROUP BY COALESCE(provider_name, provider)`

		baselineRows, err := m.db.Query(baselineQuery, baselineArgs...)
		if err != nil {
			return nil, fmt.Errorf("failed to query provider baseline cost (range): %w", err)
		}
		for baselineRows.Next() {
			var provider string
			var cost float64
			if err := baselineRows.Scan(&provider, &cost); err != nil {
				continue
			}
			baselineCost[provider] = cost
		}
		baselineRows.Close()
	}

		query := `
			SELECT
				initial_time,
				status,
				COALESCE(provider_name, provider) as provider,
				COALESCE(duration_ms, 0) as duration_ms,
				COALESCE(input_tokens, 0) as input_tokens,
				COALESCE(output_tokens, 0) as output_tokens,
				COALESCE(cache_creation_input_tokens, 0) as cache_creation,
				COALESCE(cache_read_input_tokens, 0) as cache_read,
				COALESCE(price, 0) as price
			FROM request_logs
			WHERE initial_time >= ? AND initial_time <= ?
		`
		args := []interface{}{windowStart, windowEnd}
	if endpoint != "" {
		query += ` AND endpoint = ?`
		args = append(args, endpoint)
	}
	query += ` ORDER BY initial_time ASC`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query provider stats history (range): %w", err)
	}
	defer rows.Close()

	providerBuckets := make(map[string]map[int64]*statsHistoryBucket)
	providerSummary := make(map[string]*StatsHistorySummary)
	providerLatency := make(map[string][]int64)
	providerLatencySum := make(map[string]int64)

	var summary StatsHistorySummary
	summary.Duration = durationLabel
	var latencyDurations []int64
	var latencySumMs int64

	for rows.Next() {
		var initialTimeStr string
		var status string
		var provider string
		var durationMs int64
		var inputTokens, outputTokens, cacheCreation, cacheRead int64
		var price float64

		if err := rows.Scan(&initialTimeStr, &status, &provider, &durationMs, &inputTokens, &outputTokens, &cacheCreation, &cacheRead, &price); err != nil {
			continue
		}

		initialTime := parseTimeString(initialTimeStr)
		if initialTime.IsZero() {
			continue
		}

		bucketTime := initialTime.Truncate(interval)
		bucketKey := bucketTime.Unix()

		if _, ok := providerBuckets[provider]; !ok {
			providerBuckets[provider] = make(map[int64]*statsHistoryBucket)
		}
		if _, ok := providerSummary[provider]; !ok {
			providerSummary[provider] = &StatsHistorySummary{Duration: durationLabel}
		}

		if _, ok := providerBuckets[provider][bucketKey]; !ok {
			providerBuckets[provider][bucketKey] = &statsHistoryBucket{
				dp: &StatsHistoryDataPoint{
					Timestamp: bucketTime,
				},
			}
		}

			bucket := providerBuckets[provider][bucketKey]
			isCostEligible := status != StatusPending && status != StatusTimeout && status != StatusFailover
			if isCostEligible {
				bucket.dp.Requests++
				bucket.dp.InputTokens += inputTokens
				bucket.dp.OutputTokens += outputTokens
				bucket.dp.CacheCreationInputTokens += cacheCreation
				bucket.dp.CacheReadInputTokens += cacheRead
				bucket.dp.Cost += price

				if status == StatusCompleted {
					bucket.dp.Success++
					providerSummary[provider].TotalSuccess++
					summary.TotalSuccess++
				} else if status == StatusError {
					bucket.dp.Failure++
					providerSummary[provider].TotalFailure++
					summary.TotalFailure++
				}

				if durationMs > 0 {
					bucket.durationsMs = append(bucket.durationsMs, durationMs)
					bucket.durationSumMs += durationMs

					providerLatency[provider] = append(providerLatency[provider], durationMs)
					providerLatencySum[provider] += durationMs

					latencyDurations = append(latencyDurations, durationMs)
					latencySumMs += durationMs
				}

				providerSummary[provider].TotalRequests++
				providerSummary[provider].TotalInputTokens += inputTokens
				providerSummary[provider].TotalOutputTokens += outputTokens
				providerSummary[provider].TotalCacheCreationTokens += cacheCreation
				providerSummary[provider].TotalCacheReadTokens += cacheRead
				providerSummary[provider].TotalCost += price

				summary.TotalRequests++
				summary.TotalInputTokens += inputTokens
				summary.TotalOutputTokens += outputTokens
				summary.TotalCacheCreationTokens += cacheCreation
				summary.TotalCacheReadTokens += cacheRead
				summary.TotalCost += price
			}
		}

	if summary.TotalRequests > 0 {
		summary.AvgSuccessRate = float64(summary.TotalSuccess) / float64(summary.TotalRequests) * 100
	}
	if len(latencyDurations) > 0 {
		summary.AvgDurationMs = float64(latencySumMs) / float64(len(latencyDurations))
		sort.Slice(latencyDurations, func(i, j int) bool { return latencyDurations[i] < latencyDurations[j] })
		summary.P50DurationMs = percentileMs(latencyDurations, 50)
		summary.P95DurationMs = percentileMs(latencyDurations, 95)
	}

	providerSet := make(map[string]struct{}, len(providerBuckets)+len(baselineCost))
	for p := range providerBuckets {
		providerSet[p] = struct{}{}
	}
	for p, cost := range baselineCost {
		if cost > 0 {
			providerSet[p] = struct{}{}
		}
	}

	providers := make([]string, 0, len(providerSet))
	for p := range providerSet {
		providers = append(providers, p)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })

	series := make([]ProviderStatsHistorySeries, 0, len(providers))
	for _, provider := range providers {
		if _, ok := providerBuckets[provider]; !ok {
			providerBuckets[provider] = make(map[int64]*statsHistoryBucket)
		}
		if _, ok := providerSummary[provider]; !ok {
			providerSummary[provider] = &StatsHistorySummary{Duration: durationLabel}
		}

		for _, bucket := range providerBuckets[provider] {
			if len(bucket.durationsMs) > 0 {
				bucket.dp.AvgDurationMs = float64(bucket.durationSumMs) / float64(len(bucket.durationsMs))
				sort.Slice(bucket.durationsMs, func(i, j int) bool { return bucket.durationsMs[i] < bucket.durationsMs[j] })
				bucket.dp.P50DurationMs = percentileMs(bucket.durationsMs, 50)
				bucket.dp.P95DurationMs = percentileMs(bucket.durationsMs, 95)
			}
		}

		if ps := providerSummary[provider]; ps != nil {
			if ps.TotalRequests > 0 {
				ps.AvgSuccessRate = float64(ps.TotalSuccess) / float64(ps.TotalRequests) * 100
			}
			if durations := providerLatency[provider]; len(durations) > 0 {
				ps.AvgDurationMs = float64(providerLatencySum[provider]) / float64(len(durations))
				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				ps.P50DurationMs = percentileMs(durations, 50)
				ps.P95DurationMs = percentileMs(durations, 95)
			}
		}

		dataPoints := make([]StatsHistoryDataPoint, 0, len(bucketTimes))
		for _, bt := range bucketTimes {
			key := bt.Unix()
			if bucket, ok := providerBuckets[provider][key]; ok && bucket.dp != nil {
				dataPoints = append(dataPoints, *bucket.dp)
			} else {
				dataPoints = append(dataPoints, StatsHistoryDataPoint{Timestamp: bt})
			}
		}

		series = append(series, ProviderStatsHistorySeries{
			Provider:     provider,
			BaselineCost: baselineCost[provider],
			DataPoints:   dataPoints,
			Summary:      *providerSummary[provider],
		})
	}

	return &ProviderStatsHistoryResponse{
		Providers: series,
		Summary:   summary,
	}, nil
}

// GetChannelStatsHistory returns time-series statistics for a specific channel
func (m *Manager) GetChannelStatsHistory(channelID int, duration string, endpoint string) (*ChannelStatsHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate time range and interval (same logic as GetStatsHistory)
	now := time.Now()
	var since time.Time
	var interval time.Duration
	var durationLabel string

	switch duration {
	case "1h":
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	case "6h":
		since = now.Add(-6 * time.Hour)
		interval = 5 * time.Minute
		durationLabel = "6h"
	case "24h":
		since = now.Add(-24 * time.Hour)
		interval = 15 * time.Minute
		durationLabel = "24h"
	case "today":
		year, month, day := now.Date()
		since = time.Date(year, month, day, 0, 0, 0, 0, now.Location())
		elapsed := now.Sub(since)
		if elapsed < time.Hour {
			interval = 1 * time.Minute
		} else if elapsed < 6*time.Hour {
			interval = 5 * time.Minute
		} else {
			interval = 15 * time.Minute
		}
		durationLabel = "today"
	default:
		since = now.Add(-1 * time.Hour)
		interval = 1 * time.Minute
		durationLabel = "1h"
	}

	// Build query with channel filter
	query := `
		SELECT
			initial_time,
			status,
			COALESCE(duration_ms, 0) as duration_ms,
			channel_name,
			COALESCE(input_tokens, 0) as input_tokens,
			COALESCE(output_tokens, 0) as output_tokens,
			COALESCE(cache_creation_input_tokens, 0) as cache_creation,
			COALESCE(cache_read_input_tokens, 0) as cache_read,
			COALESCE(price, 0) as price
		FROM request_logs
		WHERE initial_time >= ? AND channel_id = ?
	`
	args := []interface{}{since, channelID}

	if endpoint != "" {
		query += ` AND endpoint = ?`
		args = append(args, endpoint)
	}

	query += ` ORDER BY initial_time ASC`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel stats history: %w", err)
	}
	defer rows.Close()

	// Create time buckets
	buckets := make(map[int64]*statsHistoryBucket)
	var summary StatsHistorySummary
	var channelName string
	summary.Duration = durationLabel

	var latencyDurations []int64
	var latencySumMs int64

	for rows.Next() {
		var initialTimeStr string
		var status string
		var durationMs int64
		var chName string
		var inputTokens, outputTokens, cacheCreation, cacheRead int64
		var price float64

		if err := rows.Scan(&initialTimeStr, &status, &durationMs, &chName, &inputTokens, &outputTokens, &cacheCreation, &cacheRead, &price); err != nil {
			continue
		}

		if channelName == "" {
			channelName = chName
		}

		initialTime := parseTimeString(initialTimeStr)
		if initialTime.IsZero() {
			continue
		}

		// Calculate bucket key
		bucketTime := initialTime.Truncate(interval)
		bucketKey := bucketTime.Unix()

		if _, exists := buckets[bucketKey]; !exists {
			buckets[bucketKey] = &statsHistoryBucket{
				dp: &StatsHistoryDataPoint{
					Timestamp: bucketTime,
				},
			}
		}

		bucket := buckets[bucketKey]
		bucket.dp.Requests++
		bucket.dp.InputTokens += inputTokens
		bucket.dp.OutputTokens += outputTokens
		bucket.dp.CacheCreationInputTokens += cacheCreation
		bucket.dp.CacheReadInputTokens += cacheRead
		bucket.dp.Cost += price

		if status == StatusCompleted {
			bucket.dp.Success++
			summary.TotalSuccess++
		} else if status == StatusError || status == StatusTimeout {
			bucket.dp.Failure++
			summary.TotalFailure++
		}

		if durationMs > 0 {
			bucket.durationsMs = append(bucket.durationsMs, durationMs)
			bucket.durationSumMs += durationMs
			latencyDurations = append(latencyDurations, durationMs)
			latencySumMs += durationMs
		}

		summary.TotalRequests++
		summary.TotalInputTokens += inputTokens
		summary.TotalOutputTokens += outputTokens
		summary.TotalCacheCreationTokens += cacheCreation
		summary.TotalCacheReadTokens += cacheRead
		summary.TotalCost += price
	}

	if summary.TotalRequests > 0 {
		summary.AvgSuccessRate = float64(summary.TotalSuccess) / float64(summary.TotalRequests) * 100
	}

	if len(latencyDurations) > 0 {
		summary.AvgDurationMs = float64(latencySumMs) / float64(len(latencyDurations))
		sort.Slice(latencyDurations, func(i, j int) bool { return latencyDurations[i] < latencyDurations[j] })
		summary.P50DurationMs = percentileMs(latencyDurations, 50)
		summary.P95DurationMs = percentileMs(latencyDurations, 95)
	}

	// Convert to sorted slice
	dataPoints := make([]StatsHistoryDataPoint, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.durationsMs) > 0 {
			bucket.dp.AvgDurationMs = float64(bucket.durationSumMs) / float64(len(bucket.durationsMs))
			sort.Slice(bucket.durationsMs, func(i, j int) bool { return bucket.durationsMs[i] < bucket.durationsMs[j] })
			bucket.dp.P50DurationMs = percentileMs(bucket.durationsMs, 50)
			bucket.dp.P95DurationMs = percentileMs(bucket.durationsMs, 95)
		}
		dataPoints = append(dataPoints, *bucket.dp)
	}

	sort.Slice(dataPoints, func(i, j int) bool { return dataPoints[i].Timestamp.Before(dataPoints[j].Timestamp) })

	return &ChannelStatsHistoryResponse{
		ChannelID:   channelID,
		ChannelName: channelName,
		DataPoints:  dataPoints,
		Summary:     summary,
	}, nil
}

// generateID creates a unique request ID
func generateID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// parseTimeString parses a time string from SQLite in various formats
func parseTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	// Handle Go's internal time format with monotonic clock (e.g., "2025-12-15 22:41:43.377695946 +0800 CST m=+560.016976256")
	// Strip the monotonic clock part if present
	if idx := strings.Index(s, " m="); idx != -1 {
		s = s[:idx]
	}

	// Try standard library parsing first - it handles most RFC3339 variants
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}

	// Try Go's default time format (used when time.Time is stored as string)
	// Format: "2006-01-02 15:04:05.999999999 -0700 MST"
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 +0800 CST", s); err == nil {
		return t
	}

	// Try other common formats
	formats := []string{
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func percentileMs(sorted []int64, p float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[n-1]
	}

	// Linear interpolation between closest ranks for smoother percentiles on small samples.
	pos := (p / 100) * float64(n-1)
	lower := int(math.Floor(pos))
	upper := int(math.Ceil(pos))
	if lower < 0 {
		lower = 0
	}
	if upper >= n {
		upper = n - 1
	}
	if lower == upper {
		return sorted[lower]
	}
	weight := pos - float64(lower)
	value := float64(sorted[lower]) + (float64(sorted[upper])-float64(sorted[lower]))*weight
	return int64(math.Round(value))
}

// ChannelQuota represents persisted quota data for a channel
type ChannelQuota struct {
	ChannelID              int
	ChannelName            string
	PlanType               string
	PrimaryUsedPercent     int
	PrimaryWindowMinutes   int
	PrimaryResetAt         *time.Time
	SecondaryUsedPercent   int
	SecondaryWindowMinutes int
	SecondaryResetAt       *time.Time
	CreditsHasCredits      bool
	CreditsUnlimited       bool
	CreditsBalance         string
	IsExceeded             bool
	ExceededAt             *time.Time
	RecoverAt              *time.Time
	ExceededReason         string
	UpdatedAt              time.Time
}

// SaveChannelQuota saves or updates quota data for a channel
func (m *Manager) SaveChannelQuota(q *ChannelQuota) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	query := `
	INSERT INTO channel_quota (
		channel_id, channel_name, plan_type,
		primary_used_percent, primary_window_minutes, primary_reset_at,
		secondary_used_percent, secondary_window_minutes, secondary_reset_at,
		credits_has_credits, credits_unlimited, credits_balance,
		is_exceeded, exceeded_at, recover_at, exceeded_reason, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(channel_id) DO UPDATE SET
		channel_name = excluded.channel_name,
		plan_type = excluded.plan_type,
		primary_used_percent = excluded.primary_used_percent,
		primary_window_minutes = excluded.primary_window_minutes,
		primary_reset_at = excluded.primary_reset_at,
		secondary_used_percent = excluded.secondary_used_percent,
		secondary_window_minutes = excluded.secondary_window_minutes,
		secondary_reset_at = excluded.secondary_reset_at,
		credits_has_credits = excluded.credits_has_credits,
		credits_unlimited = excluded.credits_unlimited,
		credits_balance = excluded.credits_balance,
		is_exceeded = excluded.is_exceeded,
		exceeded_at = excluded.exceeded_at,
		recover_at = excluded.recover_at,
		exceeded_reason = excluded.exceeded_reason,
		updated_at = excluded.updated_at
	`

	_, err := m.db.Exec(query,
		q.ChannelID, q.ChannelName, q.PlanType,
		q.PrimaryUsedPercent, q.PrimaryWindowMinutes, q.PrimaryResetAt,
		q.SecondaryUsedPercent, q.SecondaryWindowMinutes, q.SecondaryResetAt,
		q.CreditsHasCredits, q.CreditsUnlimited, q.CreditsBalance,
		q.IsExceeded, q.ExceededAt, q.RecoverAt, q.ExceededReason, time.Now(),
	)
	return err
}

// GetChannelQuota retrieves quota data for a specific channel
func (m *Manager) GetChannelQuota(channelID int) (*ChannelQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `
	SELECT channel_id, channel_name, plan_type,
		primary_used_percent, primary_window_minutes, primary_reset_at,
		secondary_used_percent, secondary_window_minutes, secondary_reset_at,
		credits_has_credits, credits_unlimited, credits_balance,
		is_exceeded, exceeded_at, recover_at, exceeded_reason, updated_at
	FROM channel_quota WHERE channel_id = ?
	`

	var q ChannelQuota
	var primaryResetAt, secondaryResetAt, exceededAt, recoverAt sql.NullTime

	err := m.db.QueryRow(query, channelID).Scan(
		&q.ChannelID, &q.ChannelName, &q.PlanType,
		&q.PrimaryUsedPercent, &q.PrimaryWindowMinutes, &primaryResetAt,
		&q.SecondaryUsedPercent, &q.SecondaryWindowMinutes, &secondaryResetAt,
		&q.CreditsHasCredits, &q.CreditsUnlimited, &q.CreditsBalance,
		&q.IsExceeded, &exceededAt, &recoverAt, &q.ExceededReason, &q.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if primaryResetAt.Valid {
		q.PrimaryResetAt = &primaryResetAt.Time
	}
	if secondaryResetAt.Valid {
		q.SecondaryResetAt = &secondaryResetAt.Time
	}
	if exceededAt.Valid {
		q.ExceededAt = &exceededAt.Time
	}
	if recoverAt.Valid {
		q.RecoverAt = &recoverAt.Time
	}

	return &q, nil
}

// GetAllChannelQuotas retrieves all stored quota data
func (m *Manager) GetAllChannelQuotas() ([]*ChannelQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `
	SELECT channel_id, channel_name, plan_type,
		primary_used_percent, primary_window_minutes, primary_reset_at,
		secondary_used_percent, secondary_window_minutes, secondary_reset_at,
		credits_has_credits, credits_unlimited, credits_balance,
		is_exceeded, exceeded_at, recover_at, exceeded_reason, updated_at
	FROM channel_quota
	`

	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quotas []*ChannelQuota
	for rows.Next() {
		var q ChannelQuota
		var primaryResetAt, secondaryResetAt, exceededAt, recoverAt sql.NullTime

		err := rows.Scan(
			&q.ChannelID, &q.ChannelName, &q.PlanType,
			&q.PrimaryUsedPercent, &q.PrimaryWindowMinutes, &primaryResetAt,
			&q.SecondaryUsedPercent, &q.SecondaryWindowMinutes, &secondaryResetAt,
			&q.CreditsHasCredits, &q.CreditsUnlimited, &q.CreditsBalance,
			&q.IsExceeded, &exceededAt, &recoverAt, &q.ExceededReason, &q.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if primaryResetAt.Valid {
			q.PrimaryResetAt = &primaryResetAt.Time
		}
		if secondaryResetAt.Valid {
			q.SecondaryResetAt = &secondaryResetAt.Time
		}
		if exceededAt.Valid {
			q.ExceededAt = &exceededAt.Time
		}
		if recoverAt.Valid {
			q.RecoverAt = &recoverAt.Time
		}

		quotas = append(quotas, &q)
	}

	return quotas, rows.Err()
}
