package handlers

import "strings"

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
