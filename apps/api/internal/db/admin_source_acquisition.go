package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceprovider"
)

const (
	maxSourceAcquisitionSourceBytes = 10 << 20
	maxSourceAcquisitionListLimit   = 100
	defaultSourceAcquisitionLimit   = 50
)

var (
	errStoredSourceAcquisitionInvalid = errors.New("stored source acquisition is invalid")
	sourceAcquisitionProviderPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	sourceAcquisitionHashPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type adminSourceAcquisition struct {
	Provider                  string
	ExternalID                string
	Title                     string
	Contributors              []model.AdminSourceAcquisitionContributor
	Languages                 []string
	LandingURL                string
	ProviderRights            *string
	RepresentationLabel       *string
	RepresentationMediaType   string
	RepresentationProviderURL string
	RepresentationSizeBytes   *int64
	NormalisationVersion      string
	RetrievedContentHash      string
	NormalisedContentHash     string
	SourceText                string
	SnapshotHash              string
}

type storedSourceAcquisition struct {
	ID                        string
	Provider                  string
	ExternalID                string
	Title                     string
	ContributorsJSON          string
	LanguagesJSON             string
	LandingURL                string
	ProviderRights            sql.NullString
	RepresentationLabel       sql.NullString
	RepresentationMediaType   string
	RepresentationProviderURL string
	RepresentationSizeBytes   sql.NullInt64
	NormalisationVersion      string
	RetrievedContentHash      string
	NormalisedContentHash     string
	SnapshotHash              string
	CreatedAt                 time.Time
	RightsStatus              string
	RightsNote                sql.NullString
	RightsReviewedAt          sql.NullTime
	EditorialStatus           string
	EditorialNote             sql.NullString
	EditorialReviewedAt       sql.NullTime
	SourceText                string
}

type sourceAcquisitionScanner interface{ Scan(...any) error }

func adminSourceAcquisitionInput(candidate sourceprovider.SourceCandidate) (adminSourceAcquisition, error) {
	issues := make([]model.AdminValidationIssue, 0, 8)
	provider := string(candidate.Provider)
	if !sourceAcquisitionProviderPattern.MatchString(provider) {
		issues = append(issues, model.AdminValidationIssue{Field: "provider", Code: "invalid", Message: "Source provider is invalid"})
	}
	if !validSourceAcquisitionText(candidate.ExternalID, 128) {
		issues = append(issues, model.AdminValidationIssue{Field: "externalId", Code: "invalid", Message: "Source work identifier is invalid"})
	}
	if !validSourceAcquisitionText(candidate.Title, 1000) {
		issues = append(issues, model.AdminValidationIssue{Field: "title", Code: "invalid", Message: "Source title is invalid"})
	}
	if !validSourceAcquisitionText(candidate.LandingURL, 2048) {
		issues = append(issues, model.AdminValidationIssue{Field: "landingUrl", Code: "invalid", Message: "Source landing URL is invalid"})
	}
	if !validSourceAcquisitionText(candidate.SelectedRepresentation.MediaType, 200) || !validSourceAcquisitionText(candidate.SelectedRepresentation.URL, 2048) {
		issues = append(issues, model.AdminValidationIssue{Field: "selectedRepresentation", Code: "invalid", Message: "Selected source representation is invalid"})
	}
	if !validSourceAcquisitionText(candidate.NormalisationVersion, 128) {
		issues = append(issues, model.AdminValidationIssue{Field: "normalisationVersion", Code: "invalid", Message: "Source normalisation version is invalid"})
	}
	if !sourceAcquisitionHashPattern.MatchString(candidate.RetrievedContentHash) || !sourceAcquisitionHashPattern.MatchString(candidate.NormalisedContentHash) {
		issues = append(issues, model.AdminValidationIssue{Field: "hashes", Code: "invalid", Message: "Source provenance hashes are invalid"})
	}
	if !utf8.ValidString(candidate.SourceText) || strings.TrimSpace(candidate.SourceText) == "" || len(candidate.SourceText) > maxSourceAcquisitionSourceBytes {
		issues = append(issues, model.AdminValidationIssue{Field: "sourceText", Code: "invalid", Message: "Source text is invalid"})
	} else if sourceAcquisitionSHA256(candidate.SourceText) != candidate.NormalisedContentHash {
		issues = append(issues, model.AdminValidationIssue{Field: "sourceText", Code: "hash_mismatch", Message: "Source text does not match its normalised hash"})
	}

	contributors := make([]model.AdminSourceAcquisitionContributor, 0, len(candidate.Contributors))
	for _, contributor := range candidate.Contributors {
		if !validSourceAcquisitionText(contributor.Name, 500) || !validSourceAcquisitionText(contributor.Role, 64) {
			issues = append(issues, model.AdminValidationIssue{Field: "contributors", Code: "invalid", Message: "Source contributors are invalid"})
			break
		}
		contributors = append(contributors, model.AdminSourceAcquisitionContributor{Name: contributor.Name, Role: contributor.Role})
	}
	languages := make([]string, 0, len(candidate.Languages))
	for _, language := range candidate.Languages {
		if !validSourceAcquisitionText(language, 64) {
			issues = append(issues, model.AdminValidationIssue{Field: "languages", Code: "invalid", Message: "Source languages are invalid"})
			break
		}
		languages = append(languages, language)
	}

	var providerRights *string
	if candidate.ProviderRights != "" {
		if !validSourceAcquisitionText(candidate.ProviderRights, 1000) {
			issues = append(issues, model.AdminValidationIssue{Field: "providerRights", Code: "invalid", Message: "Provider rights information is invalid"})
		} else {
			providerRights = cloneString(&candidate.ProviderRights)
		}
	}
	var label *string
	if candidate.SelectedRepresentation.Label != "" {
		if !validSourceAcquisitionText(candidate.SelectedRepresentation.Label, 500) {
			issues = append(issues, model.AdminValidationIssue{Field: "selectedRepresentation", Code: "invalid", Message: "Selected source representation is invalid"})
		} else {
			label = cloneString(&candidate.SelectedRepresentation.Label)
		}
	}
	var size *int64
	if candidate.SelectedRepresentation.SizeBytes != 0 {
		if candidate.SelectedRepresentation.SizeBytes < 0 {
			issues = append(issues, model.AdminValidationIssue{Field: "selectedRepresentation", Code: "invalid", Message: "Selected source representation is invalid"})
		} else {
			value := candidate.SelectedRepresentation.SizeBytes
			size = &value
		}
	}
	if len(issues) > 0 {
		return adminSourceAcquisition{}, &model.AdminValidationError{Issues: issues}
	}

	input := adminSourceAcquisition{
		Provider: provider, ExternalID: candidate.ExternalID, Title: candidate.Title, Contributors: contributors, Languages: languages, LandingURL: candidate.LandingURL,
		ProviderRights: providerRights, RepresentationLabel: label, RepresentationMediaType: candidate.SelectedRepresentation.MediaType,
		RepresentationProviderURL: candidate.SelectedRepresentation.URL, RepresentationSizeBytes: size, NormalisationVersion: candidate.NormalisationVersion,
		RetrievedContentHash: candidate.RetrievedContentHash, NormalisedContentHash: candidate.NormalisedContentHash, SourceText: candidate.SourceText,
	}
	var err error
	input.SnapshotHash, err = sourceAcquisitionSnapshotHash(input)
	if err != nil {
		return adminSourceAcquisition{}, err
	}
	return input, nil
}

func validSourceAcquisitionText(value string, maximum int) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) && len(value) > 0 && len(value) <= maximum
}
func sourceAcquisitionSHA256(value string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, value)
	return hex.EncodeToString(hash.Sum(nil))
}

