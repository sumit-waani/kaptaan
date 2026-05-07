package agent

import (
	"context"

	"github.com/cto-agent/cto-agent/internal/db"
	"github.com/cto-agent/cto-agent/internal/llm"
	"github.com/cto-agent/cto-agent/internal/tools"
)

// State represents the manager lifecycle stage.
type State string

const (
	StateNew       State = "new"
	StatePlanning  State = "planning"
	StateExecuting State = "executing"
	StatePaused    State = "paused"
	StateDone      State = "done"
)

// Agent is the top-level coordinator that wires Manager and Builder.
// It satisfies the web.Agent interface so server wiring stays unchanged.
type Agent struct {
	manager *Manager
	builder *Builder
	db      *db.DB
}

func New(database *db.DB, pool *llm.Pool, executor *tools.Executor,
	send func(string), ask func(string) string) *Agent {
	builder := NewBuilder(database, pool, executor, send)
	manager := NewManager(database, pool, executor, send, ask)
	manager.builder = builder
	return &Agent{manager: manager, builder: builder, db: database}
}

// Run starts both loops.
func (a *Agent) Run(ctx context.Context) {
	go a.builder.Run(ctx)
	a.manager.Run(ctx)
}

func (a *Agent) Pause(ctx context.Context)                       { a.manager.Pause(ctx) }
func (a *Agent) Resume(ctx context.Context)                      { a.manager.Resume(ctx) }
func (a *Agent) GetStatus(ctx context.Context) (string, float64) { return a.manager.GetStatus(ctx) }
func (a *Agent) GetState(ctx context.Context) State              { return a.manager.GetState(ctx) }
func (a *Agent) IngestDoc(ctx context.Context, filename, content string) (int, error) {
	return ingestDoc(ctx, a.db, a.manager.llm, filename, content)
}
func (a *Agent) ScanRepo(ctx context.Context) (string, error) { return a.manager.exec.ScanRepo(ctx) }
