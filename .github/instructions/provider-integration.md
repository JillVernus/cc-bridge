# Provider Integration Guide

**Specialist guide for adding upstream providers to cc-bridge.**

---

## Quick Facts

- **Package**: `backend-go/internal/providers/`
- **Interface**: Implement `Provider` (3 methods: ConvertRequest, ConvertResponse, StreamResponse)
- **Registration**: Add case in `GetProvider()` factory function (`provider.go`)
- **Converters**: Live in `backend-go/internal/converters/` (if protocol differs from Claude)
- **Service types**: Configure in `.config/config.json` channel config
- **Largest existing files**: `openai.go`, `gemini.go`, `responses.go`

---

## The Provider Interface

All providers implement this minimal interface:

```go
// internal/providers/provider.go
type Provider interface {
	// Convert incoming Claude request to upstream format
	ConvertRequest(*types.ClaudeRequest) (interface{}, error)
	
	// Convert upstream response back to Claude format
	ConvertResponse(interface{}) (*types.ClaudeResponse, error)
	
	// Handle streaming (SSE) response from upstream
	StreamResponse(io.Reader, io.Writer) error
}
```

**Responsibilities:**
- **ConvertRequest**: Transform Claude Messages format → upstream format (e.g., OpenAI Chat Completions)
- **ConvertResponse**: Transform upstream JSON → Claude response format
- **StreamResponse**: Parse upstream streaming format → Claude SSE format

---

## Anatomy of a Provider

### Basic Structure

```go
// internal/providers/myprovider.go
package providers

import (
	"fmt"
	"io"
	"github.com/JillVernus/cc-bridge/internal/types"
)

// MyProvider adapts MyUpstream API to Claude format
type MyProvider struct {
	apiKey string
	apiURL string
}

func NewMyProvider(apiKey, apiURL string) *MyProvider {
	return &MyProvider{apiKey, apiURL}
}

// ConvertRequest transforms Claude request to MyUpstream format
func (p *MyProvider) ConvertRequest(req *types.ClaudeRequest) (interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	
	// Transform system prompt, messages, stop sequences, etc.
	upstreamReq := MyUpstreamRequest{
		Model:  req.Model,
		SafetySettings: convertSafety(req.Safety),
		// ... other fields
	}
	
	return upstreamReq, nil
}

// ConvertResponse transforms MyUpstream response to Claude format
func (p *MyProvider) ConvertResponse(rawResp interface{}) (*types.ClaudeResponse, error) {
	resp, ok := rawResp.(MyUpstreamResponse)
	if !ok {
		return nil, fmt.Errorf("invalid response type")
	}
	
	claudeResp := &types.ClaudeResponse{
		Content: []types.ContentBlock{
			{Type: "text", Text: resp.Output},
		},
		StopReason: convertStopReason(resp.StopType),
		Usage: types.Usage{
			InputTokens:  resp.InputTokenCount,
			OutputTokens: resp.OutputTokenCount,
		},
	}
	
	return claudeResp, nil
}

// StreamResponse handles streaming responses from upstream
func (p *MyProvider) StreamResponse(upstream io.Reader, client io.Writer) error {
	scanner := bufio.NewScanner(upstream)
	
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		
		// Parse upstream stream format
		var chunk MyUpstreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		
		// Convert to Claude SSE format
		claudeChunk := &types.StreamEvent{
			Type: "content_block_delta",
			Delta: types.Delta{
				Type: "text_delta",
				Text: chunk.TextDelta,
			},
		}
		
		// Write SSE format: "data: {...}\n\n"
		data, _ := json.Marshal(claudeChunk)
		fmt.Fprintf(client, "data: %s\n\n", string(data))
	}
	
	// Send completion marker
	fmt.Fprintf(client, "data: [DONE]\n\n")
	return scanner.Err()
}
```

---

## Common Patterns

### Message Format Conversion

```go
// claude.go pattern - messages to system prompt + text
func (p *ClaudeProvider) convertMessages(messages []types.Message) string {
	var result strings.Builder
	for _, msg := range messages {
		result.WriteString(fmt.Sprintf("%s: ", msg.Role))
		for _, block := range msg.Content {
			if block.Type == "text" {
				result.WriteString(block.Text)
			}
		}
		result.WriteString("\n\n")
	}
	return result.String()
}

// openai.go pattern - keep structure, map roles
func (p *OpenAIProvider) convertMessages(messages []types.Message) []OpenAIMessage {
	result := make([]OpenAIMessage, len(messages))
	for i, msg := range messages {
		result[i] = OpenAIMessage{
			Role:    msg.Role,  // "user", "assistant", "system"
			Content: extractText(msg),
		}
	}
	return result
}
```

### Stop Sequence Handling

```go
// Normalize stop reasons across providers
func normalizeStopReason(upstreamReason string) string {
	switch upstreamReason {
	case "length":  // OpenAI
		return "max_tokens"
	case "stop":    // Gemini
		return "stop_sequence"
	case "finish_reason":  // Custom
		return "end_turn"
	default:
		return upstreamReason
	}
}
```

### Token Counting

```go
// If upstream doesn't provide token counts, estimate
func estimateTokens(text string) int {
	// Rough heuristic: ~4 chars per token
	return (len(text) + 3) / 4
}
```

### Streaming Format Differences

