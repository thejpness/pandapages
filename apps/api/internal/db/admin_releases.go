package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

type normalizedAdminReleaseEdition struct {
	EditionKey model.AdminStoryEditionKey
	VersionID  string
}

type storedAdminRelease struct {
	ID        string
	Number    int
	CreatedAt time.Time
	Summary   model.AdminReleaseSummary
}

type adminReleaseInspection struct {
	Current                *model.AdminReleaseSummary
	Releases               []model.AdminReleaseSummary
	CompatibilityPublished *model.AdminVersionPointerSummary
	HasAnyDraft            bool
	HasUnreleasedDraft     bool
	RepairRequired         bool
}

func normalizeAdminReleaseRequest(
	req model.AdminCreateReleaseRequest,
) ([]normalizedAdminReleaseEdition, error) {
	if len(req.Editions) < 1 || len(req.Editions) > len(model.AdminStoryEditionKeys()) {
		return nil, &model.AdminValidationError{Issues: []model.AdminValidationIssue{{
			Field:   "editions",
			Code:    "invalid_count",
			Message: "Choose between one and five reading editions",
		}}}
	}

	byKey := make(map[model.AdminStoryEditionKey]string, len(req.Editions))
	versionIDs := make(map[string]struct{}, len(req.Editions))
	issues := make([]model.AdminValidationIssue, 0, 2)
	for _, item := range req.Editions {
		key := model.AdminStoryEditionKey(strings.TrimSpace(string(item.EditionKey)))
		versionID := strings.TrimSpace(item.VersionID)
		if !model.ValidAdminStoryEditionKey(key) {
			issues = append(issues, model.AdminValidationIssue{
				Field:   "editions",
				Code:    "invalid",
				Message: "Choose only supported Panda Pages reading editions",
			})
			continue
		}
		if _, exists := byKey[key]; exists {
			issues = append(issues, model.AdminValidationIssue{
				Field:   "editions",
				Code:    "duplicate",
				Message: "Each reading edition can appear only once",
			})
			continue
		}
		if !accountIDRe.MatchString(versionID) {
			issues = append(issues, model.AdminValidationIssue{
				Field:   "editions." + string(key) + ".versionId",
				Code:    "invalid",
				Message: "Choose a valid immutable edition version",
			})
			continue
		}
		if _, exists := versionIDs[versionID]; exists {
			issues = append(issues, model.AdminValidationIssue{
				Field:   "editions",
				Code:    "duplicate_version",
				Message: "A version can belong to only one release edition",
			})
			continue
		}
		byKey[key] = versionID
		versionIDs[versionID] = struct{}{}
	}
	if len(issues) > 0 {
		return nil, &model.AdminValidationError{Issues: issues}
	}

	normalized := make([]normalizedAdminReleaseEdition, 0, len(byKey))
	for _, key := range model.AdminStoryEditionKeys() {
		if versionID, ok := byKey[key]; ok {
			normalized = append(normalized, normalizedAdminReleaseEdition{
				EditionKey: key,
				VersionID:  versionID,
			})
		}
	}
	if len(normalized) != len(req.Editions) {
		return nil, &model.AdminValidationError{Issues: []model.AdminValidationIssue{{
			Field:   "editions",
			Code:    "invalid",
			Message: "Choose valid reading editions for this release",
		}}}
	}
	return normalized, nil
}

