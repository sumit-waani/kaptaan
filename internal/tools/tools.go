package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
			"Run a bash command in the project workspace. Use for: go build, go test, git ops, file inspection.",
			map[string]interface{}{
				"cmd":     str("bash command to run"),
				"timeout": intProp("timeout in seconds, default 60"),
			}, []string{"cmd"}),

		def("write_file",
			"Write content to a file in the workspace. Creates parent dirs automatically. Content must be base64-encoded.",
			map[string]interface{}{
				"path":    str("file path relative to workspace"),
				"content": str("full file content, base64-encoded"),
			}, []string{"path", "content"}),

		def("read_file",
			"Read a file from the workspace and return its content.",
			map[string]interface{}{
				"path": str("file path relative to workspace"),
			}, []string{"path"}),

		def("github_op",
			"GitHub operations. op: clone, push, pr_create, pr_list, status, checkout_branch, create_branch.",
			map[string]interface{}{
				"op":   str("one of: clone, push, pr_create, pr_list, status, checkout_branch, create_branch"),
				"args": str("for pr_create: 'title|body|branch'. for push/checkout/create_branch: branch name"),
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

// Executor runs tool calls in the workspace.
type Executor struct {
	WorkspaceDir string
	GithubRepo   string
	GithubToken  string
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
		return e.WriteFile(str("path"), str("content"))
	case "read_file":
		return e.ReadFile(str("path"))
	case "github_op":
		return e.GithubOp(ctx, str("op"), str("args"))
	default:
		// Handled at agent layer
		return Result{Output: "handled at agent layer: " + name}
	}
}

// Shell runs a bash command with timeout.
func (e *Executor) Shell(ctx context.Context, cmd string, timeoutSecs int) Result {
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	c := exec.CommandContext(tctx, "bash", "-c", cmd)
	if e.WorkspaceDir != "" {
		c.Dir = e.WorkspaceDir
	}
	env := os.Environ()
	if e.GithubToken != "" {
		env = append(env, "GH_TOKEN="+e.GithubToken, "GITHUB_TOKEN="+e.GithubToken)
	}
	c.Env = env

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		out += "\nSTDERR:\n" + stderr.String()
	}
	out = capOutput(out, 8000)

	if err != nil {
		return Result{Output: out + "\nEXIT: " + err.Error(), IsErr: true}
	}
	return Result{Output: out}
}

// WriteFile writes base64-encoded content to a workspace-relative path.
func (e *Executor) WriteFile(path, contentB64 string) Result {
	data, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		// Fallback: treat as raw text
		data = []byte(contentB64)
	}

	fullPath := path
	if e.WorkspaceDir != "" && !filepath.IsAbs(path) {
		fullPath = filepath.Join(e.WorkspaceDir, path)
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return Result{Output: "mkdir failed: " + err.Error(), IsErr: true}
	}
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return Result{Output: "write failed: " + err.Error(), IsErr: true}
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes → %s", len(data), path)}
}

// ReadFile reads a workspace-relative file.
func (e *Executor) ReadFile(path string) Result {
	fullPath := path
	if e.WorkspaceDir != "" && !filepath.IsAbs(path) {
		fullPath = filepath.Join(e.WorkspaceDir, path)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return Result{Output: "read failed: " + err.Error(), IsErr: true}
	}
	return Result{Output: capOutput(string(data), 8000)}
}

// GithubOp runs a GitHub CLI operation.
func (e *Executor) GithubOp(ctx context.Context, op, args string) Result {
	var cmd string
	switch op {
	case "clone":
		if e.GithubRepo == "" {
			return Result{Output: "GITHUB_REPO not configured", IsErr: true}
		}
		if _, err := os.Stat(e.WorkspaceDir); err == nil {
			cmd = fmt.Sprintf("cd %q && git pull origin HEAD", e.WorkspaceDir)
		} else {
			cmd = fmt.Sprintf("gh repo clone %s %q", e.GithubRepo, e.WorkspaceDir)
		}
	case "status":
		cmd = fmt.Sprintf("cd %q && git status && git log --oneline -5", e.WorkspaceDir)
	case "push":
		branch := args
		if branch == "" {
			branch = "main"
		}
		cmd = fmt.Sprintf("cd %q && git add -A && git commit -m 'cto-agent: update' --allow-empty && git push origin %s",
			e.WorkspaceDir, branch)
	case "create_branch":
		if args == "" {
			return Result{Output: "branch name required", IsErr: true}
		}
		cmd = fmt.Sprintf("cd %q && git checkout -b %s", e.WorkspaceDir, args)
	case "checkout_branch":
		if args == "" {
			return Result{Output: "branch name required", IsErr: true}
		}
		cmd = fmt.Sprintf("cd %q && git checkout %s 2>/dev/null || git checkout -b %s",
			e.WorkspaceDir, args, args)
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
		cmd = fmt.Sprintf(`cd %q && gh pr create --title %q --body %q%s`,
			e.WorkspaceDir, title, body, branchFlag)
	case "pr_list":
		cmd = fmt.Sprintf("cd %q && gh pr list --limit 10", e.WorkspaceDir)
	default:
		return Result{Output: "unknown github_op: " + op, IsErr: true}
	}
	return e.Shell(ctx, cmd, 120)
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
