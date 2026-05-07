package agent

import (
        "context"
        "fmt"
        "log"
        "strings"
        "sync"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/tools"
)

// State represents the agent's current lifecycle stage.
type State string

const (
        StateNew        State = "new"
        StateIngesting  State = "ingesting"
        StateClarifying State = "clarifying"
        StatePlanning   State = "planning"
        StateExecuting  State = "executing"
        StateReplanning State = "replanning"
        StatePaused     State = "paused"
        StateDone       State = "done"
)

// Agent is the core autonomous CTO agent.
type Agent struct {
        db      *db.DB
        llm     *llm.Pool
        exec    *tools.Executor
        send    func(string)  // send message to founder via Telegram
        ask     func(string) string // ask founder, block until reply

        mu         sync.Mutex
        state      State
        pausedFrom State
        paused     bool
        cancelLoop context.CancelFunc
}

// New creates a new Agent.
func New(database *db.DB, pool *llm.Pool, executor *tools.Executor,
        send func(string), ask func(string) string) *Agent {
        return &Agent{
                db:   database,
                llm:  pool,
                exec: executor,
                send: send,
                ask:  ask,
        }
}

// ─── State Machine ─────────────────────────────────────────────────────────

func (a *Agent) GetState(ctx context.Context) State {
        s := a.db.KVGetDefault(ctx, "agent_state", string(StateNew))
        return State(s)
}

func (a *Agent) SetState(ctx context.Context, s State) {
        _ = a.db.KVSet(ctx, "agent_state", string(s))
        _ = a.db.UpdateProjectStatus(ctx, string(s))
        a.mu.Lock()
        a.state = s
        a.mu.Unlock()
        log.Printf("[agent] state → %s", s)
}

// GetStatus returns the current agent state (from KV) and the latest trust score.
func (a *Agent) GetStatus(ctx context.Context) (string, float64) {
        state := string(a.GetState(ctx))
        bd, err := a.CalculateTrustScore(ctx)
        if err != nil {
                proj, _ := a.db.GetProject(ctx)
                if proj != nil {
                        return state, proj.TrustScore
                }
                return state, 0
        }
        return state, bd.Total
}

func (a *Agent) IsPaused(ctx context.Context) bool {
        return a.db.KVGetDefault(ctx, "agent_paused", "0") == "1"
}

func (a *Agent) Pause(ctx context.Context) {
        current := a.GetState(ctx)
        _ = a.db.KVSet(ctx, "agent_paused", "1")
        _ = a.db.KVSet(ctx, "paused_from_state", string(current))
        if a.cancelLoop != nil {
                a.cancelLoop()
        }
        a.send("⏸ Paused. Send /resume to continue.")
        log.Printf("[agent] paused from state=%s", current)
}

func (a *Agent) Resume(ctx context.Context) {
        _ = a.db.KVSet(ctx, "agent_paused", "0")
        a.send("▶️ Resuming...")
        go a.Run(context.Background())
}

// ─── Main Loop ─────────────────────────────────────────────────────────────

// Run is the main agent loop. Called on startup and after /resume.
func (a *Agent) Run(ctx context.Context) {
        loopCtx, cancel := context.WithCancel(ctx)
        a.mu.Lock()
        a.cancelLoop = cancel
        a.mu.Unlock()
        defer cancel()

        for {
                if a.IsPaused(loopCtx) {
                        log.Printf("[agent] paused — loop exit")
                        return
                }

                state := a.GetState(loopCtx)
                log.Printf("[agent] loop tick state=%s", state)

                var err error
                switch state {
                case StateNew:
                        err = a.runNew(loopCtx)
                case StateIngesting:
                        // Advance to clarifying once enough docs have been ingested
                        n, _ := a.db.CountDocChunks(loopCtx)
                        if n >= 5 {
                                a.send("📋 Enough docs ingested! Starting clarification questions...")
                                a.SetState(loopCtx, StateClarifying)
                        } else {
                                time.Sleep(5 * time.Second)
                        }
                        continue
                case StateClarifying:
                        err = a.runClarifying(loopCtx)
                case StatePlanning:
                        err = a.runPlanning(loopCtx)
                case StateExecuting:
                        err = a.runExecuting(loopCtx)
                case StateReplanning:
                        err = a.runReplanning(loopCtx)
                case StateDone:
                        a.send("✅ All tasks complete. Send a new requirement or /replan to scan for improvements.")
                        return
                default:
                        log.Printf("[agent] unknown state: %s", state)
                        time.Sleep(5 * time.Second)
                        continue
                }

                if err != nil {
                        if strings.Contains(err.Error(), "context canceled") {
                                return
                        }
                        log.Printf("[agent] state=%s error: %v", state, err)
                        a.send(fmt.Sprintf("⚠️ Error in %s: %v\nRetrying in 30s...", state, err))
                        select {
                        case <-time.After(30 * time.Second):
                        case <-loopCtx.Done():
                                return
                        }
                }
        }
}

// runNew handles the first startup — no project exists yet.
func (a *Agent) runNew(ctx context.Context) error {
        proj, err := a.db.GetProject(ctx)
        if err != nil {
                // No project — create one
                proj, err = a.db.CreateProject(ctx, "default")
                if err != nil {
                        return fmt.Errorf("create project: %w", err)
                }
        }

        if proj.Status == "new" {
                a.send("👋 Hello! I'm your CTO Agent.\n\nTo get started, please upload your project documentation as Markdown (.md) files.\n\nSend the files one by one via Telegram.")
                a.SetState(ctx, StateIngesting)
        } else {
                // Resume existing project
                a.SetState(ctx, State(proj.Status))
        }
        return nil
}

