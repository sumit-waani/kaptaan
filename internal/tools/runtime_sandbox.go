package tools

import (
        "context"
        "fmt"
        "path/filepath"
        "strings"
        "time"

        "github.com/cto-agent/cto-agent/internal/sandbox"
)

// SandboxRuntime backs an Executor with a live E2B sandbox. The Cwd field is
// the absolute path inside the sandbox where relative file/shell ops resolve.
type SandboxRuntime struct {
        Sandbox *sandbox.Sandbox
        Cwd     string
        Env     map[string]string
}

// shellAnchorDir is a path that is guaranteed to exist after bootstrap. We
// always pass it as the envd `cwd`, because E2B's envd rejects Run() requests
// when the requested cwd does not exist (and individual commands may legitimately
// delete and recreate r.Cwd, e.g. `git clone`). Logical placement inside r.Cwd
// is handled by the per-op `cd` prefix in tools.go.
const shellAnchorDir = "/home/user"

func (r *SandboxRuntime) Shell(ctx context.Context, cmd string, timeoutSecs int) Result {
        if timeoutSecs <= 0 {
                timeoutSecs = 60
        }
        // envd is given a stable anchor (always exists), but we still want commands
        // to run from r.Cwd by default — most callers (e.g. `go build ./...`) assume
        // they're in the repo dir. We try to cd into r.Cwd; if it doesn't exist yet
        // (fresh sandbox, pre-clone), we fall back to the anchor so the command
        // itself can create / clone into it.
        fullCmd := cmd
        if r.Cwd != "" && r.Cwd != shellAnchorDir {
                fullCmd = fmt.Sprintf("cd %q 2>/dev/null || cd %q; %s", r.Cwd, shellAnchorDir, cmd)
        }
        res, err := r.Sandbox.Run(ctx, fullCmd, shellAnchorDir, r.Env, time.Duration(timeoutSecs)*time.Second)
        if err != nil {
                return Result{Output: "sandbox run: " + err.Error(), IsErr: true}
        }
        out := res.Stdout
        if res.Stderr != "" {
                if out != "" {
                        out += "\n"
                }
                out += "STDERR:\n" + res.Stderr
        }
        out = capOutput(out, 8000)
        if res.ExitCode != 0 {
                return Result{
                        Output: out + fmt.Sprintf("\nEXIT: %d (%s)", res.ExitCode, res.Status),
                        IsErr:  true,
                }
        }
        return Result{Output: out}
}

func (r *SandboxRuntime) WriteFile(ctx context.Context, path string, data []byte) Result {
        full := r.absPath(path)
        if err := r.Sandbox.WriteFile(ctx, full, data); err != nil {
                return Result{Output: "write failed: " + err.Error(), IsErr: true}
        }
        return Result{Output: fmt.Sprintf("wrote %d bytes → %s", len(data), path)}
}

func (r *SandboxRuntime) ReadFile(ctx context.Context, path string) Result {
        full := r.absPath(path)
        data, err := r.Sandbox.ReadFile(ctx, full)
        if err != nil {
                return Result{Output: "read failed: " + err.Error(), IsErr: true}
        }
        return Result{Output: capOutput(string(data), 8000)}
}

func (r *SandboxRuntime) Workdir() string { return r.Cwd }

func (r *SandboxRuntime) Close(ctx context.Context) error {
        if r.Sandbox == nil {
                return nil
        }
        return r.Sandbox.Kill(ctx)
}

func (r *SandboxRuntime) absPath(p string) string {
        if filepath.IsAbs(p) {
                return p
        }
        if r.Cwd == "" {
                return "/" + strings.TrimLeft(p, "/")
        }
        return r.Cwd + "/" + strings.TrimLeft(p, "./")
}
