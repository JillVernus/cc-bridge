# CLAUDE.md

Guidance for Claude Code when working with this repository.

## Project Overview

**CC-Bridge** - Multi-vendor AI proxy server with OpenAI/Claude protocol conversion.

**Tech Stack**: Go 1.22 (backend) + Vue 3 + Vuetify (frontend) + SQLite + Docker

## Architecture

```
┌─────────────┐     ┌──────────────────────────────────────────────────┐
│   Client    │     │                   CC-Bridge                      │
│ (Claude API)│────▶│  ┌─────────┐  ┌───────────┐  ┌───────────────┐  │
└─────────────┘     │  │ Router  │─▶│ Scheduler │─▶│   Providers   │  │
                    │  └─────────┘  └───────────┘  │ ┌───────────┐ │  │
                    │       │            │         │ │  Claude   │ │  │
                    │       ▼            ▼         │ │  OpenAI   │ │  │
                    │  ┌─────────┐  ┌─────────┐   │ │  Gemini   │ │  │
                    │  │  Auth   │  │ Metrics │   │ └───────────┘ │  │
                    │  └─────────┘  └─────────┘   └───────────────┘  │
                    └──────────────────────────────────────────────────┘
```

**Request Flow**:
1. Client sends Claude-format request → Router
2. Auth middleware validates API key
3. Scheduler selects channel (priority, promotion, affinity, circuit breaker)
4. Provider converts protocol (Claude↔OpenAI↔Gemini) and forwards
5. Response converted back to Claude format → Client

## Structure

```
cc-bridge/
├── backend-go/          # Go backend (see backend-go/CLAUDE.md)
├── frontend/            # Vue 3 frontend (see frontend/CLAUDE.md)
├── docs/                # Implementation plans
├── VERSION              # Current version
└── CHANGELOG.md         # Release history
```

## Commands

```bash
# Root directory
make dev              # Dev mode with hot-reload
make build            # Production build

# Backend (cd backend-go)
make dev              # Run with air hot-reload
make test             # Run tests
make build            # Build binary

# Frontend (cd frontend)
bun install           # Install deps
bun run dev           # Dev server (port 5173)
bun run build         # Production build
bun run type-check    # TypeScript check
```

## Key APIs

| Endpoint | Purpose |
|----------|---------|
| `/v1/messages` | Claude Messages API proxy |
| `/v1/responses` | Codex Responses API proxy |
| `/api/channels` | Channel CRUD |
| `/api/ping/:id` | Channel health check |
| `/health` | Service health |

## Important Rules

- **No git ops**: Don't commit/push without explicit request
- **Version updates**: On commit, update `VERSION` + `frontend/package.json` + `CHANGELOG.md`
- **Config hot-reload**: `backend-go/.config/config.json` auto-reloads
- **Minimal changes**: Only modify what's necessary; avoid scope creep
- **Test before commit**: Run `make test` (backend) and `bun run type-check` (frontend)

## Debugging Guide

| Issue | Check |
|-------|-------|
| Config not reloading | Check file watcher logs; manually POST `/admin/config/reload` |
| Auth failing | Verify `x-api-key` header matches `ACCESS_KEY` in `.env` |
| Channel not selected | Check channel status, priority, circuit breaker state |
| Stream disconnects | Check upstream timeout settings and client keep-alive |
| Protocol mismatch | Verify `serviceType` matches upstream API format |

## Error Handling Patterns

**Backend**: Return structured JSON errors with appropriate HTTP status codes
```go
c.JSON(http.StatusBadRequest, gin.H{"error": "message"})
```

**Frontend**: Use try/catch with user-friendly toast notifications
```typescript
try { await api.call() } catch (e) { showError(e.message) }
```

## Implementation Plan Workflow

For non-trivial tasks, create a plan file before coding:

### File Naming Format
```
docs/yyyyMMdd-NN - [plan name].md
```
- `yyyyMMdd`: Date (e.g., 20260105)
- `NN`: Daily sequence, zero-padded (01, 02, 03...)
- Example: `docs/20260105-01 - Add quota management.md`

### Plan Template
```markdown
# [Plan Name]

## Background
[Why this change is needed]

## Approach
[High-level solution]

## Steps
- [ ] Step 1: Description
- [ ] Step 2: Description
- [ ] Step 3: Description

## Commits
[Added after each commit]
- `abc1234` - Step 1 complete
```

### Workflow
1. **Create plan**: Before starting implementation
2. **Update after each step**: Mark `[x]` immediately when step is done
3. **Record commits**: Add commit hash after each commit
4. **Final review**: Verify all steps completed before closing
