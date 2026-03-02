# Claude Messages to Codex Responses Translation Parity

## Date: 2026-03-01

## Scope (Strict)

This implementation is strictly limited to request translation on this path:

- Incoming endpoint: `/v1/messages` (Claude Messages request format)
- Upstream target format: Codex/OpenAI Responses request payload
- Converter implementation: `backend-go/internal/providers/responses_upstream.go`

Out of scope:

- New endpoint or router changes
- New provider routing behavior
- `/v1/messages` support for `serviceType: openai-oauth`
- Frontend/UI changes
- Response/stream translation parity changes (tool stream events, stop-reason stream semantics)

## Progress Tracker

Last updated: 2026-03-01 (implementation complete, post-merge review approved)

- [x] Plan document created
- [x] Plan review approved
- [x] Step 0 implemented: request-shape capture support (types/raw parsing)
- [x] Step 0 reviewer approved
- [x] Step 1 implemented: non-lossy ordered messages -> input conversion
- [x] Step 1 reviewer approved
- [x] Step 2 implemented: tools + tool_choice mapping parity (with web-search built-ins)
- [x] Step 2 reviewer approved
- [x] Step 3 implemented: deterministic field whitelist mapping (`thinking`, `top_p`, etc.)
- [x] Step 3 reviewer approved
- [x] Step 4 implemented: targeted tests for translator parity
- [x] Step 4 reviewer approved
- [x] Final validation + post-merge review approved

## Background

Current translator (`ResponsesUpstreamProvider`) already converts basic text and simple
tool calls, but has known lossiness and unsupported shapes:

1. Multi-block loss in one Claude message (`text + tool_use + text + tool_result`)
2. Multi-tool/multi-result collapse (first item only)
3. Ambiguous ordering behavior across mixed blocks
4. Missing support for built-in tool forms (including web-search class tools)
5. Missing capture for some request fields needed for mapping (`tool_choice`, `top_p`,
   potentially non-function tool shapes)
6. No focused unit tests for this specific conversion path

## Target Contract

### A) Envelope Mapping (Whitelist Only)

Supported and mapped explicitly:

- `model` -> `model` (after `config.RedirectModel`)
- `stream` -> `stream`
- `system` -> `instructions`
- `max_tokens` -> `max_output_tokens`
- `temperature` -> `temperature`
- `top_p` -> `top_p`
- `thinking` -> `reasoning` (see mapping table below)
- `tools` -> `tools` (function + selected built-ins)
- `tool_choice` -> `tool_choice`

Deterministic fallback policy:

- Unknown top-level fields: **drop** (do not pass-through)
- Unknown tool type: **drop + debug log warning**
- Malformed tool payload: **drop that tool + debug log warning**

No generic unknown passthrough to avoid upstream schema breakage.

### B) Thinking -> Reasoning Mapping

- `thinking.type == "enabled"` and `budget_tokens > 0` ->
  `reasoning: {"effort":"high"}` (deterministic first version)
- `thinking.type == "disabled"` or missing/invalid budget -> omit `reasoning`

Note: this is intentionally conservative for initial parity and avoids speculative
budget-to-effort heuristics.

### C) Messages -> Input Items (Strict Ordering)

For each Claude message, iterate content blocks in order with explicit flush rules:

1. Maintain a pending text buffer per message role (`message` item).
2. On `text` block: append to pending buffer.
3. On `tool_use` block:
   - flush pending text buffer to a `type:"message"` item first (if not empty),
   - emit `type:"function_call"` item.
4. On `tool_result` block:
   - flush pending text buffer to a `type:"message"` item first (if not empty),
   - emit `type:"function_call_output"` item.
5. After the last block, flush remaining pending text.

Rules:

- No dedupe
- No first-item-only collapse
- Preserve original interleaving order

### D) Tools Mapping

1. Function tools:
   - Claude function-style tool (`name`, `input_schema`) ->
     `{"type":"function","name":...,"description":...,"parameters":...}`
2. Built-in web-search class tools:
   - Accept tool objects that carry a built-in type marker and map to
     Responses built-in tool shape (`{"type":"web_search_preview"}` baseline).
