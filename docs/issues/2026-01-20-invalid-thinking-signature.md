# Invalid `signature` in `thinking` block after switching upstreams

Date: 2026-01-20

## Symptom

When sending a Claude Messages API request to an official Claude upstream, the upstream returns an error similar to:

```json
{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid `signature` in `thinking` block"},"request_id":"req_..."}
```

Captured in `sample_request/response`.

## Sample request

Captured request body in `sample_request/resquest` (note the filename spelling).

The error path `messages.1.content.0` matches the request: `messages[1].content[0]` is a `thinking` content block on an assistant message and contains a `signature`:

```json
{
  "type": "thinking",
  "thinking": "...",
  "signature": "claude#RXQ0RkNrZ0lDe..."
}
```

## Finding: signature looks double-base64 encoded

The signature has the form `claude#<outer>`, where `<outer>` is base64.

If we base64-decode `<outer>` once, the result is printable ASCII that itself *looks like base64* (in this sample it starts with `Et4F...`).

Measurements from `sample_request/resquest`:

- `outer` length: 1320 chars
- `base64_decode(outer)` → `inner` (ASCII) length: 988 chars, prefix `Et4F...`
- `base64_decode(inner)` → `inner2` (bytes) length: 739 bytes

This strongly suggests an extra wrapping layer:

- **Expected (single layer):** `claude#<inner>`
- **Observed (double layer):** `claude#base64(<inner-as-text>)`

## Why this triggers the error

Anthropic validates `thinking.signature` for `thinking` blocks. If a proxy/translator re-encodes or otherwise modifies the signature, it no longer matches what Anthropic expects and is rejected with:

`Invalid \`signature\` in \`thinking\` block`

This can surface after switching upstreams because the conversation history is re-sent, including an earlier assistant `thinking` block.

## Hypothesis (likely root cause)

An upstream translator/proxy (the upstream path described as "google antigravy") is treating an already-base64 signature string as raw bytes and base64-encoding it again, producing `claude#base64(base64(signature_bytes))`.

## Possible mitigations (to evaluate when more samples exist)

1. **Normalize (unwrap) one base64 layer** for `claude#...` signatures when they match the “double-encoded” shape:
   - `signature` starts with `claude#`
   - `base64_decode(outer)` yields printable ASCII
   - decoded ASCII itself is valid base64 and decodes cleanly
   - rewrite to `signature = "claude#" + inner` (do **not** decode further)

2. **Strip prior `thinking` blocks** from `messages[*].content` when sending to official Claude:
   - most robust across upstream boundaries
   - loses historical `thinking` artifacts (but typically preserves normal conversation if `text` + `tool_*` are kept)

## More samples to collect

- A request that succeeds *before* switching upstream, with the same conversation history
- The response (from the "google antigravy" upstream) that created the `thinking` block
- Whether the issue also happens with `redacted_thinking`
- Channel config for both upstreams (`serviceType`, `baseUrl`, `modelMapping`)

