// Package agent is the single autonomous coding agent that powers Kaptaan.
// Single project, driven by REPO_URL and GITHUB_TOKEN env vars.
// Conversation is persisted to SQLite. Scratchpad.md in the sandbox is the
// working memory for tasks. GitHub is the source of truth for PR/merge state.
package agent

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "log"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/sandbox"
        "github.com/cto-agent/cto-agent/internal/tools"
)

// fixedProjectID is used as the key for memories, conversations, and sandbox
// state. There is only one project now — config comes from env vars.
const fixedProjectID = 1

// Hooks are the small set of UI callbacks the agent invokes.
type Hooks struct {
        Send           func(projectID int, text string)
        Ask            func(projectID int, question string) string
        NotifyState    func(projectID int)
        Token          func(projectID int, token string)
        CancelStream   func(projectID int)
        FinalizeStream func(projectID int)
}

// projectSandbox is the live E2B sandbox, shared across all turns until
// the agent explicitly calls reset_sandbox.
type projectSandbox struct {
        runtime tools.Runtime
        branch  string
}

// queuedMsg holds a message waiting to be processed after the current task.
type queuedMsg struct {
        projectID int
        text      string
}

// Agent owns conversation state and serialises calls.
type Agent struct {
        db     *db.DB
        pool   *llm.Pool
        hooks  Hooks
        e2bKey string

        mu      sync.Mutex
        running map[int]bool
        cancels map[int]context.CancelFunc
        queue   chan queuedMsg // depth-1 queue for messages arriving while busy

        sbMu      sync.Mutex
        sandboxes map[int]*projectSandbox
}

func New(database *db.DB, pool *llm.Pool, e2bKey string, hooks Hooks) *Agent {
        return &Agent{
                db:        database,
                pool:      pool,
                hooks:     hooks,
                e2bKey:    e2bKey,
                running:   make(map[int]bool),
                cancels:   make(map[int]context.CancelFunc),
                queue:     make(chan queuedMsg, 1),
                sandboxes: make(map[int]*projectSandbox),
        }
}

func (a *Agent) IsRunning(projectID int) bool {
        a.mu.Lock()
        defer a.mu.Unlock()
        return a.running[projectID]
}

// HasQueued reports whether a message is sitting in the queue for this project.
func (a *Agent) HasQueued(projectID int) bool {
        return len(a.queue) > 0
}

func (a *Agent) ResetConversation(projectID int) {
        if err := a.db.ClearMessages(context.Background(), projectID); err != nil {
                log.Printf("[agent] ClearMessages: %v", err)
        }
}

// CancelTask cancels the running task for projectID. Returns false if nothing
// was running.
func (a *Agent) CancelTask(projectID int) bool {
        a.mu.Lock()
        fn := a.cancels[projectID]
        a.mu.Unlock()
        if fn == nil {
                return false
        }
        fn()
        return true
}

// HandleUserMessage processes one user turn end-to-end. Blocking.
// If the agent is already running, the message is queued (depth 1) and
// processed immediately after the current task finishes.
func (a *Agent) HandleUserMessage(ctx context.Context, projectID int, text string) error {
        a.mu.Lock()
        if a.running[projectID] {
                a.mu.Unlock()
                select {
                case a.queue <- queuedMsg{projectID: projectID, text: text}:
                        a.hooks.Send(projectID, "📥 Queued — will process after current task.")
                        if a.hooks.NotifyState != nil {
                                a.hooks.NotifyState(projectID)
                        }
                        return nil
                default:
                        return errors.New("queue is full — please wait for the current task to finish")
                }
        }
        a.running[projectID] = true
        a.mu.Unlock()

        return a.runLoop(ctx, projectID, text)
}

