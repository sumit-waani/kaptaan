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
        dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
        sqldb, err := sql.Open("sqlite", dsn)
        if err != nil {
                return nil, fmt.Errorf("open sqlite: %w", err)
        }
        sqldb.SetMaxOpenConns(1)
        d := &DB{db: sqldb}
        if err := d.migrate(); err != nil {
                return nil, fmt.Errorf("migrate: %w", err)
        }
        if err := d.EnsureDefaultProject(context.Background()); err != nil {
                return nil, fmt.Errorf("ensure default project: %w", err)
        }
        return d, nil
}

func (d *DB) Close() { d.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    name       TEXT     NOT NULL,
    scratchpad TEXT     NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER  NOT NULL DEFAULT 1,
    key        TEXT     NOT NULL,
    value      TEXT     NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (project_id, key)
);
CREATE INDEX IF NOT EXISTS config_project_idx ON config(project_id);

CREATE TABLE IF NOT EXISTS memories (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER  NOT NULL DEFAULT 1,
    key        TEXT     NOT NULL,
    content    TEXT     NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (project_id, key)
);
CREATE INDEX IF NOT EXISTS memories_project_idx ON memories(project_id);

CREATE TABLE IF NOT EXISTS messages (
    id                INTEGER  PRIMARY KEY AUTOINCREMENT,
    project_id        INTEGER  NOT NULL DEFAULT 1,
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
CREATE INDEX IF NOT EXISTS messages_project_idx ON messages(project_id);
`

func (d *DB) migrate() error {
        _, err := d.db.Exec(schema)
        return err
}

// ─── Projects ────────────────────────────────────────────────────────────────

type Project struct {
        ID        int    `json:"id"`
        Name      string `json:"name"`
        CreatedAt string `json:"created_at"`
        UpdatedAt string `json:"updated_at"`
}

func (d *DB) EnsureDefaultProject(ctx context.Context) error {
        _, err := d.db.ExecContext(ctx,
                "INSERT OR IGNORE INTO projects (id, name) VALUES (1, 'Default')")
        return err
}

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

func (d *DB) CreateProject(ctx context.Context, name string) (int, error) {
        res, err := d.db.ExecContext(ctx,
                "INSERT INTO projects (name) VALUES (?)", name)
        if err != nil {
                return 0, err
        }
        id, err := res.LastInsertId()
        return int(id), err
}

func (d *DB) DeleteProject(ctx context.Context, projectID int) error {
        _, _ = d.db.ExecContext(ctx, "DELETE FROM messages WHERE project_id=?", projectID)
        _, _ = d.db.ExecContext(ctx, "DELETE FROM memories WHERE project_id=?", projectID)
        _, _ = d.db.ExecContext(ctx, "DELETE FROM config WHERE project_id=?", projectID)
        _, err := d.db.ExecContext(ctx, "DELETE FROM projects WHERE id=?", projectID)
        return err
}

func (d *DB) RenameProject(ctx context.Context, projectID int, name string) error {
        _, err := d.db.ExecContext(ctx,
                "UPDATE projects SET name=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
                name, projectID)
        return err
}

// ─── Users / Sessions ────────────────────────────────────────────────────────

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
        var u, expStr string
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
