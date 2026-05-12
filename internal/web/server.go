// Package web hosts the HTTP API + embedded UI (multi-project).
package web

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "log"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
)


// Agent is the surface the web layer needs.
type Agent interface {
        HandleUserMessage(ctx context.Context, projectID int, text string) error
        IsRunning(projectID int) bool
        HasQueued(projectID int) bool
        ResetConversation(projectID int)
        ReadScratchpad(ctx context.Context, projectID int) (string, error)
        CancelTask(projectID int) bool
}

// ─── SSE hub ───────────────────────────────────────────────────────────────

type sseClient struct {
        projectID int
        ch        chan string
}

type sseHub struct {
        mu      sync.Mutex
        clients map[*sseClient]struct{}
}

func newHub() *sseHub { return &sseHub{clients: map[*sseClient]struct{}{}} }

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

func (h *sseHub) broadcast(projectID int, payload string) {
        h.mu.Lock()
        defer h.mu.Unlock()
        for c := range h.clients {
                if c.projectID != projectID {
                        continue
                }
                select {
                case c.ch <- payload:
                default:
                        // Client is too slow; log and skip rather than silently losing data.
                        log.Printf("[sse] dropped message for slow client on project %d", projectID)
                }
        }
}

// ─── Pending ask state ─────────────────────────────────────────────────────

type pendingAsk struct {
        question string
        reply    chan string
        cancel   chan struct{}
}

// ─── Server ────────────────────────────────────────────────────────────────

type Server struct {
        db    *db.DB
        agent Agent
        hub   *sseHub

        mu      sync.Mutex
        pending map[int]*pendingAsk
        motd    string

        streamMu  sync.Mutex
        streamBuf map[int]string // per-project streaming token accumulator
}

func New(database *db.DB) *Server {
        return &Server{
                db:        database,
                hub:       newHub(),
                pending:   map[int]*pendingAsk{},
                streamBuf: map[int]string{},
        }
}

func (s *Server) SetAgent(a Agent) { s.agent = a }

// getProjectID extracts project ID from query param, defaulting to 1.
func getProjectID(r *http.Request) int {
        s := r.URL.Query().Get("project_id")
        if s == "" {
                return 1
        }
        var id int
        if _, err := fmt.Sscanf(s, "%d", &id); err != nil || id <= 0 {
                return 1
        }
        return id
}

func (s *Server) SetMOTD(msg string) {
        s.mu.Lock()
        s.motd = msg
        s.mu.Unlock()
}

// ─── Outbound hooks ────────────────────────────────────────────────────────

// SendToProject sends a text message to the UI and persists it for SSE replay.
func (s *Server) SendToProject(projectID int, text string) {
        ts := time.Now().Format("15:04:05")
        payload := map[string]string{
                "type": "message",
                "text": text,
                "ts":   ts,
        }
        data, _ := json.Marshal(payload)
        sse := "event: msg\ndata: " + string(data) + "\n\n"

        // Persist for reconnect replay.
        if err := s.db.AppendMessage(context.Background(), projectID,
                "", "", "", "", "", "message", text, ts,
        ); err != nil {
                log.Printf("[web] AppendMessage (send): %v", err)
        }

        s.hub.broadcast(projectID, sse)
        log.Printf("[web] send → p%d: %s", projectID, trunc(text, 80))
}

// SendToken forwards a streaming content token to all SSE clients.
func (s *Server) SendToken(projectID int, token string) {
        s.streamMu.Lock()
        s.streamBuf[projectID] += token
        s.streamMu.Unlock()
        data, _ := json.Marshal(map[string]string{"text": token})
        s.hub.broadcast(projectID, "event: token\ndata: "+string(data)+"\n\n")
}

// CancelStream tells clients to discard their in-progress streaming buffer.
func (s *Server) CancelStream(projectID int) {
        s.streamMu.Lock()
        delete(s.streamBuf, projectID)
        s.streamMu.Unlock()
        s.hub.broadcast(projectID, "event: stream_cancel\ndata: {}\n\n")
}

