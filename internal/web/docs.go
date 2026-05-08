package web

import (
	"encoding/json"
	"io"
	"net/http"
)

// GET    /api/memories          — list memories
// PUT    /api/memories          — upsert {key, content}
// DELETE /api/memories?key=X    — delete one
func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		ms, err := s.db.ListMemories(ctx, fixedProjectID)
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
		if err := s.db.PutMemory(ctx, fixedProjectID, body.Key, body.Content); err != nil {
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
		if err := s.db.DeleteMemory(ctx, fixedProjectID, key); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]string{"ok": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
