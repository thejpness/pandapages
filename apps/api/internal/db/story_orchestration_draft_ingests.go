package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyingest"
	"pandapages/api/internal/storyorchestration"
)

type orchestrationDraftIngestEdition struct {
	key      model.AdminStoryEditionKey
	ingested storyingest.Output
}

type orchestrationDraftIngestTarget struct {
	storyID string
	slug    string
	source  adminSourceSnapshot
}

// CreateStoryOrchestrationDraftIngest creates four editable Story Studio
// drafts from one exact current approved review event. Every authorization,
// integrity, no-overwrite, and provenance check lives in this single short
// transaction so a review cannot become stale between checking and copying.
func (s *Store) CreateStoryOrchestrationDraftIngest(
	input model.AdminStoryOrchestrationDraftIngestInput,
) (model.AdminStoryOrchestrationDraftIngestResponse, error) {
	input.RunID = canonicalStoryOrchestrationEditorialReviewID(input.RunID)
	input.EditorialReviewID = canonicalStoryOrchestrationEditorialReviewID(input.EditorialReviewID)
	if err := input.Validate(); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// FOR UPDATE is the run-scoped serialization point. PR109 review inserts
	// take an FK key-share lock on this row, so a new rejection either commits
	// before this transaction sees currentness or waits for this commit.
	run, err := getCompletedStoryOrchestrationRunTx(ctx, tx, input.RunID, true)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}
	target, err := loadOrchestrationDraftIngestTarget(ctx, tx, run.SourceVersionID)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}
	editions, err := deterministicOrchestrationDraftEditions(run, target)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	if existing, found, err := loadExistingOrchestrationDraftIngest(ctx, tx, input, run, target, editions); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, err
		}
		return existing, nil
	}

	if err := requireCurrentApprovedOrchestrationReview(ctx, tx, input); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	slots, err := lockEmptyOrchestrationDraftEditionSlots(ctx, tx, target.storyID)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}
	for index, edition := range editions {
		if slots[index].key != edition.key {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, fmt.Errorf("%w: edition slot order", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		var existingVersionID string
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM story_versions
			WHERE edition_id = $1
			  AND content_hash = $2
		`, slots[index].id, edition.ingested.ContentHash).Scan(&existingVersionID)
		switch {
		case err == nil:
			return model.AdminStoryOrchestrationDraftIngestResponse{}, fmt.Errorf("%w: generated edition already has a historical version", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return model.AdminStoryOrchestrationDraftIngestResponse{}, err
		}
	}

	var (
		ingestID  string
		createdAt time.Time
	)
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO story_orchestration_run_draft_ingests (run_id, editorial_review_id)
		VALUES ($1, $2)
		RETURNING id, created_at
	`, input.RunID, input.EditorialReviewID).Scan(&ingestID, &createdAt); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM story_versions
		WHERE story_id = $1
	`, target.storyID).Scan(&nextVersion); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	responseEditions := make([]model.AdminStoryOrchestrationDraftIngestEdition, 0, len(editions))
	for index, edition := range editions {
		frontmatterJSON, err := json.Marshal(edition.ingested.Frontmatter)
		if err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, fmt.Errorf("%w: generated edition frontmatter", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		versionID, err := insertCanonicalStoryVersionTx(
			ctx,
			tx,
			target.storyID,
			slots[index].id,
			nextVersion+index,
			frontmatterJSON,
			edition.ingested,
		)
		if err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, err
		}
		if err := setEditionDraftPointer(ctx, tx, slots[index].id, versionID); err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO story_orchestration_run_draft_ingest_editions (
				draft_ingest_id,
				edition_id,
				story_version_id
			)
			VALUES ($1, $2, $3)
		`, ingestID, slots[index].id, versionID); err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, err
		}
		responseEditions = append(responseEditions, model.AdminStoryOrchestrationDraftIngestEdition{
			EditionKey: edition.key, EditionID: slots[index].id, StoryVersionID: versionID,
		})
	}
	if err := touchStoryAfterDraft(ctx, tx, target.storyID); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, err
	}

	return model.AdminStoryOrchestrationDraftIngestResponse{
		ID:                ingestID,
		RunID:             input.RunID,
		EditorialReviewID: input.EditorialReviewID,
		StorySlug:         target.slug,
		CreatedAt:         createdAt.UTC().Format(time.RFC3339Nano),
		Outcome:           model.AdminStoryOrchestrationDraftIngestOutcomeCreated,
		Editions:          responseEditions,
	}, nil
}