func loadAdminReleaseSummaryTx(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	releaseID string,
) (storedAdminRelease, error) {
	var (
		release       storedAdminRelease
		releaseNumber int64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, release_number, created_at
		FROM story_releases
		WHERE id = $1
		  AND story_id = $2
	`, releaseID, storyID).Scan(
		&release.ID,
		&releaseNumber,
		&release.CreatedAt,
	); err != nil {
		return storedAdminRelease{}, err
	}
	release.Number = int(releaseNumber)
	if releaseNumber <= 0 || int64(release.Number) != releaseNumber {
		return storedAdminRelease{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT edition.edition_key, version.id, version.version
		FROM story_release_editions AS member
		JOIN story_editions AS edition
		  ON edition.id = member.edition_id
		 AND edition.story_id = member.story_id
		JOIN story_versions AS version
		  ON version.id = member.story_version_id
		 AND version.edition_id = member.edition_id
		WHERE member.release_id = $1
		  AND member.story_id = $2
	`, release.ID, storyID)
	if err != nil {
		return storedAdminRelease{}, err
	}
	defer rows.Close()

	byKey := make(map[model.AdminStoryEditionKey]model.AdminReleaseEditionSummary)
	for rows.Next() {
		var (
			key           string
			versionID     string
			versionNumber int64
		)
		if err := rows.Scan(&key, &versionID, &versionNumber); err != nil {
			return storedAdminRelease{}, err
		}
		editionKey := model.AdminStoryEditionKey(key)
		version := int(versionNumber)
		if !model.ValidAdminStoryEditionKey(editionKey) ||
			versionNumber <= 0 ||
			int64(version) != versionNumber {
			return storedAdminRelease{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		if _, exists := byKey[editionKey]; exists {
			return storedAdminRelease{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		byKey[editionKey] = model.AdminReleaseEditionSummary{
			EditionKey: editionKey,
			VersionID:  versionID,
			Version:    version,
		}
	}
	if err := rows.Err(); err != nil {
		return storedAdminRelease{}, err
	}
	if len(byKey) < 1 || len(byKey) > len(model.AdminStoryEditionKeys()) {
		return storedAdminRelease{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
	}

	editions := make([]model.AdminReleaseEditionSummary, 0, len(byKey))
	for _, key := range model.AdminStoryEditionKeys() {
		if edition, ok := byKey[key]; ok {
			editions = append(editions, edition)
		}
	}
	release.Summary = model.AdminReleaseSummary{
		Release:   release.Number,
		CreatedAt: release.CreatedAt.UTC().Format(time.RFC3339Nano),
		Editions:  editions,
	}
	return release, nil
}

func validateCurrentReleaseProjectionTx(
	ctx context.Context,
	tx *sql.Tx,
	story adminStoryRow,
) (*storedAdminRelease, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT edition_key, published_version_id
		FROM story_editions
		WHERE story_id = $1
		FOR UPDATE
	`, story.ID)
	if err != nil {
		return nil, err
	}
	projected := make(map[model.AdminStoryEditionKey]*string)
	for rows.Next() {
		var (
			key         string
			publishedID sql.NullString
		)
		if err := rows.Scan(&key, &publishedID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		editionKey := model.AdminStoryEditionKey(key)
		if !model.ValidAdminStoryEditionKey(editionKey) {
			_ = rows.Close()
			return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		projected[editionKey] = nullStringValue(publishedID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if story.CurrentReleaseID == nil {
		if story.IsPublished || story.PublishedVersionID != nil {
			return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		for _, publishedID := range projected {
			if publishedID != nil {
				return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
			}
		}
		return nil, nil
	}

	current, err := loadAdminReleaseSummaryTx(ctx, tx, story.ID, *story.CurrentReleaseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		return nil, err
	}
	if len(current.Summary.Editions) == 0 {
		return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
	}

	expected := make(map[model.AdminStoryEditionKey]string, len(current.Summary.Editions))
	for _, edition := range current.Summary.Editions {
		expected[edition.EditionKey] = edition.VersionID
	}
	for _, key := range model.AdminStoryEditionKeys() {
		wantID, included := expected[key]
		gotID := projected[key]
		if included {
			if gotID == nil || *gotID != wantID {
				return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
			}
		} else if gotID != nil {
			return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
	}

	classicVersionID, hasClassic := expected[model.AdminStoryEditionClassic]
	if hasClassic {
		if !story.IsPublished ||
			story.PublishedVersionID == nil ||
			*story.PublishedVersionID != classicVersionID {
			return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
	} else if story.IsPublished || story.PublishedVersionID != nil {
		return nil, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
	}
	return &current, nil
}

func releaseMatchesSelections(
	release model.AdminReleaseSummary,
	selections []normalizedAdminReleaseEdition,
) bool {
	if len(release.Editions) != len(selections) {
		return false
	}
	for index, selection := range selections {
		edition := release.Editions[index]
		if edition.EditionKey != selection.EditionKey ||
			edition.VersionID != selection.VersionID {
			return false
		}
	}
	return true
}

// AdminCreateRelease publishes one to five immutable edition versions as one
// atomic story release. Omitted editions are intentionally not live. Release
// records are never mutated; publishing a changed manifest creates a new
// release number for the story.
func (s *Store) AdminCreateRelease(
	accountID string,
	slug string,
	req model.AdminCreateReleaseRequest,
) (model.AdminCreateReleaseResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil {
		return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseNotFound)
	}
	selections, err := normalizeAdminReleaseRequest(req)
	if err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, accountID, slug, true)
	if errors.Is(err, model.ErrAdminStoryNotFound) {
		return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseNotFound)
	}
	if err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}

	current, err := validateCurrentReleaseProjectionTx(ctx, tx, story)
	if err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}

	summaries := make([]model.AdminReleaseEditionSummary, 0, len(selections))
	editionIDs := make(map[model.AdminStoryEditionKey]string, len(selections))
	for _, selection := range selections {
		editionID, err := loadStoryEditionID(ctx, tx, story.ID, selection.EditionKey, true)
		if errors.Is(err, sql.ErrNoRows) {
			return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseNotFound)
		}
		if err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
		if err := requireVersionInEdition(ctx, tx, story.ID, editionID, selection.VersionID); errors.Is(err, sql.ErrNoRows) {
			return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseNotFound)
		} else if err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
		snapshot, err := validateStoredReaderVersion(ctx, tx, story.ID, selection.VersionID, slug)
		if errors.Is(err, errStoredVersionInvalid) {
			return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseNotFound)
		}
		if err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
		editionIDs[selection.EditionKey] = editionID
		summaries = append(summaries, model.AdminReleaseEditionSummary{
			EditionKey: selection.EditionKey,
			VersionID:  selection.VersionID,
			Version:    snapshot.Version,
		})
	}

	if current != nil && releaseMatchesSelections(current.Summary, selections) {
		if err := tx.Commit(); err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
		return model.AdminCreateReleaseResponse{
			Slug:    slug,
			Outcome: model.AdminReleaseOutcomeReusedCurrent,
			Release: current.Summary,
		}, nil
	}

	var nextRelease int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(release_number), 0) + 1
		FROM story_releases
		WHERE story_id = $1
	`, story.ID).Scan(&nextRelease); err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}
	nextReleaseValue := int(nextRelease)
	if nextRelease <= 0 || int64(nextReleaseValue) != nextRelease {
		return model.AdminCreateReleaseResponse{}, fmt.Errorf("%w", model.ErrAdminReleaseInvalid)
	}

	var (
		releaseID string
		createdAt time.Time
	)
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO story_releases (story_id, release_number)
		VALUES ($1, $2)
		RETURNING id, created_at
	`, story.ID, nextReleaseValue).Scan(&releaseID, &createdAt); err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}

	for _, summary := range summaries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO story_release_editions (
				release_id, story_id, edition_id, story_version_id
			)
			VALUES ($1, $2, $3, $4)
		`, releaseID, story.ID, editionIDs[summary.EditionKey], summary.VersionID); err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
	}

	if err := clearStoryEditionPublishedPointers(ctx, tx, story.ID); err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}
	for _, summary := range summaries {
		if err := setEditionPublishedPointer(
			ctx,
			tx,
			editionIDs[summary.EditionKey],
			summary.VersionID,
		); err != nil {
			return model.AdminCreateReleaseResponse{}, err
		}
	}

	// Lifecycle 6 keeps the existing Reader contract Classic-only. A release
	// without Classic is fully published in Story Studio, but remains absent
	// from Reader/Library until edition selection is introduced in Lifecycle 7.
	var compatibilityVersionID *string
	for _, summary := range summaries {
		if summary.EditionKey == model.AdminStoryEditionClassic {
			value := summary.VersionID
			compatibilityVersionID = &value
			break
		}
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE stories
		SET current_release_id = $2,
		    published_version_id = $3,
		    is_published = ($3::uuid IS NOT NULL),
		    updated_at = now()
		WHERE id = $1
		RETURNING updated_at
	`, story.ID, releaseID, compatibilityVersionID).Scan(&story.UpdatedAt); err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}

	release := model.AdminReleaseSummary{
		Release:   nextReleaseValue,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
		Editions:  summaries,
	}
	if err := tx.Commit(); err != nil {
		return model.AdminCreateReleaseResponse{}, err
	}
	return model.AdminCreateReleaseResponse{
		Slug:    slug,
		Outcome: model.AdminReleaseOutcomeCreated,
		Release: release,
	}, nil
}

func inspectAdminReleaseState(
	ctx context.Context,
	tx *sql.Tx,
	story adminStoryRow,
	editions map[model.AdminStoryEditionKey]inspectedAdminEdition,
) (adminReleaseInspection, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM story_releases
		WHERE story_id = $1
		ORDER BY release_number DESC, id ASC
	`, story.ID)
	if err != nil {
		return adminReleaseInspection{}, err
	}
	releaseIDs := make([]string, 0, 8)
	for rows.Next() {
		var releaseID string
		if err := rows.Scan(&releaseID); err != nil {
			_ = rows.Close()
			return adminReleaseInspection{}, err
		}
		releaseIDs = append(releaseIDs, releaseID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return adminReleaseInspection{}, err
	}
	if err := rows.Close(); err != nil {
		return adminReleaseInspection{}, err
	}

	inspection := adminReleaseInspection{
		Releases: make([]model.AdminReleaseSummary, 0, len(releaseIDs)),
	}
	var currentID string
	if story.CurrentReleaseID != nil {
		currentID = *story.CurrentReleaseID
	}
	currentFound := currentID == ""

	for _, releaseID := range releaseIDs {
		release, err := loadAdminReleaseSummaryTx(ctx, tx, story.ID, releaseID)
		if err != nil {
			if errors.Is(err, model.ErrAdminReleaseInvalid) {
				return adminReleaseInspection{}, fmt.Errorf("stored story release requires repair")
			}
			return adminReleaseInspection{}, err
		}
		inspection.Releases = append(inspection.Releases, release.Summary)
		if releaseID == currentID {
			currentFound = true
			summary := release.Summary
			inspection.Current = &summary
		}
	}
	if !currentFound {
		inspection.RepairRequired = true
	}

	liveByKey := make(map[model.AdminStoryEditionKey]model.AdminReleaseEditionSummary)
	if inspection.Current != nil {
		for _, live := range inspection.Current.Editions {
			liveByKey[live.EditionKey] = live
			edition := editions[live.EditionKey]
			ready := false
			for _, version := range edition.Versions {
				if version.Summary.VersionID == live.VersionID &&
					version.Summary.Version == live.Version &&
					version.Summary.Health == model.AdminVersionHealthReady {
					ready = true
					break
				}
			}
			if !ready {
				inspection.RepairRequired = true
			}
		}
	}

	for _, key := range model.AdminStoryEditionKeys() {
		edition := editions[key]
		if edition.Row != nil && edition.Row.DraftVersionID != nil {
			inspection.HasAnyDraft = true
			live, included := liveByKey[key]
			if !included || *edition.Row.DraftVersionID != live.VersionID {
				inspection.HasUnreleasedDraft = true
			}
		}

		var expectedPublished *string
		if live, ok := liveByKey[key]; ok {
			value := live.VersionID
			expectedPublished = &value
		}
		var actualPublished *string
		if edition.Row != nil {
			actualPublished = edition.Row.PublishedVersionID
		}
		if !equalOptionalString(expectedPublished, actualPublished) {
			inspection.RepairRequired = true
		}
	}

	if inspection.Current == nil {
		if story.IsPublished || story.PublishedVersionID != nil {
			inspection.RepairRequired = true
		}
		return inspection, nil
	}

	if len(inspection.Current.Editions) == 0 {
		inspection.RepairRequired = true
		return inspection, nil
	}
	var classicCompatibility *model.AdminReleaseEditionSummary
	for index := range inspection.Current.Editions {
		edition := &inspection.Current.Editions[index]
		if edition.EditionKey == model.AdminStoryEditionClassic {
			classicCompatibility = edition
			break
		}
	}
	if classicCompatibility == nil {
		if story.IsPublished || story.PublishedVersionID != nil {
			inspection.RepairRequired = true
		}
		return inspection, nil
	}
	inspection.CompatibilityPublished = &model.AdminVersionPointerSummary{
		VersionID: classicCompatibility.VersionID,
		Version:   classicCompatibility.Version,
	}
	if !story.IsPublished ||
		story.PublishedVersionID == nil ||
		*story.PublishedVersionID != classicCompatibility.VersionID {
		inspection.RepairRequired = true
	}
	return inspection, nil
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
