package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/storyingest"
)

var errStoredVersionInvalid = errors.New("stored story version is invalid")

type storedVersionQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type storedReaderVersionSnapshot struct {
	Version         int
	FrontmatterJSON []byte
	Markdown        string
	RenderedHTML    string
	ContentHash     string
	SegmentCount    int
}

type normalizedStoredFrontmatter struct {
	Values   map[string]any
	JSON     []byte
	Title    string
	Author   *string
	Language string
}

func normalizeStoredFrontmatter(raw []byte) (normalizedStoredFrontmatter, error) {
	title, author, language, err := libraryVersionMetadata(raw)
	if err != nil {
		return normalizedStoredFrontmatter{}, err
	}
	decoded, ok := decodeJSONDocument(raw)
	if !ok {
		return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter must be valid JSON")
	}
	values, ok := decoded.(map[string]any)
	if !ok {
		return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter must be an object")
	}

	rawTitle, ok := values["title"].(string)
	if !ok || rawTitle != title {
		return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter title is not canonical")
	}
	rawLanguage, ok := values["language"].(string)
	if !ok || rawLanguage != language {
		return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter language is not canonical")
	}
	authorValue := ""
	if author != nil {
		authorValue = *author
	}
	if rawAuthor, exists := values["author"]; exists && rawAuthor != nil {
		value, ok := rawAuthor.(string)
		if !ok {
			return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter author is not canonical")
		}
		trimmed := strings.TrimSpace(value)
		if (trimmed != "" && trimmed != authorValue) || (trimmed == "" && authorValue != "") {
			return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter author is not canonical")
		}
		if trimmed != "" && value != trimmed {
			return normalizedStoredFrontmatter{}, fmt.Errorf("frontmatter author is not canonical")
		}
	}

	normalizedValues := make(map[string]any, len(values)+1)
	for key, value := range values {
		normalizedValues[key] = value
	}
	normalizedValues["title"] = title
	normalizedValues["author"] = authorValue
	normalizedValues["language"] = language
	normalizedJSON, err := json.Marshal(normalizedValues)
	if err != nil {
		return normalizedStoredFrontmatter{}, err
	}
	return normalizedStoredFrontmatter{
		Values:   normalizedValues,
		JSON:     normalizedJSON,
		Title:    title,
		Author:   author,
		Language: language,
	}, nil
}

