package handlers

import (
	"errors"
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/forwardproxy"
	"github.com/gin-gonic/gin"
)

// GetForwardProxyConfig returns the current forward proxy configuration.
func GetForwardProxyConfig(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusOK, gin.H{
				"enabled":            false,
				"discoveryEnabled":   false,
				"interceptDomains":   []string{},
				"domainAliases":      map[string]string{},
				"xInitiatorOverride": forwardproxy.XInitiatorOverrideConfig{
					Enabled:         false,
					Mode:            forwardproxy.XInitiatorOverrideModeFixedWindow,
					DurationSeconds: 300,
					OverrideTimes:   1,
					TotalCost:       1,
				},
				"xInitiatorOverrideRuntime": forwardproxy.XInitiatorOverrideRuntimeStatus{
					Enabled: false,
					Mode:    forwardproxy.XInitiatorOverrideModeFixedWindow,
				},
				"running": false,
			})
			return
		}
		snapshot := fpServer.GetConfigSnapshot()
		c.JSON(http.StatusOK, gin.H{
			"enabled":                   snapshot.Config.Enabled,
			"discoveryEnabled":          snapshot.Config.DiscoveryEnabled,
			"interceptDomains":          snapshot.Config.InterceptDomains,
			"domainAliases":             snapshot.Config.DomainAliases,
			"xInitiatorOverride":        snapshot.Config.XInitiatorOverride,
			"xInitiatorOverrideRuntime": snapshot.Runtime,
			"running":                   snapshot.Running,
			"port":                      snapshot.Port,
		})
	}
}

// UpdateForwardProxyConfig updates the forward proxy configuration.
func UpdateForwardProxyConfig(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Forward proxy is not running. Set FORWARD_PROXY_ENABLED=true and restart."})
			return
		}

		var req struct {
			Enabled            *bool                                  `json:"enabled"`
			DiscoveryEnabled  *bool                                  `json:"discoveryEnabled"`
			InterceptDomains   []string                               `json:"interceptDomains"`
			DomainAliases      map[string]string                      `json:"domainAliases"`
			XInitiatorOverride *forwardproxy.XInitiatorOverrideConfig `json:"xInitiatorOverride"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		cfg := fpServer.GetConfig()
		if req.Enabled != nil {
			cfg.Enabled = *req.Enabled
		}
		if req.DiscoveryEnabled != nil {
			cfg.DiscoveryEnabled = *req.DiscoveryEnabled
		}
		if req.InterceptDomains != nil {
			cfg.InterceptDomains = req.InterceptDomains
		}
		if req.DomainAliases != nil {
			cfg.DomainAliases = req.DomainAliases
		}
		if req.XInitiatorOverride != nil {
			cfg.XInitiatorOverride = *req.XInitiatorOverride
		}

		if err := fpServer.UpdateConfig(cfg); err != nil {
			var validationErr *forwardproxy.ValidationError
			if errors.As(err, &validationErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": validationErr.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config: " + err.Error()})
			return
		}

		snapshot := fpServer.GetConfigSnapshot()
		c.JSON(http.StatusOK, gin.H{
			"message":                   "Forward proxy config updated",
			"enabled":                   snapshot.Config.Enabled,
			"discoveryEnabled":          snapshot.Config.DiscoveryEnabled,
			"interceptDomains":          snapshot.Config.InterceptDomains,
			"domainAliases":             snapshot.Config.DomainAliases,
			"xInitiatorOverride":        snapshot.Config.XInitiatorOverride,
			"xInitiatorOverrideRuntime": snapshot.Runtime,
			"running":                   snapshot.Running,
			"port":                      snapshot.Port,
		})
	}
}

// DownloadForwardProxyCACert serves the CA certificate for download.
func DownloadForwardProxyCACert(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Forward proxy is not running"})
			return
		}

		certPEM, err := fpServer.GetCertManager().GetCACertPEM()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read CA certificate: " + err.Error()})
			return
		}

		c.Header("Content-Disposition", "attachment; filename=ccbridge-ca.pem")
		c.Data(http.StatusOK, "application/x-pem-file", certPEM)
	}
}

// GetForwardProxyDiscovery returns aggregated forward proxy discovery entries.
func GetForwardProxyDiscovery(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusOK, gin.H{
				"entries":          []forwardproxy.DiscoveryEntry{},
				"discoveryEnabled": false,
				"running":          false,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"entries":          fpServer.GetDiscoveryEntries(),
			"discoveryEnabled": fpServer.IsDiscoveryEnabled(),
			"running":          fpServer.IsRunning(),
		})
	}
}

// ClearForwardProxyDiscovery clears persisted forward proxy discovery entries.
func ClearForwardProxyDiscovery(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Forward proxy is not running"})
			return
		}
		if err := fpServer.ClearDiscoveryEntries(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear discovery entries: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":          "Forward proxy discovery cleared",
			"entries":          []forwardproxy.DiscoveryEntry{},
			"discoveryEnabled": fpServer.IsDiscoveryEnabled(),
			"running":          fpServer.IsRunning(),
		})
	}
}
