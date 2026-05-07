package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ─── Project ───────────────────────────────────────────────────────────────

// Project is one of the user's projects.
type Project struct {
	ID          int
	Name        string
	RepoURL     string
	GithubToken string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListProjects returns every project, oldest first.
func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := d.pool.Query(ctx,
		"SELECT id,name,repo_url,github_token,created_at,updated_at FROM project ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProjectByID looks up a single project. Returns ErrNotFound on miss.
func (d *DB) GetProjectByID(ctx context.Context, id int) (*Project, error) {
	var p Project
	err := d.pool.QueryRow(ctx,
		"SELECT id,name,repo_url,github_token,created_at,updated_at FROM project WHERE id=$1", id).
		Scan(&p.ID, &p.Name, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateProject inserts a project and returns the new row.
func (d *DB) CreateProject(ctx context.Context, name, repoURL, token string) (*Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	var p Project
	err := d.pool.QueryRow(ctx,
		`INSERT INTO project (name, repo_url, github_token)
         VALUES ($1,$2,$3)
         RETURNING id,name,repo_url,github_token,created_at,updated_at`,
		name, strings.TrimSpace(repoURL), strings.TrimSpace(token)).
		Scan(&p.ID, &p.Name, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProject replaces name/repo/token. If token is the empty string it is
// cleared; pass the existing value to keep it.
func (d *DB) UpdateProject(ctx context.Context, id int, name, repoURL, token string) error {
	res, err := d.pool.Exec(ctx,
		`UPDATE project SET name=$2, repo_url=$3, github_token=$4, updated_at=NOW() WHERE id=$1`,
		id, strings.TrimSpace(name), strings.TrimSpace(repoURL), strings.TrimSpace(token))
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project (and its docs/memories via CASCADE).
// Refuses if it would leave zero projects.
func (d *DB) DeleteProject(ctx context.Context, id int) error {
	var n int
	if err := d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM project").Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastProject
	}
	res, err := d.pool.Exec(ctx, "DELETE FROM project WHERE id=$1", id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Project docs ──────────────────────────────────────────────────────────

// Doc is an uploaded reference document scoped to a project.
type Doc struct {
	ID        int
	Filename  string
	Content   string
	CreatedAt time.Time
}

// AddDoc stores a document for a project.
func (d *DB) AddDoc(ctx context.Context, projectID int, filename, content string) (int, error) {
	var id int
	err := d.pool.QueryRow(ctx,
		`INSERT INTO project_docs (project_id, filename, content)
         VALUES ($1,$2,$3) RETURNING id`,
		projectID, filename, content).Scan(&id)
	return id, err
}

// ListDocs lists docs for a project (newest first).
func (d *DB) ListDocs(ctx context.Context, projectID int) ([]Doc, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, filename, content, created_at FROM project_docs
         WHERE project_id=$1 ORDER BY id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var dd Doc
		if err := rows.Scan(&dd.ID, &dd.Filename, &dd.Content, &dd.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, dd)
	}
	return out, rows.Err()
}

// GetDoc fetches a single doc by id within a project.
func (d *DB) GetDoc(ctx context.Context, projectID, id int) (*Doc, error) {
	var dd Doc
	err := d.pool.QueryRow(ctx,
		`SELECT id, filename, content, created_at FROM project_docs
         WHERE project_id=$1 AND id=$2`, projectID, id).
		Scan(&dd.ID, &dd.Filename, &dd.Content, &dd.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &dd, nil
}

// DeleteDoc removes a doc by id within a project.
func (d *DB) DeleteDoc(ctx context.Context, projectID, id int) error {
	res, err := d.pool.Exec(ctx,
		"DELETE FROM project_docs WHERE project_id=$1 AND id=$2", projectID, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Memories ──────────────────────────────────────────────────────────────

// Memory is a long-lived note keyed by project + label.
type Memory struct {
	ID        int
	Key       string
	Content   string
	UpdatedAt time.Time
}

// PutMemory upserts a memory by (project_id, key).
func (d *DB) PutMemory(ctx context.Context, projectID int, key, content string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO memories (project_id, key, content, updated_at)
         VALUES ($1,$2,$3,NOW())
         ON CONFLICT (project_id, key)
         DO UPDATE SET content=EXCLUDED.content, updated_at=NOW()`,
		projectID, key, content)
	return err
}

// GetMemory fetches a memory by key.
func (d *DB) GetMemory(ctx context.Context, projectID int, key string) (*Memory, error) {
	var m Memory
	err := d.pool.QueryRow(ctx,
		`SELECT id, key, content, updated_at FROM memories
         WHERE project_id=$1 AND key=$2`, projectID, key).
		Scan(&m.ID, &m.Key, &m.Content, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMemories returns every memory for a project.
func (d *DB) ListMemories(ctx context.Context, projectID int) ([]Memory, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, key, content, updated_at FROM memories
         WHERE project_id=$1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Key, &m.Content, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMemory removes one memory.
func (d *DB) DeleteMemory(ctx context.Context, projectID int, key string) error {
	_, err := d.pool.Exec(ctx,
		"DELETE FROM memories WHERE project_id=$1 AND key=$2", projectID, key)
	return err
}

// ─── LLM usage ─────────────────────────────────────────────────────────────

// RecordUsage stores token-count metrics from one LLM call.
func (d *DB) RecordUsage(ctx context.Context, projectID int, provider, model string, prompt, completion int) error {
	var pid interface{} = projectID
	if projectID <= 0 {
		pid = nil
	}
	_, err := d.pool.Exec(ctx,
		`INSERT INTO llm_usage (project_id, provider, model, prompt_tokens, completion_tokens, total_tokens)
         VALUES ($1,$2,$3,$4,$5,$6)`,
		pid, provider, model, prompt, completion, prompt+completion)
	return err
}

// UsageRow is one summary row from GetUsageSummary / GetUsageToday.
type UsageRow struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	TotalTokens       int    `json:"total_tokens"`
	Calls             int    `json:"calls"`
}

// GetUsageSummary returns lifetime totals grouped by provider+model.
func (d *DB) GetUsageSummary(ctx context.Context) ([]UsageRow, error) {
	return d.usageQuery(ctx, "")
}

// GetUsageToday returns per-day totals (UTC) since 00:00.
func (d *DB) GetUsageToday(ctx context.Context) ([]UsageRow, error) {
	return d.usageQuery(ctx, "WHERE created_at >= date_trunc('day', NOW())")
}

func (d *DB) usageQuery(ctx context.Context, where string) ([]UsageRow, error) {
	q := `SELECT provider, model,
                 COALESCE(SUM(prompt_tokens),0),
                 COALESCE(SUM(completion_tokens),0),
                 COALESCE(SUM(total_tokens),0),
                 COUNT(*)
          FROM llm_usage ` + where + `
          GROUP BY provider, model
          ORDER BY total_tokens DESC`
	rows, err := d.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Provider, &r.Model, &r.PromptTokens, &r.CompletionTokens, &r.TotalTokens, &r.Calls); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
