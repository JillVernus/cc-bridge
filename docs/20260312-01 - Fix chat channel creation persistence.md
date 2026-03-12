# Fix chat channel creation persistence

## Background
The OpenAI-compatible `/v1/chat/completions` incoming flow is implemented, but chat channel creation does not survive database-backed config persistence and reload. After creating a chat channel, the UI refreshes back to an empty state because database save/load and polling paths do not fully include chat channels and chat load-balance settings.

## Approach
Make database-backed config persistence treat chat channels the same way as messages, responses, and gemini channels. Update DB save/load, DB polling reload, and JSON-to-DB migration paths, then add backend tests to cover chat round-trip persistence and reload behavior.

## Steps
- [x] Step 1: Update `backend-go/internal/config/db_storage.go` to load/save chat channels and `chat_load_balance`, and re-index chat/gemini channels on DB polling reload.
- [x] Step 2: Update JSON-to-DB migration in `backend-go/internal/config/db_storage.go` to migrate gemini/chat channels and `chat_load_balance`.
- [x] Step 3: Add backend tests in `backend-go/internal/config/config_db_save_test.go` for chat save/load round-trip and polling reload persistence.
- [x] Step 4: Run backend tests for the config persistence changes.

## Commits
