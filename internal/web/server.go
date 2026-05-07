package web

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "strings"
        "sync"
        "time"

        "strconv"

        "github.com/cto-agent/cto-agent/internal/db"
)

// Agent is the interface the web server needs — avoids circular imports.
type Agent interface {
        RunBuilderLoop(ctx context.Context)
        HandleUserMessage(ctx context.Context, text string)
        Cancel(ctx context.Context)
        Pause(ctx context.Context)
        Resume(ctx context.Context)
        IngestDoc(ctx context.Context, filename, content string) (int, error)
        GetStatus(ctx context.Context) (string, float64)
}

// ─── SSE hub ───────────────────────────────────────────────────────────────

type sseClient struct {
        ch chan string
}

type sseHub struct {
        mu      sync.Mutex
        clients map[*sseClient]struct{}
}

func newHub() *sseHub {
        return &sseHub{clients: make(map[*sseClient]struct{})}
}

func (h *sseHub) add(c *sseClient) {
        h.mu.Lock()
        h.clients[c] = struct{}{}
        h.mu.Unlock()
}

func (h *sseHub) remove(c *sseClient) {
        h.mu.Lock()
        delete(h.clients, c)
        h.mu.Unlock()
}

func (h *sseHub) broadcast(payload string) {
        h.mu.Lock()
        defer h.mu.Unlock()
        for c := range h.clients {
                select {
                case c.ch <- payload:
                default:
                }
        }
}

// ─── Server ────────────────────────────────────────────────────────────────

// Server is the embedded web UI + API server.
type Server struct {
        db    *db.DB
        agent Agent
        hub   *sseHub

        mu              sync.Mutex
        pending         chan string
        activeAskCancel chan struct{}
        askActive       bool
        pendingQuestion string // in-memory copy of active ask question
        motd            string // shown to each new SSE client on connect
}

// New creates a new Server (does not listen yet).
func New(database *db.DB) *Server {
        return &Server{
                db:      database,
                hub:     newHub(),
                pending: make(chan string, 1),
        }
}

// SetAgent wires the agent after construction (breaks init cycle).
func (s *Server) SetAgent(a Agent) { s.agent = a }

// SetMOTD sets a message-of-the-day that is pushed to each new SSE client on connect.
func (s *Server) SetMOTD(msg string) {
        s.mu.Lock()
        s.motd = msg
        s.mu.Unlock()
}

// Send broadcasts a text message to all connected browsers via SSE and persists it.
func (s *Server) Send(text string) {
        _ = s.db.AddMessage(context.Background(), "message", text)
        s.hub.broadcast(s.sseMsg("message", text))
        log.Printf("[web] send: %s", trunc(text, 80))
}

// SendPRReview broadcasts a pr_review event to all SSE clients.
// payload contains everything the UI needs to render the approval card.
func (s *Server) SendPRReview(jobID int, taskTitle, prURL, managerNote, diffSummary string) {
        payload := map[string]interface{}{
                "type":         "pr_review",
                "job_id":       jobID,
                "task_title":   taskTitle,
                "pr_url":       prURL,
                "manager_note": managerNote,
                "diff_summary": diffSummary,
                "ts":           time.Now().Format("15:04:05"),
        }
        data, _ := json.Marshal(payload)
        _ = s.db.AddMessage(context.Background(), "pr_review", string(data))
        s.hub.broadcast("event: msg\ndata: " + string(data) + "\n\n")
}

// SendBuilderStatus broadcasts a builder_status event.
// Called by the builder at key milestones.
func (s *Server) SendBuilderStatus(taskTitle, milestone, detail string) {
        payload := map[string]string{
                "type":       "builder_status",
                "task_title": taskTitle,
                "milestone":  milestone,
                "detail":     detail,
                "ts":         time.Now().Format("15:04:05"),
        }
        data, _ := json.Marshal(payload)
        s.hub.broadcast("event: msg\ndata: " + string(data) + "\n\n")
}

