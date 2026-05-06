# CTO Agent — Architecture & Design Document
> Version 1.0 | For Founder Approval Before Implementation

---

## 1. Overview

An autonomous CTO agent that ingests project documentation, analyzes a codebase, plans implementation in phases, builds/tests/verifies each phase, and iterates — all over Telegram. Runs in an ephemeral container with Postgres (Neon) for persistence.

---

## 2. Lifecycle (Big Picture)

```
STARTUP
  └─► Load env → Connect Postgres → Init schema
  └─► Check: is project registered?
        ├─ NO  → Wait for doc upload → INGEST phase
        └─ YES → Resume from last known state (checkpoint)

INGEST PHASE
  └─► Parse markdown docs → chunk → tag → store in project_docs
  └─► Clone/pull repo → scan file tree → assess coverage
  └─► Calculate Trust Score
        ├─ < 95% → Ask clarifying questions (one by one, approval loop)
        └─ ≥ 95% → Move to PLAN phase

PLAN PHASE
  └─► Generate Phases → Tasks → Subtasks
  └─► Present to founder one by one for approval
  └─► Store approved plan in DB

EXECUTE PHASE (per task)
  └─► Load context (RAG: relevant docs + task history)
  └─► Execute subtasks (shell, write_file, github_op tools)
  └─► After every .go file: go build ./...  (max 3 retries)
  └─► On task complete: go test ./... (target 85-95% coverage)
  └─► Verify → Open PR
  └─► Next task

REPLAN PHASE (when plan exhausted)
  └─► Deep repo scan → identify bugs, missing features, gaps vs docs
  └─► Generate proactive suggestions
  └─► Present suggestions one by one for approval
  └─► Approved items → new tasks → back to EXECUTE

```

---

## 3. Database Schema (Postgres / Neon)

```sql
-- Project (single project, strictly one row)
CREATE TABLE project (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'new',
    -- status: new | ingesting | planning | executing | replanning | paused | done
    trust_score FLOAT DEFAULT 0,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Raw docs uploaded by founder
CREATE TABLE project_docs (
    id          SERIAL PRIMARY KEY,
    filename    TEXT NOT NULL,
    raw_content TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Chunked, tagged doc pieces for RAG context
CREATE TABLE doc_chunks (
    id          SERIAL PRIMARY KEY,
    doc_id      INTEGER REFERENCES project_docs(id),
    chunk_text  TEXT NOT NULL,
    tags        TEXT[],         -- e.g. {feature, api, schema, rule, ui}
    relevance   TEXT,           -- short label: "auth flow", "data model" etc
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Plans (versioned — each replan = new version)
CREATE TABLE plans (
    id          SERIAL PRIMARY KEY,
    version     INTEGER NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'active',
    -- status: active | exhausted | replaced
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Tasks (phases and their tasks)
CREATE TABLE tasks (
    id          SERIAL PRIMARY KEY,
    plan_id     INTEGER REFERENCES plans(id),
    parent_id   INTEGER REFERENCES tasks(id),  -- NULL = phase, set = subtask
    phase       INTEGER NOT NULL DEFAULT 1,
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'pending',
    -- status: pending | approved | in_progress | done | failed | skipped
    is_suggestion BOOLEAN DEFAULT FALSE,
    pr_url      TEXT,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Execution log per task/subtask
CREATE TABLE task_log (
    id          SERIAL PRIMARY KEY,
    task_id     INTEGER REFERENCES tasks(id),
    event       TEXT NOT NULL,  -- tool_call | build_result | test_result | llm_msg | error
    payload     TEXT,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Clarifying Q&A (used for trust score + context)
CREATE TABLE clarifications (
    id          SERIAL PRIMARY KEY,
    question    TEXT NOT NULL,
    answer      TEXT,
    answered_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Suggestions awaiting approval
CREATE TABLE suggestions (
    id          SERIAL PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT,
    task_plan   TEXT,           -- full proposed task breakdown as JSON
    status      TEXT DEFAULT 'pending',
    -- status: pending | approved | rejected
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Persistent key-value (checkpoint, state machine, counters)
CREATE TABLE kv (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Chat messages (clearable via /clear)
CREATE TABLE messages (
    id          SERIAL PRIMARY KEY,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- LLM token usage tracking
CREATE TABLE llm_usage (
    id              SERIAL PRIMARY KEY,
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    prompt_tokens   INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_tokens    INTEGER DEFAULT 0,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
```

