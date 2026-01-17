package main

import (
	"log"
	"os"
	"time"

	"github.com/JillVernus/cc-bridge/internal/aliases"
	"github.com/JillVernus/cc-bridge/internal/apikey"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/database"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/JillVernus/cc-bridge/internal/quota"
	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

// DBStorageManager manages database-backed storage for all managers
type DBStorageManager struct {
	db             database.DB
	configStorage  *config.DBConfigStorage
	pricingStorage *pricing.DBPricingStorage
	aliasesStorage *aliases.DBAliasesStorage
	usageStorage   *quota.DBUsageStorage
	reqLogManager  *requestlog.Manager
	apiKeyManager  *apikey.Manager
}

// InitDBStorage initializes database storage if STORAGE_BACKEND=database
// Returns nil if JSON storage is used (default)
func InitDBStorage(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) *DBStorageManager {
	if envCfg.UseJSONStorage() {
		log.Printf("📁 Using JSON file storage (default)")
		return nil
	}

	log.Printf("🗄️ Using database storage (STORAGE_BACKEND=database)")

	// Create database configuration
	dbCfg := database.Config{
		Type: database.Dialect(envCfg.DatabaseType),
		URL:  envCfg.DatabaseURL,
	}

	// Default SQLite path if not specified
	if dbCfg.URL == "" && dbCfg.Type == database.DialectSQLite {
		dbCfg.URL = ".config/cc-bridge.db"
	}

	// Create database connection
	db, err := database.New(dbCfg)
	if err != nil {
		log.Printf("⚠️ Failed to initialize database storage: %v", err)
		log.Printf("📁 Falling back to JSON file storage")
		return nil
	}

	// Run migrations
	if err := database.RunMigrations(db); err != nil {
		log.Printf("⚠️ Failed to run database migrations: %v", err)
		db.Close()
		log.Printf("📁 Falling back to JSON file storage")
		return nil
	}

	// Ensure api_keys table has all required columns (for DBs migrated before columns were added)
	ensureAPIKeysColumns(db)

	// Calculate poll interval
	pollInterval := time.Duration(envCfg.ConfigPollInterval) * time.Second
	if pollInterval < time.Second {
		pollInterval = 5 * time.Second
	}

	mgr := &DBStorageManager{
		db:             db,
		configStorage:  config.NewDBConfigStorage(db, pollInterval),
		pricingStorage: pricing.NewDBPricingStorage(db, pollInterval),
		aliasesStorage: aliases.NewDBAliasesStorage(db, pollInterval),
		usageStorage:   quota.NewDBUsageStorage(db),
	}

	// Initialize RequestLogManager with shared database
	reqLogMgr, err := requestlog.NewManagerWithDB(db)
	if err != nil {
		log.Printf("⚠️ Failed to initialize request log manager with shared DB: %v", err)
	} else {
		mgr.reqLogManager = reqLogMgr
	}

	// Initialize APIKeyManager with shared database
	apiKeyMgr, err := apikey.NewManagerWithDB(db)
	if err != nil {
		log.Printf("⚠️ Failed to initialize API key manager with shared DB: %v", err)
	} else {
		mgr.apiKeyManager = apiKeyMgr
	}

	// Migrate from JSON files if needed
	if err := mgr.MigrateFromJSON(); err != nil {
		log.Printf("⚠️ JSON migration had errors: %v", err)
	}

	// Migrate from legacy request_logs.db if it exists
	legacyDBPath := ".config/request_logs.db"
	if _, err := os.Stat(legacyDBPath); err == nil {
		if err := requestlog.MigrateFromLegacyDB(legacyDBPath, db); err != nil {
			log.Printf("⚠️ Legacy database migration had errors: %v", err)
		}
	}

	// Link to managers
	mgr.configStorage.SetConfigManager(cfgManager)
	if pricingMgr := pricing.GetManager(); pricingMgr != nil {
		mgr.pricingStorage.SetPricingManager(pricingMgr)
	}
	if aliasesMgr := aliases.GetManager(); aliasesMgr != nil {
		mgr.aliasesStorage.SetAliasesManager(aliasesMgr)
	}

	// Start polling for changes
	mgr.StartPolling()

	log.Printf("✅ Database storage initialized (poll interval: %v)", pollInterval)
	return mgr
}

// MigrateFromJSON migrates all JSON config files to the database
func (m *DBStorageManager) MigrateFromJSON() error {
	var lastErr error

	// Migrate config.json
	if err := m.configStorage.MigrateFromJSONIfNeeded(".config/config.json"); err != nil {
		log.Printf("⚠️ Config migration error: %v", err)
		lastErr = err
	}

	// Migrate pricing.json
	if err := m.pricingStorage.MigrateFromJSONIfNeeded(".config/pricing.json"); err != nil {
		log.Printf("⚠️ Pricing migration error: %v", err)
		lastErr = err
	}

	// Migrate model-aliases.json
	if err := m.aliasesStorage.MigrateFromJSONIfNeeded(".config/model-aliases.json"); err != nil {
		log.Printf("⚠️ Aliases migration error: %v", err)
		lastErr = err
	}

	// Migrate quota_usage.json
	if err := m.usageStorage.MigrateFromJSONIfNeeded(".config/quota_usage.json"); err != nil {
		log.Printf("⚠️ Usage migration error: %v", err)
		lastErr = err
	}

	return lastErr
}

// StartPolling starts polling for all config changes
func (m *DBStorageManager) StartPolling() {
	m.configStorage.StartPolling()
	m.pricingStorage.StartPolling()
	m.aliasesStorage.StartPolling()
	// Usage storage doesn't need polling (direct read/write)
}

// StopPolling stops all polling
func (m *DBStorageManager) StopPolling() {
	m.configStorage.StopPolling()
	m.pricingStorage.StopPolling()
	m.aliasesStorage.StopPolling()
}

// Close closes the database connection
func (m *DBStorageManager) Close() error {
	m.StopPolling()
	return m.db.Close()
}

// GetDB returns the underlying database connection
func (m *DBStorageManager) GetDB() database.DB {
	return m.db
}

// GetUsageStorage returns the usage storage adapter
func (m *DBStorageManager) GetUsageStorage() *quota.DBUsageStorage {
	return m.usageStorage
}

// GetRequestLogManager returns the request log manager using the shared database
// Returns nil if the manager failed to initialize
func (m *DBStorageManager) GetRequestLogManager() *requestlog.Manager {
	return m.reqLogManager
}

// GetAPIKeyManager returns the API key manager using the shared database
// Returns nil if the manager failed to initialize
func (m *DBStorageManager) GetAPIKeyManager() *apikey.Manager {
	return m.apiKeyManager
}

// ensureAPIKeysColumns adds missing columns to api_keys table
// This handles databases that were migrated before all columns were added
func ensureAPIKeysColumns(db database.DB) {
	columns := []struct {
		name       string
		definition string
	}{
		{"allowed_endpoints", "TEXT"},
		{"allowed_channels_msg", "TEXT"},
		{"allowed_channels_resp", "TEXT"},
		{"allowed_models", "TEXT"},
	}

	for _, col := range columns {
		// Check if column exists
		exists, err := database.ColumnExists(db, "api_keys", col.name)
		if err != nil {
			log.Printf("⚠️ Failed to check column %s: %v", col.name, err)
			continue
		}
		if exists {
			continue
		}

		// Add column
		query := "ALTER TABLE api_keys ADD COLUMN " + col.name + " " + col.definition
		if _, err := db.Exec(query); err != nil {
			log.Printf("⚠️ Failed to add column %s to api_keys: %v", col.name, err)
		} else {
			log.Printf("✅ Added column %s to api_keys table", col.name)
		}
	}
}
