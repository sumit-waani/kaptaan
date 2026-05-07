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

        "github.com/cto-agent/cto-agent/internal/db"
)

// Agent is the interface the web server needs — avoids circular imports.
type Agent interface {
        Run(ctx context.Context)
        Pause(ctx context.Context)
        Resume(ctx context.Context)
        IngestDoc(ctx context.Context, filename, content string) (int, error)
        ScanRepo(ctx context.Context) (string, error)
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

// Send broadcasts a text message to all connected browsers via SSE.
func (s *Server) Send(text string) {
        s.hub.broadcast(s.sseMsg("message", text))
        log.Printf("[web] send: %s", trunc(text, 80))
}

// Ask broadcasts a question event and blocks until the browser POSTs a reply.
// The question is persisted to KV so it survives a server restart.
func (s *Server) Ask(question string) string {
        s.mu.Lock()
        s.askActive = true
        s.pendingQuestion = question
        s.mu.Unlock()

        _ = s.db.KVSet(context.Background(), "pending_ask", question)

        defer func() {
                s.mu.Lock()
                s.askActive = false
                s.pendingQuestion = ""
                s.mu.Unlock()
                _ = s.db.KVSet(context.Background(), "pending_ask", "")
                s.hub.broadcast("event: ask_done\ndata: {}\n\n")
        }()

        s.hub.broadcast(s.sseMsg("ask", question))
        log.Printf("[web] ask: %s", trunc(question, 80))

        select {
        case reply := <-s.pending:
                return reply
        case <-time.After(10 * time.Minute):
                s.Send("⏰ No reply in 10 min — using best judgment and continuing.")
                return ""
        }
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

        mux.HandleFunc("/", s.handleIndex)
        mux.HandleFunc("/events", s.handleSSE)

        mux.HandleFunc("/api/status", s.handleStatus)
        mux.HandleFunc("/api/score", s.handleScore)
        mux.HandleFunc("/api/tasks", s.handleTasks)
        mux.HandleFunc("/api/log", s.handleLog)
        mux.HandleFunc("/api/usage", s.handleUsage)
        mux.HandleFunc("/api/clear", s.handleClear)
        mux.HandleFunc("/api/scan", s.handleScan)
        mux.HandleFunc("/api/pause", s.handlePause)
        mux.HandleFunc("/api/resume", s.handleResume)
        mux.HandleFunc("/api/replan", s.handleReplan)
        mux.HandleFunc("/api/reply", s.handleReply)
        mux.HandleFunc("/api/upload", s.handleUpload)

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
        persistedAsk := s.db.KVGetDefault(r.Context(), "pending_ask", "")
        s.mu.Lock()
        active := s.askActive
        question := s.pendingQuestion
        s.mu.Unlock()

        if active {
                // Ask is live right now — resend the actual question text.
                fmt.Fprint(w, s.sseMsg("ask", question))
                flusher.Flush()
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

func (s *Server) handleScore(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        total, answered, _ := s.db.CountClarifications(ctx)
        clarScore := 0.5
        if total > 0 {
                clarScore = float64(answered) / float64(total)
        }
        repoScore := 0.0
        if s.db.KVGetDefault(ctx, "repo_scanned", "0") == "1" {
                repoScore = 1.0
        }
        chunks, _ := s.db.CountDocChunks(ctx)
        docScore := float64(chunks) / 20.0
        if docScore > 1 {
                docScore = 1
        }
        open := total - answered
        ambig := 1.0 - float64(open)*0.2
        if ambig < 0 {
                ambig = 0
        }
        score := (docScore*0.30 + clarScore*0.25 + repoScore*0.15 + ambig*0.10) * 100

        jsonOK(w, map[string]interface{}{
                "total":          score,
                "doc_coverage":   docScore * 100,
                "clarifications": clarScore * 100,
                "repo_scan":      repoScore * 100,
                "ambiguity":      ambig * 100,
                "chunks":         chunks,
        })
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        plan, err := s.db.GetActivePlan(ctx)
        if err != nil {
                jsonOK(w, map[string]interface{}{"plan": nil, "tasks": []interface{}{}})
                return
        }
        tasks, _ := s.db.GetTasksByPlan(ctx, plan.ID)
        type item struct {
                Phase  int    `json:"phase"`
                Title  string `json:"title"`
                Status string `json:"status"`
        }
        var items []item
        for _, t := range tasks {
                if t.ParentID != nil {
                        continue
                }
                items = append(items, item{t.Phase, t.Title, t.Status})
        }
        if items == nil {
                items = []item{}
        }
        jsonOK(w, map[string]interface{}{"version": plan.Version, "tasks": items})
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

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured — add LLM API keys and restart", http.StatusServiceUnavailable)
                return
        }
        s.Send("🔍 Scanning repo...")
        go func() {
                out, err := s.agent.ScanRepo(context.Background())
                if err != nil {
                        s.Send(fmt.Sprintf("❌ Scan failed: %v", err))
                        return
                }
                s.Send("```\n" + trunc(out, 2000) + "\n```")
        }()
        jsonOK(w, map[string]string{"ok": "scanning"})
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

func (s *Server) handleReplan(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        s.Send("🔄 Replan triggered — resuming from replanning state...")
        go s.agent.Run(context.Background())
        jsonOK(w, map[string]string{"ok": "replanning"})
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
                s.hub.broadcast(s.sseMsg("reply", "You: "+payload.Text))
                jsonOK(w, map[string]string{"ok": "sent"})
        default:
                jsonErr(w, "no question pending", http.StatusConflict)
        }
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

        s.Send(fmt.Sprintf("✅ **%s** ingested — %d chunks tagged.", header.Filename, chunks))

        n, _ := s.db.CountDocChunks(ctx)
        if n >= 5 {
                s.Send("📋 Enough docs uploaded! The agent will start asking clarifying questions soon.")
        }

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
