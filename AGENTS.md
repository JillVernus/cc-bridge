# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-01 | **Commit:** fe7c23e | **Branch:** main

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

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add new upstream provider | `backend-go/internal/providers/` | Implement `Provider` interface |
| Add protocol converter | `backend-go/internal/converters/` | Implement `ResponsesConverter` interface |
| Add API endpoint | `backend-go/internal/handlers/` | Register in main.go routes |
| Add middleware | `backend-go/internal/middleware/` | Chain in main.go |
| Add Vue component | `frontend/src/components/` | Use Vuetify + Composition API |
| Channel scheduling logic | `backend-go/internal/scheduler/` | Priority, circuit breaker, affinity |
| Request logging | `backend-go/internal/requestlog/` | SQLite persistence |
| API key management | `backend-go/internal/apikey/` | Permissions, quotas |

## CONVENTIONS

### Go Backend
- `go fmt ./...` before commit
- Table-driven tests with `t.Run()` subtests
- Tests co-located: `*_test.go` alongside source
- Handlers return `gin.HandlerFunc`
- Config hot-reload via fsnotify (no restart needed)

### Frontend
- Prettier: no semicolons, single quotes, no trailing commas, 120 width
- Vue 3 Composition API + `<script setup>`
- Vuetify 3 components, Tailwind utilities
- Bun package manager (`bun.lock`)

### Versioning
- Update `VERSION` file (root) — auto-injected into builds
- Update `CHANGELOG.md` with conventional commit style

## ANTI-PATTERNS (THIS PROJECT)

- **NEVER** commit `.env` files (use `.env.example`)
- **NEVER** use default `PROXY_ACCESS_KEY` in production
- **DEPRECATED**: `ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS` — keys in URL path
- **AVOID**: CLAUDE.md files in source dirs (AI context, not docs)

## COMMANDS

```bash
# Development
cd backend-go && make dev     # Hot-reload (Air)
cd frontend && bun run dev    # Vite dev server

# Build
make build                    # Full production build
cd backend-go && make build   # Go binary with embedded frontend

# Test
cd backend-go && make test       # All tests
cd backend-go && make test-cover # Coverage report (coverage.html)

# Format
go fmt ./...                  # Go
bunx prettier .               # Frontend
```

## NOTES

- Frontend embedded in Go binary via `//go:embed all:frontend/dist`
- Config changes auto-reload; env changes need restart
- Three API pools: Messages (`/v1/messages`), Responses (`/v1/responses`), Gemini (`/v1/gemini`)
- Circuit breaker: 50% failure rate over 10 requests triggers 15min cooldown
- Trace affinity: same user prefers same channel for 30min
