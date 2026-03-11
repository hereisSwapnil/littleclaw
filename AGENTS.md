# AGENTS.md

This file describes how the LittleClaw autonomous agent works, for both human
contributors and AI coding assistants operating on this codebase.

## Overview

LittleClaw is a single-agent system. There is one `NanoCore` instance that
processes every inbound message through a **ReAct loop** (Reason-Act), calling
tools as needed and replying via Telegram. There is no multi-agent orchestration
or task delegation; the same loop handles all requests.

## Agent Architecture

### Entry Point

`cmd/littleclaw/main.go` boots the system:

1. Loads config from `~/.littleclaw/config.json`.
2. Creates the LLM provider (OpenAI-compatible API).
3. Creates the `MessageBus` (buffered channels, cap 100).
4. Creates the `NanoCore` agent.
5. Starts the Telegram channel (polling goroutine).
6. Starts the heartbeat (5-minute background ticker).
7. Starts the cron service.
8. Enters the main loop: read from `msgBus.Inbound`, call `RunAgentLoop()`,
   write to `msgBus.Outbound`.

### The ReAct Loop

Defined in `pkg/agent/loop.go` (`RunAgentLoop` method).

```
User message
  -> Build system prompt (identity + memory + entities + history + cron)
  -> Send to LLM with tool definitions
  -> If LLM returns tool calls:
       Execute each tool -> append results -> re-send to LLM
       (repeat up to 10 iterations)
  -> Final text response -> send to user via MessageBus
```

**Key constants:**

| Constant | Value | Purpose |
|---|---|---|
| `Max iterations` | 10 | Hard cap on tool-call rounds per message |
| `identityBudgetTokens` | 800 | Token budget for SOUL/IDENTITY/USER files |
| `coreBudgetTokens` | 2000 | Token budget for MEMORY.md |
| `historyBudgetBytes` | 16000 | Byte budget for recent conversation history (~4000 tokens) |
| `entityBudgetTokens` | 800 | Token budget for auto-surfaced entities |
| `cronBudgetTokens` | 400 | Token budget for cron summaries |
| `maxToolResultChars` | 3000 | Max chars in a single tool result |
| `preCompactionThreshold` | 0.80 | Trigger early consolidation at 80% of context window |

### System Prompt Assembly

The system prompt is built fresh for every message by `buildSystemPrompt()`:

1. **Formatting rules** -- Hardcoded Telegram markdown guidance.
2. **Identity block** -- Contents of `SOUL.md`, `IDENTITY.md`, `USER.md`
   (truncated to `identityBudgetTokens`).
3. **Core memory** -- Contents of `MEMORY.md` (truncated to
   `coreBudgetTokens`).
4. **Cron summary** -- Active scheduled jobs (truncated to
   `cronBudgetTokens`).
5. **Auto-surfaced entities** -- Entities whose names appear in the user
   message (trigram similarity matching, truncated to `entityBudgetTokens`).
6. **Recent history** -- Today's and yesterday's daily logs (truncated to
   `historyBudgetBytes`).

Total budget target: ~8000 tokens.

### Pre-Compaction

When the LLM response includes `usage.prompt_tokens`, the agent tracks it. If
prompt tokens exceed 80% of the estimated context window, the heartbeat is
triggered early to consolidate memory before the next turn.

## Tool System

### Tool Registration

Tools are registered in three places:

1. **Core + Web tools** -- `pkg/tools/registry.go` registers `read_file`,
   `write_file`, `append_file`, `exec`, `send_telegram_file`, `reload_skills`,
   `web_fetch`, `web_search`, and dynamically loaded skill scripts.
2. **Memory + Cron tools** -- `pkg/agent/loop.go` (`registerMemoryTools` and
   `registerCronTools`) registers `update_core_memory`,
   `append_core_memory`, `read_core_memory`, `search_history`,
   `read_entity`, `write_entity`, `write_summary`,
   `read_internal_log`, `list_entities`, `add_cron`, `remove_cron`,
   `list_cron`.
3. **Workspace tools** -- `pkg/agent/workspace_tools.go`
   (`registerWorkspaceTools`) registers `list_workspace`,
   `create_workspace_folder`, `track_item`, `list_tracked`,
   `get_tracker_json`, `record_script_run`.

### Full Tool Inventory (26 tools)

| Tool | Source | Description |
|---|---|---|
| `read_file` | registry.go | Read a file from workspace (path-protected) |
| `write_file` | registry.go | Write/overwrite a file (path-protected) |
| `append_file` | registry.go | Append to a file (path-protected) |
| `exec` | registry.go | Execute a shell command |
| `send_telegram_file` | registry.go | Send a file to the user via Telegram |
| `reload_skills` | registry.go | Hot-reload scripts from `skills/` directory |
| `web_fetch` | web.go | Fetch a URL and return stripped text content |
| `web_search` | web.go | Search the web (Tavily -> DuckDuckGo fallback) |
| `update_core_memory` | loop.go | Replace a section in MEMORY.md |
| `append_core_memory` | loop.go | Append text to a section in MEMORY.md |
| `read_core_memory` | loop.go | Read current contents of MEMORY.md |
| `search_history` | loop.go | Full-text search across daily logs and archives |
| `read_entity` | loop.go | Read a specific entity knowledge file |
| `write_entity` | loop.go | Create/update an entity knowledge file |
| `write_summary` | loop.go | Write a daily summary to summaries directory |
| `read_internal_log` | loop.go | Read the last 4KB of INTERNAL.md |
| `list_entities` | loop.go | List all entity files |
| `add_cron` | loop.go | Schedule a recurring task |
| `remove_cron` | loop.go | Remove a scheduled task |
| `list_cron` | loop.go | List all scheduled tasks |
| `list_workspace` | workspace_tools.go | List workspace directory tree |
| `create_workspace_folder` | workspace_tools.go | Create a new workspace folder |
| `track_item` | workspace_tools.go | Track an item in a folder's tracker.json |
| `list_tracked` | workspace_tools.go | List tracked items for a folder |
| `get_tracker_json` | workspace_tools.go | Get raw tracker.json contents |
| `record_script_run` | workspace_tools.go | Record a script execution in tracker |

