package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"pandapages/api/internal/appidentity"
)

func (s *Store) identityContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := s.queryTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return context.WithTimeout(parent, timeout)
}

func validExternalIdentity(identity appidentity.ExternalIdentity) bool {
	return identity.Provider == appidentity.ProviderSupabase &&
		strings.TrimSpace(identity.Issuer) == identity.Issuer && identity.Issuer != "" && len(identity.Issuer) <= 512 &&
		strings.TrimSpace(identity.Subject) == identity.Subject && identity.Subject != "" && len(identity.Subject) <= 255 &&
		utf8.ValidString(identity.Issuer) && utf8.ValidString(identity.Subject)
}

// EnsureIdentity resolves or atomically provisions one principal, one initial
// account, and one owner membership for an externally authenticated adult.
// PostgreSQL serializes the identity tuple across API processes; the unique
// identity key remains the final integrity boundary for out-of-band writers.
func (s *Store) EnsureIdentity(parent context.Context, identity appidentity.ExternalIdentity) (appidentity.Snapshot, bool, error) {
	if !validExternalIdentity(identity) {
		return appidentity.Snapshot{}, false, appidentity.ErrInvalidState
	}

	snapshot, created, err := s.ensureIdentityOnce(parent, identity)
	if !isIdentityConflict(err) {
		return snapshot, created, err
	}

	// A writer that does not take Panda Pages' advisory lock can still win the
	// database unique key. The failed transaction contains every provisional
	// row, so rollback is complete; resolve the committed winner deterministically.
	snapshot, err = s.Identity(parent, identity)
	return snapshot, false, err
}

func (s *Store) ensureIdentityOnce(parent context.Context, identity appidentity.ExternalIdentity) (appidentity.Snapshot, bool, error) {
	ctx, cancel := s.identityContext(parent)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appidentity.Snapshot{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1 || E'\n' || $2 || E'\n' || $3, 0)
		)
	`, identity.Provider, identity.Issuer, identity.Subject); err != nil {
		return appidentity.Snapshot{}, false, err
	}

	snapshot, err := identitySnapshot(ctx, tx, identity)
	created := false
	if errors.Is(err, appidentity.ErrNotFound) {
		created = true
		var principalID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO principals (display_name)
			VALUES ($1)
			RETURNING id
		`, appidentity.InitialDisplayName).Scan(&principalID); err != nil {
			return appidentity.Snapshot{}, false, err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO external_identities (
				principal_id, provider, issuer, subject
			) VALUES ($1, $2, $3, $4)
		`, principalID, identity.Provider, identity.Issuer, identity.Subject); err != nil {
			return appidentity.Snapshot{}, false, err
		}

		var accountID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO accounts (name)
			VALUES ($1)
			RETURNING id
		`, appidentity.InitialAccountName).Scan(&accountID); err != nil {
			return appidentity.Snapshot{}, false, err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO account_memberships (principal_id, account_id, role)
			VALUES ($1, $2, $3)
		`, principalID, accountID, appidentity.RoleOwner); err != nil {
			return appidentity.Snapshot{}, false, err
		}

		snapshot, err = identitySnapshot(ctx, tx, identity)
	}
	if err != nil {
		return appidentity.Snapshot{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE external_identities
		SET last_seen_at = now()
		WHERE provider = $1 AND issuer = $2 AND subject = $3
	`, identity.Provider, identity.Issuer, identity.Subject); err != nil {
		return appidentity.Snapshot{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return appidentity.Snapshot{}, false, err
	}
	return snapshot, created, nil
}

// Identity returns the existing Panda Pages principal and memberships without
// creating application state. It is used by GET /api/auth/me after onboarding.
func (s *Store) Identity(parent context.Context, identity appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	if !validExternalIdentity(identity) {
		return appidentity.Snapshot{}, appidentity.ErrInvalidState
	}
	ctx, cancel := s.identityContext(parent)
	defer cancel()
	return identitySnapshot(ctx, s.db, identity)
}

type identityQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func identitySnapshot(ctx context.Context, query identityQuerier, identity appidentity.ExternalIdentity) (appidentity.Snapshot, error) {
	var snapshot appidentity.Snapshot
	err := query.QueryRowContext(ctx, `
		SELECT principal.id, principal.display_name
		FROM external_identities AS external_identity
		JOIN principals AS principal ON principal.id = external_identity.principal_id
		WHERE external_identity.provider = $1
		  AND external_identity.issuer = $2
		  AND external_identity.subject = $3
	`, identity.Provider, identity.Issuer, identity.Subject).Scan(&snapshot.PrincipalID, &snapshot.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return appidentity.Snapshot{}, appidentity.ErrNotFound
	}
	if err != nil {
		return appidentity.Snapshot{}, err
	}

	rows, err := query.QueryContext(ctx, `
		SELECT membership.account_id, account.name, membership.role
		FROM account_memberships AS membership
		JOIN accounts AS account ON account.id = membership.account_id
		WHERE membership.principal_id = $1
		ORDER BY membership.created_at ASC, membership.account_id ASC
	`, snapshot.PrincipalID)
	if err != nil {
		return appidentity.Snapshot{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var membership appidentity.Membership
		if err := rows.Scan(&membership.AccountID, &membership.AccountName, &membership.Role); err != nil {
			return appidentity.Snapshot{}, err
		}
		if membership.Role != appidentity.RoleOwner && membership.Role != appidentity.RoleAdult {
			return appidentity.Snapshot{}, appidentity.ErrInvalidState
		}
		snapshot.Memberships = append(snapshot.Memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return appidentity.Snapshot{}, err
	}
	if len(snapshot.Memberships) == 0 {
		return appidentity.Snapshot{}, appidentity.ErrInvalidState
	}
	return snapshot, nil
}

func isIdentityConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "external_identities_provider_issuer_subject_key"
}