// validateStoredReaderVersion applies the immutable metadata and structural
// Reader 2 contract to one exact story-owned version. It deliberately does not
// mutate a corrupt version: published versions remain immutable and repair is
// an explicit operational action.
func validateStoredReaderVersion(
	ctx context.Context,
	queryer storedVersionQueryer,
	storyID string,
	versionID string,
	slug string,
) (storedReaderVersionSnapshot, error) {
	var (
		version         int64
		frontmatterJSON string
		markdown        string
		renderedHTML    string
		contentHash     string
	)
	if err := queryer.QueryRowContext(ctx, `
		SELECT version, frontmatter::text, markdown, rendered_html, content_hash
		FROM story_versions
		WHERE id = $1
		  AND story_id = $2
		FOR UPDATE
	`, versionID, storyID).Scan(&version, &frontmatterJSON, &markdown, &renderedHTML, &contentHash); err != nil {
		return storedReaderVersionSnapshot{}, err
	}
	versionValue := int(version)
	if version <= 0 || int64(versionValue) != version {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: version number", errStoredVersionInvalid)
	}
	frontmatter, err := normalizeStoredFrontmatter([]byte(frontmatterJSON))
	if err != nil {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: immutable metadata", errStoredVersionInvalid)
	}
	snapshot := storedReaderVersionSnapshot{
		Version:         versionValue,
		FrontmatterJSON: frontmatter.JSON,
		Markdown:        markdown,
		RenderedHTML:    renderedHTML,
		ContentHash:     contentHash,
	}

	// The version-row update lock blocks new FK-backed segment inserts while
	// the selected segment-row share locks block updates and deletes through
	// publication commit. Admin writes create all segments before commit and do
	// not mutate them afterward, but these locks make that invariant explicit at
	// the final publication boundary too.
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			segment.id,
			segment.ordinal,
			segment.segment_kind,
			segment.heading_level,
			segment.content_key,
			segment.content_occurrence,
			segment.chapter_key,
			segment.chapter_occurrence,
			segment.word_count,
			segment.rendered_html
		FROM story_segments AS segment
		WHERE segment.story_version_id = $1
		ORDER BY segment.ordinal ASC
		FOR SHARE OF segment
	`, versionID)
	if err != nil {
		return storedReaderVersionSnapshot{}, err
	}
	defer rows.Close()

	identities := make([]readercontract.StoredSegmentIdentity, 0, 32)
	wordCount := int64(0)
	for rows.Next() {
		var (
			segmentID         sql.NullString
			ordinal           sql.NullInt64
			kind              sql.NullString
			headingLevel      sql.NullInt64
			contentKey        sql.NullString
			contentOccurrence sql.NullInt64
			chapterKey        sql.NullString
			chapterOccurrence sql.NullInt64
			segmentWordCount  sql.NullInt64
			renderedHTML      sql.NullString
		)
		if err := rows.Scan(
			&segmentID,
			&ordinal,
			&kind,
			&headingLevel,
			&contentKey,
			&contentOccurrence,
			&chapterKey,
			&chapterOccurrence,
			&segmentWordCount,
			&renderedHTML,
		); err != nil {
			return storedReaderVersionSnapshot{}, err
		}
		if strings.TrimSpace(segmentID.String) == "" || !ordinal.Valid || !kind.Valid ||
			!contentKey.Valid || !contentOccurrence.Valid || !segmentWordCount.Valid || !renderedHTML.Valid {
			return storedReaderVersionSnapshot{}, fmt.Errorf("%w: incomplete segment identity", errStoredVersionInvalid)
		}
		if !utf8.ValidString(renderedHTML.String) || strings.TrimSpace(renderedHTML.String) == "" {
			return storedReaderVersionSnapshot{}, fmt.Errorf("%w: unreadable segment content", errStoredVersionInvalid)
		}

		ordinalValue := int(ordinal.Int64)
		contentOccurrenceValue := int(contentOccurrence.Int64)
		if ordinal.Int64 <= 0 || int64(ordinalValue) != ordinal.Int64 ||
			contentOccurrence.Int64 <= 0 || int64(contentOccurrenceValue) != contentOccurrence.Int64 ||
			segmentWordCount.Int64 < 0 || segmentWordCount.Int64 > maxSafeJSONInteger-wordCount {
			return storedReaderVersionSnapshot{}, fmt.Errorf("%w: segment numeric value", errStoredVersionInvalid)
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
				return storedReaderVersionSnapshot{}, fmt.Errorf("%w: heading level", errStoredVersionInvalid)
			}
			identity.HeadingLevel = &value
		}
		if chapterKey.Valid != chapterOccurrence.Valid {
			return storedReaderVersionSnapshot{}, fmt.Errorf("%w: incomplete chapter identity", errStoredVersionInvalid)
		}
		if chapterKey.Valid {
			value := int(chapterOccurrence.Int64)
			if chapterOccurrence.Int64 <= 0 || int64(value) != chapterOccurrence.Int64 {
				return storedReaderVersionSnapshot{}, fmt.Errorf("%w: chapter occurrence", errStoredVersionInvalid)
			}
			key := chapterKey.String
			identity.ChapterKey = &key
			identity.ChapterOccurrence = &value
		}
		wordCount += segmentWordCount.Int64
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return storedReaderVersionSnapshot{}, err
	}
	if len(identities) == 0 {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: no readable segments", errStoredVersionInvalid)
	}
	if _, err := readercontract.ValidateStoredSegmentIdentities(identities); err != nil {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: segment identities", errStoredVersionInvalid)
	}

	authorValue := ""
	if frontmatter.Author != nil {
		authorValue = *frontmatter.Author
	}
	canonical, err := storyingest.CanonicalizeStoredBody(storyingest.Input{
		Slug:     slug,
		Title:    frontmatter.Title,
		Author:   authorValue,
		Markdown: markdown,
		Language: frontmatter.Language,
	}, frontmatter.Values)
	if err != nil {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: canonical story body", errStoredVersionInvalid)
	}
	segmentsMatch, err := storedReaderSegmentsMatch(ctx, queryer, versionID, canonical.Segments)
	if err != nil {
		return storedReaderVersionSnapshot{}, err
	}
	canonicalFrontmatterJSON, err := json.Marshal(canonical.Frontmatter)
	if err != nil {
		return storedReaderVersionSnapshot{}, err
	}
	if !jsonDocumentsEqual(canonicalFrontmatterJSON, frontmatter.JSON) ||
		canonical.Markdown != markdown || canonical.RenderedHTML != renderedHTML ||
		canonical.ContentHash != contentHash || !segmentsMatch {
		return storedReaderVersionSnapshot{}, fmt.Errorf("%w: noncanonical persisted content", errStoredVersionInvalid)
	}
	snapshot.SegmentCount = len(identities)
	return snapshot, nil
}

// storedReaderSegmentsMatch compares the immutable persisted sequence with the
// freshly ingested sequence before idempotent reuse. Structural validity alone
// is insufficient: a same-count tamper must never be reported as though the
// incoming content had just been stored.
func storedReaderSegmentsMatch(
	ctx context.Context,
	queryer storedVersionQueryer,
	versionID string,
	expected []storyingest.Segment,
) (bool, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			ordinal,
			segment_kind,
			heading_level,
			content_key,
			content_occurrence,
			chapter_key,
			chapter_occurrence,
			markdown,
			rendered_html,
			word_count
		FROM story_segments
		WHERE story_version_id = $1
		ORDER BY ordinal ASC
		FOR SHARE
	`, versionID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	index := 0
	for rows.Next() {
		var (
			ordinal           sql.NullInt64
			kind              sql.NullString
			headingLevel      sql.NullInt64
			contentKey        sql.NullString
			contentOccurrence sql.NullInt64
			chapterKey        sql.NullString
			chapterOccurrence sql.NullInt64
			markdown          sql.NullString
			renderedHTML      sql.NullString
			wordCount         sql.NullInt64
		)
		if err := rows.Scan(
			&ordinal,
			&kind,
			&headingLevel,
			&contentKey,
			&contentOccurrence,
			&chapterKey,
			&chapterOccurrence,
			&markdown,
			&renderedHTML,
			&wordCount,
		); err != nil {
			return false, err
		}
		if index >= len(expected) {
			return false, nil
		}
		segment := expected[index]
		if !ordinal.Valid || ordinal.Int64 != int64(segment.Ordinal) ||
			!kind.Valid || kind.String != string(segment.Kind) ||
			!nullableIntMatches(headingLevel, segment.HeadingLevel) ||
			!contentKey.Valid || contentKey.String != segment.ContentKey ||
			!contentOccurrence.Valid || contentOccurrence.Int64 != int64(segment.ContentOccurrence) ||
			!nullableStringMatches(chapterKey, segment.ChapterKey) ||
			!nullableIntMatches(chapterOccurrence, segment.ChapterOccurrence) ||
			!markdown.Valid || markdown.String != segment.Markdown ||
			!renderedHTML.Valid || renderedHTML.String != segment.RenderedHTML ||
			!wordCount.Valid || wordCount.Int64 != int64(segment.WordCount) {
			return false, nil
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return index == len(expected), nil
}

func nullableIntMatches(stored sql.NullInt64, expected *int) bool {
	if expected == nil {
		return !stored.Valid
	}
	return stored.Valid && stored.Int64 == int64(*expected)
}

func nullableStringMatches(stored sql.NullString, expected *string) bool {
	if expected == nil {
		return !stored.Valid
	}
	return stored.Valid && stored.String == *expected
}

func decodeJSONDocument(raw []byte) (any, bool) {
	if !json.Valid(raw) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

func jsonDocumentsEqual(left, right []byte) bool {
	leftValue, leftOK := decodeJSONDocument(left)
	rightValue, rightOK := decodeJSONDocument(right)
	if !leftOK || !rightOK {
		return false
	}
	leftValue, leftOK = normalizeJSONNumbers(leftValue)
	rightValue, rightOK = normalizeJSONNumbers(rightValue)
	return leftOK && rightOK && reflect.DeepEqual(leftValue, rightValue)
}

type canonicalJSONNumber string

func normalizeJSONNumbers(value any) (any, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, ok := new(big.Rat).SetString(typed.String())
		if !ok {
			return nil, false
		}
		return canonicalJSONNumber(number.RatString()), true
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			value, ok := normalizeJSONNumbers(child)
			if !ok {
				return nil, false
			}
			normalized[key] = value
		}
		return normalized, true
	case []any:
		normalized := make([]any, len(typed))
		for index, child := range typed {
			value, ok := normalizeJSONNumbers(child)
			if !ok {
				return nil, false
			}
			normalized[index] = value
		}
		return normalized, true
	case nil, string, bool:
		return typed, true
	default:
		return nil, false
	}
}

func (s *Store) AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	out, err := storyingest.Ingest(storyingest.Input{
		Slug:     "preview",
		Title:    "Preview",
		Author:   "",
		Markdown: req.Markdown,
		Language: "en-GB",
	})
	if err != nil {
		return model.AdminPreviewResponse{}, err
	}

	segs := make([]model.AdminSegment, 0, len(out.Segments))
	for _, seg := range out.Segments {
		segs = append(segs, model.AdminSegment{
			Ordinal:           seg.Ordinal,
			Kind:              string(seg.Kind),
			HeadingLevel:      seg.HeadingLevel,
			ContentKey:        seg.ContentKey,
			ContentOccurrence: seg.ContentOccurrence,
			ChapterKey:        seg.ChapterKey,
			ChapterOccurrence: seg.ChapterOccurrence,
			RenderedHTML:      seg.RenderedHTML,
			WordCount:         seg.WordCount,
		})
	}

	return model.AdminPreviewResponse{
		RenderedHTML: out.RenderedHTML,
		Segments:     segs,
	}, nil
}

