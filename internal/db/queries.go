package db

import (
        "context"
        "database/sql"
        "errors"
        "fmt"
        "time"
)

// ─── Memories ──────────────────────────────────────────────────────────────

type Memory struct {
        ID        int
        Key       string
        Content   string
        UpdatedAt time.Time
}

func (d *DB) PutMemory(ctx context.Context, projectID int, key, content string) error {
        _, err := d.db.ExecContext(ctx,
                `INSERT INTO memories (project_id, key, content, updated_at)
         VALUES (?,?,?,CURRENT_TIMESTAMP)
         ON CONFLICT (project_id, key)
         DO UPDATE SET content=excluded.content, updated_at=CURRENT_TIMESTAMP`,
                projectID, key, content)
        return err
}

func (d *DB) GetMemory(ctx context.Context, projectID int, key string) (*Memory, error) {
        var m Memory
        var updStr string
        err := d.db.QueryRowContext(ctx,
                `SELECT id, key, content, updated_at FROM memories
         WHERE project_id=? AND key=?`, projectID, key).
                Scan(&m.ID, &m.Key, &m.Content, &updStr)
        if errors.Is(err, sql.ErrNoRows) {
                return nil, ErrNotFound
        }
        if err != nil {
                return nil, err
        }
        m.UpdatedAt, err = parseDateTime(updStr)
        if err != nil {
                return nil, fmt.Errorf("parse updated_at: %w", err)
        }
        return &m, nil
}

func (d *DB) ListMemories(ctx context.Context, projectID int) ([]Memory, error) {
        rows, err := d.db.QueryContext(ctx,
                `SELECT id, key, content, updated_at FROM memories
         WHERE project_id=? ORDER BY updated_at DESC`, projectID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []Memory
        for rows.Next() {
                var m Memory
                var updStr string
                if scanErr := rows.Scan(&m.ID, &m.Key, &m.Content, &updStr); scanErr != nil {
                        return nil, scanErr
                }
                var parseErr error
                m.UpdatedAt, parseErr = parseDateTime(updStr)
                if parseErr != nil {
                        return nil, fmt.Errorf("parse updated_at: %w", parseErr)
                }
                out = append(out, m)
        }
        return out, rows.Err()
}

func (d *DB) DeleteMemory(ctx context.Context, projectID int, key string) error {
        _, err := d.db.ExecContext(ctx,
                "DELETE FROM memories WHERE project_id=? AND key=?", projectID, key)
        return err
}

// GetProjectScratchpad returns the stored scratchpad for a project.
func (d *DB) GetProjectScratchpad(ctx context.Context, projectID int) (string, error) {
        var s string
        err := d.db.QueryRowContext(ctx, "SELECT scratchpad FROM projects WHERE id=?", projectID).Scan(&s)
        if errors.Is(err, sql.ErrNoRows) {
                return "", ErrNotFound
        }
        return s, err
}

// SetProjectScratchpad saves scratchpad content for a project.
func (d *DB) SetProjectScratchpad(ctx context.Context, projectID int, content string) error {
        _, err := d.db.ExecContext(ctx,
                "UPDATE projects SET scratchpad=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
                content, projectID)
        return err
}

// parseDateTime handles SQLite datetime strings in multiple formats.
func parseDateTime(s string) (time.Time, error) {
        formats := []string{
                time.RFC3339,
                "2006-01-02T15:04:05Z",
                "2006-01-02 15:04:05",
                "2006-01-02 15:04:05Z",
        }
        for _, f := range formats {
                if t, err := time.Parse(f, s); err == nil {
                        return t, nil
                }
        }
        return time.Time{}, fmt.Errorf("unrecognised datetime format: %q", s)
}
