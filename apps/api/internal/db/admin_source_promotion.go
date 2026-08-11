package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

// AdminPromoteSourceAcquisition creates one immutable canonical source version
// from already-reviewed durable evidence. It intentionally performs no remote
// provider activity: eligibility and source integrity are revalidated from the
// saved acquisition and assessment inside this transaction.
func (s *Store) AdminPromoteSourceAcquisition(
	id string,
	req model.AdminSourceAcquisitionPromotionRequest,
) (model.AdminSourceAcquisitionPromotionResponse, error) {
	id, ok := canonicalSourceAcquisitionID(id)
	if !ok {
		return model.AdminSourceAcquisitionPromotionResponse{}, model.ErrAdminSourceAcquisitionNotFound
	}
	target, err := canonicalSourceAcquisitionPromotionTarget(req.Target)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM source_acquisitions WHERE id = $1 FOR UPDATE`, id).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceAcquisitionPromotionResponse{}, model.ErrAdminSourceAcquisitionNotFound
	} else if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	stored, err := loadAdminSourceAcquisitionDetail(ctx, tx, lockedID)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	acquisition, err := stored.detail()
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	assessment, err := loadCurrentEligibleSourceAssessment(ctx, tx, acquisition)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	var qualityStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM source_acquisition_quality_reviews
		WHERE acquisition_id = $1
		FOR UPDATE
	`, acquisition.ID).Scan(&qualityStatus); err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	if model.AdminSourceQualityStatus(qualityStatus) != model.AdminSourceQualityApproved {
		return model.AdminSourceAcquisitionPromotionResponse{}, model.ErrAdminSourceAcquisitionNotReady
	}

	if existing, err := loadExistingSourcePromotion(ctx, tx, acquisition.ID); err == nil {
		if !target.matches(existing.StorySlug) {
			return model.AdminSourceAcquisitionPromotionResponse{}, model.ErrAdminSourceAcquisitionAlreadyPromoted
		}
		if err := tx.Commit(); err != nil {
			return model.AdminSourceAcquisitionPromotionResponse{}, err
		}
		return model.AdminSourceAcquisitionPromotionResponse{Outcome: model.AdminSourceAcquisitionPromotionReused, Promotion: existing}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}

	story, err := target.resolveStory(ctx, tx, acquisition, assessment)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	sourceID, err := ensureCanonicalStorySource(ctx, tx, story.ID)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	version, err := createPromotedCanonicalSourceVersion(ctx, tx, sourceID, story, acquisition, assessment)
	if err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE story_sources
		SET current_version_id = $2, updated_at = now()
		WHERE id = $1
	`, sourceID, version.SourceVersionID); err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE stories SET updated_at = now() WHERE id = $1`, story.ID); err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AdminSourceAcquisitionPromotionResponse{}, err
	}
	return model.AdminSourceAcquisitionPromotionResponse{Outcome: model.AdminSourceAcquisitionPromotionCreated, Promotion: version}, nil
}

type canonicalPromotionTarget struct {
	mode      model.AdminSourceAcquisitionPromotionTargetMode
	title     string
	slug      string
	storySlug string
}

func canonicalSourceAcquisitionPromotionTarget(raw model.AdminSourceAcquisitionPromotionTarget) (canonicalPromotionTarget, error) {
	target := canonicalPromotionTarget{mode: raw.Mode, title: strings.TrimSpace(raw.Title), slug: strings.TrimSpace(raw.Slug), storySlug: strings.TrimSpace(raw.StorySlug)}
	issues := make([]model.AdminValidationIssue, 0, 3)
	switch target.mode {
	case model.AdminSourceAcquisitionPromotionTargetNewStory:
		if storyingest.ValidateSlug(target.slug) != nil {
			issues = append(issues, model.AdminValidationIssue{Field: "target.slug", Code: "invalid", Message: "Use lowercase letters, numbers, and hyphens"})
		}
		if !utf8.ValidString(raw.Title) || target.title == "" || len(target.title) > 1000 {
			issues = append(issues, model.AdminValidationIssue{Field: "target.title", Code: "invalid", Message: "Enter a valid story title"})
		}
		if target.storySlug != "" {
			issues = append(issues, model.AdminValidationIssue{Field: "target.storySlug", Code: "invalid", Message: "New story promotion cannot select an existing story"})
		}
	case model.AdminSourceAcquisitionPromotionTargetExistingStory:
		if storyingest.ValidateSlug(target.storySlug) != nil {
			issues = append(issues, model.AdminValidationIssue{Field: "target.storySlug", Code: "invalid", Message: "Select a public story"})
		}
		if target.title != "" || target.slug != "" {
			issues = append(issues, model.AdminValidationIssue{Field: "target", Code: "invalid", Message: "Existing story promotion cannot include new story fields"})
		}
	default:
		issues = append(issues, model.AdminValidationIssue{Field: "target.mode", Code: "invalid", Message: "Promotion target is invalid"})
	}
	if len(issues) > 0 {
		return canonicalPromotionTarget{}, &model.AdminValidationError{Issues: issues}
	}
	return target, nil
}

