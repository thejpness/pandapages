package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
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
			DELETE FROM story_generation_usage_events
			WHERE generation_job_id IN (
				SELECT job.id
				FROM story_generation_jobs AS job
				JOIN story_source_versions AS version ON version.id = job.source_version_id
				JOIN stories AS story ON story.id = version.story_id
				WHERE story.slug = $1
			)
		`, slug)
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
	mismatchedResult := testCompletedOrchestrationResult(t, source.VersionID, sourceText, adaptationcontract.ResultPass)
	mismatchedObservations := assignUsageObservations(t, &mismatchedResult, claimed.ID)
	if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, mismatchedObservations[0]); err != nil {
		t.Fatalf("record incomplete completed-job usage: %v", err)
	}
	beforeMismatch := orchestrationRunCount(t, adminDB, source.VersionID)
	if _, err := store.CompleteStoryGenerationJob(t.Context(), claimed.ID, mismatchedResult); err == nil {
		t.Fatal("completed job with incomplete usage ledger unexpectedly succeeded")
	}
	if afterMismatch := orchestrationRunCount(t, adminDB, source.VersionID); afterMismatch != beforeMismatch {
		t.Fatalf("mismatched completed job persisted orchestration evidence: before=%d after=%d", beforeMismatch, afterMismatch)
	}
	mismatchedJob, err := store.GetStoryGenerationJob(t.Context(), claimed.ID)
	if err != nil || mismatchedJob.Status != model.AdminStoryGenerationJobRunning || mismatchedJob.CompletedRunID != nil {
		t.Fatalf("mismatched completed job = %#v / %v", mismatchedJob, err)
	}
	if err := store.FailStoryGenerationJob(t.Context(), claimed.ID, "generation_timeout"); err != nil {
		t.Fatalf("fail mismatched completed job: %v", err)
	}
	wrongUsageJob, err := store.CreateOrReuseStoryGenerationJob(t.Context(), input)
	if err != nil {
		t.Fatalf("create wrong-usage job: %v", err)
	}
	claimed, claimedOK, err = store.ClaimNextStoryGenerationJob(t.Context())
	if err != nil || !claimedOK || claimed.ID != wrongUsageJob.ID {
		t.Fatalf("claim wrong-usage job = %#v / %v / %v", claimed, claimedOK, err)
	}
	wrongUsageResult := testCompletedOrchestrationResult(t, source.VersionID, sourceText, adaptationcontract.ResultPass)
	wrongUsageObservations := assignUsageObservations(t, &wrongUsageResult, claimed.ID)
	for index, observation := range wrongUsageObservations {
		if index == 0 {
			observation.Usage.TotalTokens++
		}
		if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, observation); err != nil {
			t.Fatalf("record wrong completed-job usage: %v", err)
		}
	}
	if _, err := store.CompleteStoryGenerationJob(t.Context(), claimed.ID, wrongUsageResult); err == nil {
		t.Fatal("completed job with mismatched final usage unexpectedly succeeded")
	}
	wrongUsageState, err := store.GetStoryGenerationJob(t.Context(), claimed.ID)
	if err != nil || wrongUsageState.Status != model.AdminStoryGenerationJobRunning || wrongUsageState.CompletedRunID != nil {
		t.Fatalf("wrong-usage completed job = %#v / %v", wrongUsageState, err)
	}
	if err := store.FailStoryGenerationJob(t.Context(), claimed.ID, "generation_timeout"); err != nil {
		t.Fatalf("fail wrong-usage completed job: %v", err)
	}

	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		job, err := store.CreateOrReuseStoryGenerationJob(t.Context(), input)
		if err != nil {
			t.Fatalf("create %q job: %v", semanticResult, err)
		}
		claimed, claimedOK, err = store.ClaimNextStoryGenerationJob(t.Context())
		if err != nil || !claimedOK || claimed.ID != job.ID {
			t.Fatalf("claim %q = %#v / %v / %v", semanticResult, claimed, claimedOK, err)
		}
		result := testCompletedOrchestrationResult(t, source.VersionID, sourceText, semanticResult)
		observations := assignUsageObservations(t, &result, claimed.ID)
		earlierObservations := []storygeneration.ResponsesUsageObservation(nil)
		if semanticResult == adaptationcontract.ResultPass {
			earlierObservations = []storygeneration.ResponsesUsageObservation{
				{
					Operation:          storygeneration.ResponsesOperationAnalyseSource,
					ProviderResponseID: "resp-abandoned-attempt-analysis",
					RequestedModel:     "requested-model",
					ReturnedModel:      "returned-model",
					Usage:              storygeneration.ResponsesUsage{InputTokens: 11, TotalTokens: 11},
				},
				{
					Operation:          storygeneration.ResponsesOperationGenerateConfidentReaders,
					ProviderResponseID: "resp-abandoned-attempt-edition",
					RequestedModel:     "requested-model",
					ReturnedModel:      "returned-model",
					Usage:              storygeneration.ResponsesUsage{InputTokens: 12, TotalTokens: 12},
				},
				{
					Operation:          storygeneration.ResponsesOperationValidateConfidentReaders,
					ProviderResponseID: "resp-abandoned-attempt-validation",
					RequestedModel:     "requested-model",
					ReturnedModel:      "returned-model",
					Usage:              storygeneration.ResponsesUsage{InputTokens: 13, TotalTokens: 13},
				},
			}
			for _, observation := range earlierObservations {
				if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, observation); err != nil {
					t.Fatalf("record abandoned-attempt usage: %v", err)
				}
			}
		}
		for _, observation := range observations {
			if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, observation); err != nil {
				t.Fatalf("record usage: %v", err)
			}
		}
		if semanticResult == adaptationcontract.ResultPass {
			if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, observations[0]); err != nil {
				t.Fatalf("idempotent duplicate usage: %v", err)
			}
			conflicting := observations[0]
			conflicting.Usage.TotalTokens++
			if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, conflicting); err == nil {
				t.Fatal("conflicting provider response ID unexpectedly accepted")
			}
		}
		persisted, err := store.CompleteStoryGenerationJob(t.Context(), claimed.ID, result)
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
		events, err := store.ListStoryGenerationUsageEvents(t.Context(), claimed.ID)
		if err != nil {
			t.Fatalf("list usage events: %v", err)
		}
		assertUsageEventsReconcileCompletedResult(t, events, persisted.Result)
		if semanticResult == adaptationcontract.ResultPass {
			if len(events) != len(observations)+len(earlierObservations) {
				t.Fatalf("completed job usage event count = %d, want %d", len(events), len(observations)+len(earlierObservations))
			}
			assertUsageEventsPresent(t, events, earlierObservations)
			if got, want := totalObservedTokens(events), totalObservedTokensFromObservations(observations)+totalObservedTokensFromObservations(earlierObservations); got != want {
				t.Fatalf("completed job total observed tokens = %d, want %d", got, want)
			}
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
	firstObserved := storygeneration.ResponsesUsageObservation{
		Operation:          storygeneration.ResponsesOperationAnalyseSource,
		ProviderResponseID: "resp-failed-job-first-observed-call",
		RequestedModel:     "requested-model",
		ReturnedModel:      "returned-model",
		Usage:              storygeneration.ResponsesUsage{InputTokens: 10, TotalTokens: 10},
	}
	if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, firstObserved); err != nil {
		t.Fatalf("record first failed-job usage: %v", err)
	}
	secondObserved := firstObserved
	secondObserved.ProviderResponseID = "resp-failed-job-replayed-call"
	if err := store.RecordStoryGenerationUsage(t.Context(), claimed.ID, secondObserved); err != nil {
		t.Fatalf("record replayed failed-job usage: %v", err)
	}
	failedEvents, err := store.ListStoryGenerationUsageEvents(t.Context(), claimed.ID)
	if err != nil || len(failedEvents) != 2 ||
		failedEvents[0].ProviderResponseID == failedEvents[1].ProviderResponseID {
		t.Fatalf("failed-job observed events = %#v / %v", failedEvents, err)
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

func assignUsageObservations(
	t *testing.T,
	result *storyorchestration.Result,
	jobID string,
) []storygeneration.ResponsesUsageObservation {
	t.Helper()
	sequence := 0
	next := func(operation storygeneration.ResponsesOperation, requested, returned string) storygeneration.ResponsesUsageObservation {
		sequence++
		usage := storygeneration.ResponsesUsage{
			InputTokens:     sequence * 10,
			CachedTokens:    sequence,
			OutputTokens:    sequence * 2,
			ReasoningTokens: sequence * 3,
			TotalTokens:     sequence * 15,
		}
		return storygeneration.ResponsesUsageObservation{
			Operation:          operation,
			ProviderResponseID: fmt.Sprintf("resp-%s-%02d", jobID, sequence),
			RequestedModel:     requested,
			ReturnedModel:      returned,
			Usage:              usage,
		}
	}
	apply := func(observation storygeneration.ResponsesUsageObservation, responseID *string, usage *storygeneration.ResponsesUsage) storygeneration.ResponsesUsageObservation {
		*responseID = observation.ProviderResponseID
		*usage = observation.Usage
		return observation
	}

	observations := make([]storygeneration.ResponsesUsageObservation, 0, 10)
	analysis := next(storygeneration.ResponsesOperationAnalyseSource, result.AnalysisArtifact.RequestedModel, result.AnalysisArtifact.ReturnedModel)
	observations = append(observations, apply(analysis, &result.AnalysisArtifact.ResponseID, &result.AnalysisArtifact.Usage))
	for index := range result.Editions {
		edition := &result.Editions[index]
		operation := map[model.AdminStoryEditionKey]storygeneration.ResponsesOperation{
			model.AdminStoryEditionConfidentReaders: storygeneration.ResponsesOperationGenerateConfidentReaders,
			model.AdminStoryEditionGrowingReaders:   storygeneration.ResponsesOperationGenerateGrowingReaders,
			model.AdminStoryEditionStoryExplorers:   storygeneration.ResponsesOperationGenerateStoryExplorers,
			model.AdminStoryEditionLittleListeners:  storygeneration.ResponsesOperationGenerateLittleListeners,
		}[edition.EditionKey]
		observation := next(operation, edition.RequestedModel, edition.ReturnedModel)
		observations = append(observations, apply(observation, &edition.ResponseID, &edition.Usage))
	}
	for index := range result.EditionAssessments {
		assessment := &result.EditionAssessments[index]
		operation := map[model.AdminStoryEditionKey]storygeneration.ResponsesOperation{
			model.AdminStoryEditionConfidentReaders: storygeneration.ResponsesOperationValidateConfidentReaders,
			model.AdminStoryEditionGrowingReaders:   storygeneration.ResponsesOperationValidateGrowingReaders,
			model.AdminStoryEditionStoryExplorers:   storygeneration.ResponsesOperationValidateStoryExplorers,
			model.AdminStoryEditionLittleListeners:  storygeneration.ResponsesOperationValidateLittleListeners,
		}[result.Editions[index].EditionKey]
		observation := next(operation, assessment.RequestedModel, assessment.ReturnedModel)
		observations = append(observations, apply(observation, &assessment.ResponseID, &assessment.Usage))
	}
	bundle := next(storygeneration.ResponsesOperationValidateBundle, result.BundleAssessment.RequestedModel, result.BundleAssessment.ReturnedModel)
	observations = append(observations, apply(bundle, &result.BundleAssessment.ResponseID, &result.BundleAssessment.Usage))
	return observations
}

func assertUsageEventsReconcileCompletedResult(
	t *testing.T,
	events []storygeneration.RecordedResponsesUsageEvent,
	result storyorchestration.Result,
) {
	t.Helper()
	want := make(map[string]storygeneration.ResponsesUsage, 10)
	want[result.AnalysisArtifact.ResponseID] = result.AnalysisArtifact.Usage
	for _, edition := range result.Editions {
		want[edition.ResponseID] = edition.Usage
	}
	for _, assessment := range result.EditionAssessments {
		want[assessment.ResponseID] = assessment.Usage
	}
	want[result.BundleAssessment.ResponseID] = result.BundleAssessment.Usage
	if len(want) != 10 {
		t.Fatalf("completed result response IDs = %d, want 10", len(want))
	}
	for _, event := range events {
		usage, ok := want[event.ProviderResponseID]
		if !ok {
			continue
		}
		if usage != event.Usage {
			t.Fatalf("usage event does not reconcile: %#v", event)
		}
		delete(want, event.ProviderResponseID)
	}
	if len(want) != 0 {
		t.Fatalf("completed result response IDs missing from ledger: %v", want)
	}
}

func assertUsageEventsPresent(t *testing.T, events []storygeneration.RecordedResponsesUsageEvent, observations []storygeneration.ResponsesUsageObservation) {
	t.Helper()
	actual := make(map[string]storygeneration.ResponsesUsage, len(events))
	for _, event := range events {
		actual[event.ProviderResponseID] = event.Usage
	}
	for _, observation := range observations {
		if actual[observation.ProviderResponseID] != observation.Usage {
			t.Fatalf("expected retained usage event missing or changed: %#v", observation)
		}
	}
}

func totalObservedTokens(events []storygeneration.RecordedResponsesUsageEvent) int {
	total := 0
	for _, event := range events {
		total += event.Usage.TotalTokens
	}
	return total
}

func totalObservedTokensFromObservations(observations []storygeneration.ResponsesUsageObservation) int {
	total := 0
	for _, observation := range observations {
		total += observation.Usage.TotalTokens
	}
	return total
}
