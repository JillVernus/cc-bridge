package handlers

import (
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/gin-gonic/gin"
)

// applyMessagesUserAgentPolicy applies UA capture/fallback for /v1/messages direct passthrough channels.
func applyMessagesUserAgentPolicy(c *gin.Context, cfgManager *config.ConfigManager, upstream *config.UpstreamConfig, req *http.Request) {
	if c == nil || cfgManager == nil || upstream == nil || req == nil {
		return
	}
	if upstream.ServiceType != "claude" {
		return
	}
	incoming := c.GetHeader("User-Agent")
	resolved := cfgManager.ResolveMessagesUserAgent(incoming)
	if incoming != "" && incoming != resolved {
		RecordDebugModifiedRequestHeader(c, "User-Agent", resolved)
	}
	req.Header.Set("User-Agent", resolved)
}

// applyResponsesUserAgentPolicy applies UA capture/fallback for /v1/responses direct passthrough channels.
func applyResponsesUserAgentPolicy(c *gin.Context, cfgManager *config.ConfigManager, upstream *config.UpstreamConfig, req *http.Request) {
	if c == nil || cfgManager == nil || upstream == nil || req == nil {
		return
	}
	if upstream.ServiceType != "responses" {
		return
	}
	incoming := c.GetHeader("User-Agent")
	resolved := cfgManager.ResolveResponsesUserAgent(incoming)
	if incoming != "" && incoming != resolved {
		RecordDebugModifiedRequestHeader(c, "User-Agent", resolved)
	}
	req.Header.Set("User-Agent", resolved)
}

func resolveResponsesUserAgentForOAuth(c *gin.Context, cfgManager *config.ConfigManager) string {
	if c == nil || cfgManager == nil {
		return config.DefaultResponsesUserAgent
	}
	incoming := c.GetHeader("User-Agent")
	resolved := cfgManager.ResolveResponsesUserAgent(incoming)
	if incoming != "" && incoming != resolved {
		RecordDebugModifiedRequestHeader(c, "User-Agent", resolved)
	}
	return resolved
}
