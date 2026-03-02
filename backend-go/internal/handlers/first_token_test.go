package handlers

import (
	"strings"
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

func TestMarkFirstSSEPayloadIfPresent(t *testing.T) {
	var first *time.Time

	markFirstSSEPayloadIfPresent("event: response.output_text.delta", &first)
	if first != nil {
		t.Fatalf("event line should not be treated as payload")
	}

	markFirstSSEPayloadIfPresent("data:   ", &first)
	if first != nil {
		t.Fatalf("empty data payload should be ignored")
	}

	markFirstSSEPayloadIfPresent("data: [DONE]", &first)
	if first != nil {
		t.Fatalf("[DONE] payload should be ignored")
	}

	markFirstSSEPayloadIfPresent(`data: {"type":"response.output_item.added","item":{"type":"function_call"}}`, &first)
	if first == nil {
		t.Fatalf("non-empty data payload should set first payload time")
	}

	original := *first
	time.Sleep(1 * time.Millisecond)
	markFirstSSEPayloadIfPresent(`data: {"type":"response.completed"}`, &first)
	if !first.Equal(original) {
		t.Fatalf("first payload time should not be overwritten once set")
	}

	// Safety: nil double pointer should be a no-op.
	markFirstSSEPayloadIfPresent(`data: {"type":"response.completed"}`, nil)
}

func TestMarkFirstSSEPayloadInChunkIfPresent(t *testing.T) {
	var first *time.Time

	markFirstSSEPayloadInChunkIfPresent(strings.Join([]string{
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","item":{"type":"function_call"}}`,
		"",
	}, "\n"), &first)
	if first == nil {
		t.Fatalf("expected first payload time from chunk helper")
	}

	original := *first
	time.Sleep(1 * time.Millisecond)
	markFirstSSEPayloadInChunkIfPresent("data: {\"type\":\"response.completed\"}\n", &first)
	if !first.Equal(original) {
		t.Fatalf("first payload time should not be overwritten once set")
	}
}

func TestMarkFirstNonEmptyChunkIfPresent(t *testing.T) {
	var first *time.Time

	markFirstNonEmptyChunkIfPresent([]byte(" \n\t"), &first)
	if first != nil {
		t.Fatalf("whitespace-only chunk should not set first payload time")
	}

	markFirstNonEmptyChunkIfPresent([]byte(`{"usageMetadata":{"promptTokenCount":3}}`), &first)
	if first == nil {
		t.Fatalf("non-empty chunk should set first payload time")
	}

	original := *first
	time.Sleep(1 * time.Millisecond)
	markFirstNonEmptyChunkIfPresent([]byte(`{"ignored":true}`), &first)
	if !first.Equal(original) {
		t.Fatalf("first payload time should not be overwritten once set")
	}
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
