package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"pandapages/api/internal/model"
)

func TestAdminSourceAcquisitionPromotionExistingStoryAndConcurrencyIntegration(t *testing.T) {
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
		t.Fatalf("refusing promotion test database %q: %v", databaseName, err)
	}

	store := newReaderIntegrationStore(t, databaseURL)
	var existingStoryID string
	if err := adminDB.QueryRow(`
		INSERT INTO stories (visibility, owner_account_id, slug, title, language, rights)
		VALUES ('public', NULL, 'promotion-existing-story', 'Existing public story', 'en', '{}'::jsonb)
		RETURNING id
	`).Scan(&existingStoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminSourceUpsert("promotion-existing-story", model.AdminSourceUpsertRequest{Title: "Manual source", Language: stringPointer("en"), SourceText: "Manual canonical source."}); err != nil {
		t.Fatal(err)
	}
	var releaseID string
	if err := adminDB.QueryRow(`INSERT INTO story_releases (story_id, release_number) VALUES ($1, 1) RETURNING id`, existingStoryID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec(`UPDATE stories SET current_release_id = $1 WHERE id = $2`, releaseID, existingStoryID); err != nil {
		t.Fatal(err)
	}

	ready := persistReadyPromotionAcquisition(t, store, "902")
	promoted, err := store.AdminPromoteSourceAcquisition(ready.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetExistingStory, StorySlug: "promotion-existing-story"}})
	if err != nil || promoted.Outcome != model.AdminSourceAcquisitionPromotionCreated || promoted.Promotion.SourceVersion != 2 {
		t.Fatalf("existing story promotion=%#v / %v", promoted, err)
	}
	var currentVersion, currentRelease string
	if err := adminDB.QueryRow(`SELECT source.current_version_id, story.current_release_id FROM story_sources AS source JOIN stories AS story ON story.id = source.story_id WHERE source.story_id = $1`, existingStoryID).Scan(&currentVersion, &currentRelease); err != nil || currentVersion != promoted.Promotion.SourceVersionID || currentRelease != releaseID {
		t.Fatalf("existing pointers=%q/%q/%v", currentVersion, currentRelease, err)
	}
	var releaseCount, sourceVersionCount int
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_releases WHERE story_id = $1`, existingStoryID).Scan(&releaseCount); err != nil || releaseCount != 1 {
		t.Fatalf("release changed: %d / %v", releaseCount, err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_source_versions WHERE story_id = $1`, existingStoryID).Scan(&sourceVersionCount); err != nil || sourceVersionCount != 2 {
		t.Fatalf("source versions=%d / %v", sourceVersionCount, err)
	}

	var otherStoryID string
	if err := adminDB.QueryRow(`
		INSERT INTO stories (visibility, owner_account_id, slug, title, language, rights)
		VALUES ('public', NULL, 'promotion-other-story', 'Other public story', 'en', '{}'::jsonb)
		RETURNING id
	`).Scan(&otherStoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminPromoteSourceAcquisition(ready.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetExistingStory, StorySlug: "promotion-other-story"}}); !errors.Is(err, model.ErrAdminSourceAcquisitionAlreadyPromoted) {
		t.Fatalf("different target error=%v", err)
	}
	var otherVersions int
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_source_versions WHERE story_id = $1`, otherStoryID).Scan(&otherVersions); err != nil || otherVersions != 0 {
		t.Fatalf("different target wrote source versions=%d / %v", otherVersions, err)
	}

	pending := persistPromotionAcquisition(t, store, "903")
	var storiesBefore int
	if err := adminDB.QueryRow(`SELECT count(*) FROM stories`).Scan(&storiesBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminPromoteSourceAcquisition(pending.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetNewStory, Title: "Blocked", Slug: "promotion-blocked"}}); !errors.Is(err, model.ErrAdminSourceAcquisitionNotReady) {
		t.Fatalf("pending quality error=%v", err)
	}
	var storiesAfter int
	if err := adminDB.QueryRow(`SELECT count(*) FROM stories`).Scan(&storiesAfter); err != nil || storiesAfter != storiesBefore {
		t.Fatalf("blocked promotion wrote story rows=%d/%d / %v", storiesBefore, storiesAfter, err)
	}

	concurrent := persistReadyPromotionAcquisition(t, store, "904")
	stores := []*Store{newReaderIntegrationStore(t, databaseURL), newReaderIntegrationStore(t, databaseURL)}
	start := make(chan struct{})
	results := make(chan model.AdminSourceAcquisitionPromotionResponse, len(stores))
	errorsOut := make(chan error, len(stores))
	var wait sync.WaitGroup
	for _, concurrentStore := range stores {
		wait.Add(1)
		go func(current *Store) {
			defer wait.Done()
			<-start
			result, callErr := current.AdminPromoteSourceAcquisition(concurrent.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetNewStory, Title: "Concurrent", Slug: "promotion-concurrent"}})
			results <- result
			errorsOut <- callErr
		}(concurrentStore)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	var promotions []model.AdminSourceAcquisitionPromotionResponse
	for callErr := range errorsOut {
		if callErr != nil {
			t.Fatalf("concurrent promotion: %v", callErr)
		}
	}
	for result := range results {
		promotions = append(promotions, result)
	}
	if len(promotions) != 2 || promotions[0].Promotion.SourceVersionID == "" || promotions[0].Promotion.SourceVersionID != promotions[1].Promotion.SourceVersionID {
		t.Fatalf("concurrent promotions=%#v", promotions)
	}
	var concurrentVersions, concurrentStories int
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_source_versions WHERE source_acquisition_id = $1`, concurrent.ID).Scan(&concurrentVersions); err != nil || concurrentVersions != 1 {
		t.Fatalf("concurrent versions=%d / %v", concurrentVersions, err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM stories WHERE slug = 'promotion-concurrent'`).Scan(&concurrentStories); err != nil || concurrentStories != 1 {
		t.Fatalf("concurrent stories=%d / %v", concurrentStories, err)
	}
}

func persistPromotionAcquisition(t *testing.T, store *Store, externalID string) model.AdminSourceAcquisitionSummary {
	t.Helper()
	candidate := testSourceAcquisitionCandidate()
	candidate.ExternalID = externalID
	candidate.LandingURL = "https://www.gutenberg.org/ebooks/" + externalID
	candidate.SelectedRepresentation.URL = "https://www.gutenberg.org/files/" + externalID + "/" + externalID + ".txt"
	persisted, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate))
	if err != nil {
		t.Fatal(err)
	}
	return persisted.Acquisition
}

func persistReadyPromotionAcquisition(t *testing.T, store *Store, externalID string) model.AdminSourceAcquisitionSummary {
	t.Helper()
	persisted := persistPromotionAcquisition(t, store, externalID)
	if _, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(persisted.ID, model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved, Note: "Exact complete source."}); err != nil {
		t.Fatal(err)
	}
	return persisted
}
