package requestlog

import (
	"database/sql"
	"fmt"
	"log"
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

	// Build where clause - exclude pending and timeout requests from statistics
	var conditions []string
	var args []interface{}

	// Only count completed requests in statistics (exclude pending/timeout requests)
	conditions = append(conditions, "status NOT IN (?, ?)")
	args = append(args, StatusPending, StatusTimeout)

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
		  AND status NOT IN (?, ?)
		GROUP BY session_id
		HAVING MAX(initial_time) > ?
		ORDER BY last_request_time DESC
	`

	rows, err := m.db.Query(query, StatusPending, StatusTimeout, cutoff)
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
