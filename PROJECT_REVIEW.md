# CC-Bridge Project Review (Security & Stability)

Reviewed at: 2025-12-12 (UTC)  
Baseline commit: `cf31ebe`

This document captures a code review focused on critical bugs and security/stability risks. It is a static review plus limited local checks (see “Local Checks Run”).

## Status Update (2025-12-13)

This repo has since been updated to address most findings in this review. For details, see the per-item “Status” notes below.

## Scope

Backend (Go/Gin):
- Entrypoint, routing, middleware/auth, proxy/forwarding logic
- Config persistence & file watching
- Multi-channel scheduler/metrics
- Request logging (SQLite)
- Sessions (Responses API)

Frontend (Vite + Vue 3):
- API client/auth handling
- Potential XSS risks impacting stored secrets

Container/build:
- Dockerfiles, docker-compose defaults, Makefiles

## High-Severity Findings (Action Required)

### 1) Unauthenticated admin endpoint: `/admin/config/reload`

**Impact:** Any remote caller can trigger a write to the config file + backup creation. This is also trivially CSRF-triggerable (HTML form POST), even if the UI is intended to be private.

**Evidence:**
- Route is registered: `backend-go/main.go:126`
- Explicitly allowed through middleware (no auth): `backend-go/internal/middleware/auth.go:18`
- Handler performs `SaveConfig()` (write) not a reload: `backend-go/internal/handlers/health.go:65`

**Recommendation:**
- Require the same access key as `/api/*` (or remove the endpoint).
- If you truly need “reload”, implement a real reload from disk (e.g., `loadConfig()`), and keep “save” separate.

**Status (2025-12-13):** Implemented. `/admin/config/reload` now requires the access key and performs an in-memory reload from the on-disk config instead of writing/saving.

---

### 2) Dangerous dev endpoint: `/admin/dev/info` leaks secrets (if exposed)

**Impact:** When enabled, returns full config (including upstream API keys) and environment config. If `ENV=development` is accidentally deployed to a reachable environment, it becomes a direct secret exfiltration endpoint.

**Evidence:**
- Route exists in development: `backend-go/main.go:129`
- Explicitly allowed through middleware (no auth): `backend-go/internal/middleware/auth.go:20`
- Returns `cfgManager.GetConfig()` and `envCfg`: `backend-go/internal/handlers/health.go:95`

**Recommendation:**
- Require auth even in development, or gate behind a separate, stronger admin auth.
- Alternatively, do not expose this endpoint at all (or only bind it to localhost).

**Status (2025-12-13):** Implemented. `/admin/dev/info` remains development-only but now requires the same access key.

---

### 3) No request body size limits on proxy endpoints (OOM/DoS)

**Impact:** A client can send a very large body and force the server to allocate it fully (`io.ReadAll`), risking OOM and crash.

**Evidence:**
- `/v1/messages`: `backend-go/internal/handlers/proxy.go:38`
- `/v1/responses`: `backend-go/internal/handlers/responses.go:48`
- Providers also re-read bodies in several places (amplifies memory usage):
  - `backend-go/internal/providers/openai.go:26`
  - `backend-go/internal/providers/claude.go:46`
  - `backend-go/internal/providers/responses.go:32`

**Recommendation:**
- Add request size limits (e.g., `http.MaxBytesReader` or Gin middleware) and reject oversized bodies early.
- Avoid repeated full-body reads where possible.

**Status (2025-12-13):** Implemented request body size limits on proxy endpoints (configurable via `MAX_REQUEST_BODY_MB`, default 20MB). Oversized requests return `413`.

---

### 4) Streaming parsers likely break on large SSE lines (default 64KB scanner limit)

**Impact:** `bufio.Scanner` has a default token limit (~64KB). Large SSE lines (tool calls, large JSON chunks) can cause “token too long” and break streaming responses unexpectedly.

**Evidence:**
- Claude stream parsing: `backend-go/internal/providers/claude.go:115`
- OpenAI stream parsing: `backend-go/internal/providers/openai.go:360`
- Gemini stream parsing: `backend-go/internal/providers/gemini.go:332`

