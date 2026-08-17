package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
	"pandapages/api/internal/storyorchestration"
)

// LoadGenerationSourceVersion returns the validated, provider-promoted
// canonical source snapshot for one immutable source-version ID. It performs
// only a short database read transaction; callers must complete it before
// invoking model-backed orchestration.
func (s *Store) LoadGenerationSourceVersion(sourceVersionID string) (storyorchestration.Input, error) {
	sourceVersionID = strings.TrimSpace(sourceVersionID)
	if !accountIDRe.MatchString(sourceVersionID) {
		return storyorchestration.Input{}, fmt.Errorf("source version ID is invalid")
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return storyorchestration.Input{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var sourceID, storyID, slug string
	err = tx.QueryRowContext(ctx, `
		SELECT version.source_id, version.story_id, story.slug
		FROM story_source_versions AS version
		JOIN stories AS story ON story.id = version.story_id
		WHERE version.id = $1
	`, sourceVersionID).Scan(&sourceID, &storyID, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		return storyorchestration.Input{}, fmt.Errorf("source version not found")
	}
	if err != nil {
		return storyorchestration.Input{}, err
	}
	if storyingest.ValidateSlug(slug) != nil {
		return storyorchestration.Input{}, fmt.Errorf("%w: story slug", errStoredSourceInvalid)
	}

	snapshot, err := loadAdminSourceVersionSnapshot(ctx, tx, storyID, sourceID, sourceVersionID)
	if err != nil {
		return storyorchestration.Input{}, err
	}
	if snapshot.ID != sourceVersionID || snapshot.Provenance == nil ||
		snapshot.Provenance.Kind != "source_acquisition" {
		return storyorchestration.Input{}, fmt.Errorf("%w: provider-promoted source provenance", errStoredSourceInvalid)
	}

	storedAcquisition, err := loadAdminSourceAcquisitionDetail(ctx, tx, snapshot.Provenance.AcquisitionID)
	if err != nil {
		return storyorchestration.Input{}, fmt.Errorf("load source acquisition evidence: %w", err)
	}
	acquisition, err := storedAcquisition.detail()
	if err != nil {
		return storyorchestration.Input{}, fmt.Errorf("validate source acquisition evidence: %w", err)
	}
	if acquisition.SourceQuality.Status != model.AdminSourceQualityApproved {
		return storyorchestration.Input{}, fmt.Errorf("source acquisition is not approved for generation")
	}

	assessment, err := loadSourceEligibilityAssessmentByHash(ctx, tx, snapshot.Provenance.AssessmentHash)
	if err != nil {
		return storyorchestration.Input{}, fmt.Errorf("load source eligibility evidence: %w", err)
	}
	eligibility, err := assessment.eligibility()
	if err != nil ||
		assessment.AcquisitionID != acquisition.ID ||
		assessment.AcquisitionSnapshot != acquisition.SnapshotHash ||
		assessment.Provider != acquisition.Provider ||
		assessment.ExternalID != acquisition.ExternalID ||
		assessment.AssessmentHash != snapshot.Provenance.AssessmentHash ||
		eligibility.PolicyVersion != copyrighteligibility.PolicyVersion ||
		eligibility.Overall != string(copyrighteligibility.OverallEligible) {
		return storyorchestration.Input{}, fmt.Errorf("%w: source eligibility evidence", errStoredSourceInvalid)
	}

	author := ""
	if snapshot.Author != nil {
		author = *snapshot.Author
	}
	sourceURL := ""
	if snapshot.SourceURL != nil {
		sourceURL = *snapshot.SourceURL
	}
	input := storyorchestration.Input{
		SourceIdentity:  sourceVersionID,
		Title:           snapshot.Title,
		Author:          author,
		Slug:            slug,
		Language:        snapshot.Language,
		SourceURL:       sourceURL,
		Rights:          cloneJSONMap(snapshot.Rights),
		CanonicalSource: snapshot.SourceText,
	}
	if err := tx.Commit(); err != nil {
		return storyorchestration.Input{}, err
	}
	return input, nil
}
