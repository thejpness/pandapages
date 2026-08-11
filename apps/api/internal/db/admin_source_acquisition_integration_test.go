package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceeligibility"
	"pandapages/api/internal/sourceprovider"
)

func TestAdminSourceAcquisitionIntegration(t *testing.T) {
	if os.Getenv(readerIntegrationGuardVar) != "1" {
		t.Skip("set PP_READER_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(readerIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required when %s=1", readerIntegrationURLVar, readerIntegrationGuardVar)
	}
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	var databaseName string
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != readerIntegrationDBName {
		t.Fatalf("refusing acquisition test in database %q: %v", databaseName, err)
	}

	store := newReaderIntegrationStore(t, databaseURL)
	candidate := testSourceAcquisitionCandidate()
	first, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate))
	if err != nil {
		t.Fatalf("persist first acquisition: %v", err)
	}
	if first.Outcome != model.AdminSourceAcquisitionOutcomeCreated || first.Acquisition.Eligibility == nil || first.Acquisition.Eligibility.Overall != "eligible" || first.Acquisition.SourceQuality.Status != model.AdminSourceQualityPending {
		t.Fatalf("first acquisition = %#v", first)
	}
	var assessmentCount, qualityCount int
	if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments WHERE acquisition_id = $1`, first.Acquisition.ID).Scan(&assessmentCount); err != nil || assessmentCount != 1 {
		t.Fatalf("initial assessment count = %d / %v", assessmentCount, err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews WHERE acquisition_id = $1`, first.Acquisition.ID).Scan(&qualityCount); err != nil || qualityCount != 1 {
		t.Fatalf("initial source-quality count = %d / %v", qualityCount, err)
	}

	quality, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(first.Acquisition.ID, model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved, Note: "Complete, intended source text."})
	if err != nil || quality.SourceQuality.Status != model.AdminSourceQualityApproved || quality.SourceQuality.Note == nil {
		t.Fatalf("source quality = %#v / %v", quality, err)
	}
	reused, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate))
	if err != nil || reused.Outcome != model.AdminSourceAcquisitionOutcomeReused || reused.Acquisition.ID != first.Acquisition.ID || reused.Acquisition.SourceQuality.Status != model.AdminSourceQualityApproved {
		t.Fatalf("healthy reuse = %#v / %v", reused, err)
	}

	t.Run("blocked evaluation writes nothing", func(t *testing.T) {
		var beforeAcquisitions, beforeAssessments, beforeQuality int
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&beforeAcquisitions); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&beforeAssessments); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&beforeQuality); err != nil {
			t.Fatal(err)
		}
		blocked := testEligibleEvaluation(candidate)
		blocked.Assessment.Overall = copyrighteligibility.OverallBlocked
		if _, err := store.AdminPersistEligibleSourceAcquisition(blocked); err == nil {
			t.Fatal("blocked evaluation unexpectedly persisted")
		}
		var afterAcquisitions, afterAssessments, afterQuality int
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&afterAcquisitions)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&afterAssessments)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&afterQuality)
		if beforeAcquisitions != afterAcquisitions || beforeAssessments != afterAssessments || beforeQuality != afterQuality {
			t.Fatalf("blocked evaluation changed rows: %d/%d/%d -> %d/%d/%d", beforeAcquisitions, beforeAssessments, beforeQuality, afterAcquisitions, afterAssessments, afterQuality)
		}
	})

	for name, mutate := range map[string]func(*sourceprovider.SourceCandidate){
		"source": func(value *sourceprovider.SourceCandidate) {
			value.SourceText = "Changed source text.\n"
			value.NormalisedContentHash = sourceAcquisitionSHA256(value.SourceText)
		},
		"provider rights": func(value *sourceprovider.SourceCandidate) { value.ProviderRights = "A changed provider statement." },
		"representation": func(value *sourceprovider.SourceCandidate) {
			value.SelectedRepresentation.URL = "https://www.gutenberg.org/files/11/11.txt"
		},
		"retrieved hash": func(value *sourceprovider.SourceCandidate) { value.RetrievedContentHash = strings.Repeat("b", 64) },
	} {
		t.Run("changed "+name+" creates distinct immutable snapshot", func(t *testing.T) {
			changed := candidate
			mutate(&changed)
			created, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(changed))
			if err != nil || created.Outcome != model.AdminSourceAcquisitionOutcomeCreated || created.Acquisition.ID == first.Acquisition.ID {
				t.Fatalf("changed %s = %#v / %v", name, created, err)
			}
		})
	}

	t.Run("corrupt stored assessment refuses reuse without mutation", func(t *testing.T) {
		assessmentCandidate := candidate
		assessmentCandidate.ExternalID = "13"
		persisted, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(assessmentCandidate))
		if err != nil {
			t.Fatal(err)
		}
		var beforeAcquisitions, beforeAssessments, beforeQuality int
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&beforeAcquisitions); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&beforeAssessments); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&beforeQuality); err != nil {
			t.Fatal(err)
		}
		if _, err := adminDB.Exec(`UPDATE source_acquisition_eligibility_assessments SET provider_evidence = '{}'::jsonb WHERE acquisition_id = $1`, persisted.Acquisition.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(assessmentCandidate)); !errors.Is(err, errStoredSourceAcquisitionInvalid) {
			t.Fatalf("corrupt assessment reuse error = %v", err)
		}
		var afterAcquisitions, afterAssessments, afterQuality int
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&afterAcquisitions)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&afterAssessments)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&afterQuality)
		if beforeAcquisitions != afterAcquisitions || beforeAssessments != afterAssessments || beforeQuality != afterQuality {
			t.Fatalf("corrupt assessment reuse changed rows: %d/%d/%d -> %d/%d/%d", beforeAcquisitions, beforeAssessments, beforeQuality, afterAcquisitions, afterAssessments, afterQuality)
		}
	})

	t.Run("corrupt immutable snapshot refuses reuse without mutation", func(t *testing.T) {
		var beforeAcquisitions, beforeAssessments, beforeQuality int
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&beforeAcquisitions); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&beforeAssessments); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&beforeQuality); err != nil {
			t.Fatal(err)
		}
		if _, err := adminDB.Exec(`UPDATE source_acquisitions SET title = $2 WHERE id = $1`, first.Acquisition.ID, "Corrupt but syntactically valid title"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(candidate)); !errors.Is(err, errStoredSourceAcquisitionInvalid) {
			t.Fatalf("corrupt reuse error = %v", err)
		}
		var afterAcquisitions, afterAssessments, afterQuality int
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions`).Scan(&afterAcquisitions)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments`).Scan(&afterAssessments)
		_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews`).Scan(&afterQuality)
		if beforeAcquisitions != afterAcquisitions || beforeAssessments != afterAssessments || beforeQuality != afterQuality {
			t.Fatalf("corrupt reuse changed rows: %d/%d/%d -> %d/%d/%d", beforeAcquisitions, beforeAssessments, beforeQuality, afterAcquisitions, afterAssessments, afterQuality)
		}
	})

	concurrent := candidate
	concurrent.ExternalID = "12"
	stores := []*Store{newReaderIntegrationStore(t, databaseURL), newReaderIntegrationStore(t, databaseURL)}
	results := make(chan model.AdminSourceAcquisitionPersistResponse, len(stores))
	errorsOut := make(chan error, len(stores))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, concurrentStore := range stores {
		wait.Add(1)
		go func(current *Store) {
			defer wait.Done()
			<-start
			result, err := current.AdminPersistEligibleSourceAcquisition(testEligibleEvaluation(concurrent))
			results <- result
			errorsOut <- err
		}(concurrentStore)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	var concurrentResults []model.AdminSourceAcquisitionPersistResponse
	for err := range errorsOut {
		if err != nil {
			t.Fatalf("concurrent persistence: %v", err)
		}
	}
	for result := range results {
		concurrentResults = append(concurrentResults, result)
	}
	if len(concurrentResults) != 2 || concurrentResults[0].Acquisition.ID == "" || concurrentResults[0].Acquisition.ID != concurrentResults[1].Acquisition.ID {
		t.Fatalf("concurrent results = %#v", concurrentResults)
	}
	concurrentInput, err := adminSourceAcquisitionInput(concurrent)
	if err != nil {
		t.Fatal(err)
	}
	var acquisitions, assessments, qualityRows int
	_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions WHERE snapshot_hash = $1`, concurrentInput.SnapshotHash).Scan(&acquisitions)
	_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_eligibility_assessments WHERE acquisition_snapshot_hash = $1`, concurrentInput.SnapshotHash).Scan(&assessments)
	_ = adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_quality_reviews AS quality JOIN source_acquisitions AS acquisition ON acquisition.id = quality.acquisition_id WHERE acquisition.snapshot_hash = $1`, concurrentInput.SnapshotHash).Scan(&qualityRows)
	if acquisitions != 1 || assessments != 1 || qualityRows != 1 {
		t.Fatalf("concurrent rows = %d/%d/%d", acquisitions, assessments, qualityRows)
	}
}

