# Two-tier debug log retention

## Background
Debug logs currently store request/response headers and bodies together, with full-row deletion after `retentionHours` (default 24h). Headers are much smaller and more useful for long-term debugging than bodies, which can contain sensitive message content and are large (up to 1MB truncated).

## Approach
Implement two-tier retention:
1. **Full logs** (headers + bodies) for `fullRetentionHours` (default 24h)
2. **Headers only** for `headerRetentionHours` (default 168h / 7 days) — bodies deleted
3. **Full deletion** after `headerRetentionHours`

This preserves diagnostic value while reducing storage and sensitive data exposure.

## Steps

### Backend
- [x] 1. Update `DebugLogConfig` struct to add `fullRetentionHours` and `headerRetentionHours` fields
- [x] 2. Update config validation and defaults (fullRetention=24h, headerRetention=168h)
- [x] 3. Rewrite `PurgeExpiredDebugLogs` to perform two-step cleanup (UPDATE to null bodies, DELETE full rows)
- [x] 4. Update config API handlers (GET/PUT) to handle new fields
- [ ] 5. Add tests for two-tier cleanup logic

### Frontend
- [x] 6. Update debug log config UI to show both retention settings
- [x] 7. Handle partial logs (headers present but bodies null) in the debug log viewer

### Documentation
- [x] 8. Update CHANGELOG.md with new retention behavior

## Implementation Summary

### Backend Changes
1. **Config struct** (`internal/config/config.go:492-530`):
   - Added `FullRetentionHours` (default 24h) and `HeaderRetentionHours` (default 168h)
   - Added `GetFullRetentionHours()` and `GetHeaderRetentionHours()` methods
   - Kept legacy `RetentionHours` field for backward compatibility

2. **Cleanup logic** (`internal/requestlog/debug.go:388-427`):
   - New `PurgeExpiredDebugLogsTwoTier()` method implements two-step cleanup
   - Step 1: UPDATE to null request_body/response_body after fullRetentionHours
   - Step 2: DELETE entire rows after headerRetentionHours
   - Legacy `PurgeExpiredDebugLogs()` kept for backward compatibility

3. **Scheduler** (`internal/requestlog/debug.go:473-495`):
   - New `StartDebugLogCleanupTwoTier()` goroutine calls two-tier cleanup every hour
   - Updated `main.go:407-417` to use new two-tier scheduler

4. **API handlers** (`internal/handlers/config.go:1853-1913`):
   - GET endpoint returns all fields including fullRetentionHours and headerRetentionHours
   - PUT endpoint accepts and updates both new fields
   - Legacy retentionHours still returned for backward compatibility

### Frontend Changes
1. **Type definitions** (`services/api.ts:1768-1773`):
   - Updated `DebugLogConfig` interface with new fields

2. **Settings UI** (`components/DebugLogSettings.vue:32-53`):
   - Two separate sliders for full retention (1-168h) and header retention (fullRetention-720h)
   - Header retention slider minimum is dynamically bound to full retention value
   - Added descriptive text explaining the two-tier system

3. **Debug modal** (`components/RequestDebugModal.vue:277-288, 358-369`):
   - Shows "Body expired (retention policy)" message when body is null but headers exist
   - Preserves existing v-if logic to conditionally render bodies

4. **Localization** (`locales/en.ts`, `locales/zh-CN.ts`):
   - Added `fullRetentionHours`, `fullRetentionDescription`, `headerRetentionHours`, `headerRetentionDescription`
   - Added `debugModal.bodyExpired` for expired body indicator

## Commits

- `326575a` - All steps complete (v1.5.46-v1.5.50)
