package aliases

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ModelAlias represents a single model alias option
type ModelAlias struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// AliasesConfig holds the model aliases configuration
type AliasesConfig struct {
	MessagesModels  []ModelAlias `json:"messagesModels"`
	ResponsesModels []ModelAlias `json:"responsesModels"`
	GeminiModels    []ModelAlias `json:"geminiModels"`
}

// AliasesManager manages model aliases configuration
type AliasesManager struct {
	mu         sync.RWMutex
	config     AliasesConfig
	configFile string
	watcher    *fsnotify.Watcher
	dbStorage  *DBAliasesStorage // Database storage adapter (nil if using JSON-only mode)
}

var (
	globalManager *AliasesManager
	once          sync.Once
)

// GetManager returns the global aliases manager
func GetManager() *AliasesManager {
	return globalManager
}

// InitManager initializes the global aliases manager
func InitManager(configFile string) (*AliasesManager, error) {
	var initErr error
	once.Do(func() {
		am := &AliasesManager{
			configFile: configFile,
		}

		if err := am.loadConfig(); err != nil {
			log.Printf("⚠️ Model aliases config not found, creating default: %v", err)
			am.config = GetDefaultAliasesConfig()
			// Auto-create the default config file
			if err := am.saveDefaultConfig(); err != nil {
				log.Printf("⚠️ Failed to create default model aliases config: %v", err)
			} else {
				log.Printf("✅ Default model aliases config created at %s", configFile)
			}
		}

		if err := am.startWatcher(); err != nil {
			log.Printf("⚠️ Failed to start model aliases config watcher: %v", err)
		}

		globalManager = am
	})
	return globalManager, initErr
}

// saveDefaultConfig saves the default configuration to file
func (am *AliasesManager) saveDefaultConfig() error {
	// Ensure directory exists
	dir := filepath.Dir(am.configFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(am.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(am.configFile, data, 0644)
}

// loadConfig loads configuration from file
func (am *AliasesManager) loadConfig() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	data, err := os.ReadFile(am.configFile)
	if err != nil {
		return err
	}

	var config AliasesConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// Backwards compatibility: older configs won't have geminiModels
	if config.MessagesModels == nil {
		config.MessagesModels = []ModelAlias{}
	}
	if config.ResponsesModels == nil {
		config.ResponsesModels = []ModelAlias{}
	}
	if config.GeminiModels == nil {
		config.GeminiModels = []ModelAlias{}
	}

	am.config = config
	log.Printf("✅ Model aliases config loaded: %d messages models, %d responses models, %d gemini models",
		len(config.MessagesModels), len(config.ResponsesModels), len(config.GeminiModels))
	return nil
}

// startWatcher starts file system watcher for hot-reload
func (am *AliasesManager) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	am.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("📝 Model aliases config updated, reloading...")
					if err := am.loadConfig(); err != nil {
						log.Printf("⚠️ Failed to reload model aliases config: %v", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("⚠️ Model aliases config watcher error: %v", err)
			}
		}
	}()

	return watcher.Add(am.configFile)
}

// GetConfig returns the current configuration
func (am *AliasesManager) GetConfig() AliasesConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.config
}

// SetDBStorage sets the database storage adapter for write-through caching
func (am *AliasesManager) SetDBStorage(dbStorage *DBAliasesStorage) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.dbStorage = dbStorage
	// Disable file watcher when using database storage (polling handles sync)
	if am.watcher != nil {
		am.watcher.Close()
		am.watcher = nil
	}
}

// UpdateConfigFromDB replaces the in-memory config with data loaded from the database.
// This is called during startup to ensure the manager has the latest DB state.
func (am *AliasesManager) UpdateConfigFromDB(config AliasesConfig) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.config = config
}

// UpdateConfig updates the configuration
func (am *AliasesManager) UpdateConfig(config AliasesConfig) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	// When using database storage, write to DB instead of JSON file
	if am.dbStorage != nil {
		am.config = config

		// Write to database asynchronously (non-blocking)
		configCopy := config
		go func() {
			if err := am.dbStorage.SaveConfigToDB(configCopy); err != nil {
				log.Printf("⚠️ Failed to sync aliases to database: %v", err)
			}
		}()

		return nil
	}

	// JSON-only mode: write to file
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(am.configFile, data, 0644); err != nil {
		return err
	}

	am.config = config
	return nil
}

// Close closes the manager
func (am *AliasesManager) Close() error {
	if am.watcher != nil {
		return am.watcher.Close()
	}
	return nil
}

// GetDefaultAliasesConfig returns the default aliases configuration
func GetDefaultAliasesConfig() AliasesConfig {
	return AliasesConfig{
		MessagesModels: []ModelAlias{
			{Value: "opus", Description: "Claude Opus"},
			{Value: "sonnet", Description: "Claude Sonnet"},
			{Value: "haiku", Description: "Claude Haiku"},
		},
		ResponsesModels: []ModelAlias{
			{Value: "codex", Description: "Codex"},
			{Value: "gpt-5.1-codex-max", Description: "GPT-5.1 Codex Max"},
			{Value: "gpt-5.1-codex", Description: "GPT-5.1 Codex"},
			{Value: "gpt-5.1-codex-mini", Description: "GPT-5.1 Codex Mini"},
			{Value: "gpt-5.1", Description: "GPT-5.1"},
		},
		GeminiModels: []ModelAlias{
			{Value: "gemini-3-flash-preview", Description: "Gemini 3 Flash Preview"},
			{Value: "gemini-3-flash", Description: "Gemini 3 Flash"},
			{Value: "gemini-3-pro-preview", Description: "Gemini 3 Pro Preview"},
			{Value: "gemini-3-pro", Description: "Gemini 3 Pro"},
		},
	}
}