---

## 4. Trust Score Calculation

Trust score = weighted sum of signals. Must reach **95%** before build phase starts.

| Signal | Weight | How Measured |
|---|---|---|
| Doc coverage | 30% | % of required sections found in docs (features, API, schema, rules) |
| Clarifications answered | 25% | answered / total questions asked |
| Repo scan complete | 15% | file tree + go module parsed successfully |
| Architecture pattern match | 20% | known Go patterns detected (handlers, models, migrations etc) |
| Ambiguity score | 10% | inverse — fewer open ambiguities = higher score |

Agent calculates this after each clarifying Q&A round and after repo scan. Shows founder current score transparently.

---

## 5. State Machine

```
new → ingesting → clarifying → planning → executing → replanning
                                    ↑                       |
                                    └───────────────────────┘
Any state → paused (via /pause)
paused → previous state (via /resume)
```

Stored in `kv` table as `agent_state`. On startup, reads state and resumes.

---

## 6. LLM Pool

**Providers (in priority order):**
```
1. DeepSeek v4-pro    → https://api.deepseek.com/v1/chat/completions
2. DeepSeek v4-flash  → https://api.deepseek.com/v1/chat/completions  
3. NIM deepseek-v4-pro → https://integrate.api.nvidia.com/v1/chat/completions
4. NIM z-ai/glm-5.1   → https://integrate.api.nvidia.com/v1/chat/completions
```

**Failover rules:**
- 429 / rate limit → cooldown 1 hour, try next
- 401 / 403 auth → cooldown 24 hours
- Timeout (>120s) → try next immediately
- All down → notify founder, set state=paused, retry every 15 min

**Token tracking:** Every response logs prompt/completion tokens to `llm_usage` table.

---

## 7. Tools Available to Agent

| Tool | Purpose |
|---|---|
| `shell` | Run bash in workspace (go build, go test, git etc) |
| `write_file` | Write file to workspace (heredoc-safe, base64 encoded) |
| `read_file` | Read file from workspace |
| `github_op` | clone, push, pr_create, pr_list, status |
| `ask_founder` | Ask clarifying question — blocks until answered |
| `search_docs` | RAG: fetch relevant doc chunks by topic/tag |
| `update_task_status` | Mark task in_progress/done/failed |
| `log_event` | Write to task_log for audit trail |

---

## 8. Context Window Strategy (Dynamic RAG)

Each LLM call gets a trimmed context packet:

```
[system prompt]
[project summary — name, status, trust score]
[active plan summary — current phase, current task, subtasks]  
[relevant doc chunks — fetched by task topic, max 3000 tokens]
[recent task_log — last 10 events]
[last N messages — sliding window, max 4000 tokens]
```

Total target: stay under 20k tokens per call. Prioritize: task_log > doc_chunks > messages.

---

## 9. Telegram Commands

| Command | Behavior |
|---|---|
| `/pause` | Pause after current tool call completes. Save checkpoint. |
| `/resume` | Resume from checkpoint. |
| `/status` | Current state, active task, trust score, plan progress. |
| `/tasks` | List current plan phases + task statuses. |
| `/usage` | Token usage by provider + model. Total + today. |
| `/clear` | Clear chat message history (tasks/plans NOT affected). |
| `/replan` | Force trigger a replan scan immediately. |
| `/score` | Show trust score breakdown. |
| `/log` | Last 10 task_log entries. |
| `/help` | All commands. |

---

## 10. Document Ingestion Flow (Telegram Upload)

