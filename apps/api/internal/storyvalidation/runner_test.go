package storyvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

type semanticGateway struct {
	calls  []storygeneration.ResponsesCall
	result storygeneration.ResponsesResult
	err    error
}

func (gateway *semanticGateway) Create(_ context.Context, call storygeneration.ResponsesCall) (storygeneration.ResponsesResult, error) {
	gateway.calls = append(gateway.calls, call)
	if gateway.err != nil {
		return storygeneration.ResponsesResult{}, gateway.err
	}
	return gateway.result, nil
}

func newValidationRunner(t *testing.T, gateway storygeneration.ResponsesGateway) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{
		Gateway:         gateway,
		Model:           "validator-model-test",
		ReasoningEffort: storygeneration.ReasoningEffortHigh,
		MaxOutputTokens: 6000,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func judgementJSON(t *testing.T, judgement SemanticJudgement) string {
	t.Helper()
	data, err := json.Marshal(judgement)
	if err != nil {
		t.Fatalf("json.Marshal(judgement) error = %v", err)
	}
	return string(data)
}

func TestNewRunnerRequiresExplicitValidatorConfiguration(t *testing.T) {
	gateway := &semanticGateway{}
	tests := []struct {
		name   string
		mutate func(*RunnerConfig)
		want   string
	}{
		{"missing gateway", func(c *RunnerConfig) { c.Gateway = nil }, "gateway is required"},
		{"missing model", func(c *RunnerConfig) { c.Model = " " }, "model is required"},
		{"bad reasoning", func(c *RunnerConfig) { c.ReasoningEffort = "extreme" }, "reasoning effort"},
		{"bad token budget", func(c *RunnerConfig) { c.MaxOutputTokens = 0 }, "max output tokens"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := RunnerConfig{
				Gateway:         gateway,
				Model:           "validator-model-test",
				ReasoningEffort: storygeneration.ReasoningEffortHigh,
				MaxOutputTokens: 6000,
			}
			test.mutate(&cfg)
			_, err := NewRunner(cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewRunner() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateEditionUsesV3JudgementPipeline(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)
	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultNeedsReview,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingScopeTooRich,
				Severity: adaptationcontract.FindingSeverityReview,
				Message:  "The edition retains more material than expected for this level.",
				Evidence: []EvidenceReference{{
					SegmentID:   "gen:growing-readers:p0002",
					Explanation: "The selected generated passage is the material requiring review.",
				}},
			},
		},
	}
	gateway := &semanticGateway{result: storygeneration.ResponsesResult{
		ResponseID: "resp_semantic",
		Model:      "validator-model-returned",
		OutputText: judgementJSON(t, judgement),
		Usage: storygeneration.ResponsesUsage{
			InputTokens:     3000,
			CachedTokens:    500,
			OutputTokens:    600,
			ReasoningTokens: 200,
			TotalTokens:     3600,
		},
	}}

	artifact, err := newValidationRunner(t, gateway).ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		Author:           "Traditional",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if err != nil {
		t.Fatalf("ValidateEdition() error = %v", err)
	}
	if len(gateway.calls) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
	}

	call := gateway.calls[0]
	if call.Model != "validator-model-test" ||
		call.ReasoningEffort != storygeneration.ReasoningEffortHigh ||
		call.MaxOutputTokens != 6000 {
		t.Fatalf("Responses call configuration = %#v", call)
	}
	if call.Prompt.Version != EditionJudgementPromptVersionV3 {
		t.Fatalf("prompt version = %q", call.Prompt.Version)
	}
	if call.StructuredOutput == nil || call.StructuredOutput.Name != editionJudgementSchemaNameV3 {
		t.Fatalf("StructuredOutput = %#v", call.StructuredOutput)
	}
	index, err := BuildEvidenceIndex(source, analysis.Analysis, []storygeneration.GeneratedEditionArtifact{generated})
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}
	assertCallCatalogueAndSchemaMatchIndex(t, call, index)

	if artifact.ValidationVersion != ValidationV3 ||
		artifact.Assessment.ValidationVersion != ValidationV3 ||
		artifact.SpecificationVersion != storygeneration.SpecificationV2 ||
		artifact.PromptVersion != EditionJudgementPromptVersionV3 ||
		artifact.RequestedModel != "validator-model-test" ||
		artifact.ReturnedModel != "validator-model-returned" ||
		artifact.ResponseID != "resp_semantic" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if artifact.Assessment.Findings[0].Evidence[0].Excerpt != "Generated growing-reader edition." ||
		artifact.Assessment.Findings[0].Evidence[0].Location != EvidenceGeneratedEdition ||
		artifact.Assessment.Findings[0].Evidence[0].EditionKey == nil ||
		*artifact.Assessment.Findings[0].Evidence[0].EditionKey != model.AdminStoryEditionGrowingReaders {
		t.Fatalf("resolved evidence = %#v", artifact.Assessment.Findings[0].Evidence[0])
	}
	if artifact.Usage.TotalTokens != 3600 {
		t.Fatalf("usage = %#v", artifact.Usage)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
}

