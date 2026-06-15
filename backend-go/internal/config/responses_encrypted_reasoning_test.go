package config

import "testing"

func TestNormalizeResponsesEncryptedReasoningMode(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		serviceType string
		raw         string
		want        string
	}{
		{
			name:        "eligible responses channel defaults to auto",
			channelType: "responses",
			serviceType: "responses",
			raw:         "",
			want:        ResponsesEncryptedReasoningModeAuto,
		},
		{
			name:        "oauth channels force off",
			channelType: "responses",
			serviceType: "openai-oauth",
			raw:         " Always ",
			want:        ResponsesEncryptedReasoningModeOff,
		},
		{
			name:        "eligible responses channel preserves off",
			channelType: "responses",
			serviceType: "responses",
			raw:         "OFF",
			want:        ResponsesEncryptedReasoningModeOff,
		},
		{
			name:        "ineligible service type is off",
			channelType: "responses",
			serviceType: "openai",
			raw:         "always",
			want:        ResponsesEncryptedReasoningModeOff,
		},
		{
			name:        "messages pool is off",
			channelType: "messages",
			serviceType: "responses",
			raw:         "auto",
			want:        ResponsesEncryptedReasoningModeOff,
		},
		{
			name:        "unknown mode falls back to auto for eligible channels",
			channelType: "responses",
			serviceType: "responses",
			raw:         "sometimes",
			want:        ResponsesEncryptedReasoningModeAuto,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeResponsesEncryptedReasoningMode(tt.channelType, tt.serviceType, tt.raw)
			if got != tt.want {
				t.Fatalf("NormalizeResponsesEncryptedReasoningMode(%q, %q, %q) = %q, want %q", tt.channelType, tt.serviceType, tt.raw, got, tt.want)
			}
		})
	}
}

func TestShouldIncludeResponsesEncryptedReasoning(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		serviceType string
		mode        string
		want        bool
	}{
		{
			name:        "auto includes for native responses channels",
			channelType: "responses",
			serviceType: "responses",
			mode:        ResponsesEncryptedReasoningModeAuto,
			want:        true,
		},
		{
			name:        "oauth channels do not include",
			channelType: "responses",
			serviceType: "openai-oauth",
			mode:        ResponsesEncryptedReasoningModeAlways,
			want:        false,
		},
		{
			name:        "off does not include",
			channelType: "responses",
			serviceType: "responses",
			mode:        ResponsesEncryptedReasoningModeOff,
			want:        false,
		},
		{
			name:        "ineligible service type does not include",
			channelType: "responses",
			serviceType: "claude",
			mode:        ResponsesEncryptedReasoningModeAlways,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldIncludeResponsesEncryptedReasoning(tt.channelType, tt.serviceType, tt.mode)
			if got != tt.want {
				t.Fatalf("ShouldIncludeResponsesEncryptedReasoning(%q, %q, %q) = %v, want %v", tt.channelType, tt.serviceType, tt.mode, got, tt.want)
			}
		})
	}
}
