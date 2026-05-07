package db

import (
        "context"
        "time"
)

// ─── Models ────────────────────────────────────────────────────────────────

type Project struct {
        ID         int
        Name       string
        Status     string
        TrustScore float64
        CreatedAt  time.Time
        UpdatedAt  time.Time
}

type ProjectDoc struct {
        ID         int
        Filename   string
        RawContent string
        CreatedAt  time.Time
}

type DocChunk struct {
        ID        int
        DocID     int
        ChunkText string
        Tags      []string
        Relevance string
        CreatedAt time.Time
}

type Plan struct {
        ID        int
        Version   int
        Status    string
        CreatedAt time.Time
}

type Task struct {
        ID           int
        PlanID       int
        ParentID     *int
        Phase        int
        Title        string
        Description  string
        Status       string
        IsSuggestion bool
        PRURL        string
        CreatedAt    time.Time
        UpdatedAt    time.Time
}

type TaskLog struct {
        ID        int
        TaskID    int
        Event     string
        Payload   string
        CreatedAt time.Time
}

type Clarification struct {
        ID         int
        Question   string
        Answer     string
        AnsweredAt *time.Time
        CreatedAt  time.Time
}

type Suggestion struct {
        ID          int
        Title       string
        Description string
        TaskPlan    string
        Status      string
        CreatedAt   time.Time
        UpdatedAt   time.Time
}

type LLMUsage struct {
        Provider         string `json:"provider"`
        Model            string `json:"model"`
        PromptTokens     int    `json:"prompt_tokens"`
        CompletionTokens int    `json:"completion_tokens"`
        TotalTokens      int    `json:"total_tokens"`
        Calls            int    `json:"calls"`
}

// AgentTrace is one entry in the manager's step-by-step debug trace.
type AgentTrace struct {
        ID        int       `json:"id"`
        Scope     string    `json:"scope"`
        Event     string    `json:"event"`
        Detail    string    `json:"detail"`
        CreatedAt time.Time `json:"created_at"`
}

// ─── Project ───────────────────────────────────────────────────────────────

func (d *DB) GetProject(ctx context.Context) (*Project, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id, name, status, trust_score, created_at, updated_at
                 FROM project ORDER BY id LIMIT 1`)
        p := &Project{}
        err := row.Scan(&p.ID, &p.Name, &p.Status, &p.TrustScore, &p.CreatedAt, &p.UpdatedAt)
        if err != nil {
                return nil, err
        }
        return p, nil
}

func (d *DB) CreateProject(ctx context.Context, name string) (*Project, error) {
        row := d.pool.QueryRow(ctx,
                `INSERT INTO project(name) VALUES($1)
                 RETURNING id, name, status, trust_score, created_at, updated_at`, name)
        p := &Project{}
        err := row.Scan(&p.ID, &p.Name, &p.Status, &p.TrustScore, &p.CreatedAt, &p.UpdatedAt)
        return p, err
}

func (d *DB) UpdateProjectStatus(ctx context.Context, status string) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE project SET status=$1, updated_at=NOW() WHERE id=(SELECT id FROM project LIMIT 1)`,
                status)
        return err
}

func (d *DB) UpdateProjectTrustScore(ctx context.Context, score float64) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE project SET trust_score=$1, updated_at=NOW() WHERE id=(SELECT id FROM project LIMIT 1)`,
                score)
        return err
}

// ─── Docs ──────────────────────────────────────────────────────────────────

func (d *DB) SaveDoc(ctx context.Context, filename, content string) (int, error) {
        var id int
        err := d.pool.QueryRow(ctx,
                `INSERT INTO project_docs(filename, raw_content) VALUES($1,$2) RETURNING id`,
                filename, content).Scan(&id)
        return id, err
}

func (d *DB) SaveDocChunk(ctx context.Context, docID int, text string, tags []string, relevance string) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO doc_chunks(doc_id, chunk_text, tags, relevance) VALUES($1,$2,$3,$4)`,
                docID, text, tags, relevance)
        return err
}

