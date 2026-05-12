// Package agent is the single autonomous coding agent that powers Kaptaan.
// Single project. All configuration (repo URL, tokens, etc.) is read from the
// DB config table at runtime — no env vars required after first boot.
package agent

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "log"
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
        db    *db.DB
        pool  *llm.Pool
        hooks Hooks

        mu      sync.Mutex
        running map[int]bool
        cancels map[int]context.CancelFunc
        queue   chan queuedMsg // depth-1 queue for messages arriving while busy

        sbMu      sync.Mutex
        sandboxes map[int]*projectSandbox
}

func New(database *db.DB, pool *llm.Pool, hooks Hooks) *Agent {
        return &Agent{
                db:        database,
                pool:      pool,
                hooks:     hooks,
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
        newMsgs := t.local[t.baseLen:]

        // Apply a cap: if total stored would exceed 199, prune oldest from DB first.
        // (199 + system = 200 total sent to LLM)
        // We round the delete count UP to the next 'user' message boundary so we
        // never split an assistant+tool_calls / tool group in half — that would
        // produce an invalid conversation that DeepSeek rejects with a 400.
        existing, _ := a.db.LoadConvo(ctx, projectID)
        total := len(existing) + len(newMsgs)
        if total > 199 {
                excess := total - 199
                // Walk forward from `excess` until we land on a 'user' message
                // (or run out of existing messages). This ensures the remaining
                // history always starts cleanly at a user turn.
                rounded := excess
                for rounded < len(existing) && existing[rounded].Role != "user" {
                        rounded++
                }
                _ = a.db.PruneConvo(ctx, projectID, rounded)
        }

        for _, m := range newMsgs {
                tcJSON := db.ToolCallsToJSON(m.ToolCalls)

                // UI fields: handleChat already persisted the user message as a UI row,
                // so commitTurn must not write another one — that would cause duplicates
                // on SSE reconnect. Final assistant messages are handled by FinalizeStream.
                // Tool results and intermediate assistant messages have no UI row.
                uiType, uiText, uiTs := "", "", ""

                if err := a.db.AppendMessage(ctx, projectID,
                        m.Role, m.Content, m.ReasoningContent, m.ToolCallID, tcJSON,
                        uiType, uiText, uiTs,
                ); err != nil {
                        log.Printf("[agent] AppendMessage: %v", err)
                }
        }
}

// loadConvo reads the persisted conversation from DB and prepends a fresh system prompt.
// The loaded history is sanitized to remove any sequences that would be rejected by
// the LLM API (e.g. orphaned tool messages left by a mid-group prune).
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
        return sanitizeConvo(msgs)
}

