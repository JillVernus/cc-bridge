package codex

import (
	"strings"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func TestParseAuthJSON_NestedFormat(t *testing.T) {
	content := `{
		"OPENAI_API_KEY": "key",
		"last_refresh": "2026-05-20T03:47:51Z",
		"tokens": {
			"access_token": "atk",
			"account_id": "acc",
			"id_token": "idt",
			"refresh_token": "rtk"
		}
	}`
	got, err := ParseAuthJSON(content)
	if err != nil {
		t.Fatalf("ParseAuthJSON returned error: %v", err)
	}
	want := &config.OAuthTokens{
		AccessToken:  "atk",
		AccountID:    "acc",
		IDToken:      "idt",
		RefreshToken: "rtk",
		LastRefresh:  "2026-05-20T03:47:51Z",
	}
	if *got != *want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseAuthJSON_NestedMissingRefreshToken(t *testing.T) {
	content := `{"tokens":{"access_token":"a","account_id":"b"}}`
	_, err := ParseAuthJSON(content)
	if err == nil || !strings.Contains(err.Error(), "refresh_token") {
		t.Errorf("expected missing refresh_token error, got %v", err)
	}
}

func TestParseAuthJSON_ExportWrapper(t *testing.T) {
	content := `{
		"exported_at": "2026-05-20T05:17:10.192Z",
		"accounts": [{
			"name": "u@example.com",
			"platform": "openai",
			"type": "oauth",
			"credentials": {
				"access_token": "atk",
				"chatgpt_account_id": "acc-uuid",
				"chatgpt_user_id": "user-x",
				"email": "u@example.com",
				"expires_at": "2026-05-30T03:47:46+00:00"
			},
			"extra": {
				"last_refresh": "2026-05-20T03:47:51.308489+00:00"
			}
		}]
	}`
	got, err := ParseAuthJSON(content)
	if err != nil {
		t.Fatalf("ParseAuthJSON returned error: %v", err)
	}
	if got.AccessToken != "atk" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "atk")
	}
	if got.AccountID != "acc-uuid" {
		t.Errorf("AccountID = %q, want %q (mapped from chatgpt_account_id)", got.AccountID, "acc-uuid")
	}
	if got.RefreshToken != "" {
		t.Errorf("RefreshToken = %q, want empty (none in export)", got.RefreshToken)
	}
	if got.LastRefresh != "2026-05-20T03:47:51.308489+00:00" {
		t.Errorf("LastRefresh = %q, want value from extra.last_refresh", got.LastRefresh)
	}
}

func TestParseAuthJSON_ExportWrapperSkipsNonMatching(t *testing.T) {
	content := `{
		"accounts": [
			{"platform": "anthropic", "type": "oauth", "credentials": {"access_token": "x", "chatgpt_account_id": "y"}},
			{"platform": "openai", "type": "api_key", "credentials": {"access_token": "x", "chatgpt_account_id": "y"}},
			{"platform": "openai", "type": "oauth", "credentials": {"access_token": "match", "chatgpt_account_id": "acc"}}
		]
	}`
	got, err := ParseAuthJSON(content)
	if err != nil {
		t.Fatalf("ParseAuthJSON returned error: %v", err)
	}
	if got.AccessToken != "match" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "match")
	}
}

func TestParseAuthJSON_ExportWrapperNoEligibleAccount(t *testing.T) {
	content := `{"accounts":[{"platform":"anthropic","type":"oauth","credentials":{"access_token":"x","chatgpt_account_id":"y"}}]}`
	_, err := ParseAuthJSON(content)
	if err == nil || !strings.Contains(err.Error(), "no eligible") {
		t.Errorf("expected no-eligible-account error, got %v", err)
	}
}

func TestParseAuthJSON_ExportWrapperMissingAccessToken(t *testing.T) {
	content := `{"accounts":[{"platform":"openai","type":"oauth","credentials":{"chatgpt_account_id":"y"}}]}`
	_, err := ParseAuthJSON(content)
	if err == nil || !strings.Contains(err.Error(), "access_token") {
		t.Errorf("expected missing access_token error, got %v", err)
	}
}

func TestParseAuthJSON_ExportWrapperMissingAccountID(t *testing.T) {
	content := `{"accounts":[{"platform":"openai","type":"oauth","credentials":{"access_token":"x"}}]}`
	_, err := ParseAuthJSON(content)
	if err == nil || !strings.Contains(err.Error(), "account_id") {
		t.Errorf("expected missing account_id error, got %v", err)
	}
}

func TestIsTokenValid_AllowsMissingRefreshToken(t *testing.T) {
	tokens := &config.OAuthTokens{AccessToken: "a", AccountID: "b"}
	if !IsTokenValid(tokens) {
		t.Errorf("IsTokenValid should accept tokens without refresh_token")
	}
}

func TestIsTokenValid_RejectsMissingAccessToken(t *testing.T) {
	tokens := &config.OAuthTokens{AccountID: "b", RefreshToken: "r"}
	if IsTokenValid(tokens) {
		t.Errorf("IsTokenValid should reject tokens without access_token")
	}
}

func TestGetValidToken_NoRefreshTokenReturnsAsIs(t *testing.T) {
	tm := NewTokenManager()
	tokens := &config.OAuthTokens{AccessToken: "expired-or-not", AccountID: "acc"}
	at, aid, updated, err := tm.GetValidToken(tokens)
	if err != nil {
		t.Fatalf("GetValidToken returned error: %v", err)
	}
	if at != "expired-or-not" || aid != "acc" {
		t.Errorf("got (%q, %q), want (%q, %q)", at, aid, "expired-or-not", "acc")
	}
	if updated != nil {
		t.Errorf("updated tokens should be nil when refresh_token is empty")
	}
}
