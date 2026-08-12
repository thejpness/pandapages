package storybenchmark

import (
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

func TestAssessmentExpectationValidate(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	confident := model.AdminStoryEditionConfidentReaders

	tests := []struct {
		name        string
		expectation AssessmentExpectation
		wantError   string
	}{
		{
			name: "valid edition expectation",
			expectation: AssessmentExpectation{
				AssessmentScope:       adaptationcontract.AssessmentScopeEdition,
				EditionKey:            &growing,
				ExpectedResult:        adaptationcontract.ResultFail,
				RequiredFindingCodes:  []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
				ForbiddenFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingInventedMoralising},
			},
		},
		{
			name: "valid bundle expectation",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
				EditionKeys:          []model.AdminStoryEditionKey{confident, growing},
				ExpectedResult:       adaptationcontract.ResultFail,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingEditionProgressionNotDistinct},
			},
		},
		{
			name: "invalid target",
			expectation: AssessmentExpectation{
				AssessmentScope: adaptationcontract.AssessmentScopeEdition,
				ExpectedResult:  adaptationcontract.ResultPass,
			},
			wantError: "assessment target is invalid",
		},
		{
			name: "unknown result",
			expectation: AssessmentExpectation{
				AssessmentScope: adaptationcontract.AssessmentScopeEdition,
				EditionKey:      &growing,
				ExpectedResult:  adaptationcontract.Result("maybe"),
			},
			wantError: "unsupported expected result",
		},
		{
			name: "structural finding rejected",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &growing,
				ExpectedResult:       adaptationcontract.ResultFail,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingInvalidUTF8},
			},
			wantError: "structural, not semantic",
		},
		{
			name: "edition finding rejected for bundle",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
				EditionKeys:          []model.AdminStoryEditionKey{confident, growing},
				ExpectedResult:       adaptationcontract.ResultFail,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
			},
			wantError: "invalid for the benchmark target",
		},
		{
			name: "bundle finding rejected for edition",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &growing,
				ExpectedResult:       adaptationcontract.ResultFail,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingEditionProgressionInverted},
			},
			wantError: "invalid for the benchmark target",
		},
		{
			name: "duplicate required finding rejected",
			expectation: AssessmentExpectation{
				AssessmentScope: adaptationcontract.AssessmentScopeEdition,
				EditionKey:      &growing,
				ExpectedResult:  adaptationcontract.ResultFail,
				RequiredFindingCodes: []adaptationcontract.FindingCode{
					adaptationcontract.FindingMotivationChanged,
					adaptationcontract.FindingMotivationChanged,
				},
			},
			wantError: "duplicates",
		},
		{
			name: "overlapping required and forbidden rejected",
			expectation: AssessmentExpectation{
				AssessmentScope:       adaptationcontract.AssessmentScopeEdition,
				EditionKey:            &growing,
				ExpectedResult:        adaptationcontract.ResultFail,
				RequiredFindingCodes:  []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
				ForbiddenFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
			},
			wantError: "both required and forbidden",
		},
		{
			name: "pass cannot require a finding",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &growing,
				ExpectedResult:       adaptationcontract.ResultPass,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingScopeTooRich},
			},
			wantError: "pass expectation cannot require findings",
		},
		{
			name: "needs review cannot require blocking finding",
			expectation: AssessmentExpectation{
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &growing,
				ExpectedResult:       adaptationcontract.ResultNeedsReview,
				RequiredFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
			},
			wantError: "cannot require blocking finding",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.expectation.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestBenchmarkVersionIsStable(t *testing.T) {
	if VersionV1 != "panda-pages-story-benchmark-v1" {
		t.Fatalf("VersionV1 = %q", VersionV1)
	}
}
