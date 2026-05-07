package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cto-agent/cto-agent/internal/agent"
)

// GET  /api/docs        — list docs for active project
// POST /api/docs        — multipart upload OR JSON {filename, content}
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		docs, err := s.db.ListDocs(ctx, pid)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type item struct {
			ID       int    `json:"id"`
			Filename string `json:"filename"`
			Bytes    int    `json:"bytes"`
			Created  string `json:"created"`
		}
		out := make([]item, 0, len(docs))
		for _, d := range docs {
			out = append(out, item{ID: d.ID, Filename: d.Filename, Bytes: len(d.Content), Created: d.CreatedAt.Format("2006-01-02 15:04")})
		}
		jsonOK(w, map[string]interface{}{"docs": out})
	case http.MethodPost:
		ct := r.Header.Get("Content-Type")
		var filename, content string
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				jsonErr(w, "bad multipart", http.StatusBadRequest)
				return
			}
			f, hdr, err := r.FormFile("file")
			if err != nil {
				jsonErr(w, "missing file field", http.StatusBadRequest)
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(io.LimitReader(f, 8<<20))
			filename = hdr.Filename
			content = string(b)
		} else {
			var body struct {
				Filename string `json:"filename"`
				Content  string `json:"content"`
			}
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			if err := json.Unmarshal(raw, &body); err != nil {
				jsonErr(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			filename = body.Filename
			content = body.Content
		}
		if filename == "" || content == "" {
			jsonErr(w, "filename and content required", http.StatusBadRequest)
			return
		}
		id, err := s.db.AddDoc(ctx, pid, filename, content)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]interface{}{"id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET    /api/docs/{id}  — download doc content
// DELETE /api/docs/{id}  — remove doc
func (s *Server) handleDocByID(w http.ResponseWriter, r *http.Request) {
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/docs/")
	rest = strings.Trim(rest, "/")
	id, err := strconv.Atoi(rest)
	if err != nil || id <= 0 {
		jsonErr(w, "invalid doc id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		d, err := s.db.GetDoc(r.Context(), pid, id)
		if err != nil {
			jsonErr(w, "not found", http.StatusNotFound)
			return
		}
		jsonOK(w, map[string]interface{}{
			"id": d.ID, "filename": d.Filename, "content": d.Content,
		})
	case http.MethodDelete:
		if err := s.db.DeleteDoc(r.Context(), pid, id); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]string{"ok": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET    /api/memories          — list memories for active project
// PUT    /api/memories          — upsert {key, content}
// DELETE /api/memories?key=X    — delete one
func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		ms, err := s.db.ListMemories(ctx, pid)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type item struct {
			Key       string `json:"key"`
			Content   string `json:"content"`
			UpdatedAt string `json:"updated_at"`
		}
		out := make([]item, 0, len(ms))
		for _, m := range ms {
			out = append(out, item{Key: m.Key, Content: m.Content, UpdatedAt: m.UpdatedAt.Format("2006-01-02 15:04")})
		}
		jsonOK(w, map[string]interface{}{"memories": out})
	case http.MethodPut:
		var body struct {
			Key     string `json:"key"`
			Content string `json:"content"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err := json.Unmarshal(raw, &body); err != nil || body.Key == "" {
			jsonErr(w, "invalid JSON / missing key", http.StatusBadRequest)
			return
		}
		if err := s.db.PutMemory(ctx, pid, body.Key, body.Content); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]string{"ok": "saved"})
	case http.MethodDelete:
		key := r.URL.Query().Get("key")
		if key == "" {
			jsonErr(w, "key required", http.StatusBadRequest)
			return
		}
		if err := s.db.DeleteMemory(ctx, pid, key); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]string{"ok": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET   /api/plans              — list plan files for active project
// GET   /api/plans?file=NAME    — read one
func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	pid, err := s.resolveProjectID(r)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if file := r.URL.Query().Get("file"); file != "" {
		c, err := agent.ReadPlan(pid, file)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonOK(w, map[string]string{"filename": file, "content": c})
		return
	}
	ps, err := agent.ListPlans(pid)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"plans": ps})
}
