package agent

import (
        "context"
        "encoding/json"
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

You receive: project documentation, recent conversation, a repo snapshot, and the latest user message.
Decide ONE response and reply with strict JSON only — no markdown, no prose around it.

Choose ONE of:
1) {"action":"reply","text":"..."}        — conversational answer, no work needed.
2) {"action":"ask","question":"..."}      — ONE focused clarifying question. Use sparingly.
3) {"action":"plan","summary":"...",
    "phases":[{"number":1,"title":"...",
      "tasks":[{"title":"...","description":"...","subtasks":["..."]}]}]}
                                          — concrete implementation plan.

Rules:
- Ground every plan in the docs and conversation. Do not invent requirements.
- Prefer reasonable assumptions (state them in the summary) over asking.
- Phase order: infra → data → core → api → ui.
- Each task = one PR. Keep tasks focused and testable.
- Keep replies brief.`

// Manager handles user messages, planning, and PR review.
// It does NOT run a background loop — every action is triggered by a user
// message routed through HandleUserMessage.
type Manager struct {
        db            *db.DB
        llm           *llm.Pool
        exec          *tools.Executor
        send          func(string)
        ask           func(string) string
        sendPRReview  func(jobID int, taskTitle, prURL, note, diff string)
        notifyBuilder func()
        notifyStatus  func()

        mu       sync.Mutex
        busy     bool
        cancelFn context.CancelFunc
}

// trace is a thin convenience around db.LogTrace + go log + verifying ctx.
func (m *Manager) trace(ctx context.Context, event, detail string) {
        log.Printf("[manager] %s — %s", event, truncateStr(detail, 120))
        m.db.LogTrace(context.Background(), "manager", event, detail)
}

// tryStartRun atomically claims the busy slot. Returns (cancel, true) on
// success, or (nil, false) if another run is already in progress.
func (m *Manager) tryStartRun(parent context.Context) (context.Context, context.CancelFunc, bool) {
        m.mu.Lock()
        if m.busy {
                m.mu.Unlock()
                return nil, nil, false
        }
        runCtx, cancel := context.WithCancel(parent)
        m.busy = true
        m.cancelFn = cancel
        m.mu.Unlock()
        if m.notifyStatus != nil {
                m.notifyStatus()
        }
        return runCtx, cancel, true
}

func (m *Manager) finishRun() {
        m.mu.Lock()
        m.busy = false
        m.cancelFn = nil
        m.mu.Unlock()
        if m.notifyStatus != nil {
                m.notifyStatus()
        }
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

type managerDecision struct {
        Action   string             `json:"action"`
        Text     string             `json:"text"`
        Question string             `json:"question"`
        Summary  string             `json:"summary"`
        Phases   []managerPlanPhase `json:"phases"`
}

func NewManager(database *db.DB, pool *llm.Pool, executor *tools.Executor,
        send func(string), ask func(string) string,
        sendPRReview func(jobID int, taskTitle, prURL, note, diff string)) *Manager {
        return &Manager{
                db:           database,
                llm:          pool,
                exec:         executor,
                send:         send,
                ask:          ask,
                sendPRReview: sendPRReview,
        }
}

// HandleUserMessage is the single entry point for all agent activity.
// Triggered when the user sends a chat message.
func (m *Manager) HandleUserMessage(ctx context.Context, userText string) {
        m.trace(ctx, "user_message_received", truncateStr(userText, 200))

        if m.IsPaused(ctx) {
                m.trace(ctx, "rejected_paused", "")
                m.send("⏸ Agent is paused. Resume from the chat header to continue.")
                return
        }

        runCtx, cancel, ok := m.tryStartRun(ctx)
        if !ok {
                m.trace(ctx, "rejected_busy", "another HandleUserMessage already running")
                m.send("⏳ Still working on your last message — hit Stop in the header if you want me to drop it.")
                return
        }
        defer func() {
                m.finishRun()
                cancel()
                m.trace(context.Background(), "run_complete", "manager idle")
        }()
        ctx = runCtx

        m.trace(ctx, "building_context", "loading docs, conversation history, repo snapshot")
        docCtx, err := BuildDocContext(ctx, m.db, "", 12)
        if err != nil {
                m.trace(ctx, "doc_context_error", err.Error())
                docCtx = "(documentation unavailable)"
        }
        convo := m.recentConvo(ctx, 20)
        repoInfo, repoErr := m.exec.ScanRepo(ctx)
        if repoErr != nil {
                m.trace(ctx, "repo_scan_error", repoErr.Error())
        }
        m.trace(ctx, "context_built",
                fmt.Sprintf("doc_chars=%d convo_chars=%d repo_chars=%d", len(docCtx), len(convo), len(repoInfo)))

        msgs := []llm.Message{
                {Role: "system", Content: managerSystemPrompt},
                {Role: "user", Content: fmt.Sprintf(`PROJECT DOCS:
%s

RECENT CONVERSATION:
%s

REPO SNAPSHOT:
%s

USER MESSAGE:
%s`, docCtx, convo, truncateStr(repoInfo, 1500), userText)},
        }

        for round := 0; round < 4; round++ {
                if ctx.Err() != nil {
                        m.trace(ctx, "stopped_by_user", fmt.Sprintf("round=%d", round))
                        m.send("⏹ Stopped.")
                        return
                }

                m.trace(ctx, "llm_request",
                        fmt.Sprintf("round=%d messages=%d", round, len(msgs)))
                started := time.Now()
                resp, err := m.llm.ChatJSON(ctx, msgs)
                dur := time.Since(started).Round(time.Millisecond)
                if err != nil {
                        if ctx.Err() != nil {
                                m.trace(ctx, "stopped_by_user", "during LLM call")
                                m.send("⏹ Stopped.")
                                return
                        }
                        m.trace(ctx, "llm_error", fmt.Sprintf("after=%s err=%s", dur, err.Error()))
                        m.send("⚠️ LLM error: " + err.Error())
                        return
                }
                if len(resp.Choices) == 0 {
                        m.trace(ctx, "llm_empty", fmt.Sprintf("after=%s", dur))
                        m.send("⚠️ Empty response from LLM.")
                        return
                }
                raw := resp.Choices[0].Message.Content
                m.trace(ctx, "llm_response",
                        fmt.Sprintf("after=%s in=%d out=%d body=%s",
                                dur, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, truncateStr(raw, 300)))

                var d managerDecision
                if err := json.Unmarshal([]byte(cleanJSON(raw)), &d); err != nil {
                        m.trace(ctx, "decision_parse_error", err.Error())
                        m.send(strings.TrimSpace(raw))
                        return
                }
                m.trace(ctx, "decision", fmt.Sprintf("action=%s", d.Action))

                switch strings.ToLower(d.Action) {
                case "reply":
                        if d.Text != "" {
                                m.send(d.Text)
                        }
                        return
                case "ask":
                        if strings.TrimSpace(d.Question) == "" {
                                m.trace(ctx, "ask_empty", "no question text")
                                return
                        }
                        m.trace(ctx, "asking_user", truncateStr(d.Question, 200))
                        answer := m.ask(d.Question)
                        m.trace(ctx, "ask_answered", truncateStr(answer, 200))
                        if strings.TrimSpace(answer) == "" {
                                return
                        }
                        msgs = append(msgs,
                                llm.Message{Role: "assistant", Content: raw},
                                llm.Message{Role: "user", Content: "ANSWER: " + answer},
                        )
                case "plan":
                        preview := formatPlanPreview(d.Summary, d.Phases)
                        m.trace(ctx, "plan_proposed", fmt.Sprintf("phases=%d summary=%s", len(d.Phases), truncateStr(d.Summary, 120)))
                        answer := m.ask(preview + "\n\nReply **go** to queue this plan, or tell me what to change.")
                        m.trace(ctx, "plan_decision", truncateStr(answer, 200))
                        if isYes(answer) || strings.EqualFold(strings.TrimSpace(answer), "go") {
                                m.persistPlanAndQueue(ctx, d.Summary, d.Phases)
                                return
                        }
                        if strings.TrimSpace(answer) == "" {
                                m.send("⌛ No reply — plan discarded. Send me a new message when ready.")
                                return
                        }
                        msgs = append(msgs,
                                llm.Message{Role: "assistant", Content: raw},
                                llm.Message{Role: "user", Content: "PLAN FEEDBACK: " + answer},
                        )
                default:
                        m.trace(ctx, "decision_unknown", "action="+d.Action)
                        if d.Text != "" {
                                m.send(d.Text)
                        } else {
                                m.send(strings.TrimSpace(raw))
                        }
                        return
                }
        }
        m.trace(ctx, "round_limit_hit", "4 rounds without resolution")
        m.send("🤔 Stopped after 4 clarification rounds — please rephrase your request with more detail.")
}

// SetNotifyStatus wires a callback the manager invokes when busy/idle changes.
func (m *Manager) SetNotifyStatus(fn func()) { m.notifyStatus = fn }

func (m *Manager) persistPlanAndQueue(ctx context.Context, summary string, phases []managerPlanPhase) {
        m.trace(ctx, "persist_plan", fmt.Sprintf("phases=%d", len(phases)))
        if len(phases) == 0 {
                m.send("⚠️ Plan was empty — nothing queued.")
                return
        }

        plan, err := m.db.CreatePlan(ctx)
        if err != nil {
                m.send("⚠️ Could not save plan: " + err.Error())
                return
        }

        var sb strings.Builder
        if strings.TrimSpace(summary) != "" {
                sb.WriteString("📋 **Plan:** ")
                sb.WriteString(summary)
                sb.WriteString("\n\n")
        } else {
                sb.WriteString("📋 **Plan ready**\n\n")
        }

        queued := 0
        for _, phase := range phases {
                sb.WriteString(fmt.Sprintf("**Phase %d — %s**\n", phase.Number, phase.Title))
                for _, t := range phase.Tasks {
                        dbTask, err := m.db.CreateTask(ctx, plan.ID, nil, phase.Number, t.Title, t.Description, false)
                        if err != nil {
                                continue
                        }
                        _ = m.db.UpdateTaskStatus(ctx, dbTask.ID, "approved")
                        for _, st := range t.Subtasks {
                                if sub, err := m.db.CreateTask(ctx, plan.ID, &dbTask.ID, phase.Number, st, "", false); err == nil && sub != nil {
                                        _ = m.db.UpdateTaskStatus(ctx, sub.ID, "pending")
                                }
                        }
                        branch := fmt.Sprintf("feature/task-%d-%s", dbTask.ID, slugify(t.Title))
                        if _, err := m.db.CreateBuilderJob(ctx, dbTask.ID, branch); err == nil {
                                queued++
                        }
                        sb.WriteString("  • ")
                        sb.WriteString(t.Title)
                        sb.WriteString("\n")
                }
        }

        sb.WriteString(fmt.Sprintf("\n%d task(s) queued for the builder.", queued))
        m.send(sb.String())

        if m.notifyBuilder != nil {
                m.notifyBuilder()
        }
}

// formatPlanPreview renders the proposed plan as markdown for user approval.
func formatPlanPreview(summary string, phases []managerPlanPhase) string {
        var sb strings.Builder
        sb.WriteString("📋 **Proposed plan**")
        if strings.TrimSpace(summary) != "" {
                sb.WriteString(" — ")
                sb.WriteString(summary)
        }
        sb.WriteString("\n\n")
        for _, p := range phases {
                sb.WriteString(fmt.Sprintf("**Phase %d — %s**\n", p.Number, p.Title))
                for _, t := range p.Tasks {
                        sb.WriteString("  • ")
                        sb.WriteString(t.Title)
                        sb.WriteString("\n")
                }
        }
        return sb.String()
}

// Cancel aborts the current HandleUserMessage run, if any.
func (m *Manager) Cancel() {
        m.mu.Lock()
        cancel := m.cancelFn
        m.mu.Unlock()
        if cancel != nil {
                cancel()
        }
}

func (m *Manager) recentConvo(ctx context.Context, limit int) string {
        msgs, err := m.db.GetRecentMessages(ctx, limit)
        if err != nil || len(msgs) == 0 {
                return "(no prior messages)"
        }
        var sb strings.Builder
        for _, msg := range msgs {
                sb.WriteString(msg.Role)
                sb.WriteString(": ")
                sb.WriteString(truncateStr(msg.Content, 400))
                sb.WriteString("\n")
        }
        return sb.String()
}

// ReviewBuilderJob is invoked by the builder after a job completes. It asks
// the LLM to produce a review note and presents the PR for human approval.
func (m *Manager) ReviewBuilderJob(ctx context.Context, job *db.BuilderJob) {
        task, err := m.db.GetTask(ctx, job.TaskID)
        if err != nil {
                return
        }

        note, err := m.generateReviewNote(ctx, task, job)
        if err != nil {
                note = "Unable to generate review note."
        }

        _ = m.db.SaveManagerNote(ctx, job.ID, note)
        _ = m.db.LogEvent(ctx, task.ID, "manager_review", truncateStr(note, 500))

        if m.sendPRReview != nil {
                m.sendPRReview(job.ID, task.Title, job.PRURL, note, truncateStr(job.DiffSummary, 5000))
        }

        answer := m.ask(fmt.Sprintf(
                "PR ready: %s\n\nManager note: %s\n\nApprove merge? (yes / no)",
                job.PRURL, note,
        ))

        // Empty answer = ask timed out without a human reply. Leave the job in
        // its current "awaiting review" state instead of auto-rejecting it so
        // the user can come back later and decide.
        if strings.TrimSpace(answer) == "" {
                m.send(fmt.Sprintf("⌛ No decision on %s — left awaiting review. Send me 'merge %d' or 'reject %d' when ready.",
                        job.PRURL, job.ID, job.ID))
                return
        }

        if isYes(answer) {
                result := m.exec.GithubOp(ctx, "merge_pr", fmt.Sprintf("%d", job.PRNumber))
                if result.IsErr {
                        m.send(fmt.Sprintf("❌ Merge failed: %s", result.Output))
                        return
                }
                _ = m.db.UpdateBuilderJobStatus(ctx, job.ID, "merged")
                _ = m.db.UpdateTaskStatus(ctx, job.TaskID, "done")
                _ = m.db.LogEvent(ctx, job.TaskID, "merged", job.PRURL)
                m.send(fmt.Sprintf("✅ Merged: %s", job.PRURL))
                return
        }

        _ = m.db.UpdateBuilderJobStatus(ctx, job.ID, "rejected")
        _ = m.db.UpdateTaskStatus(ctx, job.TaskID, "rejected")
        m.send("❌ PR rejected. Send me a message describing what to change.")
}

func (m *Manager) generateReviewNote(ctx context.Context, task *db.Task, job *db.BuilderJob) (string, error) {
        prompt := fmt.Sprintf(`Review this builder submission briefly.