// Ask broadcasts a question event and blocks until the browser POSTs a reply.

func (s *Server) Ask(question string) string {
        cancelCh := make(chan struct{})
        s.mu.Lock()
        s.askActive = true
        s.pendingQuestion = question
        s.activeAskCancel = cancelCh
        s.mu.Unlock()

        _ = s.db.KVSet(context.Background(), "pending_ask", question)

        defer func() {
                s.mu.Lock()
                s.askActive = false
                s.pendingQuestion = ""
                s.activeAskCancel = nil
                s.mu.Unlock()
                _ = s.db.KVSet(context.Background(), "pending_ask", "")
                s.hub.broadcast("event: ask_done\ndata: {}\n\n")
        }()

        _ = s.db.AddMessage(context.Background(), "ask", question)
        s.hub.broadcast(s.sseMsg("ask", question))
        log.Printf("[web] ask: %s", trunc(question, 80))

        select {
        case reply := <-s.pending:
                return reply
        case <-cancelCh:
                return ""
        case <-time.After(10 * time.Minute):
                s.Send("⏰ No reply in 10 min — using best judgment and continuing.")
                return ""
        }
}

// CancelAsk unblocks an in-flight Ask() call (returning ""), so a Stop
// from the user can immediately free the manager goroutine.
func (s *Server) CancelAsk() {
        s.mu.Lock()
        ch := s.activeAskCancel
        s.mu.Unlock()
        if ch == nil {
                return
        }
        defer func() { _ = recover() }() // already-closed channel is fine
        close(ch)
}

// sseMsg formats a JSON payload as an SSE "msg" event.
func (s *Server) sseMsg(typ, text string) string {
        data, _ := json.Marshal(map[string]string{
                "type": typ,
                "text": text,
                "ts":   time.Now().Format("15:04:05"),
        })
        return "event: msg\ndata: " + string(data) + "\n\n"
}

// broadcastStatus pushes a fresh status event to all connected clients.
func (s *Server) broadcastStatus(ctx context.Context) {
        if payload, err := s.buildStatusJSON(ctx); err == nil {
                s.hub.broadcast("event: status\ndata: " + payload + "\n\n")
        }
}

// Start registers routes and listens on :5000. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) {
        mux := http.NewServeMux()

        // Public — no session required
        mux.HandleFunc("/", s.handleIndex)
        mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
        mux.HandleFunc("/api/auth/setup", s.handleAuthSetup)
        mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
        mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)

        // Protected — valid session cookie required
        mux.HandleFunc("/events", s.requireAuth(s.handleSSE))
        mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
        mux.HandleFunc("/api/log", s.requireAuth(s.handleLog))
        mux.HandleFunc("/api/usage", s.requireAuth(s.handleUsage))
        mux.HandleFunc("/api/clear", s.requireAuth(s.handleClear))
        mux.HandleFunc("/api/pause", s.requireAuth(s.handlePause))
        mux.HandleFunc("/api/resume", s.requireAuth(s.handleResume))
        mux.HandleFunc("/api/cancel", s.requireAuth(s.handleCancel))
        mux.HandleFunc("/api/reply", s.requireAuth(s.handleReply))
        mux.HandleFunc("/api/chat", s.requireAuth(s.handleChat))
        mux.HandleFunc("/api/upload", s.requireAuth(s.handleUpload))
        mux.HandleFunc("/api/docs", s.requireAuth(s.handleDocs))
        mux.HandleFunc("/api/docs/", s.requireAuth(s.handleDocByID))
        mux.HandleFunc("/api/builder", s.requireAuth(s.handleBuilder))

        srv := &http.Server{
                Addr:    "0.0.0.0:5000",
                Handler: mux,
        }

        go func() {
                <-ctx.Done()
                _ = srv.Shutdown(context.Background())
        }()

        log.Println("[web] listening on :5000")
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                log.Printf("[web] server error: %v", err)
        }
}

