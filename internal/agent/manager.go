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

const managerSystemPrompt = `You are the Manager agent for Kaptaan — the CTO of an autonomous engineering team.

YOU own the product context. The Builder agent who actually writes code does NOT see the PRD, docs, or any prior conversation. The Builder only sees: the plan you hand it + the existing codebase via repo tools. So every task you author must be self-contained and detailed enough that a fresh engineer with only the repo can execute it without ambiguity.

You receive: project documentation, recent conversation, a repo snapshot, and the latest user message.
Decide ONE response and reply with strict JSON only — no markdown, no prose around it.

Choose ONE of:
1) {"action":"reply","text":"..."}                 — conversational answer, no work needed.
2) {"action":"ask","question":"..."}               — ONE focused clarifying question. Use sparingly.
3) {"action":"plan",
    "goal_summary":"one-paragraph recap of the product/goal",
    "outline":[{"number":1,"title":"..."},{"number":2,"title":"..."}, ...],
    "current_phase":{
      "number":1,
      "title":"...",
      "tasks":[
        {"title":"...","description":"detailed spec — what files, structs, funcs, behaviour, acceptance criteria",
         "subtasks":[{"title":"...","description":"concrete file/function-level instruction"}]}
      ]
    }}
                                                   — full project plan, but ONLY phase 1 detailed.

PLANNING DOCTRINE — read carefully:
- Outline = ALL phases (titles only). Think it through end-to-end so order/dependencies are sound.
- current_phase = ONLY the phase you want the Builder to start on right now (usually phase 1).
- After every phase merges you will be re-invoked to detail the NEXT phase. Do NOT pre-detail future phases.
- Keep each phase to 1-3 top-level tasks. Each task = one PR.
- Each task description must be a mini-spec: file paths to create/edit, structs/funcs with signatures, expected behaviour, edge cases, acceptance criteria. The Builder has no PRD.
- Each subtask must have a concrete description (not just a title). The Builder will follow them in order.
- Phase order: infra/scaffold → data → core logic → api → ui → polish/tests.
- Ground everything in the docs + conversation. Do not invent requirements.
- Prefer reasonable assumptions (state them in goal_summary) over asking.
- Keep conversational replies brief.`

// nextPhaseSystemPrompt is used when the Manager is invoked AFTER a phase
// completes to detail the next phase. The outline + completed work are
// already in the user prompt; the Manager just emits one current_phase.
const nextPhaseSystemPrompt = `You are the Manager agent for Kaptaan, continuing an existing plan.

The previous phase has merged. You must now detail the NEXT phase into concrete Builder tasks.

The Builder agent who will execute this does NOT see the PRD, docs, or prior conversation — only your task spec + the existing codebase via repo tools. Every task description must be self-contained: file paths, structs/funcs with signatures, behaviour, acceptance criteria. Every subtask must have a concrete description.

Reply with strict JSON only:
{"current_phase":{
  "number":<phase number>,
  "title":"...",
  "tasks":[
    {"title":"...","description":"mini-spec",
     "subtasks":[{"title":"...","description":"..."}]}
  ]}}

Rules:
- Detail ONLY the requested phase. Keep it to 1-3 top-level tasks. Each task = one PR.
- Reference what's already been built (you'll see merged tasks + their PRs). Do not duplicate work.
- Ground in the docs. No invention.`

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
        Title       string               `json:"title"`
        Description string               `json:"description"`
        Subtasks    []managerPlanSubtask `json:"subtasks"`
}

// managerPlanSubtask carries a concrete title + description so the Builder
// gets actionable instructions rather than just a one-line label.
type managerPlanSubtask struct {
        Title       string `json:"title"`
        Description string `json:"description"`
}

// UnmarshalJSON accepts BOTH the new object shape {"title":"...","description":"..."}
// AND the legacy string shape "just a title" so old prompts/tests/cached LLM
// outputs that still emit `"subtasks":["a","b"]` continue to parse cleanly
// instead of failing the whole plan unmarshal.
func (s *managerPlanSubtask) UnmarshalJSON(data []byte) error {
        trimmed := strings.TrimSpace(string(data))
        if len(trimmed) > 0 && trimmed[0] == '"' {
                var str string
                if err := json.Unmarshal(data, &str); err != nil {
                        return err
                }
                s.Title = str
                return nil
        }
        type raw managerPlanSubtask
        var r raw
        if err := json.Unmarshal(data, &r); err != nil {
                return err
        }
        *s = managerPlanSubtask(r)
        return nil
}

