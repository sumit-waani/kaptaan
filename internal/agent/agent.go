package agent

import (
        "context"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/tools"
)

// State is kept for backwards compatibility with stored values; the new agent
// no longer drives a state machine.
type State string

const (
        StateNew     State = "new"
        StatePaused  State = "paused"
        StateReady   State = "ready"
        StateWorking State = "thinking"
)

// Agent wires Manager and Builder. The Manager is event-driven (one
// invocation per user message) and the Builder runs a background loop to
// process queued jobs.
type Agent struct {
        manager *Manager
        builder *Builder
        db      *db.DB
}

func New(database *db.DB, pool *llm.Pool, executor *tools.Executor,
        send func(string), ask func(string) string,
        sendPRReview func(jobID int, taskTitle, prURL, note, diff string),
        sendBuilderStatus func(taskTitle, milestone, detail string)) *Agent {
        builder := NewBuilder(database, pool, executor, send, sendBuilderStatus)
        manager := NewManager(database, pool, executor, send, ask, sendPRReview)
        manager.builder = builder
        builder.onJobDone = func(ctx context.Context, job *db.BuilderJob) {
                // Run the review (which blocks waiting for the human merge approval)
                // in its own goroutine so the Builder loop can continue picking up
                // the next queued job instead of stalling on human reaction time.
                go manager.ReviewBuilderJob(context.Background(), job)
        }
        return &Agent{manager: manager, builder: builder, db: database}
}

// RunBuilderLoop runs the builder's background processing loop.
func (a *Agent) RunBuilderLoop(ctx context.Context) {
        a.builder.Run(ctx)
}

// HandleUserMessage routes a free-form user message to the Manager.
func (a *Agent) HandleUserMessage(ctx context.Context, text string) {
        a.manager.HandleUserMessage(ctx, text)
}

func (a *Agent) Pause(ctx context.Context)                       { a.manager.Pause(ctx) }
func (a *Agent) Resume(ctx context.Context)                      { a.manager.Resume(ctx) }
func (a *Agent) GetStatus(ctx context.Context) (string, float64) { return a.manager.GetStatus(ctx) }
func (a *Agent) IngestDoc(ctx context.Context, filename, content string) (int, error) {
        return ingestDoc(ctx, a.db, filename, content)
}