// FinalizeStream tells clients to commit the streaming buffer as a message,
// and persists the completed text for SSE replay on reconnect.
func (s *Server) FinalizeStream(projectID int) {
        s.streamMu.Lock()
        text := s.streamBuf[projectID]
        delete(s.streamBuf, projectID)
        s.streamMu.Unlock()

        if text != "" {
                ts := time.Now().Format("15:04:05")
                // Persist the finalized assistant message for reconnect replay.
                if err := s.db.AppendMessage(context.Background(), projectID,
                        "", "", "", "", "", "message", text, ts,
                ); err != nil {
                        log.Printf("[web] AppendMessage (finalize): %v", err)
                }
        }
        s.hub.broadcast(projectID, "event: stream_done\ndata: {}\n\n")
}

func (s *Server) AskProject(projectID int, question string) string {
        pa := &pendingAsk{
                question: question,
                reply:    make(chan string, 1),
                cancel:   make(chan struct{}),
        }
        s.mu.Lock()
        if existing := s.pending[projectID]; existing != nil {
                close(existing.cancel)
        }
        s.pending[projectID] = pa
        s.mu.Unlock()

        defer func() {
                s.mu.Lock()
                if s.pending[projectID] == pa {
                        delete(s.pending, projectID)
                }
                s.mu.Unlock()
                s.broadcastAskState(projectID)
        }()

        ts := time.Now().Format("15:04:05")
        payload, _ := json.Marshal(map[string]string{
                "type": "ask", "text": question, "ts": ts,
        })
        askSSE := "event: msg\ndata: " + string(payload) + "\n\n"

        // Persist ask for reconnect replay.
        if err := s.db.AppendMessage(context.Background(), projectID,
                "", "", "", "", "", "ask", question, ts,
        ); err != nil {
                log.Printf("[web] AppendMessage (ask): %v", err)
        }

        s.hub.broadcast(projectID, askSSE)
        s.broadcastAskState(projectID)

        select {
        case r := <-pa.reply:
                return r
        case <-pa.cancel:
                return ""
        case <-time.After(15 * time.Minute):
                s.SendToProject(projectID, "⏰ No reply in 15 minutes — proceeding without one.")
                return ""
        }
}

func (s *Server) NotifyAgentState(projectID int) {
        if s.agent == nil {
                return
        }
        payload := map[string]interface{}{
                "running": s.agent.IsRunning(projectID),
                "queued":  s.agent.HasQueued(projectID),
        }
        data, _ := json.Marshal(payload)
        s.hub.broadcast(projectID, "event: state\ndata: "+string(data)+"\n\n")
}

func (s *Server) broadcastAskState(projectID int) {
        s.mu.Lock()
        pa := s.pending[projectID]
        s.mu.Unlock()
        q := ""
        if pa != nil {
                q = pa.question
        }
        data, _ := json.Marshal(map[string]interface{}{
                "active":   pa != nil,
                "question": q,
        })
        s.hub.broadcast(projectID, "event: ask_state\ndata: "+string(data)+"\n\n")
}

// ─── HTTP routing ──────────────────────────────────────────────────────────

func (s *Server) Start(ctx context.Context) {
        mux := http.NewServeMux()

        mux.HandleFunc("/", s.handleIndex)
        mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
        mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
        mux.HandleFunc("/api/auth/setup", s.handleAuthSetup)
        mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
        mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)

        mux.HandleFunc("/api/history", s.requireAuth(s.handleHistory))
        mux.HandleFunc("/events", s.requireAuth(s.handleSSE))
        mux.HandleFunc("/api/chat", s.requireAuth(s.handleChat))
        mux.HandleFunc("/api/reply", s.requireAuth(s.handleReply))
        mux.HandleFunc("/api/memories", s.requireAuth(s.handleMemories))
        mux.HandleFunc("/api/conversation/clear", s.requireAuth(s.handleClearConvo))
        mux.HandleFunc("/api/credits", s.requireAuth(s.handleCredits))
        mux.HandleFunc("/api/scratchpad", s.requireAuth(s.handleScratchpad))
        mux.HandleFunc("/api/task/cancel", s.requireAuth(s.handleCancelTask))
        mux.HandleFunc("/api/config", s.requireAuth(s.handleConfig))
        mux.HandleFunc("/api/projects", s.requireAuth(s.handleProjects))
        mux.HandleFunc("/api/projects/create", s.requireAuth(s.handleProjectCreate))

        addrs := []string{"0.0.0.0:80", "0.0.0.0:5000"}
        servers := make([]*http.Server, len(addrs))
        for i, addr := range addrs {
                srv := &http.Server{Addr: addr, Handler: mux}
                servers[i] = srv
                go func(srv *http.Server) {
                        log.Printf("[web] listening on %s", srv.Addr)
                        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
                                log.Printf("[web] server error on %s: %v", srv.Addr, err)
                        }
                }(srv)
        }

        <-ctx.Done()
        shutCtx := context.Background()
        for _, srv := range servers {
                _ = srv.Shutdown(shutCtx)
        }
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                http.NotFound(w, r)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        f, err := staticFiles.Open("index.html")
        if err != nil {
                http.Error(w, "ui not found", http.StatusInternalServerError)
                return
        }
        defer f.Close()
        _, _ = io.Copy(w, f)
}

