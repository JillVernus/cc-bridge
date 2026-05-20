# Codex OAuth Export Wrapper Format Support

## Background

`cc-bridge` currently accepts two OAuth JSON shapes when a user adds a Codex/openai-oauth channel:

1. **Codex CLI auth.json (nested):** `{ OPENAI_API_KEY, last_refresh, tokens: { access_token, account_id, id_token, refresh_token } }`
2. **Flat format** (frontend-only convenience): tokens at root level.

A user has surfaced a third shape (see `sample_oauth/sample.json`) produced by an external Codex account exporter:

```json
{
  "exported_at": "2026-05-20T05:17:10.192Z",
  "proxies": [],
  "accounts": [{
    "name": "user@example.com",
    "platform": "openai",
    "type": "oauth",
    "credentials": {
      "access_token": "xxxxxx",
      "chatgpt_account_id": "5c8a3fea-...",
      "chatgpt_user_id": "user-...",
      "email": "user@example.com",
      "expires_at": "2026-05-30T03:47:46+00:00",
      "expires_in": 863994,
      "plan_type": "plus"
    },
    "extra": {
      "email": "user@example.com",
      "source": "register_session",
      "last_refresh": "2026-05-20T03:47:51.308489+00:00"
    },
    "concurrency": 10,
    "priority": 1
  }]
}
```

Today, pasting this into the UI fails because:
- The wrapper has no top-level `tokens` or `access_token`.
- The account id field is named `chatgpt_account_id` rather than `account_id`.
- The export omits `refresh_token` and `id_token`.

## Field Coverage Analysis

| Field cc-bridge needs              | In sample?                          | Notes                                                                 |
|------------------------------------|-------------------------------------|-----------------------------------------------------------------------|
| `access_token`                     | yes (`credentials.access_token`)    | Required                                                              |
| `account_id`                       | yes (as `chatgpt_account_id`)       | Required; rename on import                                            |
| `refresh_token`                    | **NO**                              | Currently required by both backend `ParseAuthJSON` and UI validator   |
| `id_token`                         | **NO**                              | Optional; previously used to extract email for display                |
| `last_refresh`                     | yes (`extra.last_refresh`)          | Optional                                                              |
| Email (display only)               | yes (`credentials.email`)           | Use directly; no JWT decode needed                                    |
| Token expiry (for refresh timing)  | yes (`credentials.expires_at`)      | RFC3339; today derived from JWT — fall back to this when JWT missing  |

**Decision (confirmed with user):**
- Accept the new format even though `refresh_token` is absent. The channel will operate until the access token expires (~10 days from export), then fail with upstream 401. Surface this trade-off in the UI.
- Support only single-account import: pick the first `accounts[]` entry whose `platform == "openai"` and `type == "oauth"`. Reject if no eligible entry exists.

## Approach

Treat the export wrapper as a pre-processing step in front of the existing parser. Both backend and frontend detect the wrapper by presence of `accounts[]`, unwrap to a normalized inner object, then continue through the existing flat-format path. The downstream `OAuthTokens` struct does not change.

Refresh behavior changes only where strictly necessary: when `RefreshToken` is empty, the token manager must not attempt a refresh, because the refresh call has nothing to send and would fail noisily. Instead, return the existing access token and let upstream surface the eventual 401.

## Components

### Backend: `backend-go/internal/auth/codex/codex.go`

1. **New type `CodexOAuthExport`** mirroring the wrapper shape (only the fields we read).
2. **`ParseAuthJSON` changes:**
   - First attempt to parse as `CodexOAuthExport`. If `accounts[]` is non-empty, find the first entry with `platform == "openai"` and `type == "oauth"`, normalize into the existing token shape (`chatgpt_account_id` → `account_id`, `extra.last_refresh` → `last_refresh`), and proceed.
   - Otherwise fall through to today's nested/flat handling.
   - Relax the "missing refresh_token" error: accept when `access_token` and `account_id` are present. Keep the access/account_id required-field errors.
3. **`IsTokenValid`:** drop the `RefreshToken != ""` check. Still require `AccessToken` and `AccountID`.
4. **`GetValidToken`:** when `tokens.RefreshToken == ""`, never attempt refresh. Return the current access token regardless of expiry. (Upstream will return 401 when the token finally expires; that is the documented failure mode for this import path.)
5. **`isTokenExpired`:** unchanged — still used in the refresh-token-present path.

### Backend tests: `backend-go/internal/auth/codex/codex_test.go`

Add cases for:
- Parsing the export wrapper successfully.
- Rejecting an export with zero matching accounts (e.g. only `platform: "anthropic"`).
- Skipping non-matching accounts and selecting the first matching one when multiple are present.
- `IsTokenValid` returning true when `RefreshToken` is empty but other fields are set.
- `GetValidToken` returning the access token as-is when `RefreshToken` is empty.

### Frontend: `frontend/src/components/AddChannelModal.vue`

Update `parseOAuthJson()`:
- Detect the wrapper via `Array.isArray(parsed.accounts)`.
- Pick the first matching account; on failure, set `oauthParseError` to a new i18n string.
- Map `credentials.chatgpt_account_id` → `account_id`, take `credentials.email`, take `extra.last_refresh`.
- After successful parse of the wrapper, set a separate `oauthInfoMessage` (or extend `parsedOAuthInfo`) noting "no refresh token — channel will stop working at `expires_at`".
- The existing required-field check should treat `refresh_token` as optional for the wrapper path.

### i18n: `frontend/src/locales/en.ts` and `zh-CN.ts`

Add:
- `oauthExportNoEligibleAccount` — error when wrapper has no matching account.
- `oauthExportNoRefreshTokenWarning` — info shown after successful import (interpolates `expires_at`).

## Data Flow

```
User paste → parseOAuthJson()
  ├─ wrapper detected → unwrap accounts[0] → normalized inner object
  ├─ nested detected  → existing path
  └─ flat detected    → existing path
        ↓
  OAuthTokens { access, account_id, [refresh], [id_token], [last_refresh] }
        ↓
  POST /api/channels  (backend re-validates via ParseAuthJSON)
        ↓
  TokenManager.GetValidToken:
    if RefreshToken == "" → return AccessToken unchanged
    else                  → existing refresh-on-expiry logic
```

## Error Handling

- Wrapper with empty/missing `accounts[]` → "no eligible openai oauth account in export".
- Wrapper account missing `access_token` or `chatgpt_account_id` → existing "missing access_token" / "missing account_id" errors.
- Wrapper account with no `refresh_token` → **no error**, channel created, UI shows expiry warning.
- Runtime: when access token expires and no refresh_token is available, upstream returns 401. This propagates through the existing error path; no special handling.

## Testing

- Backend unit tests cover all parser branches and the no-refresh-token `GetValidToken` path.
- Frontend: manual smoke test — paste the sample, confirm channel saves, confirm warning text renders. (Existing parser already has no automated coverage; no new test framework introduced here.)
- Run `cd backend-go && make test` and `cd frontend && bun run type-check` before commit.

## Out of Scope

- Bulk-importing every account in the wrapper.
- Persisting `concurrency` / `priority` from the wrapper.
- Persisting `expires_at` as a first-class column (JWT parsing already covers the refresh-present path; for the no-refresh path we just trust upstream 401).
- Supporting non-`openai` platforms in the export.
- Auto-prompting the user to paste a refresh_token separately.