// runLoop processes text, then drains the queue before releasing the running flag.
func (a *Agent) runLoop(ctx context.Context, projectID int, text string) error {
        taskCtx, cancel := context.WithCancel(ctx)
        a.mu.Lock()
        a.cancels[projectID] = cancel
        a.mu.Unlock()

        cancelled := false
        defer func() {
                cancel()
                a.mu.Lock()
                delete(a.cancels, projectID)
                a.mu.Unlock()

                if cancelled {
                        a.hooks.Send(projectID, "⛔ Task cancelled.")
                        t := newTurn(a, projectID)
                        a.commitTurn(projectID, t)
                }

                // Check queue before releasing the running flag.
                select {
                case next := <-a.queue:
                        if a.hooks.NotifyState != nil {
                                a.hooks.NotifyState(projectID)
                        }
                        // Stay running and process the queued message.
                        _ = a.runLoop(ctx, next.projectID, next.text)
                        return
                default:
                }
                a.mu.Lock()
                a.running[projectID] = false
                a.mu.Unlock()
                if a.hooks.NotifyState != nil {
                        a.hooks.NotifyState(projectID)
                }
        }()

        if a.hooks.NotifyState != nil {
                a.hooks.NotifyState(projectID)
        }

        turn := newTurn(a, projectID)
        turn.appendUser(text)

        const maxIterations = 200
        for i := 0; i < maxIterations; i++ {
                // Stream all LLM responses; cancel the stream visually if tool calls follow.
                resp, err := a.pool.ChatStream(taskCtx, turn.messages(), turn.toolDefs(), func(token string) {
                        if a.hooks.Token != nil {
                                a.hooks.Token(projectID, token)
                        }
                })
                if err != nil {
                        if errors.Is(err, context.Canceled) {
                                cancelled = true
                                if a.hooks.CancelStream != nil {
                                        a.hooks.CancelStream(projectID)
                                }
                                return nil
                        }
                        a.hooks.Send(projectID, "❌ LLM error: "+err.Error())
                        return err
                }
                choice := resp.Choices[0].Message
                turn.appendAssistant(choice)

                if len(choice.ToolCalls) == 0 {
                        // Final response — finalize the streaming bubble on the frontend.
                        if strings.TrimSpace(choice.Content) != "" {
                                if a.hooks.FinalizeStream != nil {
                                        a.hooks.FinalizeStream(projectID)
                                }
                        }
                        a.commitTurn(projectID, turn)
                        return nil
                }

                // Intermediate response with tool calls — discard any streamed content.
                if a.hooks.CancelStream != nil {
                        a.hooks.CancelStream(projectID)
                }

                if pre := strings.TrimSpace(choice.Content); pre != "" {
                        a.hooks.Send(projectID, pre)
                }

                // ── Parallel tool dispatch ────────────────────────────────────────────
                type toolResult struct {
                        id     string
                        output string
                }
                results := make([]toolResult, len(choice.ToolCalls))
                var wg sync.WaitGroup
                for idx, call := range choice.ToolCalls {
                        wg.Add(1)
                        go func(i int, c llm.ToolCall) {
                                defer wg.Done()
                                log.Printf("[tool] → %s %s", c.Function.Name, truncate(c.Function.Arguments, 200))
                                tStart := time.Now()
                                out := turn.dispatch(taskCtx, c)
                                log.Printf("[tool] ← %s %.1fs | %s", c.Function.Name, time.Since(tStart).Seconds(), truncate(out, 150))
                                results[i] = toolResult{id: c.ID, output: out}
                        }(idx, call)
                }
                wg.Wait()

                for _, r := range results {
                        turn.appendToolResult(r.id, r.output)
                }
        }
        a.hooks.Send(projectID, "⚠️ Reached the iteration limit for this turn. Send another message to continue.")
        a.commitTurn(projectID, turn)
        return nil
}

// commitTurn persists new messages from this turn to the DB.
// It writes turn.local[turn.baseLen:] — everything added during the current turn.
func (a *Agent) commitTurn(projectID int, t *turn) {
        ctx := context.Background()
        ts := time.Now().Format("15:04:05")
        newMsgs := t.local[t.baseLen:]

        // Apply a cap: if total stored would exceed 199, prune oldest from DB first.
        // (199 + system = 200 total sent to LLM)
        existing, _ := a.db.LoadConvo(ctx, projectID)
        total := len(existing) + len(newMsgs)
        if total > 199 {
                excess := total - 199
                // Delete the oldest `excess` LLM rows for this project.
                _ = a.db.PruneConvo(ctx, projectID, excess)
        }

        for _, m := range newMsgs {
                tcJSON := db.ToolCallsToJSON(m.ToolCalls)

                // Determine UI fields: only user and final assistant messages are visible.
                uiType, uiText, uiTs := "", "", ""
                if m.Role == "user" {
                        uiType = "user"
                        uiText = m.Content
                        uiTs = ts
                }
                // Final assistant messages are handled by FinalizeStream (server writes UI row).
                // Tool results and intermediate assistant messages have no UI row.

                if err := a.db.AppendMessage(ctx, projectID,
                        m.Role, m.Content, m.ReasoningContent, m.ToolCallID, tcJSON,
                        uiType, uiText, uiTs,
                ); err != nil {
                        log.Printf("[agent] AppendMessage: %v", err)
                }
        }
}

