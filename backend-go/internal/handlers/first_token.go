package handlers

import (
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/utils"
)

func streamDetectorForServiceType(serviceType string) *utils.FirstTokenDetector {
	switch strings.ToLower(strings.TrimSpace(serviceType)) {
	case "claude":
		return utils.NewFirstTokenDetector(utils.FirstTokenProtocolClaudeSSE)
	case "openai", "openai_chat", "openaiold":
		return utils.NewFirstTokenDetector(utils.FirstTokenProtocolOpenAIChatSSE)
	case "responses", "openai-oauth":
		return utils.NewFirstTokenDetector(utils.FirstTokenProtocolResponsesSSE)
	case "gemini":
		return utils.NewFirstTokenDetector(utils.FirstTokenProtocolGeminiRaw)
	default:
		return nil
	}
}

func markFirstTokenIfDetected(detected bool, firstTokenTime **time.Time) {
	if !detected || firstTokenTime == nil || *firstTokenTime != nil {
		return
	}
	ts := time.Now()
	*firstTokenTime = &ts
}

func firstTokenDurationFromStart(startTime time.Time, firstTokenTime *time.Time) int64 {
	if firstTokenTime == nil {
		return 0
	}
	durationMs := firstTokenTime.Sub(startTime).Milliseconds()
	if durationMs < 0 {
		return 0
	}
	return durationMs
}
