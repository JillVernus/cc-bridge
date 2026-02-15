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
	req.Header.Set("User-Agent", cfgManager.ResolveMessagesUserAgent(c.GetHeader("User-Agent")))
}

// applyResponsesUserAgentPolicy applies UA capture/fallback for /v1/responses direct passthrough channels.
func applyResponsesUserAgentPolicy(c *gin.Context, cfgManager *config.ConfigManager, upstream *config.UpstreamConfig, req *http.Request) {
	if c == nil || cfgManager == nil || upstream == nil || req == nil {
		return
	}
	if upstream.ServiceType != "responses" {
		return
	}
	req.Header.Set("User-Agent", cfgManager.ResolveResponsesUserAgent(c.GetHeader("User-Agent")))
}

func resolveResponsesUserAgentForOAuth(c *gin.Context, cfgManager *config.ConfigManager) string {
	if c == nil || cfgManager == nil {
		return config.DefaultResponsesUserAgent
	}
	return cfgManager.ResolveResponsesUserAgent(c.GetHeader("User-Agent"))
}
