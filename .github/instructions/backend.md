# Backend Development Guide

**Specialist guide for Go backend development in cc-bridge.**

---

## Quick Facts

- **Language**: Go 1.22+
- **Framework**: Gin Web Framework (lightweight, fast routing)
- **Package manager**: Go Modules (`go.mod`)
- **DB**: SQLite (request logs), PostgreSQL (optional)
- **Testing**: Built-in `testing` package + table-driven tests
- **Hot-reload**: Air (watches `.go` files, rebuilds on save)
- **Startup**: ~100ms from cold start
- **Build**: Single binary with embedded frontend (Vue SFCs)

---

## Project Structure

```
backend-go/
├── main.go                # Entry point (927 lines - routes, init wiring)
├── version.go             # Auto-injected version at build time
├── go.mod / go.sum        # Dependencies
├── internal/
│   ├── handlers/          # HTTP endpoints (largest module by lines)
│   │   ├── proxy.go       # Messages API (2689L)
│   │   ├── responses.go   # Responses API (2892L)
│   │   ├── gemini.go      # Gemini API (1746L)
│   │   ├── config.go      # Channel CRUD (1444L)
│   │   ├── *_test.go      # Co-located tests
│   │   └── AGENTS.md      # Handler patterns
│   ├── providers/         # Upstream service adapters
│   │   ├── provider.go    # Interface definition
│   │   ├── claude.go      # Claude API provider
│   │   ├── openai.go      # OpenAI provider
│   │   ├── gemini.go      # Gemini provider
│   │   ├── responses.go   # Responses provider
│   │   ├── *_test.go      # Provider tests
│   │   └── AGENTS.md      # Provider interface guide
│   ├── converters/        # Protocol translators
│   │   ├── factory.go     # Provider registry
│   │   ├── claude_to_*.go # Translation implementations
│   │   └── *_test.go      # Converter tests
│   ├── middleware/        # HTTP middleware
│   │   ├── auth.go        # API key validation
│   │   ├── cors.go        # CORS headers
│   │   ├── ratelimit.go   # Rate limiting
│   │   └── *_test.go      # Middleware tests
│   ├── config/            # Configuration management
│   │   ├── config.go      # ConfigManager + hot-reload
│   │   ├── types.go       # Config structs
│   │   ├── validation.go  # Schema validation
│   │   └── *_test.go      # Config tests
│   ├── scheduler/         # Channel scheduling
│   │   ├── channel_scheduler.go # Main scheduler
│   │   ├── circuit_breaker.go   # Failure isolation
│   │   ├── affinity.go          # User affinity (30min window)
│   │   └── *_test.go            # Scheduler tests
│   ├── session/           # Responses API session tracking
│   ├── metrics/           # Channel health + sliding window stats
│   ├── requestlog/        # SQLite request logging
│   ├── apikey/            # API key CRUD + permissions
│   ├── quota/             # Usage tracking per channel
│   ├── pricing/           # Token cost calculation
│   ├── database/          # DB initialization
│   ├── types/             # Shared Go types
│   ├── utils/             # Helper functions
│   └── logger/            # Structured logging
├── cmd/
│   └── dbmigrate/         # Database migration CLI tool
├── .config/
│   ├── config.json        # Runtime config (hot-reloads)
│   ├── config.example.json # Config template
│   └── backups/           # Config backup history
├── Makefile               # Build tasks
├── build.sh               # Build script (embeds frontend)
├── Dockerfile             # Container image
├── README.md              # Backend documentation
├── DEV_GUIDE.md           # Development workflow
└── AGENTS.md              # Backend patterns overview
```

---

## Development Workflow

### Fast Local Development

```bash
cd backend-go

# Terminal 1: Watch Go files, hot-reload on change
make dev

# Terminal 2: In other terminal, test as you go
make test

# Terminal 3: View logs in real-time
tail -f logs/app.log
```

**What happens when you save:**
1. Air detects change to `.go` file
2. Rebuilds binary (~500ms)
3. Restarts server (~100ms start time)
4. Browser auto-refreshes via Vite HMR

### Building & Deployment

```bash
# Development build (includes debug symbols, no optimization)
make build-current

# Production build (optimized, stripped)
make build

# Run built binary
./cc-bridge --config .config/config.json
```

---

## Handler Development Pattern

See [handler-development.md](handler-development.md) for full guide.

**Quick reference:**

```go
func MyHandler(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Fetch fresh config at request time (supports hot-reload)
		cfg := cfgManager.GetConfig()
		
		// Parse + validate request
		var req MyRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid"})
			return
		}
		
		// Business logic
		result, err := processRequest(cfg, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		// Return response
		c.JSON(http.StatusOK, result)
	}
}

// Register in main.go
func setupRoutes(router *gin.Engine, cfgManager *config.ConfigManager) {
	router.POST("/api/my-endpoint", handlers.MyHandler(cfgManager))
}
```

### Key Patterns

