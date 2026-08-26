package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyorchestration"
)

const storyGenerationWorkerAdvisoryKey = "pandapages:story-generation-worker:v1"

// CreateOrReuseStoryGenerationJob records one accepted generation request, or
// returns its source-version's existing active job. The partial unique index is
// the final duplicate-cost guard under concurrent requests.
func (s *Store) CreateOrReuseStoryGenerationJob(
	parent context.Context,
	input model.AdminStoryGenerationJobCreateInput,
) (model.AdminStoryGenerationJob, error) {
	if err := input.Validate(); err != nil {
		return model.AdminStoryGenerationJob{}, err
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()

	job, err := scanStoryGenerationJob(s.db.QueryRowContext(ctx, `
		INSERT INTO story_generation_jobs (
			source_version_id,
			requester_principal_id,
			requester_account_id
		)
		SELECT $1, $2, $3
		WHERE EXISTS (SELECT 1 FROM story_source_versions WHERE id = $1)
		ON CONFLICT (source_version_id) WHERE status IN ('queued', 'running')
		DO UPDATE SET updated_at = story_generation_jobs.updated_at
		RETURNING
			id, source_version_id, status, stage, failure_code, completed_run_id,
			created_at, started_at, completed_at, requester_principal_id, requester_account_id
	`, input.SourceVersionID, input.RequesterPrincipalID, input.RequesterAccountID))
	if err != nil {
		return model.AdminStoryGenerationJob{}, err
	}
	return job, nil
}

func (s *Store) GetStoryGenerationJob(parent context.Context, jobID string) (model.AdminStoryGenerationJob, error) {
	jobID = strings.TrimSpace(jobID)
	if !accountIDRe.MatchString(jobID) {
		return model.AdminStoryGenerationJob{}, fmt.Errorf("story generation job ID is invalid")
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	return scanStoryGenerationJob(s.db.QueryRowContext(ctx, `
		SELECT
			id, source_version_id, status, stage, failure_code, completed_run_id,
			created_at, started_at, completed_at, requester_principal_id, requester_account_id
		FROM story_generation_jobs
		WHERE id = $1
	`, jobID))
}

func (s *Store) GetActiveStoryGenerationJobForSourceVersion(
	parent context.Context,
	sourceVersionID string,
) (model.AdminStoryGenerationJob, error) {
	sourceVersionID = strings.TrimSpace(sourceVersionID)
	if !accountIDRe.MatchString(sourceVersionID) {
		return model.AdminStoryGenerationJob{}, fmt.Errorf("source version ID is invalid")
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	return scanStoryGenerationJob(s.db.QueryRowContext(ctx, `
		SELECT
			id, source_version_id, status, stage, failure_code, completed_run_id,
			created_at, started_at, completed_at, requester_principal_id, requester_account_id
		FROM story_generation_jobs
		WHERE source_version_id = $1
		  AND status IN ('queued', 'running')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, sourceVersionID))
}

// ClaimNextStoryGenerationJob atomically moves the oldest queued job into its
// first real operation stage. SKIP LOCKED keeps this safe if deployment later
// deliberately adds another worker.
func (s *Store) ClaimNextStoryGenerationJob(parent context.Context) (model.AdminStoryGenerationJob, bool, error) {
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminStoryGenerationJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	job, err := scanStoryGenerationJob(tx.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id
			FROM story_generation_jobs
			WHERE status = 'queued'
			ORDER BY created_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE story_generation_jobs AS job
		SET
			status = 'running',
			stage = 'analysing_source',
			started_at = now(),
			updated_at = now()
		FROM candidate
		WHERE job.id = candidate.id
		RETURNING
			job.id, job.source_version_id, job.status, job.stage, job.failure_code, job.completed_run_id,
			job.created_at, job.started_at, job.completed_at, job.requester_principal_id, job.requester_account_id
	`))
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return model.AdminStoryGenerationJob{}, false, err
		}
		return model.AdminStoryGenerationJob{}, false, nil
	}
	if err != nil {
		return model.AdminStoryGenerationJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryGenerationJob{}, false, err
	}
	return job, true, nil
}

func (s *Store) UpdateStoryGenerationJobStage(
	parent context.Context,
	jobID string,
	stage model.AdminStoryGenerationJobStage,
) error {
	if !model.ValidAdminStoryGenerationJobStage(stage) ||
		stage == model.AdminStoryGenerationJobStageQueued ||
		stage == model.AdminStoryGenerationJobStageCompleted ||
		stage == model.AdminStoryGenerationJobStageFailed {
		return fmt.Errorf("story generation job stage is invalid")
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE story_generation_jobs
		SET stage = $2, updated_at = now()
		WHERE id = $1 AND status = 'running'
	`, jobID, stage)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// CompleteStoryGenerationJob atomically persists immutable complete evidence
// and marks its operational job complete. A failed job transition therefore
// can never expose partial evidence as a completed orchestration run.
func (s *Store) CompleteStoryGenerationJob(
	parent context.Context,
	jobID string,
	result storyorchestration.Result,
) (storyorchestration.PersistedRun, error) {
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var sourceVersionID string
	if err := tx.QueryRowContext(ctx, `
		SELECT source_version_id
		FROM story_generation_jobs
		WHERE id = $1 AND status = 'running'
		FOR UPDATE
	`, jobID).Scan(&sourceVersionID); err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	persisted, err := persistCompletedStoryOrchestrationRunTx(ctx, tx, sourceVersionID, result)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	update, err := tx.ExecContext(ctx, `
		UPDATE story_generation_jobs
		SET
			status = 'completed',
			stage = 'completed',
			completed_run_id = $2,
			completed_at = now(),
			updated_at = now()
		WHERE id = $1 AND status = 'running'
	`, jobID, persisted.ID)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	updated, err := update.RowsAffected()
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	if updated != 1 {
		return storyorchestration.PersistedRun{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	return persisted, nil
}

func (s *Store) FailStoryGenerationJob(parent context.Context, jobID, failureCode string) error {
	failureCode = strings.TrimSpace(failureCode)
	if !model.ValidAdminStoryGenerationJobFailureCode(failureCode) {
		return fmt.Errorf("story generation job failure code is invalid")
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE story_generation_jobs
		SET
			status = 'failed',
			stage = 'failed',
			failure_code = $2,
			completed_at = now(),
			updated_at = now()
		WHERE id = $1 AND status = 'running'
	`, jobID, failureCode)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// RequeueRunningStoryGenerationJobs recovers work left running by a previous
// process after this worker has acquired the singleton PostgreSQL lock.
func (s *Store) RequeueRunningStoryGenerationJobs(parent context.Context) (int64, error) {
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE story_generation_jobs
		SET
			status = 'queued',
			stage = 'queued',
			started_at = NULL,
			updated_at = now()
		WHERE status = 'running'
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) RequeueStoryGenerationJob(parent context.Context, jobID string) error {
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE story_generation_jobs
		SET
			status = 'queued',
			stage = 'queued',
			started_at = NULL,
			updated_at = now()
		WHERE id = $1 AND status = 'running'
	`, jobID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// AcquireStoryGenerationWorkerLock takes a session-scoped PostgreSQL advisory
// lock. The caller must hold the returned connection until worker shutdown;
// PostgreSQL releases it automatically if the process dies.
type StoryGenerationWorkerLock interface {
	Release()
}

type storyGenerationWorkerLock struct {
	conn *sql.Conn
}

func (lock storyGenerationWorkerLock) Release() {
	ReleaseStoryGenerationWorkerLock(lock.conn)
}

func (s *Store) AcquireStoryGenerationWorkerLock(parent context.Context) (StoryGenerationWorkerLock, bool, error) {
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, storyGenerationWorkerAdvisoryKey).Scan(&acquired)
	if err != nil || !acquired {
		_ = conn.Close()
		return nil, false, err
	}
	return storyGenerationWorkerLock{conn: conn}, true, nil
}

func ReleaseStoryGenerationWorkerLock(conn *sql.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, storyGenerationWorkerAdvisoryKey)
	_ = conn.Close()
}

func scanStoryGenerationJob(row *sql.Row) (model.AdminStoryGenerationJob, error) {
	var (
		job         model.AdminStoryGenerationJob
		failureCode sql.NullString
		completedID sql.NullString
		createdAt   time.Time
		startedAt   sql.NullTime
		completedAt sql.NullTime
	)
	if err := row.Scan(
		&job.ID,
		&job.SourceVersionID,
		&job.Status,
		&job.Stage,
		&failureCode,
		&completedID,
		&createdAt,
		&startedAt,
		&completedAt,
		&job.RequesterPrincipalID,
		&job.RequesterAccountID,
	); err != nil {
		return model.AdminStoryGenerationJob{}, err
	}
	job.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	if failureCode.Valid {
		value := failureCode.String
		job.FailureCode = &value
	}
	if completedID.Valid {
		value := completedID.String
		job.CompletedRunID = &value
	}
	if startedAt.Valid {
		value := startedAt.Time.UTC().Format(time.RFC3339Nano)
		job.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC().Format(time.RFC3339Nano)
		job.CompletedAt = &value
	}
	return job, nil
}
