package db

import (
	"context"
	"database/sql"
	"fmt"

	"pandapages/api/internal/model"
)

// loadCanonicalSourceProvenance validates that a canonical source version is
// either manual (no provider linkage) or carries one complete, healthy
// acquisition/assessment pair. It deliberately does not downgrade corrupt
// provider provenance to a manual source.
func loadCanonicalSourceProvenance(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	versionID string,
) (*canonicalSourceProvenance, error) {
	var acquisitionID, assessmentID, provider, externalID, acquisitionSnapshotHash, assessmentHash sql.NullString
	err := queryer.QueryRowContext(ctx, `
		SELECT
			version.source_acquisition_id,
			version.source_eligibility_assessment_id,
			acquisition.provider,
			acquisition.external_id,
			acquisition.snapshot_hash,
			assessment.assessment_hash
		FROM story_source_versions AS version
		LEFT JOIN source_acquisitions AS acquisition
			ON acquisition.id = version.source_acquisition_id
		LEFT JOIN source_acquisition_eligibility_assessments AS assessment
			ON assessment.id = version.source_eligibility_assessment_id
			AND assessment.acquisition_id = version.source_acquisition_id
		WHERE version.id = $1
	`, versionID).Scan(&acquisitionID, &assessmentID, &provider, &externalID, &acquisitionSnapshotHash, &assessmentHash)
	if err != nil {
		return nil, err
	}
	if !acquisitionID.Valid && !assessmentID.Valid && !provider.Valid && !externalID.Valid && !acquisitionSnapshotHash.Valid && !assessmentHash.Valid {
		return nil, nil
	}
	if !acquisitionID.Valid || !assessmentID.Valid || !provider.Valid || !externalID.Valid || !acquisitionSnapshotHash.Valid || !assessmentHash.Valid ||
		!sourceAcquisitionProviderPattern.MatchString(provider.String) ||
		!validSourceAcquisitionText(externalID.String, 128) ||
		!sourceAcquisitionHashPattern.MatchString(acquisitionSnapshotHash.String) ||
		!sourceAcquisitionHashPattern.MatchString(assessmentHash.String) {
		return nil, fmt.Errorf("%w: source provenance", errStoredSourceInvalid)
	}
	assessment, err := loadSourceEligibilityAssessmentByHash(ctx, queryer, assessmentHash.String)
	if err != nil || assessment.ID != assessmentID.String || assessment.AcquisitionID != acquisitionID.String ||
		assessment.Provider != provider.String || assessment.ExternalID != externalID.String || assessment.AcquisitionSnapshot != acquisitionSnapshotHash.String {
		return nil, fmt.Errorf("%w: source provenance", errStoredSourceInvalid)
	}
	if _, err := assessment.eligibility(); err != nil {
		return nil, fmt.Errorf("%w: source provenance", errStoredSourceInvalid)
	}
	return &canonicalSourceProvenance{
		AcquisitionID:           acquisitionID.String,
		AcquisitionSnapshotHash: acquisitionSnapshotHash.String,
		AssessmentID:            assessmentID.String,
		Provider:                provider.String,
		ExternalID:              externalID.String,
		AssessmentHash:          assessmentHash.String,
	}, nil
}

func sourceProvenanceResponse(value *canonicalSourceProvenance) *model.AdminSourceProvenance {
	if value == nil {
		return nil
	}
	return &model.AdminSourceProvenance{
		Kind:           "source_acquisition",
		AcquisitionID:  value.AcquisitionID,
		Provider:       value.Provider,
		ExternalID:     value.ExternalID,
		AssessmentHash: value.AssessmentHash,
	}
}
