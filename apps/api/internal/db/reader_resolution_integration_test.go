package db

import (
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestReaderResolutionIntegration(t *testing.T) {
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

	const (
		accountID = "d4230000-0000-4000-8000-000000000001"
		profileID = "d4230000-0000-4000-8000-000000000002"
		slug      = "reader-release-resolution-integration"
	)
	if _, err := adminDB.Exec(
		`INSERT INTO accounts (id, name) VALUES ($1, 'Reader Resolution Integration')`,
		accountID,
	); err != nil {
		t.Fatalf("insert Reader resolution account: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO profiles (id, account_id, name, reading_level)
		VALUES ($1, $2, 'Growing Reader', 'growing-readers')
	`, profileID, accountID); err != nil {
		t.Fatalf("insert Reader resolution profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM reader_story_edition_overrides WHERE account_id = $1`, accountID)
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = 'reader-release-resolution-integration'`)
		_, _ = adminDB.Exec(`DELETE FROM profiles WHERE account_id = $1`, accountID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	author := "Panda Pages Resolution Fixture"
	language := "en-GB"
	initialTitle := "TEST ONLY - Reader Release Resolution"
	bundle, err := store.AdminEditionBundleUpsert(model.AdminEditionBundleUpsertRequest{
		Slug:     slug,
		Title:    initialTitle,
		Author:   &author,
		Language: &language,
		Editions: []model.AdminEditionBundleInput{
			{EditionKey: model.AdminStoryEditionClassic, Markdown: "# Classic\n\nClassic release body.\n"},
			{EditionKey: model.AdminStoryEditionConfidentReaders, Markdown: "# Confident\n\nConfident release body.\n"},
			{EditionKey: model.AdminStoryEditionGrowingReaders, Markdown: "# Growing\n\nGrowing release body.\n"},
			{EditionKey: model.AdminStoryEditionStoryExplorers, Markdown: "# Explorers\n\nExplorer release body.\n"},
			{EditionKey: model.AdminStoryEditionLittleListeners, Markdown: "# Listeners\n\nListener release body.\n"},
		},
	})
	if err != nil {
		t.Fatalf("ingest Reader resolution editions: %v", err)
	}
	if len(bundle.Results) != len(model.ReaderEditionKeys()) {
		t.Fatalf("bundle results = %d, want %d", len(bundle.Results), len(model.ReaderEditionKeys()))
	}

	versionIDByKey := make(map[model.ReaderEditionKey]string, len(bundle.Results))
	versionByKey := make(map[model.ReaderEditionKey]int, len(bundle.Results))
	for _, result := range bundle.Results {
		versionIDByKey[result.EditionKey] = result.VersionID
		versionByKey[result.EditionKey] = result.Version
	}

	release := func(keys ...model.ReaderEditionKey) {
		t.Helper()
		editions := make([]model.AdminReleaseEditionRequest, 0, len(keys))
		for _, key := range keys {
			editions = append(editions, model.AdminReleaseEditionRequest{
				EditionKey: key,
				VersionID:  versionIDByKey[key],
			})
		}
		if _, err := store.AdminCreateRelease(slug, model.AdminCreateReleaseRequest{
			Editions: editions,
		}); err != nil {
			t.Fatalf("create Reader resolution release %v: %v", keys, err)
		}
	}

	release(
		model.ReaderEditionClassic,
		model.ReaderEditionConfidentReaders,
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
		model.ReaderEditionLittleListeners,
	)

	var storyID string
	if err := adminDB.QueryRow(
		`SELECT id FROM stories WHERE slug = $1`,
		slug,
	).Scan(&storyID); err != nil {
		t.Fatalf("read Reader resolution story id: %v", err)
	}

	assertSelected := func(
		wantEdition model.ReaderEditionKey,
		wantEligible []model.ReaderEditionKey,
	) model.ReaderResolvedStory {
		t.Helper()
		resolution, err := store.ReaderResolve(accountID, profileID, slug)
		if err != nil {
			t.Fatalf("resolve selected %q: %v", wantEdition, err)
		}
		if resolution.State != model.ReaderResolutionSelected ||
			resolution.Story == nil ||
			resolution.Story.EditionKey != wantEdition ||
			!reflect.DeepEqual(resolution.EligibleEditions, wantEligible) {
			t.Fatalf(
				"selected resolution = %#v, want edition %q eligible %#v",
				resolution,
				wantEdition,
				wantEligible,
			)
		}
		if resolution.Story.Slug != slug ||
			resolution.Story.Title != initialTitle ||
			resolution.Story.Language != language ||
			resolution.Story.Version != versionByKey[wantEdition] ||
			len(resolution.Story.Segments) == 0 {
			t.Fatalf("selected Reader story = %#v", resolution.Story)
		}
		return *resolution.Story
	}

	growingEligible := []model.ReaderEditionKey{
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
		model.ReaderEditionLittleListeners,
	}
	assertSelected(model.ReaderEditionGrowingReaders, growingEligible)

	const locatorJSON = `{
		"schema": 2,
		"segment": {
			"key": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"occurrence": 1,
			"ordinal": 1,
			"offset": 0
		}
	}`
	if _, err := adminDB.Exec(`
		INSERT INTO reading_progress (
			account_id,
			profile_id,
			story_id,
			story_version_id,
			locator,
			percent
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, 0.4)
	`, accountID, profileID, storyID, versionIDByKey[model.ReaderEditionStoryExplorers], locatorJSON); err != nil {
		t.Fatalf("insert Reader resolution progress: %v", err)
	}
	assertSelected(model.ReaderEditionStoryExplorers, growingEligible)

	if err := store.ReaderStoryEditionOverridePut(
		accountID,
		profileID,
		slug,
		model.ReaderEditionLittleListeners,
	); err != nil {
		t.Fatalf("store Reader resolution override: %v", err)
	}
	assertSelected(model.ReaderEditionLittleListeners, growingEligible)

	// A newer draft may update mutable story metadata, but Reader resolution
	// must continue to present the immutable selected release version metadata.
	growing := model.AdminStoryEditionGrowingReaders
	if _, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{
		Slug:       slug,
		EditionKey: &growing,
		Title:      "DRAFT METADATA MUST NOT LEAK",
		Author:     &author,
		Language:   &language,
		Markdown:   "# New draft\n\nThis draft is not in the current release.\n",
	}); err != nil {
		t.Fatalf("create post-release draft: %v", err)
	}
	assertSelected(model.ReaderEditionLittleListeners, growingEligible)

	release(
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
	)
	// The stored Listener override is now stale and must be ignored. Exact
	// current-release progress still resolves the Explorer edition.
	assertSelected(
		model.ReaderEditionStoryExplorers,
		[]model.ReaderEditionKey{
			model.ReaderEditionGrowingReaders,
			model.ReaderEditionStoryExplorers,
		},
	)

	if _, err := store.ReaderStoryEditionOverrideClear(accountID, profileID, slug); err != nil {
		t.Fatalf("clear Reader resolution override: %v", err)
	}
	if _, err := adminDB.Exec(`
		UPDATE reading_progress
		SET story_version_id = $4
		WHERE account_id = $1
		  AND profile_id = $2
		  AND story_id = $3
	`, accountID, profileID, storyID, versionIDByKey[model.ReaderEditionLittleListeners]); err != nil {
		t.Fatalf("make Reader resolution progress stale: %v", err)
	}
	assertSelected(model.ReaderEditionGrowingReaders, []model.ReaderEditionKey{
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
	})

	release(
		model.ReaderEditionClassic,
		model.ReaderEditionConfidentReaders,
	)
	if _, err := store.ReaderResolve(accountID, profileID, slug); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("zero-eligible Reader resolution error = %v, want sql.ErrNoRows", err)
	}

	release(model.ReaderEditionLittleListeners)
	assertSelected(
		model.ReaderEditionLittleListeners,
		[]model.ReaderEditionKey{model.ReaderEditionLittleListeners},
	)
}
