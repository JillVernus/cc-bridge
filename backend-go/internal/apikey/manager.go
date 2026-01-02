package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	keyPrefix    = "sk-"
	keyLength    = 48 // 48 random alphanumeric chars after prefix
	prefixLength = 8  // First 8 chars for display
)

// Manager manages API key storage and validation using SQLite
type Manager struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]*ValidatedKey // key_hash -> ValidatedKey
}

// NewManager creates a new API key manager with SQLite storage
func NewManager(db *sql.DB) (*Manager, error) {
	m := &Manager{
		db:    db,
		cache: make(map[string]*ValidatedKey),
	}

	if err := m.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize api_keys schema: %w", err)
	}

	// Load active keys into cache
	if err := m.refreshCache(); err != nil {
		log.Printf("Warning: failed to load API keys into cache: %v", err)
	}

	log.Printf("API key manager initialized")
	return m, nil
}

// initSchema creates the api_keys table and indexes
func (m *Manager) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		description TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		is_admin BOOLEAN NOT NULL DEFAULT 0,
		rate_limit_rpm INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
	CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);
	`

	_, err := m.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add rate_limit_rpm column if it doesn't exist
	_, err = m.db.Exec(`ALTER TABLE api_keys ADD COLUMN rate_limit_rpm INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		// Ignore duplicate-column errors; otherwise fail fast since later queries rely on this column.
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("rate_limit_rpm column migration failed: %w", err)
		}
	}

	// Migration: add permission columns if they don't exist
	permissionColumns := []string{
		"allowed_endpoints TEXT",
		"allowed_channels_msg TEXT",
		"allowed_channels_resp TEXT",
		"allowed_models TEXT",
	}
	for _, col := range permissionColumns {
		colName := strings.Split(col, " ")[0]
		_, err = m.db.Exec(fmt.Sprintf(`ALTER TABLE api_keys ADD COLUMN %s`, col))
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("Warning: failed to add %s column: %v", colName, err)
		}
	}

	return nil
}

// refreshCache loads all active keys into the in-memory cache
func (m *Manager) refreshCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rows, err := m.db.Query(`
		SELECT id, name, key_hash, is_admin, rate_limit_rpm,
			   allowed_endpoints, allowed_channels_msg, allowed_channels_resp, allowed_models
		FROM api_keys
		WHERE status = ?
	`, StatusActive)
	if err != nil {
		return err
	}
	defer rows.Close()

	newCache := make(map[string]*ValidatedKey)
	for rows.Next() {
		var id int64
		var name, keyHash string
		var isAdmin bool
		var rateLimitRPM int
		var allowedEndpoints, allowedChannelsMsg, allowedChannelsResp, allowedModels sql.NullString

		if err := rows.Scan(&id, &name, &keyHash, &isAdmin, &rateLimitRPM,
			&allowedEndpoints, &allowedChannelsMsg, &allowedChannelsResp, &allowedModels); err != nil {
			return err
		}
		newCache[keyHash] = &ValidatedKey{
			ID:                  id,
			Name:                name,
			IsAdmin:             isAdmin,
			RateLimitRPM:        rateLimitRPM,
			AllowedEndpoints:    unmarshalStringSlice(allowedEndpoints),
			AllowedChannelsMsg:  unmarshalIntSlice(allowedChannelsMsg),
			AllowedChannelsResp: unmarshalIntSlice(allowedChannelsResp),
			AllowedModels:       unmarshalStringSlice(allowedModels),
		}
	}

	m.cache = newCache
	return nil
}

// generateKey creates a new random API key
func generateKey() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, keyLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return keyPrefix + string(b), nil
}

// hashKey creates a SHA-256 hash of the key
func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// getKeyPrefix extracts the display prefix from a key
func getKeyPrefix(key string) string {
	if len(key) < prefixLength {
		return key + "..."
	}
	return key[:prefixLength] + "..."
}

