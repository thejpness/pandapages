package db

import (
	"context"
	"database/sql"
)

const classicStoryEditionKey = "classic"

// ensureClassicEdition is the temporary compatibility bridge for the existing
// single-edition admin contract. Migration 20 backfills all existing stories;
// newly created stories receive their Classic edition explicitly here.
func ensureClassicEdition(ctx context.Context, tx *sql.Tx, storyID string) (string, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO story_editions (story_id, edition_key)
		VALUES ($1, $2)
		ON CONFLICT (story_id, edition_key) DO NOTHING
	`, storyID, classicStoryEditionKey); err != nil {
		return "", err
	}
	return loadClassicEditionID(ctx, tx, storyID, true)
}

func loadClassicEditionID(ctx context.Context, tx *sql.Tx, storyID string, lock bool) (string, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}

	var editionID string
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM story_editions
		WHERE story_id = $1
		  AND edition_key = $2
	`+lockClause, storyID, classicStoryEditionKey).Scan(&editionID)
	return editionID, err
}

func requireVersionInEdition(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	editionID string,
	versionID string,
) error {
	var id string
	return tx.QueryRowContext(ctx, `
		SELECT id
		FROM story_versions
		WHERE id = $1
		  AND story_id = $2
		  AND edition_id = $3
		FOR UPDATE
	`, versionID, storyID, editionID).Scan(&id)
}

func setEditionDraftPointer(ctx context.Context, tx *sql.Tx, editionID, versionID string) error {
	var id string
	return tx.QueryRowContext(ctx, `
		UPDATE story_editions
		SET draft_version_id = $2,
		    updated_at = now()
		WHERE id = $1
		  AND edition_key = $3
		RETURNING id
	`, editionID, versionID, classicStoryEditionKey).Scan(&id)
}

func setEditionPublishedPointer(ctx context.Context, tx *sql.Tx, editionID, versionID string) error {
	var id string
	return tx.QueryRowContext(ctx, `
		UPDATE story_editions
		SET published_version_id = $2,
		    updated_at = now()
		WHERE id = $1
		  AND edition_key = $3
		RETURNING id
	`, editionID, versionID, classicStoryEditionKey).Scan(&id)
}

func clearEditionPublishedPointer(ctx context.Context, tx *sql.Tx, editionID string) error {
	var id string
	return tx.QueryRowContext(ctx, `
		UPDATE story_editions
		SET published_version_id = NULL,
		    updated_at = CASE
		      WHEN published_version_id IS NOT NULL THEN now()
		      ELSE updated_at
		    END
		WHERE id = $1
		  AND edition_key = $2
		RETURNING id
	`, editionID, classicStoryEditionKey).Scan(&id)
}
