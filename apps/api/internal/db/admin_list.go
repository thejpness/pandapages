package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/storyingest"
)

type adminStoryRow struct {
	ID                 string
	Slug               string
	IsPublished        bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DraftVersionID     *string
	PublishedVersionID *string
}

type inspectedAdminVersion struct {
	Summary    model.AdminVersionSummary
	Inspection adminVersionInspection
}

// adminVersionInspection is deliberately metadata-only. Catalogue and detail
// reads can establish a safe health state without selecting private Markdown,
// rendered story HTML, or rendered segment content into the application.
// The protected source route and write boundaries perform the deeper canonical
// snapshot validation when those bodies are actually required.
type adminVersionInspection struct {
	Version      int
	CreatedAt    time.Time
	Frontmatter  normalizedStoredFrontmatter
	SegmentCount int
	WordCount    int
	ChapterCount int
}

type inspectedAdminStory struct {
	Row      adminStoryRow
	Summary  model.AdminStorySummary
	Versions []model.AdminVersionSummary
}

func (s *Store) AdminListStories(accountID string) (model.AdminStoriesListResponse, error) {
	accountID = strings.TrimSpace(accountID)
	if !accountIDRe.MatchString(accountID) {
		return model.AdminStoriesListResponse{}, fmt.Errorf("account required")
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminStoriesListResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, slug, is_published, created_at, updated_at, draft_version_id, published_version_id
		FROM stories
		WHERE account_id = $1
		ORDER BY updated_at DESC, slug ASC
	`, accountID)
	if err != nil {
		return model.AdminStoriesListResponse{}, err
	}
	stories := make([]adminStoryRow, 0, 64)
	for rows.Next() {
		story, err := scanAdminStory(rows)
		if err != nil {
			_ = rows.Close()
			return model.AdminStoriesListResponse{}, err
		}
		stories = append(stories, story)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return model.AdminStoriesListResponse{}, err
	}
	if err := rows.Close(); err != nil {
		return model.AdminStoriesListResponse{}, err
	}

	items := make([]model.AdminStorySummary, 0, len(stories))
	for _, story := range stories {
		inspected, err := inspectAdminStory(ctx, tx, story)
		if err != nil {
			return model.AdminStoriesListResponse{}, err
		}
		items = append(items, inspected.Summary)
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoriesListResponse{}, err
	}
	return model.AdminStoriesListResponse{Items: items}, nil
}

func (s *Store) AdminGetStory(accountID, slug string) (model.AdminStoryDetailResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil {
		return model.AdminStoryDetailResponse{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminStoryDetailResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, accountID, slug, false)
	if err != nil {
		return model.AdminStoryDetailResponse{}, err
	}
	inspected, err := inspectAdminStory(ctx, tx, story)
	if err != nil {
		return model.AdminStoryDetailResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminStoryDetailResponse{}, err
	}
	return adminStoryDetail(inspected), nil
}

func (s *Store) AdminGetVersionSource(accountID, slug, versionID string) (model.AdminVersionSourceResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)
	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil || !accountIDRe.MatchString(versionID) {
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

	return model.AdminVersionSourceResponse{
		Slug:         story.Slug,
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
		IsDraft:      equalOptionalID(story.DraftVersionID, versionID),
		IsPublished:  story.IsPublished && equalOptionalID(story.PublishedVersionID, versionID),
		Health:       model.AdminVersionHealthReady,
	}, nil
}

type adminStoryScanner interface {
	Scan(...any) error
}

func scanAdminStory(scanner adminStoryScanner) (adminStoryRow, error) {
	var (
		story       adminStoryRow
		draftID     sql.NullString
		publishedID sql.NullString
	)
	if err := scanner.Scan(
		&story.ID,
		&story.Slug,
		&story.IsPublished,
		&story.CreatedAt,
		&story.UpdatedAt,
		&draftID,
		&publishedID,
	); err != nil {
		return adminStoryRow{}, err
	}
	story.DraftVersionID = nullStringValue(draftID)
	story.PublishedVersionID = nullStringValue(publishedID)
	return story, nil
}

func loadAdminStory(ctx context.Context, tx *sql.Tx, accountID, slug string, lock bool) (adminStoryRow, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	story, err := scanAdminStory(tx.QueryRowContext(ctx, `
		SELECT id, slug, is_published, created_at, updated_at, draft_version_id, published_version_id
		FROM stories
		WHERE account_id = $1
		  AND slug = $2
	`+lockClause, accountID, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return adminStoryRow{}, fmt.Errorf("%w", model.ErrAdminStoryNotFound)
	}
	return story, err
}

func inspectAdminStory(ctx context.Context, tx *sql.Tx, story adminStoryRow) (inspectedAdminStory, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, version, created_at
		FROM story_versions
		WHERE story_id = $1
		ORDER BY version DESC, id ASC
	`, story.ID)
	if err != nil {
		return inspectedAdminStory{}, err
	}
	type versionRow struct {
		ID        string
		Version   int64
		CreatedAt time.Time
	}
	versionRows := make([]versionRow, 0, 8)
	for rows.Next() {
		var version versionRow
		if err := rows.Scan(&version.ID, &version.Version, &version.CreatedAt); err != nil {
			_ = rows.Close()
			return inspectedAdminStory{}, err
		}
		versionRows = append(versionRows, version)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return inspectedAdminStory{}, err
	}
	if err := rows.Close(); err != nil {
		return inspectedAdminStory{}, err
	}

	versions := make([]inspectedAdminVersion, 0, len(versionRows))
	byID := make(map[string]inspectedAdminVersion, len(versionRows))
	repairRequired := false
	for _, version := range versionRows {
		versionNumber := positiveVersion(version.Version)
		inspected := inspectedAdminVersion{Summary: model.AdminVersionSummary{
			VersionID:   version.ID,
			Version:     versionNumber,
			CreatedAt:   version.CreatedAt.UTC().Format(time.RFC3339Nano),
			IsDraft:     equalOptionalID(story.DraftVersionID, version.ID),
			IsPublished: story.IsPublished && equalOptionalID(story.PublishedVersionID, version.ID),
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
			repairRequired = true
		case errors.Is(validationErr, sql.ErrNoRows):
			inspected.Summary.Health = model.AdminVersionHealthUnavailable
			repairRequired = true
		default:
			return inspectedAdminStory{}, validationErr
		}
		if version.Version <= 0 || int64(versionNumber) != version.Version {
			repairRequired = true
			inspected.Summary.Health = model.AdminVersionHealthRepairRequired
		}
		versions = append(versions, inspected)
		byID[version.ID] = inspected
	}

	draftPointer, draftValid := adminPointer(story.DraftVersionID, byID)
	publishedPointer, publishedValid := adminPointer(story.PublishedVersionID, byID)
	if !draftValid || !publishedValid ||
		(story.IsPublished && story.PublishedVersionID == nil) ||
		(!story.IsPublished && story.PublishedVersionID != nil) ||
		len(versions) == 0 {
		repairRequired = true
	}

	metadata := selectAdminMetadata(story, versions, byID)
	if metadata == nil {
		repairRequired = true
	}
	status := adminStoryStatus(story, repairRequired)
	title := "Story requires repair"
	language := "und"
	var author *string
	var sourceURL *string
	rights := map[string]any{}
	if metadata != nil {
		title = metadata.Title
		author = cloneString(metadata.Author)
		language = metadata.Language
		rights = cloneJSONMap(metadata.Rights)
		sourceURL = cloneString(metadata.SourceURL)
	}

	publicVersions := make([]model.AdminVersionSummary, 0, len(versions))
	for _, version := range versions {
		publicVersions = append(publicVersions, version.Summary)
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
			Status:           status,
			PublishedVersion: publishedPointer,
			DraftVersion:     draftPointer,
			VersionCount:     len(versions),
			UpdatedAt:        story.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
		Versions: publicVersions,
	}, nil
}

func inspectAdminVersion(
	ctx context.Context,
	queryer storedVersionQueryer,
	storyID string,
	versionID string,
) (adminVersionInspection, error) {
	var (
		version              int64
		createdAt            time.Time
		frontmatterJSON      string
		markdownReadable     sql.NullBool
		renderedHTMLReadable sql.NullBool
		contentHash          sql.NullString
		computedContentHash  sql.NullString
	)
	if err := queryer.QueryRowContext(ctx, `
		SELECT
			version,
			created_at,
			frontmatter::text,
			btrim(markdown) <> '',
			btrim(rendered_html) <> '',
			content_hash,
			pg_catalog.encode(
				pg_catalog.sha256(pg_catalog.convert_to(markdown, 'UTF8')),
				'hex'
			)
		FROM story_versions
		WHERE id = $1
		  AND story_id = $2
	`, versionID, storyID).Scan(
		&version,
		&createdAt,
		&frontmatterJSON,
		&markdownReadable,
		&renderedHTMLReadable,
		&contentHash,
		&computedContentHash,
	); err != nil {
		return adminVersionInspection{}, err
	}

	versionValue := int(version)
	if version <= 0 || int64(versionValue) != version ||
		!markdownReadable.Valid || !markdownReadable.Bool ||
		!renderedHTMLReadable.Valid || !renderedHTMLReadable.Bool ||
		!contentHash.Valid || !computedContentHash.Valid ||
		!readercontract.ValidContentKey(contentHash.String) ||
		contentHash.String != computedContentHash.String {
		return adminVersionInspection{}, fmt.Errorf("%w: immutable version metadata", errStoredVersionInvalid)
	}
	frontmatter, err := normalizeStoredFrontmatter([]byte(frontmatterJSON))
	if err != nil {
		return adminVersionInspection{}, fmt.Errorf("%w: immutable metadata", errStoredVersionInvalid)
	}

	rows, err := queryer.QueryContext(ctx, `
		SELECT
			ordinal,
			segment_kind,
			heading_level,
			content_key,
			content_occurrence,
			chapter_key,
			chapter_occurrence,
			word_count,
			btrim(markdown) <> '',
			btrim(rendered_html) <> '',
			pg_catalog.encode(
				pg_catalog.sha256(
					pg_catalog.convert_to(
						segment_kind
						  || pg_catalog.chr(31)
						  || COALESCE(heading_level, 0)::text
						  || pg_catalog.chr(31)
						  || pg_catalog.replace(
							  pg_catalog.replace(markdown, E'\r\n', E'\n'),
							  E'\r',
							  E'\n'
							),
						'UTF8'
					)
				),
				'hex'
			)
		FROM story_segments
		WHERE story_version_id = $1
		ORDER BY ordinal ASC
	`, versionID)
	if err != nil {
		return adminVersionInspection{}, err
	}
	defer rows.Close()

	identities := make([]readercontract.StoredSegmentIdentity, 0, 32)
	wordCount := int64(0)
	for rows.Next() {
		var (
			ordinal           sql.NullInt64
			kind              sql.NullString
			headingLevel      sql.NullInt64
			contentKey        sql.NullString
			contentOccurrence sql.NullInt64
			chapterKey        sql.NullString
			chapterOccurrence sql.NullInt64
			segmentWordCount  sql.NullInt64
			markdownReadable  sql.NullBool
			renderedReadable  sql.NullBool
			computedKey       sql.NullString
		)
		if err := rows.Scan(
			&ordinal,
			&kind,
			&headingLevel,
			&contentKey,
			&contentOccurrence,
			&chapterKey,
			&chapterOccurrence,
			&segmentWordCount,
			&markdownReadable,
			&renderedReadable,
			&computedKey,
		); err != nil {
			return adminVersionInspection{}, err
		}
		if !ordinal.Valid || !kind.Valid || !contentKey.Valid || !contentOccurrence.Valid ||
			!segmentWordCount.Valid || !markdownReadable.Valid || !markdownReadable.Bool ||
			!renderedReadable.Valid || !renderedReadable.Bool || !computedKey.Valid ||
			computedKey.String != contentKey.String {
			return adminVersionInspection{}, fmt.Errorf("%w: incomplete segment", errStoredVersionInvalid)
		}

		ordinalValue := int(ordinal.Int64)
		contentOccurrenceValue := int(contentOccurrence.Int64)
		if ordinal.Int64 <= 0 || int64(ordinalValue) != ordinal.Int64 ||
			contentOccurrence.Int64 <= 0 || int64(contentOccurrenceValue) != contentOccurrence.Int64 ||
			segmentWordCount.Int64 < 0 || segmentWordCount.Int64 > maxSafeJSONInteger-wordCount {
			return adminVersionInspection{}, fmt.Errorf("%w: segment numeric value", errStoredVersionInvalid)
		}

		identity := readercontract.StoredSegmentIdentity{
			Ordinal:           ordinalValue,
			Kind:              readercontract.SegmentKind(kind.String),
			ContentKey:        contentKey.String,
			ContentOccurrence: contentOccurrenceValue,
		}
		if headingLevel.Valid {
			value := int(headingLevel.Int64)
			if int64(value) != headingLevel.Int64 {
				return adminVersionInspection{}, fmt.Errorf("%w: heading level", errStoredVersionInvalid)
			}
			identity.HeadingLevel = &value
		}
		if chapterKey.Valid != chapterOccurrence.Valid {
			return adminVersionInspection{}, fmt.Errorf("%w: incomplete chapter identity", errStoredVersionInvalid)
		}
		if chapterKey.Valid {
			value := int(chapterOccurrence.Int64)
			if chapterOccurrence.Int64 <= 0 || int64(value) != chapterOccurrence.Int64 {
				return adminVersionInspection{}, fmt.Errorf("%w: chapter occurrence", errStoredVersionInvalid)
			}
			key := chapterKey.String
			identity.ChapterKey = &key
			identity.ChapterOccurrence = &value
		}
		wordCount += segmentWordCount.Int64
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return adminVersionInspection{}, err
	}
	if len(identities) == 0 {
		return adminVersionInspection{}, fmt.Errorf("%w: no readable segments", errStoredVersionInvalid)
	}
	chapterCount, err := readercontract.ValidateStoredSegmentIdentities(identities)
	if err != nil {
		return adminVersionInspection{}, fmt.Errorf("%w: segment identities", errStoredVersionInvalid)
	}

	return adminVersionInspection{
		Version:      versionValue,
		CreatedAt:    createdAt,
		Frontmatter:  frontmatter,
		SegmentCount: len(identities),
		WordCount:    int(wordCount),
		ChapterCount: chapterCount,
	}, nil
}

func adminStoryDetail(story inspectedAdminStory) model.AdminStoryDetailResponse {
	summary := story.Summary
	return model.AdminStoryDetailResponse{
		Slug:             summary.Slug,
		Title:            summary.Title,
		Author:           summary.Author,
		Language:         summary.Language,
		Rights:           summary.Rights,
		SourceURL:        summary.SourceURL,
		Status:           summary.Status,
		PublishedVersion: summary.PublishedVersion,
		DraftVersion:     summary.DraftVersion,
		VersionCount:     summary.VersionCount,
		CreatedAt:        story.Row.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        summary.UpdatedAt,
		Versions:         story.Versions,
	}
}

func adminStoryStatusResponse(story inspectedAdminStory) model.AdminStoryStatusResponse {
	return model.AdminStoryStatusResponse{
		Slug:             story.Summary.Slug,
		Status:           story.Summary.Status,
		PublishedVersion: story.Summary.PublishedVersion,
		DraftVersion:     story.Summary.DraftVersion,
		VersionCount:     story.Summary.VersionCount,
		UpdatedAt:        story.Summary.UpdatedAt,
	}
}

func adminStoryStatus(story adminStoryRow, repairRequired bool) model.AdminStoryStatus {
	if repairRequired {
		return model.AdminStoryStatusRepairRequired
	}
	if story.IsPublished {
		if story.DraftVersionID != nil && *story.DraftVersionID != *story.PublishedVersionID {
			return model.AdminStoryStatusPublishedWithDraft
		}
		return model.AdminStoryStatusPublished
	}
	if story.DraftVersionID != nil {
		return model.AdminStoryStatusDraftOnly
	}
	return model.AdminStoryStatusUnpublished
}

func selectAdminMetadata(
	story adminStoryRow,
	versions []inspectedAdminVersion,
	byID map[string]inspectedAdminVersion,
) *normalizedStoredFrontmatter {
	for _, pointer := range []*string{story.DraftVersionID, story.PublishedVersionID} {
		if pointer == nil {
			continue
		}
		version, ok := byID[*pointer]
		if ok && version.Summary.Health == model.AdminVersionHealthReady {
			metadata := version.Inspection.Frontmatter
			return &metadata
		}
	}
	for _, version := range versions {
		if version.Summary.Health == model.AdminVersionHealthReady {
			metadata := version.Inspection.Frontmatter
			return &metadata
		}
	}
	return nil
}

func adminPointer(
	id *string,
	versions map[string]inspectedAdminVersion,
) (*model.AdminVersionPointerSummary, bool) {
	if id == nil {
		return nil, true
	}
	version, ok := versions[*id]
	if !ok {
		return nil, false
	}
	return &model.AdminVersionPointerSummary{
		VersionID: version.Summary.VersionID,
		Version:   version.Summary.Version,
	}, version.Summary.Health == model.AdminVersionHealthReady
}

func positiveVersion(value int64) int {
	converted := int(value)
	if value <= 0 || int64(converted) != value {
		return 1
	}
	return converted
}

func equalOptionalID(value *string, target string) bool {
	return value != nil && *value == target
}

func nullStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneJSONMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, child := range value {
		result[key] = child
	}
	return result
}
