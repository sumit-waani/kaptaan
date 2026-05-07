package db

import (
        "context"
        "strconv"
)

// ActiveProjectID returns the currently-selected project id (KV-backed).
// Defaults to 1 (the bootstrapped default project).
func (d *DB) ActiveProjectID(ctx context.Context) int {
        v := d.KVGetDefault(ctx, "active_project_id", "")
        n, _ := strconv.Atoi(v)
        if n <= 0 {
                return 1
        }
        // Sanity-check: if the stored id no longer exists, fall back to 1.
        var ok int
        _ = d.pool.QueryRow(ctx, `SELECT 1 FROM project WHERE id=$1`, n).Scan(&ok)
        if ok == 0 {
                return 1
        }
        return n
}

// SetActiveProject stores the active project id in KV.
func (d *DB) SetActiveProject(ctx context.Context, id int) error {
        return d.KVSet(ctx, "active_project_id", strconv.Itoa(id))
}

// ListProjects returns all projects ordered by id.
func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
        rows, err := d.pool.Query(ctx, `
        SELECT id, name, status, trust_score, repo_url, github_token, created_at, updated_at
        FROM project ORDER BY id ASC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []Project
        for rows.Next() {
                var p Project
                if err := rows.Scan(&p.ID, &p.Name, &p.Status, &p.TrustScore, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt); err != nil {
                        return nil, err
                }
                out = append(out, p)
        }
        return out, rows.Err()
}

// GetProjectByID returns one project.
func (d *DB) GetProjectByID(ctx context.Context, id int) (*Project, error) {
        row := d.pool.QueryRow(ctx, `
        SELECT id, name, status, trust_score, repo_url, github_token, created_at, updated_at
        FROM project WHERE id=$1`, id)
        p := &Project{}
        err := row.Scan(&p.ID, &p.Name, &p.Status, &p.TrustScore, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt)
        if err != nil {
                return nil, err
        }
        return p, nil
}

// GetActiveProject returns the currently-selected project (full row).
func (d *DB) GetActiveProject(ctx context.Context) (*Project, error) {
        return d.GetProjectByID(ctx, d.ActiveProjectID(ctx))
}

// GetTaskProjectID returns the project_id that owns the given task (via its
// plan). Returns 0 + error when the task is not found.
func (d *DB) GetTaskProjectID(ctx context.Context, taskID int) (int, error) {
        var pid int
        err := d.pool.QueryRow(ctx,
                `SELECT p.project_id FROM tasks t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1`,
                taskID).Scan(&pid)
        return pid, err
}

// CreateProjectFull inserts a project with repo config and returns it.
func (d *DB) CreateProjectFull(ctx context.Context, name, repoURL, githubToken string) (*Project, error) {
        row := d.pool.QueryRow(ctx, `
        INSERT INTO project (name, repo_url, github_token)
        VALUES ($1, $2, $3)
        RETURNING id, name, status, trust_score, repo_url, github_token, created_at, updated_at`,
                name, repoURL, githubToken)
        p := &Project{}
        err := row.Scan(&p.ID, &p.Name, &p.Status, &p.TrustScore, &p.RepoURL, &p.GithubToken, &p.CreatedAt, &p.UpdatedAt)
        return p, err
}

// UpdateProjectRepo updates name + repo_url + github_token for a project.
func (d *DB) UpdateProjectRepo(ctx context.Context, id int, name, repoURL, githubToken string) error {
        _, err := d.pool.Exec(ctx, `
        UPDATE project
        SET name=$2, repo_url=$3, github_token=$4, updated_at=NOW()
        WHERE id=$1`, id, name, repoURL, githubToken)
        return err
}

// DeleteProject removes a project and (cascades / explicit-deletes) all its data.
// Refuses to delete the very last remaining project.
func (d *DB) DeleteProject(ctx context.Context, id int) error {
        var count int
        if err := d.pool.QueryRow(ctx, `SELECT COUNT(*) FROM project`).Scan(&count); err != nil {
                return err
        }
        if count <= 1 {
                return ErrLastProject
        }
        if err := d.ClearProjectData(ctx, id); err != nil {
                return err
        }
        _, err := d.pool.Exec(ctx, `DELETE FROM project WHERE id=$1`, id)
        return err
}

// ClearProjectData removes all chats, plans, tasks, jobs, suggestions,
// clarifications, traces, logs and docs for a project — but keeps the
// project row itself.
func (d *DB) ClearProjectData(ctx context.Context, id int) error {
        stmts := []string{
                `DELETE FROM messages       WHERE project_id=$1`,
                `DELETE FROM clarifications WHERE project_id=$1`,
                `DELETE FROM suggestions    WHERE project_id=$1`,
                `DELETE FROM agent_trace    WHERE project_id=$1`,
                `DELETE FROM task_log       WHERE project_id=$1`,
                `DELETE FROM builder_jobs   WHERE project_id=$1`,
                `DELETE FROM tasks WHERE plan_id IN (SELECT id FROM plans WHERE project_id=$1)`,
                `DELETE FROM plans          WHERE project_id=$1`,
                `DELETE FROM doc_chunks     WHERE project_id=$1`,
                `DELETE FROM project_docs   WHERE project_id=$1`,
        }
        for _, s := range stmts {
                if _, err := d.pool.Exec(ctx, s, id); err != nil {
                        return err
                }
        }
        return nil
}

// ErrLastProject is returned when DeleteProject is called on the only project.
var ErrLastProject = constErr("cannot delete the last remaining project")

type constErr string

func (e constErr) Error() string { return string(e) }
