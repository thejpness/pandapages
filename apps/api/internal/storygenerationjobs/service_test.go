package storygenerationjobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/db"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
)

const testSourceVersionID = "11111111-1111-4111-8111-111111111111"

type fakeLock struct {
	mu       sync.Mutex
	released bool
}

func (lock *fakeLock) Release() {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	lock.released = true
}

type fakeStore struct {
	mu               sync.Mutex
	jobs             []model.AdminStoryGenerationJob
	stages           []model.AdminStoryGenerationJobStage
	completed        []storyorchestration.Result
	usageEvents      []storygeneration.ResponsesUsageObservation
	usageContextErrs []error
	usageDeadlines   []time.Time
	usageError       error
	failures         []string
	requeued         []string
	recovered        int64
	lock             *fakeLock
	claimCalls       int
	recoveryCalls    int
	enqueueCounter   int
}

func (store *fakeStore) CreateOrReuseStoryGenerationJob(_ context.Context, input model.AdminStoryGenerationJobCreateInput) (model.AdminStoryGenerationJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, job := range store.jobs {
		if job.SourceVersionID == input.SourceVersionID && (job.Status == model.AdminStoryGenerationJobQueued || job.Status == model.AdminStoryGenerationJobRunning) {
			return job, nil
		}
	}
	store.enqueueCounter++
	job := queuedJob(input.SourceVersionID)
	job.ID = "22222222-2222-4222-8222-22222222222" + string(rune('0'+store.enqueueCounter))
	store.jobs = append(store.jobs, job)
	return job, nil
}

func (store *fakeStore) GetStoryGenerationJob(_ context.Context, jobID string) (model.AdminStoryGenerationJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, job := range store.jobs {
		if job.ID == jobID {
			return job, nil
		}
	}
	return model.AdminStoryGenerationJob{}, errors.New("not found")
}

func (store *fakeStore) GetActiveStoryGenerationJobForSourceVersion(_ context.Context, sourceVersionID string) (model.AdminStoryGenerationJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, job := range store.jobs {
		if job.SourceVersionID == sourceVersionID && (job.Status == model.AdminStoryGenerationJobQueued || job.Status == model.AdminStoryGenerationJobRunning) {
			return job, nil
		}
	}
	return model.AdminStoryGenerationJob{}, errors.New("not found")
}

func (store *fakeStore) ClaimNextStoryGenerationJob(_ context.Context) (model.AdminStoryGenerationJob, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	for index := range store.jobs {
		if store.jobs[index].Status != model.AdminStoryGenerationJobQueued {
			continue
		}
		store.jobs[index].Status = model.AdminStoryGenerationJobRunning
		store.jobs[index].Stage = model.AdminStoryGenerationJobStageAnalysingSource
		job := store.jobs[index]
		return job, true, nil
	}
	return model.AdminStoryGenerationJob{}, false, nil
}

func (store *fakeStore) UpdateStoryGenerationJobStage(_ context.Context, jobID string, stage model.AdminStoryGenerationJobStage) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.jobs {
		if store.jobs[index].ID == jobID && store.jobs[index].Status == model.AdminStoryGenerationJobRunning {
			store.jobs[index].Stage = stage
			store.stages = append(store.stages, stage)
			return nil
		}
	}
	return errors.New("job not running")
}

func (store *fakeStore) CompleteStoryGenerationJob(_ context.Context, jobID string, result storyorchestration.Result) (storyorchestration.PersistedRun, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.jobs {
		if store.jobs[index].ID == jobID && store.jobs[index].Status == model.AdminStoryGenerationJobRunning {
			store.jobs[index].Status = model.AdminStoryGenerationJobCompleted
			store.jobs[index].Stage = model.AdminStoryGenerationJobStageCompleted
			runID := "33333333-3333-4333-8333-333333333333"
			store.jobs[index].CompletedRunID = &runID
			store.completed = append(store.completed, result)
			return storyorchestration.PersistedRun{ID: runID, SourceVersionID: result.SourceIdentity, Result: result}, nil
		}
	}
	return storyorchestration.PersistedRun{}, errors.New("job not running")
}

