package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

func TestClassifyStreamingRequestLogOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		httpStatus     int
		ctxErr         error
		streamErr      error
		wantStatus     string
		wantHTTPStatus int
		wantError      string
	}{
		{
			name:           "completed without errors",
			httpStatus:     200,
			wantStatus:     requestlog.StatusCompleted,
			wantHTTPStatus: 200,
			wantError:      "",
		},
		{
			name:           "completed when context canceled after stream finished",
			httpStatus:     200,
			ctxErr:         context.Canceled,
			wantStatus:     requestlog.StatusCompleted,
			wantHTTPStatus: 200,
			wantError:      "",
		},
		{
			name:           "client disconnect by broken pipe",
			httpStatus:     200,
			streamErr:      errors.New("write tcp 10.0.0.1:443: broken pipe"),
			wantStatus:     requestlog.StatusError,
			wantHTTPStatus: 499,
			wantError:      "client disconnected during streaming response",
		},
		{
			name:           "client disconnect by context canceled with stream error",
			httpStatus:     200,
			ctxErr:         context.Canceled,
			streamErr:      errors.New("write failed"),
			wantStatus:     requestlog.StatusError,
			wantHTTPStatus: 499,
			wantError:      "client disconnected during streaming response",
		},
		{
			name:           "streaming transport error",
			httpStatus:     200,
			streamErr:      errors.New("unexpected EOF"),
			wantStatus:     requestlog.StatusError,
			wantHTTPStatus: 500,
			wantError:      "unexpected EOF",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, httpStatus, errMsg := classifyStreamingRequestLogOutcome(tt.httpStatus, tt.ctxErr, tt.streamErr)
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
			if httpStatus != tt.wantHTTPStatus {
				t.Fatalf("httpStatus = %d, want %d", httpStatus, tt.wantHTTPStatus)
			}
			if errMsg != tt.wantError {
				t.Fatalf("error = %q, want %q", errMsg, tt.wantError)
			}
		})
	}
}
