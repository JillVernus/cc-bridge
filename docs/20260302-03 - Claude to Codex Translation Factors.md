# Claude to Codex Translation Factors (Living Reference)

## Date

2026-03-02

## Purpose

Capture all critical factors required to reliably translate:

- Incoming format: Claude Messages (`/v1/messages`)
- Upstream format: Codex/OpenAI Responses
- Return path: Responses (normal/stream) -> Claude Messages

This document is intended as a reusable checklist for future maintenance and for
similar cross-protocol proxy projects.

## Scope

- Translation behavior and compatibility factors only
- Includes correctness, observability, and performance factors
- Includes post-implementation learnings from parity/performance fixes

## Architecture Summary

1. Receive Claude Messages request.
2. Convert to Responses request payload.
3. Send upstream using channel type:
   - `responses` (API key)
   - `openai-oauth` (OAuth headers/token flow)
4. Convert Responses output (normal or streaming) back to Claude shape.
5. Preserve logging/usage/session continuity.

## A. Request Translation Factors (Claude -> Responses)

Required request-envelope mappings:

- `model` -> `model` (after model redirect)
- `stream` -> `stream`
- `system` -> `instructions`
- `max_tokens` -> `max_output_tokens`
- `temperature` -> `temperature`
- `top_p` -> `top_p`
- `thinking` -> `reasoning`
- `tools` -> `tools`
- `tool_choice` -> `tool_choice`

Message/content conversion factors:

- Preserve original content order.
- Flush pending text before each tool block transition.
- Support multiple tool uses/results in one request.
- No first-item-only collapse and no dedupe.

Tool conversion factors:

- Function tool mapping (`name`, `description`, `input_schema` -> Responses function).
- Built-in tool compatibility (for supported built-ins such as web-search variants).
- Unknown/malformed tool handling should be deterministic (drop + log warning).

Model/reasoning factors:

- Support model suffix reasoning pattern (example: `gpt-5.3-codex(xhigh)`).
- Strip suffix for upstream `model` and map suffix to `reasoning.effort`.
- Suffix-based effort overrides per-request default reasoning mapping.

## B. Session and Cache Continuity Factors (Performance Critical)

This is a key latency factor for Claude -> Codex bridging.

`prompt_cache_key` continuity:

- If incoming already has `prompt_cache_key`, preserve it.
- If missing, derive deterministically in this order:
  1. `Session_id` header
  2. Parsed session suffix from Claude compound `metadata.user_id`
     (`..._account__session_<session_id>`)
  3. `Conversation_id` header

Important safety rule:

- Do not use raw non-compound `metadata.user_id` as `prompt_cache_key`.
  This can leak stable user identifiers and incorrectly couple unrelated sessions.

OAuth header continuity:

- For Codex OAuth upstream, set `Session_id` in this order:
  1. Incoming `Session_id` header
  2. Request `prompt_cache_key`
  3. Incoming `Conversation_id` header

Why this matters:

- Stable session/cache keying helps keep prompt cache hit behavior aligned with
  native Codex flows.
- Cache misses increase latency and can increase token cost.

## C. Upstream Header Factors (OAuth Path)

Required OAuth request headers:

- `Authorization: Bearer <access_token>`
- `Chatgpt-Account-Id: <account_id>`
- `Originator` (forwarded or default)
- `User-Agent` (Codex-compatible)
- `Conversation_id` (if present)
- `Session_id` (resolved fallback chain above)
- `Content-Type: application/json`
- `Accept: application/json` or `text/event-stream` by request mode

## D. Response Translation Factors (Responses -> Claude)

Normal response factors:

- `output_text` -> Claude text content blocks.
- `function_call` -> Claude `tool_use`.
- Final `stop_reason`:
  - `tool_use` when tool calls present
  - `end_turn` otherwise

Usage factors:

- Parse both modern and fallback usage shapes.
- Map cached token fields:
  - `input_tokens_details.cached_tokens` -> `cache_read_input_tokens`
- Adjust displayed Claude `input_tokens` to avoid double-counting cached reads.

## E. Stream Translation Factors (Responses SSE -> Claude SSE)

Envelope correctness factors:

- Emit exactly one `message_start`.
- Ensure block start/delta/stop sequencing is valid.
- Emit exactly one terminal `message_delta` + `message_stop`.
- Prevent duplicate stop events.

Text stream factors:

- `response.output_text.delta` -> Claude `content_block_delta` with `text_delta`.

Tool-use stream factors:

- `response.output_item.added` (`function_call`) -> `content_block_start` (`tool_use`)
- `response.function_call_arguments.delta` -> `content_block_delta` (`input_json_delta`)
- `response.function_call_arguments.done` / `response.output_item.done` ->
  `content_block_stop`
- Ensure stop reason becomes `tool_use` when tool call exists.

Model display factors:

- Claude `message_start.model` should show upstream model.
- Include reasoning effort display when available:
  - Example: `gpt-5.3-codex (xhigh)`
- Handle delayed reasoning events (effort may arrive after created/in_progress).

## F. Logging and Metrics Factors

- Request log session tracking should stay aligned with conversation/session IDs.
- Stream synthesizer usage extraction must support Responses/OAuth stream shapes.
- Main log table token metrics must include cached token dimensions.

## G. Regression Test Checklist

Request-side tests:

- Mixed `text + tool_use + text + tool_result` ordering.
- Multi-tool and multi-result preservation.
- `tool_choice`, `top_p`, `thinking/reasoning` mappings.
- Model suffix -> reasoning mapping behavior.
- Prompt cache key derivation:
  - from `Session_id`
  - from compound metadata session
  - negative case: plain metadata user id must not be used

Response/stream tests:

- Single well-formed SSE envelope (`message_start`/`message_stop` counts).
- Upstream model mapping in `message_start`.
- Delayed reasoning effort in model display.
- Tool-use streaming emits full Claude tool blocks and arguments deltas.
- Stop reason correctness (`tool_use` vs `end_turn`).
- Usage parsing includes cache read tokens.

## H. Portable Lessons for Other Projects

1. Translators need explicit state machines for streaming protocols.
2. Cache/session continuity should be treated as a first-class API contract.
3. Never map identity fields directly into cache/session fields without strict format checks.
4. Add parity tests from real captured samples, not synthetic-only payloads.
5. Keep one living checklist doc per protocol bridge to avoid repeated rediscovery.

## Quick Triage Guide

If translated path is slower than native path:

1. Check whether `prompt_cache_key` is present and stable across turns.
2. Verify upstream `Session_id` continuity on OAuth path.
3. Compare cache-related token metrics (`cached_tokens` / `cache_read_input_tokens`).
4. Ensure prompt prefix is stable enough for cache reuse.
5. Confirm tool-use stream includes full argument deltas (not stop reason only).