func (target canonicalPromotionTarget) matches(storySlug string) bool {
	if target.mode == model.AdminSourceAcquisitionPromotionTargetNewStory {
		return target.slug == storySlug
	}
	return target.storySlug == storySlug
}

type promotionStory struct{ ID, Slug, Title string }

func (target canonicalPromotionTarget) resolveStory(ctx context.Context, tx *sql.Tx, acquisition model.AdminSourceAcquisitionDetail, assessment storedSourceEligibilityAssessment) (promotionStory, error) {
	if target.mode == model.AdminSourceAcquisitionPromotionTargetExistingStory {
		var story promotionStory
		err := tx.QueryRowContext(ctx, `
			SELECT id, slug, title FROM stories
			WHERE slug = $1 AND visibility = 'public' AND owner_account_id IS NULL
			FOR UPDATE
		`, target.storySlug).Scan(&story.ID, &story.Slug, &story.Title)
		if errors.Is(err, sql.ErrNoRows) {
			return promotionStory{}, model.ErrAdminSourceAcquisitionPromotionTarget
		}
		return story, err
	}
	language, err := promotionLanguage(acquisition.Languages)
	if err != nil {
		return promotionStory{}, err
	}
	author, err := promotionAuthor(assessment)
	if err != nil {
		return promotionStory{}, err
	}
	var story promotionStory
	err = tx.QueryRowContext(ctx, `
		INSERT INTO stories (visibility, owner_account_id, slug, title, author, language, rights)
		VALUES ('public', NULL, $1, $2, $3, $4, '{}'::jsonb)
		ON CONFLICT (slug) DO NOTHING
		RETURNING id, slug, title
	`, target.slug, target.title, author, language).Scan(&story.ID, &story.Slug, &story.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return promotionStory{}, model.ErrAdminSourceAcquisitionPromotionConflict
	}
	return story, err
}

func promotionLanguage(languages []string) (string, error) {
	if len(languages) != 1 || !validSourceAcquisitionText(languages[0], 64) {
		return "", model.ErrAdminSourceAcquisitionNotReady
	}
	return languages[0], nil
}

func promotionAuthor(assessment storedSourceEligibilityAssessment) (*string, error) {
	eligibility, err := assessment.eligibility()
	if err != nil || eligibility.EffectiveUK.AuthorName == "" || !utf8.ValidString(eligibility.EffectiveUK.AuthorName) {
		return nil, model.ErrAdminSourceAcquisitionNotReady
	}
	name := eligibility.EffectiveUK.AuthorName
	return &name, nil
}

