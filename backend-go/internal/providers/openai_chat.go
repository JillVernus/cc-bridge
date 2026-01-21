package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JillVernus/cc-bridge/internal/config"
	"github.com/JillVernus/cc-bridge/internal/types"
	"github.com/JillVernus/cc-bridge/internal/utils"
	"github.com/gin-gonic/gin"
)

// OpenAIChatProvider bridges Claude Messages requests to upstreams that only support basic
// OpenAI Chat Completions (no native tool calling).
type OpenAIChatProvider struct {
	triggerSignal        string
	thinkingEnabled      bool
	hasTools             bool
	stream               bool
	estimatedInputTokens int
	requestedModel       string
}

type openAIChatThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type openAIChatClaudeRequest struct {
	Model       string                    `json:"model"`
	Messages    []types.ClaudeMessage     `json:"messages"`
	System      interface{}               `json:"system,omitempty"`
	MaxTokens   int                       `json:"max_tokens,omitempty"`
	Temperature float64                   `json:"temperature,omitempty"`
	Stream      bool                      `json:"stream,omitempty"`
	Tools       []types.ClaudeTool        `json:"tools,omitempty"`
	Thinking    *openAIChatThinkingConfig `json:"thinking,omitempty"`
}

func (p *OpenAIChatProvider) ConvertToProviderRequest(c *gin.Context, upstream *config.UpstreamConfig, apiKey string) (*http.Request, []byte, error) {
	originalBodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("读取请求体失败: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(originalBodyBytes))

	var claudeReq openAIChatClaudeRequest
	if err := json.Unmarshal(originalBodyBytes, &claudeReq); err != nil {
		return nil, originalBodyBytes, fmt.Errorf("解析Claude请求体失败: %w", err)
	}

	p.stream = claudeReq.Stream
	p.hasTools = len(claudeReq.Tools) > 0
	p.thinkingEnabled = claudeReq.Thinking != nil && strings.ToLower(strings.TrimSpace(claudeReq.Thinking.Type)) == "enabled"

	model := config.RedirectModel(claudeReq.Model, upstream)
	p.requestedModel = model

	var trigger string
	if p.hasTools {
		trigger, err = generateTriggerSignal()
		if err != nil {
			return nil, originalBodyBytes, fmt.Errorf("生成 trigger signal 失败: %w", err)
		}
	}
	p.triggerSignal = trigger

	messages := make([]map[string]interface{}, 0, len(claudeReq.Messages)+2)

	// Tool prompt injection goes first, so the model always sees the protocol.
	if p.hasTools {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": injectToolPrompt(claudeReq.Tools, trigger),
		})
	}

	// Preserve original system content as a separate system message (same as sample repo).
	if claudeReq.System != nil {
		systemText := normalizeClaudeBlocks(claudeReq.System, "", p.thinkingEnabled)
		if systemText != "" {
			messages = append(messages, map[string]interface{}{
				"role":    "system",
				"content": systemText,
			})
		}
	}

	for _, msg := range claudeReq.Messages {
		role := "user"
		if strings.ToLower(msg.Role) == "assistant" {
			role = "assistant"
		}

		content := normalizeClaudeBlocks(msg.Content, trigger, p.thinkingEnabled)

		if role == "user" && p.thinkingEnabled {
			content += openAIChatThinkingHint
		}

		messages = append(messages, map[string]interface{}{
			"role":    role,
			"content": content,
		})
	}

	appendContinueAssistantHint(messages)

	// Best-effort token estimate for request logging (some OpenAI-compatible upstreams don't return usage).
	p.estimatedInputTokens = estimateOpenAIChatInputTokens(messages)

	openaiReq := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   claudeReq.Stream,
	}
	// NOTE: Don't set `stream_options.include_usage` here by default; not all OpenAI-compatible
	// upstreams accept it. We rely on estimation for request logs when usage isn't provided.
	if claudeReq.MaxTokens > 0 {
		openaiReq["max_tokens"] = claudeReq.MaxTokens
	}
	if claudeReq.Temperature != 0 {
		openaiReq["temperature"] = claudeReq.Temperature
	}

	reqBodyBytes, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, originalBodyBytes, fmt.Errorf("序列化 OpenAI Chat 请求体失败: %w", err)
	}

	// Build URL with the same /v1 suffix detection used by the OpenAI provider.
	baseURL := strings.TrimSuffix(upstream.BaseURL, "/")
	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)
	hasVersionSuffix := versionPattern.MatchString(baseURL)
	endpoint := "/chat/completions"
	if !hasVersionSuffix {
		endpoint = "/v1" + endpoint
	}
	url := baseURL + endpoint

	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, originalBodyBytes, fmt.Errorf("创建 OpenAI Chat 请求失败: %w", err)
	}

	// Use minimal headers for OpenAI-compatible upstreams to avoid forwarding Anthropic-specific headers.
	req.Header = utils.PrepareMinimalHeaders(req.URL.Host)
	utils.SetAuthenticationHeader(req.Header, apiKey)
	if claudeReq.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	return req, originalBodyBytes, nil
}

