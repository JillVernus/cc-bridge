package continuethinking

import (
	"context"
	"log"
)

type foldState struct {
	cfg    FoldConfig
	driver Driver
	ctx    context.Context

	seq  seqCounter
	dsOI int

	baseResponse map[string]any
	sawDone      bool

	finalOutput    []map[string]any // reasoning (all rounds) + final flushed items
	totalUsage     totalUsage       // summed across rounds -> proxy_billed_usage
	firstUsage     *RoundUsage      // round-1 usage -> agent-facing input/cached
	lastRoundUsage *RoundUsage      // most recent round's usage -> final-round non-reasoning output
	replayTail     []any            // accumulates [*roundReasoning, commentaryMarker]
	roundsInfo     []map[string]any // per-round {round, reasoning_tokens, n}
}

type seqCounter struct{ n int }

func (s *seqCounter) next() int { v := s.n; s.n++; return v }

type totalUsage struct {
	InputTokens, OutputTokens, CachedTokens, TotalTokens, ReasoningTokens int
}

func (t *totalUsage) add(u RoundUsage) {
	t.InputTokens += u.InputTokens
	t.OutputTokens += u.OutputTokens
	t.CachedTokens += u.CachedTokens
	t.TotalTokens += u.TotalTokens
	t.ReasoningTokens += u.ReasoningTokens
}

