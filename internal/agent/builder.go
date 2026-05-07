package agent

import (
        "context"
        "fmt"
        "log"
        "regexp"
        "strconv"
        "strings"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/sandbox"
        "github.com/cto-agent/cto-agent/internal/tools"
        "github.com/jackc/pgx/v5"
)

const builderSystemPrompt = `You are the Builder agent for Kaptaan — an autonomous software engineer.
Your job: implement the assigned task by writing Go code, running builds and tests, and opening a GitHub PR.

RULES:
1. Start with search_docs to load relevant documentation.
2. Check repo status with github_op(status).
3. Create a feature branch: feature/task-{id}-{slug}.
4. After every .go file written: run go build ./... — fix errors before continuing. Max 3 retries.
5. After all code: run go test ./... -cover. Target 85%+ coverage.
6. Commit all changes, open a PR with a clear title and description.
7. Base64-encode all file content when using write_file.
8. Update task status via update_task_status when starting and finishing.
9. Log significant events via log_event.
10. Never ask the human questions — use ask_founder only if truly blocked on a requirement ambiguity.`

const (
        maxBuildRetries      = 3
        maxBuilderIterations = 50
        maxJobRetries        = 2

        // sandboxRepoDir is the absolute path inside every E2B sandbox where the
        // repo gets cloned. The user inside envd is always "user" → /home/user/repo.
        sandboxRepoDir = "/home/user/repo"

        // sandboxLifetimeSecs is how long we ask E2B to keep the sandbox alive.
        // Generous so even slow CI-style builds finish before the platform kills it.
        sandboxLifetimeSecs = 1800
)

// BuilderConfig is the per-environment config the Builder needs to spin up
// fresh per-job sandboxes.
type BuilderConfig struct {
        E2BAPIKey   string
        GithubRepo  string
        GithubToken string
}

// Builder runs as a background goroutine, picks queued jobs from DB,
// spins up a fresh E2B sandbox per job, executes the build loop inside it,
// then destroys the sandbox.
type Builder struct {
        db                *db.DB
        llm               *llm.Pool
        cfg               BuilderConfig
        send              func(string)
        sendBuilderStatus func(taskTitle, milestone, detail string)
        notifyState       func()
        onJobDone         onJobDoneFn

        notify chan struct{}
}

// SetNotifyState wires a callback the builder calls whenever queue/running
// state may have changed (broadcast a fresh snapshot to UIs).
func (b *Builder) SetNotifyState(fn func()) { b.notifyState = fn }

func (b *Builder) emitState() {
        if b.notifyState != nil {
                b.notifyState()
        }
}

type buildRuntime struct {
        buildFailures int
        buildOutput   string
        testOutput    string
        testPassed    bool
        prURL         string
        prNumber      int
        diffSummary   string
}

func NewBuilder(database *db.DB, pool *llm.Pool, cfg BuilderConfig,
        send func(string), sendBuilderStatus func(taskTitle, milestone, detail string)) *Builder {
        return &Builder{
                db:                database,
                llm:               pool,
                cfg:               cfg,
                send:              send,
                sendBuilderStatus: sendBuilderStatus,
                notify:            make(chan struct{}, 1),
        }
}

// onJobDone is invoked after a builder job completes successfully so the
// manager can review and ask the user for merge approval. Wired by Agent.New.
type onJobDoneFn = func(ctx context.Context, job *db.BuilderJob)

// Notify wakes the builder loop when a new job is queued.
func (b *Builder) Notify() {
        select {
        case b.notify <- struct{}{}:
        default:
        }
}

// status emits a builder milestone: persists it to task_log, pushes it to
// the UI via SSE, and is the single source of truth for the Builder page.
// taskID may be 0 (e.g. before a task is loaded); we still SSE-broadcast.
func (b *Builder) status(taskTitle, milestone, detail string) {
        if b.sendBuilderStatus != nil {
                b.sendBuilderStatus(taskTitle, milestone, detail)
        }
}

// statusForTask is like status but also persists to task_log so the Builder
// page can replay milestones after a refresh.
func (b *Builder) statusForTask(ctx context.Context, taskID int, taskTitle, milestone, detail string) {
        b.status(taskTitle, milestone, detail)
        if taskID > 0 {
                payload := milestone
                if strings.TrimSpace(detail) != "" {
                        payload = milestone + ": " + detail
                }
                _ = b.db.LogEvent(ctx, taskID, "builder_status", payload)
        }
}

