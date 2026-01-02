package apikey

import (
	"path/filepath"
	"strings"
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
	return false
}

// GetAllowedChannels returns the allowed channel indices for the given endpoint type
// Returns nil if all channels are allowed
func (vk *ValidatedKey) GetAllowedChannels(isResponses bool) []int {
	if vk == nil {
		return nil
	}
	if isResponses {
		return vk.AllowedChannelsResp
	}
	return vk.AllowedChannelsMsg
}

// HasChannelRestriction returns true if the key has channel restrictions for the given endpoint type
func (vk *ValidatedKey) HasChannelRestriction(isResponses bool) bool {
	channels := vk.GetAllowedChannels(isResponses)
	return len(channels) > 0
}

// IsChannelAllowed checks if a specific channel index is allowed
func (vk *ValidatedKey) IsChannelAllowed(channelIndex int, isResponses bool) bool {
	channels := vk.GetAllowedChannels(isResponses)
	if len(channels) == 0 {
		return true // nil = all allowed
	}
	for _, idx := range channels {
		if idx == channelIndex {
			return true
		}
	}
	return false
}
