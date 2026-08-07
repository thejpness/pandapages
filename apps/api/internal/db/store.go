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

func strPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := strings.TrimSpace(ns.String)
	if v == "" {
		return nil
	}
	out := v
	return &out
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

func (s *Store) Library(accountID string) (model.LibraryReadModel, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	// Segment rows are kept in this single statement so catalogue metadata and
	// Reader 2 identities all come from one PostgreSQL snapshot. Reading
	// progress is intentionally absent: it is reader-profile scoped and never
	// inferred for an account-scoped catalogue response.
	rows, err := s.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT
				story.id AS story_id,
				story.slug,
				story.created_at,
				story.updated_at,
				story.published_version_id AS requested_published_version_id,
				version.id AS published_version_id,
				version.version AS published_version,
				version.frontmatter::text AS frontmatter
			FROM stories AS story
			LEFT JOIN story_versions AS version
			  ON version.id = story.published_version_id
			 AND version.story_id = story.id
			WHERE story.account_id = $1
			  AND story.is_published = true
		)
		SELECT
			candidates.story_id,
			candidates.slug,
			candidates.frontmatter,
			candidates.requested_published_version_id,
			candidates.published_version_id,
			candidates.published_version,
			segment.id,
			segment.ordinal,
			segment.segment_kind,
			segment.heading_level,
			segment.content_key,
			segment.content_occurrence,
			segment.chapter_key,
			segment.chapter_occurrence,
			segment.word_count
		FROM candidates
		LEFT JOIN story_segments AS segment
		  ON segment.story_version_id = candidates.published_version_id
		ORDER BY
			candidates.updated_at DESC,
			candidates.created_at DESC,
			candidates.slug ASC,
			candidates.story_id ASC,
			segment.ordinal ASC NULLS FIRST
	`, accountID)
	if err != nil {
		return model.LibraryReadModel{}, err
	}
	defer rows.Close()

	type storyAccumulator struct {
		item               model.StoryItem
		storyID            string
		publishedVersionID string
		identities         []readercontract.StoredSegmentIdentity
		wordCount          int64
		invalid            bool
	}

	result := model.LibraryReadModel{Items: make([]model.StoryItem, 0, 16)}
	var current *storyAccumulator
	finalize := func() error {
		if current == nil {
			return nil
		}
		if len(current.identities) == 0 {
			current.invalid = true
		}
		if !current.invalid {
			chapterCount, err := readercontract.ValidateStoredSegmentIdentities(current.identities)
			if err != nil {
				current.invalid = true
			} else {
				current.item.WordCount = current.wordCount
				current.item.ChapterCount = int64(chapterCount)
			}
		}
		if current.invalid {
			if result.UnavailableItemCount == maxSafeJSONInteger {
				return fmt.Errorf("unavailable library item count exceeds the safe JSON integer range")
			}
			result.UnavailableItemCount++
			return nil
		}
		result.Items = append(result.Items, current.item)
		return nil
	}

	for rows.Next() {
		var (
			storyID              string
			slug                 string
			frontmatterJSON      sql.NullString
			requestedVersionID   sql.NullString
			publishedVersionID   sql.NullString
			publishedVersion     sql.NullInt64
			segmentID            sql.NullString
			segmentOrdinal       sql.NullInt64
			segmentKind          sql.NullString
			segmentHeadingLevel  sql.NullInt64
			segmentContentKey    sql.NullString
			segmentOccurrence    sql.NullInt64
			segmentChapterKey    sql.NullString
			segmentChapterNumber sql.NullInt64
			segmentWordCount     sql.NullInt64
		)
		if err := rows.Scan(
			&storyID,
			&slug,
			&frontmatterJSON,
			&requestedVersionID,
			&publishedVersionID,
			&publishedVersion,
			&segmentID,
			&segmentOrdinal,
			&segmentKind,
			&segmentHeadingLevel,
			&segmentContentKey,
			&segmentOccurrence,
			&segmentChapterKey,
			&segmentChapterNumber,
			&segmentWordCount,
		); err != nil {
			return model.LibraryReadModel{}, err
		}

		if current == nil || current.storyID != storyID {
			if err := finalize(); err != nil {
				return model.LibraryReadModel{}, err
			}
			current = &storyAccumulator{
				item:       model.StoryItem{Slug: slug},
				storyID:    storyID,
				identities: make([]readercontract.StoredSegmentIdentity, 0, 32),
			}

			if strings.TrimSpace(storyID) == "" || !validLibrarySlug(slug) ||
				!requestedVersionID.Valid || strings.TrimSpace(requestedVersionID.String) == "" ||
				!publishedVersionID.Valid || strings.TrimSpace(publishedVersionID.String) == "" ||
				requestedVersionID.String != publishedVersionID.String ||
				!frontmatterJSON.Valid || !publishedVersion.Valid {
				current.invalid = true
			} else {
				current.publishedVersionID = publishedVersionID.String
			}
			versionValue := int(publishedVersion.Int64)
			if !publishedVersion.Valid || publishedVersion.Int64 <= 0 || int64(versionValue) != publishedVersion.Int64 {
				current.invalid = true
			} else {
				current.item.PublishedVersion = versionValue
			}

			title, author, language, metadataErr := libraryVersionMetadata([]byte(frontmatterJSON.String))
			if !frontmatterJSON.Valid || metadataErr != nil {
				current.invalid = true
			} else {
				current.item.Title = title
				current.item.Author = author
				current.item.Language = language
			}

		}

		if !segmentID.Valid {
			if segmentOrdinal.Valid || segmentKind.Valid || segmentHeadingLevel.Valid ||
				segmentContentKey.Valid || segmentOccurrence.Valid || segmentChapterKey.Valid ||
				segmentChapterNumber.Valid || segmentWordCount.Valid {
				current.invalid = true
			}
			continue
		}
		if strings.TrimSpace(segmentID.String) == "" || !segmentOrdinal.Valid || !segmentKind.Valid ||
			!segmentContentKey.Valid || !segmentOccurrence.Valid || !segmentWordCount.Valid {
			current.invalid = true
			continue
		}

		ordinal := int(segmentOrdinal.Int64)
		occurrence := int(segmentOccurrence.Int64)
		if segmentOrdinal.Int64 <= 0 || int64(ordinal) != segmentOrdinal.Int64 ||
			segmentOccurrence.Int64 <= 0 || int64(occurrence) != segmentOccurrence.Int64 ||
			segmentWordCount.Int64 < 0 || segmentWordCount.Int64 > maxSafeJSONInteger-current.wordCount {
			current.invalid = true
			continue
		}

		identity := readercontract.StoredSegmentIdentity{
			Ordinal:           ordinal,
			Kind:              readercontract.SegmentKind(segmentKind.String),
			ContentKey:        segmentContentKey.String,
			ContentOccurrence: occurrence,
		}
		if segmentHeadingLevel.Valid {
			value := int(segmentHeadingLevel.Int64)
			if int64(value) != segmentHeadingLevel.Int64 {
				current.invalid = true
				continue
			}
			identity.HeadingLevel = &value
		}
		if segmentChapterKey.Valid != segmentChapterNumber.Valid {
			current.invalid = true
			continue
		}
		if segmentChapterKey.Valid {
			value := int(segmentChapterNumber.Int64)
			if segmentChapterNumber.Int64 <= 0 || int64(value) != segmentChapterNumber.Int64 {
				current.invalid = true
				continue
			}
			key := segmentChapterKey.String
			identity.ChapterKey = &key
			identity.ChapterOccurrence = &value
		}
		current.wordCount += segmentWordCount.Int64
		current.identities = append(current.identities, identity)
	}
	if err := rows.Err(); err != nil {
		return model.LibraryReadModel{}, err
	}
	if err := finalize(); err != nil {
		return model.LibraryReadModel{}, err
	}
	return result, nil
}

/* ----------------------------- Reader ----------------------------- */

func (s *Store) ReaderStory(accountID, slug string) (model.ReaderStory, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ReaderStory{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var storyID, versionID string
	err = tx.QueryRowContext(ctx, `
		SELECT id, published_version_id
		FROM stories
		WHERE account_id = $1
		  AND slug = $2
		  AND is_published = true
		  AND published_version_id IS NOT NULL
		FOR SHARE
	`, accountID, slug).Scan(&storyID, &versionID)
	if err != nil {
		return model.ReaderStory{}, err
	}
	if _, err := validateStoredReaderVersion(ctx, tx, storyID, versionID, slug); err != nil {
		if errors.Is(err, errStoredVersionInvalid) || errors.Is(err, sql.ErrNoRows) {
			return model.ReaderStory{}, sql.ErrNoRows
		}
		return model.ReaderStory{}, err
	}
	// The story-row lock keeps the publication pointer stable while the selected
	// immutable version is revalidated. This statement then returns one coherent
	// PostgreSQL snapshot for that validated published version.
	// from another.
	rows, err := tx.QueryContext(ctx, `
		SELECT
			st.slug,
			st.title,
			NULLIF(BTRIM(st.author), ''),
			st.language,
			version.version,
			segment.ordinal,
			segment.segment_kind,
			segment.heading_level,
			segment.content_key,
			segment.content_occurrence,
			segment.chapter_key,
			segment.chapter_occurrence,
			segment.rendered_html,
			segment.word_count
		FROM stories st
		JOIN story_versions AS version
		  ON version.id = st.published_version_id
		 AND version.story_id = st.id
		LEFT JOIN story_segments AS segment
		  ON segment.story_version_id = version.id
		WHERE st.account_id = $1
		  AND st.slug = $2
		  AND st.is_published = true
		  AND st.published_version_id IS NOT NULL
		ORDER BY segment.ordinal
	`, accountID, slug)
	if err != nil {
		return model.ReaderStory{}, err
	}
	defer rows.Close()

	var story model.ReaderStory
	found := false
	story.Segments = make([]model.ReaderSegment, 0, 64)
	for rows.Next() {
		var (
			author            sql.NullString
			ordinal           sql.NullInt64
			kind              sql.NullString
			headingLevel      sql.NullInt64
			contentKey        sql.NullString
			contentOccurrence sql.NullInt64
			chapterKey        sql.NullString
			chapterOccurrence sql.NullInt64
			renderedHTML      sql.NullString
			wordCount         sql.NullInt64
		)
		if err := rows.Scan(
			&story.Slug,
			&story.Title,
			&author,
			&story.Language,
			&story.Version,
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
			return model.ReaderStory{}, err
		}
		found = true
		story.Author = strPtr(author)
		if !ordinal.Valid {
			continue
		}

		segment := model.ReaderSegment{
			Ordinal:           int(ordinal.Int64),
			Kind:              kind.String,
			ContentKey:        contentKey.String,
			ContentOccurrence: int(contentOccurrence.Int64),
			RenderedHTML:      renderedHTML.String,
			WordCount:         int(wordCount.Int64),
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
		return model.ReaderStory{}, err
	}
	if !found {
		return model.ReaderStory{}, sql.ErrNoRows
	}
	if len(story.Segments) == 0 {
		// Historical versions created outside the current ingestion path must not
		// produce a successful but unreadable Reader payload.
		return model.ReaderStory{}, sql.ErrNoRows
	}
	storedIdentities := make([]readercontract.StoredSegmentIdentity, 0, len(story.Segments))
	for _, segment := range story.Segments {
		if segment.WordCount < 0 {
			return model.ReaderStory{}, fmt.Errorf("published Reader segment word count is invalid")
		}
		storedIdentities = append(storedIdentities, readercontract.StoredSegmentIdentity{
			Ordinal:           segment.Ordinal,
			Kind:              readercontract.SegmentKind(segment.Kind),
			HeadingLevel:      segment.HeadingLevel,
			ContentKey:        segment.ContentKey,
			ContentOccurrence: segment.ContentOccurrence,
			ChapterKey:        segment.ChapterKey,
			ChapterOccurrence: segment.ChapterOccurrence,
		})
	}
	if _, err := readercontract.ValidateStoredSegmentIdentities(storedIdentities); err != nil {
		return model.ReaderStory{}, fmt.Errorf("validate published Reader segment identities: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.ReaderStory{}, err
	}
	return story, nil
}

/* ----------------------------- Progress ----------------------------- */

func (s *Store) ProgressGet(accountID, profileID, slug string) (model.ProgressResponse, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var (
		hasProgress bool
		version     sql.NullInt64
		locatorJSON []byte
		percent     sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			rp.story_version_id IS NOT NULL,
			sv.version,
			rp.locator,
			rp.percent
		FROM stories st
		LEFT JOIN reading_progress rp
		  ON rp.story_id = st.id
		 AND rp.account_id = $1
		 AND rp.profile_id = $2
		LEFT JOIN story_versions sv
		  ON sv.id = rp.story_version_id
		 AND sv.story_id = st.id
		WHERE st.account_id = $1
		  AND st.slug = $3
		  AND st.is_published = true
		  AND st.published_version_id IS NOT NULL
	`, accountID, profileID, slug).Scan(&hasProgress, &version, &locatorJSON, &percent)
	if err != nil {
		return model.ProgressResponse{}, err
	}
	if !hasProgress {
		return model.ProgressResponse{Progress: nil}, nil
	}
	if !version.Valid || !percent.Valid {
		return model.ProgressResponse{}, fmt.Errorf("stored progress is incomplete")
	}

	var locator readercontract.Locator
	if err := json.Unmarshal(locatorJSON, &locator); err != nil {
		return model.ProgressResponse{}, fmt.Errorf("decode stored Reader locator: %w", err)
	}
	if err := locator.Validate(); err != nil {
		return model.ProgressResponse{}, fmt.Errorf("validate stored Reader locator: %w", err)
	}
	return model.ProgressResponse{Progress: &model.Progress{
		Version: int(version.Int64),
		Locator: locator,
		Percent: clamp01(percent.Float64),
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

	var storyID, versionID string
	if err := tx.QueryRowContext(ctx, `
		SELECT story.id, version.id
		FROM stories AS story
		JOIN story_versions AS version
		  ON version.id = story.published_version_id
		 AND version.story_id = story.id
		 AND version.version = $3
		WHERE story.account_id = $1
		  AND story.slug = $2
		  AND story.is_published = true
		  AND story.published_version_id IS NOT NULL
		FOR SHARE OF story
	`, accountID, slug, version).Scan(&storyID, &versionID); err != nil {
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
	`, accountID, profileID, storyID, versionID, locatorJSON, percent); err != nil {
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

	rows, err := s.db.QueryContext(ctx, `
		SELECT st.slug, rp.percent, rp.updated_at
		FROM reading_progress rp
		JOIN stories st
		  ON st.id = rp.story_id
		 AND st.account_id = rp.account_id
		WHERE rp.account_id = $2
		  AND rp.profile_id = $3
		  AND st.account_id = $2
		  AND st.published_version_id IS NOT NULL
		ORDER BY rp.updated_at DESC
		LIMIT $1
	`, limit, accountID, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ContinueItem, 0, limit)
	for rows.Next() {
		var it model.ContinueItem
		if err := rows.Scan(&it.Slug, &it.Percent, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.Percent = clamp01(it.Percent)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

/* ----------------------------- Settings / Journey ---------------------------- */

type settingsExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ensureAccountSettingsRow(ctx context.Context, execer settingsExecer, accountID string) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO account_settings (account_id)
		VALUES ($1)
		ON CONFLICT (account_id) DO NOTHING
	`, accountID)
	return err
}

func (s *Store) SettingsGet(accountID string) (model.SettingsPayload, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	if err := ensureAccountSettingsRow(ctx, s.db, accountID); err != nil {
		return model.SettingsPayload{}, err
	}

	var (
		childID   sql.NullString
		childName sql.NullString
		ageMonths sql.NullInt32
		interests json.RawMessage
		sens      json.RawMessage

		promptID    sql.NullString
		promptName  sql.NullString
		promptRules json.RawMessage
		schemaVer   sql.NullInt32
	)

	// Scope child/prompt via JOIN conditions to avoid cross-account leakage.
	err := s.db.QueryRowContext(ctx, `
		SELECT
			cp.id::text,
			cp.name,
			cp.age_months,
			COALESCE(cp.interests, '[]'::jsonb),
			COALESCE(cp.sensitivities, '[]'::jsonb),
			pp.id::text,
			pp.name,
			COALESCE(pp.rules, '{}'::jsonb),
			pp.schema_version
		FROM account_settings ps
		LEFT JOIN child_profiles cp
			ON cp.id = ps.active_child_profile_id
		   AND cp.account_id = ps.account_id
		LEFT JOIN prompt_profiles pp
			ON pp.id = ps.active_prompt_profile_id
		   AND pp.account_id = ps.account_id
		WHERE ps.account_id = $1
	`, accountID).Scan(
		&childID, &childName, &ageMonths, &interests, &sens,
		&promptID, &promptName, &promptRules, &schemaVer,
	)
	if err != nil {
		return model.SettingsPayload{}, err
	}

	out := model.SettingsPayload{}

	if childID.Valid {
		out.Child.ID = strings.TrimSpace(childID.String)
		if childName.Valid {
			out.Child.Name = strings.TrimSpace(childName.String)
		}
		if ageMonths.Valid {
			out.Child.AgeMonths = int(ageMonths.Int32)
		}
		_ = json.Unmarshal(interests, &out.Child.Interests)
		_ = json.Unmarshal(sens, &out.Child.Sensitivities)
	}

	if promptID.Valid {
		out.Prompt.ID = strings.TrimSpace(promptID.String)
		if promptName.Valid {
			out.Prompt.Name = strings.TrimSpace(promptName.String)
		}
		if schemaVer.Valid {
			out.Prompt.SchemaVersion = int(schemaVer.Int32)
		}
		out.Prompt.Rules = promptRules
	}

	return out, nil
}

func (s *Store) SettingsPut(accountID string, payload model.SettingsUpsert) (model.SettingsPayload, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	// harden inputs
	payload.Child.ID = strings.TrimSpace(payload.Child.ID)
	payload.Child.Name = strings.TrimSpace(payload.Child.Name)
	if payload.Child.AgeMonths < 0 {
		payload.Child.AgeMonths = 0
	}
	if payload.Child.Interests == nil {
		payload.Child.Interests = []string{}
	}
	if payload.Child.Sensitivities == nil {
		payload.Child.Sensitivities = []string{}
	}

	payload.Prompt.ID = strings.TrimSpace(payload.Prompt.ID)
	payload.Prompt.Name = strings.TrimSpace(payload.Prompt.Name)
	if payload.Prompt.SchemaVersion <= 0 {
		payload.Prompt.SchemaVersion = 1
	}
	if len(payload.Prompt.Rules) == 0 {
		payload.Prompt.Rules = json.RawMessage(`{}`)
	}
	// prompt_profiles.name is NOT NULL
	if payload.Prompt.Name == "" {
		payload.Prompt.Name = "Panda Pages prompt v1"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SettingsPayload{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureAccountSettingsRow(ctx, tx, accountID); err != nil {
		return model.SettingsPayload{}, err
	}

	var childID string
	if payload.Child.Name != "" {
		intsJSON, _ := json.Marshal(payload.Child.Interests)
		sensJSON, _ := json.Marshal(payload.Child.Sensitivities)

		if payload.Child.ID != "" {
			childID = payload.Child.ID
			// scope update by account_id to avoid cross-account updates
			res, err := tx.ExecContext(ctx, `
				UPDATE child_profiles
				SET name=$3, age_months=$4, interests=$5::jsonb, sensitivities=$6::jsonb, updated_at=now()
				WHERE id=$1 AND account_id=$2
			`, childID, accountID, payload.Child.Name, payload.Child.AgeMonths, string(intsJSON), string(sensJSON))
			if err != nil {
				return model.SettingsPayload{}, err
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				// If the id doesn't belong to this account, treat as insert.
				childID = ""
			}
		}

		if childID == "" {
			err = tx.QueryRowContext(ctx, `
				INSERT INTO child_profiles (account_id, name, age_months, interests, sensitivities)
				VALUES ($1,$2,$3,$4::jsonb,$5::jsonb)
				RETURNING id
			`, accountID, payload.Child.Name, payload.Child.AgeMonths, string(intsJSON), string(sensJSON)).Scan(&childID)
			if err != nil {
				return model.SettingsPayload{}, err
			}
		}
	}

	var promptID string
	// Proceed if we have an ID OR any meaningful prompt data (we default name/rules/schemaVersion above)
	if payload.Prompt.ID != "" || payload.Prompt.Name != "" || len(payload.Prompt.Rules) > 0 {
		rules := payload.Prompt.Rules

		if payload.Prompt.ID != "" {
			promptID = payload.Prompt.ID
			// scope update by account_id to avoid cross-account updates
			res, err := tx.ExecContext(ctx, `
				UPDATE prompt_profiles
				SET name=$3, rules=$4::jsonb, schema_version=$5, updated_at=now()
				WHERE id=$1 AND account_id=$2
			`, promptID, accountID, payload.Prompt.Name, string(rules), payload.Prompt.SchemaVersion)
			if err != nil {
				return model.SettingsPayload{}, err
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				promptID = ""
			}
		}

		if promptID == "" {
			err = tx.QueryRowContext(ctx, `
				INSERT INTO prompt_profiles (account_id, name, rules, schema_version)
				VALUES ($1,$2,$3::jsonb,$4)
				RETURNING id
			`, accountID, payload.Prompt.Name, string(rules), payload.Prompt.SchemaVersion).Scan(&promptID)
			if err != nil {
				return model.SettingsPayload{}, err
			}
		}
	}

	if childID != "" || promptID != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE account_settings
			SET active_child_profile_id = COALESCE(NULLIF($2,'' )::uuid, active_child_profile_id),
			    active_prompt_profile_id = COALESCE(NULLIF($3,'' )::uuid, active_prompt_profile_id),
			    updated_at = now()
			WHERE account_id = $1
		`, accountID, childID, promptID)
		if err != nil {
			return model.SettingsPayload{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.SettingsPayload{}, err
	}

	return s.SettingsGet(accountID)
}
