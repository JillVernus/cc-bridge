package utils

import (
	"encoding/json"
	"regexp"
	"strings"
)

// FirstTokenProtocol identifies the stream protocol/parser strategy.
type FirstTokenProtocol string

const (
	FirstTokenProtocolClaudeSSE     FirstTokenProtocol = "claude_sse"
	FirstTokenProtocolOpenAIChatSSE FirstTokenProtocol = "openai_chat_sse"
	FirstTokenProtocolResponsesSSE  FirstTokenProtocol = "responses_sse"
	FirstTokenProtocolGeminiRaw     FirstTokenProtocol = "gemini_raw"
)

var geminiTextRegex = regexp.MustCompile(`"text"\s*:\s*"([^"]+)"`)

// FirstTokenDetector detects whether a stream has produced its first token.
//
// Memory safety:
// - Uses a bounded sliding buffer for ObserveBytes.
// - Stops parsing as soon as the first token is detected.
type FirstTokenDetector struct {
	protocol     FirstTokenProtocol
	lastEvent    string
	detected     bool
	pending      string
	maxBufferLen int
}

func NewFirstTokenDetector(protocol FirstTokenProtocol) *FirstTokenDetector {
	return &FirstTokenDetector{
		protocol:     protocol,
		maxBufferLen: 4096,
	}
}

func (d *FirstTokenDetector) Done() bool {
	return d.detected
}

// ObserveLine inspects one logical stream line.
func (d *FirstTokenDetector) ObserveLine(line string) bool {
	if d.detected {
		return false
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	if strings.HasPrefix(trimmed, "event:") {
		d.lastEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		return false
	}

	if strings.HasPrefix(trimmed, "data:") {
		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if payload == "" || payload == "[DONE]" {
			return false
		}
		return d.observePayload(payload)
	}

	if d.protocol == FirstTokenProtocolGeminiRaw {
		return d.detectGeminiText(trimmed)
	}

	return false
}

// ObserveChunk inspects a chunk that may contain multiple lines/events.
func (d *FirstTokenDetector) ObserveChunk(chunk string) bool {
	if d.detected {
		return false
	}
	if chunk == "" {
		return false
	}

	for _, line := range strings.Split(chunk, "\n") {
		if d.ObserveLine(line) {
			return true
		}
	}

	if d.protocol == FirstTokenProtocolGeminiRaw {
		return d.detectGeminiText(chunk)
	}

	return false
}

// ObserveBytes inspects raw stream bytes with a bounded sliding window.
func (d *FirstTokenDetector) ObserveBytes(data []byte) bool {
	if d.detected {
		return false
	}
	if len(data) == 0 {
		return false
	}

	d.pending += string(data)
	if len(d.pending) > d.maxBufferLen*2 {
		d.pending = d.pending[len(d.pending)-d.maxBufferLen:]
	}

	for {
		idx := strings.IndexByte(d.pending, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(d.pending[:idx], "\r")
		d.pending = d.pending[idx+1:]
		if d.ObserveLine(line) {
			return true
		}
	}

	if d.protocol == FirstTokenProtocolGeminiRaw && d.detectGeminiText(d.pending) {
		d.detected = true
		return true
	}

	if len(d.pending) > d.maxBufferLen {
		d.pending = d.pending[len(d.pending)-d.maxBufferLen:]
	}
	return false
}

func (d *FirstTokenDetector) observePayload(payload string) bool {
	switch d.protocol {
	case FirstTokenProtocolClaudeSSE:
		if d.lastEvent == "content_block_delta" {
			d.detected = true
			return true
		}
		return false
	case FirstTokenProtocolOpenAIChatSSE:
		if d.detectOpenAIChatContent(payload) {
			d.detected = true
			return true
		}
		return false
	case FirstTokenProtocolResponsesSSE:
		if d.detectResponsesText(payload) {
			d.detected = true
			return true
		}
		return false
	case FirstTokenProtocolGeminiRaw:
		if d.detectGeminiText(payload) {
			d.detected = true
			return true
		}
		return false
	default:
		return false
	}
}

func (d *FirstTokenDetector) detectOpenAIChatContent(payload string) bool {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return false
	}

	choices, ok := msg["choices"].([]interface{})
	if !ok {
		return false
	}

	for _, choice := range choices {
		choiceMap, ok := choice.(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choiceMap["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := delta["content"].(string)
		if ok && strings.TrimSpace(content) != "" {
			return true
		}
	}

	return false
}

func (d *FirstTokenDetector) detectResponsesText(payload string) bool {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return false
	}

	// Prefer payload-declared type when present. Some upstreams omit/alter event lines,
	// and stale lastEvent should not block valid output_text detection.
	eventType := ""
	if typeVal, ok := msg["type"].(string); ok {
		eventType = strings.TrimSpace(typeVal)
	}
	if eventType == "" {
		eventType = d.lastEvent
	}

	switch eventType {
	case "response.output_text.delta":
		delta, _ := msg["delta"].(string)
		return strings.TrimSpace(delta) != ""
	case "response.output_text.done":
		text, _ := msg["text"].(string)
		return strings.TrimSpace(text) != ""
	case "response.content_part.added", "response.content_part.done":
		part, _ := msg["part"].(map[string]interface{})
		partType, _ := part["type"].(string)
		if partType != "output_text" {
			return false
		}
		text, _ := part["text"].(string)
		return strings.TrimSpace(text) != ""
	case "response.output_item.added", "response.output_item.done":
		item, _ := msg["item"].(map[string]interface{})
		return hasResponseItemOutputText(item)
	case "response.completed", "response.done":
		responseObj, _ := msg["response"].(map[string]interface{})
		return hasResponseOutputText(responseObj)
	default:
		return false
	}
}

func hasResponseItemOutputText(item map[string]interface{}) bool {
	if len(item) == 0 {
		return false
	}
	itemType, _ := item["type"].(string)
	if itemType != "message" {
		return false
	}
	content, _ := item["content"].([]interface{})
	for _, blockAny := range content {
		block, ok := blockAny.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		if blockType != "output_text" {
			continue
		}
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

func hasResponseOutputText(responseObj map[string]interface{}) bool {
	if len(responseObj) == 0 {
		return false
	}
	output, _ := responseObj["output"].([]interface{})
	for _, itemAny := range output {
		item, ok := itemAny.(map[string]interface{})
		if !ok {
			continue
		}
		if hasResponseItemOutputText(item) {
			return true
		}
	}
	return false
}

func (d *FirstTokenDetector) detectGeminiText(payload string) bool {
	if payload == "" {
		return false
	}

	var msg interface{}
	if err := json.Unmarshal([]byte(payload), &msg); err == nil {
		if hasNonEmptyTextField(msg) {
			return true
		}
	}

	matches := geminiTextRegex.FindAllStringSubmatch(payload, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if strings.TrimSpace(m[1]) != "" {
			return true
		}
	}

	return false
}

func hasNonEmptyTextField(v interface{}) bool {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if k == "text" {
				if s, ok := child.(string); ok && strings.TrimSpace(s) != "" {
					return true
				}
			}
			if hasNonEmptyTextField(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range val {
			if hasNonEmptyTextField(child) {
				return true
			}
		}
	}
	return false
}
