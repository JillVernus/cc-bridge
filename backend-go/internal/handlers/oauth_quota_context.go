package handlers

import (
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func resolveOAuthQuotaUpdateContext(upstream *config.UpstreamConfig, logChannelIndex int, logChannelName string) (int, string, string) {
	if upstream == nil {
		return logChannelIndex, "", strings.TrimSpace(logChannelName)
	}

	channelIndex := logChannelIndex
	if channelIndex < 0 {
		channelIndex = upstream.Index
	}

	channelName := strings.TrimSpace(logChannelName)
	if channelName == "" {
		channelName = strings.TrimSpace(upstream.Name)
	}

	return channelIndex, strings.TrimSpace(upstream.ID), channelName
}
