package handlers

import (
	"bytes"
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

func markFirstSSEPayloadIfPresent(line string, firstPayloadTime **time.Time) {
	if firstPayloadTime == nil || *firstPayloadTime != nil {
		return
	}

	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return
	}

	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}

	ts := time.Now()
	*firstPayloadTime = &ts
}

func markFirstSSEPayloadInChunkIfPresent(chunk string, firstPayloadTime **time.Time) {
	if firstPayloadTime == nil || *firstPayloadTime != nil || chunk == "" {
		return
	}
	for _, line := range strings.Split(chunk, "\n") {
		markFirstSSEPayloadIfPresent(line, firstPayloadTime)
		if *firstPayloadTime != nil {
			return
		}
	}
}

func markFirstNonEmptyChunkIfPresent(chunk []byte, firstPayloadTime **time.Time) {
	if firstPayloadTime == nil || *firstPayloadTime != nil {
		return
	}
	if len(bytes.TrimSpace(chunk)) == 0 {
		return
	}
	ts := time.Now()
	*firstPayloadTime = &ts
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
