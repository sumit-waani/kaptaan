# Kaptaan

An autonomous coding agent web application built in Go. It integrates with DeepSeek LLM and E2B sandbox to perform software engineering tasks autonomously via a real-time chat UI.

## Tech Stack

- **Backend**: Go (standard library + SQLite via `modernc.org/sqlite`)
- **Frontend**: Vanilla HTML/CSS/JS with Alpine.js (embedded in the Go binary)
- **LLM**: DeepSeek API (DeepSeek-V4-Pro)
- **Sandbox**: E2B for isolated code execution
- **Auth**: Session-based with bcrypt password hashing

## Project Structure

- `main.go` - Entry point: wires up DB, LLM pool, agent, and web server
- `internal/agent/` - Core autonomous agent logic (state machine, tool dispatch)
- `internal/db/` - SQLite database layer (conversations, memories, config)
- `internal/llm/` - DeepSeek API client with streaming and retry logic
- `internal/sandbox/` - E2B sandbox REST integration
- `internal/tools/` - Tool implementations the agent can call
- `internal/web/` - HTTP server, SSE hub, auth, and embedded static assets

## Running

The app runs on port 5000. Configure API keys in the Settings panel of the UI after first login.

## Configuration (stored in SQLite, settable via UI)

- `deepseek_api_key` - DeepSeek API key
- `deepseek_model` - Model name (default: deepseek-v4-pro)
- `e2b_api_key` - E2B sandbox API key
- `repo_url` - GitHub repository URL for the agent to work on
- `github_token` - GitHub personal access token

## User Preferences

- Port 5000 for the web server
- Configuration managed through the UI (not env vars)