func ensureCanonicalStorySource(ctx context.Context, tx *sql.Tx, storyID string) (string, error) {
	var sourceID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM story_sources WHERE story_id = $1 FOR UPDATE`, storyID).Scan(&sourceID)
	if err == nil {
		return sourceID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	returning := tx.QueryRowContext(ctx, `INSERT INTO story_sources (story_id) VALUES ($1) RETURNING id`, storyID)
	if err := returning.Scan(&sourceID); err != nil {
		return "", err
	}
	return sourceID, nil
}

func createPromotedCanonicalSourceVersion(ctx context.Context, tx *sql.Tx, sourceID string, story promotionStory, acquisition model.AdminSourceAcquisitionDetail, assessment storedSourceEligibilityAssessment) (model.AdminSourceAcquisitionPromotion, error) {
	language, err := promotionLanguage(acquisition.Languages)
	if err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	author, err := promotionAuthor(assessment)
	if err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	canonical := adminCanonicalSource{
		Title: acquisition.Title, Author: author, Language: language, Rights: map[string]any{}, SourceURL: &acquisition.LandingURL, SourceText: acquisition.SourceText,
		Provenance: &canonicalSourceProvenance{AcquisitionID: acquisition.ID, AcquisitionSnapshotHash: acquisition.SnapshotHash, AssessmentID: assessment.ID, Provider: acquisition.Provider, ExternalID: acquisition.ExternalID, AssessmentHash: assessment.AssessmentHash},
	}
	canonical.SnapshotHash, err = canonicalSourceSnapshotHash(canonical)
	if err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM story_source_versions WHERE source_id = $1`, sourceID).Scan(&nextVersion); err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	var versionID string
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO story_source_versions (source_id, story_id, version, title, author, language, rights, source_url, source_text, source_acquisition_id, source_eligibility_assessment_id, snapshot_hash)
		VALUES ($1,$2,$3,$4,$5,$6,'{}'::jsonb,$7,$8,$9,$10,$11)
		RETURNING id, created_at
	`, sourceID, story.ID, nextVersion, canonical.Title, canonical.Author, canonical.Language, canonical.SourceURL, canonical.SourceText, canonical.Provenance.AcquisitionID, canonical.Provenance.AssessmentID, canonical.SnapshotHash).Scan(&versionID, &createdAt)
	if err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	return model.AdminSourceAcquisitionPromotion{StoryID: story.ID, StorySlug: story.Slug, StoryTitle: story.Title, SourceVersionID: versionID, SourceVersion: nextVersion, PromotedAt: createdAt.UTC().Format(time.RFC3339Nano)}, nil
}

func loadCurrentEligibleSourceAssessment(ctx context.Context, tx *sql.Tx, acquisition model.AdminSourceAcquisitionDetail) (storedSourceEligibilityAssessment, error) {
	var hash string
	err := tx.QueryRowContext(ctx, `
		SELECT assessment_hash
		FROM source_acquisition_eligibility_assessments
		WHERE acquisition_id = $1 AND policy_version = $2 AND overall_status = 'eligible'
		ORDER BY evaluation_date DESC, evaluated_at DESC, id DESC
		LIMIT 1
	`, acquisition.ID, copyrighteligibility.PolicyVersion).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return storedSourceEligibilityAssessment{}, model.ErrAdminSourceAcquisitionNotReady
	}
	if err != nil {
		return storedSourceEligibilityAssessment{}, err
	}
	assessment, err := loadSourceEligibilityAssessmentByHash(ctx, tx, hash)
	if err != nil {
		return storedSourceEligibilityAssessment{}, err
	}
	eligibility, err := assessment.eligibility()
	if err != nil || assessment.AcquisitionID != acquisition.ID || assessment.AcquisitionSnapshot != acquisition.SnapshotHash || assessment.Provider != acquisition.Provider || assessment.ExternalID != acquisition.ExternalID || eligibility.PolicyVersion != copyrighteligibility.PolicyVersion || eligibility.Overall != string(copyrighteligibility.OverallEligible) {
		return storedSourceEligibilityAssessment{}, errStoredSourceAcquisitionInvalid
	}
	return assessment, nil
}

func loadExistingSourcePromotion(ctx context.Context, tx *sql.Tx, acquisitionID string) (model.AdminSourceAcquisitionPromotion, error) {
	var result model.AdminSourceAcquisitionPromotion
	var sourceID string
	var promotedAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT version.id, version.source_id, version.story_id, version.version, version.created_at, story.slug, story.title
		FROM story_source_versions AS version
		JOIN stories AS story ON story.id = version.story_id
		WHERE version.source_acquisition_id = $1
	`, acquisitionID).Scan(&result.SourceVersionID, &sourceID, &result.StoryID, &result.SourceVersion, &promotedAt, &result.StorySlug, &result.StoryTitle)
	if err != nil {
		return model.AdminSourceAcquisitionPromotion{}, err
	}
	snapshot, err := loadAdminSourceVersionSnapshot(ctx, tx, result.StoryID, sourceID, result.SourceVersionID)
	if err != nil || snapshot.Provenance == nil || snapshot.Provenance.AcquisitionID != acquisitionID {
		return model.AdminSourceAcquisitionPromotion{}, errStoredSourceInvalid
	}
	result.PromotedAt = promotedAt.UTC().Format(time.RFC3339Nano)
	return result, nil
}
