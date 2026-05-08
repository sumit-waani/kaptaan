package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ─── Memories ──────────────────────────────────────────────────────────────

type Memory struct {
	ID        int
	Key       string
	Content   string
	UpdatedAt time.Time
}

func (d *DB) PutMemory(ctx context.Context, projectID int, key, content string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO memories (project_id, key, content, updated_at)
         VALUES ($1,$2,$3,NOW())
         ON CONFLICT (project_id, key)
         DO UPDATE SET content=EXCLUDED.content, updated_at=NOW()`,
		projectID, key, content)
	return err
}

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

func (d *DB) DeleteMemory(ctx context.Context, projectID int, key string) error {
	_, err := d.pool.Exec(ctx,
		"DELETE FROM memories WHERE project_id=$1 AND key=$2", projectID, key)
	return err
}
