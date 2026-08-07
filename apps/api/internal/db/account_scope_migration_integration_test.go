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
	var progressKey, settingsKey string
	if err := database.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'reading_progress'::regclass AND contype = 'p'`).Scan(&progressKey); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(progressKey, "account_id") ||
		!strings.Contains(progressKey, "profile_id") ||
		!strings.Contains(progressKey, "story_id") {
		t.Fatalf("progress key = %s", progressKey)
	}
	if err := database.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'account_settings'::regclass AND contype = 'p'`).Scan(&settingsKey); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(settingsKey, "account_id") {
		t.Fatalf("settings key = %s", settingsKey)
	}
}