3. Unknown built-in types:
   - Drop and emit debug warning.

### E) Tool Choice Mapping

Supported mapping:

- Claude `tool_choice: "auto"` -> Responses `"auto"`
- Claude `tool_choice: "none"` -> Responses `"none"`
- Claude `tool_choice: "any"` -> Responses `"required"`
- Claude named-tool choice object/string (if present) ->
  Responses function choice object

## Implementation Steps (Sequential + Reviewer Gate)

### Step 0 - Request Shape Capture (Mandatory Foundation)

Files:

- `backend-go/internal/types/types.go`
- `backend-go/internal/providers/responses_upstream.go`

Work:

- [x] Extend Claude request/tool types enough to read required fields:
  - `tool_choice`
  - `top_p`
  - built-in tool type markers
- [x] Keep backward compatibility for existing request parsing
- [x] Add converter-side helpers to safely read optional raw fields

Review gate:

- [x] Reviewer approval required before Step 1

### Step 1 - Non-Lossy Ordered Message Conversion

File:

- `backend-go/internal/providers/responses_upstream.go`

Work:

- [x] Replace single-item message conversion with multi-item ordered conversion
- [x] Implement explicit flush rules around non-text blocks
- [x] Preserve simple text-only output behavior

Review gate:

- [x] Reviewer approval required before Step 2

### Step 2 - Tools + Tool Choice Parity

File:

- `backend-go/internal/providers/responses_upstream.go`

Work:

- [x] Function tool conversion parity
- [x] Built-in web-search tool conversion (`web_search_preview` baseline)
- [x] Deterministic handling for malformed/unknown tools (drop + warn)
- [x] `tool_choice` mapping (`auto`/`none`/`any`/named tool)

Review gate:

- [x] Reviewer approval required before Step 3

### Step 3 - Deterministic Envelope Field Mapping

File:

- `backend-go/internal/providers/responses_upstream.go`

Work:

- [x] Add whitelist field mapping for `top_p`
- [x] Add deterministic `thinking -> reasoning` mapping
- [x] Ensure no generic unknown passthrough

Review gate:

- [x] Reviewer approval required before Step 4

### Step 4 - Targeted Test Coverage

File:

- `backend-go/internal/providers/responses_upstream_test.go` (new)

Test matrix:

- [x] Ordered mixed blocks: `text -> tool_use -> text -> tool_result`
- [x] Multiple `tool_use` blocks in one message
- [x] Multiple `tool_result` blocks in one message
- [x] Repeated tool call payloads (no dedupe)
- [x] Function tool mapping
- [x] Built-in web-search tool mapping
- [x] `tool_choice` mapping coverage
- [x] `top_p` mapping coverage
- [x] `thinking` mapping coverage
- [x] Text-only regression coverage

Review gate:

- [x] Reviewer approval required before final validation

### Step 5 - Validation + Post-Merge Review

Validation commands:

- [x] `cd backend-go && go test ./internal/providers -run 'Convert(ToResponsesRequest|MessagesToInput)' -count=1`
- [x] `cd backend-go && go test ./internal/converters -count=1`
- [x] `cd backend-go && go test ./...` (best-effort final sanity)

Post-merge review:

- [x] Reviewer sign-off required on merged result

## Risks and Mitigations

1. Built-in tool schema mismatch across clients
   - Mitigation: explicit mapping table + narrow baseline support + tests
2. Regressions in currently-working simple flows
   - Mitigation: keep existing text-only behavior and regression tests
3. Over-accepting unknown fields
   - Mitigation: strict whitelist with deterministic drop policy

## Acceptance Criteria

Implementation is complete only when all are true:

1. `/v1/messages -> Responses` request translation preserves mixed-block order.
2. All tool-use/tool-result blocks are preserved (no collapse).
3. Function tools and web-search built-in tools are converted deterministically.
4. `tool_choice`, `top_p`, and `thinking` mappings behave per this plan.
5. Targeted tests pass and reviewer gives final approval.
