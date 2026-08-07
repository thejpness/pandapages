package db

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/model"
)

const (
	identityIntegrationURLVar   = "PP_IDENTITY_STORE_TEST_DATABASE_URL"
	identityIntegrationGuardVar = "PP_IDENTITY_STORE_TEST_DISPOSABLE"
	identityIntegrationDBName   = "pandapages_identity_store_test"
)

func TestIdentityStoreIntegration(t *testing.T) {
	if os.Getenv(identityIntegrationGuardVar) != "1" {
		t.Skip("set PP_IDENTITY_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(identityIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required", identityIntegrationURLVar)
	}
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	var databaseName string
	if err := admin.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != identityIntegrationDBName {
		t.Fatalf("refusing identity integration database %q: %v", databaseName, err)
	}

	identity := appidentity.ExternalIdentity{
		Provider: appidentity.ProviderSupabase,
		Issuer:   "https://project-ref.supabase.co/auth/v1",
		Subject:  "123e4567-e89b-12d3-a456-426614174000",
	}

	const callers = 16
	stores := make([]*Store, callers)
	for index := range stores {
		stores[index] = MustOpenWithOptions(databaseURL, Options{
			MaxOpenConns: 2,
			MaxIdleConns: 1,
			QueryTimeout: 10 * time.Second,
		})
		t.Cleanup(func() { _ = stores[index].Close() })
	}
	type result struct {
		snapshot appidentity.Snapshot
		created  bool
		err      error
	}
	results := make([]result, callers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers)
	for index, store := range stores {
		go func() {
			defer wait.Done()
			<-start
			results[index].snapshot, results[index].created, results[index].err = store.EnsureIdentity(t.Context(), identity)
		}()
	}
	close(start)
	wait.Wait()

	wantPrincipal := results[0].snapshot.PrincipalID
	if results[0].err != nil || wantPrincipal == "" || len(results[0].snapshot.Memberships) != 1 {
		t.Fatalf("first onboarding result = %#v, created=%t, err=%v", results[0].snapshot, results[0].created, results[0].err)
	}
	wantAccount := results[0].snapshot.Memberships[0].AccountID
	createdCount := 0
	for index, result := range results {
		if result.err != nil {
			t.Errorf("caller %d: %v", index, result.err)
			continue
		}
		if result.created {
			createdCount++
		}
		if result.snapshot.PrincipalID != wantPrincipal || len(result.snapshot.Memberships) != 1 || result.snapshot.Memberships[0].AccountID != wantAccount || result.snapshot.Memberships[0].Role != appidentity.RoleOwner {
			t.Errorf("caller %d snapshot = %#v", index, result.snapshot)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created results = %d, want exactly 1", createdCount)
	}

	repeated, created, err := stores[0].EnsureIdentity(t.Context(), identity)
	if err != nil || created || repeated.PrincipalID != wantPrincipal || repeated.Memberships[0].AccountID != wantAccount {
		t.Fatalf("repeat onboarding = %#v, created=%t, err=%v", repeated, created, err)
	}
	lookedUp, err := stores[0].Identity(t.Context(), identity)
	if err != nil || lookedUp.PrincipalID != wantPrincipal || lookedUp.Memberships[0].AccountID != wantAccount {
		t.Fatalf("identity lookup = %#v, err=%v", lookedUp, err)
	}

	var principals, identities, memberships, profiles int
	if err := admin.QueryRow(`
		SELECT
		  (SELECT count(*) FROM principals),
		  (SELECT count(*) FROM external_identities WHERE provider=$1 AND issuer=$2 AND subject=$3),
		  (SELECT count(*) FROM account_memberships WHERE principal_id=$4 AND account_id=$5 AND role='owner'),
		  (SELECT count(*) FROM profiles WHERE account_id=$5)
	`, identity.Provider, identity.Issuer, identity.Subject, wantPrincipal, wantAccount).Scan(&principals, &identities, &memberships, &profiles); err != nil {
		t.Fatal(err)
	}
	if principals != 1 || identities != 1 || memberships != 1 || profiles != 0 {
		t.Fatalf("principal/identity/membership/profile counts = %d/%d/%d/%d, want 1/1/1/0", principals, identities, memberships, profiles)
	}

	// Profiles remain an explicit, valid-empty reader context after onboarding.
	// Insert fixture rows directly here to verify the account-scoped repository;
	// EnsureIdentity itself must never provision a profile.
	const profileOne = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const profileTwo = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := admin.Exec(`
		INSERT INTO profiles (id, account_id, name)
		VALUES ($1, $2, 'Zoe'), ($3, $2, 'Ada')
	`, profileOne, wantAccount, profileTwo); err != nil {
		t.Fatalf("insert explicit profile fixtures: %v", err)
	}
	profilesForAccount, err := stores[0].Profiles(wantAccount)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profilesForAccount) != 2 || profilesForAccount[0].ID != profileTwo || profilesForAccount[0].Name != "Ada" || profilesForAccount[1].ID != profileOne || profilesForAccount[1].Name != "Zoe" {
		t.Fatalf("account-scoped profiles = %#v", profilesForAccount)
	}
	if exists, err := stores[0].ProfileExists(wantAccount, profileOne); err != nil || !exists {
		t.Fatalf("own profile lookup = %t, %v", exists, err)
	}

	var legacyAccount string
	if err := admin.QueryRow(`SELECT id FROM accounts WHERE id <> $1 ORDER BY created_at, id LIMIT 1`, wantAccount).Scan(&legacyAccount); err != nil {
		t.Fatalf("read pre-existing legacy account: %v", err)
	}
	if legacyAccount == wantAccount {
		t.Fatal("onboarding reused the pre-existing account")
	}
	if exists, err := stores[0].ProfileExists(legacyAccount, profileOne); err != nil || exists {
		t.Fatalf("cross-account profile lookup = %t, %v", exists, err)
	}

	createdProfile, err := stores[0].CreateProfile(wantAccount, "Milo")
	if err != nil || createdProfile.ID == "" || createdProfile.Name != "Milo" {
		t.Fatalf("create account-scoped profile = %#v, %v", createdProfile, err)
	}
	renamedProfile, err := stores[0].UpdateProfile(wantAccount, profileOne, "Mira")
	if err != nil || renamedProfile.ID != profileOne || renamedProfile.Name != "Mira" {
		t.Fatalf("rename account-scoped profile = %#v, %v", renamedProfile, err)
	}
	if _, err := stores[0].UpdateProfile(wantAccount, profileOne, "Ada"); !errors.Is(err, model.ErrProfileNameConflict) {
		t.Fatalf("duplicate profile name error = %v", err)
	}
	if _, err := stores[0].UpdateProfile(legacyAccount, profileOne, "Elsewhere"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-account profile update error = %v", err)
	}
	if err := stores[0].DeleteProfile(legacyAccount, profileOne); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-account profile delete error = %v", err)
	}
	for _, id := range []string{profileOne, profileTwo, createdProfile.ID} {
		if err := stores[0].DeleteProfile(wantAccount, id); err != nil {
			t.Fatalf("delete account-scoped profile %s: %v", id, err)
		}
	}
	profilesForAccount, err = stores[0].Profiles(wantAccount)
	if err != nil || len(profilesForAccount) != 0 {
		t.Fatalf("final profile deletion/list = %#v, %v", profilesForAccount, err)
	}
	var remainingAccounts, remainingMemberships int
	if err := admin.QueryRow(`
		SELECT
		  (SELECT count(*) FROM accounts WHERE id = $1),
		  (SELECT count(*) FROM account_memberships WHERE principal_id = $2 AND account_id = $1)
	`, wantAccount, wantPrincipal).Scan(&remainingAccounts, &remainingMemberships); err != nil {
		t.Fatal(err)
	}
	if remainingAccounts != 1 || remainingMemberships != 1 {
		t.Fatalf("profile delete removed unrelated account data: accounts/memberships = %d/%d", remainingAccounts, remainingMemberships)
	}

	t.Run("database constraints fail closed", func(t *testing.T) {
		assertConstraint(t, admin, "external_identities_provider_issuer_subject_key", `
			INSERT INTO external_identities (principal_id, provider, issuer, subject)
			VALUES ($1, $2, $3, $4)
		`, wantPrincipal, identity.Provider, identity.Issuer, identity.Subject)
		assertConstraint(t, admin, "account_memberships_role_check", `
			INSERT INTO account_memberships (principal_id, account_id, role)
			VALUES ($1, $2, 'child')
		`, wantPrincipal, legacyAccount)
		assertConstraint(t, admin, "account_memberships_account_fkey", `
			INSERT INTO account_memberships (principal_id, account_id, role)
			VALUES ($1, 'ffffffff-ffff-4fff-8fff-ffffffffffff', 'adult')
		`, wantPrincipal)
		assertConstraint(t, admin, "account_memberships_pkey", `
			INSERT INTO account_memberships (principal_id, account_id, role)
			VALUES ($1, $2, 'adult')
		`, wantPrincipal, wantAccount)
	})

	if _, _, err := stores[0].EnsureIdentity(t.Context(), appidentity.ExternalIdentity{
		Provider: appidentity.ProviderSupabase,
		Issuer:   identity.Issuer,
		Subject:  " subject ",
	}); !errors.Is(err, appidentity.ErrInvalidState) {
		t.Fatalf("invalid identity error = %v", err)
	}
}

func assertConstraint(t *testing.T, database *sql.DB, name, statement string, args ...any) {
	t.Helper()
	_, err := database.Exec(statement, args...)
	if err == nil {
		t.Fatalf("constraint %s unexpectedly accepted statement", name)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.ConstraintName != name {
		t.Fatalf("constraint error = %v, want %s", err, name)
	}
}