// loadConvo reads the persisted conversation from DB and prepends a fresh system prompt.
func (a *Agent) loadConvo(projectID int) []llm.Message {
        rows, err := a.db.LoadConvo(context.Background(), projectID)
        if err != nil {
                log.Printf("[agent] LoadConvo: %v", err)
        }
        msgs := make([]llm.Message, 0, len(rows)+1)
        // System prompt placeholder — will be replaced in newTurn.
        msgs = append(msgs, llm.Message{Role: "system", Content: ""})
        for _, r := range rows {
                m := llm.Message{
                        Role:             r.Role,
                        Content:          r.Content,
                        ReasoningContent: r.ReasoningContent,
                        ToolCallID:       r.ToolCallID,
                }
                if r.ToolCalls != "" {
                        _ = json.Unmarshal([]byte(r.ToolCalls), &m.ToolCalls)
                }
                msgs = append(msgs, m)
        }
        return msgs
}

// ─── Per-turn state ────────────────────────────────────────────────────────

type turn struct {
        a         *Agent
        projectID int
        local     []llm.Message
        baseLen   int // index into local where new messages start (for commitTurn)
}

func newTurn(a *Agent, projectID int) *turn {
        t := &turn{a: a, projectID: projectID}
        prior := a.loadConvo(projectID)
        if len(prior) == 0 {
                t.local = []llm.Message{{Role: "system", Content: t.systemPrompt()}}
        } else {
                prior[0] = llm.Message{Role: "system", Content: t.systemPrompt()}
                t.local = prior
        }
        t.baseLen = len(t.local)
        return t
}

func (t *turn) messages() []llm.Message { return t.local }

func (t *turn) appendUser(text string) {
        t.local = append(t.local, llm.Message{Role: "user", Content: text})
}

func (t *turn) appendAssistant(m llm.Message) {
        m.Role = "assistant"
        t.local = append(t.local, m)
}

func (t *turn) appendToolResult(id, output string) {
        if len(output) > 16000 {
                output = output[:16000] + "\n…[truncated]"
        }
        t.local = append(t.local, llm.Message{
                Role:       "tool",
                ToolCallID: id,
                Content:    output,
        })
}

