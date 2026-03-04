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

Littleclaw is a hyper-personalized, context-aware AI running on a deterministically scheduled core. It utilizes a ReAct pattern nano-agent, event-driven message bus, and deep integration with the local OS to effectively become an autonomous digital entity.

### ✨ Key Features

- **Multi-layered Memory Architecture**: Seamlessly reads and updates `CRON.json`, `HISTORY.md`, `MEMORY.md`, and dynamic entities so it never forgets context across sessions.
- **Dynamic Skills & Tools**: Sandbox-integrated tool loader that dynamically loads shell scripts, Python, or Go binaries from the `skills/` directory at runtime.
- **Deterministic Background Cron**: Unbreakable schedule daemon replacing volatile swarm logic. Background loops run on `@every` definitions with robust state serialization.
- **Local & Cloud Provider Support**: Use large models via OpenAI/OpenRouter, or seamlessly switch to a local Ollama service for 100% offline edge execution.
- **Event-Driven Bus**: Native bidirectional bus, fully integrated with Telegram and local processing nodes for instantaneous background task notification.

### 🚀 Quick Start

#### Requirements
- Go 1.21+
- [Ollama](https://ollama.ai/) (optional for local ML models)

#### Installation

**Fastest Way (Linux / macOS)**
Run the automated installation script to clone, build, and install Littleclaw instantly:
```bash
curl -sSL https://raw.githubusercontent.com/hereisswapnil/littleclaw/main/install.sh | bash
```

**Manual Approach**
If you prefer to install manually, clone the repository, build, and configure the project.

```bash
# Build the binary
go build -o bin/littleclaw ./cmd/littleclaw/...
```

#### First Time Setup
Run the interactive setup configuration to initialize settings:
```bash
littleclaw configure
```

You'll be guided through selecting your preferred LLM provider, entering API keys (or picking local Ollama), and setting up your Telegram communication endpoints.

#### Usage

To boot up the Littleclaw Heartbeat and Cron Daemon:
```bash
./bin/littleclaw
```

Once running, send a message to your configured Telegram bot to start interacting. 

### 🧹 Advanced Settings
If you want to clear Littleclaw's memory and history completely to start fresh:
```bash
./bin/littleclaw reset
```

### 📜 License
This project is licensed under the [MIT License](LICENSE).

---

_Littleclaw is an evolving project. It is always learning, listening, and consolidating its knowledge._