func TestValidateBundleUsesV3JudgementPipeline(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(
		t, source, analysis, model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGrowing edition keeps the secondary scene.",
	)
	explorers := validGeneratedEdition(
		t, source, analysis, model.AdminStoryEditionStoryExplorers,
		"# Jack and the Beanstalk\n\nExplorer edition keeps the secondary scene.",
	)
	keys := []model.AdminStoryEditionKey{
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
	}
	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys:          keys,
		Result:               adaptationcontract.ResultFail,
		Findings: []JudgementFinding{
			{
				Code:     adaptationcontract.FindingEditionProgressionNotDistinct,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "The adjacent editions retain effectively the same secondary scope.",
				Evidence: []EvidenceReference{
					{SegmentID: "gen:growing-readers:p0002", Explanation: "Older target retains the scene."},
					{SegmentID: "gen:story-explorers:p0002", Explanation: "Younger target retains the scene."},
				},
			},
		},
	}
	gateway := &semanticGateway{result: storygeneration.ResponsesResult{
		ResponseID: "resp_bundle",
		Model:      "validator-model-returned",
		OutputText: judgementJSON(t, judgement),
	}}

	artifact, err := newValidationRunner(t, gateway).ValidateBundle(context.Background(), BundleValidationPromptInput{
		Title:             "Jack and the Beanstalk",
		Author:            "Traditional",
		CanonicalSource:   source,
		AnalysisArtifact:  analysis,
		GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing, explorers},
	})
	if err != nil {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
	if len(gateway.calls) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
	}
	call := gateway.calls[0]
	if call.Prompt.Version != BundleJudgementPromptVersionV3 ||
		call.StructuredOutput == nil || call.StructuredOutput.Name != bundleJudgementSchemaNameV3 {
		t.Fatalf("bundle Responses call = %#v", call)
	}
	index, err := BuildEvidenceIndex(source, analysis.Analysis, []storygeneration.GeneratedEditionArtifact{growing, explorers})
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}
	assertCallCatalogueAndSchemaMatchIndex(t, call, index)
	if artifact.ValidationVersion != ValidationV3 ||
		artifact.Assessment.ValidationVersion != ValidationV3 ||
		artifact.PromptVersion != BundleJudgementPromptVersionV3 ||
		!sameEditionKeys(artifact.Assessment.EditionKeys, keys) {
		t.Fatalf("bundle artifact = %#v", artifact)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
}

func TestValidateEditionFailsClosedForV3OutputBoundaries(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t, source, analysis, model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)
	validInput := EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	}
	unknown := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultNeedsReview,
		Findings: []JudgementFinding{{
			Code:     adaptationcontract.FindingScopeTooRich,
			Severity: adaptationcontract.FindingSeverityReview,
			Message:  "Review scope.",
			Evidence: []EvidenceReference{{SegmentID: "gen:growing-readers:p9999", Explanation: "Unknown segment."}},
		}},
	}
	wrongScope := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			model.AdminStoryEditionGrowingReaders,
			model.AdminStoryEditionStoryExplorers,
		},
		Result:   adaptationcontract.ResultPass,
		Findings: []JudgementFinding{},
	}
	wrongTarget := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionStoryExplorers),
		Result:               adaptationcontract.ResultPass,
		Findings:             []JudgementFinding{},
	}
	forbiddenEvidence := `{"validationVersion":"panda-pages-semantic-validation-v3","specificationVersion":"panda-pages-story-adaptation-v2","assessmentScope":"edition","editionKey":"growing-readers","result":"needs_review","findings":[{"code":"scope_too_rich","severity":"review","message":"Review scope.","evidence":[{"segmentId":"gen:growing-readers:p0002","explanation":"Selected text.","excerpt":"model-authored"}]}]}`

	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"unknown segment", judgementJSON(t, unknown), "unknown evidence segment"},
		{"forbidden V2 evidence field", forbiddenEvidence, "contains unknown field"},
		{"wrong scope", judgementJSON(t, wrongScope), "returned scope"},
		{"wrong target", judgementJSON(t, wrongTarget), "wrong edition target"},
		{"malformed output", `{`, "decode edition semantic judgement"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &semanticGateway{result: storygeneration.ResponsesResult{
				ResponseID: "resp_" + strings.ReplaceAll(test.name, " ", "_"),
				Model:      "validator-model-returned",
				OutputText: test.output,
			}}
			_, err := newValidationRunner(t, gateway).ValidateEdition(context.Background(), validInput)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateEdition() error = %v, want substring %q", err, test.want)
			}
			if len(gateway.calls) != 1 {
				t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
			}
		})
	}
}