func (st *foldState) logf(format string, args ...any) {
	if st.cfg.Log != nil {
		st.cfg.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// run drives the N-round fold over st.driver. Downstream output is delivered
// through driver.Emit / driver.EmitDone; nothing is returned.
func (st *foldState) run() {
	round := 0

	for {
		round++

		input, ok := st.driver.ReadRound(st.ctx)
		if !ok {
			st.driver.Emit(st.syntheticIncomplete("upstream_error"))
			return
		}
		events := input.Events
		firstPayloadAt := input.FirstPayloadAt
		roundCompleteAt := input.CompleteAt
		requestSentAt := input.RequestSentAt
		statusCode := input.StatusCode

		// Per-round accumulators.
		oiMap := map[any]int{}
		itemKind := map[any]string{}
		var outBuffer []bufferEntry
		var roundReasoning []map[string]any
		var terminal map[string]any
		var roundUsage *RoundUsage
		sawErrorEvent := false
		errorStatus := 0

		for _, ev := range events {
			if ev.Done {
				st.sawDone = true
				continue
			}
			obj := ev.Data
			if obj == nil {
				continue
			}
			t, _ := obj["type"].(string)

			// Bare upstream `error` frame (credit exhausted / rate limited /
			// auth failure mid-connection). Not a response.* terminal: forward
			// it downstream verbatim so the client sees the real error, finalize
			// the round log as an error, and stop the fold.
			if isErrorEvent(t) {
				sawErrorEvent = true
				errorStatus = errorStatusFromEvent(obj, statusCode)
				st.driver.Emit(obj)
				u, model, _ := extractUsage(obj)
				u.Round = round
				u.Status = "error"
				u.HTTPStatus = errorStatus
				u.ResponseModel = model
				u.FirstEventAt = firstPayloadAt
				u.RequestSentAt = requestSentAt
				u.RoundCompleteAt = roundCompleteAt
				u.Trace = RoundTrace{
					RequestBody:     input.RequestBody,
					ResponseBody:    input.ResponseBody,
					ResponseStatus:  errorStatus,
					ResponseHeaders: input.ResponseHeaders,
					ReasoningEffort: roundReasoningEffort(st.cfg.BaseBody),
				}
				roundUsage = &u
				break
			}

			// Lifecycle: emit created+in_progress on round 1 only.
			if t == "response.created" || t == "response.in_progress" {
				if round == 1 {
					if t == "response.created" {
						if r, ok := obj["response"].(map[string]any); ok {
							st.baseResponse = r
						}
					}
					obj["sequence_number"] = st.seq.next()
					st.driver.Emit(obj)
				}
				continue
			}

			if isTerminal(t) {
				terminal = obj
				u, model, _ := extractUsage(obj)
				u.Round = round
				if statusCode >= 400 {
					u.Status = "error"
				} else {
					u.Status = "completed"
				}
				u.HTTPStatus = statusCode
				u.ResponseModel = model
				u.FirstEventAt = firstPayloadAt
				u.RequestSentAt = requestSentAt
				u.RoundCompleteAt = roundCompleteAt
				u.Trace = RoundTrace{
					RequestBody:     input.RequestBody,
					ResponseBody:    input.ResponseBody,
					ResponseStatus:  input.StatusCode,
					ResponseHeaders: input.ResponseHeaders,
					ReasoningEffort: roundReasoningEffort(st.cfg.BaseBody),
				}
				roundUsage = &u
				break
			}

			upOI := obj["output_index"]

			if t == "response.output_item.added" {
				item, _ := obj["item"].(map[string]any)
				itype, _ := item["type"].(string)
				if itype == "reasoning" {
					itemKind[upOI] = "reasoning"
					oiMap[upOI] = st.dsOI
					obj["output_index"] = st.dsOI
					st.dsOI++
					obj["sequence_number"] = st.seq.next()
					st.driver.Emit(obj)
				} else { // message | function_call -> buffer (tentative)
					itemKind[upOI] = "buffered"
					outBuffer = append(outBuffer, bufferEntry{
						oi:    upOI,
						itype: itype,
						item:  item,
						objs:  []map[string]any{obj},
					})
				}
				continue
			}

			kind := itemKind[upOI]
			switch kind {
			case "reasoning":
				if mapped, ok := oiMap[upOI]; ok {
					obj["output_index"] = mapped
				}
				obj["sequence_number"] = st.seq.next()
				if t == "response.output_item.done" {
					if ritem, ok := obj["item"].(map[string]any); ok {
						roundReasoning = append(roundReasoning, ritem)
						st.finalOutput = append(st.finalOutput, ritem)
					}
				}
				st.driver.Emit(obj)
			case "buffered":
				if entry := findBuffer(outBuffer, upOI); entry != nil {
					entry.objs = append(entry.objs, obj)
					if t == "response.output_item.done" {
						if ritem, ok := obj["item"].(map[string]any); ok {
							entry.item = ritem
						}
					}
				}
			default:
				// Item-scoped event with no tracked added; forward best-effort.
				obj["sequence_number"] = st.seq.next()
				st.driver.Emit(obj)
			}
		}

		// --- round decision ---
		// A bare upstream error frame ends the turn immediately: the error was
		// already forwarded downstream; report usage for the log row and stop.
		// Never continue on an error (no reasoning to replay).
		if sawErrorEvent {
			if roundUsage != nil {
				st.totalUsage.add(*roundUsage)
				if round == 1 {
					st.firstUsage = roundUsage
				}
				st.lastRoundUsage = roundUsage
			}
			st.logf("continue-thinking round %d: upstream error frame (status=%d) -> stop", round, errorStatus)
			if st.cfg.OnRoundUsage != nil && roundUsage != nil {
				st.cfg.OnRoundUsage(round, *roundUsage)
			}
			if st.sawDone {
				st.driver.EmitDone()
			}
			return
		}

		sawTerminal := terminal != nil
		var rt int
		if roundUsage != nil {
			rt = roundUsage.ReasoningTokens
			st.totalUsage.add(*roundUsage)
			if round == 1 {
				st.firstUsage = roundUsage
			}
			st.lastRoundUsage = roundUsage
		}
		n := TierN(rt)
		st.roundsInfo = append(st.roundsInfo, map[string]any{
			"round":            round,
			"reasoning_tokens": rt,
			"n":                n,
		})

		hasEnc := len(roundReasoning) > 0 && reasoningHasEncryptedContent(roundReasoning[len(roundReasoning)-1])
		withinCaps := MaxTotalOutputTokens == 0 || st.totalUsage.OutputTokens < MaxTotalOutputTokens
		doContinue := sawTerminal && ShouldContinue(rt) && hasEnc && round <= MaxContinue && withinCaps

		// stoppedReason when we stop while still on the 518n-2 pattern.
		stoppedReason := ""
		if !doContinue && IsTruncationPattern(rt) {
			switch {
			case !hasEnc:
				stoppedReason = "no_encrypted_content"
			case round > MaxContinue:
				stoppedReason = "max_continue"
			case !withinCaps:
				stoppedReason = "max_total_output_tokens"
			default:
				stoppedReason = "tier_out_of_window"
			}
		}

		decision := "clean"
		switch {
		case doContinue:
			decision = "continue"
		case !sawTerminal:
			decision = "upstream_eof"
		case stoppedReason != "":
			decision = stoppedReason
		}
		st.logf("continue-thinking round %d: %s | n=%d -> %s", round, fmtUsage(roundUsage), n, decision)

		// Report per-round usage for logging (round 1 = existing log).
		if st.cfg.OnRoundUsage != nil && roundUsage != nil {
			st.cfg.OnRoundUsage(round, *roundUsage)
		}

		if doContinue {
			st.replayTail = append(st.replayTail, reasoningAsAny(roundReasoning)...)
			st.replayTail = append(st.replayTail, CommentaryMessage(MarkerText))

			// forward_marker is hardcoded false -> no downstream emit.

			payload := BuildRoundPayload(st.cfg.BaseBody,
				append(append([]any{}, st.cfg.OrigInput...), st.replayTail...),
				true) // force encrypted reasoning include
			if !st.driver.OpenContinuation(st.ctx, payload) {
				st.logf("continue-thinking: continuation round %d open failed", round+1)
				st.driver.Emit(st.syntheticIncomplete("upstream_error"))
				return
			}
			continue
		}

		// --- stop ---
		if !sawTerminal {
			// Upstream closed without a terminal event. Do NOT flush tentative output.
			st.driver.Emit(st.syntheticIncomplete("upstream_eof"))
			return
		}

		// Clean finish: flush this round's tentative output as the real answer.
		flushedFinal := false
		for _, entry := range outBuffer {
			flushedFinal = true
			for _, obj := range st.flushEntryObjs(entry) {
				st.driver.Emit(obj)
			}
			st.dsOI++
			if entry.item != nil {
				st.finalOutput = append(st.finalOutput, entry.item)
			}
		}

		status := "completed"
		if tresp, ok := terminal["response"].(map[string]any); ok {
			if s, ok := tresp["status"].(string); ok && s != "" {
				status = s
			}
		}
		st.driver.Emit(st.reconstructTerminal(terminal, status, flushedFinal, stoppedReason))
		if st.sawDone {
			st.driver.EmitDone()
		}
		st.logf("continue-thinking done: %d round(s) | status=%s stop=%s",
			round, status, orDefault(stoppedReason, "natural"))
		return
	}
}

func fmtUsage(u *RoundUsage) string {
	if u == nil {
		return "no-usage"
	}
	return "in=" + itoa(u.InputTokens) +
		" cached=" + itoa(u.CachedTokens) +
		" out=" + itoa(u.OutputTokens) +
		" reason=" + itoa(u.ReasoningTokens) +
		" total=" + itoa(u.TotalTokens)
}