// ─── SSE ───────────────────────────────────────────────────────────────────

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        uiMsgs, err := s.db.LoadUIMessages(r.Context(), getProjectID(r))
        if err != nil {
                log.Printf("[web] LoadUIMessages (history): %v", err)
                jsonErr(w, "failed to load history", http.StatusInternalServerError)
                return
        }
        type msgItem struct {
                Type string `json:"type"`
                Text string `json:"text"`
                Ts   string `json:"ts"`
        }
        items := make([]msgItem, 0, len(uiMsgs))
        for _, m := range uiMsgs {
                items = append(items, msgItem{Type: m.UIType, Text: m.UIText, Ts: m.UITs})
        }
        jsonOK(w, map[string]interface{}{"messages": items})
}

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

        c := &sseClient{projectID: getProjectID(r), ch: make(chan string, 1024)}
        s.hub.add(c)
        defer s.hub.remove(c)

        if s.agent != nil {
                data, _ := json.Marshal(map[string]interface{}{
                        "running": s.agent.IsRunning(getProjectID(r)),
                        "queued":  s.agent.HasQueued(getProjectID(r)),
                })
                fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
        }
        s.mu.Lock()
        pa := s.pending[getProjectID(r)]
        motd := s.motd
        s.mu.Unlock()
        if pa != nil {
                askState, _ := json.Marshal(map[string]interface{}{"active": true, "question": pa.question})
                fmt.Fprintf(w, "event: ask_state\ndata: %s\n\n", askState)
        }
        if motd != "" {
                motdData, _ := json.Marshal(map[string]string{"type": "message", "text": motd, "ts": time.Now().Format("15:04:05")})
                fmt.Fprintf(w, "event: msg\ndata: %s\n\n", motdData)
        }
        flusher.Flush()

        tick := time.NewTicker(20 * time.Second)
        defer tick.Stop()
        for {
                select {
                case <-r.Context().Done():
                        return
                case payload := <-c.ch:
                        fmt.Fprint(w, payload)
                        flusher.Flush()
                case <-tick.C:
                        fmt.Fprint(w, ": heartbeat\n\n")
                        flusher.Flush()
                }
        }
}

