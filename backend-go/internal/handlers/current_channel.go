package handlers

import (
	"fmt"
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// GetCurrentMessagesChannel returns the current Claude(Messages) channel name.
// - Normal channel: returns its own name
// - Composite channel: returns the mapped target channel name for model pattern "opus"
//
// Note: We intentionally skip the multi-channel mode check here.
// Composite channels list their targets as separate upstream entries, which makes
// IsMultiChannelMode return true even when there is logically one primary channel.
// GetCurrentUpstream returns the first active (primary) channel, which is the
// correct answer for both single-channel and composite setups.
func GetCurrentMessagesChannel(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
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
		resolved, found := config.ResolveCompositeMappingWithPools(upstream, "opus", cfg.Upstream, cfg.ResponsesUpstream)
		if !found {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": fmt.Sprintf("Composite channel '%s' has no valid opus mapping target", upstream.Name),
				"code":  "NO_OPUS_MAPPING_TARGET",
			})
			return
		}

		var targetChannels []config.UpstreamConfig
		switch resolved.TargetPool {
		case config.CompositeTargetPoolResponses:
			targetChannels = cfg.ResponsesUpstream
		default:
			targetChannels = cfg.Upstream
		}
		if resolved.TargetIndex < 0 || resolved.TargetIndex >= len(targetChannels) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": fmt.Sprintf("Composite channel '%s' has no valid opus mapping target", upstream.Name),
				"code":  "NO_OPUS_MAPPING_TARGET",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"channelName": targetChannels[resolved.TargetIndex].Name,
		})
	}
}