**Recommendation:**
- Set a larger scanner buffer (`scanner.Buffer(...)`) or replace with `bufio.Reader` and manual line reading.
- Add tests for large SSE lines.

**Status (2025-12-13):** Implemented a larger `bufio.Scanner` buffer (4MB) for provider streaming parsers.

---

### 5) Responses session handling bug: `previous_id` can be incorrect; token accounting can explode

**Impact:**
- `previous_id` can end up equal to the current response ID (breaks client-side chaining).
- Session token accounting can be massively over-counted (because total tokens are added once per output item), causing premature eviction and incorrect stats.

**Evidence:**
- `UpdateLastResponseID` is called before `previous_id` is computed: `backend-go/internal/handlers/responses.go:855`
- Then `previous_id` is set from `sess.LastResponseID` (already updated): `backend-go/internal/handlers/responses.go:861`
- Assistant messages appended with `responsesResp.Usage.TotalTokens` for *each* output item: `backend-go/internal/handlers/responses.go:851`

**Recommendation:**
- Capture the old `LastResponseID` before updating it, and set `previous_id` from the old value.
- Only add token usage once per response (not once per output item), or distribute appropriately if you have per-item token data.

**Status (2025-12-13):** Implemented. `previous_id` now chains correctly and token usage is added once per response (no longer multiplied by output items).

---

### 6) Config and backup file permissions are too open (secret leakage)

**Impact:** Upstream API keys are stored in `.config/config.json`. File mode is `0644` and directory mode is `0755` → readable by other local users in multi-user environments; backups also use `0644`. With Docker volume mounts, host-side permissions matter a lot.

**Evidence:**
- `.config` directory created with `0755`: `backend-go/internal/config/config.go:229`
- Config file written with `0644`: `backend-go/internal/config/config.go:401`
- Backup dir created with `0755`: `backend-go/internal/config/config.go:418`
- Backup file written with `0644`: `backend-go/internal/config/config.go:433`

**Recommendation:**
- Use `0700` for config directories and `0600` for config files/backups (or configurable).
- Consider encrypting secrets at rest if threat model requires it.

**Status (2025-12-13):** Not changed (explicitly kept as-is).

---

### 7) Container hardening gaps

**Impact:** Runtime image uses `alpine:latest` and does not set a non-root `USER`. This increases blast radius in case of RCE/supply chain compromise.

**Evidence:**
- `FROM alpine:latest AS runtime`: `Dockerfile:34`
- No `USER` directive: `Dockerfile:33` onward

**Recommendation:**
- Pin base images by digest or at least a stable tag.
- Create and run as a non-root user; restrict filesystem permissions for `/app/.config` and logs.

**Status (2025-12-13):** Not changed (explicitly kept as-is).

## Medium-Severity Findings

### CORS configuration is risky/misleading

**Impact:** In development, origin allowlist uses substring match for `"localhost"` (can match attacker-controlled domains containing that substring). Also sets credentials `true` unconditionally; combined with default `CORS_ORIGIN="*"` is confusing and can lead to unsafe assumptions.

**Evidence:**
- Dev origin check uses `strings.Contains(origin, "localhost")`: `backend-go/internal/middleware/cors.go:17`
- Always sets `Access-Control-Allow-Credentials: true`: `backend-go/internal/middleware/cors.go:27`
- Default `CORS_ORIGIN` is `"*"`: `backend-go/internal/config/env.go:57`

**Recommendation:**
- Use strict origin matching (scheme + host + port).
- Don’t set `Allow-Credentials` unless you actually use cookie-based auth and a strict allowlist.

**Status (2025-12-13):** Implemented. CORS behavior is now strict and controlled by `ENABLE_CORS` and `CORS_ORIGIN`.

### Access key accepted via query parameter (`?key=`)

**Impact:** Query-string secrets can leak via logs, browser history, Referer headers, and screenshots.

