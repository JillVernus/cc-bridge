package providers

import (
	"encoding/json"
	"regexp"
	"strings"
)

type openAIChatParsedInvokeCall struct {
	Name      string
	Arguments map[string]interface{}
}

type openAIChatParserEvent struct {
	Type    string // "text", "tool_call", "thinking", "end"
	Content string
	Call    *openAIChatParsedInvokeCall
}

var (
	openAIChatInvokeNameRe = regexp.MustCompile(`(?is)<invoke\b[^>]*name="([^"]+)"[^>]*>`)
	openAIChatParamRe      = regexp.MustCompile(`(?is)<parameter\b[^>]*name="([^"]+)"[^>]*>([\s\S]*?)</parameter>`)
)

type openAIChatToolifyParser struct {
	triggerSignal   string
	thinkingEnabled bool

	triggerRunes []rune

	buffer        []rune
	captureBuffer []rune
	capturing     bool

	thinkingMode   bool
	thinkingBuffer []rune
	thinkingEndRunes []rune

	events []openAIChatParserEvent
}

type openAIChatThinkingTag struct {
	Start []rune
	End   []rune
}

var openAIChatThinkingTags = []openAIChatThinkingTag{
	{Start: []rune("<thinking>"), End: []rune("</thinking>")},
	{Start: []rune("<antml\b:thinking>"), End: []rune("</antml\b:thinking>")},
	{Start: []rune("<antml\\b:thinking>"), End: []rune("</antml\\b:thinking>")},
	{Start: []rune("<antml:thinking>"), End: []rune("</antml:thinking>")},
}

func newOpenAIChatToolifyParser(triggerSignal string, thinkingEnabled bool) *openAIChatToolifyParser {
	return &openAIChatToolifyParser{
		triggerSignal:   triggerSignal,
		thinkingEnabled: thinkingEnabled,
		triggerRunes:    []rune(triggerSignal),
		buffer:          make([]rune, 0, 1024),
		captureBuffer:   make([]rune, 0, 1024),
		thinkingBuffer:  make([]rune, 0, 1024),
		thinkingEndRunes: nil,
		events:          make([]openAIChatParserEvent, 0, 32),
	}
}

func (p *openAIChatToolifyParser) FeedString(s string) {
	for _, r := range s {
		p.FeedRune(r)
	}
}

func (p *openAIChatToolifyParser) FeedRune(r rune) {
	toolProtocolEnabled := p.triggerSignal != ""

	if !toolProtocolEnabled {
		p.handleRuneWithoutTrigger(r)
		return
	}

	if p.thinkingEnabled {
		if p.thinkingMode {
			p.thinkingBuffer = append(p.thinkingBuffer, r)
			if len(p.thinkingEndRunes) > 0 && hasSuffixRunes(p.thinkingBuffer, p.thinkingEndRunes) {
				thinkingContent := string(p.thinkingBuffer[:len(p.thinkingBuffer)-len(p.thinkingEndRunes)])
				thinkingContent = strings.TrimLeft(thinkingContent, " \t\r\n")
				thinkingContent = strings.TrimPrefix(thinkingContent, ">")
				thinkingContent = strings.TrimLeft(thinkingContent, " \t\r\n")
				if thinkingContent != "" {
					p.events = append(p.events, openAIChatParserEvent{Type: "thinking", Content: thinkingContent})
				}
				p.thinkingBuffer = p.thinkingBuffer[:0]
				p.thinkingMode = false
				p.thinkingEndRunes = nil
			}
			return
		}
		if p.maybeEnterThinking(r) {
			return
		}
	}

	if p.capturing {
		p.captureBuffer = append(p.captureBuffer, r)
		p.tryEmitInvokes(false)
		return
	}

	p.buffer = append(p.buffer, r)

	if hasSuffixRunes(p.buffer, p.triggerRunes) {
		textPortion := string(p.buffer[:len(p.buffer)-len(p.triggerRunes)])
		if textPortion != "" {
			p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: textPortion})
		}
		p.buffer = p.buffer[:0]
		p.capturing = true
		p.captureBuffer = p.captureBuffer[:0]
		return
	}

	p.flushTextBufferIfNeeded()
}

