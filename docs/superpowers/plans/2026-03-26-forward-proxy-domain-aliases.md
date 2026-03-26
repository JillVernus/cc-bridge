# Forward Proxy Domain Aliases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable forward-proxy domain aliases so intercepted domains can display a short alias everywhere they appear in the UI, especially the request log channel column.

**Architecture:** Extend the persisted forward-proxy config with an exact-match `domainAliases` map keyed by normalized host. Centralize alias resolution in forward-proxy backend helpers, then expose the alias map through the existing forward-proxy settings and discovery views so intercepted request logs and other forward-proxy displays reuse the same names.

**Tech Stack:** Go 1.22 backend, Gin handlers, existing forward-proxy JSON config, Vue 3 + Vuetify frontend, TypeScript API models.

---

### Task 1: Backend Alias Resolution

**Files:**
- Modify: `backend-go/internal/forwardproxy/server.go`
- Modify: `backend-go/internal/forwardproxy/requestlog_capture.go`
- Test: `backend-go/internal/forwardproxy/requestlog_capture_test.go`

- [ ] **Step 1: Write failing backend tests**

Add tests that expect:
- normalized config stores lowercase trimmed host aliases
- intercepted request logs use alias in `ProviderName`
- raw host is still used when no alias exists

- [ ] **Step 2: Run backend tests to verify failure**

Run: `go test ./internal/forwardproxy -run 'Test(HandleHTTPForward_InterceptedUnknownEndpointCreatesMainLogRow|CreateInterceptedPendingLog_UsesConfiguredDomainAlias|NormalizeDomainAliases)'`
Expected: FAIL because domain alias config and resolution do not exist yet

- [ ] **Step 3: Implement minimal backend support**

Add `DomainAliases map[string]string` to forward-proxy config, normalize alias keys/values on load/update, and add a helper to resolve display names for intercepted hosts. Use that helper when creating intercepted pending/completion log records.

- [ ] **Step 4: Run backend tests to verify pass**

Run: `go test ./internal/forwardproxy -run 'Test(HandleHTTPForward_InterceptedUnknownEndpointCreatesMainLogRow|CreateInterceptedPendingLog_UsesConfiguredDomainAlias|NormalizeDomainAliases)'`
Expected: PASS

### Task 2: Frontend Config And Reused Display

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/ForwardProxySettings.vue`
- Modify: `frontend/src/components/ForwardProxyDiscoveryView.vue`

- [ ] **Step 1: Extend frontend types first**

Add `domainAliases` to the forward-proxy config API types so UI code is type-safe.

- [ ] **Step 2: Implement settings editor**

Add editable rows for `domain -> alias`, filter empty values on save, and keep the interaction aligned with the existing intercept-domain editor.

- [ ] **Step 3: Reuse aliases in discovery display**

Show the alias next to a discovered host when configured so the discovery tab matches the log display.

- [ ] **Step 4: Run frontend verification**

Run: `cd frontend && bun run build`
Expected: PASS

### Task 3: Final Verification

**Files:**
- Modify: `backend-go/internal/forwardproxy/requestlog_capture_test.go` (if assertion polish is needed)

- [ ] **Step 1: Run focused backend package tests**

Run: `cd backend-go && go test ./internal/forwardproxy`
Expected: PASS

- [ ] **Step 2: Run frontend build again after final review**

Run: `cd frontend && bun run build`
Expected: PASS

- [ ] **Step 3: Summarize migration behavior**

Document that new intercepted logs use aliases immediately while historical log rows keep their previously stored provider name until new traffic updates them.
