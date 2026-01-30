package apikey

import (
	"path/filepath"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/utils"
)

// CheckEndpointPermission checks if the API key can access the given endpoint
// endpoint: "messages" or "responses"
// Returns true if allowed, false if denied
func (vk *ValidatedKey) CheckEndpointPermission(endpoint string) bool {
	if vk == nil || len(vk.AllowedEndpoints) == 0 {
		return true // nil = all allowed
	}
	for _, e := range vk.AllowedEndpoints {
		if strings.EqualFold(e, endpoint) {
			return true
		}
	}
	return false
}

// CheckModelPermission checks if the API key can use the given model
// Uses glob pattern matching (e.g., "claude-sonnet-*" matches "claude-sonnet-4-5-20250514")
// Returns true if allowed, false if denied
func (vk *ValidatedKey) CheckModelPermission(model string) bool {
	if vk == nil || len(vk.AllowedModels) == 0 {
		return true // nil = all allowed
	}

	// Support optional "thinking suffix" appended as "(...)" at the end of the model string.
	// We preserve full-string matching first, then fall back to the base model for compatibility.
	baseModel, _, hasSuffix := utils.SplitModelSuffix(model)

	modelLower := strings.ToLower(model)
	for _, pattern := range vk.AllowedModels {
		patternLower := strings.ToLower(pattern)

		// Try exact match first
		if patternLower == modelLower {
			return true
		}

		// Try glob pattern match
		if matched, _ := filepath.Match(patternLower, modelLower); matched {
			return true
		}

		// Try prefix match for patterns ending with *
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(patternLower, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return true
			}
		}
	}

	if hasSuffix {
		// Retry permission check against the base model (without suffix), to avoid breaking
		// existing allow-lists that were configured with base model IDs.
		baseLower := strings.ToLower(baseModel)
		for _, pattern := range vk.AllowedModels {
			patternLower := strings.ToLower(pattern)
			if patternLower == baseLower {
				return true
			}
			if matched, _ := filepath.Match(patternLower, baseLower); matched {
				return true
			}
			if strings.HasSuffix(pattern, "*") {
				prefix := strings.TrimSuffix(patternLower, "*")
				if strings.HasPrefix(baseLower, prefix) {
					return true
				}
			}
		}
	}

	return false
}

// GetAllowedChannelsByType returns allowed channel IDs for a given endpoint type.
// Valid channelType values: "messages", "responses", "gemini".
// Returns nil if all channels are allowed.
func (vk *ValidatedKey) GetAllowedChannelsByType(channelType string) []string {
	if vk == nil {
		return nil
	}

	switch strings.ToLower(channelType) {
	case "responses":
		return vk.AllowedChannelsResp
	case "gemini":
		return vk.AllowedChannelsGemini
	default: // "messages"
		return vk.AllowedChannelsMsg
	}
}

// GetAllowedChannels returns the allowed channel IDs for the given endpoint type
// Returns nil if all channels are allowed
func (vk *ValidatedKey) GetAllowedChannels(isResponses bool) []string {
	if vk == nil {
		return nil
	}
	if isResponses {
		return vk.GetAllowedChannelsByType("responses")
	}
	return vk.GetAllowedChannelsByType("messages")
}

// HasChannelRestriction returns true if the key has channel restrictions for the given endpoint type
func (vk *ValidatedKey) HasChannelRestriction(isResponses bool) bool {
	channels := vk.GetAllowedChannels(isResponses)
	return len(channels) > 0
}

// IsChannelAllowed checks if a specific channel ID is allowed
func (vk *ValidatedKey) IsChannelAllowed(channelID string, isResponses bool) bool {
	channels := vk.GetAllowedChannels(isResponses)
	if len(channels) == 0 {
		return true // nil = all allowed
	}
	for _, id := range channels {
		if id == channelID {
			return true
		}
	}
	return false
}
