// Package tools exposes the tool surface the LLM can call. The Executor
// glues those tools to a pluggable Runtime — either an E2B sandbox (used by
// the Builder) or a no-op runtime (used by the Manager, which only plans).
package tools

import (
        "bytes"
        "context"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"

        "github.com/cto-agent/cto-agent/internal/llm"
)

// Definitions returns all tools available to the agent.
func Definitions() []llm.Tool {
        def := func(name, desc string, props map[string]interface{}, required []string) llm.Tool {
                var t llm.Tool
                t.Type = "function"
                t.Function.Name = name
                t.Function.Description = desc
                t.Function.Parameters = map[string]interface{}{
                        "type": "object", "properties": props, "required": required,
                }
                return t
        }
        str := func(desc string) map[string]interface{} {
                return map[string]interface{}{"type": "string", "description": desc}
        }
        intProp := func(desc string) map[string]interface{} {
                return map[string]interface{}{"type": "integer", "description": desc}
        }

        return []llm.Tool{
                def("shell",
                        "Run a bash command in the sandbox workspace. Use for: go build, go test, git ops, file inspection.",
                        map[string]interface{}{
                                "cmd":     str("bash command to run"),
                                "timeout": intProp("timeout in seconds, default 60"),
                        }, []string{"cmd"}),

                def("write_file",
                        "Write content to a file in the workspace. Creates parent dirs automatically. Content must be base64-encoded.",
                        map[string]interface{}{
                                "path":    str("file path relative to the workspace root"),
                                "content": str("full file content, base64-encoded"),
                        }, []string{"path", "content"}),

                def("read_file",
                        "Read a file from the workspace and return its content.",
                        map[string]interface{}{
                                "path": str("file path relative to the workspace root"),
                        }, []string{"path"}),

                def("github_op",
                        "GitHub operations. op: clone, push, pr_create, pr_list, status, checkout_branch, create_branch, merge_pr.",
                        map[string]interface{}{
                                "op":   str("one of: clone, push, pr_create, pr_list, status, checkout_branch, create_branch, merge_pr"),
                                "args": str("for pr_create: 'title|body|branch'. for push/checkout/create_branch: branch name. for merge_pr: PR number"),
                        }, []string{"op"}),

                def("search_docs",
                        "Search project documentation chunks by tags for relevant context.",
                        map[string]interface{}{
                                "tags":  str("comma-separated tags: feature,api,schema,rule,ui,data,config"),
                                "limit": intProp("max chunks to return, default 5"),
                        }, []string{"tags"}),

                def("update_task_status",
                        "Update task/subtask status in the DB.",
                        map[string]interface{}{
                                "task_id": intProp("task ID"),
                                "status":  str("in_progress | done | failed | skipped"),
                        }, []string{"task_id", "status"}),

                def("log_event",
                        "Write an event to the task audit log.",
                        map[string]interface{}{
                                "task_id": intProp("task ID"),
                                "event":   str("tool_call | build_result | test_result | note | error"),
                                "payload": str("event details"),
                        }, []string{"task_id", "event", "payload"}),

                def("ask_founder",
                        "Ask the founder a question when genuinely blocked or approval needed. Blocks until answered.",
                        map[string]interface{}{
                                "question": str("the specific question"),
                        }, []string{"question"}),
        }
}

// Result is the output of a tool execution.
type Result struct {
        Output string
        IsErr  bool
}

func (r Result) String() string {
        if r.IsErr {
                return "ERROR: " + r.Output
        }
        return r.Output
}

// Runtime is the substrate that actually executes tool side-effects.
// Implementations: SandboxRuntime (E2B, used by Builder), NoopRuntime (Manager).
type Runtime interface {
        Shell(ctx context.Context, cmd string, timeoutSecs int) Result
        WriteFile(ctx context.Context, path string, content []byte) Result
        ReadFile(ctx context.Context, path string) Result
        // Workdir is a human-readable label for the workspace root inside this runtime.
        Workdir() string
        // Close releases the runtime's resources (kills the sandbox, etc).
        Close(ctx context.Context) error
}

