# Gemini Incoming Endpoint (Direct Passthrough)

## Background

Currently, CC-Bridge accepts requests in Claude format (`/v1/messages`) and Codex Responses format (`/v1/responses`), then routes them to various upstream providers (Claude, OpenAI, Gemini, etc.) with protocol translation as needed.

**Frontend tabs represent INCOMING API endpoints:**
| Tab | Incoming Endpoint | Channels define upstream routing |
|-----|-------------------|----------------------------------|
| Claude | `/v1/messages` | Can route to claude/openai/gemini upstreams with translation |
| Codex | `/v1/responses` | Can route to various upstreams with translation |

This plan adds:
1. New incoming endpoint `/v1/gemini/models/*` that accepts **Gemini-native format** requests
2. New **Gemini tab** in frontend with dedicated channel list (`GeminiUpstream`)
3. Direct passthrough to Gemini upstreams (no translation for now)

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         CC-Bridge Frontend                               │
├─────────────────┬─────────────────┬─────────────────────────────────────┤
│   Claude Tab    │   Codex Tab     │   Gemini Tab (NEW)                  │
│   /v1/messages  │   /v1/responses │   /v1/gemini/models/*               │
│                 │                 │                                     │
│ Channels:       │ Channels:       │ Channels:                           │
│ - claude→claude │ - responses→*   │ - gemini→gemini (passthrough)      │
│ - claude→openai │                 │ - (future: gemini→claude, etc.)    │
│ - claude→gemini │                 │                                     │
└─────────────────┴─────────────────┴─────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         CC-Bridge Backend                                │
├─────────────────┬─────────────────┬─────────────────────────────────────┤
│ config.Upstream │ config.Responses│ config.GeminiUpstream (NEW)         │
│                 │ Upstream        │                                     │
└─────────────────┴─────────────────┴─────────────────────────────────────┘
```

**Request Flow:**
```
Client (Gemini format)
    ↓
POST /v1/gemini/models/{model}:generateContent
POST /v1/gemini/models/{model}:streamGenerateContent
    ↓
Auth middleware (existing)
    ↓
GeminiHandler (uses GeminiUpstream channel list)
    ↓
Channel Scheduler (select from GeminiUpstream)
    ↓
GeminiPassthroughProvider
    ↓
Gemini API
    ↓
Response (Gemini format, passthrough)
    ↓
Client
```

## Implementation Steps

### Phase 1: Backend - Core Handler & Provider ✅ DONE

#### Step 1.1: Create Gemini Passthrough Provider ✅
**File:** `backend-go/internal/providers/gemini_passthrough.go`

- [x] Read original request body
- [x] Validate model/action path (security: reject `..`, `/`, invalid chars)
- [x] Extract model from URL path
- [x] Build target URL with query string (strip sensitive params like `key`)
- [x] Forward headers transparently
- [x] Set Gemini auth header

#### Step 1.2: Create Gemini Incoming Handler ✅
**File:** `backend-go/internal/handlers/gemini.go`

- [x] Authentication check
- [x] Endpoint permission check (`gemini` endpoint)
- [x] Read and store request body
- [x] Extract model from URL path
- [x] Create pending request log entry
- [x] Handle streaming vs non-streaming responses
- [x] Update request log on completion

#### Step 1.3: Register Routes ✅
**File:** `backend-go/main.go`

```go
geminiGroup := v1Group.Group("/gemini")
{
    geminiGroup.POST("/models/*action", handlers.GeminiHandlerWithAPIKey(...))
}
```

### Phase 2: Backend - Dedicated Channel List ✅ DONE

#### Step 2.1: Add GeminiUpstream to Config ✅
**File:** `backend-go/internal/config/config.go`

- [x] Added `GeminiUpstream []UpstreamConfig` field to Config struct
- [x] Added `GeminiLoadBalance string` field
- [x] Added default values in `NewConfigManager`
- [x] Added index/ID migration for GeminiUpstream on startup
- [x] Added `validateChannelKeys` check for Gemini channels
- [x] Added `warnInsecureChannels` check for Gemini channels

#### Step 2.2: Add ConfigManager Methods ✅
**File:** `backend-go/internal/config/config.go`

- [x] `GetCurrentGeminiUpstream()` - Get current Gemini channel
- [x] `GetGeminiUpstreams()` - List all Gemini channels
- [x] `AddGeminiUpstream()` - Add new Gemini channel
- [x] `UpdateGeminiUpstream()` - Update existing channel
- [x] `RemoveGeminiUpstream()` - Delete channel
- [x] `AddGeminiAPIKey()` - Add API key to channel
- [x] `RemoveGeminiAPIKeyByIndex()` - Remove API key by index
- [x] `ReorderGeminiChannels()` - Reorder channels
- [x] `SetGeminiChannelStatus()` - Set channel status
- [x] `GetNextGeminiAPIKey()` - Key rotation with GeminiLoadBalance
- [x] `SetGeminiLoadBalance()` - Set load balance strategy

#### Step 2.3: Add Gemini Channel CRUD APIs ✅
**File:** `backend-go/internal/handlers/gemini_config.go`

- [x] `GET /api/gemini/channels` - List Gemini channels
- [x] `POST /api/gemini/channels` - Add Gemini channel
- [x] `PUT /api/gemini/channels/:id` - Update Gemini channel
- [x] `DELETE /api/gemini/channels/:id` - Delete Gemini channel
- [x] `POST /api/gemini/channels/:id/keys` - Add API key
- [x] `DELETE /api/gemini/channels/:id/keys/index/:keyIndex` - Remove API key
- [x] `POST /api/gemini/channels/reorder` - Reorder channels
- [x] `PATCH /api/gemini/channels/:id/status` - Set channel status
- [x] `GET /api/gemini/channels/metrics` - Get channel metrics
- [x] `PUT /api/gemini/loadbalance` - Update load balance strategy

#### Step 2.4: Add Scheduler Support for Gemini Channels ✅
**File:** `backend-go/internal/scheduler/channel_scheduler.go`

- [x] Added `geminiMetricsManager *metrics.MetricsManager` field
- [x] `IsGeminiMultiChannelMode()` - Check if multi-channel mode
- [x] `GetActiveGeminiChannelCount()` - Get active channel count
- [x] `SelectGeminiChannel()` - Select channel with circuit breaker
- [x] `RecordGeminiSuccess()` - Record successful request
- [x] `RecordGeminiFailure()` - Record failed request
- [x] `ResetGeminiChannelMetrics()` - Reset channel metrics
- [x] `GetGeminiMetricsManager()` - Get metrics manager

#### Step 2.5: Update Gemini Handler to Use GeminiUpstream ✅
**File:** `backend-go/internal/handlers/gemini.go`

- [x] Changed from `SelectChannelByServiceType()` to `SelectGeminiChannel()`
- [x] Changed from `IsMultiChannelMode(false)` to `IsGeminiMultiChannelMode()`
- [x] Changed from `GetActiveChannelCount(false)` to `GetActiveGeminiChannelCount()`
- [x] Changed from `RecordSuccess/Failure(idx, false)` to `RecordGeminiSuccess/Failure(idx)`
- [x] Changed from `GetNextAPIKey()` to `GetNextGeminiAPIKey()` for key rotation

#### Step 2.6: Update Scheduler Stats Endpoint ✅
**File:** `backend-go/internal/handlers/channel_metrics_handler.go`

- [x] Updated `GetSchedulerStats` to support `?type=gemini` parameter

#### Step 2.7: Clean Up Deprecated Methods ✅
**Files:** `backend-go/internal/scheduler/channel_scheduler.go`, `backend-go/internal/config/config.go`

- [x] Removed `SelectChannelByServiceType()` from scheduler
- [x] Removed `getActiveChannelsByServiceType()` from scheduler
- [x] Removed `GetUpstreamByServiceType()` from config

#### Step 2.8: Security Fix ✅
**File:** `backend-go/internal/providers/gemini_passthrough.go`

- [x] Fixed `sanitizeQueryString()` to return empty string on parse error (prevents key leakage)

#### Step 2.9: Database Storage Support ✅
**File:** `backend-go/internal/config/db_storage.go`

- [x] Added `syncChannelsTx` call for "gemini" channel type in `SaveConfigToDB`
- [x] Added `gemini_load_balance` setting to database
- [x] Load `GeminiUpstream` from database in `LoadConfigFromDB`
- [x] Load `gemini_load_balance` setting from database

### Phase 3: Frontend - Gemini Tab ✅ DONE

#### Step 3.1: Add Gemini Icon ✅
**Files:** `frontend/src/assets/gemini.svg`, `frontend/src/plugins/vuetify.ts`

- [x] Added Gemini SVG icon
- [x] Registered icon in Vuetify

#### Step 3.2: Add Gemini Tab Navigation ✅
**File:** `frontend/src/App.vue`

- [x] Added Gemini tab button with icon
- [x] Updated `activeTab` type to include 'gemini'
- [x] Added `geminiChannelsData` ref
- [x] Updated `currentChannelsData` computed for gemini
- [x] Updated `channelTypeForComponents` computed
- [x] Updated `refreshChannels` to fetch gemini data
- [x] Updated `saveChannel` to handle gemini
- [x] Updated `confirmDeleteChannel` for gemini
- [x] Updated `addApiKey` for gemini
- [x] Updated `confirmDeleteApiKey` for gemini
- [x] Updated `updateLoadBalance` for gemini
- [x] Updated auto-refresh interval for gemini

#### Step 3.3: Add API Service Methods ✅
**File:** `frontend/src/services/api.ts`

- [x] `getGeminiChannels()` - List channels
- [x] `addGeminiChannel()` - Add channel
- [x] `updateGeminiChannel()` - Update channel
- [x] `deleteGeminiChannel()` - Delete channel
- [x] `addGeminiApiKey()` - Add API key
- [x] `removeGeminiApiKeyByIndex()` - Remove API key
- [x] `reorderGeminiChannels()` - Reorder channels
- [x] `setGeminiChannelStatus()` - Set status
- [x] `getGeminiChannelMetrics()` - Get metrics
- [x] `updateGeminiLoadBalance()` - Update load balance
- [x] Updated `getSchedulerStats()` to support 'gemini' type

#### Step 3.4: Update Components for Gemini ✅
**File:** `frontend/src/components/ChannelOrchestration.vue`

- [x] Updated `channelType` prop to include 'gemini'
- [x] Updated API calls to use correct endpoints

**File:** `frontend/src/components/AddChannelModal.vue`

- [x] Updated `channelType` prop to include 'gemini'
- [x] Updated `getDefaultServiceType()` for gemini
- [x] Updated `getDefaultServiceTypeValue()` for gemini
- [x] Updated `getDefaultBaseUrl()` for gemini (`https://generativelanguage.googleapis.com/v1beta`)

### Phase 4: Bug Fixes ✅ DONE

#### Fix 4.1: Missing 'current' Field in API Response ✅
**File:** `backend-go/internal/handlers/gemini_config.go`

- [x] Added `"current": -1` to `GetGeminiUpstreams` response
- [x] Frontend `ChannelsResponse` interface expects this field

#### Fix 4.2: Database Storage Support ✅
**File:** `backend-go/internal/config/db_storage.go`

- [x] Added Gemini channel sync to `SaveConfigToDB`
- [x] Added Gemini channel loading in `LoadConfigFromDB`
- [x] Added `gemini_load_balance` setting persistence
- [x] Fixes issue where Gemini channels weren't persisted when using PostgreSQL

## Files Summary

### Phase 1 ✅ DONE

| File | Status | Description |
|------|--------|-------------|
| `backend-go/internal/providers/gemini_passthrough.go` | ✅ Created | Passthrough provider |
| `backend-go/internal/handlers/gemini.go` | ✅ Created | Incoming handler |
| `backend-go/main.go` | ✅ Modified | Route registration |

### Phase 2 ✅ DONE

| File | Status | Description |
|------|--------|-------------|
| `backend-go/internal/config/config.go` | ✅ Modified | GeminiUpstream field, CRUD methods, load balance, migration |
| `backend-go/internal/config/db_storage.go` | ✅ Modified | Database storage support for Gemini channels |
| `backend-go/internal/handlers/gemini_config.go` | ✅ Created | Gemini channel CRUD APIs |
| `backend-go/internal/handlers/config.go` | ✅ Modified | UpdateGeminiLoadBalance handler |
| `backend-go/internal/handlers/channel_metrics_handler.go` | ✅ Modified | Gemini support in GetSchedulerStats |
| `backend-go/internal/scheduler/channel_scheduler.go` | ✅ Modified | geminiMetricsManager, Gemini selection methods |
| `backend-go/main.go` | ✅ Modified | Gemini channel API routes |

### Phase 3 ✅ DONE

| File | Status | Description |
|------|--------|-------------|
| `frontend/src/assets/gemini.svg` | ✅ Created | Gemini icon |
| `frontend/src/plugins/vuetify.ts` | ✅ Modified | Register Gemini icon |
| `frontend/src/App.vue` | ✅ Modified | Gemini tab, data, methods |
| `frontend/src/services/api.ts` | ✅ Modified | Gemini API methods |
| `frontend/src/components/ChannelOrchestration.vue` | ✅ Modified | Support 'gemini' channelType |
| `frontend/src/components/AddChannelModal.vue` | ✅ Modified | Gemini defaults |

## Current State

**✅ FULLY IMPLEMENTED:**
- The `/v1/gemini/models/*` endpoint is functional
- It routes to channels in the dedicated **Gemini tab** with `GeminiUpstream` channel list
- Frontend has full CRUD functionality for Gemini channels
- Load balancing works correctly for Gemini channels
- Key rotation uses `GetNextGeminiAPIKey` with `GeminiLoadBalance` strategy
- Scheduler stats support `?type=gemini` parameter
- Channel metrics and circuit breakers work independently
- **Database storage (PostgreSQL) fully supported** - channels persist correctly

## Storage Modes

CC-Bridge supports two storage modes:

### 1. JSON File Storage (Default)
- Config saved to `backend-go/.config/config.json`
- File watcher detects changes and reloads automatically
- Suitable for single-instance deployments

### 2. Database Storage (PostgreSQL/SQLite)
- Enabled with `STORAGE_BACKEND=database`
- Config saved to `channels` and `settings` tables
- Polling mechanism syncs changes across multiple instances
- Suitable for multi-instance deployments
- **Gemini channels fully supported** (as of fix 769b31d)

## Testing

```bash
# Non-streaming
curl -X POST "http://localhost:8080/v1/gemini/models/gemini-2.0-flash:generateContent" \
  -H "x-api-key: YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
  }'

# Streaming
curl -X POST "http://localhost:8080/v1/gemini/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "x-api-key: YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
  }'
```

## Commits

- `bb8ea56` - feat: add Gemini incoming endpoint with direct passthrough
- `9d39d45` - chore: add .ccb_config and sample_request to gitignore
- `3bf6978` - fix: add missing 'current' field in Gemini channels API response
- `769b31d` - fix: add GeminiUpstream sync to database storage

## Known Issues

None - all features fully implemented and tested.
