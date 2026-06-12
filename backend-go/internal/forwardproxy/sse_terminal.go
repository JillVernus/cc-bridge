package forwardproxy

import (
	"bytes"
	"io"
	"strings"

	"github.com/tidwall/gjson"
)

const terminalSSEDetectorMaxBufferedLine = 10 * 1024 * 1024

type terminalSSEResponseBody struct {
	body     io.ReadCloser
	detector *terminalSSEDetector
	done     bool
	pending  []byte
}

func wrapTerminalSSEResponseBody(path string, body io.ReadCloser) io.ReadCloser {
	detector := newTerminalSSEDetector(path)
	if detector == nil {
		return body
	}
	return &terminalSSEResponseBody{
		body:     body,
		detector: detector,
	}
}

func (b *terminalSSEResponseBody) Read(p []byte) (int, error) {
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}

	if b.done {
		return 0, io.EOF
	}

	n, err := b.body.Read(p)
	if n > 0 {
		if done, consumed := b.detector.Observe(p[:n]); done {
			b.done = true
			_ = b.body.Close()
			b.pending = []byte("\n")
			if consumed < n {
				n = consumed
			}
		}
		return n, nil
	}
	return n, err
}

func (b *terminalSSEResponseBody) Close() error {
	return b.body.Close()
}

type terminalSSEDetector struct {
	line      bytes.Buffer
	lastEvent string
}

func newTerminalSSEDetector(path string) *terminalSSEDetector {
	if detectInterceptedRequestKind(path).logType != "responses" {
		return nil
	}
	return &terminalSSEDetector{}
}

func (d *terminalSSEDetector) Observe(data []byte) (bool, int) {
	if len(data) == 0 {
		return false, 0
	}

	for i, b := range data {
		d.line.WriteByte(b)
		if d.line.Len() > terminalSSEDetectorMaxBufferedLine {
			tail := append([]byte(nil), d.line.Bytes()[d.line.Len()-terminalSSEDetectorMaxBufferedLine:]...)
			d.line.Reset()
			_, _ = d.line.Write(tail)
		}
		if b == '\n' {
			line := d.line.String()
			d.line.Reset()
			if d.observeLine(line) {
				return true, i + 1
			}
		}
	}
	return false, 0
}

func (d *terminalSSEDetector) observeLine(rawLine string) bool {
	line := strings.TrimSpace(rawLine)
	if line == "" {
		d.lastEvent = ""
		return false
	}

	if strings.HasPrefix(line, "event:") {
		d.lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return false
	}

	if !strings.HasPrefix(line, "data:") {
		return false
	}

	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "[DONE]" || d.lastEvent == "response.completed" || d.lastEvent == "response.done" {
		return true
	}

	eventType := strings.TrimSpace(gjson.Get(payload, "type").String())
	return eventType == "response.completed" || eventType == "response.done"
}
