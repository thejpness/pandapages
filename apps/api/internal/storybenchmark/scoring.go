package storybenchmark

import (
	"fmt"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storyvalidation"
)

type AssessmentScore struct {
	ResultMatched                  bool                             `json:"resultMatched"`
	RequiredFindingsExpected       int                              `json:"requiredFindingsExpected"`
	RequiredFindingsDetected       int                              `json:"requiredFindingsDetected"`
	MissingRequiredFindingCodes    []adaptationcontract.FindingCode `json:"missingRequiredFindingCodes"`
	ForbiddenFindingsChecked       int                              `json:"forbiddenFindingsChecked"`
	ForbiddenFindingsTriggered     int                              `json:"forbiddenFindingsTriggered"`
	TriggeredForbiddenFindingCodes []adaptationcontract.FindingCode `json:"triggeredForbiddenFindingCodes"`
	ExpectationMet                 bool                             `json:"expectationMet"`
}

type Summary struct {
	Trials                     int `json:"trials"`
	ExpectationsMet            int `json:"expectationsMet"`
	ResultMatches              int `json:"resultMatches"`
	RequiredFindingsExpected   int `json:"requiredFindingsExpected"`
	RequiredFindingsDetected   int `json:"requiredFindingsDetected"`
	ForbiddenFindingsChecked   int `json:"forbiddenFindingsChecked"`
	ForbiddenFindingsTriggered int `json:"forbiddenFindingsTriggered"`
}

func ScoreAssessment(expectation AssessmentExpectation, assessment storyvalidation.Assessment) (AssessmentScore, error) {
	if err := expectation.Validate(); err != nil {
		return AssessmentScore{}, fmt.Errorf("benchmark expectation is invalid: %w", err)
	}
	if err := assessment.Validate(); err != nil {
		return AssessmentScore{}, fmt.Errorf("semantic assessment is invalid: %w", err)
	}
	if err := validateAssessmentTargetMatch(expectation, assessment); err != nil {
		return AssessmentScore{}, err
	}

	actualCodes := make(map[adaptationcontract.FindingCode]struct{}, len(assessment.Findings))
	for _, finding := range assessment.Findings {
		actualCodes[finding.Code] = struct{}{}
	}

	score := AssessmentScore{
		ResultMatched:                  assessment.Result == expectation.ExpectedResult,
		RequiredFindingsExpected:       len(expectation.RequiredFindingCodes),
		MissingRequiredFindingCodes:    make([]adaptationcontract.FindingCode, 0),
		ForbiddenFindingsChecked:       len(expectation.ForbiddenFindingCodes),
		TriggeredForbiddenFindingCodes: make([]adaptationcontract.FindingCode, 0),
	}

	for _, code := range expectation.RequiredFindingCodes {
		if _, exists := actualCodes[code]; exists {
			score.RequiredFindingsDetected++
			continue
		}
		score.MissingRequiredFindingCodes = append(score.MissingRequiredFindingCodes, code)
	}
	for _, code := range expectation.ForbiddenFindingCodes {
		if _, exists := actualCodes[code]; !exists {
			continue
		}
		score.ForbiddenFindingsTriggered++
		score.TriggeredForbiddenFindingCodes = append(score.TriggeredForbiddenFindingCodes, code)
	}

	score.ExpectationMet = score.ResultMatched &&
		len(score.MissingRequiredFindingCodes) == 0 &&
		len(score.TriggeredForbiddenFindingCodes) == 0
	return score, nil
}

func Summarize(scores []AssessmentScore) Summary {
	summary := Summary{Trials: len(scores)}
	for _, score := range scores {
		if score.ExpectationMet {
			summary.ExpectationsMet++
		}
		if score.ResultMatched {
			summary.ResultMatches++
		}
		summary.RequiredFindingsExpected += score.RequiredFindingsExpected
		summary.RequiredFindingsDetected += score.RequiredFindingsDetected
		summary.ForbiddenFindingsChecked += score.ForbiddenFindingsChecked
		summary.ForbiddenFindingsTriggered += score.ForbiddenFindingsTriggered
	}
	return summary
}

func validateAssessmentTargetMatch(expectation AssessmentExpectation, assessment storyvalidation.Assessment) error {
	if assessment.AssessmentScope != expectation.AssessmentScope {
		return fmt.Errorf(
			"semantic assessment scope %q does not match benchmark expectation scope %q",
			assessment.AssessmentScope,
			expectation.AssessmentScope,
		)
	}
	if !sameOptionalEditionKey(assessment.EditionKey, expectation.EditionKey) {
		return fmt.Errorf("semantic assessment edition target does not match benchmark expectation")
	}
	if !sameEditionKeys(assessment.EditionKeys, expectation.EditionKeys) {
		return fmt.Errorf("semantic assessment bundle targets do not match benchmark expectation")
	}
	return nil
}

func sameOptionalEditionKey(left, right *model.AdminStoryEditionKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameEditionKeys(left, right []model.AdminStoryEditionKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
