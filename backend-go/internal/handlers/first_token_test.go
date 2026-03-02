package handlers

import (
	"testing"
	"time"

	"github.com/JillVernus/cc-bridge/internal/utils"
)

func TestStreamDetectorForServiceType(t *testing.T) {
	tests := []struct {
		name         string
		serviceType  string
		expectNil    bool
		detectLine   string
		expectDetect bool
		preludeEvent string
	}{
		{
			name:         "claude",
			serviceType:  "claude",
			preludeEvent: "event: content_block_delta",
			detectLine:   `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`,
			expectDetect: true,
		},
		{
			name:         "openai chat alias",
			serviceType:  "openai_chat",
			detectLine:   `data: {"choices":[{"delta":{"content":"hi"}}]}`,
			expectDetect: true,
		},
		{
			name:         "responses",
			serviceType:  "responses",
			preludeEvent: "event: response.output_text.done",
			detectLine:   `data: {"type":"response.output_text.done","text":"hi"}`,
			expectDetect: true,
		},
		{
			name:         "gemini",
			serviceType:  "gemini",
			detectLine:   `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`,
			expectDetect: true,
		},
		{
			name:        "unknown",
			serviceType: "custom",
			expectNil:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := streamDetectorForServiceType(tc.serviceType)
			if tc.expectNil {
				if d != nil {
					t.Fatalf("expected nil detector for unknown service type")
				}
				return
			}
			if d == nil {
				t.Fatalf("expected detector for service type %q", tc.serviceType)
			}
			if tc.preludeEvent != "" {
				d.ObserveLine(tc.preludeEvent)
			}
			got := d.ObserveLine(tc.detectLine)
			if got != tc.expectDetect {
				t.Fatalf("detect result mismatch: got=%v want=%v", got, tc.expectDetect)
			}
		})
	}
}

func TestMarkFirstTokenIfDetected(t *testing.T) {
	var first *time.Time

	markFirstTokenIfDetected(false, &first)
	if first != nil {
		t.Fatalf("first token time should remain nil when detection is false")
	}

	markFirstTokenIfDetected(true, &first)
	if first == nil {
		t.Fatalf("first token time should be set when detection is true")
	}

	original := *first
	time.Sleep(1 * time.Millisecond)
	markFirstTokenIfDetected(true, &first)
	if !first.Equal(original) {
		t.Fatalf("first token time should not be overwritten once set")
	}

	// Safety: nil double pointer should be a no-op.
	markFirstTokenIfDetected(true, nil)
}

func TestFirstTokenDurationFromStart(t *testing.T) {
	start := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

	if got := firstTokenDurationFromStart(start, nil); got != 0 {
		t.Fatalf("expected 0 duration for nil first token, got %d", got)
	}

	first := start.Add(450 * time.Millisecond)
	if got := firstTokenDurationFromStart(start, &first); got != 450 {
		t.Fatalf("expected 450ms, got %d", got)
	}

	before := start.Add(-100 * time.Millisecond)
	if got := firstTokenDurationFromStart(start, &before); got != 0 {
		t.Fatalf("expected clamped 0ms for negative duration, got %d", got)
	}
}

func TestStreamDetectorServiceTypeAliases(t *testing.T) {
	aliases := []string{"openai", "openaiold", "openai-oauth"}
	for _, alias := range aliases {
		d := streamDetectorForServiceType(alias)
		if d == nil {
			t.Fatalf("expected detector for alias %q", alias)
		}
	}

	// Ensure helper maps to valid detector types and not nil for known protocol constants.
	if utils.NewFirstTokenDetector(utils.FirstTokenProtocolClaudeSSE) == nil {
		t.Fatalf("expected detector constructor to return non-nil")
	}
}
