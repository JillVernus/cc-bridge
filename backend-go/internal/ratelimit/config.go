package ratelimit

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ConfigManager manages rate limit configuration with hot-reload support
type ConfigManager struct {
	mu         sync.RWMutex
	config     RateLimitConfig
	configFile string
	watcher    *fsnotify.Watcher
	onChange   func(RateLimitConfig) // callback when config changes
}

var (
	globalManager *ConfigManager
	once          sync.Once
)

// GetManager returns the global rate limit config manager
func GetManager() *ConfigManager {
	return globalManager
}

// InitManager initializes the global rate limit config manager
func InitManager(configFile string) (*ConfigManager, error) {
	var initErr error
	once.Do(func() {
		cm := &ConfigManager{
			configFile: configFile,
		}

		if err := cm.loadConfig(); err != nil {
			log.Printf("⚠️ Rate limit config file not found, using defaults: %v", err)
			cm.config = cloneRateLimitConfig(GetDefaultConfig())
			// Save default config to file
			if err := cm.saveConfig(); err != nil {
				log.Printf("⚠️ Failed to save default rate limit config: %v", err)
			}
		}

		// Start file watcher
		if err := cm.startWatcher(); err != nil {
			log.Printf("⚠️ Failed to start rate limit config watcher: %v", err)
		}

		globalManager = cm
	})
	return globalManager, initErr
}

// loadConfig loads configuration from file
func (cm *ConfigManager) loadConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		return err
	}

	var config RateLimitConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	if err := validateRateLimitConfig(config); err != nil {
		return err
	}

	cm.config = cloneRateLimitConfig(config)
	log.Printf("✅ Rate limit config loaded: API=%d rpm, Portal=%d rpm",
		config.API.RequestsPerMinute, config.Portal.RequestsPerMinute)
	return nil
}

// saveConfig saves configuration to file
func (cm *ConfigManager) saveConfig() error {
	// Ensure directory exists
	dir := filepath.Dir(cm.configFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	cm.mu.RLock()
	cfg := cloneRateLimitConfig(cm.config)
	cm.mu.RUnlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.configFile, data, 0644)
}

// startWatcher starts file change monitoring
func (cm *ConfigManager) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	cm.watcher = watcher

	configBase := filepath.Base(cm.configFile)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// We watch the directory; ignore unrelated files.
				if filepath.Base(event.Name) != configBase {
					continue
				}

				// Many editors update files via atomic rename/create, not only Write.
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					log.Printf("📝 Rate limit config file updated, reloading...")
					if err := cm.loadConfig(); err != nil {
						log.Printf("⚠️ Failed to reload rate limit config: %v", err)
						continue
					}

					cm.mu.RLock()
					cfg := cloneRateLimitConfig(cm.config)
					cb := cm.onChange
					cm.mu.RUnlock()

					if cb != nil {
						cb(cfg)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("⚠️ Rate limit config watcher error: %v", err)
			}
		}
	}()

	// Watch the config file's directory to handle file creation
	dir := filepath.Dir(cm.configFile)
	if err := watcher.Add(dir); err != nil {
		// Try watching the file directly if directory watch fails
		return watcher.Add(cm.configFile)
	}
	return nil
}

// SetOnChangeCallback sets a callback function to be called when config changes
func (cm *ConfigManager) SetOnChangeCallback(callback func(RateLimitConfig)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onChange = callback
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() RateLimitConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cloneRateLimitConfig(cm.config)
}

// GetAPIConfig returns the API rate limit configuration
func (cm *ConfigManager) GetAPIConfig() EndpointRateLimit {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.API
}

// GetPortalConfig returns the Portal rate limit configuration
func (cm *ConfigManager) GetPortalConfig() EndpointRateLimit {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.Portal
}

// GetAuthFailureConfig returns the auth failure rate limit configuration
func (cm *ConfigManager) GetAuthFailureConfig() AuthFailureConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cfg := cm.config.AuthFailure
	cfg.Thresholds = cloneThresholds(cfg.Thresholds)
	return cfg
}

// UpdateConfig updates the configuration and saves to file
func (cm *ConfigManager) UpdateConfig(config RateLimitConfig) error {
	if err := validateRateLimitConfig(config); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(cm.configFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(cm.configFile, data, 0644); err != nil {
		return err
	}

	cm.mu.Lock()
	cm.config = cloneRateLimitConfig(config)
	cb := cm.onChange
	cfg := cloneRateLimitConfig(cm.config)
	cm.mu.Unlock()

	log.Printf("✅ Rate limit config updated: API=%d rpm, Portal=%d rpm",
		config.API.RequestsPerMinute, config.Portal.RequestsPerMinute)

	// Trigger callback
	if cb != nil {
		cb(cfg)
	}

	return nil
}

// Close closes the config manager and stops the file watcher
func (cm *ConfigManager) Close() error {
	if cm.watcher != nil {
		return cm.watcher.Close()
	}
	return nil
}

func validateRateLimitConfig(config RateLimitConfig) error {
	const maxRPM = 10000         // Max 10,000 requests per minute
	const maxBlockMinutes = 1440 // Max 24 hours (1440 minutes)

	if config.API.RequestsPerMinute < 0 {
		return fmt.Errorf("api.requestsPerMinute must be non-negative")
	}
	if config.API.RequestsPerMinute > maxRPM {
		return fmt.Errorf("api.requestsPerMinute cannot exceed %d", maxRPM)
	}

	if config.Portal.RequestsPerMinute < 0 {
		return fmt.Errorf("portal.requestsPerMinute must be non-negative")
	}
	if config.Portal.RequestsPerMinute > maxRPM {
		return fmt.Errorf("portal.requestsPerMinute cannot exceed %d", maxRPM)
	}

	// Validate auth failure thresholds
	for i, threshold := range config.AuthFailure.Thresholds {
		if threshold.Failures <= 0 {
			return fmt.Errorf("authFailure.thresholds[%d].failures must be positive", i)
		}
		if threshold.BlockMinutes <= 0 {
			return fmt.Errorf("authFailure.thresholds[%d].blockMinutes must be positive", i)
		}
		if threshold.BlockMinutes > maxBlockMinutes {
			return fmt.Errorf("authFailure.thresholds[%d].blockMinutes cannot exceed %d", i, maxBlockMinutes)
		}
		if i > 0 && threshold.Failures <= config.AuthFailure.Thresholds[i-1].Failures {
			return fmt.Errorf("authFailure.thresholds must be in ascending order by failures")
		}
	}

	return nil
}

func cloneRateLimitConfig(cfg RateLimitConfig) RateLimitConfig {
	cfg.AuthFailure.Thresholds = cloneThresholds(cfg.AuthFailure.Thresholds)
	return cfg
}

func cloneThresholds(thresholds []AuthFailureThreshold) []AuthFailureThreshold {
	if thresholds == nil {
		return nil
	}
	dst := make([]AuthFailureThreshold, len(thresholds))
	copy(dst, thresholds)
	return dst
}
