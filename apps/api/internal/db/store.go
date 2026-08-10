package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db           *sql.DB
	queryTimeout time.Duration
}

type Options struct {
	ConnMaxLifetime time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	QueryTimeout    time.Duration
}

func MustOpen(url string) *Store {
	return MustOpenWithOptions(url, Options{
		ConnMaxLifetime: 30 * time.Minute,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		QueryTimeout:    3 * time.Second,
	})
}

func MustOpenWithOptions(url string, opt Options) *Store {
	if strings.TrimSpace(url) == "" {
		panic("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		panic(err)
	}

	// pool tuning
	if opt.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(opt.ConnMaxLifetime)
	}
	if opt.MaxOpenConns > 0 {
		db.SetMaxOpenConns(opt.MaxOpenConns)
	}
	if opt.MaxIdleConns > 0 {
		db.SetMaxIdleConns(opt.MaxIdleConns)
	}

	qt := opt.QueryTimeout
	if qt <= 0 {
		qt = 3 * time.Second
	}

	// ping with timeout to avoid hanging startup
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		panic(err)
	}

	return &Store{
		db:           db,
		queryTimeout: qt,
	}
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ctx() (context.Context, context.CancelFunc) {
	qt := s.queryTimeout
	if qt <= 0 {
		qt = 3 * time.Second
	}
	return context.WithTimeout(context.Background(), qt)
}

func clamp01(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

var accountIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

/* ----------------------------- Library ----------------------------- */

const maxSafeJSONInteger int64 = 1<<53 - 1

var librarySlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validLibrarySlug(slug string) bool {
	return utf8.ValidString(slug) && librarySlugPattern.MatchString(slug)
}

func libraryVersionMetadata(
	frontmatterJSON []byte,
) (string, *string, string, error) {
	if !utf8.Valid(frontmatterJSON) {
		return "", nil, "", fmt.Errorf("published version frontmatter is not valid UTF-8")
	}
	var frontmatter map[string]json.RawMessage
	if err := json.Unmarshal(frontmatterJSON, &frontmatter); err != nil || frontmatter == nil {
		if err == nil {
			err = fmt.Errorf("frontmatter must be an object")
		}
		return "", nil, "", fmt.Errorf("decode published version frontmatter: %w", err)
	}

	// Only immutable version-owned metadata is safe here. The story columns are
	// updated when a newer draft is uploaded, before that draft is published.
	// Falling back to them would expose draft metadata beside older published
	// content.
	var titleValue string
	if raw, ok := frontmatter["title"]; !ok {
		return "", nil, "", fmt.Errorf("published version title is missing")
	} else if err := json.Unmarshal(raw, &titleValue); err != nil {
		return "", nil, "", fmt.Errorf("published version title is not a string")
	}
	title := strings.TrimSpace(titleValue)
	if title == "" || !utf8.ValidString(title) {
		return "", nil, "", fmt.Errorf("published version title is invalid")
	}

	var author *string
	if raw, ok := frontmatter["author"]; ok {
		if string(raw) == "null" {
			author = nil
		} else {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", nil, "", fmt.Errorf("published version author is not a string or null")
			}
			value = strings.TrimSpace(value)
			if !utf8.ValidString(value) {
				return "", nil, "", fmt.Errorf("published version author is invalid")
			}
			if value == "" {
				author = nil
			} else {
				author = &value
			}
		}
	}

	var languageValue string
	if raw, ok := frontmatter["language"]; !ok {
		return "", nil, "", fmt.Errorf("published version language is missing")
	} else if err := json.Unmarshal(raw, &languageValue); err != nil {
		return "", nil, "", fmt.Errorf("published version language is not a string")
	}
	language := strings.TrimSpace(languageValue)
	if language == "" || !utf8.ValidString(language) {
		return "", nil, "", fmt.Errorf("published version language is invalid")
	}

	return title, author, language, nil
}

/* ----------------------------- Progress ----------------------------- */