// Executor binds tool calls to a Runtime + GitHub config. The Runtime decides
// where shells run and where files live; GitHub config is shared across runtimes.
//
// Resolver lets the Executor pick up live per-project GitHub config (e.g. the
// Manager merging a PR for whichever project is currently active). When set,
// any non-empty value it returns overrides the static GithubRepo/GithubToken
// fields. Errors fall back to the static fields.
type Executor struct {
        Runtime     Runtime
        GithubRepo  string
        GithubToken string
        Resolver    func(context.Context) (repo, token string, err error)
}

// resolvedRepoToken returns the GitHub repo+token to use for this call,
// preferring the live Resolver and falling back to the static fields.
func (e *Executor) resolvedRepoToken(ctx context.Context) (string, string) {
        repo, token := e.GithubRepo, e.GithubToken
        if e.Resolver != nil {
                if r, t, err := e.Resolver(ctx); err == nil {
                        if strings.TrimSpace(r) != "" {
                                repo = r
                        }
                        if strings.TrimSpace(t) != "" {
                                token = t
                        }
                }
        }
        return repo, token
}

// NewNoopExecutor returns an Executor whose runtime errors out for shell/file
// ops but still supports merge_pr via the GitHub REST API. Used by the Manager.
func NewNoopExecutor(githubRepo, githubToken string) *Executor {
        return &Executor{
                Runtime:     NoopRuntime{},
                GithubRepo:  githubRepo,
                GithubToken: githubToken,
        }
}

// Run dispatches a named tool call.
// search_docs / update_task_status / log_event / ask_founder are handled by the agent layer.
func (e *Executor) Run(ctx context.Context, name, argsJSON string) Result {
        args := parseArgs(argsJSON)

        str := func(k string) string { return args[k] }
        intVal := func(k string, def int) int {
                if v, ok := args[k]; ok && v != "" {
                        var n int
                        fmt.Sscanf(v, "%d", &n)
                        if n > 0 {
                                return n
                        }
                }
                return def
        }

        switch name {
        case "shell":
                return e.Shell(ctx, str("cmd"), intVal("timeout", 60))
        case "write_file":
                return e.WriteFile(ctx, str("path"), str("content"))
        case "read_file":
                return e.ReadFile(ctx, str("path"))
        case "github_op":
                return e.GithubOp(ctx, str("op"), str("args"))
        default:
                return Result{Output: "handled at agent layer: " + name}
        }
}

// Shell runs a bash command in the runtime.
func (e *Executor) Shell(ctx context.Context, cmd string, timeoutSecs int) Result {
        return e.Runtime.Shell(ctx, cmd, timeoutSecs)
}

// WriteFile decodes base64 content and writes via the runtime.
func (e *Executor) WriteFile(ctx context.Context, path, contentB64 string) Result {
        data, err := base64.StdEncoding.DecodeString(contentB64)
        if err != nil {
                // Tolerate raw text from sloppy LLM output.
                data = []byte(contentB64)
        }
        return e.Runtime.WriteFile(ctx, path, data)
}

// ReadFile reads via the runtime.
func (e *Executor) ReadFile(ctx context.Context, path string) Result {
        return e.Runtime.ReadFile(ctx, path)
}

