package config

import "testing"

func TestNormalizeDangerousOthersFailoverRules_ConvertsOthersFailover(t *testing.T) {
	rules := []FailoverRule{
		{
			ErrorCodes: "others",
			ActionChain: []ActionStep{
				{Action: ActionRetry, WaitSeconds: 5, MaxAttempts: 2},
				{Action: ActionFailover},
			},
		},
		{
			ErrorCodes:  "429",
			ActionChain: []ActionStep{{Action: ActionFailover}},
		},
	}

	normalized, changed := NormalizeDangerousOthersFailoverRules(rules)
	if changed != 1 {
		t.Fatalf("expected 1 changed rule, got %d", changed)
	}
	if normalized[0].ActionChain[0].Action != ActionRetry {
		t.Fatalf("expected first action to remain retry, got %q", normalized[0].ActionChain[0].Action)
	}
	if normalized[0].ActionChain[1].Action != ActionReturnError {
		t.Fatalf("expected others failover action converted to return_error, got %q", normalized[0].ActionChain[1].Action)
	}
	if normalized[1].ActionChain[0].Action != ActionFailover {
		t.Fatalf("expected non-others rule to remain failover, got %q", normalized[1].ActionChain[0].Action)
	}
}

func TestNormalizeDangerousOthersFailoverRules_ConvertsLegacyOthersAction(t *testing.T) {
	rules := []FailoverRule{
		{
			ErrorCodes: "others",
			Action:     ActionFailoverImmediate,
		},
	}

	normalized, changed := NormalizeDangerousOthersFailoverRules(rules)
	if changed != 1 {
		t.Fatalf("expected 1 changed legacy rule, got %d", changed)
	}
	if normalized[0].Action != "" {
		t.Fatalf("expected legacy action field cleared after migration, got %q", normalized[0].Action)
	}
	if len(normalized[0].ActionChain) != 1 {
		t.Fatalf("expected migrated action chain length 1, got %d", len(normalized[0].ActionChain))
	}
	if normalized[0].ActionChain[0].Action != ActionReturnError {
		t.Fatalf("expected legacy others failover to become return_error, got %q", normalized[0].ActionChain[0].Action)
	}
}