// ensureSandbox returns the persistent sandbox. On first call it tries to
// resume a previously paused sandbox (ID stored in DB), falling back to
// creating a fresh one if none exists or the saved ID is stale.
func (a *Agent) ensureSandbox(ctx context.Context, projectID int) (tools.Runtime, error) {
        a.sbMu.Lock()
        ps := a.sandboxes[projectID]
        a.sbMu.Unlock()
        if ps != nil {
                // Quick health-check: if envd is unreachable (paused/killed by E2B in-session),
                // drop the stale entry and fall through to reconnect via DB.
                if sr, ok := ps.runtime.(*tools.SandboxRuntime); ok && sr.Sandbox.Ping(ctx) {
                        return ps.runtime, nil
                }
                log.Printf("[agent] in-memory sandbox is unreachable — reconnecting")
                a.sbMu.Lock()
                delete(a.sandboxes, projectID)
                a.sbMu.Unlock()
        }

        if a.e2bKey == "" {
                return nil, errors.New("E2B_API_KEY is not configured — sandbox tools are unavailable")
        }

        // Try to reconnect to a previously saved (paused) sandbox.
        if mem, err := a.db.GetMemory(ctx, projectID, "_sandbox_id"); err == nil && mem.Content != "" {
                a.hooks.Send(projectID, "🔄 reconnecting to saved sandbox…")
                sb, err := sandbox.Connect(ctx, a.e2bKey, mem.Content)
                if err == nil {
                        runtime := &tools.SandboxRuntime{
                                Sandbox: sb,
                                Cwd:     "/home/user/workspace",
                                Env: map[string]string{
                                        "HOME": "/home/user",
                                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                                },
                        }
                        // Sync with latest from origin/main on every reconnect.
                        if os.Getenv("GITHUB_TOKEN") != "" {
                                if r := runtime.Shell(ctx, "cd /home/user/workspace && git fetch origin && git merge origin/main --no-edit", 60); r.IsErr {
                                        log.Printf("[agent] git sync failed: %s", r.Output)
                                }
                        }
                        ps = &projectSandbox{runtime: runtime, branch: "kaptaan"}
                        a.sbMu.Lock()
                        a.sandboxes[projectID] = ps
                        a.sbMu.Unlock()
                        a.hooks.Send(projectID, "✅ sandbox reconnected, branch: `kaptaan`")
                        return runtime, nil
                }
                // Saved ID is stale — fall through to create a fresh sandbox.
                log.Printf("[agent] sandbox reconnect failed (%v) — creating new one", err)
                _ = a.db.DeleteMemory(ctx, projectID, "_sandbox_id")
        }

        a.hooks.Send(projectID, "🛠 spinning up sandbox…")
        sb, err := sandbox.Create(ctx, a.e2bKey, "base", 3600)
        if err != nil {
                return nil, fmt.Errorf("sandbox create: %w", err)
        }

        // Persist sandbox reference for future reconnects.
        sandboxRef := sb.ID + ":" + sb.ClientID
        if err := a.db.PutMemory(ctx, projectID, "_sandbox_id", sandboxRef); err != nil {
                log.Printf("[agent] failed to store sandbox id: %v", err)
        }

        runtime := &tools.SandboxRuntime{
                Sandbox: sb,
                Cwd:     "/home/user/workspace",
                Env: map[string]string{
                        "HOME": "/home/user",
                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                },
        }

        if r := runtime.Shell(ctx, "mkdir -p /home/user/workspace", 30); r.IsErr {
                log.Printf("[agent] mkdir workspace: %s", r.Output)
        }

        repoURL := normalizeRepoURL(os.Getenv("REPO_URL"))
        githubToken := os.Getenv("GITHUB_TOKEN")

        if repoURL != "" && githubToken != "" {
                cloneURL := injectToken(repoURL, githubToken)
                cmd := fmt.Sprintf(
                        "cd /home/user && rm -rf workspace && git clone %s workspace"+
                                " && cd workspace"+
                                " && git config user.email kaptaan@local"+
                                " && git config user.name Kaptaan"+
                                " && (git checkout kaptaan 2>/dev/null || git checkout -b kaptaan)",
                        shellQuote(cloneURL),
                )
                if r := runtime.Shell(ctx, cmd, 180); r.IsErr {
                        log.Printf("[agent] git clone failed: %s", r.Output)
                        a.hooks.Send(projectID, "⚠️ git clone failed:\n```\n"+truncate(r.Output, 800)+"\n```\nProceeding with empty workspace.")
                } else {
                        a.hooks.Send(projectID, "✅ repo cloned, branch: `kaptaan`")
                }
        }

        ps = &projectSandbox{runtime: runtime, branch: "kaptaan"}
        a.sbMu.Lock()
        a.sandboxes[projectID] = ps
        a.sbMu.Unlock()
        return runtime, nil
}

// ReadScratchpad reads scratchpad.md from the active sandbox.
// Used by the settings UI. Does not spin up a new sandbox if none exists.
func (a *Agent) ReadScratchpad(ctx context.Context, projectID int) (string, error) {
        a.sbMu.Lock()
        ps := a.sandboxes[projectID]
        a.sbMu.Unlock()

        if ps != nil {
                r := ps.runtime.ReadFile(ctx, "/home/user/workspace/scratchpad.md")
                if r.IsErr {
                        return "", fmt.Errorf("%s", r.Output)
                }
                return r.Output, nil
        }

        // No in-memory sandbox — try to reconnect to a paused one from DB.
        if a.e2bKey == "" {
                return "", fmt.Errorf("no active sandbox")
        }
        mem, err := a.db.GetMemory(ctx, projectID, "_sandbox_id")
        if err != nil || mem.Content == "" {
                return "", fmt.Errorf("no active sandbox — send a task first")
        }
        sb, err := sandbox.Connect(ctx, a.e2bKey, mem.Content)
        if err != nil {
                return "", fmt.Errorf("sandbox unavailable: %w", err)
        }
        rt := &tools.SandboxRuntime{
                Sandbox: sb,
                Cwd:     "/home/user/workspace",
                Env: map[string]string{
                        "HOME": "/home/user",
                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                },
        }
        a.sbMu.Lock()
        a.sandboxes[projectID] = &projectSandbox{runtime: rt, branch: "kaptaan"}
        a.sbMu.Unlock()
        r := rt.ReadFile(ctx, "/home/user/workspace/scratchpad.md")
        if r.IsErr {
                return "", fmt.Errorf("%s", r.Output)
        }
        return r.Output, nil
}

