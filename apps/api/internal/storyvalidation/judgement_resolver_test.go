package storyvalidation

import (
	"encoding/json"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func TestResolveSemanticJudgementDeterministicallyBuildsExactEvidence(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	canonicalSource := "First canonical block.\r\n\r\n  Canonical block with surrounding spaces.  \r\n"
	generatedMarkdown := "# Growing\r\n\r\n  Generated block with surrounding spaces.  \r\n"
	index, err := BuildEvidenceIndex(
		canonicalSource,
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: growing,
				Markdown:   generatedMarkdown,
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

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
				Message:  "The generated edition changes a source-grounded motivation.",
				Evidence: []EvidenceReference{
					{
						SegmentID:   "src:p0002",
						Explanation: "Source explanation remains model-authored.",
					},
					{
						SegmentID:   "ana:characters:0:explicitMotivations:0",
						Explanation: "Analysis explanation remains model-authored.",
					},
					{
						SegmentID:   "gen:growing-readers:p0002",
						Explanation: "Generated explanation remains model-authored.",
					},
				},
			},
		},
	}

	assessment, err := ResolveSemanticJudgement(judgement, index)
	if err != nil {
		t.Fatalf("ResolveSemanticJudgement() error = %v", err)
	}
	if err := assessment.Validate(); err != nil {
		t.Fatalf("resolved Assessment.Validate() error = %v", err)
	}

	if assessment.ValidationVersion != ValidationV3 ||
		assessment.SpecificationVersion != judgement.SpecificationVersion ||
		assessment.AssessmentScope != judgement.AssessmentScope ||
		assessment.EditionKey == nil || *assessment.EditionKey != growing ||
		assessment.Result != judgement.Result {
		t.Fatalf("resolved assessment envelope = %#v", assessment)
	}
	if len(assessment.Findings) != 1 {
		t.Fatalf("resolved findings = %d, want 1", len(assessment.Findings))
	}

	finding := assessment.Findings[0]
	if finding.Code != judgement.Findings[0].Code ||
		finding.Severity != judgement.Findings[0].Severity ||
		finding.Message != judgement.Findings[0].Message {
		t.Fatalf("resolved finding = %#v", finding)
	}
	if len(finding.Evidence) != len(judgement.Findings[0].Evidence) {
		t.Fatalf("resolved evidence = %d, want %d", len(finding.Evidence), len(judgement.Findings[0].Evidence))
	}

	tests := []struct {
		name       string
		id         EvidenceSegmentID
		location   EvidenceLocation
		editionKey *model.AdminStoryEditionKey
		excerpt    string
	}{
		{
			name:     "canonical source",
			id:       "src:p0002",
			location: EvidenceCanonicalSource,
			excerpt:  "  Canonical block with surrounding spaces.  ",
		},
		{
			name:     "StoryAnalysis",
			id:       "ana:characters:0:explicitMotivations:0",
			location: EvidenceStoryAnalysis,
			excerpt:  "Keep the harbour light burning.",
		},
		{
			name:       "generated edition",
			id:         "gen:growing-readers:p0002",
			location:   EvidenceGeneratedEdition,
			editionKey: &growing,
			excerpt:    "  Generated block with surrounding spaces.  ",
		},
	}

	for evidenceIndex, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			segment, err := index.Resolve(test.id)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", test.id, err)
			}
			evidence := finding.Evidence[evidenceIndex]
			if evidence.Location != test.location ||
				!sameEvidenceEditionKey(evidence.EditionKey, test.editionKey) ||
				evidence.Excerpt != test.excerpt ||
				evidence.Excerpt != segment.Text ||
				evidence.Explanation != judgement.Findings[0].Evidence[evidenceIndex].Explanation {
				t.Fatalf("resolved evidence = %#v, segment = %#v", evidence, segment)
			}
		})
	}
}