// sanitizeConvo removes message sequences that would be rejected by the LLM API:
//  1. tool messages not preceded (directly or via other tool messages) by an
//     assistant message that carried tool_calls — these are left orphaned when
//     PruneConvo splits a group mid-way.
//  2. a trailing assistant message that has tool_calls but no following tool
//     messages — the LLM expects results before the conversation can continue.
func sanitizeConvo(msgs []llm.Message) []llm.Message {
        out := make([]llm.Message, 0, len(msgs))
        for i, m := range msgs {
                if m.Role != "tool" {
                        out = append(out, m)
                        continue
                }
                // Walk backwards through the original slice to find the anchor
                // assistant message for this tool message.
                valid := false
                for j := i - 1; j >= 0; j-- {
                        if msgs[j].Role == "tool" {
                                continue // same tool-result group, keep scanning
                        }
                        if msgs[j].Role == "assistant" && len(msgs[j].ToolCalls) > 0 {
                                valid = true
                        }
                        break
                }
                if valid {
                        out = append(out, m)
                } else {
                        log.Printf("[agent] sanitizeConvo: dropping orphaned tool message (id=%s)", m.ToolCallID)
                }
        }

        // Strip any trailing assistant message that has tool_calls but no tool
        // responses — the API would reject the next request immediately.
        for len(out) > 0 {
                last := out[len(out)-1]
                if last.Role == "assistant" && len(last.ToolCalls) > 0 {
                        log.Printf("[agent] sanitizeConvo: dropping trailing assistant with unresolved tool_calls")
                        out = out[:len(out)-1]
                } else {
                        break
                }
        }

        return out
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
//
// The entire check-and-create sequence runs under sbMu to prevent a race
// where two parallel tool calls both see no sandbox and both spin up a new
// E2B instance simultaneously.
func (a *Agent) ensureSandbox(ctx context.Context, projectID int) (tools.Runtime, error) {
        a.sbMu.Lock()
        defer a.sbMu.Unlock()

        // Re-check inside the lock — another goroutine may have created it
        // while we were waiting to acquire.
        if ps := a.sandboxes[projectID]; ps != nil {
                // Quick health-check: if envd is unreachable (paused/killed by E2B in-session),
                // drop the stale entry and fall through to reconnect via DB.
                if sr, ok := ps.runtime.(*tools.SandboxRuntime); ok && sr.Sandbox.Ping(ctx) {
                        return ps.runtime, nil
                }
                log.Printf("[agent] in-memory sandbox is unreachable — reconnecting")
                delete(a.sandboxes, projectID)
        }

        e2bKey := a.db.GetConfig(ctx, "e2b_api_key")
        if e2bKey == "" {
                return nil, errors.New("e2b_api_key is not configured — set it in Settings → Configuration")
        }

        // Try to reconnect to a previously saved (paused) sandbox.
        if savedID := a.db.GetConfig(ctx, "_sandbox_id"); savedID != "" {
                a.hooks.Send(projectID, "🔄 reconnecting to saved sandbox…")
                sb, err := sandbox.Connect(ctx, e2bKey, savedID)
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
                        if a.db.GetConfig(ctx, "github_token") != "" {
                                if r := runtime.Shell(ctx, "cd /home/user/workspace && git fetch origin && git merge origin/main --no-edit", 60); r.IsErr {
                                        log.Printf("[agent] git sync failed: %s", r.Output)
                                }
                        }
                        a.sandboxes[projectID] = &projectSandbox{runtime: runtime, branch: "kaptaan"}
                        a.hooks.Send(projectID, "✅ sandbox reconnected, branch: `kaptaan`")
                        return runtime, nil
                }
                // Saved ID is stale — fall through to create a fresh sandbox.
                log.Printf("[agent] sandbox reconnect failed (%v) — creating new one", err)
                _ = a.db.SetConfig(ctx, "_sandbox_id", "")
        }

        a.hooks.Send(projectID, "🛠 spinning up sandbox…")
        sb, err := sandbox.Create(ctx, e2bKey, "base", 3600)
        if err != nil {
                return nil, fmt.Errorf("sandbox create: %w", err)
        }

        // Persist sandbox reference for future reconnects (stored in config, not
        // memories, so the agent cannot accidentally delete it).
        sandboxRef := sb.ID + ":" + sb.ClientID
        if err := a.db.SetConfig(ctx, "_sandbox_id", sandboxRef); err != nil {
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

        repoURL := normalizeRepoURL(a.db.GetConfig(ctx, "repo_url"))
        githubToken := a.db.GetConfig(ctx, "github_token")

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

        a.sandboxes[projectID] = &projectSandbox{runtime: runtime, branch: "kaptaan"}
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
        e2bKey := a.db.GetConfig(ctx, "e2b_api_key")
        if e2bKey == "" {
                return "", fmt.Errorf("no active sandbox")
        }
        savedID := a.db.GetConfig(ctx, "_sandbox_id")
        if savedID == "" {
                return "", fmt.Errorf("no active sandbox — send a task first")
        }
        sb, err := sandbox.Connect(ctx, e2bKey, savedID)
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
        // Clear saved sandbox ID so next ensureSandbox creates a fresh one.
        _ = a.db.SetConfig(ctx, "_sandbox_id", "")
}

// systemPrompt is rebuilt each turn so config and memory changes are current.
func (t *turn) systemPrompt() string {
        ctx := context.Background()
        mems, _ := t.a.db.ListMemories(ctx, t.projectID)

        // If the user has set a custom system prompt, use it and append memories.
        if custom := t.a.db.GetConfig(ctx, "system_prompt"); custom != "" {
                var b strings.Builder
                b.WriteString(custom)
                if len(mems) > 0 {
                        b.WriteString("\n\n## Stored memories\n")
                        for _, m := range mems {
                                fmt.Fprintf(&b, "- `%s` — %s\n", m.Key, truncate(m.Content, 120))
                        }
                }
                return b.String()
        }

        repoURL := normalizeRepoURL(t.a.db.GetConfig(ctx, "repo_url"))
        githubToken := t.a.db.GetConfig(ctx, "github_token")

        var b strings.Builder
        b.WriteString("You are **Kaptaan**, an autonomous coding agent.\n\n")

        b.WriteString("## Active project\n")
        if repoURL != "" {
                fmt.Fprintf(&b, "- repo: %s\n", repoURL)
        } else {
                b.WriteString("- repo: (none — set repo_url in Settings → Configuration)\n")
        }
        if githubToken == "" {
                b.WriteString("- github_token: (missing — set in Settings → Configuration — PR ops disabled)\n")
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

	b.WriteString("## SSH, GitHub, and Cloudflare tools\n")
	b.WriteString("- `ssh_exec(host, cmd)` — run commands on configured hosts (keys in Settings)\n")
	b.WriteString("- `ssh_upload(host, content, path)` / `ssh_read(host, path)` — file transfer\n")
	b.WriteString("- `gh_list_issues(state)` / `gh_create_issue(title, body)` / `gh_close_issue(number)`\n")
	b.WriteString("- `gh_list_workflows` / `gh_trigger_workflow(id, ref)` / `gh_get_workflow_run(run_id)`\n")
	b.WriteString("- `gh_list_branches` / `gh_delete_branch(branch)` / `gh_get_file(path, ref)`\n")
	b.WriteString("- `cf_list_dns_records(type)` / `cf_create_dns(...)` / `cf_update_dns(...)` / `cf_delete_dns(id)`\n")
	b.WriteString("- `cf_purge_cache(files)` / `cf_get_analytics(since_hours)`\n")
	b.WriteString("- All credentials stay server-side — the LLM only sees logical names like host=\"prod\".\n\n")
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
		return "sandbox reset"

	// ── SSH tools ──
	case "ssh_exec":
		host := getStr(args, "host")
		cmd := getStr(args, "cmd")
		timeout := getInt(args, "timeout_secs", 30)
		if host == "" || cmd == "" {
			return "ERROR: ssh_exec requires `host` and `cmd`"
		}
		return t.sshExec(ctx, host, cmd, timeout)

	case "ssh_upload":
		host := getStr(args, "host")
		content := getStr(args, "local_content")
		remote := getStr(args, "remote_path")
		if host == "" || remote == "" {
			return "ERROR: ssh_upload requires `host`, `local_content`, and `remote_path`"
		}
		return t.sshUpload(ctx, host, content, remote)

	case "ssh_read":
		host := getStr(args, "host")
		remote := getStr(args, "remote_path")
		if host == "" || remote == "" {
			return "ERROR: ssh_read requires `host` and `remote_path`"
		}
		return t.sshRead(ctx, host, remote)

	// ── GitHub tools ──
	case "gh_list_issues":
		state := getStr(args, "state")
		return t.ghListIssues(ctx, state)

	case "gh_create_issue":
		title := getStr(args, "title")
		body := getStr(args, "body")
		if title == "" || body == "" {
			return "ERROR: gh_create_issue requires `title` and `body`"
		}
		return t.ghCreateIssue(ctx, title, body)

	case "gh_close_issue":
		num := getInt(args, "number", 0)
		if num <= 0 {
			return "ERROR: gh_close_issue requires `number`"
		}
		return t.ghCloseIssue(ctx, num)

	case "gh_list_workflows":
		return t.ghListWorkflows(ctx)

	case "gh_trigger_workflow":
		wfID := getStr(args, "workflow_id")
		ref := getStr(args, "ref")
		if wfID == "" {
			return "ERROR: gh_trigger_workflow requires `workflow_id`"
		}
		return t.ghTriggerWorkflow(ctx, wfID, ref)

	case "gh_get_workflow_run":
		runID := getInt(args, "run_id", 0)
		if runID <= 0 {
			return "ERROR: gh_get_workflow_run requires `run_id`"
		}
		return t.ghGetWorkflowRun(ctx, runID)

	case "gh_list_branches":
		return t.ghListBranches(ctx)

	case "gh_delete_branch":
		branch := getStr(args, "branch")
		if branch == "" {
			return "ERROR: gh_delete_branch requires `branch`"
		}
		return t.ghDeleteBranch(ctx, branch)

	case "gh_get_file":
		path := getStr(args, "path")
		ref := getStr(args, "ref")
		if path == "" {
			return "ERROR: gh_get_file requires `path`"
		}
		return t.ghGetFile(ctx, path, ref)

	// ── Cloudflare tools ──
	case "cf_list_dns_records":
		recType := getStr(args, "type")
		return t.cfListDNS(ctx, recType)

	case "cf_create_dns":
		recType := getStr(args, "type")
		name := getStr(args, "name")
		content := getStr(args, "content")
		ttl := getInt(args, "ttl", 1)
		proxied := getBool(args, "proxied", false)
		if recType == "" || name == "" || content == "" {
			return "ERROR: cf_create_dns requires `type`, `name`, and `content`"
		}
		return t.cfCreateDNS(ctx, recType, name, content, ttl, proxied)

	case "cf_update_dns":
		recID := getStr(args, "record_id")
		recType := getStr(args, "type")
		name := getStr(args, "name")
		content := getStr(args, "content")
		proxied := getBool(args, "proxied", false)
		if recID == "" || recType == "" || name == "" || content == "" {
			return "ERROR: cf_update_dns requires `record_id`, `type`, `name`, and `content`"
		}
		return t.cfUpdateDNS(ctx, recID, recType, name, content, proxied)

	case "cf_delete_dns":
		recID := getStr(args, "record_id")
		if recID == "" {
			return "ERROR: cf_delete_dns requires `record_id`"
		}
		return t.cfDeleteDNS(ctx, recID)

	case "cf_purge_cache":
		files := getStr(args, "files")
		if files == "" {
			return "ERROR: cf_purge_cache requires `files`"
		}
		return t.cfPurgeCache(ctx, files)

	case "cf_get_analytics":
		sinceHours := getInt(args, "since_hours", 24)
		return t.cfGetAnalytics(ctx, sinceHours)

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
func getBool(args map[string]interface{}, key string, def bool) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	}
	return def
}
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
