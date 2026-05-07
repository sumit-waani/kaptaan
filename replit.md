# Kaptaan — Autonomous CTO Agent

## Project Overview

Kaptaan is an autonomous CTO agent for non-technical founders. It connects to a Telegram bot and acts as a senior engineer — ingesting project documentation, asking clarifying questions, generating phased implementation plans, writing Go code, running tests, and opening GitHub PRs.

### Architecture

- **Language:** Go 1.25
- **Bot Interface:** Telegram Bot API (via `github.com/go-telegram-bot-api/telegram-bot-api/v5`)
- **Database:** PostgreSQL (via `github.com/jackc/pgx/v5`)
- **LLM Providers:** DeepSeek, NVIDIA NIM (with automatic failover)
- **Entry point:** `cmd/kaptaan/main.go`

### Package Layout

```
cmd/kaptaan/       — main entrypoint
internal/agent/    — agent state machine (ingest, plan, execute, replan)
internal/bot/      — Telegram bot handler and command routing
internal/db/       — PostgreSQL schema + queries
internal/llm/      — LLM provider pool with failover
internal/tools/    — Shell, file, and GitHub CLI executor
```

### Agent Lifecycle

1. **Ingesting** — founder uploads `.md` docs via Telegram
2. **Clarifying** — agent asks LLM-generated questions to build trust score
3. **Planning** — LLM generates phased implementation plan, founder approves each task
4. **Executing** — agent runs agentic loop: writes code, runs `go build`, runs tests, opens PRs
5. **Replanning** — scans repo for gaps/bugs, suggests improvements

### Required Environment Variables

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | From @BotFather on Telegram |
| `DATABASE_URL` | PostgreSQL connection string (set automatically by Replit) |
| `DEEPSEEK_API_KEY` | DeepSeek API key (at least one LLM key required) |
| `NIM_API_KEY_1` | NVIDIA NIM API key (optional, used first as free tier) |
| `NIM_API_KEY_2` | Second NVIDIA NIM API key (optional fallback) |
| `GITHUB_REPO` | Target repo, e.g. `myorg/myrepo` |
| `GITHUB_TOKEN` | GitHub personal access token with repo + PR permissions |
| `WORKSPACE_DIR` | Local clone directory (default: `/tmp/kaptaan-workspace`) |

### Running

The app requires `TELEGRAM_BOT_TOKEN` and at least one LLM API key to start. It will fail fast if these are missing.

## User Preferences

- Idiomatic Go code with proper error handling and context propagation
- Table-driven tests
- One PR per task phase
