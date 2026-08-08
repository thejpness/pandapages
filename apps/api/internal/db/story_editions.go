package db

import (
	"context"
	"database/sql"

	"pandapages/api/internal/model"
)

const classicStoryEditionKey = model.AdminStoryEditionClassic

func ensureStoryEdition(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	editionKey model.AdminStoryEditionKey,
) (string, error) {
	if !model.ValidAdminStoryEditionKey(editionKey) {
		return "", sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO story_editions (story_id, edition_key)
		VALUES ($1, $2)
		ON CONFLICT (story_id, edition_key) DO NOTHING
	`, storyID, editionKey); err != nil {
		return "", err
	}
	return loadStoryEditionID(ctx, tx, storyID, editionKey, true)
}

func ensureClassicEdition(ctx context.Context, tx *sql.Tx, storyID string) (string, error) {
	return ensureStoryEdition(ctx, tx, storyID, classicStoryEditionKey)
}

func loadStoryEditionID(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	editionKey model.AdminStoryEditionKey,
	lock bool,
) (string, error) {
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
	`+lockClause, storyID, editionKey).Scan(&editionID)
	return editionID, err
}

func loadClassicEditionID(ctx context.Context, tx *sql.Tx, storyID string, lock bool) (string, error) {
	return loadStoryEditionID(ctx, tx, storyID, classicStoryEditionKey, lock)
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
		RETURNING id
	`, editionID, versionID).Scan(&id)
}

func setEditionPublishedPointer(ctx context.Context, tx *sql.Tx, editionID, versionID string) error {
	var id string
	return tx.QueryRowContext(ctx, `
		UPDATE story_editions
		SET published_version_id = $2,
		    updated_at = now()
		WHERE id = $1
		RETURNING id
	`, editionID, versionID).Scan(&id)
}

func clearStoryEditionPublishedPointers(ctx context.Context, tx *sql.Tx, storyID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE story_editions
		SET published_version_id = NULL,
		    updated_at = now()
		WHERE story_id = $1
		  AND published_version_id IS NOT NULL
	`, storyID)
	return err
}