func (d *DB) SearchDocChunks(ctx context.Context, tags []string, limit int) ([]DocChunk, error) {
        // Try tag-overlap search first.
        rows, err := d.pool.Query(ctx,
                `SELECT id, doc_id, chunk_text, tags, relevance FROM doc_chunks
                 WHERE tags && $1
                 ORDER BY created_at DESC LIMIT $2`,
                tags, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var chunks []DocChunk
        for rows.Next() {
                var c DocChunk
                if err := rows.Scan(&c.ID, &c.DocID, &c.ChunkText, &c.Tags, &c.Relevance); err != nil {
                        return nil, err
                }
                chunks = append(chunks, c)
        }
        if err := rows.Err(); err != nil {
                return nil, err
        }

        // Ingestion no longer LLM-tags chunks (everything is tagged "doc"), so
        // callers requesting topical tags would get nothing. Fall back to recent
        // chunks so the Builder can still see documentation.
        if len(chunks) == 0 {
                fbRows, err := d.pool.Query(ctx,
                        `SELECT id, doc_id, chunk_text, tags, relevance FROM doc_chunks
                         ORDER BY created_at DESC LIMIT $1`, limit)
                if err != nil {
                        return nil, err
                }
                defer fbRows.Close()
                for fbRows.Next() {
                        var c DocChunk
                        if err := fbRows.Scan(&c.ID, &c.DocID, &c.ChunkText, &c.Tags, &c.Relevance); err != nil {
                                return nil, err
                        }
                        chunks = append(chunks, c)
                }
                return chunks, fbRows.Err()
        }

        return chunks, nil
}

func (d *DB) GetAllDocChunks(ctx context.Context) ([]DocChunk, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, doc_id, chunk_text, tags, relevance FROM doc_chunks ORDER BY doc_id, id`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var chunks []DocChunk
        for rows.Next() {
                var c DocChunk
                if err := rows.Scan(&c.ID, &c.DocID, &c.ChunkText, &c.Tags, &c.Relevance); err != nil {
                        return nil, err
                }
                chunks = append(chunks, c)
        }
        return chunks, rows.Err()
}

func (d *DB) CountDocChunks(ctx context.Context) (int, error) {
        var n int
        err := d.pool.QueryRow(ctx, `SELECT COUNT(*) FROM doc_chunks`).Scan(&n)
        return n, err
}

// DeleteDoc removes a project doc; chunks are cascade-deleted via FK.
func (d *DB) DeleteDoc(ctx context.Context, id int) error {
        _, err := d.pool.Exec(ctx, `DELETE FROM project_docs WHERE id = $1`, id)
        return err
}

func (d *DB) ListDocs(ctx context.Context) ([]ProjectDoc, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, filename, created_at FROM project_docs ORDER BY created_at DESC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var docs []ProjectDoc
        for rows.Next() {
                var doc ProjectDoc
                if err := rows.Scan(&doc.ID, &doc.Filename, &doc.CreatedAt); err != nil {
                        return nil, err
                }
                docs = append(docs, doc)
        }
        return docs, rows.Err()
}

// ─── Plans ─────────────────────────────────────────────────────────────────

func (d *DB) CreatePlan(ctx context.Context) (*Plan, error) {
        var maxVersion int
        d.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM plans`).Scan(&maxVersion)

        row := d.pool.QueryRow(ctx,
                `INSERT INTO plans(version) VALUES($1) RETURNING id, version, status, created_at`,
                maxVersion+1)
        p := &Plan{}
        err := row.Scan(&p.ID, &p.Version, &p.Status, &p.CreatedAt)
        return p, err
}

func (d *DB) GetActivePlan(ctx context.Context) (*Plan, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id, version, status, created_at FROM plans WHERE status='active' ORDER BY id DESC LIMIT 1`)
        p := &Plan{}
        err := row.Scan(&p.ID, &p.Version, &p.Status, &p.CreatedAt)
        if err != nil {
                return nil, err
        }
        return p, nil
}

func (d *DB) ExhaustPlan(ctx context.Context, planID int) error {
        _, err := d.pool.Exec(ctx, `UPDATE plans SET status='exhausted' WHERE id=$1`, planID)
        return err
}

// ─── Tasks ─────────────────────────────────────────────────────────────────

func (d *DB) CreateTask(ctx context.Context, planID int, parentID *int, phase int, title, desc string, isSuggestion bool) (*Task, error) {
        row := d.pool.QueryRow(ctx,
                `INSERT INTO tasks(plan_id,parent_id,phase,title,description,is_suggestion)
                 VALUES($1,$2,$3,$4,$5,$6)
                 RETURNING id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at`,
                planID, parentID, phase, title, desc, isSuggestion)
        return scanTask(row)
}

func (d *DB) UpdateTaskStatus(ctx context.Context, taskID int, status string) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE tasks SET status=$1, updated_at=NOW() WHERE id=$2`, status, taskID)
        return err
}