// AdminDraftUpsert is account-scoped. Body hashes retain their historical role
// as idempotency candidate keys, but reuse succeeds only when the complete
// locked immutable version still matches the canonical incoming story.
func (s *Store) AdminDraftUpsert(accountID string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return model.AdminDraftUpsertResponse{}, fmt.Errorf("account required")
	}

	slug := strings.TrimSpace(req.Slug)
	title := strings.TrimSpace(req.Title)
	md := req.Markdown

	author := ""
	if req.Author != nil {
		author = strings.TrimSpace(*req.Author)
	}

	lang := "en-GB"
	if req.Language != nil && strings.TrimSpace(*req.Language) != "" {
		lang = strings.TrimSpace(*req.Language)
	}

	srcURL := ""
	if req.SourceURL != nil {
		srcURL = strings.TrimSpace(*req.SourceURL)
	}

	ing, err := storyingest.Ingest(storyingest.Input{
		Slug:      slug,
		Title:     title,
		Author:    author,
		Markdown:  md,
		Language:  lang,
		SourceURL: srcURL,
		Rights:    req.Rights,
	})
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}
	frontmatterJSON, err := json.Marshal(ing.Frontmatter)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// story upsert (account-scoped)
	sourceJSON, _ := json.Marshal(ing.Source)
	rightsJSON, _ := json.Marshal(ing.Rights)

	var storyID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO stories (account_id, slug, title, author, language, source, rights, updated_at)
		VALUES ($1,$2,$3,NULLIF(BTRIM($4),''),$5,$6::jsonb,$7::jsonb, now())
		ON CONFLICT (account_id, slug) DO UPDATE SET
			title=EXCLUDED.title,
			author=EXCLUDED.author,
			language=EXCLUDED.language,
			source=EXCLUDED.source,
			rights=EXCLUDED.rights,
			updated_at=now()
		RETURNING id
	`, accountID, ing.Slug, ing.Title, ing.Author, ing.Language, string(sourceJSON), string(rightsJSON)).Scan(&storyID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// Body hashes identify possible idempotency targets for compatibility with
	// existing versions. Reuse still requires the complete locked immutable
	// version to equal this request. Metadata-only changes therefore return the
	// explicit repair-required conflict instead of silently reusing old metadata
	// or changing the established body-hash identity policy.
	candidateRows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM story_versions
		WHERE story_id = $1
		  AND (content_hash = $2 OR markdown = $3)
		ORDER BY version ASC, id ASC
		FOR UPDATE
	`, storyID, ing.ContentHash, ing.Markdown)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}
	existingVersionIDs := make([]string, 0, 2)
	for candidateRows.Next() {
		var candidateID string
		if err := candidateRows.Scan(&candidateID); err != nil {
			_ = candidateRows.Close()
			return model.AdminDraftUpsertResponse{}, err
		}
		if strings.TrimSpace(candidateID) == "" {
			_ = candidateRows.Close()
			return model.AdminDraftUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminVersionRepairRequired)
		}
		existingVersionIDs = append(existingVersionIDs, candidateID)
	}
	if err := candidateRows.Err(); err != nil {
		_ = candidateRows.Close()
		return model.AdminDraftUpsertResponse{}, err
	}
	if err := candidateRows.Close(); err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}
	if len(existingVersionIDs) > 1 {
		return model.AdminDraftUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminVersionRepairRequired)
	}

	if len(existingVersionIDs) == 1 {
		existingVersionID := existingVersionIDs[0]
		storedVersion, validationErr := validateStoredReaderVersion(ctx, tx, storyID, existingVersionID, ing.Slug)
		if errors.Is(validationErr, errStoredVersionInvalid) || errors.Is(validationErr, sql.ErrNoRows) {
			return model.AdminDraftUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminVersionRepairRequired)
		}
		if validationErr != nil {
			return model.AdminDraftUpsertResponse{}, validationErr
		}
		segmentsMatch, matchErr := storedReaderSegmentsMatch(ctx, tx, existingVersionID, ing.Segments)
		if matchErr != nil {
			return model.AdminDraftUpsertResponse{}, matchErr
		}
		if !segmentsMatch || storedVersion.SegmentCount != len(ing.Segments) ||
			storedVersion.Markdown != ing.Markdown || storedVersion.RenderedHTML != ing.RenderedHTML ||
			storedVersion.ContentHash != ing.ContentHash ||
			!jsonDocumentsEqual(storedVersion.FrontmatterJSON, frontmatterJSON) {
			return model.AdminDraftUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminVersionRepairRequired)
		}

		// point draft at the existing version
		_, err = tx.ExecContext(ctx, `
			UPDATE stories
			SET draft_version_id=$2,
			    updated_at=now()
			WHERE id=$1
		`, storyID, existingVersionID)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}

		// contributors link (still useful even if content existed)
		if strings.TrimSpace(ing.Author) != "" {
			var contribID string
			// No-op update returns id reliably (requires UNIQUE(contributors.name))
			_ = tx.QueryRowContext(ctx, `
				INSERT INTO contributors (name)
				VALUES ($1)
				ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
				RETURNING id
			`, ing.Author).Scan(&contribID)

			if strings.TrimSpace(contribID) != "" {
				_, _ = tx.ExecContext(ctx, `
					INSERT INTO story_contributors (story_id, contributor_id, role)
					VALUES ($1,$2,'author')
					ON CONFLICT DO NOTHING
				`, storyID, contribID)
			}
		}

		if err := tx.Commit(); err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}

		return model.AdminDraftUpsertResponse{
			StoryID:        storyID,
			StoryVersionID: existingVersionID,
			Slug:           ing.Slug,
			Version:        storedVersion.Version,
			SegmentsCount:  storedVersion.SegmentCount,
			RenderedHTML:   storedVersion.RenderedHTML,
		}, nil
	}

	// next version number (only for new content)
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM story_versions
		WHERE story_id = $1
	`, storyID).Scan(&nextVersion); err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	var versionID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO story_versions (story_id, version, frontmatter, markdown, rendered_html, content_hash)
		VALUES ($1,$2,$3::jsonb,$4,$5,$6)
		RETURNING id
	`, storyID, nextVersion, string(frontmatterJSON), ing.Markdown, ing.RenderedHTML, ing.ContentHash).Scan(&versionID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// --- Sections (chapters) + segment section assignment ---
	headingText := func(md string) string {
		s := strings.TrimSpace(md)
		s = strings.TrimLeft(s, "#")
		return strings.TrimSpace(s)
	}

	type chapter struct {
		StartSegOrdinal int
		Title           string
		SectionOrdinal  int
		ID              string
	}
	chapters := make([]chapter, 0, 16)

	for _, seg := range ing.Segments {
		if seg.Kind == "heading" && seg.HeadingLevel != nil && *seg.HeadingLevel == 2 {
			t := headingText(seg.Markdown)
			if strings.TrimSpace(t) == "" {
				t = fmt.Sprintf("Chapter %d", len(chapters)+1)
			}
			chapters = append(chapters, chapter{
				StartSegOrdinal: seg.Ordinal,
				Title:           t,
				SectionOrdinal:  len(chapters) + 1,
			})
		}
	}

	sectionIDByStart := map[int]string{}

	if len(chapters) == 0 {
		// No chapters -> one generic section for whole story
		var sectionID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO story_sections (story_version_id, kind, title, ordinal)
			VALUES ($1, 'section', NULL, 1)
			RETURNING id
		`, versionID).Scan(&sectionID)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}
		sectionIDByStart[1] = sectionID
	} else {
		for i := range chapters {
			var secID string
			err = tx.QueryRowContext(ctx, `
				INSERT INTO story_sections (story_version_id, kind, title, ordinal)
				VALUES ($1, 'chapter', $2, $3)
				RETURNING id
			`, versionID, chapters[i].Title, chapters[i].SectionOrdinal).Scan(&secID)
			if err != nil {
				return model.AdminDraftUpsertResponse{}, err
			}
			chapters[i].ID = secID
			sectionIDByStart[chapters[i].StartSegOrdinal] = secID
		}
	}

	var currentChapterID string

	for _, seg := range ing.Segments {
		var sectionArg any = nil

		if len(chapters) == 0 {
			sectionArg = sectionIDByStart[1]
		} else {
			// H1 title stays unsectioned; H2 starts a chapter; everything after belongs to current chapter
			if seg.Kind == "heading" && seg.HeadingLevel != nil && *seg.HeadingLevel == 1 {
				sectionArg = nil
			} else if seg.Kind == "heading" && seg.HeadingLevel != nil && *seg.HeadingLevel == 2 {
				if id, ok := sectionIDByStart[seg.Ordinal]; ok {
					currentChapterID = id
					sectionArg = currentChapterID
				}
			} else if currentChapterID != "" {
				sectionArg = currentChapterID
			} else {
				sectionArg = nil
			}
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO story_segments (
				story_version_id, section_id, ordinal,
				segment_kind, heading_level, content_key, content_occurrence,
				chapter_key, chapter_occurrence,
				markdown, rendered_html, word_count
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`,
			versionID,
			sectionArg,
			seg.Ordinal,
			string(seg.Kind),
			seg.HeadingLevel,
			seg.ContentKey,
			seg.ContentOccurrence,
			seg.ChapterKey,
			seg.ChapterOccurrence,
			seg.Markdown,
			seg.RenderedHTML,
			seg.WordCount,
		)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}
	}

	// update draft pointer ONLY (publish is separate endpoint)
	_, err = tx.ExecContext(ctx, `
		UPDATE stories
		SET draft_version_id=$2,
		    updated_at=now()
		WHERE id=$1
	`, storyID, versionID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// contributors: ensure author exists & link if provided
	if strings.TrimSpace(ing.Author) != "" {
		var contribID string
		// No-op update returns id reliably (requires UNIQUE(contributors.name))
		_ = tx.QueryRowContext(ctx, `
			INSERT INTO contributors (name)
			VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, ing.Author).Scan(&contribID)

		if strings.TrimSpace(contribID) != "" {
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO story_contributors (story_id, contributor_id, role)
				VALUES ($1,$2,'author')
				ON CONFLICT DO NOTHING
			`, storyID, contribID)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	return model.AdminDraftUpsertResponse{
		StoryID:        storyID,
		StoryVersionID: versionID,
		Slug:           ing.Slug,
		Version:        nextVersion,
		SegmentsCount:  len(ing.Segments),
		RenderedHTML:   ing.RenderedHTML,
	}, nil
}

func (s *Store) AdminPublish(accountID string, slug string, versionID string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)

	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil || !accountIDRe.MatchString(versionID) {
		return fmt.Errorf("%w", model.ErrAdminPublishInvalid)
	}

	// READ COMMITTED lets the segment-locking query observe a mutation that
	// completed while it waited for the version lock. The locks then keep the
	// validated version stable until the pointer update commits.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the account-owned story first. The old pointer remains unchanged
	// unless every immutable version invariant validates and the transaction
	// commits.
	var storyID string
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM stories
		WHERE account_id = $1
		  AND slug = $2
		FOR UPDATE
	`, accountID, slug).Scan(&storyID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w", model.ErrAdminPublishNotFound)
	}
	if err != nil {
		return err
	}

	if _, err := validateStoredReaderVersion(ctx, tx, storyID, versionID, slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w", model.ErrAdminPublishNotFound)
		}
		if errors.Is(err, errStoredVersionInvalid) {
			return fmt.Errorf("%w", model.ErrAdminPublishInvalid)
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE stories
		SET published_version_id = $2,
		    is_published = true,
		    updated_at = now()
		WHERE id = $1
	`, storyID, versionID); err != nil {
		return err
	}
	return tx.Commit()
}
