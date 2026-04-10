# Example Prompts — Using CC-Bridge Instructions

This file shows real-world examples of how to invoke and reference the specialized instructions.

---

## Handler Development Examples

### Prompt 1: "Build the channel health check endpoint"

```
Create a new handler for channel health checks at GET /api/ping/:id

Requirements:
- Return { status: "healthy" | "unhealthy", latency_ms: 100 }
- Include error handling if channel not found
- Record metrics for each ping
- Write tests with table-driven approach

Reference: .github/instructions/handler-development.md
```

**What the AI will do:**
1. Open handler-development.md #The-Handler-Pattern
2. Copy the handler signature template
3. Implement the health check logic
4. Jump to #Testing section for test structure
5. Create both handler + test file

---

### Prompt 2: "Add request validation middleware"

```
Add middleware to validate all inbound requests:
- Check x-api-key header (required)
- Validate Content-Type is JSON
- Reject requests over 10MB
- Return 400 for invalid requests

Should hook into the request flow before handlers.

Hint: Check handler-development.md #Error-Handling for status codes
```

**What the AI will do:**
1. Read handler-development.md #Error-Handling for HTTP status codes
2. Create `backend-go/internal/middleware/validation.go`
3. Register in main.go before request routing
4. Write tests for each validation rule

---

## Provider Integration Examples

### Prompt 3: "Add support for Anthropic's Batch API"

```
Create a new provider for Anthropic Batch API that converts:
- Claude Messages format → Batch request format
- Batch response → Claude format

The batch API returns results asynchronously, but we'll implement 
synchronous polling for now.

Reference files:
- Existing provider example: backend-go/internal/providers/claude.go
- Template: .github/instructions/provider-integration.md

Include table-driven tests for request/response conversion.
```

**What the AI will do:**
1. Read provider-integration.md #Anatomy-of-a-Provider
2. Study `backend-go/internal/providers/claude.go` structure
3. Create anthropic_batch.go with ConvertRequest/Response/StreamResponse
4. Add tests from #Testing section template
5. Register in `GetProvider()` factory

---

### Prompt 4: "Support streaming from Cohere API"

```
Add Cohere as an upstream provider with streaming support.

Cohere's streaming format: newline-delimited JSON with "text" field

Follow the pattern from openai.go but adapt for Cohere's format.

Reference: .github/instructions/provider-integration.md #Streaming-Format-Differences
```

**What the AI will do:**
1. Jump to provider-integration.md #Streaming-Format-Differences 
2. See the pattern for handling different stream formats
3. Create cohere.go with custom StreamResponse parser
4. Handle Cohere's newline-delimited JSON format
5. Write streaming tests with mock data

---

## Frontend Component Examples

### Prompt 5: "Build the API key management UI"

```
Create a Vue 3 component for API key CRUD:
- List existing keys with revoke button
- Form to generate new key (show once after creation)
- Search/filter keys by name
- Confirm before revoking

Use Vuetify 3 v-data-table for the list view.
Format with Prettier (120 chars, no semicolons).

Reference: .github/instructions/frontend-components.md
```

**What the AI will do:**
1. Read frontend-components.md #Composition-API-Pattern 
2. Use template with defineProps, defineEmits, setup
3. Jump to #API-Client-Integration for API call patterns
4. Reference #Styling-Guide for Vuetify + Tailwind
5. Write TypeScript types for key data
6. Run formatter at the end

---

### Prompt 6: "Add real-time request log streaming"

```
Create a component that streams request logs in real-time using SSE.

Should:
- Connect to GET /api/logs?stream=true (SSE endpoint)
- Display logs in a scrollable table
- Auto-scroll to latest entry
- Show connection status (connected/disconnected)
- Handle reconnection on disconnect

Hint: Check useLogStream composable for SSE pattern.

Reference: .github/instructions/frontend-components.md #Composables
```

**What the AI will do:**
1. Find useLogStream composable in frontend/src/composables/
2. Read frontend-components.md #Using-Composables
3. See the #API-Client-Integration pattern for async data
4. Create component with SSE connection wrapper
5. Use Vuetify v-data-table for display
6. Add connection status indicator

---

## Backend Workflow Examples

### Prompt 7: "Set up local development with Go hot-reload"

```
I want to work on the Messages API handler and see changes immediately.

What's the dev workflow for:
1. Starting hot-reload
2. Running tests while developing
3. Debugging with breakpoints

Reference: .github/instructions/backend.md
```

**What the AI will do:**
1. Point to backend.md #Development-Workflow section
2. Show `make dev` for hot-reload
3. Show `make test` for running tests
4. Reference #Debugging section for Delve setup

---

### Prompt 8: "I need to cache config changes but still support hot-reload"

