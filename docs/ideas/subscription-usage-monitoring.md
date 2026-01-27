# Subscription Usage Monitoring for Claude Code

## Overview

Monitor token usage and costs for Claude Code users on **official Anthropic subscriptions** (Pro/Max) without intercepting API traffic. This approach reads local transcript files that Claude Code already generates, avoiding any T&C concerns.

## Background

### Why Not Proxy Subscription Traffic?

1. **`ANTHROPIC_BASE_URL`** is designed for alternative API providers (Bedrock, Vertex, OpenRouter), not for proxying official subscription traffic
2. Subscription mode uses OAuth tokens with unclear header differences from API key mode
3. Claude Code is closed source - we cannot verify exact auth behavior
4. Risk of violating Anthropic's Terms & Conditions

### The Solution: Local Transcript Monitoring

Claude Code writes detailed transcript files locally at:
```
~/.claude/projects/<project-path>/<session-id>.jsonl
```

These files contain **complete token usage data** for every API response. We can:
1. Use Claude Code's **hook system** to trigger on session events
2. Parse the transcript file to extract token/cost data
3. POST metrics to cc-bridge for unified monitoring

## Data Available

### From Transcript File (JSONL)

Each assistant message contains:

```json
{
  "sessionId": "7a712ed7-d6a5-4126-a72c-8164498af896",
  "timestamp": "2026-01-27T14:12:53.509Z",
  "cwd": "/workspaces/projects/workspace/cc-bridge",
  "gitBranch": "main",
  "version": "2.1.20",
  "slug": "prancy-wandering-moon",
  "message": {
    "model": "claude-opus-4-5-20251101",
    "id": "msg_018Gf3iQDMJ5P1kCiYAWEy65",
    "usage": {
      "input_tokens": 1,
      "cache_creation_input_tokens": 905,
      "cache_read_input_tokens": 86106,
      "output_tokens": 25,
      "service_tier": "standard"
    }
  }
}
```

### From Hook Input (stdin JSON)

```json
{
  "session_id": "abc123",
  "transcript_path": "~/.claude/projects/.../session.jsonl",
  "cwd": "/path/to/project",
  "hook_event_name": "Stop",
  "permission_mode": "default"
}
```

## Data Mapping: cc-bridge RequestLog vs Transcript

| cc-bridge Field | Transcript Source | Available |
|-----------------|-------------------|-----------|
| ID | `uuid` | ✅ |
| Status | (always "completed") | ✅ |
| InitialTime | `timestamp` | ✅ |
| CompleteTime | `timestamp` | ✅ |
| DurationMs | (calculate from messages) | ⚠️ |
| Type | "claude" | ✅ |
| ProviderName | "subscription" | ✅ |
| Model | `message.model` | ✅ |
| ResponseModel | `message.model` | ✅ |
| **InputTokens** | `message.usage.input_tokens` | ✅ |
| **OutputTokens** | `message.usage.output_tokens` | ✅ |
| **CacheCreationInputTokens** | `message.usage.cache_creation_input_tokens` | ✅ |
| **CacheReadInputTokens** | `message.usage.cache_read_input_tokens` | ✅ |
| TotalTokens | (sum of above) | ✅ |
| Price | (calculate from model pricing) | ✅ |
| HTTPStatus | (assume 200) | ✅ |
| Stream | (not available) | ❌ |
| ChannelID | (N/A) | ❌ |
| ChannelName | "subscription" | ✅ |
| Endpoint | "/v1/messages" | ✅ |
| ClientID | (derive from cwd/machine) | ⚠️ |
| **SessionID** | `sessionId` | ✅ |
| APIKeyID | (N/A for subscription) | ❌ |

### Bonus Data from Transcript

| Field | Use |
|-------|-----|
| `cwd` | Project directory |
| `gitBranch` | Git branch context |
| `version` | Claude Code version |
| `slug` | Human-readable session name |
| `message.id` | Anthropic's message ID |

## Claude Code Hook System

### Available Hook Events

| Event | When | Useful for |
|-------|------|------------|
| **SessionStart** | Session begins | Track new sessions |
| **UserPromptSubmit** | User sends message | Count user interactions |
| **PreToolUse** | Before tool runs | N/A |
| **PostToolUse** | After tool succeeds | Real-time per-request logging |
| **Stop** | Claude finishes turn | **Best: Sum tokens for turn** |
| **SubagentStop** | Subagent finishes | Track subagent usage |
| **SessionEnd** | Session terminates | **Final session summary** |
| **Notification** | Alert sent | **Alert user of costs** |

### Hook Configuration

Location: `~/.claude/settings.json` or `.claude/settings.json`

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/ccbridge-report.sh"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/ccbridge-session-end.sh"
          }
        ]
      }
    ]
  }
}
```

### Hook Script Example

```bash
#!/bin/bash
# ~/.claude/hooks/ccbridge-report.sh

