package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"pandapages/api/internal/model"
)

// Profiles returns only the selection fields for profiles belonging to one
// explicit account. Name then ID provides deterministic presentation ordering.
func (s *Store) Profiles(accountID string) ([]model.ReaderProfile, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name
		FROM profiles
		WHERE account_id = $1
		ORDER BY name ASC, id ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := []model.ReaderProfile{}
	for rows.Next() {
		var profile model.ReaderProfile
		if err := rows.Scan(&profile.ID, &profile.Name); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

// ProfileExists uses one account-scoped query so callers never learn whether a
// profile exists outside their already-authorized selected account.
func (s *Store) ProfileExists(accountID, profileID string) (bool, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM profiles
			WHERE account_id = $1 AND id = $2
		)
	`, accountID, profileID).Scan(&exists)
	return exists, err
}

// CreateProfile attaches a new reader profile only to the caller's already
// authorized account. IDs retain the database's gen_random_uuid convention.
func (s *Store) CreateProfile(accountID, name string) (model.ReaderProfile, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var profile model.ReaderProfile
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO profiles (account_id, name)
		VALUES ($1, $2)
		RETURNING id::text, name
	`, accountID, name).Scan(&profile.ID, &profile.Name)
	if isProfileNameConflict(err) {
		return model.ReaderProfile{}, model.ErrProfileNameConflict
	}
	return profile, err
}

// UpdateProfile changes a profile only when its id belongs to the explicit
// account. sql.ErrNoRows deliberately covers both missing and cross-account
// profiles for callers.
func (s *Store) UpdateProfile(accountID, profileID, name string) (model.ReaderProfile, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var profile model.ReaderProfile
	err := s.db.QueryRowContext(ctx, `
		UPDATE profiles
		SET name = $3, updated_at = now()
		WHERE account_id = $1 AND id = $2
		RETURNING id::text, name
	`, accountID, profileID, name).Scan(&profile.ID, &profile.Name)
	if isProfileNameConflict(err) {
		return model.ReaderProfile{}, model.ErrProfileNameConflict
	}
	return profile, err
}

// DeleteProfile hard-deletes only the selected account's profile. The current
// account-scoped progress and settings schema has no profile FK, so this cannot
// delete account or story data. sql.ErrNoRows remains externally forbidden.
func (s *Store) DeleteProfile(accountID, profileID string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	var deletedID string
	return s.db.QueryRowContext(ctx, `
		DELETE FROM profiles
		WHERE account_id = $1 AND id = $2
		RETURNING id::text
	`, accountID, profileID).Scan(&deletedID)
}

func isProfileNameConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "ux_profiles_account_name"
}
