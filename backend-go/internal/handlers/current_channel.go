package handlers

import (
	"fmt"
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/scheduler"
	"github.com/gin-gonic/gin"
)

// GetCurrentMessagesChannel returns the current Claude(Messages) channel name.
// - Normal channel: returns its own name
// - Composite channel: returns the mapped target channel name for model pattern "opus"
func GetCurrentMessagesChannel(cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		// In multi-channel mode, channel selection is dynamic per request.
		// No single fixed "current" channel exists.
		if sch != nil && sch.IsMultiChannelMode(false) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Current channel is dynamic in multi-channel mode",
				"code":  "MULTI_CHANNEL_MODE",
			})
			return
		}

		upstream, err := cfgManager.GetCurrentUpstream()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "No channels configured. Please add a channel in the admin UI.",
				"code":  "NO_UPSTREAM",
			})
			return
		}

		// Normal channel: return current channel name directly.
		if !config.IsCompositeChannel(upstream) {
			c.JSON(http.StatusOK, gin.H{
				"channelName": upstream.Name,
			})
			return
		}

		// Composite channel: resolve the channel assigned for opus.
		cfg := cfgManager.GetConfig()
		_, targetIndex, _, found := config.ResolveCompositeMapping(upstream, "opus", cfg.Upstream)
		if !found || targetIndex < 0 || targetIndex >= len(cfg.Upstream) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": fmt.Sprintf("Composite channel '%s' has no valid opus mapping target", upstream.Name),
				"code":  "NO_OPUS_MAPPING_TARGET",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"channelName": cfg.Upstream[targetIndex].Name,
		})
	}
}