func (p *OpenAIChatProvider) ConvertToClaudeResponse(providerResp *types.ProviderResponse) (*types.ClaudeResponse, error) {
	// Expect OpenAI Chat Completions response JSON.
	var openaiResp map[string]interface{}
	if err := json.Unmarshal(providerResp.Body, &openaiResp); err != nil {
		return nil, err
	}

	contentText := ""
	if choices, ok := openaiResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if s, ok := msg["content"].(string); ok {
					contentText = s
				}
			}
		}
	}

	parser := newOpenAIChatToolifyParser(p.triggerSignal, p.thinkingEnabled)
	parser.FeedString(contentText)
	parser.Finish()
	events := parser.ConsumeEvents()

	claudeResp := &types.ClaudeResponse{
		ID:      generateID(),
		Type:    "message",
		Role:    "assistant",
		Content: []types.ClaudeContent{},
	}

	// Best-effort usage extraction.
	// - Prefer upstream `usage` when present.
	// - Fall back to rough estimation when the upstream omits usage.
	claudeResp.Usage = extractOpenAIChatUsage(openaiResp)
	if claudeResp.Usage == nil {
		claudeResp.Usage = &types.Usage{
			InputTokens:  p.estimatedInputTokens,
			OutputTokens: estimateTokensFromText(contentText),
		}
	}

	hasToolUse := false
	for _, ev := range events {
		switch ev.Type {
		case "text":
			if ev.Content != "" {
				claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
					Type: "text",
					Text: ev.Content,
				})
			}
		case "thinking":
			if ev.Content != "" {
				claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
					Type:     "thinking",
					Thinking: ev.Content,
				})
			}
		case "tool_call":
			if ev.Call == nil {
				continue
			}
			hasToolUse = true
			claudeResp.Content = append(claudeResp.Content, types.ClaudeContent{
				Type:  "tool_use",
				ID:    generateID(),
				Name:  ev.Call.Name,
				Input: ev.Call.Arguments,
			})
		}
		if hasToolUse {
			break
		}
	}

	if hasToolUse {
		claudeResp.StopReason = "tool_use"
	} else {
		claudeResp.StopReason = "end_turn"
	}

	return claudeResp, nil
}