func (a *Agent) resetSandbox(ctx context.Context, projectID int) {
        a.sbMu.Lock()
        ps := a.sandboxes[projectID]
        delete(a.sandboxes, projectID)
        a.sbMu.Unlock()
        if ps != nil {
                _ = ps.runtime.Close(ctx)
        }
        // Remove saved sandbox ID so next ensureSandbox creates a fresh one.
        _ = a.db.DeleteMemory(ctx, projectID, "_sandbox_id")
}

// systemPrompt is rebuilt each turn so memory changes are always current.
func (t *turn) systemPrompt() string {
        mems, _ := t.a.db.ListMemories(context.Background(), t.projectID)

        repoURL := normalizeRepoURL(os.Getenv("REPO_URL"))
        githubToken := os.Getenv("GITHUB_TOKEN")

        var b strings.Builder
        b.WriteString("You are **Kaptaan**, an autonomous coding agent.\n\n")

        b.WriteString("## Active project\n")
        if repoURL != "" {
                fmt.Fprintf(&b, "- repo: %s\n", repoURL)
        } else {
                b.WriteString("- repo: (none — set REPO_URL env var)\n")
        }
        if githubToken == "" {
                b.WriteString("- github_token: (missing — PR ops disabled)\n")
        }
        b.WriteString("- workdir in sandbox: /home/user/workspace\n\n")

        b.WriteString("## How you work\n")
        b.WriteString("When given a task:\n")
        b.WriteString("1. Write a simple todo list to scratchpad.md using `write_scratchpad`\n")
        b.WriteString("2. Execute todos one by one\n")
        b.WriteString("3. Check off each completed item in scratchpad using `write_scratchpad`\n")
        b.WriteString("4. After all todos are done: verify output against the original instructions\n")
        b.WriteString("5. Push branch with `shell` (`git push -u origin kaptaan`), call `reset_sandbox` to clean up\n")
        b.WriteString("6. Write a short summary to the user with `send`\n\n")
        b.WriteString("- Do NOT create plan files. Use scratchpad.md as your working memory.\n")
        b.WriteString("- For docs in the repo: use `read_file` and `grep_repo` directly on the /docs folder.\n")
        b.WriteString("- Use `send` to give progress updates. Use `ask` ONLY when genuinely blocked.\n")
        b.WriteString("- Use `write_memory` to persist long-lived facts (architecture decisions, conventions).\n")
        b.WriteString("- When multiple independent operations are needed (e.g. reading several files), call all tools in one turn.\n\n")

        b.WriteString("## Sandbox & git workflow\n")
        b.WriteString("- The sandbox persists across turns (and across server restarts via pause/resume).\n")
        b.WriteString("- Call `reset_sandbox` only when a task is fully done — it kills the sandbox.\n")
        b.WriteString("- The working branch is always `kaptaan` (fixed — do not create other branches).\n")
        b.WriteString("- Commit frequently with `git_commit` after each meaningful chunk of work.\n")
        b.WriteString("- To publish work: use `shell` to run `git push -u origin kaptaan`.\n\n")

        if len(mems) > 0 {
                b.WriteString("## Stored memories\n")
                for _, m := range mems {
                        fmt.Fprintf(&b, "- `%s` — %s\n", m.Key, truncate(m.Content, 120))
                }
                b.WriteString("\n")
        }
        return b.String()
}

