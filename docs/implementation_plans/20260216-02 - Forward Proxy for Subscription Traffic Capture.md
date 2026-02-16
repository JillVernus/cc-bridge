# Forward Proxy for Subscription/OAuth Traffic Capture

## Date: 2026-02-16

## Background

CC-Bridge currently works as an **API proxy** for Claude Code — translating protocols,
selecting channels, and logging all API call metrics accurately. This works well for
**API mode** (pay-per-call) where `ANTHROPIC_BASE_URL` points to cc-bridge.

However, API calls are expensive. The plan is to switch to Anthropic's **subscription
plan** (Pro/Max), which uses **OAuth authentication** and talks directly to Anthropic,
bypassing cc-bridge entirely. This means **no request logs, no usage metrics**.

**Goal**: Capture accurate per-API-call metrics from subscription/OAuth Claude Code
sessions for internal audit purposes.

## Investigation Summary

### Phase 1 Hook Ingest (existing, completed)

- `POST /api/logs/hooks/anthropic` endpoint is built and working
- Accepts hook-submitted payloads, maps to `request_logs` schema
- Supports idempotency, auto-pricing, API key attribution, SSE events
- See: `docs/implementation_plans/20260216-01 - Anthropic Hook Log Ingest API Phase 1.md`

### Hook Stdin Investigation

We installed a capture script (`scripts/hooks/ccbridge-hook-capture.sh`) to capture
raw stdin JSON from all Claude Code hook events.

**Finding: Hook stdin contains NO metrics.**

| Event | Fields Available |
|-------|-----------------|
| SessionStart | session_id, transcript_path, cwd, model |
| UserPromptSubmit | session_id, transcript_path, cwd, prompt |
| Stop | session_id, transcript_path, cwd, stop_hook_active |
| PreToolUse | session_id, tool_name, tool_input |
| PostToolUse | session_id, tool_name, tool_input, tool_response |

No token counts, no usage, no cost, no per-API-call data in any hook event stdin.

### Transcript JSONL Investigation

Checked the transcript file for a "hi" test turn.

**Finding: Transcript records initial usage, not final.**

The transcript stores usage from the `message_start` SSE event (initial estimate),
not the `message_delta` event (final count):

- Transcript: `output_tokens: 2` (initial, WRONG)
- Actual HTTP stream `message_delta`: `output_tokens: 13` (final, CORRECT)

### Reference Repo (CCometixLine)

Cloned at `sample_request/reference_repo/CCometixLine`.

**Finding: Uses statusline, not hooks.**

CCometixLine is a **statusline command** integration, not a hook-based system.
Claude Code pipes different stdin to the statusline command that includes
`cost.total_cost_usd`, `total_duration_ms`, etc. But this is **cumulative session
data**, not per-API-call metrics.

### Internal Haiku Calls

When sending "hi", Claude Code fires 2 Haiku API calls (routing/classification)
+ 1 Opus call. Only the Opus assistant message appears in the transcript.

**Finding: Internal Haiku routing calls are invisible in OAuth mode.**

Not in hooks, not in transcript, not in statusline. Only visible through a proxy.

### Data Source Comparison

| Source | Per-call? | Has model? | Has tokens? | output_tokens correct? | Has cost? |
|--------|-----------|------------|-------------|----------------------|-----------|
| Hook stdin | Yes (event) | No (only SessionStart) | No | N/A | No |
| Transcript JSONL | Yes (per assistant msg) | Yes | Yes | **No** (initial only) | No |
| Statusline stdin | No (cumulative) | Yes | No | N/A | Yes (cumulative) |
| Proxy (cc-bridge) | Yes | Yes | Yes | Yes | Yes |

**Conclusion: Proxy is the only way to capture accurate per-call metrics.**

## Rejected Approaches

### Option A: ANTHROPIC_BASE_URL passthrough

Set `ANTHROPIC_BASE_URL=http://cc-bridge:3000` and add a passthrough channel type.

**Rejected**: Subscription/OAuth uses different auth method than API mode. Mismatch
in headers or auth flow could violate Anthropic's TOS and risk subscription ban.

### Hook-based ingest (Phase 2)

Use hooks to trigger transcript parsing and metric ingestion.

**Rejected for audit use**: Metrics are inaccurate (wrong output_tokens, missing
internal calls). Acceptable as approximate fallback but not for internal audit.

## Chosen Approach: HTTPS Forward Proxy in cc-bridge

### Concept

Add a **passive HTTPS forward proxy** to cc-bridge that captures subscription/OAuth
traffic without modifying it.

```
Claude Code
  │
  │  HTTPS_PROXY=http://localhost:3001
  │
  ▼
cc-bridge Forward Proxy (:3001)
  │
  │  1. CONNECT api.anthropic.com:443
  │  2. TLS termination (custom CA cert)
  │  3. Read request/response (tee the stream)
  │  4. Parse SSE events for metrics
  │  5. Log to request_logs database
  │
  ▼
api.anthropic.com (unchanged traffic)
```

