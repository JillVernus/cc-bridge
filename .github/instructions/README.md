# Instructions Directory — Quick Reference

This directory contains specialized development guides for different parts of cc-bridge.

---

## When to Use Each Guide

| Guide | Best For | Time to Read |
|-------|----------|--------------|
| **[handler-development.md](handler-development.md)** | Building HTTP endpoints, streaming, error handling | 10-15 min |
| **[provider-integration.md](provider-integration.md)** | Adding upstream AI providers, protocol conversion | 10-15 min |
| **[frontend-components.md](frontend-components.md)** | Vue 3 components, Vuetify, TypeScript patterns | 10-15 min |
| **[backend.md](backend.md)** | Go project structure, patterns, testing, deployment | 15-20 min |
| **[database.md](database.md)** | Schema migrations, SQLite/PostgreSQL, versioning | 10-15 min |

---

## Getting Started

### Scenario 1: "I'm building a new API endpoint"

**Follow this path:**
1. Read [backend.md](backend.md) (30 sec: Quick Facts section)
2. Read [handler-development.md](handler-development.md) (full)
3. Reference [The Handler Pattern](handler-development.md#the-handler-pattern) section
4. Use [Testing](handler-development.md#testing) section to write tests

**Files you'll create:**
- `backend-go/internal/handlers/myendpoint.go`
- `backend-go/internal/handlers/myendpoint_test.go`

---

### Scenario 2: "I'm adding a new upstream AI provider"

**Follow this path:**
1. Read [provider-integration.md](provider-integration.md) (full)
2. Reference [The Provider Interface](provider-integration.md#the-provider-interface) section
3. Look at existing providers: `backend-go/internal/providers/{claude,openai,gemini}.go`
4. Use [Testing](provider-integration.md#testing) to validate

**Files you'll create:**
- `backend-go/internal/providers/myprovider.go`
- `backend-go/internal/providers/myprovider_test.go`

---

### Scenario 3: "I'm building a frontend UI component"

**Follow this path:**
1. Read [frontend-components.md](frontend-components.md) (full)
2. Start with [Composition API Pattern](frontend-components.md#composition-api-pattern)
3. Reference [Common Patterns](frontend-components.md#common-patterns) as needed
4. Use [Styling Guide](frontend-components.md#styling-guide) for CSS

**Files you'll create:**
- `frontend/src/components/MyComponent.vue`
- Update `frontend/src/locales/en.json` and `zh.json` for i18n

---

### Scenario 4: "I need to understand the Go backend architecture"

**Follow this path:**
1. Read [backend.md](backend.md) (full) for project structure
2. Read [Handler Development Pattern](backend.md#handler-development-pattern) section
3. Read [Provider Development Pattern](backend.md#provider-development-pattern) section
4. Reference subsystems: [Channel Scheduler](backend.md#channel-scheduler), [Metrics & Health](backend.md#metrics--health)

**Key files to study:**
- `backend-go/main.go` — entry point, routing setup
- `backend-go/internal/handlers/proxy.go` — Messages API implementation
- `backend-go/internal/scheduler/channel_scheduler.go` — scheduling logic

---

### Scenario 5: "I'm adding a database schema change"

**Follow this path:**
1. Read [database.md](database.md) (full)
2. Read [Migration File Format](database.md#migration-file-format)
3. Reference [Common Migration Patterns](database.md#common-migration-patterns)
4. Use [Testing Migrations](database.md#testing-migrations) to validate

**Files you'll create:**
- `backend-go/cmd/dbmigrate/migrations/NNN_your_migration.sql`

---

## Tips for AI Assistants

1. **Always check the main copilot-instructions.md first**
   - Provides overall project context
   - Links to these specialized guides
   - Contains key concepts and anti-patterns

2. **Use the "WHERE TO LOOK" tables**
   - Each guide has a quick reference of file locations
   - Speeds up navigation in large codebase

3. **Follow the patterns documented**
   - Each guide shows tested patterns from existing code
   - Copy-paste where possible; customize where needed

4. **Check for type examples**
   - HTTP request/response samples
   - Config structure examples
   - Test patterns you can reuse

5. **Reference existing implementations**
   - Look at similar existing code before creating new
   - Many patterns already proven in current codebase

---

## Index of All Guides

### Backend

- [backend.md](backend.md) — Go project structure, development, testing, deployment
- [handler-development.md](handler-development.md) — HTTP endpoint patterns, streaming, error handling
- [provider-integration.md](provider-integration.md) — Adding upstream providers, protocol conversion
- [database.md](database.md) — Schema migrations, versioning, troubleshooting

### Frontend

- [frontend-components.md](frontend-components.md) — Vue 3 components, Vuetify, TypeScript, i18n, API integration

### Release & Operations

See skill files in `.claude/skills/release-workflow/` for automated versioning

---

## Common Questions

### Q: I don't know where to start. What should I read first?

**A:** Read [../copilot-instructions.md](../copilot-instructions.md) (root instructions). It has the high-level overview and points to these specialized guides.

### Q: Can I skip the "Quick Facts" sections?

**A:** No. Quick Facts give you the essential context (file locations, key patterns) needed to understand the rest. Reading it takes 30 seconds; understanding the guide without it takes 10x longer.

### Q: Where do I find examples of existing code?

**A:** Each guide has references to existing files. For example:
- [handler-development.md](handler-development.md#testing) references `backend-go/internal/handlers/*_test.go`
- [frontend-components.md](frontend-components.md#component-map) references `frontend/src/components/` directory

### Q: How do I know if I'm following the right pattern?

**A:** Look for `✅ Good` and `❌ Bad` examples in the guides. Also check the corresponding AGENTS.md file in that subsystem (`backend-go/internal/handlers/AGENTS.md`, etc.)

### Q: Should I read all the guides?

**A:** No. Read only what's relevant to your task:
- Adding backend feature? → [backend.md](backend.md) + [handler-development.md](handler-development.md)
- Adding provider? → [provider-integration.md](provider-integration.md)
- Frontend work? → [frontend-components.md](frontend-components.md)
- Database changes? → [database.md](database.md)

Reading time for one focused guide: 10-20 minutes.

---

## Feedback & Updates

These guides are living documents. If you find:
- **Unclear sections** — Ask for clarification
- **Outdated patterns** — Report and I'll update
- **Missing examples** — Request additions
- **Incorrect information** — Correct me; I appreciate corrections

Last updated: April 2026

---

## Quick Links

- **Project root instructions**: [../copilot-instructions.md](../copilot-instructions.md)
- **AGENTS.md files** (detailed patterns): [../../AGENTS.md](../../AGENTS.md), [../../backend-go/AGENTS.md](../../backend-go/AGENTS.md), [../../frontend/AGENTS.md](../../frontend/AGENTS.md)
- **Architecture**: [../../ARCHITECTURE.md](../../ARCHITECTURE.md)
- **Environment setup**: [../../ENVIRONMENT.md](../../ENVIRONMENT.md)
- **Development guide**: [../../DEVELOPMENT.md](../../DEVELOPMENT.md)