func (p *openAIChatToolifyParser) Finish() {
	if len(p.buffer) > 0 {
		p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: string(p.buffer)})
	}

	if p.thinkingEnabled && p.thinkingMode && len(p.thinkingBuffer) > 0 {
		thinkingContent := strings.TrimLeft(string(p.thinkingBuffer), " \t\r\n")
		thinkingContent = strings.TrimPrefix(thinkingContent, ">")
		thinkingContent = strings.TrimLeft(thinkingContent, " \t\r\n")
		if thinkingContent != "" {
			p.events = append(p.events, openAIChatParserEvent{Type: "thinking", Content: thinkingContent})
		}
	}

	p.tryEmitInvokes(true)
	p.events = append(p.events, openAIChatParserEvent{Type: "end"})

	p.buffer = nil
	p.captureBuffer = nil
	p.capturing = false
	p.thinkingBuffer = nil
	p.thinkingMode = false
	p.thinkingEndRunes = nil
}

func (p *openAIChatToolifyParser) ConsumeEvents() []openAIChatParserEvent {
	if len(p.events) == 0 {
		return nil
	}
	out := make([]openAIChatParserEvent, len(p.events))
	copy(out, p.events)
	p.events = p.events[:0]
	return out
}

func (p *openAIChatToolifyParser) flushTextBufferIfNeeded() {
	// In tool mode, keep a small tail so we don't lose trigger detection across flush boundaries.
	keepTail := 0
	if len(p.triggerRunes) > 0 {
		keepTail = len(p.triggerRunes) - 1
	}
	if p.thinkingEnabled {
		for _, tag := range openAIChatThinkingTags {
			if n := len(tag.Start) - 1; n > keepTail {
				keepTail = n
			}
		}
	}
	if keepTail < 0 {
		keepTail = 0
	}

	const flushThreshold = 256
	if len(p.buffer) < flushThreshold+keepTail {
		return
	}

	flushLen := len(p.buffer) - keepTail
	if flushLen <= 0 {
		return
	}
	p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: string(p.buffer[:flushLen])})
	p.buffer = append(p.buffer[:0], p.buffer[flushLen:]...)
}

func (p *openAIChatToolifyParser) tryEmitInvokes(force bool) {
	lower := strings.ToLower(string(p.captureBuffer))
	startIdx := strings.Index(lower, "<invoke")
	if startIdx == -1 {
		if !force {
			return
		}
		if len(p.captureBuffer) > 0 {
			p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: string(p.captureBuffer)})
		}
		p.captureBuffer = p.captureBuffer[:0]
		p.capturing = false
		return
	}

	captureStr := string(p.captureBuffer)
	endIdx := strings.Index(strings.ToLower(captureStr[startIdx:]), "</invoke>")
	if endIdx == -1 {
		return
	}
	endIdx = startIdx + endIdx
	endPos := endIdx + len("</invoke>")
	invokeXML := captureStr[startIdx:endPos]
	afterInvoke := captureStr[endPos:]
	afterTrimmed := strings.TrimLeft(afterInvoke, " \t\r\n")

	if afterTrimmed != "" && !strings.HasPrefix(strings.ToLower(afterTrimmed), "<invoke") && !force {
		// Not a tool call sequence; fall back to plain text.
		p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: captureStr})
		p.captureBuffer = p.captureBuffer[:0]
		p.capturing = false
		return
	}

	before := captureStr[:startIdx]
	// Trim leading whitespace after trigger signal
	before = strings.TrimLeft(before, " \t\r\n")
	if before != "" {
		p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: before})
	}

	call := parseOpenAIChatInvokeXML(invokeXML)
	if call != nil {
		p.events = append(p.events, openAIChatParserEvent{Type: "tool_call", Call: call})

		// Filter out subsequent invoke tags; keep non-tool-call text.
		remaining := afterInvoke
		filtered := ""
		for {
			trimmed := strings.TrimLeft(remaining, " \t\r\n")
			if trimmed == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "<invoke") {
				nextEnd := strings.Index(strings.ToLower(trimmed), "</invoke>")
				if nextEnd != -1 {
					remaining = trimmed[nextEnd+len("</invoke>"):]
					continue
				}
			}
			filtered = remaining
			break
		}

		if strings.TrimSpace(filtered) != "" {
			p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: filtered})
		}
	} else {
		p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: captureStr})
	}

	p.captureBuffer = p.captureBuffer[:0]
	p.capturing = false
}

