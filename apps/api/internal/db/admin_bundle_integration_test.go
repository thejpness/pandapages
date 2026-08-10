package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestAdminEditionBundleIntegration(t *testing.T) {
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
		t.Fatalf("refusing bundle setup in database %q; want %q", databaseName, readerIntegrationDBName)
	}

	const accountID = "d2140000-0000-4000-8000-000000000001"
	const slug = "five-edition-bundle-integration"
	if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Five Edition Bundle Integration') ON CONFLICT (id) DO NOTHING`, accountID); err != nil {
		t.Fatalf("insert bundle account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = $1`, slug)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	author := "Panda Pages Bundle Fixture"
	language := "en-GB"
	request := model.AdminEditionBundleUpsertRequest{
		Slug: slug, Title: "Five Edition Bundle", Author: &author, Language: &language,
		Rights: map[string]any{"label": "Public domain"},
		Editions: []model.AdminEditionBundleInput{
			{EditionKey: model.AdminStoryEditionClassic, Markdown: "# Five Edition Bundle\n\nClassic body.\n"},
			{EditionKey: model.AdminStoryEditionConfidentReaders, Markdown: "# Five Edition Bundle\n\nConfident Readers body.\n"},
			{EditionKey: model.AdminStoryEditionGrowingReaders, Markdown: "# Five Edition Bundle\n\nGrowing Readers body.\n"},
			{EditionKey: model.AdminStoryEditionStoryExplorers, Markdown: "# Five Edition Bundle\n\nStory Explorers body.\n"},
			{EditionKey: model.AdminStoryEditionLittleListeners, Markdown: "# Five Edition Bundle\n\nLittle Listeners body.\n"},
		},
	}

	first, err := store.AdminEditionBundleUpsert(accountID, request)
	if err != nil {
		t.Fatalf("initial five-edition ingest: %v", err)
	}
	if first.Slug != slug || len(first.Results) != 5 {
		t.Fatalf("initial bundle response = %#v", first)
	}
	for index, key := range model.AdminStoryEditionKeys() {
		result := first.Results[index]
		if result.EditionKey != key || result.Version != index+1 || result.Outcome != model.AdminEditionIngestOutcomeCreated {
			t.Fatalf("initial result %d = %#v", index, result)
		}
	}

	var storyID, classicDraftID string
	var currentReleaseID sql.NullString
	var editionCount, versionCount int
	if err := adminDB.QueryRow(`SELECT id, current_release_id FROM stories WHERE slug = $1`, slug).Scan(&storyID, &currentReleaseID); err != nil {
		t.Fatalf("read bundle story: %v", err)
	}
	if err := adminDB.QueryRow(`
		SELECT draft_version_id
		FROM story_editions
		WHERE story_id = $1 AND edition_key = 'classic'
	`, storyID).Scan(&classicDraftID); err != nil {
		t.Fatalf("read Classic draft pointer: %v", err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_editions WHERE story_id = $1`, storyID).Scan(&editionCount); err != nil {
		t.Fatalf("count bundle editions: %v", err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, storyID).Scan(&versionCount); err != nil {
		t.Fatalf("count bundle versions: %v", err)
	}
	if editionCount != 5 || versionCount != 5 || classicDraftID != first.Results[0].VersionID || currentReleaseID.Valid {
		t.Fatalf("bundle state editions/versions/classic-draft/current-release = %d/%d/%q/%v", editionCount, versionCount, classicDraftID, currentReleaseID)
	}

	reused, err := store.AdminEditionBundleUpsert(accountID, request)
	if err != nil {
		t.Fatalf("idempotent five-edition ingest: %v", err)
	}
	for index, result := range reused.Results {
		if result.Outcome != model.AdminEditionIngestOutcomeReused || result.VersionID != first.Results[index].VersionID || result.Version != first.Results[index].Version {
			t.Fatalf("reused result %d = %#v", index, result)
		}
	}

	invalid := request
	invalid.Editions = append([]model.AdminEditionBundleInput(nil), request.Editions...)
	invalid.Editions[0].Markdown = "# Five Edition Bundle\n\nChanged Classic body.\n"
	invalid.Editions[4].Markdown = "   "
	if _, err := store.AdminEditionBundleUpsert(accountID, invalid); err == nil {
		t.Fatal("partially invalid bundle unexpectedly succeeded")
	} else {
		var validationErr *model.AdminValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("partially invalid bundle error = %v, want validation error", err)
		}
		found := false
		for _, issue := range validationErr.Issues {
			if issue.Field == "editions.little-listeners.markdown" {
				found = true
			}
		}
		if !found {
			t.Fatalf("bundle validation issues = %#v", validationErr.Issues)
		}
	}
	var afterFailureCount int
	var afterFailureDraft string
	if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, storyID).Scan(&afterFailureCount); err != nil {
		t.Fatalf("count versions after rollback: %v", err)
	}
	if err := adminDB.QueryRow(`
		SELECT draft_version_id
		FROM story_editions
		WHERE story_id = $1 AND edition_key = 'classic'
	`, storyID).Scan(&afterFailureDraft); err != nil {
		t.Fatalf("read Classic draft after rollback: %v", err)
	}
	if afterFailureCount != 5 || afterFailureDraft != first.Results[0].VersionID {
		t.Fatalf("failed bundle was not atomic: versions=%d draft=%q", afterFailureCount, afterFailureDraft)
	}
}
