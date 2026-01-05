# CLAUDE.md

Guidance for Claude Code when working with this repository.

## Project Overview

**CC-Bridge** - Multi-vendor AI proxy server with OpenAI/Claude protocol conversion.

**Tech Stack**: Go 1.22 (backend) + Vue 3 + Vuetify (frontend) + SQLite + Docker

## Structure

```
cc-bridge/
├── backend-go/          # Go backend (see backend-go/CLAUDE.md)
└── frontend/            # Vue 3 frontend (see frontend/CLAUDE.md)
```

## Common Commands

```bash
# Root directory
make dev              # Dev mode with hot-reload
make build            # Production build

# Backend (cd backend-go)
make dev / make test / make build

# Frontend (cd frontend)
bun install / bun run dev / bun run build / bun run type-check
```

## Key APIs

- `/v1/messages` - Claude Messages API
- `/v1/responses` - Codex Responses API

## Important Rules

- **No git ops**: Don't commit/push without explicit request
- **Version updates**: On commit, update `VERSION` + `frontend/package.json` + `CHANGELOG.md`
- **Config hot-reload**: `backend-go/.config/config.json` auto-reloads

## Implementation Plan

For non-trivial tasks:

1. **Create plan**: `docs/yyyyMMdd - [plan name].md` with implementation steps
2. **Update status**: Mark each step as done after implementation
3. **Track commits**: After commit, add commit hash/info to the plan for traceability
