package continuethinking

import "maps"

// CommentaryMessage builds a single phase:"commentary" assistant message — the
// clean continuation provocation (the default, replacing a synthetic tool pair).
// `phase` is an official Responses-API field; it carries no synthetic tool, so
// it is safe to inject. Verified live to re-ingest the replayed reasoning and
// defeat the 518n-2 truncation identically to the legacy tool-pair method.
func CommentaryMessage(text string) map[string]any {
	return map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []any{
			map[string]any{
				"type": "output_text",
				"text": text,
			},
		},
		"phase": "commentary",
	}
}

// BuildRoundPayload takes the agent's request body and shapes it for one upstream
// round. It never invents model/instructions/reasoning/tools — those are the
// agent's. It only: forces stream=true (we always stream upstream), ensures
// encrypted reasoning is in `include`, sets the round's `input`, and (on
// continuation rounds) drops `previous_response_id` since we carry state
// explicitly in `input`.
//
// baseBody is not mutated; a shallow copy is returned with overridden keys.
func BuildRoundPayload(baseBody map[string]any, inputItems []any, forceEncrypted bool) map[string]any {
	body := make(map[string]any, len(baseBody)+2)
	maps.Copy(body, baseBody)
	body["stream"] = true
	body["input"] = inputItems

	if _, hasInclude := body["include"]; hasInclude || forceEncrypted {
		body["include"] = mergeInclude(body["include"], forceEncrypted)
	}
	// We carry state explicitly in `input`, so a server-side previous_response_id
	// chain is dropped on continuation rounds.
	delete(body, "previous_response_id")
	return body
}

// mergeInclude returns the include array with encrypted reasoning appended when
// forceEncrypted is true and it is not already present.
func mergeInclude(include any, forceEncrypted bool) []any {
	items := []any{}
	if list, ok := include.([]any); ok {
		items = append(items, list...)
	}
	if !forceEncrypted {
		return items
	}
	hasEnc := false
	for _, it := range items {
		if s, ok := it.(string); ok && s == ReasoningTokensInclude {
			hasEnc = true
			break
		}
	}
	if !hasEnc {
		items = append(items, ReasoningTokensInclude)
	}
	return items
}
