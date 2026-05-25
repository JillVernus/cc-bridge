package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
)

func sanitizeCodexResponsesCompatibilityRequest(bodyBytes []byte) ([]byte, bool, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, false, err
	}

	changed := false
	for _, key := range []string{
		"include",
		"client_metadata",
		"context_management",
		"output_config",
		"reasoning",
		"text",
	} {
		if _, ok := req[key]; ok {
			delete(req, key)
			changed = true
		}
	}

	if input, ok := req["input"].([]interface{}); ok {
		filtered := make([]interface{}, 0, len(input))
		for _, item := range input {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				filtered = append(filtered, item)
				continue
			}
			itemType, _ := itemMap["type"].(string)
			switch itemType {
			case "message":
				filtered = append(filtered, sanitizeResponsesCompatibilityMessage(itemMap))
			case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "tool_search_call", "tool_search_output":
				if message, ok := convertResponsesCompatibilityHistoryItemToMessage(itemMap, itemType); ok {
					filtered = append(filtered, message)
				}
				changed = true
			default:
				if itemType != "reasoning" {
					if message, ok := convertResponsesCompatibilityHistoryItemToMessage(itemMap, itemType); ok {
						filtered = append(filtered, message)
					}
				}
				changed = true
			}
		}
		req["input"] = filtered
	}

	if tools, ok := req["tools"].([]interface{}); ok {
		filtered := make([]interface{}, 0, len(tools))
		for _, tool := range tools {
			toolMap, ok := tool.(map[string]interface{})
			if !ok {
				changed = true
				continue
			}
			toolType, _ := toolMap["type"].(string)
			if toolType == "function" {
				filtered = append(filtered, tool)
			} else {
				changed = true
			}
		}
		req["tools"] = filtered
		if len(filtered) == 0 {
			delete(req, "tools")
			delete(req, "tool_choice")
			delete(req, "parallel_tool_calls")
		}
	}

	if !changed {
		return bodyBytes, false, nil
	}

	sanitized, err := json.Marshal(req)
	if err != nil {
		return nil, false, err
	}
	return sanitized, true, nil
}

func sanitizeResponsesCompatibilityMessage(item map[string]interface{}) map[string]interface{} {
	filtered := map[string]interface{}{
		"type": "message",
	}
	if role, _ := item["role"].(string); role == "assistant" || role == "user" || role == "system" {
		filtered["role"] = role
	} else {
		filtered["role"] = "user"
	}
	if content, ok := item["content"]; ok {
		filtered["content"] = content
	} else {
		filtered["content"] = []interface{}{map[string]interface{}{"type": "input_text", "text": ""}}
	}
	return filtered
}

func convertResponsesCompatibilityHistoryItemToMessage(item map[string]interface{}, itemType string) (map[string]interface{}, bool) {
	if itemType == "" {
		itemType = "unknown"
	}

	text := ""
	switch itemType {
	case "function_call":
		name, _ := item["name"].(string)
		arguments, _ := item["arguments"].(string)
		text = fmt.Sprintf("[tool call: %s]\n%s", name, arguments)
	case "function_call_output":
		output, _ := item["output"].(string)
		text = fmt.Sprintf("[tool output]\n%s", output)
	case "custom_tool_call":
		name, _ := item["name"].(string)
		input, _ := item["input"].(string)
		text = fmt.Sprintf("[custom tool call: %s]\n%s", name, input)
	case "custom_tool_call_output":
		output, _ := item["output"].(string)
		text = fmt.Sprintf("[custom tool output]\n%s", output)
	case "tool_search_call":
		text = "[tool search call]"
		if arguments, ok := item["arguments"]; ok {
			if encoded, err := json.Marshal(arguments); err == nil {
				text += "\n" + string(encoded)
			}
		}
	case "tool_search_output":
		text = "[tool search output]"
		if tools, ok := item["tools"]; ok {
			if encoded, err := json.Marshal(tools); err == nil {
				text += "\n" + string(encoded)
			}
		}
	default:
		if encoded, err := json.Marshal(item); err == nil {
			text = fmt.Sprintf("[%s]\n%s", itemType, string(encoded))
		}
	}

	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	return map[string]interface{}{
		"type": "message",
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "input_text",
				"text": text,
			},
		},
	}, true
}

func shouldRetryResponsesCompatibility(status int, bodyBytes []byte) bool {
	if status != 400 {
		return false
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return false
	}
	message := strings.ToLower(payload.Error.Message)
	return payload.Error.Type == "invalid_request_error" &&
		strings.Contains(message, "decode") &&
		strings.Contains(message, "responses") &&
		strings.Contains(message, "request")
}
