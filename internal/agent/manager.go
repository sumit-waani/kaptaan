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
	"github.com/cto-agent/cto-agent/internal/tools"
)

const managerSystemPrompt = `You are the Manager agent for Kaptaan — an autonomous CTO agent.
Your job: understand project docs, create phased implementation plans, assign tasks to the Builder agent, and review code submissions.

RULES:
1. Plans must be grounded in uploaded documentation only.
2. Each task = one PR. Keep tasks focused.
3. When reviewing a PR: check diff quality, test coverage, and whether it matches the task description.
4. Use ask_founder ONLY when genuinely blocked — not for routine decisions.
5. Be brief with the human. No walls of text.
6. Phase order: infra → data → core logic → API → UI.`

// Manager handles human interaction, planning, task assignment, and PR review.
type Manager struct {
	db      *db.DB
	llm     *llm.Pool
	exec    *tools.Executor
	send    func(string)
	ask     func(string) string
	builder *Builder

	mu         sync.Mutex
	cancelLoop context.CancelFunc
}

type managerPlan struct {
	Phases []managerPlanPhase `json:"phases"`
}

type managerPlanPhase struct {
	Number int               `json:"number"`
	Title  string            `json:"title"`
	Tasks  []managerPlanTask `json:"tasks"`
}

type managerPlanTask struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Subtasks    []string `json:"subtasks"`
}

func NewManager(database *db.DB, pool *llm.Pool, executor *tools.Executor,
	send func(string), ask func(string) string) *Manager {
	return &Manager{
		db:   database,
		llm:  pool,
		exec: executor,
		send: send,
		ask:  ask,
	}
}

// Run is the manager's main loop.
// States: StateNew → StatePlanning → StateExecuting → StateDone
func (m *Manager) Run(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancelLoop = cancel
	m.mu.Unlock()
	defer cancel()

	for {
		if m.IsPaused(loopCtx) {
			return
		}

		state := m.GetState(loopCtx)
		var err error

		switch state {
		case StateNew:
			err = m.runNew(loopCtx)
		case StatePlanning:
			err = m.runPlanning(loopCtx)
		case StateExecuting:
			err = m.runExecuting(loopCtx)
		case StatePaused:
			select {
			case <-time.After(2 * time.Second):
			case <-loopCtx.Done():
				return
			}
			continue
		case StateDone:
			return
		default:
			m.SetState(loopCtx, StateNew)
			continue
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[manager] state=%s error: %v", state, err)
			m.send(fmt.Sprintf("⚠️ Manager error in %s: %v", state, err))
			select {
			case <-time.After(5 * time.Second):
			case <-loopCtx.Done():
				return
			}
		}
	}
}

// runNew: send greeting, move to StatePlanning
func (m *Manager) runNew(ctx context.Context) error {
	if _, err := m.db.GetProject(ctx); err != nil {
		if _, err := m.db.CreateProject(ctx, "default"); err != nil {
			return err
		}
	}
	m.send("👋 Hi — I am your Kaptaan Manager agent. Upload docs and I will draft an implementation plan.")
	m.SetState(ctx, StatePlanning)
	return nil
}