// Create creates a new API key
func (m *Manager) Create(req *CreateAPIKeyRequest) (*CreateAPIKeyResponse, error) {
	key, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	keyHash := hashKey(key)
	keyPrefixStr := getKeyPrefix(key)
	now := time.Now()

	// Marshal permission fields to JSON
	allowedEndpoints := marshalJSONNullable(req.AllowedEndpoints)
	allowedChannelsMsg := marshalJSONNullable(req.AllowedChannelsMsg)
	allowedChannelsResp := marshalJSONNullable(req.AllowedChannelsResp)
	allowedModels := marshalJSONNullable(req.AllowedModels)

	result, err := m.db.Exec(`
		INSERT INTO api_keys (name, key_hash, key_prefix, description, status, is_admin, rate_limit_rpm,
			allowed_endpoints, allowed_channels_msg, allowed_channels_resp, allowed_models,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, req.Name, keyHash, keyPrefixStr, req.Description, StatusActive, req.IsAdmin, req.RateLimitRPM,
		allowedEndpoints, allowedChannelsMsg, allowedChannelsResp, allowedModels, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert API key: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	// Add to cache
	m.mu.Lock()
	m.cache[keyHash] = &ValidatedKey{
		ID:                  id,
		Name:                req.Name,
		IsAdmin:             req.IsAdmin,
		RateLimitRPM:        req.RateLimitRPM,
		AllowedEndpoints:    req.AllowedEndpoints,
		AllowedChannelsMsg:  req.AllowedChannelsMsg,
		AllowedChannelsResp: req.AllowedChannelsResp,
		AllowedModels:       req.AllowedModels,
	}
	m.mu.Unlock()

	return &CreateAPIKeyResponse{
		APIKey: APIKey{
			ID:                  id,
			Name:                req.Name,
			KeyPrefix:           keyPrefixStr,
			Description:         req.Description,
			Status:              StatusActive,
			IsAdmin:             req.IsAdmin,
			RateLimitRPM:        req.RateLimitRPM,
			AllowedEndpoints:    req.AllowedEndpoints,
			AllowedChannelsMsg:  req.AllowedChannelsMsg,
			AllowedChannelsResp: req.AllowedChannelsResp,
			AllowedModels:       req.AllowedModels,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		Key: key,
	}, nil
}

// GetByID retrieves an API key by ID
func (m *Manager) GetByID(id int64) (*APIKey, error) {
	var key APIKey
	var createdAt, updatedAt string
	var lastUsedAt sql.NullString
	var description sql.NullString
	var allowedEndpoints, allowedChannelsMsg, allowedChannelsResp, allowedModels sql.NullString

	err := m.db.QueryRow(`
		SELECT id, name, key_prefix, description, status, is_admin, rate_limit_rpm,
			   allowed_endpoints, allowed_channels_msg, allowed_channels_resp, allowed_models,
			   created_at, updated_at, last_used_at
		FROM api_keys
		WHERE id = ?
	`, id).Scan(&key.ID, &key.Name, &key.KeyPrefix, &description, &key.Status, &key.IsAdmin, &key.RateLimitRPM,
		&allowedEndpoints, &allowedChannelsMsg, &allowedChannelsResp, &allowedModels,
		&createdAt, &updatedAt, &lastUsedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	if description.Valid {
		key.Description = description.String
	}
	key.CreatedAt = parseTimeString(createdAt)
	key.UpdatedAt = parseTimeString(updatedAt)
	if lastUsedAt.Valid {
		t := parseTimeString(lastUsedAt.String)
		key.LastUsedAt = &t
	}

	// Parse permission fields
	key.AllowedEndpoints = unmarshalStringSlice(allowedEndpoints)
	key.AllowedChannelsMsg = unmarshalIntSlice(allowedChannelsMsg)
	key.AllowedChannelsResp = unmarshalIntSlice(allowedChannelsResp)
	key.AllowedModels = unmarshalStringSlice(allowedModels)

	return &key, nil
}

// List retrieves all API keys with optional filtering
func (m *Manager) List(filter *APIKeyFilter) (*APIKeyListResponse, error) {
	if filter == nil {
		filter = &APIKeyFilter{}
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	var conditions []string
	var args []interface{}

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM api_keys %s", whereClause)
	var total int64
	if err := m.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count API keys: %w", err)
	}

	// Get records
	query := fmt.Sprintf(`
		SELECT id, name, key_prefix, description, status, is_admin, rate_limit_rpm,
			   allowed_endpoints, allowed_channels_msg, allowed_channels_resp, allowed_models,
			   created_at, updated_at, last_used_at
		FROM api_keys
		%s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query API keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		var createdAt, updatedAt string
		var lastUsedAt sql.NullString
		var description sql.NullString
		var allowedEndpoints, allowedChannelsMsg, allowedChannelsResp, allowedModels sql.NullString

		err := rows.Scan(&key.ID, &key.Name, &key.KeyPrefix, &description, &key.Status, &key.IsAdmin, &key.RateLimitRPM,
			&allowedEndpoints, &allowedChannelsMsg, &allowedChannelsResp, &allowedModels,
			&createdAt, &updatedAt, &lastUsedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}

		if description.Valid {
			key.Description = description.String
		}
		key.CreatedAt = parseTimeString(createdAt)
		key.UpdatedAt = parseTimeString(updatedAt)
		if lastUsedAt.Valid {
			t := parseTimeString(lastUsedAt.String)
			key.LastUsedAt = &t
		}

		// Parse permission fields
		key.AllowedEndpoints = unmarshalStringSlice(allowedEndpoints)
		key.AllowedChannelsMsg = unmarshalIntSlice(allowedChannelsMsg)
		key.AllowedChannelsResp = unmarshalIntSlice(allowedChannelsResp)
		key.AllowedModels = unmarshalStringSlice(allowedModels)

		keys = append(keys, key)
	}

	return &APIKeyListResponse{
		Keys:    keys,
		Total:   total,
		HasMore: int64(filter.Offset+len(keys)) < total,
	}, nil
}

