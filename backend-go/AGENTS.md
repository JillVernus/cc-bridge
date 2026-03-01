# BACKEND-GO

Go 1.22 service with Gin. Embeds frontend at build time. Hot-reload config via fsnotify.

## STRUCTURE

```
backend-go/
├── main.go              # Entry point (927 lines - routes, init, wiring)
├── internal/
│   ├── handlers/        # HTTP handlers (see handlers/AGENTS.md)
│   ├── providers/       # Upstream adapters (see providers/AGENTS.md)
│   ├── converters/      # Protocol converters (Responses ↔ Chat)
│   ├── config/          # ConfigManager, hot-reload, env parsing
│   ├── middleware/      # Auth, CORS, rate limiting
│   ├── scheduler/       # Channel selection, circuit breaker
│   ├── session/         # Responses API session tracking
│   ├── metrics/         # Channel health, sliding window
│   ├── requestlog/      # SQLite request logging
│   ├── apikey/          # API key permissions, quotas
│   ├── quota/           # Usage tracking per channel
│   └── pricing/         # Token cost calculation
├── cmd/dbmigrate/       # DB migration CLI tool
└── frontend/dist/       # Embedded frontend (built from ../frontend)
```

## WHERE TO LOOK

| Task | Location | Key File |
|------|----------|----------|
| Add upstream provider | `internal/providers/` | `provider.go` (interface) |
| Add protocol converter | `internal/converters/` | `factory.go` (registry) |
| Add HTTP endpoint | `internal/handlers/` | Register in `main.go` |
| Modify auth logic | `internal/middleware/` | `auth.go` |
| Change scheduling | `internal/scheduler/` | `channel_scheduler.go` |
| Config hot-reload | `internal/config/` | `config.go` |

## PATTERNS

### Handler Pattern
```go
func MyHandler(cfgManager *config.ConfigManager) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Access config: cfgManager.GetConfig()
        c.JSON(200, gin.H{"result": "ok"})
    }
}
```

### Provider Interface
```go
type Provider interface {
    ConvertRequest(*ClaudeRequest) (*UpstreamRequest, error)
    ConvertResponse(*UpstreamResponse) (*ClaudeResponse, error)
    StreamResponse(io.Reader, io.Writer) error
}
```

### Config Hot-Reload
- `ConfigManager.Watch()` uses fsnotify
- Changes to `.config/config.json` auto-reload
- Env changes (`.env`) require restart

## TESTING

- `make test` — Run all tests
- `make test-cover` — Generate `coverage.html`
- Table-driven tests with `t.Run()` subtests
- Use `t.TempDir()` for temp files, `t.Helper()` for helpers
- HTTP tests: `gin.SetMode(gin.TestMode)` + `httptest`

## ANTI-PATTERNS

- **NEVER** put business logic in `main.go` (already 927 lines)
- **NEVER** use `as any` equivalents — Go is strongly typed
- **AVOID** global state — pass managers via function params

## COMMANDS

```bash
make dev          # Hot-reload with Air
make build        # Production binary
make test         # All tests
make test-cover   # Coverage HTML
make fmt          # go fmt ./...
make lint         # golangci-lint
```