func loadOrchestrationDraftIngestTarget(
	ctx context.Context,
	tx *sql.Tx,
	sourceVersionID string,
) (orchestrationDraftIngestTarget, error) {
	source, err := loadStoryOrchestrationSourceVersion(ctx, tx, sourceVersionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errStoredSourceInvalid) {
			return orchestrationDraftIngestTarget{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
		}
		return orchestrationDraftIngestTarget{}, err
	}
	var target orchestrationDraftIngestTarget
	target.source = source
	if err := tx.QueryRowContext(ctx, `
		SELECT story.id, story.slug
		FROM story_source_versions AS version
		JOIN stories AS story ON story.id = version.story_id
		WHERE version.id = $1
		FOR UPDATE OF story
	`, sourceVersionID).Scan(&target.storyID, &target.slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return orchestrationDraftIngestTarget{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
		}
		return orchestrationDraftIngestTarget{}, err
	}
	if err := storyingest.ValidateSlug(target.slug); err != nil {
		return orchestrationDraftIngestTarget{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
	}
	return target, nil
}

func deterministicOrchestrationDraftEditions(
	run storyorchestration.PersistedRun,
	target orchestrationDraftIngestTarget,
) ([]orchestrationDraftIngestEdition, error) {
	keys := storygeneration.DerivedEditionKeysV2()
	if len(keys) != 4 || len(run.Result.Editions) != len(keys) {
		return nil, fmt.Errorf("%w: generated edition set", model.ErrAdminStoryOrchestrationDraftIngestConflict)
	}
	author := ""
	if target.source.Author != nil {
		author = *target.source.Author
	}
	sourceURL := ""
	if target.source.SourceURL != nil {
		sourceURL = *target.source.SourceURL
	}
	result := make([]orchestrationDraftIngestEdition, 0, len(keys))
	for index, key := range keys {
		artifact := run.Result.Editions[index]
		if artifact.EditionKey != key {
			return nil, fmt.Errorf("%w: generated edition order", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		ingested, err := storyingest.Ingest(storyingest.Input{
			Slug:      target.slug,
			Title:     target.source.Title,
			Author:    author,
			Markdown:  artifact.Markdown,
			Language:  target.source.Language,
			SourceURL: sourceURL,
			Rights:    cloneJSONMap(target.source.Rights),
		})
		if err != nil || ingested.Markdown != artifact.Markdown || ingested.ContentHash != artifact.ContentSHA256 {
			return nil, fmt.Errorf("%w: generated edition deterministic ingest", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		result = append(result, orchestrationDraftIngestEdition{key: key, ingested: ingested})
	}
	return result, nil
}

func requireCurrentApprovedOrchestrationReview(
	ctx context.Context,
	tx *sql.Tx,
	input model.AdminStoryOrchestrationDraftIngestInput,
) error {
	review, err := scanStoryOrchestrationEditorialReview(tx.QueryRowContext(ctx, `
		SELECT id, run_id, decision, reviewer_principal_id, reviewer_account_id, created_at
		FROM story_orchestration_run_editorial_reviews
		WHERE id = $1
		  AND run_id = $2
	`, input.EditorialReviewID, input.RunID))
	if err != nil {
		return err
	}
	if review.Decision != model.AdminStoryOrchestrationEditorialDecisionApproved {
		return fmt.Errorf("%w: editorial decision is not approved", model.ErrAdminStoryOrchestrationDraftIngestConflict)
	}
	var currentID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM story_orchestration_run_editorial_reviews
		WHERE run_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, input.RunID).Scan(&currentID); err != nil {
		return err
	}
	if currentID != input.EditorialReviewID {
		return fmt.Errorf("%w: editorial approval is no longer current", model.ErrAdminStoryOrchestrationDraftIngestConflict)
	}
	return nil
}

type lockedOrchestrationDraftEditionSlot struct {
	id  string
	key model.AdminStoryEditionKey
}

func lockEmptyOrchestrationDraftEditionSlots(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
) ([]lockedOrchestrationDraftEditionSlot, error) {
	keys := storygeneration.DerivedEditionKeysV2()
	slots := make([]lockedOrchestrationDraftEditionSlot, 0, len(keys))
	for _, key := range keys {
		if _, err := ensureStoryEdition(ctx, tx, storyID, key); err != nil {
			return nil, err
		}
		var (
			id      string
			draftID sql.NullString
		)
		if err := tx.QueryRowContext(ctx, `
			SELECT id, draft_version_id
			FROM story_editions
			WHERE story_id = $1
			  AND edition_key = $2
			FOR UPDATE
		`, storyID, key).Scan(&id, &draftID); err != nil {
			return nil, err
		}
		if draftID.Valid {
			return nil, fmt.Errorf("%w: editable draft already exists", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		slots = append(slots, lockedOrchestrationDraftEditionSlot{id: id, key: key})
	}
	return slots, nil
}

func loadExistingOrchestrationDraftIngest(
	ctx context.Context,
	tx *sql.Tx,
	input model.AdminStoryOrchestrationDraftIngestInput,
	run storyorchestration.PersistedRun,
	target orchestrationDraftIngestTarget,
	editions []orchestrationDraftIngestEdition,
) (model.AdminStoryOrchestrationDraftIngestResponse, bool, error) {
	var (
		ingestID       string
		storedRunID    string
		storedReviewID string
		createdAt      time.Time
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, run_id, editorial_review_id, created_at
		FROM story_orchestration_run_draft_ingests
		WHERE editorial_review_id = $1
	`, input.EditorialReviewID).Scan(&ingestID, &storedRunID, &storedReviewID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, nil
	}
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, err
	}
	if storedRunID != input.RunID || storedReviewID != input.EditorialReviewID || run.ID != input.RunID {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, fmt.Errorf("%w: persisted ingest identity", model.ErrAdminStoryOrchestrationDraftIngestConflict)
	}

	type storedMapping struct {
		editionID        string
		storyVersionID   string
		editionKey       model.AdminStoryEditionKey
		storyID          string
		versionEditionID string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT mapping.edition_id, mapping.story_version_id, edition.edition_key, version.story_id, version.edition_id
		FROM story_orchestration_run_draft_ingest_editions AS mapping
		JOIN story_editions AS edition ON edition.id = mapping.edition_id
		JOIN story_versions AS version ON version.id = mapping.story_version_id
		WHERE mapping.draft_ingest_id = $1
	`, ingestID)
	if err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, err
	}
	defer rows.Close()
	mappings := make(map[model.AdminStoryEditionKey]storedMapping, len(editions))
	for rows.Next() {
		var mapping storedMapping
		if err := rows.Scan(&mapping.editionID, &mapping.storyVersionID, &mapping.editionKey, &mapping.storyID, &mapping.versionEditionID); err != nil {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, false, err
		}
		if _, exists := mappings[mapping.editionKey]; exists {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, false, fmt.Errorf("%w: duplicate persisted ingest edition", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		mappings[mapping.editionKey] = mapping
	}
	if err := rows.Err(); err != nil {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, err
	}
	if len(mappings) != len(editions) {
		return model.AdminStoryOrchestrationDraftIngestResponse{}, false, fmt.Errorf("%w: incomplete persisted ingest", model.ErrAdminStoryOrchestrationDraftIngestConflict)
	}

	responseEditions := make([]model.AdminStoryOrchestrationDraftIngestEdition, 0, len(editions))
	for _, expected := range editions {
		mapping, ok := mappings[expected.key]
		if !ok || mapping.storyID != target.storyID || mapping.editionID != mapping.versionEditionID {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, false, fmt.Errorf("%w: persisted ingest edition binding", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		version, err := validateStoredReaderVersion(ctx, tx, target.storyID, mapping.storyVersionID, target.slug)
		if err != nil || version.Markdown != expected.ingested.Markdown || version.RenderedHTML != expected.ingested.RenderedHTML || version.ContentHash != expected.ingested.ContentHash {
			return model.AdminStoryOrchestrationDraftIngestResponse{}, false, fmt.Errorf("%w: persisted ingest version", model.ErrAdminStoryOrchestrationDraftIngestConflict)
		}
		responseEditions = append(responseEditions, model.AdminStoryOrchestrationDraftIngestEdition{
			EditionKey: expected.key, EditionID: mapping.editionID, StoryVersionID: mapping.storyVersionID,
		})
	}
	return model.AdminStoryOrchestrationDraftIngestResponse{
		ID:                ingestID,
		RunID:             input.RunID,
		EditorialReviewID: input.EditorialReviewID,
		StorySlug:         target.slug,
		CreatedAt:         createdAt.UTC().Format(time.RFC3339Nano),
		Outcome:           model.AdminStoryOrchestrationDraftIngestOutcomeReused,
		Editions:          responseEditions,
	}, true, nil
}
