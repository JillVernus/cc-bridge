package providers

import (
	"io"
	"strings"
	"testing"
	"time"
)

func assertStreamChannelsClose(t *testing.T, name string, provider Provider, input string) {
	t.Helper()

	eventChan, errChan, err := provider.HandleStreamResponse(io.NopCloser(strings.NewReader(input)))
	if err != nil {
		t.Fatalf("%s: HandleStreamResponse returned error: %v", name, err)
	}

	done := make(chan struct{})
	go func() {
		for range eventChan {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("%s: event channel did not close", name)
	}

	select {
	case _, ok := <-errChan:
		if !ok {
			return
		}
		t.Fatalf("%s: expected err channel to close without values", name)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("%s: err channel did not close", name)
	}
}

func TestStreamProviders_CloseErrChannelOnCleanEOF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider Provider
		input    string
	}{
		{
			name:     "responses upstream",
			provider: &ResponsesUpstreamProvider{},
			input: strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"Hi"}`,
				``,
				`data: {"type":"response.completed","response":{"model":"gpt-5.3-codex","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
		{
			name:     "openai",
			provider: &OpenAIProvider{},
			input: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`,
				``,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
		{
			name:     "openai chat",
			provider: &OpenAIChatProvider{},
			input: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`,
				``,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
		{
			name:     "gemini",
			provider: &GeminiProvider{},
			input: strings.Join([]string{
				`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
				``,
				`data: {"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}`,
				``,
			}, "\n"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertStreamChannelsClose(t, tt.name, tt.provider, tt.input)
		})
	}
}
