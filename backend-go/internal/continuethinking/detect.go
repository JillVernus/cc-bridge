// Package continuethinking implements a "continue-thinking" fold for Codex /
// OpenAI Responses-compatible upstreams.
//
// OpenAI Codex hard-caps the reasoning token budget, forcing a truncation at
// reasoning_tokens == TruncationStep*n - 2 (516, 1034, 1552, ...). This package
// detects that fingerprint, silently asks the model to continue thinking, and
// folds multiple upstream streaming rounds into one downstream SSE response.
//
// Ported from sample_source/CodexCont (Python). The whole feature is isolated
// in this package so it can be removed wholesale; handlers only call FoldStream.
package continuethinking

// Hardcoded continuation configuration (CodexCont defaults). These are package
// consts rather than channel config so the feature surface stays minimal and
// removal is a pure delete.
const (
	// TruncationStep is the step of the 518n-2 truncation fingerprint.
	TruncationStep = 518
	// MaxContinue is the hard round cap after round 1 (primary runaway guard).
	MaxContinue = 8
	// MinN is the minimum truncation tier n that triggers continuation.
	MinN = 1
	// MaxN is the tier cap: stop forcing continuation once n > MaxN. 0 = uncapped.
	MaxN = 6
	// MaxTotalOutputTokens caps cumulative output tokens across rounds. 0 = off.
	MaxTotalOutputTokens = 0
	// ForwardMarker (commentary method): surface the marker downstream so the
	// agent echoes it back next turn. false = hidden/clean agent history.
	ForwardMarker = false
	// RechunkFinalAnswer re-slices the buffered final message into uniform deltas.
	// false = pass original deltas through unchanged.
	RechunkFinalAnswer = false
	// RechunkSize is the delta slice size when RechunkFinalAnswer is true.
	RechunkSize = 16
	// MarkerText is the commentary assistant message text nudging continuation.
	MarkerText = "Continue thinking..."
)

// IsTruncationPattern reports whether tokens lands exactly on
// TruncationStep*n - 2 (516, 1034, ...). This is the Codex hard-cap signature.
func IsTruncationPattern(tokens int) bool {
	return tokens >= TruncationStep-2 && (tokens+2)%TruncationStep == 0
}

// TierN returns the tier n for a truncation-pattern token count, or 0 if the
// count is not on the pattern. (516 => 1, 1034 => 2, ...)
func TierN(tokens int) int {
	if !IsTruncationPattern(tokens) {
		return 0
	}
	return (tokens + 2) / TruncationStep
}

// ShouldContinue reports whether continuation should be attempted for the given
// reasoning token count: truncated AND MinN <= tier n <= MaxN (MaxN=0 uncapped).
func ShouldContinue(tokens int) bool {
	n := TierN(tokens)
	if n == 0 {
		return false
	}
	if n < MinN {
		return false
	}
	if MaxN != 0 && n > MaxN {
		return false
	}
	return true
}
