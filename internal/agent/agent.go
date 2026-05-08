// Package agent is the single autonomous coding agent that powers Kaptaan.
// One agent, one project at a time. Conversation lives in memory. Plan files
// on disk are the working memory across messages. GitHub is the source of
// truth for PR/merge state.
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

// Hooks are the small set of UI callbacks the agent invokes.
type Hooks struct {
        // Send broadcasts a markdown message to the project's clients.
        Send func(projectID int, text string)
        // Ask blocks until the project's user replies via the UI.
        Ask func(projectID int, question string) string
        // NotifyState pushes updated agent state (idle / running) to the UI.
        NotifyState func(projectID int)
}

// projectSandbox is the live E2B sandbox for one project, shared across all
// turns until the agent explicitly calls reset_sandbox.
type projectSandbox struct {
        runtime tools.Runtime
        branch  string // working branch auto-created on first use
}

// Agent owns per-project conversation state and serialises calls per project.
type Agent struct {
        db     *db.DB
        pool   *llm.Pool
        hooks  Hooks
        e2bKey string

        mu      sync.Mutex
        convo   map[int][]llm.Message // in-memory chat per project
        running map[int]bool          // serialise per project

        // Sandbox persists across turns (not per-turn). One sandbox per project,
        // kept alive until reset_sandbox is called or the server restarts.
        sbMu      sync.Mutex
        sandboxes map[int]*projectSandbox
}

// New wires the agent.
func New(database *db.DB, pool *llm.Pool, e2bKey string, hooks Hooks) *Agent {
        return &Agent{
                db:        database,
                pool:      pool,
                hooks:     hooks,
                e2bKey:    e2bKey,
                convo:     make(map[int][]llm.Message),
                running:   make(map[int]bool),
                sandboxes: make(map[int]*projectSandbox),
        }
}

// IsRunning reports whether an agent loop is in flight for a project.
func (a *Agent) IsRunning(projectID int) bool {
        a.mu.Lock()
        defer a.mu.Unlock()
        return a.running[projectID]
}

// ResetConversation clears the in-memory conversation for one project.
func (a *Agent) ResetConversation(projectID int) {
        a.mu.Lock()
        delete(a.convo, projectID)
        a.mu.Unlock()
}