func (d *DB) UpdateTaskPR(ctx context.Context, taskID int, prURL string) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE tasks SET pr_url=$1, updated_at=NOW() WHERE id=$2`, prURL, taskID)
        return err
}

func (d *DB) GetTask(ctx context.Context, taskID int) (*Task, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at
                 FROM tasks WHERE id=$1`, taskID)
        return scanTask(row)
}

func (d *DB) GetTasksByPlan(ctx context.Context, planID int) ([]Task, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at
                 FROM tasks WHERE plan_id=$1 ORDER BY phase, id`, planID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        return scanTasks(rows)
}

func (d *DB) GetNextPendingTask(ctx context.Context, planID int) (*Task, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at
                 FROM tasks
                 WHERE plan_id=$1 AND parent_id IS NULL AND status='approved'
                 ORDER BY phase, id LIMIT 1`, planID)
        return scanTask(row)
}

func (d *DB) GetSubtasks(ctx context.Context, parentID int) ([]Task, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at
                 FROM tasks WHERE parent_id=$1 ORDER BY id`, parentID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        return scanTasks(rows)
}

func (d *DB) GetNextPendingSubtask(ctx context.Context, parentID int) (*Task, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id,plan_id,parent_id,phase,title,description,status,is_suggestion,pr_url,created_at,updated_at
                 FROM tasks WHERE parent_id=$1 AND status IN ('pending','approved')
                 ORDER BY id LIMIT 1`, parentID)
        return scanTask(row)
}

// ─── Task Log ──────────────────────────────────────────────────────────────

func (d *DB) LogEvent(ctx context.Context, taskID int, event, payload string) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO task_log(task_id, event, payload) VALUES($1,$2,$3)`,
                taskID, event, payload)
        return err
}

func (d *DB) GetRecentLogs(ctx context.Context, taskID, limit int) ([]TaskLog, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, task_id, event, payload, created_at
                 FROM task_log WHERE task_id=$1 ORDER BY id DESC LIMIT $2`,
                taskID, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var logs []TaskLog
        for rows.Next() {
                var l TaskLog
                if err := rows.Scan(&l.ID, &l.TaskID, &l.Event, &l.Payload, &l.CreatedAt); err != nil {
                        return nil, err
                }
                logs = append(logs, l)
        }
        return logs, rows.Err()
}

func (d *DB) GetGlobalRecentLogs(ctx context.Context, limit int) ([]TaskLog, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, task_id, event, payload, created_at
                 FROM task_log ORDER BY id DESC LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var logs []TaskLog
        for rows.Next() {
                var l TaskLog
                if err := rows.Scan(&l.ID, &l.TaskID, &l.Event, &l.Payload, &l.CreatedAt); err != nil {
                        return nil, err
                }
                logs = append(logs, l)
        }
        return logs, rows.Err()
}

// ─── Clarifications ────────────────────────────────────────────────────────

func (d *DB) CreateClarification(ctx context.Context, question string) (int, error) {
        var id int
        err := d.pool.QueryRow(ctx,
                `INSERT INTO clarifications(question) VALUES($1) RETURNING id`, question).Scan(&id)
        return id, err
}

func (d *DB) AnswerClarification(ctx context.Context, id int, answer string) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE clarifications SET answer=$1, answered_at=NOW() WHERE id=$2`, answer, id)
        return err
}

func (d *DB) GetUnansweredClarification(ctx context.Context) (*Clarification, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id, question, answer, answered_at, created_at
                 FROM clarifications WHERE answered_at IS NULL ORDER BY id LIMIT 1`)
        c := &Clarification{}
        err := row.Scan(&c.ID, &c.Question, &c.Answer, &c.AnsweredAt, &c.CreatedAt)
        if err != nil {
                return nil, err
        }
        return c, nil
}

func (d *DB) GetAllClarifications(ctx context.Context) ([]Clarification, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, question, answer, answered_at, created_at
                 FROM clarifications ORDER BY id`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var cs []Clarification
        for rows.Next() {
                var c Clarification
                if err := rows.Scan(&c.ID, &c.Question, &c.Answer, &c.AnsweredAt, &c.CreatedAt); err != nil {
                        return nil, err
                }
                cs = append(cs, c)
        }
        return cs, rows.Err()
}

func (d *DB) CountClarifications(ctx context.Context) (total, answered int, err error) {
        err = d.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(answered_at) FROM clarifications`).
                Scan(&total, &answered)
        return
}

// ─── Suggestions ───────────────────────────────────────────────────────────

