# PROJECT KNOWLEDGE BASE

**Updated:** 2026-03-28 | **Go:** 1.22 | **Node:** Bun | **Branch:** main

## OVERVIEW

CC-Bridge: Multi-provider AI proxy with OpenAI/Claude protocol conversion. Go 1.22 backend (Gin) + Vue 3 frontend (Vuetify). Single-binary deployment with embedded frontend.

## STRUCTURE

```
cc-bridge/
├── backend-go/           # Go service (see backend-go/AGENTS.md)
│   └── internal/         # handlers/, providers/, converters/, config/, middleware/
├── frontend/             # Vue 3 + Vuetify (see frontend/AGENTS.md)
│   └── src/components/   # 19 Vue SFCs
├── docs/                 # Implementation plans (yyyyMMdd-NN format)
├── .config/              # Runtime: config.json, request_logs.db
└── VERSION               # Single source of truth for version
```

## BUILD/TEST/LINT COMMANDS

### Development
```bash
cd backend-go && make dev              # Hot-reload (Air)
cd frontend && bun run dev             # Vite dev server (port 5173)
make dev                               # Both simultaneously
```

### Build & Package
```bash
make build                             # Production build (Go + embedded frontend)
cd backend-go && make build            # Go binary only
cd frontend && bun run build           # Frontend dist/ only
```

### Testing
```bash
cd backend-go && make test             # Run all tests
cd backend-go && make test-cover       # Coverage HTML report (coverage.html)
cd backend-go && go test -v ./...      # Verbose all tests
cd backend-go && go test -run TestName ./... # Single test by name
cd backend-go && go test -run TestSuite/TestCase ./internal/middleware # Single test in package
cd backend-go && go test ./internal/handlers -v -count=1  # Run tests once (no caching)
```

### Linting & Formatting
```bash
cd backend-go && make lint             # golangci-lint (all checks)
cd backend-go && make fmt              # go fmt ./...
cd frontend && bunx prettier --check . # Check formatting
cd frontend && bunx prettier --write . # Auto-format
cd frontend && bun run type-check      # TypeScript check
```

## CODE STYLE GUIDELINES

### Go Backend

**Naming:**
- Package names: lowercase, no underscores (`handlers`, `middleware`)
- Functions/interfaces: PascalCase, exported if public
- Constants: UPPER_SNAKE_CASE (standard Go, but rare)
- Variables: camelCase, short names acceptable (i, err, ctx)

**Imports:**
- Order: stdlib → third-party → internal
- Group with blank lines between each tier
- Use full paths: `github.com/JillVernus/cc-bridge/internal/config`

**Error Handling:**
- Always check errors: `if err != nil { return err }`
- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Never ignore with `_ = err`

**Types:**
- Interfaces live in `*_interface.go` files (or main file if small)
- Structs embed interfaces: `type Handler struct { manager Manager }`
- Receiver names: `(m *Manager)` not `(this *Manager)`

**Testing:**
- Co-locate tests: `main.go` → `main_test.go`
- Table-driven tests with `t.Run()` subtests
- Use `t.TempDir()` for temp files, `t.Helper()` for helpers
- HTTP tests: `gin.SetMode(gin.TestMode)` + `httptest.NewRecorder()`
- Setup/teardown: use `t.Cleanup()` or defer

**Functions:**
- Handlers return `gin.HandlerFunc`
- Keep functions under 50 lines when possible
- Avoid global state — pass managers via parameters
- Config hot-reload: receive `*ConfigManager`, call `GetConfig()`

### Frontend (Vue 3 + TypeScript)

**Naming:**
- Components: PascalCase (`ChannelOrchestration.vue`)
- Composables: camelCase with `use` prefix (`useLocale.ts`)
- Props/emits: camelCase in templates, same in TypeScript

**Composition API:**
- Always use `<script setup>` (no Options API)
- Define types: `defineProps<{ items: string[] }>()`
- Define emits: `defineEmits<{ select: [id: string] }>()`
- Composables return reactive state + functions

**Styling:**
- Prettier config (no semicolons, single quotes, 120 width)
- Vuetify 3 components for UI (v-btn, v-card, v-data-table)
- Tailwind CSS for custom styles (`class="flex gap-2"`)
- CSS Grid/Flexbox for layouts, avoid inline styles

**TypeScript:**
- Export interfaces from `.ts` files, use in `.vue` files
- Avoid `any` — use `unknown` + type guards or generics
- Type API responses and request payloads

### Shared Conventions

**Database:**
- Migrations: `cmd/dbmigrate/` (SQLite)
- Schema: versioned, reversible

**Config:**
- `.env.example` → `.env` (local only, never commit `.env`)
- `.config/config.json` hot-reloads without restart
- Env changes require restart

**Versioning:**
- Update `VERSION` file (root) — auto-injected into builds
- Update `CHANGELOG.md` with conventional commit style (feat:, fix:, refactor:)

## WHERE TO LOOK

| Task | Location |
|------|----------|
| Add upstream provider | `backend-go/internal/providers/` |
| Add protocol converter | `backend-go/internal/converters/` |
| Add API endpoint | `backend-go/internal/handlers/` |
| Add middleware | `backend-go/internal/middleware/` |
| Add Vue component | `frontend/src/components/` |
| Channel scheduling | `backend-go/internal/scheduler/` |
| Request logging | `backend-go/internal/requestlog/` |

## ANTI-PATTERNS

- **NEVER** commit `.env` files (use `.env.example`)
- **NEVER** use default `PROXY_ACCESS_KEY` in production
- **NEVER** put business logic in `backend-go/main.go` (already 927 lines)
- **DEPRECATED**: `ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS` — keys in URL path
- **AVOID**: CLAUDE.md files in source dirs (AI context, not docs)

## KEY CONCEPTS

- Frontend embedded in Go binary via `//go:embed all:frontend/dist`
- Config changes auto-reload; env changes need restart
- Three API pools: Messages, Responses, Gemini with separate scheduling
- Circuit breaker: 50% failure rate over 10 requests → 15min cooldown
- Trace affinity: same user → same channel for 30min window
