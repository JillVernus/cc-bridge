# HANDLERS

HTTP handlers for all API endpoints. Largest module by line count.

## COMPLEXITY HOTSPOTS

| File | Lines | Purpose |
|------|-------|---------|
| `responses.go` | 2892 | Codex Responses API proxy |
| `proxy.go` | 2689 | Claude Messages API proxy |
| `gemini.go` | 1746 | Gemini API passthrough |
| `config.go` | 1444 | Channel CRUD, config API |

## WHERE TO LOOK

| Task | File |
|------|------|
| Messages API (`/v1/messages`) | `proxy.go` |
| Responses API (`/v1/responses`) | `responses.go` |
| Gemini API (`/v1/gemini/*`) | `gemini.go` |
| Channel management (`/api/channels/*`) | `config.go` |
| Request logs (`/api/logs/*`) | `requestlog_handler.go` |
| API key management (`/api/keys/*`) | `apikey_handler.go` |
| Health check | `health.go` |
| Pricing config | `pricing.go` |

## HANDLER SIGNATURE

All handlers follow this pattern:
```go
func HandlerName(deps ...interface{}) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. Parse request
        // 2. Validate
        // 3. Call business logic
        // 4. Return JSON response
    }
}
```

## REQUEST FLOW (proxy.go / responses.go)

1. Parse incoming Claude/Responses request
2. Select channel via `ChannelScheduler.SelectChannel()`
3. Get converter via `converters.NewConverter(serviceType)`
4. Convert request → upstream format
5. Forward to upstream, handle streaming
6. Convert response → Claude format
7. Record metrics (success/failure)
8. Return to client

## STREAMING

- SSE format: `data: {...}\n\n`
- Flush after each chunk: `c.Writer.Flush()`
- Handle `[DONE]` sentinel for OpenAI streams

## ERROR HANDLING

```go
c.JSON(http.StatusBadRequest, gin.H{"error": "message"})
c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
```

## TESTING

- Use `gin.SetMode(gin.TestMode)`
- Create test ConfigManager with `t.TempDir()`
- Use `httptest.NewRecorder()` for response capture