// ─── UI ────────────────────────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                http.NotFound(w, r)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, indexHTML)
}

// ─── SSE stream ────────────────────────────────────────────────────────────

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
        flusher, ok := w.(http.Flusher)
        if !ok {
                http.Error(w, "streaming not supported", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("X-Accel-Buffering", "no")

        client := &sseClient{ch: make(chan string, 64)}
        s.hub.add(client)
        defer s.hub.remove(client)

        // Greet new connection with current status
        if payload, err := s.buildStatusJSON(r.Context()); err == nil {
                fmt.Fprintf(w, "event: status\ndata: %s\n\n", payload)
                flusher.Flush()
        }

        // Replay recent message history so the browser feed is populated on connect.
        // Track the role of the last replayed message to avoid duplicate ask bubbles.
        lastHistoryRole := ""
        if history, err := s.db.GetRecentMessages(r.Context(), 200); err == nil && len(history) > 0 {
                for _, m := range history {
                        var data []byte
                        if m.Role == "pr_review" && json.Valid([]byte(m.Content)) {
                                data = []byte(m.Content)
                        } else {
                                data, _ = json.Marshal(map[string]string{
                                        "type": m.Role,
                                        "text": m.Content,
                                        "ts":   m.Timestamp,
                                })
                        }
                        fmt.Fprintf(w, "event: msg\ndata: %s\n\n", data)
                        lastHistoryRole = m.Role
                }
                fmt.Fprint(w, "event: history_end\ndata: {}\n\n")
                flusher.Flush()
        }

        // Send message-of-the-day if set (e.g. "no LLM keys configured")
        s.mu.Lock()
        motd := s.motd
        s.mu.Unlock()
        if motd != "" {
                fmt.Fprint(w, s.sseMsg("message", motd))
                flusher.Flush()
        }

        // Re-broadcast any pending ask to the new client.
        // Check in-memory first (ask is live in this process); fall back to KV
        // which survives server restarts.
        // Skip resending the question text if history replay already surfaced it
        // as the last message (prevents a duplicate bubble in the feed).
        persistedAsk := s.db.KVGetDefault(r.Context(), "pending_ask", "")
        s.mu.Lock()
        active := s.askActive
        question := s.pendingQuestion
        s.mu.Unlock()

        if active {
                if lastHistoryRole != "ask" {
                        // Ask is live but history didn't end on an ask — show the question.
                        fmt.Fprint(w, s.sseMsg("ask", question))
                        flusher.Flush()
                }
                // If history already ended with the ask bubble the JS already set
                // askActive = true via the replayed msg event — nothing more to send.
        } else if persistedAsk != "" {
                // Server restarted while agent was mid-ask — let the user know and
                // surface the question so they can still reply once the agent loop
                // re-issues it.
                fmt.Fprint(w, s.sseMsg("message", "🔄 Reconnected — agent is resuming. Pending question:"))
                fmt.Fprint(w, s.sseMsg("ask", persistedAsk))
                flusher.Flush()
        }

        tick := time.NewTicker(20 * time.Second)
        defer tick.Stop()

        for {
                select {
                case <-r.Context().Done():
                        return
                case payload, ok := <-client.ch:
                        if !ok {
                                return
                        }
                        fmt.Fprint(w, payload)
                        flusher.Flush()
                case <-tick.C:
                        fmt.Fprint(w, ": heartbeat\n\n")
                        flusher.Flush()
                }
        }
}

// ─── REST handlers ─────────────────────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
        payload, err := s.buildStatusJSON(r.Context())
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, payload)
}

