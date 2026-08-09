package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readerresolution"
)

func currentReleaseReaderEditions(
	ctx context.Context,
	tx *sql.Tx,
	access readerProfileStoryAccess,
) ([]readerresolution.ReleaseEdition, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT edition.edition_key, member.story_version_id
		FROM story_release_editions AS member
		JOIN story_editions AS edition
		  ON edition.id = member.edition_id
		 AND edition.story_id = member.story_id
		JOIN story_versions AS version
		  ON version.id = member.story_version_id
		 AND version.story_id = member.story_id
		 AND version.edition_id = member.edition_id
		WHERE member.release_id = $1
		  AND member.story_id = $2
	`, access.CurrentReleaseID, access.StoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	releaseEditions := make([]readerresolution.ReleaseEdition, 0, 5)
	for rows.Next() {
		var (
			editionKeyValue string
			versionID       string
		)
		if err := rows.Scan(&editionKeyValue, &versionID); err != nil {
			return nil, err
		}
		editionKey := model.ReaderEditionKey(editionKeyValue)
		if !model.ValidReaderEditionKey(editionKey) {
			return nil, fmt.Errorf("stored current-release edition key is invalid")
		}
		releaseEditions = append(releaseEditions, readerresolution.ReleaseEdition{
			EditionKey: editionKey,
			VersionID:  versionID,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return releaseEditions, nil
}

func readerResolutionOverride(
	ctx context.Context,
	tx *sql.Tx,
	accountID string,
	profileID string,
	storyID string,
) (*model.ReaderEditionKey, error) {
	var editionKeyValue string
	err := tx.QueryRowContext(ctx, `
		SELECT edition_key
		FROM reader_story_edition_overrides
		WHERE account_id = $1
		  AND profile_id = $2
		  AND story_id = $3
	`, accountID, profileID, storyID).Scan(&editionKeyValue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	editionKey := model.ReaderEditionKey(editionKeyValue)
	if !model.ValidReaderEditionKey(editionKey) {
		return nil, fmt.Errorf("stored reader edition override is invalid")
	}
	return &editionKey, nil
}

func readerResolutionProgressVersionID(
	ctx context.Context,
	tx *sql.Tx,
	accountID string,
	profileID string,
	storyID string,
) (*string, error) {
	var versionID string
	err := tx.QueryRowContext(ctx, `
		SELECT story_version_id
		FROM reading_progress
		WHERE account_id = $1
		  AND profile_id = $2
		  AND story_id = $3
	`, accountID, profileID, storyID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &versionID, nil
}

func readerEligibleEditionKeys(
	eligible []readerresolution.ReleaseEdition,
) []model.ReaderEditionKey {
	keys := make([]model.ReaderEditionKey, 0, len(eligible))
	for _, edition := range eligible {
		keys = append(keys, edition.EditionKey)
	}
	return keys
}

func readResolvedReaderStory(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	slug string,
	editionKey model.ReaderEditionKey,
	versionID string,
) (model.ReaderResolvedStory, error) {
	snapshot, err := validateStoredReaderVersion(ctx, tx, storyID, versionID, slug)
	if err != nil {
		if errors.Is(err, errStoredVersionInvalid) || errors.Is(err, sql.ErrNoRows) {
			return model.ReaderResolvedStory{}, sql.ErrNoRows
		}
		return model.ReaderResolvedStory{}, err
	}

	story := model.ReaderStory{
		Slug:     slug,
		Title:    snapshot.Frontmatter.Title,
		Author:   snapshot.Frontmatter.Author,
		Language: snapshot.Frontmatter.Language,
		Version:  snapshot.Version,
		Segments: make([]model.ReaderSegment, 0, snapshot.SegmentCount),
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			segment.ordinal,
			segment.segment_kind,
			segment.heading_level,
			segment.content_key,
			segment.content_occurrence,
			segment.chapter_key,
			segment.chapter_occurrence,
			segment.rendered_html,
			segment.word_count
		FROM story_segments AS segment
		WHERE segment.story_version_id = $1
		ORDER BY segment.ordinal
	`, versionID)
	if err != nil {
		return model.ReaderResolvedStory{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ordinal           int
			kind              string
			headingLevel      sql.NullInt64
			contentKey        string
			contentOccurrence int
			chapterKey        sql.NullString
			chapterOccurrence sql.NullInt64
			renderedHTML      string
			wordCount         int
		)
		if err := rows.Scan(
			&ordinal,
			&kind,
			&headingLevel,
			&contentKey,
			&contentOccurrence,
			&chapterKey,
			&chapterOccurrence,
			&renderedHTML,
			&wordCount,
		); err != nil {
			return model.ReaderResolvedStory{}, err
		}

		segment := model.ReaderSegment{
			Ordinal:           ordinal,
			Kind:              kind,
			ContentKey:        contentKey,
			ContentOccurrence: contentOccurrence,
			RenderedHTML:      renderedHTML,
			WordCount:         wordCount,
		}
		if headingLevel.Valid {
			value := int(headingLevel.Int64)
			segment.HeadingLevel = &value
		}
		if chapterKey.Valid {
			value := chapterKey.String
			segment.ChapterKey = &value
		}
		if chapterOccurrence.Valid {
			value := int(chapterOccurrence.Int64)
			segment.ChapterOccurrence = &value
		}
		story.Segments = append(story.Segments, segment)
	}
	if err := rows.Err(); err != nil {
		return model.ReaderResolvedStory{}, err
	}
	if len(story.Segments) != snapshot.SegmentCount {
		return model.ReaderResolvedStory{}, fmt.Errorf("resolved Reader segment count changed after validation")
	}

	return model.ReaderResolvedStory{
		ReaderStory: story,
		EditionKey:  editionKey,
	}, nil
}