### Key Principles

1. **Zero modification** — requests and responses forwarded untouched
2. **Passive observer** — just capture, parse, and log
3. **Completely separate** from existing cc-bridge API proxy (no translation,
   no channel selection, no header rewriting)
4. **Scoped to Claude Code** — only the Claude Code process uses
   `HTTPS_PROXY`, not system-wide

### What We Parse from the Anthropic SSE Stream

```
event: message_start
data: {"message":{"id":"msg_...","model":"claude-opus-4-6","usage":{
  "input_tokens":3,"output_tokens":2,
  "cache_creation_input_tokens":25037,"cache_read_input_tokens":0}}}

event: message_delta
data: {"usage":{"output_tokens":13}}    ← final output token count

event: message_stop
data: {"amazon-bedrock-invocationMetrics":{
  "inputTokenCount":3,"outputTokenCount":13,
  "invocationLatency":3037,"firstByteLatency":2635,
  "cacheReadInputTokenCount":0,"cacheWriteInputTokenCount":25037}}
```

### Metrics Extracted

From `message_start`: model, message.id, input_tokens, cache tokens
From `message_delta`: final output_tokens (correct count)
From `message_stop`: invocation latency, first byte latency

### Open Design Decisions

1. **Port**: Same port as cc-bridge (:3000) or separate port (:3001)?
2. **Domain scope**: Only MITM `api.anthropic.com`, blind tunnel everything else?
3. **CA cert management**: Auto-generate on first startup, or require manual config?
4. **Integration**: Log directly via `requestlog.Manager`, or POST to existing
   ingest API (`/api/logs/hooks/anthropic`)?

### Technical Components Needed

1. HTTP CONNECT handler (Go `net/http` standard library)
2. TLS MITM with custom CA (`crypto/tls`, on-the-fly cert for api.anthropic.com)
3. HTTP request/response tee (read without modifying)
4. Anthropic SSE stream parser (extract usage from event types)
5. Request log integration (map to existing schema)
6. CA cert generation/management

### Dev Container Setup (planned)

```bash
# 1. cc-bridge generates/loads CA cert on startup
# 2. Install CA cert in dev container trust store
sudo cp /path/to/ccbridge-ca.pem /usr/local/share/ca-certificates/ccbridge.crt
sudo update-ca-certificates

# 3. Launch Claude Code with proxy scoped to this process
HTTPS_PROXY=http://localhost:3001 claude
```

## Files Created During Investigation

- `scripts/hooks/ccbridge-hook-capture.sh` — raw hook stdin capture script
- `scripts/hooks/claude-settings.capture.json` — capture hook event config

## Next Steps

- [x] Finalize open design decisions (port, domain scope, CA management)
- [x] Create detailed implementation plan
- [x] Implement HTTPS forward proxy in cc-bridge
- [x] Test with subscription/OAuth Claude Code session
- [x] Verify accurate metrics in cc-bridge live log UI

## Implementation (Completed v1.4.0)

### Design Decisions Resolved

| Decision | Resolution |
|----------|-----------|
| Port | Separate port (:3001), configurable via `FORWARD_PROXY_PORT` |
| Domain scope | Configurable MITM domain list, default `api.anthropic.com`, blind tunnel for everything else |
| CA cert management | Auto-generate on first startup in `.config/certs/`, downloadable via API |
| Integration | Direct `requestlog.Manager` integration (same process, no HTTP overhead) |

### Files Created

```
backend-go/internal/forwardproxy/
  server.go          - Forward proxy server, CONNECT handler, HTTP forward, blind tunnel, config persistence
  cert.go            - CA cert auto-generation, per-host cert signing, LRU cache
  interceptor.go     - TLS MITM, HTTP request/response tee, two-phase logging
  anthropic.go       - StreamParserWriter (wraps StreamSynthesizer), metric extraction, request_log creation
  anthropic_test.go  - SSE parsing, JSON parsing, client session extraction tests
  cert_test.go       - CA generation, host cert, cache, TLS handshake tests

backend-go/internal/handlers/
  forward_proxy.go   - API handlers: GET/PUT config, CA cert download

frontend/src/components/
  ForwardProxySettings.vue  - Settings dialog: enable/disable, domain management, CA download
```

### Key Implementation Details

- Two proxy paths: CONNECT+MITM and HTTP forward (absolute URL)
- StreamSynthesizer reuse for SSE metric extraction (same as normal proxy)
- Two-phase logging: pending on request, completed on response
- Hop-by-hop header stripping on incoming request before cloning
- Dedicated HTTP client with Proxy: nil and ForceAttemptHTTP2: true
