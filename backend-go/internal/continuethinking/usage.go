package continuethinking

import (
	"encoding/json"
	"net/http"
	"time"
)

// ReasoningTokensInclude is the include-array entry that asks the upstream to
// return encrypted reasoning content (needed to replay reasoning across rounds).
const ReasoningTokensInclude = "reasoning.encrypted_content"

// RoundUsage is the per-round upstream usage reported to the logging callback.
type RoundUsage struct {
	Round                int
	InputTokens          int
	OutputTokens         int
	CachedTokens         int
	TotalTokens          int
	ReasoningTokens      int // 0 if absent
	ResponseModel        string
	HTTPStatus           int
	Status               string    // "completed" | "error" | "incomplete"
	Truncated            bool      // true iff this round hit the 518n-2 fingerprint
	N                    int       // truncation tier (0 if not truncated)
	ContinueThinkingRole string    // "hold" for continued rounds, "folded_return" for the final folded round
	FirstEventAt         time.Time // wall-clock time the round's first `data:` payload was read (first-token proxy signal); zero if none
	RequestSentAt        time.Time // wall-clock time the round's upstream request was dispatched (TTFT base); zero if unknown
	RoundCompleteAt      time.Time // wall-clock time the round's upstream stream finished

	// Trace carries the per-round request/response artifacts for debug logging.
	// RequestBody is the serialized upstream request body for the round;
	// ResponseBody is the raw upstream SSE response bytes; ResponseStatus/
	// ResponseHeaders mirror the upstream HTTP response. ReasoningEffort is the
	// round's `reasoning.effort` (continuation rounds inherit round-1's value).
	Trace RoundTrace
}

// RoundTrace holds the per-round request/response artifacts reported to the
// logging/debug callback. The fold populates this so the handler can persist a
// debug log per folded round (mirroring non-fold channels).
type RoundTrace struct {
	RequestBody     []byte
	ResponseBody    []byte
	ResponseStatus  int
	ResponseHeaders http.Header
	ReasoningEffort string
}

// extractUsage pulls the usage block out of a terminal-event JSON object
// (the decoded payload of a `response.completed`/`response.failed`/
// `response.incomplete` event). It reads
// response.usage.{input_tokens,output_tokens,total_tokens,
// input_tokens_details.cached_tokens,
// output_tokens_details.reasoning_tokens} and response.model.
//
// Returns (usage, responseModel, ok). ok is false when there is no usage block.
func extractUsage(event map[string]any) (RoundUsage, string, bool) {
	resp, _ := event["response"].(map[string]any)
	if resp == nil {
		return RoundUsage{}, "", false
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage == nil {
		return RoundUsage{}, asString(resp["model"]), false
	}
	u := RoundUsage{
		InputTokens:  asInt(usage["input_tokens"]),
		OutputTokens: asInt(usage["output_tokens"]),
		TotalTokens:  asInt(usage["total_tokens"]),
	}
	if itd, ok := usage["input_tokens_details"].(map[string]any); ok {
		u.CachedTokens = asInt(itd["cached_tokens"])
	}
	if otd, ok := usage["output_tokens_details"].(map[string]any); ok {
		u.ReasoningTokens = asInt(otd["reasoning_tokens"])
	}
	u.Truncated = IsTruncationPattern(u.ReasoningTokens)
	u.N = TierN(u.ReasoningTokens)
	return u, asString(resp["model"]), true
}

// reasoningTokensFromItem returns the encrypted_content of a reasoning output
// item, if present (used to decide whether a truncated round can be replayed).
func reasoningHasEncryptedContent(item map[string]any) bool {
	if item == nil {
		return false
	}
	enc, _ := item["encrypted_content"].(string)
	return enc != ""
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
