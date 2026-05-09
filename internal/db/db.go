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
	return d, nil
}

func (d *DB) Close() { d.db.Close() }

const schema = `
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
    project_id  INTEGER NOT NULL,
    key         TEXT NOT NULL,
    content     TEXT NOT NULL,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (project_id, key)
);
CREATE INDEX IF NOT EXISTS memories_project_idx ON memories(project_id);
`

func (d *DB) migrate() error {
	if _, err := d.db.Exec(schema); err != nil {
		return err
	}
	return nil
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