```
Founder sends .md file via Telegram
  └─► Bot receives document
  └─► Download file content
  └─► Store raw in project_docs
  └─► Chunker splits by heading (H2/H3 boundaries)
  └─► LLM tags each chunk: [feature|api|schema|rule|ui|data|config|other]
  └─► Store chunks in doc_chunks with tags
  └─► Recalculate trust score
  └─► Report to founder: "Ingested X chunks, Y tags. Trust score: Z%"
```

Multiple docs can be uploaded. Each one is processed and added.

---

## 11. Approval Flow (One by One)

Used for: clarifying questions, plan tasks, suggestions.

```
Agent sends: "❓ Question / 📋 Task plan / 💡 Suggestion"
  └─► Waits for founder reply
  └─► Founder replies: yes/no or free text answer
  └─► Agent processes and moves to next item
  └─► Loop until all items addressed
```

For task/suggestion approvals specifically:
```
Agent: "📋 Task 2/5: Implement JWT auth middleware
        Subtasks:
        1. Write middleware/auth.go
        2. Write tests (auth_test.go)  
        3. Wire into router
        Approve? (yes/no)"

Founder: "yes"
Agent: → marks task approved → moves to next
```

---

## 12. Build & Test Rules

- After every `.go` file written: `go build ./...`
- Max 3 build retries per file. 3rd failure = stop + notify founder
- After each task complete: `go test ./... -cover`
- Target: **85-95% test coverage**
- If coverage < 85%: agent writes more tests before moving to PR
- PR created only when: build passes + tests pass + coverage target met

---

## 13. Replan Cycle

```
Plan exhausted → REPLAN triggered
  └─► git pull (get latest)
  └─► Full file tree scan
  └─► go build ./... (health check)
  └─► go test ./... (coverage check)
  └─► LLM analyzes: gaps vs docs, known bugs, missing features
  └─► Generate suggestion list
  └─► Present each suggestion to founder (one by one, yes/no)
  └─► Approved suggestions → new tasks in new plan version
  └─► Back to EXECUTE phase
```

---

## 14. Crash Recovery / Resume

All critical state in Postgres:
- `kv.agent_state` — current state machine position
- `kv.current_task_id` — which task was in progress
- `kv.current_subtask_index` — which subtask
- `kv.checkpoint_messages` — last N messages for LLM context rebuild

On startup:
```
Read agent_state from kv
  ├─ executing → resume current task from checkpoint
  ├─ paused    → wait for /resume
  ├─ clarifying → re-ask the unanswered question
  └─ planning  → re-present the next unapproved task
```

`/resume` does the same thing explicitly.

---

## 15. File Structure (Go Project)

```
cmd/
  agent/
    main.go          ← entrypoint, Telegram bot loop
internal/
  agent/
    agent.go         ← core agent loop, state machine
    planner.go       ← plan generation, trust score
    executor.go      ← tool execution, build/test runner
    context.go       ← dynamic context builder (RAG)
    ingest.go        ← doc parsing, chunking, tagging
  db/
    db.go            ← postgres connection, queries
    schema.go        ← schema init
  llm/
    pool.go          ← provider pool, failover, usage tracking
    types.go         ← LLMMessage, ToolCall, etc
  bot/
    bot.go           ← telegram handler, commands
    approval.go      ← approval flow helpers
  tools/
    tools.go         ← tool definitions
    shell.go         ← shell executor
    github.go        ← github operations
go.mod
go.sum
.env.example
Dockerfile
```

---

## 16. Environment Variables

```env
TELEGRAM_TOKEN=
DEEPSEEK_API_KEY=
NIM_API_KEY_1=
NIM_API_KEY_2=
DATABASE_URL=          # Neon postgres connection string
GITHUB_TOKEN=
GITHUB_REPO=           # owner/repo
WORKSPACE_DIR=workspace
```

---

## 17. What Is NOT in Scope (v1)

- Multi-project support
- Streaming LLM responses
- Web dashboard
- Non-Go projects
- Non-Markdown docs
- Multiple founders / auth

---

*This document is the source of truth for implementation. Agent will be constrained to build only what's described here.*
