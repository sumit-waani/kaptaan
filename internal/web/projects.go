package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cto-agent/cto-agent/internal/db"
)

type projectDTO struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	RepoURL     string `json:"repo_url"`
	HasToken    bool   `json:"has_token"`
	GithubToken string `json:"github_token_masked"`
	CreatedAt   string `json:"created_at"`
}

func toDTO(p db.Project) projectDTO {
	return projectDTO{
		ID:          p.ID,
		Name:        p.Name,
		RepoURL:     p.RepoURL,
		HasToken:    strings.TrimSpace(p.GithubToken) != "",
		GithubToken: maskToken(p.GithubToken),
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

// GET  /api/projects        — list every project
// POST /api/projects        — create {name, repo_url, github_token}
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		ps, err := s.db.ListProjects(ctx)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]projectDTO, 0, len(ps))
		for _, p := range ps {
			out = append(out, toDTO(p))
		}
		jsonOK(w, map[string]interface{}{"projects": out})
	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			RepoURL     string `json:"repo_url"`
			GithubToken string `json:"github_token"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err := json.Unmarshal(raw, &body); err != nil {
			jsonErr(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		p, err := s.db.CreateProject(ctx, body.Name, body.RepoURL, body.GithubToken)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonOK(w, map[string]interface{}{"project": toDTO(*p)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// PATCH  /api/projects/{id}   — update name + repo (+ optional token)
// DELETE /api/projects/{id}   — delete (refuses if it's the last one)
func (s *Server) handleProjectByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(rest)
	if err != nil || id <= 0 {
		jsonErr(w, "invalid project id", http.StatusBadRequest)
		return
	}
	existing, err := s.db.GetProjectByID(ctx, id)
	if err != nil {
		jsonErr(w, "project not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Name        string  `json:"name"`
			RepoURL     string  `json:"repo_url"`
			GithubToken *string `json:"github_token"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err := json.Unmarshal(raw, &body); err != nil {
			jsonErr(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = existing.Name
		}
		token := existing.GithubToken
		if body.GithubToken != nil {
			token = strings.TrimSpace(*body.GithubToken)
		}
		if err := s.db.UpdateProject(ctx, id, name, body.RepoURL, token); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, _ := s.db.GetProjectByID(ctx, id)
		jsonOK(w, map[string]interface{}{"project": toDTO(*updated)})

	case http.MethodDelete:
		if err := s.db.DeleteProject(ctx, id); err != nil {
			if errors.Is(err, db.ErrLastProject) {
				jsonErr(w, err.Error(), http.StatusBadRequest)
				return
			}
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.agent != nil {
			s.agent.ResetConversation(id)
		}
		jsonOK(w, map[string]string{"ok": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
