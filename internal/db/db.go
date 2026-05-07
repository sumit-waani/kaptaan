package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool for all database operations.
type DB struct {
	pool *pgxpool.Pool
}

// New connects to Postgres and initialises the schema.
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

// Pool exposes the underlying pool (for transactions).
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

const schema = `
CREATE TABLE IF NOT EXISTS project (
        id          SERIAL PRIMARY KEY,
        name        TEXT NOT NULL DEFAULT 'default',
        status      TEXT NOT NULL DEFAULT 'new',
        trust_score FLOAT NOT NULL DEFAULT 0,
        created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS project_docs (
        id          SERIAL PRIMARY KEY,
        filename    TEXT NOT NULL,
        raw_content TEXT NOT NULL,
        created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS doc_chunks (
        id          SERIAL PRIMARY KEY,
        doc_id      INTEGER NOT NULL REFERENCES project_docs(id) ON DELETE CASCADE,
        chunk_text  TEXT NOT NULL,
        tags        TEXT[] NOT NULL DEFAULT '{}',
        relevance   TEXT NOT NULL DEFAULT '',
        created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plans (
        id         SERIAL PRIMARY KEY,
        version    INTEGER NOT NULL DEFAULT 1,
        status     TEXT NOT NULL DEFAULT 'active',
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tasks (
        id            SERIAL PRIMARY KEY,
        plan_id       INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
        parent_id     INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
        phase         INTEGER NOT NULL DEFAULT 1,
        title         TEXT NOT NULL,
        description   TEXT NOT NULL DEFAULT '',
        status        TEXT NOT NULL DEFAULT 'pending',
        is_suggestion BOOLEAN NOT NULL DEFAULT FALSE,
        pr_url        TEXT NOT NULL DEFAULT '',
        created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS builder_jobs (
        id           SERIAL PRIMARY KEY,
        task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
        status       TEXT NOT NULL DEFAULT 'queued',
        branch       TEXT NOT NULL DEFAULT '',
        pr_url       TEXT NOT NULL DEFAULT '',
        pr_number    INTEGER NOT NULL DEFAULT 0,
        diff_summary TEXT NOT NULL DEFAULT '',
        test_output  TEXT NOT NULL DEFAULT '',
        build_output TEXT NOT NULL DEFAULT '',
        started_at   TIMESTAMPTZ,
        finished_at  TIMESTAMPTZ,
        created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS manager_notes (
        id         SERIAL PRIMARY KEY,
        job_id     INTEGER NOT NULL REFERENCES builder_jobs(id) ON DELETE CASCADE,
        note       TEXT NOT NULL DEFAULT '',
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS task_log (
        id         SERIAL PRIMARY KEY,
        task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
        event      TEXT NOT NULL,
        payload    TEXT NOT NULL DEFAULT '',
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS clarifications (
        id          SERIAL PRIMARY KEY,
        question    TEXT NOT NULL,
        answer      TEXT NOT NULL DEFAULT '',
        answered_at TIMESTAMPTZ,
        created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS suggestions (
        id          SERIAL PRIMARY KEY,
        title       TEXT NOT NULL,
        description TEXT NOT NULL DEFAULT '',
        task_plan   TEXT NOT NULL DEFAULT '',
        status      TEXT NOT NULL DEFAULT 'pending',
        created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS kv (
        key        TEXT PRIMARY KEY,
        value      TEXT NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS messages (
        id         SERIAL PRIMARY KEY,
        role       TEXT NOT NULL,
        content    TEXT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS llm_usage (
        id                SERIAL PRIMARY KEY,
        provider          TEXT NOT NULL,
        model             TEXT NOT NULL,
        prompt_tokens     INTEGER NOT NULL DEFAULT 0,
        completion_tokens INTEGER NOT NULL DEFAULT 0,
        total_tokens      INTEGER NOT NULL DEFAULT 0,
        created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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
`

func (d *DB) migrate(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, schema)
	return err
}
