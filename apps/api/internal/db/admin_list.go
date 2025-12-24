package db

import (
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
)

func (s *Store) AdminListStories(accountID string) (model.AdminStoriesListResponse, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return model.AdminStoriesListResponse{}, fmt.Errorf("account required")
	}

	ctx, cancel := s.ctx()
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			slug,
			title,
			author,
			language,
			is_published,
			created_at,
			updated_at,
			draft_version_id,
			published_version_id
		FROM stories
		WHERE account_id = $1
		ORDER BY updated_at DESC
		LIMIT 200
	`, accountID)
	if err != nil {
		return model.AdminStoriesListResponse{}, err
	}
	defer rows.Close()

	items := make([]model.AdminStoryListItem, 0, 64)

	for rows.Next() {
		var (
			slug      string
			title     string
			author    *string
			language  string
			published bool

			created time.Time
			updated time.Time

			draftID     *string
			publishedID *string
		)

		if err := rows.Scan(
			&slug,
			&title,
			&author,
			&language,
			&published,
			&created,
			&updated,
			&draftID,
			&publishedID,
		); err != nil {
			return model.AdminStoriesListResponse{}, err
		}

		items = append(items, model.AdminStoryListItem{
			Slug:               slug,
			Title:              title,
			Author:             author,
			Language:           language,
			IsPublished:        published,
			CreatedAt:          created.UTC().Format(time.RFC3339),
			UpdatedAt:          updated.UTC().Format(time.RFC3339),
			DraftVersionID:     draftID,
			PublishedVersionID: publishedID,
		})
	}

	if err := rows.Err(); err != nil {
		return model.AdminStoriesListResponse{}, err
	}

	return model.AdminStoriesListResponse{Items: items}, nil
}
