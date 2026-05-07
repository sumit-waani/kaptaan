package agent

import (
        "context"
        "log"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/tools"
)

// Agent wires Manager and Builder. The Manager is event-driven (one
// invocation per user message) and the Builder runs a background loop to
// process queued jobs — each job in its own fresh E2B sandbox.
type Agent struct {
        manager *Manager
        builder *Builder
        db      *db.DB
}

// New constructs an Agent.
//
// managerExec is the Executor used by the Manager (typically backed by
// tools.NoopRuntime — the Manager does not run shells; it merges PRs via the
// GitHub REST API). builderCfg holds the secrets/config the Builder needs to
// spawn a fresh E2B sandbox per job.
func New(database *db.DB, pool *llm.Pool,
        managerExec *tools.Executor, builderCfg BuilderConfig,
        send func(string), ask func(string) string,
        sendPRReview func(jobID int, taskTitle, prURL, note, diff string),
        sendBuilderStatus func(taskTitle, milestone, detail string),
        notifyStatus func()) *Agent {
        builder := NewBuilder(database, pool, builderCfg, send, sendBuilderStatus)
        manager := NewManager(database, pool, managerExec, send, ask, sendPRReview)
        manager.notifyBuilder = builder.Notify
        manager.notifyStatus = notifyStatus
        builder.onJobDone = func(_ context.Context, job *db.BuilderJob) {
                // Run the review (which blocks waiting for the human merge approval)
                // in its own goroutine so the Builder loop can continue picking up
                // the next queued job instead of stalling on human reaction time.
                go manager.ReviewBuilderJob(context.Background(), job)
        }
        return &Agent{manager: manager, builder: builder, db: database}
}

// RunBuilderLoop runs the builder's background processing loop.
func (a *Agent) RunBuilderLoop(ctx context.Context) { a.builder.Run(ctx) }

// SetNotifyBuilderState wires a callback the builder triggers whenever its
// queue/running state may have changed (so the UI can re-fetch /api/builder/state).
func (a *Agent) SetNotifyBuilderState(fn func()) { a.builder.SetNotifyState(fn) }

// HandleUserMessage routes a free-form user message to the Manager.
func (a *Agent) HandleUserMessage(ctx context.Context, text string) {
        a.manager.HandleUserMessage(ctx, text)
}

// Cancel aborts the in-flight Manager run, if any.
func (a *Agent) Cancel(ctx context.Context) { a.manager.Cancel() }

func (a *Agent) Pause(ctx context.Context)                       { a.manager.Pause(ctx) }
func (a *Agent) Resume(ctx context.Context)                      { a.manager.Resume(ctx) }
func (a *Agent) GetStatus(ctx context.Context) (string, float64) { return a.manager.GetStatus(ctx) }
func (a *Agent) IngestDoc(ctx context.Context, filename, content string) (int, error) {
        return ingestDoc(ctx, a.db, filename, content)
}

// ResumePendingReviews looks for builder jobs that completed but were never
// merged or rejected (e.g. because the server restarted while waiting for the
// user's yes/no) and re-spawns the review goroutines so the questions come
// back into the chat.
func (a *Agent) ResumePendingReviews(ctx context.Context) {
        jobs, err := a.db.ListJobsAwaitingReview(ctx)
        if err != nil {
                log.Printf("[agent] could not list awaiting-review jobs: %v", err)
                return
        }
        for i := range jobs {
                job := jobs[i]
                log.Printf("[agent] resuming review for job %d (task %d)", job.ID, job.TaskID)
                go a.manager.ReviewBuilderJob(context.Background(), &job)
        }
}
