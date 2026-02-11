package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/JillVernus/cc-bridge/internal/config"
)

// contentFilterResult holds the result of a content filter check.
type contentFilterResult struct {
	Matched        bool   // Whether a keyword was matched
	MatchedKeyword string // The keyword that matched
	AssembledText  string // The assembled text from the response
}

// checkContentFilterOnBody checks a response body (already read) for content filter keywords.
// Works for both non-streaming JSON responses and buffered SSE streams.
func checkContentFilterOnBody(bodyBytes []byte, filter *config.ContentFilter) contentFilterResult {
	if filter == nil || !filter.Enabled || len(filter.Keywords) == 0 {
		return contentFilterResult{}
	}

	text := extractTextFromBody(bodyBytes)
	if text == "" {
		return contentFilterResult{}
	}

	return matchKeywords(text, filter.Keywords)
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

	if filter == nil || !filter.Enabled || len(filter.Keywords) == 0 {
		return contentFilterResult{}, bodyBytes, nil
	}

	text := extractTextFromSSE(bodyBytes)
	if text == "" {
		return contentFilterResult{}, bodyBytes, nil
	}

	result := matchKeywords(text, filter.Keywords)
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

// matchKeywords checks text against a list of keywords (case-insensitive substring match).
func matchKeywords(text string, keywords []string) contentFilterResult {
	lowerText := strings.ToLower(text)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lowerText, strings.ToLower(kw)) {
			log.Printf("🚫 Content filter matched keyword: %q in response text", kw)
			return contentFilterResult{
				Matched:        true,
				MatchedKeyword: kw,
				AssembledText:  text,
			}
		}
	}
	return contentFilterResult{AssembledText: text}
}
