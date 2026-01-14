package handlers

import (
	"sort"
	"time"

	"github.com/JillVernus/cc-bridge/internal/pricing"
	"github.com/gin-gonic/gin"
)

// OpenAI-compatible model object
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAI-compatible model list response
type ModelListResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// GetModels returns OpenAI-compatible model list from pricing config
func GetModels() gin.HandlerFunc {
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	return func(c *gin.Context) {
		pm := pricing.GetManager()
		if pm == nil {
			c.JSON(500, gin.H{"error": "Pricing manager not initialized"})
			return
		}

		exportableModels := pm.GetExportableModels()

		// Extract model IDs and sort them
		modelIDs := make([]string, 0, len(exportableModels))
		for modelID := range exportableModels {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)

		// Build response
		models := make([]Model, 0, len(modelIDs))
		for _, id := range modelIDs {
			models = append(models, Model{
				ID:      id,
				Object:  "model",
				Created: created,
				OwnedBy: "cc-bridge",
			})
		}

		c.JSON(200, ModelListResponse{
			Object: "list",
			Data:   models,
		})
	}
}
