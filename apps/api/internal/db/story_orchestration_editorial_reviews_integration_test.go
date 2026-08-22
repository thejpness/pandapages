package db

import (
	"database/sql"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storyeditorialreview"
)

const (
	editorialReviewIntegrationPrincipalID = "d1500000-0000-4000-8000-000000000001"
	editorialReviewIntegrationAccountID   = "d1500000-0000-4000-8000-000000000002"
)

func TestStoryOrchestrationEditorialReviewsIntegration(t *testing.T) {
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
		t.Fatalf("refusing editorial review database %q: %v", databaseName, err)
	}

	const slug = "story-orchestration-editorial-reviews-integration"
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_run_editorial_reviews
			WHERE run_id IN (
				SELECT run.id
				FROM story_orchestration_runs AS run
				JOIN story_source_versions AS version ON version.id = run.source_version_id
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
		_, _ = adminDB.Exec(`DELETE FROM account_memberships WHERE principal_id = $1 AND account_id = $2`, editorialReviewIntegrationPrincipalID, editorialReviewIntegrationAccountID)
		_, _ = adminDB.Exec(`DELETE FROM principals WHERE id = $1`, editorialReviewIntegrationPrincipalID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, editorialReviewIntegrationAccountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Editorial review integration account')`, editorialReviewIntegrationAccountID); err != nil {
		t.Fatalf("insert reviewer account: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO principals (id, display_name) VALUES ($1, 'Editorial review integration principal')`, editorialReviewIntegrationPrincipalID); err != nil {
		t.Fatalf("insert reviewer principal: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO account_memberships (principal_id, account_id, role) VALUES ($1, $2, 'owner')`, editorialReviewIntegrationPrincipalID, editorialReviewIntegrationAccountID); err != nil {
		t.Fatalf("insert reviewer membership: %v", err)
	}

	sourceText := "# The Editorial Lantern\n\nA traveller follows a lantern home.\n"
	source, err := store.AdminSourceUpsert(slug, model.AdminSourceUpsertRequest{
		Title: "The Editorial Lantern", Language: stringPointer("en-GB"), SourceText: sourceText,
	})
	if err != nil {
		t.Fatalf("create canonical source: %v", err)
	}

	newRun := func(t *testing.T, semanticResult adaptationcontract.Result) string {
		t.Helper()
		persisted, err := store.PersistCompletedStoryOrchestrationRun(source.VersionID, testCompletedOrchestrationResult(t, source.VersionID, sourceText, semanticResult))
		if err != nil {
			t.Fatalf("persist %q run: %v", semanticResult, err)
		}
		return persisted.ID
	}
	runID := newRun(t, adaptationcontract.ResultPass)

	zeroRunID := newRun(t, adaptationcontract.ResultPass)
	zero, err := store.ListStoryOrchestrationEditorialReviews(zeroRunID, 0)
	if err != nil || len(zero.Items) != 0 {
		t.Fatalf("zero review history = %#v / %v", zero, err)
	}

	service, err := storyeditorialreview.New(storyeditorialreview.Config{
		ValidatedRunReader: store,
		Writer:             store,
		Reader:             store,
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(decision model.AdminStoryOrchestrationEditorialDecision) model.AdminStoryOrchestrationEditorialReview {
		t.Helper()
		review, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
			RunID:               runID,
			Decision:            decision,
			ReviewerPrincipalID: editorialReviewIntegrationPrincipalID,
			ReviewerAccountID:   editorialReviewIntegrationAccountID,
		})
		if err != nil {
			t.Fatalf("append %q review: %v", decision, err)
		}
		if review.RunID != runID || review.Decision != decision || review.ReviewerPrincipalID != editorialReviewIntegrationPrincipalID || review.ReviewerAccountID != editorialReviewIntegrationAccountID || review.ID == "" || review.CreatedAt == "" {
			t.Fatalf("stored review = %#v", review)
		}
		return review
	}
	first := create(model.AdminStoryOrchestrationEditorialDecisionApproved)
	second := create(model.AdminStoryOrchestrationEditorialDecisionApproved)
	third := create(model.AdminStoryOrchestrationEditorialDecisionRejected)
	if first.ID == second.ID || second.ID == third.ID {
		t.Fatalf("append-only identifiers were reused: %#v %#v %#v", first, second, third)
	}

	// Identical timestamps deliberately exercise the documented ID tie-breaker.
	if _, err := adminDB.Exec(`UPDATE story_orchestration_run_editorial_reviews SET created_at = $1 WHERE run_id = $2`, time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC), runID); err != nil {
		t.Fatalf("align review timestamps: %v", err)
	}
	history, err := store.ListStoryOrchestrationEditorialReviews(runID, 100)
	if err != nil || len(history.Items) != 3 {
		t.Fatalf("review history = %#v / %v", history, err)
	}
	wantIDs := []string{first.ID, second.ID, third.ID}
	sort.Sort(sort.Reverse(sort.StringSlice(wantIDs)))
	for index, wantID := range wantIDs {
		if history.Items[index].ID != wantID {
			t.Fatalf("history[%d] = %#v, want ID %q", index, history.Items[index], wantID)
		}
	}
	limited, err := store.ListStoryOrchestrationEditorialReviews(runID, 1)
	if err != nil || len(limited.Items) != 1 || limited.Items[0].ID != wantIDs[0] {
		t.Fatalf("bounded history = %#v / %v", limited, err)
	}
	if _, err := store.ListStoryOrchestrationEditorialReviews(runID, 101); err == nil {
		t.Fatal("limit above maximum unexpectedly succeeded")
	}
	if _, err := store.ListStoryOrchestrationEditorialReviews("00000000-0000-4000-8000-000000000001", 50); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown run error = %v", err)
	}

	if _, err := store.CreateStoryOrchestrationEditorialReview(model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID: runID, Decision: model.AdminStoryOrchestrationEditorialDecisionApproved,
		ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: "d1500000-0000-4000-8000-000000000099",
	}); err == nil {
		t.Fatal("missing reviewer membership unexpectedly persisted")
	}
	if _, err := store.CreateStoryOrchestrationEditorialReview(model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID: "00000000-0000-4000-8000-000000000001", Decision: model.AdminStoryOrchestrationEditorialDecisionApproved,
		ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: editorialReviewIntegrationAccountID,
	}); err == nil {
		t.Fatal("unknown run unexpectedly persisted")
	}
	if _, err := adminDB.Exec(`
		INSERT INTO story_orchestration_run_editorial_reviews (run_id, decision, reviewer_principal_id, reviewer_account_id)
		VALUES ($1, 'invalid', $2, $3)
	`, runID, editorialReviewIntegrationPrincipalID, editorialReviewIntegrationAccountID); err == nil {
		t.Fatal("decision CHECK constraint unexpectedly accepted invalid decision")
	}

	var concurrent sync.WaitGroup
	errs := make(chan error, 2)
	for _, decision := range []model.AdminStoryOrchestrationEditorialDecision{
		model.AdminStoryOrchestrationEditorialDecisionApproved,
		model.AdminStoryOrchestrationEditorialDecisionRejected,
	} {
		concurrent.Add(1)
		go func(decision model.AdminStoryOrchestrationEditorialDecision) {
			defer concurrent.Done()
			_, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
				RunID: runID, Decision: decision,
				ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: editorialReviewIntegrationAccountID,
			})
			errs <- err
		}(decision)
	}
	concurrent.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}
	concurrentHistory, err := store.ListStoryOrchestrationEditorialReviews(runID, 100)
	if err != nil || len(concurrentHistory.Items) != 5 {
		t.Fatalf("concurrent history = %#v / %v", concurrentHistory, err)
	}

	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		decisionRunID := newRun(t, semanticResult)
		for _, decision := range []model.AdminStoryOrchestrationEditorialDecision{
			model.AdminStoryOrchestrationEditorialDecisionApproved,
			model.AdminStoryOrchestrationEditorialDecisionRejected,
		} {
			if _, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
				RunID: decisionRunID, Decision: decision,
				ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: editorialReviewIntegrationAccountID,
			}); err != nil {
				t.Fatalf("machine %q with human %q: %v", semanticResult, decision, err)
			}
		}
	}

	corruptRunID := newRun(t, adaptationcontract.ResultPass)
	if _, err := adminDB.Exec(`
		UPDATE story_orchestration_runs
		SET artifacts = '{"analysisArtifact":{},"editions":[{},{},{},{}],"editionAssessments":[{},{},{},{}],"bundleAssessment":{}}'::jsonb
		WHERE id = $1
	`, corruptRunID); err != nil {
		t.Fatalf("tamper retained artifacts: %v", err)
	}
	if _, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID: corruptRunID, Decision: model.AdminStoryOrchestrationEditorialDecisionApproved,
		ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: editorialReviewIntegrationAccountID,
	}); !errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired) {
		t.Fatalf("corrupt run create error = %v", err)
	}
	// History uses the deliberately lightweight existence boundary, so a known
	// corrupt run still has a bounded empty history rather than becoming a 404.
	if history, err := store.ListStoryOrchestrationEditorialReviews(corruptRunID, 50); err != nil || len(history.Items) != 0 {
		t.Fatalf("corrupt run lightweight history = %#v / %v", history, err)
	}

	invalidSourceRunID := newRun(t, adaptationcontract.ResultPass)
	if _, err := adminDB.Exec(`UPDATE story_source_versions SET source_text = 'tampered canonical source' WHERE id = $1`, source.VersionID); err != nil {
		t.Fatalf("tamper source binding: %v", err)
	}
	if _, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID: invalidSourceRunID, Decision: model.AdminStoryOrchestrationEditorialDecisionRejected,
		ReviewerPrincipalID: editorialReviewIntegrationPrincipalID, ReviewerAccountID: editorialReviewIntegrationAccountID,
	}); !errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired) {
		t.Fatalf("source-binding-invalid create error = %v", err)
	}
}
