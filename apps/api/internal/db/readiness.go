package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"pandapages/api/internal/readiness"
	"pandapages/api/internal/schema"
)

var (
	// ErrDatabaseUnavailable means PostgreSQL could not complete the readiness
	// probe. The underlying error is deliberately not part of the HTTP contract.
	ErrDatabaseUnavailable = readiness.ErrDatabaseUnavailable
	// ErrSchemaNotReady means Goose metadata is missing, inaccessible, incomplete,
	// behind, or ahead of the schema version understood by this API binary.
	ErrSchemaNotReady = readiness.ErrSchemaNotReady
)

type migrationState struct {
	count      int64
	minimum    int64
	maximum    int64
	allApplied bool
}

type readinessProbe interface {
	Ping(context.Context) error
	MigrationMetadataExists(context.Context) (bool, error)
	MigrationState(context.Context) (migrationState, error)
}

type sqlReadinessProbe struct {
	db *sql.DB
}

func (probe sqlReadinessProbe) Ping(ctx context.Context) error {
	return probe.db.PingContext(ctx)
}

func (probe sqlReadinessProbe) MigrationMetadataExists(ctx context.Context) (bool, error) {
	var exists bool
	err := probe.db.QueryRowContext(ctx, `
		SELECT to_regclass('public.goose_db_version') IS NOT NULL
	`).Scan(&exists)
	return exists, err
}

func (probe sqlReadinessProbe) MigrationState(ctx context.Context) (migrationState, error) {
	var state migrationState
	err := probe.db.QueryRowContext(ctx, `
		WITH latest_version_state AS (
			SELECT DISTINCT ON (version_id)
				version_id,
				is_applied
			FROM public.goose_db_version
			WHERE version_id > 0
			ORDER BY version_id, id DESC
		)
		SELECT
			COUNT(*),
			COALESCE(MIN(version_id), 0),
			COALESCE(MAX(version_id), 0),
			COALESCE(BOOL_AND(is_applied), false)
		FROM latest_version_state
	`).Scan(&state.count, &state.minimum, &state.maximum, &state.allApplied)
	return state, err
}

// CheckReadiness checks PostgreSQL connectivity and Goose's latest recorded
// state using the caller's single deadline. It never applies migrations.
func (s *Store) CheckReadiness(ctx context.Context) error {
	return checkReadiness(ctx, sqlReadinessProbe{db: s.db})
}

func checkReadiness(ctx context.Context, probe readinessProbe) error {
	if err := probe.Ping(ctx); err != nil {
		return ErrDatabaseUnavailable
	}

	exists, err := probe.MigrationMetadataExists(ctx)
	if err != nil {
		return ErrDatabaseUnavailable
	}
	if !exists {
		return ErrSchemaNotReady
	}

	state, err := probe.MigrationState(ctx)
	if err != nil {
		return classifyMigrationStateError(ctx, err)
	}

	if state.count != schema.ExpectedMigrationVersion ||
		state.minimum != 1 ||
		state.maximum != schema.ExpectedMigrationVersion ||
		!state.allApplied {
		return ErrSchemaNotReady
	}

	return nil
}

func classifyMigrationStateError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ErrDatabaseUnavailable
	}

	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && strings.HasPrefix(postgresError.Code, "42") {
		// PostgreSQL class 42 covers missing/malformed schema objects and
		// insufficient privilege. All mean the expected Goose state cannot be
		// proven, while transport and connection failures remain database errors.
		return ErrSchemaNotReady
	}

	return ErrDatabaseUnavailable
}
