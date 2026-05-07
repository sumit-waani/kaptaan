package web

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "strconv"
        "strings"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/jackc/pgx/v5"
)

type projectDTO struct {
        ID          int    `json:"id"`
        Name        string `json:"name"`
        Status      string `json:"status"`
        RepoURL     string `json:"repo_url"`
        GithubToken string `json:"github_token"`
        HasToken    bool   `json:"has_token"`
        Active      bool   `json:"active"`
        CreatedAt   string `json:"created_at"`
}

func toDTO(p db.Project, activeID int) projectDTO {
        return projectDTO{
                ID:          p.ID,
                Name:        p.Name,
                Status:      p.Status,
                RepoURL:     p.RepoURL,
                GithubToken: maskToken(p.GithubToken),
                HasToken:    strings.TrimSpace(p.GithubToken) != "",
                Active:      p.ID == activeID,
                CreatedAt:   p.CreatedAt.Format("2006-01-02 15:04"),
        }
}

func maskToken(t string) string {
        t = strings.TrimSpace(t)
        if t == "" {
                return ""
        }
        if len(t) <= 6 {
                return "••••"
        }
        return t[:3] + "…" + t[len(t)-3:]
}

// GET /api/projects        — list
// POST /api/projects       — create  {name, repo_url, github_token}
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        switch r.Method {
        case http.MethodGet:
                ps, err := s.db.ListProjects(ctx)
                if err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                activeID := s.db.ActiveProjectID(ctx)
                out := make([]projectDTO, 0, len(ps))
                for _, p := range ps {
                        out = append(out, toDTO(p, activeID))
                }
                jsonOK(w, map[string]interface{}{"projects": out, "active_id": activeID})
        case http.MethodPost:
                var body struct {
                        Name        string `json:"name"`
                        RepoURL     string `json:"repo_url"`
                        GithubToken string `json:"github_token"`
                        Activate    bool   `json:"activate"`
                }
                raw, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
                if err := json.Unmarshal(raw, &body); err != nil {
                        jsonErr(w, "invalid json", http.StatusBadRequest)
                        return
                }
                body.Name = strings.TrimSpace(body.Name)
                if body.Name == "" {
                        jsonErr(w, "name is required", http.StatusBadRequest)
                        return
                }
                p, err := s.db.CreateProjectFull(ctx, body.Name,
                        strings.TrimSpace(body.RepoURL), strings.TrimSpace(body.GithubToken))
                if err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                if body.Activate {
                        _ = s.db.SetActiveProject(ctx, p.ID)
                        s.BroadcastStatus(ctx)
                        s.BroadcastBuilderState(ctx)
                }
                s.Send(fmt.Sprintf("📁 Project created: **%s**", p.Name))
                jsonOK(w, map[string]interface{}{"project": toDTO(*p, s.db.ActiveProjectID(ctx))})
        default:
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
}

