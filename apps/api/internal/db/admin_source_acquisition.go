package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceeligibility"
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
	QualityStatus             string
	QualityNote               sql.NullString
	QualityReviewedAt         sql.NullTime
	AssessmentHash            sql.NullString
	AssessmentPolicyVersion   sql.NullString
	AssessmentEvaluationDate  sql.NullTime
	AssessmentEvaluatedAt     sql.NullTime
	AssessmentUSStatus        sql.NullString
	AssessmentUSReason        sql.NullString
	AssessmentOPDSRights      sql.NullString
	AssessmentRDFRights       sql.NullString
	AssessmentHeaderRights    sql.NullString
	AssessmentUKStatus        sql.NullString
	AssessmentUKReason        sql.NullString
	AssessmentOverallStatus   sql.NullString
	AssessmentOverallReason   sql.NullString
	AssessmentEffectiveUKJSON sql.NullString
	AssessmentProviderJSON    sql.NullString
	PromotionStoryID          sql.NullString
	PromotionStorySlug        sql.NullString
	PromotionStoryTitle       sql.NullString
	PromotionVersionID        sql.NullString
	PromotionVersion          sql.NullInt64
	PromotionCreatedAt        sql.NullTime
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

// AdminPersistEligibleSourceAcquisition writes an immutable candidate and its
// successful immutable assessment in one transaction. Blocked evaluations
// never call this method and therefore cannot create partial state.
func (s *Store) AdminPersistEligibleSourceAcquisition(evaluation sourceeligibility.Evaluation) (model.AdminSourceAcquisitionPersistResponse, error) {
	if evaluation.Assessment.Overall != copyrighteligibility.OverallEligible {
		return model.AdminSourceAcquisitionPersistResponse{}, &model.AdminValidationError{Issues: []model.AdminValidationIssue{{Field: "eligibility", Code: "blocked", Message: "Copyright eligibility is blocked"}}}
	}
	input, err := adminSourceAcquisitionInput(evaluation.Candidate)
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
		stored, err := loadAdminSourceAcquisitionDetailBySnapshotHash(ctx, tx, input.SnapshotHash)
		if err != nil {
			return model.AdminSourceAcquisitionPersistResponse{}, err
		}
		detail, err := stored.detail()
		if err != nil {
			return model.AdminSourceAcquisitionPersistResponse{}, err
		}
		if !sourceAcquisitionDetailMatchesInput(detail, input) {
			return model.AdminSourceAcquisitionPersistResponse{}, errStoredSourceAcquisitionInvalid
		}
		id = detail.ID
		outcome = model.AdminSourceAcquisitionOutcomeReused
	} else if err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_acquisition_quality_reviews (acquisition_id, status) VALUES ($1, 'pending') ON CONFLICT (acquisition_id) DO NOTHING`, id); err != nil {
		return model.AdminSourceAcquisitionPersistResponse{}, err
	}
	if err := persistSourceAcquisitionEligibilityAssessment(ctx, tx, id, input, evaluation); err != nil {
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

func (s *Store) AdminUpdateSourceAcquisitionSourceQualityReview(id string, req model.AdminSourceQualityReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error) {
	id, ok := canonicalSourceAcquisitionID(id)
	if !ok {
		return model.AdminSourceAcquisitionSummary{}, model.ErrAdminSourceAcquisitionNotFound
	}
	status, note, err := canonicalSourceAcquisitionQualityReview(req)
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
	if _, err := lockSourceAcquisitionQualityReview(ctx, tx, id); err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	var promoted bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM story_source_versions WHERE source_acquisition_id = $1)`, id).Scan(&promoted); err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	if promoted {
		return model.AdminSourceAcquisitionSummary{}, model.ErrAdminSourceAcquisitionAlreadyPromoted
	}
	result, err := tx.ExecContext(ctx, `UPDATE source_acquisition_quality_reviews SET status = $2, note = $3, reviewed_at = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END WHERE acquisition_id = $1`, id, status, note)
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

