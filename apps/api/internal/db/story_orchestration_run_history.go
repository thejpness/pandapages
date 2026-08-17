package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
)

const (
	defaultStoryOrchestrationRunHistoryLimit = 50
	maxStoryOrchestrationRunHistoryLimit     = 100
)

// ListCompletedStoryOrchestrationRuns returns bounded newest-first metadata
// for one exact existing source version. It deliberately does not decode or
// revalidate each run's retained artifact envelope; detailed evidence reads
// use GetCompletedStoryOrchestrationRun instead.
func (s *Store) ListCompletedStoryOrchestrationRuns(
	sourceVersionID string,
	limit int,
) (model.AdminStoryOrchestrationRunsListResponse, error) {
	sourceVersionID = strings.TrimSpace(sourceVersionID)
	if !accountIDRe.MatchString(sourceVersionID) {
		return model.AdminStoryOrchestrationRunsListResponse{}, fmt.Errorf("source version ID is invalid")
	}
	if limit == 0 {
		limit = defaultStoryOrchestrationRunHistoryLimit
	}
	if limit < 1 || limit > maxStoryOrchestrationRunHistoryLimit {
		return model.AdminStoryOrchestrationRunsListResponse{}, fmt.Errorf("story orchestration run history limit is invalid")
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminStoryOrchestrationRunsListResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM story_source_versions WHERE id = $1`, sourceVersionID).Scan(&existingID); err != nil {
		return model.AdminStoryOrchestrationRunsListResponse{}, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, source_version_id, source_sha256, semantic_result, created_at
		FROM story_orchestration_runs
		WHERE source_version_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, sourceVersionID, limit)
	if err != nil {
		return model.AdminStoryOrchestrationRunsListResponse{}, err
	}
	defer rows.Close()

	items := make([]model.AdminStoryOrchestrationRunSummary, 0, limit)
	for rows.Next() {
		var item model.AdminStoryOrchestrationRunSummary
		var createdAt time.Time
		if err := rows.Scan(&item.ID, &item.SourceVersionID, &item.SourceSHA256, &item.SemanticResult, &createdAt); err != nil {
			return model.AdminStoryOrchestrationRunsListResponse{}, err
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return model.AdminStoryOrchestrationRunsListResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryOrchestrationRunsListResponse{}, err
	}
	return model.AdminStoryOrchestrationRunsListResponse{Items: items}, nil
}
