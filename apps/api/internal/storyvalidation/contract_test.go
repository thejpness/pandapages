package storyvalidation

import (
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func editionKey(key model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	value := key
	return &value
}

func validFailAssessment() Assessment {
	return Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultFail,
		Findings: []Finding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "Jack is given a kinder motivation than the source supplies.",
				Evidence: []Evidence{
					{
						Location:    EvidenceCanonicalSource,
						Excerpt:     "Jack agrees to trade the cow for the beans.",
						Explanation: "The source does not give Jack an altruistic reason for the trade.",
					},
					{
						Location:    EvidenceGeneratedEdition,
						EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
						Excerpt:     "Jack wanted to save his mother from worry.",
						Explanation: "The generated edition invents a nobler motive.",
					},
				},
			},
		},
	}
}

func TestAssessmentValidateAcceptsPassReviewAndFail(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		assessment := Assessment{
			ValidationVersion:    ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           editionKey(model.AdminStoryEditionConfidentReaders),
			Result:               adaptationcontract.ResultPass,
			Findings:             []Finding{},
		}
		if err := assessment.Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})

	t.Run("needs review", func(t *testing.T) {
		assessment := Assessment{
			ValidationVersion:    ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           editionKey(model.AdminStoryEditionStoryExplorers),
			Result:               adaptationcontract.ResultNeedsReview,
			Findings: []Finding{
				{
					Code:     adaptationcontract.FindingScopeTooRich,
					Severity: adaptationcontract.FindingSeverityReview,
					Message:  "The edition retains more secondary description than expected.",
					Evidence: []Evidence{
						{
							Location:    EvidenceGeneratedEdition,
							EditionKey:  editionKey(model.AdminStoryEditionStoryExplorers),
							Excerpt:     "A long descriptive passage remains.",
							Explanation: "This passage is incidental to the essential narrative.",
						},
					},
				},
			},
		}
		if err := assessment.Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})

	t.Run("fail", func(t *testing.T) {
		if err := validFailAssessment().Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})

	t.Run("bundle", func(t *testing.T) {
		assessment := Assessment{
			ValidationVersion:    ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
			EditionKeys: []model.AdminStoryEditionKey{
				model.AdminStoryEditionGrowingReaders,
				model.AdminStoryEditionStoryExplorers,
			},
			Result: adaptationcontract.ResultFail,
			Findings: []Finding{
				{
					Code:     adaptationcontract.FindingEditionProgressionNotDistinct,
					Severity: adaptationcontract.FindingSeverityBlocking,
					Message:  "The adjacent editions are not materially different in narrative scope.",
					Evidence: []Evidence{
						{
							Location:    EvidenceGeneratedEdition,
							EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
							Excerpt:     "The same secondary scene is retained in full.",
							Explanation: "The older edition retains the scene.",
						},
						{
							Location:    EvidenceGeneratedEdition,
							EditionKey:  editionKey(model.AdminStoryEditionStoryExplorers),
							Excerpt:     "The same secondary scene is retained in full.",
							Explanation: "The younger edition preserves effectively the same scope.",
						},
					},
				},
			},
		}
		if err := assessment.Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})
}

func TestAssessmentValidateRejectsInvalidContractAndEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Assessment)
		want   string
	}{
		{
			name: "wrong validation version",
			mutate: func(a *Assessment) {
				a.ValidationVersion = "future"
			},
			want: "validation version",
		},
		{
			name: "wrong specification version",
			mutate: func(a *Assessment) {
				a.SpecificationVersion = "future"
			},
			want: "specification version",
		},
		{
			name: "finding without evidence",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence = nil
			},
			want: "evidence is required",
		},
		{
			name: "structural code rejected",
			mutate: func(a *Assessment) {
				a.Findings[0].Code = adaptationcontract.FindingRawHTMLPresent
				a.Findings[0].Severity = adaptationcontract.FindingSeverityBlocking
			},
			want: "structural",
		},
		{
			name: "canonical severity enforced",
			mutate: func(a *Assessment) {
				a.Findings[0].Severity = adaptationcontract.FindingSeverityReview
			},
			want: "severity",
		},
		{
			name: "result invariant enforced",
			mutate: func(a *Assessment) {
				a.Result = adaptationcontract.ResultPass
			},
			want: "pass assessments",
		},
		{
			name: "unsupported evidence location",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[0].Location = "memory"
			},
			want: "unsupported evidence location",
		},
		{
			name: "source evidence cannot name edition",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[0].EditionKey = editionKey(model.AdminStoryEditionGrowingReaders)
			},
			want: "must not contain editionKey",
		},
		{
			name: "generated evidence requires edition",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[1].EditionKey = nil
			},
			want: "requires editionKey",
		},
		{
			name: "generated evidence target must match",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[1].EditionKey = editionKey(model.AdminStoryEditionLittleListeners)
			},
			want: "outside the assessment target",
		},
		{
			name: "blank excerpt rejected",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[0].Excerpt = " "
			},
			want: "excerpt is required",
		},
		{
			name: "blank explanation rejected",
			mutate: func(a *Assessment) {
				a.Findings[0].Evidence[0].Explanation = " "
			},
			want: "explanation is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := validFailAssessment()
			test.mutate(&assessment)
			err := assessment.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestAssessmentValidateDelegatesTargetRulesToPR91Contract(t *testing.T) {
	assessment := validFailAssessment()
	assessment.AssessmentScope = adaptationcontract.AssessmentScopeBundle
	assessment.EditionKey = nil
	assessment.EditionKeys = []model.AdminStoryEditionKey{
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionGrowingReaders,
	}
	assessment.Findings[0].Code = adaptationcontract.FindingEditionProgressionNotDistinct
	assessment.Findings[0].Message = "Scope ordering is inverted."
	assessment.Findings[0].Evidence = []Evidence{
		{
			Location:    EvidenceGeneratedEdition,
			EditionKey:  editionKey(model.AdminStoryEditionStoryExplorers),
			Excerpt:     "Younger edition excerpt.",
			Explanation: "Evidence for the younger target.",
		},
	}

	err := assessment.Validate()
	if err == nil || !strings.Contains(err.Error(), "canonical modern edition order") {
		t.Fatalf("Validate() error = %v, want canonical order failure", err)
	}
}
