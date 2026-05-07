// Package web hosts the HTTP API + embedded UI. Stateless: the active
// project is sent on every request via the X-Project-ID header (or ?project=
// query param). No global "active project" pointer.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cto-agent/cto-agent/internal/db"
)

// Agent is the surface the web layer needs.
type Agent interface {
	HandleUserMessage(ctx context.Context, projectID int, text string) error
	IsRunning(projectID int) bool
	ResetConversation(projectID int)
}

// ─── SSE hub (per-project) ─────────────────────────────────────────────────

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

// ─── Pending ask state (per-project) ───────────────────────────────────────

type pendingAsk struct {
	question string
	reply    chan string
	cancel   chan struct{}
}

// ─── Server ────────────────────────────────────────────────────────────────

// Server is the embedded web UI + JSON API.
type Server struct {
	db    *db.DB
	agent Agent
	hub   *sseHub

	mu      sync.Mutex
	pending map[int]*pendingAsk // projectID → in-flight ask
	motd    string
}

// New creates a Server (does not listen yet).
func New(database *db.DB) *Server {
	return &Server{
		db:      database,
		hub:     newHub(),
		pending: map[int]*pendingAsk{},
	}
}

// SetAgent wires the agent post-construction (breaks init cycle).
func (s *Server) SetAgent(a Agent) { s.agent = a }

// SetMOTD sets a one-shot message pushed to each new SSE client.
func (s *Server) SetMOTD(msg string) {
	s.mu.Lock()
	s.motd = msg
	s.mu.Unlock()
}

// ─── Outbound hooks ────────────────────────────────────────────────────────

// SendToProject broadcasts a markdown message to one project's clients.
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

// AskProject blocks until the project's user replies (or times out / cancels).
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

	// Push as a chat message AND surface ask_active for the composer.
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

// NotifyAgentState pushes a state event for one project.
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

// ─── HTTP plumbing ─────────────────────────────────────────────────────────

// Start registers routes and serves on :5000 until ctx is cancelled.
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
	mux.HandleFunc("/api/usage", s.requireAuth(s.handleUsage))
	mux.HandleFunc("/api/projects", s.requireAuth(s.handleProjects))
	mux.HandleFunc("/api/projects/", s.requireAuth(s.handleProjectByID))
	mux.HandleFunc("/api/docs", s.requireAuth(s.handleDocs))
	mux.HandleFunc("/api/docs/", s.requireAuth(s.handleDocByID))
	mux.HandleFunc("/api/memories", s.requireAuth(s.handleMemories))
	mux.HandleFunc("/api/plans", s.requireAuth(s.handlePlans))
	mux.HandleFunc("/api/conversation/clear", s.requireAuth(s.handleClearConvo))

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

// resolveProjectID reads X-Project-ID then ?project=. Validates against DB.
// Returns 0 + an HTTP-status-friendly error message on failure.
func (s *Server) resolveProjectID(r *http.Request) (int, error) {
	v := r.Header.Get("X-Project-ID")
	if v == "" {
		v = r.URL.Query().Get("project")
	}
	if v == "" {
		return 0, errors.New("missing project id (set X-Project-ID header or ?project=)")
	}
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid project id %q", v)
	}
	if _, err := s.db.GetProjectByID(r.Context(), id); err != nil {
		return 0, fmt.Errorf("project %d not found", id)
	}
	return id, nil
}

// ─── SSE ───────────────────────────────────────────────────────────────────

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	c := &sseClient{projectID: pid, ch: make(chan string, 64)}
	s.hub.add(c)
	defer s.hub.remove(c)

	// Greet with current state.
	if s.agent != nil {
		data, _ := json.Marshal(map[string]bool{"running": s.agent.IsRunning(pid)})
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
	}
	s.mu.Lock()
	pa := s.pending[pid]
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
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
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
	// Echo to feed.
	echo, _ := json.Marshal(map[string]string{
		"type": "user", "text": payload.Text, "ts": time.Now().Format("15:04:05"),
	})
	s.hub.broadcast(pid, "event: msg\ndata: "+string(echo)+"\n\n")

	if s.agent == nil {
		jsonErr(w, "agent not configured", http.StatusServiceUnavailable)
		return
	}
	go func(text string) {
		if err := s.agent.HandleUserMessage(context.Background(), pid, text); err != nil {
			s.SendToProject(pid, "❌ "+err.Error())
		}
	}(payload.Text)
	jsonOK(w, map[string]string{"ok": "queued"})
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
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
	pa := s.pending[pid]
	s.mu.Unlock()
	if pa == nil {
		jsonErr(w, "no pending question for this project", http.StatusBadRequest)
		return
	}
	select {
	case pa.reply <- payload.Text:
		echo, _ := json.Marshal(map[string]string{
			"type": "reply", "text": "You: " + payload.Text, "ts": time.Now().Format("15:04:05"),
		})
		s.hub.broadcast(pid, "event: msg\ndata: "+string(echo)+"\n\n")
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
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.agent != nil {
		s.agent.ResetConversation(pid)
	}
	s.SendToProject(pid, "🧹 Conversation cleared.")
	jsonOK(w, map[string]string{"ok": "cleared"})
}

// ─── Usage ─────────────────────────────────────────────────────────────────

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	all, _ := s.db.GetUsageSummary(r.Context())
	today, _ := s.db.GetUsageToday(r.Context())
	jsonOK(w, map[string]interface{}{"all": all, "today": today})
}

// ─── helpers ───────────────────────────────────────────────────────────────

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
	return s[:n] + "…"
}
