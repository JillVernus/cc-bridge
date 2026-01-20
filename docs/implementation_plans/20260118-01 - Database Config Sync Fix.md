# Database Config Sync Fix

## Background

After migrating to PostgreSQL for HA support, configuration changes made in one CC-Bridge instance are not syncing to other instances sharing the same database. Request logs sync correctly, but channel configurations do not.

**Root Cause**: The `ConfigManager.saveConfigLocked()` function only writes to the JSON file, never to the database. The polling mechanism in other instances never detects changes because the database is never updated.

## Current Architecture

```
Instance A:
  ConfigManager.saveConfigLocked() → JSON file only
  ❌ Database NOT updated

Instance B:
  DBConfigStorage.pollLoop() → Checks database every 5s
  ❌ No changes detected (DB wasn't updated)
  ❌ Never reloads config
```

## Approach

Implement a **write-through cache** pattern where configuration changes are written to both:
1. JSON file (for backward compatibility and file-based deployments)
2. Database (for multi-instance sync)

The `ConfigManager` needs a reference to `DBConfigStorage` so it can call `SaveConfigToDB()` during `saveConfigLocked()`.

## Steps

- [x] Step 1: Add `dbStorage` field to `ConfigManager` struct
- [x] Step 2: Add `SetDBStorage()` method to `ConfigManager`
- [x] Step 3: Update `saveConfigLocked()` to also write to database when available
- [x] Step 4: Update `db_storage_init.go` to link DBConfigStorage to ConfigManager
- [x] Step 5: Test multi-instance sync

## Commits

- `13a2285` - fix: enable multi-instance config sync for PostgreSQL deployments
- `9bff564` - fix: use PostgreSQL placeholders in SaveConfigToDB