// runPlanning: load docs from DB, use LLM to generate plan,
// present each phase to human via ask(), get yes/no approval,
// save approved tasks to DB with status="approved", move to StateExecuting
func (m *Manager) runPlanning(ctx context.Context) error {
	nChunks, _ := m.db.CountDocChunks(ctx)
	if nChunks == 0 {
		m.send("📄 Please upload at least one Markdown document so I can plan accurately.")
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	docCtx, err := BuildDocContext(ctx, m.db, "infra data core api ui", 12)
	if err != nil {
		return err
	}
	repoInfo, _ := m.exec.ScanRepo(ctx)

	prompt := fmt.Sprintf(`Using ONLY the provided project docs, create a phased implementation plan.

DOCS:
%s

REPO SNAPSHOT:
%s

Return valid JSON only:
{
  "phases": [
    {
      "number": 1,
      "title": "Phase title",
      "tasks": [
        {
          "title": "Task title",
          "description": "What and why",
          "subtasks": ["step 1", "step 2"]
        }
      ]
    }
  ]
}

Constraints:
- Phase order: infra → data → core logic → API → UI
- 3-6 phases
- each task should fit into one PR
- keep tasks focused and testable`, docCtx, truncateStr(repoInfo, 2000))

	resp, err := m.llm.ChatJSON(ctx, []llm.Message{
		{Role: "system", Content: managerSystemPrompt},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return err
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("empty planning response")
	}

	var plan managerPlan
	if err := json.Unmarshal([]byte(cleanJSON(resp.Choices[0].Message.Content)), &plan); err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}
	if len(plan.Phases) == 0 {
		return fmt.Errorf("planning response had no phases")
	}

	dbPlan, err := m.db.CreatePlan(ctx)
	if err != nil {
		return err
	}

	approvedCount := 0
	for _, phase := range plan.Phases {
		phaseIntro := fmt.Sprintf("📍 Phase %d — %s\nTasks: %d\nProceed with this phase? (yes/no)", phase.Number, phase.Title, len(phase.Tasks))
		if !isYes(m.ask(phaseIntro)) {
			continue
		}

		for _, task := range phase.Tasks {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))
			sb.WriteString(task.Description)
			sb.WriteString("\n\nSubtasks:\n")
			for i, st := range task.Subtasks {
				sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, st))
			}
			sb.WriteString("\nApprove this task? (yes/no)")

			if !isYes(m.ask(sb.String())) {
				continue
			}

			dbTask, err := m.db.CreateTask(ctx, dbPlan.ID, nil, phase.Number, task.Title, task.Description, false)
			if err != nil {
				return err
			}
			if err := m.db.UpdateTaskStatus(ctx, dbTask.ID, "approved"); err != nil {
				return err
			}
			for _, st := range task.Subtasks {
				sub, err := m.db.CreateTask(ctx, dbPlan.ID, &dbTask.ID, phase.Number, st, "", false)
				if err != nil {
					return err
				}
				_ = m.db.UpdateTaskStatus(ctx, sub.ID, "pending")
			}
			approvedCount++
		}
	}

	if approvedCount == 0 {
		m.send("⚠️ No tasks were approved. I will stay in planning until you approve at least one task.")
		return nil
	}

	m.send(fmt.Sprintf("✅ Plan saved. %d task(s) approved and queued for execution.", approvedCount))
	m.SetState(ctx, StateExecuting)
	return nil
}

// runExecuting: queue approved tasks for builder, then review completed PRs.
func (m *Manager) runExecuting(ctx context.Context) error {
	plan, err := m.db.GetActivePlan(ctx)
	if err != nil {
		m.send("✅ No active plan found. Marking workflow done.")
		m.SetState(ctx, StateDone)
		return nil
	}

	tasks, err := m.db.GetTasksByPlan(ctx, plan.ID)
	if err != nil {
		return err
	}

	var next *db.Task
	for i := range tasks {
		t := tasks[i]
		if t.ParentID != nil || t.Status != "approved" {
			continue
		}
		job, err := m.db.GetJobForTask(ctx, t.ID)
		if err == nil && job != nil {
			if job.Status == "queued" || job.Status == "running" {
				continue
			}
			if job.Status == "done" {
				return m.reviewAndPresent(ctx, job)
			}
		}
		taskCopy := t
		next = &taskCopy
		break
	}

	if next == nil {
		m.send("🏁 All approved tasks are complete.")
		_ = m.db.ExhaustPlan(ctx, plan.ID)
		m.SetState(ctx, StateDone)
		return nil
	}

	branch := fmt.Sprintf("feature/task-%d-%s", next.ID, slugify(next.Title))
	job, err := m.db.CreateBuilderJob(ctx, next.ID, branch)
	if err != nil {
		return err
	}
	_ = m.db.UpdateTaskStatus(ctx, next.ID, "in_progress")
	_ = m.db.LogEvent(ctx, next.ID, "builder_job_queued", fmt.Sprintf("job_id=%d branch=%s", job.ID, branch))
	m.send(fmt.Sprintf("🧱 Builder queued for task: %s", next.Title))

	if m.builder != nil {
		m.builder.Notify()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
			latest, err := m.db.GetBuilderJob(ctx, job.ID)
			if err != nil {
				continue
			}
			if latest.Status == "done" {
				return m.reviewAndPresent(ctx, latest)
			}
			if latest.Status == "failed" {
				m.send(fmt.Sprintf("❌ Builder failed on task: %s", next.Title))
				_ = m.db.UpdateTaskStatus(ctx, next.ID, "failed")
				_ = m.db.LogEvent(ctx, next.ID, "builder_failed", truncateStr(latest.BuildOutput+"\n"+latest.TestOutput, 400))
				return nil
			}
		}
	}
}

