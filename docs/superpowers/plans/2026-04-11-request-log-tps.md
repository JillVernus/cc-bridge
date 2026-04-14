# Request Log TPS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end TPS to the request log table and request details modal using existing request-log fields.

**Architecture:** Introduce a small shared frontend helper that computes TPS from `outputTokens` and `durationMs`, then reuse it in both UI surfaces. Keep the change frontend-only to avoid redundant persisted data and backend payload churn.

**Tech Stack:** Vue 3, TypeScript, vue-tsc, Bun

---

### Task 1: Add Shared TPS Helper With Test Coverage

**Files:**
- Create: `frontend/src/utils/requestLogTps.ts`
- Create: `frontend/src/utils/requestLogTps.test.ts`
- Modify: `frontend/package.json`

- [ ] **Step 1: Write the failing test**

Add a small test file that covers:
- valid completed request computes expected TPS
- pending request returns null
- missing `firstTokenDurationMs` still computes TPS when `durationMs` exists
- zero or negative total duration returns null
- zero `outputTokens` returns null

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && bun test src/utils/requestLogTps.test.ts`
Expected: FAIL because helper does not exist yet and `test` script support is not wired.

- [ ] **Step 3: Write minimal implementation**

Create a helper that:
- exports a function to compute numeric TPS or `null`
- exports a formatter returning `-` or `N.N tok/s`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && bun test src/utils/requestLogTps.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/src/utils/requestLogTps.ts frontend/src/utils/requestLogTps.test.ts
git commit -m "feat: add request log tps helper"
```

### Task 2: Show TPS In The Request Log Table

**Files:**
- Modify: `frontend/src/components/RequestLogTable.vue`
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/zh-CN.ts`

- [ ] **Step 1: Write the failing integration expectation**

Use the helper test file or a small additional assertion if needed to lock formatting behavior used by the table label.

- [ ] **Step 2: Run the relevant test to verify it fails**

Run: `cd frontend && bun test src/utils/requestLogTps.test.ts`
Expected: FAIL if the new formatting/label behavior is not implemented yet.

- [ ] **Step 3: Write minimal implementation**

Update the table to:
- import the shared TPS formatter
- add a `tps` column definition and defaults
- render `-` for invalid values
- show formatted TPS for valid rows

- [ ] **Step 4: Run verification**

Run: `cd frontend && bun test src/utils/requestLogTps.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/RequestLogTable.vue frontend/src/locales/en.ts frontend/src/locales/zh-CN.ts
git commit -m "feat: show request log tps in table"
```

### Task 3: Show TPS In The Request Details Modal And Verify Frontend

**Files:**
- Modify: `frontend/src/components/RequestDebugModal.vue`

- [ ] **Step 1: Write the failing expectation**

Extend the helper test coverage if needed to lock the final display format shared by the modal.

- [ ] **Step 2: Run the relevant test to verify it fails**

Run: `cd frontend && bun test src/utils/requestLogTps.test.ts`
Expected: FAIL if the final formatting contract changed and is not implemented.

- [ ] **Step 3: Write minimal implementation**

Update the modal timing section to show a `TPS` row using the shared helper.

- [ ] **Step 4: Run full frontend verification**

Run:
- `cd frontend && bun test src/utils/requestLogTps.test.ts`
- `cd frontend && bun run type-check`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/RequestDebugModal.vue
git commit -m "feat: show request log tps in details modal"
```
