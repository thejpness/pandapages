package storyvalidation

import (
	"encoding/json"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func TestSemanticJudgementValidateAcceptsSegmentReferences(t *testing.T) {
	key := model.AdminStoryEditionGrowingReaders
	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &key,
		Result:               adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "The generated edition changes Mara's motivation.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "ana:characters:0:explicitMotivations:0",
						Explanation: "The analysis records Mara's source-grounded motivation.",
					},
					{
						SegmentID:   "gen:growing-readers:p0002",
						Explanation: "The edition gives Mara a different motivation.",
					},
				},
			},
		},
	}

	if err := judgement.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSemanticJudgementValidateRejectsMissingReferenceFields(t *testing.T) {
	key := model.AdminStoryEditionGrowingReaders

	tests := []struct {
		name      string
		reference EvidenceReference
		want      string
	}{
		{
			name: "missing segment ID",
			reference: EvidenceReference{
				Explanation: "Explanation.",
			},
			want: "segmentId is required",
		},
		{
			name: "missing explanation",
			reference: EvidenceReference{
				SegmentID: "src:p0001",
			},
			want: "explanation is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			judgement := SemanticJudgement{
				ValidationVersion:    ValidationV3,
				SpecificationVersion: storygeneration.SpecificationV2,
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &key,
				Result:               adaptationcontract.ResultFail,
				Findings: []JudgementFinding{
					{
						Code:     adaptationcontract.FindingMotivationChanged,
						Severity: adaptationcontract.FindingSeverityBlocking,
						Message:  "Changed motivation.",
						Evidence: []EvidenceReference{test.reference},
					},
				},
			}

			err := judgement.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestSemanticJudgementValidateReusesPR91SemanticContract(t *testing.T) {
	key := model.AdminStoryEditionGrowingReaders

	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &key,
		Result:               adaptationcontract.ResultNeedsReview,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "Changed motivation.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "src:p0001",
						Explanation: "Evidence.",
					},
				},
			},
		},
	}

	err := judgement.Validate()
	if err == nil || !strings.Contains(err.Error(), "needs_review") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSemanticJudgementValidateAgainstEvidenceIndexFailsClosed(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	explorers := model.AdminStoryEditionStoryExplorers

	index, err := BuildEvidenceIndex(
		"Canonical paragraph.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: growing,
				Markdown:   "# Growing\n\nGrowing paragraph.",
			},
			{
				EditionKey: explorers,
				Markdown:   "# Explorers\n\nExplorers paragraph.",
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	tests := []struct {
		name      string
		segmentID EvidenceSegmentID
		want      string
	}{
		{
			name:      "unknown segment",
			segmentID: "src:p9999",
			want:      "unknown evidence segment",
		},
		{
			name:      "wrong generated edition",
			segmentID: "gen:story-explorers:p0002",
			want:      "outside the judgement target",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			judgement := SemanticJudgement{
				ValidationVersion:    ValidationV3,
				SpecificationVersion: storygeneration.SpecificationV2,
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &growing,
				Result:               adaptationcontract.ResultFail,
				Findings: []JudgementFinding{
					{
						Code:     adaptationcontract.FindingMotivationChanged,
						Severity: adaptationcontract.FindingSeverityBlocking,
						Message:  "Changed motivation.",
						Evidence: []EvidenceReference{
							{
								SegmentID:   test.segmentID,
								Explanation: "Evidence.",
							},
						},
					},
				},
			}

			err := judgement.ValidateAgainstEvidenceIndex(index)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"ValidateAgainstEvidenceIndex() error = %v, want substring %q",
					err,
					test.want,
				)
			}
		})
	}

	valid := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &growing,
		Result:               adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "Changed motivation.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "src:p0001",
						Explanation: "Source evidence.",
					},
					{
						SegmentID:   "gen:growing-readers:p0002",
						Explanation: "Generated evidence.",
					},
				},
			},
		},
	}

	if err := valid.ValidateAgainstEvidenceIndex(index); err != nil {
		t.Fatalf("valid ValidateAgainstEvidenceIndex() error = %v", err)
	}
}