// Run is the builder's main loop. Runs forever until ctx is cancelled.
func (b *Builder) Run(ctx context.Context) {
        b.recoverStaleJobs(ctx)
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-ctx.Done():
                        return
                case <-b.notify:
                case <-ticker.C:
                }

                if err := b.processNextJob(ctx); err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        log.Printf("[builder] process job: %v", err)
                }
        }
}

func (b *Builder) recoverStaleJobs(ctx context.Context) {
        stale, err := b.db.GetStaleBuilderJobs(ctx)
        if err != nil || len(stale) == 0 {
                return
        }
        for _, job := range stale {
                log.Printf("[builder] recovering stale job %d for task %d", job.ID, job.TaskID)
                _ = b.db.RequeueBuilderJob(ctx, job.ID)
                b.send(fmt.Sprintf("🔄 Recovering interrupted build job for task ID %d", job.TaskID))
        }
        b.Notify()
}

// processNextJob picks the oldest queued builder_job, marks it running, spins
// up a fresh sandbox, runs the agentic build loop, and applies retry policy.
// The sandbox is always destroyed at the end (success or failure).
func (b *Builder) processNextJob(ctx context.Context) error {
        if b.db.KVGetDefault(ctx, "agent_paused", "0") == "1" {
                return nil
        }

        job, err := b.db.GetNextQueuedJob(ctx)
        if err != nil {
                if err == pgx.ErrNoRows {
                        return nil
                }
                return err
        }

        task, err := b.db.GetTask(ctx, job.TaskID)
        if err != nil {
                _ = b.db.UpdateBuilderJobStatus(ctx, job.ID, "failed")
                return err
        }

        _ = b.db.UpdateBuilderJob(ctx, job.ID, "running", job.PRURL, job.PRNumber, job.DiffSummary, job.TestOutput, job.BuildOutput)
        _ = b.db.UpdateTaskStatus(ctx, task.ID, "in_progress")
        _ = b.db.LogEvent(ctx, task.ID, "builder_start", fmt.Sprintf("job_id=%d", job.ID))
        b.send(fmt.Sprintf("🔧 Builder started: %s", task.Title))
        b.emitState()
        defer b.emitState()

        exec, cleanup, err := b.spawnJobExecutor(ctx, task)
        if err != nil {
                _ = b.db.LogEvent(ctx, task.ID, "sandbox_error", truncateStr(err.Error(), 500))
                return b.handleJobFailure(ctx, job, task, fmt.Errorf("sandbox spawn: %w", err))
        }
        defer cleanup()

        if err := b.runBuildLoop(ctx, exec, job, task); err != nil {
                return b.handleJobFailure(ctx, job, task, err)
        }

        if b.onJobDone != nil {
                if latest, err := b.db.GetBuilderJob(ctx, job.ID); err == nil {
                        b.onJobDone(ctx, latest)
                }
        }
        return nil
}

