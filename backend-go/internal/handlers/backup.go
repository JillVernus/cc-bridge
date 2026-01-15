package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/aliases"
	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/gin-gonic/gin"
)

// BackupData represents the combined backup of config and pricing
type BackupData struct {
	Version   string                 `json:"version"`
	CreatedAt string                 `json:"createdAt"`
	Config    *config.Config         `json:"config"`
	Pricing   *pricing.PricingConfig `json:"pricing,omitempty"`
	Aliases   *aliases.AliasesConfig `json:"aliases,omitempty"`
}

// BackupInfo represents metadata about a backup file
type BackupInfo struct {
	Filename  string `json:"filename"`
	CreatedAt string `json:"createdAt"`
	Size      int64  `json:"size"`
}

const (
	backupDirName   = ".config/backups"
	backupPrefix    = "manual_backup_"
	backupExtension = ".json"
)

// getBackupDir returns the backup directory path
func getBackupDir() string {
	return backupDirName
}

// CreateBackup creates a manual backup of config and pricing
func CreateBackup(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		backupDir := getBackupDir()

		// Ensure backup directory exists
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			c.JSON(500, gin.H{"error": "Failed to create backup directory: " + err.Error()})
			return
		}

		// Get current config
		cfg := cfgManager.GetConfig()

		// Get current pricing (may be nil if not initialized)
		var pricingConfig *pricing.PricingConfig
		pm := pricing.GetManager()
		if pm != nil {
			pc := pm.GetConfig()
			pricingConfig = &pc
		}

		// Get current aliases (may be nil if not initialized)
		var aliasesConfig *aliases.AliasesConfig
		am := aliases.GetManager()
		if am != nil {
			ac := am.GetConfig()
			aliasesConfig = &ac
		}

		// Create backup data
		timestamp := time.Now()
		backupData := BackupData{
			Version:   "1.0",
			CreatedAt: timestamp.Format(time.RFC3339),
			Config:    &cfg,
			Pricing:   pricingConfig,
			Aliases:   aliasesConfig,
		}

		// Marshal to JSON
		data, err := json.MarshalIndent(backupData, "", "  ")
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to serialize backup data: " + err.Error()})
			return
		}

		// Create backup filename with timestamp
		filename := fmt.Sprintf("%s%s%s", backupPrefix, timestamp.Format("2006-01-02_150405"), backupExtension)
		backupPath := filepath.Join(backupDir, filename)

		// Write backup file
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			c.JSON(500, gin.H{"error": "Failed to write backup file: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message":   "Backup created successfully",
			"filename":  filename,
			"createdAt": timestamp.Format(time.RFC3339),
			"size":      len(data),
		})
	}
}

// ListBackups returns a list of available backup files
func ListBackups() gin.HandlerFunc {
	return func(c *gin.Context) {
		backupDir := getBackupDir()

		// Check if backup directory exists
		if _, err := os.Stat(backupDir); os.IsNotExist(err) {
			c.JSON(200, gin.H{"backups": []BackupInfo{}})
			return
		}

		// Read directory
		entries, err := os.ReadDir(backupDir)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to read backup directory: " + err.Error()})
			return
		}

		var backups []BackupInfo
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			filename := entry.Name()
			// Only include manual backup files
			if !strings.HasPrefix(filename, backupPrefix) || !strings.HasSuffix(filename, backupExtension) {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			// Parse timestamp from filename
			// Format: manual_backup_2006-01-02_150405.json
			timestampStr := strings.TrimPrefix(filename, backupPrefix)
			timestampStr = strings.TrimSuffix(timestampStr, backupExtension)

			createdAt := ""
			if t, err := time.Parse("2006-01-02_150405", timestampStr); err == nil {
				createdAt = t.Format(time.RFC3339)
			} else {
				// Fallback to file modification time
				createdAt = info.ModTime().Format(time.RFC3339)
			}

			backups = append(backups, BackupInfo{
				Filename:  filename,
				CreatedAt: createdAt,
				Size:      info.Size(),
			})
		}

		// Sort by creation time (newest first)
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].CreatedAt > backups[j].CreatedAt
		})

		c.JSON(200, gin.H{"backups": backups})
	}
}

// RestoreBackup restores config and pricing from a backup file
func RestoreBackup(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := c.Param("filename")
		if filename == "" {
			c.JSON(400, gin.H{"error": "Filename is required"})
			return
		}

		// Validate filename to prevent directory traversal
		if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
			c.JSON(400, gin.H{"error": "Invalid filename"})
			return
		}

		// Only allow restoring manual backup files
		if !strings.HasPrefix(filename, backupPrefix) || !strings.HasSuffix(filename, backupExtension) {
			c.JSON(400, gin.H{"error": "Invalid backup file"})
			return
		}

		backupPath := filepath.Join(getBackupDir(), filename)

		// Check if file exists
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "Backup file not found"})
			return
		}

		// Read backup file
		file, err := os.Open(backupPath)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to open backup file: " + err.Error()})
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to read backup file: " + err.Error()})
			return
		}

		// Parse backup data
		var backupData BackupData
		if err := json.Unmarshal(data, &backupData); err != nil {
			c.JSON(500, gin.H{"error": "Failed to parse backup data: " + err.Error()})
			return
		}

		// Restore config
		if backupData.Config != nil {
			if err := cfgManager.RestoreConfig(*backupData.Config); err != nil {
				c.JSON(500, gin.H{"error": "Failed to restore config: " + err.Error()})
				return
			}
		}

		// Restore pricing
		if backupData.Pricing != nil {
			pm := pricing.GetManager()
			if pm != nil {
				if err := pm.UpdateConfig(*backupData.Pricing); err != nil {
					c.JSON(500, gin.H{"error": "Failed to restore pricing: " + err.Error()})
					return
				}
			}
		}

		// Restore aliases
		if backupData.Aliases != nil {
			am := aliases.GetManager()
			if am != nil {
				if err := am.UpdateConfig(*backupData.Aliases); err != nil {
					c.JSON(500, gin.H{"error": "Failed to restore aliases: " + err.Error()})
					return
				}
			}
		}

		c.JSON(200, gin.H{
			"message":    "Backup restored successfully",
			"filename":   filename,
			"restoredAt": time.Now().Format(time.RFC3339),
		})
	}
}

// DeleteBackup deletes a specific backup file
func DeleteBackup() gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := c.Param("filename")
		if filename == "" {
			c.JSON(400, gin.H{"error": "Filename is required"})
			return
		}

		// Validate filename to prevent directory traversal
		if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
			c.JSON(400, gin.H{"error": "Invalid filename"})
			return
		}

		// Only allow deleting manual backup files
		if !strings.HasPrefix(filename, backupPrefix) || !strings.HasSuffix(filename, backupExtension) {
			c.JSON(400, gin.H{"error": "Invalid backup file"})
			return
		}

		backupPath := filepath.Join(getBackupDir(), filename)

		// Check if file exists
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "Backup file not found"})
			return
		}

		// Delete the file
		if err := os.Remove(backupPath); err != nil {
			c.JSON(500, gin.H{"error": "Failed to delete backup file: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message":  "Backup deleted successfully",
			"filename": filename,
		})
	}
}