### Tool Execution Flow

1. LLM returns one or more `tool_calls` in its response.
2. For each tool call, the agent looks up the handler in the registry by name.
3. The handler receives `(ctx, args map[string]interface{})` and returns a
   `*ToolResult` with fields:
   - `ForLLM` -- Text fed back to the model.
   - `ForUser` -- (Optional) Text sent directly to the user.
   - `Files` -- (Optional) File paths to send to the user.
4. Results are appended to the message history and the loop continues.

### Path Protection

All file tools (`read_file`, `write_file`, `append_file`) enforce that paths
stay within the workspace. Attempts to escape with `..` or absolute paths are
rejected. The check lives in `registry.go` (`resolveAndProtectPath`).

### Dynamic Skills

Scripts placed in `workspace/skills/` are auto-registered as tools on startup.
Each script becomes a tool named after its filename (without extension). The
agent can call `reload_skills` to pick up new scripts at runtime. Scripts
receive arguments as environment variables.

## Memory System

Defined in `pkg/memory/memory.go`. Five tiers:

### Tier 1: Daily Logs

- Path: `memory/YYYY-MM-DD.md`
- Every user/assistant exchange is appended to today's file.
- When a daily log exceeds 8KB, the heartbeat summarizes it.
- The system prompt includes today's and yesterday's logs as recent history.

### Tier 2: Core Memory (MEMORY.md)

- Path: `memory/MEMORY.md`
- Long-term facts organized by section headers (`## Section Name`).
- Modified via `update_core_memory` (replace section) or `append_core_memory`.
- Versioned backups: `MEMORY.md.v1`, `.v2`, etc. (max 5 kept).

### Tier 3: Entities

- Path: `memory/ENTITIES/<normalized_name>.md`
- Deep knowledge files for people, projects, topics.
- Names are normalized (lowercased, spaces to underscores, non-alnum stripped).
- Auto-surfaced in the system prompt when the user message matches an entity
  name via **trigram similarity** (threshold: 0.3).

### Tier 4: Summaries

- Path: `memory/summaries/YYYY-MM-DD_summary.md`
- Generated by the heartbeat when daily logs grow large.
- Provide compressed historical context.

### Tier 5: Internal Log

- Path: `memory/INTERNAL.md`
- Background reasoning, heartbeat activity, consolidation notes.
- Rotates at 1MB (archived to `INTERNAL.md.archive.YYYYMMDD`).
- Readback capped at 4KB via `read_internal_log`.

### Dirty Flag

The memory store has an atomic `dirty` flag. It is set whenever new history is
appended and cleared by the heartbeat after consolidation. This prevents
redundant consolidation cycles.

## Heartbeat

Defined in `pkg/agent/heartbeat.go`. Runs every 5 minutes in a background
goroutine.

Each tick:

1. **Check dirty flag** -- Skip if no new activity since last tick.
2. **Consolidate** -- Append reasoning notes to `INTERNAL.md`.
3. **Summarize** -- If today's daily log exceeds 8KB, generate a summary.
4. **Pre-compaction check** -- If the agent detected it was approaching the
   context window limit, trigger early consolidation.
5. **Clear dirty flag**.

## Cron Service

Defined in `pkg/agent/cron.go`. Persisted in `CRON.json`.

- Supports `@every <duration>` and standard cron expressions.
- Each job stores: ID, expression, prompt, status, lastRun, nextRun, error count.
- Run history is logged to `cron/runs/<jobID>.jsonl` (one JSON line per run).
- On tick, the cron service sends the job's prompt through the normal
  `MessageBus.Inbound` channel, so it is processed by the same ReAct loop.

## Message Flow

```
Telegram Bot (polling)
  |
  v
MessageBus.Inbound  <-- also fed by CronService
  |
  v
main loop (main.go)
  |
  v
NanoCore.RunAgentLoop()
  |  builds system prompt
  |  sends to LLM provider
  |  executes tool calls (0-10 iterations)
  |
  v
MessageBus.Outbound
  |
  v
Telegram Bot (sends reply)
```

## Guidelines for AI Assistants

When working on this codebase:

- **No test files exist yet.** Consider adding `*_test.go` files alongside new
  features.
- **All tools must be registered** in one of the three registration functions.
  Adding a new tool means: (1) define the `ToolDefinition` schema, (2) write
  the `Handler` function, (3) register both in the appropriate `register*`
  method.
- **Path protection is mandatory** for any tool that accesses the filesystem.
  Use `resolveAndProtectPath` from `registry.go`.
- **Memory writes set the dirty flag.** If you add a new way to write to
  memory, call `s.dirty.Store(true)` so the heartbeat picks it up.
- **The provider is OpenAI-compatible only.** All providers must speak the
  OpenAI chat completions API (OpenAI, OpenRouter, Ollama).
