# Handler Development Guide

**Specialist guide for building new HTTP endpoints in cc-bridge.**

---

## Quick Facts

- **Package**: `backend-go/internal/handlers/`
- **Interface**: Implement `gin.HandlerFunc`
- **Config access**: Receive `*config.ConfigManager` → call `GetConfig()` at request time
- **Error pattern**: Return structured JSON with appropriate HTTP status
- **Testing**: Use `gin.SetMode(gin.TestMode)` + table-driven tests
- **Largest files**: `proxy.go` (2689L, Messages), `responses.go` (2892L, Responses API), `gemini.go` (1746L)

---

## The Handler Pattern

All handlers follow this signature and structure:

```go
// handlers/myendpoint.go
func MyEndpointHandler(cfgManager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Retrieve current config (fresh at request time!)
		cfg := cfgManager.GetConfig()
		
		// 2. Parse & validate incoming request
		var req MyRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid request format",
			})
			return
		}
		
		// 3. Business logic (delegate to managers/services)
		result, err := doSomething(cfg, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("operation failed: %v", err),
			})
			return
		}
		
		// 4. Return success response
		c.JSON(http.StatusOK, result)
	}
}
```

**Why this pattern?**
- Config is fresh at request time (hot-reload support)
- Clear separation: parse → validate → execute → respond
- Consistent error handling across all endpoints
- Easy to test with dependency injection

---

## Request Parsing & Validation

### Bind Request Body (JSON)

```go
var req struct {
	Model      string   `json:"model" binding:"required"`
	Messages   []Message `json:"messages" binding:"required"`
	Temperature float32 `json:"temperature,omitempty"`
}

if err := c.BindJSON(&req); err != nil {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
	return
}
```

### Extract URL Parameters

```go
id := c.Param("id")  // /api/channels/:id
page := c.Query("page")  // ?page=1
```

### Query Params with Defaults

```go
limit := 10
if l := c.Query("limit"); l != "" {
	limit, _ = strconv.Atoi(l)
}
```

---

## Streaming Responses

For SSE (Server-Sent Events) endpoints like `/v1/messages` and `/v1/responses`:

```go
c.Header("Content-Type", "text/event-stream")
c.Header("Cache-Control", "no-cache")
c.Header("Connection", "keep-alive")

// Stream responses from upstream
for chunk := range upstreamStream {
	// Convert chunk to Claude format
	if convertedChunk, err := converter.Convert(chunk); err == nil {
		fmt.Fprintf(c.Writer, "data: %s\n\n", toJSON(convertedChunk))
		c.Writer.Flush()  // Critical: flush after each chunk
	}
}

// Signal completion
fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
c.Writer.Flush()
```

**Key points:**
- Always set `Content-Type: text/event-stream`
- Flush after each chunk with `c.Writer.Flush()`
- End with `[DONE]` sentinel (OpenAI convention)
- Handle upstream disconnects gracefully

---

## Error Handling

### HTTP Status Codes

| Status | When | Example |
|--------|------|---------|
| `200 OK` | Request succeeded | Channel updated successfully |
| `400 Bad Request` | Invalid input | Missing required field |
| `401 Unauthorized` | Auth failed | Invalid API key |
| `404 Not Found` | Resource missing | Channel ID doesn't exist |
| `429 Too Many Requests` | Rate limited | Quota exceeded |
| `500 Internal Server Error` | Server error | Database connection lost |
| `502 Bad Gateway` | Upstream error | OpenAI API unavailable |
| `503 Service Unavailable` | All channels down | Circuit breaker triggered |

### Error Response Format

```go
c.JSON(statusCode, gin.H{
	"error": "human-readable message",
	"details": "optional debug info",  // Only in non-prod
})
```

### Upstream Request Failures

```go
resp, err := provider.ForwardRequest(ctx, upstreamRequest)
if err != nil {
	if errors.Is(err, context.DeadlineExceeded) {
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "upstream timeout"})
	} else if isNetworkError(err) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
	return
}
```

---

## Testing

### Setup

```go
// handlers/myendpoint_test.go
func TestMyEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)  // Critical!
	
	cfgManager := &config.ConfigManager{
		// Mock config
	}
	
	router := gin.New()
	router.POST("/my/endpoint", MyEndpointHandler(cfgManager))
	
	// Now run test
}
```

### Table-Driven Test Template

```go
tests := []struct {
	name           string
	request        MyRequest
	expectedStatus int
	expectedError  string
	setup          func(*config.ConfigManager)  // Optional setup
}{
	{
		name: "valid request succeeds",
		request: MyRequest{Model: "claude-3-sonnet"},
		expectedStatus: http.StatusOK,
	},
	{
		name: "missing model returns 400",
		request: MyRequest{},
		expectedStatus: http.StatusBadRequest,
		expectedError: "model required",
	},
}

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		// Setup
		cfg := &config.ConfigManager{}
		if tt.setup != nil {
			tt.setup(cfg)
		}
		
		// Act
		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			"POST",
			"/my/endpoint",
			bytes.NewReader(marshalJSON(tt.request)),
		)
		router.ServeHTTP(w, req)
		
		// Assert
		if w.Code != tt.expectedStatus {
			t.Errorf("status: got %d, want %d", w.Code, tt.expectedStatus)
		}
	})
}
```

---

## Registration in main.go

After creating your handler, register it in `main.go`:

```go
// In the setupRoutes() function
api := router.Group("/api")
{
	api.POST("/my-endpoint", handlers.MyEndpointHandler(cfgManager))
	api.GET("/my-endpoint/:id", handlers.GetMyEndpointHandler(cfgManager))
}
```

**Note:** `main.go` is already 927 lines; keep handler logic in `handlers/` package.

---

## Common Tasks

### Accessing Scheduler

```go
scheduler := scheduler.NewChannelScheduler(cfg)
channel, err := scheduler.SelectChannel(upstream, userID)
if err != nil {
	// All channels down or circuit breaker active
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no channels available"})
	return
}
```

### Recording Metrics

```go
metrics := cfg.Metrics
metrics.RecordRequest(
	channel.ID,
	successFlag,
	latencyMs,
	inputTokens,
	outputTokens,
)
```

### Request Logging

From `requestlog/` package:
```go
logger := requestlog.NewLogger(cfg.RequestLogDB)
logger.Log(requestlog.Record{
	Timestamp: time.Now(),
	UserID: userID,
	ChannelID: channel.ID,
	Model: req.Model,
	InputTokens: inputTokens,
	OutputTokens: outputTokens,
})
```

### Channel Affinity

```go
// Same user always routes to same channel for 30min window
affinity := scheduler.GetAffinity(userID)
if affinity != nil && affinity.IsValid() {
	channel = affinity.Channel
} else {
	channel = scheduler.SelectChannel(upstream, userID)
}
```

---

## See Also

- **Request flow**: [backend-go/internal/handlers/AGENTS.md](../../backend-go/internal/handlers/AGENTS.md)
- **Provider interface**: [backend-go/internal/providers/AGENTS.md](../../backend-go/internal/providers/AGENTS.md)
- **Config patterns**: [backend-go/AGENTS.md](../../backend-go/AGENTS.md)
- **Main entry**: [backend-go/main.go](../../backend-go/main.go) - routes, init wiring

---

*Last updated: April 2026*