// ReaderResolve is the authoritative Reader release-resolution read path.
//
// Resolution is performed while the mutable profile Reading Level and story
// current_release_id rows are share-locked. Release manifests and story
// versions are immutable, so a decision cannot silently cross releases.
//
// A stored override or progress row may remain after a later release or Reading
// Level change. readerresolution.Resolve treats those signals as usable only
// when they still point to an eligible member of the current release.
func (s *Store) ReaderResolve(
	accountID string,
	profileID string,
	slug string,
) (model.ReaderResolution, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ReaderResolution{}, err
	}
	defer func() { _ = tx.Rollback() }()

	access, err := lockReaderProfileStoryAccess(ctx, tx, accountID, profileID, slug)
	if err != nil {
		return model.ReaderResolution{}, err
	}
	releaseEditions, err := currentReleaseReaderEditions(ctx, tx, access)
	if err != nil {
		return model.ReaderResolution{}, err
	}
	overrideEdition, err := readerResolutionOverride(
		ctx,
		tx,
		accountID,
		profileID,
		access.StoryID,
	)
	if err != nil {
		return model.ReaderResolution{}, err
	}
	progressVersionID, err := readerResolutionProgressVersionID(
		ctx,
		tx,
		accountID,
		profileID,
		access.StoryID,
	)
	if err != nil {
		return model.ReaderResolution{}, err
	}

	decision, err := readerresolution.Resolve(readerresolution.Input{
		ReadingLevel:      access.ReadingLevel,
		ReleaseEditions:   releaseEditions,
		OverrideEdition:   overrideEdition,
		ProgressVersionID: progressVersionID,
	})
	if err != nil {
		return model.ReaderResolution{}, err
	}
	eligible := readerEligibleEditionKeys(decision.Eligible)

	switch decision.Kind {
	case readerresolution.DecisionUnavailable:
		return model.ReaderResolution{}, sql.ErrNoRows

	case readerresolution.DecisionChooser:
		if err := tx.Commit(); err != nil {
			return model.ReaderResolution{}, err
		}
		return model.ReaderResolution{
			State:            model.ReaderResolutionChooser,
			EligibleEditions: eligible,
			Story:            nil,
		}, nil

	case readerresolution.DecisionSelected:
		if decision.Selected == nil {
			return model.ReaderResolution{}, fmt.Errorf("Reader resolver selected no edition")
		}
		story, err := readResolvedReaderStory(
			ctx,
			tx,
			access.StoryID,
			slug,
			decision.Selected.EditionKey,
			decision.Selected.VersionID,
		)
		if err != nil {
			return model.ReaderResolution{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.ReaderResolution{}, err
		}
		return model.ReaderResolution{
			State:            model.ReaderResolutionSelected,
			EligibleEditions: eligible,
			Story:            &story,
		}, nil

	default:
		return model.ReaderResolution{}, fmt.Errorf("unknown Reader resolution decision")
	}
}
