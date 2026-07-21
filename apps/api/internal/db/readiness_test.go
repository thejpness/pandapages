package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"pandapages/api/internal/schema"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeReadinessProbe struct {
	ping           func(context.Context) error
	metadataExists func(context.Context) (bool, error)
	migrationState func(context.Context) (migrationState, error)
}

func (probe fakeReadinessProbe) Ping(ctx context.Context) error {
	if probe.ping != nil {
		return probe.ping(ctx)
	}
	return nil
}

func (probe fakeReadinessProbe) MigrationMetadataExists(ctx context.Context) (bool, error) {
	if probe.metadataExists != nil {
		return probe.metadataExists(ctx)
	}
	return true, nil
}

func (probe fakeReadinessProbe) MigrationState(ctx context.Context) (migrationState, error) {
	if probe.migrationState != nil {
		return probe.migrationState(ctx)
	}
	return migrationState{
		count:      schema.ExpectedMigrationVersion,
		minimum:    1,
		maximum:    schema.ExpectedMigrationVersion,
		allApplied: true,
	}, nil
}

func TestCheckReadiness(t *testing.T) {
	tests := []struct {
		name  string
		probe fakeReadinessProbe
		want  error
	}{
		{name: "ready", probe: fakeReadinessProbe{}},
		{
			name: "database unavailable",
			probe: fakeReadinessProbe{ping: func(context.Context) error {
				return errors.New("private database error")
			}},
			want: ErrDatabaseUnavailable,
		},
		{
			name: "migration metadata lookup unavailable",
			probe: fakeReadinessProbe{metadataExists: func(context.Context) (bool, error) {
				return false, errors.New("private database error")
			}},
			want: ErrDatabaseUnavailable,
		},
		{
			name: "migration metadata missing",
			probe: fakeReadinessProbe{metadataExists: func(context.Context) (bool, error) {
				return false, nil
			}},
			want: ErrSchemaNotReady,
		},
		{
			name: "migration metadata inaccessible",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{}, &pgconn.PgError{Code: "42501", Message: "private permission detail"}
			}},
			want: ErrSchemaNotReady,
		},
		{
			name: "database fails after metadata lookup",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{}, errors.New("private connection failure")
			}},
			want: ErrDatabaseUnavailable,
		},
		{
			name: "outdated schema",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{count: schema.ExpectedMigrationVersion - 1, minimum: 1, maximum: schema.ExpectedMigrationVersion - 1, allApplied: true}, nil
			}},
			want: ErrSchemaNotReady,
		},
		{
			name: "future schema",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{count: schema.ExpectedMigrationVersion + 1, minimum: 1, maximum: schema.ExpectedMigrationVersion + 1, allApplied: true}, nil
			}},
			want: ErrSchemaNotReady,
		},
		{
			name: "migration gap",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{count: schema.ExpectedMigrationVersion - 1, minimum: 1, maximum: schema.ExpectedMigrationVersion, allApplied: true}, nil
			}},
			want: ErrSchemaNotReady,
		},
		{
			name: "down or incomplete migration state",
			probe: fakeReadinessProbe{migrationState: func(context.Context) (migrationState, error) {
				return migrationState{count: schema.ExpectedMigrationVersion, minimum: 1, maximum: schema.ExpectedMigrationVersion, allApplied: false}, nil
			}},
			want: ErrSchemaNotReady,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkReadiness(context.Background(), test.probe)
			if !errors.Is(err, test.want) {
				t.Fatalf("checkReadiness() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCheckReadinessHonoursCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	probe := fakeReadinessProbe{ping: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	started := time.Now()
	err := checkReadiness(ctx, probe)
	if !errors.Is(err, ErrDatabaseUnavailable) {
		t.Fatalf("checkReadiness() error = %v, want %v", err, ErrDatabaseUnavailable)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("checkReadiness() took %v after deadline", elapsed)
	}
}