func TestValidateBundleRejectsWrongScopeAndTargets(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(t, source, analysis, model.AdminStoryEditionGrowingReaders, "# Growing\n\nGrowing edition.")
	explorers := validGeneratedEdition(t, source, analysis, model.AdminStoryEditionStoryExplorers, "# Explorers\n\nExplorer edition.")
	input := BundleValidationPromptInput{
		Title:             "Jack and the Beanstalk",
		CanonicalSource:   source,
		AnalysisArtifact:  analysis,
		GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing, explorers},
	}
	wrongScope := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultPass,
		Findings:             []JudgementFinding{},
	}
	wrongTargets := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			model.AdminStoryEditionConfidentReaders,
			model.AdminStoryEditionGrowingReaders,
		},
		Result:   adaptationcontract.ResultPass,
		Findings: []JudgementFinding{},
	}
	wrongOrder := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			model.AdminStoryEditionStoryExplorers,
			model.AdminStoryEditionGrowingReaders,
		},
		Result:   adaptationcontract.ResultPass,
		Findings: []JudgementFinding{},
	}

	for _, test := range []struct {
		name      string
		judgement SemanticJudgement
		want      string
	}{
		{"wrong scope", wrongScope, "returned scope"},
		{"wrong targets", wrongTargets, "wrong edition targets"},
		{"wrong order", wrongOrder, "canonical modern edition order"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := &semanticGateway{result: storygeneration.ResponsesResult{
				ResponseID: "resp_bundle_bad",
				Model:      "validator-model-returned",
				OutputText: judgementJSON(t, test.judgement),
			}}
			_, err := newValidationRunner(t, gateway).ValidateBundle(context.Background(), input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateBundle() error = %v, want substring %q", err, test.want)
			}
			if len(gateway.calls) != 1 {
				t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
			}
		})
	}
}

func TestValidateRunnerValidatesProvenanceBeforeEvidenceIndex(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t, source, analysis, model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)
	analysis.SourceSHA256 = strings.Repeat("0", 64)
	gateway := &semanticGateway{}

	_, err := newValidationRunner(t, gateway).ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match canonical source") {
		t.Fatalf("ValidateEdition() error = %v", err)
	}
	if len(gateway.calls) != 0 {
		t.Fatalf("gateway calls = %d, want 0", len(gateway.calls))
	}
}

func TestRealArtifactEvidenceVerificationCannotBeBypassedByDifferentIndex(t *testing.T) {
	realSource := "Real canonical source."
	analysis := validAnalysisArtifact(t, realSource)
	realEdition := validGeneratedEdition(
		t, realSource, analysis, model.AdminStoryEditionGrowingReaders,
		"# Growing\n\nReal generated edition.",
	)
	index, err := BuildEvidenceIndex(
		"Different canonical source.",
		analysis.Analysis,
		[]storygeneration.GeneratedEditionArtifact{{
			EditionKey: model.AdminStoryEditionGrowingReaders,
			Markdown:   "# Growing\n\nDifferent generated edition.",
		}},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}
	judgement := SemanticJudgement{
		ValidationVersion:    ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultNeedsReview,
		Findings: []JudgementFinding{{
			Code:     adaptationcontract.FindingScopeTooRich,
			Severity: adaptationcontract.FindingSeverityReview,
			Message:  "Review scope.",
			Evidence: []EvidenceReference{{SegmentID: "src:p0001", Explanation: "Different source evidence."}},
		}},
	}
	assessment, err := ResolveSemanticJudgement(judgement, index)
	if err != nil {
		t.Fatalf("ResolveSemanticJudgement() error = %v", err)
	}
	if err := validateAssessmentEvidence(assessment, realSource, analysis.Analysis, []storygeneration.GeneratedEditionArtifact{realEdition}); err == nil || !strings.Contains(err.Error(), "not present in canonical source") {
		t.Fatalf("validateAssessmentEvidence() error = %v", err)
	}
}

