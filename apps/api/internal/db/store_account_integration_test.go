package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/httpapi"
	"pandapages/api/internal/model"
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

	t.Run("default profile creation is concurrent and singular", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)

		const accountID = "33333333-3333-4333-8333-333333333333"
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Profiles')`, accountID); err != nil {
			t.Fatalf("insert profile account: %v", err)
		}

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
				ctx, cancel := store.ctx()
				defer cancel()
				results[i].id, results[i].err = store.getDefaultProfileID(ctx, accountID)
			}()
		}
		close(start)
		wg.Wait()

		wantID := results[0].id
		if results[0].err != nil || wantID == "" {
			t.Fatalf("first default resolution = %q / %v", wantID, results[0].err)
		}
		for i, result := range results {
			if result.err != nil {
				t.Errorf("default resolver %d: %v", i, result.err)
			} else if result.id != wantID {
				t.Errorf("default resolver %d id = %q, want %q", i, result.id, wantID)
			}
		}

		var profileCount, defaultCount, namedDefaultCount int
		if err := adminDB.QueryRow(`
			SELECT
				count(*),
				count(*) FILTER (WHERE is_default),
				count(*) FILTER (WHERE name = 'Default' AND is_default)
			FROM profiles
			WHERE account_id = $1
		`, accountID).Scan(&profileCount, &defaultCount, &namedDefaultCount); err != nil {
			t.Fatalf("count concurrent defaults: %v", err)
		}
		if profileCount != 1 || defaultCount != 1 || namedDefaultCount != 1 {
			t.Fatalf("concurrent profiles/defaults/named defaults = %d/%d/%d, want 1/1/1", profileCount, defaultCount, namedDefaultCount)
		}

	})

	t.Run("invalid default marker repair is concurrent and singular", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		const (
			accountID       = "34343434-3434-4343-8343-343434343434"
			markedProfile   = "34343434-1000-4000-8000-000000000001"
			otherProfile    = "34343434-1000-4000-8000-000000000002"
			progressStory   = "34343434-3000-4000-8000-000000000001"
			progressVersion = "34343434-4000-4000-8000-000000000001"
		)
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Concurrent repair')`, accountID); err != nil {
			t.Fatalf("insert repair account: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO profiles (id, account_id, name, is_default, created_at) VALUES
				($1, $3, 'Incorrect marker', true, '2026-01-01T00:00:00Z'),
				($2, $3, 'Unrelated reader', false, '2026-01-02T00:00:00Z')
		`, markedProfile, otherProfile, accountID); err != nil {
			t.Fatalf("insert repair profiles: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO stories (id, account_id, slug, title)
			VALUES ($1, $2, 'repair-progress', 'Repair progress')
		`, progressStory, accountID); err != nil {
			t.Fatalf("insert repair story: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_versions (id, story_id, version, markdown, rendered_html)
			VALUES ($1, $2, 1, 'repair body', '<p>repair body</p>')
		`, progressVersion, progressStory); err != nil {
			t.Fatalf("insert repair story version: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO reading_progress (account_id, profile_id, story_id, story_version_id, percent)
			VALUES ($1, $2, $3, $4, 0.37)
		`, accountID, markedProfile, progressStory, progressVersion); err != nil {
			t.Fatalf("insert repair progress: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO profile_settings (account_id, profile_id)
			VALUES ($1, $2)
		`, accountID, markedProfile); err != nil {
			t.Fatalf("insert repair settings: %v", err)
		}

		var originalCreatedAt time.Time
		if err := adminDB.QueryRow(`SELECT created_at FROM profiles WHERE id = $1`, markedProfile).Scan(&originalCreatedAt); err != nil {
			t.Fatalf("read marked profile timestamp: %v", err)
		}
		var originalProgressPercent float64
		if err := adminDB.QueryRow(`
			SELECT percent
			FROM reading_progress
			WHERE profile_id = $1 AND story_id = $2
		`, markedProfile, progressStory).Scan(&originalProgressPercent); err != nil {
			t.Fatalf("read repair progress before concurrent resolution: %v", err)
		}

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
				ctx, cancel := store.ctx()
				defer cancel()
				results[i].id, results[i].err = store.getDefaultProfileID(ctx, accountID)
			}()
		}
		close(start)
		wg.Wait()

		wantID := results[0].id
		if results[0].err != nil || wantID == "" {
			t.Fatalf("first repair resolution = %q / %v", wantID, results[0].err)
		}
		for i, result := range results {
			if result.err != nil {
				t.Errorf("repair resolver %d: %v", i, result.err)
			} else if result.id != wantID {
				t.Errorf("repair resolver %d id = %q, want %q", i, result.id, wantID)
			}
		}

		var defaultID, defaultAccountID string
		if err := adminDB.QueryRow(`
			SELECT id, account_id
			FROM profiles
			WHERE account_id = $1
			  AND name = 'Default'
			  AND is_default
		`, accountID).Scan(&defaultID, &defaultAccountID); err != nil {
			t.Fatalf("read repaired Default: %v", err)
		}
		if defaultID != wantID || defaultAccountID != accountID {
			t.Fatalf("repaired Default = %q/%q, want %q/%q", defaultID, defaultAccountID, wantID, accountID)
		}

		var totalProfiles, namedDefaults, activeDefaults, activeNamedProfiles int
		if err := adminDB.QueryRow(`
			SELECT
				count(*),
				count(*) FILTER (WHERE name = 'Default'),
				count(*) FILTER (WHERE name = 'Default' AND is_default),
				count(*) FILTER (WHERE name <> 'Default' AND is_default)
			FROM profiles
			WHERE account_id = $1
		`, accountID).Scan(&totalProfiles, &namedDefaults, &activeDefaults, &activeNamedProfiles); err != nil {
			t.Fatalf("count repaired profiles: %v", err)
		}
		if totalProfiles != 3 || namedDefaults != 1 || activeDefaults != 1 || activeNamedProfiles != 0 {
			t.Fatalf("repaired profiles/defaults/active defaults/active named = %d/%d/%d/%d, want 3/1/1/0", totalProfiles, namedDefaults, activeDefaults, activeNamedProfiles)
		}

		var markedName string
		var markedIsDefault bool
		var markedCreatedAt time.Time
		if err := adminDB.QueryRow(`
			SELECT name, is_default, created_at
			FROM profiles
			WHERE id = $1 AND account_id = $2
		`, markedProfile, accountID).Scan(&markedName, &markedIsDefault, &markedCreatedAt); err != nil {
			t.Fatalf("read repaired old marker: %v", err)
		}
		if markedName != "Incorrect marker" || markedIsDefault || !markedCreatedAt.Equal(originalCreatedAt) {
			t.Fatalf("old marker after repair = %q/default:%t/created:%s", markedName, markedIsDefault, markedCreatedAt)
		}

		var unrelatedName string
		var unrelatedIsDefault bool
		if err := adminDB.QueryRow(`
			SELECT name, is_default
			FROM profiles
			WHERE id = $1 AND account_id = $2
		`, otherProfile, accountID).Scan(&unrelatedName, &unrelatedIsDefault); err != nil {
			t.Fatalf("read unrelated profile: %v", err)
		}
		if unrelatedName != "Unrelated reader" || unrelatedIsDefault {
			t.Fatalf("unrelated profile after repair = %q/default:%t", unrelatedName, unrelatedIsDefault)
		}

		var progressAccountID, progressProfileID, progressStoryID, progressVersionID string
		var progressPercent float64
		if err := adminDB.QueryRow(`
			SELECT account_id, profile_id, story_id, story_version_id, percent
			FROM reading_progress
			WHERE profile_id = $1 AND story_id = $2
		`, markedProfile, progressStory).Scan(&progressAccountID, &progressProfileID, &progressStoryID, &progressVersionID, &progressPercent); err != nil {
			t.Fatalf("read repaired progress: %v", err)
		}
		if progressAccountID != accountID || progressProfileID != markedProfile || progressStoryID != progressStory || progressVersionID != progressVersion || progressPercent != originalProgressPercent || math.Abs(progressPercent-0.37) > 1e-6 {
			t.Fatalf("progress after repair = %q/%q/%q/%q/%v", progressAccountID, progressProfileID, progressStoryID, progressVersionID, progressPercent)
		}

		var settingsAccountID, settingsProfileID string
		if err := adminDB.QueryRow(`
			SELECT account_id, profile_id
			FROM profile_settings
			WHERE profile_id = $1
		`, markedProfile).Scan(&settingsAccountID, &settingsProfileID); err != nil {
			t.Fatalf("read repaired settings: %v", err)
		}
		if settingsAccountID != accountID || settingsProfileID != markedProfile {
			t.Fatalf("settings after repair = %q/%q", settingsAccountID, settingsProfileID)
		}

		var defaultProgress, defaultSettings int
		if err := adminDB.QueryRow(`
			SELECT
				(SELECT count(*) FROM reading_progress WHERE profile_id = $1),
				(SELECT count(*) FROM profile_settings WHERE profile_id = $1)
		`, defaultID).Scan(&defaultProgress, &defaultSettings); err != nil {
			t.Fatalf("read new Default relations: %v", err)
		}
		if defaultProgress != 0 || defaultSettings != 0 {
			t.Fatalf("new Default relations = progress:%d settings:%d, want 0/0", defaultProgress, defaultSettings)
		}
	})

	t.Run("missing Default creates one without repurposing named profiles", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)
		const (
			accountID = "33333333-3333-4333-8333-333333333333"
			oldestID  = "33333333-0000-4000-8000-000000000001"
			markedID  = "33333333-0000-4000-8000-000000000002"
		)
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Profiles')`, accountID); err != nil {
			t.Fatalf("insert profile account: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO profiles (id, account_id, name, is_default, created_at) VALUES
				($1, $3, 'Older named reader', false, '2026-01-01T00:00:00Z'),
				($2, $3, 'Incorrect marker', true, '2026-01-02T00:00:00Z')
		`, oldestID, markedID, accountID); err != nil {
			t.Fatalf("insert named profiles without Default: %v", err)
		}

		ctx, cancel := store.ctx()
		got, err := store.getDefaultProfileID(ctx, accountID)
		cancel()
		if err != nil {
			t.Fatalf("repair missing Default: %v", err)
		}
		if got == oldestID || got == markedID {
			t.Fatalf("repair selected existing named profile %q instead of creating Default", got)
		}

		var profiles, namedDefaults, activeNamedDefaults, activeNamedProfiles int
		if err := adminDB.QueryRow(`
			SELECT
				count(*),
				count(*) FILTER (WHERE name = 'Default'),
				count(*) FILTER (WHERE name = 'Default' AND is_default),
				count(*) FILTER (WHERE name <> 'Default' AND is_default)
			FROM profiles
			WHERE account_id = $1
		`, accountID).Scan(&profiles, &namedDefaults, &activeNamedDefaults, &activeNamedProfiles); err != nil {
			t.Fatalf("inspect Default repair: %v", err)
		}
		if profiles != 3 || namedDefaults != 1 || activeNamedDefaults != 1 || activeNamedProfiles != 0 {
			t.Fatalf("Default repair profiles/defaults/active defaults/active named = %d/%d/%d/%d, want 3/1/1/0", profiles, namedDefaults, activeNamedDefaults, activeNamedProfiles)
		}
	})

	t.Run("valid Default lookup does not wait for the account repair lock", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)
		const accountID = "33333333-3333-4333-8333-333333333333"
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Profiles')`, accountID); err != nil {
			t.Fatalf("insert profile account: %v", err)
		}
		if _, err := adminDB.Exec(`INSERT INTO profiles (account_id, name, is_default) VALUES ($1, 'Default', true)`, accountID); err != nil {
			t.Fatalf("insert valid Default profile: %v", err)
		}

		lockTx, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("begin account lock transaction: %v", err)
		}
		defer func() { _ = lockTx.Rollback() }()
		if _, err := lockTx.Exec(`SELECT id FROM accounts WHERE id = $1 FOR UPDATE`, accountID); err != nil {
			t.Fatalf("lock account row: %v", err)
		}

		type result struct {
			id  string
			err error
		}
		resultCh := make(chan result, 1)
		go func() {
			ctx, cancel := store.ctx()
			defer cancel()
			id, err := store.getDefaultProfileID(ctx, accountID)
			resultCh <- result{id: id, err: err}
		}()

		select {
		case result := <-resultCh:
			if result.err != nil || result.id == "" {
				t.Fatalf("non-locking Default lookup = %q / %v", result.id, result.err)
			}
		case <-time.After(time.Second):
			t.Fatal("valid Default lookup waited for the account repair lock")
		}
	})

	t.Run("account deletion is restricted while owned rows exist", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		const accountID = "44444444-4444-4444-8444-444444444444"
		if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Owned')`, accountID); err != nil {
			t.Fatalf("insert owned account: %v", err)
		}
		if _, err := adminDB.Exec(`INSERT INTO profiles (account_id, name, is_default) VALUES ($1, 'Reader', true)`, accountID); err != nil {
			t.Fatalf("insert owned profile: %v", err)
		}
		if _, err := adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID); err == nil {
			t.Fatal("owned account deletion succeeded")
		}
		if _, err := adminDB.Exec(`DELETE FROM profiles WHERE account_id = $1`, accountID); err != nil {
			t.Fatalf("delete owned profile explicitly: %v", err)
		}
		if _, err := adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
			t.Fatalf("delete empty account: %v", err)
		}
	})

	t.Run("settings writes and active selections remain account scoped", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)
		const (
			accountA = "55555555-5555-4555-8555-555555555555"
			accountB = "66666666-6666-4666-8666-666666666666"
			profileA = "55555555-0000-4000-8000-000000000001"
			profileB = "66666666-0000-4000-8000-000000000001"
		)
		if _, err := adminDB.Exec(`
			INSERT INTO accounts (id, name) VALUES ($1, 'Settings A'), ($2, 'Settings B')
		`, accountA, accountB); err != nil {
			t.Fatalf("insert settings accounts: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO profiles (id, account_id, name, is_default) VALUES
				($1, $3, 'Default', true),
				($2, $4, 'Default', true)
		`, profileA, profileB, accountA, accountB); err != nil {
			t.Fatalf("insert settings profiles: %v", err)
		}

		empty, err := store.SettingsGet(accountA)
		if err != nil {
			t.Fatalf("SettingsGet empty: %v", err)
		}
		if empty.Child.ID != "" || empty.Prompt.ID != "" {
			t.Fatalf("empty settings = %#v", empty)
		}
		var initialSettingsAccount string
		if err := adminDB.QueryRow(`SELECT account_id FROM profile_settings WHERE profile_id = $1`, profileA).Scan(&initialSettingsAccount); err != nil {
			t.Fatalf("read empty settings ownership: %v", err)
		}
		if initialSettingsAccount != accountA {
			t.Fatalf("empty settings account = %q, want %q", initialSettingsAccount, accountA)
		}

		savedA, err := store.SettingsPut(accountA, model.SettingsUpsert{
			Child: model.ChildProfile{
				Name:          "Reader child A",
				AgeMonths:     84,
				Interests:     []string{"pandas"},
				Sensitivities: []string{"storms"},
			},
			Prompt: model.PromptProfile{
				Name:          "Prompt A",
				SchemaVersion: 2,
				Rules:         json.RawMessage(`{"tone":"calm"}`),
			},
		})
		if err != nil {
			t.Fatalf("SettingsPut account A: %v", err)
		}
		if savedA.Child.ID == "" || savedA.Prompt.ID == "" || savedA.Child.Name != "Reader child A" || savedA.Prompt.Name != "Prompt A" {
			t.Fatalf("saved account A settings = %#v", savedA)
		}

		var childAccount, promptAccount, settingsAccount string
		if err := adminDB.QueryRow(`
			SELECT child.account_id, prompt.account_id, settings.account_id
			FROM profile_settings AS settings
			JOIN child_profiles AS child ON child.id = settings.active_child_profile_id
			JOIN prompt_profiles AS prompt ON prompt.id = settings.active_prompt_profile_id
			WHERE settings.profile_id = $1
		`, profileA).Scan(&childAccount, &promptAccount, &settingsAccount); err != nil {
			t.Fatalf("read account A settings ownership: %v", err)
		}
		if childAccount != accountA || promptAccount != accountA || settingsAccount != accountA {
			t.Fatalf("account A settings ownership = %q/%q/%q", childAccount, promptAccount, settingsAccount)
		}

		// Foreign IDs do not disclose or attach the other account's rows. The
		// existing contract creates new account-owned configuration instead.
		savedB, err := store.SettingsPut(accountB, model.SettingsUpsert{
			Child: model.ChildProfile{
				ID:            savedA.Child.ID,
				Name:          "Reader child B",
				AgeMonths:     96,
				Interests:     []string{"space"},
				Sensitivities: []string{},
			},
			Prompt: model.PromptProfile{
				ID:            savedA.Prompt.ID,
				Name:          "Prompt B",
				SchemaVersion: 3,
				Rules:         json.RawMessage(`{"tone":"bright"}`),
			},
		})
		if err != nil {
			t.Fatalf("SettingsPut account B with foreign IDs: %v", err)
		}
		if savedB.Child.ID == "" || savedB.Prompt.ID == "" || savedB.Child.ID == savedA.Child.ID || savedB.Prompt.ID == savedA.Prompt.ID {
			t.Fatalf("account B reused account A settings IDs: A=%#v B=%#v", savedA, savedB)
		}

		if _, err := adminDB.Exec(`
			UPDATE profile_settings
			SET active_child_profile_id = $2
			WHERE profile_id = $1
		`, profileA, savedB.Child.ID); err == nil {
			t.Fatal("cross-account child selection update succeeded")
		}
		if _, err := adminDB.Exec(`
			UPDATE profile_settings
			SET active_prompt_profile_id = $2
			WHERE profile_id = $1
		`, profileA, savedB.Prompt.ID); err == nil {
			t.Fatal("cross-account prompt selection update succeeded")
		}
		if _, err := adminDB.Exec(`UPDATE profile_settings SET account_id = $2 WHERE profile_id = $1`, profileA, accountB); err == nil {
			t.Fatal("settings ownership update succeeded")
		}

		if _, err := adminDB.Exec(`DELETE FROM child_profiles WHERE id = $1`, savedA.Child.ID); err != nil {
			t.Fatalf("delete selected child profile: %v", err)
		}
		if _, err := adminDB.Exec(`DELETE FROM prompt_profiles WHERE id = $1`, savedA.Prompt.ID); err != nil {
			t.Fatalf("delete selected prompt profile: %v", err)
		}
		var childID, promptID sql.NullString
		if err := adminDB.QueryRow(`
			SELECT active_child_profile_id, active_prompt_profile_id, account_id
			FROM profile_settings
			WHERE profile_id = $1
		`, profileA).Scan(&childID, &promptID, &settingsAccount); err != nil {
			t.Fatalf("read settings after selected-row deletion: %v", err)
		}
		if childID.Valid || promptID.Valid || settingsAccount != accountA {
			t.Fatalf("settings after selected-row deletion = child:%v prompt:%v account:%q", childID, promptID, settingsAccount)
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
		// Library first-use provisioning created the account's explicit default.
		// Simulate a later controlled account-deletion workflow by removing owned
		// child rows explicitly before deleting the now-empty disposable account.
		if _, err := adminDB.Exec(`DELETE FROM profiles WHERE account_id = $1`, claims.AccountID); err != nil {
			t.Fatalf("remove session account profiles: %v", err)
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

	t.Run("library read model remains published-version and account scoped", func(t *testing.T) {
		resetAccountIntegrationData(t, adminDB)
		store := newAccountIntegrationStore(t, databaseURL)

		const (
			accountA           = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			accountB           = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
			accountC           = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
			accountD           = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
			profileA           = "aaaaaaaa-2000-4000-8000-000000000001"
			profileAOther      = "aaaaaaaa-2000-4000-8000-000000000002"
			profileB           = "bbbbbbbb-2000-4000-8000-000000000001"
			storyA             = "aaaaaaaa-0000-4000-8000-000000000001"
			noProgressStoryA   = "aaaaaaaa-0000-4000-8000-000000000002"
			draftStoryA        = "aaaaaaaa-0000-4000-8000-000000000003"
			unpublishedStoryA  = "aaaaaaaa-0000-4000-8000-000000000004"
			noPointerStoryA    = "aaaaaaaa-0000-4000-8000-000000000005"
			crossPointerStoryA = "aaaaaaaa-0000-4000-8000-000000000006"
			samePointerStoryA  = "aaaaaaaa-0000-4000-8000-000000000007"
			storyB             = "bbbbbbbb-0000-4000-8000-000000000001"
			versionA1          = "aaaaaaaa-1000-4000-8000-000000000001"
			versionA2          = "aaaaaaaa-1000-4000-8000-000000000002"
			noProgressVersionA = "aaaaaaaa-1000-4000-8000-000000000003"
			draftVersionA      = "aaaaaaaa-1000-4000-8000-000000000004"
			unpublishedVersion = "aaaaaaaa-1000-4000-8000-000000000005"
			versionB           = "bbbbbbbb-1000-4000-8000-000000000001"
			missingPointerC    = "cccccccc-0000-4000-8000-000000000001"
			crossPointerC      = "cccccccc-0000-4000-8000-000000000002"
			validStoryD        = "dddddddd-0000-4000-8000-000000000001"
			corruptStoryD      = "dddddddd-0000-4000-8000-000000000002"
			validVersionD      = "dddddddd-1000-4000-8000-000000000001"
			corruptVersionD    = "dddddddd-1000-4000-8000-000000000002"
		)
		progressTime := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)

		if _, err := adminDB.Exec(
			`INSERT INTO accounts (id, name) VALUES ($1, 'Account A'), ($2, 'Account B')`,
			accountA, accountB,
		); err != nil {
			t.Fatalf("insert library accounts: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO profiles (id, account_id, name, is_default, created_at) VALUES
				($1, $3, 'Default', true, '2026-07-19T10:00:00Z'),
				($2, $4, 'Default', true, '2026-07-19T10:00:00Z'),
				($5, $3, 'Other', false, '2026-07-19T09:00:00Z')
		`, profileA, profileB, accountA, accountB, profileAOther); err != nil {
			t.Fatalf("insert library profiles: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO stories (
				id, account_id, slug, title, author, language, is_published, created_at, updated_at
			) VALUES
				($3, $1, 'shared-story', 'Unpublished draft title', 'Unpublished draft author', 'fr', true, $9, $9),
				($4, $1, 'no-progress', 'Legacy metadata title', NULL, 'en-GB', true, $9, $9),
				($5, $1, 'draft-only', 'Draft only', NULL, 'en-GB', false, $9, $9),
				($6, $1, 'unpublished-pointer', 'Unpublished pointer', NULL, 'en-GB', false, $9, $9),
				($7, $1, 'published-without-pointer', 'No pointer', NULL, 'en-GB', true, $9, $9),
				($8, $1, 'cross-story-pointer', 'Cross pointer', NULL, 'en-GB', true, $9, $9),
				($10, $1, 'same-account-cross-story-pointer', 'Same-account cross pointer', NULL, 'en-GB', true, $9, $9),
				($11, $2, 'shared-story', 'Account B story', 'Account B author', 'cy', true, $9, $9)
		`,
			accountA, accountB, storyA, noProgressStoryA, draftStoryA, unpublishedStoryA,
			noPointerStoryA, crossPointerStoryA, progressTime.Add(-time.Hour), samePointerStoryA, storyB,
		); err != nil {
			t.Fatalf("insert account-scoped stories: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_versions (id, story_id, version, frontmatter, rendered_html) VALUES
				($1, $7, 1, '{"title":"Published version one","author":"Published author","language":"en-GB"}', '<p>A1</p>'),
				($2, $7, 2, '{"title":"Published version two","author":null,"language":"en"}', '<p>A2</p>'),
				($3, $8, 1, '{"title":"No progress published","language":"en-GB"}', '<p>No progress</p>'),
				($4, $9, 1, '{"title":"Draft version","author":"Draft author","language":"en-GB"}', '<p>Draft</p>'),
				($5, $10, 1, '{"title":"Hidden version","author":"Hidden author","language":"en-GB"}', '<p>Hidden</p>'),
				($6, $11, 1, '{"title":"Account B published","author":"Account B version author","language":"cy"}', '<p>B</p>')
		`,
			versionA1, versionA2, noProgressVersionA, draftVersionA, unpublishedVersion, versionB,
			storyA, noProgressStoryA, draftStoryA, unpublishedStoryA, storyB,
		); err != nil {
			t.Fatalf("insert account-scoped story versions: %v", err)
		}

		keyA := strings.Repeat("a", 64)
		keyB := strings.Repeat("b", 64)
		chapterKey := strings.Repeat("c", 64)
		keyD := strings.Repeat("d", 64)
		keyE := strings.Repeat("e", 64)
		if _, err := adminDB.Exec(`
			INSERT INTO story_segments (
				story_version_id, ordinal, segment_kind, heading_level,
				content_key, content_occurrence, chapter_key, chapter_occurrence, word_count
			) VALUES
				($1, 1, 'heading', 1, $7, 1, NULL, NULL, 3),
				($1, 2, 'paragraph', NULL, $8, 1, NULL, NULL, 5),
				($1, 3, 'heading', 2, $9, 1, $9, 1, 2),
				($1, 4, 'paragraph', NULL, $10, 1, $9, 1, 7),
				($1, 5, 'heading', 3, $11, 1, $9, 1, 2),
				($1, 6, 'heading', 2, $9, 2, $9, 2, 2),
				($2, 1, 'heading', 1, $7, 1, NULL, NULL, 4),
				($2, 2, 'paragraph', NULL, $8, 1, NULL, NULL, 10),
				($2, 3, 'heading', 2, $11, 1, $11, 1, 3),
				($2, 4, 'paragraph', NULL, $10, 1, $11, 1, 8),
				($3, 1, 'paragraph', NULL, $7, 1, NULL, NULL, 9),
				($4, 1, 'paragraph', NULL, $7, 1, NULL, NULL, 999),
				($5, 1, 'paragraph', NULL, $7, 1, NULL, NULL, 888),
				($6, 1, 'heading', 2, $11, 1, $11, 1, 6)
		`,
			versionA1, versionA2, noProgressVersionA, draftVersionA, unpublishedVersion, versionB,
			keyA, keyB, chapterKey, keyD, keyE,
		); err != nil {
			t.Fatalf("insert published and draft segments: %v", err)
		}
		if _, err := adminDB.Exec(`
			UPDATE stories
			SET published_version_id = CASE id
				WHEN $1 THEN $7::uuid
				WHEN $2 THEN $8::uuid
				WHEN $3 THEN $9::uuid
				WHEN $4 THEN $10::uuid
				WHEN $5 THEN $7::uuid
				WHEN $6 THEN $10::uuid
			END
			WHERE id IN ($1, $2, $3, $4, $5, $6)
		`, storyA, noProgressStoryA, unpublishedStoryA, crossPointerStoryA, samePointerStoryA, storyB,
			versionA1, noProgressVersionA, unpublishedVersion, versionB); err != nil {
			t.Fatalf("publish account-scoped stories: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO reading_progress (
				account_id, profile_id, story_id, story_version_id, percent, updated_at
			) VALUES
				($11, $1, $4, $7, 0.42, $10),
				($12, $2, $5, $8, 0.73, $10),
				($11, $3, $4, $7, 0.99, $10 + interval '1 hour'),
				($11, $3, $6, $9, 0.88, $10 + interval '2 hours')
		`,
			profileA, profileB, profileAOther,
			storyA, storyB, noProgressStoryA,
			versionA1, versionB, noProgressVersionA,
			progressTime, accountA, accountB,
		); err != nil {
			t.Fatalf("insert account-scoped progress: %v", err)
		}

		libraryA, err := store.Library(accountA)
		if err != nil {
			t.Fatalf("Library(account A): %v", err)
		}
		itemsA := libraryA.Items
		if libraryA.UnavailableItemCount != 3 {
			t.Fatalf("Library(account A) unavailable count = %d, want 3", libraryA.UnavailableItemCount)
		}
		if len(itemsA) != 2 || itemsA[0].Slug != "no-progress" || itemsA[1].Slug != "shared-story" {
			t.Fatalf("Library(account A) ordering/scope = %#v", itemsA)
		}
		if itemsA[0].Title != "No progress published" || itemsA[0].Language != "en-GB" ||
			itemsA[0].Author != nil || itemsA[0].PublishedVersion != 1 ||
			itemsA[0].WordCount != 9 || itemsA[0].ChapterCount != 0 || itemsA[0].Progress != nil {
			t.Fatalf("version-owned/no-progress item = %#v", itemsA[0])
		}
		current := itemsA[1]
		if current.Title != "Published version one" || current.Author == nil || *current.Author != "Published author" ||
			current.Language != "en-GB" || current.PublishedVersion != 1 ||
			current.WordCount != 21 || current.ChapterCount != 2 {
			t.Fatalf("current published-version item = %#v", current)
		}
		if current.Progress == nil || current.Progress.Version != 1 ||
			math.Abs(current.Progress.Percent-0.42) > 0.0001 ||
			!current.Progress.UpdatedAt.Equal(progressTime) || !current.Progress.IsCurrentVersion {
			t.Fatalf("current-version progress = %#v", current.Progress)
		}

		// Distinguish an empty account from an account whose complete shelf is
		// quarantined. A foreign pointer is counted, but its metadata is never read.
		if _, err := adminDB.Exec(
			`INSERT INTO accounts (id, name) VALUES ($1, 'All invalid'), ($2, 'Empty')`,
			accountC, accountD,
		); err != nil {
			t.Fatalf("insert all-invalid and empty accounts: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO stories (
				id, account_id, slug, title, language, is_published, published_version_id, created_at, updated_at
			) VALUES
				($1, $3, 'missing-pointer', 'Mutable missing pointer', 'fr', true, NULL, now(), now()),
				($2, $3, 'foreign-pointer', 'Mutable foreign pointer', 'fr', true, $4, now(), now())
		`, missingPointerC, crossPointerC, accountC, versionB); err != nil {
			t.Fatalf("insert all-invalid Library candidates: %v", err)
		}
		allInvalid, err := store.Library(accountC)
		if err != nil {
			t.Fatalf("Library(all-invalid account): %v", err)
		}
		if len(allInvalid.Items) != 0 || allInvalid.UnavailableItemCount != 2 {
			t.Fatalf("all-invalid Library = %#v", allInvalid)
		}
		allInvalidJSON, err := json.Marshal(allInvalid)
		if err != nil {
			t.Fatalf("encode all-invalid Library: %v", err)
		}
		if strings.Contains(string(allInvalidJSON), "Account B published") || strings.Contains(string(allInvalidJSON), `"cy"`) {
			t.Fatalf("foreign immutable metadata crossed accounts: %s", allInvalidJSON)
		}
		emptyAccount, err := store.Library(accountD)
		if err != nil {
			t.Fatalf("Library(empty account): %v", err)
		}
		if len(emptyAccount.Items) != 0 || emptyAccount.UnavailableItemCount != 0 {
			t.Fatalf("empty-account Library = %#v", emptyAccount)
		}

		// A corrupt immutable version quarantines only itself: a healthy sibling
		// remains available and mutable story metadata is never used as fallback.
		if _, err := adminDB.Exec(`
			INSERT INTO stories (id, account_id, slug, title, language, is_published, created_at, updated_at) VALUES
				($1, $3, 'valid-sibling', 'Mutable valid title', 'fr', true, now(), now()),
				($2, $3, 'corrupt-sibling', 'Mutable corrupt fallback', 'fr', true, now(), now())
		`, validStoryD, corruptStoryD, accountD); err != nil {
			t.Fatalf("insert partial-library stories: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_versions (id, story_id, version, frontmatter, rendered_html) VALUES
				($1, $3, 1, '{"title":"Valid immutable sibling","language":"en-GB"}', '<p>Valid</p>'),
				($2, $4, 1, '{"language":"en-GB"}', '<p>Corrupt metadata</p>')
		`, validVersionD, corruptVersionD, validStoryD, corruptStoryD); err != nil {
			t.Fatalf("insert partial-library versions: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_segments (
				story_version_id, ordinal, segment_kind, content_key, content_occurrence, word_count
			) VALUES
				($1, 1, 'paragraph', $3, 1, 4),
				($2, 1, 'paragraph', $3, 1, 5)
		`, validVersionD, corruptVersionD, keyA); err != nil {
			t.Fatalf("insert partial-library segments: %v", err)
		}
		if _, err := adminDB.Exec(`
			UPDATE stories
			SET published_version_id = CASE id WHEN $1 THEN $3::uuid WHEN $2 THEN $4::uuid END
			WHERE id IN ($1, $2)
		`, validStoryD, corruptStoryD, validVersionD, corruptVersionD); err != nil {
			t.Fatalf("set partial-library pointers: %v", err)
		}
		oneValidOneCorrupt, err := store.Library(accountD)
		if err != nil {
			t.Fatalf("Library(one valid and one corrupt): %v", err)
		}
		if len(oneValidOneCorrupt.Items) != 1 || oneValidOneCorrupt.UnavailableItemCount != 1 ||
			oneValidOneCorrupt.Items[0].Slug != "valid-sibling" || oneValidOneCorrupt.Items[0].Title != "Valid immutable sibling" {
			t.Fatalf("one-valid/one-corrupt Library = %#v", oneValidOneCorrupt)
		}
		oneValidOneCorruptJSON, err := json.Marshal(oneValidOneCorrupt)
		if err != nil {
			t.Fatalf("encode one-valid/one-corrupt Library: %v", err)
		}
		if strings.Contains(string(oneValidOneCorruptJSON), "Mutable corrupt fallback") {
			t.Fatalf("partial Library exposed mutable corrupt fallback: %s", oneValidOneCorruptJSON)
		}

		// A historical version with no Reader segments is quarantined from the
		// shelf and direct Reader access instead of producing a broken Read action.
		const (
			zeroStory   = "aaaaaaaa-0000-4000-8000-000000000009"
			zeroVersion = "aaaaaaaa-1000-4000-8000-000000000009"
		)
		if _, err := adminDB.Exec(`
			INSERT INTO stories (
				id, account_id, slug, title, language, is_published, created_at, updated_at
			) VALUES ($1, $2, 'historical-empty', 'Mutable draft title', 'fr', false, now(), now())
		`, zeroStory, accountA); err != nil {
			t.Fatalf("insert historical zero-segment publication: %v", err)
		}
		if _, err := adminDB.Exec(`
			INSERT INTO story_versions (id, story_id, version, frontmatter, rendered_html)
			VALUES ($1, $2, 1, '{"title":"Historical empty","language":"en-GB"}', '')
		`, zeroVersion, zeroStory); err != nil {
			t.Fatalf("insert historical zero-segment version: %v", err)
		}
		if err := store.AdminPublish(accountA, "historical-empty", zeroVersion); !errors.Is(err, model.ErrAdminPublishInvalid) {
			t.Fatalf("zero-segment AdminPublish error = %v", err)
		}
		var (
			isPublished bool
			publishedID sql.NullString
		)
		if err := adminDB.QueryRow(`SELECT is_published, published_version_id FROM stories WHERE id = $1`, zeroStory).Scan(&isPublished, &publishedID); err != nil {
			t.Fatalf("read rejected zero-segment publication state: %v", err)
		}
		if isPublished || publishedID.Valid {
			t.Fatalf("rejected zero-segment publication mutated story: published=%t pointer=%#v", isPublished, publishedID)
		}
		if _, err := adminDB.Exec(`UPDATE stories SET is_published = true, published_version_id = $2 WHERE id = $1`, zeroStory, zeroVersion); err != nil {
			t.Fatalf("publish historical zero-segment version: %v", err)
		}
		emptyQuarantine, err := store.Library(accountA)
		if err != nil {
			t.Fatalf("Library(account A) with historical empty story: %v", err)
		}
		if emptyQuarantine.UnavailableItemCount != 4 || len(emptyQuarantine.Items) != 2 {
			t.Fatalf("historical empty quarantine = %#v", emptyQuarantine)
		}
		if _, err := store.ReaderStory(accountA, "historical-empty"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("historical empty ReaderStory error = %v, want sql.ErrNoRows", err)
		}
		if _, err := adminDB.Exec(`UPDATE stories SET published_version_id = NULL WHERE id = $1`, zeroStory); err != nil {
			t.Fatalf("clear historical zero-segment pointer: %v", err)
		}
		if _, err := adminDB.Exec(`DELETE FROM stories WHERE id = $1`, zeroStory); err != nil {
			t.Fatalf("remove historical zero-segment publication: %v", err)
		}

		// A newer draft already changed the mutable story columns above. If the
		// immutable published version is incomplete, quarantine only that item
		// instead of exposing draft metadata or disabling the valid shelf.
		if _, err := adminDB.Exec(`
			UPDATE story_versions
			SET frontmatter = '{"author":"Published author","language":"en-GB"}'::jsonb
			WHERE id = $1
		`, versionA1); err != nil {
			t.Fatalf("make published metadata incomplete: %v", err)
		}
		partial, err := store.Library(accountA)
		if err != nil {
			t.Fatalf("Library(account A) with corrupt immutable metadata: %v", err)
		}
		if partial.UnavailableItemCount != 4 || len(partial.Items) != 1 || partial.Items[0].Slug != "no-progress" {
			t.Fatalf("partial Library with corrupt metadata = %#v", partial)
		}
		for _, item := range partial.Items {
			if item.Title == "Unpublished draft title" || item.Author != nil && *item.Author == "Unpublished draft author" {
				t.Fatalf("partial Library exposed mutable draft metadata: %#v", item)
			}
		}
		if _, err := adminDB.Exec(`
			UPDATE story_versions
			SET frontmatter = '{"title":"Published version one","author":"Published author","language":"en-GB"}'::jsonb
			WHERE id = $1
		`, versionA1); err != nil {
			t.Fatalf("restore published metadata: %v", err)
		}

		libraryB, err := store.Library(accountB)
		if err != nil {
			t.Fatalf("Library(account B): %v", err)
		}
		itemsB := libraryB.Items
		if libraryB.UnavailableItemCount != 0 {
			t.Fatalf("Library(account B) unavailable count = %d, want 0", libraryB.UnavailableItemCount)
		}
		if len(itemsB) != 1 || itemsB[0].Slug != "shared-story" || itemsB[0].Title != "Account B published" ||
			itemsB[0].Progress == nil || itemsB[0].Progress.Version != 1 ||
			math.Abs(itemsB[0].Progress.Percent-0.73) > 0.0001 || !itemsB[0].Progress.IsCurrentVersion {
			t.Fatalf("Library(account B) = %#v", itemsB)
		}

		if _, err := adminDB.Exec(`
			UPDATE stories
			SET published_version_id = $2, updated_at = updated_at
			WHERE id = $1
		`, storyA, versionA2); err != nil {
			t.Fatalf("republish account A story: %v", err)
		}
		updatedLibrary, err := store.Library(accountA)
		if err != nil {
			t.Fatalf("Library(account A) after republish: %v", err)
		}
		updatedItems := updatedLibrary.Items
		if updatedLibrary.UnavailableItemCount != 3 {
			t.Fatalf("republished Library unavailable count = %d, want 3", updatedLibrary.UnavailableItemCount)
		}
		updated := updatedItems[1]
		if updated.Title != "Published version two" || updated.Author != nil || updated.Language != "en" ||
			updated.PublishedVersion != 2 || updated.WordCount != 25 || updated.ChapterCount != 1 {
			t.Fatalf("republished item = %#v", updated)
		}
		if updated.Progress == nil || updated.Progress.Version != 1 || updated.Progress.IsCurrentVersion ||
			math.Abs(updated.Progress.Percent-0.42) > 0.0001 || !updated.Progress.UpdatedAt.Equal(progressTime) {
			t.Fatalf("old-version progress = %#v", updated.Progress)
		}

		if _, err := adminDB.Exec(`UPDATE story_segments SET word_count = -1 WHERE story_version_id = $1 AND ordinal = 1`, versionA2); err != nil {
			t.Fatalf("corrupt aggregate fixture: %v", err)
		}
		invalidAggregate, err := store.Library(accountA)
		if err != nil || invalidAggregate.UnavailableItemCount != 4 || len(invalidAggregate.Items) != 1 {
			t.Fatalf("malformed aggregate quarantine = %#v / %v", invalidAggregate, err)
		}
		if _, err := adminDB.Exec(`UPDATE story_segments SET word_count = 4 WHERE story_version_id = $1 AND ordinal = 1`, versionA2); err != nil {
			t.Fatalf("restore aggregate fixture: %v", err)
		}

		if _, err := adminDB.Exec(`UPDATE story_segments SET chapter_occurrence = 2 WHERE story_version_id = $1 AND ordinal = 4`, versionA2); err != nil {
			t.Fatalf("corrupt chapter propagation fixture: %v", err)
		}
		invalidIdentity, err := store.Library(accountA)
		if err != nil || invalidIdentity.UnavailableItemCount != 4 || len(invalidIdentity.Items) != 1 {
			t.Fatalf("malformed identity quarantine = %#v / %v", invalidIdentity, err)
		}
		if _, err := store.ReaderStory(accountA, "shared-story"); err == nil || !strings.Contains(err.Error(), "segment identities") {
			t.Fatalf("ReaderStory malformed identity error = %v", err)
		}
		if _, err := adminDB.Exec(`UPDATE story_segments SET chapter_occurrence = 1 WHERE story_version_id = $1 AND ordinal = 4`, versionA2); err != nil {
			t.Fatalf("restore chapter propagation fixture: %v", err)
		}

		if _, err := adminDB.Exec(`UPDATE reading_progress SET percent = 1.5 WHERE profile_id = $1 AND story_id = $2`, profileA, storyA); err != nil {
			t.Fatalf("corrupt progress fixture: %v", err)
		}
		invalidProgress, err := store.Library(accountA)
		if err != nil || invalidProgress.UnavailableItemCount != 4 || len(invalidProgress.Items) != 1 {
			t.Fatalf("malformed progress quarantine = %#v / %v", invalidProgress, err)
		}
		if _, err := adminDB.Exec(`UPDATE reading_progress SET percent = 0.42 WHERE profile_id = $1 AND story_id = $2`, profileA, storyA); err != nil {
			t.Fatalf("restore progress fixture: %v", err)
		}

		if _, err := adminDB.Exec(`UPDATE reading_progress SET story_version_id = $3 WHERE profile_id = $1 AND story_id = $2`, profileA, storyA, versionB); err == nil {
			t.Fatal("cross-story progress version update succeeded")
		}
		var retainedVersionID string
		if err := adminDB.QueryRow(`SELECT story_version_id FROM reading_progress WHERE profile_id = $1 AND story_id = $2`, profileA, storyA).Scan(&retainedVersionID); err != nil {
			t.Fatalf("read progress after rejected cross-story update: %v", err)
		}
		if retainedVersionID != versionA1 {
			t.Fatalf("progress version after rejected update = %q, want %q", retainedVersionID, versionA1)
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
		db:           database,
		queryTimeout: 10 * time.Second,
	}
}

func setupAccountIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()

	statements := []string{
		`DROP TABLE IF EXISTS profile_settings, reading_progress, story_segments, story_versions, stories, child_profiles, prompt_profiles, profiles, accounts CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE accounts (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name text NOT NULL DEFAULT 'Default',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE profiles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
			name text NOT NULL,
			is_default boolean NOT NULL DEFAULT false,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (account_id, name),
			UNIQUE (id, account_id)
		)`,
		`CREATE UNIQUE INDEX profiles_one_default_per_account_idx
			ON profiles (account_id) WHERE is_default`,
		`CREATE TABLE child_profiles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
			name text,
			age_months integer NOT NULL,
			interests jsonb NOT NULL DEFAULT '[]'::jsonb,
			sensitivities jsonb NOT NULL DEFAULT '[]'::jsonb,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (id, account_id)
		)`,
		`CREATE TABLE prompt_profiles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
			name text NOT NULL,
			rules jsonb NOT NULL DEFAULT '{}'::jsonb,
			schema_version integer NOT NULL DEFAULT 1,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (id, account_id)
		)`,
		`CREATE TABLE profile_settings (
			profile_id uuid PRIMARY KEY,
			account_id uuid NOT NULL,
			active_child_profile_id uuid,
			active_prompt_profile_id uuid,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			FOREIGN KEY (profile_id, account_id)
				REFERENCES profiles(id, account_id) ON DELETE CASCADE,
			FOREIGN KEY (active_child_profile_id, account_id)
				REFERENCES child_profiles(id, account_id)
				ON DELETE SET NULL (active_child_profile_id),
			FOREIGN KEY (active_prompt_profile_id, account_id)
				REFERENCES prompt_profiles(id, account_id)
				ON DELETE SET NULL (active_prompt_profile_id)
		)`,
		`CREATE TABLE stories (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
			slug text NOT NULL,
			title text NOT NULL,
			author text,
			language text NOT NULL DEFAULT 'en-GB',
			source jsonb NOT NULL DEFAULT '{}'::jsonb,
			rights jsonb NOT NULL DEFAULT '{}'::jsonb,
			is_published boolean NOT NULL DEFAULT false,
			draft_version_id uuid,
			published_version_id uuid,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (account_id, slug),
			UNIQUE (id, account_id)
		)`,
		`CREATE TABLE story_versions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			version integer NOT NULL,
			frontmatter jsonb NOT NULL DEFAULT '{}'::jsonb,
			markdown text NOT NULL DEFAULT '',
			rendered_html text NOT NULL,
			content_hash text NOT NULL DEFAULT '',
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (story_id, version),
			UNIQUE (id, story_id)
		)`,
		`ALTER TABLE stories
			ADD CONSTRAINT stories_draft_version_test_fkey
			FOREIGN KEY (draft_version_id) REFERENCES story_versions(id) ON DELETE SET NULL`,
		`ALTER TABLE stories
			ADD CONSTRAINT stories_published_version_test_fkey
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
			markdown text NOT NULL DEFAULT '',
			rendered_html text NOT NULL DEFAULT '',
			word_count integer NOT NULL,
			UNIQUE (story_version_id, ordinal)
		)`,
		`CREATE TABLE reading_progress (
			account_id uuid NOT NULL,
			profile_id uuid NOT NULL,
			story_id uuid NOT NULL,
			story_version_id uuid NOT NULL,
			percent real NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (profile_id, story_id),
			FOREIGN KEY (profile_id, account_id)
				REFERENCES profiles(id, account_id) ON DELETE CASCADE,
			FOREIGN KEY (story_id, account_id)
				REFERENCES stories(id, account_id) ON DELETE CASCADE,
			FOREIGN KEY (story_version_id, story_id)
				REFERENCES story_versions(id, story_id)
		)`,
	}

	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("prepare disposable account schema: %v", err)
		}
	}
}

func resetAccountIntegrationData(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`TRUNCATE TABLE profile_settings, reading_progress, story_segments, story_versions, stories, child_profiles, prompt_profiles, profiles, accounts CASCADE`); err != nil {
		t.Fatalf("reset disposable account data: %v", err)
	}
}
