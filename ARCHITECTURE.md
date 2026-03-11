# Architecture

This document describes the system architecture of LittleClaw.

## Package Structure

```
littleclaw/
├── cmd/littleclaw/
│   └── main.go                  # CLI entry point (configure / reset / run)
├── pkg/
│   ├── agent/
│   │   ├── loop.go              # NanoCore ReAct loop, system prompt builder,
│   │   │                        #   memory tools, cron tools
│   │   ├── heartbeat.go         # Background consolidation (5-min ticker)
│   │   ├── cron.go              # Cron scheduler with persistence & run logs
│   │   └── workspace_tools.go   # Workspace management tools
│   ├── memory/
│   │   └── memory.go            # Multi-tier memory system (5 tiers)
│   ├── tools/
│   │   ├── registry.go          # Tool registry, core tools, path protection
│   │   └── web.go               # web_fetch and web_search tools
│   ├── providers/
│   │   ├── types.go             # Provider, Message, ToolDefinition interfaces
│   │   ├── openai_provider.go   # OpenAI-compatible chat completions provider
│   │   ├── transcription.go     # TranscriptionProvider interface
│   │   ├── groq_transcription.go
│   │   ├── openai_transcription.go
│   │   └── whisper_cli_transcription.go
│   ├── bus/
│   │   └── bus.go               # Channel-based message bus
│   ├── channels/telegram/
│   │   └── telegram.go          # Telegram bot (polling, voice, photos, files)
│   ├── workspace/
│   │   └── workspace.go         # Structured workspace with folder tracking
│   └── config/
│       └── config.go            # JSON config management (~/.littleclaw/config.json)
├── AGENTS.md                    # Agent architecture reference
├── ARCHITECTURE.md              # This file
├── CONTRIBUTING.md              # Contributor guide
└── Makefile                     # Build targets
```

## Startup Sequence

`cmd/littleclaw/main.go` orchestrates the full boot:

```
1. Load config           config.LoadConfig()
2. Create LLM provider   providers.NewOpenAIProvider()
3. Create message bus     bus.NewMessageBus(100)
4. Create NanoCore        agent.NewNanoCore()
     └─ memory.NewStore()
     └─ workspace.NewManager()
     └─ agent.NewCronService()
     └─ tools.NewRegistry()
     └─ registerMemoryTools()
     └─ registerCronTools()
     └─ registerWorkspaceTools()
5. Start Telegram         telegram.NewBot() + bot.Start()
6. Start heartbeat        go nc.StartHeartbeat(ctx)
7. Start cron             cronSvc.Start()
8. Enter main loop        for msg := range msgBus.Inbound { ... }
```

## Component Diagram

```
┌──────────────────────────────────────────────────────────┐
│                      main.go                             │
│                    (main loop)                           │
│                                                          │
│    ┌─────────┐     ┌──────────┐     ┌─────────────┐     │
│    │ Telegram ├────>│ Message  ├────>│  NanoCore   │     │
│    │   Bot    │<────┤   Bus    │<────┤ (ReAct Loop)│     │
│    └─────────┘     └────┬─────┘     └──────┬──────┘     │
│                         │                  │             │
│                   ┌─────┴───┐      ┌───────┴───────┐    │
│                   │  Cron   │      │ Tool Registry  │    │
│                   │ Service │      │  (26 tools)    │    │
│                   └─────────┘      └───────┬───────┘    │
│                                            │             │
│             ┌──────────────┬───────────────┤             │
│             │              │               │             │
│       ┌─────┴─────┐ ┌─────┴─────┐ ┌───────┴──────┐     │
│       │  Memory   │ │ Workspace │ │   Web Tools  │     │
│       │  Store    │ │  Manager  │ │ (fetch/search)│     │
│       └───────────┘ └───────────┘ └──────────────┘     │
└──────────────────────────────────────────────────────────┘
```

## Data Flow

### User Message Flow

```
User types in Telegram
  → Telegram Bot receives update (long polling)
  → Bot creates InboundMessage {Text, ChatID, Channel}
  → Message sent to MessageBus.Inbound channel
  → main loop reads from channel
  → NanoCore.RunAgentLoop(ctx, text)
      1. Build system prompt (identity + memory + entities + history + cron)
      2. Send to LLM provider via Chat()
      3. If tool_calls in response:
           For each tool_call:
             Look up handler in registry
             Execute handler(ctx, args) → ToolResult
             Append result to messages
           Re-send to LLM (repeat, max 10 iterations)
      4. Final text response extracted
  → Response sent to MessageBus.Outbound channel
  → Telegram Bot sends reply to user
```

### Cron Job Flow

```
CronService tick fires
  → Job prompt injected into MessageBus.Inbound
  → Processed by same main loop / ReAct loop as user messages
  → Run result logged to cron/runs/<jobID>.jsonl
  → Response sent via MessageBus.Outbound → Telegram
```

### Heartbeat Flow

```
Every 5 minutes (background goroutine):
  1. Check dirty flag → skip if clean
  2. Append consolidation notes to INTERNAL.md
  3. If today's daily log > 8KB → generate summary
  4. If pre-compaction threshold hit → early consolidation
  5. Clear dirty flag
```

## Memory Architecture

Five tiers of persistence, from hot to cold:

| Tier | Storage | Lifecycle | Access |
|------|---------|-----------|--------|
| 1. Daily Logs | `memory/YYYY-MM-DD.md` | Created daily, summarized when > 8KB | System prompt (today + yesterday) |
| 2. Core Memory | `memory/MEMORY.md` | Permanent, section-based, versioned | System prompt (every call) |
| 3. Entities | `memory/ENTITIES/*.md` | Permanent, per-topic | Auto-surfaced by trigram match |
| 4. Summaries | `memory/summaries/*_summary.md` | Generated from daily logs | Available via `search_history` |
| 5. Internal Log | `memory/INTERNAL.md` | Rotates at 1MB | Via `read_internal_log` (4KB cap) |

## LLM Provider

All providers implement the `Provider` interface from `pkg/providers/types.go`:

```go
type Provider interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
}
```

Only the OpenAI-compatible implementation exists (`openai_provider.go`). It
works with OpenAI, OpenRouter, and Ollama by varying the base URL and API key.

## Transcription Providers

Three implementations of the `TranscriptionProvider` interface:

| Provider | File | Requires |
|----------|------|----------|
| Groq | `groq_transcription.go` | Groq API key |
| OpenAI Whisper | `openai_transcription.go` | OpenAI API key |
| Local Whisper | `whisper_cli_transcription.go` | `whisper` CLI installed |

The Telegram bot uses the configured transcription provider to convert voice
messages to text before passing them to the agent.

## Configuration

Stored at `~/.littleclaw/config.json` (file permissions: 0600).

Fields: Telegram bot token, allowed user ID, LLM provider type/URL/key/model,
transcription provider type/key, Tavily API key.

Managed via the interactive `littleclaw configure` wizard using `promptui`.

## Concurrency Model

- **Single main goroutine** processes messages sequentially from the bus.
- **Telegram bot** runs in its own goroutine (long polling).
- **Heartbeat** runs in its own goroutine (5-min ticker).
- **Cron service** runs in its own goroutine (per the `robfig/cron` library).
- **Memory store** uses `sync.RWMutex` for concurrent access safety.
- **NanoCore** uses `sync.Mutex` on `chatMu` to protect last chat ID/channel.
- **Dirty flag** uses `atomic.Bool` for lock-free coordination between the
  main loop and heartbeat.
