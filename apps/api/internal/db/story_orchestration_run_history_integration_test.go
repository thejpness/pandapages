package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

func TestStoryOrchestrationRunHistoryIntegration(t *testing.T) {
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
		t.Fatalf("refusing orchestration history database %q: %v", databaseName, err)
	}

	const primarySlug = "story-orchestration-run-history"
	const otherSlug = "story-orchestration-run-history-other"
	t.Cleanup(func() {
		for _, slug := range []string{primarySlug, otherSlug} {
			_, _ = adminDB.Exec(`
				DELETE FROM story_orchestration_runs
				WHERE source_version_id IN (
					SELECT version.id
					FROM story_source_versions AS version
					JOIN stories AS story ON story.id = version.story_id
					WHERE story.slug = $1
				)
			`, slug)
			_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = $1`, slug)
		}
	})

	store := newReaderIntegrationStore(t, databaseURL)
	primaryText := "# Primary Lantern Tale\n\nA traveller follows a lantern home.\n"
	primary, err := store.AdminSourceUpsert(primarySlug, model.AdminSourceUpsertRequest{
		Title: "Primary Lantern Tale", Language: stringPointer("en-GB"), SourceText: primaryText,
	})
	if err != nil {
		t.Fatalf("create primary source version: %v", err)
	}

	empty, err := store.ListCompletedStoryOrchestrationRuns(primary.VersionID, 50)
	if err != nil {
		t.Fatalf("list empty source version: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("empty history = %#v", empty)
	}

	otherText := "# Other Lantern Tale\n\nA different traveller follows a lantern home.\n"
	other, err := store.AdminSourceUpsert(otherSlug, model.AdminSourceUpsertRequest{
		Title: "Other Lantern Tale", Language: stringPointer("en-GB"), SourceText: otherText,
	})
	if err != nil {
		t.Fatalf("create other source version: %v", err)
	}

	type storedRun struct {
		id       string
		semantic adaptationcontract.Result
		sha      string
	}
	persist := func(semantic adaptationcontract.Result) storedRun {
		t.Helper()
		result := testCompletedOrchestrationResult(t, primary.VersionID, primaryText, semantic)
		run, err := store.PersistCompletedStoryOrchestrationRun(primary.VersionID, result)
		if err != nil {
			t.Fatalf("persist %q run: %v", semantic, err)
		}
		return storedRun{id: run.ID, semantic: semantic, sha: result.SourceSHA256}
	}
	oldest := persist(adaptationcontract.ResultPass)
	one, err := store.ListCompletedStoryOrchestrationRuns(primary.VersionID, 50)
	if err != nil {
		t.Fatalf("list one-run history: %v", err)
	}
	if len(one.Items) != 1 ||
		one.Items[0].ID != oldest.id ||
		one.Items[0].SemanticResult != string(adaptationcontract.ResultPass) {
		t.Fatalf("one-run history = %#v", one)
	}
	tieFirst := persist(adaptationcontract.ResultNeedsReview)
	tieSecond := persist(adaptationcontract.ResultFail)
	otherResult := testCompletedOrchestrationResult(t, other.VersionID, otherText, adaptationcontract.ResultPass)
	if _, err := store.PersistCompletedStoryOrchestrationRun(other.VersionID, otherResult); err != nil {
		t.Fatalf("persist other source run: %v", err)
	}

	oldestAt := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	tiedAt := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	for _, update := range []struct {
		id string
		at time.Time
	}{
		{id: oldest.id, at: oldestAt},
		{id: tieFirst.id, at: tiedAt},
		{id: tieSecond.id, at: tiedAt},
	} {
		if _, err := adminDB.Exec(`UPDATE story_orchestration_runs SET created_at = $2 WHERE id = $1`, update.id, update.at); err != nil {
			t.Fatalf("set deterministic run timestamp: %v", err)
		}
	}

	history, err := store.ListCompletedStoryOrchestrationRuns(primary.VersionID, 50)
	if err != nil {
		t.Fatalf("list primary history: %v", err)
	}
	if len(history.Items) != 3 {
		t.Fatalf("primary history length = %d: %#v", len(history.Items), history)
	}
	tieHigh, tieLow := tieFirst, tieSecond
	if tieLow.id > tieHigh.id {
		tieHigh, tieLow = tieLow, tieHigh
	}
	want := []storedRun{tieHigh, tieLow, oldest}
	for index, expected := range want {
		item := history.Items[index]
		if item.ID != expected.id ||
			item.SourceVersionID != primary.VersionID ||
			item.SourceSHA256 != expected.sha ||
			item.SemanticResult != string(expected.semantic) {
			t.Fatalf("history item %d = %#v, want %#v", index, item, expected)
		}
	}
	if history.Items[0].CreatedAt != tiedAt.Format(time.RFC3339Nano) ||
		history.Items[2].CreatedAt != oldestAt.Format(time.RFC3339Nano) {
		t.Fatalf("history timestamps = %#v", history.Items)
	}

	limited, err := store.ListCompletedStoryOrchestrationRuns(primary.VersionID, 1)
	if err != nil {
		t.Fatalf("list limited history: %v", err)
	}
	if len(limited.Items) != 1 || limited.Items[0].ID != tieHigh.id {
		t.Fatalf("limited history = %#v", limited)
	}

	if _, err := store.ListCompletedStoryOrchestrationRuns("00000000-0000-4000-8000-000000000001", 50); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown source version error = %v", err)
	}
	if _, err := store.ListCompletedStoryOrchestrationRuns("not-a-uuid", 50); err == nil {
		t.Fatal("malformed source version ID unexpectedly listed")
	}
}