| Task | Pattern |
|------|---------|
| Get fresh config | `cfg := cfgManager.GetConfig()` (always at request time) |
| Select channel | `scheduler.SelectChannel(upstream, userID)` |
| Forward to upstream | `provider.ConvertRequest()` → HTTP call → `provider.ConvertResponse()` |
| Stream response | Set `Content-Type: text/event-stream`, flush after each chunk |
| Record error | Return structured JSON with HTTP status code |
| Log request | `requestlog.NewLogger(db).Log(record)` |

---

## Provider Development Pattern

See [provider-integration.md](provider-integration.md) for full guide.

**Quick reference:**

```go
type MyProvider struct {
	apiKey string
	apiURL string
}

// Convert Claude request to upstream format
func (p *MyProvider) ConvertRequest(req *types.ClaudeRequest) (interface{}, error) {
	return MyUpstreamReq{
		Model: req.Model,
		Messages: convertMessages(req.Messages),
	}, nil
}

// Convert upstream response to Claude format
func (p *MyProvider) ConvertResponse(raw interface{}) (*types.ClaudeResponse, error) {
	resp := raw.(MyUpstreamResponse)
	return &types.ClaudeResponse{
		Content: []types.ContentBlock{{Type: "text", Text: resp.Output}},
		Usage: types.Usage{
			InputTokens:  resp.InputCount,
			OutputTokens: resp.OutputCount,
		},
	}, nil
}

// Handle streaming responses
func (p *MyProvider) StreamResponse(upstream io.Reader, client io.Writer) error {
	scanner := bufio.NewScanner(upstream)
	for scanner.Scan() {
		chunk := parseChunk(scanner.Bytes())
		fmt.Fprintf(client, "data: %s\n\n", toJSON(chunk))
	}
	fmt.Fprintf(client, "data: [DONE]\n\n")
	return nil
}
```

**Register in `providers/provider.go`:**

```go
func GetProvider(serviceType string, cfg ProviderConfig) Provider {
	switch serviceType {
	case "myprovider":
		return NewMyProvider(cfg.APIKey, cfg.APIURL)
	}
	return nil
}
```

---

## Testing Strategy

### Table-Driven Tests (Standard Pattern)

```go
// handlers/myhandler_test.go
func TestMyHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)  // Critical!
	
	tests := []struct {
		name           string
		request        MyRequest
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "valid request",
			request:        MyRequest{Model: "claude-3"},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid model",
			request:        MyRequest{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "model required",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cfgManager := &config.ConfigManager{}
			router := gin.New()
			router.POST("/test", MyHandler(cfgManager))
			
			// Act
			w := httptest.NewRecorder()
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
			router.ServeHTTP(w, req)
			
			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}
```

### Running Tests

```bash
# Run all tests
make test

# Run single package
go test ./internal/handlers -v

# Run single test
go test -run TestMyHandler ./internal/handlers -v

# Run with coverage report
make test-cover  # Generates coverage.html
```

### Testing Best Practices

1. **Co-locate**: `main.go` → `main_test.go` (same directory)
2. **Table-driven**: Use `t.Run()` for subtests
3. **Utilities**: `t.TempDir()`, `t.Helper()`, `t.Cleanup()`
4. **HTTP tests**: Always set `gin.SetMode(gin.TestMode)` first
5. **No globals**: Pass managers via function parameters
6. **Error wrapping**: `fmt.Errorf("context: %w", err)`

---

## Configuration & Hot-Reload

### Config Management

Config lives in `.config/config.json` and auto-reloads via fsnotify.

```go
// Access config fresh at request time
cfg := cfgManager.GetConfig()

// Config structure (read from JSON)
type Config struct {
	Channels []Channel
	Pricing  PricingConfig
	RateLimit RateLimitConfig
	// ...
}
```

### Environment Variables

```bash
# .env (never commit, local only)
PROXY_ACCESS_KEY=sk-test-... # Primary auth key
DB_PATH=.config/request_logs.db
LOG_LEVEL=debug
PORT=8080
```

**Key variables:**
- `PROXY_ACCESS_KEY` — API key for client requests
- `DB_PATH` — SQLite database location
- `LOG_LEVEL` — debug, info, warn, error
- `PORT` — HTTP server port (default 8080)

### Updating Config at Runtime

**Client updates via API:**
```bash
curl -X PUT http://localhost:8080/api/config \
  -H "x-api-key: $PROXY_ACCESS_KEY" \
  -H "Content-Type: application/json" \
  -d @new-config.json
```

**ConfigManager watches file and reloads automatically** — no restart needed!

---

## Key Subsystems

### Channel Scheduler

Selects which upstream to use:

```go
scheduler := scheduler.NewChannelScheduler(cfg)

// Selects by priority, affinity, circuit breaker, round-robin
channel, err := scheduler.SelectChannel("messages", userID)
if err != nil {
	// All channels down or circuit breaker active
	return fmt.Errorf("no channels available")
}

// Send request to upstream
response, err := upstream.Call(channel.Credentials, request)
```

**Selection algorithm:**
1. Check affinity (same user → same channel for 30min)
2. Filter by channel priority
3. Skip channels in circuit breaker
4. Round-robin among available channels

### Metrics & Health

Track per-channel stats in sliding window:

