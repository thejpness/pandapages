package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestAdminReleaseIntegration(t *testing.T) {
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

	const accountID = "d6140000-0000-4000-8000-000000000001"
	const profileID = "d6140000-0000-4000-8000-000000000002"
	const slug = "partial-release-integration"
	if _, err := adminDB.Exec(`
		INSERT INTO accounts (id, name)
		VALUES ($1, 'Partial Release Integration')
		ON CONFLICT (id) DO NOTHING
	`, accountID); err != nil {
		t.Fatalf("insert release account: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO profiles (id, account_id, name, reading_level)
		VALUES ($1, $2, 'Partial Release Listener', 'little-listeners')
	`, profileID, accountID); err != nil {
		t.Fatalf("insert release profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE account_id = $1 AND slug = $2`, accountID, slug)
		_, _ = adminDB.Exec(`DELETE FROM profiles WHERE id = $1 AND account_id = $2`, profileID, accountID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	language := "en-GB"
	classic := model.AdminStoryEditionClassic
	growing := model.AdminStoryEditionGrowingReaders
	listeners := model.AdminStoryEditionLittleListeners

	classicDraft, err := store.AdminDraftUpsert(accountID, model.AdminDraftUpsertRequest{
		Slug: slug, EditionKey: &classic, Title: "Partial Release", Language: &language,
		Markdown: "# Partial Release\n\nClassic body.\n",
	})
	if err != nil {
		t.Fatalf("create Classic draft: %v", err)
	}
	growingDraft, err := store.AdminDraftUpsert(accountID, model.AdminDraftUpsertRequest{
		Slug: slug, EditionKey: &growing, Title: "Partial Release", Language: &language,
		Markdown: "# Partial Release\n\nGrowing body.\n",
	})
	if err != nil {
		t.Fatalf("create Growing Readers draft: %v", err)
	}
	listenerDraft, err := store.AdminDraftUpsert(accountID, model.AdminDraftUpsertRequest{
		Slug: slug, EditionKey: &listeners, Title: "Partial Release", Language: &language,
		Markdown: "# Partial Release\n\nLittle Listeners body.\n",
	})
	if err != nil {
		t.Fatalf("create Little Listeners draft: %v", err)
	}

	first, err := store.AdminCreateRelease(accountID, slug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{{
			EditionKey: growing,
			VersionID:  growingDraft.VersionID,
		}},
	})
	if err != nil {
		t.Fatalf("publish Growing-only release: %v", err)
	}
	if first.Outcome != model.AdminReleaseOutcomeCreated ||
		first.Release.Release != 1 ||
		len(first.Release.Editions) != 1 ||
		first.Release.Editions[0].EditionKey != growing {
		t.Fatalf("first partial release = %#v", first)
	}

	var (
		storyID          string
		currentReleaseID string
	)
	if err := adminDB.QueryRow(`
		SELECT id, current_release_id
		FROM stories
		WHERE account_id = $1 AND slug = $2
	`, accountID, slug).Scan(&storyID, &currentReleaseID); err != nil {
		t.Fatalf("read first release state: %v", err)
	}
	if currentReleaseID == "" {
		t.Fatal("Growing-only release did not become current")
	}
	if _, err := store.ReaderStory(accountID, slug); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Growing-only release became Reader-visible in Lifecycle 6: %v", err)
	}
	library, err := store.ReaderLibrary(accountID, profileID)
	if err != nil {
		t.Fatalf("load profile Library after Growing-only release: %v", err)
	}
	for _, item := range library.Items {
		if item.Slug == slug {
			t.Fatalf("Growing-only release became visible to Little Listeners profile: %#v", item)
		}
	}

	var liveManifest string
	if err := adminDB.QueryRow(`
		SELECT string_agg(edition.edition_key || ':' || member.story_version_id::text, ',' ORDER BY edition.edition_key)
		FROM story_release_editions AS member
		JOIN story_editions AS edition ON edition.id = member.edition_id
		WHERE member.release_id = $1 AND member.story_id = $2
	`, currentReleaseID, storyID).Scan(&liveManifest); err != nil {
		t.Fatalf("read first release manifest: %v", err)
	}
	if liveManifest != "growing-readers:"+growingDraft.VersionID {
		t.Fatalf("Growing-only release manifest = %s", liveManifest)
	}

	second, err := store.AdminCreateRelease(accountID, slug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: listeners, VersionID: listenerDraft.VersionID},
			{EditionKey: classic, VersionID: classicDraft.VersionID},
		},
	})
	if err != nil {
		t.Fatalf("publish Classic + Little Listeners release: %v", err)
	}
	if second.Release.Release != 2 ||
		len(second.Release.Editions) != 2 ||
		second.Release.Editions[0].EditionKey != classic ||
		second.Release.Editions[1].EditionKey != listeners {
		t.Fatalf("second partial release = %#v", second)
	}

	if err := adminDB.QueryRow(`
		SELECT current_release_id
		FROM stories
		WHERE id = $1
	`, storyID).Scan(&currentReleaseID); err != nil {
		t.Fatalf("read second release state: %v", err)
	}
	var classicLiveVersion string
	if err := adminDB.QueryRow(`
		SELECT member.story_version_id
		FROM story_release_editions AS member
		JOIN story_editions AS edition ON edition.id = member.edition_id
		WHERE member.release_id = $1
		  AND member.story_id = $2
		  AND edition.edition_key = 'classic'
	`, currentReleaseID, storyID).Scan(&classicLiveVersion); err != nil {
		t.Fatalf("read Classic member from current release: %v", err)
	}
	if classicLiveVersion != classicDraft.VersionID {
		t.Fatalf("Classic current-release version = %q, want %q", classicLiveVersion, classicDraft.VersionID)
	}
	library, err = store.ReaderLibrary(accountID, profileID)
	if err != nil {
		t.Fatalf("load profile Library after Classic + Listener release: %v", err)
	}
	var releasedItem *model.ReaderLibraryItem
	for index := range library.Items {
		if library.Items[index].Slug == slug {
			releasedItem = &library.Items[index]
			break
		}
	}
	if releasedItem == nil || releasedItem.State != model.ReaderResolutionSelected ||
		releasedItem.SelectedEdition == nil ||
		*releasedItem.SelectedEdition != model.ReaderEditionLittleListeners {
		t.Fatalf("Little Listeners profile release resolution = %#v", releasedItem)
	}
	readerStory, err := store.ReaderStory(accountID, slug)
	if err != nil {
		t.Fatalf("Classic release was not Reader-visible: %v", err)
	}
	if readerStory.Version != classicDraft.Version {
		t.Fatalf("Reader compatibility followed version %d, want Classic %d", readerStory.Version, classicDraft.Version)
	}

	reused, err := store.AdminCreateRelease(accountID, slug, model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: classic, VersionID: classicDraft.VersionID},
			{EditionKey: listeners, VersionID: listenerDraft.VersionID},
		},
	})
	if err != nil {
		t.Fatalf("reuse current release: %v", err)
	}
	if reused.Outcome != model.AdminReleaseOutcomeReusedCurrent ||
		reused.Release.Release != 2 {
		t.Fatalf("current release reuse = %#v", reused)
	}
	var releaseCount int
	if err := adminDB.QueryRow(`
		SELECT count(*) FROM story_releases WHERE story_id = $1
	`, storyID).Scan(&releaseCount); err != nil {
		t.Fatalf("count releases: %v", err)
	}
	if releaseCount != 2 {
		t.Fatalf("release count after reuse = %d, want 2", releaseCount)
	}

	invalid := model.AdminCreateReleaseRequest{
		Editions: []model.AdminReleaseEditionRequest{
			{EditionKey: classic, VersionID: classicDraft.VersionID},
			{EditionKey: growing, VersionID: listenerDraft.VersionID},
		},
	}
	if _, err := store.AdminCreateRelease(accountID, slug, invalid); !errors.Is(err, model.ErrAdminReleaseNotFound) {
		t.Fatalf("cross-edition release error = %v, want not found", err)
	}
	var afterInvalidRelease string
	if err := adminDB.QueryRow(`
		SELECT current_release_id FROM stories WHERE id = $1
	`, storyID).Scan(&afterInvalidRelease); err != nil {
		t.Fatalf("read current release after invalid request: %v", err)
	}
	if afterInvalidRelease != currentReleaseID {
		t.Fatalf("invalid release changed current pointer: %s -> %s", currentReleaseID, afterInvalidRelease)
	}

	status, err := store.AdminUnpublish(accountID, slug)
	if err != nil {
		t.Fatalf("unpublish current release: %v", err)
	}
	if status.CurrentRelease != nil || status.Status != model.AdminStoryStatusDraftOnly {
		t.Fatalf("unpublished release status = %#v", status)
	}
	var (
		currentAfter sql.NullString
		historyCount int
	)
	if err := adminDB.QueryRow(`
		SELECT current_release_id
		FROM stories
		WHERE id = $1
	`, storyID).Scan(&currentAfter); err != nil {
		t.Fatalf("read unpublish story state: %v", err)
	}
	if err := adminDB.QueryRow(`
		SELECT count(*) FROM story_releases WHERE story_id = $1
	`, storyID).Scan(&historyCount); err != nil {
		t.Fatalf("count retained release history: %v", err)
	}
	if currentAfter.Valid || historyCount != 2 {
		t.Fatalf("unpublish state: current=%v history=%d", currentAfter, historyCount)
	}
}