// PATCH  /api/projects/{id}              — update name + repo
// DELETE /api/projects/{id}              — delete
// POST   /api/projects/{id}/activate     — switch
// POST   /api/projects/{id}/clear        — wipe data, keep project row
func (s *Server) handleProjectByID(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
        rest = strings.Trim(rest, "/")
        parts := strings.Split(rest, "/")
        if len(parts) == 0 || parts[0] == "" {
                http.NotFound(w, r)
                return
        }
        id, err := strconv.Atoi(parts[0])
        if err != nil || id <= 0 {
                jsonErr(w, "invalid project id", http.StatusBadRequest)
                return
        }
        subAction := ""
        if len(parts) > 1 {
                subAction = parts[1]
        }

        switch {
        case subAction == "activate" && r.Method == http.MethodPost:
                if _, err := s.db.GetProjectByID(ctx, id); err != nil {
                        jsonErr(w, "project not found", http.StatusNotFound)
                        return
                }
                if err := s.db.SetActiveProject(ctx, id); err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                // Kill any in-flight ask + in-memory pending question — it
                // belongs to the previous project's conversation.
                s.clearPendingAsk()
                s.BroadcastStatus(ctx)
                s.BroadcastBuilderState(ctx)
                s.Send(fmt.Sprintf("🔀 Active project switched to **#%d**", id))
                jsonOK(w, map[string]interface{}{"ok": "activated", "active_id": id})

        case subAction == "clear" && r.Method == http.MethodPost:
                wasActive := s.db.ActiveProjectID(ctx) == id
                if err := s.db.ClearProjectData(ctx, id); err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                if wasActive {
                        s.clearPendingAsk()
                }
                s.BroadcastStatus(ctx)
                s.BroadcastBuilderState(ctx)
                s.Send(fmt.Sprintf("🧹 Cleared project #%d data (chats, plans, jobs, docs).", id))
                jsonOK(w, map[string]string{"ok": "cleared"})

        case subAction == "" && r.Method == http.MethodPatch:
                var body struct {
                        Name        string  `json:"name"`
                        RepoURL     string  `json:"repo_url"`
                        GithubToken *string `json:"github_token"`
                }
                raw, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
                if err := json.Unmarshal(raw, &body); err != nil {
                        jsonErr(w, "invalid json", http.StatusBadRequest)
                        return
                }
                existing, err := s.db.GetProjectByID(ctx, id)
                if err != nil {
                        jsonErr(w, "project not found", http.StatusNotFound)
                        return
                }
                name := strings.TrimSpace(body.Name)
                if name == "" {
                        name = existing.Name
                }
                repo := strings.TrimSpace(body.RepoURL)
                // nil token => keep existing; empty string => clear; non-empty => set.
                token := existing.GithubToken
                if body.GithubToken != nil {
                        token = strings.TrimSpace(*body.GithubToken)
                }
                if err := s.db.UpdateProjectRepo(ctx, id, name, repo, token); err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                s.BroadcastStatus(ctx)
                jsonOK(w, map[string]string{"ok": "updated"})

        case subAction == "" && r.Method == http.MethodDelete:
                // Capture active-ness BEFORE the row is gone — ActiveProjectID()
                // silently falls back to 1 once the stored id no longer exists,
                // which would otherwise mask the "we deleted the active one" case.
                wasActive := s.db.ActiveProjectID(ctx) == id
                if err := s.db.DeleteProject(ctx, id); err != nil {
                        if errors.Is(err, db.ErrLastProject) {
                                jsonErr(w, err.Error(), http.StatusBadRequest)
                                return
                        }
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                if wasActive {
                        // Always reassign to a real remaining project; never let
                        // the KV pointer dangle (the silent fallback to id=1 made
                        // recreated projects inherit project-1 chat history).
                        s.clearPendingAsk()
                        if ps, err := s.db.ListProjects(ctx); err == nil && len(ps) > 0 {
                                _ = s.db.SetActiveProject(ctx, ps[0].ID)
                        }
                }
                s.BroadcastStatus(ctx)
                s.BroadcastBuilderState(ctx)
                s.Send(fmt.Sprintf("🗑 Project #%d deleted.", id))
                jsonOK(w, map[string]string{"ok": "deleted"})

        default:
                http.Error(w, "method/path not allowed", http.StatusMethodNotAllowed)
        }
}

// ─── Builder live state ─────────────────────────────────────────────────────

type builderStateMilestone struct {
        Time    string `json:"time"`
        Event   string `json:"event"`
        Payload string `json:"payload"`
}

type builderStateRunning struct {
        JobID       int                     `json:"job_id"`
        TaskID      int                     `json:"task_id"`
        TaskTitle   string                  `json:"task_title"`
        TaskDesc    string                  `json:"task_desc"`
        Branch      string                  `json:"branch"`
        StartedAt   string                  `json:"started_at"`
        RetryCount  int                     `json:"retry_count"`
        MaxRetries  int                     `json:"max_retries"`
        BuildOutput string                  `json:"build_output"`
        TestOutput  string                  `json:"test_output"`
        Subtasks    []builderStateSubtask   `json:"subtasks"`
        Milestones  []builderStateMilestone `json:"milestones"`
}

type builderStateSubtask struct {
        ID     int    `json:"id"`
        Title  string `json:"title"`
        Status string `json:"status"`
}

type builderStateQueueItem struct {
        JobID     int    `json:"job_id"`
        TaskID    int    `json:"task_id"`
        TaskTitle string `json:"task_title"`
        Branch    string `json:"branch"`
        Status    string `json:"status,omitempty"`
        PRURL     string `json:"pr_url,omitempty"`
}

type builderStateFailed struct {
        JobID       int    `json:"job_id"`
        TaskID      int    `json:"task_id"`
        TaskTitle   string `json:"task_title"`
        Branch      string `json:"branch"`
        BuildOutput string `json:"build_output"`
        TestOutput  string `json:"test_output"`
        RetryCount  int    `json:"retry_count"`
        FailedAt    string `json:"failed_at"`
}

type builderStatePayload struct {
        Running *builderStateRunning    `json:"running"`
        Queue   []builderStateQueueItem `json:"queue"`
        Recent  []builderStateQueueItem `json:"recent"`
        Failed  *builderStateFailed     `json:"failed,omitempty"`
        Updated string                  `json:"updated"`
}

func (s *Server) handleBuilderState(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        payload, err := s.buildBuilderStateJSON(r.Context())
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, payload)
}

