package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ pool *pgxpool.Pool }

var ErrNotFound = errors.New("not found")

func New(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 5
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	d := &DB{pool: pool}
	if err := d.migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) Close() { d.pool.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id            SERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS memories (
    id          SERIAL PRIMARY KEY,
    project_id  INTEGER NOT NULL,
    key         TEXT NOT NULL,
    content     TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, key)
);
CREATE INDEX IF NOT EXISTS memories_project_idx ON memories(project_id);
`

func (d *DB) migrate(ctx context.Context) error {
	// Drop FK from memories before dropping project table.
	_, _ = d.pool.Exec(ctx, "ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_project_id_fkey")

	// Drop tables removed from the schema.
	removed := []string{"project", "project_docs", "llm_usage"}
	for _, t := range removed {
		if _, err := d.pool.Exec(ctx, "DROP TABLE IF EXISTS "+t+" CASCADE"); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}

	// Drop old legacy tables.
	legacy := []string{
		"agent_trace", "task_log", "manager_notes", "builder_jobs",
		"tasks", "plans", "clarifications", "suggestions",
		"doc_chunks", "messages", "kv",
	}
	for _, t := range legacy {
		if _, err := d.pool.Exec(ctx, "DROP TABLE IF EXISTS "+t+" CASCADE"); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}

	if _, err := d.pool.Exec(ctx, schema); err != nil {
		return err
	}
	return nil
}

// ─── Users / Sessions ──────────────────────────────────────────────────────

func (d *DB) HasUser(ctx context.Context) bool {
	var n int
	_ = d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n > 0
}

func (d *DB) CreateUser(ctx context.Context, username, passwordHash string) error {
	_, err := d.pool.Exec(ctx,
		"INSERT INTO users (username, password_hash) VALUES ($1,$2)",
		username, passwordHash)
	return err
}

func (d *DB) GetUserPasswordHash(ctx context.Context, username string) (string, error) {
	var h string
	err := d.pool.QueryRow(ctx,
		"SELECT password_hash FROM users WHERE username=$1", username).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (d *DB) CreateSession(ctx context.Context, token, username string, expires time.Time) error {
	_, err := d.pool.Exec(ctx,
		"INSERT INTO sessions (token, username, expires_at) VALUES ($1,$2,$3)",
		token, username, expires)
	return err
}

func (d *DB) GetSession(ctx context.Context, token string) (string, time.Time, error) {
	var u string
	var exp time.Time
	err := d.pool.QueryRow(ctx,
		"SELECT username, expires_at FROM sessions WHERE token=$1", token).Scan(&u, &exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrNotFound
	}
	return u, exp, err
}

func (d *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := d.pool.Exec(ctx, "DELETE FROM sessions WHERE token=$1", token)
	return err
}
