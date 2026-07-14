package db

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/httpapi"
	"pandapages/api/internal/session"
)

const (
	accountIntegrationURLVar   = "PP_ACCOUNT_STORE_TEST_DATABASE_URL"
	accountIntegrationGuardVar = "PP_ACCOUNT_STORE_TEST_DISPOSABLE"
	accountIntegrationDBName   = "pandapages_account_store_test"
)

func TestAccountStoreIntegration(t *testing.T) {
	if os.Getenv(accountIntegrationGuardVar) != "1" {
		t.Skip("set PP_ACCOUNT_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}

	databaseURL := strings.TrimSpace(os.Getenv(accountIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required when %s=1", accountIntegrationURLVar, accountIntegrationGuardVar)
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
	if databaseName != accountIntegrationDBName {
		t.Fatalf("refusing destructive integration setup in database %q; want %q", databaseName, accountIntegrationDBName)
	}

	setupAccountIntegrationSchema(t, adminDB)

	t.Run("empty table initialization is concurrent and singular", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)

		const callers = 12
		stores := make([]*Store, 0, callers)
		for range callers {
			stores = append(stores, newAccountIntegrationStore(t, databaseURL))
		}

		type result struct {
			id  string
			err error
		}
		results := make([]result, callers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(callers)

		for i, store := range stores {
			go func() {
				defer wg.Done()
				<-start
				results[i].id, results[i].err = store.EnsureDefaultAccount()
			}()
		}

		close(start)
		wg.Wait()

		wantID := results[0].id
		if results[0].err != nil {
			t.Fatalf("EnsureDefaultAccount caller 0: %v", results[0].err)
		}
		if wantID == "" {
			t.Fatal("EnsureDefaultAccount caller 0 returned an empty id")
		}
		for i, result := range results {
			if result.err != nil {
				t.Errorf("EnsureDefaultAccount caller %d: %v", i, result.err)
				continue
			}
			if result.id != wantID {
				t.Errorf("EnsureDefaultAccount caller %d id = %q, want %q", i, result.id, wantID)
			}
		}

		var count int
		if err := adminDB.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count); err != nil {
			t.Fatalf("count initialized accounts: %v", err)
		}
		if count != 1 {
			t.Fatalf("initialized accounts = %d, want exactly 1", count)
		}
	})

	t.Run("selection is deterministic and not indefinitely cached", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)

		const initialID = "00000000-0000-4000-8000-000000000003"
		if _, err := adminDB.Exec(`
			INSERT INTO accounts (id, name, created_at)
			VALUES ($1, 'Initial', '2026-01-02T00:00:00Z')
		`, initialID); err != nil {
			t.Fatalf("insert initial account: %v", err)
		}

		got, err := store.EnsureDefaultAccount()
		if err != nil {
			t.Fatalf("select initial account: %v", err)
		}
		if got != initialID {
			t.Fatalf("initial account id = %q, want %q", got, initialID)
		}

		const (
			tiedLargerID  = "00000000-0000-4000-8000-000000000002"
			tiedSmallerID = "00000000-0000-4000-8000-000000000001"
		)
		if _, err := adminDB.Exec(`
			INSERT INTO accounts (id, name, created_at)
			VALUES
				($1, 'Earlier larger id', '2026-01-01T00:00:00Z'),
				($2, 'Earlier smaller id', '2026-01-01T00:00:00Z')
		`, tiedLargerID, tiedSmallerID); err != nil {
			t.Fatalf("insert deterministic accounts: %v", err)
		}

		got, err = store.EnsureDefaultAccount()
		if err != nil {
			t.Fatalf("reselect oldest account: %v", err)
		}
		if got != tiedSmallerID {
			t.Fatalf("oldest account id = %q, want timestamp/id winner %q", got, tiedSmallerID)
		}

		var count int
		if err := adminDB.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count); err != nil {
			t.Fatalf("count existing accounts: %v", err)
		}
		if count != 3 {
			t.Fatalf("existing accounts = %d, want all 3 preserved", count)
		}
	})

	t.Run("account existence validates identifiers", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)

		const existingID = "11111111-1111-4111-8111-111111111111"
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Existing')`, existingID); err != nil {
			t.Fatalf("insert existing account: %v", err)
		}

		for _, test := range []struct {
			name string
			id   string
			want bool
		}{
			{name: "existing", id: existingID, want: true},
			{name: "existing with surrounding whitespace", id: "  " + existingID + "\t", want: true},
			{name: "unknown canonical uuid", id: "22222222-2222-4222-8222-222222222222", want: false},
			{name: "empty", id: "", want: false},
			{name: "malformed", id: "not-an-account", want: false},
			{name: "uuid with suffix", id: existingID + "-extra", want: false},
		} {
			t.Run(test.name, func(t *testing.T) {
				got, err := store.AccountExists(test.id)
				if err != nil {
					t.Fatalf("AccountExists(%q): %v", test.id, err)
				}
				if got != test.want {
					t.Fatalf("AccountExists(%q) = %t, want %t", test.id, got, test.want)
				}
			})
		}
	})

	t.Run("real HTTP session journey and removed account rejection", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)
		now := time.Date(2026, time.July, 14, 17, 10, 41, 0, time.UTC)
		sessions, err := session.New(
			"disposable-integration-session-secret",
			false,
			session.WithClock(func() time.Time { return now }),
		)
		if err != nil {
			t.Fatalf("create session manager: %v", err)
		}
		handler := httpapi.New(httpapi.Config{
			Passcode: "123456",
			Sessions: sessions,
		}, store)

		unlockRequest := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/auth/unlock",
			strings.NewReader(`{"passcode":"123456"}`),
		)
		unlockRequest.Header.Set("Content-Type", "application/json")
		unlockResponse := httptest.NewRecorder()
		handler.ServeHTTP(unlockResponse, unlockRequest)
		if unlockResponse.Code != http.StatusOK {
			t.Fatalf("unlock status = %d; body = %s", unlockResponse.Code, unlockResponse.Body.String())
		}

		var sessionCookie *http.Cookie
		for _, cookie := range unlockResponse.Result().Cookies() {
			if cookie.Name == session.CookieName {
				sessionCookie = cookie
				break
			}
		}
		if sessionCookie == nil {
			t.Fatal("unlock did not issue pp_session")
		}

		assertIntegrationStatus(t, handler, sessionCookie, true)

		libraryRequest := httptest.NewRequest(http.MethodGet, "/api/v1/library", nil)
		libraryRequest.AddCookie(sessionCookie)
		libraryResponse := httptest.NewRecorder()
		handler.ServeHTTP(libraryResponse, libraryRequest)
		if libraryResponse.Code != http.StatusOK {
			t.Fatalf("library status = %d; body = %s", libraryResponse.Code, libraryResponse.Body.String())
		}

		logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		logoutRequest.AddCookie(sessionCookie)
		logoutResponse := httptest.NewRecorder()
		handler.ServeHTTP(logoutResponse, logoutRequest)
		if logoutResponse.Code != http.StatusOK {
			t.Fatalf("logout status = %d; body = %s", logoutResponse.Code, logoutResponse.Body.String())
		}
		assertIntegrationStatus(t, handler, nil, false)

		claims, err := sessions.Verify(sessionCookie.Value)
		if err != nil {
			t.Fatalf("verify issued session before account removal: %v", err)
		}
		if _, err := adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, claims.AccountID); err != nil {
			t.Fatalf("remove session account: %v", err)
		}

		assertIntegrationStatus(t, handler, sessionCookie, false)
		unknownRequest := httptest.NewRequest(http.MethodGet, "/api/v1/library", nil)
		unknownRequest.AddCookie(sessionCookie)
		unknownResponse := httptest.NewRecorder()
		handler.ServeHTTP(unknownResponse, unknownRequest)
		if unknownResponse.Code != http.StatusUnauthorized {
			t.Fatalf("removed-account library status = %d, want %d; body = %s", unknownResponse.Code, http.StatusUnauthorized, unknownResponse.Body.String())
		}
	})

	t.Run("library remains account scoped", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)

		const (
			accountA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			accountB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
			storyA   = "aaaaaaaa-0000-4000-8000-000000000001"
			storyB   = "bbbbbbbb-0000-4000-8000-000000000001"
			draftA   = "aaaaaaaa-0000-4000-8000-000000000002"
			versionA = "aaaaaaaa-1000-4000-8000-000000000001"
			versionB = "bbbbbbbb-1000-4000-8000-000000000001"
		)

		if _, err := adminDB.Exec(
			`INSERT INTO accounts (id, name) VALUES ($1, 'Account A'), ($2, 'Account B')`,
			accountA, accountB,
		); err != nil {
			t.Fatalf("insert library accounts: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO stories (id, account_id, slug, title) VALUES
				($3, $1, 'account-a-story', 'Account A Story'),
				($4, $2, 'account-b-story', 'Account B Story'),
				($5, $1, 'account-a-draft', 'Account A Draft')
		`, accountA, accountB, storyA, storyB, draftA); err != nil {
			t.Fatalf("insert account-scoped stories: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_versions (id, story_id, version, rendered_html) VALUES
				($3, $1, 1, '<p>A</p>'),
				($4, $2, 1, '<p>B</p>')
		`, storyA, storyB, versionA, versionB); err != nil {
			t.Fatalf("insert account-scoped story versions: %v", err)
		}
		if _, err := adminDB.Exec(`
			UPDATE stories
			SET published_version_id = CASE id WHEN $1 THEN $3::uuid WHEN $2 THEN $4::uuid END
			WHERE id IN ($1, $2)
		`, storyA, storyB, versionA, versionB); err != nil {
			t.Fatalf("publish account-scoped stories: %v", err)
		}

		itemsA, err := store.Library(accountA)
		if err != nil {
			t.Fatalf("Library(account A): %v", err)
		}
		if len(itemsA) != 1 || itemsA[0].Slug != "account-a-story" {
			t.Fatalf("Library(account A) = %#v, want only account-a-story", itemsA)
		}

		itemsB, err := store.Library(accountB)
		if err != nil {
			t.Fatalf("Library(account B): %v", err)
		}
		if len(itemsB) != 1 || itemsB[0].Slug != "account-b-story" {
			t.Fatalf("Library(account B) = %#v, want only account-b-story", itemsB)
		}
	})
}