func (p *OpenAIChatProvider) HandleStreamResponse(body io.ReadCloser) (<-chan string, <-chan error, error) {
	eventChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer body.Close()

		emitter := newOpenAIChatClaudeStreamEmitter(eventChan)
		emitter.EmitMessageStart(p.requestedModel, p.estimatedInputTokens)

		parser := newOpenAIChatToolifyParser(p.triggerSignal, p.thinkingEnabled)

		outputRuneCount := 0
		var textBuf strings.Builder
		lastTextFlush := time.Now()
		flushText := func(force bool) {
			if textBuf.Len() == 0 {
				return
			}
			if !force && time.Since(lastTextFlush) < 150*time.Millisecond && textBuf.Len() < 256 {
				return
			}
			emitter.EmitText(textBuf.String())
			textBuf.Reset()
			lastTextFlush = time.Now()
		}

		scanner := bufio.NewScanner(body)
		const maxCapacity = 4 * 1024 * 1024
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		finishReason := ""

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				break
			}
			jsonStr := payload

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
				continue
			}

			if errObj, ok := chunk["error"]; ok {
				errChan <- fmt.Errorf("upstream error: %v", errObj)
				return
			}

			choices, ok := chunk["choices"].([]interface{})
			if !ok || len(choices) == 0 {
				continue
			}
			choice, ok := choices[0].(map[string]interface{})
			if !ok {
				continue
			}

			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				finishReason = fr
			}

			delta, ok := choice["delta"].(map[string]interface{})
			if !ok {
				continue
			}

			text := extractOpenAIChatDeltaText(delta)
			if text == "" {
				continue
			}

			parser.FeedString(text)
			for _, ev := range parser.ConsumeEvents() {
				switch ev.Type {
				case "text":
					outputRuneCount += utf8.RuneCountInString(ev.Content)
					textBuf.WriteString(ev.Content)
					flushText(false)
				case "thinking":
					outputRuneCount += utf8.RuneCountInString(ev.Content)
					flushText(true)
					emitter.EmitThinking(ev.Content)
				case "tool_call":
					if ev.Call == nil {
						continue
					}
					flushText(true)
					emitter.EmitToolUse(ev.Call.Name, ev.Call.Arguments)
					emitter.Close("tool_use", p.estimatedInputTokens, estimateTokensFromRunes(outputRuneCount))
					return
				}
			}
		}

		parser.Finish()
		for _, ev := range parser.ConsumeEvents() {
			switch ev.Type {
			case "text":
				outputRuneCount += utf8.RuneCountInString(ev.Content)
				textBuf.WriteString(ev.Content)
				flushText(true)
			case "thinking":
				outputRuneCount += utf8.RuneCountInString(ev.Content)
				flushText(true)
				emitter.EmitThinking(ev.Content)
			case "tool_call":
				if ev.Call == nil {
					continue
				}
				flushText(true)
				emitter.EmitToolUse(ev.Call.Name, ev.Call.Arguments)
				emitter.Close("tool_use", p.estimatedInputTokens, estimateTokensFromRunes(outputRuneCount))
				return
			}
		}

		flushText(true)
		stopReason := "end_turn"
		if finishReason == "length" {
			stopReason = "max_tokens"
		}
		emitter.Close(stopReason, p.estimatedInputTokens, estimateTokensFromRunes(outputRuneCount))
	}()

	return eventChan, errChan, nil
}

func extractOpenAIChatDeltaText(delta map[string]interface{}) string {
	if delta == nil {
		return ""
	}

	content, ok := delta["content"]
	if !ok || content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var b strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]interface{}:
				if t, ok := p["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	case map[string]interface{}:
		if t, ok := v["text"].(string); ok {
			return t
		}
	}

	return ""
}

func extractOpenAIChatUsage(openaiResp map[string]interface{}) *types.Usage {
	if openaiResp == nil {
		return nil
	}
	usageAny, ok := openaiResp["usage"]
	if !ok || usageAny == nil {
		return nil
	}
	usageMap, ok := usageAny.(map[string]interface{})
	if !ok {
		return nil
	}

	promptTokens := getIntFromAny(usageMap["prompt_tokens"])
	completionTokens := getIntFromAny(usageMap["completion_tokens"])
	totalTokens := getIntFromAny(usageMap["total_tokens"])
	if promptTokens == 0 && completionTokens == 0 {
		// Some upstreams only return total_tokens; treat it as output when nothing else exists.
		if totalTokens == 0 {
			return nil
		}
		return &types.Usage{InputTokens: 0, OutputTokens: totalTokens, TotalTokens: totalTokens}
	}
	return &types.Usage{
		InputTokens:      promptTokens,
		OutputTokens:     completionTokens,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

func getIntFromAny(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := json.Number(n).Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

func estimateOpenAIChatInputTokens(messages []map[string]interface{}) int {
	totalRunes := 0
	for _, msg := range messages {
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		totalRunes += utf8.RuneCountInString(content)
	}
	return estimateTokensFromRunes(totalRunes)
}

func estimateTokensFromText(text string) int {
	if text == "" {
		return 0
	}
	return estimateTokensFromRunes(utf8.RuneCountInString(text))
}

func estimateTokensFromRunes(runeCount int) int {
	if runeCount <= 0 {
		return 0
	}
	// Rough heuristic: ~4 chars per token.
	// Keep it stable and deterministic for request logging (not for billing accuracy).
	return int(math.Ceil(float64(runeCount) / 4.0))
}
