package continuethinking

import (
	"maps"
	"strconv"
)

// bufferEntry holds the tentative events + final item for a buffered
// (message | function_call) output item of one round.
type bufferEntry struct {
	oi    any
	itype string
	item  map[string]any
	objs  []map[string]any
}

func findBuffer(buf []bufferEntry, oi any) *bufferEntry {
	for i := range buf {
		if buf[i].oi == oi {
			return &buf[i]
		}
	}
	return nil
}

func isTerminal(t string) bool {
	switch t {
	case "response.completed", "response.failed", "response.incomplete", "response.done":
		return true
	}
	return false
}

// IsTerminalType reports whether an event type is a round-terminating event.
// Exported so non-SSE drivers (e.g. the WebSocket transport) can break their
// read loop on the same set the fold engine treats as terminal.
func IsTerminalType(t string) bool { return isTerminal(t) }

func reasoningAsAny(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	out = append(out, toAnySlice(items)...)
	return out
}

func toAnySlice(items []map[string]any) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}

// flushEntryObjs rewrites a buffered (message | function_call) item's events
// for downstream emission (output_index/sequence_number) and returns them in
// order. rechunk is hardcoded off, so original deltas pass through unchanged.
func (st *foldState) flushEntryObjs(entry bufferEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entry.objs))
	for _, obj := range entry.objs {
		if _, ok := obj["output_index"]; ok {
			obj["output_index"] = st.dsOI
		}
		obj["sequence_number"] = st.seq.next()
		out = append(out, obj)
	}
	return out
}

// agentUsage builds the usage as if the fold were ONE response, for the
// downstream agent:
//   - input / cached = round 1 (what the agent actually sent)
//   - reasoning = summed across rounds
//   - output = reasoning + final flushed round's non-reasoning part
//   - total = input + output
func (st *foldState) agentUsage(flushedFinal bool, finalRound *RoundUsage) map[string]any {
	inTok := 0
	cached := -1
	if st.firstUsage != nil {
		inTok = st.firstUsage.InputTokens
		cached = st.firstUsage.CachedTokens
	}
	reasoning := st.totalUsage.ReasoningTokens

	finalNonReason := 0
	if flushedFinal && finalRound != nil {
		finalNonReason = max(0, finalRound.OutputTokens-finalRound.ReasoningTokens)
	}
	outTok := reasoning + finalNonReason

	usage := map[string]any{
		"input_tokens":  inTok,
		"output_tokens": outTok,
		"total_tokens":  inTok + outTok,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": reasoning,
		},
	}
	if cached >= 0 {
		usage["input_tokens_details"] = map[string]any{"cached_tokens": cached}
	}
	return usage
}

// reconstructTerminal builds the final downstream terminal event: keep round-1's
// response identity (id/created_at), take status from the upstream terminal, and
// supply the reconstructed output + single-response usage + proxy metadata.
func (st *foldState) reconstructTerminal(terminal map[string]any, status string, flushedFinal bool, stoppedReason string) map[string]any {
	tresp, _ := terminal["response"].(map[string]any)
	resp := map[string]any{}
	switch {
	case st.baseResponse != nil:
		maps.Copy(resp, st.baseResponse)
	case tresp != nil:
		maps.Copy(resp, tresp)
	}
	resp["output"] = st.finalOutput
	resp["usage"] = st.agentUsage(flushedFinal, st.lastRoundUsage)
	if status != "" {
		resp["status"] = status
	}
	if tresp != nil {
		if ind, ok := tresp["incomplete_details"]; ok {
			resp["incomplete_details"] = ind
		}
	}

	withProxyMetadata(resp, st.roundsInfo, stoppedReason, st.billedUsage())

	ttype, _ := terminal["type"].(string)
	if ttype == "" {
		ttype = "response.completed"
	}
	return map[string]any{
		"type":            ttype,
		"response":        resp,
		"sequence_number": st.seq.next(),
	}
}

// syntheticIncomplete emits a response.incomplete terminal when continuation is
// stopped by a guard or error.
func (st *foldState) syntheticIncomplete(reason string) map[string]any {
	resp := map[string]any{}
	if st.baseResponse != nil {
		maps.Copy(resp, st.baseResponse)
	}
	resp["output"] = st.finalOutput
	resp["usage"] = st.agentUsage(false, nil)
	resp["status"] = "incomplete"
	resp["incomplete_details"] = map[string]any{"reason": reason}
	withProxyMetadata(resp, st.roundsInfo, reason, st.billedUsage())
	return map[string]any{
		"type":            "response.incomplete",
		"response":        resp,
		"sequence_number": st.seq.next(),
	}
}

func (st *foldState) billedUsage() map[string]any {
	return map[string]any{
		"input_tokens":  st.totalUsage.InputTokens,
		"output_tokens": st.totalUsage.OutputTokens,
		"total_tokens":  st.totalUsage.TotalTokens,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": st.totalUsage.ReasoningTokens,
		},
	}
}

func withProxyMetadata(resp map[string]any, rounds []map[string]any, stoppedReason string, billed map[string]any) {
	md, ok := resp["metadata"].(map[string]any)
	merged := map[string]any{}
	if ok {
		maps.Copy(merged, md)
	}
	merged["proxy_rounds"] = rounds
	merged["proxy_billed_usage"] = billed
	if stoppedReason != "" {
		merged["proxy_stopped_reason"] = stoppedReason
	}
	resp["metadata"] = merged
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func itoa(i int) string { return strconv.Itoa(i) }

// roundReasoningEffort extracts the `reasoning.effort` string from the base
// request body. Continuation rounds inherit the round-1 effort (the fold never
// changes it), so reading from BaseBody once per round suffices.
func roundReasoningEffort(baseBody map[string]any) string {
	if baseBody == nil {
		return ""
	}
	if r, ok := baseBody["reasoning"].(map[string]any); ok {
		if eff, ok := r["effort"].(string); ok {
			return eff
		}
	}
	return ""
}
