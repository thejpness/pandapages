package db

import (
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
)

func TestReaderLibraryIntegration(t *testing.T) {
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
		accountID = "e5250000-0000-4000-8000-000000000001"
		profileID = "e5250000-0000-4000-8000-000000000002"
		slug      = "reader-library-integration"
	)
	if _, err := adminDB.Exec(
		`INSERT INTO accounts (id, name) VALUES ($1, 'Reader Library Integration')`,
		accountID,
	); err != nil {
		t.Fatalf("insert Reader Library account: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO profiles (id, account_id, name, reading_level)
		VALUES ($1, $2, 'Growing Library Reader', 'growing-readers')
	`, profileID, accountID); err != nil {
		t.Fatalf("insert Reader Library profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM reader_story_edition_overrides WHERE account_id = $1`, accountID)
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = 'reader-library-integration'`)
		_, _ = adminDB.Exec(`DELETE FROM profiles WHERE account_id = $1`, accountID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	author := "Panda Pages Library Fixture"
	language := "en-GB"
	title := "TEST ONLY — Profile Library"
	bundle, err := store.AdminEditionBundleUpsert(model.AdminEditionBundleUpsertRequest{
		Slug:     slug,
		Title:    title,
		Author:   &author,
		Language: &language,
		Editions: []model.AdminEditionBundleInput{
			{
				EditionKey: model.AdminStoryEditionClassic,
				Markdown:   "# Classic\n\nA longer Classic library body with several words.\n",
			},
			{
				EditionKey: model.AdminStoryEditionConfidentReaders,
				Markdown:   "# Confident\n\nA confident library body with words.\n",
			},
			{
				EditionKey: model.AdminStoryEditionGrowingReaders,
				Markdown:   "# Growing\n\nA growing library body.\n",
			},
			{
				EditionKey: model.AdminStoryEditionStoryExplorers,
				Markdown:   "# Explorers\n\nExplorer body.\n",
			},
			{
				EditionKey: model.AdminStoryEditionLittleListeners,
				Markdown:   "# Listeners\n\nListen.\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("ingest Reader Library editions: %v", err)
	}

	versionIDByKey := make(map[model.ReaderEditionKey]string, len(bundle.Results))
	versionByKey := make(map[model.ReaderEditionKey]int, len(bundle.Results))
	wordCountByKey := make(map[model.ReaderEditionKey]int, len(bundle.Results))
	chapterCountByKey := make(map[model.ReaderEditionKey]int, len(bundle.Results))
	for _, result := range bundle.Results {
		versionIDByKey[result.EditionKey] = result.VersionID
		versionByKey[result.EditionKey] = result.Version
		wordCountByKey[result.EditionKey] = result.WordCount
		chapterCountByKey[result.EditionKey] = result.ChapterCount
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
			t.Fatalf("create Reader Library release %v: %v", keys, err)
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
		t.Fatalf("read Reader Library story id: %v", err)
	}

	findItem := func(library model.ReaderLibraryReadModel) *model.ReaderLibraryItem {
		t.Helper()
		for index := range library.Items {
			if library.Items[index].Slug == slug {
				return &library.Items[index]
			}
		}
		return nil
	}
	loadItem := func() (model.ReaderLibraryReadModel, *model.ReaderLibraryItem) {
		t.Helper()
		library, err := store.ReaderLibrary(accountID, profileID)
		if err != nil {
			t.Fatalf("ReaderLibrary: %v", err)
		}
		return library, findItem(library)
	}
	assertEligible := func(
		item *model.ReaderLibraryItem,
		want []model.ReaderEditionKey,
	) {
		t.Helper()
		if item == nil {
			t.Fatal("Reader Library item is missing")
		}
		got := make([]model.ReaderEditionKey, 0, len(item.EligibleEditions))
		for _, edition := range item.EligibleEditions {
			got = append(got, edition.EditionKey)
			if edition.Version != versionByKey[edition.EditionKey] ||
				edition.WordCount != int64(wordCountByKey[edition.EditionKey]) ||
				edition.ChapterCount != int64(chapterCountByKey[edition.EditionKey]) {
				t.Fatalf("Reader Library edition summary = %#v", edition)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Reader Library eligible editions = %#v, want %#v", got, want)
		}
		if item.Title != title ||
			item.Author == nil || *item.Author != author ||
			item.Language != language {
			t.Fatalf("Reader Library immutable identity = %#v", item)
		}
	}

	growingEligible := []model.ReaderEditionKey{
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
		model.ReaderEditionLittleListeners,
	}
	library, item := loadItem()
	if library.UnavailableItemCount != 0 ||
		item == nil ||
		item.State != model.ReaderResolutionChooser ||
		item.SelectedEdition != nil ||
		item.Progress != nil {
		t.Fatalf("initial Reader Library chooser = %#v / %#v", library, item)
	}
	assertEligible(item, growingEligible)

	const locatorJSON = `{
		"schema": 2,
		"segment": {
			"key": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"occurrence": 1,
			"ordinal": 1,
			"offset": 0
		}
	}`
	progressTime := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	if _, err := adminDB.Exec(`
		INSERT INTO reading_progress (
			account_id,
			profile_id,
			story_id,
			story_version_id,
			locator,
			percent,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, 0.4, $6)
	`, accountID, profileID, storyID,
		versionIDByKey[model.ReaderEditionStoryExplorers],
		locatorJSON,
		progressTime,
	); err != nil {
		t.Fatalf("insert Reader Library progress: %v", err)
	}

	library, item = loadItem()
	if library.UnavailableItemCount != 0 ||
		item == nil ||
		item.State != model.ReaderResolutionSelected ||
		item.SelectedEdition == nil ||
		*item.SelectedEdition != model.ReaderEditionStoryExplorers ||
		item.Progress == nil ||
		item.Progress.Version != versionByKey[model.ReaderEditionStoryExplorers] ||
		item.Progress.Percent != 0.4 ||
		!item.Progress.UpdatedAt.Equal(progressTime) ||
		!item.Progress.IsResolvedVersion {
		t.Fatalf("progress-selected Reader Library = %#v / %#v", library, item)
	}
	assertEligible(item, growingEligible)

	if err := store.ReaderStoryEditionOverridePut(
		accountID,
		profileID,
		slug,
		model.ReaderEditionLittleListeners,
	); err != nil {
		t.Fatalf("store Reader Library override: %v", err)
	}
	_, item = loadItem()
	if item == nil ||
		item.SelectedEdition == nil ||
		*item.SelectedEdition != model.ReaderEditionLittleListeners ||
		item.Progress == nil ||
		item.Progress.IsResolvedVersion {
		t.Fatalf("override-selected Reader Library = %#v", item)
	}

	release(
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
	)
	_, item = loadItem()
	if item == nil ||
		item.SelectedEdition == nil ||
		*item.SelectedEdition != model.ReaderEditionStoryExplorers ||
		item.Progress == nil ||
		!item.Progress.IsResolvedVersion {
		t.Fatalf("stale-override Reader Library = %#v", item)
	}
	assertEligible(item, []model.ReaderEditionKey{
		model.ReaderEditionGrowingReaders,
		model.ReaderEditionStoryExplorers,
	})

	if _, err := store.ReaderStoryEditionOverrideClear(accountID, profileID, slug); err != nil {
		t.Fatalf("clear Reader Library override: %v", err)
	}
	if _, err := adminDB.Exec(`
		UPDATE reading_progress
		SET story_version_id = $4
		WHERE account_id = $1
		  AND profile_id = $2
		  AND story_id = $3
	`, accountID, profileID, storyID,
		versionIDByKey[model.ReaderEditionLittleListeners],
	); err != nil {
		t.Fatalf("make Reader Library progress stale: %v", err)
	}
	_, item = loadItem()
	if item == nil ||
		item.State != model.ReaderResolutionChooser ||
		item.SelectedEdition != nil ||
		item.Progress == nil ||
		item.Progress.Version != versionByKey[model.ReaderEditionLittleListeners] ||
		item.Progress.IsResolvedVersion {
		t.Fatalf("stale-progress Reader Library chooser = %#v", item)
	}

	release(
		model.ReaderEditionClassic,
		model.ReaderEditionConfidentReaders,
	)
	library, item = loadItem()
	if item != nil || library.UnavailableItemCount != 0 {
		t.Fatalf("zero-eligible Reader Library = %#v / %#v", library, item)
	}

	if _, err := adminDB.Exec(`
		UPDATE reading_progress
		SET story_version_id = $4
		WHERE account_id = $1
		  AND profile_id = $2
		  AND story_id = $3
	`, accountID, profileID, storyID,
		versionIDByKey[model.ReaderEditionStoryExplorers],
	); err != nil {
		t.Fatalf("make sole-edition progress stale: %v", err)
	}
	release(model.ReaderEditionLittleListeners)
	_, item = loadItem()
	if item == nil ||
		item.State != model.ReaderResolutionSelected ||
		item.SelectedEdition == nil ||
		*item.SelectedEdition != model.ReaderEditionLittleListeners ||
		item.Progress == nil ||
		item.Progress.IsResolvedVersion {
		t.Fatalf("sole-eligible Reader Library = %#v", item)
	}

	// Corrupt content in an inaccessible edition must not poison the profile's
	// Library item. Corrupt content in an eligible edition must quarantine the
	// story without exposing its metadata.
	release(
		model.ReaderEditionClassic,
		model.ReaderEditionLittleListeners,
	)
	if _, err := adminDB.Exec(`
		UPDATE story_versions
		SET frontmatter = '{}'::jsonb
		WHERE id = $1
	`, versionIDByKey[model.ReaderEditionClassic]); err != nil {
		t.Fatalf("corrupt disallowed Classic edition: %v", err)
	}
	library, item = loadItem()
	if item == nil ||
		library.UnavailableItemCount != 0 ||
		item.SelectedEdition == nil ||
		*item.SelectedEdition != model.ReaderEditionLittleListeners {
		t.Fatalf("disallowed corrupt edition poisoned Reader Library = %#v / %#v", library, item)
	}

	if _, err := adminDB.Exec(`
		UPDATE story_versions
		SET frontmatter = '{}'::jsonb
		WHERE id = $1
	`, versionIDByKey[model.ReaderEditionLittleListeners]); err != nil {
		t.Fatalf("corrupt eligible Listener edition: %v", err)
	}
	library, item = loadItem()
	if item != nil || library.UnavailableItemCount != 1 {
		t.Fatalf("eligible corrupt edition was not quarantined = %#v / %#v", library, item)
	}

	// Two independently valid eligible editions with different immutable story
	// identity metadata cannot be represented by one Library card without
	// inventing a representative edition. Quarantine that story instead.
	const mismatchSlug = "reader-library-metadata-mismatch"
	growingKey := model.AdminStoryEditionGrowingReaders
	mismatchGrowing, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{
		Slug:       mismatchSlug,
		EditionKey: &growingKey,
		Title:      "Mismatch title A",
		Author:     &author,
		Language:   &language,
		Markdown:   "# Mismatch A\n\nGrowing body.\n",
	})
	if err != nil {
		t.Fatalf("create metadata-mismatch Growing edition: %v", err)
	}
	explorerKey := model.AdminStoryEditionStoryExplorers
	mismatchExplorer, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{
		Slug:       mismatchSlug,
		EditionKey: &explorerKey,
		Title:      "Mismatch title B",
		Author:     &author,
		Language:   &language,
		Markdown:   "# Mismatch B\n\nExplorer body.\n",
	})
	if err != nil {
		t.Fatalf("create metadata-mismatch Explorer edition: %v", err)
	}
	if _, err := store.AdminCreateRelease(mismatchSlug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: growingKey, VersionID: mismatchGrowing.VersionID},
			{EditionKey: explorerKey, VersionID: mismatchExplorer.VersionID},
		},
	}); err != nil {
		t.Fatalf("release metadata-mismatch editions: %v", err)
	}
	library, err = store.ReaderLibrary(accountID, profileID)
	if err != nil {
		t.Fatalf("ReaderLibrary with metadata mismatch: %v", err)
	}
	if findItem(library) != nil || library.UnavailableItemCount != 2 {
		t.Fatalf("metadata mismatch was not quarantined with corrupt eligible item: %#v", library)
	}
	for _, libraryItem := range library.Items {
		if libraryItem.Slug == mismatchSlug {
			t.Fatalf("metadata-mismatch story leaked into Reader Library: %#v", libraryItem)
		}
	}

	if _, err := store.ReaderLibrary(accountID, "ffffffff-ffff-4fff-8fff-ffffffffffff"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing Reader Library profile error = %v, want sql.ErrNoRows", err)
	}
}
