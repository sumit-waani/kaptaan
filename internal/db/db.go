package db

import (
        "context"
        "database/sql"
        "errors"
        "fmt"
        "os"
        "time"

        _ "modernc.org/sqlite"
)

type DB struct{ db *sql.DB }

var ErrNotFound = errors.New("not found")

func New(_ context.Context, _ string) (*DB, error) {
        path := os.Getenv("DB_PATH")
        if path == "" {
                path = "kaptaan.db"
        }
        sqldb, err := sql.Open("sqlite", path)
        if err != nil {
                return nil, fmt.Errorf("open sqlite: %w", err)
        }
        sqldb.SetMaxOpenConns(1)
        d := &DB{db: sqldb}
        if err := d.migrate(); err != nil {
                return nil, fmt.Errorf("migrate: %w", err)
        }
        // Seed default project so existing config rows migrate cleanly.
        if err := d.EnsureDefaultProject(context.Background()); err != nil {
                return nil, fmt.Errorf("ensure default project: %w", err)
        }
        return d, nil
}

func (d *DB) Close() { d.db.Close() }

// schemaBase creates the core tables (no indexes that reference columns added
// via ALTER TABLE, so this is safe to run against any existing schema).
const schemaBase = `
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS memories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT NOT NULL,
    content     TEXT NOT NULL,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id                INTEGER  PRIMARY KEY AUTOINCREMENT,
    role              TEXT     NOT NULL DEFAULT '',
    content           TEXT     NOT NULL DEFAULT '',
    reasoning_content TEXT     NOT NULL DEFAULT '',
    tool_call_id      TEXT     NOT NULL DEFAULT '',
    tool_calls        TEXT     NOT NULL DEFAULT '',
    ui_type           TEXT     NOT NULL DEFAULT '',
    ui_text           TEXT     NOT NULL DEFAULT '',
    ui_ts             TEXT     NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT    NOT NULL,
    value      TEXT    NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func (d *DB) migrate() error {
        // Step 1: create tables without any column references that may not exist yet.
        if _, err := d.db.Exec(schemaBase); err != nil {
                return err
        }

        // Step 2: additive column migrations — safe to re-run; duplicate-column errors are ignored.
        alterations := []string{
                `ALTER TABLE config    ADD COLUMN project_id INTEGER NOT NULL DEFAULT 1`,
                `ALTER TABLE memories  ADD COLUMN project_id INTEGER NOT NULL DEFAULT 1`,
                `ALTER TABLE messages  ADD COLUMN project_id INTEGER NOT NULL DEFAULT 1`,
                `ALTER TABLE config    ADD COLUMN UNIQUE_project_key TEXT`,   // placeholder, dropped below
        }
        // Only the first three matter; drop the placeholder entry.
        alterations = alterations[:3]
        for _, stmt := range alterations {
                if _, err := d.db.Exec(stmt); err != nil {
                        if !isSQLiteColumnExists(err) {
                                return err
                        }
                }
        }

        // Step 3: create indexes and unique constraints now that project_id exists everywhere.
        indexes := []string{
                `CREATE INDEX IF NOT EXISTS memories_project_idx ON memories(project_id)`,
                `CREATE INDEX IF NOT EXISTS messages_project_idx ON messages(project_id)`,
                `CREATE INDEX IF NOT EXISTS config_project_idx   ON config(project_id)`,
                `CREATE UNIQUE INDEX IF NOT EXISTS memories_project_key_uidx ON memories(project_id, key)`,
                `CREATE UNIQUE INDEX IF NOT EXISTS config_project_key_uidx   ON config(project_id, key)`,
        }
        for _, stmt := range indexes {
                if _, err := d.db.Exec(stmt); err != nil {
                        return err
                }
        }
        return nil
}

// isSQLiteColumnExists returns true when the error is SQLite's "duplicate column name" error,
// which is raised when we try to ADD a column that already exists.
func isSQLiteColumnExists(err error) bool {
        if err == nil {
                return false
        }
        s := err.Error()
        return contains(s, "duplicate column name") || contains(s, "already exists")
}

func contains(s, sub string) bool {
        return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
        for i := 0; i <= len(s)-len(sub); i++ {
                if s[i:i+len(sub)] == sub {
                        return true
                }
        }
        return false
}

// ─── Projects ───────────────────────────────────────────────────────────────

type Project struct {
        ID        int
        Name      string
        CreatedAt string
        UpdatedAt string
}

// EnsureDefaultProject creates project 1 named "Default" if it doesn't exist.
// Safe to call on every startup.
func (d *DB) EnsureDefaultProject(ctx context.Context) error {
        _, err := d.db.ExecContext(ctx,
                "INSERT OR IGNORE INTO projects (id, name) VALUES (1, 'Default')")
        return err
}

// ListProjects returns all projects ordered by id.
func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
        rows, err := d.db.QueryContext(ctx,
                "SELECT id, name, created_at, updated_at FROM projects ORDER BY id")
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []Project
        for rows.Next() {
                var p Project
                if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
                        return nil, err
                }
                out = append(out, p)
        }
        return out, rows.Err()
}

// CreateProject inserts a new project and returns its ID.
func (d *DB) CreateProject(ctx context.Context, name string) (int, error) {
        res, err := d.db.ExecContext(ctx,
                "INSERT INTO projects (name) VALUES (?)", name)
        if err != nil {
                return 0, err
        }
        id, err := res.LastInsertId()
        return int(id), err
}

// DeleteProject deletes a project and all its associated data.
func (d *DB) DeleteProject(ctx context.Context, projectID int) error {
        _, _ = d.db.ExecContext(ctx, "DELETE FROM messages WHERE project_id=?", projectID)
        _, _ = d.db.ExecContext(ctx, "DELETE FROM memories WHERE project_id=?", projectID)
        _, _ = d.db.ExecContext(ctx, "DELETE FROM config WHERE project_id=?", projectID)
        _, err := d.db.ExecContext(ctx, "DELETE FROM projects WHERE id=?", projectID)
        return err
}

// RenameProject updates the name of a project.
func (d *DB) RenameProject(ctx context.Context, projectID int, name string) error {
        _, err := d.db.ExecContext(ctx,
                "UPDATE projects SET name=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
                name, projectID)
        return err
}

// ─── Users / Sessions ──────────────────────────────────────────────────────

func (d *DB) HasUser(ctx context.Context) bool {
        var n int
        _ = d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
        return n > 0
}

func (d *DB) CreateUser(ctx context.Context, username, passwordHash string) error {
        _, err := d.db.ExecContext(ctx,
                "INSERT INTO users (username, password_hash) VALUES (?,?)",
                username, passwordHash)
        return err
}

func (d *DB) GetUserPasswordHash(ctx context.Context, username string) (string, error) {
        var h string
        err := d.db.QueryRowContext(ctx,
                "SELECT password_hash FROM users WHERE username=?", username).Scan(&h)
        if errors.Is(err, sql.ErrNoRows) {
                return "", ErrNotFound
        }
        return h, err
}

func (d *DB) CreateSession(ctx context.Context, token, username string, expires time.Time) error {
        _, err := d.db.ExecContext(ctx,
                "INSERT INTO sessions (token, username, expires_at) VALUES (?,?,?)",
                token, username, expires.UTC().Format(time.RFC3339))
        return err
}

func (d *DB) GetSession(ctx context.Context, token string) (string, time.Time, error) {
        var u string
        var expStr string
        err := d.db.QueryRowContext(ctx,
                "SELECT username, expires_at FROM sessions WHERE token=?", token).Scan(&u, &expStr)
        if errors.Is(err, sql.ErrNoRows) {
                return "", time.Time{}, ErrNotFound
        }
        if err != nil {
                return "", time.Time{}, err
        }
        exp, err := time.Parse(time.RFC3339, expStr)
        if err != nil {
                return "", time.Time{}, fmt.Errorf("parse expires_at: %w", err)
        }
        return u, exp, nil
}

func (d *DB) DeleteSession(ctx context.Context, token string) error {
        _, err := d.db.ExecContext(ctx, "DELETE FROM sessions WHERE token=?", token)
        return err
}
