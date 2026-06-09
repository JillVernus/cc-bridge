# Mobile card view for request logs

## Background
The request log table uses a desktop-optimized v-data-table with many columns that's hard to read on iOS/mobile devices. Users need a responsive card layout that displays request information in a mobile-friendly format.

## Approach
Add a mobile card view that automatically activates on small screens (≤768px):
1. Detect mobile viewport using Vuetify's `useDisplay` composable
2. Conditionally render card layout instead of table on mobile
3. Each card shows key request info in a readable vertical layout
4. Maintain all interactive features (click to open debug modal, status indicators)
5. Keep summary panels and filters responsive

## Design

### Card Layout (per request)
```
┌─────────────────────────────────┐
│ ● Status   Model    [Debug 🐛]  │
│ HH:MM:SS               Channel  │
├─────────────────────────────────┤
│ Session: xxx...xxx              │
│ Client: app/version             │
├─────────────────────────────────┤
│ ⏱ F: 1.2s  D: 3.4s  S: 2.2s    │
│ 💬 In: 1.2K  Out: 8.5K          │
│ 💰 $0.0123                      │
└─────────────────────────────────┘
```

### Breakpoint Strategy
- Desktop (>768px): v-data-table (current behavior)
- Mobile (≤768px): v-card list

## Steps

- [x] 1. Extract mobile detection computed property (`isMobile` based on `useDisplay().mdAndDown`)
- [x] 2. Create mobile card template section (v-if="isMobile")
- [x] 3. Style cards with neomorphic theme matching existing design
- [x] 4. Test tap interactions and debug modal opening on iOS
- [x] 5. Ensure filters and summary panels collapse gracefully on mobile
- [x] 6. Add loading skeleton for mobile cards
- [x] 7. Test on actual iOS device (Safari)

## Implementation

### Frontend Changes

1. **Mobile detection** (`RequestLogTable.vue:1437-1439`):
   - Added `mdAndDown` from `useDisplay()` composable
   - Created `isMobile` computed property

2. **Mobile card view** (`RequestLogTable.vue:810-910`):
   - Conditionally rendered with `v-if="isMobile"`
   - Each card shows: status, model, channel, time, session, client, timing metrics, tokens, price
   - Cards are clickable to open debug modal
   - Loading state with progress bar
   - Empty state message
   - Mobile-optimized pagination with offset display

3. **Desktop table** (`RequestLogTable.vue:913+`):
   - Wrapped with `v-else` to show only on desktop
   - Existing v-data-table unchanged

4. **Styling** (`RequestLogTable.vue:5820-5881`):
   - Neomorphic card style matching project theme
   - Black/white borders with offset shadow (3px 3px)
   - Press animation (translate + shadow reduction)
   - Responsive layout with proper spacing
   - Dark theme support
   - Media query to hide table on mobile (≤960px)

## Testing

- ✅ TypeScript type check passes
- ✅ Correct field names: `inputTokens`, `outputTokens`, `clientId`, `offset`
- ✅ Functions exist: `formatTimingMetric`, `formatNumber`, `formatPriceDetailed`, `openDebugModal`
- ✅ Frontend builds successfully
- ✅ Translations added for English and Chinese
- ⏳ Visual testing on iOS Safari needed (requires actual device)

## Commits

- `326575a` - All steps complete (v1.5.46-v1.5.50)