```go
// OpenAI: `data: {"choices":[{"delta":{"content":"..."}}]}\n\n`
// Claude:  `event: content_block_delta\ndata: {"delta":{"type":"text_delta","text":"..."}}\n\n`
// Gemini:  `[{"candidates":[...]}]` (line-delimited JSON array)

func (p *MyProvider) StreamResponse(upstream io.Reader, client io.Writer) error {
	scanner := bufio.NewScanner(upstream)
	
	for scanner.Scan() {
		if p.isOpenAIFormat() {
			return p.parseOpenAIStream(scanner, client)
		} else if p.isGeminiFormat() {
			return p.parseGeminiStream(scanner, client)
		} else if p.isClaudeFormat() {
			return p.parseClaudeStream(scanner, client)
		}
	}
	
	return scanner.Err()
}
```

---

## Registration

### Step 1: Register in Factory Function

```go
// internal/providers/provider.go
func GetProvider(serviceType string, config ConfigForProvider) Provider {
	switch serviceType {
	case "claude":
		return NewClaudeProvider(config.APIKey, config.APIURL)
	case "openai":
		return NewOpenAIProvider(config.APIKey, config.APIURL)
	case "myprovider":  // ← ADD HERE
		return NewMyProvider(config.APIKey, config.APIURL)
	default:
		return nil
	}
}
```

### Step 2: Add Service Type to Config Schema

```json
{
	"channels": [
		{
			"name": "My Upstream",
			"serviceType": "myprovider",  ← Include in schema validation
			"credentials": {
				"apiKey": "sk-..."
			}
		}
	]
}
```

### Step 3: Verify in Channel Creation

Handler validates `serviceType` matches registered providers:
```go
if handlers.GetProvider(channel.ServiceType, ...) == nil {
	c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported service type"})
	return
}
```

---

## Testing

### Table-Driven Provider Tests

```go
// internal/providers/myprovider_test.go
func TestMyProviderConvertRequest(t *testing.T) {
	tests := []struct {
		name        string
		input       *types.ClaudeRequest
		expect      MyUpstreamRequest
		expectError bool
	}{
		{
			name: "basic message conversion",
			input: &types.ClaudeRequest{
				Model: "claude-3",
				Messages: []types.Message{
					{Role: "user", Content: []types.ContentBlock{
						{Type: "text", Text: "Hello"},
					}},
				},
			},
			expect: MyUpstreamRequest{
				Model: "my-model",
				// ... check translated fields
			},
		},
		{
			name:        "nil request returns error",
			input:       nil,
			expectError: true,
		},
	}
	
	provider := NewMyProvider("key", "https://api.example.com")
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.ConvertRequest(tt.input)
			
			if (err != nil) != tt.expectError {
				t.Errorf("error: got %v, want error=%v", err, tt.expectError)
			}
			
			if !tt.expectError && !reflect.DeepEqual(result, tt.expect) {
				t.Errorf("request mismatch:\ngot:  %+v\nwant: %+v", result, tt.expect)
			}
		})
	}
}
```

### Mock Upstream for Streaming Tests

```go
func TestMyProviderStreamResponse(t *testing.T) {
	provider := NewMyProvider("key", "https://api.example.com")
	
	// Mock upstream stream
	upstreamData := []byte(`chunk1
chunk2
chunk3
`)
	upstream := bytes.NewReader(upstreamData)
	
	// Capture client output
	var buf bytes.Buffer
	err := provider.StreamResponse(upstream, &buf)
	
	if err != nil {
		t.Fatalf("StreamResponse error: %v", err)
	}
	
	// Verify SSE format
	output := buf.String()
	if !strings.Contains(output, "data:") {
		t.Error("missing SSE format")
	}
}
```

---

## Converters (If Needed)

If your provider has a fundamentally different protocol (e.g., Codex Responses API), create a converter:

```go
// internal/converters/myprovider_converter.go
type MyProviderConverter struct{}

func (c *MyProviderConverter) ToClaudeMessage(msg interface{}) (*types.ClaudeResponse, error) {
	// Convert upstream format to Claude format
}

func (c *MyProviderConverter) FromClaudeMessage(req *types.ClaudeRequest) (interface{}, error) {
	// Convert Claude format to upstream format
}
```

Then register in `converters/factory.go`:
```go
func NewConverter(serviceType string) Converter {
	switch serviceType {
	case "myprovider":
		return &MyProviderConverter{}
	// ...
	}
}
```

---

## Pre-Launch Checklist

- [ ] Implement all three Provider interface methods
- [ ] Add test cases for request/response conversion
- [ ] Add streaming test with mock data
- [ ] Register in `GetProvider()` factory function
- [ ] Test integration with handler (call provider.ConvertRequest in test)
- [ ] Verify error handling (nil inputs, malformed upstream)
- [ ] Document service type in config schema
- [ ] Run `make test` with new provider tests passing
- [ ] Add provider type to AGENTS.md service types table

---

## See Also

- **Provider interface details**: [backend-go/internal/providers/AGENTS.md](../../backend-go/internal/providers/AGENTS.md)
- **Handler integration**: [.github/instructions/handler-development.md](handler-development.md)
- **Protocol converters**: [backend-go/internal/converters/](../../backend-go/internal/converters/)
- **Existing providers**: [backend-go/internal/providers/](../../backend-go/internal/providers/) (claude.go, openai.go, gemini.go)

---

*Last updated: April 2026*
