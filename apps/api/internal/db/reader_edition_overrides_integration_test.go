package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestReaderEditionOverrideIntegration(t *testing.T) {
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
		accountID        = "d2720000-0000-4000-8000-000000000001"
		profileID        = "d2720000-0000-4000-8000-000000000002"
		foreignAccountID = "d2720000-0000-4000-8000-000000000011"
		foreignProfileID = "d2720000-0000-4000-8000-000000000012"
		slug             = "reader-edition-override-integration"
	)
	if _, err := adminDB.Exec(`
		INSERT INTO accounts (id, name) VALUES
		  ($1, 'Reader Edition Override Integration'),
		  ($2, 'Reader Edition Override Foreign')
	`, accountID, foreignAccountID); err != nil {
		t.Fatalf("insert override accounts: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO profiles (id, account_id, name, reading_level) VALUES
		  ($1, $2, 'Growing Reader', 'growing-readers'),
		  ($3, $4, 'Foreign Reader', 'classic')
	`, profileID, accountID, foreignProfileID, foreignAccountID); err != nil {
		t.Fatalf("insert override profiles: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM reader_story_edition_overrides WHERE account_id IN ($1, $2)`, accountID, foreignAccountID)
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE account_id IN ($1, $2)`, accountID, foreignAccountID)
		_, _ = adminDB.Exec(`DELETE FROM profiles WHERE account_id IN ($1, $2)`, accountID, foreignAccountID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id IN ($1, $2)`, accountID, foreignAccountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	author := "Panda Pages Reader Resolution Fixture"
	language := "en-GB"
	bundle, err := store.AdminEditionBundleUpsert(accountID, model.AdminEditionBundleUpsertRequest{
		Slug: slug, Title: "Reader Edition Override", Author: &author, Language: &language,
		Editions: []model.AdminEditionBundleInput{
			{EditionKey: model.AdminStoryEditionClassic, Markdown: "# Reader Edition Override\n\nClassic body.\n"},
			{EditionKey: model.AdminStoryEditionConfidentReaders, Markdown: "# Reader Edition Override\n\nConfident body.\n"},
			{EditionKey: model.AdminStoryEditionGrowingReaders, Markdown: "# Reader Edition Override\n\nGrowing body.\n"},
			{EditionKey: model.AdminStoryEditionStoryExplorers, Markdown: "# Reader Edition Override\n\nExplorer body.\n"},
			{EditionKey: model.AdminStoryEditionLittleListeners, Markdown: "# Reader Edition Override\n\nListener body.\n"},
		},
	})
	if err != nil {
		t.Fatalf("ingest override editions: %v", err)
	}
	versionByKey := make(map[model.ReaderEditionKey]string, len(bundle.Results))
	for _, result := range bundle.Results {
		versionByKey[result.EditionKey] = result.VersionID
	}

	_, err = store.AdminCreateRelease(accountID, slug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: model.AdminStoryEditionGrowingReaders, VersionID: versionByKey[model.ReaderEditionGrowingReaders]},
			{EditionKey: model.AdminStoryEditionStoryExplorers, VersionID: versionByKey[model.ReaderEditionStoryExplorers]},
			{EditionKey: model.AdminStoryEditionLittleListeners, VersionID: versionByKey[model.ReaderEditionLittleListeners]},
		},
	})
	if err != nil {
		t.Fatalf("create initial override release: %v", err)
	}

	if override, err := store.ReaderStoryEditionOverrideGet(accountID, profileID, slug); err != nil || override != nil {
		t.Fatalf("initial override = %#v / %v, want nil", override, err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionClassic); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disallowed Classic override error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionConfidentReaders); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disallowed Confident override error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionStoryExplorers); err != nil {
		t.Fatalf("store eligible override: %v", err)
	}
	override, err := store.ReaderStoryEditionOverrideGet(accountID, profileID, slug)
	if err != nil || override == nil || *override != model.ReaderEditionStoryExplorers {
		t.Fatalf("stored override = %#v / %v", override, err)
	}

	_, err = store.AdminCreateRelease(accountID, slug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: model.AdminStoryEditionGrowingReaders, VersionID: versionByKey[model.ReaderEditionGrowingReaders]},
			{EditionKey: model.AdminStoryEditionLittleListeners, VersionID: versionByKey[model.ReaderEditionLittleListeners]},
		},
	})
	if err != nil {
		t.Fatalf("create replacement override release: %v", err)
	}

	override, err = store.ReaderStoryEditionOverrideGet(accountID, profileID, slug)
	if err != nil || override == nil || *override != model.ReaderEditionStoryExplorers {
		t.Fatalf("stale stored override = %#v / %v", override, err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionStoryExplorers); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("non-current Explorer override error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionLittleListeners); err != nil {
		t.Fatalf("replace override with current Listener: %v", err)
	}

	if _, err := adminDB.Exec(`
		UPDATE profiles
		SET reading_level = 'little-listeners'
		WHERE account_id = $1 AND id = $2
	`, accountID, profileID); err != nil {
		t.Fatalf("lower profile reading level: %v", err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, profileID, slug, model.ReaderEditionGrowingReaders); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("now-disallowed Growing override error = %v, want sql.ErrNoRows", err)
	}
	if _, err := store.ReaderStoryEditionOverrideGet(accountID, foreignProfileID, slug); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-account override read error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ReaderStoryEditionOverridePut(accountID, foreignProfileID, slug, model.ReaderEditionLittleListeners); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-account override write error = %v, want sql.ErrNoRows", err)
	}

	removed, err := store.ReaderStoryEditionOverrideClear(accountID, profileID, slug)
	if err != nil || !removed {
		t.Fatalf("clear stored override = %v / %v", removed, err)
	}
	removed, err = store.ReaderStoryEditionOverrideClear(accountID, profileID, slug)
	if err != nil || removed {
		t.Fatalf("idempotent clear = %v / %v", removed, err)
	}
	if override, err := store.ReaderStoryEditionOverrideGet(accountID, profileID, slug); err != nil || override != nil {
		t.Fatalf("override after clear = %#v / %v", override, err)
	}

	var storyID string
	if err := adminDB.QueryRow(`SELECT id FROM stories WHERE account_id = $1 AND slug = $2`, accountID, slug).Scan(&storyID); err != nil {
		t.Fatalf("read override story id: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO reader_story_edition_overrides (account_id, profile_id, story_id, edition_key)
		VALUES ($1, $2, $3, 'not-a-reader-edition')
	`, accountID, profileID, storyID); err == nil {
		t.Fatal("database accepted an invalid override edition key")
	}
	if _, err := adminDB.Exec(`
		INSERT INTO reader_story_edition_overrides (account_id, profile_id, story_id, edition_key)
		VALUES ($1, $2, $3, 'little-listeners')
	`, accountID, foreignProfileID, storyID); err == nil {
		t.Fatal("database accepted a cross-account override profile")
	}
}
