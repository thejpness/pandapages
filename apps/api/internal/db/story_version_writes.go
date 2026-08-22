package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/storyingest"
)

// insertCanonicalStoryVersionTx writes one immutable story-version snapshot
// and its deterministic Reader structure. It intentionally does not advance
// an edition pointer or mutate publication state; callers own those lifecycle
// decisions inside their transaction.
func insertCanonicalStoryVersionTx(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	editionID string,
	version int,
	frontmatterJSON []byte,
	ing storyingest.Output,
) (string, error) {
	var versionID string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO story_versions (
			story_id, edition_id, version, frontmatter, markdown, rendered_html, content_hash
		)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)
		RETURNING id
	`, storyID, editionID, version, string(frontmatterJSON), ing.Markdown, ing.RenderedHTML, ing.ContentHash).Scan(&versionID)
	if err != nil {
		return "", err
	}

	headingText := func(markdown string) string {
		value := strings.TrimSpace(markdown)
		value = strings.TrimLeft(value, "#")
		return strings.TrimSpace(value)
	}
	type chapter struct {
		startSegmentOrdinal int
		title               string
		sectionOrdinal      int
		id                  string
	}
	chapters := make([]chapter, 0, 16)
	for _, segment := range ing.Segments {
		if segment.Kind == readercontract.SegmentKindHeading && segment.HeadingLevel != nil && *segment.HeadingLevel == 2 {
			title := headingText(segment.Markdown)
			if title == "" {
				title = fmt.Sprintf("Chapter %d", len(chapters)+1)
			}
			chapters = append(chapters, chapter{
				startSegmentOrdinal: segment.Ordinal,
				title:               title,
				sectionOrdinal:      len(chapters) + 1,
			})
		}
	}

	sectionIDByStart := make(map[int]string, len(chapters)+1)
	if len(chapters) == 0 {
		var sectionID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO story_sections (story_version_id, kind, title, ordinal)
			VALUES ($1, 'section', NULL, 1)
			RETURNING id
		`, versionID).Scan(&sectionID); err != nil {
			return "", err
		}
		sectionIDByStart[1] = sectionID
	} else {
		for index := range chapters {
			var sectionID string
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO story_sections (story_version_id, kind, title, ordinal)
				VALUES ($1, 'chapter', $2, $3)
				RETURNING id
			`, versionID, chapters[index].title, chapters[index].sectionOrdinal).Scan(&sectionID); err != nil {
				return "", err
			}
			chapters[index].id = sectionID
			sectionIDByStart[chapters[index].startSegmentOrdinal] = sectionID
		}
	}

	currentChapterID := ""
	for _, segment := range ing.Segments {
		var sectionID any
		if len(chapters) == 0 {
			sectionID = sectionIDByStart[1]
		} else if segment.Kind == readercontract.SegmentKindHeading && segment.HeadingLevel != nil && *segment.HeadingLevel == 1 {
			sectionID = nil
		} else if segment.Kind == readercontract.SegmentKindHeading && segment.HeadingLevel != nil && *segment.HeadingLevel == 2 {
			if value, ok := sectionIDByStart[segment.Ordinal]; ok {
				currentChapterID = value
				sectionID = currentChapterID
			}
		} else if currentChapterID != "" {
			sectionID = currentChapterID
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO story_segments (
				story_version_id, section_id, ordinal,
				segment_kind, heading_level, content_key, content_occurrence,
				chapter_key, chapter_occurrence,
				markdown, rendered_html, word_count
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`,
			versionID,
			sectionID,
			segment.Ordinal,
			string(segment.Kind),
			segment.HeadingLevel,
			segment.ContentKey,
			segment.ContentOccurrence,
			segment.ChapterKey,
			segment.ChapterOccurrence,
			segment.Markdown,
			segment.RenderedHTML,
			segment.WordCount,
		); err != nil {
			return "", err
		}
	}
	return versionID, nil
}
