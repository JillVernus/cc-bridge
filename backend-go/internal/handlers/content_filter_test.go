package handlers

import (
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func TestCheckContentFilterOnBody_WithRuleSpecificStatusCode(t *testing.T) {
	filter := &config.ContentFilter{
		Enabled: true,
		Rules: []config.ContentFilterRule{
			{Keyword: "insufficient_model_capacity", StatusCode: 503},
			{Keyword: "too many requests", StatusCode: 429},
		},
	}

	body := []byte(`{"content":[{"type":"text","text":"Error: INSUFFICIENT_MODEL_CAPACITY from upstream"}]}`)
	result := checkContentFilterOnBody(body, filter)

	if !result.Matched {
		t.Fatalf("expected matched=true, got false")
	}
	if result.MatchedKeyword != "insufficient_model_capacity" {
		t.Fatalf("expected matched keyword %q, got %q", "insufficient_model_capacity", result.MatchedKeyword)
	}
	if result.MatchedStatusCode != 503 {
		t.Fatalf("expected status code 503, got %d", result.MatchedStatusCode)
	}
}

func TestCheckContentFilterOnBody_LegacyKeywordsFallback(t *testing.T) {
	filter := &config.ContentFilter{
		Enabled:    true,
		Keywords:   []string{"rate limit", "quota exceeded"},
		StatusCode: 429,
	}

	body := []byte(`{"error":{"message":"quota exceeded for this key"}}`)
	result := checkContentFilterOnBody(body, filter)

	if !result.Matched {
		t.Fatalf("expected matched=true, got false")
	}
	if result.MatchedKeyword != "quota exceeded" {
		t.Fatalf("expected matched keyword %q, got %q", "quota exceeded", result.MatchedKeyword)
	}
	if result.MatchedStatusCode != 429 {
		t.Fatalf("expected status code 429, got %d", result.MatchedStatusCode)
	}
}

func TestResolveContentFilterRules_PrioritizesRulesOverLegacyFields(t *testing.T) {
	filter := &config.ContentFilter{
		Enabled: true,
		Rules: []config.ContentFilterRule{
			{Keyword: "first", StatusCode: 503},
		},
		Keywords:   []string{"legacy"},
		StatusCode: 429,
	}

	rules := resolveContentFilterRules(filter)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Keyword != "first" {
		t.Fatalf("expected rule keyword %q, got %q", "first", rules[0].Keyword)
	}
	if rules[0].StatusCode != 503 {
		t.Fatalf("expected rule status code 503, got %d", rules[0].StatusCode)
	}
}

func TestCheckContentFilterOnBody_FirstMatchedRuleWins(t *testing.T) {
	filter := &config.ContentFilter{
		Enabled: true,
		Rules: []config.ContentFilterRule{
			{Keyword: "error", StatusCode: 500},
			{Keyword: "error", StatusCode: 429},
		},
	}

	body := []byte(`{"content":[{"type":"text","text":"error: overloaded"}]}`)
	result := checkContentFilterOnBody(body, filter)

	if !result.Matched {
		t.Fatalf("expected matched=true, got false")
	}
	if result.MatchedStatusCode != 500 {
		t.Fatalf("expected first rule status code 500, got %d", result.MatchedStatusCode)
	}
}