// GithubOp runs a GitHub CLI operation. merge_pr is special-cased to use the
// REST API directly so it works even when there is no live workspace runtime
// (e.g. the Manager merging an approved PR).
func (e *Executor) GithubOp(ctx context.Context, op, args string) Result {
        if op == "merge_pr" {
                return e.mergePR(ctx, args)
        }
        wd := e.Runtime.Workdir()
        cdPrefix := ""
        if wd != "" {
                cdPrefix = fmt.Sprintf("cd %q && ", wd)
        }

        var cmd string
        switch op {
        case "clone":
                repo, _ := e.resolvedRepoToken(ctx)
                if repo == "" {
                        return Result{Output: "GITHUB_REPO not configured", IsErr: true}
                }
                // Always clone fresh — the sandbox is ephemeral so there is nothing to pull.
                cmd = fmt.Sprintf("rm -rf %q && gh repo clone %s %q", wd, repo, wd)
        case "status":
                cmd = cdPrefix + "git status && git log --oneline -5"
        case "push":
                branch := args
                if branch == "" {
                        branch = "main"
                }
                cmd = cdPrefix + fmt.Sprintf("git add -A && git commit -m 'kaptaan: update' --allow-empty && git push -u origin %s", branch)
        case "create_branch":
                if args == "" {
                        return Result{Output: "branch name required", IsErr: true}
                }
                cmd = cdPrefix + fmt.Sprintf("git checkout -b %s", args)
        case "checkout_branch":
                if args == "" {
                        return Result{Output: "branch name required", IsErr: true}
                }
                cmd = cdPrefix + fmt.Sprintf("git checkout %s 2>/dev/null || git checkout -b %s", args, args)
        case "pr_create":
                parts := strings.SplitN(args, "|", 3)
                if len(parts) < 2 {
                        return Result{Output: "pr_create: need title|body or title|body|branch", IsErr: true}
                }
                title, body := parts[0], parts[1]
                branchFlag := ""
                if len(parts) == 3 && parts[2] != "" {
                        branchFlag = fmt.Sprintf(" --head %q", parts[2])
                }
                cmd = cdPrefix + fmt.Sprintf(`gh pr create --title %q --body %q%s`, title, body, branchFlag)
        case "pr_list":
                cmd = cdPrefix + "gh pr list --limit 10"
        default:
                return Result{Output: "unknown github_op: " + op, IsErr: true}
        }
        return e.Runtime.Shell(ctx, cmd, 180)
}

// mergePR merges a PR via the GitHub REST API (no shell required). Used by the
// Manager which doesn't have a live sandbox runtime.
func (e *Executor) mergePR(ctx context.Context, prNumber string) Result {
        prNumber = strings.TrimSpace(prNumber)
        if prNumber == "" {
                return Result{Output: "pr number required", IsErr: true}
        }
        repo, token := e.resolvedRepoToken(ctx)
        if repo == "" || token == "" {
                return Result{Output: "GITHUB_REPO and GITHUB_TOKEN must be set", IsErr: true}
        }
        url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/merge", repo, prNumber)
        body, _ := json.Marshal(map[string]string{"merge_method": "merge"})
        req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
        req.Header.Set("Authorization", "Bearer "+token)
        req.Header.Set("Accept", "application/vnd.github+json")
        req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
        req.Header.Set("Content-Type", "application/json")
        cl := &http.Client{Timeout: 30 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return Result{Output: "merge http: " + err.Error(), IsErr: true}
        }
        defer res.Body.Close()
        respBody, _ := io.ReadAll(res.Body)
        if res.StatusCode/100 != 2 {
                return Result{Output: fmt.Sprintf("merge failed: %s: %s", res.Status, string(respBody)), IsErr: true}
        }
        return Result{Output: "merged: " + capOutput(string(respBody), 800)}
}

// ScanRepo returns a brief snapshot of the workspace, or a stub if the runtime
// has no workspace (Manager). It is purely informational for the LLM prompt.
func (e *Executor) ScanRepo(ctx context.Context) (string, error) {
        r := e.Runtime.Shell(ctx,
                `find . -type f -name "*.go" | head -100 && echo "---" && cat go.mod 2>/dev/null || echo "no go.mod"`,
                30)
        if r.IsErr {
                return "(repo not loaded — Manager works from project docs only; Builder sees the live tree)", nil
        }
        return r.Output, nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func parseArgs(raw string) map[string]string {
        out := map[string]string{}
        if raw == "" {
                return out
        }
        var m map[string]interface{}
        if err := json.Unmarshal([]byte(raw), &m); err != nil {
                return out
        }
        for k, v := range m {
                out[k] = fmt.Sprintf("%v", v)
        }
        return out
}

func capOutput(s string, n int) string {
        if len(s) <= n {
                return s
        }
        half := n / 2
        return s[:half] + "\n...[truncated]...\n" + s[len(s)-half:]
}
