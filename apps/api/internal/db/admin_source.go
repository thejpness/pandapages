package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

var errStoredSourceInvalid = errors.New("stored canonical source is invalid")

type adminCanonicalSource struct {
	Title        string
	Author       *string
	Language     string
	Rights       map[string]any
	SourceURL    *string
	SourceText   string
	SnapshotHash string
}

type adminSourceMetadata struct {
	Version   int
	Title     string
	Author    *string
	Language  string
	Rights    map[string]any
	SourceURL *string
	CreatedAt time.Time
}

type adminSourceSnapshot struct {
	ID string
	adminSourceMetadata
	SourceText   string
	SnapshotHash string
}

type inspectedAdminSource struct {
	SourceID string
	Summary  model.AdminStorySourceSummary
	Current  *adminSourceMetadata
	Versions []model.AdminSourceVersionSummary
}

func canonicalAdminSourceInput(req model.AdminSourceUpsertRequest) (adminCanonicalSource, error) {
	title := strings.TrimSpace(req.Title)
	language := "en-GB"
	if req.Language != nil && strings.TrimSpace(*req.Language) != "" {
		language = strings.TrimSpace(*req.Language)
	}

	var author *string
	if req.Author != nil {
		value := strings.TrimSpace(*req.Author)
		if value != "" {
			author = &value
		}
	}

	var sourceURL *string
	if req.SourceURL != nil {
		value := strings.TrimSpace(*req.SourceURL)
		if value != "" {
			sourceURL = &value
		}
	}

	issues := make([]model.AdminValidationIssue, 0, 5)
	if title == "" {
		issues = append(issues, model.AdminValidationIssue{
			Field: "title", Code: "required", Message: "Enter the original source title",
		})
	} else if !utf8.ValidString(req.Title) {
		issues = append(issues, model.AdminValidationIssue{
			Field: "title", Code: "invalid_encoding", Message: "Enter valid source title text",
		})
	}
	if !utf8.ValidString(language) ||
		(req.Author != nil && !utf8.ValidString(*req.Author)) ||
		(req.SourceURL != nil && !utf8.ValidString(*req.SourceURL)) {
		issues = append(issues, model.AdminValidationIssue{
			Field: "metadata", Code: "invalid_encoding", Message: "Enter valid source metadata",
		})
	}
	if strings.TrimSpace(req.SourceText) == "" {
		issues = append(issues, model.AdminValidationIssue{
			Field: "sourceText", Code: "required", Message: "Enter the original source text",
		})
	} else if !utf8.ValidString(req.SourceText) {
		issues = append(issues, model.AdminValidationIssue{
			Field: "sourceText", Code: "invalid_encoding", Message: "Enter valid source text",
		})
	}

	rights := map[string]any{}
	encodedRights, err := json.Marshal(req.Rights)
	if err != nil {
		issues = append(issues, model.AdminValidationIssue{
			Field: "rights", Code: "invalid", Message: "Enter valid rights information",
		})
	} else if req.Rights != nil {
		decoded, ok := decodeJSONDocument(encodedRights)
		if !ok {
			issues = append(issues, model.AdminValidationIssue{
				Field: "rights", Code: "invalid", Message: "Enter valid rights information",
			})
		} else if value, ok := decoded.(map[string]any); ok {
			rights = value
		} else {
			issues = append(issues, model.AdminValidationIssue{
				Field: "rights", Code: "invalid", Message: "Enter valid rights information",
			})
		}
	}
	if len(issues) > 0 {
		return adminCanonicalSource{}, &model.AdminValidationError{Issues: issues}
	}

	source := adminCanonicalSource{
		Title:      title,
		Author:     author,
		Language:   language,
		Rights:     rights,
		SourceURL:  sourceURL,
		SourceText: req.SourceText,
	}
	source.SnapshotHash, err = canonicalSourceSnapshotHash(source)
	if err != nil {
		return adminCanonicalSource{}, err
	}
	return source, nil
}

