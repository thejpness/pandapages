package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db           *sql.DB
	queryTimeout time.Duration

	mu sync.Mutex

	// cached "Default" profile per account
	defaultProfileByAccount map[string]string
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
		db:                      db,
		queryTimeout:            qt,
		defaultProfileByAccount: map[string]string{},
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

/* ----------------------------- Accounts (Phase A) ----------------------------- */

// Stable application-scoped key for serializing default-account creation.
const ensureDefaultAccountLockID int64 = 0x50504143434f554e

var accountIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// EnsureDefaultAccount returns the deterministically oldest account id, creating
// one when the table is empty. The transaction-level advisory lock coordinates
// initialization across processes and replicas; correctness does not depend on
// this Store's in-process mutex or a cached account id.
func (s *Store) EnsureDefaultAccount() (string, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, ensureDefaultAccountLockID); err != nil {
		return "", err
	}

	selectOldest := func() (string, error) {
		var id string
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM accounts
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		`).Scan(&id)
		return id, err
	}

	id, err := selectOldest()
	if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx, `
				INSERT INTO accounts (name)
				VALUES ('Default')
			`); err != nil {
			return "", err
		}
		id, err = selectOldest()
	}
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return id, nil
}

// AccountExists reports whether accountID identifies an existing account.
// Malformed identifiers are treated as absent instead of being sent to
// PostgreSQL as invalid UUID input.
func (s *Store) AccountExists(accountID string) (bool, error) {
	accountID = strings.TrimSpace(accountID)
	if !accountIDRe.MatchString(accountID) {
		return false, nil
	}

	ctx, cancel := s.ctx()
	defer cancel()

	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM accounts
			WHERE id = $1
		)
	`, accountID).Scan(&exists)
	return exists, err
}

/* ----------------------------- Profiles ----------------------------- */

func (s *Store) getDefaultProfileID(ctx context.Context, accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", sql.ErrNoRows
	}

	// cache check
	s.mu.Lock()
	if s.defaultProfileByAccount == nil {
		s.defaultProfileByAccount = map[string]string{}
	}
	if id := s.defaultProfileByAccount[accountID]; id != "" {
		s.mu.Unlock()
		return id, nil
	}
	s.mu.Unlock()

	// select oldest Default for this account
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM profiles
		WHERE account_id = $1 AND name = 'Default'
		ORDER BY created_at ASC
		LIMIT 1
	`, accountID).Scan(&id)

	if err == sql.ErrNoRows {
		// create one if none exist
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO profiles (account_id, name)
			SELECT $1, 'Default'
			WHERE NOT EXISTS (
				SELECT 1 FROM profiles WHERE account_id = $1 AND name = 'Default'
			)
		`, accountID)
		if err != nil {
			return "", err
		}

		// reselect
		err = s.db.QueryRowContext(ctx, `
			SELECT id
			FROM profiles
			WHERE account_id = $1 AND name = 'Default'
			ORDER BY created_at ASC
			LIMIT 1
		`, accountID).Scan(&id)
	}
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.defaultProfileByAccount[accountID] = id
	s.mu.Unlock()

	return id, nil
}

/* ----------------------------- Library ----------------------------- */

