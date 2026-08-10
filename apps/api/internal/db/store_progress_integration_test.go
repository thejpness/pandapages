package db

import (
	"database/sql"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
)

const (
	progressIntegrationURLVar   = "PP_PROGRESS_STORE_TEST_DATABASE_URL"
	progressIntegrationGuardVar = "PP_PROGRESS_STORE_TEST_DISPOSABLE"
	progressIntegrationDBName   = "pandapages_progress_store_test"
	progressKeyA                = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	progressKeyB                = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	progressChapterKey          = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func progressLocator(key string, occurrence, ordinal int, offset float64, chapter bool) readercontract.Locator {
	locator := readercontract.Locator{
		Schema: 2,
		Segment: readercontract.LocatorSegment{
			Key:        key,
			Occurrence: occurrence,
			Ordinal:    ordinal,
			Offset:     offset,
		},
	}
	if chapter {
		locator.Chapter = &readercontract.LocatorChapter{
			Key:        progressChapterKey,
			Occurrence: 1,
		}
	}
	return locator
}

func TestProgressStoreIntegration(t *testing.T) {
	if os.Getenv(progressIntegrationGuardVar) != "1" {
		t.Skip("set PP_PROGRESS_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}

	databaseURL := strings.TrimSpace(os.Getenv(progressIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required when %s=1", progressIntegrationURLVar, progressIntegrationGuardVar)
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
	if databaseName != progressIntegrationDBName {
		t.Fatalf("refusing destructive integration setup in database %q; want %q", databaseName, progressIntegrationDBName)
	}

	setupProgressIntegrationSchema(t, adminDB)
	store := newProgressIntegrationStore(t, databaseURL)

	const (
		accountA       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		accountB       = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		profileA       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa01"
		profileA2      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa02"
		profileLittle  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa03"
		profileB       = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		storyA         = "aaaaaaaa-0000-4000-8000-000000000001"
		editionA       = "aaaaaaaa-2000-4000-8000-000000000001"
		editionLittle  = "aaaaaaaa-2000-4000-8000-000000000002"
		versionA       = "aaaaaaaa-1000-4000-8000-000000000001"
		versionLittle  = "aaaaaaaa-1000-4000-8000-000000000002"
		releaseA       = "aaaaaaaa-3000-4000-8000-000000000001"
		releaseClassic = "aaaaaaaa-3000-4000-8000-000000000002"
		slug           = "shared-slug"
	)

	if _, err := adminDB.Exec(
		`INSERT INTO accounts (id, name) VALUES ($1, 'Account A'), ($2, 'Account B')`,
		accountA,
		accountB,
	); err != nil {
		t.Fatalf("insert progress accounts: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO profiles (id, account_id, name, reading_level) VALUES
			($1, $2, 'Ted', 'classic'),
			($3, $2, 'Alex', 'classic'),
			($4, $5, 'Sam', 'classic'),
			($6, $2, 'Mila', 'little-listeners')`,
		profileA, accountA, profileA2, profileB, accountB, profileLittle,
	); err != nil {
		t.Fatalf("insert progress profiles: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO stories (id, visibility, owner_account_id, slug, title) VALUES ($1, 'public', NULL, $2, 'Public Story')`,
		storyA,
		slug,
	); err != nil {
		t.Fatalf("insert account A story: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_editions (id, story_id, edition_key) VALUES
			($1, $2, 'classic'),
			($3, $2, 'little-listeners')`,
		editionA,
		storyA,
		editionLittle,
	); err != nil {
		t.Fatalf("insert account A editions: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_versions (id, story_id, edition_id, version, rendered_html) VALUES ($1, $2, $3, 1, '<p>A</p>')`,
		versionA,
		storyA,
		editionA,
	); err != nil {
		t.Fatalf("insert account A version: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_versions (id, story_id, edition_id, version, rendered_html) VALUES ($1, $2, $3, 2, '<p>Little</p>')`,
		versionLittle,
		storyA,
		editionLittle,
	); err != nil {
		t.Fatalf("insert account A Little Listeners version: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO story_segments (
			story_version_id, ordinal, segment_kind, heading_level,
			content_key, content_occurrence, chapter_key, chapter_occurrence,
			markdown, rendered_html, word_count
		) VALUES
			($1, 1, 'paragraph', NULL, $2, 1, NULL, NULL, 'Opening', '<p>Opening</p>', 1),
			($1, 2, 'heading', 2, $3, 1, $3, 1, '## Chapter', '<h2>Chapter</h2>', 1),
			($1, 3, 'paragraph', NULL, $4, 1, $3, 1, 'Inside', '<p>Inside</p>', 1)
	`, versionA, progressKeyA, progressChapterKey, progressKeyB); err != nil {
		t.Fatalf("insert account A segments: %v", err)
	}
	if _, err := adminDB.Exec(`
		INSERT INTO story_segments (
			story_version_id, ordinal, segment_kind, heading_level,
			content_key, content_occurrence, chapter_key, chapter_occurrence,
			markdown, rendered_html, word_count
		) VALUES
			($1, 1, 'paragraph', NULL, $2, 1, NULL, NULL, 'Little opening', '<p>Little opening</p>', 2)
	`, versionLittle, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"); err != nil {
		t.Fatalf("insert account A Little Listeners segment: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_releases (id, story_id, release_number) VALUES ($1, $2, 1)`,
		releaseA,
		storyA,
	); err != nil {
		t.Fatalf("insert account A release: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_release_editions (release_id, story_id, edition_id, story_version_id) VALUES
			($1, $2, $3, $4),
			($1, $2, $5, $6)`,
		releaseA,
		storyA,
		editionA,
		versionA,
		editionLittle,
		versionLittle,
	); err != nil {
		t.Fatalf("insert account A release members: %v", err)
	}
	if _, err := adminDB.Exec(
		`UPDATE stories SET current_release_id = $2 WHERE id = $1`,
		storyA,
		releaseA,
	); err != nil {
		t.Fatalf("make account A release current: %v", err)
	}

	t.Run("known empty progress is distinct from a missing story", func(t *testing.T) {
		got, err := store.ProgressGet(accountA, profileA, slug)
		if err != nil {
			t.Fatalf("ProgressGet empty: %v", err)
		}
		if got.Progress != nil {
			t.Fatalf("empty progress = %#v, want nil", got.Progress)
		}
		if _, err := store.ProgressGet(accountA, profileA, "missing-story"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing ProgressGet error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("valid typed put creates and updates progress", func(t *testing.T) {
		first := progressLocator(progressKeyA, 1, 1, 0.25, false)
		if err := store.ProgressPut(accountA, profileA, slug, 1, first, 0.25); err != nil {
			t.Fatalf("ProgressPut first: %v", err)
		}
		got, err := store.ProgressGet(accountA, profileA, slug)
		if err != nil {
			t.Fatalf("ProgressGet first: %v", err)
		}
		assertProgressState(t, got, 1, first, 0.25)

		later := progressLocator(progressKeyB, 1, 3, 0.5, true)
		if err := store.ProgressPut(accountA, profileA, slug, 1, later, 0.75); err != nil {
			t.Fatalf("ProgressPut update: %v", err)
		}
		got, err = store.ProgressGet(accountA, profileA, slug)
		if err != nil {
			t.Fatalf("ProgressGet update: %v", err)
		}
		assertProgressState(t, got, 1, later, 0.75)

		var rows int
		if err := adminDB.QueryRow(`SELECT count(*) FROM reading_progress`).Scan(&rows); err != nil {
			t.Fatalf("count progress rows: %v", err)
		}
		if rows != 1 {
			t.Fatalf("progress rows = %d, want 1", rows)
		}
		var storedAccountID, storedProfileID string
		if err := adminDB.QueryRow(`SELECT account_id, profile_id FROM reading_progress`).Scan(&storedAccountID, &storedProfileID); err != nil {
			t.Fatalf("read progress ownership: %v", err)
		}
		if storedAccountID != accountA {
			t.Fatalf("progress account = %q, want %q", storedAccountID, accountA)
		}
		if storedProfileID != profileA {
			t.Fatalf("progress profile = %q, want %q", storedProfileID, profileA)
		}
	})

	t.Run("profiles retain independent progress for the same story", func(t *testing.T) {
		first := progressLocator(progressKeyA, 1, 1, 0.2, false)
		second := progressLocator(progressKeyB, 1, 3, 0.7, true)
		if err := store.ProgressPut(accountA, profileA, slug, 1, first, 0.2); err != nil {
			t.Fatalf("ProgressPut profile A: %v", err)
		}
		if err := store.ProgressPut(accountA, profileA2, slug, 1, second, 0.7); err != nil {
			t.Fatalf("ProgressPut profile A2: %v", err)
		}
		gotA, err := store.ProgressGet(accountA, profileA, slug)
		if err != nil {
			t.Fatalf("ProgressGet profile A: %v", err)
		}
		gotA2, err := store.ProgressGet(accountA, profileA2, slug)
		if err != nil {
			t.Fatalf("ProgressGet profile A2: %v", err)
		}
		assertProgressState(t, gotA, 1, first, 0.2)
		assertProgressState(t, gotA2, 1, second, 0.7)

		continueA, err := store.ContinueRecent(accountA, profileA, 3)
		if err != nil || len(continueA) != 1 || continueA[0].Slug != slug || math.Abs(continueA[0].Percent-0.2) > 0.0001 {
			t.Fatalf("profile A continue = %#v / %v", continueA, err)
		}
		continueA2, err := store.ContinueRecent(accountA, profileA2, 3)
		if err != nil || len(continueA2) != 1 || continueA2[0].Slug != slug || math.Abs(continueA2[0].Percent-0.7) > 0.0001 {
			t.Fatalf("profile A2 continue = %#v / %v", continueA2, err)
		}
	})

	t.Run("identity mismatches are rejected without changing confirmed progress", func(t *testing.T) {
		confirmed := progressLocator(progressKeyB, 1, 3, 0.5, true)
		tests := []struct {
			name   string
			mutate func(*readercontract.Locator)
		}{
			{name: "key", mutate: func(locator *readercontract.Locator) { locator.Segment.Key = progressKeyA }},
			{name: "occurrence", mutate: func(locator *readercontract.Locator) { locator.Segment.Occurrence = 2 }},
			{name: "ordinal", mutate: func(locator *readercontract.Locator) { locator.Segment.Ordinal = 99 }},
			{name: "chapter absent", mutate: func(locator *readercontract.Locator) { locator.Chapter = nil }},
			{name: "chapter key", mutate: func(locator *readercontract.Locator) { locator.Chapter.Key = progressKeyA }},
			{name: "chapter occurrence", mutate: func(locator *readercontract.Locator) { locator.Chapter.Occurrence = 2 }},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				candidate := confirmed
				chapter := *confirmed.Chapter
				candidate.Chapter = &chapter
				test.mutate(&candidate)
				if err := store.ProgressPut(accountA, profileA, slug, 1, candidate, 0.9); !errors.Is(err, readercontract.ErrLocatorMismatch) {
					t.Fatalf("ProgressPut error = %v, want locator mismatch", err)
				}
				got, err := store.ProgressGet(accountA, profileA, slug)
				if err != nil {
					t.Fatalf("ProgressGet after mismatch: %v", err)
				}
				assertProgressState(t, got, 1, progressLocator(progressKeyA, 1, 1, 0.2, false), 0.2)
			})
		}
	})

	t.Run("percentage is rejected rather than clamped", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		for _, invalid := range []float64{-0.01, 1.01, math.Inf(1), math.NaN()} {
			if err := store.ProgressPut(accountA, profileA, slug, 1, locator, invalid); err == nil {
				t.Fatalf("ProgressPut accepted invalid percent %v", invalid)
			}
		}
	})

	t.Run("missing story and version return sql ErrNoRows", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		if err := store.ProgressPut(accountA, profileA, "missing-story", 1, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-story error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, profileA, slug, 99, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-version error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("public stories keep progress independent across accounts", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		if err := store.ProgressPut(accountB, profileB, slug, 1, locator, 0.2); err != nil {
			t.Fatalf("public cross-account ProgressPut: %v", err)
		}
		got, err := store.ProgressGet(accountB, profileB, slug)
		if err != nil {
			t.Fatalf("public cross-account ProgressGet: %v", err)
		}
		assertProgressState(t, got, 1, locator, 0.2)
		continueB, err := store.ContinueRecent(accountB, profileB, 3)
		if err != nil || len(continueB) != 1 || continueB[0].Slug != slug {
			t.Fatalf("public cross-account ContinueRecent = %#v / %v", continueB, err)
		}
	})

	t.Run("duplicate global story slugs are rejected", func(t *testing.T) {
		if _, err := adminDB.Exec(
			`INSERT INTO stories (id, visibility, owner_account_id, slug, title) VALUES ($1, 'public', NULL, $2, 'Duplicate slug')`,
			"bbbbbbbb-0000-4000-8000-000000000001",
			slug,
		); err == nil {
			t.Fatal("database accepted a duplicate global story slug")
		}
	})

	t.Run("reading level gates current release progress without deleting stale history", func(t *testing.T) {
		littleLocator := progressLocator(
			"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			1,
			1,
			0.4,
			false,
		)
		classicLocator := progressLocator(progressKeyA, 1, 1, 0.2, false)

		if err := store.ProgressPut(accountA, profileLittle, slug, 1, classicLocator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("Little Listeners Classic ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, profileLittle, slug, 2, littleLocator, 0.64); err != nil {
			t.Fatalf("Little Listeners current ProgressPut: %v", err)
		}
		got, err := store.ProgressGet(accountA, profileLittle, slug)
		if err != nil {
			t.Fatalf("Little Listeners ProgressGet: %v", err)
		}
		assertProgressState(t, got, 2, littleLocator, 0.64)

		items, err := store.ContinueRecent(accountA, profileLittle, 3)
		if err != nil || len(items) != 1 || items[0].Slug != slug {
			t.Fatalf("Little Listeners ContinueRecent = %#v / %v", items, err)
		}

		if _, err := adminDB.Exec(
			`INSERT INTO story_releases (id, story_id, release_number) VALUES ($1, $2, 2)`,
			releaseClassic,
			storyA,
		); err != nil {
			t.Fatalf("insert Classic-only release: %v", err)
		}
		if _, err := adminDB.Exec(
			`INSERT INTO story_release_editions (release_id, story_id, edition_id, story_version_id) VALUES ($1, $2, $3, $4)`,
			releaseClassic,
			storyA,
			editionA,
			versionA,
		); err != nil {
			t.Fatalf("insert Classic-only release member: %v", err)
		}
		if _, err := adminDB.Exec(
			`UPDATE stories SET current_release_id = $2 WHERE id = $1`,
			storyA,
			releaseClassic,
		); err != nil {
			t.Fatalf("make Classic-only release current: %v", err)
		}

		if _, err := store.ProgressGet(accountA, profileLittle, slug); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ineligible ProgressGet error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, profileLittle, slug, 2, littleLocator, 0.7); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("stale Little Listeners ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		items, err = store.ContinueRecent(accountA, profileLittle, 3)
		if err != nil || len(items) != 0 {
			t.Fatalf("ineligible ContinueRecent = %#v / %v, want empty", items, err)
		}

		var storedRows int
		if err := adminDB.QueryRow(`
			SELECT count(*)
			FROM reading_progress
			WHERE account_id = $1 AND profile_id = $2 AND story_id = $3
		`, accountA, profileLittle, storyA).Scan(&storedRows); err != nil {
			t.Fatalf("count retained stale progress: %v", err)
		}
		if storedRows != 1 {
			t.Fatalf("retained stale progress rows = %d, want 1", storedRows)
		}

		if _, err := adminDB.Exec(`
			UPDATE profiles
			SET reading_level = 'classic'
			WHERE account_id = $1 AND id = $2
		`, accountA, profileLittle); err != nil {
			t.Fatalf("raise profile Reading Level to Classic: %v", err)
		}

		got, err = store.ProgressGet(accountA, profileLittle, slug)
		if err != nil {
			t.Fatalf("eligible story with stale progress ProgressGet: %v", err)
		}
		assertProgressState(t, got, 2, littleLocator, 0.64)
		if err := store.ProgressPut(accountA, profileLittle, slug, 2, littleLocator, 0.72); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("non-current stale version ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, profileLittle, slug, 1, classicLocator, 0.72); err != nil {
			t.Fatalf("current eligible Classic ProgressPut: %v", err)
		}
		got, err = store.ProgressGet(accountA, profileLittle, slug)
		if err != nil {
			t.Fatalf("current Classic ProgressGet: %v", err)
		}
		assertProgressState(t, got, 1, classicLocator, 0.72)
	})

	t.Run("private stories fail closed for another account", func(t *testing.T) {
		if _, err := adminDB.Exec(`UPDATE stories SET visibility = 'private', owner_account_id = $2 WHERE id = $1`, storyA, accountA); err != nil {
			t.Fatalf("make story private: %v", err)
		}
		if _, err := store.ProgressGet(accountA, profileA, slug); err != nil {
			t.Fatalf("private owner ProgressGet: %v", err)
		}
		for _, candidateSlug := range []string{slug, "missing-private-story"} {
			if _, err := store.ProgressGet(accountB, profileB, candidateSlug); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("foreign private ProgressGet(%q) error = %v, want sql.ErrNoRows", candidateSlug, err)
			}
		}
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		if err := store.ProgressPut(accountB, profileB, slug, 1, locator, 0.4); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("foreign private ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		items, err := store.ContinueRecent(accountB, profileB, 3)
		if err != nil || len(items) != 0 {
			t.Fatalf("foreign private ContinueRecent = %#v / %v, want empty", items, err)
		}
	})
}

func newProgressIntegrationStore(t *testing.T, databaseURL string) *Store {
	t.Helper()
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open progress Store database: %v", err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		t.Fatalf("ping progress Store database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	return &Store{
		db:           database,
		queryTimeout: 10 * time.Second,
	}
}

func setupProgressIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`DROP TABLE IF EXISTS reading_progress, story_segments, story_release_editions, story_releases, story_versions, story_editions, stories, profiles, accounts CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE accounts (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE profiles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
			name text NOT NULL,
			reading_level text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (account_id, name),
			UNIQUE (id, account_id)
		)`,
		`CREATE TABLE stories (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			slug text NOT NULL UNIQUE,
			visibility text NOT NULL CHECK (visibility IN ('public', 'private')),
			owner_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
			title text NOT NULL,
			current_release_id uuid,
			CHECK ((visibility = 'public' AND owner_account_id IS NULL) OR (visibility = 'private' AND owner_account_id IS NOT NULL))
		)`,
		`CREATE TABLE story_editions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			edition_key text NOT NULL,
			UNIQUE (story_id, edition_key),
			UNIQUE (id, story_id)
		)`,
		`CREATE TABLE story_versions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			edition_id uuid NOT NULL,
			version integer NOT NULL,
			rendered_html text NOT NULL,
			UNIQUE (story_id, version),
			UNIQUE (id, story_id),
			UNIQUE (id, edition_id),
			FOREIGN KEY (edition_id, story_id)
				REFERENCES story_editions(id, story_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE story_releases (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			release_number integer NOT NULL,
			UNIQUE (story_id, release_number),
			UNIQUE (id, story_id)
		)`,
		`ALTER TABLE stories
			ADD CONSTRAINT stories_current_release_progress_test_fkey
			FOREIGN KEY (current_release_id, id)
			REFERENCES story_releases(id, story_id)
			ON DELETE SET NULL (current_release_id)`,
		`CREATE TABLE story_release_editions (
			release_id uuid NOT NULL,
			story_id uuid NOT NULL,
			edition_id uuid NOT NULL,
			story_version_id uuid NOT NULL,
			PRIMARY KEY (release_id, edition_id),
			UNIQUE (release_id, story_version_id),
			FOREIGN KEY (release_id, story_id)
				REFERENCES story_releases(id, story_id) ON DELETE CASCADE,
			FOREIGN KEY (edition_id, story_id)
				REFERENCES story_editions(id, story_id) ON DELETE CASCADE,
			FOREIGN KEY (story_version_id, edition_id)
				REFERENCES story_versions(id, edition_id) ON DELETE RESTRICT
		)`,
		`CREATE TABLE story_segments (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_version_id uuid NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
			ordinal integer NOT NULL,
			segment_kind text NOT NULL,
			heading_level integer,
			content_key text NOT NULL,
			content_occurrence integer NOT NULL,
			chapter_key text,
			chapter_occurrence integer,
			markdown text NOT NULL,
			rendered_html text NOT NULL,
			word_count integer NOT NULL,
			UNIQUE (story_version_id, ordinal),
			UNIQUE (story_version_id, content_key, content_occurrence)
		)`,
		`CREATE TABLE reading_progress (
			account_id uuid NOT NULL,
			profile_id uuid NOT NULL,
			story_id uuid NOT NULL,
			story_version_id uuid NOT NULL,
			locator jsonb NOT NULL,
			percent real NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (account_id, profile_id, story_id),
			FOREIGN KEY (profile_id, account_id)
				REFERENCES profiles(id, account_id) ON DELETE CASCADE,
			FOREIGN KEY (story_id)
				REFERENCES stories(id) ON DELETE CASCADE,
			FOREIGN KEY (story_version_id, story_id)
				REFERENCES story_versions(id, story_id)
		)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("prepare disposable progress schema: %v", err)
		}
	}
}

func assertProgressState(
	t *testing.T,
	response model.ProgressResponse,
	wantVersion int,
	wantLocator readercontract.Locator,
	wantPercent float64,
) {
	t.Helper()
	if response.Progress == nil {
		t.Fatal("progress = nil, want stored state")
	}
	if response.Progress.Version != wantVersion {
		t.Fatalf("version = %d, want %d", response.Progress.Version, wantVersion)
	}
	if !reflect.DeepEqual(response.Progress.Locator, wantLocator) {
		t.Fatalf("locator = %#v, want %#v", response.Progress.Locator, wantLocator)
	}
	if math.Abs(response.Progress.Percent-wantPercent) > 0.0001 {
		t.Fatalf("percent = %v, want %v", response.Progress.Percent, wantPercent)
	}
}
