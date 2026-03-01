# OpenAI Chat Completions Incoming Endpoint

## Background

Currently, CC-Bridge accepts requests in three incoming formats:

| Tab | Incoming Endpoint | Incoming Format |
|-----|-------------------|-----------------|
| Claude | `/v1/messages` | Claude Messages API |
| Codex | `/v1/responses` | Codex Responses API |
| Gemini | `/v1/gemini/models/*` | Gemini Native API |

Each incoming endpoint has its own dedicated channel pool and can route to various upstream providers (Claude, OpenAI, Gemini) with protocol translation.

**This plan adds:**
1. New incoming endpoint `/v1/chat/completions` that accepts **OpenAI Chat Completions format** requests
2. New **Chat tab** in frontend with dedicated channel list (`ChatUpstream`)
3. Full protocol conversion to Claude/OpenAI/Gemini upstreams
4. Full tool calling support including `tool_choice` parameter
5. Reasoning/thinking mapping (OpenAI `reasoning_effort` → Claude thinking)

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              CC-Bridge Frontend                                      │
├─────────────────┬─────────────────┬─────────────────┬───────────────────────────────┤
│   Claude Tab    │   Codex Tab     │   Gemini Tab    │   Chat Tab (NEW)              │
│   /v1/messages  │   /v1/responses │   /v1/gemini/*  │   /v1/chat/completions        │
│                 │                 │                 │                               │
│ Channels:       │ Channels:       │ Channels:       │ Channels:                     │
│ - claude→*      │ - responses→*   │ - gemini→*      │ - chat→claude (NEW)          │
│                 │                 │                 │ - chat→openai (passthrough)  │
│                 │                 │                 │ - chat→gemini (NEW)          │
└─────────────────┴─────────────────┴─────────────────┴───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              CC-Bridge Backend                                       │
├─────────────────┬─────────────────┬─────────────────┬───────────────────────────────┤
│ config.Upstream │ config.Responses│ config.Gemini   │ config.ChatUpstream (NEW)     │
│                 │ Upstream        │ Upstream        │                               │
└─────────────────┴─────────────────┴─────────────────┴───────────────────────────────┘
```

**Request Flow:**
```
Client (OpenAI Chat format)
    ↓
POST /v1/chat/completions
    ↓
Auth middleware (existing)
    ↓
ChatCompletionsHandler (uses ChatUpstream channel list)
    ↓
Channel Scheduler (select from ChatUpstream)
    ↓
┌─────────────────────────────────────────────────┐
│ Provider Selection (based on serviceType)       │
├─────────────────────────────────────────────────┤
│ claude    → ChatToClaudeConverter → Claude API  │
│ openai    → Passthrough           → OpenAI API  │
│ gemini    → ChatToGeminiConverter → Gemini API  │
└─────────────────────────────────────────────────┘
    ↓
Response Converter (upstream format → OpenAI Chat format)
    ↓
Client (OpenAI Chat format response)
```

## Specifications

### Supported OpenAI Chat Completions Request Fields

| Field | Support | Notes |
|-------|---------|-------|
| `model` | ✅ | Required, mapped via channel's `modelMapping` |
| `messages` | ✅ | Full support including system, user, assistant, tool roles |
| `temperature` | ✅ | Passed through |
| `top_p` | ✅ | Passed through |
| `max_tokens` / `max_completion_tokens` | ✅ | Mapped to upstream equivalent |
| `stream` | ✅ | Full streaming support |
| `stream_options.include_usage` | ✅ | Return usage in final chunk |
| `tools` | ✅ | Full tool/function calling support |
| `tool_choice` | ✅ | `none`, `auto`, `required`, or specific function |
| `parallel_tool_calls` | ✅ | Passed through where supported |
| `frequency_penalty` | ✅ | Passed through |
| `presence_penalty` | ✅ | Passed through |
| `n` | ⚠️ | Only `n=1` supported initially |
| `logprobs` | ❌ | Not supported (Claude doesn't support) |
| `modalities` | ❌ | Text only (no audio) |

### Reasoning/Thinking Mapping

| OpenAI `reasoning_effort` | Claude `thinking.type` | Claude `thinking.budget_tokens` |
|---------------------------|------------------------|--------------------------------|
| `none` | `disabled` | - |
| `low` | `enabled` | 5000 |
| `medium` | `enabled` | 10000 |
| `high` | `enabled` | 20000 |

### Response Format

Non-streaming response:
```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "gpt-4o",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Response text",
      "tool_calls": [...]
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 100,
    "completion_tokens": 50,
    "total_tokens": 150
  }
}
```

Streaming SSE format:
```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
```

## Implementation Steps

### Phase 1: Backend - Config & Infrastructure

#### Step 1.1: Add ChatUpstream to Config
**File:** `backend-go/internal/config/config.go`

- [ ] Add `ChatUpstream []UpstreamConfig` field to `Config` struct
- [ ] Add `ChatLoadBalance string` field to `Config` struct
- [ ] Add default values in config initialization
- [ ] Add index/ID migration for ChatUpstream on startup
- [ ] Add `validateChannelKeys` check for Chat channels
- [ ] Add `warnInsecureChannels` check for Chat channels

#### Step 1.2: Add ConfigManager Methods
**File:** `backend-go/internal/config/config.go`

- [ ] `GetChatUpstreams()` - List all Chat channels
- [ ] `AddChatUpstream()` - Add new Chat channel
- [ ] `UpdateChatUpstream()` - Update existing channel
- [ ] `RemoveChatUpstream()` - Delete channel
- [ ] `AddChatAPIKey()` - Add API key to channel
- [ ] `RemoveChatAPIKeyByIndex()` - Remove API key by index
- [ ] `ReorderChatChannels()` - Reorder channels
- [ ] `SetChatChannelStatus()` - Set channel status
- [ ] `GetNextChatAPIKey()` - Key rotation with ChatLoadBalance
- [ ] `SetChatLoadBalance()` - Set load balance strategy

#### Step 1.3: Add Scheduler Support for Chat Channels
**File:** `backend-go/internal/scheduler/channel_scheduler.go`

- [ ] Add `chatMetricsManager *metrics.MetricsManager` field
- [ ] Add `chatRoundRobinCounter int` field
- [ ] `IsChatMultiChannelMode()` - Check if multi-channel mode
- [ ] `GetActiveChatChannelCount()` - Get active channel count
- [ ] `SelectChatChannel()` - Select channel with circuit breaker and trace affinity
- [ ] `RecordChatSuccess()` - Record successful request
- [ ] `RecordChatFailure()` - Record failed request
- [ ] `ResetChatChannelMetrics()` - Reset channel metrics
- [ ] `GetChatMetricsManager()` - Get metrics manager

### Phase 2: Backend - Converters

#### Step 2.1: Create OpenAI Chat Request Types
**File:** `backend-go/internal/types/chat_types.go`

- [ ] `ChatCompletionsRequest` - Full request structure
- [ ] `ChatCompletionsMessage` - Message with all fields
- [ ] `ChatCompletionsTool` - Tool definition
- [ ] `ChatCompletionsToolCall` - Tool call in response
- [ ] `ChatCompletionsResponse` - Non-streaming response
- [ ] `ChatCompletionsChunk` - Streaming chunk

#### Step 2.2: Create Chat → Claude Converter
**File:** `backend-go/internal/converters/chat_to_claude.go`

- [ ] `ConvertChatToClaudeRequest()` - Main conversion function
- [ ] Extract system message from messages array → Claude `system` field
- [ ] Convert `messages[]` → Claude `messages[]`
  - [ ] Handle `role: user` → `role: user`
  - [ ] Handle `role: assistant` with `tool_calls` → `role: assistant` with `tool_use` content blocks
  - [ ] Handle `role: tool` → `role: user` with `tool_result` content block
- [ ] Convert `tools[]` → Claude `tools[]`
  - [ ] Map `function.parameters` → `input_schema`
- [ ] Convert `tool_choice` → Claude format
  - [ ] `none` → `{"type": "none"}`
  - [ ] `auto` → `{"type": "auto"}`
  - [ ] `required` → `{"type": "any"}`
  - [ ] `{"function": {"name": "x"}}` → `{"type": "tool", "name": "x"}`
- [ ] Convert `reasoning_effort` → Claude `thinking`
- [ ] Map `max_tokens` / `max_completion_tokens`

#### Step 2.3: Create Claude → Chat Response Converter
**File:** `backend-go/internal/converters/claude_to_chat.go`

- [ ] `ConvertClaudeToChatResponse()` - Non-streaming conversion
- [ ] `ConvertClaudeToChatStream()` - Streaming SSE conversion
- [ ] Map Claude `content[]` → OpenAI `choices[].message`
  - [ ] Text blocks → `content` string
  - [ ] `tool_use` blocks → `tool_calls[]`
- [ ] Map `stop_reason` → `finish_reason`
  - [ ] `end_turn` → `stop`
  - [ ] `max_tokens` → `length`
  - [ ] `tool_use` → `tool_calls`
  - [ ] `stop_sequence` → `stop`
- [ ] Map `usage` fields
  - [ ] `input_tokens` → `prompt_tokens`
  - [ ] `output_tokens` → `completion_tokens`

#### Step 2.4: Create Chat → Gemini Converter
**File:** `backend-go/internal/converters/chat_to_gemini.go`

- [ ] `ConvertChatToGeminiRequest()` - Main conversion function
- [ ] Convert `messages[]` → Gemini `contents[]`
  - [ ] `role: user` → `role: user`
  - [ ] `role: assistant` → `role: model`
  - [ ] `role: system` → `systemInstruction`
- [ ] Convert `tools[]` → Gemini `tools[].functionDeclarations[]`
- [ ] Convert `tool_choice` → Gemini `toolConfig`

#### Step 2.5: Create Gemini → Chat Response Converter
**File:** `backend-go/internal/converters/gemini_to_chat.go`

- [ ] `ConvertGeminiToChatResponse()` - Non-streaming conversion
- [ ] `ConvertGeminiToChatStream()` - Streaming conversion
- [ ] Map Gemini response → OpenAI Chat format

### Phase 3: Backend - Handler

#### Step 3.1: Create Chat Completions Handler
**File:** `backend-go/internal/handlers/chat_completions.go`

- [ ] `ChatCompletionsHandler()` - Base handler
- [ ] `ChatCompletionsHandlerWithAPIKey()` - Handler with API key support
- [ ] Authentication check (reuse existing middleware)
- [ ] Endpoint permission check (`chat` endpoint)
- [ ] Read and parse request body
- [ ] Model permission check
- [ ] Create pending request log entry
- [ ] Single-channel vs multi-channel routing
- [ ] `handleChatSingleChannel()` - Single channel logic
- [ ] `handleChatMultiChannel()` - Multi-channel with failover
- [ ] Streaming response handling
- [ ] Non-streaming response handling
- [ ] Update request log on completion
- [ ] Error handling and response formatting

#### Step 3.2: Create Chat Channel CRUD APIs
**File:** `backend-go/internal/handlers/chat_config.go`

- [ ] `GET /api/chat/channels` - List Chat channels
- [ ] `POST /api/chat/channels` - Add Chat channel
- [ ] `PUT /api/chat/channels/:id` - Update Chat channel
- [ ] `DELETE /api/chat/channels/:id` - Delete Chat channel
- [ ] `POST /api/chat/channels/:id/keys` - Add API key
- [ ] `DELETE /api/chat/channels/:id/keys/index/:keyIndex` - Remove API key
- [ ] `POST /api/chat/channels/reorder` - Reorder channels
- [ ] `PATCH /api/chat/channels/:id/status` - Set channel status
- [ ] `GET /api/chat/channels/metrics` - Get channel metrics
- [ ] `PUT /api/chat/loadbalance` - Update load balance strategy

#### Step 3.3: Register Routes
**File:** `backend-go/main.go`

```go
// Chat Completions incoming endpoint
v1Group.POST("/chat/completions", handlers.ChatCompletionsHandlerWithAPIKey(
    envCfg, cfgManager, channelScheduler, reqLogManager, 
    apiKeyManager, usageQuotaManager, failoverTracker, channelRateLimiter))

// Chat channel management APIs
chatAPIGroup := apiGroup.Group("/chat")
{
    chatAPIGroup.GET("/channels", handlers.ListChatChannels(cfgManager, channelScheduler))
    chatAPIGroup.POST("/channels", handlers.AddChatChannel(cfgManager))
    chatAPIGroup.PUT("/channels/:id", handlers.UpdateChatChannel(cfgManager))
    chatAPIGroup.DELETE("/channels/:id", handlers.DeleteChatChannel(cfgManager))
    // ... etc
}
```

### Phase 4: Backend - API Key Permissions

#### Step 4.1: Add Chat Endpoint Permission
**File:** `backend-go/internal/apikey/apikey.go`

- [ ] Add `chat` to `AllowedEndpoints` validation
- [ ] Update `CheckEndpointPermission()` to handle `chat`

### Phase 5: Frontend - UI

#### Step 5.1: Add OpenAI Icon
**File:** `frontend/src/assets/openai.svg`

- [ ] Add OpenAI logo SVG file
- [ ] Register in Vuetify custom icons (`frontend/src/plugins/vuetify.ts`)

#### Step 5.2: Add Chat Tab to App.vue
**File:** `frontend/src/App.vue`

- [ ] Add `chat` to `activeTab` type
- [ ] Add `chatChannelsData` ref for Chat channels
- [ ] Add Chat tab in navigation
- [ ] Add Chat tab content with `ChannelOrchestration` component
- [ ] Wire up API calls for Chat channels

#### Step 5.3: Update API Service
**File:** `frontend/src/services/api.ts`

- [ ] `getChatChannels()` - Fetch Chat channels
- [ ] `addChatChannel()` - Create Chat channel
- [ ] `updateChatChannel()` - Update Chat channel
- [ ] `deleteChatChannel()` - Delete Chat channel
- [ ] `addChatChannelKey()` - Add API key
- [ ] `removeChatChannelKey()` - Remove API key
- [ ] `reorderChatChannels()` - Reorder channels
- [ ] `setChatChannelStatus()` - Set channel status
- [ ] `getChatChannelMetrics()` - Get metrics
- [ ] `setChatLoadBalance()` - Set load balance

#### Step 5.4: Update ChannelOrchestration Component
**File:** `frontend/src/components/ChannelOrchestration.vue`

- [ ] Add `chat` channel type support
- [ ] Use OpenAI icon for Chat tab
- [ ] Wire up Chat-specific API calls

#### Step 5.5: Add i18n Translations
**Files:** `frontend/src/locales/en.json`, `frontend/src/locales/zh.json`

- [ ] Add translations for Chat tab and related UI text

### Phase 6: Testing

#### Step 6.1: Converter Tests
**File:** `backend-go/internal/converters/chat_to_claude_test.go`

- [ ] Test basic message conversion
- [ ] Test system message extraction
- [ ] Test tool calling conversion
- [ ] Test tool_choice conversion
- [ ] Test reasoning_effort mapping

**File:** `backend-go/internal/converters/claude_to_chat_test.go`

- [ ] Test basic response conversion
- [ ] Test tool_calls conversion
- [ ] Test finish_reason mapping
- [ ] Test usage conversion
- [ ] Test streaming chunk conversion

#### Step 6.2: Handler Tests
**File:** `backend-go/internal/handlers/chat_completions_test.go`

- [ ] Test authentication
- [ ] Test request parsing
- [ ] Test model permission check
- [ ] Test streaming response
- [ ] Test non-streaming response
- [ ] Test error handling

### Phase 7: Documentation

- [ ] Update `AGENTS.md` with Chat endpoint info
- [ ] Update `README.md` with new endpoint
- [ ] Update `CHANGELOG.md`

## Conversion Reference

### Messages Conversion: OpenAI → Claude

**OpenAI Input:**
```json
{
  "messages": [
    {"role": "system", "content": "You are helpful."},
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi!", "tool_calls": [
      {"id": "call_1", "type": "function", "function": {"name": "get_time", "arguments": "{}"}}
    ]},
    {"role": "tool", "tool_call_id": "call_1", "content": "14:30"}
  ]
}
```

**Claude Output:**
```json
{
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": [
      {"type": "text", "text": "Hi!"},
      {"type": "tool_use", "id": "call_1", "name": "get_time", "input": {}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "call_1", "content": "14:30"}
    ]}
  ]
}
```

### Tools Conversion: OpenAI → Claude

**OpenAI Input:**
```json
{
  "tools": [{
    "type": "function",
    "function": {
      "name": "get_weather",
      "description": "Get weather for location",
      "parameters": {
        "type": "object",
        "properties": {"location": {"type": "string"}},
        "required": ["location"]
      }
    }
  }]
}
```

**Claude Output:**
```json
{
  "tools": [{
    "name": "get_weather",
    "description": "Get weather for location",
    "input_schema": {
      "type": "object",
      "properties": {"location": {"type": "string"}},
      "required": ["location"]
    }
  }]
}
```

### Response Conversion: Claude → OpenAI

**Claude Input:**
```json
{
  "id": "msg_xxx",
  "content": [
    {"type": "text", "text": "The weather is sunny."},
    {"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"location": "Paris"}}
  ],
  "stop_reason": "tool_use",
  "usage": {"input_tokens": 100, "output_tokens": 50}
}
```

**OpenAI Output:**
```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "The weather is sunny.",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {"name": "get_weather", "arguments": "{\"location\":\"Paris\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": {"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}
}
```

## Estimated Effort

| Phase | Effort | Time |
|-------|--------|------|
| Phase 1: Config & Infrastructure | Low | 4-6 hours |
| Phase 2: Converters | High | 1.5-2 days |
| Phase 3: Handler | Medium-High | 1 day |
| Phase 4: API Key Permissions | Low | 1 hour |
| Phase 5: Frontend UI | Medium | 4-6 hours |
| Phase 6: Testing | Medium | 1 day |
| Phase 7: Documentation | Low | 2 hours |
| **Total** | | **4-6 days** |

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Tool calling edge cases | Medium | Medium | Comprehensive test coverage |
| Streaming format differences | Medium | High | Reference existing converters |
| Performance overhead from conversion | Low | Medium | Profile and optimize hot paths |
| Breaking existing endpoints | Low | High | Isolated implementation, no shared code changes |

## Success Criteria

1. ✅ `/v1/chat/completions` endpoint accepts valid OpenAI Chat requests
2. ✅ Responses match OpenAI Chat Completions format exactly
3. ✅ Streaming works correctly with proper SSE format
4. ✅ Tool calling works end-to-end with Claude upstream
5. ✅ Frontend shows Chat tab with channel management
6. ✅ All existing endpoints continue to work unchanged
7. ✅ Test coverage > 80% for new converter code