func TestAssessmentArtifactValidationVersionMatrix(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(t, source, analysis, model.AdminStoryEditionGrowingReaders, "# Growing\n\nGrowing edition.")
	explorers := validGeneratedEdition(t, source, analysis, model.AdminStoryEditionStoryExplorers, "# Explorers\n\nExplorer edition.")
	v2Edition := buildRunnerTestArtifact(t, ValidationV2, adaptationcontract.AssessmentScopeEdition, []storygeneration.GeneratedEditionArtifact{growing}, EditionValidationPromptVersionV2, analysis)
	v3Edition := buildRunnerTestArtifact(t, ValidationV3, adaptationcontract.AssessmentScopeEdition, []storygeneration.GeneratedEditionArtifact{growing}, EditionJudgementPromptVersionV3, analysis)
	v3Bundle := buildRunnerTestArtifact(t, ValidationV3, adaptationcontract.AssessmentScopeBundle, []storygeneration.GeneratedEditionArtifact{growing, explorers}, BundleJudgementPromptVersionV3, analysis)
	if err := v2Edition.Validate(); err != nil {
		t.Fatalf("historical V2 artifact.Validate() error = %v", err)
	}
	if err := v3Edition.Validate(); err != nil {
		t.Fatalf("V3 edition artifact.Validate() error = %v", err)
	}
	if err := v3Bundle.Validate(); err != nil {
		t.Fatalf("V3 bundle artifact.Validate() error = %v", err)
	}

	v2OuterV3Nested := v2Edition
	v2OuterV3Nested.Assessment.ValidationVersion = ValidationV3
	v2OuterV3Nested.AssessmentSHA256 = runnerTestAssessmentDigest(t, v2OuterV3Nested.Assessment)
	v3OuterV2Nested := v3Edition
	v3OuterV2Nested.Assessment.ValidationVersion = ValidationV2
	v3OuterV2Nested.AssessmentSHA256 = runnerTestAssessmentDigest(t, v3OuterV2Nested.Assessment)
	v2V3Prompt := v2Edition
	v2V3Prompt.PromptVersion = EditionJudgementPromptVersionV3
	v3V2Prompt := v3Edition
	v3V2Prompt.PromptVersion = EditionValidationPromptVersionV2
	editionBundlePrompt := v2Edition
	editionBundlePrompt.PromptVersion = BundleValidationPromptVersionV2
	bundleEditionPrompt := v3Bundle
	bundleEditionPrompt.PromptVersion = EditionJudgementPromptVersionV3
	unknownVersion := v3Edition
	unknownVersion.ValidationVersion = "unknown"

	for _, test := range []struct {
		name     string
		artifact AssessmentArtifact
		want     string
	}{
		{"V2 outer with V3 nested", v2OuterV3Nested, "assessment validation version"},
		{"V3 outer with V2 nested", v3OuterV2Nested, "assessment validation version"},
		{"V2 artifact with V3 prompt", v2V3Prompt, "prompt version"},
		{"V3 artifact with V2 prompt", v3V2Prompt, "prompt version"},
		{"edition artifact with bundle prompt", editionBundlePrompt, "prompt version"},
		{"bundle artifact with edition prompt", bundleEditionPrompt, "prompt version"},
		{"unknown artifact version", unknownVersion, "validation version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.artifact.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("AssessmentArtifact.Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidationRunnerPreservesGatewayFailures(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(t, source, analysis, model.AdminStoryEditionGrowingReaders, "# Growing\n\nGenerated edition.")
	input := EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	}
	sentinel := errors.New("provider failed")
	gateway := &semanticGateway{err: sentinel}
	_, err := newValidationRunner(t, gateway).ValidateEdition(context.Background(), input)
	if !errors.Is(err, sentinel) || len(gateway.calls) != 1 {
		t.Fatalf("gateway error = %v, calls = %d", err, len(gateway.calls))
	}

	gateway = &semanticGateway{result: storygeneration.ResponsesResult{
		ResponseID: "resp_empty",
		Model:      "validator-model-returned",
		OutputText: "",
	}}
	_, err = newValidationRunner(t, gateway).ValidateEdition(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "decode edition semantic judgement") || len(gateway.calls) != 1 {
		t.Fatalf("empty output error = %v, calls = %d", err, len(gateway.calls))
	}
}

func assertCallCatalogueAndSchemaMatchIndex(t *testing.T, call storygeneration.ResponsesCall, index EvidenceIndex) {
	t.Helper()
	var promptInput struct {
		EvidenceCatalogue []evidenceCatalogueEntry `json:"evidenceCatalogue"`
	}
	if err := json.Unmarshal([]byte(call.Prompt.UserInputJSON), &promptInput); err != nil {
		t.Fatalf("json.Unmarshal(prompt user input) error = %v", err)
	}
	segments := index.Segments()
	if len(promptInput.EvidenceCatalogue) != len(segments) {
		t.Fatalf("prompt catalogue length = %d, want %d", len(promptInput.EvidenceCatalogue), len(segments))
	}
	wantIDs := make([]string, 0, len(segments))
	for position, segment := range segments {
		if promptInput.EvidenceCatalogue[position].SegmentID != segment.ID ||
			promptInput.EvidenceCatalogue[position].Text != segment.Text {
			t.Fatalf("prompt catalogue entry %d = %#v, want %#v", position, promptInput.EvidenceCatalogue[position], segment)
		}
		wantIDs = append(wantIDs, string(segment.ID))
	}

	var schema map[string]any
	if err := json.Unmarshal(call.StructuredOutput.Schema, &schema); err != nil {
		t.Fatalf("json.Unmarshal(StructuredOutput.Schema) error = %v", err)
	}
	properties := schema["properties"].(map[string]any)
	findings := properties["findings"].(map[string]any)
	findingItems := findings["items"].(map[string]any)
	evidence := findingItems["properties"].(map[string]any)["evidence"].(map[string]any)
	evidenceItems := evidence["items"].(map[string]any)
	segmentID := evidenceItems["properties"].(map[string]any)["segmentId"].(map[string]any)
	values := segmentID["enum"].([]any)
	gotIDs := make([]string, 0, len(values))
	for _, value := range values {
		gotIDs = append(gotIDs, value.(string))
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("schema segment IDs = %v, want %v", gotIDs, wantIDs)
	}
}

func buildRunnerTestArtifact(
	t *testing.T,
	version ValidationVersion,
	scope adaptationcontract.AssessmentScope,
	editions []storygeneration.GeneratedEditionArtifact,
	promptVersion storygeneration.PromptVersion,
	analysis storygeneration.StoryAnalysisArtifact,
) AssessmentArtifact {
	t.Helper()
	assessment := Assessment{
		ValidationVersion:    version,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      scope,
		Result:               adaptationcontract.ResultPass,
		Findings:             []Finding{},
	}
	switch scope {
	case adaptationcontract.AssessmentScopeEdition:
		assessment.EditionKey = editionKey(editions[0].EditionKey)
	case adaptationcontract.AssessmentScopeBundle:
		assessment.EditionKeys = make([]model.AdminStoryEditionKey, 0, len(editions))
		for _, edition := range editions {
			assessment.EditionKeys = append(assessment.EditionKeys, edition.EditionKey)
		}
	}
	artifact, err := buildAssessmentArtifact(
		promptVersion,
		"validator-model-test",
		storygeneration.ReasoningEffortHigh,
		storygeneration.ResponsesResult{ResponseID: "resp_artifact", Model: "validator-model-returned"},
		analysis,
		editions,
		assessment,
	)
	if err != nil {
		t.Fatalf("buildAssessmentArtifact() error = %v", err)
	}
	return artifact
}

func runnerTestAssessmentDigest(t *testing.T, assessment Assessment) string {
	t.Helper()
	digest, err := assessmentSHA256(assessment)
	if err != nil {
		t.Fatalf("assessmentSHA256() error = %v", err)
	}
	return digest
}