func (store *fakeStore) FailStoryGenerationJob(_ context.Context, jobID, failureCode string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.jobs {
		if store.jobs[index].ID == jobID && store.jobs[index].Status == model.AdminStoryGenerationJobRunning {
			store.jobs[index].Status = model.AdminStoryGenerationJobFailed
			store.jobs[index].Stage = model.AdminStoryGenerationJobStageFailed
			store.failures = append(store.failures, failureCode)
			return nil
		}
	}
	return errors.New("job not running")
}

func (store *fakeStore) RecordStoryGenerationUsage(ctx context.Context, jobID string, observation storygeneration.ResponsesUsageObservation) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.usageContextErrs = append(store.usageContextErrs, ctx.Err())
	deadline, _ := ctx.Deadline()
	store.usageDeadlines = append(store.usageDeadlines, deadline)
	if store.usageError != nil {
		return store.usageError
	}
	for _, job := range store.jobs {
		if job.ID == jobID {
			store.usageEvents = append(store.usageEvents, observation)
			return nil
		}
	}
	return errors.New("job not found")
}

func (store *fakeStore) RequeueRunningStoryGenerationJobs(_ context.Context) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recoveryCalls++
	for index := range store.jobs {
		if store.jobs[index].Status == model.AdminStoryGenerationJobRunning {
			store.jobs[index].Status = model.AdminStoryGenerationJobQueued
			store.jobs[index].Stage = model.AdminStoryGenerationJobStageQueued
		}
	}
	return store.recovered, nil
}

func (store *fakeStore) RequeueStoryGenerationJob(_ context.Context, jobID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.jobs {
		if store.jobs[index].ID == jobID && store.jobs[index].Status == model.AdminStoryGenerationJobRunning {
			store.jobs[index].Status = model.AdminStoryGenerationJobQueued
			store.jobs[index].Stage = model.AdminStoryGenerationJobStageQueued
			store.requeued = append(store.requeued, jobID)
			return nil
		}
	}
	return errors.New("job not running")
}

func (store *fakeStore) AcquireStoryGenerationWorkerLock(context.Context) (db.StoryGenerationWorkerLock, bool, error) {
	if store.lock == nil {
		store.lock = &fakeLock{}
	}
	return store.lock, true, nil
}

type fakeRunner struct {
	stages []storyorchestration.Stage
	result storyorchestration.Result
	err    error
	run    func(context.Context, storyorchestration.StageReporter) (storyorchestration.Result, error)
	calls  int
}

func (runner *fakeRunner) Generate(ctx context.Context, _ string, report storyorchestration.StageReporter) (storyorchestration.Result, error) {
	runner.calls++
	if runner.run != nil {
		return runner.run(ctx, report)
	}
	for _, stage := range runner.stages {
		if err := report(stage); err != nil {
			return storyorchestration.Result{}, err
		}
	}
	return runner.result, runner.err
}

func queuedJob(sourceVersionID string) model.AdminStoryGenerationJob {
	return model.AdminStoryGenerationJob{
		ID:              "22222222-2222-4222-8222-222222222222",
		SourceVersionID: sourceVersionID,
		Status:          model.AdminStoryGenerationJobQueued,
		Stage:           model.AdminStoryGenerationJobStageQueued,
		CreatedAt:       "2026-08-26T09:00:00Z",
	}
}

