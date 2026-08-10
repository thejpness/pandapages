package db

import (
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"pandapages/api/internal/model"
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
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	if databaseName != readerIntegrationDBName {
		t.Fatalf("refusing acquisition test in database %q; want %q", databaseName, readerIntegrationDBName)
	}

	store := newReaderIntegrationStore(t, databaseURL)
	candidate := testSourceAcquisitionCandidate()
	first, err := store.AdminPersistSourceAcquisition(candidate)
	if err != nil {
		t.Fatalf("persist first acquisition: %v", err)
	}
	if first.Outcome != model.AdminSourceAcquisitionOutcomeCreated ||
		first.Acquisition.Review.Rights.Status != model.AdminSourceAcquisitionReviewPending ||
		first.Acquisition.Review.Editorial.Status != model.AdminSourceAcquisitionReviewPending {
		t.Fatalf("first acquisition = %#v", first)
	}
	if first.Acquisition.ProviderRights == nil || *first.Acquisition.ProviderRights != candidate.ProviderRights {
		t.Fatalf("provider rights = %#v, want evidence only", first.Acquisition.ProviderRights)
	}
	assertRejected := func(name, statement string, args ...any) {
		t.Helper()
		if _, err := adminDB.Exec(statement, args...); err == nil {
			t.Fatalf("database accepted %s", name)
		}
	}
	assertRejected("invalid acquisition hash", `UPDATE source_acquisitions SET snapshot_hash = 'not-a-sha256' WHERE id = $1`, first.Acquisition.ID)
	assertRejected("approved rights without rationale", `UPDATE source_acquisition_reviews SET rights_status = 'approved', rights_note = NULL, rights_reviewed_at = now() WHERE acquisition_id = $1`, first.Acquisition.ID)
	var initialReviewCount int
	if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_reviews WHERE acquisition_id = $1`, first.Acquisition.ID).Scan(&initialReviewCount); err != nil || initialReviewCount != 1 {
		t.Fatalf("initial review row count = %d / %v", initialReviewCount, err)
	}

	detail, err := store.AdminGetSourceAcquisition(first.Acquisition.ID)
	if err != nil || detail.SourceText != candidate.SourceText || detail.SnapshotHash != first.Acquisition.SnapshotHash {
		t.Fatalf("acquisition detail = %#v / %v", detail, err)
	}

	rights, err := store.AdminUpdateSourceAcquisitionRightsReview(first.Acquisition.ID, model.AdminSourceAcquisitionReviewUpdateRequest{
		Status: model.AdminSourceAcquisitionReviewApproved,
		Note:   "Reviewed for Panda Pages rights use.",
	})
	if err != nil || rights.Review.Rights.Status != model.AdminSourceAcquisitionReviewApproved || rights.Review.Rights.Note == nil || rights.Review.Rights.ReviewedAt == nil || rights.Review.Editorial.Status != model.AdminSourceAcquisitionReviewPending {
		t.Fatalf("rights review = %#v / %v", rights, err)
	}

	editorial, err := store.AdminUpdateSourceAcquisitionEditorialReview(first.Acquisition.ID, model.AdminSourceAcquisitionReviewUpdateRequest{
		Status: model.AdminSourceAcquisitionReviewRejected,
		Note:   "Needs editorial source-quality review.",
	})
	if err != nil || !reflect.DeepEqual(editorial.Review.Rights, rights.Review.Rights) || editorial.Review.Editorial.Status != model.AdminSourceAcquisitionReviewRejected || editorial.Review.Editorial.Note == nil || editorial.Review.Editorial.ReviewedAt == nil {
		t.Fatalf("editorial review = %#v / %v", editorial, err)
	}

	reused, err := store.AdminPersistSourceAcquisition(candidate)
	if err != nil || reused.Outcome != model.AdminSourceAcquisitionOutcomeReused || reused.Acquisition.ID != first.Acquisition.ID || !reflect.DeepEqual(reused.Acquisition.Review, editorial.Review) {
		t.Fatalf("reused acquisition = %#v / %v", reused, err)
	}

	for _, test := range []struct {
		name   string
		mutate func(sourceprovider.SourceCandidate) sourceprovider.SourceCandidate
	}{
		{"source", func(changed sourceprovider.SourceCandidate) sourceprovider.SourceCandidate {
			changed.SourceText = "A changed source text.\n"
			changed.NormalisedContentHash = sourceAcquisitionSHA256(changed.SourceText)
			return changed
		}},
		{"provider rights", func(changed sourceprovider.SourceCandidate) sourceprovider.SourceCandidate {
			changed.ProviderRights = "A changed provider statement."
			return changed
		}},
		{"representation", func(changed sourceprovider.SourceCandidate) sourceprovider.SourceCandidate {
			changed.SelectedRepresentation.URL = "https://www.gutenberg.org/files/11/11.txt"
			return changed
		}},
		{"retrieved hash", func(changed sourceprovider.SourceCandidate) sourceprovider.SourceCandidate {
			changed.RetrievedContentHash = strings.Repeat("b", 64)
			return changed
		}},
	} {
		t.Run("changed "+test.name+" creates a distinct immutable snapshot", func(t *testing.T) {
			created, err := store.AdminPersistSourceAcquisition(test.mutate(candidate))
			if err != nil || created.Outcome != model.AdminSourceAcquisitionOutcomeCreated || created.Acquisition.ID == first.Acquisition.ID {
				t.Fatalf("changed %s acquisition = %#v / %v", test.name, created, err)
			}
		})
	}

	if _, err := store.AdminUpdateSourceAcquisitionRightsReview(first.Acquisition.ID, model.AdminSourceAcquisitionReviewUpdateRequest{Status: model.AdminSourceAcquisitionReviewPending}); err != nil {
		t.Fatalf("reset rights review to pending: %v", err)
	}
	reset, err := store.AdminGetSourceAcquisition(first.Acquisition.ID)
	if err != nil || reset.Review.Rights.Status != model.AdminSourceAcquisitionReviewPending || reset.Review.Rights.Note != nil || reset.Review.Rights.ReviewedAt != nil || !reflect.DeepEqual(reset.Review.Editorial, editorial.Review.Editorial) {
		t.Fatalf("reset review = %#v / %v", reset.Review, err)
	}

	for _, request := range []model.AdminSourceAcquisitionReviewUpdateRequest{
		{Status: model.AdminSourceAcquisitionReviewApproved},
		{Status: "not-a-status", Note: "Invalid"},
		{Status: model.AdminSourceAcquisitionReviewRejected, Note: strings.Repeat("a", 4001)},
	} {
		if _, err := store.AdminUpdateSourceAcquisitionEditorialReview(first.Acquisition.ID, request); err == nil {
			t.Fatalf("invalid editorial review unexpectedly succeeded: %#v", request)
		}
	}
	if _, err := store.AdminGetSourceAcquisition("11111111-1111-4111-8111-111111111112"); !errors.Is(err, model.ErrAdminSourceAcquisitionNotFound) {
		t.Fatalf("unknown acquisition error = %v", err)
	}

	concurrent := candidate
	concurrent.ExternalID = "12"
	stores := []*Store{newReaderIntegrationStore(t, databaseURL), newReaderIntegrationStore(t, databaseURL)}
	results := make(chan model.AdminSourceAcquisitionPersistResponse, len(stores))
	errorsOut := make(chan error, len(stores))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, concurrentStore := range stores {
		wait.Add(1)
		go func(concurrentStore *Store) {
			defer wait.Done()
			<-start
			result, err := concurrentStore.AdminPersistSourceAcquisition(concurrent)
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
		t.Fatalf("concurrent acquisition results = %#v", concurrentResults)
	}
	concurrentInput, err := adminSourceAcquisitionInput(concurrent)
	if err != nil {
		t.Fatalf("concurrent input: %v", err)
	}
	var acquisitionCount, reviewCount int
	if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisitions WHERE snapshot_hash = $1`, concurrentInput.SnapshotHash).Scan(&acquisitionCount); err != nil {
		t.Fatalf("count concurrent acquisitions: %v", err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM source_acquisition_reviews AS review JOIN source_acquisitions AS acquisition ON acquisition.id = review.acquisition_id WHERE acquisition.snapshot_hash = $1`, concurrentInput.SnapshotHash).Scan(&reviewCount); err != nil {
		t.Fatalf("count concurrent reviews: %v", err)
	}
	if acquisitionCount != 1 || reviewCount != 1 {
		t.Fatalf("concurrent rows = acquisitions %d reviews %d", acquisitionCount, reviewCount)
	}

	list, err := store.AdminListSourceAcquisitions(100)
	if err != nil || len(list.Items) < 6 {
		t.Fatalf("acquisition list = %#v / %v", list, err)
	}
	for index := 1; index < len(list.Items); index++ {
		previous, current := list.Items[index-1], list.Items[index]
		if previous.CreatedAt < current.CreatedAt || (previous.CreatedAt == current.CreatedAt && previous.ID < current.ID) {
			t.Fatalf("acquisition list is not newest-first: %#v", list.Items)
		}
	}
}
