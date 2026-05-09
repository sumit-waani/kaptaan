package db

import (
        "context"
        "database/sql"
        "errors"
)

// GetConfig returns the stored value for key, or "" if not found.
func (d *DB) GetConfig(ctx context.Context, key string) string {
        var v string
        err := d.db.QueryRowContext(ctx, "SELECT value FROM config WHERE key=?", key).Scan(&v)
        if errors.Is(err, sql.ErrNoRows) || err != nil {
                return ""
        }
        return v
}

// SetConfig upserts a config key/value pair.
func (d *DB) SetConfig(ctx context.Context, key, value string) error {
        _, err := d.db.ExecContext(ctx, `
                INSERT INTO config (key, value, updated_at)
                VALUES (?, ?, CURRENT_TIMESTAMP)
                ON CONFLICT (key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
                key, value)
        return err
}

// ListConfig returns all config entries as a key→value map.
func (d *DB) ListConfig(ctx context.Context) (map[string]string, error) {
        rows, err := d.db.QueryContext(ctx, "SELECT key, value FROM config ORDER BY key")
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        out := make(map[string]string)
        for rows.Next() {
                var k, v string
                if err := rows.Scan(&k, &v); err != nil {
                        return nil, err
                }
                out[k] = v
        }
        return out, rows.Err()
}