func testEligibleEvaluation(candidate sourceprovider.SourceCandidate) sourceeligibility.Evaluation {
	death := 1898
	provider := copyrighteligibility.ProviderEvidence{Provider: string(sourceprovider.ProjectGutenberg), ExternalID: candidate.ExternalID, Title: candidate.Title, Rights: copyrighteligibility.ProviderRightsPublicDomain, Contributors: []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}}, Languages: []string{"en"}, EvidenceDigest: strings.Repeat("d", 64)}
	effective := copyrighteligibility.UKEvidence{WorkTitle: candidate.Title, WorkCategory: copyrighteligibility.WorkCategoryOrdinaryLiterary, WorkCategoryReferences: []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: "Ordinary literary work"}}, Authorship: copyrighteligibility.AuthorshipSingleKnown, AuthorshipReferences: []copyrighteligibility.EvidenceReference{{Source: "Project Gutenberg RDF", Fact: "One author"}}, Author: copyrighteligibility.PersonEvidence{Name: "Lewis Carroll", DeathYear: death, References: []copyrighteligibility.EvidenceReference{{Source: "Project Gutenberg RDF", Fact: "Death year"}}}, FirstPublication: copyrighteligibility.PublicationEvidence{Year: 1865, References: []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: "First published in 1865"}}}, Translation: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: "No translation in acquired text"}}}, AdditionalTextualContribution: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: "No additional textual contribution"}}}, UnpublishedAtEnd1988: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: "Published before 1988"}}}}
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{EvaluationDate: date, US: copyrighteligibility.USProviderEvidence{OPDSRights: copyrighteligibility.ProviderRightsPublicDomain, RDFRights: copyrighteligibility.ProviderRightsPublicDomain, HeaderRights: copyrighteligibility.SourceHeaderRightsPublicDomain}, UK: effective})
	return sourceeligibility.Evaluation{Candidate: candidate, ProviderEvidence: provider, OPDSRights: copyrighteligibility.ProviderRightsPublicDomain, HeaderRights: copyrighteligibility.SourceHeaderRightsPublicDomain, EffectiveUKEvidence: effective, Assessment: assessment, EvaluationDate: date, EvaluatedAt: date.Add(time.Hour)}
}