func (d *DB) CreateSuggestion(ctx context.Context, title, desc, taskPlan string) (int, error) {
        var id int
        err := d.pool.QueryRow(ctx,
                `INSERT INTO suggestions(title,description,task_plan) VALUES($1,$2,$3) RETURNING id`,
                title, desc, taskPlan).Scan(&id)
        return id, err
}

func (d *DB) UpdateSuggestionStatus(ctx context.Context, id int, status string) error {
        _, err := d.pool.Exec(ctx,
                `UPDATE suggestions SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
        return err
}

func (d *DB) GetPendingSuggestion(ctx context.Context) (*Suggestion, error) {
        row := d.pool.QueryRow(ctx,
                `SELECT id, title, description, task_plan, status, created_at, updated_at
                 FROM suggestions WHERE status='pending' ORDER BY id LIMIT 1`)
        s := &Suggestion{}
        err := row.Scan(&s.ID, &s.Title, &s.Description, &s.TaskPlan, &s.Status, &s.CreatedAt, &s.UpdatedAt)
        if err != nil {
                return nil, err
        }
        return s, nil
}

// ─── KV ────────────────────────────────────────────────────────────────────

func (d *DB) KVSet(ctx context.Context, key, value string) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO kv(key,value,updated_at) VALUES($1,$2,NOW())
                 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`,
                key, value)
        return err
}

func (d *DB) KVGet(ctx context.Context, key string) (string, error) {
        var v string
        err := d.pool.QueryRow(ctx, `SELECT value FROM kv WHERE key=$1`, key).Scan(&v)
        return v, err
}

func (d *DB) KVGetDefault(ctx context.Context, key, def string) string {
        v, err := d.KVGet(ctx, key)
        if err != nil {
                return def
        }
        return v
}

// ─── Messages ──────────────────────────────────────────────────────────────

func (d *DB) AddMessage(ctx context.Context, role, content string) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO messages(role,content) VALUES($1,$2)`, role, content)
        return err
}

func (d *DB) GetHistory(ctx context.Context, limit int) ([]struct{ Role, Content string }, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT role, content FROM (
                        SELECT role, content, id FROM messages ORDER BY id DESC LIMIT $1
                ) sub ORDER BY id ASC`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var msgs []struct{ Role, Content string }
        for rows.Next() {
                var m struct{ Role, Content string }
                if err := rows.Scan(&m.Role, &m.Content); err != nil {
                        return nil, err
                }
                msgs = append(msgs, m)
        }
        return msgs, rows.Err()
}

func (d *DB) GetRecentMessages(ctx context.Context, limit int) ([]struct {
        Role      string
        Content   string
        Timestamp string
}, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT role, content, created_at FROM (
                        SELECT role, content, created_at, id FROM messages ORDER BY id DESC LIMIT $1
                ) sub ORDER BY id ASC`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var msgs []struct {
                Role      string
                Content   string
                Timestamp string
        }
        for rows.Next() {
                var role, content string
                var ts time.Time
                if err := rows.Scan(&role, &content, &ts); err != nil {
                        return nil, err
                }
                msgs = append(msgs, struct {
                        Role      string
                        Content   string
                        Timestamp string
                }{role, content, ts.Format("15:04:05")})
        }
        return msgs, rows.Err()
}

func (d *DB) ClearMessages(ctx context.Context) error {
        _, err := d.pool.Exec(ctx, `DELETE FROM messages`)
        return err
}

// ─── LLM Usage ─────────────────────────────────────────────────────────────

func (d *DB) RecordUsage(ctx context.Context, provider, model string, prompt, completion int) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO llm_usage(provider,model,prompt_tokens,completion_tokens,total_tokens)
                 VALUES($1,$2,$3,$4,$5)`,
                provider, model, prompt, completion, prompt+completion)
        return err
}

func (d *DB) GetUsageSummary(ctx context.Context) ([]LLMUsage, error) {
        return d.queryUsage(ctx, `
        SELECT provider, model,
               SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens), COUNT(*)
        FROM llm_usage
        GROUP BY provider, model
        ORDER BY SUM(total_tokens) DESC`)
}

func (d *DB) GetUsageToday(ctx context.Context) ([]LLMUsage, error) {
        return d.queryUsage(ctx, `
        SELECT provider, model,
               SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens), COUNT(*)
        FROM llm_usage
        WHERE created_at >= CURRENT_DATE
        GROUP BY provider, model
        ORDER BY SUM(total_tokens) DESC`)
}

