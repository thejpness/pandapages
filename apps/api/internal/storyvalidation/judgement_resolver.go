package storyvalidation

import (
	"fmt"

	"pandapages/api/internal/model"
)

// ResolveSemanticJudgement deterministically resolves model-selected evidence
// segment references into a final evidence-bearing Assessment.
func ResolveSemanticJudgement(
	judgement SemanticJudgement,
	index EvidenceIndex,
) (Assessment, error) {
	if err := judgement.ValidateAgainstEvidenceIndex(index); err != nil {
		return Assessment{}, fmt.Errorf("validate semantic judgement references: %w", err)
	}

	assessment := Assessment{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: judgement.SpecificationVersion,
		AssessmentScope:      judgement.AssessmentScope,
		EditionKey:           cloneEditionKey(judgement.EditionKey),
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), judgement.EditionKeys...),
		Result:               judgement.Result,
		Findings:             make([]Finding, 0, len(judgement.Findings)),
	}

	for findingIndex, judgementFinding := range judgement.Findings {
		finding := Finding{
			Code:     judgementFinding.Code,
			Severity: judgementFinding.Severity,
			Message:  judgementFinding.Message,
			Evidence: make([]Evidence, 0, len(judgementFinding.Evidence)),
		}

		for evidenceIndex, reference := range judgementFinding.Evidence {
			segment, err := index.Resolve(reference.SegmentID)
			if err != nil {
				return Assessment{}, fmt.Errorf(
					"resolve finding %d evidence %d: %w",
					findingIndex+1,
					evidenceIndex+1,
					err,
				)
			}
			finding.Evidence = append(finding.Evidence, Evidence{
				Location:    segment.Location,
				EditionKey:  cloneEditionKey(segment.EditionKey),
				Excerpt:     segment.Text,
				Explanation: reference.Explanation,
			})
		}

		assessment.Findings = append(assessment.Findings, finding)
	}

	if err := assessment.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("validate resolved semantic assessment: %w", err)
	}
	return assessment, nil
}

func cloneEditionKey(key *model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	if key == nil {
		return nil
	}
	clone := *key
	return &clone
}
