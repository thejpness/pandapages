// Package storygenerationjobs provides the durable operational lifecycle for
// background generation. It deliberately keeps completed immutable evidence in
// story_orchestration_runs rather than storing partial artifacts in a job.
package storygenerationjobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pandapages/api/internal/db"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
)

const (
	defaultPollInterval = time.Second
	defaultJobTimeout   = time.Hour
)

// Runner separates trusted source loading/orchestration from durable job
// transitions. It must not persist a completed run itself.
type Runner interface {
	Generate(context.Context, string, storyorchestration.StageReporter) (storyorchestration.Result, error)
}

// Store is the narrow durable coordination boundary for generation jobs.
type Store interface {
	CreateOrReuseStoryGenerationJob(context.Context, model.AdminStoryGenerationJobCreateInput) (model.AdminStoryGenerationJob, error)
	GetStoryGenerationJob(context.Context, string) (model.AdminStoryGenerationJob, error)
	GetActiveStoryGenerationJobForSourceVersion(context.Context, string) (model.AdminStoryGenerationJob, error)
	ClaimNextStoryGenerationJob(context.Context) (model.AdminStoryGenerationJob, bool, error)
	UpdateStoryGenerationJobStage(context.Context, string, model.AdminStoryGenerationJobStage) error
	CompleteStoryGenerationJob(context.Context, string, storyorchestration.Result) (storyorchestration.PersistedRun, error)
	FailStoryGenerationJob(context.Context, string, string) error
	RecordStoryGenerationUsage(context.Context, string, storygeneration.ResponsesUsageObservation) error
	RequeueRunningStoryGenerationJobs(context.Context) (int64, error)
	RequeueStoryGenerationJob(context.Context, string) error
	AcquireStoryGenerationWorkerLock(context.Context) (db.StoryGenerationWorkerLock, bool, error)
}

type Config struct {
	Store        Store
	Runner       Runner
	Logger       *slog.Logger
	PollInterval time.Duration
	JobTimeout   time.Duration
}

// Service accepts and reads durable jobs while owning one conservative worker.
type Service struct {
	store        Store
	runner       Runner
	logger       *slog.Logger
	pollInterval time.Duration
	jobTimeout   time.Duration
	wake         chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("story generation job store is required")
	}
	if cfg.Runner == nil {
		return nil, fmt.Errorf("story generation job runner is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	jobTimeout := cfg.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = defaultJobTimeout
	}
	return &Service{
		store:        cfg.Store,
		runner:       cfg.Runner,
		logger:       logger,
		pollInterval: pollInterval,
		jobTimeout:   jobTimeout,
		wake:         make(chan struct{}, 1),
	}, nil
}

func (service *Service) Enqueue(
	ctx context.Context,
	input model.AdminStoryGenerationJobCreateInput,
) (model.AdminStoryGenerationJob, error) {
	job, err := service.store.CreateOrReuseStoryGenerationJob(ctx, input)
	if err != nil {
		return model.AdminStoryGenerationJob{}, err
	}
	select {
	case service.wake <- struct{}{}:
	default:
	}
	return job, nil
}

func (service *Service) Get(ctx context.Context, jobID string) (model.AdminStoryGenerationJob, error) {
	return service.store.GetStoryGenerationJob(ctx, jobID)
}

func (service *Service) GetActiveForSourceVersion(ctx context.Context, sourceVersionID string) (model.AdminStoryGenerationJob, error) {
	return service.store.GetActiveStoryGenerationJobForSourceVersion(ctx, sourceVersionID)
}

// Start obtains the singleton PostgreSQL worker lock, recovers jobs that were
// left running by a dead process, and then starts one bounded worker.
func (service *Service) Start(parent context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.done != nil {
		return fmt.Errorf("story generation job worker is already started")
	}

	lock, acquired, err := service.store.AcquireStoryGenerationWorkerLock(parent)
	if err != nil {
		return fmt.Errorf("acquire story generation worker lock: %w", err)
	}
	if !acquired {
		service.logger.Warn("story generation job worker is already active elsewhere")
		return nil
	}
	recovered, err := service.store.RequeueRunningStoryGenerationJobs(parent)
	if err != nil {
		lock.Release()
		return fmt.Errorf("recover story generation jobs: %w", err)
	}
	if recovered > 0 {
		service.logger.Warn("requeued abandoned story generation jobs", "count", recovered)
	}

	ctx, cancel := context.WithCancel(parent)
	service.cancel = cancel
	service.done = make(chan struct{})
	go service.run(ctx, lock, service.done)
	return nil
}

