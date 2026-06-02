# Add new-row slide-in animation

## Background

The requests page (`frontend/src/components/RequestLogTable.vue`) already pushes new
rows instantly via SSE (`log:created`) and already flashes new/updated rows green
(`row-flash` keyframe). What it lacks — compared to the axonhub reference effect — is
the physical motion: a new row currently just *pops* into the top of the table. The user
wants new rows to *slide in* from the top while keeping the existing green flash (a
stock-ticker style reveal).

This is purely additive (Option A from the assessment): layer a slide-in transform on
top of the existing instant-push + green-flash behavior. No change to SSE timing, no new
dependencies, no change to the table machinery.

Known limitation (accepted): with a CSS-only enter animation, the entering row animates
but the rows below shift in one step rather than smoothly gliding down. The full
push-down (FLIP) effect would require re-hosting rows in a `<TransitionGroup>` (Option B),
which is out of scope here.

## Approach

- Distinguish *newly created* rows (slide-in + flash) from *updated* rows (flash only)
  by introducing a `newIds` reactive set alongside the existing `updatedIds`.
- Add a `row-enter` keyframe that combines `translateY(-20px) -> 0` + opacity with the
  existing green background fade, and apply it via `getRowProps`.
- Wire `handleLogCreated` (SSE) and the polling-fallback silent-refresh path to populate
  `newIds` for genuinely new rows; keep `updatedIds` for status changes.

## Steps

- [x] Step 1: Add `newIds` reactive set near `updatedIds`
- [x] Step 2: Update `getRowProps` to apply `row-enter` (new) vs `row-flash` (updated)
- [x] Step 3: Set `newIds` in `handleLogCreated` instead of `updatedIds`
- [x] Step 4: Split new-vs-updated detection in the silent-refresh (polling) path
- [x] Step 5: Add `row-enter` keyframe + selector in the component styles
- [x] Step 6: Verify with `bun run type-check`

## Follow-up: push-down (FLIP) motion

After testing, the new row slid in but the rows below snapped down (the accepted CSS-only
limitation). Added the FLIP technique to make existing rows glide down — without giving up
the Vuetify `v-data-table` or its custom cell slots:

- [x] Add `logTableRef` on the `v-data-table`
- [x] `captureRowTops()` — record each row's `top` (keyed by log id) before the prepend
- [x] `playRowShift()` — after `nextTick`, invert the delta with `transform: translateY`
      then transition it back to 0 (300ms ease-out); newly inserted row is skipped so its
      `row-enter` animation is untouched
- [x] Wire both into `handleLogCreated` around the list mutation
- [x] Re-verify `bun run type-check` + tests (26 pass / 0 fail)

This is the real axonhub effect (equivalent to framer-motion's `layout` prop), implemented
with plain DOM measurement — no new dependency.

## Commits

[Added after each commit]
