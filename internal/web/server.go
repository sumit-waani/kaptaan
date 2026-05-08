// Package web hosts the HTTP API + embedded UI. Single-project: all requests
// operate on fixedProjectID = 1. No project switching or selection needed.
package web

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/cto-agent/cto-agent/internal/db"
)

const fixedProjectID = 1

// Agent is the surface the web layer needs.
type Agent interface {
        HandleUserMessage(ctx context.Context, projectID int, text string) error
        IsRunning(projectID int) bool
        ResetConversation(projectID int)
        ReadScratchpad(ctx context.Context, projectID int) (string, error)
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
}

func New(database *db.DB) *Server {
        return &Server{
                db:      database,
                hub:     newHub(),
                pending: map[int]*pendingAsk{},
        }
}

func (s *Server) SetAgent(a Agent) { s.agent = a }

func (s *Server) SetMOTD(msg string) {
        s.mu.Lock()
        s.motd = msg
        s.mu.Unlock()
}

// ─── Outbound hooks ────────────────────────────────────────────────────────

func (s *Server) SendToProject(projectID int, text string) {
        payload := map[string]string{
                "type": "message",
                "text": text,
                "ts":   time.Now().Format("15:04:05"),
        }
        data, _ := json.Marshal(payload)
        s.hub.broadcast(projectID, "event: msg\ndata: "+string(data)+"\n\n")
        log.Printf("[web] send → p%d: %s", projectID, trunc(text, 80))
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

        payload, _ := json.Marshal(map[string]string{
                "type": "ask", "text": question, "ts": time.Now().Format("15:04:05"),
        })
        s.hub.broadcast(projectID, "event: msg\ndata: "+string(payload)+"\n\n")
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
        mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
        mux.HandleFunc("/api/auth/setup", s.handleAuthSetup)
        mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
        mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)

        mux.HandleFunc("/events", s.requireAuth(s.handleSSE))
        mux.HandleFunc("/api/chat", s.requireAuth(s.handleChat))
        mux.HandleFunc("/api/reply", s.requireAuth(s.handleReply))
        mux.HandleFunc("/api/memories", s.requireAuth(s.handleMemories))
        mux.HandleFunc("/api/conversation/clear", s.requireAuth(s.handleClearConvo))
        mux.HandleFunc("/api/credits", s.requireAuth(s.handleCredits))
        mux.HandleFunc("/api/scratchpad", s.requireAuth(s.handleScratchpad))

        srv := &http.Server{Addr: "0.0.0.0:5000", Handler: mux}
        go func() {
                <-ctx.Done()
                _ = srv.Shutdown(context.Background())
        }()
        log.Println("[web] listening on :5000")
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
                log.Printf("[web] server error: %v", err)
        }
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                http.NotFound(w, r)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, indexHTML)
}

// ─── SSE ───────────────────────────────────────────────────────────────────

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

        c := &sseClient{projectID: fixedProjectID, ch: make(chan string, 64)}
        s.hub.add(c)
        defer s.hub.remove(c)

        if s.agent != nil {
                data, _ := json.Marshal(map[string]bool{"running": s.agent.IsRunning(fixedProjectID)})
                fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
        }
        s.mu.Lock()
        pa := s.pending[fixedProjectID]
        motd := s.motd
        s.mu.Unlock()
        if pa != nil {
                askMsg, _ := json.Marshal(map[string]string{"type": "ask", "text": pa.question, "ts": time.Now().Format("15:04:05")})
                fmt.Fprintf(w, "event: msg\ndata: %s\n\n", askMsg)
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
        echo, _ := json.Marshal(map[string]string{
                "type": "user", "text": payload.Text, "ts": time.Now().Format("15:04:05"),
        })
        s.hub.broadcast(fixedProjectID, "event: msg\ndata: "+string(echo)+"\n\n")

        if s.agent == nil {
                jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
                return
        }
        go func(text string) {
                if err := s.agent.HandleUserMessage(context.Background(), fixedProjectID, text); err != nil {
                        s.SendToProject(fixedProjectID, "❌ "+err.Error())
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
        pa := s.pending[fixedProjectID]
        s.mu.Unlock()
        if pa == nil {
                jsonErr(w, "no pending question", http.StatusBadRequest)
                return
        }
        select {
        case pa.reply <- payload.Text:
                echo, _ := json.Marshal(map[string]string{
                        "type": "reply", "text": "You: " + payload.Text, "ts": time.Now().Format("15:04:05"),
                })
                s.hub.broadcast(fixedProjectID, "event: msg\ndata: "+string(echo)+"\n\n")
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
        if s.agent != nil {
                s.agent.ResetConversation(fixedProjectID)
        }
        s.SendToProject(fixedProjectID, "🧹 Conversation cleared.")
        jsonOK(w, map[string]string{"ok": "cleared"})
}

// ─── Credits ───────────────────────────────────────────────────────────────

func (s *Server) handleCredits(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                http.Error(w, "GET required", http.StatusMethodNotAllowed)
                return
        }
        key := os.Getenv("DEEPSEEK_API_KEY")
        if key == "" {
                jsonErr(w, "DEEPSEEK_API_KEY not configured", http.StatusServiceUnavailable)
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
        content, err := s.agent.ReadScratchpad(r.Context(), fixedProjectID)
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
