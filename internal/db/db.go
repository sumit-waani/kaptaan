// Package db is a thin pgx wrapper that owns the (six-table) schema and
// every SQL query in Kaptaan. Single-user, multi-project, no KV, no agent
// state — GitHub is the source of truth for PR/merge state, plan files on
// disk are the source of truth for in-flight work.
package db

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool.
type DB struct{ pool *pgxpool.Pool }

// ErrNotFound is returned when a row is missing.
var ErrNotFound = errors.New("not found")

// ErrLastProject blocks deletion of the only remaining project.
var ErrLastProject = errors.New("cannot delete the last project")

// New connects, pings, runs migrations, and returns a ready DB.
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

// Close releases all connections.
func (d *DB) Close() { d.pool.Close() }

// Six tables, period. No kv, no messages, no plans/tasks/jobs/logs.
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

CREATE TABLE IF NOT EXISTS project (
    id           SERIAL PRIMARY KEY,
    name         TEXT NOT NULL,
    repo_url     TEXT NOT NULL DEFAULT '',
    github_token TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS project_docs (
    id          SERIAL PRIMARY KEY,
    project_id  INTEGER NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS project_docs_project_idx ON project_docs(project_id);

CREATE TABLE IF NOT EXISTS memories (
    id          SERIAL PRIMARY KEY,
    project_id  INTEGER NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    content     TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, key)
);
CREATE INDEX IF NOT EXISTS memories_project_idx ON memories(project_id);

CREATE TABLE IF NOT EXISTS llm_usage (
    id                SERIAL PRIMARY KEY,
    project_id        INTEGER REFERENCES project(id) ON DELETE SET NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS llm_usage_created_idx ON llm_usage(created_at);
`

// migrate is idempotent: it drops only genuinely obsolete legacy tables that
// no longer exist in the schema, then runs CREATE TABLE IF NOT EXISTS for all
// current tables. User data (docs, memories, usage) is never touched.
func (d *DB) migrate(ctx context.Context) error {
        // Drop legacy tables from the old architecture. These never held
        // user-authored content — GitHub and plan files on disk were the source
        // of truth. Safe to drop unconditionally because the new schema doesn't
        // reference them.
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
        // Trim legacy columns from project — best-effort, ignored if absent.
        _, _ = d.pool.Exec(ctx, "ALTER TABLE project DROP COLUMN IF EXISTS status")
        _, _ = d.pool.Exec(ctx, "ALTER TABLE project DROP COLUMN IF EXISTS trust_score")

        // CREATE TABLE IF NOT EXISTS is idempotent; user data is preserved.
        if _, err := d.pool.Exec(ctx, schema); err != nil {
                return err
        }
        return nil
}

// ─── Users / Sessions ──────────────────────────────────────────────────────

// HasUser reports whether at least one account exists.
func (d *DB) HasUser(ctx context.Context) bool {
        var n int
        _ = d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
        return n > 0
}

// CreateUser inserts a single user with the given bcrypt hash.
func (d *DB) CreateUser(ctx context.Context, username, passwordHash string) error {
        _, err := d.pool.Exec(ctx,
                "INSERT INTO users (username, password_hash) VALUES ($1,$2)",
                username, passwordHash)
        return err
}

// GetUserPasswordHash returns the bcrypt hash for a username.
func (d *DB) GetUserPasswordHash(ctx context.Context, username string) (string, error) {
        var h string
        err := d.pool.QueryRow(ctx,
                "SELECT password_hash FROM users WHERE username=$1", username).Scan(&h)
        if errors.Is(err, pgx.ErrNoRows) {
                return "", ErrNotFound
        }
        return h, err
}

// CreateSession stores a new session token.
func (d *DB) CreateSession(ctx context.Context, token, username string, expires time.Time) error {
        _, err := d.pool.Exec(ctx,
                "INSERT INTO sessions (token, username, expires_at) VALUES ($1,$2,$3)",
                token, username, expires)
        return err
}

// GetSession looks up a session by token.
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

// DeleteSession removes a session by token.
func (d *DB) DeleteSession(ctx context.Context, token string) error {
        _, err := d.pool.Exec(ctx, "DELETE FROM sessions WHERE token=$1", token)
        return err
}
