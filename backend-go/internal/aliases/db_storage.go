package aliases

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// DBAliasesStorage provides database-backed storage for model aliases
type DBAliasesStorage struct {
	db           database.DB
	pollInterval time.Duration
	stopPoll     chan struct{}
	pollWg       sync.WaitGroup
	lastVersion  int64
	am           *AliasesManager
}

// NewDBAliasesStorage creates a new database aliases storage adapter
func NewDBAliasesStorage(db database.DB, pollInterval time.Duration) *DBAliasesStorage {
	return &DBAliasesStorage{
		db:           db,
		pollInterval: pollInterval,
		stopPoll:     make(chan struct{}),
	}
}

// SetAliasesManager sets the AliasesManager to sync with
func (s *DBAliasesStorage) SetAliasesManager(am *AliasesManager) {
	s.am = am
}

// MigrateFromJSONIfNeeded checks if JSON config exists and DB is empty, then migrates
func (s *DBAliasesStorage) MigrateFromJSONIfNeeded(jsonPath string) error {
	// Check if model_aliases table is empty
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM model_aliases").Scan(&count); err != nil {
		return fmt.Errorf("failed to check model_aliases table: %w", err)
	}

	if count > 0 {
		log.Printf("📦 Database already has %d model aliases, skipping JSON migration", count)
		return nil
	}

	// Check if JSON config exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		// No JSON file, insert default aliases
		log.Printf("📦 No aliases JSON found, inserting default aliases to database...")
		return s.SaveConfigToDB(GetDefaultAliasesConfig())
	}

	log.Printf("📦 Migrating aliases from %s to database...", jsonPath)

	// Read and parse JSON (reuse existing manager's loadConfig logic)
	am := &AliasesManager{configFile: jsonPath}
	if err := am.loadConfig(); err != nil {
		// If can't read, use defaults
		return s.SaveConfigToDB(GetDefaultAliasesConfig())
	}

	// Save to database
	if err := s.SaveConfigToDB(am.config); err != nil {
		return fmt.Errorf("failed to save aliases to database: %w", err)
	}

	// Backup JSON file
	backupPath := jsonPath + ".migrated-" + time.Now().Format("20060102-150405")
	if err := os.Rename(jsonPath, backupPath); err != nil {
		log.Printf("⚠️ Failed to backup aliases JSON: %v", err)
	} else {
		log.Printf("✅ Aliases config migrated to database. Backup: %s", backupPath)
	}

	return nil
}

// LoadConfigFromDB loads aliases configuration from the database
func (s *DBAliasesStorage) LoadConfigFromDB() (AliasesConfig, error) {
	config := AliasesConfig{
		MessagesModels:  []ModelAlias{},
		ResponsesModels: []ModelAlias{},
	}

	rows, err := s.db.Query(`
		SELECT alias_type, value, description, sort_order
		FROM model_aliases
		ORDER BY sort_order, id
	`)
	if err != nil {
		return config, fmt.Errorf("failed to query model_aliases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			aliasType   string
			value       string
			description *string
			sortOrder   int
		)

		err := rows.Scan(&aliasType, &value, &description, &sortOrder)
		if err != nil {
			log.Printf("⚠️ Failed to scan model alias: %v", err)
			continue
		}

		alias := ModelAlias{
			Value: value,
		}
		if description != nil {
			alias.Description = *description
		}

		switch aliasType {
		case "messages":
			config.MessagesModels = append(config.MessagesModels, alias)
		case "responses":
			config.ResponsesModels = append(config.ResponsesModels, alias)
		}
	}

	return config, nil
}

// SaveConfigToDB saves aliases configuration to the database
func (s *DBAliasesStorage) SaveConfigToDB(config AliasesConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing aliases
	if _, err := tx.Exec("DELETE FROM model_aliases"); err != nil {
		return fmt.Errorf("failed to clear model_aliases: %w", err)
	}

	// Insert Messages aliases
	for i, alias := range config.MessagesModels {
		_, err := tx.Exec(`
			INSERT INTO model_aliases (alias_type, value, description, sort_order)
			VALUES (?, ?, ?, ?)
		`, "messages", alias.Value, alias.Description, i)
		if err != nil {
			return fmt.Errorf("failed to insert messages alias %s: %w", alias.Value, err)
		}
	}

	// Insert Responses aliases
	for i, alias := range config.ResponsesModels {
		_, err := tx.Exec(`
			INSERT INTO model_aliases (alias_type, value, description, sort_order)
			VALUES (?, ?, ?, ?)
		`, "responses", alias.Value, alias.Description, i)
		if err != nil {
			return fmt.Errorf("failed to insert responses alias %s: %w", alias.Value, err)
		}
	}

	return tx.Commit()
}

// StartPolling starts polling for alias changes
func (s *DBAliasesStorage) StartPolling() {
	s.pollWg.Add(1)
	go s.pollLoop()
	log.Printf("🔄 Started DB aliases polling (interval: %v)", s.pollInterval)
}

// StopPolling stops the polling loop
func (s *DBAliasesStorage) StopPolling() {
	close(s.stopPoll)
	s.pollWg.Wait()
	log.Printf("🔄 Stopped DB aliases polling")
}

// pollLoop polls the database for alias changes
func (s *DBAliasesStorage) pollLoop() {
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

// checkForChanges checks if aliases have changed in the database
func (s *DBAliasesStorage) checkForChanges() {
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `SELECT COALESCE(MAX(EXTRACT(EPOCH FROM updated_at))::bigint, 0) FROM model_aliases`
	} else {
		query = `SELECT COALESCE(MAX(strftime('%s', updated_at)), 0) FROM model_aliases`
	}

	var version int64
	err := s.db.QueryRow(query).Scan(&version)

	if err != nil {
		log.Printf("⚠️ Failed to check aliases version: %v", err)
		return
	}

	if version > s.lastVersion {
		log.Printf("🔄 Aliases change detected, reloading...")
		s.lastVersion = version

		if s.am != nil {
			config, err := s.LoadConfigFromDB()
			if err != nil {
				log.Printf("⚠️ Failed to reload aliases from DB: %v", err)
				return
			}

			s.am.mu.Lock()
			s.am.config = config
			s.am.mu.Unlock()

			log.Printf("✅ Aliases reloaded from database: %d messages, %d responses",
				len(config.MessagesModels), len(config.ResponsesModels))
		}
	}
}

// GetDB returns the underlying database connection
func (s *DBAliasesStorage) GetDB() database.DB {
	return s.db
}
