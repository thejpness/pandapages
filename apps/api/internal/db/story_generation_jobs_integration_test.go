package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

const (
	storyGenerationJobsIntegrationPrincipalID = "c1600000-0000-4000-8000-000000000001"
	storyGenerationJobsIntegrationAccountID   = "c1600000-0000-4000-8000-000000000002"
)

func TestStoryGenerationJobsIntegration(t *testing.T) {
	if os.Getenv(readerIntegrationGuardVar) != "1" {
		t.Skip("set PP_READER_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(readerIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required", readerIntegrationURLVar)
	}
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	var databaseName string
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != readerIntegrationDBName {
		t.Fatalf("refusing generation jobs database %q: %v", databaseName, err)
	}

	const slug = "story-generation-jobs-integration"
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`
			DELETE FROM story_generation_jobs
			WHERE source_version_id IN (
				SELECT version.id
				FROM story_source_versions AS version
				JOIN stories AS story ON story.id = version.story_id
				WHERE story.slug = $1
			)
		`, slug)
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_runs
			WHERE source_version_id IN (
				SELECT version.id
				FROM story_source_versions AS version
				JOIN stories AS story ON story.id = version.story_id
				WHERE story.slug = $1
			)
		`, slug)
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = $1`, slug)
		_, _ = adminDB.Exec(`DELETE FROM account_memberships WHERE principal_id = $1 AND account_id = $2`, storyGenerationJobsIntegrationPrincipalID, storyGenerationJobsIntegrationAccountID)
		_, _ = adminDB.Exec(`DELETE FROM principals WHERE id = $1`, storyGenerationJobsIntegrationPrincipalID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, storyGenerationJobsIntegrationAccountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Generation jobs integration account')`, storyGenerationJobsIntegrationAccountID); err != nil {
		t.Fatalf("insert requester account: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO principals (id, display_name) VALUES ($1, 'Generation jobs integration principal')`, storyGenerationJobsIntegrationPrincipalID); err != nil {
		t.Fatalf("insert requester principal: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO account_memberships (principal_id, account_id, role) VALUES ($1, $2, 'owner')`, storyGenerationJobsIntegrationPrincipalID, storyGenerationJobsIntegrationAccountID); err != nil {
		t.Fatalf("insert requester membership: %v", err)
	}

	sourceText := "# The Durable Lantern\n\nA traveller follows a lantern home.\n"
	source, err := store.AdminSourceUpsert(slug, model.AdminSourceUpsertRequest{
		Title: "The Durable Lantern", Language: stringPointer("en-GB"), SourceText: sourceText,
	})
	if err != nil {
		t.Fatalf("create canonical source: %v", err)
	}
	input := model.AdminStoryGenerationJobCreateInput{
		SourceVersionID: source.VersionID, RequesterPrincipalID: storyGenerationJobsIntegrationPrincipalID, RequesterAccountID: storyGenerationJobsIntegrationAccountID,
	}

	var jobs [2]model.AdminStoryGenerationJob
	errs := make(chan error, len(jobs))
	var requests sync.WaitGroup
	for index := range jobs {
		requests.Add(1)
		go func(index int) {
			defer requests.Done()
			jobs[index], err = store.CreateOrReuseStoryGenerationJob(t.Context(), input)
			errs <- err
		}(index)
	}
	requests.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create/reuse: %v", err)
		}
	}
	if jobs[0].ID == "" || jobs[0].ID != jobs[1].ID || jobs[0].Status != model.AdminStoryGenerationJobQueued || jobs[0].Stage != model.AdminStoryGenerationJobStageQueued {
		t.Fatalf("active duplicate protection returned %#v and %#v", jobs[0], jobs[1])
	}
	var activeCount int
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_generation_jobs WHERE source_version_id = $1 AND status IN ('queued', 'running')`, source.VersionID).Scan(&activeCount); err != nil || activeCount != 1 {
		t.Fatalf("active job count = %d / %v, want 1", activeCount, err)
	}

	claimed, claimedOK, err := store.ClaimNextStoryGenerationJob(t.Context())
	if err != nil || !claimedOK || claimed.ID != jobs[0].ID || claimed.Status != model.AdminStoryGenerationJobRunning || claimed.Stage != model.AdminStoryGenerationJobStageAnalysingSource {
		t.Fatalf("claim = %#v / %v / %v", claimed, claimedOK, err)
	}
	if err := store.UpdateStoryGenerationJobStage(t.Context(), claimed.ID, model.AdminStoryGenerationJobStageGeneratingConfidentReaders); err != nil {
		t.Fatalf("persist stage: %v", err)
	}

	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		if semanticResult != adaptationcontract.ResultPass {
			job, err := store.CreateOrReuseStoryGenerationJob(t.Context(), input)
			if err != nil {
				t.Fatalf("create %q job: %v", semanticResult, err)
			}
			claimed, claimedOK, err = store.ClaimNextStoryGenerationJob(t.Context())
			if err != nil || !claimedOK || claimed.ID != job.ID {
				t.Fatalf("claim %q = %#v / %v / %v", semanticResult, claimed, claimedOK, err)
			}
		}
		persisted, err := store.CompleteStoryGenerationJob(t.Context(), claimed.ID, testCompletedOrchestrationResult(t, source.VersionID, sourceText, semanticResult))
		if err != nil {
			t.Fatalf("complete %q job: %v", semanticResult, err)
		}
		completed, err := store.GetStoryGenerationJob(t.Context(), claimed.ID)
		if err != nil || completed.Status != model.AdminStoryGenerationJobCompleted || completed.Stage != model.AdminStoryGenerationJobStageCompleted || completed.CompletedRunID == nil || *completed.CompletedRunID != persisted.ID || completed.FailureCode != nil || completed.CompletedAt == nil {
			t.Fatalf("completed %q job = %#v / %v", semanticResult, completed, err)
		}
		if persisted.Result.SemanticResult != semanticResult {
			t.Fatalf("completed %q semantic result = %q", semanticResult, persisted.Result.SemanticResult)
		}
	}

	failed, err := store.CreateOrReuseStoryGenerationJob(t.Context(), input)
	if err != nil {
		t.Fatalf("create failure job: %v", err)
	}
	claimed, claimedOK, err = store.ClaimNextStoryGenerationJob(t.Context())
	if err != nil || !claimedOK || claimed.ID != failed.ID {
		t.Fatalf("claim failure job = %#v / %v / %v", claimed, claimedOK, err)
	}
	beforeFailure := orchestrationRunCount(t, adminDB, source.VersionID)
	if err := store.FailStoryGenerationJob(t.Context(), claimed.ID, "provider detail must not be stored"); err == nil {
		t.Fatal("unsafe failure code unexpectedly persisted")
	}
	stillRunning, err := store.GetStoryGenerationJob(t.Context(), claimed.ID)
	if err != nil || stillRunning.Status != model.AdminStoryGenerationJobRunning {
		t.Fatalf("unsafe failure attempt changed job = %#v / %v", stillRunning, err)
	}
	if err := store.FailStoryGenerationJob(t.Context(), claimed.ID, "generation_timeout"); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if afterFailure := orchestrationRunCount(t, adminDB, source.VersionID); afterFailure != beforeFailure {
		t.Fatalf("failed job created completed evidence: before=%d after=%d", beforeFailure, afterFailure)
	}
	loadedFailure, err := store.GetStoryGenerationJob(t.Context(), claimed.ID)
	if err != nil || loadedFailure.Status != model.AdminStoryGenerationJobFailed || loadedFailure.Stage != model.AdminStoryGenerationJobStageFailed || loadedFailure.FailureCode == nil || *loadedFailure.FailureCode != "generation_timeout" || loadedFailure.CompletedRunID != nil {
		t.Fatalf("failed job = %#v / %v", loadedFailure, err)
	}

	recoverable, err := store.CreateOrReuseStoryGenerationJob(t.Context(), input)
	if err != nil {
		t.Fatalf("create recoverable job: %v", err)
	}
	claimed, claimedOK, err = store.ClaimNextStoryGenerationJob(t.Context())
	if err != nil || !claimedOK || claimed.ID != recoverable.ID {
		t.Fatalf("claim recoverable job = %#v / %v / %v", claimed, claimedOK, err)
	}
	if requeued, err := store.RequeueRunningStoryGenerationJobs(t.Context()); err != nil || requeued != 1 {
		t.Fatalf("recover abandoned jobs = %d / %v", requeued, err)
	}
	recovered, err := store.GetActiveStoryGenerationJobForSourceVersion(t.Context(), source.VersionID)
	if err != nil || recovered.ID != recoverable.ID || recovered.Status != model.AdminStoryGenerationJobQueued || recovered.Stage != model.AdminStoryGenerationJobStageQueued || recovered.StartedAt != nil {
		t.Fatalf("recovered job = %#v / %v", recovered, err)
	}

	unknown := input
	unknown.SourceVersionID = "00000000-0000-4000-8000-000000000001"
	if _, err := store.CreateOrReuseStoryGenerationJob(t.Context(), unknown); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown source create error = %v", err)
	}
}
