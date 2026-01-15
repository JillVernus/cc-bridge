package handlers

import (
	"github.com/JillVernus/cc-bridge/internal/aliases"
	"github.com/gin-gonic/gin"
)

// GetAliases returns the current model aliases configuration
func GetAliases() gin.HandlerFunc {
	return func(c *gin.Context) {
		am := aliases.GetManager()
		if am == nil {
			c.JSON(500, gin.H{"error": "Aliases manager not initialized"})
			return
		}

		config := am.GetConfig()
		c.JSON(200, config)
	}
}

// UpdateAliases updates the entire model aliases configuration
func UpdateAliases() gin.HandlerFunc {
	return func(c *gin.Context) {
		am := aliases.GetManager()
		if am == nil {
			c.JSON(500, gin.H{"error": "Aliases manager not initialized"})
			return
		}

		var config aliases.AliasesConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		if err := am.UpdateConfig(config); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save aliases config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "Model aliases updated",
			"config":  config,
		})
	}
}

// ResetAliasesToDefault resets model aliases to default values
func ResetAliasesToDefault() gin.HandlerFunc {
	return func(c *gin.Context) {
		am := aliases.GetManager()
		if am == nil {
			c.JSON(500, gin.H{"error": "Aliases manager not initialized"})
			return
		}

		defaultConfig := aliases.GetDefaultAliasesConfig()
		if err := am.UpdateConfig(defaultConfig); err != nil {
			c.JSON(500, gin.H{"error": "Failed to reset aliases config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "Model aliases reset to defaults",
			"config":  defaultConfig,
		})
	}
}