```
Current issue: Handlers fetch config fresh on every request 
(performance OK but want to optimize).

How can we cache config while still supporting hot-reload without restart?

Be sure to follow the patterns in: .github/instructions/backend.md #Configuration--Hot-Reload
```

**What the AI will do:**
1. Read backend.md #Configuration--Hot-Reload
2. Explain: "Always fresh per-request for hot-reload support"
3. Suggest: Optional middleware-level caching with invalidation
4. Show: How to hook into ConfigManager's Watch() for cache invalidation
5. Reference: config/config.go for hot-reload mechanism

---

## Database Migration Examples

### Prompt 9: "Add request deduplication table"

```
Add a schema to track request IDs for deduplication:
- Table: request_dedup
- Columns: request_id (PK), timestamp, ttl_days
- Index on timestamp for cleanup

Create migration with both UP and DOWN.
Include data cleanup logic for expired entries.

Reference: .github/instructions/database.md #Creating-New-Migrations
```

**What the AI will do:**
1. Read database.md #Naming-Convention
2. Determine next migration number: 011
3. Read #Migration-File-Format for structure
4. Create 011_add_request_dedup_table.sql
5. Add UP migration with table + index
6. Add DOWN migration to drop table
7. Test both directions

---

### Prompt 10: "Migrate from SQLite to PostgreSQL"

```
We need to support PostgreSQL for production deployments.

Steps:
1. Update connection logic to support both SQLite (dev) and PostgreSQL
2. Ensure schema is identical in both
3. Create migration tool to sync schema

Reference: .github/instructions/database.md
```

**What the AI will do:**
1. Show database.md #Database-Schema-Overview
2. Explain: Same schema, different connection strings
3. Suggest: Conditional in db initialization based on env var
4. Reference migrations to be version-agnostic
5. Create sync script for production data migration

---

## Integration Examples (Multiple Guides)

### Prompt 11: "Add quota enforcement to the Messages API"

```
Requirements:
1. Create new quota_limits table (migrations)
2. Add quota check middleware
3. Return 429 if quota exceeded
4. Add UI panel to set quotas

This touches:
- Database schema (.github/instructions/database.md)
- Middleware/handlers (.github/instructions/handler-development.md)
- Frontend (.github/instructions/frontend-components.md)

Start with the architecture; ask questions if anything is unclear.
```

**What the AI will do:**
1. Create migration: database.md #Creating-New-Migrations
2. Create middleware: handler-development.md #Error-Handling (429 status)
3. Create frontend panel: frontend-components.md component template
4. Wire everything together in main.go
5. Write integration tests

---

## Skill Usage Examples

### Prompt 12: "Release version 1.5.0"

```
We're ready to ship version 1.5.0 with the new quota system.

Process done in past releases:
- Bump VERSION, frontend/package.json, CHANGELOG.md
- Add conventional commit message
- Create git tag v1.5.0

Can you automate this?

Skill reference: .claude/skills/release-workflow/SKILL.md
```

**What the AI will do:**
1. Read release-workflow SKILL.md
2. Verify all tests pass first
3. Run: `/release-workflow bump minor quota`
4. Commits all changed files
5. Tags v1.5.0
6. Instructions complete, ready to `git push`

---

## Anti-Pattern Examples (What NOT to Do)

### Prompt 13: "Check my handler implementation against best practices"

```
I wrote this handler. Is it following cc-bridge patterns?

[code snippet of handler]

Reference: .github/instructions/handler-development.md #Code-Style--Conventions
```

**What the AI will do:**
1. Review against handler-development.md patterns
2. Point out: "Handler signature looks good, follows gin.HandlerFunc pattern"
3. Flag: "Config should be fetched fresh at request time, not cached"
4. Suggest: "Consider extracting business logic to separate function"
5. Reference: AGENTS.md for 50-line function limit

---

## Now You're Ready

These examples show how the instructions are designed to be **specific, actionable, and referenceable**.

**Key principles:**
1. ✅ **Be specific** — "Add handler for /api/ping" not just "add endpoint"
2. ✅ **Reference the guide** — "See handler-development.md #Testing"
3. ✅ **Show the context** — "I'm working on quota system, need to..." 
4. ✅ **Ask for patterns** — "Should I follow the approach in..."
5. ✅ **Verify against templates** — "Does my code match the pattern?"

---

## See Also

- **Main instructions**: [../copilot-instructions.md](../copilot-instructions.md)
- **Instructions directory**: [README.md](README.md)
- **Implementation plan template**: [../../docs/implementation_plans/TEMPLATE.md](../../docs/implementation_plans/TEMPLATE.md)

---

*Last updated: April 2026*
