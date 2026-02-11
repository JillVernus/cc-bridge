package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
)

const defaultContentFilterStatusCode = 429

// contentFilterResult holds the result of a content filter check.
type contentFilterResult struct {
	Matched           bool   // Whether a keyword was matched
	MatchedKeyword    string // The keyword that matched
	MatchedStatusCode int    // The HTTP status code to synthesize on match
	AssembledText     string // The assembled text from the response
}

// checkContentFilterOnBody checks a response body (already read) for content filter keywords.
// Works for both non-streaming JSON responses and buffered SSE streams.
func checkContentFilterOnBody(bodyBytes []byte, filter *config.ContentFilter) contentFilterResult {
	rules := resolveContentFilterRules(filter)
	if filter == nil || !filter.Enabled || len(rules) == 0 {
		return contentFilterResult{}
	}

	text := extractTextFromBody(bodyBytes)
	if text == "" {
		return contentFilterResult{}
	}

	return matchRules(text, rules)
}

// checkContentFilterOnStream buffers an SSE stream, assembles text from content_block_delta
// events, and checks for content filter keywords. Returns the buffered body bytes so the
// caller can restore the response body for normal processing if no match is found.
func checkContentFilterOnStream(resp *http.Response, filter *config.ContentFilter) (contentFilterResult, []byte, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return contentFilterResult{}, nil, err
	}

	rules := resolveContentFilterRules(filter)
	if filter == nil || !filter.Enabled || len(rules) == 0 {
		return contentFilterResult{}, bodyBytes, nil
	}

	text := extractTextFromSSE(bodyBytes)
	if text == "" {
		return contentFilterResult{}, bodyBytes, nil
	}

	result := matchRules(text, rules)
	return result, bodyBytes, nil
}

// extractTextFromBody extracts text content from a JSON response body.
// Handles Claude response format with content[].text fields.
func extractTextFromBody(bodyBytes []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return ""
	}

	var sb strings.Builder

	// Check content array (Claude format)
	if content, ok := resp["content"].([]interface{}); ok {
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if text, ok := blockMap["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
	}

	// Also check top-level error field
	if errField, ok := resp["error"]; ok {
		if errStr, ok := errField.(string); ok {
			sb.WriteString(errStr)
		} else if errMap, ok := errField.(map[string]interface{}); ok {
			if msg, ok := errMap["message"].(string); ok {
				sb.WriteString(msg)
			}
		}
	}

	return sb.String()
}

// extractTextFromSSE parses SSE event data and extracts text from content_block_delta events.
func extractTextFromSSE(bodyBytes []byte) string {
	var sb strings.Builder
	lines := strings.Split(string(bodyBytes), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if text, ok := delta["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		case "content_block_start":
			if block, ok := event["content_block"].(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
	}

	return sb.String()
}

// resolveContentFilterRules normalizes configured rules and handles legacy fields fallback.
// Behavior: Rules (new format) are preferred; if empty, fallback to legacy Keywords + StatusCode.
func resolveContentFilterRules(filter *config.ContentFilter) []config.ContentFilterRule {
	if filter == nil {
		return nil
	}

	rules := make([]config.ContentFilterRule, 0, len(filter.Rules))

	// New format: rules[] where each keyword has its own status code.
	for _, rule := range filter.Rules {
		keyword := strings.TrimSpace(rule.Keyword)
		if keyword == "" {
			continue
		}
		rules = append(rules, config.ContentFilterRule{
			Keyword:    keyword,
			StatusCode: normalizeContentFilterStatusCode(rule.StatusCode),
		})
	}
	if len(rules) > 0 {
		return rules
	}

	// Legacy format fallback: keywords[] + single statusCode.
	legacyStatusCode := normalizeContentFilterStatusCode(filter.StatusCode)
	for _, keyword := range filter.Keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		rules = append(rules, config.ContentFilterRule{
			Keyword:    trimmed,
			StatusCode: legacyStatusCode,
		})
	}

	return rules
}

func normalizeContentFilterStatusCode(statusCode int) int {
	if statusCode < 400 || statusCode > 599 {
		return defaultContentFilterStatusCode
	}
	return statusCode
}

// matchRules checks text against rules (case-insensitive substring match).
// First matched rule wins.
func matchRules(text string, rules []config.ContentFilterRule) contentFilterResult {
	lowerText := strings.ToLower(text)
	for _, rule := range rules {
		keyword := strings.TrimSpace(rule.Keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(lowerText, strings.ToLower(keyword)) {
			statusCode := normalizeContentFilterStatusCode(rule.StatusCode)
			log.Printf("🚫 Content filter matched keyword: %q in response text, status=%d", keyword, statusCode)
			return contentFilterResult{
				Matched:           true,
				MatchedKeyword:    keyword,
				MatchedStatusCode: statusCode,
				AssembledText:     text,
			}
		}
	}
	return contentFilterResult{AssembledText: text}
}
