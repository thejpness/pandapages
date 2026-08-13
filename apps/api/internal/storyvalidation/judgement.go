package storyvalidation

import (
	"fmt"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

const ValidationV3 ValidationVersion = "panda-pages-semantic-validation-v3"

type EvidenceReference struct {
	SegmentID   EvidenceSegmentID `json:"segmentId"`
	Explanation string            `json:"explanation"`
}

type JudgementFinding struct {
	Code     adaptationcontract.FindingCode     `json:"code"`
	Severity adaptationcontract.FindingSeverity `json:"severity"`
	Message  string                             `json:"message"`
	Evidence []EvidenceReference                `json:"evidence"`
}

// SemanticJudgement is the model-facing v3 contract.
//
// Unlike the final evidence-bearing Assessment, the model does not author
// excerpts, evidence locations, or edition bindings. It identifies deterministic
// evidence segments and explains their relevance. Resolution into final Evidence
// is a separate deterministic step.
type SemanticJudgement struct {
	ValidationVersion    ValidationVersion                    `json:"validationVersion"`
	SpecificationVersion storygeneration.SpecificationVersion `json:"specificationVersion"`
	AssessmentScope      adaptationcontract.AssessmentScope   `json:"assessmentScope"`
	EditionKey           *model.AdminStoryEditionKey          `json:"editionKey,omitempty"`
	EditionKeys          []model.AdminStoryEditionKey         `json:"editionKeys,omitempty"`
	Result               adaptationcontract.Result            `json:"result"`
	Findings             []JudgementFinding                   `json:"findings"`
}

func (judgement SemanticJudgement) Validate() error {
	if judgement.ValidationVersion != ValidationV3 {
		return fmt.Errorf("validation version must equal %q", ValidationV3)
	}
	if judgement.SpecificationVersion != storygeneration.SpecificationV2 {
		return fmt.Errorf(
			"specification version must equal %q",
			storygeneration.SpecificationV2,
		)
	}

	base := adaptationcontract.Assessment{
		ContractVersion: adaptationcontract.VersionV1,
		AssessmentScope: judgement.AssessmentScope,
		EditionKey:      judgement.EditionKey,
		EditionKeys:     append([]model.AdminStoryEditionKey(nil), judgement.EditionKeys...),
		Result:          judgement.Result,
		Findings:        make([]adaptationcontract.Finding, 0, len(judgement.Findings)),
	}

	for _, finding := range judgement.Findings {
		base.Findings = append(base.Findings, adaptationcontract.Finding{
			Code:     finding.Code,
			Severity: finding.Severity,
			Message:  finding.Message,
		})
	}

	if err := base.ValidateSemantic(); err != nil {
		return fmt.Errorf("semantic judgement envelope is invalid: %w", err)
	}

	for findingIndex, finding := range judgement.Findings {
		if len(finding.Evidence) == 0 {
			return fmt.Errorf("finding %d: evidence is required", findingIndex+1)
		}
		for evidenceIndex, evidence := range finding.Evidence {
			if strings.TrimSpace(string(evidence.SegmentID)) == "" {
				return fmt.Errorf(
					"finding %d evidence %d: segmentId is required",
					findingIndex+1,
					evidenceIndex+1,
				)
			}
			if strings.TrimSpace(evidence.Explanation) == "" {
				return fmt.Errorf(
					"finding %d evidence %d: explanation is required",
					findingIndex+1,
					evidenceIndex+1,
				)
			}
		}
	}

	return nil
}

func (judgement SemanticJudgement) ValidateAgainstEvidenceIndex(index EvidenceIndex) error {
	if err := judgement.Validate(); err != nil {
		return err
	}

	for findingIndex, finding := range judgement.Findings {
		for evidenceIndex, reference := range finding.Evidence {
			segment, err := index.Resolve(reference.SegmentID)
			if err != nil {
				return fmt.Errorf(
					"finding %d evidence %d: %w",
					findingIndex+1,
					evidenceIndex+1,
					err,
				)
			}

			if segment.Location == EvidenceGeneratedEdition {
				if segment.EditionKey == nil {
					return fmt.Errorf(
						"finding %d evidence %d: generated-edition segment %q has no edition key",
						findingIndex+1,
						evidenceIndex+1,
						reference.SegmentID,
					)
				}
				if !judgementContainsEdition(judgement, *segment.EditionKey) {
					return fmt.Errorf(
						"finding %d evidence %d: segment %q belongs to edition %q outside the judgement target",
						findingIndex+1,
						evidenceIndex+1,
						reference.SegmentID,
						*segment.EditionKey,
					)
				}
			}
		}
	}

	return nil
}

func judgementContainsEdition(
	judgement SemanticJudgement,
	key model.AdminStoryEditionKey,
) bool {
	switch judgement.AssessmentScope {
	case adaptationcontract.AssessmentScopeEdition:
		return judgement.EditionKey != nil && *judgement.EditionKey == key
	case adaptationcontract.AssessmentScopeBundle:
		for _, candidate := range judgement.EditionKeys {
			if candidate == key {
				return true
			}
		}
	}

	return false
}