// Update updates an API key's name, description, rate limit, and permissions
func (m *Manager) Update(id int64, req *UpdateAPIKeyRequest) (*APIKey, error) {
	// Build update query dynamically
	var updates []string
	var args []interface{}
	permissionsChanged := false

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Description != nil {
		updates = append(updates, "description = ?")
		args = append(args, *req.Description)
	}
	if req.RateLimitRPM != nil {
		updates = append(updates, "rate_limit_rpm = ?")
		args = append(args, *req.RateLimitRPM)
	}

	// Handle permission fields
	if req.AllowedEndpoints != nil {
		updates = append(updates, "allowed_endpoints = ?")
		args = append(args, marshalJSONNullable(*req.AllowedEndpoints))
		permissionsChanged = true
	}
	if req.AllowedChannelsMsg != nil {
		updates = append(updates, "allowed_channels_msg = ?")
		args = append(args, marshalJSONNullable(*req.AllowedChannelsMsg))
		permissionsChanged = true
	}
	if req.AllowedChannelsResp != nil {
		updates = append(updates, "allowed_channels_resp = ?")
		args = append(args, marshalJSONNullable(*req.AllowedChannelsResp))
		permissionsChanged = true
	}
	if req.AllowedModels != nil {
		updates = append(updates, "allowed_models = ?")
		args = append(args, marshalJSONNullable(*req.AllowedModels))
		permissionsChanged = true
	}

	if len(updates) == 0 {
		return m.GetByID(id)
	}

	updates = append(updates, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = ?", strings.Join(updates, ", "))
	result, err := m.db.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update API key: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("API key not found")
	}

	// Update cache if name, rate limit, or permissions changed
	if req.Name != nil || req.RateLimitRPM != nil || permissionsChanged {
		m.refreshCache()
	}

	return m.GetByID(id)
}

// Delete permanently deletes an API key
func (m *Manager) Delete(id int64) error {
	// Get the key hash first to remove from cache
	var keyHash string
	err := m.db.QueryRow("SELECT key_hash FROM api_keys WHERE id = ?", id).Scan(&keyHash)
	if err == sql.ErrNoRows {
		return fmt.Errorf("API key not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get API key: %w", err)
	}

	result, err := m.db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("API key not found")
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.cache, keyHash)
	m.mu.Unlock()

	return nil
}

// SetStatus updates the status of an API key
func (m *Manager) SetStatus(id int64, status string) error {
	if status != StatusActive && status != StatusDisabled && status != StatusRevoked {
		return fmt.Errorf("invalid status: %s", status)
	}

	result, err := m.db.Exec(`
		UPDATE api_keys SET status = ?, updated_at = ? WHERE id = ?
	`, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update API key status: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("API key not found")
	}

	// Refresh cache to reflect status change
	m.refreshCache()

	return nil
}

// Enable enables a disabled API key
func (m *Manager) Enable(id int64) error {
	// Check current status - can only enable disabled keys
	var currentStatus string
	err := m.db.QueryRow("SELECT status FROM api_keys WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("API key not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get API key: %w", err)
	}

	if currentStatus == StatusRevoked {
		return fmt.Errorf("cannot enable a revoked key")
	}
	if currentStatus == StatusActive {
		return nil // Already active
	}

	return m.SetStatus(id, StatusActive)
}

