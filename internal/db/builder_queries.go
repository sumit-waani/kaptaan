package db

import (
        "context"
        "time"
)

type BuilderJob struct {
        ID          int
        TaskID      int
        Status      string
        Branch      string
        PRURL       string
        PRNumber    int
        RetryCount  int
        DiffSummary string
        TestOutput  string
        BuildOutput string
        StartedAt   *time.Time
        FinishedAt  *time.Time
        CreatedAt   time.Time
        UpdatedAt   time.Time
}

// CreateBuilderJob inserts a new queued job for a task.
func (d *DB) CreateBuilderJob(ctx context.Context, taskID int, branch string) (*BuilderJob, error) {
        row := d.pool.QueryRow(ctx, `
        INSERT INTO builder_jobs(task_id, status, branch)
        VALUES($1, 'queued', $2)
        RETURNING id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at`,
                taskID, branch)
        return scanBuilderJob(row)
}

// GetNextQueuedJob returns the oldest job with status='queued'.
func (d *DB) GetNextQueuedJob(ctx context.Context) (*BuilderJob, error) {
        row := d.pool.QueryRow(ctx, `
        SELECT id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at
        FROM builder_jobs
        WHERE status='queued'
        ORDER BY created_at ASC, id ASC
        LIMIT 1`)
        return scanBuilderJob(row)
}

// UpdateBuilderJob updates status + output fields.
func (d *DB) UpdateBuilderJob(ctx context.Context, id int, status, prURL string, prNumber int, diffSummary, testOutput, buildOutput string) error {
        _, err := d.pool.Exec(ctx, `
        UPDATE builder_jobs
        SET status=$2,
            pr_url=$3,
            pr_number=$4,
            diff_summary=$5,
            test_output=$6,
            build_output=$7,
            started_at=CASE WHEN $2='running' AND started_at IS NULL THEN NOW() ELSE started_at END,
            finished_at=CASE WHEN $2 IN ('done','failed','rejected','awaiting_review','merged') THEN NOW() ELSE finished_at END,
            updated_at=NOW()
        WHERE id=$1`,
                id, status, prURL, prNumber, diffSummary, testOutput, buildOutput)
        return err
}

// GetBuilderJob returns a job by ID.
func (d *DB) GetBuilderJob(ctx context.Context, id int) (*BuilderJob, error) {
        row := d.pool.QueryRow(ctx, `
        SELECT id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at
        FROM builder_jobs
        WHERE id=$1`, id)
        return scanBuilderJob(row)
}

// GetJobForTask returns the most recent job for a task.
func (d *DB) GetJobForTask(ctx context.Context, taskID int) (*BuilderJob, error) {
        row := d.pool.QueryRow(ctx, `
        SELECT id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at
        FROM builder_jobs
        WHERE task_id=$1
        ORDER BY id DESC
        LIMIT 1`, taskID)
        return scanBuilderJob(row)
}

// UpdateBuilderJobRetry sets retry_count and queues the job again.
func (d *DB) UpdateBuilderJobRetry(ctx context.Context, id, retryCount int) error {
        _, err := d.pool.Exec(ctx, `
        UPDATE builder_jobs
        SET retry_count=$2,
            status='queued',
            started_at=NULL,
            finished_at=NULL,
            updated_at=NOW()
        WHERE id=$1`,
                id, retryCount)
        return err
}

// RequeueBuilderJob sets status back to queued for retry.
func (d *DB) RequeueBuilderJob(ctx context.Context, id int) error {
        _, err := d.pool.Exec(ctx, `
        UPDATE builder_jobs
        SET status='queued',
            started_at=NULL,
            finished_at=NULL,
            updated_at=NOW()
        WHERE id=$1`, id)
        return err
}

// UpdateBuilderJobStatus updates only the status field.
func (d *DB) UpdateBuilderJobStatus(ctx context.Context, id int, status string) error {
        _, err := d.pool.Exec(ctx, `
        UPDATE builder_jobs
        SET status=$2,
            started_at=CASE WHEN $2='running' AND started_at IS NULL THEN NOW() ELSE started_at END,
            finished_at=CASE WHEN $2 IN ('done','failed','rejected','awaiting_review','merged') THEN NOW() ELSE finished_at END,
            updated_at=NOW()
        WHERE id=$1`,
                id, status)
        return err
}

