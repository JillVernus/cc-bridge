package quota

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// DBUsageStorage provides database-backed storage for channel usage
type DBUsageStorage struct {
	db database.DB
}

// NewDBUsageStorage creates a new database usage storage adapter
func NewDBUsageStorage(db database.DB) *DBUsageStorage {
	return &DBUsageStorage{db: db}
}

// MigrateFromJSONIfNeeded checks if JSON file exists and DB is empty, then migrates
func (s *DBUsageStorage) MigrateFromJSONIfNeeded(jsonPath string) error {
	// Check if channel_usage table is empty
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM channel_usage").Scan(&count); err != nil {
		return fmt.Errorf("failed to check channel_usage table: %w", err)
	}

	if count > 0 {
		log.Printf("📦 Database already has %d channel usage records, skipping JSON migration", count)
		return nil
	}

	// Check if JSON file exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		log.Printf("📦 No quota usage JSON found, starting fresh")
		return nil
	}

	log.Printf("📦 Migrating quota usage from %s to database...", jsonPath)

	// Read JSON file
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read quota usage JSON: %w", err)
	}

	var usage UsageFile
	if err := json.Unmarshal(data, &usage); err != nil {
		return fmt.Errorf("failed to parse quota usage JSON: %w", err)
	}

	// Migrate to database
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Migrate Messages usage
	for key, u := range usage.Messages {
		channelID, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		var lastResetAt *string
		if !u.LastResetAt.IsZero() {
			t := u.LastResetAt.Format(time.RFC3339)
			lastResetAt = &t
		}
		_, err = tx.Exec(`
			INSERT INTO channel_usage (channel_id, channel_type, used, last_reset_at)
			VALUES (?, 'messages', ?, ?)
		`, channelID, u.Used, lastResetAt)
		if err != nil {
			log.Printf("⚠️ Failed to migrate messages usage %d: %v", channelID, err)
		}
	}

	// Migrate Responses usage
	for key, u := range usage.Responses {
		channelID, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		var lastResetAt *string
		if !u.LastResetAt.IsZero() {
			t := u.LastResetAt.Format(time.RFC3339)
			lastResetAt = &t
		}
		_, err = tx.Exec(`
			INSERT INTO channel_usage (channel_id, channel_type, used, last_reset_at)
			VALUES (?, 'responses', ?, ?)
		`, channelID, u.Used, lastResetAt)
		if err != nil {
			log.Printf("⚠️ Failed to migrate responses usage %d: %v", channelID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	// Backup JSON file
	backupPath := jsonPath + ".migrated-" + time.Now().Format("20060102-150405")
	if err := os.Rename(jsonPath, backupPath); err != nil {
		log.Printf("⚠️ Failed to backup quota usage JSON: %v", err)
	} else {
		log.Printf("✅ Quota usage migrated to database. Backup: %s", backupPath)
	}

	return nil
}

// GetUsage returns the current usage for a channel
func (s *DBUsageStorage) GetUsage(channelID int, channelType string) *ChannelUsage {
	var used float64
	var lastResetAt *string

	err := s.db.QueryRow(`
		SELECT used, last_reset_at FROM channel_usage
		WHERE channel_id = ? AND channel_type = ?
	`, channelID, channelType).Scan(&used, &lastResetAt)

	if err != nil {
		return nil
	}

	usage := &ChannelUsage{Used: used}
	if lastResetAt != nil && *lastResetAt != "" {
		if t, err := time.Parse(time.RFC3339, *lastResetAt); err == nil {
			usage.LastResetAt = t
		}
	}

	return usage
}

// IncrementUsage increments the usage for a channel
func (s *DBUsageStorage) IncrementUsage(channelID int, channelType string, amount float64) error {
	// Try to update existing record
	result, err := s.db.Exec(`
		UPDATE channel_usage
		SET used = used + ?, updated_at = CURRENT_TIMESTAMP
		WHERE channel_id = ? AND channel_type = ?
	`, amount, channelID, channelType)

	if err != nil {
		return fmt.Errorf("failed to update channel usage: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		// Insert new record
		_, err = s.db.Exec(`
			INSERT INTO channel_usage (channel_id, channel_type, used, last_reset_at)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		`, channelID, channelType, amount)
		if err != nil {
			return fmt.Errorf("failed to insert channel usage: %w", err)
		}
	}

	return nil
}

// ResetUsage resets the usage for a channel
func (s *DBUsageStorage) ResetUsage(channelID int, channelType string) error {
	_, err := s.db.Exec(`
		UPDATE channel_usage
		SET used = 0, last_reset_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE channel_id = ? AND channel_type = ?
	`, channelID, channelType)

	if err != nil {
		return fmt.Errorf("failed to reset channel usage: %w", err)
	}

	log.Printf("🔄 Channel [%d] (%s) quota reset", channelID, channelType)
	return nil
}

// LoadAll loads all usage data from the database
func (s *DBUsageStorage) LoadAll() (UsageFile, error) {
	usage := UsageFile{
		Messages:  make(map[string]ChannelUsage),
		Responses: make(map[string]ChannelUsage),
	}

	rows, err := s.db.Query(`
		SELECT channel_id, channel_type, used, last_reset_at
		FROM channel_usage
	`)
	if err != nil {
		return usage, fmt.Errorf("failed to query channel_usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			channelID   int
			channelType string
			used        float64
			lastResetAt *string
		)

		err := rows.Scan(&channelID, &channelType, &used, &lastResetAt)
		if err != nil {
			log.Printf("⚠️ Failed to scan channel usage: %v", err)
			continue
		}

		u := ChannelUsage{Used: used}
		if lastResetAt != nil && *lastResetAt != "" {
			if t, err := time.Parse(time.RFC3339, *lastResetAt); err == nil {
				u.LastResetAt = t
			}
		}

		key := strconv.Itoa(channelID)
		switch channelType {
		case "messages":
			usage.Messages[key] = u
		case "responses":
			usage.Responses[key] = u
		}
	}

	return usage, nil
}

// GetDB returns the underlying database connection
func (s *DBUsageStorage) GetDB() database.DB {
	return s.db
}
