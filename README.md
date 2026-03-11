# 🦀 Littleclaw

<p align="center">
  <img src="assets/logo.png" width="300" alt="Littleclaw Logo">
</p>

<p align="center">
  <img src="https://img.shields.io/github/repo-size/hereisswapnil/littleclaw?style=flat-square" alt="Repo Size">
  <img src="https://img.shields.io/badge/Status-Active-success.svg?style=flat-square" alt="Status: Active">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License: MIT"></a>
</p>

## The Lightweight, Hyper-Personalized AI Nano-Agent

Littleclaw is a hyper-personalized, context-aware AI running on a deterministically scheduled core. It uses a ReAct-pattern nano-agent, event-driven message bus, and deep integration with the local OS to effectively become an autonomous digital entity — controlled entirely through Telegram.

### ✨ Key Features

- **Multi-layered Memory Architecture** — Persistent `MEMORY.md` for core facts, daily conversation logs (`YYYY-MM-DD.md`) with auto-summarization, `INTERNAL.md` for background reasoning, and per-entity knowledge files with trigram-based auto-surfacing. Auto-consolidates context via a background heartbeat.
- **Workspace Identity Files** — `SOUL.md`, `IDENTITY.md`, and `USER.md` scaffolded automatically on first boot. The agent reads these on every call, giving it a persistent personality and knowledge of the user across restarts.
- **Cron with Full Run History** — Schedule recurring tasks with `@every` expressions or cron syntax. Every run is logged to `cron/runs/<jobID>.jsonl` with status (`ok`/`error`), duration, next-run time, and consecutive error count — mirroring how openclaw tracks jobs.
- **Web Search & Fetch** — Built-in `web_search` (Tavily primary → DuckDuckGo fallback, no key needed) and `web_fetch` (reads any URL) tools for real-time internet access. No `curl` hacks required.
- **Dynamic Skills** — Drop `.sh` or `.py` scripts into the `skills/` directory and they become callable tools instantly (use `reload_skills` to hot-reload).
- **Local & Cloud LLMs** — OpenAI, OpenRouter, or a fully offline Ollama instance. Switch via `littleclaw configure`.
- **Voice Messages** — Transcribe Telegram voice notes via Groq, OpenAI Whisper, or a local Whisper CLI.

### 🚀 Quick Start

#### Requirements
- Go 1.25+
- [Ollama](https://ollama.ai/) (optional, for local/offline models)

#### Installation

**One-liner (Linux / macOS)**
```bash
curl -sSL https://raw.githubusercontent.com/hereisswapnil/littleclaw/main/install.sh | bash
```

**Manual build**
```bash
git clone https://github.com/hereisswapnil/littleclaw.git
cd littleclaw
go build -o bin/littleclaw ./cmd/littleclaw/...
```

#### First-time Setup

```bash
littleclaw configure
```

The interactive wizard walks you through:
- Telegram bot token and allowed user ID
- LLM provider (OpenAI / OpenRouter / Ollama) and model name
- Transcription provider (Groq / OpenAI Whisper / local Whisper CLI / none)
- Tavily API key for web search (optional — DuckDuckGo is used automatically if omitted)

#### Running

```bash
./bin/littleclaw
```

Then message your Telegram bot to start. The agent boots with cron scheduling, background memory consolidation (heartbeat), and live web access ready to go.

### 💬 Example Prompts

- *"Remind me to drink water every hour"*
- *"Remember that I'm allergic to peanuts"*
- *"Search the web for the latest Go 1.25 release notes"*
- *"Fetch this URL and summarize it: https://..."*
- *"Show me all my scheduled tasks"*
- *"Create a Python script that does X and run it every day at 9 AM"*

### 🧹 Reset

To wipe all memory, history, entities, and workspace files and start fresh:
```bash
./bin/littleclaw reset
```

### 📁 Workspace Layout

After first boot, `~/.littleclaw/workspace/` contains:

```
workspace/
├── SOUL.md            # Agent personality & behavioral rules
├── IDENTITY.md        # Agent name, capabilities, purpose
├── USER.md            # What the agent knows about you (grows over time)
├── HEARTBEAT.md       # Last-active timestamp (updated every loop)
├── CRON.json          # Scheduled jobs with state (lastRun, nextRun, status)
├── cron/runs/         # Per-job JSONL run logs
├── INDEX.json         # Workspace folder index
├── memory/
│   ├── MEMORY.md      # Core long-term facts (versioned backups kept)
│   ├── INTERNAL.md    # Background reasoning log (rotates at 1 MB)
│   ├── YYYY-MM-DD.md  # Daily conversation logs (one per day)
│   ├── ENTITIES/      # Deep knowledge files per person/project/topic
│   └── summaries/     # Auto-generated daily summaries (when logs > 8 KB)
└── skills/            # Drop .sh or .py scripts here to add new tools
```

### 📜 License

[MIT](LICENSE)

### 📖 Documentation

- [AGENTS.md](AGENTS.md) -- How the agent works (for contributors and AI assistants)
- [ARCHITECTURE.md](ARCHITECTURE.md) -- System architecture and package structure
- [CONTRIBUTING.md](CONTRIBUTING.md) -- How to build, test, and contribute

---

_Littleclaw is an evolving project. It is always learning, listening, and consolidating its knowledge._