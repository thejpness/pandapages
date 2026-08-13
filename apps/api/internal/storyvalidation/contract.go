package storyvalidation

import (
	"fmt"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

type ValidationVersion string

const ValidationV2 ValidationVersion = "panda-pages-semantic-validation-v2"

type EvidenceLocation string

const (
	EvidenceCanonicalSource  EvidenceLocation = "canonical_source"
	EvidenceStoryAnalysis    EvidenceLocation = "story_analysis"
	EvidenceGeneratedEdition EvidenceLocation = "generated_edition"
)

type Evidence struct {
	Location    EvidenceLocation            `json:"location"`
	EditionKey  *model.AdminStoryEditionKey `json:"editionKey"`
	Excerpt     string                      `json:"excerpt"`
	Explanation string                      `json:"explanation"`
}

type Finding struct {
	Code     adaptationcontract.FindingCode     `json:"code"`
	Severity adaptationcontract.FindingSeverity `json:"severity"`
	Message  string                             `json:"message"`
	Evidence []Evidence                         `json:"evidence"`
}

type Assessment struct {
	ValidationVersion    ValidationVersion                    `json:"validationVersion"`
	SpecificationVersion storygeneration.SpecificationVersion `json:"specificationVersion"`
	AssessmentScope      adaptationcontract.AssessmentScope   `json:"assessmentScope"`
	EditionKey           *model.AdminStoryEditionKey          `json:"editionKey,omitempty"`
	EditionKeys          []model.AdminStoryEditionKey         `json:"editionKeys,omitempty"`
	Result               adaptationcontract.Result            `json:"result"`
	Findings             []Finding                            `json:"findings"`
}

func (assessment Assessment) Validate() error {
	switch assessment.ValidationVersion {
	case ValidationV2, ValidationV3:
	default:
		return fmt.Errorf(
			"validation version must equal %q or %q",
			ValidationV2,
			ValidationV3,
		)
	}
	if assessment.SpecificationVersion != storygeneration.SpecificationV2 {
		return fmt.Errorf("specification version must equal %q", storygeneration.SpecificationV2)
	}

	base := adaptationcontract.Assessment{
		ContractVersion: adaptationcontract.VersionV1,
		AssessmentScope: assessment.AssessmentScope,
		EditionKey:      assessment.EditionKey,
		EditionKeys:     append([]model.AdminStoryEditionKey(nil), assessment.EditionKeys...),
		Result:          assessment.Result,
		Findings:        make([]adaptationcontract.Finding, 0, len(assessment.Findings)),
	}
	for _, finding := range assessment.Findings {
		base.Findings = append(base.Findings, adaptationcontract.Finding{
			Code:     finding.Code,
			Severity: finding.Severity,
			Message:  finding.Message,
		})
	}
	if err := base.ValidateSemantic(); err != nil {
		return fmt.Errorf("semantic assessment envelope is invalid: %w", err)
	}

	for index, finding := range assessment.Findings {
		if len(finding.Evidence) == 0 {
			return fmt.Errorf("finding %d: evidence is required", index+1)
		}
		for evidenceIndex, evidence := range finding.Evidence {
			if err := validateEvidence(assessment, evidence); err != nil {
				return fmt.Errorf("finding %d evidence %d: %w", index+1, evidenceIndex+1, err)
			}
		}
	}

	return nil
}

func validateEvidence(assessment Assessment, evidence Evidence) error {
	switch evidence.Location {
	case EvidenceCanonicalSource, EvidenceStoryAnalysis:
		if evidence.EditionKey != nil {
			return fmt.Errorf("%q evidence must not contain editionKey", evidence.Location)
		}
	case EvidenceGeneratedEdition:
		if evidence.EditionKey == nil {
			return fmt.Errorf("generated_edition evidence requires editionKey")
		}
		if !assessmentContainsEdition(assessment, *evidence.EditionKey) {
			return fmt.Errorf("generated_edition evidence editionKey %q is outside the assessment target", *evidence.EditionKey)
		}
	default:
		return fmt.Errorf("unsupported evidence location %q", evidence.Location)
	}

	if strings.TrimSpace(evidence.Excerpt) == "" {
		return fmt.Errorf("excerpt is required")
	}
	if strings.TrimSpace(evidence.Explanation) == "" {
		return fmt.Errorf("explanation is required")
	}
	return nil
}

func assessmentContainsEdition(assessment Assessment, key model.AdminStoryEditionKey) bool {
	switch assessment.AssessmentScope {
	case adaptationcontract.AssessmentScopeEdition:
		return assessment.EditionKey != nil && *assessment.EditionKey == key
	case adaptationcontract.AssessmentScopeBundle:
		for _, candidate := range assessment.EditionKeys {
			if candidate == key {
				return true
			}
		}
	}
	return false
}
