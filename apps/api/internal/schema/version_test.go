package schema

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d{5})_[A-Za-z0-9_]+\.sql$`)

func TestExpectedMigrationVersionMatchesTrackedMigrations(t *testing.T) {
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	versions := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilenamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			if len(entry.Name()) >= 4 && entry.Name()[len(entry.Name())-4:] == ".sql" {
				t.Errorf("migration filename %q does not follow NNNNN_name.sql", entry.Name())
			}
			continue
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			t.Fatalf("parse migration version from %q: %v", entry.Name(), err)
		}
		versions = append(versions, version)
	}

	if len(versions) == 0 {
		t.Fatal("no tracked migrations found")
	}
	sort.Ints(versions)
	for index, version := range versions {
		want := index + 1
		if version != want {
			t.Fatalf("migration versions = %v; want contiguous versions beginning at 1 (first mismatch: got %d, want %d)", versions, version, want)
		}
	}

	latest := int64(versions[len(versions)-1])
	if latest != ExpectedMigrationVersion {
		t.Fatalf("ExpectedMigrationVersion = %d, highest tracked migration = %d; update them together", ExpectedMigrationVersion, latest)
	}

	if int64(len(versions)) != ExpectedMigrationVersion {
		t.Fatalf("tracked migration count = %d, expected version = %d", len(versions), ExpectedMigrationVersion)
	}
}