// runClarifying processes the next unanswered clarification.
func (a *Agent) runClarifying(ctx context.Context) error {
        clarif, err := a.db.GetUnansweredClarification(ctx)
        if err != nil {
                // No unanswered questions — check trust score
                return a.checkTrustAndAdvance(ctx)
        }

        // Ask the founder
        answer := a.ask(fmt.Sprintf("❓ Clarifying question:\n\n%s", clarif.Question))
        if answer == "" {
                return nil
        }

        if err := a.db.AnswerClarification(ctx, clarif.ID, answer); err != nil {
                return err
        }

        // Recalculate trust
        bd, err := a.CalculateTrustScore(ctx)
        if err != nil {
                return err
        }
        a.send(bd.String())

        return a.checkTrustAndAdvance(ctx)
}

func (a *Agent) checkTrustAndAdvance(ctx context.Context) error {
        ready, bd, err := a.ReadyToBuild(ctx)
        if err != nil {
                return err
        }
        if ready {
                a.send(fmt.Sprintf("✅ Trust score: %.1f%% — ready to plan!\n\n%s", bd.Total, bd.String()))
                a.SetState(ctx, StatePlanning)
        } else {
                // Generate more clarifying questions if needed
                if err := a.generateClarifyingQuestions(ctx); err != nil {
                        return err
                }
                // Loop back — next tick will ask
        }
        return nil
}

// ─── Context Builder ───────────────────────────────────────────────────────

// BuildContext assembles a rich context packet for the LLM.
func (a *Agent) BuildContext(ctx context.Context, taskTopic string) (string, error) {
        var sb strings.Builder

        // Project summary
        proj, _ := a.db.GetProject(ctx)
        if proj != nil {
                sb.WriteString(fmt.Sprintf("=== PROJECT ===\nName: %s | Status: %s | Trust: %.1f%%\n\n",
                        proj.Name, proj.Status, proj.TrustScore))
        }

        // Active plan + tasks
        plan, err := a.db.GetActivePlan(ctx)
        if err == nil {
                tasks, _ := a.db.GetTasksByPlan(ctx, plan.ID)
                sb.WriteString(fmt.Sprintf("=== PLAN v%d ===\n", plan.Version))
                for _, t := range tasks {
                        if t.ParentID == nil {
                                icon := taskIcon(t.Status)
                                sb.WriteString(fmt.Sprintf("%s Phase %d: %s [%s]\n", icon, t.Phase, t.Title, t.Status))
                                // Subtasks
                                subs, _ := a.db.GetSubtasks(ctx, t.ID)
                                for _, s := range subs {
                                        sb.WriteString(fmt.Sprintf("   %s %s [%s]\n", taskIcon(s.Status), s.Title, s.Status))
                                }
                        }
                }
                sb.WriteString("\n")
        }

        // Recent logs
        logs, _ := a.db.GetGlobalRecentLogs(ctx, 10)
        if len(logs) > 0 {
                sb.WriteString("=== RECENT ACTIVITY ===\n")
                for i := len(logs) - 1; i >= 0; i-- {
                        l := logs[i]
                        sb.WriteString(fmt.Sprintf("[%s] %s: %s\n",
                                l.CreatedAt.Format("15:04:05"), l.Event, truncateStr(l.Payload, 100)))
                }
                sb.WriteString("\n")
        }

        // Relevant docs
        if taskTopic != "" {
                docCtx, err := a.BuildDocContext(ctx, taskTopic, 4)
                if err == nil && docCtx != "" {
                        sb.WriteString(docCtx)
                }
        }

        // Answered clarifications (summary)
        clarifs, _ := a.db.GetAllClarifications(ctx)
        if len(clarifs) > 0 {
                sb.WriteString("=== KEY DECISIONS (from founder) ===\n")
                for _, c := range clarifs {
                        if c.Answer != "" {
                                sb.WriteString(fmt.Sprintf("Q: %s\nA: %s\n\n", c.Question, c.Answer))
                        }
                }
        }

        return sb.String(), nil
}

func taskIcon(status string) string {
        switch status {
        case "done":
                return "✅"
        case "in_progress":
                return "🔄"
        case "failed":
                return "❌"
        case "skipped":
                return "⏭"
        case "approved":
                return "👍"
        default:
                return "⏳"
        }
}

func truncateStr(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "..."
}

// ─── System Prompt ─────────────────────────────────────────────────────────

const systemPrompt = `You are an autonomous CTO Agent — a senior software engineer and technical lead working for a non-technical founder.

IDENTITY:
- You build real, production-quality Go code
- You are constrained strictly to what the project documentation specifies
- You never deviate from the docs without founder approval
- You work autonomously — no permission needed for pure technical decisions

RULES:
1. DOCS ARE LAW. Every feature you build must be traceable to the documentation.
2. After every .go file you write, run: go build ./... — fix errors before moving on. Max 3 retries.
3. Test coverage target: 85-95%. Write tests. Run go test ./... -cover before every PR.
4. One PR per task phase. Focused, clean PRs.
5. If you want to suggest something NOT in the docs, use ask_founder — never implement unilaterally.
6. Be brief in messages to founder — essentials only. No walls of text.
7. Always use search_docs before starting a task to load relevant context.
8. Always update_task_status when starting/finishing tasks.
9. Always log_event for significant actions (build pass/fail, test results, PRs).
10. Base64-encode all file content when using write_file tool.

GOLANG STANDARDS:
- Idiomatic Go: proper error handling, context propagation, no naked goroutines
- Package structure: cmd/, internal/, with clear separation
- Use pgx/v5 for Postgres, standard library where possible
- Table-driven tests
- Meaningful variable names, proper comments on exported symbols`