func canonicalSourceSnapshotHash(source adminCanonicalSource) (string, error) {
	payload := struct {
		Title      string         `json:"title"`
		Author     *string        `json:"author"`
		Language   string         `json:"language"`
		Rights     map[string]any `json:"rights"`
		SourceURL  *string        `json:"sourceUrl"`
		SourceText string         `json:"sourceText"`
	}{
		Title:      source.Title,
		Author:     source.Author,
		Language:   source.Language,
		Rights:     source.Rights,
		SourceURL:  source.SourceURL,
		SourceText: source.SourceText,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeStoredSourceMetadata(
	version int64,
	title string,
	author sql.NullString,
	language string,
	rightsJSON string,
	sourceURL sql.NullString,
	createdAt time.Time,
) (adminSourceMetadata, error) {
	versionValue := int(version)
	if version <= 0 || int64(versionValue) != version {
		return adminSourceMetadata{}, fmt.Errorf("%w: version", errStoredSourceInvalid)
	}
	if !utf8.ValidString(title) || strings.TrimSpace(title) == "" || title != strings.TrimSpace(title) {
		return adminSourceMetadata{}, fmt.Errorf("%w: title", errStoredSourceInvalid)
	}
	if !utf8.ValidString(language) || strings.TrimSpace(language) == "" || language != strings.TrimSpace(language) {
		return adminSourceMetadata{}, fmt.Errorf("%w: language", errStoredSourceInvalid)
	}

	var authorValue *string
	if author.Valid {
		if !utf8.ValidString(author.String) || strings.TrimSpace(author.String) == "" ||
			author.String != strings.TrimSpace(author.String) {
			return adminSourceMetadata{}, fmt.Errorf("%w: author", errStoredSourceInvalid)
		}
		value := author.String
		authorValue = &value
	}

	var sourceURLValue *string
	if sourceURL.Valid {
		if !utf8.ValidString(sourceURL.String) || strings.TrimSpace(sourceURL.String) == "" ||
			sourceURL.String != strings.TrimSpace(sourceURL.String) {
			return adminSourceMetadata{}, fmt.Errorf("%w: source URL", errStoredSourceInvalid)
		}
		value := sourceURL.String
		sourceURLValue = &value
	}

	decodedRights, ok := decodeJSONDocument([]byte(rightsJSON))
	if !ok {
		return adminSourceMetadata{}, fmt.Errorf("%w: rights", errStoredSourceInvalid)
	}
	rights, ok := decodedRights.(map[string]any)
	if !ok {
		return adminSourceMetadata{}, fmt.Errorf("%w: rights", errStoredSourceInvalid)
	}

	return adminSourceMetadata{
		Version:   versionValue,
		Title:     title,
		Author:    authorValue,
		Language:  language,
		Rights:    rights,
		SourceURL: sourceURLValue,
		CreatedAt: createdAt,
	}, nil
}

func loadAdminSourceVersionSnapshot(
	ctx context.Context,
	queryer storedVersionQueryer,
	storyID string,
	sourceID string,
	versionID string,
) (adminSourceSnapshot, error) {
	var (
		id           string
		version      int64
		title        string
		author       sql.NullString
		language     string
		rightsJSON   string
		sourceURL    sql.NullString
		sourceText   string
		snapshotHash string
		createdAt    time.Time
	)
	if err := queryer.QueryRowContext(ctx, `
		SELECT
			id,
			version,
			title,
			author,
			language,
			rights::text,
			source_url,
			source_text,
			snapshot_hash,
			created_at
		FROM story_source_versions
		WHERE id = $1
		  AND source_id = $2
		  AND story_id = $3
	`, versionID, sourceID, storyID).Scan(
		&id,
		&version,
		&title,
		&author,
		&language,
		&rightsJSON,
		&sourceURL,
		&sourceText,
		&snapshotHash,
		&createdAt,
	); err != nil {
		return adminSourceSnapshot{}, err
	}
	if !utf8.ValidString(sourceText) || strings.TrimSpace(sourceText) == "" ||
		len(snapshotHash) != 64 {
		return adminSourceSnapshot{}, fmt.Errorf("%w: source body", errStoredSourceInvalid)
	}
	if _, err := hex.DecodeString(snapshotHash); err != nil {
		return adminSourceSnapshot{}, fmt.Errorf("%w: snapshot hash", errStoredSourceInvalid)
	}

	metadata, err := normalizeStoredSourceMetadata(
		version,
		title,
		author,
		language,
		rightsJSON,
		sourceURL,
		createdAt,
	)
	if err != nil {
		return adminSourceSnapshot{}, err
	}
	canonical := adminCanonicalSource{
		Title:      metadata.Title,
		Author:     cloneString(metadata.Author),
		Language:   metadata.Language,
		Rights:     cloneJSONMap(metadata.Rights),
		SourceURL:  cloneString(metadata.SourceURL),
		SourceText: sourceText,
	}
	computed, err := canonicalSourceSnapshotHash(canonical)
	if err != nil {
		return adminSourceSnapshot{}, err
	}
	if computed != snapshotHash {
		return adminSourceSnapshot{}, fmt.Errorf("%w: snapshot hash mismatch", errStoredSourceInvalid)
	}

	return adminSourceSnapshot{
		ID:                  id,
		adminSourceMetadata: metadata,
		SourceText:          sourceText,
		SnapshotHash:        snapshotHash,
	}, nil
}

func inspectAdminStorySource(
	ctx context.Context,
	queryer storedVersionQueryer,
	storyID string,
) (inspectedAdminSource, error) {
	var (
		sourceID        string
		currentVersion  sql.NullString
		sourceUpdatedAt time.Time
	)
	err := queryer.QueryRowContext(ctx, `
		SELECT id, current_version_id, updated_at
		FROM story_sources
		WHERE story_id = $1
	`, storyID).Scan(&sourceID, &currentVersion, &sourceUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return inspectedAdminSource{
			Summary: model.AdminStorySourceSummary{
				Status: model.AdminSourceStatusMissing,
			},
			Versions: []model.AdminSourceVersionSummary{},
		}, nil
	}
	if err != nil {
		return inspectedAdminSource{}, err
	}

	rows, err := queryer.QueryContext(ctx, `
		SELECT id, version, title, author, language, rights::text, source_url, created_at
		FROM story_source_versions
		WHERE source_id = $1
		  AND story_id = $2
		ORDER BY version DESC, id ASC
	`, sourceID, storyID)
	if err != nil {
		return inspectedAdminSource{}, err
	}
	defer rows.Close()

	versions := make([]model.AdminSourceVersionSummary, 0, 4)
	var currentMetadata *adminSourceMetadata
	currentFound := false
	for rows.Next() {
		var (
			versionID  string
			version    int64
			title      string
			author     sql.NullString
			language   string
			rightsJSON string
			sourceURL  sql.NullString
			createdAt  time.Time
		)
		if err := rows.Scan(
			&versionID,
			&version,
			&title,
			&author,
			&language,
			&rightsJSON,
			&sourceURL,
			&createdAt,
		); err != nil {
			return inspectedAdminSource{}, err
		}
		metadata, err := normalizeStoredSourceMetadata(
			version,
			title,
			author,
			language,
			rightsJSON,
			sourceURL,
			createdAt,
		)
		if err != nil {
			return inspectedAdminSource{}, err
		}
		isCurrent := currentVersion.Valid && currentVersion.String == versionID
		versions = append(versions, model.AdminSourceVersionSummary{
			VersionID: versionID,
			Version:   metadata.Version,
			Title:     metadata.Title,
			Author:    cloneString(metadata.Author),
			Language:  metadata.Language,
			Rights:    cloneJSONMap(metadata.Rights),
			SourceURL: cloneString(metadata.SourceURL),
			CreatedAt: metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
			IsCurrent: isCurrent,
		})
		if isCurrent {
			currentFound = true
			value := metadata
			value.Author = cloneString(metadata.Author)
			value.SourceURL = cloneString(metadata.SourceURL)
			value.Rights = cloneJSONMap(metadata.Rights)
			currentMetadata = &value
		}
	}
	if err := rows.Err(); err != nil {
		return inspectedAdminSource{}, err
	}

	updatedAt := sourceUpdatedAt.UTC().Format(time.RFC3339Nano)
	status := model.AdminSourceStatusReady
	var pointer *model.AdminSourceVersionPointerSummary
	if !currentVersion.Valid || !currentFound || len(versions) == 0 || currentMetadata == nil {
		status = model.AdminSourceStatusRepairRequired
	} else {
		pointer = &model.AdminSourceVersionPointerSummary{
			VersionID: currentVersion.String,
			Version:   currentMetadata.Version,
		}
	}

	return inspectedAdminSource{
		SourceID: sourceID,
		Summary: model.AdminStorySourceSummary{
			Status:         status,
			CurrentVersion: pointer,
			VersionCount:   len(versions),
			UpdatedAt:      &updatedAt,
		},
		Current:  currentMetadata,
		Versions: versions,
	}, nil
}

func (s *Store) AdminSourceUpsert(
	accountID string,
	slug string,
	req model.AdminSourceUpsertRequest,
) (model.AdminSourceUpsertResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	if !accountIDRe.MatchString(accountID) {
		return model.AdminSourceUpsertResponse{}, fmt.Errorf("account required")
	}
	if storyingest.ValidateSlug(slug) != nil {
		return model.AdminSourceUpsertResponse{}, &model.AdminValidationError{Issues: []model.AdminValidationIssue{{
			Field: "slug", Code: "invalid", Message: "Use lowercase letters, numbers, and hyphens",
		}}}
	}
	source, err := canonicalAdminSourceInput(req)
	if err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rightsJSON, err := json.Marshal(source.Rights)
	if err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	var storyID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO stories (
			visibility, owner_account_id, slug, title, author, language, rights
		)
		VALUES ('public', NULL, $1,$2,$3,$4,$5::jsonb)
		ON CONFLICT (slug) DO UPDATE SET
			updated_at = stories.updated_at
		WHERE stories.visibility = '`+adminPublicStoryVisibility+`'
		RETURNING id
	`,
		slug,
		source.Title,
		source.Author,
		source.Language,
		string(rightsJSON),
	).Scan(&storyID); errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	} else if err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	var (
		sourceID         string
		currentVersionID sql.NullString
		sourceCreated    bool
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, current_version_id
		FROM story_sources
		WHERE story_id = $1
		FOR UPDATE
	`, storyID).Scan(&sourceID, &currentVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO story_sources (story_id)
			VALUES ($1)
			RETURNING id, current_version_id
		`, storyID).Scan(&sourceID, &currentVersionID); err != nil {
			return model.AdminSourceUpsertResponse{}, err
		}
		sourceCreated = true
	} else if err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	var (
		existingVersionID string
		existingVersion   int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, version
		FROM story_source_versions
		WHERE source_id = $1
		  AND snapshot_hash = $2
		FOR UPDATE
	`, sourceID, source.SnapshotHash).Scan(&existingVersionID, &existingVersion)
	switch {
	case err == nil:
		stored, validationErr := loadAdminSourceVersionSnapshot(
			ctx,
			tx,
			storyID,
			sourceID,
			existingVersionID,
		)
		if errors.Is(validationErr, errStoredSourceInvalid) {
			return model.AdminSourceUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminSourceRepairRequired)
		}
		if validationErr != nil {
			return model.AdminSourceUpsertResponse{}, validationErr
		}
		if stored.SnapshotHash != source.SnapshotHash ||
			stored.Title != source.Title ||
			!sourceStringPointersEqual(stored.Author, source.Author) ||
			stored.Language != source.Language ||
			!jsonDocumentsEqual(mustJSONMap(stored.Rights), mustJSONMap(source.Rights)) ||
			!sourceStringPointersEqual(stored.SourceURL, source.SourceURL) ||
			stored.SourceText != source.SourceText {
			return model.AdminSourceUpsertResponse{}, fmt.Errorf("%w", model.ErrAdminSourceRepairRequired)
		}

		if !currentVersionID.Valid || currentVersionID.String != existingVersionID {
			if _, err := tx.ExecContext(ctx, `
				UPDATE story_sources
				SET current_version_id = $2,
				    updated_at = now()
				WHERE id = $1
			`, sourceID, existingVersionID); err != nil {
				return model.AdminSourceUpsertResponse{}, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE stories SET updated_at = now() WHERE id = $1
			`, storyID); err != nil {
				return model.AdminSourceUpsertResponse{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return model.AdminSourceUpsertResponse{}, err
		}
		return model.AdminSourceUpsertResponse{
			Slug:      slug,
			VersionID: existingVersionID,
			Version:   existingVersion,
			Outcome:   model.AdminSourceOutcomeReused,
		}, nil

	case errors.Is(err, sql.ErrNoRows):
		// continue with one new immutable source revision.
	default:
		return model.AdminSourceUpsertResponse{}, err
	}

	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM story_source_versions
		WHERE source_id = $1
	`, sourceID).Scan(&nextVersion); err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	var versionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO story_source_versions (
			source_id,
			story_id,
			version,
			title,
			author,
			language,
			rights,
			source_url,
			source_text,
			snapshot_hash
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)
		RETURNING id
	`,
		sourceID,
		storyID,
		nextVersion,
		source.Title,
		source.Author,
		source.Language,
		string(rightsJSON),
		source.SourceURL,
		source.SourceText,
		source.SnapshotHash,
	).Scan(&versionID); err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE story_sources
		SET current_version_id = $2,
		    updated_at = now()
		WHERE id = $1
	`, sourceID, versionID); err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE stories SET updated_at = now() WHERE id = $1
	`, storyID); err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.AdminSourceUpsertResponse{}, err
	}
	outcome := model.AdminSourceOutcomeCreatedVersion
	if sourceCreated {
		outcome = model.AdminSourceOutcomeCreatedSource
	}
	return model.AdminSourceUpsertResponse{
		Slug:      slug,
		VersionID: versionID,
		Version:   nextVersion,
		Outcome:   outcome,
	}, nil
}

func sourceStringPointersEqual(left, right *string) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

func mustJSONMap(value map[string]any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func (s *Store) AdminGetSource(
	accountID string,
	slug string,
) (model.AdminSourceDetailResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	if !accountIDRe.MatchString(accountID) || storyingest.ValidateSlug(slug) != nil {
		return model.AdminSourceDetailResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminSourceDetailResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, slug, false)
	if errors.Is(err, model.ErrAdminStoryNotFound) {
		return model.AdminSourceDetailResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}
	if err != nil {
		return model.AdminSourceDetailResponse{}, err
	}
	source, err := inspectAdminStorySource(ctx, tx, story.ID)
	if err != nil {
		return model.AdminSourceDetailResponse{}, err
	}
	if source.Summary.Status == model.AdminSourceStatusMissing {
		return model.AdminSourceDetailResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceDetailResponse{}, err
	}
	return model.AdminSourceDetailResponse{
		Slug:           story.Slug,
		Status:         source.Summary.Status,
		CurrentVersion: source.Summary.CurrentVersion,
		VersionCount:   source.Summary.VersionCount,
		UpdatedAt:      source.Summary.UpdatedAt,
		Versions:       source.Versions,
	}, nil
}

func (s *Store) AdminGetSourceVersion(
	accountID string,
	slug string,
	versionID string,
) (model.AdminSourceVersionResponse, error) {
	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)
	if !accountIDRe.MatchString(accountID) ||
		storyingest.ValidateSlug(slug) != nil ||
		!accountIDRe.MatchString(versionID) {
		return model.AdminSourceVersionResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminSourceVersionResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	story, err := loadAdminStory(ctx, tx, slug, false)
	if errors.Is(err, model.ErrAdminStoryNotFound) {
		return model.AdminSourceVersionResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}
	if err != nil {
		return model.AdminSourceVersionResponse{}, err
	}

	var (
		sourceID       string
		currentVersion sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, current_version_id
		FROM story_sources
		WHERE story_id = $1
	`, story.ID).Scan(&sourceID, &currentVersion); errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceVersionResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	} else if err != nil {
		return model.AdminSourceVersionResponse{}, err
	}

	snapshot, err := loadAdminSourceVersionSnapshot(ctx, tx, story.ID, sourceID, versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceVersionResponse{}, fmt.Errorf("%w", model.ErrAdminSourceNotFound)
	}
	if errors.Is(err, errStoredSourceInvalid) {
		return model.AdminSourceVersionResponse{}, fmt.Errorf("%w", model.ErrAdminSourceRepairRequired)
	}
	if err != nil {
		return model.AdminSourceVersionResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceVersionResponse{}, err
	}

	return model.AdminSourceVersionResponse{
		Slug:       story.Slug,
		VersionID:  snapshot.ID,
		Version:    snapshot.Version,
		Title:      snapshot.Title,
		Author:     cloneString(snapshot.Author),
		Language:   snapshot.Language,
		Rights:     cloneJSONMap(snapshot.Rights),
		SourceURL:  cloneString(snapshot.SourceURL),
		SourceText: snapshot.SourceText,
		CreatedAt:  snapshot.CreatedAt.UTC().Format(time.RFC3339Nano),
		IsCurrent:  currentVersion.Valid && currentVersion.String == snapshot.ID,
	}, nil
}
