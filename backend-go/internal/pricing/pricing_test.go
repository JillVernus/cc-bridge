package pricing

import (
	"testing"
)

func TestApplyFastModeMultiplier_NotFastMode(t *testing.T) {
	orig := &PriceMultipliers{
		InputMultiplier:         1.5,
		OutputMultiplier:        1.5,
		CacheCreationMultiplier: 1.5,
		CacheReadMultiplier:     1.5,
	}
	result := ApplyFastModeMultiplier(orig, false)
	if result != orig {
		t.Error("expected same pointer when isFastMode is false")
	}
}

func TestApplyFastModeMultiplier_NilNotFast(t *testing.T) {
	result := ApplyFastModeMultiplier(nil, false)
	if result != nil {
		t.Error("expected nil when input is nil and isFastMode is false")
	}
}

func TestApplyFastModeMultiplier_NilFastMode(t *testing.T) {
	result := ApplyFastModeMultiplier(nil, true)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.InputMultiplier != 2.0 || result.OutputMultiplier != 2.0 ||
		result.CacheCreationMultiplier != 2.0 || result.CacheReadMultiplier != 2.0 {
		t.Errorf("expected all 2.0, got input=%v output=%v cacheCreation=%v cacheRead=%v",
			result.InputMultiplier, result.OutputMultiplier,
			result.CacheCreationMultiplier, result.CacheReadMultiplier)
	}
}

func TestApplyFastModeMultiplier_WithExistingMultipliers(t *testing.T) {
	orig := &PriceMultipliers{
		InputMultiplier:         1.5,
		OutputMultiplier:        2.0,
		CacheCreationMultiplier: 1.0,
		CacheReadMultiplier:     0.5,
	}
	result := ApplyFastModeMultiplier(orig, true)
	if result == orig {
		t.Error("expected new pointer when isFastMode is true")
	}
	if result.InputMultiplier != 3.0 {
		t.Errorf("InputMultiplier: expected 3.0, got %v", result.InputMultiplier)
	}
	if result.OutputMultiplier != 4.0 {
		t.Errorf("OutputMultiplier: expected 4.0, got %v", result.OutputMultiplier)
	}
	if result.CacheCreationMultiplier != 2.0 {
		t.Errorf("CacheCreationMultiplier: expected 2.0, got %v", result.CacheCreationMultiplier)
	}
	if result.CacheReadMultiplier != 1.0 {
		t.Errorf("CacheReadMultiplier: expected 1.0, got %v", result.CacheReadMultiplier)
	}
}

func TestApplyFastModeMultiplier_ZeroMultipliersTreatedAsOne(t *testing.T) {
	orig := &PriceMultipliers{
		InputMultiplier:         0,
		OutputMultiplier:        0,
		CacheCreationMultiplier: 0,
		CacheReadMultiplier:     0,
	}
	result := ApplyFastModeMultiplier(orig, true)
	// Zero multipliers are treated as 1.0 by getEffectiveMultiplier, so 1.0 * 2.0 = 2.0
	if result.InputMultiplier != 2.0 || result.OutputMultiplier != 2.0 ||
		result.CacheCreationMultiplier != 2.0 || result.CacheReadMultiplier != 2.0 {
		t.Errorf("expected all 2.0 (zero treated as 1.0), got input=%v output=%v cacheCreation=%v cacheRead=%v",
			result.InputMultiplier, result.OutputMultiplier,
			result.CacheCreationMultiplier, result.CacheReadMultiplier)
	}
}
