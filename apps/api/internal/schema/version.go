// Package schema defines the application schema version expected by this API
// binary. Migrations remain owned and executed by Goose; the API only checks
// their recorded state for readiness.
package schema

// ExpectedMigrationVersion is the highest Goose migration version this API
// understands. version_test.go prevents this value drifting from the tracked
// migration files.
const ExpectedMigrationVersion int64 = 14
