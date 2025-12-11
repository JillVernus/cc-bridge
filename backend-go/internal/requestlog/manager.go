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

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("⚠️ Failed to enable WAL mode: %v", err)
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
	// Create table without status index first (for backward compatibility)
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
		user_id TEXT,
		error TEXT,
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
		provider, provider_name, model, input_tokens, output_tokens,
		cache_creation_input_tokens, cache_read_input_tokens, total_tokens,
		price, http_status, stream, channel_id, channel_name,
		endpoint, user_id, error, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := m.db.Exec(query,
		record.ID,
		record.Status,
		record.InitialTime,
		record.CompleteTime,
		record.DurationMs,
		record.Type,
		record.ProviderName,
		record.Model,
		record.InputTokens,
		record.OutputTokens,
		record.CacheCreationInputTokens,
		record.CacheReadInputTokens,
		record.TotalTokens,
		record.Price,
		record.HTTPStatus,
		record.Stream,
		record.ChannelID,
		record.ChannelName,
		record.Endpoint,
		record.UserID,
		record.Error,
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
		model = ?,
		input_tokens = ?,
		output_tokens = ?,
		cache_creation_input_tokens = ?,
		cache_read_input_tokens = ?,
		total_tokens = ?,
		price = ?,
		http_status = ?,
		channel_name = ?,
		error = ?
	WHERE id = ?
	`

	result, err := m.db.Exec(query,
		record.Status,
		record.CompleteTime,
		record.DurationMs,
		record.Type,
		record.ProviderName,
		record.Model,
		record.InputTokens,
		record.OutputTokens,
		record.CacheCreationInputTokens,
		record.CacheReadInputTokens,
		record.TotalTokens,
		record.Price,
		record.HTTPStatus,
		record.ChannelName,
		record.Error,
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
			   provider, provider_name, model, input_tokens, output_tokens,
			   cache_creation_input_tokens, cache_read_input_tokens, total_tokens,
			   price, http_status, stream, channel_id, channel_name,
			   endpoint, user_id, error, created_at
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
		var channelID sql.NullInt64
		var channelName, endpoint, userID, errorStr, status, providerName sql.NullString
		var initialTime, completeTime, createdAt sql.NullString

		err := rows.Scan(
			&r.ID, &status, &initialTime, &completeTime, &r.DurationMs,
			&r.Type, &providerName, &r.Model, &r.InputTokens, &r.OutputTokens,
			&r.CacheCreationInputTokens, &r.CacheReadInputTokens, &r.TotalTokens,
			&r.Price, &r.HTTPStatus, &r.Stream, &channelID, &channelName,
			&endpoint, &userID, &errorStr, &createdAt,
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
		if userID.Valid {
			r.UserID = userID.String
		}
		if errorStr.Valid {
			r.Error = errorStr.String
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

	// Build where clause
	var conditions []string
	var args []interface{}

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

	stats := &RequestLogStats{
		ByProvider: make(map[string]ProviderStats),
		ByModel:    make(map[string]ModelStats),
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
			   endpoint, user_id, error, created_at
		FROM request_logs
		WHERE id = ?
	`

	var r RequestLog
	var channelID sql.NullInt64
	var channelName, endpoint, userID, errorStr sql.NullString

	err := m.db.QueryRow(query, id).Scan(
		&r.ID, &r.InitialTime, &r.CompleteTime, &r.DurationMs,
		&r.Type, &r.Model, &r.InputTokens, &r.OutputTokens,
		&r.CacheCreationInputTokens, &r.CacheReadInputTokens, &r.TotalTokens,
		&r.Price, &r.HTTPStatus, &r.Stream, &channelID, &channelName,
		&endpoint, &userID, &errorStr, &r.CreatedAt,
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
	if userID.Valid {
		r.UserID = userID.String
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

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// generateID creates a unique request ID
func generateID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// parseTimeString parses a time string from SQLite in various formats
func parseTimeString(s string) time.Time {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999Z07:00",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