func (s *Server) buildBuilderStateJSON(ctx context.Context) (string, error) {
        out := builderStatePayload{
                Updated: time.Now().Format("15:04:05"),
        }

        if running, err := s.db.GetRunningJob(ctx); err == nil && running != nil {
                task, _ := s.db.GetTask(ctx, running.TaskID)
                title := "(unknown task)"
                desc := ""
                if task != nil {
                        title = task.Title
                        desc = task.Description
                }
                startedAt := ""
                if running.StartedAt != nil {
                        startedAt = running.StartedAt.Format("15:04:05")
                }
                r := &builderStateRunning{
                        JobID:       running.ID,
                        TaskID:      running.TaskID,
                        TaskTitle:   title,
                        TaskDesc:    desc,
                        Branch:      running.Branch,
                        StartedAt:   startedAt,
                        RetryCount:  running.RetryCount,
                        MaxRetries:  3, // matches maxJobRetries+1 in builder.go
                        BuildOutput: trunc(running.BuildOutput, 4000),
                        TestOutput:  trunc(running.TestOutput, 4000),
                }
                if subs, err := s.db.GetSubtasks(ctx, running.TaskID); err == nil {
                        for _, st := range subs {
                                r.Subtasks = append(r.Subtasks, builderStateSubtask{
                                        ID: st.ID, Title: st.Title, Status: st.Status,
                                })
                        }
                }
                if logs, err := s.db.GetRecentLogs(ctx, running.TaskID, 50); err == nil {
                        for i := len(logs) - 1; i >= 0; i-- {
                                l := logs[i]
                                r.Milestones = append(r.Milestones, builderStateMilestone{
                                        Time:    l.CreatedAt.Format("15:04:05"),
                                        Event:   l.Event,
                                        Payload: trunc(l.Payload, 240),
                                })
                        }
                }
                out.Running = r
        }

        if queued, err := s.db.ListQueuedJobs(ctx); err == nil {
                for _, q := range queued {
                        out.Queue = append(out.Queue, builderStateQueueItem{
                                JobID: q.ID, TaskID: q.TaskID, TaskTitle: q.TaskTitle, Branch: q.Branch,
                        })
                }
        }

        if recent, err := s.db.ListRecentBuilderJobs(ctx, 10); err == nil {
                for _, j := range recent {
                        if out.Running != nil && j.ID == out.Running.JobID {
                                continue
                        }
                        out.Recent = append(out.Recent, builderStateQueueItem{
                                JobID: j.ID, TaskID: j.TaskID, TaskTitle: j.TaskTitle,
                                Branch: j.Branch, Status: j.Status, PRURL: j.PRURL,
                        })
                        // Surface the most recent failure (if any) with its full
                        // build/test output so the user can see WHY it failed.
                        if out.Failed == nil && j.Status == "failed" {
                                if fj, err := s.db.GetBuilderJob(ctx, j.ID); err == nil && fj != nil {
                                        finishedAt := ""
                                        if fj.FinishedAt != nil {
                                                finishedAt = fj.FinishedAt.Format("15:04:05")
                                        }
                                        out.Failed = &builderStateFailed{
                                                JobID:       fj.ID,
                                                TaskID:      fj.TaskID,
                                                TaskTitle:   j.TaskTitle,
                                                Branch:      fj.Branch,
                                                BuildOutput: trunc(fj.BuildOutput, 4000),
                                                TestOutput:  trunc(fj.TestOutput, 4000),
                                                RetryCount:  fj.RetryCount,
                                                FailedAt:    finishedAt,
                                        }
                                }
                        }
                }
        }

        data, err := json.Marshal(out)
        if err != nil {
                return "", err
        }
        return string(data), nil
}