func (p *openAIChatToolifyParser) handleRuneWithoutTrigger(r rune) {
	if !p.thinkingEnabled {
		p.buffer = append(p.buffer, r)
		if len(p.buffer) >= 256 {
			p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: string(p.buffer)})
			p.buffer = p.buffer[:0]
		}
		return
	}

	if p.thinkingMode {
		p.thinkingBuffer = append(p.thinkingBuffer, r)
		if len(p.thinkingEndRunes) > 0 && hasSuffixRunes(p.thinkingBuffer, p.thinkingEndRunes) {
			thinkingContent := string(p.thinkingBuffer[:len(p.thinkingBuffer)-len(p.thinkingEndRunes)])
			thinkingContent = strings.TrimLeft(thinkingContent, " \t\r\n")
			thinkingContent = strings.TrimPrefix(thinkingContent, ">")
			thinkingContent = strings.TrimLeft(thinkingContent, " \t\r\n")
			if thinkingContent != "" {
				p.events = append(p.events, openAIChatParserEvent{Type: "thinking", Content: thinkingContent})
			}
			p.thinkingBuffer = p.thinkingBuffer[:0]
			p.thinkingMode = false
			p.thinkingEndRunes = nil
		}
		return
	}

	p.buffer = append(p.buffer, r)
	for _, tag := range openAIChatThinkingTags {
		if hasSuffixRunes(p.buffer, tag.Start) {
			textPortion := string(p.buffer[:len(p.buffer)-len(tag.Start)])
			if textPortion != "" {
				p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: textPortion})
			}
			p.buffer = p.buffer[:0]
			p.thinkingMode = true
			p.thinkingEndRunes = tag.End
			p.thinkingBuffer = p.thinkingBuffer[:0]
			return
		}
	}

	maxKeepTail := 0
	for _, tag := range openAIChatThinkingTags {
		if n := len(tag.Start) - 1; n > maxKeepTail {
			maxKeepTail = n
		}
	}
	const flushThreshold = 256
	if len(p.buffer) < flushThreshold+maxKeepTail {
		return
	}
	flushLen := len(p.buffer) - maxKeepTail
	if flushLen <= 0 {
		return
	}
	p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: string(p.buffer[:flushLen])})
	p.buffer = append(p.buffer[:0], p.buffer[flushLen:]...)
}

func (p *openAIChatToolifyParser) maybeEnterThinking(r rune) bool {
	if p.thinkingMode || !p.thinkingEnabled {
		return false
	}
	for _, tag := range openAIChatThinkingTags {
		tagLen := len(tag.Start)
		if tagLen == 0 {
			continue
		}
		if len(p.buffer) < tagLen-1 {
			continue
		}
		start := len(p.buffer) - (tagLen - 1)
		matched := true
		for i := 0; i < tagLen-1; i++ {
			if p.buffer[start+i] != tag.Start[i] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if r != tag.Start[tagLen-1] {
			continue
		}

		textPortion := string(p.buffer[:start])
		if textPortion != "" {
			p.events = append(p.events, openAIChatParserEvent{Type: "text", Content: textPortion})
		}
		p.buffer = p.buffer[:0]
		p.thinkingMode = true
		p.thinkingEndRunes = tag.End
		p.thinkingBuffer = p.thinkingBuffer[:0]
		p.thinkingBuffer = append(p.thinkingBuffer, r)
		return true
	}
	return false
}

func parseOpenAIChatInvokeXML(xml string) *openAIChatParsedInvokeCall {
	m := openAIChatInvokeNameRe.FindStringSubmatch(xml)
	if len(m) < 2 {
		return nil
	}
	name := m[1]
	params := map[string]interface{}{}
	matches := openAIChatParamRe.FindAllStringSubmatch(xml, -1)
	for _, mm := range matches {
		if len(mm) < 3 {
			continue
		}
		key := mm[1]
		rawValue := strings.TrimSpace(mm[2])
		if rawValue == "" {
			params[key] = ""
			continue
		}
		var val interface{}
		if err := json.Unmarshal([]byte(rawValue), &val); err == nil {
			params[key] = val
		} else {
			params[key] = rawValue
		}
	}

	return &openAIChatParsedInvokeCall{Name: name, Arguments: params}
}

func hasSuffixRunes(buf []rune, suffix []rune) bool {
	if len(suffix) == 0 {
		return true
	}
	if len(buf) < len(suffix) {
		return false
	}
	start := len(buf) - len(suffix)
	for i := range suffix {
		if buf[start+i] != suffix[i] {
			return false
		}
	}
	return true
}