// spawnJobExecutor creates a fresh E2B sandbox, bootstraps it (Go toolchain +
// git config + gh auth), and returns an Executor backed by a SandboxRuntime
// pointing at the sandbox's repo dir. The cleanup func always destroys the
// sandbox — call it via defer.
func (b *Builder) spawnJobExecutor(ctx context.Context, task *db.Task) (*tools.Executor, func(), error) {
        if b.cfg.E2BAPIKey == "" {
                return nil, func() {}, fmt.Errorf("E2B_API_KEY not configured")
        }

        // Resolve per-project repo+token, with env vars as fallback.
        repo, token := b.cfg.GithubRepo, b.cfg.GithubToken
        if proj, err := b.db.GetActiveProject(ctx); err == nil && proj != nil {
                if strings.TrimSpace(proj.RepoURL) != "" {
                        repo = proj.RepoURL
                }
                if strings.TrimSpace(proj.GithubToken) != "" {
                        token = proj.GithubToken
                }
        }
        if token == "" {
                return nil, func() {}, fmt.Errorf("GITHUB_TOKEN not configured for active project")
        }
        if repo == "" {
                return nil, func() {}, fmt.Errorf("GITHUB_REPO not configured for active project")
        }

        b.statusForTask(ctx, task.ID, task.Title, "sandbox", "Provisioning fresh E2B sandbox")
        sb, err := sandbox.Create(ctx, b.cfg.E2BAPIKey, "base", sandboxLifetimeSecs)
        if err != nil {
                return nil, func() {}, err
        }
        log.Printf("[builder] sandbox %s created for task %d", sb.ID, task.ID)

        cleanup := func() {
                killCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
                defer cancel()
                if err := sb.Kill(killCtx); err != nil {
                        log.Printf("[builder] sandbox %s kill: %v", sb.ID, err)
                } else {
                        log.Printf("[builder] sandbox %s destroyed", sb.ID)
                }
        }

        // Strip everything from the host env — only forward what the sandbox needs.
        // gh CLI authenticates from GH_TOKEN; Go defaults GOPATH/GOCACHE under HOME.
        env := map[string]string{
                "GH_TOKEN": token,
                "PATH":     "/home/user/.local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                "HOME":     "/home/user",
        }

        b.statusForTask(ctx, task.ID, task.Title, "sandbox", "Installing Go toolchain")
        bootstrap := `set -e
mkdir -p $HOME/.local $HOME/go $HOME/.cache/go-build
if [ ! -x $HOME/.local/go/bin/go ]; then
  cd $HOME/.local
  curl -sSL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz -o go.tar.gz
  tar -xzf go.tar.gz
  rm go.tar.gz
fi
git config --global user.email "kaptaan@bot.local"
git config --global user.name  "Kaptaan Bot"
echo "go: $($HOME/.local/go/bin/go version)"
echo "gh: $(gh --version | head -1)"`
        res, err := sb.Run(ctx, bootstrap, "/home/user", env, 5*time.Minute)
        if err != nil {
                cleanup()
                return nil, func() {}, fmt.Errorf("bootstrap: %w", err)
        }
        if res.ExitCode != 0 {
                cleanup()
                return nil, func() {}, fmt.Errorf("bootstrap failed (exit %d): %s\n%s",
                        res.ExitCode, res.Stdout, res.Stderr)
        }
        log.Printf("[builder] sandbox %s bootstrapped: %s", sb.ID, strings.TrimSpace(res.Stdout))

        rt := &tools.SandboxRuntime{
                Sandbox: sb,
                Cwd:     sandboxRepoDir,
                Env:     env,
        }
        exec := &tools.Executor{
                Runtime:     rt,
                GithubRepo:  repo,
                GithubToken: token,
        }
        return exec, cleanup, nil
}

// handleJobFailure encapsulates the retry-or-fail policy.
func (b *Builder) handleJobFailure(ctx context.Context, job *db.BuilderJob, task *db.Task, err error) error {
        if job.RetryCount < maxJobRetries {
                nextRetry := job.RetryCount + 1
                _ = b.db.UpdateBuilderJobRetry(ctx, job.ID, nextRetry)
                _ = b.db.LogEvent(ctx, task.ID, "builder_retry", fmt.Sprintf("attempt=%d err=%s", nextRetry, truncateStr(err.Error(), 300)))
                b.send(fmt.Sprintf("⚠️ Builder job failed (attempt %d/%d). Retrying in 60s...", nextRetry, maxJobRetries+1))
                select {
                case <-time.After(60 * time.Second):
                case <-ctx.Done():
                        return ctx.Err()
                }
                _ = b.db.RequeueBuilderJob(ctx, job.ID)
                return nil
        }

        _ = b.db.UpdateBuilderJobStatus(ctx, job.ID, "failed")
        _ = b.db.UpdateTaskStatus(ctx, job.TaskID, "failed")
        _ = b.db.LogEvent(ctx, task.ID, "builder_failed", truncateStr(err.Error(), 500))
        b.send(fmt.Sprintf("❌ Builder job permanently failed after %d attempts: %s", maxJobRetries+1, task.Title))
        return nil
}