// GetStaleBuilderJobs returns jobs stuck in running state for over 10 minutes.
func (d *DB) GetStaleBuilderJobs(ctx context.Context) ([]BuilderJob, error) {
        rows, err := d.pool.Query(ctx, `
        SELECT id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at
        FROM builder_jobs
        WHERE status='running' AND updated_at < NOW() - INTERVAL '10 minutes'
        ORDER BY updated_at ASC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var jobs []BuilderJob
        for rows.Next() {
                job, err := scanBuilderJob(rows)
                if err != nil {
                        return nil, err
                }
                jobs = append(jobs, *job)
        }
        return jobs, rows.Err()
}

// ListJobsAwaitingReview returns builder jobs that have completed and are
// waiting for the user's merge/reject decision.
func (d *DB) ListJobsAwaitingReview(ctx context.Context) ([]BuilderJob, error) {
        rows, err := d.pool.Query(ctx, `
        SELECT id, task_id, status, branch, pr_url, pr_number, retry_count, diff_summary, test_output, build_output,
            started_at, finished_at, created_at, updated_at
        FROM builder_jobs
        WHERE status='awaiting_review'
        ORDER BY created_at ASC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var jobs []BuilderJob
        for rows.Next() {
                j, err := scanBuilderJob(rows)
                if err != nil {
                        return nil, err
                }
                jobs = append(jobs, *j)
        }
        return jobs, rows.Err()
}

// BuilderJobSummary is a light-weight job row joined with its task title for
// the Settings → Builder Activity panel.
type BuilderJobSummary struct {
        ID        int        `json:"id"`
        TaskID    int        `json:"task_id"`
        TaskTitle string     `json:"task_title"`
        Status    string     `json:"status"`
        Branch    string     `json:"branch"`
        PRURL     string     `json:"pr_url"`
        UpdatedAt time.Time  `json:"updated_at"`
        StartedAt *time.Time `json:"started_at"`
}

// ListRecentBuilderJobs returns up to `limit` most-recently-updated jobs,
// joined with their task title.
func (d *DB) ListRecentBuilderJobs(ctx context.Context, limit int) ([]BuilderJobSummary, error) {
        rows, err := d.pool.Query(ctx, `
        SELECT bj.id, bj.task_id, COALESCE(t.title, '(unknown)'), bj.status, bj.branch, bj.pr_url, bj.updated_at, bj.started_at
        FROM builder_jobs bj
        LEFT JOIN tasks t ON t.id = bj.task_id
        ORDER BY bj.updated_at DESC
        LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []BuilderJobSummary
        for rows.Next() {
                var s BuilderJobSummary
                if err := rows.Scan(&s.ID, &s.TaskID, &s.TaskTitle, &s.Status, &s.Branch, &s.PRURL, &s.UpdatedAt, &s.StartedAt); err != nil {
                        return nil, err
                }
                out = append(out, s)
        }
        return out, rows.Err()
}

// SaveManagerNote saves the manager's review note for a job.
func (d *DB) SaveManagerNote(ctx context.Context, jobID int, note string) error {
        _, err := d.pool.Exec(ctx, `INSERT INTO manager_notes(job_id, note) VALUES($1, $2)`, jobID, note)
        return err
}

// GetManagerNote returns the manager's note for a job.
func (d *DB) GetManagerNote(ctx context.Context, jobID int) (string, error) {
        var note string
        err := d.pool.QueryRow(ctx, `SELECT note FROM manager_notes WHERE job_id=$1 ORDER BY id DESC LIMIT 1`, jobID).Scan(&note)
        return note, err
}

func scanBuilderJob(row scannable) (*BuilderJob, error) {
        j := &BuilderJob{}
        err := row.Scan(
                &j.ID,
                &j.TaskID,
                &j.Status,
                &j.Branch,
                &j.PRURL,
                &j.PRNumber,
                &j.RetryCount,
                &j.DiffSummary,
                &j.TestOutput,
                &j.BuildOutput,
                &j.StartedAt,
                &j.FinishedAt,
                &j.CreatedAt,
                &j.UpdatedAt,
        )
        if err != nil {
                return nil, err
        }
        return j, nil
}
