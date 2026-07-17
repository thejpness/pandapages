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
		accountA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		accountB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		storyA   = "aaaaaaaa-0000-4000-8000-000000000001"
		versionA = "aaaaaaaa-1000-4000-8000-000000000001"
		slug     = "shared-slug"
	)

	if _, err := adminDB.Exec(
		`INSERT INTO accounts (id, name) VALUES ($1, 'Account A'), ($2, 'Account B')`,
		accountA,
		accountB,
	); err != nil {
		t.Fatalf("insert progress accounts: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO stories (id, account_id, slug, title, is_published) VALUES ($1, $2, $3, 'Account A Story', true)`,
		storyA,
		accountA,
		slug,
	); err != nil {
		t.Fatalf("insert account A story: %v", err)
	}
	if _, err := adminDB.Exec(
		`INSERT INTO story_versions (id, story_id, version, rendered_html) VALUES ($1, $2, 1, '<p>A</p>')`,
		versionA,
		storyA,
	); err != nil {
		t.Fatalf("insert account A version: %v", err)
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
	if _, err := adminDB.Exec(
		`UPDATE stories SET published_version_id = $2 WHERE id = $1`,
		storyA,
		versionA,
	); err != nil {
		t.Fatalf("publish account A story: %v", err)
	}

	t.Run("known empty progress is distinct from a missing story", func(t *testing.T) {
		got, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet empty: %v", err)
		}
		if got.Progress != nil {
			t.Fatalf("empty progress = %#v, want nil", got.Progress)
		}
		if _, err := store.ProgressGet(accountA, "missing-story"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing ProgressGet error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("valid typed put creates and updates progress", func(t *testing.T) {
		first := progressLocator(progressKeyA, 1, 1, 0.25, false)
		if err := store.ProgressPut(accountA, slug, 1, first, 0.25); err != nil {
			t.Fatalf("ProgressPut first: %v", err)
		}
		got, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet first: %v", err)
		}
		assertProgressState(t, got, 1, first, 0.25)

		later := progressLocator(progressKeyB, 1, 3, 0.5, true)
		if err := store.ProgressPut(accountA, slug, 1, later, 0.75); err != nil {
			t.Fatalf("ProgressPut update: %v", err)
		}
		got, err = store.ProgressGet(accountA, slug)
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
				if err := store.ProgressPut(accountA, slug, 1, candidate, 0.9); !errors.Is(err, readercontract.ErrLocatorMismatch) {
					t.Fatalf("ProgressPut error = %v, want locator mismatch", err)
				}
				got, err := store.ProgressGet(accountA, slug)
				if err != nil {
					t.Fatalf("ProgressGet after mismatch: %v", err)
				}
				assertProgressState(t, got, 1, confirmed, 0.75)
			})
		}
	})

	t.Run("percentage is rejected rather than clamped", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		for _, invalid := range []float64{-0.01, 1.01, math.Inf(1), math.NaN()} {
			if err := store.ProgressPut(accountA, slug, 1, locator, invalid); err == nil {
				t.Fatalf("ProgressPut accepted invalid percent %v", invalid)
			}
		}
	})

	t.Run("missing story and version return sql ErrNoRows", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		if err := store.ProgressPut(accountA, "missing-story", 1, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-story error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, slug, 2, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-version error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("another account cannot access the first account story", func(t *testing.T) {
		locator := progressLocator(progressKeyA, 1, 1, 0, false)
		if _, err := store.ProgressGet(accountB, slug); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cross-account ProgressGet error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountB, slug, 1, locator, 0.2); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cross-account ProgressPut error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("identical slugs remain independent between accounts", func(t *testing.T) {
		const (
			storyB   = "bbbbbbbb-0000-4000-8000-000000000001"
			versionB = "bbbbbbbb-1000-4000-8000-000000000001"
		)
		if _, err := adminDB.Exec(
			`INSERT INTO stories (id, account_id, slug, title, is_published) VALUES ($1, $2, $3, 'Account B Story', true)`,
			storyB,
			accountB,
			slug,
		); err != nil {
			t.Fatalf("insert account B story: %v", err)
		}
		if _, err := adminDB.Exec(
			`INSERT INTO story_versions (id, story_id, version, rendered_html) VALUES ($1, $2, 1, '<p>B</p>')`,
			versionB,
			storyB,
		); err != nil {
			t.Fatalf("insert account B version: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_segments (
				story_version_id, ordinal, segment_kind, content_key,
				content_occurrence, markdown, rendered_html, word_count
			) VALUES ($1, 1, 'paragraph', $2, 1, 'Other', '<p>Other</p>', 1)
		`, versionB, progressKeyB); err != nil {
			t.Fatalf("insert account B segment: %v", err)
		}
		if _, err := adminDB.Exec(`UPDATE stories SET published_version_id = $2 WHERE id = $1`, storyB, versionB); err != nil {
			t.Fatalf("publish account B story: %v", err)
		}

		locatorA := progressLocator(progressKeyA, 1, 1, 0.9, false)
		locatorB := progressLocator(progressKeyB, 1, 1, 0.4, false)
		if err := store.ProgressPut(accountA, slug, 1, locatorA, 0.91); err != nil {
			t.Fatalf("ProgressPut account A independent: %v", err)
		}
		if err := store.ProgressPut(accountB, slug, 1, locatorB, 0.4); err != nil {
			t.Fatalf("ProgressPut account B independent: %v", err)
		}

		gotA, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet account A independent: %v", err)
		}
		gotB, err := store.ProgressGet(accountB, slug)
		if err != nil {
			t.Fatalf("ProgressGet account B independent: %v", err)
		}
		assertProgressState(t, gotA, 1, locatorA, 0.91)
		assertProgressState(t, gotB, 1, locatorB, 0.4)
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
		db:                      database,
		queryTimeout:            10 * time.Second,
		defaultProfileByAccount: map[string]string{},
	}
}

func setupProgressIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`DROP TABLE IF EXISTS reading_progress, story_segments, story_versions, stories, profiles, accounts CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE accounts (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE profiles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (account_id, name)
		)`,
		`CREATE TABLE stories (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			slug text NOT NULL,
			title text NOT NULL,
			is_published boolean NOT NULL DEFAULT false,
			published_version_id uuid,
			UNIQUE (account_id, slug)
		)`,
		`CREATE TABLE story_versions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			version integer NOT NULL,
			rendered_html text NOT NULL,
			UNIQUE (story_id, version)
		)`,
		`ALTER TABLE stories
			ADD CONSTRAINT stories_published_version_progress_test_fkey
			FOREIGN KEY (published_version_id) REFERENCES story_versions(id) ON DELETE SET NULL`,
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
			profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			story_version_id uuid NOT NULL REFERENCES story_versions(id),
			locator jsonb NOT NULL,
			percent real NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (profile_id, story_id)
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
