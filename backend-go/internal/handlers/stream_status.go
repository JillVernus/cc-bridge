package handlers

import (
	"strings"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

func classifyStreamingRequestLogOutcome(httpStatus int, ctxErr error, streamErr error) (string, int, string) {
	if streamErr != nil {
		if isClientStreamDisconnect(ctxErr, streamErr) {
			return requestlog.StatusError, 499, "client disconnected during streaming response"
		}
		return requestlog.StatusError, 500, streamErr.Error()
	}

	// Some SSE clients close immediately after the final chunk, which cancels request
	// context after the stream has already finished successfully.
	return requestlog.StatusCompleted, httpStatus, ""
}

func isClientStreamDisconnect(ctxErr error, streamErr error) bool {
	if ctxErr != nil {
		return true
	}
	if streamErr == nil {
		return false
	}

	errMsg := strings.ToLower(streamErr.Error())
	return strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "client disconnected") ||
		strings.Contains(errMsg, "context canceled")
}