// suppress unused import warning if pgx is later removed
var _ = pgx.ErrNoRows

// ─── Plan tree (Phase → Task → Job) ─────────────────────────────────────────

type planTreeJob struct {
        JobID      int    `json:"job_id"`
        Status     string `json:"status"`
        Branch     string `json:"branch"`
        PRURL      string `json:"pr_url"`
        RetryCount int    `json:"retry_count"`
}

type planTreeTask struct {
        ID          int            `json:"id"`
        Title       string         `json:"title"`
        Description string         `json:"description"`
        Status      string         `json:"status"`
        PRURL       string         `json:"pr_url"`
        Subtasks    []planTreeTask `json:"subtasks"`
        Job         *planTreeJob   `json:"job,omitempty"`
}

type planTreePhase struct {
        Number int            `json:"number"`
        Tasks  []planTreeTask `json:"tasks"`
}

type planTreePayload struct {
        Plan          *planTreeMeta   `json:"plan"`
        Phases        []planTreePhase `json:"phases"`
        Empty         bool            `json:"empty"`
        RepoConnected bool            `json:"repo_connected"`
}

type planTreeMeta struct {
        ID      int    `json:"id"`
        Version int    `json:"version"`
        Status  string `json:"status"`
}

func (s *Server) handlePlanTree(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        ctx := r.Context()
        out := planTreePayload{Empty: true}

        if proj, err := s.db.GetProject(ctx); err == nil && proj != nil {
                out.RepoConnected = strings.TrimSpace(proj.RepoURL) != "" && strings.TrimSpace(proj.GithubToken) != ""
        }

        plan, err := s.db.GetActivePlan(ctx)
        if err != nil || plan == nil {
                jsonOK(w, out)
                return
        }
        out.Plan = &planTreeMeta{ID: plan.ID, Version: plan.Version, Status: plan.Status}
        out.Empty = false

        tasks, err := s.db.GetTasksByPlan(ctx, plan.ID)
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }

        // Index by phase, separating top-level from subtasks.
        phaseMap := map[int]*planTreePhase{}
        topByID := map[int]*planTreeTask{}
        var phaseOrder []int
        for _, t := range tasks {
                if t.ParentID != nil {
                        continue
                }
                p, ok := phaseMap[t.Phase]
                if !ok {
                        p = &planTreePhase{Number: t.Phase}
                        phaseMap[t.Phase] = p
                        phaseOrder = append(phaseOrder, t.Phase)
                }
                pt := planTreeTask{
                        ID: t.ID, Title: t.Title, Description: t.Description,
                        Status: t.Status, PRURL: t.PRURL,
                }
                if job, err := s.db.GetLatestJobForTask(ctx, t.ID); err == nil && job != nil {
                        pt.Job = &planTreeJob{
                                JobID: job.ID, Status: job.Status, Branch: job.Branch,
                                PRURL: job.PRURL, RetryCount: job.RetryCount,
                        }
                }
                p.Tasks = append(p.Tasks, pt)
                topByID[t.ID] = &p.Tasks[len(p.Tasks)-1]
        }
        // Attach subtasks.
        for _, t := range tasks {
                if t.ParentID == nil {
                        continue
                }
                parent, ok := topByID[*t.ParentID]
                if !ok {
                        continue
                }
                parent.Subtasks = append(parent.Subtasks, planTreeTask{
                        ID: t.ID, Title: t.Title, Description: t.Description,
                        Status: t.Status, PRURL: t.PRURL,
                })
        }
        // Stable phase order (sorted numerically, but phaseOrder already
        // captured insert order which equals tasks' order which already came
        // back ordered by phase, id from the SQL).
        for _, n := range phaseOrder {
                out.Phases = append(out.Phases, *phaseMap[n])
        }

        jsonOK(w, out)
}
