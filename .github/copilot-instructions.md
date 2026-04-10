# Copilot Instructions for CC-Bridge

**Project**: CC-Bridge - Multi-provider AI proxy with OpenAI/Claude protocol conversion  
**Tech Stack**: Go 1.22 (backend, Gin) + Vue 3 + Vuetify (frontend, Bun) + SQLite + Docker  
**Last Updated**: April 2026

---

## 🎯 Quick Start

### Essential Commands

```bash
# Root directory
make dev              # Run both backend + frontend with hot-reload
make build            # Production build (Go binary with embedded frontend)

# Backend only (cd backend-go)
make dev              # Air hot-reload
make test             # Run all tests
make test-cover       # Generate coverage.html
make lint             # golangci-lint
make fmt              # go fmt ./...

# Frontend only (cd frontend)
bun install           # Install deps
bun run dev           # Vite dev server (port 5173)
bun run build         # Production build → dist/
bun run type-check    # TypeScript check
bunx prettier --write . # Format code
```

### Critical Paths

| Task | See File | Details |
|------|----------|---------|
| **Backend patterns & architecture** | [AGENTS.md](AGENTS.md) | High-level overview, key concepts |
| **Backend specialist guide** | [backend-go/AGENTS.md](backend-go/AGENTS.md) | Package structure, patterns, anti-patterns |
| **Handler implementation** | [backend-go/internal/handlers/AGENTS.md](backend-go/internal/handlers/AGENTS.md) | Request flow, streaming, error handling |
| **Provider/converter work** | [backend-go/internal/providers/AGENTS.md](backend-go/internal/providers/AGENTS.md) | Protocol conversion interface |
| **Frontend guide** | [frontend/AGENTS.md](frontend/AGENTS.md) | Vue 3 + Vuetify patterns, composables |
| **Architecture deep-dive** | [ARCHITECTURE.md](ARCHITECTURE.md) | System design, tech stack justification |
| **Environment setup** | [ENVIRONMENT.md](ENVIRONMENT.md) | Config, env vars, running modes |
| **Development guide** | [DEVELOPMENT.md](DEVELOPMENT.md) | Setup, workflow, debugging tips |
| **AI guidelines** | [CLAUDE.md](CLAUDE.md) | Guidance for AI-assisted coding |

---

## 📁 Project Structure

```
cc-bridge/
├── backend-go/       # Go service (see backend-go/AGENTS.md)
│   ├── main.go       # Entry point (927 lines - routes, init wiring)
│   ├── internal/     # Business logic
│   │   ├── handlers/      # HTTP endpoints
│   │   ├── providers/     # Upstream API adapters
│   │   ├── converters/    # Protocol translators
│   │   ├── middleware/    # Auth, CORS, rate limiting
│   │   ├── config/        # Configuration + hot-reload
│   │   ├── scheduler/     # Channel selection + circuit breaker
│   │   ├── session/       # Responses API session tracking
│   │   └── ...
│   └── cmd/dbmigrate/     # Migration CLI
├── frontend/         # Vue 3 + Vuetify UI (see frontend/AGENTS.md)
│   ├── src/
│   │   ├── components/    # 19 Vue SFCs
│   │   ├── composables/   # useLocale, useLogStream, useTheme
│   │   ├── services/      # API client
│   │   └── locales/       # i18n (EN, CN)
│   └── dist/              # Embedded in Go binary at build
├── docs/             # Implementation plans & specs
├── .config/          # Runtime config (config.json, backups)
├── .env              # Env vars (NEVER commit, use .env.example)
├── VERSION           # Version source of truth
└── *.md              # Documentation files
```

---

## 🔑 Key Concepts

### Request Flow (Simplified)

```
Client (Claude API format)
    ↓
[Auth Middleware] → Validate API key
    ↓
[Router] → Match endpoint (/v1/messages, /v1/responses, /v1/gemini/*)
    ↓
[Handler] → Parse request, select channel
    ↓
[Scheduler] → Choose upstream (by priority, affinity, circuit breaker)
    ↓
[Converter] → Protocol translation (Claude ↔ OpenAI ↔ Gemini)
    ↓
[Provider] → Forward to upstream, handle streaming
    ↓
[Converter] → Response back to Claude format
    ↓
[Metrics] → Record success/failure, latency, tokens
    ↓
Client (Claude API response)
```

### Three API Pools

- **Messages** (`/v1/messages`): Claude Messages API proxy
- **Responses** (`/v1/responses`): Codex Responses API proxy
- **Gemini** (`/v1/gemini/*`): Google Gemini native passthrough

Each has separate scheduling, channel pools, and configuration.

### Config Hot-Reload

- Changes to `.config/config.json` auto-reload **without restart** via fsnotify
- Changes to `.env` require **restart**
- ConfigManager accessed by: `cfgManager.GetConfig()` at request time

### Channel Scheduler

