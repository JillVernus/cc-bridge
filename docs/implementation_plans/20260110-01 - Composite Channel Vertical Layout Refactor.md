# Composite Channel Vertical Layout Refactor

## Background

The current CompositeChannelEditor uses a horizontal layout for the failover chain:

```
model : [Channel A] → [Channel B] → [Channel C] [+]
```

This creates UX issues:
1. **Width constraints** - Only fits 2 channels before wrapping to the next line
2. **Inconsistent wrapping** - Wrapped items look disconnected from the chain
3. **Poor semantic mapping** - Failover chains are sequential (try first → fallback), which maps better to vertical flow

## Approach

Refactor to a vertical card-based layout where each model pattern gets its own expandable card with a vertical channel list:

```
┌─────────────────────────────────────┐
│ haiku                          [✓]  │
├─────────────────────────────────────┤
│  ① [Channel A Dropdown        ▼]   │
│              ↓                      │
│  ② [Channel B Dropdown        ▼]   │
│              ↓                      │
│  ③ [Channel C Dropdown        ▼]   │
│                                     │
│  [+ Add Fallback Channel]           │
└─────────────────────────────────────┘
```

Key design decisions:
- Each channel row has a priority number badge (①②③)
- Downward arrows (↓) between channels indicate failover direction
- Remove button (×) on each non-primary channel
- Full-width dropdowns for better readability
- Add button at the bottom of the chain

## Steps

- [x] Step 1: Refactor template structure
  - Replace horizontal `channel-chain` with vertical `channel-list`
  - Change arrow icon from `mdi-arrow-right` to `mdi-arrow-down`
  - Move add button below the channel list
  - Add priority number badges to each channel row

- [x] Step 2: Update CSS styles
  - Remove horizontal flex layout from `.channel-chain`
  - Create new `.channel-list` with vertical flex direction
  - Style priority badges (circular, numbered)
  - Adjust spacing for vertical layout
  - Make channel dropdowns full-width within container

- [x] Step 3: Enhance channel row structure
  - Wrap each channel in a styled row container
  - Position remove button inline with dropdown
  - Add hover state for better interactivity
  - Style primary channel distinctly (first in chain)

- [x] Step 4: Update responsive styles
  - Adjust mobile breakpoint styles
  - Ensure touch-friendly spacing
  - Test at various viewport widths

- [x] Step 5: Verify and test
  - Run `bun run type-check` to verify no TypeScript errors
  - Manual visual verification in browser
  - Test add/remove channel functionality
  - Test dropdown selection behavior

## Commits

- `8e38dbb` - refactor: composite channel editor vertical 3-column layout v1.3.153

## Technical Notes

### Files to modify
- `frontend/src/components/CompositeChannelEditor.vue` - Main component (template + styles)

### No changes needed
- Script logic remains the same (add/remove/update functions)
- Props and emits unchanged
- Validation logic unchanged

### CSS class changes
| Current | New |
|---------|-----|
| `.channel-chain` | `.channel-list` (vertical) |
| `.channel-select-wrapper` | `.channel-row` (full-width) |
| N/A | `.priority-badge` (new) |
| N/A | `.arrow-separator` (new) |
