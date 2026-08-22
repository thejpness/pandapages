package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
)

const (
	defaultStoryOrchestrationEditorialReviewLimit = 50
	maxStoryOrchestrationEditorialReviewLimit     = 100
)

// CreateStoryOrchestrationEditorialReview persists one immutable review event.
// The caller must first validate the exact completed run through the editorial
// review service; this method still validates all server-owned event input.
func (s *Store) CreateStoryOrchestrationEditorialReview(
	input model.AdminStoryOrchestrationEditorialReviewCreateInput,
) (model.AdminStoryOrchestrationEditorialReview, error) {
	input.RunID = canonicalStoryOrchestrationEditorialReviewID(input.RunID)
	input.ReviewerPrincipalID = canonicalStoryOrchestrationEditorialReviewID(input.ReviewerPrincipalID)
	input.ReviewerAccountID = canonicalStoryOrchestrationEditorialReviewID(input.ReviewerAccountID)
	if err := input.Validate(); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	defer func() { _ = tx.Rollback() }()

	review, err := scanStoryOrchestrationEditorialReview(tx.QueryRowContext(ctx, `
		INSERT INTO story_orchestration_run_editorial_reviews (
			run_id,
			decision,
			reviewer_principal_id,
			reviewer_account_id
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id, run_id, decision, reviewer_principal_id, reviewer_account_id, created_at
	`, input.RunID, input.Decision, input.ReviewerPrincipalID, input.ReviewerAccountID))
	if err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	return review, nil
}

// ListStoryOrchestrationEditorialReviews returns bounded newest-first review
// events after confirming that the exact orchestration run exists. It does not
// decode the run's potentially large retained artifacts; creation owns that
// stronger validation boundary.
func (s *Store) ListStoryOrchestrationEditorialReviews(
	runID string,
	limit int,
) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error) {
	runID = canonicalStoryOrchestrationEditorialReviewID(runID)
	if runID == "" {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, fmt.Errorf("story orchestration run ID is invalid")
	}
	if limit == 0 {
		limit = defaultStoryOrchestrationEditorialReviewLimit
	}
	if limit < 1 || limit > maxStoryOrchestrationEditorialReviewLimit {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, fmt.Errorf("story orchestration editorial review history limit is invalid")
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM story_orchestration_runs WHERE id = $1`, runID).Scan(&existingID); err != nil {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, run_id, decision, reviewer_principal_id, reviewer_account_id, created_at
		FROM story_orchestration_run_editorial_reviews
		WHERE run_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, runID, limit)
	if err != nil {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
	}
	defer rows.Close()

	items := make([]model.AdminStoryOrchestrationEditorialReview, 0, limit)
	for rows.Next() {
		review, err := scanStoryOrchestrationEditorialReview(rows)
		if err != nil {
			return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
		}
		items = append(items, review)
	}
	if err := rows.Err(); err != nil {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryOrchestrationEditorialReviewsListResponse{}, err
	}
	return model.AdminStoryOrchestrationEditorialReviewsListResponse{Items: items}, nil
}

type storyOrchestrationEditorialReviewScanner interface {
	Scan(...any) error
}

func scanStoryOrchestrationEditorialReview(scanner storyOrchestrationEditorialReviewScanner) (model.AdminStoryOrchestrationEditorialReview, error) {
	var (
		review    model.AdminStoryOrchestrationEditorialReview
		createdAt time.Time
	)
	if err := scanner.Scan(
		&review.ID,
		&review.RunID,
		&review.Decision,
		&review.ReviewerPrincipalID,
		&review.ReviewerAccountID,
		&createdAt,
	); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	review.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	if err := review.Validate(); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, fmt.Errorf("stored story orchestration editorial review is invalid")
	}
	return review, nil
}

func canonicalStoryOrchestrationEditorialReviewID(raw string) string {
	value := strings.TrimSpace(raw)
	if !accountIDRe.MatchString(value) {
		return ""
	}
	return strings.ToLower(value)
}
