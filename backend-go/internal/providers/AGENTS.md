# PROVIDERS

Upstream service adapters. Protocol conversion between Claude format and upstream APIs.

## FILES

| File | Purpose |
|------|---------|
| `provider.go` | `Provider` interface definition |
| `claude.go` | Claude API (passthrough) |
| `openai.go` | OpenAI Chat Completions |
| `openai_chat.go` | OpenAI Chat format handling |
| `openai_chat_stream.go` | OpenAI streaming response |
| `openai_chat_parser.go` | Parse OpenAI responses |
| `openai_chat_prompt.go` | System prompt injection |
| `gemini.go` | Google Gemini API |
| `gemini_passthrough.go` | Gemini native passthrough |
| `responses.go` | Codex Responses upstream |
| `responses_upstream.go` | Responses API client |

## PROVIDER INTERFACE

```go
type Provider interface {
    // Convert Claude request to upstream format
    ConvertRequest(*ClaudeRequest) (*UpstreamRequest, error)
    
    // Convert upstream response to Claude format
    ConvertResponse(*UpstreamResponse) (*ClaudeResponse, error)
    
    // Handle streaming responses
    StreamResponse(io.Reader, io.Writer) error
}
```

## ADDING A NEW PROVIDER

1. Create `newprovider.go` in this directory
2. Implement `Provider` interface
3. Register in `GetProvider()` factory function
4. Add `serviceType` value to config schema

## SERVICE TYPES

| serviceType | Provider | Upstream Format |
|-------------|----------|-----------------|
| `claude` | ClaudeProvider | Claude Messages API |
| `openai` | OpenAIProvider | OpenAI Chat Completions |
| `openaiold` | OpenAIOldProvider | OpenAI Legacy Completions |
| `gemini` | GeminiProvider | Google Gemini API |
| `responses` | ResponsesProvider | Codex Responses API |

## STREAMING CONVENTIONS

- OpenAI: `data: {...}\n\n` with `[DONE]` sentinel
- Claude: `event: ...\ndata: {...}\n\n`
- Gemini: JSON array chunks

## TESTING

- `openai_chat_parser_test.go` — Response parsing
- `openai_chat_prompt_test.go` — Prompt formatting
- Use table-driven tests with `t.Run()`
