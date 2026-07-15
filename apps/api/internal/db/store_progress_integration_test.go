package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
)

const (
	progressIntegrationURLVar   = "PP_PROGRESS_STORE_TEST_DATABASE_URL"
	progressIntegrationGuardVar = "PP_PROGRESS_STORE_TEST_DISPOSABLE"
	progressIntegrationDBName   = "pandapages_progress_store_test"
)

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
		`INSERT INTO stories (id, account_id, slug, title) VALUES ($1, $2, $3, 'Account A Story')`,
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
	if _, err := adminDB.Exec(
		`UPDATE stories SET published_version_id = $2 WHERE id = $1`,
		storyA,
		versionA,
	); err != nil {
		t.Fatalf("publish account A story: %v", err)
	}

	t.Run("first put creates and get returns progress", func(t *testing.T) {
		locator := json.RawMessage(`{"mode":"scroll","scrollY":120}`)
		if err := store.ProgressPut(accountA, slug, 1, locator, 0.25); err != nil {
			t.Fatalf("ProgressPut first: %v", err)
		}

		got, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet first: %v", err)
		}
		assertProgressState(t, got, 1, locator, 0.25)

		var rows int
		if err := adminDB.QueryRow(`SELECT count(*) FROM reading_progress`).Scan(&rows); err != nil {
			t.Fatalf("count first progress row: %v", err)
		}
		if rows != 1 {
			t.Fatalf("progress rows = %d, want 1", rows)
		}
	})

	t.Run("later put updates the existing row", func(t *testing.T) {
		locator := json.RawMessage(`{"mode":"scroll","scrollY":640}`)
		if err := store.ProgressPut(accountA, slug, 1, locator, 0.75); err != nil {
			t.Fatalf("ProgressPut update: %v", err)
		}
		got, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet update: %v", err)
		}
		assertProgressState(t, got, 1, locator, 0.75)

		var rows int
		if err := adminDB.QueryRow(`SELECT count(*) FROM reading_progress`).Scan(&rows); err != nil {
			t.Fatalf("count updated progress rows: %v", err)
		}
		if rows != 1 {
			t.Fatalf("updated progress rows = %d, want 1", rows)
		}
	})

	t.Run("percentage remains clamped", func(t *testing.T) {
		locator := json.RawMessage(`{"mode":"scroll","scrollY":900}`)
		if err := store.ProgressPut(accountA, slug, 1, locator, 4); err != nil {
			t.Fatalf("ProgressPut upper clamp: %v", err)
		}
		got, err := store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet upper clamp: %v", err)
		}
		assertProgressState(t, got, 1, locator, 1)

		if err := store.ProgressPut(accountA, slug, 1, locator, -4); err != nil {
			t.Fatalf("ProgressPut lower clamp: %v", err)
		}
		got, err = store.ProgressGet(accountA, slug)
		if err != nil {
			t.Fatalf("ProgressGet lower clamp: %v", err)
		}
		assertProgressState(t, got, 1, locator, 0)
	})

	t.Run("missing story and version return sql ErrNoRows", func(t *testing.T) {
		locator := json.RawMessage(`{"mode":"scroll","scrollY":1}`)
		if err := store.ProgressPut(accountA, "missing-story", 1, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-story error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(accountA, slug, 2, locator, 0.1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing-version error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("another account cannot access the first account story", func(t *testing.T) {
		locator := json.RawMessage(`{"mode":"scroll","scrollY":50}`)
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
			`INSERT INTO stories (id, account_id, slug, title) VALUES ($1, $2, $3, 'Account B Story')`,
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
		if _, err := adminDB.Exec(
			`UPDATE stories SET published_version_id = $2 WHERE id = $1`,
			storyB,
			versionB,
		); err != nil {
			t.Fatalf("publish account B story: %v", err)
		}

		locatorA := json.RawMessage(`{"mode":"scroll","scrollY":910}`)
		locatorB := json.RawMessage(`{"mode":"paged","page":3}`)
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

		var rows int
		if err := adminDB.QueryRow(`SELECT count(*) FROM reading_progress`).Scan(&rows); err != nil {
			t.Fatalf("count independent progress rows: %v", err)
		}
		if rows != 2 {
			t.Fatalf("independent progress rows = %d, want 2", rows)
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
		db:                      database,
		queryTimeout:            10 * time.Second,
		defaultProfileByAccount: map[string]string{},
	}
}

func setupProgressIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`DROP TABLE IF EXISTS reading_progress, story_versions, stories, profiles, accounts CASCADE`,
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
	state model.ProgressState,
	wantVersion int,
	wantLocator json.RawMessage,
	wantPercent float64,
) {
	t.Helper()
	var gotLocator any
	if err := json.Unmarshal(state.Locator, &gotLocator); err != nil {
		t.Fatalf("decode stored locator: %v", err)
	}
	var expectedLocator any
	if err := json.Unmarshal(wantLocator, &expectedLocator); err != nil {
		t.Fatalf("decode expected locator: %v", err)
	}
	if state.Version != wantVersion {
		t.Fatalf("version = %d, want %d", state.Version, wantVersion)
	}
	if !reflect.DeepEqual(gotLocator, expectedLocator) {
		t.Fatalf("locator = %#v, want %#v", gotLocator, expectedLocator)
	}
	if math.Abs(state.Percent-wantPercent) > 0.0001 {
		t.Fatalf("percent = %v, want %v", state.Percent, wantPercent)
	}
}
