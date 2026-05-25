package handlers

import (
	"encoding/json"
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
			case "message", "function_call", "function_call_output":
				filtered = append(filtered, sanitizeResponsesCompatibilityInputItem(itemMap, itemType))
			default:
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

func sanitizeResponsesCompatibilityInputItem(item map[string]interface{}, itemType string) map[string]interface{} {
	allowed := map[string]bool{"type": true}
	switch itemType {
	case "message":
		allowed["role"] = true
		allowed["content"] = true
		allowed["status"] = true
	case "function_call":
		allowed["call_id"] = true
		allowed["name"] = true
		allowed["arguments"] = true
		allowed["status"] = true
	case "function_call_output":
		allowed["call_id"] = true
		allowed["output"] = true
		allowed["status"] = true
	}

	filtered := make(map[string]interface{}, len(allowed))
	for key, value := range item {
		if allowed[key] {
			filtered[key] = value
		}
	}
	return filtered
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