func (s *Store) Library(accountID string) ([]model.StoryItem, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.slug, s.title, NULLIF(BTRIM(s.author), '')
		FROM stories s
		WHERE s.account_id = $1
		  AND s.published_version_id IS NOT NULL
		ORDER BY s.updated_at DESC, s.created_at DESC
		LIMIT 100
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StoryItem, 0, 16)
	for rows.Next() {
		var it model.StoryItem
		var author sql.NullString
		if err := rows.Scan(&it.Slug, &it.Title, &author); err != nil {
			return nil, err
		}
		it.Author = strPtr(author)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

/* ----------------------------- Reader ----------------------------- */

func (s *Store) ReaderStory(accountID, slug string) (model.ReaderStory, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	// One SQL statement gives all rows one PostgreSQL statement snapshot. A
	// publication change cannot mix metadata from one version with segments
	// from another.
	rows, err := s.db.QueryContext(ctx, `
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
	return story, nil
}

/* ----------------------------- Progress ----------------------------- */

func (s *Store) ProgressGet(accountID, slug string) (model.ProgressResponse, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return model.ProgressResponse{}, err
	}

	var (
		hasProgress bool
		version     sql.NullInt64
		locatorJSON []byte
		percent     sql.NullFloat64
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT
			rp.story_version_id IS NOT NULL,
			sv.version,
			rp.locator,
			rp.percent
		FROM stories st
		LEFT JOIN reading_progress rp
		  ON rp.story_id = st.id
		 AND rp.profile_id = $3
		LEFT JOIN story_versions sv
		  ON sv.id = rp.story_version_id
		 AND sv.story_id = st.id
		WHERE st.account_id = $1
		  AND st.slug = $2
		  AND st.is_published = true
		  AND st.published_version_id IS NOT NULL
	`, accountID, slug, profileID).Scan(&hasProgress, &version, &locatorJSON, &percent)
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

func (s *Store) ProgressPut(accountID, slug string, version int, locator readercontract.Locator, percent float64) error {
	ctx, cancel := s.ctx()
	defer cancel()

	if err := locator.Validate(); err != nil {
		return fmt.Errorf("%w: %v", readercontract.ErrLocatorMismatch, err)
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 1 {
		return fmt.Errorf("progress percent must be between 0 and 1")
	}

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return err
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
		INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (profile_id, story_id)
		DO UPDATE SET
			story_version_id=EXCLUDED.story_version_id,
			locator=EXCLUDED.locator,
			percent=EXCLUDED.percent,
			updated_at=now()
	`, profileID, storyID, versionID, locatorJSON, percent); err != nil {
		return err
	}

	return tx.Commit()
}

/* ------------------------- Continue / Recent -------------------- */

func (s *Store) ContinueRecent(accountID string, limit int) ([]model.ContinueItem, error) {
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}

	ctx, cancel := s.ctx()
	defer cancel()

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT st.slug, rp.percent, rp.updated_at
		FROM reading_progress rp
		JOIN stories st ON st.id = rp.story_id
		WHERE st.account_id = $2
		  AND st.published_version_id IS NOT NULL
		  AND rp.profile_id = $3
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

func (s *Store) ensureProfileSettingsRow(ctx context.Context, profileID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO profile_settings (profile_id)
		VALUES ($1)
		ON CONFLICT (profile_id) DO NOTHING
	`, profileID)
	return err
}

func (s *Store) SettingsGet(accountID string) (model.SettingsPayload, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return model.SettingsPayload{}, err
	}
	if err := s.ensureProfileSettingsRow(ctx, profileID); err != nil {
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
	err = s.db.QueryRowContext(ctx, `
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
		FROM profile_settings ps
		LEFT JOIN child_profiles cp
			ON cp.id = ps.active_child_profile_id
		   AND cp.account_id = $2
		LEFT JOIN prompt_profiles pp
			ON pp.id = ps.active_prompt_profile_id
		   AND pp.account_id = $2
		WHERE ps.profile_id = $1
	`, profileID, accountID).Scan(
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
		payload.Prompt.Name = "Default prompt v1"
	}

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return model.SettingsPayload{}, err
	}
	if err := s.ensureProfileSettingsRow(ctx, profileID); err != nil {
		return model.SettingsPayload{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SettingsPayload{}, err
	}
	defer func() { _ = tx.Rollback() }()

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
			UPDATE profile_settings
			SET active_child_profile_id = COALESCE(NULLIF($2,'' )::uuid, active_child_profile_id),
			    active_prompt_profile_id = COALESCE(NULLIF($3,'' )::uuid, active_prompt_profile_id),
			    updated_at = now()
			WHERE profile_id = $1
		`, profileID, childID, promptID)
		if err != nil {
			return model.SettingsPayload{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.SettingsPayload{}, err
	}

	return s.SettingsGet(accountID)
}