```go
metrics := cfg.Metrics

metrics.RecordRequest(channelID, success, latencyMs, inputTokens, outputTokens)

// Get stats for dashboard
stats := metrics.GetChannelStats(channelID)
// Returns: success rate, avg latency, total requests, errors
```

### Request Logging

SQLite database for audit trail:

```go
logger := requestlog.NewLogger(cfg.RequestLogDB)

logger.Log(requestlog.Record{
	Timestamp:    time.Now(),
	UserID:       userID,
	ChannelID:    channel.ID,
	Model:        "claude-3-sonnet",
	InputTokens: 500,
	OutputTokens: 1200,
	Cost:         0.025,
	Duration:     2500 * time.Millisecond,
})
```

---

## Code Style & Conventions

### Naming

```go
// Packages: lowercase, no underscores
package handlers
package middleware

// Functions/types: PascalCase, exported if public
func NewChannelScheduler()
type Provider interface {}

// Variables: camelCase, short names acceptable
var err error
for i := 0; i < len(items); i++ {}

// Receivers: (m *Manager) not (this *Manager)
func (m *Manager) GetConfig() *Config {}
```

### Imports

```go
import (
	"encoding/json"  // stdlib
	"fmt"
	"io"

	"github.com/gin-gonic/gin"  // third-party
	"github.com/mattn/go-sqlite3"

	"github.com/JillVernus/cc-bridge/internal/config"  // internal
	"github.com/JillVernus/cc-bridge/internal/types"
)
```

### Error Handling

Always wrap errors with context:

```go
// ✅ Good
if err != nil {
	return fmt.Errorf("failed to load config: %w", err)
}

// ❌ Bad
if err != nil {
	return err  // Lost context
}

if err != nil {
	panic(err)  // Never in production
}
```

### Functions

Keep functions under 50 lines when possible:

```go
// ✅ Good: Focused, single responsibility
func (h *Handler) parseRequest(c *gin.Context) (*Request, error) {
	var req Request
	if err := c.BindJSON(&req); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return &req, nil
}

// ❌ Bad: Does too much (parse → validate → authorize → execute)
func (h *Handler) handleRequest(c *gin.Context) {
	// ... 200 lines of logic ...
}
```

---

## Linting & Formatting

### Code Quality Checks

```bash
# Run golangci-lint (ESLint equivalent for Go)
make lint

# Auto-format code
make fmt

# Check for unused imports, variables
go vet ./...
```

**golangci-lint rules** check for:
- Unused variables/imports
- Unhandled errors
- Nil pointers
- Race conditions
- Code complexity

### Pre-Commit Checklist

- [ ] `make lint` passes (no warnings)
- [ ] `make fmt` applied (consistent formatting)
- [ ] `make test` passes all tests
- [ ] No uncommitted changes in vendor/
- [ ] VERSION file updated if releasing

---

## Performance Optimization

### Profiling

```bash
# CPU profiling during test
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof ./...
go tool pprof mem.prof

# Benchmark a function
go test -bench=BenchmarkMyFunction -benchmem ./...
```

### Common Bottlenecks

1. **Config access**: Cache config reference locally when possible (but refresh per request)
2. **JSON marshaling**: Use `json.Encoder` for streaming instead of `json.Marshal` for large payloads
3. **Database**: Use transaction batches for bulk inserts
4. **HTTP clients**: Reuse HTTP client, don't create per-request

---

## Debugging

### Using Delve (Interactive Debugger)

```bash
# Set breakpoint and run debugger
dlv debug ./cmd/main.go
(dlv) break main.main
(dlv) continue
(dlv) next
(dlv) print variableName
(dlv) quit
```

### Logging Best Practices

```go
import "log/slog"

// Structured logging (better than fmt.Println)
slog.Info("channel selected",
	"channel_id", channel.ID,
	"user_id", userID,
	"latency_ms", latency,
)

slog.Error("upstream request failed",
	"channel_id", channel.ID,
	"error", err.Error(),
	"retry_count", retries,
)
```

### Adding debug logs temporarily

```bash
# Set log level to debug
export LOG_LEVEL=debug

make dev  # Logs will be more verbose
```

---

## Deployment

### Building Production Binary

```bash
# Single binary with embedded frontend
make build

# Output: ./cc-bridge (ready to deploy)

# Test it
./cc-bridge --version
./cc-bridge --config .config/config.json
```

### Docker

```bash
docker build -t cc-bridge:1.2.3 .
docker run -p 8080:8080 -v $(pwd)/.config:/app/.config cc-bridge:1.2.3
```

### Environment Variables for Production

```bash
PROXY_ACCESS_KEY=sk-prod-xxx
DB_PATH=/persistent/request_logs.db
LOG_LEVEL=warn  # Less verbose in prod
PORT=8080
```

---

## See Also

- **Handler guide**: [handler-development.md](handler-development.md)
- **Provider guide**: [provider-integration.md](provider-integration.md)
- **Config details**: [backend-go/internal/config/AGENTS.md](../../backend-go/internal/config/AGENTS.md)
- **Main entry**: [backend-go/main.go](../../backend-go/main.go)
- **Backend overview**: [backend-go/AGENTS.md](../../backend-go/AGENTS.md)

---

*Last updated: April 2026*