func sourceAcquisitionSnapshotHash(input adminSourceAcquisition) (string, error) {
	payload := struct {
		Provider                  string                                    `json:"provider"`
		ExternalID                string                                    `json:"externalId"`
		Title                     string                                    `json:"title"`
		Contributors              []model.AdminSourceAcquisitionContributor `json:"contributors"`
		Languages                 []string                                  `json:"languages"`
		LandingURL                string                                    `json:"landingUrl"`
		ProviderRights            *string                                   `json:"providerRights"`
		RepresentationLabel       *string                                   `json:"representationLabel"`
		RepresentationMediaType   string                                    `json:"representationMediaType"`
		RepresentationProviderURL string                                    `json:"representationProviderUrl"`
		RepresentationSizeBytes   *int64                                    `json:"representationSizeBytes"`
		NormalisationVersion      string                                    `json:"normalisationVersion"`
		RetrievedContentHash      string                                    `json:"retrievedContentHash"`
		NormalisedContentHash     string                                    `json:"normalisedContentHash"`
		SourceText                string                                    `json:"sourceText"`
	}{
		Provider:                  input.Provider,
		ExternalID:                input.ExternalID,
		Title:                     input.Title,
		Contributors:              input.Contributors,
		Languages:                 input.Languages,
		LandingURL:                input.LandingURL,
		ProviderRights:            input.ProviderRights,
		RepresentationLabel:       input.RepresentationLabel,
		RepresentationMediaType:   input.RepresentationMediaType,
		RepresentationProviderURL: input.RepresentationProviderURL,
		RepresentationSizeBytes:   input.RepresentationSizeBytes,
		NormalisationVersion:      input.NormalisationVersion,
		RetrievedContentHash:      input.RetrievedContentHash,
		NormalisedContentHash:     input.NormalisedContentHash,
		SourceText:                input.SourceText,
	}
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode(payload); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Store) AdminPersistSourceAcquisition(candidate sourceprovider.SourceCandidate) (model.AdminSourceAcquisitionPersistResponse, error) {
	input, err := adminSourceAcquisitionInput(candidate)
	if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	contributorsJSON, err := json.Marshal(input.Contributors)
	if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	languagesJSON, err := json.Marshal(input.Languages)
	if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO source_acquisitions (provider, external_id, title, contributors, languages, landing_url, provider_rights, representation_label, representation_media_type, representation_provider_url, representation_size_bytes, normalisation_version, retrieved_content_hash, normalised_content_hash, source_text, snapshot_hash) VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT (snapshot_hash) DO NOTHING RETURNING id`, input.Provider, input.ExternalID, input.Title, string(contributorsJSON), string(languagesJSON), input.LandingURL, input.ProviderRights, input.RepresentationLabel, input.RepresentationMediaType, input.RepresentationProviderURL, input.RepresentationSizeBytes, input.NormalisationVersion, input.RetrievedContentHash, input.NormalisedContentHash, input.SourceText, input.SnapshotHash).Scan(&id)
	outcome := model.AdminSourceAcquisitionOutcomeCreated
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM source_acquisitions WHERE snapshot_hash = $1`, input.SnapshotHash).Scan(&id); err != nil {
			return model.AdminSourceAcquisitionPersistResponse{}, err
		}
		outcome = model.AdminSourceAcquisitionOutcomeReused
	} else if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_acquisition_reviews (acquisition_id, rights_status, editorial_status) VALUES ($1, 'pending', 'pending') ON CONFLICT (acquisition_id) DO NOTHING`, id); err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	summary, err := loadAdminSourceAcquisitionSummary(ctx, tx, id)
	if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	return model.AdminSourceAcquisitionPersistResponse{Outcome: outcome, Acquisition: summary}, nil
}

func (s *Store) AdminListSourceAcquisitions(limit int) (model.AdminSourceAcquisitionsListResponse, error) {
	if limit == 0 {
		limit = defaultSourceAcquisitionLimit
	}
	if limit < 1 || limit > maxSourceAcquisitionListLimit {
		return model.AdminSourceAcquisitionsListResponse{}, &model.AdminValidationError{Issues: []model.AdminValidationIssue{{Field: "limit", Code: "invalid", Message: "Source acquisition list limit is invalid"}}}
	}
	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminSourceAcquisitionsListResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, sourceAcquisitionSelect(false)+` ORDER BY acquisition.created_at DESC, acquisition.id DESC LIMIT $1`, limit)
	if err != nil {
		return model.AdminSourceAcquisitionsListResponse{}, err
	}
	defer rows.Close()
	items := make([]model.AdminSourceAcquisitionSummary, 0, limit)
	for rows.Next() {
		stored, err := scanStoredSourceAcquisition(rows, false)
		if err != nil {
			return model.AdminSourceAcquisitionsListResponse{}, err
		}
		summary, err := stored.sourceSummary()
		if err != nil {
			return model.AdminSourceAcquisitionsListResponse{}, err
		}
		items = append(items, summary)
	}
	if err := rows.Err(); err != nil {
		return model.AdminSourceAcquisitionsListResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceAcquisitionsListResponse{}, err
	}
	return model.AdminSourceAcquisitionsListResponse{Items: items}, nil
}

func (s *Store) AdminGetSourceAcquisition(id string) (model.AdminSourceAcquisitionDetail, error) {
	id, ok := canonicalSourceAcquisitionID(id)
	if !ok {
		return model.AdminSourceAcquisitionDetail{}, model.ErrAdminSourceAcquisitionNotFound
	}
	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.AdminSourceAcquisitionDetail{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := loadAdminSourceAcquisitionDetail(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceAcquisitionDetail{}, model.ErrAdminSourceAcquisitionNotFound
	}
	if err != nil {
		return model.AdminSourceAcquisitionDetail{}, err
	}
	detail, err := stored.detail()
	if err != nil {
		return model.AdminSourceAcquisitionDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceAcquisitionDetail{}, err
	}
	return detail, nil
}

func (s *Store) AdminUpdateSourceAcquisitionRightsReview(id string, req model.AdminSourceAcquisitionReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error) {
	return s.adminUpdateSourceAcquisitionReview(id, req, "rights")
}
func (s *Store) AdminUpdateSourceAcquisitionEditorialReview(id string, req model.AdminSourceAcquisitionReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error) {
	return s.adminUpdateSourceAcquisitionReview(id, req, "editorial")
}

func (s *Store) adminUpdateSourceAcquisitionReview(id string, req model.AdminSourceAcquisitionReviewUpdateRequest, dimension string) (model.AdminSourceAcquisitionSummary, error) {
	id, ok := canonicalSourceAcquisitionID(id)
	if !ok {
		return model.AdminSourceAcquisitionSummary{}, model.ErrAdminSourceAcquisitionNotFound
	}
	status, note, err := canonicalSourceAcquisitionReview(req)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var update string
	switch dimension {
	case "rights":
		update = `UPDATE source_acquisition_reviews SET rights_status = $2, rights_note = $3, rights_reviewed_at = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END WHERE acquisition_id = $1`
	case "editorial":
		update = `UPDATE source_acquisition_reviews SET editorial_status = $2, editorial_note = $3, editorial_reviewed_at = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END WHERE acquisition_id = $1`
	default:
		return model.AdminSourceAcquisitionSummary{}, fmt.Errorf("unsupported review dimension")
	}
	result, err := tx.ExecContext(ctx, update, id, status, note)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	if changed == 0 {
		return model.AdminSourceAcquisitionSummary{}, model.ErrAdminSourceAcquisitionNotFound
	}
	summary, err := loadAdminSourceAcquisitionSummary(ctx, tx, id)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	return summary, nil
}

func canonicalSourceAcquisitionID(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if !accountIDRe.MatchString(value) {
		return "", false
	}
	return strings.ToLower(value), true
}
func canonicalSourceAcquisitionReview(req model.AdminSourceAcquisitionReviewUpdateRequest) (model.AdminSourceAcquisitionReviewStatus, *string, error) {
	status, note := req.Status, strings.TrimSpace(req.Note)
	issues := make([]model.AdminValidationIssue, 0, 2)
	switch status {
	case model.AdminSourceAcquisitionReviewPending:
		if note != "" {
			issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "invalid", Message: "Pending review cannot include a note"})
		}
	case model.AdminSourceAcquisitionReviewApproved, model.AdminSourceAcquisitionReviewRejected:
		if !utf8.ValidString(req.Note) || note == "" || len(note) > 4000 {
			issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "required", Message: "Provide a review rationale"})
		}
	default:
		issues = append(issues, model.AdminValidationIssue{Field: "status", Code: "invalid", Message: "Review status is invalid"})
	}
	if !utf8.ValidString(req.Note) {
		issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "invalid_encoding", Message: "Review note must be valid text"})
	}
	if len(issues) > 0 {
		return "", nil, &model.AdminValidationError{Issues: issues}
	}
	if status == model.AdminSourceAcquisitionReviewPending {
		return status, nil, nil
	}
	return status, &note, nil
}

const sourceAcquisitionColumns = `acquisition.id, acquisition.provider, acquisition.external_id, acquisition.title, acquisition.contributors::text, acquisition.languages::text, acquisition.landing_url, acquisition.provider_rights, acquisition.representation_label, acquisition.representation_media_type, acquisition.representation_provider_url, acquisition.representation_size_bytes, acquisition.normalisation_version, acquisition.retrieved_content_hash, acquisition.normalised_content_hash, acquisition.snapshot_hash, acquisition.created_at, review.rights_status, review.rights_note, review.rights_reviewed_at, review.editorial_status, review.editorial_note, review.editorial_reviewed_at`
const sourceAcquisitionFrom = ` FROM source_acquisitions AS acquisition JOIN source_acquisition_reviews AS review ON review.acquisition_id = acquisition.id`

func sourceAcquisitionSelect(includeSourceText bool) string {
	columns := sourceAcquisitionColumns
	if includeSourceText {
		columns += `, acquisition.source_text`
	}
	return `SELECT ` + columns + sourceAcquisitionFrom
}

func loadAdminSourceAcquisitionSummary(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (model.AdminSourceAcquisitionSummary, error) {
	stored, err := scanStoredSourceAcquisition(queryer.QueryRowContext(ctx, sourceAcquisitionSelect(false)+` WHERE acquisition.id = $1`, id), false)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	return stored.sourceSummary()
}
func loadAdminSourceAcquisitionDetail(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (storedSourceAcquisition, error) {
	return scanStoredSourceAcquisition(queryer.QueryRowContext(ctx, sourceAcquisitionSelect(true)+` WHERE acquisition.id = $1`, id), true)
}

func scanStoredSourceAcquisition(scanner sourceAcquisitionScanner, includeSourceText bool) (storedSourceAcquisition, error) {
	var stored storedSourceAcquisition
	arguments := []any{&stored.ID, &stored.Provider, &stored.ExternalID, &stored.Title, &stored.ContributorsJSON, &stored.LanguagesJSON, &stored.LandingURL, &stored.ProviderRights, &stored.RepresentationLabel, &stored.RepresentationMediaType, &stored.RepresentationProviderURL, &stored.RepresentationSizeBytes, &stored.NormalisationVersion, &stored.RetrievedContentHash, &stored.NormalisedContentHash, &stored.SnapshotHash, &stored.CreatedAt, &stored.RightsStatus, &stored.RightsNote, &stored.RightsReviewedAt, &stored.EditorialStatus, &stored.EditorialNote, &stored.EditorialReviewedAt}
	if includeSourceText {
		arguments = append(arguments, &stored.SourceText)
	}
	if err := scanner.Scan(arguments...); err != nil {
		return storedSourceAcquisition{}, err
	}
	return stored, nil
}

func (stored storedSourceAcquisition) sourceSummary() (model.AdminSourceAcquisitionSummary, error) {
	if !sourceAcquisitionProviderPattern.MatchString(stored.Provider) || !validSourceAcquisitionText(stored.ExternalID, 128) || !validSourceAcquisitionText(stored.Title, 1000) || !validSourceAcquisitionText(stored.LandingURL, 2048) || !validSourceAcquisitionText(stored.RepresentationMediaType, 200) || !validSourceAcquisitionText(stored.RepresentationProviderURL, 2048) || !validSourceAcquisitionText(stored.NormalisationVersion, 128) || !sourceAcquisitionHashPattern.MatchString(stored.RetrievedContentHash) || !sourceAcquisitionHashPattern.MatchString(stored.NormalisedContentHash) || !sourceAcquisitionHashPattern.MatchString(stored.SnapshotHash) {
		return model.AdminSourceAcquisitionSummary{}, errStoredSourceAcquisitionInvalid
	}
	contributors, err := decodeSourceAcquisitionContributors(stored.ContributorsJSON)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	languages, err := decodeSourceAcquisitionLanguages(stored.LanguagesJSON)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	providerRights, err := nullableSourceAcquisitionText(stored.ProviderRights, 1000)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	label, err := nullableSourceAcquisitionText(stored.RepresentationLabel, 500)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	var size *int64
	if stored.RepresentationSizeBytes.Valid {
		if stored.RepresentationSizeBytes.Int64 <= 0 {
			return model.AdminSourceAcquisitionSummary{}, errStoredSourceAcquisitionInvalid
		}
		value := stored.RepresentationSizeBytes.Int64
		size = &value
	}
	rights, err := sourceAcquisitionReviewDimension(stored.RightsStatus, stored.RightsNote, stored.RightsReviewedAt)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	editorial, err := sourceAcquisitionReviewDimension(stored.EditorialStatus, stored.EditorialNote, stored.EditorialReviewedAt)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	return model.AdminSourceAcquisitionSummary{ID: stored.ID, Provider: stored.Provider, ExternalID: stored.ExternalID, Title: stored.Title, Contributors: contributors, Languages: languages, LandingURL: stored.LandingURL, ProviderRights: providerRights, SelectedRepresentation: model.AdminSourceAcquisitionRepresentation{Label: label, MediaType: stored.RepresentationMediaType, ProviderURL: stored.RepresentationProviderURL, SizeBytes: size}, NormalisationVersion: stored.NormalisationVersion, RetrievedContentHash: stored.RetrievedContentHash, NormalisedContentHash: stored.NormalisedContentHash, SnapshotHash: stored.SnapshotHash, CreatedAt: stored.CreatedAt.UTC().Format(time.RFC3339Nano), Review: model.AdminSourceAcquisitionReview{Rights: rights, Editorial: editorial}}, nil
}

func (stored storedSourceAcquisition) detail() (model.AdminSourceAcquisitionDetail, error) {
	summary, err := stored.sourceSummary()
	if err != nil {
		return model.AdminSourceAcquisitionDetail{}, err
	}
	if !utf8.ValidString(stored.SourceText) || strings.TrimSpace(stored.SourceText) == "" || len(stored.SourceText) > maxSourceAcquisitionSourceBytes || sourceAcquisitionSHA256(stored.SourceText) != summary.NormalisedContentHash {
		return model.AdminSourceAcquisitionDetail{}, errStoredSourceAcquisitionInvalid
	}
	snapshotHash, err := sourceAcquisitionSnapshotHash(adminSourceAcquisition{
		Provider:                  summary.Provider,
		ExternalID:                summary.ExternalID,
		Title:                     summary.Title,
		Contributors:              summary.Contributors,
		Languages:                 summary.Languages,
		LandingURL:                summary.LandingURL,
		ProviderRights:            summary.ProviderRights,
		RepresentationLabel:       summary.SelectedRepresentation.Label,
		RepresentationMediaType:   summary.SelectedRepresentation.MediaType,
		RepresentationProviderURL: summary.SelectedRepresentation.ProviderURL,
		RepresentationSizeBytes:   summary.SelectedRepresentation.SizeBytes,
		NormalisationVersion:      summary.NormalisationVersion,
		RetrievedContentHash:      summary.RetrievedContentHash,
		NormalisedContentHash:     summary.NormalisedContentHash,
		SourceText:                stored.SourceText,
	})
	if err != nil || snapshotHash != summary.SnapshotHash {
		return model.AdminSourceAcquisitionDetail{}, errStoredSourceAcquisitionInvalid
	}
	return model.AdminSourceAcquisitionDetail{AdminSourceAcquisitionSummary: summary, SourceText: stored.SourceText}, nil
}
func decodeSourceAcquisitionContributors(raw string) ([]model.AdminSourceAcquisitionContributor, error) {
	var contributors []model.AdminSourceAcquisitionContributor
	if err := json.Unmarshal([]byte(raw), &contributors); err != nil || contributors == nil {
		return nil, errStoredSourceAcquisitionInvalid
	}
	for _, contributor := range contributors {
		if !validSourceAcquisitionText(contributor.Name, 500) || !validSourceAcquisitionText(contributor.Role, 64) {
			return nil, errStoredSourceAcquisitionInvalid
		}
	}
	return contributors, nil
}
func decodeSourceAcquisitionLanguages(raw string) ([]string, error) {
	var languages []string
	if err := json.Unmarshal([]byte(raw), &languages); err != nil || languages == nil {
		return nil, errStoredSourceAcquisitionInvalid
	}
	for _, language := range languages {
		if !validSourceAcquisitionText(language, 64) {
			return nil, errStoredSourceAcquisitionInvalid
		}
	}
	return languages, nil
}
func nullableSourceAcquisitionText(value sql.NullString, maximum int) (*string, error) {
	if !value.Valid {
		return nil, nil
	}
	if !validSourceAcquisitionText(value.String, maximum) {
		return nil, errStoredSourceAcquisitionInvalid
	}
	result := value.String
	return &result, nil
}
func sourceAcquisitionReviewDimension(status string, note sql.NullString, reviewedAt sql.NullTime) (model.AdminSourceAcquisitionReviewDimension, error) {
	parsed := model.AdminSourceAcquisitionReviewStatus(status)
	switch parsed {
	case model.AdminSourceAcquisitionReviewPending:
		if note.Valid || reviewedAt.Valid {
			return model.AdminSourceAcquisitionReviewDimension{}, errStoredSourceAcquisitionInvalid
		}
		return model.AdminSourceAcquisitionReviewDimension{Status: parsed}, nil
	case model.AdminSourceAcquisitionReviewApproved, model.AdminSourceAcquisitionReviewRejected:
		if !note.Valid || !validSourceAcquisitionText(note.String, 4000) || !reviewedAt.Valid {
			return model.AdminSourceAcquisitionReviewDimension{}, errStoredSourceAcquisitionInvalid
		}
		noteValue := note.String
		timestamp := reviewedAt.Time.UTC().Format(time.RFC3339Nano)
		return model.AdminSourceAcquisitionReviewDimension{Status: parsed, Note: &noteValue, ReviewedAt: &timestamp}, nil
	default:
		return model.AdminSourceAcquisitionReviewDimension{}, errStoredSourceAcquisitionInvalid
	}
}