func (d *DB) queryUsage(ctx context.Context, sql string) ([]LLMUsage, error) {
        rows, err := d.pool.Query(ctx, sql)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        usage := []LLMUsage{}
        for rows.Next() {
                var u LLMUsage
                if err := rows.Scan(&u.Provider, &u.Model, &u.PromptTokens, &u.CompletionTokens, &u.TotalTokens, &u.Calls); err != nil {
                        return nil, err
                }
                usage = append(usage, u)
        }
        return usage, rows.Err()
}

// ─── Agent Trace (deep debug log) ──────────────────────────────────────────

// LogTrace appends one step to the agent_trace table. Errors are swallowed
// to keep the hot path simple; tracing is best-effort.
func (d *DB) LogTrace(ctx context.Context, scope, event, detail string) {
        if len(detail) > 4000 {
                detail = detail[:4000] + "…(truncated)"
        }
        _, _ = d.pool.Exec(ctx,
                `INSERT INTO agent_trace(scope, event, detail) VALUES($1, $2, $3)`,
                scope, event, detail)
}

// ListRecentTraces returns up to `limit` most recent trace entries (newest first).
func (d *DB) ListRecentTraces(ctx context.Context, limit int) ([]AgentTrace, error) {
        rows, err := d.pool.Query(ctx,
                `SELECT id, scope, event, detail, created_at
         FROM agent_trace ORDER BY id DESC LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        out := []AgentTrace{}
        for rows.Next() {
                var t AgentTrace
                if err := rows.Scan(&t.ID, &t.Scope, &t.Event, &t.Detail, &t.CreatedAt); err != nil {
                        return nil, err
                }
                out = append(out, t)
        }
        return out, rows.Err()
}

// ─── Scan helpers ──────────────────────────────────────────────────────────

type scannable interface {
        Scan(dest ...any) error
}

func scanTask(row scannable) (*Task, error) {
        t := &Task{}
        err := row.Scan(&t.ID, &t.PlanID, &t.ParentID, &t.Phase,
                &t.Title, &t.Description, &t.Status,
                &t.IsSuggestion, &t.PRURL, &t.CreatedAt, &t.UpdatedAt)
        if err != nil {
                return nil, err
        }
        return t, nil
}

func scanTasks(rows interface {
        Next() bool
        Scan(...any) error
        Err() error
}) ([]Task, error) {
        var tasks []Task
        for rows.Next() {
                t := &Task{}
                if err := rows.Scan(&t.ID, &t.PlanID, &t.ParentID, &t.Phase,
                        &t.Title, &t.Description, &t.Status,
                        &t.IsSuggestion, &t.PRURL, &t.CreatedAt, &t.UpdatedAt); err != nil {
                        return nil, err
                }
                tasks = append(tasks, *t)
        }
        return tasks, rows.Err()
}

// ─── Auth ──────────────────────────────────────────────────────────────────

// HasUser returns true when at least one user account exists.
func (d *DB) HasUser(ctx context.Context) bool {
        var n int
        _ = d.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
        return n > 0
}

// CreateUser inserts the single user record. Fails if a user already exists.
func (d *DB) CreateUser(ctx context.Context, username, passwordHash string) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO users(username, password_hash) VALUES($1,$2)`,
                username, passwordHash)
        return err
}

// GetUserPasswordHash returns the bcrypt hash for the given username.
func (d *DB) GetUserPasswordHash(ctx context.Context, username string) (string, error) {
        var hash string
        err := d.pool.QueryRow(ctx,
                `SELECT password_hash FROM users WHERE username=$1`, username).Scan(&hash)
        return hash, err
}

// CreateSession persists a new session token.
func (d *DB) CreateSession(ctx context.Context, token, username string, expiresAt time.Time) error {
        _, err := d.pool.Exec(ctx,
                `INSERT INTO sessions(token, username, expires_at) VALUES($1,$2,$3)`,
                token, username, expiresAt)
        return err
}

// GetSession returns the username and expiry for a token.
func (d *DB) GetSession(ctx context.Context, token string) (string, time.Time, error) {
        var username string
        var exp time.Time
        err := d.pool.QueryRow(ctx,
                `SELECT username, expires_at FROM sessions WHERE token=$1`, token).Scan(&username, &exp)
        return username, exp, err
}

// DeleteSession removes a session by token.
func (d *DB) DeleteSession(ctx context.Context, token string) error {
        _, err := d.pool.Exec(ctx, `DELETE FROM sessions WHERE token=$1`, token)
        return err
}
