package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"pandapages/api/internal/model"
	"pandapages/api/internal/profilepin"
)

const (
	profilePINMaxFailures  = 5
	profilePINLockDuration = 15 * time.Minute
)

// Profiles returns only the selection fields for profiles belonging to one
// explicit account. Name then ID provides deterministic presentation ordering.
func (s *Store) Profiles(accountID string) ([]model.ReaderProfile, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, pin_hash IS NOT NULL
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
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.PINEnabled); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

// SetProfilePIN stores a bcrypt encoding only and clears the profile-local
// verification lockout. The account/profile condition intentionally prevents
// global profile probing.
func (s *Store) SetProfilePIN(accountID, profileID, encodedHash string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	var updatedID string
	return s.db.QueryRowContext(ctx, `
		UPDATE profiles
		SET pin_hash = $3,
			pin_failed_attempts = 0,
			pin_lock_until = NULL,
			updated_at = now()
		WHERE account_id = $1 AND id = $2
		RETURNING id::text
	`, accountID, profileID, encodedHash).Scan(&updatedID)
}

// RemoveProfilePIN removes the local reader-mode gate and clears only its
// profile-local throttling state. It never affects adult authentication.
func (s *Store) RemoveProfilePIN(accountID, profileID string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	var updatedID string
	return s.db.QueryRowContext(ctx, `
		UPDATE profiles
		SET pin_hash = NULL,
			pin_failed_attempts = 0,
			pin_lock_until = NULL,
			updated_at = now()
		WHERE account_id = $1 AND id = $2
		RETURNING id::text
	`, accountID, profileID).Scan(&updatedID)
}

// VerifyProfilePIN compares a candidate inside a row-locked, account-scoped
// transaction. A profile without a PIN succeeds directly; the caller still
// needs the normal bearer and account membership boundary before reaching it.
func (s *Store) VerifyProfilePIN(accountID, profileID, candidate string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		hash      sql.NullString
		failures  int
		lockUntil sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
		SELECT pin_hash, pin_failed_attempts, pin_lock_until
		FROM profiles
		WHERE account_id = $1 AND id = $2
		FOR UPDATE
	`, accountID, profileID).Scan(&hash, &failures, &lockUntil)
	if err != nil {
		return err
	}
	if !hash.Valid || hash.String == "" {
		return tx.Commit()
	}

	now := time.Now().UTC()
	if lockUntil.Valid && lockUntil.Time.After(now) {
		return model.ErrProfilePINRateLimited
	}
	if lockUntil.Valid {
		failures = 0
	}

	matched, err := profilepin.Matches(hash.String, candidate)
	if err != nil {
		return err
	}
	if matched {
		if _, err := tx.ExecContext(ctx, `
			UPDATE profiles
			SET pin_failed_attempts = 0, pin_lock_until = NULL, updated_at = now()
			WHERE account_id = $1 AND id = $2
		`, accountID, profileID); err != nil {
			return err
		}
		return tx.Commit()
	}

	failures++
	if failures >= profilePINMaxFailures {
		if _, err := tx.ExecContext(ctx, `
			UPDATE profiles
			SET pin_failed_attempts = $3,
				pin_lock_until = $4,
				updated_at = now()
			WHERE account_id = $1 AND id = $2
		`, accountID, profileID, failures, now.Add(profilePINLockDuration)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return model.ErrProfilePINRateLimited
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE profiles
		SET pin_failed_attempts = $3, pin_lock_until = NULL, updated_at = now()
		WHERE account_id = $1 AND id = $2
	`, accountID, profileID, failures); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return model.ErrProfilePINInvalid
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

// DeleteProfile hard-deletes only the selected account's profile. The database
// cascades only that profile's reader progress; it cannot delete account or
// story data. sql.ErrNoRows remains externally forbidden.
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