// runBuildLoop is the LLM + tool loop for one job, scoped to a single sandbox.
func (b *Builder) runBuildLoop(ctx context.Context, exec *tools.Executor, job *db.BuilderJob, task *db.Task) error {
        cloneResult := exec.GithubOp(ctx, "clone", "")
        if cloneResult.IsErr {
                return fmt.Errorf("repo setup: %s", cloneResult.Output)
        }

        branch := job.Branch
        if strings.TrimSpace(branch) == "" {
                branch = fmt.Sprintf("feature/task-%d-%s", task.ID, slugify(task.Title))
        }
        checkout := exec.GithubOp(ctx, "checkout_branch", branch)
        if checkout.IsErr {
                create := exec.GithubOp(ctx, "create_branch", branch)
                if create.IsErr {
                        return fmt.Errorf("branch setup: %s", create.Output)
                }
        }

        b.statusForTask(ctx, task.ID, task.Title, "started", fmt.Sprintf("Branch: %s", branch))
        b.statusForTask(ctx, task.ID, task.Title, "coding", "Writing files")

        docCtx, _ := BuildDocContext(ctx, b.db, task.Title+" "+task.Description, 8)
        subtasks, _ := b.db.GetSubtasks(ctx, task.ID)
        var subtaskList strings.Builder
        for i, st := range subtasks {
                subtaskList.WriteString(fmt.Sprintf("%d. %s\n", i+1, st.Title))
        }

        userPrompt := fmt.Sprintf(`TASK ID: %d
TASK TITLE: %s
TASK DESCRIPTION: %s
BRANCH: %s
WORKSPACE: %s

SUBTASKS:
%s

Execute the task now end-to-end and open a PR.

Relevant docs:
%s`,
                task.ID,
                task.Title,
                task.Description,
                branch,
                sandboxRepoDir,
                subtaskList.String(),
                docCtx,
        )

        messages := []llm.Message{
                {Role: "system", Content: builderSystemPrompt},
                {Role: "user", Content: userPrompt},
        }
        toolDefs := tools.Definitions()
        runtime := &buildRuntime{}

        for i := 0; i < maxBuilderIterations; i++ {
                resp, err := b.llm.Chat(ctx, messages, toolDefs)
                if err != nil {
                        return fmt.Errorf("llm: %w", err)
                }
                if len(resp.Choices) == 0 {
                        return fmt.Errorf("empty llm response")
                }

                msg := resp.Choices[0].Message
                messages = append(messages, msg)

                if text := strings.TrimSpace(msg.Content); text != "" {
                        _ = b.db.LogEvent(ctx, task.ID, "builder_msg", truncateStr(text, 300))
                }

                if len(msg.ToolCalls) == 0 || resp.Choices[0].FinishReason == "stop" {
                        break
                }

                for _, tc := range msg.ToolCalls {
                        output := b.handleToolCall(ctx, exec, task, tc, runtime)
                        messages = append(messages, llm.Message{
                                Role:       "tool",
                                ToolCallID: tc.ID,
                                Content:    output,
                        })
                        if runtime.buildFailures >= maxBuildRetries {
                                return fmt.Errorf("build failed %d times", maxBuildRetries)
                        }
                }
        }

        b.statusForTask(ctx, task.ID, task.Title, "building", "Running go build ./...")
        if runtime.buildOutput == "" {
                buildResult := exec.Shell(ctx, "go build ./... 2>&1", 180)
                runtime.buildOutput = buildResult.Output
                if buildResult.IsErr {
                        return fmt.Errorf("final build failed: %s", truncateStr(buildResult.Output, 1000))
                }
        }
        b.statusForTask(ctx, task.ID, task.Title, "building", "go build ./... — pass")

        if !runtime.testPassed {
                b.statusForTask(ctx, task.ID, task.Title, "testing", "Running go test ./... -cover")
                result := exec.Shell(ctx, "go test ./... -cover 2>&1", 240)
                runtime.testOutput = result.Output
                if result.IsErr {
                        return fmt.Errorf("final tests failed: %s", truncateStr(result.Output, 1000))
                }
                runtime.testPassed = true
        }
        b.statusForTask(ctx, task.ID, task.Title, "testing", "go test ./... -cover — pass")

        if runtime.prURL == "" {
                title := fmt.Sprintf("Task %d: %s", task.ID, task.Title)
                body := fmt.Sprintf("Implements task #%d\n\n%s", task.ID, task.Description)
                prCreate := exec.GithubOp(ctx, "pr_create", fmt.Sprintf("%s|%s|%s", title, body, branch))
                if prCreate.IsErr {
                        return fmt.Errorf("create pr: %s", truncateStr(prCreate.Output, 1000))
                }
                runtime.prURL = extractPRURL(prCreate.Output)
        }

        if runtime.prURL == "" {
                return fmt.Errorf("pr url not found")
        }
        if runtime.prNumber == 0 {
                runtime.prNumber = prNumberFromURL(runtime.prURL)
        }
        if runtime.prNumber == 0 {
                return fmt.Errorf("pr number not found")
        }

        diffResult := exec.Shell(ctx,
                fmt.Sprintf("gh pr diff %d 2>&1 | head -200", runtime.prNumber),
                60,
        )
        runtime.diffSummary = truncateStr(diffResult.Output, 5000)

        b.statusForTask(ctx, task.ID, task.Title, "pr_opened", runtime.prURL)

        _ = b.db.UpdateTaskPR(ctx, task.ID, runtime.prURL)
        _ = b.db.UpdateTaskStatus(ctx, task.ID, "done")
        if err := b.db.UpdateBuilderJob(ctx, job.ID, "awaiting_review", runtime.prURL, runtime.prNumber, runtime.diffSummary, runtime.testOutput, runtime.buildOutput); err != nil {
                return err
        }
        _ = b.db.LogEvent(ctx, task.ID, "pr_ready", fmt.Sprintf("pr=%s", runtime.prURL))

        b.send(fmt.Sprintf("✅ PR ready for review: %s", runtime.prURL))
        return nil
}

