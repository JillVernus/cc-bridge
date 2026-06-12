package forwardproxy

import (
	"io"
	"strings"
	"testing"
)

func TestTerminalSSEResponseBodyIncludesTerminalFrameDelimiterBeforeEOF(t *testing.T) {
	body := &chunkedReadCloser{
		chunks: []string{
			`data: {"type":"response.output_text.delta","delta":"Hi"}` + "\n\n",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n",
			`data: {"type":"response.output_text.delta","delta":"late"}` + "\n\n",
		},
	}

	got, err := io.ReadAll(wrapTerminalSSEResponseBody("/v1/responses", body))
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	response := string(got)
	completedIndex := strings.Index(response, `"type":"response.completed"`)
	if completedIndex < 0 {
		t.Fatalf("expected response.completed to be forwarded, got %q", response)
	}
	if !strings.Contains(response[completedIndex:], "\n\n") {
		t.Fatalf("expected terminal SSE frame delimiter before EOF, got %q", response)
	}
	if strings.Contains(response, `"delta":"late"`) {
		t.Fatalf("expected wrapper to stop after terminal SSE frame, got %q", response)
	}
}

type chunkedReadCloser struct {
	chunks []string
	index  int
}

func (c *chunkedReadCloser) Read(p []byte) (int, error) {
	if c.index >= len(c.chunks) {
		return 0, io.EOF
	}

	chunk := c.chunks[c.index]
	c.index++
	return copy(p, chunk), nil
}

func (c *chunkedReadCloser) Close() error {
	return nil
}
