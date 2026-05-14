package web

import (
        "encoding/json"
        "io"
        "net/http"
)

var globalConfigKeys = []string{
        "deepseek_api_key",
        "deepseek_model",
        "e2b_api_key",
        "system_prompt",
}

var projectConfigKeys = []string{
        "repo_url",
        "github_token",
}

// GET  /api/global-config        → {config: {key: value, ...}}
// POST /api/global-config        → body {key, value} → upserts one entry at project_id=0
func (s *Server) handleGlobalConfig(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        switch r.Method {
        case http.MethodGet:
                cfg, err := s.db.ListConfig(ctx, 0)
                if err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                for _, k := range globalConfigKeys {
                        if _, ok := cfg[k]; !ok {
                                cfg[k] = ""
                        }
                }
                jsonOK(w, map[string]interface{}{"config": cfg})

        case http.MethodPost:
                var body struct {
                        Key   string `json:"key"`
                        Value string `json:"value"`
                }
                raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
                if err := json.Unmarshal(raw, &body); err != nil || body.Key == "" {
                        jsonErr(w, "invalid JSON / missing key", http.StatusBadRequest)
                        return
                }
                if err := s.db.SetConfig(ctx, 0, body.Key, body.Value); err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                jsonOK(w, map[string]string{"ok": "saved"})

        default:
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
}

// GET  /api/config?project_id=N  → {config: {key: value, ...}}  (project-scoped keys only)
// POST /api/config?project_id=N  → body {key, value}
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        projectID := getProjectID(r)
        switch r.Method {
        case http.MethodGet:
                cfg, err := s.db.ListConfig(ctx, projectID)
                if err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                for _, k := range projectConfigKeys {
                        if _, ok := cfg[k]; !ok {
                                cfg[k] = ""
                        }
                }
                jsonOK(w, map[string]interface{}{"config": cfg})

        case http.MethodPost:
                var body struct {
                        Key   string `json:"key"`
                        Value string `json:"value"`
                }
                raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
                if err := json.Unmarshal(raw, &body); err != nil || body.Key == "" {
                        jsonErr(w, "invalid JSON / missing key", http.StatusBadRequest)
                        return
                }
                if err := s.db.SetConfig(ctx, projectID, body.Key, body.Value); err != nil {
                        jsonErr(w, err.Error(), http.StatusInternalServerError)
                        return
                }
                jsonOK(w, map[string]string{"ok": "saved"})

        default:
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
}