// dispatch executes one tool call and returns the textual result.
func (t *turn) dispatch(ctx context.Context, call llm.ToolCall) string {
        name := call.Function.Name
        args := map[string]interface{}{}
        if call.Function.Arguments != "" {
                _ = json.Unmarshal([]byte(call.Function.Arguments), &args)
        }
        t.a.hooks.Send(t.projectID, fmt.Sprintf("`tool` **%s** %s", name, summariseArgs(args)))

        switch name {
        case "send":
                text := getStr(args, "text")
                if text == "" {
                        return "ERROR: send requires `text`"
                }
                t.a.hooks.Send(t.projectID, text)
                return "ok"

        case "ask":
                q := getStr(args, "question")
                if q == "" {
                        return "ERROR: ask requires `question`"
                }
                reply := t.a.hooks.Ask(t.projectID, q)
                if reply == "" {
                        return "(no reply / cancelled)"
                }
                return "user replied: " + reply

        case "write_scratchpad":
                content := getStr(args, "content")
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.WriteFile(ctx, "/home/user/workspace/scratchpad.md", []byte(content))
                return r.Output

        case "read_scratchpad":
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.ReadFile(ctx, "/home/user/workspace/scratchpad.md")
                return r.Output

        case "list_memories":
                ms, err := t.a.db.ListMemories(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                if len(ms) == 0 {
                        return "(no memories yet)"
                }
                var b strings.Builder
                for _, m := range ms {
                        fmt.Fprintf(&b, "%s — %s\n", m.Key, truncate(m.Content, 200))
                }
                return b.String()

        case "read_memory":
                key := getStr(args, "key")
                m, err := t.a.db.GetMemory(ctx, t.projectID, key)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                return m.Content

        case "write_memory":
                key := getStr(args, "key")
                val := getStr(args, "content")
                if key == "" || val == "" {
                        return "ERROR: write_memory requires `key` and `content`"
                }
                if err := t.a.db.PutMemory(ctx, t.projectID, key, val); err != nil {
                        return "ERROR: " + err.Error()
                }
                return "stored memory " + key

        case "delete_memory":
                key := getStr(args, "key")
                if err := t.a.db.DeleteMemory(ctx, t.projectID, key); err != nil {
                        return "ERROR: " + err.Error()
                }
                return "deleted memory " + key

        case "list_repo":
                path := getStr(args, "path")
                if path == "" {
                        path = "."
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.Shell(ctx, "ls -la "+shellQuote(path), 30)
                return r.Output

        case "read_file":
                path := getStr(args, "path")
                if path == "" {
                        return "ERROR: read_file requires `path`"
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.ReadFile(ctx, path)
                return r.Output

        case "grep_repo":
                pattern := getStr(args, "pattern")
                path := getStr(args, "path")
                if pattern == "" {
                        return "ERROR: grep_repo requires `pattern`"
                }
                if path == "" {
                        path = "."
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.Shell(ctx, fmt.Sprintf("grep -rn --color=never %s %s | head -200", shellQuote(pattern), shellQuote(path)), 60)
                return r.Output

        case "write_file":
                path := getStr(args, "path")
                content := getStr(args, "content")
                if path == "" {
                        return "ERROR: write_file requires `path`"
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.WriteFile(ctx, path, []byte(content))
                return r.Output

        case "shell":
                cmd := getStr(args, "cmd")
                timeout := getInt(args, "timeout_secs", 60)
                if cmd == "" {
                        return "ERROR: shell requires `cmd`"
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.Shell(ctx, cmd, timeout)
                return r.Output

        case "git_commit":
                msg := getStr(args, "message")
                if msg == "" {
                        return "ERROR: git_commit requires `message`"
                }
                rt, err := t.a.ensureSandbox(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                r := rt.Shell(ctx, "cd /home/user/workspace && git add -A && git commit -m "+shellQuote(msg), 60)
                return r.Output

        case "reset_sandbox":
                t.a.resetSandbox(ctx, t.projectID)
                return "sandbox reset"

        default:
                return fmt.Sprintf("ERROR: unknown tool %q", name)
        }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getStr(args map[string]interface{}, key string) string {
        v, _ := args[key].(string)
        return v
}

func getInt(args map[string]interface{}, key string, def int) int {
        switch v := args[key].(type) {
        case float64:
                return int(v)
        case int:
                return v
        }
        return def
}

func summariseArgs(args map[string]interface{}) string {
        if len(args) == 0 {
                return ""
        }
        b, _ := json.Marshal(args)
        return truncate(string(b), 120)
}

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "…"
}

// shellQuote wraps s in single quotes, escaping any existing single quotes.
func shellQuote(s string) string {
        return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// normalizeRepoURL strips any embedded credentials from a repo URL.
func normalizeRepoURL(raw string) string {
        raw = strings.TrimSpace(raw)
        if raw == "" {
                return ""
        }
        if idx := strings.Index(raw, "@"); idx != -1 && strings.HasPrefix(raw, "https://") {
                raw = "https://" + raw[idx+1:]
        }
        return strings.TrimSuffix(raw, ".git")
}

// injectToken returns a clone URL with the GitHub token embedded.
func injectToken(repoURL, token string) string {
        repoURL = strings.TrimSpace(repoURL)
        repoURL = strings.TrimSuffix(repoURL, ".git")
        if idx := strings.Index(repoURL, "@"); idx != -1 && strings.HasPrefix(repoURL, "https://") {
                repoURL = "https://" + repoURL[idx+1:]
        }
        repoURL = strings.TrimPrefix(repoURL, "https://")
        return "https://" + token + "@" + repoURL + ".git"
}
