# Mobile-friendly summary cards

## Background
The summary tables (statistics by provider/model/client/session/apiKey) at the top of the request log page use horizontal scrolling on mobile. iOS Safari has poor support for horizontal scroll gestures on table elements, making the right columns (output tokens, cache metrics, cost) inaccessible on iPhone/iPad.

## Approach
Convert summary tables to vertical card stacks on mobile (≤960px):
1. Detect mobile viewport (reuse existing `isMobile` computed)
2. Replace horizontal table with vertical card list for each row
3. Each card shows: name, requests, input, output, cache stats, cost
4. Use same neomorphic card style as request log cards
5. Keep desktop table view unchanged

## Design

### Desktop (current)
```
┌────────────────────────────────────────────────┐
│ Provider │ Requests │ Input │ Output │ Cost   │
├──────────┼──────────┼───────┼────────┼────────┤
│ Claude   │    1,234 │ 123K  │  456K  │ $1.23  │
│ OpenAI   │      567 │  78K  │  234K  │ $0.67  │
└────────────────────────────────────────────────┘
```

### Mobile (new)
```
┌─────────────────────────────────┐
│ 🔹 Claude                       │
│ 1,234 requests                  │
│ In: 123K | Out: 456K | $1.23   │
└─────────────────────────────────┘
┌─────────────────────────────────┐
│ 🔹 OpenAI                       │
│ 567 requests                    │
│ In: 78K | Out: 234K | $0.67    │
└─────────────────────────────────┘
```

## Steps

- [x] 1. Add mobile summary card template (v-if="isMobile" in summary section)
- [x] 2. Create card layout showing all metrics vertically
- [x] 3. Handle all groupBy modes (provider/model/client/session/apiKey)
- [x] 4. Style cards to match request log mobile cards
- [x] 5. Add loading skeleton for mobile summary cards
- [x] 6. Test on iOS Safari

## Implementation

### Frontend Changes

1. **Mobile summary cards** (`RequestLogTable.vue:50-130`):
   - Conditionally rendered with `v-if="isMobile"` after group-by toggle
   - Each card shows: name/key, request count chip, input/output tokens, cache creation/hit, hit rate, cost
   - Supports all groupBy modes (provider, model, client, session, apiKey)
   - Clickable client IDs open alias dialog
   - Flash animation on updates (reuses `currentUpdatedSet`)
   - Total card with primary color background
   - Empty state message

2. **Desktop table** (`RequestLogTable.vue:133-385`):
   - Wrapped with `v-else` (desktop only)
   - Existing table with headers, body, footer unchanged

3. **Styling** (`RequestLogTable.vue:5957-6025`):
   - `.mobile-summary-list`: Max height 400px with scrolling
   - `.mobile-summary-card`: Neomorphic style (2px border + 2px shadow)
   - `.mobile-summary-total`: Primary border with 3px offset shadow
   - Responsive icons and compact layout
   - Dark theme support

## Testing

- ✅ TypeScript type check passes
- ✅ Frontend builds successfully
- ✅ All groupBy modes supported
- ✅ Reuses existing functions: `formatSummaryKey`, `formatNumber`, `formatPriceSummary`, `calcHitRate`
- ✅ Client ID click interaction preserved
- ⏳ Visual testing on iOS Safari needed (no horizontal scroll required)

## Commits

- `326575a` - All steps complete (v1.5.46-v1.5.50)
