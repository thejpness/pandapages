package storybenchmark

import (
	"fmt"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

type Version string

const VersionV1 Version = "panda-pages-story-benchmark-v1"

type AssessmentExpectation struct {
	AssessmentScope       adaptationcontract.AssessmentScope `json:"assessmentScope"`
	EditionKey            *model.AdminStoryEditionKey        `json:"editionKey,omitempty"`
	EditionKeys           []model.AdminStoryEditionKey       `json:"editionKeys,omitempty"`
	ExpectedResult        adaptationcontract.Result          `json:"expectedResult"`
	RequiredFindingCodes  []adaptationcontract.FindingCode   `json:"requiredFindingCodes"`
	ForbiddenFindingCodes []adaptationcontract.FindingCode   `json:"forbiddenFindingCodes"`
}

func (expectation AssessmentExpectation) Validate() error {
	if err := validateExpectedResult(expectation.ExpectedResult); err != nil {
		return err
	}
	if err := validateExpectationTarget(expectation); err != nil {
		return err
	}

	required, err := validateExpectationFindingCodes(expectation, "required", expectation.RequiredFindingCodes)
	if err != nil {
		return err
	}
	forbidden, err := validateExpectationFindingCodes(expectation, "forbidden", expectation.ForbiddenFindingCodes)
	if err != nil {
		return err
	}

	for code := range required {
		if _, exists := forbidden[code]; exists {
			return fmt.Errorf("finding code %q cannot be both required and forbidden", code)
		}
	}

	if expectation.ExpectedResult == adaptationcontract.ResultPass && len(required) != 0 {
		return fmt.Errorf("pass expectation cannot require findings")
	}
	if expectation.ExpectedResult == adaptationcontract.ResultNeedsReview {
		for code := range required {
			severity, ok := adaptationcontract.CanonicalSeverity(code)
			if !ok {
				return fmt.Errorf("required finding code %q has no canonical severity", code)
			}
			if severity == adaptationcontract.FindingSeverityBlocking {
				return fmt.Errorf("needs_review expectation cannot require blocking finding %q", code)
			}
		}
	}

	return nil
}

func validateExpectedResult(result adaptationcontract.Result) error {
	switch result {
	case adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail:
		return nil
	default:
		return fmt.Errorf("unsupported expected result %q", result)
	}
}

func validateExpectationTarget(expectation AssessmentExpectation) error {
	assessment := adaptationcontract.Assessment{
		ContractVersion: adaptationcontract.VersionV1,
		AssessmentScope: expectation.AssessmentScope,
		EditionKey:      copyEditionKey(expectation.EditionKey),
		EditionKeys:     append([]model.AdminStoryEditionKey(nil), expectation.EditionKeys...),
		Result:          adaptationcontract.ResultPass,
		Findings:        []adaptationcontract.Finding{},
	}
	if err := assessment.ValidateSemantic(); err != nil {
		return fmt.Errorf("benchmark assessment target is invalid: %w", err)
	}
	return nil
}

func validateExpectationFindingCodes(
	expectation AssessmentExpectation,
	label string,
	codes []adaptationcontract.FindingCode,
) (map[adaptationcontract.FindingCode]struct{}, error) {
	seen := make(map[adaptationcontract.FindingCode]struct{}, len(codes))
	for index, code := range codes {
		if _, exists := seen[code]; exists {
			return nil, fmt.Errorf("%s finding code %d duplicates %q", label, index+1, code)
		}

		kind, ok := adaptationcontract.FindingKindFor(code)
		if !ok {
			return nil, fmt.Errorf("%s finding code %d is unsupported: %q", label, index+1, code)
		}
		if kind != adaptationcontract.FindingKindSemantic {
			return nil, fmt.Errorf("%s finding code %q is structural, not semantic", label, code)
		}
		severity, ok := adaptationcontract.CanonicalSeverity(code)
		if !ok {
			return nil, fmt.Errorf("%s finding code %q has no canonical severity", label, code)
		}

		result := adaptationcontract.ResultNeedsReview
		if severity == adaptationcontract.FindingSeverityBlocking {
			result = adaptationcontract.ResultFail
		}
		assessment := adaptationcontract.Assessment{
			ContractVersion: adaptationcontract.VersionV1,
			AssessmentScope: expectation.AssessmentScope,
			EditionKey:      copyEditionKey(expectation.EditionKey),
			EditionKeys:     append([]model.AdminStoryEditionKey(nil), expectation.EditionKeys...),
			Result:          result,
			Findings: []adaptationcontract.Finding{
				{
					Code:     code,
					Severity: severity,
					Message:  "benchmark expectation validation",
				},
			},
		}
		if err := assessment.ValidateSemantic(); err != nil {
			return nil, fmt.Errorf("%s finding code %q is invalid for the benchmark target: %w", label, code, err)
		}
		seen[code] = struct{}{}
	}
	return seen, nil
}

func copyEditionKey(value *model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
