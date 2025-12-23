package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"pandapages/api/internal/model"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db           *sql.DB
	queryTimeout time.Duration

	mu sync.Mutex

	// cached "default account" (Phase A)
	defaultAccountID string

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

// EnsureDefaultAccount returns the oldest account id (creates one if needed).
func (s *Store) EnsureDefaultAccount() (string, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	// fast path cache
	s.mu.Lock()
	if s.defaultAccountID != "" {
		id := s.defaultAccountID
		s.mu.Unlock()
		return id, nil
	}
	s.mu.Unlock()

	// pick oldest
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM accounts
		ORDER BY created_at ASC
		LIMIT 1
	`).Scan(&id)

	if err == sql.ErrNoRows {
		// create then reselect
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO accounts (name)
			VALUES ('Default')
		`)
		if err != nil {
			return "", err
		}
		err = s.db.QueryRowContext(ctx, `
			SELECT id
			FROM accounts
			ORDER BY created_at ASC
			LIMIT 1
		`).Scan(&id)
	}
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.defaultAccountID = id
	s.mu.Unlock()

	return id, nil
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

/* ----------------------------- Story ----------------------------- */

func (s *Store) StoryLatest(accountID, slug string) (model.StoryPayload, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	var p model.StoryPayload
	var author sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT st.slug, st.title, NULLIF(BTRIM(st.author), ''), v.version, v.rendered_html
		FROM stories st
		JOIN story_versions v ON v.id = st.published_version_id
		WHERE st.account_id = $1
		  AND st.slug = $2
		  AND st.published_version_id IS NOT NULL
	`, accountID, slug).Scan(&p.Slug, &p.Title, &author, &p.Version, &p.RenderedHTML)
	if err != nil {
		return p, err
	}

	p.Author = strPtr(author)
	return p, nil
}

func (s *Store) StorySegments(accountID, slug string) (model.StorySegmentsPayload, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	// Get published version + version number (scoped)
	var versionID string
	var version int
	err := s.db.QueryRowContext(ctx, `
		SELECT sv.id, sv.version
		FROM stories st
		JOIN story_versions sv ON sv.id = st.published_version_id
		WHERE st.account_id = $1
		  AND st.slug = $2
		  AND st.published_version_id IS NOT NULL
	`, accountID, slug).Scan(&versionID, &version)
	if err != nil {
		return model.StorySegmentsPayload{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ordinal, locator, rendered_html
		FROM story_segments
		WHERE story_version_id = $1
		ORDER BY ordinal
	`, versionID)
	if err != nil {
		return model.StorySegmentsPayload{}, err
	}
	defer rows.Close()

	segs := make([]model.Segment, 0, 64)
	for rows.Next() {
		var seg model.Segment
		if err := rows.Scan(&seg.Ordinal, &seg.Locator, &seg.RenderedHTML); err != nil {
			return model.StorySegmentsPayload{}, err
		}
		segs = append(segs, seg)
	}
	if err := rows.Err(); err != nil {
		return model.StorySegmentsPayload{}, err
	}

	return model.StorySegmentsPayload{
		Slug:     slug,
		Version:  version,
		Segments: segs,
	}, nil
}

/* ----------------------------- Progress ----------------------------- */

func (s *Store) ProgressGet(accountID, slug string) (model.ProgressState, error) {
	ctx, cancel := s.ctx()
	defer cancel()

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return model.ProgressState{}, err
	}

	var st model.ProgressState
	err = s.db.QueryRowContext(ctx, `
		SELECT sv.version, rp.locator, rp.percent
		FROM stories st
		JOIN reading_progress rp ON rp.story_id = st.id AND rp.profile_id = $3
		JOIN story_versions sv ON sv.id = rp.story_version_id
		WHERE st.account_id = $1
		  AND st.slug = $2
		  AND st.published_version_id IS NOT NULL
	`, accountID, slug, profileID).Scan(&st.Version, &st.Locator, &st.Percent)

	if err == nil {
		st.Percent = clamp01(st.Percent)
	}
	return st, err
}

func (s *Store) ProgressPut(accountID, slug string, version int, locator json.RawMessage, percent float64) error {
	ctx, cancel := s.ctx()
	defer cancel()

	profileID, err := s.getDefaultProfileID(ctx, accountID)
	if err != nil {
		return err
	}

	percent = clamp01(percent)

	// ensure story exists + published pointer (scoped)
	var storyID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM stories
		WHERE account_id = $1
		  AND slug = $2
		  AND published_version_id IS NOT NULL
	`, accountID, slug).Scan(&storyID); err != nil {
		return err
	}

	// ensure version exists for that story
	var versionID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM story_versions
		WHERE story_id = $1 AND version = $2
	`, storyID, version).Scan(&versionID); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (profile_id, story_id)
		DO UPDATE SET
			story_version_id=EXCLUDED.story_version_id,
			locator=EXCLUDED.locator,
			percent=EXCLUDED.percent,
			updated_at=now()
	`, profileID, storyID, versionID, locator, percent)

	return err
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
