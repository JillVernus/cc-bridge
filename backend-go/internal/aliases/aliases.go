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
}

// AliasesManager manages model aliases configuration
type AliasesManager struct {
	mu         sync.RWMutex
	config     AliasesConfig
	configFile string
	watcher    *fsnotify.Watcher
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

	am.config = config
	log.Printf("✅ Model aliases config loaded: %d messages models, %d responses models",
		len(config.MessagesModels), len(config.ResponsesModels))
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

// UpdateConfig updates the configuration
func (am *AliasesManager) UpdateConfig(config AliasesConfig) error {
	am.mu.Lock()
	defer am.mu.Unlock()

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
	}
}
