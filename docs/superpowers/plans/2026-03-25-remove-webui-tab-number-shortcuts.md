# Remove WebUI Tab Number Shortcuts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the numeric keyboard shortcuts that switch top-level WebUI tabs while preserving existing `Escape` dialog-closing behavior.

**Architecture:** The change stays inside `frontend/src/App.vue`, where the global `keydown` handler already separates `Escape` handling from numeric tab switching. We will delete only the numeric tab-switch block so the rest of the handler and lifecycle wiring remain unchanged.

**Tech Stack:** Vue 3, TypeScript, Vuetify, Vite, Bun

---

### Task 1: Remove Numeric Tab Switching

**Files:**
- Modify: `frontend/src/App.vue`
- Validation: `frontend/package.json`

- [ ] **Step 1: Confirm the current shortcut logic location**

Read `frontend/src/App.vue` and verify that `handleKeydown` maps number keys to
`activeTab` separately from the `Escape` block.

- [ ] **Step 2: Remove the numeric tab-switch block**

Delete the authenticated `event.key === '1'/'2'/'3'/'4'` logic and leave the
rest of `handleKeydown` unchanged.

- [ ] **Step 3: Run type-check verification**

Run: `bun run type-check`
Expected: command succeeds with no TypeScript errors.

- [ ] **Step 4: Run build verification**

Run: `bun run build`
Expected: Vite production build succeeds.

- [ ] **Step 5: Review the merged result**

Check the diff and confirm:
- only the numeric shortcut behavior was removed
- `Escape` handling still exists
- no unrelated frontend behavior was edited
