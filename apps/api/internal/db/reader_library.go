package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readerresolution"
)

type readerLibraryCandidate struct {
	StoryID          string
	Slug             string
	CurrentReleaseID string
}

type readerLibraryProgressState struct {
	VersionID string
	Version   int
	Percent   float64
	UpdatedAt time.Time
}

func readerLibraryProgress(
	ctx context.Context,
	tx *sql.Tx,
	accountID string,
	profileID string,
	storyID string,
) (*readerLibraryProgressState, error) {
	var state readerLibraryProgressState
	err := tx.QueryRowContext(ctx, `
		SELECT
			progress.story_version_id,
			version.version,
			progress.percent,
			progress.updated_at
		FROM reading_progress AS progress
		JOIN story_versions AS version
		  ON version.id = progress.story_version_id
		 AND version.story_id = progress.story_id
		WHERE progress.account_id = $1
		  AND progress.profile_id = $2
		  AND progress.story_id = $3
	`, accountID, profileID, storyID).Scan(
		&state.VersionID,
		&state.Version,
		&state.Percent,
		&state.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(state.VersionID) == "" ||
		state.Version <= 0 ||
		math.IsNaN(state.Percent) ||
		math.IsInf(state.Percent, 0) ||
		state.Percent < 0 ||
		state.Percent > 1 ||
		state.UpdatedAt.IsZero() {
		return nil, fmt.Errorf("stored Reader Library progress is invalid")
	}
	return &state, nil
}

func sameReaderLibraryAuthor(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func incrementReaderLibraryUnavailable(result *model.ReaderLibraryReadModel) error {
	if result.UnavailableItemCount == maxSafeJSONInteger {
		return fmt.Errorf("unavailable Reader Library item count exceeds the safe JSON integer range")
	}
	result.UnavailableItemCount++
	return nil
}

func readerLibraryEditionSummary(
	ctx context.Context,
	tx *sql.Tx,
	storyID string,
	slug string,
	edition readerresolution.ReleaseEdition,
) (model.ReaderLibraryEditionSummary, storedReaderVersionSnapshot, error) {
	snapshot, err := inspectStoredReaderVersion(ctx, tx, storyID, edition.VersionID, slug)
	if err != nil {
		return model.ReaderLibraryEditionSummary{}, storedReaderVersionSnapshot{}, err
	}
	if snapshot.Version <= 0 ||
		snapshot.WordCount < 0 ||
		int64(snapshot.WordCount) > maxSafeJSONInteger ||
		snapshot.ChapterCount < 0 ||
		int64(snapshot.ChapterCount) > maxSafeJSONInteger {
		return model.ReaderLibraryEditionSummary{}, storedReaderVersionSnapshot{},
			fmt.Errorf("%w: invalid Reader Library counts", errStoredVersionInvalid)
	}
	return model.ReaderLibraryEditionSummary{
		EditionKey:   edition.EditionKey,
		Version:      snapshot.Version,
		WordCount:    int64(snapshot.WordCount),
		ChapterCount: int64(snapshot.ChapterCount),
	}, snapshot, nil
}

// ReaderLibrary returns the authoritative profile-scoped Reader bookshelf.
//
// The profile Reading Level and every candidate story current-release pointer
// are share-locked separately. This keeps mutable access/release configuration
// stable without relying on a locking join. Release manifests and story versions
// are immutable, so every visible item is resolved from one coherent release view.
//
// Zero eligible editions make a story invisible. A malformed eligible immutable
// edition quarantines only that story into UnavailableItemCount. No Classic,
// highest, nearest, or other representative edition is invented for chooser
// items.
func (s *Store) ReaderLibrary(
	accountID string,
	profileID string,
) (model.ReaderLibraryReadModel, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ReaderLibraryReadModel{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var readingLevelValue string
	if err := tx.QueryRowContext(ctx, `
		SELECT reading_level
		FROM profiles
		WHERE account_id = $1
		  AND id = $2
		FOR SHARE
	`, accountID, profileID).Scan(&readingLevelValue); err != nil {
		return model.ReaderLibraryReadModel{}, err
	}
	readingLevel := model.ReaderEditionKey(readingLevelValue)
	if !model.ValidReaderEditionKey(readingLevel) {
		return model.ReaderLibraryReadModel{}, fmt.Errorf("stored reader reading level is invalid")
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT story.id, story.slug, story.current_release_id
		FROM stories AS story
		WHERE story.account_id = $1
		  AND story.current_release_id IS NOT NULL
		ORDER BY
			story.updated_at DESC,
			story.created_at DESC,
			story.slug ASC,
			story.id ASC
		FOR SHARE OF story
	`, accountID)
	if err != nil {
		return model.ReaderLibraryReadModel{}, err
	}

	candidates := make([]readerLibraryCandidate, 0, 32)
	for rows.Next() {
		var candidate readerLibraryCandidate
		if err := rows.Scan(
			&candidate.StoryID,
			&candidate.Slug,
			&candidate.CurrentReleaseID,
		); err != nil {
			_ = rows.Close()
			return model.ReaderLibraryReadModel{}, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return model.ReaderLibraryReadModel{}, err
	}
	if err := rows.Close(); err != nil {
		return model.ReaderLibraryReadModel{}, err
	}

	result := model.ReaderLibraryReadModel{
		Items: make([]model.ReaderLibraryItem, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.StoryID) == "" ||
			!validLibrarySlug(candidate.Slug) ||
			strings.TrimSpace(candidate.CurrentReleaseID) == "" {
			if err := incrementReaderLibraryUnavailable(&result); err != nil {
				return model.ReaderLibraryReadModel{}, err
			}
			continue
		}

		access := readerProfileStoryAccess{
			ReadingLevel:     readingLevel,
			StoryID:          candidate.StoryID,
			CurrentReleaseID: candidate.CurrentReleaseID,
		}
		releaseEditions, err := currentReleaseReaderEditions(ctx, tx, access)
		if err != nil {
			return model.ReaderLibraryReadModel{}, err
		}
		overrideEdition, err := readerResolutionOverride(
			ctx,
			tx,
			accountID,
			profileID,
			candidate.StoryID,
		)
		if err != nil {
			return model.ReaderLibraryReadModel{}, err
		}
		progress, err := readerLibraryProgress(
			ctx,
			tx,
			accountID,
			profileID,
			candidate.StoryID,
		)
		if err != nil {
			return model.ReaderLibraryReadModel{}, err
		}
		var progressVersionID *string
		if progress != nil {
			progressVersionID = &progress.VersionID
		}

		decision, err := readerresolution.Resolve(readerresolution.Input{
			ReadingLevel:      readingLevel,
			ReleaseEditions:   releaseEditions,
			OverrideEdition:   overrideEdition,
			ProgressVersionID: progressVersionID,
		})
		if err != nil {
			if err := incrementReaderLibraryUnavailable(&result); err != nil {
				return model.ReaderLibraryReadModel{}, err
			}
			continue
		}
		if decision.Kind == readerresolution.DecisionUnavailable {
			continue
		}
		if decision.Kind != readerresolution.DecisionChooser &&
			decision.Kind != readerresolution.DecisionSelected {
			if err := incrementReaderLibraryUnavailable(&result); err != nil {
				return model.ReaderLibraryReadModel{}, err
			}
			continue
		}

		item := model.ReaderLibraryItem{
			Slug:  candidate.Slug,
			State: model.ReaderResolutionState(decision.Kind),
			EligibleEditions: make(
				[]model.ReaderLibraryEditionSummary,
				0,
				len(decision.Eligible),
			),
		}
		var (
			commonTitle    string
			commonAuthor   *string
			commonLanguage string
			haveMetadata   bool
			itemInvalid    bool
		)
		for _, edition := range decision.Eligible {
			summary, snapshot, err := readerLibraryEditionSummary(
				ctx,
				tx,
				candidate.StoryID,
				candidate.Slug,
				edition,
			)
			if err != nil {
				if errors.Is(err, errStoredVersionInvalid) ||
					errors.Is(err, sql.ErrNoRows) {
					itemInvalid = true
					break
				}
				return model.ReaderLibraryReadModel{}, err
			}
			if !haveMetadata {
				commonTitle = snapshot.Frontmatter.Title
				commonAuthor = snapshot.Frontmatter.Author
				commonLanguage = snapshot.Frontmatter.Language
				haveMetadata = true
			} else if commonTitle != snapshot.Frontmatter.Title ||
				!sameReaderLibraryAuthor(commonAuthor, snapshot.Frontmatter.Author) ||
				commonLanguage != snapshot.Frontmatter.Language {
				itemInvalid = true
				break
			}
			item.EligibleEditions = append(item.EligibleEditions, summary)
		}
		if itemInvalid ||
			!haveMetadata ||
			len(item.EligibleEditions) != len(decision.Eligible) {
			if err := incrementReaderLibraryUnavailable(&result); err != nil {
				return model.ReaderLibraryReadModel{}, err
			}
			continue
		}

		item.Title = commonTitle
		item.Author = commonAuthor
		item.Language = commonLanguage

		if decision.Kind == readerresolution.DecisionSelected {
			if decision.Selected == nil {
				if err := incrementReaderLibraryUnavailable(&result); err != nil {
					return model.ReaderLibraryReadModel{}, err
				}
				continue
			}
			selected := decision.Selected.EditionKey
			item.SelectedEdition = &selected
		} else if decision.Selected != nil {
			if err := incrementReaderLibraryUnavailable(&result); err != nil {
				return model.ReaderLibraryReadModel{}, err
			}
			continue
		}

		if progress != nil {
			item.Progress = &model.ReaderLibraryProgressSummary{
				Version:   progress.Version,
				Percent:   progress.Percent,
				UpdatedAt: progress.UpdatedAt,
				IsResolvedVersion: decision.Selected != nil &&
					progress.VersionID == decision.Selected.VersionID,
			}
		}
		result.Items = append(result.Items, item)
	}

	if err := tx.Commit(); err != nil {
		return model.ReaderLibraryReadModel{}, err
	}
	return result, nil
}
