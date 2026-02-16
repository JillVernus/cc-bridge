package handlers

import (
	"net/http"

	"github.com/JillVernus/cc-bridge/internal/forwardproxy"
	"github.com/gin-gonic/gin"
)

// GetForwardProxyConfig returns the current forward proxy configuration.
func GetForwardProxyConfig(fpServer *forwardproxy.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if fpServer == nil {
			c.JSON(http.StatusOK, gin.H{
				"enabled":          false,
				"interceptDomains": []string{},
				"running":          false,
			})
			return
		}
		cfg := fpServer.GetConfig()
		c.JSON(http.StatusOK, gin.H{
			"enabled":          cfg.Enabled,
			"interceptDomains": cfg.InterceptDomains,
			"running":          fpServer.IsRunning(),
			"port":             fpServer.GetPort(),
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
			Enabled          *bool    `json:"enabled"`
			InterceptDomains []string `json:"interceptDomains"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}

		cfg := fpServer.GetConfig()
		if req.Enabled != nil {
			cfg.Enabled = *req.Enabled
		}
		if req.InterceptDomains != nil {
			cfg.InterceptDomains = req.InterceptDomains
		}

		if err := fpServer.UpdateConfig(cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config: " + err.Error()})
			return
		}

		updatedCfg := fpServer.GetConfig()
		c.JSON(http.StatusOK, gin.H{
			"message":          "Forward proxy config updated",
			"enabled":          updatedCfg.Enabled,
			"interceptDomains": updatedCfg.InterceptDomains,
			"running":          fpServer.IsRunning(),
			"port":             fpServer.GetPort(),
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
