package db

import "pandapages/api/internal/model"

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
