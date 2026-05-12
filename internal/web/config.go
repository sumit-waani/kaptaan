package web

import (
	"encoding/json"
	"io"
	"net/http"
)

// knownConfigKeys is the canonical list of config keys the UI exposes.
var knownConfigKeys = []string{
	"deepseek_api_key",
	"deepseek_model",
	"e2b_api_key",
	"repo_url",
	"github_token",
	"system_prompt",
	"cf_api_token",
	"cf_zone_id",
	"ssh_hosts",
}

// GET  /api/config  → {config: {key: value, ...}}
// POST /api/config  → body {key, value} → upserts one entry
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.db.ListConfig(ctx)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Fill in empty defaults so the UI always sees every key.
		for _, k := range knownConfigKeys {
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
		if err := s.db.SetConfig(ctx, body.Key, body.Value); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]string{"ok": "saved"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
