package continuethinking

import "testing"

func TestIsTruncationPattern(t *testing.T) {
	cases := []struct {
		tokens int
		want   bool
	}{
		{516, true},   // n=1, the common Codex cap
		{1034, true},  // n=2
		{1552, true},  // n=3
		{2070, true},  // n=4
		{2588, true},  // n=5
		{3106, true},  // n=6
		{3624, true},  // n=7
		{500, false},  // natural finish
		{520, false},
		{1000, false},
		{515, false},  // just below first trigger
		{0, false},
		{-1, false},
	}
	for _, c := range cases {
		if got := IsTruncationPattern(c.tokens); got != c.want {
			t.Errorf("IsTruncationPattern(%d) = %v, want %v", c.tokens, got, c.want)
		}
	}
}

func TestTierN(t *testing.T) {
	cases := []struct {
		tokens int
		want   int
	}{
		{516, 1},
		{1034, 2},
		{1552, 3},
		{500, 0},
		{0, 0},
	}
	for _, c := range cases {
		if got := TierN(c.tokens); got != c.want {
			t.Errorf("TierN(%d) = %d, want %d", c.tokens, got, c.want)
		}
	}
}

func TestShouldContinue(t *testing.T) {
	cases := []struct {
		tokens int
		want   bool
	}{
		{516, true},   // n=1, within [1,6]
		{1034, true},  // n=2
		{3106, true},  // n=6 (boundary, inclusive)
		{3624, false}, // n=7 > MaxN=6
		{500, false},  // not truncated
		{0, false},
	}
	for _, c := range cases {
		if got := ShouldContinue(c.tokens); got != c.want {
			t.Errorf("ShouldContinue(%d) = %v, want %v", c.tokens, got, c.want)
		}
	}
}