func TestSemanticJudgementValidateAgainstEvidenceIndexBoundsBundleReferences(t *testing.T) {
	confident := model.AdminStoryEditionConfidentReaders
	growing := model.AdminStoryEditionGrowingReaders
	explorers := model.AdminStoryEditionStoryExplorers

	index, err := BuildEvidenceIndex(
		"Canonical paragraph.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: confident,
				Markdown:   "# Confident\n\nConfident paragraph.",
			},
			{
				EditionKey: growing,
				Markdown:   "# Growing\n\nGrowing paragraph.",
			},
			{
				EditionKey: explorers,
				Markdown:   "# Explorers\n\nExplorers paragraph.",
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			growing,
			explorers,
		},
		Result: adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingEditionProgressionNotDistinct,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "The two target editions retain the same scope.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "gen:growing-readers:p0002",
						Explanation: "The older target retains the material.",
					},
					{
						SegmentID:   "gen:story-explorers:p0002",
						Explanation: "The younger target also retains the material.",
					},
				},
			},
		},
	}

	if err := judgement.ValidateAgainstEvidenceIndex(index); err != nil {
		t.Fatalf("ValidateAgainstEvidenceIndex() error = %v", err)
	}

	judgement.Findings[0].Evidence = append(judgement.Findings[0].Evidence,
		EvidenceReference{
			SegmentID:   "gen:confident-readers:p0002",
			Explanation: "This reference is outside the declared bundle target.",
		},
	)

	err = judgement.ValidateAgainstEvidenceIndex(index)
	if err == nil || !strings.Contains(err.Error(), "outside the judgement target") {
		t.Fatalf("ValidateAgainstEvidenceIndex() error = %v, want bundle-target failure", err)
	}
}

func TestDecodeSemanticJudgementJSONUsesStrictBoundary(t *testing.T) {
	key := model.AdminStoryEditionGrowingReaders

	valid := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &key,
		Result:               adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "Changed motivation.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "src:p0001",
						Explanation: "Source evidence.",
					},
				},
			},
		},
	}

	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("json.Marshal(valid) error = %v", err)
	}

	decoded, err := DecodeSemanticJudgementJSON(data)
	if err != nil {
		t.Fatalf("DecodeSemanticJudgementJSON() error = %v", err)
	}
	if decoded.ValidationVersion != ValidationV3 {
		t.Fatalf("ValidationVersion = %q", decoded.ValidationVersion)
	}

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "unknown evidence field",
			data: []byte(strings.Replace(
				string(data),
				`"segmentId":"src:p0001"`,
				`"segmentId":"src:p0001","excerpt":"fabricated"`,
				1,
			)),
			want: `unknown field "excerpt"`,
		},
		{
			name: "model-authored evidence location",
			data: []byte(strings.Replace(
				string(data),
				`"segmentId":"src:p0001"`,
				`"segmentId":"src:p0001","location":"canonical_source"`,
				1,
			)),
			want: `unknown field "location"`,
		},
		{
			name: "model-authored evidence edition key",
			data: []byte(strings.Replace(
				string(data),
				`"segmentId":"src:p0001"`,
				`"segmentId":"src:p0001","editionKey":"growing-readers"`,
				1,
			)),
			want: `unknown field "editionKey"`,
		},
		{
			name: "duplicate segment ID",
			data: []byte(strings.Replace(
				string(data),
				`"segmentId":"src:p0001"`,
				`"segmentId":"src:p0001","segmentId":"src:p0002"`,
				1,
			)),
			want: `duplicate object key "segmentId"`,
		},
		{
			name: "trailing value",
			data: append(append([]byte(nil), data...), []byte(` {}`)...),
			want: "trailing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeSemanticJudgementJSON(test.data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"DecodeSemanticJudgementJSON() error = %v, want substring %q",
					err,
					test.want,
				)
			}
		})
	}
}

func TestDecodeSemanticJudgementJSONArrayBoundaries(t *testing.T) {
	const editionKey = "growing-readers"

	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name: "pass allows empty findings",
			data: `{"validationVersion":"panda-pages-semantic-validation-v3","specificationVersion":"panda-pages-adaptation-v2","assessmentScope":"edition","editionKey":"` + editionKey + `","result":"pass","findings":[]}`,
		},
		{
			name:    "null findings",
			data:    `{"validationVersion":"panda-pages-semantic-validation-v3","specificationVersion":"panda-pages-adaptation-v2","assessmentScope":"edition","editionKey":"` + editionKey + `","result":"pass","findings":null}`,
			wantErr: "findings must be a JSON array",
		},
		{
			name:    "null finding evidence",
			data:    `{"validationVersion":"panda-pages-semantic-validation-v3","specificationVersion":"panda-pages-adaptation-v2","assessmentScope":"edition","editionKey":"` + editionKey + `","result":"fail","findings":[{"code":"motivation_changed","severity":"blocking","message":"Changed motivation.","evidence":null}]}`,
			wantErr: "findings[0].evidence must be a JSON array",
		},
		{
			name:    "empty finding evidence",
			data:    `{"validationVersion":"panda-pages-semantic-validation-v3","specificationVersion":"panda-pages-adaptation-v2","assessmentScope":"edition","editionKey":"` + editionKey + `","result":"fail","findings":[{"code":"motivation_changed","severity":"blocking","message":"Changed motivation.","evidence":[]}]}`,
			wantErr: "finding 1: evidence is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeSemanticJudgementJSON([]byte(test.data))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("DecodeSemanticJudgementJSON() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf(
					"DecodeSemanticJudgementJSON() error = %v, want substring %q",
					err,
					test.wantErr,
				)
			}
		})
	}
}