INPUT=$(cat)
TRANSCRIPT=$(echo "$INPUT" | jq -r '.transcript_path')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id')

# Get latest assistant message with usage
LATEST=$(grep '"type":"assistant"' "$TRANSCRIPT" | grep '"usage"' | tail -1)

MODEL=$(echo "$LATEST" | jq -r '.message.model')
INPUT_TOKENS=$(echo "$LATEST" | jq -r '.message.usage.input_tokens')
OUTPUT_TOKENS=$(echo "$LATEST" | jq -r '.message.usage.output_tokens')
CACHE_CREATE=$(echo "$LATEST" | jq -r '.message.usage.cache_creation_input_tokens // 0')
CACHE_READ=$(echo "$LATEST" | jq -r '.message.usage.cache_read_input_tokens // 0')
TIMESTAMP=$(echo "$LATEST" | jq -r '.timestamp')
MSG_ID=$(echo "$LATEST" | jq -r '.message.id')

# POST to cc-bridge
curl -s -X POST http://localhost:3000/api/subscription/observe \
  -H "Content-Type: application/json" \
  -H "x-api-key: $CCBRIDGE_API_KEY" \
  -d "{
    \"session_id\": \"$SESSION_ID\",
    \"message_id\": \"$MSG_ID\",
    \"model\": \"$MODEL\",
    \"input_tokens\": $INPUT_TOKENS,
    \"output_tokens\": $OUTPUT_TOKENS,
    \"cache_creation_input_tokens\": $CACHE_CREATE,
    \"cache_read_input_tokens\": $CACHE_READ,
    \"timestamp\": \"$TIMESTAMP\",
    \"source\": \"subscription\"
  }"
```

## Implementation Components

### 1. cc-bridge Backend

**New endpoint**: `POST /api/subscription/observe`

```go
type SubscriptionObservation struct {
    SessionID                string    `json:"session_id"`
    MessageID                string    `json:"message_id"`
    Model                    string    `json:"model"`
    InputTokens              int       `json:"input_tokens"`
    OutputTokens             int       `json:"output_tokens"`
    CacheCreationInputTokens int       `json:"cache_creation_input_tokens"`
    CacheReadInputTokens     int       `json:"cache_read_input_tokens"`
    Timestamp                time.Time `json:"timestamp"`
    Source                   string    `json:"source"` // "subscription"
}
```

**Storage**: Reuse existing `RequestLog` table with:
- `ChannelName = "subscription"`
- `Type = "claude"`
- `Endpoint = "/v1/messages"`
- `APIKeyID = NULL` (or special marker for subscription)

### 2. Hook Scripts Package

Create installable hook scripts:
- `ccbridge-report.sh` - Report each turn's usage
- `ccbridge-session-end.sh` - Final session summary
- `ccbridge-alert.sh` - Optional cost alerts via Notification hook

### 3. Frontend Dashboard

- Add "subscription" as a data source filter
- Show subscription usage alongside API usage
- Distinguish in charts/tables with different color/icon

### 4. Setup/Configuration

User setup flow:
1. Install hook scripts to `~/.claude/hooks/`
2. Add hook config to `~/.claude/settings.json`
3. Set `CCBRIDGE_API_KEY` and `CCBRIDGE_URL` environment variables
4. Optionally configure cost alert thresholds

## Notification/Alert Feature

Use the `Notification` hook to alert users about costs:

```json
{
  "hooks": {
    "Notification": [
      {
        "matcher": "idle_prompt",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/ccbridge-cost-check.sh"
          }
        ]
      }
    ]
  }
}
```

The script could check accumulated session cost and show warnings.

## Benefits

1. **Zero traffic interference** - No proxy, no T&C concerns
2. **Complete token data** - All 4 token types available
3. **Session tracking** - Full session context (project, branch, etc.)
4. **Unified dashboard** - Same UI for API and subscription monitoring
5. **Alerting** - Leverage Claude Code's notification system

## Limitations

1. **Local only** - Requires hook scripts on each machine
2. **Not real-time** - Data arrives after each turn completes
3. **No request details** - Can't see request body/headers
4. **Requires user setup** - Manual configuration needed

## Future Enhancements

- Auto-installer script for hooks
- Claude Code plugin for easier setup
- Session cost alerts with configurable thresholds
- Historical cost trends per project/branch
- Team aggregation for multiple machines

## References

- [Claude Code Hooks Reference](https://code.claude.com/docs/en/hooks)
- [Claude Code Hook Guide](https://claude.com/blog/how-to-configure-hooks)
- Transcript location: `~/.claude/projects/<project-path>/<session-id>.jsonl`
- Stats cache: `~/.claude/stats-cache.json`
