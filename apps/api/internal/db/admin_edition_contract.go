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

type adminEditionRow struct {
	ID                 string
	Key                model.AdminStoryEditionKey
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DraftVersionID     *string
	PublishedVersionID *string
}

type inspectedAdminEdition struct {
	Row      *adminEditionRow
	Versions []inspectedAdminVersion
	Summary  model.AdminEditionSummary
	Detail   model.AdminEditionDetail
}

// inspectAdminStoryByEdition is the edition-aware Story Studio read model.
// Existing top-level fields remain a Classic compatibility view while the
// additive editions array always exposes the five canonical logical slots.
func inspectAdminStoryByEdition(
	ctx context.Context,
	tx *sql.Tx,
	story adminStoryRow,
) (inspectedAdminStory, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, edition_key, created_at, updated_at, draft_version_id, published_version_id
		FROM story_editions
		WHERE story_id = $1
	`, story.ID)
	if err != nil {
		return inspectedAdminStory{}, err
	}

	editionByID := make(map[string]*adminEditionRow, len(model.AdminStoryEditionKeys()))
	editionByKey := make(map[model.AdminStoryEditionKey]*adminEditionRow, len(model.AdminStoryEditionKeys()))
	for rows.Next() {
		var (
			row         adminEditionRow
			key         string
			draftID     sql.NullString
			publishedID sql.NullString
		)
		if err := rows.Scan(
			&row.ID,
			&key,
			&row.CreatedAt,
			&row.UpdatedAt,
			&draftID,
			&publishedID,
		); err != nil {
			_ = rows.Close()
			return inspectedAdminStory{}, err
		}
		row.Key = model.AdminStoryEditionKey(key)
		if !model.ValidAdminStoryEditionKey(row.Key) {
			_ = rows.Close()
			return inspectedAdminStory{}, fmt.Errorf("stored story edition key is invalid")
		}
		if _, exists := editionByKey[row.Key]; exists {
			_ = rows.Close()
			return inspectedAdminStory{}, fmt.Errorf("duplicate stored story edition")
		}
		row.DraftVersionID = nullStringValue(draftID)
		row.PublishedVersionID = nullStringValue(publishedID)
		editionByID[row.ID] = &row
		editionByKey[row.Key] = &row
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return inspectedAdminStory{}, err
	}
	if err := rows.Close(); err != nil {
		return inspectedAdminStory{}, err
	}

	versionRows, err := tx.QueryContext(ctx, `
		SELECT id, edition_id, version, created_at
		FROM story_versions
		WHERE story_id = $1
		ORDER BY version DESC, id ASC
	`, story.ID)
	if err != nil {
		return inspectedAdminStory{}, err
	}
	type versionRow struct {
		ID        string
		EditionID string
		Version   int64
		CreatedAt time.Time
	}
	rawVersions := make([]versionRow, 0, 8)
	for versionRows.Next() {
		var version versionRow
		if err := versionRows.Scan(
			&version.ID,
			&version.EditionID,
			&version.Version,
			&version.CreatedAt,
		); err != nil {
			_ = versionRows.Close()
			return inspectedAdminStory{}, err
		}
		rawVersions = append(rawVersions, version)
	}
	if err := versionRows.Err(); err != nil {
		_ = versionRows.Close()
		return inspectedAdminStory{}, err
	}
	if err := versionRows.Close(); err != nil {
		return inspectedAdminStory{}, err
	}

	grouped := make(map[model.AdminStoryEditionKey][]inspectedAdminVersion, len(model.AdminStoryEditionKeys()))
	repairByKey := make(map[model.AdminStoryEditionKey]bool, len(model.AdminStoryEditionKeys()))
	for _, version := range rawVersions {
		edition, ok := editionByID[version.EditionID]
		if !ok {
			return inspectedAdminStory{}, fmt.Errorf("story version references an unavailable edition")
		}
		versionNumber := positiveVersion(version.Version)
		inspected := inspectedAdminVersion{Summary: model.AdminVersionSummary{
			EditionKey:  edition.Key,
			VersionID:   version.ID,
			Version:     versionNumber,
			CreatedAt:   version.CreatedAt.UTC().Format(time.RFC3339Nano),
			IsDraft:     equalOptionalID(edition.DraftVersionID, version.ID),
			IsPublished: equalOptionalID(edition.PublishedVersionID, version.ID),
			Health:      model.AdminVersionHealthRepairRequired,
		}}

		inspection, validationErr := inspectAdminVersion(ctx, tx, story.ID, version.ID)
		switch {
		case validationErr == nil:
			inspected.Inspection = inspection
			inspected.Summary.Version = inspection.Version
			inspected.Summary.SegmentCount = inspection.SegmentCount
			inspected.Summary.WordCount = inspection.WordCount
			inspected.Summary.ChapterCount = inspection.ChapterCount
			inspected.Summary.Health = model.AdminVersionHealthReady
		case errors.Is(validationErr, errStoredVersionInvalid):
			repairByKey[edition.Key] = true
		case errors.Is(validationErr, sql.ErrNoRows):
			inspected.Summary.Health = model.AdminVersionHealthUnavailable
			repairByKey[edition.Key] = true
		default:
			return inspectedAdminStory{}, validationErr
		}
		if version.Version <= 0 || int64(versionNumber) != version.Version {
			repairByKey[edition.Key] = true
			inspected.Summary.Health = model.AdminVersionHealthRepairRequired
		}
		grouped[edition.Key] = append(grouped[edition.Key], inspected)
	}

	inspectedByKey := make(map[model.AdminStoryEditionKey]inspectedAdminEdition, len(model.AdminStoryEditionKeys()))
	for _, key := range model.AdminStoryEditionKeys() {
		row := editionByKey[key]
		versions := grouped[key]
		if versions == nil {
			versions = []inspectedAdminVersion{}
		}
		byID := make(map[string]inspectedAdminVersion, len(versions))
		publicVersions := make([]model.AdminVersionSummary, 0, len(versions))
		for _, version := range versions {
			byID[version.Summary.VersionID] = version
			publicVersions = append(publicVersions, version.Summary)
		}

		var (
			draftPointer     *model.AdminVersionPointerSummary
			publishedPointer *model.AdminVersionPointerSummary
			updatedAt        *string
		)
		repairRequired := repairByKey[key]
		if row != nil {
			draftValid := true
			publishedValid := true
			draftPointer, draftValid = adminPointer(row.DraftVersionID, byID)
			publishedPointer, publishedValid = adminPointer(row.PublishedVersionID, byID)
			if !draftValid || !publishedValid {
				repairRequired = true
			}
			value := row.UpdatedAt.UTC().Format(time.RFC3339Nano)
			updatedAt = &value
		} else if len(versions) != 0 {
			repairRequired = true
		}

		status := adminEditionStatus(row, len(versions), repairRequired)
		summary := model.AdminEditionSummary{
			EditionKey:       key,
			Status:           status,
			PublishedVersion: publishedPointer,
			DraftVersion:     draftPointer,
			VersionCount:     len(versions),
			UpdatedAt:        updatedAt,
		}
		detail := model.AdminEditionDetail{
			EditionKey:       summary.EditionKey,
			Status:           summary.Status,
			PublishedVersion: summary.PublishedVersion,
			DraftVersion:     summary.DraftVersion,
			VersionCount:     summary.VersionCount,
			UpdatedAt:        summary.UpdatedAt,
			Versions:         publicVersions,
		}
		inspectedByKey[key] = inspectedAdminEdition{
			Row:      row,
			Versions: versions,
			Summary:  summary,
			Detail:   detail,
		}
	}

	// Edition rows are authoritative for adapted reading state. Canonical
	// source metadata is authoritative for Story Studio identity whenever an
	// explicit source exists; an edition is never treated as the original.
	source, err := inspectAdminStorySource(ctx, tx, story.ID)
	if err != nil {
		return inspectedAdminStory{}, err
	}
	editionMetadata := selectEditionMetadata(inspectedByKey)
	title := "Story requires repair"
	language := "und"
	var author *string
	var sourceURL *string
	rights := map[string]any{}
	metadataAvailable := false
	if source.Summary.Status == model.AdminSourceStatusReady && source.Current != nil {
		title = source.Current.Title
		author = cloneString(source.Current.Author)
		language = source.Current.Language
		rights = cloneJSONMap(source.Current.Rights)
		sourceURL = cloneString(source.Current.SourceURL)
		metadataAvailable = true
	} else if editionMetadata != nil {
		title = editionMetadata.Title
		author = cloneString(editionMetadata.Author)
		language = editionMetadata.Language
		rights = cloneJSONMap(editionMetadata.Rights)
		sourceURL = cloneString(editionMetadata.SourceURL)
		metadataAvailable = true
	}

	editionSummaries := make([]model.AdminEditionSummary, 0, len(model.AdminStoryEditionKeys()))
	editionDetails := make([]model.AdminEditionDetail, 0, len(model.AdminStoryEditionKeys()))
	for _, key := range model.AdminStoryEditionKeys() {
		edition := inspectedByKey[key]
		editionSummaries = append(editionSummaries, edition.Summary)
		editionDetails = append(editionDetails, edition.Detail)
	}

	classic := inspectedByKey[model.AdminStoryEditionClassic]
	topLevelStatus := adminStoryStatusFromEdition(classic.Summary.Status)
	if !metadataAvailable || source.Summary.Status == model.AdminSourceStatusRepairRequired {
		topLevelStatus = model.AdminStoryStatusRepairRequired
	}

	return inspectedAdminStory{
		Row: story,
		Summary: model.AdminStorySummary{
			Slug:             story.Slug,
			Title:            title,
			Author:           author,
			Language:         language,
			Rights:           rights,
			SourceURL:        sourceURL,
			Status:           topLevelStatus,
			PublishedVersion: classic.Summary.PublishedVersion,
			DraftVersion:     classic.Summary.DraftVersion,
			VersionCount:     classic.Summary.VersionCount,
			UpdatedAt:        story.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Source:           source.Summary,
			Editions:         editionSummaries,
		},
		Versions: classic.Detail.Versions,
		Editions: editionDetails,
	}, nil
}

func adminEditionStatus(
	row *adminEditionRow,
	versionCount int,
	repairRequired bool,
) model.AdminEditionStatus {
	if repairRequired {
		return model.AdminEditionStatusRepairRequired
	}
	if row == nil || versionCount == 0 {
		return model.AdminEditionStatusEmpty
	}
	if row.PublishedVersionID != nil {
		if row.DraftVersionID != nil && *row.DraftVersionID != *row.PublishedVersionID {
			return model.AdminEditionStatusPublishedWithDraft
		}
		return model.AdminEditionStatusPublished
	}
	if row.DraftVersionID != nil {
		return model.AdminEditionStatusDraftOnly
	}
	return model.AdminEditionStatusUnpublished
}

func adminStoryStatusFromEdition(status model.AdminEditionStatus) model.AdminStoryStatus {
	switch status {
	case model.AdminEditionStatusDraftOnly:
		return model.AdminStoryStatusDraftOnly
	case model.AdminEditionStatusPublished:
		return model.AdminStoryStatusPublished
	case model.AdminEditionStatusPublishedWithDraft:
		return model.AdminStoryStatusPublishedWithDraft
	case model.AdminEditionStatusRepairRequired:
		return model.AdminStoryStatusRepairRequired
	case model.AdminEditionStatusEmpty, model.AdminEditionStatusUnpublished:
		return model.AdminStoryStatusUnpublished
	default:
		return model.AdminStoryStatusRepairRequired
	}
}

func selectEditionMetadata(
	editions map[model.AdminStoryEditionKey]inspectedAdminEdition,
) *normalizedStoredFrontmatter {
	keys := model.AdminStoryEditionKeys()
	ordered := make([]model.AdminStoryEditionKey, 0, len(keys))
	ordered = append(ordered, model.AdminStoryEditionClassic)
	for _, key := range keys {
		if key != model.AdminStoryEditionClassic {
			ordered = append(ordered, key)
		}
	}
	for _, key := range ordered {
		for _, version := range editions[key].Versions {
			if version.Summary.Health == model.AdminVersionHealthReady {
				metadata := version.Inspection.Frontmatter
				return &metadata
			}
		}
	}
	return nil
}

// AdminGetEditionVersionSource is the protected edition-aware source read.
// The legacy source route calls this with Classic so existing clients cannot
// accidentally follow a non-Classic version identifier.
func (s *Store) AdminGetEditionVersionSource(
	accountID string,
	slug string,
	editionKey model.AdminStoryEditionKey,
	versionID string,
) (model.AdminVersionSourceResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)
	editionKey = model.AdminStoryEditionKey(strings.TrimSpace(string(editionKey)))
	if !accountIDRe.MatchString(accountID) ||
		storyingest.ValidateSlug(slug) != nil ||
		!model.ValidAdminStoryEditionKey(editionKey) ||
		!accountIDRe.MatchString(versionID) {
		return model.AdminVersionSourceResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminVersionSourceResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, accountID, slug, false)
	if err != nil {
		return model.AdminVersionSourceResponse{}, err
	}

	var (
		editionID   string
		draftID     sql.NullString
		publishedID sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, draft_version_id, published_version_id
		FROM story_editions
		WHERE story_id = $1
		  AND edition_key = $2
	`, story.ID, editionKey).Scan(&editionID, &draftID, &publishedID); errors.Is(err, sql.ErrNoRows) {
		return model.AdminVersionSourceResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	} else if err != nil {
		return model.AdminVersionSourceResponse{}, err
	}

	var ownedVersionID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM story_versions
		WHERE id = $1
		  AND story_id = $2
		  AND edition_id = $3
	`, versionID, story.ID, editionID).Scan(&ownedVersionID); errors.Is(err, sql.ErrNoRows) {
		return model.AdminVersionSourceResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	} else if err != nil {
		return model.AdminVersionSourceResponse{}, err
	}

	snapshot, err := inspectStoredReaderVersion(ctx, tx, story.ID, versionID, story.Slug)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AdminVersionSourceResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	}
	if errors.Is(err, errStoredVersionInvalid) {
		return model.AdminVersionSourceResponse{}, fmt.Errorf("%w", model.ErrAdminVersionRepairRequired)
	}
	if err != nil {
		return model.AdminVersionSourceResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminVersionSourceResponse{}, err
	}

	draft := nullStringValue(draftID)
	published := nullStringValue(publishedID)
	return model.AdminVersionSourceResponse{
		Slug:         story.Slug,
		EditionKey:   editionKey,
		VersionID:    versionID,
		Version:      snapshot.Version,
		Title:        snapshot.Frontmatter.Title,
		Author:       snapshot.Frontmatter.Author,
		Language:     snapshot.Frontmatter.Language,
		Rights:       cloneJSONMap(snapshot.Frontmatter.Rights),
		SourceURL:    cloneString(snapshot.Frontmatter.SourceURL),
		Markdown:     snapshot.Markdown,
		RenderedHTML: snapshot.RenderedHTML,
		SegmentCount: snapshot.SegmentCount,
		WordCount:    snapshot.WordCount,
		ChapterCount: snapshot.ChapterCount,
		CreatedAt:    snapshot.CreatedAt.UTC().Format(time.RFC3339Nano),
		IsDraft:      equalOptionalID(draft, versionID),
		IsPublished:  equalOptionalID(published, versionID),
		Health:       model.AdminVersionHealthReady,
	}, nil
}
