package db

import (
	"database/sql"
	"fmt"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readerresolution"
)

// ReaderStoryEditionOverrideGet returns the stored explicit edition choice for
// one profile/story. A nil override is distinct from an unavailable profile or
// story, which returns sql.ErrNoRows.
func (s *Store) ReaderStoryEditionOverrideGet(
	accountID string,
	profileID string,
	slug string,
) (*model.ReaderEditionKey, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var editionKey sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT override.edition_key
		FROM profiles AS profile
		JOIN stories AS story
		  ON story.account_id = profile.account_id
		LEFT JOIN reader_story_edition_overrides AS override
		  ON override.account_id = profile.account_id
		 AND override.profile_id = profile.id
		 AND override.story_id = story.id
		WHERE profile.account_id = $1
		  AND profile.id = $2
		  AND story.slug = $3
	`, accountID, profileID, slug).Scan(&editionKey)
	if err != nil {
		return nil, err
	}
	if !editionKey.Valid {
		return nil, nil
	}

	key := model.ReaderEditionKey(editionKey.String)
	if !model.ValidReaderEditionKey(key) {
		return nil, fmt.Errorf("stored reader edition override is invalid")
	}
	return &key, nil
}

// ReaderStoryEditionOverridePut persists only a choice that the profile's
// reading level permits and that is a member of the story's current release.
// Later release or reading-level changes may make the stored choice stale; the
// resolver deliberately ignores stale overrides rather than treating them as
// publication authority.
func (s *Store) ReaderStoryEditionOverridePut(
	accountID string,
	profileID string,
	slug string,
	editionKey model.ReaderEditionKey,
) error {
	if !model.ValidReaderEditionKey(editionKey) {
		return fmt.Errorf("invalid reader edition override")
	}

	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		readingLevelValue string
		storyID           string
		currentReleaseID  sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT profile.reading_level, story.id, story.current_release_id
		FROM profiles AS profile
		JOIN stories AS story
		  ON story.account_id = profile.account_id
		WHERE profile.account_id = $1
		  AND profile.id = $2
		  AND story.slug = $3
		FOR SHARE OF profile, story
	`, accountID, profileID, slug).Scan(
		&readingLevelValue,
		&storyID,
		&currentReleaseID,
	)
	if err != nil {
		return err
	}
	readingLevel := model.ReaderEditionKey(readingLevelValue)
	if !model.ValidReaderEditionKey(readingLevel) {
		return fmt.Errorf("stored reader reading level is invalid")
	}
	if !readerresolution.Allows(readingLevel, editionKey) || !currentReleaseID.Valid {
		return sql.ErrNoRows
	}

	var currentMember bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM story_release_editions AS member
			JOIN story_editions AS edition
			  ON edition.id = member.edition_id
			 AND edition.story_id = member.story_id
			WHERE member.release_id = $1
			  AND member.story_id = $2
			  AND edition.edition_key = $3
		)
	`, currentReleaseID.String, storyID, editionKey).Scan(&currentMember); err != nil {
		return err
	}
	if !currentMember {
		return sql.ErrNoRows
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reader_story_edition_overrides (
			account_id,
			profile_id,
			story_id,
			edition_key,
			updated_at
		)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (account_id, profile_id, story_id)
		DO UPDATE SET
			edition_key = EXCLUDED.edition_key,
			updated_at = now()
	`, accountID, profileID, storyID, editionKey); err != nil {
		return err
	}

	return tx.Commit()
}

// ReaderStoryEditionOverrideClear removes only the selected profile/story
// override. It is intentionally idempotent and never changes progress.
func (s *Store) ReaderStoryEditionOverrideClear(
	accountID string,
	profileID string,
	slug string,
) (bool, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM reader_story_edition_overrides AS override
		USING profiles AS profile, stories AS story
		WHERE override.account_id = $1
		  AND override.profile_id = $2
		  AND profile.account_id = override.account_id
		  AND profile.id = override.profile_id
		  AND story.account_id = override.account_id
		  AND story.id = override.story_id
		  AND story.slug = $3
	`, accountID, profileID, slug)
	if err != nil {
		return false, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}
