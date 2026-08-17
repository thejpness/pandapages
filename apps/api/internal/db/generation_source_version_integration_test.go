package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestGenerationSourceVersionIntegration(t *testing.T) {
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
		t.Fatalf("refusing generation-source test database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug LIKE 'generation-source-%'`)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	promote := func(t *testing.T, externalID, slug string) (model.AdminSourceAcquisitionSummary, model.AdminSourceAcquisitionPromotion, string) {
		t.Helper()
		candidate := testSourceAcquisitionCandidate()
		candidate.ExternalID = externalID
		candidate.LandingURL = "https://www.gutenberg.org/ebooks/" + externalID
		candidate.SelectedRepresentation.URL = "https://www.gutenberg.org/files/" + externalID + "/" + externalID + ".txt"
		persisted, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(persisted.Acquisition.ID, model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved, Note: "Approved complete canonical source."}); err != nil {
			t.Fatal(err)
		}
		promoted, err := store.AdminPromoteSourceAcquisition(persisted.Acquisition.ID, model.AdminSourceAcquisitionPromotionRequest{Target: model.AdminSourceAcquisitionPromotionTarget{
			Mode:  model.AdminSourceAcquisitionPromotionTargetNewStory,
			Title: "Generation source " + externalID,
			Slug:  slug,
		}})
		if err != nil || promoted.Outcome != model.AdminSourceAcquisitionPromotionCreated {
			t.Fatalf("promotion = %#v / %v", promoted, err)
		}
		return persisted.Acquisition, promoted.Promotion, candidate.SourceText
	}

	t.Run("valid promoted source and historical version", func(t *testing.T) {
		_, promoted, sourceText := promote(t, "1201", "generation-source-valid")
		input, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID)
		if err != nil {
			t.Fatal(err)
		}
		if input.SourceIdentity != promoted.SourceVersionID || input.CanonicalSource != sourceText || input.Title != "Alice's Adventures in Wonderland" || input.Slug != "generation-source-valid" {
			t.Fatalf("generation input = %#v", input)
		}

		later, err := store.AdminSourceUpsert(promoted.StorySlug, model.AdminSourceUpsertRequest{Title: "Manual later revision", Language: stringPointer("en"), SourceText: "Manual source text that is not generation input."})
		if err != nil {
			t.Fatal(err)
		}
		if later.VersionID == promoted.SourceVersionID {
			t.Fatal("manual revision did not move the current source pointer")
		}
		historical, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID)
		if err != nil || historical.SourceIdentity != promoted.SourceVersionID || historical.CanonicalSource != sourceText {
			t.Fatalf("historical promoted input = %#v / %v", historical, err)
		}
	})

	t.Run("unknown and manual sources reject", func(t *testing.T) {
		if _, err := store.LoadGenerationSourceVersion("00000000-0000-4000-8000-000000001201"); err == nil {
			t.Fatal("unknown source version unexpectedly loaded")
		}
		manual, err := store.AdminSourceUpsert("generation-source-manual", model.AdminSourceUpsertRequest{Title: "Manual source", Language: stringPointer("en"), SourceText: "Manual canonical source."})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadGenerationSourceVersion(manual.VersionID); err == nil {
			t.Fatal("manual source version unexpectedly loaded")
		}
	})

	t.Run("broken provenance rejects", func(t *testing.T) {
		_, promoted, _ := promote(t, "1202", "generation-source-provenance")
		if _, err := adminDB.Exec(`
			UPDATE story_source_versions
			SET source_acquisition_id = NULL, source_eligibility_assessment_id = NULL
			WHERE id = $1
		`, promoted.SourceVersionID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID); err == nil {
			t.Fatal("broken provenance unexpectedly loaded")
		}
	})

	t.Run("unapproved quality rejects", func(t *testing.T) {
		acquisition, promoted, _ := promote(t, "1203", "generation-source-quality")
		if _, err := adminDB.Exec(`
			UPDATE source_acquisition_quality_reviews
			SET status = 'rejected', note = 'No longer approved.', reviewed_at = now()
			WHERE acquisition_id = $1
		`, acquisition.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID); err == nil {
			t.Fatal("unapproved-quality source unexpectedly loaded")
		}
	})

	t.Run("tampered eligibility assessment provenance rejects", func(t *testing.T) {
		_, promoted, _ := promote(t, "1204", "generation-source-eligibility")
		if _, err := adminDB.Exec(`
			UPDATE source_acquisition_eligibility_assessments
			SET policy_version = 'stale-policy'
			WHERE id = (
				SELECT source_eligibility_assessment_id
				FROM story_source_versions
				WHERE id = $1
			)
		`, promoted.SourceVersionID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID); err == nil {
			t.Fatal("tampered eligibility assessment unexpectedly loaded")
		}
	})

	t.Run("hash-consistent stale eligibility policy rejects at provenance", func(t *testing.T) {
		_, promoted, _ := promote(t, "1205", "generation-source-stale-policy")
		ctx := context.Background()

		var sourceID, storyID, assessmentID string
		if err := adminDB.QueryRow(`
			SELECT source_id, story_id, source_eligibility_assessment_id
			FROM story_source_versions
			WHERE id = $1
		`, promoted.SourceVersionID).Scan(&sourceID, &storyID, &assessmentID); err != nil {
			t.Fatal(err)
		}
		var originalAssessmentHash string
		if err := adminDB.QueryRow(`
			SELECT assessment_hash
			FROM source_acquisition_eligibility_assessments
			WHERE id = $1
		`, assessmentID).Scan(&originalAssessmentHash); err != nil {
			t.Fatal(err)
		}
		snapshot, err := loadAdminSourceVersionSnapshot(ctx, adminDB, storyID, sourceID, promoted.SourceVersionID)
		if err != nil {
			t.Fatal(err)
		}
		assessment, err := loadSourceEligibilityAssessmentByHash(ctx, adminDB, originalAssessmentHash)
		if err != nil {
			t.Fatal(err)
		}
		eligibility, err := assessment.eligibility()
		if err != nil {
			t.Fatal(err)
		}

		staleEligibility := *eligibility
		staleEligibility.PolicyVersion = "panda-pages-copyright-v2"
		staleAssessmentHash, err := sourceEligibilityAssessmentHash(assessment.AcquisitionSnapshot, assessment.Provider, assessment.ExternalID, staleEligibility)
		if err != nil {
			t.Fatal(err)
		}
		staleCanonical := adminCanonicalSource{
			Title:      snapshot.Title,
			Author:     cloneString(snapshot.Author),
			Language:   snapshot.Language,
			Rights:     cloneJSONMap(snapshot.Rights),
			SourceURL:  cloneString(snapshot.SourceURL),
			SourceText: snapshot.SourceText,
			Provenance: &canonicalSourceProvenance{
				AcquisitionID:           assessment.AcquisitionID,
				AcquisitionSnapshotHash: assessment.AcquisitionSnapshot,
				AssessmentID:            assessment.ID,
				Provider:                assessment.Provider,
				ExternalID:              assessment.ExternalID,
				AssessmentHash:          staleAssessmentHash,
			},
		}
		staleSnapshotHash, err := canonicalSourceSnapshotHash(staleCanonical)
		if err != nil {
			t.Fatal(err)
		}

		tx, err := adminDB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`UPDATE source_acquisition_eligibility_assessments SET policy_version = $2, assessment_hash = $3 WHERE id = $1`, assessment.ID, staleEligibility.PolicyVersion, staleAssessmentHash); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`UPDATE story_source_versions SET snapshot_hash = $2 WHERE id = $1`, promoted.SourceVersionID, staleSnapshotHash); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		if computed, err := sourceEligibilityAssessmentHash(assessment.AcquisitionSnapshot, assessment.Provider, assessment.ExternalID, staleEligibility); err != nil || computed != staleAssessmentHash {
			t.Fatalf("stale assessment hash = %q / %v, want %q", computed, err, staleAssessmentHash)
		}
		if computed, err := canonicalSourceSnapshotHash(staleCanonical); err != nil || computed != staleSnapshotHash {
			t.Fatalf("stale source snapshot hash = %q / %v, want %q", computed, err, staleSnapshotHash)
		}

		if _, err := loadCanonicalSourceProvenance(ctx, adminDB, promoted.SourceVersionID); !errors.Is(err, errStoredSourceInvalid) || !strings.Contains(err.Error(), "source provenance") {
			t.Fatalf("stale policy provenance error = %v", err)
		}
		if _, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID); !errors.Is(err, errStoredSourceInvalid) || !strings.Contains(err.Error(), "source provenance") {
			t.Fatalf("stale policy generation-source error = %v", err)
		}
	})

	t.Run("tampered canonical snapshot rejects", func(t *testing.T) {
		_, promoted, _ := promote(t, "1206", "generation-source-snapshot")
		if _, err := adminDB.Exec(`
			UPDATE story_source_versions
			SET source_text = source_text || 'tampered'
			WHERE id = $1
		`, promoted.SourceVersionID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadGenerationSourceVersion(promoted.SourceVersionID); err == nil {
			t.Fatal("tampered canonical source unexpectedly loaded")
		}
	})
}
