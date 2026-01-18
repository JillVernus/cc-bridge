# Configurable Model Aliases

## Background

The model mapping feature in channel edit form has hardcoded source model options:
- **Messages API**: `opus`, `sonnet`, `haiku`
- **Responses API**: `codex`, `gpt-5.1-codex-max`, `gpt-5.1-codex`, `gpt-5.1-codex-mini`, `gpt-5.1`

Users cannot add custom source models without code changes. This plan adds a system setting to define source model aliases, following the same pattern as pricing config.

## Approach

Create a new `model-aliases.json` config with manager, API endpoints, and frontend settings dialog. The channel edit form will fetch source models from this config instead of hardcoded values.

## Files to Modify/Create

### Backend (Go)
| File | Action |
|------|--------|
| `backend-go/internal/aliases/aliases.go` | **Create** - Manager with file watching |
| `backend-go/internal/handlers/aliases.go` | **Create** - API handlers |
| `backend-go/internal/handlers/backup.go` | **Modify** - Add aliases to backup scope |
| `backend-go/main.go` | **Modify** - Init manager, register routes |

### Frontend (Vue/TypeScript)
| File | Action |
|------|--------|
| `frontend/src/services/api.ts` | **Modify** - Add types and API methods |
| `frontend/src/components/ModelAliasSettings.vue` | **Create** - Settings dialog |
| `frontend/src/components/AddChannelModal.vue` | **Modify** - Fetch from config |
| `frontend/src/locales/en.ts` | **Modify** - Add i18n strings |
| `frontend/src/locales/zh-CN.ts` | **Modify** - Add i18n strings |
| `frontend/src/App.vue` | **Modify** - Add settings button/dialog |

### Config
| File | Action |
|------|--------|
| `.config/model-aliases.json` | **Create** - Default config (auto-created on first run) |

## Steps

- [x] Step 1: Backend - Create Aliases Manager
- [x] Step 2: Backend - Create API Handlers
- [x] Step 3: Backend - Add to Backup Scope
- [x] Step 4: Backend - Register in main.go
- [x] Step 5: Frontend - Add Types and API Methods
- [x] Step 6: Frontend - Create Settings Dialog
- [x] Step 7: Frontend - Update AddChannelModal
- [x] Step 8: Frontend - Add i18n Strings
- [x] Step 9: Frontend - Add Settings Entry Point

### Step 1: Backend - Create Aliases Manager
Create `backend-go/internal/aliases/aliases.go`:
- `ModelAlias` struct: `{ value, description }`
- `AliasesConfig` struct: `{ messagesModels, responsesModels []ModelAlias }`
- `AliasesManager` with singleton pattern, file watching, thread-safe access
- Default config with current hardcoded values

### Step 2: Backend - Create API Handlers
Create `backend-go/internal/handlers/aliases.go`:
- `GET /api/aliases` - Get config
- `PUT /api/aliases` - Update entire config
- `POST /api/aliases/reset` - Reset to defaults

### Step 3: Backend - Add to Backup Scope
Modify `backend-go/internal/handlers/backup.go`:
- Add `Aliases *aliases.AliasesConfig` to `BackupData` struct
- Include aliases in `CreateBackup()`
- Restore aliases in `RestoreBackup()`

### Step 4: Backend - Register in main.go
Modify `backend-go/main.go`:
- Import aliases package
- Initialize `aliases.InitManager(".config/model-aliases.json")`
- Register routes under `/api/aliases`

### Step 5: Frontend - Add Types and API Methods
Modify `frontend/src/services/api.ts`:
```typescript
interface ModelAlias {
  value: string
  description?: string
}

interface AliasesConfig {
  messagesModels: ModelAlias[]
  responsesModels: ModelAlias[]
}

// Methods: getAliases(), updateAliases(), resetAliasesToDefault()
```

### Step 6: Frontend - Create Settings Dialog
Create `frontend/src/components/ModelAliasSettings.vue`:
- Two sections: Messages API models, Responses API models
- Add/Edit/Delete/Reorder operations for each section
- Reset to defaults button

### Step 7: Frontend - Update AddChannelModal
Modify `frontend/src/components/AddChannelModal.vue`:
- Fetch aliases config on mount
- Replace hardcoded `allSourceModelOptions` with fetched config
- Fallback to defaults if fetch fails

### Step 8: Frontend - Add i18n Strings
Add to both `en.ts` and `zh-CN.ts`:
```typescript
modelAliases: {
  title: 'Model Aliases',
  messagesModels: 'Messages API Models',
  responsesModels: 'Responses API Models',
  addModel: 'Add Model',
  // ... etc
}
```

### Step 9: Frontend - Add Settings Entry Point
Modify `frontend/src/App.vue`:
- Add "Model Aliases" button in settings area (near Pricing Settings)
- Import and use `ModelAliasSettings` component

## Data Structure

### Config File: `.config/model-aliases.json`
```json
{
  "messagesModels": [
    { "value": "opus", "description": "Claude Opus" },
    { "value": "sonnet", "description": "Claude Sonnet" },
    { "value": "haiku", "description": "Claude Haiku" }
  ],
  "responsesModels": [
    { "value": "codex", "description": "Codex" },
    { "value": "gpt-5.1-codex-max", "description": "GPT-5.1 Codex Max" },
    { "value": "gpt-5.1-codex", "description": "GPT-5.1 Codex" },
    { "value": "gpt-5.1-codex-mini", "description": "GPT-5.1 Codex Mini" },
    { "value": "gpt-5.1", "description": "GPT-5.1" }
  ]
}
```

### Backup Data Structure Update
```go
type BackupData struct {
    Version   string                  `json:"version"`
    CreatedAt string                  `json:"createdAt"`
    Config    *config.Config          `json:"config"`
    Pricing   *pricing.PricingConfig  `json:"pricing,omitempty"`
    Aliases   *aliases.AliasesConfig  `json:"aliases,omitempty"`  // NEW
}
```

## Verification

1. **Backend Build**: `cd backend-go && make build`
2. **Frontend Type Check**: `cd frontend && bun run type-check`
3. **API Test**:
   - `GET /api/model-aliases` returns default config
   - `PUT /api/model-aliases` updates config
   - `POST /api/model-aliases/reset` resets to defaults
4. **UI Test**:
   - Open Model Aliases settings dialog
   - Add/edit/delete models
   - Verify changes appear in channel edit form's model mapping dropdown
5. **Backup Test**:
   - Create backup, verify aliases included
   - Restore backup, verify aliases restored
6. **Hot Reload Test**:
   - Manually edit `.config/model-aliases.json`
   - Verify changes reflected without restart

## Commits
<!-- Added after each commit -->
