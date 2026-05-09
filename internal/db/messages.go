package db

import (
        "context"
        "encoding/json"
)

// DBMessage is one row from the messages table.
type DBMessage struct {
        ID               int
        Role             string
        Content          string
        ReasoningContent string
        ToolCallID       string
        ToolCalls        string // JSON array string, may be empty
        UIType           string // "user" | "message" | "ask" | "reply" | "" (empty = LLM-only row)
        UIText           string
        UITs             string
}

// AppendMessage inserts one row into the messages table.
// Use role/content/etc for LLM rows; use uiType/uiText/uiTs for UI display rows.
// A row can carry both (e.g. user message) or only one set.
func (d *DB) AppendMessage(ctx context.Context, projectID int, role, content, reasoningContent, toolCallID, toolCalls, uiType, uiText, uiTs string) error {
        _, err := d.db.ExecContext(ctx, `
                INSERT INTO messages
                  (project_id, role, content, reasoning_content, tool_call_id, tool_calls, ui_type, ui_text, ui_ts)
                VALUES (?,?,?,?,?,?,?,?,?)`,
                projectID, role, content, reasoningContent, toolCallID, toolCalls, uiType, uiText, uiTs)
        return err
}

// LoadConvo returns the last 199 LLM messages (role != '' and role != 'system')
// for the given project, ordered oldest-first. The caller prepends the system prompt.
func (d *DB) LoadConvo(ctx context.Context, projectID int) ([]DBMessage, error) {
        rows, err := d.db.QueryContext(ctx, `
                SELECT id, role, content, reasoning_content, tool_call_id, tool_calls
                FROM messages
                WHERE project_id=? AND role != '' AND role != 'system'
                ORDER BY id DESC LIMIT 199`, projectID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []DBMessage
        for rows.Next() {
                var m DBMessage
                if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.ReasoningContent, &m.ToolCallID, &m.ToolCalls); err != nil {
                        return nil, err
                }
                out = append(out, m)
        }
        if err := rows.Err(); err != nil {
                return nil, err
        }
        // reverse: DB returned newest-first, we need oldest-first for LLM
        for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
                out[i], out[j] = out[j], out[i]
        }
        return out, nil
}

// LoadUIMessages returns all rows that have a ui_type set, ordered oldest-first.
// Used to replay the chat history to a reconnecting SSE client.
func (d *DB) LoadUIMessages(ctx context.Context, projectID int) ([]DBMessage, error) {
        rows, err := d.db.QueryContext(ctx, `
                SELECT id, ui_type, ui_text, ui_ts
                FROM messages
                WHERE project_id=? AND ui_type != ''
                ORDER BY id ASC`, projectID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []DBMessage
        for rows.Next() {
                var m DBMessage
                if err := rows.Scan(&m.ID, &m.UIType, &m.UIText, &m.UITs); err != nil {
                        return nil, err
                }
                out = append(out, m)
        }
        return out, rows.Err()
}

// ClearMessages deletes all messages for a project (both LLM and UI rows).
func (d *DB) ClearMessages(ctx context.Context, projectID int) error {
        _, err := d.db.ExecContext(ctx, "DELETE FROM messages WHERE project_id=?", projectID)
        return err
}

// PruneConvo deletes the oldest `n` LLM rows (role != '' and role != 'system')
// for the given project to keep the stored history within the cap.
func (d *DB) PruneConvo(ctx context.Context, projectID, n int) error {
        _, err := d.db.ExecContext(ctx, `
                DELETE FROM messages
                WHERE id IN (
                        SELECT id FROM messages
                        WHERE project_id=? AND role != '' AND role != 'system'
                        ORDER BY id ASC LIMIT ?
                )`, projectID, n)
        return err
}

// ToolCallsToJSON marshals a slice of tool-call structs to a JSON string.
// Returns "" on empty input so we don't write "null" to the DB.
func ToolCallsToJSON(v interface{}) string {
        if v == nil {
                return ""
        }
        b, err := json.Marshal(v)
        if err != nil || string(b) == "null" {
                return ""
        }
        return string(b)
}