func (s *Server) buildStatusJSON(ctx context.Context) (string, error) {
        name := "default"
        if proj, err := s.db.GetProject(ctx); err == nil {
                name = proj.Name
        }

        state, trust := "new", 0.0
        if s.agent != nil {
                state, trust = s.agent.GetStatus(ctx)
        }

        plan, _ := s.db.GetActivePlan(ctx)
        planInfo := "none"
        if plan != nil {
                tasks, _ := s.db.GetTasksByPlan(ctx, plan.ID)
                done, total := 0, 0
                for _, t := range tasks {
                        if t.ParentID == nil {
                                total++
                                if t.Status == "done" {
                                        done++
                                }
                        }
                }
                planInfo = fmt.Sprintf("v%d — %d/%d tasks done", plan.Version, done, total)
        }

        out, err := json.Marshal(map[string]interface{}{
                "project": name,
                "state":   state,
                "trust":   trust,
                "plan":    planInfo,
        })
        return string(out), err
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
        logs, _ := s.db.GetGlobalRecentLogs(r.Context(), 10)
        type item struct {
                Time  string `json:"time"`
                Event string `json:"event"`
                Text  string `json:"text"`
        }
        items := []item{}
        for i := len(logs) - 1; i >= 0; i-- {
                l := logs[i]
                items = append(items, item{l.CreatedAt.Format("15:04:05"), l.Event, trunc(l.Payload, 120)})
        }
        jsonOK(w, map[string]interface{}{"logs": items})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        all, _ := s.db.GetUsageSummary(ctx)
        today, _ := s.db.GetUsageToday(ctx)
        jsonOK(w, map[string]interface{}{"all": all, "today": today})
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if err := s.db.ClearMessages(r.Context()); err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        s.Send("🧹 Chat history cleared.")
        jsonOK(w, map[string]string{"ok": "cleared"})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        s.agent.Pause(r.Context())
        s.broadcastStatus(r.Context())
        jsonOK(w, map[string]string{"ok": "paused"})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        s.agent.Resume(r.Context())
        s.broadcastStatus(r.Context())
        jsonOK(w, map[string]string{"ok": "resuming"})
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }

        s.mu.Lock()
        active := s.askActive
        s.mu.Unlock()

        if !active {
                jsonErr(w, "no active question", http.StatusBadRequest)
                return
        }

        body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
        var payload struct {
                Text string `json:"text"`
        }
        if err := json.Unmarshal(body, &payload); err != nil || payload.Text == "" {
                _ = r.ParseForm()
                payload.Text = strings.TrimSpace(r.FormValue("text"))
        }
        payload.Text = strings.TrimSpace(payload.Text)

        if payload.Text == "" {
                jsonErr(w, "empty reply", http.StatusBadRequest)
                return
        }

        select {
        case s.pending <- payload.Text:
                _ = s.db.AddMessage(r.Context(), "reply", "You: "+payload.Text)
                s.hub.broadcast(s.sseMsg("reply", "You: "+payload.Text))
                jsonOK(w, map[string]string{"ok": "sent"})
        default:
                jsonErr(w, "no question pending", http.StatusConflict)
        }
}

// handleChat accepts a free-form user message at any time. If a question is
// currently active it is routed through the reply channel so the agent can
// continue; otherwise the message is just stored and broadcast so it shows in
// the feed.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }

        body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
        var payload struct {
                Text string `json:"text"`
        }
        if err := json.Unmarshal(body, &payload); err != nil || payload.Text == "" {
                _ = r.ParseForm()
                payload.Text = strings.TrimSpace(r.FormValue("text"))
        }
        payload.Text = strings.TrimSpace(payload.Text)

        if payload.Text == "" {
                jsonErr(w, "empty message", http.StatusBadRequest)
                return
        }

        s.mu.Lock()
        active := s.askActive
        s.mu.Unlock()

        if active {
                select {
                case s.pending <- payload.Text:
                        _ = s.db.AddMessage(r.Context(), "reply", "You: "+payload.Text)
                        s.hub.broadcast(s.sseMsg("reply", "You: "+payload.Text))
                        jsonOK(w, map[string]string{"ok": "sent"})
                        return
                default:
                }
        }

        _ = s.db.AddMessage(r.Context(), "reply", "You: "+payload.Text)
        s.hub.broadcast(s.sseMsg("reply", "You: "+payload.Text))

        // No active question — this is a fresh request to the planner. Run it
        // in a background goroutine so the HTTP handler returns immediately.
        if s.agent != nil {
                text := payload.Text
                go s.agent.HandleUserMessage(context.Background(), text)
        }
        jsonOK(w, map[string]string{"ok": "queued"})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        s.agent.Cancel(r.Context())
        s.CancelAsk()
        jsonOK(w, map[string]string{"ok": "cancelled"})
}