func (service *Service) Stop(ctx context.Context) error {
	service.mu.Lock()
	cancel := service.cancel
	done := service.done
	service.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *Service) run(ctx context.Context, lock db.StoryGenerationWorkerLock, done chan<- struct{}) {
	defer close(done)
	defer lock.Release()

	ticker := time.NewTicker(service.pollInterval)
	defer ticker.Stop()
	for {
		processed, err := service.processNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			service.logger.Error("story generation job worker iteration failed", "category", "job_worker", "err", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-service.wake:
		case <-ticker.C:
		}
	}
}

func (service *Service) processNext(ctx context.Context) (bool, error) {
	job, claimed, err := service.store.ClaimNextStoryGenerationJob(ctx)
	if err != nil || !claimed {
		return false, err
	}
	service.logger.Info("story generation job started",
		"generation_job_id", job.ID,
		"source_version_id", job.SourceVersionID,
		"stage", job.Stage,
	)
	service.execute(ctx, job)
	return true, nil
}

func (service *Service) execute(workerContext context.Context, job model.AdminStoryGenerationJob) {
	started := time.Now()
	jobContext, cancel := context.WithTimeout(workerContext, service.jobTimeout)
	defer cancel()
	usageContext := storygeneration.WithResponsesUsageRecorder(jobContext, generationUsageRecorder{
		store: service.store,
		jobID: job.ID,
	})
	lastStage := time.Now()
	currentStage := job.Stage
	report := func(stage storyorchestration.Stage) error {
		jobStage := model.AdminStoryGenerationJobStage(stage)
		if !model.ValidAdminStoryGenerationJobStage(jobStage) {
			return fmt.Errorf("unknown story generation stage %q", stage)
		}
		if err := service.store.UpdateStoryGenerationJobStage(jobContext, job.ID, jobStage); err != nil {
			return err
		}
		service.logger.Info("story generation job stage",
			"generation_job_id", job.ID,
			"source_version_id", job.SourceVersionID,
			"stage", stage,
			"duration", time.Since(lastStage),
		)
		lastStage = time.Now()
		currentStage = jobStage
		return nil
	}

	result, err := service.runner.Generate(usageContext, job.SourceVersionID, report)
	if err == nil {
		persisted, completeErr := service.store.CompleteStoryGenerationJob(jobContext, job.ID, result)
		if completeErr == nil {
			service.logger.Info("story generation job completed",
				"generation_job_id", job.ID,
				"source_version_id", job.SourceVersionID,
				"story_orchestration_run_id", persisted.ID,
				"semantic_result", persisted.Result.SemanticResult,
				"duration", time.Since(started),
			)
			return
		}
		err = fmt.Errorf("complete story generation job: %w", completeErr)
	}

	if workerContext.Err() != nil {
		requeueContext, requeueCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer requeueCancel()
		if requeueErr := service.store.RequeueStoryGenerationJob(requeueContext, job.ID); requeueErr != nil && !errors.Is(requeueErr, sql.ErrNoRows) {
			service.logger.Error("requeue interrupted story generation job failed", "generation_job_id", job.ID, "err", requeueErr)
		}
		return
	}

	failureCode := generationFailureCode(err, jobContext)
	if failErr := service.store.FailStoryGenerationJob(context.Background(), job.ID, failureCode); failErr != nil && !errors.Is(failErr, sql.ErrNoRows) {
		service.logger.Error("mark story generation job failed failed", "generation_job_id", job.ID, "err", failErr)
		return
	}
	service.logger.Error("story generation job failed",
		"generation_job_id", job.ID,
		"source_version_id", job.SourceVersionID,
		"stage", currentStage,
		"category", failureCode,
		"duration", time.Since(started),
	)
}

// generationUsageRecorder makes one short, bounded persistence attempt after
// the provider response has been safely observed. It deliberately does not
// inherit the remaining model/job deadline: a response received at that
// boundary must not be discarded solely because the model context expired.
type generationUsageRecorder struct {
	store Store
	jobID string
}

func (recorder generationUsageRecorder) RecordResponsesUsage(
	_ context.Context,
	observation storygeneration.ResponsesUsageObservation,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return recorder.store.RecordStoryGenerationUsage(ctx, recorder.jobID, observation)
}

func generationFailureCode(err error, ctx context.Context) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "generation_timeout"
	case errors.Is(err, storygeneration.ErrOpenAIUnauthorized), errors.Is(err, storygeneration.ErrOpenAIUnavailable), errors.Is(err, storygeneration.ErrOpenAIQuotaExceeded):
		return "generation_unavailable"
	case errors.Is(err, storygeneration.ErrOpenAIRateLimited):
		return "generation_rate_limited"
	case errors.Is(err, storygeneration.ErrOpenAIResponseInvalid), errors.Is(err, storygeneration.ErrOpenAIResponseIncomplete), errors.Is(err, storygeneration.ErrOpenAIResponseRefused):
		return "generation_upstream_invalid"
	default:
		return "generation_failed"
	}
}