// Disable disables an API key (can be re-enabled)
func (m *Manager) Disable(id int64) error {
	var currentStatus string
	err := m.db.QueryRow("SELECT status FROM api_keys WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("API key not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get API key: %w", err)
	}

	if currentStatus == StatusRevoked {
		return fmt.Errorf("cannot disable a revoked key")
	}

	return m.SetStatus(id, StatusDisabled)
}

// Revoke permanently revokes an API key (cannot be re-enabled)
func (m *Manager) Revoke(id int64) error {
	return m.SetStatus(id, StatusRevoked)
}

// Validate validates an API key and returns the key metadata if valid
// Returns nil if the key is invalid or not found
func (m *Manager) Validate(key string) *ValidatedKey {
	if key == "" {
		return nil
	}

	keyHash := hashKey(key)

	// Check cache first
	m.mu.RLock()
	if vk, ok := m.cache[keyHash]; ok {
		m.mu.RUnlock()
		// Update last_used_at asynchronously
		go m.updateLastUsed(vk.ID)
		return vk
	}
	m.mu.RUnlock()

	// Not in cache, check database (might be a newly created key)
	var id int64
	var name, status string
	var isAdmin bool
	var rateLimitRPM int
	var allowedEndpoints, allowedChannelsMsg, allowedChannelsResp, allowedModels sql.NullString

	err := m.db.QueryRow(`
		SELECT id, name, status, is_admin, rate_limit_rpm,
			   allowed_endpoints, allowed_channels_msg, allowed_channels_resp, allowed_models
		FROM api_keys
		WHERE key_hash = ?
	`, keyHash).Scan(&id, &name, &status, &isAdmin, &rateLimitRPM,
		&allowedEndpoints, &allowedChannelsMsg, &allowedChannelsResp, &allowedModels)

	if err != nil || status != StatusActive {
		return nil
	}

	vk := &ValidatedKey{
		ID:                  id,
		Name:                name,
		IsAdmin:             isAdmin,
		RateLimitRPM:        rateLimitRPM,
		AllowedEndpoints:    unmarshalStringSlice(allowedEndpoints),
		AllowedChannelsMsg:  unmarshalIntSlice(allowedChannelsMsg),
		AllowedChannelsResp: unmarshalIntSlice(allowedChannelsResp),
		AllowedModels:       unmarshalStringSlice(allowedModels),
	}

	// Add to cache
	m.mu.Lock()
	m.cache[keyHash] = vk
	m.mu.Unlock()

	// Update last_used_at asynchronously
	go m.updateLastUsed(id)

	return vk
}

// updateLastUsed updates the last_used_at timestamp for a key
// Uses retry logic to handle database busy errors
func (m *Manager) updateLastUsed(id int64) {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		_, err := m.db.Exec("UPDATE api_keys SET last_used_at = ? WHERE id = ?", time.Now(), id)
		if err == nil {
			return
		}
		// If database is busy, wait a bit and retry
		if i < maxRetries-1 {
			time.Sleep(time.Duration(50*(i+1)) * time.Millisecond)
		}
	}
	// Silently ignore if all retries fail - this is a non-critical update
}

// parseTimeString parses a time string from SQLite
func parseTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	// Strip monotonic clock part if present
	if idx := strings.Index(s, " m="); idx != -1 {
		s = s[:idx]
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 +0800 CST",
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

// marshalJSONNullable marshals a slice to JSON for database storage
// Returns sql.NullString with Valid=false if slice is nil or empty
func marshalJSONNullable(v interface{}) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}

	data, err := json.Marshal(v)
	if err != nil || string(data) == "null" || string(data) == "[]" {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

// unmarshalStringSlice unmarshals a JSON string slice from database
func unmarshalStringSlice(s sql.NullString) []string {
	if !s.Valid || s.String == "" || s.String == "null" || s.String == "[]" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s.String), &result); err != nil {
		return nil
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// unmarshalIntSlice unmarshals a JSON int slice from database
func unmarshalIntSlice(s sql.NullString) []int {
	if !s.Valid || s.String == "" || s.String == "null" || s.String == "[]" {
		return nil
	}
	var result []int
	if err := json.Unmarshal([]byte(s.String), &result); err != nil {
		return nil
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