// reviewAndPresent: manager LLM reviews diff+test output, asks human for merge approval.
func (m *Manager) reviewAndPresent(ctx context.Context, job *db.BuilderJob) error {
	task, err := m.db.GetTask(ctx, job.TaskID)
	if err != nil {
		return err
	}

	reviewPrompt := fmt.Sprintf(`Review this builder submission briefly.
Task: %s
Description: %s

Diff summary:
%s

Build output:
%s

Test output:
%s

Respond with 3-5 short lines covering quality, risks, and readiness.`,
		task.Title,
		task.Description,
		truncateStr(job.DiffSummary, 2500),
		truncateStr(job.BuildOutput, 1200),
		truncateStr(job.TestOutput, 1200),
	)

	note := "Review unavailable; please inspect the PR manually."
	resp, err := m.llm.Chat(ctx, []llm.Message{
		{Role: "system", Content: managerSystemPrompt},
		{Role: "user", Content: reviewPrompt},
	}, nil)
	if err == nil && len(resp.Choices) > 0 && strings.TrimSpace(resp.Choices[0].Message.Content) != "" {
		note = strings.TrimSpace(resp.Choices[0].Message.Content)
	}

	_ = m.db.SaveManagerNote(ctx, job.ID, note)
	_ = m.db.LogEvent(ctx, task.ID, "manager_review", truncateStr(note, 500))

	question := fmt.Sprintf("🧾 PR ready for review\n\nTask: %s\nPR: %s\n\nDiff summary:\n%s\n\nManager note:\n%s\n\nApprove merge? (yes/no)",
		task.Title,
		job.PRURL,
		truncateStr(job.DiffSummary, 1200),
		note,
	)

	if isYes(m.ask(question)) {
		if job.PRNumber <= 0 {
			m.send("⚠️ Cannot merge: missing PR number.")
			return nil
		}
		result := m.exec.GithubOp(ctx, "merge_pr", fmt.Sprintf("%d", job.PRNumber))
		if result.IsErr {
			m.send("⚠️ Merge failed. Please review manually: " + truncateStr(result.Output, 400))
			_ = m.db.LogEvent(ctx, task.ID, "merge_failed", truncateStr(result.Output, 400))
			return nil
		}
		_ = m.db.UpdateTaskStatus(ctx, task.ID, "done")
		_ = m.db.LogEvent(ctx, task.ID, "merged", fmt.Sprintf("pr=%d", job.PRNumber))
		m.send(fmt.Sprintf("✅ Merged PR #%d for task: %s", job.PRNumber, task.Title))
		return nil
	}

	_ = m.db.UpdateBuilderJob(ctx, job.ID, "rejected", job.PRURL, job.PRNumber, job.DiffSummary, job.TestOutput, job.BuildOutput)
	_ = m.db.UpdateTaskStatus(ctx, task.ID, "rejected")
	_ = m.db.LogEvent(ctx, task.ID, "merge_rejected", fmt.Sprintf("pr=%d", job.PRNumber))
	m.send(fmt.Sprintf("⏭ PR not approved for task: %s", task.Title))
	return nil
}

// GetState / SetState: read/write agent_state KV key.
func (m *Manager) GetState(ctx context.Context) State {
	s := m.db.KVGetDefault(ctx, "agent_state", string(StateNew))
	return State(s)
}

func (m *Manager) SetState(ctx context.Context, s State) {
	_ = m.db.KVSet(ctx, "agent_state", string(s))
	_ = m.db.UpdateProjectStatus(ctx, string(s))
	log.Printf("[manager] state -> %s", s)
}

// GetStatus returns state string and 0 trust (trust system removed).
func (m *Manager) GetStatus(ctx context.Context) (string, float64) {
	return string(m.GetState(ctx)), 0
}

// Pause / Resume.
func (m *Manager) Pause(ctx context.Context) {
	current := m.GetState(ctx)
	_ = m.db.KVSet(ctx, "agent_paused", "1")
	_ = m.db.KVSet(ctx, "paused_from_state", string(current))
	m.SetState(ctx, StatePaused)

	m.mu.Lock()
	cancel := m.cancelLoop
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	m.send("⏸ Paused. Send /resume to continue.")
}

func (m *Manager) Resume(ctx context.Context) {
	_ = m.db.KVSet(ctx, "agent_paused", "0")
	prev := State(m.db.KVGetDefault(ctx, "paused_from_state", string(StatePlanning)))
	if prev == "" || prev == StatePaused {
		prev = StatePlanning
	}
	m.SetState(ctx, prev)
	m.send("▶️ Resuming...")
	go m.Run(context.Background())
}

// IsPaused reads agent_paused KV key.
func (m *Manager) IsPaused(ctx context.Context) bool {
	return m.db.KVGetDefault(ctx, "agent_paused", "0") == "1"
}
