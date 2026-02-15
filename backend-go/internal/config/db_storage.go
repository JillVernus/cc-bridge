package config

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// DBConfigStorage provides database-backed storage for configuration
// It works alongside the existing ConfigManager, syncing config to/from DB
type DBConfigStorage struct {
	db           database.DB
	pollInterval time.Duration
	stopPoll     chan struct{}
	pollWg       sync.WaitGroup
	lastVersion  int64
	cm           *ConfigManager
}

// NewDBConfigStorage creates a new database config storage adapter
func NewDBConfigStorage(db database.DB, pollInterval time.Duration) *DBConfigStorage {
	return &DBConfigStorage{
		db:           db,
		pollInterval: pollInterval,
		stopPoll:     make(chan struct{}),
	}
}

// SetConfigManager sets the ConfigManager to sync with
func (s *DBConfigStorage) SetConfigManager(cm *ConfigManager) {
	s.cm = cm
}

// MigrateFromJSONIfNeeded checks if JSON config exists and DB is empty, then migrates
func (s *DBConfigStorage) MigrateFromJSONIfNeeded(jsonPath string) error {
	// Check if channels table is empty
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&count); err != nil {
		return fmt.Errorf("failed to check channels table: %w", err)
	}

	if count > 0 {
		log.Printf("📦 Database already has %d channels, skipping JSON migration", count)
		return nil
	}

	// Check if JSON config exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		log.Printf("📦 No JSON config found at %s, starting fresh", jsonPath)
		return nil
	}

	log.Printf("📦 Migrating configuration from %s to database...", jsonPath)

	// Read JSON config
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read JSON config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse JSON config: %w", err)
	}

	// Begin transaction
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Migrate Messages channels
	for i, upstream := range config.Upstream {
		if err := s.insertChannelTx(tx, "messages", i, &upstream); err != nil {
			return fmt.Errorf("failed to insert messages channel %d: %w", i, err)
		}
	}

	// Migrate Responses channels
	for i, upstream := range config.ResponsesUpstream {
		if err := s.insertChannelTx(tx, "responses", i, &upstream); err != nil {
			return fmt.Errorf("failed to insert responses channel %d: %w", i, err)
		}
	}

	// Migrate settings
	settings := map[string]string{
		"messages_load_balance":  config.LoadBalance,
		"responses_load_balance": config.ResponsesLoadBalance,
		"gemini_load_balance":    config.GeminiLoadBalance,
	}

	// Migrate debug log config
	debugConfig, _ := json.Marshal(config.DebugLog)
	settings["debug_log"] = string(debugConfig)

	// Migrate failover config
	failoverConfig, _ := json.Marshal(config.Failover)
	settings["failover"] = string(failoverConfig)

	// Migrate user-agent config
	userAgentConfig, _ := json.Marshal(config.UserAgent)
	settings["user_agent"] = string(userAgentConfig)

	for key, value := range settings {
		if value == "" {
			continue
		}
		_, err := tx.Exec(
			"INSERT INTO settings (key, value, category) VALUES (?, ?, ?)",
			key, value, "config",
		)
		if err != nil {
			return fmt.Errorf("failed to insert setting %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	// Create backup of JSON file
	backupPath := jsonPath + ".migrated-" + time.Now().Format("20060102-150405")
	if err := os.Rename(jsonPath, backupPath); err != nil {
		log.Printf("⚠️ Failed to backup JSON config: %v", err)
	} else {
		log.Printf("✅ JSON config migrated to database. Backup: %s", backupPath)
	}

	return nil
}

// insertChannelTx inserts a channel into the database within a transaction
func (s *DBConfigStorage) insertChannelTx(tx *database.Tx, channelType string, index int, upstream *UpstreamConfig) error {
	channelID := upstream.ID

	// Serialize complex fields to JSON
	apiKeys, _ := json.Marshal(upstream.APIKeys)
	modelMapping, _ := json.Marshal(upstream.ModelMapping)
	priceMultipliers, _ := json.Marshal(upstream.PriceMultipliers)
	quotaModels, _ := json.Marshal(upstream.QuotaModels)
	compositeMappings, _ := json.Marshal(upstream.CompositeMappings)
	var contentFilter []byte
	if upstream.ContentFilter != nil {
		contentFilter, _ = json.Marshal(upstream.ContentFilter)
	}

	var oauthTokens []byte
	if upstream.OAuthTokens != nil {
		oauthTokens, _ = json.Marshal(upstream.OAuthTokens)
	}

	var promotionUntil *string
	if upstream.PromotionUntil != nil {
		t := upstream.PromotionUntil.Format(time.RFC3339)
		promotionUntil = &t
	}

	var quotaResetAt *string
	if upstream.QuotaResetAt != nil {
		t := upstream.QuotaResetAt.Format(time.RFC3339)
		quotaResetAt = &t
	}

	status := upstream.Status
	if status == "" {
		status = "active"
	}

	// Build dialect-aware INSERT query
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `
			INSERT INTO channels (
				channel_id, channel_type, name, description, website, service_type,
				base_url, insecure_skip_verify, status, priority, promotion_until,
				response_header_timeout, quota_type, quota_limit, quota_reset_at,
				quota_reset_interval, quota_reset_unit, quota_reset_mode,
				rate_limit_rpm, queue_enabled, queue_timeout, key_load_balance,
				api_keys, model_mapping, price_multipliers, oauth_tokens,
				quota_models, composite_mappings, content_filter
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
		`
	} else {
		query = `
			INSERT INTO channels (
				channel_id, channel_type, name, description, website, service_type,
				base_url, insecure_skip_verify, status, priority, promotion_until,
				response_header_timeout, quota_type, quota_limit, quota_reset_at,
				quota_reset_interval, quota_reset_unit, quota_reset_mode,
				rate_limit_rpm, queue_enabled, queue_timeout, key_load_balance,
				api_keys, model_mapping, price_multipliers, oauth_tokens,
				quota_models, composite_mappings, content_filter
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
	}

	_, err := tx.Exec(query,
		channelID, channelType, upstream.Name, upstream.Description, upstream.Website,
		upstream.ServiceType, upstream.BaseURL, upstream.InsecureSkipVerify, status,
		upstream.Priority, promotionUntil, upstream.ResponseHeaderTimeoutSecs,
		upstream.QuotaType, upstream.QuotaLimit, quotaResetAt,
		upstream.QuotaResetInterval, upstream.QuotaResetUnit, upstream.QuotaResetMode,
		upstream.RateLimitRpm, upstream.QueueEnabled, upstream.QueueTimeout,
		upstream.KeyLoadBalance, string(apiKeys), string(modelMapping),
		string(priceMultipliers), string(oauthTokens), string(quotaModels),
		string(compositeMappings), string(contentFilter),
	)
	return err
}

// LoadConfigFromDB loads the full configuration from the database
func (s *DBConfigStorage) LoadConfigFromDB() (*Config, error) {
	config := &Config{}

	// Load settings
	query := "SELECT key, value FROM settings WHERE category = ?"
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = "SELECT key, value FROM settings WHERE category = $1"
	}
	rows, err := s.db.Query(query, "config")
	if err != nil {
		return nil, fmt.Errorf("failed to query settings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		switch key {
		case "messages_load_balance":
			config.LoadBalance = value
		case "responses_load_balance":
			config.ResponsesLoadBalance = value
		case "gemini_load_balance":
			config.GeminiLoadBalance = value
		case "debug_log":
			json.Unmarshal([]byte(value), &config.DebugLog)
		case "failover":
			json.Unmarshal([]byte(value), &config.Failover)
		case "user_agent":
			json.Unmarshal([]byte(value), &config.UserAgent)
		}
	}

	// Load Messages channels
	config.Upstream, err = s.loadChannels("messages")
	if err != nil {
		return nil, fmt.Errorf("failed to load messages channels: %w", err)
	}

	// Load Responses channels
	config.ResponsesUpstream, err = s.loadChannels("responses")
	if err != nil {
		return nil, fmt.Errorf("failed to load responses channels: %w", err)
	}

	// Load Gemini channels
	config.GeminiUpstream, err = s.loadChannels("gemini")
	if err != nil {
		return nil, fmt.Errorf("failed to load gemini channels: %w", err)
	}

	// Ensure defaults are present for missing/legacy records.
	normalizeUserAgentConfig(&config.UserAgent)

	return config, nil
}

// loadChannels loads channels of a specific type from the database
func (s *DBConfigStorage) loadChannels(channelType string) ([]UpstreamConfig, error) {
	query := `
		SELECT id, channel_id, name, description, website, service_type,
			base_url, insecure_skip_verify, status, priority, promotion_until,
			response_header_timeout, quota_type, quota_limit, quota_reset_at,
			quota_reset_interval, quota_reset_unit, quota_reset_mode,
			rate_limit_rpm, queue_enabled, queue_timeout, key_load_balance,
			api_keys, model_mapping, price_multipliers, oauth_tokens,
			quota_models, composite_mappings, content_filter
		FROM channels
		WHERE channel_type = ?
		ORDER BY priority, id
	`
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `
			SELECT id, channel_id, name, description, website, service_type,
				base_url, insecure_skip_verify, status, priority, promotion_until,
				response_header_timeout, quota_type, quota_limit, quota_reset_at,
				quota_reset_interval, quota_reset_unit, quota_reset_mode,
				rate_limit_rpm, queue_enabled, queue_timeout, key_load_balance,
				api_keys, model_mapping, price_multipliers, oauth_tokens,
				quota_models, composite_mappings, content_filter
			FROM channels
			WHERE channel_type = $1
			ORDER BY priority, id
		`
	}
	rows, err := s.db.Query(query, channelType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []UpstreamConfig
	for rows.Next() {
		var (
			id                    int
			channelID             string
			name, desc, website   sql.NullString
			serviceType, baseURL  string
			insecureSkipVerify    bool
			status                string
			priority              int
			promotionUntil        sql.NullString
			responseHeaderTimeout int
			quotaType             sql.NullString
			quotaLimit            float64
			quotaResetAt          sql.NullString
			quotaResetInterval    int
			quotaResetUnit        sql.NullString
			quotaResetMode        sql.NullString
			rateLimitRpm          int
			queueEnabled          bool
			queueTimeout          int
			keyLoadBalance        sql.NullString
			apiKeysJSON           sql.NullString
			modelMappingJSON      sql.NullString
			priceMultipliersJSON  sql.NullString
			oauthTokensJSON       sql.NullString
			quotaModelsJSON       sql.NullString
			compositeMappingsJSON sql.NullString
			contentFilterJSON     sql.NullString
		)

		err := rows.Scan(
			&id, &channelID, &name, &desc, &website, &serviceType,
			&baseURL, &insecureSkipVerify, &status, &priority, &promotionUntil,
			&responseHeaderTimeout, &quotaType, &quotaLimit, &quotaResetAt,
			&quotaResetInterval, &quotaResetUnit, &quotaResetMode,
			&rateLimitRpm, &queueEnabled, &queueTimeout, &keyLoadBalance,
			&apiKeysJSON, &modelMappingJSON, &priceMultipliersJSON, &oauthTokensJSON,
			&quotaModelsJSON, &compositeMappingsJSON, &contentFilterJSON,
		)
		if err != nil {
			log.Printf("⚠️ Failed to scan channel: %v", err)
			continue
		}

		upstream := UpstreamConfig{
			Index:                     len(channels),
			ID:                        channelID,
			Name:                      name.String,
			Description:               desc.String,
			Website:                   website.String,
			ServiceType:               serviceType,
			BaseURL:                   baseURL,
			InsecureSkipVerify:        insecureSkipVerify,
			Status:                    status,
			Priority:                  priority,
			ResponseHeaderTimeoutSecs: responseHeaderTimeout,
			QuotaType:                 quotaType.String,
			QuotaLimit:                quotaLimit,
			QuotaResetInterval:        quotaResetInterval,
			QuotaResetUnit:            quotaResetUnit.String,
			QuotaResetMode:            quotaResetMode.String,
			RateLimitRpm:              rateLimitRpm,
			QueueEnabled:              queueEnabled,
			QueueTimeout:              queueTimeout,
			KeyLoadBalance:            keyLoadBalance.String,
		}

		// Parse promotion until
		if promotionUntil.Valid && promotionUntil.String != "" {
			if t, err := time.Parse(time.RFC3339, promotionUntil.String); err == nil {
				upstream.PromotionUntil = &t
			}
		}

		// Parse quota reset at
		if quotaResetAt.Valid && quotaResetAt.String != "" {
			if t, err := time.Parse(time.RFC3339, quotaResetAt.String); err == nil {
				upstream.QuotaResetAt = &t
			}
		}

		// Parse JSON fields
		if apiKeysJSON.Valid && apiKeysJSON.String != "" {
			json.Unmarshal([]byte(apiKeysJSON.String), &upstream.APIKeys)
		}
		if modelMappingJSON.Valid && modelMappingJSON.String != "" {
			json.Unmarshal([]byte(modelMappingJSON.String), &upstream.ModelMapping)
		}
		if priceMultipliersJSON.Valid && priceMultipliersJSON.String != "" {
			json.Unmarshal([]byte(priceMultipliersJSON.String), &upstream.PriceMultipliers)
		}
		if oauthTokensJSON.Valid && oauthTokensJSON.String != "" {
			json.Unmarshal([]byte(oauthTokensJSON.String), &upstream.OAuthTokens)
		}
		if quotaModelsJSON.Valid && quotaModelsJSON.String != "" {
			json.Unmarshal([]byte(quotaModelsJSON.String), &upstream.QuotaModels)
		}
		if compositeMappingsJSON.Valid && compositeMappingsJSON.String != "" {
			json.Unmarshal([]byte(compositeMappingsJSON.String), &upstream.CompositeMappings)
		}
		if contentFilterJSON.Valid && contentFilterJSON.String != "" {
			json.Unmarshal([]byte(contentFilterJSON.String), &upstream.ContentFilter)
		}

		channels = append(channels, upstream)
	}

	return channels, nil
}

// SaveConfigToDB saves the current configuration to the database
// Uses smart UPDATE/INSERT/DELETE to only modify changed channels
func (s *DBConfigStorage) SaveConfigToDB(config *Config) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Sync Messages channels
	if err := s.syncChannelsTx(tx, "messages", config.Upstream); err != nil {
		return fmt.Errorf("failed to sync messages channels: %w", err)
	}

	// Sync Responses channels
	if err := s.syncChannelsTx(tx, "responses", config.ResponsesUpstream); err != nil {
		return fmt.Errorf("failed to sync responses channels: %w", err)
	}

	// Sync Gemini channels
	if err := s.syncChannelsTx(tx, "gemini", config.GeminiUpstream); err != nil {
		return fmt.Errorf("failed to sync gemini channels: %w", err)
	}

	// Update settings
	settings := map[string]string{
		"messages_load_balance":  config.LoadBalance,
		"responses_load_balance": config.ResponsesLoadBalance,
		"gemini_load_balance":    config.GeminiLoadBalance,
	}

	debugConfig, _ := json.Marshal(config.DebugLog)
	settings["debug_log"] = string(debugConfig)

	failoverConfig, _ := json.Marshal(config.Failover)
	settings["failover"] = string(failoverConfig)

	userAgentConfig, _ := json.Marshal(config.UserAgent)
	settings["user_agent"] = string(userAgentConfig)

	for key, value := range settings {
		var query string
		if s.db.Dialect() == database.DialectPostgreSQL {
			query = `
				INSERT INTO settings (key, value, category, updated_at)
				VALUES ($1, $2, 'config', CURRENT_TIMESTAMP)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
			`
		} else {
			query = `
				INSERT INTO settings (key, value, category, updated_at)
				VALUES (?, ?, 'config', CURRENT_TIMESTAMP)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
			`
		}
		_, err := tx.Exec(query, key, value)
		if err != nil {
			return fmt.Errorf("failed to upsert setting %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// syncChannelsTx synchronizes channels of a specific type using smart UPDATE/INSERT/DELETE
func (s *DBConfigStorage) syncChannelsTx(tx *database.Tx, channelType string, channels []UpstreamConfig) error {
	// Load existing channel IDs from database
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = "SELECT channel_id FROM channels WHERE channel_type = $1"
	} else {
		query = "SELECT channel_id FROM channels WHERE channel_type = ?"
	}

	rows, err := tx.Query(query, channelType)
	if err != nil {
		return fmt.Errorf("failed to query existing channels: %w", err)
	}

	existingIDs := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan channel_id: %w", err)
		}
		existingIDs[id] = true
	}
	rows.Close()

	// Track which IDs we're keeping
	keepIDs := make(map[string]bool)

	// UPDATE or INSERT each channel
	for i := range channels {
		upstream := &channels[i]
		// Generate channel ID if not present and write it back to the slice
		// so that insertChannelTx uses the same ID (preventing double-generation)
		if upstream.ID == "" {
			upstream.ID = generateChannelID()
		}
		keepIDs[upstream.ID] = true

		if existingIDs[upstream.ID] {
			// UPDATE existing channel
			if err := s.updateChannelTx(tx, channelType, upstream.ID, i, upstream); err != nil {
				return fmt.Errorf("failed to update channel %s: %w", upstream.ID, err)
			}
		} else {
			// INSERT new channel
			if err := s.insertChannelTx(tx, channelType, i, upstream); err != nil {
				return fmt.Errorf("failed to insert channel %s: %w", upstream.ID, err)
			}
		}
	}

	// DELETE channels that are no longer in the config
	for id := range existingIDs {
		if !keepIDs[id] {
			var deleteQuery string
			if s.db.Dialect() == database.DialectPostgreSQL {
				deleteQuery = "DELETE FROM channels WHERE channel_id = $1 AND channel_type = $2"
			} else {
				deleteQuery = "DELETE FROM channels WHERE channel_id = ? AND channel_type = ?"
			}
			if _, err := tx.Exec(deleteQuery, id, channelType); err != nil {
				return fmt.Errorf("failed to delete channel %s: %w", id, err)
			}
		}
	}

	return nil
}

// updateChannelTx updates an existing channel in the database
func (s *DBConfigStorage) updateChannelTx(tx *database.Tx, channelType string, channelID string, index int, upstream *UpstreamConfig) error {
	// Serialize complex fields to JSON
	apiKeys, _ := json.Marshal(upstream.APIKeys)
	modelMapping, _ := json.Marshal(upstream.ModelMapping)
	priceMultipliers, _ := json.Marshal(upstream.PriceMultipliers)
	quotaModels, _ := json.Marshal(upstream.QuotaModels)
	compositeMappings, _ := json.Marshal(upstream.CompositeMappings)
	var contentFilter []byte
	if upstream.ContentFilter != nil {
		contentFilter, _ = json.Marshal(upstream.ContentFilter)
	}

	var oauthTokens []byte
	if upstream.OAuthTokens != nil {
		oauthTokens, _ = json.Marshal(upstream.OAuthTokens)
	}

	var promotionUntil *string
	if upstream.PromotionUntil != nil {
		t := upstream.PromotionUntil.Format(time.RFC3339)
		promotionUntil = &t
	}

	var quotaResetAt *string
	if upstream.QuotaResetAt != nil {
		t := upstream.QuotaResetAt.Format(time.RFC3339)
		quotaResetAt = &t
	}

	status := upstream.Status
	if status == "" {
		status = "active"
	}

	// Build dialect-aware UPDATE query
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `
			UPDATE channels SET
				name = $1, description = $2, website = $3, service_type = $4,
				base_url = $5, insecure_skip_verify = $6, status = $7, priority = $8,
				promotion_until = $9, response_header_timeout = $10, quota_type = $11,
				quota_limit = $12, quota_reset_at = $13, quota_reset_interval = $14,
				quota_reset_unit = $15, quota_reset_mode = $16, rate_limit_rpm = $17,
				queue_enabled = $18, queue_timeout = $19, key_load_balance = $20,
				api_keys = $21, model_mapping = $22, price_multipliers = $23,
				oauth_tokens = $24, quota_models = $25, composite_mappings = $26,
				content_filter = $27,
				updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = $28 AND channel_type = $29
		`
	} else {
		query = `
			UPDATE channels SET
				name = ?, description = ?, website = ?, service_type = ?,
				base_url = ?, insecure_skip_verify = ?, status = ?, priority = ?,
				promotion_until = ?, response_header_timeout = ?, quota_type = ?,
				quota_limit = ?, quota_reset_at = ?, quota_reset_interval = ?,
				quota_reset_unit = ?, quota_reset_mode = ?, rate_limit_rpm = ?,
				queue_enabled = ?, queue_timeout = ?, key_load_balance = ?,
				api_keys = ?, model_mapping = ?, price_multipliers = ?,
				oauth_tokens = ?, quota_models = ?, composite_mappings = ?,
				content_filter = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ? AND channel_type = ?
		`
	}

	_, err := tx.Exec(query,
		upstream.Name, upstream.Description, upstream.Website, upstream.ServiceType,
		upstream.BaseURL, upstream.InsecureSkipVerify, status, upstream.Priority,
		promotionUntil, upstream.ResponseHeaderTimeoutSecs, upstream.QuotaType,
		upstream.QuotaLimit, quotaResetAt, upstream.QuotaResetInterval,
		upstream.QuotaResetUnit, upstream.QuotaResetMode, upstream.RateLimitRpm,
		upstream.QueueEnabled, upstream.QueueTimeout, upstream.KeyLoadBalance,
		string(apiKeys), string(modelMapping), string(priceMultipliers),
		string(oauthTokens), string(quotaModels), string(compositeMappings), string(contentFilter),
		channelID, channelType,
	)
	return err
}

// StartPolling starts polling for configuration changes
func (s *DBConfigStorage) StartPolling() {
	s.pollWg.Add(1)
	go s.pollLoop()
	log.Printf("🔄 Started DB config polling (interval: %v)", s.pollInterval)
}

// StopPolling stops the polling loop
func (s *DBConfigStorage) StopPolling() {
	close(s.stopPoll)
	s.pollWg.Wait()
	log.Printf("🔄 Stopped DB config polling")
}

// pollLoop polls the database for configuration changes
func (s *DBConfigStorage) pollLoop() {
	defer s.pollWg.Done()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopPoll:
			return
		case <-ticker.C:
			s.checkForChanges()
		}
	}
}

// checkForChanges checks if the configuration has changed in the database
func (s *DBConfigStorage) checkForChanges() {
	// Get the max updated_at timestamp from channels and settings
	// Use dialect-appropriate timestamp extraction
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `
			SELECT COALESCE(MAX(ts), 0) FROM (
				SELECT MAX(EXTRACT(EPOCH FROM updated_at))::bigint as ts FROM channels
				UNION ALL
				SELECT MAX(EXTRACT(EPOCH FROM updated_at))::bigint as ts FROM settings WHERE category = 'config'
			) sub
		`
	} else {
		query = `
			SELECT COALESCE(MAX(ts), 0) FROM (
				SELECT MAX(strftime('%s', updated_at)) as ts FROM channels
				UNION ALL
				SELECT MAX(strftime('%s', updated_at)) as ts FROM settings WHERE category = 'config'
			)
		`
	}

	var version int64
	err := s.db.QueryRow(query).Scan(&version)

	if err != nil {
		log.Printf("⚠️ Failed to check config version: %v", err)
		return
	}

	if version > s.lastVersion {
		log.Printf("🔄 Configuration change detected (version: %d -> %d), reloading...", s.lastVersion, version)
		s.lastVersion = version

		if s.cm != nil {
			// Reload config from DB and update ConfigManager
			config, err := s.LoadConfigFromDB()
			if err != nil {
				log.Printf("⚠️ Failed to reload config from DB: %v", err)
				return
			}

			s.cm.mu.Lock()
			s.cm.config = *config
			// Re-index channels
			for i := range s.cm.config.Upstream {
				s.cm.config.Upstream[i].Index = i
			}
			for i := range s.cm.config.ResponsesUpstream {
				s.cm.config.ResponsesUpstream[i].Index = i
			}
			s.cm.mu.Unlock()

			log.Printf("✅ Configuration reloaded from database")
		}
	}
}

// GetDB returns the underlying database connection
func (s *DBConfigStorage) GetDB() database.DB {
	return s.db
}
