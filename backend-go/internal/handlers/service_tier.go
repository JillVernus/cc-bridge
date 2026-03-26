package handlers

import (
	"encoding/json"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func isResponsesFastModeUpstream(serviceType string) bool {
	return serviceType == "responses" || serviceType == "openai-oauth"
}

func isFastModeForMessagesBridge(speed, upstreamServiceType string) bool {
	return strings.EqualFold(strings.TrimSpace(speed), "fast") && isResponsesFastModeUpstream(upstreamServiceType)
}

func normalizeResponsesServiceTier(serviceTier string) string {
	if strings.EqualFold(strings.TrimSpace(serviceTier), "priority") {
		return "priority"
	}
	return strings.TrimSpace(serviceTier)
}

func serviceTierForFastMode(isFastMode bool) string {
	if isFastMode {
		return "priority"
	}
	return ""
}

func shouldForceCodexPriorityOverride(upstream *config.UpstreamConfig) bool {
	if upstream == nil {
		return false
	}
	if upstream.ServiceType != "responses" && upstream.ServiceType != "openai-oauth" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(upstream.CodexServiceTierOverride), "force_priority")
}

func resolveEffectiveResponsesServiceTier(
	bodyBytes []byte,
	upstream *config.UpstreamConfig,
) ([]byte, string, bool, bool, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
		return nil, "", false, false, err
	}

	serviceTier := ""
	if rawTier, ok := reqMap["service_tier"].(string); ok {
		serviceTier = normalizeResponsesServiceTier(rawTier)
	}

	if shouldForceCodexPriorityOverride(upstream) && (serviceTier == "" || serviceTier == "default") {
		reqMap["service_tier"] = "priority"
		effectiveBody, err := json.Marshal(reqMap)
		if err != nil {
			return nil, "", false, false, err
		}
		return effectiveBody, "priority", true, true, nil
	}

	return bodyBytes, serviceTier, serviceTier == "priority", false, nil
}
