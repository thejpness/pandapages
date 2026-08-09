package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"
)

const (
	accountScopeIntegrationURLVar   = "PP_ACCOUNT_STORE_TEST_DATABASE_URL"
	accountScopeIntegrationGuardVar = "PP_ACCOUNT_STORE_TEST_DISPOSABLE"
)

func TestAccountScopedMigrationSchema(t *testing.T) {
	if os.Getenv(accountScopeIntegrationGuardVar) != "1" {
		t.Skip("set PP_ACCOUNT_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(accountScopeIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required", accountScopeIntegrationURLVar)
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var progressKey string
	if err := database.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'reading_progress'::regclass AND contype = 'p'`).Scan(&progressKey); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(progressKey, "account_id") ||
		!strings.Contains(progressKey, "profile_id") ||
		!strings.Contains(progressKey, "story_id") {
		t.Fatalf("progress key = %s", progressKey)
	}
	var retiredRelations int
	if err := database.QueryRow(`
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN (
		    'account_settings',
		    'child_profiles',
		    'prompt_profiles',
		    'generation_jobs'
		  )
	`).Scan(&retiredRelations); err != nil {
		t.Fatal(err)
	}
	if retiredRelations != 0 {
		t.Fatalf("retired relations = %d", retiredRelations)
	}
	var generationStatusPresent bool
	if err := database.QueryRow(
		`SELECT to_regtype('public.generation_status') IS NOT NULL`,
	).Scan(&generationStatusPresent); err != nil {
		t.Fatal(err)
	}
	if generationStatusPresent {
		t.Fatal("generation_status still exists")
	}
}