func (s *Server) handleBuilder(w http.ResponseWriter, r *http.Request) {
        jobs, err := s.db.ListRecentBuilderJobs(r.Context(), 12)
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        type item struct {
                ID        int    `json:"id"`
                TaskTitle string `json:"task_title"`
                Status    string `json:"status"`
                Branch    string `json:"branch"`
                PRURL     string `json:"pr_url"`
                Updated   string `json:"updated"`
        }
        out := []item{}
        for _, j := range jobs {
                out = append(out, item{
                        ID:        j.ID,
                        TaskTitle: j.TaskTitle,
                        Status:    j.Status,
                        Branch:    j.Branch,
                        PRURL:     j.PRURL,
                        Updated:   j.UpdatedAt.Format("2006-01-02 15:04"),
                })
        }
        jsonOK(w, map[string]interface{}{"jobs": out})
}

func (s *Server) handleDocByID(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodDelete {
                http.Error(w, "DELETE required", http.StatusMethodNotAllowed)
                return
        }
        idStr := strings.TrimPrefix(r.URL.Path, "/api/docs/")
        idStr = strings.TrimSuffix(idStr, "/")
        id, err := strconv.Atoi(idStr)
        if err != nil || id <= 0 {
                jsonErr(w, "invalid id", http.StatusBadRequest)
                return
        }
        if err := s.db.DeleteDoc(r.Context(), id); err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        jsonOK(w, map[string]string{"ok": "deleted"})
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
        docs, err := s.db.ListDocs(r.Context())
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        type item struct {
                ID       int    `json:"id"`
                Filename string `json:"filename"`
                Uploaded string `json:"uploaded"`
        }
        out := []item{}
        for _, d := range docs {
                out = append(out, item{d.ID, d.Filename, d.CreatedAt.Format("2006-01-02 15:04")})
        }
        jsonOK(w, map[string]interface{}{"docs": out})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }

        ctx := r.Context()
        if err := r.ParseMultipartForm(10 << 20); err != nil {
                jsonErr(w, "parse multipart: "+err.Error(), http.StatusBadRequest)
                return
        }

        file, header, err := r.FormFile("file")
        if err != nil {
                jsonErr(w, "file field missing", http.StatusBadRequest)
                return
        }
        defer file.Close()

        if !strings.HasSuffix(strings.ToLower(header.Filename), ".md") {
                jsonErr(w, "only .md files are accepted", http.StatusBadRequest)
                return
        }

        data, err := io.ReadAll(io.LimitReader(file, 5<<20))
        if err != nil {
                jsonErr(w, "read file: "+err.Error(), http.StatusInternalServerError)
                return
        }

        if s.agent == nil {
                jsonErr(w, "agent not configured — add LLM API keys and restart", http.StatusServiceUnavailable)
                return
        }

        s.Send(fmt.Sprintf("📄 Received **%s** — ingesting...", header.Filename))

        chunks, err := s.agent.IngestDoc(ctx, header.Filename, string(data))
        if err != nil {
                s.Send(fmt.Sprintf("❌ Ingest failed: %v", err))
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }

        s.Send(fmt.Sprintf("✅ **%s** ingested — %d chunks ready.", header.Filename, chunks))

        jsonOK(w, map[string]interface{}{"chunks": chunks, "filename": header.Filename})
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v interface{}) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(code)
        _ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func trunc(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "..."
}
