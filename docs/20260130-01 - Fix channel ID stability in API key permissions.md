# Fix Channel ID Stability in API Key Permissions

## Background

**Bug**: API key allowed channels settings break when channels are added or deleted.

**Root Cause**: API keys store channel permissions as **array indices** (`[]int`), but channel indices change when channels are added/deleted. Channels already have stable string IDs (`UpstreamConfig.ID`), but these aren't being used.

**Example**:
1. User sets API key to allow channel at index 0 (Channel A)
2. User adds a new channel, which gets inserted before Channel A
3. Channel A is now at index 1
4. API key still has `AllowedChannelsMsg: [0]`
5. Now the API key allows the new channel (index 0) instead of Channel A

## Approach

Change `AllowedChannels*` fields from `[]int` (indices) to `[]string` (stable channel IDs) throughout the stack:
- Backend types and database schema
- Permission checking logic
- Handler integration (proxy, responses, gemini)
- Frontend channel selection

Include a migration to convert existing index-based permissions to ID-based.

## Design Decisions

### Composite Channel Semantics (By Design)
Allowing a composite channel implicitly grants access to all its target channels. This is intentional:
- Fast setup: allow one composite channel instead of each target
- Easy editing: modify composite mappings without updating API key permissions

### Fail-Closed Security
When migration cannot map an index to a valid channel ID:
- Do NOT convert to empty `[]` (which means "unrestricted")
- Keep the unmappable restriction and deny access
- Log warning for admin review

### API Compatibility
Frontend and backend must deploy together since `number[]` → `string[]` is a breaking change.

## Steps

- [x] Step 1: Update backend types (`apikey/types.go`)
  - Change `AllowedChannelsMsg`, `AllowedChannelsResp`, `AllowedChannelsGemini` from `[]int` to `[]string`
  - Update comments to clarify these are channel IDs, not indices

- [x] Step 2: Add channel ID resolver to apikey manager
  - Inject a resolver function `func(endpointType string, index int) string` into `Manager`
  - This allows migration without tight coupling to `ConfigManager`
  - Resolver returns channel ID for given index, or empty string if invalid

- [x] Step 3: Update backend manager (`apikey/manager.go`)
  - Implement tolerant decoding: try `[]string` first, fallback to `[]int` with conversion
  - Run migration in `refreshCache` before caching validated keys
  - Use fail-closed behavior: if index cannot be mapped, keep a sentinel value like `"__invalid__"` that will never match
  - Write back converted data after successful migration
  - Remove unused `unmarshalIntSlice` after migration

- [x] Step 4: Update permission checking (`apikey/permissions.go`)
  - Change `GetAllowedChannelsByType` return type from `[]int` to `[]string`
  - Change `IsChannelAllowed` to accept channel ID (string) instead of index (int)
  - Update `GetAllowedChannels` signature
  - Remove `IsChannelAllowed` if confirmed unused (or update if we find callers)

- [x] Step 5: Update handlers (proxy.go, responses.go, gemini.go)
  - These call `GetAllowedChannels()` / `GetAllowedChannelsByType()` and pass to scheduler
  - Update to work with `[]string` (channel IDs) instead of `[]int`
  - Verify scheduler accepts channel IDs for filtering

- [x] Step 6: Update frontend (`APIKeyManagement.vue`)
  - Change channel select `value` from `c.index` to `c.id`
  - Keep display as `[index] name` for UX consistency
  - Ensure form state uses string arrays

- [ ] Step 7: Test and verify
  - Test adding/deleting channels doesn't break API key permissions
  - Test existing API keys with channel permissions still work after migration
  - Test permission checking with channel IDs
  - Test migration edge cases: valid mapping, partial mapping, all-invalid mapping
  - Test fail-closed behavior when indices can't be resolved

## Files to Modify

| File | Changes |
|------|---------|
| `backend-go/internal/apikey/types.go` | Change `[]int` → `[]string` for channel fields |
| `backend-go/internal/apikey/manager.go` | Add resolver injection, tolerant decoding, migration logic |
| `backend-go/internal/apikey/permissions.go` | Update signatures and logic, remove dead code |
| `backend-go/internal/handlers/proxy.go` | Update to use `[]string` for allowed channels |
| `backend-go/internal/handlers/responses.go` | Update to use `[]string` for allowed channels |
| `backend-go/internal/handlers/gemini.go` | Update to use `[]string` for allowed channels |
| `backend-go/internal/scheduler/channel_scheduler.go` | Verify/update to filter by channel ID |
| `frontend/src/components/APIKeyManagement.vue` | Use channel ID instead of index |

## Migration Strategy

### Tolerant Decoding (in `refreshCache` and key loading)
1. Read JSON from database
2. Try parsing as `[]string` first
3. If fails, try parsing as `[]int`
4. If `[]int` detected, convert using resolver:
   - For each index, call resolver to get channel ID
   - If resolver returns empty string (invalid index), use `"__invalid_idx_N__"` sentinel
   - This ensures fail-closed: sentinel will never match a real channel
5. Write converted `[]string` back to database
6. Log migration: `"Migrated API key %d channel permissions from indices to IDs"`

### Resolver Implementation
```go
type ChannelIDResolver func(endpointType string, index int) string

func (m *Manager) SetChannelIDResolver(resolver ChannelIDResolver)
```

Called from main.go after both `ConfigManager` and `APIKeyManager` are initialized:
```go
apiKeyManager.SetChannelIDResolver(func(endpointType string, index int) string {
    channels := configManager.GetChannelsByEndpoint(endpointType)
    if index >= 0 && index < len(channels) {
        return channels[index].ID
    }
    return ""
})
```

### Edge Cases
- **Already-broken data**: If indices shifted before migration, we persist current (possibly wrong) mapping. Users may need to re-verify and re-save permissions.
- **Deleted channels**: Indices pointing to deleted channels become `__invalid__` sentinels → access denied (fail-closed)
- **Empty restrictions**: `[]` or `null` still means "unrestricted" (no change)

## Commits

[Added after each commit]