**Evidence:**
- `getAPIKey()` falls back to query parameter: `backend-go/internal/middleware/auth.go:118`

**Recommendation:**
- Remove query parameter auth in the backend, or gate it behind explicit opt-in in development only.

**Status (2025-12-13):** Implemented. Backend no longer accepts the access key via query parameter; headers only.

### Config concurrency/encapsulation sharp edges

**Impact:** `GetConfig()` returns slices/maps by value but not deep-copied; callers can accidentally share underlying arrays. Some code reads config fields without holding locks.

**Evidence:**
- `GetConfig()` returns `cm.config` directly: `backend-go/internal/config/config.go:496`
- `GetNextAPIKey` reads `cm.config.LoadBalance` without holding a lock: `backend-go/internal/config/config.go:528`

**Recommendation:**
- Deep copy config data when returning it, or expose read-only views.
- Ensure all config reads are lock-protected or use immutable snapshots.

**Status (2025-12-13):** Not changed (explicitly kept as-is).

### Unused/misleading env options

**Impact:** Several environment fields exist but are never applied (rate limit, healthcheck enabled flag, enable CORS flag, etc.), which can mislead operators into believing protections are enabled.

**Evidence:**
- Definitions: `backend-go/internal/config/env.go:19`
- Defaults: `backend-go/internal/config/env.go:56`, `backend-go/internal/config/env.go:61`
- No usage found in code (search result only hits `env.go`).

**Recommendation:**
- Either implement these controls or remove the options to avoid false confidence.

**Status (2025-12-13):** Code behavior not changed; docs now mark these env vars as “(planning)” to avoid false confidence.

### URL “version suffix” regex may not match `v1beta1`-style paths

**Impact:** Some upstream base URLs may be joined incorrectly if they include `.../v1beta1` (digits + letters + digits).

**Evidence:**
- Regex: `regexp.MustCompile(`/v\\d+[a-z]*$`)`
  - `backend-go/internal/providers/claude.go:61`
  - `backend-go/internal/providers/openai.go:69`
  - `backend-go/internal/providers/responses.go:127`

**Recommendation:**
- Adjust regex to cover common patterns (`v1beta1`, `v1alpha2`, etc.), or use URL path joining logic that doesn’t rely on regex heuristics.

**Status (2025-12-13):** Not changed (explicitly kept as-is).

## Frontend Notes

### Admin access key stored in `localStorage`

**Impact:** If any XSS is introduced, the attacker can steal `proxyAccessKey`. The app also supports importing the key from `?key=` in the URL.

**Evidence:**
- Reads `?key=` and persists to localStorage: `frontend/src/services/api.ts:115`, `frontend/src/services/api.ts:127`

**Recommendation:**
- Prefer session-only storage (memory / sessionStorage) and avoid URL-based secrets.
- Consider using a proper login flow + HttpOnly cookies if the threat model includes XSS.

**Status (2025-12-13):** Implemented. The frontend no longer imports secrets from `?key=` and stores the access key in `sessionStorage` (with a one-time migration from legacy `localStorage`).

### `innerHTML` usage for SVG icons (low risk here)

**Evidence:**
- `innerHTML: svgContent` from bundled assets: `frontend/src/plugins/vuetify.ts:31`

**Note:** This is probably fine because the SVGs are imported from local build assets, but keep it in mind if icon sources ever become dynamic/user-supplied.

**Status (2025-12-13):** Not changed (explicitly kept as-is).

## Local Checks Run

Backend:
- `cd backend-go && go test ./...`
- `cd backend-go && go vet ./...`
- `cd backend-go && go test -race ./...`

All passed, but many critical paths (handlers/providers/session behavior) have little to no test coverage.

## Open Questions (need clarification)

1) Is `/admin/config/reload` intended to be publicly callable? If not, it should be authenticated immediately.
2) Is `/admin/dev/info` intended only for local debugging? If yes, it should require the access key or be disabled by default.

**Status (2025-12-13):** Clarified/handled. Both endpoints now require the access key; `/admin/dev/info` is still development-only.