// lockSourceAcquisitionQualityReview serializes the two lifecycle mutations
// that may change the ready state: a source-quality decision and promotion.
// The acquisition is immutable evidence and must never be locked FOR UPDATE by
// the runtime role.
func lockSourceAcquisitionQualityReview(ctx context.Context, tx *sql.Tx, acquisitionID string) (model.AdminSourceQualityStatus, error) {
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM source_acquisition_quality_reviews
		WHERE acquisition_id = $1
		FOR UPDATE
	`, acquisitionID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return "", model.ErrAdminSourceAcquisitionNotFound
	} else if err != nil {
		return "", err
	}
	return model.AdminSourceQualityStatus(status), nil
}

func canonicalSourceAcquisitionID(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if !accountIDRe.MatchString(value) {
		return "", false
	}
	return strings.ToLower(value), true
}
func canonicalSourceAcquisitionQualityReview(req model.AdminSourceQualityReviewUpdateRequest) (model.AdminSourceQualityStatus, *string, error) {
	status, note := req.Status, strings.TrimSpace(req.Note)
	issues := make([]model.AdminValidationIssue, 0, 2)
	switch status {
	case model.AdminSourceQualityPending:
		if note != "" {
			issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "invalid", Message: "Pending source quality review cannot include a note"})
		}
	case model.AdminSourceQualityApproved, model.AdminSourceQualityRejected:
		if !utf8.ValidString(req.Note) || note == "" || len(note) > 4000 {
			issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "required", Message: "Provide a source quality rationale"})
		}
	default:
		issues = append(issues, model.AdminValidationIssue{Field: "status", Code: "invalid", Message: "Source quality status is invalid"})
	}
	if !utf8.ValidString(req.Note) {
		issues = append(issues, model.AdminValidationIssue{Field: "note", Code: "invalid_encoding", Message: "Source quality note must be valid text"})
	}
	if len(issues) > 0 {
		return "", nil, &model.AdminValidationError{Issues: issues}
	}
	if status == model.AdminSourceQualityPending {
		return status, nil, nil
	}
	return status, &note, nil
}

const sourceAcquisitionColumns = `acquisition.id, acquisition.provider, acquisition.external_id, acquisition.title, acquisition.contributors::text, acquisition.languages::text, acquisition.landing_url, acquisition.provider_rights, acquisition.representation_label, acquisition.representation_media_type, acquisition.representation_provider_url, acquisition.representation_size_bytes, acquisition.normalisation_version, acquisition.retrieved_content_hash, acquisition.normalised_content_hash, acquisition.snapshot_hash, acquisition.created_at, quality.status, quality.note, quality.reviewed_at, assessment.assessment_hash, assessment.policy_version, assessment.evaluation_date, assessment.evaluated_at, assessment.us_status, assessment.us_reason, assessment.opds_rights, assessment.rdf_rights, assessment.header_rights, assessment.uk_status, assessment.uk_reason, assessment.overall_status, assessment.overall_reason, assessment.effective_uk_evidence::text, assessment.provider_evidence::text, promotion.story_id, promotion.story_slug, promotion.story_title, promotion.version_id, promotion.version, promotion.created_at`

func sourceAcquisitionFrom() string {
	return ` FROM source_acquisitions AS acquisition
JOIN source_acquisition_quality_reviews AS quality ON quality.acquisition_id = acquisition.id
LEFT JOIN LATERAL (
	SELECT eligibility.assessment_hash, eligibility.policy_version, eligibility.evaluation_date, eligibility.evaluated_at,
		eligibility.us_status, eligibility.us_reason, eligibility.opds_rights, eligibility.rdf_rights, eligibility.header_rights,
		eligibility.uk_status, eligibility.uk_reason, eligibility.overall_status, eligibility.overall_reason,
		eligibility.effective_uk_evidence, eligibility.provider_evidence
	FROM source_acquisition_eligibility_assessments AS eligibility
	WHERE eligibility.acquisition_id = acquisition.id
		AND eligibility.policy_version = '` + copyrighteligibility.PolicyVersion + `'
		AND eligibility.overall_status = 'eligible'
	ORDER BY eligibility.evaluation_date DESC, eligibility.evaluated_at DESC, eligibility.id DESC
	LIMIT 1
) AS assessment ON true
LEFT JOIN LATERAL (
	SELECT version.story_id, story.slug AS story_slug, story.title AS story_title, version.id AS version_id, version.version, version.created_at
	FROM story_source_versions AS version
	JOIN stories AS story ON story.id = version.story_id
	WHERE version.source_acquisition_id = acquisition.id
	LIMIT 1
) AS promotion ON true`
}

func sourceAcquisitionSelect(includeSourceText bool) string {
	columns := sourceAcquisitionColumns
	if includeSourceText {
		columns += `, acquisition.source_text`
	}
	return `SELECT ` + columns + sourceAcquisitionFrom()
}

func sourceAcquisitionDetailMatchesInput(detail model.AdminSourceAcquisitionDetail, input adminSourceAcquisition) bool {
	summary := detail.AdminSourceAcquisitionSummary
	if summary.ID == "" ||
		summary.Provider != input.Provider ||
		summary.ExternalID != input.ExternalID ||
		summary.Title != input.Title ||
		summary.LandingURL != input.LandingURL ||
		summary.SelectedRepresentation.MediaType != input.RepresentationMediaType ||
		summary.SelectedRepresentation.ProviderURL != input.RepresentationProviderURL ||
		summary.NormalisationVersion != input.NormalisationVersion ||
		summary.RetrievedContentHash != input.RetrievedContentHash ||
		summary.NormalisedContentHash != input.NormalisedContentHash ||
		summary.SnapshotHash != input.SnapshotHash ||
		detail.SourceText != input.SourceText ||
		!sameSourceAcquisitionOptionalString(summary.ProviderRights, input.ProviderRights) ||
		!sameSourceAcquisitionOptionalString(summary.SelectedRepresentation.Label, input.RepresentationLabel) ||
		!sameSourceAcquisitionOptionalInt64(summary.SelectedRepresentation.SizeBytes, input.RepresentationSizeBytes) ||
		len(summary.Contributors) != len(input.Contributors) ||
		len(summary.Languages) != len(input.Languages) {
		return false
	}
	for index, contributor := range input.Contributors {
		if summary.Contributors[index] != contributor {
			return false
		}
	}
	for index, language := range input.Languages {
		if summary.Languages[index] != language {
			return false
		}
	}
	return true
}

func sameSourceAcquisitionOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameSourceAcquisitionOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
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

func loadAdminSourceAcquisitionDetailBySnapshotHash(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, snapshotHash string) (storedSourceAcquisition, error) {
	return scanStoredSourceAcquisition(queryer.QueryRowContext(ctx, sourceAcquisitionSelect(true)+` WHERE acquisition.snapshot_hash = $1`, snapshotHash), true)
}

func scanStoredSourceAcquisition(scanner sourceAcquisitionScanner, includeSourceText bool) (storedSourceAcquisition, error) {
	var stored storedSourceAcquisition
	arguments := []any{&stored.ID, &stored.Provider, &stored.ExternalID, &stored.Title, &stored.ContributorsJSON, &stored.LanguagesJSON, &stored.LandingURL, &stored.ProviderRights, &stored.RepresentationLabel, &stored.RepresentationMediaType, &stored.RepresentationProviderURL, &stored.RepresentationSizeBytes, &stored.NormalisationVersion, &stored.RetrievedContentHash, &stored.NormalisedContentHash, &stored.SnapshotHash, &stored.CreatedAt, &stored.QualityStatus, &stored.QualityNote, &stored.QualityReviewedAt, &stored.AssessmentHash, &stored.AssessmentPolicyVersion, &stored.AssessmentEvaluationDate, &stored.AssessmentEvaluatedAt, &stored.AssessmentUSStatus, &stored.AssessmentUSReason, &stored.AssessmentOPDSRights, &stored.AssessmentRDFRights, &stored.AssessmentHeaderRights, &stored.AssessmentUKStatus, &stored.AssessmentUKReason, &stored.AssessmentOverallStatus, &stored.AssessmentOverallReason, &stored.AssessmentEffectiveUKJSON, &stored.AssessmentProviderJSON, &stored.PromotionStoryID, &stored.PromotionStorySlug, &stored.PromotionStoryTitle, &stored.PromotionVersionID, &stored.PromotionVersion, &stored.PromotionCreatedAt}
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
	quality, err := sourceAcquisitionQualityReview(stored.QualityStatus, stored.QualityNote, stored.QualityReviewedAt)
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	eligibility, err := stored.eligibility()
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	promotion, err := stored.promotion()
	if err != nil {
		return model.AdminSourceAcquisitionSummary{}, err
	}
	return model.AdminSourceAcquisitionSummary{ID: stored.ID, Provider: stored.Provider, ExternalID: stored.ExternalID, Title: stored.Title, Contributors: contributors, Languages: languages, LandingURL: stored.LandingURL, ProviderRights: providerRights, SelectedRepresentation: model.AdminSourceAcquisitionRepresentation{Label: label, MediaType: stored.RepresentationMediaType, ProviderURL: stored.RepresentationProviderURL, SizeBytes: size}, NormalisationVersion: stored.NormalisationVersion, RetrievedContentHash: stored.RetrievedContentHash, NormalisedContentHash: stored.NormalisedContentHash, SnapshotHash: stored.SnapshotHash, CreatedAt: stored.CreatedAt.UTC().Format(time.RFC3339Nano), Eligibility: eligibility, SourceQuality: quality, Promotion: promotion}, nil
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
func sourceAcquisitionQualityReview(status string, note sql.NullString, reviewedAt sql.NullTime) (model.AdminSourceQualityReview, error) {
	parsed := model.AdminSourceQualityStatus(status)
	switch parsed {
	case model.AdminSourceQualityPending:
		if note.Valid || reviewedAt.Valid {
			return model.AdminSourceQualityReview{}, errStoredSourceAcquisitionInvalid
		}
		return model.AdminSourceQualityReview{Status: parsed}, nil
	case model.AdminSourceQualityApproved, model.AdminSourceQualityRejected:
		if !note.Valid || !validSourceAcquisitionText(note.String, 4000) || !reviewedAt.Valid {
			return model.AdminSourceQualityReview{}, errStoredSourceAcquisitionInvalid
		}
		noteValue := note.String
		timestamp := reviewedAt.Time.UTC().Format(time.RFC3339Nano)
		return model.AdminSourceQualityReview{Status: parsed, Note: &noteValue, ReviewedAt: &timestamp}, nil
	default:
		return model.AdminSourceQualityReview{}, errStoredSourceAcquisitionInvalid
	}
}

type sourceEligibilityProviderSnapshot struct {
	ProviderTitle string                                    `json:"providerTitle"`
	Contributors  []model.AdminCopyrightContributorEvidence `json:"contributors"`
	RDFDigest     string                                    `json:"rdfDigest"`
}

type sourceEligibilityAssessmentInput struct {
	AcquisitionID string
	SnapshotHash  string
	Provider      string
	ExternalID    string
	Eligibility   model.AdminSourceEligibility
	Hash          string
}

func sourceEligibilityAssessmentInputFor(acquisitionID string, acquisition adminSourceAcquisition, evaluation sourceeligibility.Evaluation) (sourceEligibilityAssessmentInput, error) {
	if evaluation.Candidate.Provider != sourceprovider.ID(acquisition.Provider) || evaluation.Candidate.ExternalID != acquisition.ExternalID ||
		evaluation.ProviderEvidence.Provider != acquisition.Provider || evaluation.ProviderEvidence.ExternalID != acquisition.ExternalID ||
		strings.TrimSpace(evaluation.EffectiveUKEvidence.WorkTitle) != strings.TrimSpace(evaluation.ProviderEvidence.Title) ||
		evaluation.Assessment.PolicyVersion != copyrighteligibility.PolicyVersion ||
		evaluation.Assessment.Overall != copyrighteligibility.OverallEligible ||
		evaluation.Assessment.US.Status != copyrighteligibility.JurisdictionEligible ||
		evaluation.Assessment.UK.Status != copyrighteligibility.JurisdictionEligible {
		return sourceEligibilityAssessmentInput{}, errStoredSourceAcquisitionInvalid
	}
	providerEvidence := sourceEligibilityProviderSnapshot{
		ProviderTitle: evaluation.ProviderEvidence.Title,
		RDFDigest:     evaluation.ProviderEvidence.EvidenceDigest,
		Contributors:  make([]model.AdminCopyrightContributorEvidence, 0, len(evaluation.ProviderEvidence.Contributors)),
	}
	for _, contributor := range evaluation.ProviderEvidence.Contributors {
		providerEvidence.Contributors = append(providerEvidence.Contributors, model.AdminCopyrightContributorEvidence{
			Name: contributor.Name, Role: contributor.Role, BirthYear: contributor.BirthYear, DeathYear: contributor.DeathYear,
		})
	}
	eligibility := model.AdminSourceEligibility{
		PolicyVersion:  evaluation.Assessment.PolicyVersion,
		EvaluationDate: evaluation.EvaluationDate.UTC().Format("2006-01-02"),
		EvaluatedAt:    evaluation.EvaluatedAt.UTC().Format(time.RFC3339Nano),
		US:             model.AdminCopyrightJurisdiction{Status: string(evaluation.Assessment.US.Status), Reason: string(evaluation.Assessment.US.Reason)},
		UK:             model.AdminCopyrightJurisdiction{Status: string(evaluation.Assessment.UK.Status), Reason: string(evaluation.Assessment.UK.Reason)},
		Overall:        string(evaluation.Assessment.Overall),
		OverallReason:  string(evaluation.Assessment.OverallReason),
		OPDSRights:     string(evaluation.OPDSRights),
		RDFRights:      string(evaluation.ProviderEvidence.Rights),
		HeaderRights:   string(evaluation.HeaderRights),
		ProviderTitle:  providerEvidence.ProviderTitle,
		Contributors:   providerEvidence.Contributors,
		RDFDigest:      providerEvidence.RDFDigest,
		EffectiveUK:    modelEffectiveUKEvidence(evaluation.EffectiveUKEvidence),
	}
	if !validateSourceEligibility(eligibility) {
		return sourceEligibilityAssessmentInput{}, errStoredSourceAcquisitionInvalid
	}
	hash, err := sourceEligibilityAssessmentHash(acquisition.SnapshotHash, acquisition.Provider, acquisition.ExternalID, eligibility)
	if err != nil {
		return sourceEligibilityAssessmentInput{}, err
	}
	return sourceEligibilityAssessmentInput{AcquisitionID: acquisitionID, SnapshotHash: acquisition.SnapshotHash, Provider: acquisition.Provider, ExternalID: acquisition.ExternalID, Eligibility: eligibility, Hash: hash}, nil
}

// sourceEligibilityAssessmentHash identifies immutable assessment evidence.
// evaluated_at records when the server performed the check, but is not part of
// its semantic identity: an identical same-day policy evaluation must reuse
// the stored evidence rather than creating timestamp-only duplicates.
func sourceEligibilityAssessmentHash(snapshotHash, provider, externalID string, eligibility model.AdminSourceEligibility) (string, error) {
	eligibility.EvaluatedAt = ""
	eligibility.AssessmentHash = nil
	payload := struct {
		AcquisitionSnapshotHash string                       `json:"acquisitionSnapshotHash"`
		Provider                string                       `json:"provider"`
		ExternalID              string                       `json:"externalId"`
		Eligibility             model.AdminSourceEligibility `json:"eligibility"`
	}{AcquisitionSnapshotHash: snapshotHash, Provider: provider, ExternalID: externalID, Eligibility: eligibility}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func persistSourceAcquisitionEligibilityAssessment(ctx context.Context, tx *sql.Tx, acquisitionID string, acquisition adminSourceAcquisition, evaluation sourceeligibility.Evaluation) error {
	input, err := sourceEligibilityAssessmentInputFor(acquisitionID, acquisition, evaluation)
	if err != nil {
		return err
	}
	effectiveJSON, err := json.Marshal(input.Eligibility.EffectiveUK)
	if err != nil {
		return err
	}
	providerJSON, err := json.Marshal(sourceEligibilityProviderSnapshot{ProviderTitle: input.Eligibility.ProviderTitle, Contributors: input.Eligibility.Contributors, RDFDigest: input.Eligibility.RDFDigest})
	if err != nil {
		return err
	}
	var persistedID string
	err = tx.QueryRowContext(ctx, `INSERT INTO source_acquisition_eligibility_assessments (acquisition_id, acquisition_snapshot_hash, provider, external_id, policy_version, evaluation_date, evaluated_at, us_status, us_reason, opds_rights, rdf_rights, header_rights, uk_status, uk_reason, effective_uk_evidence, provider_evidence, overall_status, overall_reason, assessment_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16::jsonb,$17,$18,$19) ON CONFLICT (assessment_hash) DO NOTHING RETURNING id`, input.AcquisitionID, input.SnapshotHash, input.Provider, input.ExternalID, input.Eligibility.PolicyVersion, input.Eligibility.EvaluationDate, input.Eligibility.EvaluatedAt, input.Eligibility.US.Status, input.Eligibility.US.Reason, input.Eligibility.OPDSRights, input.Eligibility.RDFRights, input.Eligibility.HeaderRights, input.Eligibility.UK.Status, input.Eligibility.UK.Reason, string(effectiveJSON), string(providerJSON), input.Eligibility.Overall, input.Eligibility.OverallReason, input.Hash).Scan(&persistedID)
	if errors.Is(err, sql.ErrNoRows) {
		stored, err := loadSourceEligibilityAssessmentByHash(ctx, tx, input.Hash)
		if err != nil {
			return err
		}
		if !stored.matches(input) {
			return errStoredSourceAcquisitionInvalid
		}
		return nil
	}
	return err
}

type storedSourceEligibilityAssessment struct {
	ID                   string
	AcquisitionID        string
	AcquisitionSnapshot  string
	Provider             string
	ExternalID           string
	PolicyVersion        string
	EvaluationDate       time.Time
	EvaluatedAt          time.Time
	USStatus             string
	USReason             string
	OPDSRights           string
	RDFRights            string
	HeaderRights         string
	UKStatus             string
	UKReason             string
	EffectiveUKJSON      string
	ProviderEvidenceJSON string
	OverallStatus        string
	OverallReason        string
	AssessmentHash       string
}

func loadSourceEligibilityAssessmentByHash(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, hash string) (storedSourceEligibilityAssessment, error) {
	var stored storedSourceEligibilityAssessment
	err := queryer.QueryRowContext(ctx, `SELECT id, acquisition_id, acquisition_snapshot_hash, provider, external_id, policy_version, evaluation_date, evaluated_at, us_status, us_reason, opds_rights, rdf_rights, header_rights, uk_status, uk_reason, effective_uk_evidence::text, provider_evidence::text, overall_status, overall_reason, assessment_hash FROM source_acquisition_eligibility_assessments WHERE assessment_hash = $1`, hash).Scan(&stored.ID, &stored.AcquisitionID, &stored.AcquisitionSnapshot, &stored.Provider, &stored.ExternalID, &stored.PolicyVersion, &stored.EvaluationDate, &stored.EvaluatedAt, &stored.USStatus, &stored.USReason, &stored.OPDSRights, &stored.RDFRights, &stored.HeaderRights, &stored.UKStatus, &stored.UKReason, &stored.EffectiveUKJSON, &stored.ProviderEvidenceJSON, &stored.OverallStatus, &stored.OverallReason, &stored.AssessmentHash)
	return stored, err
}

func (stored storedSourceEligibilityAssessment) matches(input sourceEligibilityAssessmentInput) bool {
	eligibility, err := stored.eligibility()
	if err != nil || stored.AcquisitionID != input.AcquisitionID || stored.AcquisitionSnapshot != input.SnapshotHash || stored.Provider != input.Provider || stored.ExternalID != input.ExternalID || stored.AssessmentHash != input.Hash {
		return false
	}
	hash, err := sourceEligibilityAssessmentHash(stored.AcquisitionSnapshot, stored.Provider, stored.ExternalID, *eligibility)
	if err != nil {
		return false
	}
	return hash == stored.AssessmentHash && stored.AssessmentHash == input.Hash && sourceEligibilityEqual(*eligibility, input.Eligibility)
}

func (stored storedSourceEligibilityAssessment) eligibility() (*model.AdminSourceEligibility, error) {
	effective := model.AdminSourceEligibilityEffectiveUKEvidence{}
	provider := sourceEligibilityProviderSnapshot{}
	if decodeStoredSourceEligibilityJSON(stored.EffectiveUKJSON, &effective) != nil || decodeStoredSourceEligibilityJSON(stored.ProviderEvidenceJSON, &provider) != nil {
		return nil, errStoredSourceAcquisitionInvalid
	}
	result := model.AdminSourceEligibility{
		PolicyVersion: stored.PolicyVersion, EvaluationDate: stored.EvaluationDate.UTC().Format("2006-01-02"), EvaluatedAt: stored.EvaluatedAt.UTC().Format(time.RFC3339Nano),
		US: model.AdminCopyrightJurisdiction{Status: stored.USStatus, Reason: stored.USReason}, UK: model.AdminCopyrightJurisdiction{Status: stored.UKStatus, Reason: stored.UKReason},
		Overall: stored.OverallStatus, OverallReason: stored.OverallReason, OPDSRights: stored.OPDSRights, RDFRights: stored.RDFRights, HeaderRights: stored.HeaderRights,
		ProviderTitle: provider.ProviderTitle, Contributors: provider.Contributors, RDFDigest: provider.RDFDigest, EffectiveUK: effective,
	}
	if !sourceAcquisitionHashPattern.MatchString(stored.AssessmentHash) || !validateSourceEligibility(result) {
		return nil, errStoredSourceAcquisitionInvalid
	}
	reconstructedHash, err := sourceEligibilityAssessmentHash(stored.AcquisitionSnapshot, stored.Provider, stored.ExternalID, result)
	if err != nil || reconstructedHash != stored.AssessmentHash {
		return nil, errStoredSourceAcquisitionInvalid
	}
	result.AssessmentHash = cloneString(&stored.AssessmentHash)
	return &result, nil
}

func (stored storedSourceAcquisition) eligibility() (*model.AdminSourceEligibility, error) {
	if !stored.AssessmentHash.Valid {
		if stored.AssessmentPolicyVersion.Valid || stored.AssessmentEvaluationDate.Valid || stored.AssessmentEvaluatedAt.Valid || stored.AssessmentUSStatus.Valid || stored.AssessmentUSReason.Valid || stored.AssessmentOPDSRights.Valid || stored.AssessmentRDFRights.Valid || stored.AssessmentHeaderRights.Valid || stored.AssessmentUKStatus.Valid || stored.AssessmentUKReason.Valid || stored.AssessmentOverallStatus.Valid || stored.AssessmentOverallReason.Valid || stored.AssessmentEffectiveUKJSON.Valid || stored.AssessmentProviderJSON.Valid {
			return nil, errStoredSourceAcquisitionInvalid
		}
		return nil, nil
	}
	if !stored.AssessmentPolicyVersion.Valid || !stored.AssessmentEvaluationDate.Valid || !stored.AssessmentEvaluatedAt.Valid || !stored.AssessmentUSStatus.Valid || !stored.AssessmentUSReason.Valid || !stored.AssessmentOPDSRights.Valid || !stored.AssessmentRDFRights.Valid || !stored.AssessmentHeaderRights.Valid || !stored.AssessmentUKStatus.Valid || !stored.AssessmentUKReason.Valid || !stored.AssessmentOverallStatus.Valid || !stored.AssessmentOverallReason.Valid || !stored.AssessmentEffectiveUKJSON.Valid || !stored.AssessmentProviderJSON.Valid {
		return nil, errStoredSourceAcquisitionInvalid
	}
	return (storedSourceEligibilityAssessment{AcquisitionID: stored.ID, AcquisitionSnapshot: stored.SnapshotHash, Provider: stored.Provider, ExternalID: stored.ExternalID, PolicyVersion: stored.AssessmentPolicyVersion.String, EvaluationDate: stored.AssessmentEvaluationDate.Time, EvaluatedAt: stored.AssessmentEvaluatedAt.Time, USStatus: stored.AssessmentUSStatus.String, USReason: stored.AssessmentUSReason.String, OPDSRights: stored.AssessmentOPDSRights.String, RDFRights: stored.AssessmentRDFRights.String, HeaderRights: stored.AssessmentHeaderRights.String, UKStatus: stored.AssessmentUKStatus.String, UKReason: stored.AssessmentUKReason.String, EffectiveUKJSON: stored.AssessmentEffectiveUKJSON.String, ProviderEvidenceJSON: stored.AssessmentProviderJSON.String, OverallStatus: stored.AssessmentOverallStatus.String, OverallReason: stored.AssessmentOverallReason.String, AssessmentHash: stored.AssessmentHash.String}).eligibility()
}

func decodeStoredSourceEligibilityJSON(value string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return err
	}
	return nil
}

func modelEffectiveUKEvidence(value copyrighteligibility.UKEvidence) model.AdminSourceEligibilityEffectiveUKEvidence {
	return model.AdminSourceEligibilityEffectiveUKEvidence{WorkTitle: value.WorkTitle, WorkCategory: string(value.WorkCategory), WorkCategoryReferences: modelEvidenceReferences(value.WorkCategoryReferences), Authorship: string(value.Authorship), AuthorshipReferences: modelEvidenceReferences(value.AuthorshipReferences), AuthorName: value.Author.Name, AuthorDeathYear: value.Author.DeathYear, AuthorReferences: modelEvidenceReferences(value.Author.References), FirstPublicationYear: value.FirstPublication.Year, FirstPublicationRefs: modelEvidenceReferences(value.FirstPublication.References), Translation: modelFactEvidence(value.Translation), AdditionalTextual: modelFactEvidence(value.AdditionalTextualContribution), UnpublishedAtEnd1988: modelFactEvidence(value.UnpublishedAtEnd1988)}
}

func modelFactEvidence(value copyrighteligibility.FactEvidence) model.AdminCopyrightFactEvidence {
	return model.AdminCopyrightFactEvidence{State: string(value.State), References: modelEvidenceReferences(value.References)}
}

func modelEvidenceReferences(values []copyrighteligibility.EvidenceReference) []model.AdminCopyrightEvidenceReference {
	result := make([]model.AdminCopyrightEvidenceReference, 0, len(values))
	for _, value := range values {
		result = append(result, model.AdminCopyrightEvidenceReference{Source: value.Source, Fact: value.Fact, Locator: sourceAcquisitionOptionalString(value.Locator), Identifier: sourceAcquisitionOptionalString(value.Identifier), Digest: sourceAcquisitionOptionalString(value.Digest)})
	}
	return result
}

func sourceAcquisitionOptionalString(value string) *string {
	if value == "" {
		return nil
	}
	result := value
	return &result
}

func validateSourceEligibility(value model.AdminSourceEligibility) bool {
	if value.PolicyVersion != copyrighteligibility.PolicyVersion || value.EvaluationDate == "" || value.EvaluatedAt == "" || !sourceAcquisitionHashPattern.MatchString(value.RDFDigest) || value.ProviderTitle == "" || strings.TrimSpace(value.EffectiveUK.WorkTitle) != strings.TrimSpace(value.ProviderTitle) || !copyrighteligibility.IsJurisdictionStatus(copyrighteligibility.JurisdictionStatus(value.US.Status)) || !copyrighteligibility.IsReasonCode(copyrighteligibility.ReasonCode(value.US.Reason)) || !copyrighteligibility.IsJurisdictionStatus(copyrighteligibility.JurisdictionStatus(value.UK.Status)) || !copyrighteligibility.IsReasonCode(copyrighteligibility.ReasonCode(value.UK.Reason)) || !copyrighteligibility.IsOverallStatus(copyrighteligibility.OverallStatus(value.Overall)) || !copyrighteligibility.IsReasonCode(copyrighteligibility.ReasonCode(value.OverallReason)) {
		return false
	}
	if _, err := time.Parse("2006-01-02", value.EvaluationDate); err != nil {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, value.EvaluatedAt); err != nil {
		return false
	}
	if value.US.Status != string(copyrighteligibility.JurisdictionEligible) || value.UK.Status != string(copyrighteligibility.JurisdictionEligible) || value.Overall != string(copyrighteligibility.OverallEligible) {
		return false
	}
	if !validProviderRights(value.OPDSRights) || !validProviderRights(value.RDFRights) || !validHeaderRights(value.HeaderRights) || !validEffectiveUKEvidence(value.EffectiveUK) {
		return false
	}
	for _, contributor := range value.Contributors {
		if !validSourceAcquisitionText(contributor.Name, 500) || !validSourceAcquisitionText(contributor.Role, 64) || (contributor.BirthYear != nil && *contributor.BirthYear == 0) || (contributor.DeathYear != nil && *contributor.DeathYear == 0) {
			return false
		}
	}
	return sourceEligibilityMatchesPolicy(value)
}

// sourceEligibilityMatchesPolicy independently reconstructs policy v2 from
// stored evidence. Database rows are evidence, not inherently trustworthy:
// malformed or internally contradictory assessment data fails closed.
func sourceEligibilityMatchesPolicy(value model.AdminSourceEligibility) bool {
	evaluationDate, err := time.Parse("2006-01-02", value.EvaluationDate)
	if err != nil {
		return false
	}
	ukEvidence, ok := policyUKEvidence(value.EffectiveUK)
	if !ok {
		return false
	}
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{
		EvaluationDate: evaluationDate,
		US: copyrighteligibility.USProviderEvidence{
			OPDSRights:   copyrighteligibility.ProviderRightsClassification(value.OPDSRights),
			RDFRights:    copyrighteligibility.ProviderRightsClassification(value.RDFRights),
			HeaderRights: copyrighteligibility.SourceHeaderRightsClassification(value.HeaderRights),
		},
		UK: ukEvidence,
	})
	return assessment.PolicyVersion == value.PolicyVersion &&
		string(assessment.US.Status) == value.US.Status && string(assessment.US.Reason) == value.US.Reason &&
		string(assessment.UK.Status) == value.UK.Status && string(assessment.UK.Reason) == value.UK.Reason &&
		string(assessment.Overall) == value.Overall && string(assessment.OverallReason) == value.OverallReason
}

func policyUKEvidence(value model.AdminSourceEligibilityEffectiveUKEvidence) (copyrighteligibility.UKEvidence, bool) {
	convertReferences := func(values []model.AdminCopyrightEvidenceReference) []copyrighteligibility.EvidenceReference {
		result := make([]copyrighteligibility.EvidenceReference, 0, len(values))
		for _, reference := range values {
			result = append(result, copyrighteligibility.EvidenceReference{Source: reference.Source, Fact: reference.Fact, Locator: sourceAcquisitionDereference(reference.Locator), Identifier: sourceAcquisitionDereference(reference.Identifier), Digest: sourceAcquisitionDereference(reference.Digest)})
		}
		return result
	}
	convertFact := func(fact model.AdminCopyrightFactEvidence) copyrighteligibility.FactEvidence {
		return copyrighteligibility.FactEvidence{State: copyrighteligibility.FactState(fact.State), References: convertReferences(fact.References)}
	}
	return copyrighteligibility.UKEvidence{
		WorkTitle:                     value.WorkTitle,
		WorkCategory:                  copyrighteligibility.WorkCategory(value.WorkCategory),
		WorkCategoryReferences:        convertReferences(value.WorkCategoryReferences),
		Authorship:                    copyrighteligibility.AuthorshipCategory(value.Authorship),
		AuthorshipReferences:          convertReferences(value.AuthorshipReferences),
		Author:                        copyrighteligibility.PersonEvidence{Name: value.AuthorName, DeathYear: value.AuthorDeathYear, References: convertReferences(value.AuthorReferences)},
		FirstPublication:              copyrighteligibility.PublicationEvidence{Year: value.FirstPublicationYear, References: convertReferences(value.FirstPublicationRefs)},
		Translation:                   convertFact(value.Translation),
		AdditionalTextualContribution: convertFact(value.AdditionalTextual),
		UnpublishedAtEnd1988:          convertFact(value.UnpublishedAtEnd1988),
	}, true
}

func sourceAcquisitionDereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validProviderRights(value string) bool {
	return value == string(copyrighteligibility.ProviderRightsPublicDomain) || value == string(copyrighteligibility.ProviderRightsRestricted) || value == string(copyrighteligibility.ProviderRightsUnknown)
}
func validHeaderRights(value string) bool {
	return value == string(copyrighteligibility.SourceHeaderRightsPublicDomain) || value == string(copyrighteligibility.SourceHeaderRightsRestricted) || value == string(copyrighteligibility.SourceHeaderRightsNoClassification) || value == string(copyrighteligibility.SourceHeaderRightsConflicting)
}

func validEffectiveUKEvidence(value model.AdminSourceEligibilityEffectiveUKEvidence) bool {
	if !validSourceAcquisitionText(value.WorkTitle, 500) || value.WorkCategory != string(copyrighteligibility.WorkCategoryOrdinaryLiterary) || value.Authorship != string(copyrighteligibility.AuthorshipSingleKnown) || !validSourceAcquisitionText(value.AuthorName, 500) || value.AuthorDeathYear < 1 || value.FirstPublicationYear < 1 || !validEvidenceReferences(value.WorkCategoryReferences) || !validEvidenceReferences(value.AuthorshipReferences) || !validEvidenceReferences(value.AuthorReferences) || !validEvidenceReferences(value.FirstPublicationRefs) {
		return false
	}
	return validFactEvidence(value.Translation) && validFactEvidence(value.AdditionalTextual) && validFactEvidence(value.UnpublishedAtEnd1988)
}

func validFactEvidence(value model.AdminCopyrightFactEvidence) bool {
	return (value.State == string(copyrighteligibility.FactNoneConfirmed) || value.State == string(copyrighteligibility.FactPresent) || value.State == string(copyrighteligibility.FactUnknown)) && validEvidenceReferences(value.References)
}

func validEvidenceReferences(values []model.AdminCopyrightEvidenceReference) bool {
	if len(values) == 0 || len(values) > 8 {
		return false
	}
	for _, value := range values {
		if !validSourceAcquisitionText(value.Source, 500) || !validSourceAcquisitionText(value.Fact, 1000) || (value.Locator != nil && !validSourceAcquisitionText(*value.Locator, 2048)) || (value.Identifier != nil && !validSourceAcquisitionText(*value.Identifier, 256)) || (value.Digest != nil && !sourceAcquisitionHashPattern.MatchString(*value.Digest)) {
			return false
		}
	}
	return true
}

func sourceEligibilityEqual(left, right model.AdminSourceEligibility) bool {
	left.AssessmentHash, right.AssessmentHash = nil, nil
	left.EvaluatedAt, right.EvaluatedAt = "", ""
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
