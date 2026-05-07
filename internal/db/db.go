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
        id           SERIAL PRIMARY KEY,
        name         TEXT NOT NULL DEFAULT 'default',
        status       TEXT NOT NULL DEFAULT 'new',
        trust_score  FLOAT NOT NULL DEFAULT 0,
        repo_url     TEXT NOT NULL DEFAULT '',
        github_token TEXT NOT NULL DEFAULT '',
        created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
        retry_count  INTEGER NOT NULL DEFAULT 0,
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
        if _, err := d.pool.Exec(ctx, schema); err != nil {
                return err
        }
        _, _ = d.pool.Exec(ctx,
                `ALTER TABLE builder_jobs ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0`)
        if _, err := d.pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS agent_trace (
            id         SERIAL PRIMARY KEY,
            scope      TEXT NOT NULL DEFAULT '',
            event      TEXT NOT NULL DEFAULT '',
            detail     TEXT NOT NULL DEFAULT '',
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
        CREATE INDEX IF NOT EXISTS agent_trace_id_desc ON agent_trace(id DESC);`); err != nil {
                return err
        }

        // Per-project columns. Default 1 so existing rows get attached to the
        // bootstrapped default project below.
        projectScopedTables := []string{
                "plans", "messages", "clarifications", "suggestions",
                "agent_trace", "project_docs", "doc_chunks", "task_log", "builder_jobs",
        }
        for _, t := range projectScopedTables {
                if _, err := d.pool.Exec(ctx,
                        `ALTER TABLE `+t+` ADD COLUMN IF NOT EXISTS project_id INTEGER NOT NULL DEFAULT 1`); err != nil {
                        return fmt.Errorf("add project_id to %s: %w", t, err)
                }
                _, _ = d.pool.Exec(ctx,
                        `CREATE INDEX IF NOT EXISTS `+t+`_project_id_idx ON `+t+`(project_id)`)

                // Add ON DELETE CASCADE FK so deleting a project automatically
                // wipes all its rows. Idempotent: skipped if FK already exists.
                fkName := t + "_project_id_fkey"
                _, _ = d.pool.Exec(ctx, `
                DO $$
                BEGIN
                    IF NOT EXISTS (
                        SELECT 1 FROM pg_constraint WHERE conname = '`+fkName+`'
                    ) THEN
                        ALTER TABLE `+t+`
                            ADD CONSTRAINT `+fkName+`
                            FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE CASCADE;
                    END IF;
                END $$;`)
        }
        // New project columns on the project table itself (older deployments).
        _, _ = d.pool.Exec(ctx, `ALTER TABLE project ADD COLUMN IF NOT EXISTS repo_url     TEXT NOT NULL DEFAULT ''`)
        _, _ = d.pool.Exec(ctx, `ALTER TABLE project ADD COLUMN IF NOT EXISTS github_token TEXT NOT NULL DEFAULT ''`)

        // Phase-at-a-time planning: outline holds the high-level phase list
        // (JSON array of {number,title}) and current_phase is the phase the
        // Manager has currently detailed into tasks. On merge of the last task
        // of current_phase the Manager is re-invoked to detail the next phase.
        _, _ = d.pool.Exec(ctx, `ALTER TABLE plans ADD COLUMN IF NOT EXISTS outline       TEXT    NOT NULL DEFAULT ''`)
        _, _ = d.pool.Exec(ctx, `ALTER TABLE plans ADD COLUMN IF NOT EXISTS current_phase INTEGER NOT NULL DEFAULT 1`)
        _, _ = d.pool.Exec(ctx, `ALTER TABLE plans ADD COLUMN IF NOT EXISTS goal_summary  TEXT    NOT NULL DEFAULT ''`)

        // Seed a default project ONLY when the table is completely empty (i.e.
        // a brand-new install). Previously we re-inserted id=1 on every boot,
        // which caused the "default project keeps coming back after delete"
        // bug. Now, once a user deletes the default and creates their own,
        // the default never resurrects.
        if _, err := d.pool.Exec(ctx, `
            INSERT INTO project (name)
            SELECT 'default'
            WHERE NOT EXISTS (SELECT 1 FROM project)`); err != nil {
                return fmt.Errorf("seed default project: %w", err)
        }
        _, _ = d.pool.Exec(ctx,
                `SELECT setval(pg_get_serial_sequence('project','id'),
                              GREATEST(COALESCE((SELECT MAX(id) FROM project), 1), 1))`)

        return nil
}