func (s *Store) ProgressGet(accountID, profileID, slug string) (model.ProgressResponse, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ProgressResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	access, err := lockReaderProfileStoryAccess(ctx, tx, accountID, profileID, slug)
	if err != nil {
		return model.ProgressResponse{}, err
	}
	eligible, err := currentReleaseHasAllowedReaderEdition(ctx, tx, access)
	if err != nil {
		return model.ProgressResponse{}, err
	}
	if !eligible {
		return model.ProgressResponse{}, sql.ErrNoRows
	}

	var (
		version     int
		locatorJSON []byte
		percent     float64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT version.version, progress.locator, progress.percent
		FROM reading_progress AS progress
		JOIN story_versions AS version
		  ON version.id = progress.story_version_id
		 AND version.story_id = progress.story_id
		WHERE progress.account_id = $1
		  AND progress.profile_id = $2
		  AND progress.story_id = $3
	`, accountID, profileID, access.StoryID).Scan(
		&version,
		&locatorJSON,
		&percent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return model.ProgressResponse{}, err
		}
		return model.ProgressResponse{Progress: nil}, nil
	}
	if err != nil {
		return model.ProgressResponse{}, err
	}

	var locator readercontract.Locator
	if err := json.Unmarshal(locatorJSON, &locator); err != nil {
		return model.ProgressResponse{}, fmt.Errorf("decode stored Reader locator: %w", err)
	}
	if err := locator.Validate(); err != nil {
		return model.ProgressResponse{}, fmt.Errorf("validate stored Reader locator: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.ProgressResponse{}, err
	}
	return model.ProgressResponse{Progress: &model.Progress{
		Version: version,
		Locator: locator,
		Percent: clamp01(percent),
	}}, nil
}

func (s *Store) ProgressPut(accountID, profileID, slug string, version int, locator readercontract.Locator, percent float64) error {
	ctx, cancel := s.ctx()
	defer cancel()

	if err := locator.Validate(); err != nil {
		return fmt.Errorf("%w: %v", readercontract.ErrLocatorMismatch, err)
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 1 {
		return fmt.Errorf("progress percent must be between 0 and 1")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	access, err := lockReaderProfileStoryAccess(ctx, tx, accountID, profileID, slug)
	if err != nil {
		return err
	}
	versionID, err := currentReleaseReaderVersionByNumber(ctx, tx, access, version)
	if err != nil {
		return err
	}

	var (
		storedKey               string
		storedOccurrence        int
		storedChapterKey        sql.NullString
		storedChapterOccurrence sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT
			content_key,
			content_occurrence,
			chapter_key,
			chapter_occurrence
		FROM story_segments
		WHERE story_version_id = $1
		  AND ordinal = $2
	`, versionID, locator.Segment.Ordinal).Scan(
		&storedKey,
		&storedOccurrence,
		&storedChapterKey,
		&storedChapterOccurrence,
	); err != nil {
		if err == sql.ErrNoRows {
			return readercontract.ErrLocatorMismatch
		}
		return err
	}

	if storedKey != locator.Segment.Key || storedOccurrence != locator.Segment.Occurrence {
		return readercontract.ErrLocatorMismatch
	}
	if storedChapterKey.Valid != (locator.Chapter != nil) {
		return readercontract.ErrLocatorMismatch
	}
	if storedChapterKey.Valid {
		if !storedChapterOccurrence.Valid ||
			storedChapterKey.String != locator.Chapter.Key ||
			int(storedChapterOccurrence.Int64) != locator.Chapter.Occurrence {
			return readercontract.ErrLocatorMismatch
		}
	}

	locatorJSON, err := json.Marshal(locator)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO reading_progress (account_id, profile_id, story_id, story_version_id, locator, percent, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (account_id, profile_id, story_id)
		DO UPDATE SET
			account_id=EXCLUDED.account_id,
			profile_id=EXCLUDED.profile_id,
			story_version_id=EXCLUDED.story_version_id,
			locator=EXCLUDED.locator,
			percent=EXCLUDED.percent,
			updated_at=now()
	`, accountID, profileID, access.StoryID, versionID, locatorJSON, percent); err != nil {
		return err
	}

	return tx.Commit()
}

/* ------------------------- Continue / Recent -------------------- */

func (s *Store) ContinueRecent(accountID, profileID string, limit int) ([]model.ContinueItem, error) {
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}

	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	allowed, err := readerEditionAllowanceFlags(model.ReaderEditionKey(readingLevelValue))
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT story.slug, progress.percent, progress.updated_at
		FROM reading_progress AS progress
		JOIN stories AS story
		  ON story.id = progress.story_id
		WHERE `+readerStoryAccessPredicate+`
		  AND progress.account_id = $1
		  AND progress.profile_id = $2
		  AND EXISTS (
			SELECT 1
			FROM story_release_editions AS member
			JOIN story_editions AS edition
			  ON edition.id = member.edition_id
			 AND edition.story_id = member.story_id
			WHERE member.release_id = story.current_release_id
			  AND member.story_id = story.id
			  AND (
				($4 AND edition.edition_key = 'classic')
				OR ($5 AND edition.edition_key = 'confident-readers')
				OR ($6 AND edition.edition_key = 'growing-readers')
				OR ($7 AND edition.edition_key = 'story-explorers')
				OR ($8 AND edition.edition_key = 'little-listeners')
			  )
		  )
		ORDER BY progress.updated_at DESC
		LIMIT $3
	`, accountID, profileID, limit,
		allowed[0], allowed[1], allowed[2], allowed[3], allowed[4],
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ContinueItem, 0, limit)
	for rows.Next() {
		var item model.ContinueItem
		if err := rows.Scan(&item.Slug, &item.Percent, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Percent = clamp01(item.Percent)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
