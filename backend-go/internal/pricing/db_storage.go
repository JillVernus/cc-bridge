package pricing

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// DBPricingStorage provides database-backed storage for pricing configuration
type DBPricingStorage struct {
	db           database.DB
	pollInterval time.Duration
	stopPoll     chan struct{}
	pollWg       sync.WaitGroup
	lastVersion  int64
	pm           *PricingManager
}

// NewDBPricingStorage creates a new database pricing storage adapter
func NewDBPricingStorage(db database.DB, pollInterval time.Duration) *DBPricingStorage {
	return &DBPricingStorage{
		db:           db,
		pollInterval: pollInterval,
		stopPoll:     make(chan struct{}),
	}
}

// SetPricingManager sets the PricingManager to sync with
func (s *DBPricingStorage) SetPricingManager(pm *PricingManager) {
	s.pm = pm
}

// MigrateFromJSONIfNeeded checks if JSON config exists and DB is empty, then migrates
func (s *DBPricingStorage) MigrateFromJSONIfNeeded(jsonPath string) error {
	// Check if model_pricing table is empty
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM model_pricing").Scan(&count); err != nil {
		return fmt.Errorf("failed to check model_pricing table: %w", err)
	}

	if count > 0 {
		log.Printf("📦 Database already has %d model prices, skipping JSON migration", count)
		return nil
	}

	// Check if JSON config exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		// No JSON file, insert default pricing
		log.Printf("📦 No pricing JSON found, inserting default pricing to database...")
		return s.SaveConfigToDB(getDefaultPricingConfig())
	}

	log.Printf("📦 Migrating pricing from %s to database...", jsonPath)

	// Read JSON config
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read pricing JSON: %w", err)
	}

	var config PricingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse pricing JSON: %w", err)
	}

	// Save to database
	if err := s.SaveConfigToDB(config); err != nil {
		return fmt.Errorf("failed to save pricing to database: %w", err)
	}

	// Save currency setting
	if config.Currency != "" {
		_, err := s.db.Exec(`
			INSERT INTO settings (key, value, category)
			VALUES ('pricing_currency', ?, 'pricing')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, config.Currency)
		if err != nil {
			log.Printf("⚠️ Failed to save pricing currency: %v", err)
		}
	}

	// Backup JSON file
	backupPath := jsonPath + ".migrated-" + time.Now().Format("20060102-150405")
	if err := os.Rename(jsonPath, backupPath); err != nil {
		log.Printf("⚠️ Failed to backup pricing JSON: %v", err)
	} else {
		log.Printf("✅ Pricing config migrated to database. Backup: %s", backupPath)
	}

	return nil
}

// LoadConfigFromDB loads pricing configuration from the database
func (s *DBPricingStorage) LoadConfigFromDB() (PricingConfig, error) {
	config := PricingConfig{
		Models:   make(map[string]ModelPricing),
		Currency: "USD",
	}

	// Load currency from settings
	var currency string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = 'pricing_currency'").Scan(&currency)
	if err == nil && currency != "" {
		config.Currency = currency
	}

	// Load model pricing
	rows, err := s.db.Query(`
		SELECT model_id, input_price, output_price, cache_creation_price,
			cache_read_price, description, export_to_models
		FROM model_pricing
	`)
	if err != nil {
		return config, fmt.Errorf("failed to query model_pricing: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			modelID                            string
			inputPrice, outputPrice            float64
			cacheCreationPrice, cacheReadPrice *float64
			description                        *string
			exportToModels                     *bool
		)

		err := rows.Scan(&modelID, &inputPrice, &outputPrice,
			&cacheCreationPrice, &cacheReadPrice, &description, &exportToModels)
		if err != nil {
			log.Printf("⚠️ Failed to scan model pricing: %v", err)
			continue
		}

		pricing := ModelPricing{
			InputPrice:         inputPrice,
			OutputPrice:        outputPrice,
			CacheCreationPrice: cacheCreationPrice,
			CacheReadPrice:     cacheReadPrice,
			ExportToModels:     exportToModels,
		}
		if description != nil {
			pricing.Description = *description
		}

		config.Models[modelID] = pricing
	}

	return config, nil
}

// SaveConfigToDB saves pricing configuration to the database
func (s *DBPricingStorage) SaveConfigToDB(config PricingConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing pricing
	if _, err := tx.Exec("DELETE FROM model_pricing"); err != nil {
		return fmt.Errorf("failed to clear model_pricing: %w", err)
	}

	// Insert all model prices
	for modelID, pricing := range config.Models {
		_, err := tx.Exec(`
			INSERT INTO model_pricing (model_id, input_price, output_price,
				cache_creation_price, cache_read_price, description, export_to_models)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, modelID, pricing.InputPrice, pricing.OutputPrice,
			pricing.CacheCreationPrice, pricing.CacheReadPrice,
			pricing.Description, pricing.ExportToModels)
		if err != nil {
			return fmt.Errorf("failed to insert model %s: %w", modelID, err)
		}
	}

	// Update currency setting
	_, err = tx.Exec(`
		INSERT INTO settings (key, value, category, updated_at)
		VALUES ('pricing_currency', ?, 'pricing', CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, config.Currency)
	if err != nil {
		return fmt.Errorf("failed to save currency setting: %w", err)
	}

	return tx.Commit()
}

// StartPolling starts polling for pricing changes
func (s *DBPricingStorage) StartPolling() {
	s.pollWg.Add(1)
	go s.pollLoop()
	log.Printf("🔄 Started DB pricing polling (interval: %v)", s.pollInterval)
}

// StopPolling stops the polling loop
func (s *DBPricingStorage) StopPolling() {
	close(s.stopPoll)
	s.pollWg.Wait()
	log.Printf("🔄 Stopped DB pricing polling")
}

// pollLoop polls the database for pricing changes
func (s *DBPricingStorage) pollLoop() {
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

// checkForChanges checks if pricing has changed in the database
func (s *DBPricingStorage) checkForChanges() {
	var query string
	if s.db.Dialect() == database.DialectPostgreSQL {
		query = `SELECT COALESCE(MAX(EXTRACT(EPOCH FROM updated_at))::bigint, 0) FROM model_pricing`
	} else {
		query = `SELECT COALESCE(MAX(strftime('%s', updated_at)), 0) FROM model_pricing`
	}

	var version int64
	err := s.db.QueryRow(query).Scan(&version)

	if err != nil {
		log.Printf("⚠️ Failed to check pricing version: %v", err)
		return
	}

	if version > s.lastVersion {
		log.Printf("🔄 Pricing change detected, reloading...")
		s.lastVersion = version

		if s.pm != nil {
			config, err := s.LoadConfigFromDB()
			if err != nil {
				log.Printf("⚠️ Failed to reload pricing from DB: %v", err)
				return
			}

			s.pm.mu.Lock()
			s.pm.config = config
			s.pm.mu.Unlock()

			log.Printf("✅ Pricing reloaded from database: %d models", len(config.Models))
		}
	}
}

// GetDB returns the underlying database connection
func (s *DBPricingStorage) GetDB() database.DB {
	return s.db
}
