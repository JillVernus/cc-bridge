package providers

import (
	"encoding/json"
	"fmt"
	"time"
)

type openAIChatClaudeStreamEmitter struct {
	eventChan chan string

	nextBlockIndex int

	textBlockOpen     bool
	thinkingBlockOpen bool

	messageID string

	hasToolUse bool
}

func newOpenAIChatClaudeStreamEmitter(eventChan chan string) *openAIChatClaudeStreamEmitter {
	return &openAIChatClaudeStreamEmitter{
		eventChan: eventChan,
		messageID: fmt.Sprintf("msg_%d", time.Now().UnixNano()),
	}
}

func (e *openAIChatClaudeStreamEmitter) EmitMessageStart(model string, inputTokens int) {
	if model == "" {
		model = "openai_chat"
	}
	data := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            e.messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
			"content":     []interface{}{},
			"stop_reason": nil,
		},
	}
	e.send("message_start", data)
}

func (e *openAIChatClaudeStreamEmitter) EmitText(text string) {
	if text == "" {
		return
	}
	e.ensureTextBlock()
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": e.nextBlockIndex - 1,
		"delta": map[string]string{
			"type": "text_delta",
			"text": text,
		},
	}
	e.send("content_block_delta", data)
}

func (e *openAIChatClaudeStreamEmitter) EmitThinking(content string) {
	if content == "" {
		return
	}
	e.ensureThinkingBlock()
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": e.nextBlockIndex - 1,
		"delta": map[string]string{
			"type":     "thinking_delta",
			"thinking": content,
		},
	}
	e.send("content_block_delta", data)
}

func (e *openAIChatClaudeStreamEmitter) EmitToolUse(name string, input map[string]interface{}) {
	e.endTextBlock()
	e.endThinkingBlock()

	index := e.nextBlockIndex
	e.nextBlockIndex++

	toolID := fmt.Sprintf("toolu_%d", time.Now().UnixNano())
	start := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type": "tool_use",
			"id":   toolID,
			"name": name,
		},
	}
	e.send("content_block_start", start)

	inputJSON, _ := json.Marshal(input)
	delta := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]string{
			"type":         "input_json_delta",
			"partial_json": string(inputJSON),
		},
	}
	e.send("content_block_delta", delta)

	stop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	e.send("content_block_stop", stop)

	e.hasToolUse = true
}

func (e *openAIChatClaudeStreamEmitter) EmitMessageDeltaStopReason(stopReason string, inputTokens int, outputTokens int) {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	data := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
	}
	// Attach usage for request logging. Claude's streaming schema typically includes only
	// output_tokens here, but adding input_tokens is tolerated and helps our logs.
	if inputTokens > 0 || outputTokens > 0 {
		data["usage"] = map[string]int{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		}
	}
	e.send("message_delta", data)
}

func (e *openAIChatClaudeStreamEmitter) EmitMessageStop() {
	data := map[string]interface{}{
		"type": "message_stop",
	}
	e.send("message_stop", data)
}

func (e *openAIChatClaudeStreamEmitter) ensureTextBlock() {
	if e.textBlockOpen {
		return
	}
	e.endThinkingBlock()

	index := e.nextBlockIndex
	e.nextBlockIndex++

	start := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]string{
			"type": "text",
			"text": "",
		},
	}
	e.send("content_block_start", start)
	e.textBlockOpen = true
}

func (e *openAIChatClaudeStreamEmitter) ensureThinkingBlock() {
	if e.thinkingBlockOpen {
		return
	}
	e.endTextBlock()

	index := e.nextBlockIndex
	e.nextBlockIndex++

	start := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]string{
			"type":     "thinking",
			"thinking": "",
		},
	}
	e.send("content_block_start", start)
	e.thinkingBlockOpen = true
}

func (e *openAIChatClaudeStreamEmitter) endTextBlock() {
	if !e.textBlockOpen {
		return
	}
	index := e.nextBlockIndex - 1
	stop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	e.send("content_block_stop", stop)
	e.textBlockOpen = false
}

func (e *openAIChatClaudeStreamEmitter) endThinkingBlock() {
	if !e.thinkingBlockOpen {
		return
	}
	index := e.nextBlockIndex - 1
	stop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	e.send("content_block_stop", stop)
	e.thinkingBlockOpen = false
}

func (e *openAIChatClaudeStreamEmitter) Close(stopReason string, inputTokens int, outputTokens int) {
	e.endTextBlock()
	e.endThinkingBlock()
	e.EmitMessageDeltaStopReason(stopReason, inputTokens, outputTokens)
	e.EmitMessageStop()
}

func (e *openAIChatClaudeStreamEmitter) send(event string, data interface{}) {
	b, _ := json.Marshal(data)
	e.eventChan <- fmt.Sprintf("event: %s\ndata: %s\n\n", event, b)
}
