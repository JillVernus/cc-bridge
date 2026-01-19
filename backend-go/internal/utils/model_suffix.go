package utils

import "strings"

// SplitModelSuffix splits a model string of the form "base(suffix)" where "(suffix)"
// is the final segment of the string. It is intentionally permissive: it does not
// validate the suffix value, so callers can support pass-through for unknown suffixes.
//
// Examples:
// - "gpt-5.2(high)"  -> base="gpt-5.2", suffix="high", ok=true
// - "gpt-5.2()"      -> base="gpt-5.2", suffix="", ok=true
// - "gpt-5.2"        -> base="gpt-5.2", suffix="", ok=false
func SplitModelSuffix(model string) (base string, suffix string, ok bool) {
	model = strings.TrimSpace(model)
	if len(model) < 2 || !strings.HasSuffix(model, ")") {
		return model, "", false
	}
	openIdx := strings.LastIndex(model, "(")
	if openIdx <= 0 || openIdx >= len(model)-1 {
		return model, "", false
	}

	inner := model[openIdx+1 : len(model)-1]
	// Disallow nested parentheses in the suffix segment; treat as a normal model string.
	if strings.ContainsAny(inner, "()") {
		return model, "", false
	}

	return model[:openIdx], inner, true
}

// ApplyModelSuffix applies a suffix to a base model. Empty suffix means "no suffix".
// Whitespace is trimmed from both base and suffix.
func ApplyModelSuffix(base, suffix string) string {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if base == "" {
		return base
	}
	if suffix == "" {
		return base
	}
	return base + "(" + suffix + ")"
}

// NormalizeThinkingSuffix normalizes a reasoning/thinking suffix value.
// It lowercases and trims whitespace. It returns "" for empty/whitespace.
func NormalizeThinkingSuffix(suffix string) string {
	return strings.ToLower(strings.TrimSpace(suffix))
}

// IsSupportedThinkingSuffix returns true if suffix is one of: low, medium, high, xhigh.
// Empty string is considered supported (meaning "no suffix").
func IsSupportedThinkingSuffix(suffix string) bool {
	switch NormalizeThinkingSuffix(suffix) {
	case "", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}
