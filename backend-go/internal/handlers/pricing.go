package handlers

import (
	"github.com/JillVernus/claude-proxy/internal/pricing"
	"github.com/gin-gonic/gin"
)

// GetPricing 获取定价配置
func GetPricing() gin.HandlerFunc {
	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		config := pm.GetConfig()
		c.JSON(200, config)
	}
}

// UpdatePricing 更新定价配置
func UpdatePricing() gin.HandlerFunc {
	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		var config pricing.PricingConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		// 验证配置
		if config.Models == nil {
			config.Models = make(map[string]pricing.ModelPricing)
		}
		if config.Currency == "" {
			config.Currency = "USD"
		}

		if err := pm.UpdateConfig(config); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save pricing config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "定价配置已更新",
			"config":  config,
		})
	}
}

// AddModelPricing 添加或更新单个模型的定价
func AddModelPricing() gin.HandlerFunc {
	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		modelName := c.Param("model")
		if modelName == "" {
			c.JSON(400, gin.H{"error": "Model name is required"})
			return
		}

		var modelPricing pricing.ModelPricing
		if err := c.ShouldBindJSON(&modelPricing); err != nil {
			c.JSON(400, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		config := pm.GetConfig()
		if config.Models == nil {
			config.Models = make(map[string]pricing.ModelPricing)
		}
		config.Models[modelName] = modelPricing

		if err := pm.UpdateConfig(config); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save pricing config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "模型定价已更新",
			"model":   modelName,
			"pricing": modelPricing,
		})
	}
}

// DeleteModelPricing 删除单个模型的定价
func DeleteModelPricing() gin.HandlerFunc {
	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		modelName := c.Param("model")
		if modelName == "" {
			c.JSON(400, gin.H{"error": "Model name is required"})
			return
		}

		config := pm.GetConfig()
		if config.Models == nil {
			c.JSON(404, gin.H{"error": "Model not found"})
			return
		}

		if _, exists := config.Models[modelName]; !exists {
			c.JSON(404, gin.H{"error": "Model not found"})
			return
		}

		delete(config.Models, modelName)

		if err := pm.UpdateConfig(config); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save pricing config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "模型定价已删除",
			"model":   modelName,
		})
	}
}

// ResetPricingToDefault 重置定价配置为默认值
func ResetPricingToDefault() gin.HandlerFunc {
	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		defaultConfig := pricing.GetDefaultPricingConfig()
		if err := pm.UpdateConfig(defaultConfig); err != nil {
			c.JSON(500, gin.H{"error": "Failed to reset pricing config: " + err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "定价配置已重置为默认值",
			"config":  defaultConfig,
		})
	}
}
