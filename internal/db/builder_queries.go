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
		RETURNING id, task_id, status, branch, pr_url, pr_number, diff_summary, test_output, build_output,
			started_at, finished_at, created_at, updated_at`,
		taskID, branch)
	return scanBuilderJob(row)
}

// GetNextQueuedJob returns the oldest job with status='queued'.
func (d *DB) GetNextQueuedJob(ctx context.Context) (*BuilderJob, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT id, task_id, status, branch, pr_url, pr_number, diff_summary, test_output, build_output,
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
			finished_at=CASE WHEN $2 IN ('done','failed','rejected') THEN NOW() ELSE finished_at END,
			updated_at=NOW()
		WHERE id=$1`,
		id, status, prURL, prNumber, diffSummary, testOutput, buildOutput)
	return err
}

// GetBuilderJob returns a job by ID.
func (d *DB) GetBuilderJob(ctx context.Context, id int) (*BuilderJob, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT id, task_id, status, branch, pr_url, pr_number, diff_summary, test_output, build_output,
			started_at, finished_at, created_at, updated_at
		FROM builder_jobs
		WHERE id=$1`, id)
	return scanBuilderJob(row)
}

// GetJobForTask returns the most recent job for a task.
func (d *DB) GetJobForTask(ctx context.Context, taskID int) (*BuilderJob, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT id, task_id, status, branch, pr_url, pr_number, diff_summary, test_output, build_output,
			started_at, finished_at, created_at, updated_at
		FROM builder_jobs
		WHERE task_id=$1
		ORDER BY id DESC
		LIMIT 1`, taskID)
	return scanBuilderJob(row)
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
