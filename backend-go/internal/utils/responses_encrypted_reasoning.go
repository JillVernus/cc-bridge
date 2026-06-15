package utils

const ResponsesEncryptedReasoningInclude = "reasoning.encrypted_content"

// EnsureResponsesEncryptedReasoningInclude appends the encrypted reasoning
// include flag without changing the request's stream mode.
func EnsureResponsesEncryptedReasoningInclude(req map[string]interface{}) bool {
	if req == nil {
		return false
	}

	rawInclude, exists := req["include"]
	if !exists || rawInclude == nil {
		req["include"] = []interface{}{ResponsesEncryptedReasoningInclude}
		return true
	}

	include, ok := rawInclude.([]interface{})
	if !ok {
		req["include"] = []interface{}{ResponsesEncryptedReasoningInclude}
		return true
	}

	for _, item := range include {
		if value, ok := item.(string); ok && value == ResponsesEncryptedReasoningInclude {
			return false
		}
	}

	req["include"] = append(include, ResponsesEncryptedReasoningInclude)
	return true
}