func (b *Builder) handleToolCall(ctx context.Context, exec *tools.Executor, task *db.Task, tc llm.ToolCall, rt *buildRuntime) string {
        args := parseToolArgs(tc.Function.Arguments)
        _ = b.db.LogEvent(ctx, task.ID, "tool_call", fmt.Sprintf("%s: %s", tc.Function.Name, truncateStr(tc.Function.Arguments, 200)))

        switch tc.Function.Name {
        case "ask_founder":
                return "Founder interaction is disabled in builder background mode. Proceed with best judgment."

        case "search_docs":
                tags := strings.Split(args["tags"], ",")
                limit, _ := strconv.Atoi(args["limit"])
                if limit <= 0 {
                        limit = 5
                }
                chunks, err := b.db.SearchDocChunks(ctx, tags, limit)
                if err != nil || len(chunks) == 0 {
                        return "No matching documentation found."
                }
                var sb strings.Builder
                for _, c := range chunks {
                        sb.WriteString(fmt.Sprintf("[%s]\n%s\n\n", c.Relevance, c.ChunkText))
                }
                return sb.String()

        case "update_task_status":
                id, _ := strconv.Atoi(args["task_id"])
                status := strings.TrimSpace(args["status"])
                if id > 0 && status != "" {
                        _ = b.db.UpdateTaskStatus(ctx, id, status)
                }
                return fmt.Sprintf("Task %d status updated to %s", id, status)

        case "log_event":
                id, _ := strconv.Atoi(args["task_id"])
                if id <= 0 {
                        id = task.ID
                }
                _ = b.db.LogEvent(ctx, id, args["event"], args["payload"])
                return "logged"

        case "write_file":
                path := strings.TrimSpace(args["path"])
                if path != "" {
                        b.statusForTask(ctx, task.ID, task.Title, "coding", fmt.Sprintf("Updating %s", path))
                }
                result := exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
                out := result.String()
                if strings.HasSuffix(path, ".go") {
                        build := exec.Shell(ctx, "go build ./... 2>&1", 180)
                        rt.buildOutput = build.Output
                        if build.IsErr {
                                rt.buildFailures++
                                out += fmt.Sprintf("\n\nBUILD FAILED (%d/%d):\n%s", rt.buildFailures, maxBuildRetries, build.Output)
                        } else {
                                rt.buildFailures = 0
                                out += "\n\n✅ Build passes."
                        }
                }
                return out

        case "shell":
                cmd := args["cmd"]
                if strings.Contains(cmd, "go build") {
                        b.statusForTask(ctx, task.ID, task.Title, "building", "Running go build ./...")
                }
                if strings.Contains(cmd, "go test") {
                        b.statusForTask(ctx, task.ID, task.Title, "testing", "Running go test ./... -cover")
                }
                result := exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
                out := result.String()
                if strings.Contains(cmd, "go test") {
                        rt.testOutput = out
                        if !result.IsErr {
                                rt.testPassed = true
                                out += "\ncoverage: " + extractCoverage(out)
                                b.statusForTask(ctx, task.ID, task.Title, "testing", "go test ./... -cover — pass")
                        }
                }
                if strings.Contains(cmd, "go build") {
                        rt.buildOutput = out
                        if !result.IsErr {
                                b.statusForTask(ctx, task.ID, task.Title, "building", "go build ./... — pass")
                        }
                }
                return out

        case "github_op":
                op := strings.TrimSpace(args["op"])
                gResult := exec.GithubOp(ctx, op, args["args"])
                out := gResult.String()
                if op == "pr_create" && !gResult.IsErr {
                        prURL := extractPRURL(gResult.Output)
                        if prURL != "" {
                                rt.prURL = prURL
                                rt.prNumber = prNumberFromURL(prURL)
                                b.statusForTask(ctx, task.ID, task.Title, "pr_opened", prURL)
                        }
                }
                return out

        default:
                result := exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
                return result.String()
        }
}

func extractPRURL(output string) string {
        re := regexp.MustCompile(`https?://[^\s]+/pull/\d+`)
        match := re.FindString(output)
        return strings.TrimSpace(match)
}