func TestResolveSemanticJudgementPreservesBundleTargetAndPass(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	explorers := model.AdminStoryEditionStoryExplorers
	index, err := BuildEvidenceIndex(
		"Canonical paragraph.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{EditionKey: growing, Markdown: "# Growing\n\nGrowing paragraph."},
			{EditionKey: explorers, Markdown: "# Explorers\n\nExplorers paragraph."},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	bundle := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys:          []model.AdminStoryEditionKey{growing, explorers},
		Result:               adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingEditionProgressionNotDistinct,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "The two editions retain the same material.",
				Evidence: []EvidenceReference{
					{SegmentID: "gen:growing-readers:p0002", Explanation: "Older target."},
					{SegmentID: "gen:story-explorers:p0002", Explanation: "Younger target."},
				},
			},
		},
	}
	assessment, err := ResolveSemanticJudgement(bundle, index)
	if err != nil {
		t.Fatalf("ResolveSemanticJudgement(bundle) error = %v", err)
	}
	if !sameEditionKeys(assessment.EditionKeys, bundle.EditionKeys) || assessment.Result != bundle.Result {
		t.Fatalf("resolved bundle assessment = %#v", assessment)
	}

	pass := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &growing,
		Result:               adaptationcontract.ResultPass,
		Findings:             []JudgementFinding{},
	}
	assessment, err = ResolveSemanticJudgement(pass, index)
	if err != nil {
		t.Fatalf("ResolveSemanticJudgement(pass) error = %v", err)
	}
	if assessment.ValidationVersion != ValidationV3 || assessment.Result != adaptationcontract.ResultPass || len(assessment.Findings) != 0 {
		t.Fatalf("resolved pass assessment = %#v", assessment)
	}
}

func TestResolveSemanticJudgementFailsClosed(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	explorers := model.AdminStoryEditionStoryExplorers
	index, err := BuildEvidenceIndex(
		"Canonical paragraph.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{EditionKey: growing, Markdown: "# Growing\n\nGrowing paragraph."},
			{EditionKey: explorers, Markdown: "# Explorers\n\nExplorers paragraph."},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	valid := func(segmentID EvidenceSegmentID) SemanticJudgement {
		return SemanticJudgement{
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
					Evidence: []EvidenceReference{{SegmentID: segmentID, Explanation: "Evidence."}},
				},
			},
		}
	}

	tests := []struct {
		name      string
		judgement SemanticJudgement
		want      string
	}{
		{
			name:      "unknown exact segment ID has no fallback",
			judgement: valid("gen:growing-readers:p0002-approximate"),
			want:      "unknown evidence segment",
		},
		{
			name:      "generated segment outside target",
			judgement: valid("gen:story-explorers:p0002"),
			want:      "outside the judgement target",
		},
		{
			name: "invalid judgement fails before resolution",
			judgement: SemanticJudgement{
				ValidationVersion: "invalid",
			},
			want: "validation version",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveSemanticJudgement(test.judgement, index)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ResolveSemanticJudgement() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestAssessmentValidationVersionsKeepDecoderV2Only(t *testing.T) {
	assessment := validFailAssessment()
	if err := assessment.Validate(); err != nil {
		t.Fatalf("v2 Assessment.Validate() error = %v", err)
	}

	v2JSON, err := json.Marshal(assessment)
	if err != nil {
		t.Fatalf("json.Marshal(v2 assessment) error = %v", err)
	}
	if _, err := DecodeAssessmentJSON(v2JSON); err != nil {
		t.Fatalf("DecodeAssessmentJSON(v2) error = %v", err)
	}

	assessment.ValidationVersion = ValidationV3
	if err := assessment.Validate(); err != nil {
		t.Fatalf("v3 Assessment.Validate() error = %v", err)
	}

	v3JSON, err := json.Marshal(assessment)
	if err != nil {
		t.Fatalf("json.Marshal(v3 assessment) error = %v", err)
	}
	_, err = DecodeAssessmentJSON(v3JSON)
	if err == nil || !strings.Contains(err.Error(), "validation version must equal") {
		t.Fatalf("DecodeAssessmentJSON(v3) error = %v, want v2-only failure", err)
	}
}