func assertIntegrationStatus(t *testing.T, handler http.Handler, cookie *http.Cookie, wantUnlocked bool) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status response = %d; body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Unlocked bool `json:"unlocked"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if payload.Unlocked != wantUnlocked {
		t.Fatalf("status unlocked = %t, want %t", payload.Unlocked, wantUnlocked)
	}
}

func newAccountIntegrationStore(t *testing.T, databaseURL string) *Store {
	t.Helper()

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open Store database: %v", err)
	}
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		t.Fatalf("ping Store database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	return &Store{
		db:                      database,
		queryTimeout:            10 * time.Second,
		defaultProfileByAccount: map[string]string{},
	}
}

func setupAccountIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()

	statements := []string{
		`DROP TABLE IF EXISTS story_versions, stories, accounts CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE accounts (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name text NOT NULL DEFAULT 'Default',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE stories (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL,
			slug text NOT NULL,
			title text NOT NULL,
			author text,
			published_version_id uuid,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
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
			ADD CONSTRAINT stories_published_version_test_fkey
			FOREIGN KEY (published_version_id) REFERENCES story_versions(id) ON DELETE SET NULL`,
	}

	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("prepare disposable account schema: %v", err)
		}
	}
}

func resetAccountIntegrationData(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`TRUNCATE TABLE story_versions, stories, accounts CASCADE`); err != nil {
		t.Fatalf("reset disposable account data: %v", err)
	}
}
