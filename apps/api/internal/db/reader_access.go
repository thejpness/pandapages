package db

import (
	"context"
	"database/sql"
	"fmt"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readerresolution"
)

type readerProfileStoryAccess struct {
	ReadingLevel     model.ReaderEditionKey
	StoryID          string
	CurrentReleaseID string
}

// lockReaderProfileStoryAccess takes the mutable profile and story rows
// separately. This keeps Reading Level and current-release selection stable
// without relying on a locking join that PostgreSQL could later recheck.
func lockReaderProfileStoryAccess(
	ctx context.Context,
	tx *sql.Tx,
	accountID string,
	profileID string,
	slug string,
) (readerProfileStoryAccess, error) {
	var readingLevelValue string
	if err := tx.QueryRowContext(ctx, `
		SELECT reading_level
		FROM profiles
		WHERE account_id = $1
		  AND id = $2
		FOR SHARE
	`, accountID, profileID).Scan(&readingLevelValue); err != nil {
		return readerProfileStoryAccess{}, err
	}
	readingLevel := model.ReaderEditionKey(readingLevelValue)
	if !model.ValidReaderEditionKey(readingLevel) {
		return readerProfileStoryAccess{}, fmt.Errorf("stored reader reading level is invalid")
	}

	var (
		storyID          string
		currentReleaseID sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT story.id, story.current_release_id
		FROM stories AS story
		WHERE story.account_id = $1
		  AND story.slug = $2
		FOR SHARE OF story
	`, accountID, slug).Scan(&storyID, &currentReleaseID); err != nil {
		return readerProfileStoryAccess{}, err
	}
	if !currentReleaseID.Valid {
		return readerProfileStoryAccess{}, sql.ErrNoRows
	}

	return readerProfileStoryAccess{
		ReadingLevel:     readingLevel,
		StoryID:          storyID,
		CurrentReleaseID: currentReleaseID.String,
	}, nil
}

func currentReleaseHasAllowedReaderEdition(
	ctx context.Context,
	tx *sql.Tx,
	access readerProfileStoryAccess,
) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT edition.edition_key
		FROM story_release_editions AS member
		JOIN story_editions AS edition
		  ON edition.id = member.edition_id
		 AND edition.story_id = member.story_id
		WHERE member.release_id = $1
		  AND member.story_id = $2
	`, access.CurrentReleaseID, access.StoryID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var editionKeyValue string
		if err := rows.Scan(&editionKeyValue); err != nil {
			return false, err
		}
		editionKey := model.ReaderEditionKey(editionKeyValue)
		if !model.ValidReaderEditionKey(editionKey) {
			return false, fmt.Errorf("stored current-release edition key is invalid")
		}
		if readerresolution.Allows(access.ReadingLevel, editionKey) {
			if err := rows.Close(); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func currentReleaseReaderVersionByNumber(
	ctx context.Context,
	tx *sql.Tx,
	access readerProfileStoryAccess,
	version int,
) (string, error) {
	var (
		versionID       string
		editionKeyValue string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT member.story_version_id, edition.edition_key
		FROM story_release_editions AS member
		JOIN story_editions AS edition
		  ON edition.id = member.edition_id
		 AND edition.story_id = member.story_id
		JOIN story_versions AS version
		  ON version.id = member.story_version_id
		 AND version.story_id = member.story_id
		 AND version.edition_id = member.edition_id
		WHERE member.release_id = $1
		  AND member.story_id = $2
		  AND version.version = $3
	`, access.CurrentReleaseID, access.StoryID, version).Scan(&versionID, &editionKeyValue); err != nil {
		return "", err
	}

	editionKey := model.ReaderEditionKey(editionKeyValue)
	if !model.ValidReaderEditionKey(editionKey) {
		return "", fmt.Errorf("stored current-release edition key is invalid")
	}
	if !readerresolution.Allows(access.ReadingLevel, editionKey) {
		return "", sql.ErrNoRows
	}
	return versionID, nil
}

func readerEditionAllowanceFlags(
	readingLevel model.ReaderEditionKey,
) ([5]bool, error) {
	if !model.ValidReaderEditionKey(readingLevel) {
		return [5]bool{}, fmt.Errorf("stored reader reading level is invalid")
	}
	keys := model.ReaderEditionKeys()
	if len(keys) != 5 {
		return [5]bool{}, fmt.Errorf("canonical reader edition set is invalid")
	}
	var allowed [5]bool
	for index, key := range keys {
		allowed[index] = readerresolution.Allows(readingLevel, key)
	}
	return allowed, nil
}
