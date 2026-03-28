# X-Initiator Log Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a compact forward-proxy-only indicator in the main request log table that shows whether a request arrived with `X-Initiator: user` and whether it was overridden to `agent`.

**Architecture:** Persist the original and effective `X-Initiator` values on forward-proxy request-log records, include them in request-log SSE payloads, and render a small icon in the model cell for intercepted forward-proxy rows. The icon stays compact while the tooltip provides the exact meaning (`user` or `user → agent`).

**Tech Stack:** Go request-log manager and forward-proxy handlers, Vue 3 + Vuetify request log table, SQLite-backed request log storage

---

### Task 1: Add request-log persistence for initiator metadata

**Files:**
- Modify: `backend-go/internal/requestlog/types.go`
- Modify: `backend-go/internal/requestlog/manager.go`
- Modify: `backend-go/internal/requestlog/events.go`
- Modify: `backend-go/internal/requestlog/pg_notify.go`
- Create: `backend-go/internal/database/migrations/011_request_logs_x_initiator.sql`
- Test: `backend-go/internal/requestlog/service_tier_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRequestLog_XInitiatorMetadata_RoundTrip(t *testing.T) {
	record := &RequestLog{
		OriginalXInitiator:  "user",
		EffectiveXInitiator: "agent",
	}
	// Assert GetByID, GetRecent, SSE complete, SSE partial, and event payloads preserve both fields.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend-go && go test ./internal/requestlog -run TestRequestLog_XInitiatorMetadata_RoundTrip`
Expected: FAIL because the request log schema and event payloads do not yet carry initiator fields.

- [ ] **Step 3: Write minimal implementation**

```go
type RequestLog struct {
	OriginalXInitiator  string `json:"originalXInitiator,omitempty"`
	EffectiveXInitiator string `json:"effectiveXInitiator,omitempty"`
}
```

Add request-log columns plus scan/insert/update/query wiring in the request log manager, PostgreSQL LISTEN fetch paths, and a shared-database migration so upgraded unified-database deployments receive the new columns too. Add both fields to log created/updated SSE payloads.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend-go && go test ./internal/requestlog -run TestRequestLog_XInitiatorMetadata_RoundTrip`
Expected: PASS

### Task 2: Populate initiator metadata for forward-proxy intercepted logs

**Files:**
- Modify: `backend-go/internal/forwardproxy/requestlog_capture.go`
- Modify: `backend-go/internal/forwardproxy/interceptor.go` (if needed for propagation)
- Test: `backend-go/internal/forwardproxy/first_token_paths_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHandleHTTPForward_PersistsXInitiatorMetadata(t *testing.T) {
	req.Header.Set("X-Initiator", "user")
	// Assert original=user and effective=user on first request,
	// or original=user and effective=agent when override is active.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend-go && go test ./internal/forwardproxy -run TestHandleHTTPForward_PersistsXInitiatorMetadata`
Expected: FAIL because intercepted request logs do not yet record initiator metadata.

- [ ] **Step 3: Write minimal implementation**

```go
func createInterceptedPendingLog(...) *requestlog.RequestLog {
	return &requestlog.RequestLog{
		OriginalXInitiator:  original,
		EffectiveXInitiator: effective,
	}
}
```

Capture the original header before override is applied, capture the effective header after override is applied, and store both on the pending forward-proxy log record.
Ensure the pending-to-completed lifecycle preserves these values by keeping existing DB values when completion updates omit them.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend-go && go test ./internal/forwardproxy -run TestHandleHTTPForward_PersistsXInitiatorMetadata`
Expected: PASS

### Task 3: Render compact model-cell indicator in the request log table

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/composables/useLogStream.ts`
- Modify: `frontend/src/components/RequestLogTable.vue`

- [ ] **Step 1: Extend frontend request-log types**

```ts
originalXInitiator?: string
effectiveXInitiator?: string
```

- [ ] **Step 2: Wire SSE/list payloads into table state**

Update `handleLogCreated` and `handleLogUpdated` to retain both initiator fields on in-memory rows.

- [ ] **Step 3: Add the compact indicator**

```vue
<v-icon v-if="showXInitiatorIcon(item)">mdi-account-outline</v-icon>
<v-icon v-else-if="showXInitiatorOverrideIcon(item)">mdi-account-switch-outline</v-icon>
```

Render only for forward-proxy intercepted rows where the original initiator was `user`, and use a tooltip to distinguish `user` from `user → agent`. Apply the indicator in both the dedicated model column and the stacked provider+model rendering so responsive mode stays consistent.

- [ ] **Step 4: Verify frontend build**

Run: `cd frontend && bun run build`
Expected: PASS

### Task 4: Final verification

**Files:**
- No code changes expected

- [ ] **Step 1: Run focused backend tests**

Run: `cd backend-go && go test ./internal/requestlog ./internal/forwardproxy`
Expected: PASS

- [ ] **Step 2: Run frontend type/build verification**

Run: `cd frontend && bun run type-check && bun run build`
Expected: PASS
