package db

import (
	"fmt"
	"strings"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

// AdminUnpublish atomically removes only the public pointer. Immutable
// versions, the draft pointer, and reading progress remain untouched.
func (s *Store) AdminUnpublish(accountID, slug string) (model.AdminStoryStatusResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil {
		return model.AdminStoryStatusResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminStoryStatusResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, accountID, slug, true)
	if err != nil {
		return model.AdminStoryStatusResponse{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE stories
		SET published_version_id = NULL,
		    is_published = false,
		    updated_at = CASE
		      WHEN published_version_id IS NOT NULL OR is_published THEN now()
		      ELSE updated_at
		    END
		WHERE id = $1
		RETURNING updated_at
	`, story.ID).Scan(&story.UpdatedAt); err != nil {
		return model.AdminStoryStatusResponse{}, err
	}
	story.IsPublished = false
	story.PublishedVersionID = nil

	inspected, err := inspectAdminStory(ctx, tx, story)
	if err != nil {
		return model.AdminStoryStatusResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryStatusResponse{}, err
	}
	return adminStoryStatusResponse(inspected), nil
}