Selects upstream using:
1. **Channel Priority** — explicit ordering
2. **Affinity** — same user stays on same channel (30min window)
3. **Circuit Breaker** — 50% failure over 10 requests → 15min cooldown
4. **Fallback** — rotate to next available channel

---

## 🎨 Code Style Guidelines

### Go Backend

**Naming:**
- Packages: lowercase, no underscores (`handlers`, `middleware`)
- Functions/interfaces: PascalCase, exported if public
- Variables: camelCase, short names acceptable (i, err, ctx)
- Receivers: `(m *Manager)` not `(this *Manager)`

**Imports:**
- Order: stdlib → third-party → internal, separated by blank lines
- Paths: `github.com/JillVernus/cc-bridge/internal/config`

**Error Handling:**
```go
if err != nil {
    return fmt.Errorf("context: %w", err)  // Always wrap
}
```

**Interfaces & Types:**
- Interfaces: define in `*_interface.go` or main file if small
- Structs: embed dependencies as fields, not globals
- Handler signature: `func (...) gin.HandlerFunc { return func(c *gin.Context) {} }`

**Testing:**
- Co-locate: `main.go` → `main_test.go`
- Table-driven: use `t.Run()` subtests
- Utilities: `t.TempDir()`, `t.Helper()`, `t.Cleanup()`
- HTTP: `gin.SetMode(gin.TestMode)` + `httptest.NewRecorder()`

### Frontend (Vue 3 + TypeScript)

**Composition API Only:**
- Use `<script setup>` (no Options API)
- Props: `defineProps<{ items: string[] }>()`
- Emits: `defineEmits<{ select: [id: string] }>()`

**Naming:**
- Components: PascalCase (`ChannelOrchestration.vue`)
- Composables: camelCase + `use` prefix (`useLocale.ts`)
- Props/emits: camelCase

**Styling:**
- Vuetify 3 components: `v-btn`, `v-card`, `v-data-table`
- Tailwind CSS for custom: `class="flex gap-2 mb-4"`
- CSS Grid/Flexbox for layouts

**TypeScript:**
- Export interfaces from `.ts` files
- No `any` — use `unknown` + type guards or generics
- Type API requests and responses

**Prettier Config** (enforced):
```json
{
  "semi": false,
  "singleQuote": true,
  "trailingComma": "none",
  "printWidth": 120
}
```

---

## ⚠️ Anti-Patterns & Rules

### NEVER

- **Commit `.env` files** — use `.env.example` instead
- **Use `ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS`** — deprecated
- **Use default `PROXY_ACCESS_KEY`** in production
- **Put business logic in `main.go`** — already 927 lines
- **Global state in Go** — pass managers via function parameters
- **Ignore errors** — check every `if err != nil`
- **Return `any` types in Go** — strongly typed only

### AVOID

- CLAUDE.md files in source directories (use top-level docs/)
- Inline styles in Vue — use Tailwind utilities or `<style scoped>`
- Nested tables in Vue — use pagination or expandable rows for large datasets
- HTTP tests without `gin.SetMode(gin.TestMode)`
- Functions longer than 50 lines

### Versioning

- Update `VERSION` file (source of truth)
- Auto-injected into builds via `version.go`
- Commit message style: `feat:`, `fix:`, `refactor:` (conventional commits)

---

## 🔍 File Locations by Task

| Task | File/Directory |
|------|----------------|
| Add upstream provider | `backend-go/internal/providers/newprovider.go` |
| Add protocol converter | `backend-go/internal/converters/factory.go` (register) |
| Add HTTP endpoint | `backend-go/internal/handlers/` + register in `main.go` |
| Modify auth | `backend-go/internal/middleware/auth.go` |
| Change scheduler logic | `backend-go/internal/scheduler/channel_scheduler.go` |
| Config management | `backend-go/internal/config/config.go` |
| Database migrations | `backend-go/cmd/dbmigrate/` |
| Add Vue component | `frontend/src/components/NewComponent.vue` |
| Add composable | `frontend/src/composables/useNewComposable.ts` |
| Request logging | `backend-go/internal/requestlog/` |
| Metrics/health | `backend-go/internal/metrics/` |

---