func newTestService(t *testing.T, store *fakeStore, runner *fakeRunner) *Service {
	t.Helper()
	service, err := New(Config{Store: store, Runner: runner, PollInterval: time.Hour, JobTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestWorkerClaimsStagesAndCompletesOneDurableJob(t *testing.T) {
	stages := []storyorchestration.Stage{
		storyorchestration.StageAnalysingSource,
		storyorchestration.StageGeneratingConfidentReaders,
		storyorchestration.StageValidatingBundle,
	}
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultNeedsReview}
	runner := &fakeRunner{stages: stages, result: result}
	service := newTestService(t, store, runner)

	processed, err := service.processNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed/error = %v/%v", processed, err)
	}
	if runner.calls != 1 || len(store.completed) != 1 || len(store.failures) != 0 {
		t.Fatalf("calls/completed/failures = %d/%d/%d", runner.calls, len(store.completed), len(store.failures))
	}
	if store.jobs[0].Status != model.AdminStoryGenerationJobCompleted || store.jobs[0].CompletedRunID == nil || store.completed[0].SemanticResult != adaptationcontract.ResultNeedsReview {
		t.Fatalf("completed job/result = %#v/%#v", store.jobs[0], store.completed[0])
	}
	wantStages := []model.AdminStoryGenerationJobStage{
		model.AdminStoryGenerationJobStageAnalysingSource,
		model.AdminStoryGenerationJobStageGeneratingConfidentReaders,
		model.AdminStoryGenerationJobStageValidatingBundle,
	}
	if len(store.stages) != len(wantStages) {
		t.Fatalf("stages = %v, want %v", store.stages, wantStages)
	}
	for index := range wantStages {
		if store.stages[index] != wantStages[index] {
			t.Fatalf("stage %d = %q, want %q", index, store.stages[index], wantStages[index])
		}
	}
}

func TestWorkerFailureCreatesNoCompletedRun(t *testing.T) {
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	service := newTestService(t, store, &fakeRunner{err: storygeneration.ErrOpenAIUnavailable})

	processed, err := service.processNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed/error = %v/%v", processed, err)
	}
	if len(store.completed) != 0 || len(store.failures) != 1 || store.failures[0] != "generation_unavailable" || store.jobs[0].Status != model.AdminStoryGenerationJobFailed {
		t.Fatalf("completed/failures/job = %d/%v/%#v", len(store.completed), store.failures, store.jobs[0])
	}
}

func TestWorkerExhaustedRateLimitCreatesNoCompletedRun(t *testing.T) {
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	service := newTestService(t, store, &fakeRunner{err: storygeneration.ErrOpenAIRateLimited})

	processed, err := service.processNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed/error = %v/%v", processed, err)
	}
	if len(store.completed) != 0 || len(store.failures) != 1 || store.failures[0] != "generation_rate_limited" || store.jobs[0].Status != model.AdminStoryGenerationJobFailed {
		t.Fatalf("completed/failures/job = %d/%v/%#v", len(store.completed), store.failures, store.jobs[0])
	}
}