Task: %s
Description: %s

Diff summary:
%s

Build output:
%s

Test output:
%s

Respond with 3-5 short lines covering quality, risks, and readiness.`,
                task.Title, task.Description,
                truncateStr(job.DiffSummary, 2500),
                truncateStr(job.BuildOutput, 1200),
                truncateStr(job.TestOutput, 1200),
        )

        resp, err := m.llm.Chat(ctx, []llm.Message{
                {Role: "system", Content: managerSystemPrompt},
                {Role: "user", Content: prompt},
        }, nil)
        if err != nil {
                return "", err
        }
        if len(resp.Choices) == 0 {
                return "", fmt.Errorf("empty review")
        }
        note := strings.TrimSpace(resp.Choices[0].Message.Content)
        if note == "" {
                return "", fmt.Errorf("empty review")
        }
        return note, nil
}

// ── Pause / Resume ──────────────────────────────────────────────────────────

func (m *Manager) IsPaused(ctx context.Context) bool {
        return m.db.KVGetDefault(ctx, "agent_paused", "0") == "1"
}

func (m *Manager) Pause(ctx context.Context) {
        _ = m.db.KVSet(ctx, "agent_paused", "1")
        _ = m.db.UpdateProjectStatus(ctx, "paused")
        m.send("⏸ Agent paused.")
        log.Printf("[manager] paused")
}

func (m *Manager) Resume(ctx context.Context) {
        _ = m.db.KVSet(ctx, "agent_paused", "0")
        _ = m.db.UpdateProjectStatus(ctx, "ready")
        m.send("▶️ Agent resumed.")
        log.Printf("[manager] resumed")
}

// GetStatus returns a coarse status string for the UI header.
func (m *Manager) GetStatus(ctx context.Context) (string, float64) {
        if m.IsPaused(ctx) {
                return "paused", 0
        }
        m.mu.Lock()
        busy := m.busy
        m.mu.Unlock()
        if busy {
                return "thinking", 0
        }
        return "ready", 0
}