// ─── Chat & reply ──────────────────────────────────────────────────────────

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        body, _ := io.ReadAll(io.LimitReader(r.Body, 256*1024))
        var payload struct {
                Text string `json:"text"`
        }
        if err := json.Unmarshal(body, &payload); err != nil {
                jsonErr(w, "invalid JSON", http.StatusBadRequest)
                return
        }
        payload.Text = strings.TrimSpace(payload.Text)
        if payload.Text == "" {
                jsonErr(w, "empty message", http.StatusBadRequest)
                return
        }

        ts := time.Now().Format("15:04:05")
        echo, _ := json.Marshal(map[string]string{
                "type": "user", "text": payload.Text, "ts": ts,
        })
        echoSSE := "event: msg\ndata: " + string(echo) + "\n\n"

        // Persist user message for reconnect replay.
        if err := s.db.AppendMessage(r.Context(), getProjectID(r),
                "", "", "", "", "", "user", payload.Text, ts,
        ); err != nil {
                log.Printf("[web] AppendMessage (user echo): %v", err)
        }
        s.hub.broadcast(getProjectID(r), echoSSE)

        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        projectID := getProjectID(r)
        go func(text string) {
                if err := s.agent.HandleUserMessage(context.Background(), projectID, text); err != nil {
                        s.SendToProject(projectID, "❌ "+err.Error())
                }
        }(payload.Text)
        jsonOK(w, map[string]string{"ok": "queued"})
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
        var payload struct {
                Text string `json:"text"`
        }
        if err := json.Unmarshal(body, &payload); err != nil {
                jsonErr(w, "invalid JSON", http.StatusBadRequest)
                return
        }
        payload.Text = strings.TrimSpace(payload.Text)
        if payload.Text == "" {
                jsonErr(w, "empty reply", http.StatusBadRequest)
                return
        }
        s.mu.Lock()
        pa := s.pending[getProjectID(r)]
        s.mu.Unlock()
        if pa == nil {
                jsonErr(w, "no pending question", http.StatusBadRequest)
                return
        }
        select {
        case pa.reply <- payload.Text:
                ts := time.Now().Format("15:04:05")
                replyText := "You: " + payload.Text
                echo, _ := json.Marshal(map[string]string{
                        "type": "reply", "text": replyText, "ts": ts,
                })
                // Persist reply for reconnect replay.
                if err := s.db.AppendMessage(r.Context(), getProjectID(r),
                        "", "", "", "", "", "reply", replyText, ts,
                ); err != nil {
                        log.Printf("[web] AppendMessage (reply): %v", err)
                }
                s.hub.broadcast(getProjectID(r), "event: msg\ndata: "+string(echo)+"\n\n")
                jsonOK(w, map[string]string{"ok": "sent"})
        default:
                jsonErr(w, "reply channel full", http.StatusConflict)
        }
}

func (s *Server) handleClearConvo(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        // Clear both LLM convo rows and UI display rows from DB.
        if err := s.db.ClearMessages(r.Context(), getProjectID(r)); err != nil {
                log.Printf("[web] ClearMessages: %v", err)
        }
        if s.agent != nil {
                // ResetConversation also calls ClearMessages; no-op here but kept for
                // any future in-memory state the agent might hold.
                s.agent.ResetConversation(getProjectID(r))
        }
        s.SendToProject(getProjectID(r), "🧹 Conversation cleared.")
        jsonOK(w, map[string]string{"ok": "cleared"})
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        if !s.agent.CancelTask(getProjectID(r)) {
                jsonErr(w, "no task running", http.StatusBadRequest)
                return
        }
        jsonOK(w, map[string]string{"ok": "cancelled"})
}

// ─── Credits ───────────────────────────────────────────────────────────────

func (s *Server) handleCredits(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        key := s.db.GetConfig(r.Context(), getProjectID(r), "deepseek_api_key")
        if key == "" {
                jsonErr(w, "deepseek_api_key not configured — set it in Settings → Configuration", http.StatusServiceUnavailable)
                return
        }
        req, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.deepseek.com/user/balance", nil)
        if err != nil {
                jsonErr(w, "request error", http.StatusInternalServerError)
                return
        }
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                jsonErr(w, "deepseek api error: "+err.Error(), http.StatusBadGateway)
                return
        }
        defer resp.Body.Close()
        data, _ := io.ReadAll(resp.Body)
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(resp.StatusCode)
        _, _ = w.Write(data)
}

// ─── Projects ──────────────────────────────────────────────────────────────

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        projects, err := s.db.ListProjects(r.Context())
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        if projects == nil {
                projects = []db.Project{}
        }
        jsonOK(w, map[string]interface{}{"projects": projects})
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "POST required", http.StatusMethodNotAllowed)
                return
        }
        var body struct {
                Name string `json:"name"`
        }
        raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
        if err := json.Unmarshal(raw, &body); err != nil || body.Name == "" {
                jsonErr(w, "invalid JSON / missing name", http.StatusBadRequest)
                return
        }
        id, err := s.db.CreateProject(r.Context(), body.Name)
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        jsonOK(w, map[string]interface{}{"id": id, "name": body.Name})
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

func (s *Server) handleScratchpad(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        content, err := s.agent.ReadScratchpad(r.Context(), getProjectID(r))
        if err != nil {
                jsonErr(w, err.Error(), http.StatusInternalServerError)
                return
        }
        jsonOK(w, map[string]string{"content": content})
}

func trunc(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "…"
}