func TestAcceptedJobOutlivesOriginatingRequestCancellation(t *testing.T) {
	store := &fakeStore{}
	result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultPass}
	service := newTestService(t, store, &fakeRunner{result: result})
	requestContext, cancel := context.WithCancel(context.Background())
	job, err := service.Enqueue(requestContext, model.AdminStoryGenerationJobCreateInput{
		SourceVersionID:      testSourceVersionID,
		RequesterPrincipalID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		RequesterAccountID:   "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	processed, err := service.processNext(context.Background())
	if err != nil || !processed || len(store.completed) != 1 || store.completed[0].SourceIdentity != job.SourceVersionID {
		t.Fatalf("processed/error/completed = %v/%v/%#v", processed, err, store.completed)
	}
}

func TestWorkerRequeuesInterruptedJobAndRecoversRunningJobsOnStart(t *testing.T) {
	running := queuedJob(testSourceVersionID)
	running.Status = model.AdminStoryGenerationJobRunning
	running.Stage = model.AdminStoryGenerationJobStageGeneratingGrowingReaders
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{running}, recovered: 1}
	service := newTestService(t, store, &fakeRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if store.recoveryCalls != 1 {
		t.Fatalf("recovery calls = %d, want 1", store.recoveryCalls)
	}
	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := service.Stop(stopContext); err != nil {
		t.Fatal(err)
	}
	if store.lock == nil || !store.lock.released {
		t.Fatal("worker lock was not released")
	}
}

func TestWorkerProcessesOnlyOneJobAtATime(t *testing.T) {
	secondSourceVersionID := "12121212-1212-4212-8212-121212121212"
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{
		queuedJob(testSourceVersionID),
		queuedJob(secondSourceVersionID),
	}}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	runner := &fakeRunner{run: func(_ context.Context, _ storyorchestration.StageReporter) (storyorchestration.Result, error) {
		once.Do(func() {
			close(firstStarted)
			<-releaseFirst
		})
		return storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultPass}, nil
	}}
	service := newTestService(t, store, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	<-firstStarted
	store.mu.Lock()
	claimCalls := store.claimCalls
	completed := len(store.completed)
	store.mu.Unlock()
	if claimCalls != 1 || completed != 0 {
		t.Fatalf("claimed/completed while first job blocks = %d/%d, want 1/0", claimCalls, completed)
	}
	close(releaseFirst)

	deadline := time.After(time.Second)
	for {
		store.mu.Lock()
		completed = len(store.completed)
		store.mu.Unlock()
		if completed == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("completed jobs = %d, want 2", completed)
		case <-time.After(time.Millisecond):
		}
	}
	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := service.Stop(stopContext); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerRequeuesInsteadOfFailingWhenProcessContextStops(t *testing.T) {
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	root, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{run: func(ctx context.Context, _ storyorchestration.StageReporter) (storyorchestration.Result, error) {
		cancel()
		<-ctx.Done()
		return storyorchestration.Result{}, ctx.Err()
	}}
	service := newTestService(t, store, runner)
	processed, err := service.processNext(root)
	if err != nil || !processed || len(store.requeued) != 1 || len(store.failures) != 0 || store.jobs[0].Status != model.AdminStoryGenerationJobQueued {
		t.Fatalf("processed/error/requeued/failures/job = %v/%v/%v/%v/%#v", processed, err, store.requeued, store.failures, store.jobs[0])
	}
}

func TestWorkerQuotaExhaustionCreatesNoCompletedRun(t *testing.T) {
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	service := newTestService(t, store, &fakeRunner{err: storygeneration.ErrOpenAIQuotaExceeded})

	processed, err := service.processNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed/error = %v/%v", processed, err)
	}
	if len(store.completed) != 0 ||
		len(store.failures) != 1 ||
		store.failures[0] != "generation_unavailable" ||
		store.jobs[0].Status != model.AdminStoryGenerationJobFailed {
		t.Fatalf(
			"completed/failures/job = %d/%v/%#v",
			len(store.completed),
			store.failures,
			store.jobs[0],
		)
	}
}

func TestGenerationUsageRecorderUsesBoundedIndependentPersistenceContext(t *testing.T) {
	store := &fakeStore{jobs: []model.AdminStoryGenerationJob{queuedJob(testSourceVersionID)}}
	recorder := generationUsageRecorder{store: store, jobID: store.jobs[0].ID}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	observation := storygeneration.ResponsesUsageObservation{
		Operation:          storygeneration.ResponsesOperationAnalyseSource,
		ProviderResponseID: "resp-observed-before-cancellation",
		RequestedModel:     "requested-model",
		ReturnedModel:      "returned-model",
		Usage:              storygeneration.ResponsesUsage{InputTokens: 1, TotalTokens: 1},
	}
	if err := recorder.RecordResponsesUsage(canceled, observation); err != nil {
		t.Fatalf("RecordResponsesUsage() error = %v", err)
	}
	if len(store.usageEvents) != 1 || len(store.usageContextErrs) != 1 || store.usageContextErrs[0] != nil {
		t.Fatalf("usage events/context errors = %#v/%#v", store.usageEvents, store.usageContextErrs)
	}
	if len(store.usageDeadlines) != 1 || time.Until(store.usageDeadlines[0]) <= 0 || time.Until(store.usageDeadlines[0]) > 5*time.Second {
		t.Fatalf("usage persistence deadline = %v", store.usageDeadlines)
	}
}