// outlineEntry is the high-level phase list persisted on the plan. It only
// carries titles — full task details are generated on-demand per phase.
type outlineEntry struct {
        Number int    `json:"number"`
        Title  string `json:"title"`
}

type managerDecision struct {
        Action       string             `json:"action"`
        Text         string             `json:"text"`
        Question     string             `json:"question"`
        GoalSummary  string             `json:"goal_summary"`
        Outline      []outlineEntry     `json:"outline"`
        CurrentPhase *managerPlanPhase  `json:"current_phase"`
        // Legacy field kept so old prompts/tests still parse, but ignored.
        Phases []managerPlanPhase `json:"phases"`
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

        // Hard gate: refuse to do any planning or task-spawning work unless
        // the active project has a GitHub repo + token configured. The
        // Builder cannot clone or open PRs without these, so generating a
        // plan would just queue tasks that fail.
        proj, err := m.db.GetActiveProject(ctx)
        if err != nil || proj == nil {
                m.send("⚠️ No active project. Open **Settings → Projects** and create one before sending instructions.")
                return
        }
        if strings.TrimSpace(proj.RepoURL) == "" || strings.TrimSpace(proj.GithubToken) == "" {
                m.send("🔌 **Repo not connected for `" + proj.Name + "`.**\n\n" +
                        "Open **Settings → Projects → Edit** and set:\n\n" +
                        "• GitHub repo (`owner/name`)\n" +
                        "• GitHub token (with `repo` scope)\n\n" +
                        "I can't plan or build until the repo is wired up.")
                m.trace(ctx, "rejected_no_repo", proj.Name)
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
        docCtx := m.buildFullDocContext(ctx, 60000)
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
                        // Tolerate the legacy {"phases":[...]} shape: if the LLM ignored
                        // the new schema, lift phase 1 into current_phase and use the rest
                        // as the outline.
                        if d.CurrentPhase == nil && len(d.Phases) > 0 {
                                first := d.Phases[0]
                                d.CurrentPhase = &first
                                if len(d.Outline) == 0 {
                                        for _, p := range d.Phases {
                                                d.Outline = append(d.Outline, outlineEntry{Number: p.Number, Title: p.Title})
                                        }
                                }
                        }
                        if d.CurrentPhase == nil || len(d.CurrentPhase.Tasks) == 0 {
                                m.trace(ctx, "plan_empty", "no current_phase tasks")
                                m.send("⚠️ Manager produced an empty plan — please rephrase.")
                                return
                        }
                        if len(d.Outline) == 0 {
                                d.Outline = []outlineEntry{{Number: d.CurrentPhase.Number, Title: d.CurrentPhase.Title}}
                        }
                        preview := formatPlanPreview(d.GoalSummary, d.Outline, d.CurrentPhase)
                        m.trace(ctx, "plan_proposed",
                                fmt.Sprintf("outline=%d current_phase=%d tasks=%d goal=%s",
                                        len(d.Outline), d.CurrentPhase.Number, len(d.CurrentPhase.Tasks), truncateStr(d.GoalSummary, 120)))
                        answer := m.ask(preview + "\n\nReply **go** to queue this plan, or tell me what to change.")
                        m.trace(ctx, "plan_decision", truncateStr(answer, 200))
                        if isYes(answer) || strings.EqualFold(strings.TrimSpace(answer), "go") {
                                m.persistInitialPlan(ctx, d.GoalSummary, d.Outline, d.CurrentPhase)
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

// persistInitialPlan saves a brand-new plan: the full phase outline + the
// detailed tasks for the FIRST phase only. Subsequent phases are detailed
// on demand by planNextPhase after the prior phase merges.
func (m *Manager) persistInitialPlan(ctx context.Context, goal string, outline []outlineEntry, current *managerPlanPhase) {
        m.trace(ctx, "persist_initial_plan",
                fmt.Sprintf("phases=%d current_phase=%d tasks=%d", len(outline), current.Number, len(current.Tasks)))

        plan, err := m.db.CreatePlan(ctx)
        if err != nil {
                m.send("⚠️ Could not save plan: " + err.Error())
                return
        }
        outlineJSON, _ := json.Marshal(outline)
        _ = m.db.SetPlanOutline(ctx, plan.ID, string(outlineJSON), goal)
        _ = m.db.SetPlanCurrentPhase(ctx, plan.ID, current.Number)

        firstID, firstTitle, totalTasks := m.persistPhaseTasks(ctx, plan.ID, current)

        var sb strings.Builder
        sb.WriteString("📋 **Plan ready**\n\n")
        if strings.TrimSpace(goal) != "" {
                sb.WriteString("**Goal:** ")
                sb.WriteString(goal)
                sb.WriteString("\n\n")
        }
        sb.WriteString("**Outline (all phases):**\n")
        for _, p := range outline {
                marker := "•"
                if p.Number == current.Number {
                        marker = "▶"
                }
                sb.WriteString(fmt.Sprintf("%s Phase %d — %s\n", marker, p.Number, p.Title))
        }
        sb.WriteString(fmt.Sprintf("\n**Phase %d detailed (%d task(s)):**\n", current.Number, totalTasks))
        for _, t := range current.Tasks {
                sb.WriteString("  • ")
                sb.WriteString(t.Title)
                sb.WriteString("\n")
        }

        if firstID > 0 {
                branch := fmt.Sprintf("feature/task-%d-%s", firstID, slugify(firstTitle))
                if _, err := m.db.CreateBuilderJob(ctx, firstID, branch); err != nil {
                        m.send("⚠️ Could not queue first builder job: " + err.Error())
                }
                sb.WriteString(fmt.Sprintf("\nBuilder will start with **%s**. I'll detail Phase %d only after this phase merges.\n",
                        firstTitle, current.Number+1))
        } else {
                sb.WriteString("\n⚠️ No tasks were created.")
        }
        m.send(sb.String())

        if m.notifyBuilder != nil {
                m.notifyBuilder()
        }
}

// persistPhaseTasks inserts the top-level tasks + subtasks for one phase
// and returns (firstTaskID, firstTaskTitle, totalTopLevelTasks).
func (m *Manager) persistPhaseTasks(ctx context.Context, planID int, phase *managerPlanPhase) (int, string, int) {
        firstID := 0
        firstTitle := ""
        total := 0
        for _, t := range phase.Tasks {
                dbTask, err := m.db.CreateTask(ctx, planID, nil, phase.Number, t.Title, t.Description, false)
                if err != nil || dbTask == nil {
                        continue
                }
                _ = m.db.UpdateTaskStatus(ctx, dbTask.ID, "approved")
                for _, st := range t.Subtasks {
                        if sub, err := m.db.CreateTask(ctx, planID, &dbTask.ID, phase.Number, st.Title, st.Description, false); err == nil && sub != nil {
                                _ = m.db.UpdateTaskStatus(ctx, sub.ID, "pending")
                        }
                }
                if firstID == 0 {
                        firstID = dbTask.ID
                        firstTitle = t.Title
                }
                total++
        }
        return firstID, firstTitle, total
}

// enqueueNextTaskAfterMerge is called after a PR merges. It first looks for
// another approved-but-unqueued task in the CURRENT phase. If the current
// phase is exhausted, it asks the Manager LLM to detail the next phase from
// the outline. If no more phases exist, the plan is marked exhausted.
//
// Backward-compat: legacy plans created before phase-at-a-time planning have
// no outline persisted (empty string) and current_phase=1. For those we fall
// back to the old cross-phase queueing via GetNextTaskToQueue so an upgrade
// in the middle of a long plan doesn't strand the remaining tasks.
func (m *Manager) enqueueNextTaskAfterMerge(ctx context.Context) {
        plan, err := m.db.GetActivePlan(ctx)
        if err != nil || plan == nil {
                return
        }

        var outline []outlineEntry
        if strings.TrimSpace(plan.Outline) != "" {
                _ = json.Unmarshal([]byte(plan.Outline), &outline)
        }

        // Legacy / pre-migration plans: no outline at all → use the old
        // queue-next-across-phases path so already-approved tasks finish.
        if len(outline) == 0 {
                if next, err := m.db.GetNextTaskToQueue(ctx, plan.ID); err == nil && next != nil {
                        m.queueTask(ctx, next)
                        return
                }
                _ = m.db.ExhaustPlan(ctx, plan.ID)
                m.send("🎉 All planned tasks merged. Plan complete.")
                return
        }

        // 1. Try the current phase.
        if next := m.nextQueueableInPhase(ctx, plan.ID, plan.CurrentPhase); next != nil {
                m.queueTask(ctx, next)
                return
        }

        // 2. Current phase is empty → next phase from outline.
        nextPhase := m.findNextPhase(outline, plan.CurrentPhase)
        if nextPhase == nil {
                _ = m.db.ExhaustPlan(ctx, plan.ID)
                m.send("🎉 All phases merged. Plan complete.")
                return
        }

        // 3. Detail the next phase via a fresh Manager LLM call.
        m.send(fmt.Sprintf("✅ Phase %d complete. Detailing Phase %d — %s …", plan.CurrentPhase, nextPhase.Number, nextPhase.Title))
        go m.planNextPhase(context.Background(), plan, outline, *nextPhase)
}

func (m *Manager) findNextPhase(outline []outlineEntry, current int) *outlineEntry {
        var best *outlineEntry
        for i := range outline {
                p := outline[i]
                if p.Number > current {
                        if best == nil || p.Number < best.Number {
                                best = &p
                        }
                }
        }
        return best
}

func (m *Manager) nextQueueableInPhase(ctx context.Context, planID, phase int) *db.Task {
        tasks, err := m.db.GetTopLevelTasksByPhase(ctx, planID, phase)
        if err != nil {
                return nil
        }
        for i := range tasks {
                t := tasks[i]
                if t.Status != "approved" {
                        continue
                }
                if existing, _ := m.db.GetLatestJobForTask(ctx, t.ID); existing != nil {
                        continue
                }
                return &t
        }
        return nil
}

func (m *Manager) queueTask(ctx context.Context, t *db.Task) {
        branch := fmt.Sprintf("feature/task-%d-%s", t.ID, slugify(t.Title))
        if _, err := m.db.CreateBuilderJob(ctx, t.ID, branch); err != nil {
                m.send("⚠️ Could not queue next task: " + err.Error())
                return
        }
        m.send(fmt.Sprintf("➡️ Next task queued: **%s** (phase %d)", t.Title, t.Phase))
        if m.notifyBuilder != nil {
                m.notifyBuilder()
        }
}

// planNextPhase invokes the Manager LLM in next-phase mode, asking it to
// detail one specific phase from the outline. On success it persists the
// new tasks and queues the first one for the Builder.
//
// Liveness: this runs in a goroutine spawned by enqueueNextTaskAfterMerge
// (post-merge). If the Manager is busy handling a concurrent user message,
// we retry with backoff rather than dropping the transition — otherwise the
// next phase would never get planned and the plan would silently stall.
func (m *Manager) planNextPhase(ctx context.Context, plan *db.Plan, outline []outlineEntry, phase outlineEntry) {
        var (
                runCtx context.Context
                cancel context.CancelFunc
                ok     bool
        )
        backoff := time.Second
        for attempt := 0; attempt < 30; attempt++ {
                runCtx, cancel, ok = m.tryStartRun(ctx)
                if ok {
                        break
                }
                select {
                case <-ctx.Done():
                        return
                case <-time.After(backoff):
                }
                if backoff < 10*time.Second {
                        backoff *= 2
                }
        }
        if !ok {
                m.send(fmt.Sprintf("⚠️ Could not detail Phase %d — Manager stayed busy too long. Send any message to retry.", phase.Number))
                return
        }
        defer func() {
                m.finishRun()
                cancel()
        }()
        ctx = runCtx

        docs := m.buildFullDocContext(ctx, 60000)
        history := m.buildMergedHistory(ctx, plan.ID)

        var outlineSB strings.Builder
        for _, p := range outline {
                marker := " "
                if p.Number < phase.Number {
                        marker = "✓"
                } else if p.Number == phase.Number {
                        marker = "▶"
                }
                outlineSB.WriteString(fmt.Sprintf("  %s Phase %d — %s\n", marker, p.Number, p.Title))
        }

        userPrompt := fmt.Sprintf(`PROJECT DOCS (PRD/spec):
%s

GOAL SUMMARY:
%s

PLAN OUTLINE:
%s

ALREADY MERGED:
%s

NEXT PHASE TO DETAIL: Phase %d — %s

Produce the JSON for this phase only (current_phase). Reference what already exists in the merged history; do not duplicate work.`,
                docs,
                fallback(plan.GoalSummary, "(none recorded)"),
                outlineSB.String(),
                history,
                phase.Number, phase.Title,
        )

        m.trace(ctx, "next_phase_llm_request", fmt.Sprintf("phase=%d", phase.Number))
        resp, err := m.llm.ChatJSON(ctx, []llm.Message{
                {Role: "system", Content: nextPhaseSystemPrompt},
                {Role: "user", Content: userPrompt},
        })
        if err != nil {
                m.trace(ctx, "next_phase_llm_error", err.Error())
                m.send(fmt.Sprintf("⚠️ Could not detail Phase %d: %s", phase.Number, err.Error()))
                return
        }
        if len(resp.Choices) == 0 {
                m.send(fmt.Sprintf("⚠️ Empty response detailing Phase %d.", phase.Number))
                return
        }
        raw := resp.Choices[0].Message.Content
        m.trace(ctx, "next_phase_llm_response",
                fmt.Sprintf("body=%s", truncateStr(raw, 300)))

        var d managerDecision
        if err := json.Unmarshal([]byte(cleanJSON(raw)), &d); err != nil {
                m.trace(ctx, "next_phase_parse_error", err.Error())
                m.send("⚠️ Could not parse next-phase plan.")
                return
        }
        if d.CurrentPhase == nil || len(d.CurrentPhase.Tasks) == 0 {
                m.send(fmt.Sprintf("⚠️ Manager returned no tasks for Phase %d.", phase.Number))
                return
        }
        // Force the phase number to match what we asked for.
        d.CurrentPhase.Number = phase.Number
        if strings.TrimSpace(d.CurrentPhase.Title) == "" {
                d.CurrentPhase.Title = phase.Title
        }

        firstID, firstTitle, total := m.persistPhaseTasks(ctx, plan.ID, d.CurrentPhase)
        _ = m.db.SetPlanCurrentPhase(ctx, plan.ID, phase.Number)

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("📋 **Phase %d detailed — %s** (%d task(s))\n", phase.Number, d.CurrentPhase.Title, total))
        for _, t := range d.CurrentPhase.Tasks {
                sb.WriteString("  • ")
                sb.WriteString(t.Title)
                sb.WriteString("\n")
        }
        if firstID > 0 {
                branch := fmt.Sprintf("feature/task-%d-%s", firstID, slugify(firstTitle))
                if _, err := m.db.CreateBuilderJob(ctx, firstID, branch); err != nil {
                        m.send("⚠️ Could not queue first builder job: " + err.Error())
                }
                sb.WriteString(fmt.Sprintf("\nBuilder will start with **%s**.", firstTitle))
        }
        m.send(sb.String())
        if m.notifyBuilder != nil {
                m.notifyBuilder()
        }
}

// buildFullDocContext loads the complete raw content of every project doc,
// concatenated oldest-first, capped at maxBytes total. Unlike BuildDocContext
// (which returns chunked snippets) this gives the Manager the full PRD.
func (m *Manager) buildFullDocContext(ctx context.Context, maxBytes int) string {
        docs, err := m.db.GetAllDocsFull(ctx)
        if err != nil || len(docs) == 0 {
                return "(no documentation uploaded yet)"
        }
        var sb strings.Builder
        sb.WriteString("=== PROJECT DOCUMENTATION ===\n\n")
        for _, d := range docs {
                sb.WriteString(fmt.Sprintf("--- %s ---\n", d.Filename))
                sb.WriteString(d.RawContent)
                sb.WriteString("\n\n")
                if sb.Len() >= maxBytes {
                        sb.WriteString(fmt.Sprintf("\n(…doc context truncated at %d bytes)\n", maxBytes))
                        break
                }
        }
        return sb.String()
}

// buildMergedHistory lists every top-level task in the plan that has been
// merged, with its phase + PR URL. Lets the next-phase planner see what
// already exists so it doesn't duplicate work.
func (m *Manager) buildMergedHistory(ctx context.Context, planID int) string {
        tasks, err := m.db.GetTasksByPlan(ctx, planID)
        if err != nil || len(tasks) == 0 {
                return "(nothing merged yet)"
        }
        var sb strings.Builder
        any := false
        for _, t := range tasks {
                if t.ParentID != nil {
                        continue
                }
                if t.Status != "done" {
                        continue
                }
                any = true
                pr := t.PRURL
                if pr == "" {
                        pr = "(no PR URL)"
                }
                sb.WriteString(fmt.Sprintf("- [Phase %d] %s — %s\n", t.Phase, t.Title, pr))
        }
        if !any {
                return "(nothing merged yet)"
        }
        return sb.String()
}

func fallback(s, def string) string {
        if strings.TrimSpace(s) == "" {
                return def
        }
        return s
}

// formatPlanPreview renders the proposed plan as markdown for user approval:
// goal + full outline (titles only) + the current phase expanded with tasks
// and subtasks (with their descriptions).
func formatPlanPreview(goal string, outline []outlineEntry, current *managerPlanPhase) string {
        var sb strings.Builder
        sb.WriteString("📋 **Proposed plan**\n\n")
        if strings.TrimSpace(goal) != "" {
                sb.WriteString("**Goal:** ")
                sb.WriteString(goal)
                sb.WriteString("\n\n")
        }
        if len(outline) > 0 {
                sb.WriteString("**Outline (all phases):**\n")
                for _, p := range outline {
                        marker := "•"
                        if current != nil && p.Number == current.Number {
                                marker = "▶"
                        }
                        sb.WriteString(fmt.Sprintf("%s Phase %d — %s\n", marker, p.Number, p.Title))
                }
                sb.WriteString("\n")
        }
        if current != nil {
                sb.WriteString(fmt.Sprintf("**Phase %d — %s** (detailed now; later phases get detailed after this merges):\n",
                        current.Number, current.Title))
                for i, t := range current.Tasks {
                        sb.WriteString(fmt.Sprintf("  %d. **%s** — %s\n", i+1, t.Title, truncateStr(t.Description, 240)))
                        for j, st := range t.Subtasks {
                                sb.WriteString(fmt.Sprintf("       %d.%d %s — %s\n", i+1, j+1, st.Title, truncateStr(st.Description, 180)))
                        }
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
                // Sequential phase execution: now that this PR is merged we
                // are safe from merge conflicts, so enqueue the NEXT task in
                // the plan (and only that one — Builder will pick it up).
                m.enqueueNextTaskAfterMerge(ctx)
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

// pauseKey scopes the pause flag per active project so pausing one project
// no longer freezes every other project's manager + builder.
func (m *Manager) pauseKey(ctx context.Context) string {
        return fmt.Sprintf("agent_paused:%d", m.db.ActiveProjectID(ctx))
}

func (m *Manager) IsPaused(ctx context.Context) bool {
        return m.db.KVGetDefault(ctx, m.pauseKey(ctx), "0") == "1"
}

func (m *Manager) Pause(ctx context.Context) {
        _ = m.db.KVSet(ctx, m.pauseKey(ctx), "1")
        _ = m.db.UpdateProjectStatus(ctx, "paused")
        m.send("⏸ Agent paused.")
        log.Printf("[manager] paused (project %d)", m.db.ActiveProjectID(ctx))
}

func (m *Manager) Resume(ctx context.Context) {
        _ = m.db.KVSet(ctx, m.pauseKey(ctx), "0")
        _ = m.db.UpdateProjectStatus(ctx, "ready")
        m.send("▶️ Agent resumed.")
        log.Printf("[manager] resumed (project %d)", m.db.ActiveProjectID(ctx))
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
