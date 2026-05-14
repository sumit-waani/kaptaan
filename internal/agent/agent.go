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

// Hooks are the small set of UI callbacks the agent invokes.
type Hooks struct {
        Send           func(projectID int, text string)
        Ask            func(projectID int, question string) string
        NotifyState    func(projectID int)
        Token          func(projectID int, token string)
        CancelStream   func(projectID int)
        FinalizeStream func(projectID int)
}

// projectSandbox is the live Daytona workspace, shared across all turns
// until the task finishes. Daytona auto-pauses on inactivity.
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

        sbMu      sync.Mutex              // guards sandboxes map and sbLocks map only
        sbLocks   map[int]*sync.Mutex     // per-project lock for ensureSandbox
        sandboxes map[int]*projectSandbox // per-project live workspace handle
}

func New(database *db.DB, pool *llm.Pool, hooks Hooks) *Agent {
        return &Agent{
                db:        database,
                pool:      pool,
                hooks:     hooks,
                running:   make(map[int]bool),
                cancels:   make(map[int]context.CancelFunc),
                queue:     make(chan queuedMsg, 1),
                sbLocks:   make(map[int]*sync.Mutex),
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

// ensureSandbox returns the persistent Daytona workspace for a project.
// On first call it tries to reconnect to a previously used workspace (ID in DB),
// starting it if it was auto-paused. Falls back to creating a fresh workspace
// if none exists or the saved ID is stale.
//
// The entire check-and-create sequence runs under sbMu to prevent a race
// where two parallel tool calls both see no workspace and spin up new ones.
func (a *Agent) ensureSandbox(ctx context.Context, projectID int) (tools.Runtime, error) {
        // Step 1: get or create the per-project lock (brief global lock)
        a.sbMu.Lock()
        if a.sbLocks[projectID] == nil {
                a.sbLocks[projectID] = &sync.Mutex{}
        }
        pLock := a.sbLocks[projectID]
        a.sbMu.Unlock()

        // Step 2: serialise calls for the SAME project only.
        // Different projects now run concurrently; sbMu is never held during
        // slow network ops (Ping / Connect / Create / shell).
        pLock.Lock()
        defer pLock.Unlock()

        // Step 3: check in-memory handle (brief global lock)
        a.sbMu.Lock()
        ps := a.sandboxes[projectID]
        a.sbMu.Unlock()

        if ps != nil {
                if sr, ok := ps.runtime.(*tools.SandboxRuntime); ok && sr.Sandbox.Ping(ctx) {
                        return ps.runtime, nil
                }
                log.Printf("[agent] in-memory workspace unreachable â reconnecting")
                a.sbMu.Lock()
                delete(a.sandboxes, projectID)
                a.sbMu.Unlock()
        }

        daytonaKey := a.db.GetConfig(ctx, 0, "daytona_api_key")
        if daytonaKey == "" {
                return nil, errors.New("daytona_api_key is not configured â set it in Setup â Sandbox")
        }

	daytonaOrgID := a.db.GetConfig(ctx, 0, "daytona_org_id")
        mkRuntime := func(sb *sandbox.Sandbox) *tools.SandboxRuntime {
                return &tools.SandboxRuntime{
                        Sandbox: sb,
                        Cwd:     "/home/daytona/workspace",
                        Env: map[string]string{
                                "HOME": "/home/daytona",
                                "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                        },
                }
        }

        saveAndReturn := func(runtime *tools.SandboxRuntime) tools.Runtime {
                a.sbMu.Lock()
                a.sandboxes[projectID] = &projectSandbox{runtime: runtime, branch: "kaptaan"}
                a.sbMu.Unlock()
                return runtime
        }

        // Step 4: try reconnecting to saved workspace
        if savedID := a.db.GetConfig(ctx, projectID, "_sandbox_id"); savedID != "" {
                handle := sandbox.NewHandle(daytonaKey, daytonaOrgID, savedID)
                if handle.ID != "" && handle.Ping(ctx) {
                        runtime := mkRuntime(handle)
                        if a.db.GetConfig(ctx, projectID, "github_token") != "" {
                                if r := runtime.Shell(ctx, "git fetch origin && git merge origin/main --no-edit", 60); r.IsErr {
                                        log.Printf("[agent] git sync failed: %s", r.Output)
                                }
                        }
                        a.hooks.Send(projectID, "â workspace reconnected, branch: `kaptaan`")
                        return saveAndReturn(runtime), nil
                }

                // Workspace auto-paused â start it back up.
                a.hooks.Send(projectID, "ð starting workspaceâ¦")
                sb, err := sandbox.Connect(ctx, daytonaKey, daytonaOrgID, savedID)
                if err == nil {
                        runtime := mkRuntime(sb)
                        if a.db.GetConfig(ctx, projectID, "github_token") != "" {
                                if r := runtime.Shell(ctx, "git fetch origin && git merge origin/main --no-edit", 60); r.IsErr {
                                        log.Printf("[agent] git sync failed: %s", r.Output)
                                }
                        }
                        a.hooks.Send(projectID, "â workspace ready, branch: `kaptaan`")
                        return saveAndReturn(runtime), nil
                }
                log.Printf("[agent] workspace reconnect failed (%v) â creating new one", err)
                _ = a.db.SetConfig(ctx, projectID, "_sandbox_id", "")
        }

        // Step 5: create a brand-new workspace
        a.hooks.Send(projectID, "ð  creating workspaceâ¦")
        sb, err := sandbox.Create(ctx, daytonaKey, daytonaOrgID, 0)
        if err != nil {
                return nil, fmt.Errorf("workspace create: %w", err)
        }
        if err := a.db.SetConfig(ctx, projectID, "_sandbox_id", sb.ID); err != nil {
                log.Printf("[agent] failed to store workspace id: %v", err)
        }

        runtime := mkRuntime(sb)

        if r := runtime.Shell(ctx, "mkdir -p /home/daytona/workspace", 30); r.IsErr {
                log.Printf("[agent] mkdir workspace: %s", r.Output)
        }

        repoURL := normalizeRepoURL(a.db.GetConfig(ctx, projectID, "repo_url"))
        githubToken := a.db.GetConfig(ctx, projectID, "github_token")

        if repoURL != "" && githubToken != "" {
                cloneURL := injectToken(repoURL, githubToken)
                cmd := fmt.Sprintf(
                        "cd /home/daytona && rm -rf workspace && git clone %s workspace"+
                                " && cd workspace"+
                                " && git config user.email kaptaan@local"+
                                " && git config user.name Kaptaan"+
                                " && (git checkout kaptaan 2>/dev/null || git checkout -b kaptaan)",
                        shellQuote(cloneURL),
                )
                if r := runtime.Shell(ctx, cmd, 180); r.IsErr {
                        log.Printf("[agent] git clone failed: %s", r.Output)
                        a.hooks.Send(projectID, "â ï¸ git clone failed:\n```\n"+truncate(r.Output, 800)+"\n```\nProceeding with empty workspace.")
                } else {
                        a.hooks.Send(projectID, "â repo cloned, branch: `kaptaan`")
                }
        }

        return saveAndReturn(runtime), nil
}

// ReadScratchpad reads the scratchpad from the DB.
// Used by the settings UI.
func (a *Agent) ReadScratchpad(ctx context.Context, projectID int) (string, error) {
        return a.db.GetProjectScratchpad(ctx, projectID)
}

// systemPrompt is built from the DB. There is no hardcoded fallback.
// system_prompt is stored in global config (project_id=0).
func (t *turn) systemPrompt() string {
        ctx := context.Background()
        mems, _ := t.a.db.ListMemories(ctx, t.projectID)

        if custom := t.a.db.GetConfig(ctx, 0, "system_prompt"); custom != "" {
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
        return "You are Kaptaan. No system_prompt configured — owner must set it in Settings → Configuration.\n"
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
                if err := t.a.db.SetProjectScratchpad(ctx, t.projectID, content); err != nil {
                        return "ERROR: " + err.Error()
                }
                return "ok"

        case "read_scratchpad":
                content, err := t.a.db.GetProjectScratchpad(ctx, t.projectID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                return content

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
                if key == "" {
                        return "ERROR: delete_memory requires `key`"
                }
                if err := t.a.db.DeleteMemory(ctx, t.projectID, key); err != nil {
                        if errors.Is(err, db.ErrNotFound) {
                                return "ERROR: memory key " + key + " not found"
                        }
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
                r := rt.Shell(ctx, "git add -A && git commit -m "+shellQuote(msg), 60)
                return r.Output

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

func getBool(args map[string]interface{}, key string, def bool) bool {
        switch v := args[key].(type) {
        case bool:
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