## 📊 API Endpoints Reference

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/v1/messages` | POST | Claude Messages API proxy |
| `/v1/responses` | POST | Codex Responses API proxy |
| `/v1/gemini/*` | * | Gemini API passthrough |
| `/api/channels` | GET/POST | List, create channels |
| `/api/channels/:id` | PUT/DELETE | Update, delete channel |
| `/api/ping/:id` | GET | Channel health check |
| `/api/logs` | GET | Request log history |
| `/api/keys` | GET/POST/DELETE | API key management |
| `/api/pricing` | GET/PUT | Token pricing config |
| `/api/ratelimit` | GET/PUT | Rate limit config |
| `/health` | GET | Service health |

---

## 🚀 Common Development Workflows

### Adding a New Upstream Provider

1. **Create provider**: `backend-go/internal/providers/newprovider.go`
2. **Implement interface**: `ConvertRequest()`, `ConvertResponse()`, `StreamResponse()`
3. **Register**: Add case to `GetProvider()` factory function (see `provider.go`)
4. **Add serviceType**: Register new enum value in config schema
5. **Create converter**: `backend-go/internal/converters/newprovider_converter.go` if protocol differs
6. **Test**: Table-driven tests in `newprovider_test.go`
7. **Update docs**: Add to providers AGENTS.md service types table

### Adding a New Handler/Endpoint

1. **Create handler**: `backend-go/internal/handlers/newendpoint.go`
   ```go
   func NewEndpointHandler(cfgManager *config.ConfigManager) gin.HandlerFunc {
       return func(c *gin.Context) {
           cfg := cfgManager.GetConfig()
           // Implementation
           c.JSON(200, gin.H{"result": "ok"})
       }
   }
   ```
2. **Register in main.go**: Add route group and handler
3. **Validate input**: Parse request, check required fields
4. **Handle errors**: Return appropriate HTTP status + error JSON
5. **Test**: `newendpoint_test.go` with gin.TestMode
6. **Document**: Update handlers AGENTS.md "WHERE TO LOOK" table

### Modifying Frontend UI

1. **Edit component**: `frontend/src/components/ComponentName.vue`
2. **Use setup syntax**: `<script setup lang="ts">`
3. **Style responsively**: Tailwind + Vuetify grid system
4. **Type props/emits**: Use `defineProps<{}>` and `defineEmits<{}>`
5. **Test**: `bun run type-check` for TypeScript errors
6. **Format**: `bunx prettier --write .` (enforced)
7. **Check i18n**: Update `frontend/src/locales/{en,zh}.json` if adding text

---

## 📚 References for Specialists

### Backend Go
- Detailed patterns: [backend-go/AGENTS.md](backend-go/AGENTS.md)
- Handler specifics: [backend-go/internal/handlers/AGENTS.md](backend-go/internal/handlers/AGENTS.md)
- Provider interface: [backend-go/internal/providers/AGENTS.md](backend-go/internal/providers/AGENTS.md)

### Frontend Vue 3
- Vue patterns: [frontend/AGENTS.md](frontend/AGENTS.md)
- Component structure, composables, styling

### System Design
- Architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Tech justification, system diagram, data flow

### Development Environment
- Setup guide: [DEVELOPMENT.md](DEVELOPMENT.md)
- Config guide: [ENVIRONMENT.md](ENVIRONMENT.md)
- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)

### Implementation Planning
- Plan format: `docs/yyyyMMdd-NN - [name].md`
- All plans in: `docs/` + `docs/implementation_plans/`

---

## ✅ Pre-Commit Checklist

Before committing code:

- [ ] Run `make lint` (backend) or `bun run type-check` (frontend)
- [ ] Run `make test` (backend) for coverage
- [ ] Format code: `make fmt` (backend) or `bunx prettier --write .` (frontend)
- [ ] No `.env` files committed (use `.env.example`)
- [ ] Update `VERSION` file if releasing
- [ ] Update `CHANGELOG.md` with conventional commits
- [ ] Update `frontend/package.json` version if releasing
- [ ] Tests pass: `cd backend-go && make test`

---

## 🔗 Useful Git Patterns

```bash
# Branching
git checkout -b feature/your-feature-name

# View commits
git log --oneline -10

# Interactive rebase for squashing
git rebase -i HEAD~3

# Prune branches
git branch -d feature/old-feature
```

**Never:**
- Force push (`git push -f`) to main branch
- Commit `.env` or `.db` files

---

## 💡 Tips for AI Assistants

1. **Check existing AGENTS.md** before suggesting architectural changes — each module has documented patterns
2. **Follow "config at request time"** pattern — handlers receive `ConfigManager`, never cache config
3. **Test with `gin.TestMode`** — set before creating test HTTP contexts
4. **Use table-driven tests** — far easier to add cases and read intent
5. **Link to existing docs** — don't duplicate code examples; reference the AGENTS files
6. **Verify Prettier formatting** — frontend has strict 120-char line width, no semicolons
7. **Hot-reload workflow** — `make dev` watches both dirs; edit code and browser auto-refreshes
8. **Circuit breaker state** — channels can be temporarily disabled; check metrics when debugging failures

---

## 📞 Need Help?

1. **Specific module questions** → Find the matching `AGENTS.md` file in that subdirectory
2. **Architecture questions** → [ARCHITECTURE.md](ARCHITECTURE.md)
3. **Setup issues** → [ENVIRONMENT.md](ENVIRONMENT.md) and [DEVELOPMENT.md](DEVELOPMENT.md)
4. **Code style questions** → This file's "Code Style Guidelines" section
5. **Implementation planning** → See `docs/` folder for existing plans, follow format

---

Generated for AI agents on April 2026. Last sync with project repository.