// HandleUserMessage processes one user turn end-to-end. Blocking. Returns an
// error if another turn is already in flight for this project.
func (a *Agent) HandleUserMessage(ctx context.Context, projectID int, text string) error {
        a.mu.Lock()
        if a.running[projectID] {
                a.mu.Unlock()
                return errors.New("agent is already working on this project")
        }
        a.running[projectID] = true
        a.mu.Unlock()

        defer func() {
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

        proj, err := a.db.GetProjectByID(ctx, projectID)
        if err != nil {
                return fmt.Errorf("project: %w", err)
        }

        turn := newTurn(a, proj)
        defer turn.cleanup()

        turn.appendUser(text)

        const maxIterations = 30
        for i := 0; i < maxIterations; i++ {
                resp, err := a.pool.Chat(ctx, turn.messages(), turn.toolDefs())
                if err != nil {
                        a.hooks.Send(projectID, "❌ LLM error: "+err.Error())
                        turn.persistUsage(ctx, projectID, 0, 0)
                        return err
                }
                choice := resp.Choices[0].Message
                turn.appendAssistant(choice)
                turn.persistUsage(ctx, projectID, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

                // No tool calls = final answer.
                if len(choice.ToolCalls) == 0 {
                        if strings.TrimSpace(choice.Content) != "" {
                                a.hooks.Send(projectID, choice.Content)
                        }
                        a.commitTurn(projectID, turn)
                        return nil
                }

                // Stream a thinking-out-loud preamble if the model included one.
                if pre := strings.TrimSpace(choice.Content); pre != "" {
                        a.hooks.Send(projectID, pre)
                }

                for _, call := range choice.ToolCalls {
                        out := turn.dispatch(ctx, call)
                        turn.appendToolResult(call.ID, out)
                }
        }
        a.hooks.Send(projectID, "⚠️ Reached the iteration limit for this turn. Send another message to continue.")
        a.commitTurn(projectID, turn)
        return nil
}

// commitTurn copies the turn's accumulated messages back to the persistent
// in-memory conversation, capped at the most recent 80 messages so context
// doesn't balloon.
func (a *Agent) commitTurn(projectID int, t *turn) {
        a.mu.Lock()
        defer a.mu.Unlock()
        msgs := t.local
        if len(msgs) > 80 {
                // Always keep the system prompt + tail.
                msgs = append([]llm.Message{msgs[0]}, msgs[len(msgs)-79:]...)
        }
        a.convo[projectID] = msgs
}

func (a *Agent) loadConvo(projectID int) []llm.Message {
        a.mu.Lock()
        defer a.mu.Unlock()
        src := a.convo[projectID]
        dst := make([]llm.Message, len(src))
        copy(dst, src)
        return dst
}

// ─── Per-turn state ────────────────────────────────────────────────────────

type turn struct {
        a           *Agent
        proj        *db.Project
        local       []llm.Message
        planWritten bool
        // sandbox lives on Agent.sandboxes now — not here.
}

func newTurn(a *Agent, proj *db.Project) *turn {
        t := &turn{a: a, proj: proj}
        prior := a.loadConvo(proj.ID)
        if len(prior) == 0 {
                t.local = []llm.Message{{Role: "system", Content: t.systemPrompt()}}
        } else {
                // Refresh system prompt every turn so project-context updates land.
                prior[0] = llm.Message{Role: "system", Content: t.systemPrompt()}
                t.local = prior
        }
        return t
}

func (t *turn) cleanup() {
        // Sandbox is per-project now and lives on Agent.sandboxes — nothing to do here.
}

func (t *turn) messages() []llm.Message { return t.local }

func (t *turn) appendUser(text string) {
        t.local = append(t.local, llm.Message{Role: "user", Content: text})
}

func (t *turn) appendAssistant(m llm.Message) {
        // Force content to be a non-nil string per DeepSeek's strict parser.
        if m.Content == "" && len(m.ToolCalls) > 0 {
                m.Content = ""
        }
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

func (t *turn) persistUsage(ctx context.Context, projectID, prompt, completion int) {
        if prompt == 0 && completion == 0 {
                return
        }
        _ = t.a.db.RecordUsage(ctx, projectID, "deepseek", t.a.pool.ActiveModel(), prompt, completion)
}

// ensureSandbox returns the project's persistent sandbox, creating it on first
// call. The sandbox lives until resetSandbox is called — it survives across
// all turns so file edits and git state are preserved between messages.
func (a *Agent) ensureSandbox(ctx context.Context, proj *db.Project) (tools.Runtime, error) {
        a.sbMu.Lock()
        ps := a.sandboxes[proj.ID]
        a.sbMu.Unlock()
        if ps != nil {
                return ps.runtime, nil
        }

        if a.e2bKey == "" {
                return nil, errors.New("E2B_API_KEY is not configured — sandbox tools are unavailable")
        }

        a.hooks.Send(proj.ID, "🛠 spinning up sandbox…")
        sb, err := sandbox.Create(ctx, a.e2bKey, "base", 3600) // 1-hour timeout
        if err != nil {
                return nil, fmt.Errorf("sandbox create: %w", err)
        }
        runtime := &tools.SandboxRuntime{
                Sandbox: sb,
                Cwd:     "/home/user/workspace",
                Env: map[string]string{
                        "HOME": "/home/user",
                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                },
        }

        // Pre-create the workspace dir so cd succeeds even without a clone.
        if r := runtime.Shell(ctx, "mkdir -p /home/user/workspace", 30); r.IsErr {
                log.Printf("[agent] mkdir workspace: %s", r.Output)
        }

        var branch string
        repoURL := normalizeRepoURL(proj.RepoURL)
        if repoURL != "" && proj.GithubToken != "" {
                cloneURL := injectToken(repoURL, proj.GithubToken)
                // IMPORTANT: start with "cd /home/user" so we are NOT inside
                // /home/user/workspace when we rm -rf it. If we were inside it,
                // git would fail with "Unable to read current working directory"
                // because the process's cwd was just deleted.
                cmd := fmt.Sprintf(
                        "cd /home/user && rm -rf workspace && git clone %s workspace"+
                                " && cd workspace"+
                                " && git config user.email kaptaan@local"+
                                " && git config user.name Kaptaan",
                        shellQuote(cloneURL),
                )
                if r := runtime.Shell(ctx, cmd, 180); r.IsErr {
                        log.Printf("[agent] git clone failed (proj=%d repoURL=%s):\n%s", proj.ID, repoURL, r.Output)
                        a.hooks.Send(proj.ID, "⚠️ git clone failed:\n```\n"+truncate(r.Output, 800)+"\n```\nProceeding with empty workspace.")
                } else {
                        // Auto-create a working branch so all work in this session is isolated.
                        branch = "kaptaan/" + time.Now().UTC().Format("20060102-150405")
                        if r := runtime.Shell(ctx, "cd /home/user/workspace && git checkout -b "+shellQuote(branch), 30); r.IsErr {
                                log.Printf("[agent] branch create failed: %s", r.Output)
                                branch = ""
                        } else {
                                a.hooks.Send(proj.ID, "✅ repo cloned, working branch: `"+branch+"`")
                        }
                }
        }

        ps = &projectSandbox{runtime: runtime, branch: branch}
        a.sbMu.Lock()
        a.sandboxes[proj.ID] = ps
        a.sbMu.Unlock()
        return runtime, nil
}

// resetSandbox closes the current sandbox for a project and removes it so the
// next tool call that needs a sandbox spins up a fresh one.
func (a *Agent) resetSandbox(ctx context.Context, projectID int) {
        a.sbMu.Lock()
        ps := a.sandboxes[projectID]
        delete(a.sandboxes, projectID)
        a.sbMu.Unlock()
        if ps != nil {
                _ = ps.runtime.Close(ctx)
        }
}

// systemPrompt is rebuilt each turn so project-context changes (repo URL,
// memories list) are always current.
func (t *turn) systemPrompt() string {
        mems, _ := t.a.db.ListMemories(context.Background(), t.proj.ID)
        plans, _ := ListPlans(t.proj.ID)
        docs, _ := t.a.db.ListDocs(context.Background(), t.proj.ID)

        var b strings.Builder
        b.WriteString("You are **Kaptaan**, an autonomous coding agent.\n\n")
        b.WriteString("## Active project\n")
        fmt.Fprintf(&b, "- name: %s\n", t.proj.Name)
        if t.proj.RepoURL != "" {
                fmt.Fprintf(&b, "- repo: %s\n", t.proj.RepoURL)
        } else {
                b.WriteString("- repo: (none — `shell`, `git_commit`, `open_pr`, `merge_pr` will fail until a repo URL + GitHub token are set on the project)\n")
        }
        if t.proj.GithubToken == "" {
                b.WriteString("- github_token: (missing — PR ops disabled)\n")
        }
        fmt.Fprintf(&b, "- workdir in sandbox: /home/user/workspace\n\n")

        b.WriteString("## How you work\n")
        b.WriteString("- For any non-trivial code change you MUST first call `write_plan` with a short slug and a markdown plan describing intent, files to touch, and verification steps. Only after a plan is written this turn can you call mutating tools (`write_file`, `shell`, `git_commit`, `open_pr`, `merge_pr`).\n")
        b.WriteString("- Conversation only / questions / explanations do NOT need a plan.\n")
        b.WriteString("- Plans live as files on disk — use `list_plans` and `read_plan` to recall what you decided in earlier turns.\n")
        b.WriteString("- Use `write_memory` to persist long-lived facts about this project (architecture decisions, conventions). Memories survive forever.\n")
        b.WriteString("- Use `send` to give the user progress updates between tool calls. Markdown is supported.\n")
        b.WriteString("- Use `ask` to get yes/no or short answers from the user when blocked.\n\n")

        b.WriteString("## Sandbox & git workflow\n")
        b.WriteString("- **The sandbox persists across all turns** until you call `reset_sandbox`. All files, installed packages, and git state are preserved between messages — do NOT re-clone or reinstall between phases.\n")
        b.WriteString("- When the sandbox first starts, the repo is cloned and a timestamped branch `kaptaan/YYYYMMDD-HHMMss` is created automatically. All your work goes on that branch.\n")
        b.WriteString("- **Commit frequently** with `git_commit` after each meaningful chunk of work (e.g. after each phase). Never leave a long stretch of work uncommitted.\n")
        b.WriteString("- When all phases of a task are done, call `open_pr` to push the branch and open a pull request. Then ask the user to review it.\n")
        b.WriteString("- Only merge with `merge_pr` after the user explicitly approves.\n")
        b.WriteString("- When starting a **brand new unrelated task**, call `reset_sandbox` first so you get a fresh clone on a new branch. Do NOT call `reset_sandbox` between phases of the same task.\n\n")

        if len(plans) > 0 {
                b.WriteString("## Existing plan files (newest first)\n")
                for i, p := range plans {
                        if i >= 10 {
                                fmt.Fprintf(&b, "- … and %d more\n", len(plans)-10)
                                break
                        }
                        fmt.Fprintf(&b, "- `%s`\n", p.Filename)
                }
                b.WriteString("\n")
        }
        if len(mems) > 0 {
                b.WriteString("## Stored memories\n")
                for _, m := range mems {
                        fmt.Fprintf(&b, "- `%s` — %s\n", m.Key, truncate(m.Content, 120))
                }
                b.WriteString("\n")
        }
        if len(docs) > 0 {
                b.WriteString("## Reference docs uploaded by the user\n")
                for _, d := range docs {
                        fmt.Fprintf(&b, "- `%s` (id %d)\n", d.Filename, d.ID)
                }
                b.WriteString("\n")
        }
        return b.String()
}

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "…"
}

// normalizeRepoURL accepts any of these forms and returns a full https URL:
//   - https://github.com/owner/repo          (already correct)
//   - github.com/owner/repo                  (missing scheme)
//   - owner/repo                             (bare slug)
//   - git@github.com:owner/repo.git          (SSH — returned unchanged)
func normalizeRepoURL(u string) string {
        u = strings.TrimSpace(u)
        if u == "" {
                return ""
        }
        if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "git@") {
                return u
        }
        // github.com/owner/repo
        if strings.HasPrefix(u, "github.com/") {
                return "https://" + u
        }
        // bare owner/repo slug
        if !strings.Contains(u, "://") && strings.Count(u, "/") == 1 {
                return "https://github.com/" + u
        }
        return u
}

// injectToken rewrites https://github.com/foo/bar into
// https://x-access-token:<TOKEN>@github.com/foo/bar so `git clone` and `git
// push` can authenticate without a credential helper.
func injectToken(repoURL, token string) string {
        if token == "" || !strings.HasPrefix(repoURL, "https://") {
                return repoURL
        }
        return "https://x-access-token:" + token + "@" + strings.TrimPrefix(repoURL, "https://")
}

// dispatch executes one tool call and returns the textual result.
func (t *turn) dispatch(ctx context.Context, call llm.ToolCall) string {
        name := call.Function.Name
        args := map[string]interface{}{}
        if call.Function.Arguments != "" {
                _ = json.Unmarshal([]byte(call.Function.Arguments), &args)
        }
        t.a.hooks.Send(t.proj.ID, fmt.Sprintf("`tool` **%s** %s", name, summariseArgs(args)))

        mutating := map[string]bool{
                "write_file": true, "shell": true, "git_commit": true,
                "open_pr": true, "merge_pr": true,
        }
        if mutating[name] && !t.planWritten {
                return "ERROR: refusing to call `" + name + "` because no plan has been written this turn. Call `write_plan` first with a brief plan describing what you intend to do."
        }

        switch name {
        case "send":
                text := getStr(args, "text")
                if text == "" {
                        return "ERROR: send requires `text`"
                }
                t.a.hooks.Send(t.proj.ID, text)
                return "ok"

        case "ask":
                q := getStr(args, "question")
                if q == "" {
                        return "ERROR: ask requires `question`"
                }
                reply := t.a.hooks.Ask(t.proj.ID, q)
                if reply == "" {
                        return "(no reply / cancelled)"
                }
                return "user replied: " + reply

        case "list_plans":
                ps, err := ListPlans(t.proj.ID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                if len(ps) == 0 {
                        return "(no plan files yet)"
                }
                var b strings.Builder
                for _, p := range ps {
                        fmt.Fprintf(&b, "%s  (%s, %d bytes)\n", p.Filename, p.Created, p.Bytes)
                }
                return b.String()

        case "read_plan":
                fn := getStr(args, "filename")
                c, err := ReadPlan(t.proj.ID, fn)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                return c

        case "write_plan":
                slug := getStr(args, "slug")
                body := getStr(args, "content")
                if slug == "" || body == "" {
                        return "ERROR: write_plan requires `slug` and `content`"
                }
                fn, err := WritePlan(t.proj.ID, slug, body)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                t.planWritten = true
                t.a.hooks.Send(t.proj.ID, "📝 plan written: `"+fn+"`")
                return "wrote " + fn

        case "update_plan":
                fn := getStr(args, "filename")
                body := getStr(args, "content")
                if fn == "" || body == "" {
                        return "ERROR: update_plan requires `filename` and `content`"
                }
                if err := UpdatePlan(t.proj.ID, fn, body); err != nil {
                        return "ERROR: " + err.Error()
                }
                t.planWritten = true
                return "updated " + fn

        case "list_docs":
                docs, err := t.a.db.ListDocs(ctx, t.proj.ID)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                if len(docs) == 0 {
                        return "(no docs uploaded yet)"
                }
                var b strings.Builder
                for _, d := range docs {
                        fmt.Fprintf(&b, "id=%d  %s  (%d bytes)\n", d.ID, d.Filename, len(d.Content))
                }
                return b.String()

        case "read_doc":
                id := getInt(args, "id", 0)
                if id <= 0 {
                        return "ERROR: read_doc requires `id`"
                }
                d, err := t.a.db.GetDoc(ctx, t.proj.ID, id)
                if err != nil {
                        return "ERROR: doc not found (id=" + fmt.Sprint(id) + ")"
                }
                return d.Content

        case "list_memories":
                ms, err := t.a.db.ListMemories(ctx, t.proj.ID)
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
                m, err := t.a.db.GetMemory(ctx, t.proj.ID, key)
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
                if err := t.a.db.PutMemory(ctx, t.proj.ID, key, val); err != nil {
                        return "ERROR: " + err.Error()
                }
                return "stored memory " + key

        case "delete_memory":
                key := getStr(args, "key")
                if err := t.a.db.DeleteMemory(ctx, t.proj.ID, key); err != nil {
                        return "ERROR: " + err.Error()
                }
                return "deleted memory " + key

        case "list_repo":
                path := getStr(args, "path")
                if path == "" {
                        path = "."
                }
                rt, err := t.a.ensureSandbox(ctx, t.proj)
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
                rt, err := t.a.ensureSandbox(ctx, t.proj)
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
                rt, err := t.a.ensureSandbox(ctx, t.proj)
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
                rt, err := t.a.ensureSandbox(ctx, t.proj)
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
                rt, err := t.a.ensureSandbox(ctx, t.proj)
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
                rt, err := t.a.ensureSandbox(ctx, t.proj)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                // Always commit on the current branch (auto-created at sandbox start).
                // Do not allow the agent to switch branches mid-session via git_commit.
                r := rt.Shell(ctx, "cd /home/user/workspace && git add -A && git commit -m "+shellQuote(msg), 60)
                return r.Output

        case "open_pr":
                title := getStr(args, "title")
                body := getStr(args, "body")
                base := getStr(args, "base")
                if base == "" {
                        base = "main"
                }
                if title == "" {
                        return "ERROR: open_pr requires `title`"
                }
                repoURL := normalizeRepoURL(t.proj.RepoURL)
                if t.proj.GithubToken == "" || repoURL == "" {
                        return "ERROR: project has no GitHub repo or token"
                }
                // Determine which branch to push.
                t.a.sbMu.Lock()
                ps := t.a.sandboxes[t.proj.ID]
                t.a.sbMu.Unlock()
                branch := ""
                if ps != nil {
                        branch = ps.branch
                }
                if branch == "" {
                        // Fall back to whatever branch the sandbox is currently on.
                        rt, err := t.a.ensureSandbox(ctx, t.proj)
                        if err != nil {
                                return "ERROR: " + err.Error()
                        }
                        r := rt.Shell(ctx, "cd /home/user/workspace && git branch --show-current", 15)
                        branch = strings.TrimSpace(r.Output)
                }
                if branch == "" {
                        return "ERROR: could not determine current branch"
                }
                rt, err := t.a.ensureSandbox(ctx, t.proj)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                push := rt.Shell(ctx, "cd /home/user/workspace && git push -u origin "+shellQuote(branch), 120)
                if push.IsErr {
                        return "push failed:\n" + push.Output
                }
                owner, repo, perr := parseOwnerRepo(repoURL)
                if perr != nil {
                        return "ERROR: " + perr.Error()
                }
                pr, err := githubCreatePR(ctx, t.proj.GithubToken, owner, repo, title, body, branch, base)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                t.a.hooks.Send(t.proj.ID, fmt.Sprintf("🔗 PR opened: %s", pr.HTMLURL))
                out, _ := json.Marshal(pr)
                return string(out)

        case "merge_pr":
                num := getInt(args, "number", 0)
                if num == 0 {
                        return "ERROR: merge_pr requires `number`"
                }
                repoURL := normalizeRepoURL(t.proj.RepoURL)
                if t.proj.GithubToken == "" || repoURL == "" {
                        return "ERROR: project has no GitHub repo or token"
                }
                owner, repo, perr := parseOwnerRepo(repoURL)
                if perr != nil {
                        return "ERROR: " + perr.Error()
                }
                res, err := githubMergePR(ctx, t.proj.GithubToken, owner, repo, num)
                if err != nil {
                        return "ERROR: " + err.Error()
                }
                t.a.hooks.Send(t.proj.ID, fmt.Sprintf("✅ Merged PR #%d", num))
                return res

        case "reset_sandbox":
                t.a.resetSandbox(ctx, t.proj.ID)
                t.planWritten = false // fresh sandbox = fresh slate, allow new plan
                return "sandbox closed. Next tool call that needs it will spin up a fresh one with a clean clone."

        default:
                return "ERROR: unknown tool " + name
        }
}

func summariseArgs(args map[string]interface{}) string {
        if len(args) == 0 {
                return ""
        }
        parts := []string{}
        for k, v := range args {
                s := fmt.Sprintf("%v", v)
                if len(s) > 60 {
                        s = s[:60] + "…"
                }
                parts = append(parts, k+"="+s)
        }
        return "(" + strings.Join(parts, ", ") + ")"
}

func getStr(m map[string]interface{}, k string) string {
        if v, ok := m[k]; ok {
                if s, ok := v.(string); ok {
                        return s
                }
        }
        return ""
}

func getInt(m map[string]interface{}, k string, def int) int {
        if v, ok := m[k]; ok {
                switch n := v.(type) {
                case float64:
                        return int(n)
                case int:
                        return n
                case string:
                        var i int
                        _, _ = fmt.Sscanf(n, "%d", &i)
                        if i != 0 {
                                return i
                        }
                }
        }
        return def
}

func shellQuote(s string) string {
        return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// makeMessageWithoutSecrets keeps the agent loop log scrubber compact.
var _ = os.Stdout
var _ = time.Now
