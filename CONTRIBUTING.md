# Contributing to LittleClaw

## Prerequisites

- Go 1.25+
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- At least one LLM provider configured (OpenAI, OpenRouter, or Ollama)

## Building

```bash
# Clone the repository
git clone https://github.com/hereisswapnil/littleclaw.git
cd littleclaw

# Build
go build -o bin/littleclaw ./cmd/littleclaw/...

# Or use the Makefile
make build
```

## Running

```bash
# First-time setup (interactive wizard)
./bin/littleclaw configure

# Start the agent
./bin/littleclaw

# Reset all data (memory, entities, workspace)
./bin/littleclaw reset
```

## Project Layout

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full package structure and
component diagram.

Key packages:

| Package | What it does |
|---------|-------------|
| `cmd/littleclaw` | CLI entry point |
| `pkg/agent` | ReAct loop, heartbeat, cron, workspace tools |
| `pkg/memory` | Multi-tier memory system |
| `pkg/tools` | Tool registry, core tools, web tools |
| `pkg/providers` | LLM and transcription providers |
| `pkg/bus` | Message bus |
| `pkg/channels/telegram` | Telegram bot integration |
| `pkg/workspace` | Structured workspace management |
| `pkg/config` | Configuration management |

## Adding a New Tool

1. Decide where it belongs:
   - **Core/file/web tool** -> `pkg/tools/registry.go`
   - **Memory-related tool** -> `pkg/agent/loop.go` (`registerMemoryTools`)
   - **Cron-related tool** -> `pkg/agent/loop.go` (`registerCronTools`)
   - **Workspace tool** -> `pkg/agent/workspace_tools.go` (`registerWorkspaceTools`)

2. Define the tool schema:
   ```go
   providers.ToolDefinition{
       Name:        "my_tool",
       Description: "What this tool does",
       Parameters: map[string]interface{}{
           "type": "object",
           "properties": map[string]interface{}{
               "param1": map[string]interface{}{
                   "type":        "string",
                   "description": "What param1 is for",
               },
           },
           "required": []string{"param1"},
       },
   }
   ```

3. Write the handler:
   ```go
   func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
       param1, _ := args["param1"].(string)
       // ... do work ...
       return &tools.ToolResult{ForLLM: "result text"}
   }
   ```

4. Register both in the appropriate `register*` function using
   `nc.toolRegistry.Register(definition, handler)`.

5. If accessing files, use `resolveAndProtectPath` to enforce workspace
   boundaries.

6. If writing to memory, call `s.dirty.Store(true)` so the heartbeat picks
   up the change.

## Code Style

- Standard `gofmt` formatting.
- No external linter is configured; `go vet` is the minimum bar.
- Error messages should be lowercase and not end with punctuation (Go convention).
- Tool handlers return `*tools.ToolResult`, never raw errors. Wrap errors into
  `ForLLM` so the LLM can reason about failures.

## Testing

There are currently no tests in the project. If you add a feature, consider
adding `*_test.go` files alongside the code.

```bash
# Run all tests
go test ./...

# Or use the Makefile
make test
```

## Checks Before Submitting

```bash
make vet      # go vet ./...
make build    # ensure it compiles
make test     # run tests (if any)
```
