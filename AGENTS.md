# Repository Guidelines

## Project Structure & Module Organization
- `backend-go/`: Go service (Gin), embeds built frontend, commands via `Makefile`; Go packages live under `internal/`.
- `frontend/`: web UI (Vite + Vue 3). Built assets are embedded by the Go build.
- Config and docs: `.env.example` in `backend-go/`, env guidance in `ENVIRONMENT.md`, architecture notes in `ARCHITECTURE.md`, dev flow in `DEVELOPMENT.md`.

## Build, Test, and Development Commands
- Go backend: `cd backend-go && make dev` (hot reload with Air), `make build` (release binary), `make run` (go run), `make clean`.
- Go testing: `cd backend-go && make test` (all packages), `make test-cover` (+ coverage artifacts).
- Frontend: `cd frontend && bun install && bun run dev` (dev server), `bun run build` (production build).
- Full build: `make run` (from root, builds frontend + runs backend).
- Docker: `docker-compose up -d` uses `Dockerfile`/`Dockerfile_China`; keep `.env` aligned first.

## Coding Style & Naming Conventions
- Go: run `go fmt ./...`; prefer idiomatic Go naming (MixedCaps for exports, lowerCamel for locals); keep packages small and purpose-driven.
- JS/TS: follow Prettier defaults (`bunx prettier .` if needed), TypeScript strictness from `tsconfig.json`.
- Files: configs use `.env`/`.json`; avoid committing environment files; keep secrets out of VCS.

## Testing Guidelines
- Default to Go tests; place new tests alongside code under `backend-go/internal/...` with `_test.go` suffix.
- Aim to cover new handlers/middleware and upstream error paths; prefer table-driven tests and `httptest` utilities.
- Use `make test-cover` to confirm regressions and inspect coverage (`coverage.html`).
- For JS/TS additions, mirror existing test locations (none today—add lightweight unit tests if adding logic-heavy modules).

## Commit & Pull Request Guidelines
- Commits follow conventional prefixes seen in history (`feat:`, `fix:`, etc.); keep messages imperative and scoped.
- PRs: describe intent, key changes, and risk; link issues; note breaking changes; include run/test commands executed.
- UI changes: attach before/after screenshots or brief clip; backend behavior changes: include sample requests/responses when relevant.

## Security & Configuration Tips
- Copy `backend-go/.env.example` to `.env` and set a strong `PROXY_ACCESS_KEY`; never commit populated `.env`.
- Review `ENV` vs runtime modes (development vs production) and ensure ports/keys match deployment target.
- Limit access logs and response logs in production when handling sensitive data; rotate keys stored in `.config/` appropriately.
