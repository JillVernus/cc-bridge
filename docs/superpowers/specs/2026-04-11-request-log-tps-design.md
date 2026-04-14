# Request Log TPS Design

## Goal

Add a `tokens per second` metric to the request log UI so users can quickly compare end-to-end output throughput in both the main request log table and the request details modal.

## Scope

- Show TPS in the main request log table.
- Show TPS in the request details modal metadata.
- Compute TPS from existing request-log fields already available in the frontend.
- Do not add a backend schema field or persist TPS in the database.

## Metric Definition

TPS means end-to-end output throughput over the full request duration.

Formula:

`TPS = outputTokens / (durationMs / 1000)`

Guardrails:

- Show `-` for pending requests.
- Show `-` when `outputTokens <= 0`.
- Show `-` when `durationMs <= 0`.

Display format:

- Round to 1 decimal place.
- Render as `28.4 tok/s`.

## Architecture

Use a small shared frontend helper that accepts a `RequestLog` and returns either a numeric TPS value or `null` when the metric is not valid. Both the request log table and request details modal will use the same helper to avoid drift.

This approach avoids backend changes because the necessary source fields already exist in API payloads and SSE updates:

- `outputTokens`
- `durationMs`
- `status`

## UI Placement

### Main Request Log Table

Add a new `TPS` column near the existing timing columns so users can read:

- first token latency
- total duration
- end-to-end output throughput

The cell should show:

- `-` for invalid or pending records
- formatted TPS text for valid completed/error rows

### Request Details Modal

Add a new row in the `Timing` section below `Duration`:

- `TPS`

This keeps timing metrics grouped together in one place.

## Testing

Because the frontend currently does not have a component test harness, add focused unit-style coverage around the shared helper:

- valid completed request returns expected TPS
- pending request returns null
- missing first-token timing still returns expected TPS when total duration exists
- zero or negative total duration returns null
- zero output tokens returns null

Then run frontend type-checking to verify the component integrations.
