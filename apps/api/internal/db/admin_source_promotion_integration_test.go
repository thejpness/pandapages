package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestAdminSourceAcquisitionPromotionIntegration(t *testing.T) {
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
	candidate := testSourceAcquisitionCandidate()
	candidate.ExternalID = "901"
	candidate.LandingURL = "https://www.gutenberg.org/ebooks/901"
	candidate.SelectedRepresentation.URL = "https://www.gutenberg.org/files/901/901.txt"
	persisted, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(persisted.Acquisition.ID, model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved, Note: "Exact complete source."}); err != nil {
		t.Fatal(err)
	}

	created, err := store.AdminPromoteSourceAcquisition(persisted.Acquisition.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetNewStory, Title: "Promoted Alice", Slug: "promotion-integration-alice"}})
	if err != nil || created.Outcome != model.AdminSourceAcquisitionPromotionCreated {
		t.Fatalf("new promotion=%#v / %v", created, err)
	}
	var visibility string
	var owner sql.NullString
	var currentRelease sql.NullString
	var sourceText, acquisitionID, assessmentID string
	if err := adminDB.QueryRow(`SELECT visibility, owner_account_id, current_release_id FROM stories WHERE id = $1`, created.Promotion.StoryID).Scan(&visibility, &owner, &currentRelease); err != nil || visibility != "public" || owner.Valid || currentRelease.Valid {
		t.Fatalf("promoted story=%q/%v/%v/%v", visibility, owner, currentRelease, err)
	}
	if err := adminDB.QueryRow(`SELECT source_text, source_acquisition_id, source_eligibility_assessment_id FROM story_source_versions WHERE id = $1`, created.Promotion.SourceVersionID).Scan(&sourceText, &acquisitionID, &assessmentID); err != nil || sourceText != candidate.SourceText || acquisitionID != persisted.Acquisition.ID || assessmentID == "" {
		t.Fatalf("version provenance=%q/%q/%q/%v", sourceText, acquisitionID, assessmentID, err)
	}
	var releases, editions int
	_ = adminDB.QueryRow(`SELECT count(*) FROM story_releases WHERE story_id = $1`, created.Promotion.StoryID).Scan(&releases)
	_ = adminDB.QueryRow(`SELECT count(*) FROM story_editions WHERE story_id = $1`, created.Promotion.StoryID).Scan(&editions)
	if releases != 0 || editions != 0 {
		t.Fatalf("promotion created release/edition rows: %d/%d", releases, editions)
	}

	reused, err := store.AdminPromoteSourceAcquisition(persisted.Acquisition.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetNewStory, Title: "Promoted Alice", Slug: "promotion-integration-alice"}})
	if err != nil || reused.Outcome != model.AdminSourceAcquisitionPromotionReused || reused.Promotion.SourceVersionID != created.Promotion.SourceVersionID {
		t.Fatalf("promotion reuse=%#v/%v", reused, err)
	}

	if _, err := store.AdminSourceUpsert(created.Promotion.StorySlug, model.AdminSourceUpsertRequest{Title: "Manual later revision", Language: stringPointer("en"), SourceText: "Manual later source text."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminPromoteSourceAcquisition(persisted.Acquisition.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{Mode: model.AdminSourceAcquisitionPromotionTargetNewStory, Title: "Promoted Alice", Slug: "promotion-integration-alice"}}); err != nil {
		t.Fatal(err)
	}
	var currentVersion string
	if err := adminDB.QueryRow(`SELECT current_version_id FROM story_sources WHERE story_id = $1`, created.Promotion.StoryID).Scan(&currentVersion); err != nil || currentVersion == created.Promotion.SourceVersionID {
		t.Fatalf("historical retry moved current pointer backwards: %q / %v", currentVersion, err)
	}

	if _, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(persisted.Acquisition.ID, model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityRejected, Note: "Too late"}); !errors.Is(err, model.ErrAdminSourceAcquisitionAlreadyPromoted) {
		t.Fatalf("quality lock error=%v", err)
	}
}
