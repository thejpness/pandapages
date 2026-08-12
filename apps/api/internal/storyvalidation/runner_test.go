package storyvalidation

import (
	"context"
	"encoding/json"
	"errors"
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

func assessmentJSON(t *testing.T, assessment Assessment) string {
	t.Helper()
	data, err := json.Marshal(assessment)
	if err != nil {
		t.Fatalf("json.Marshal(assessment) error = %v", err)
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

func TestValidateEditionUsesConfiguredModelAndStrictSchema(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)

	assessment := Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultNeedsReview,
		Findings: []Finding{
			{
				Code:     adaptationcontract.FindingScopeTooRich,
				Severity: adaptationcontract.FindingSeverityReview,
				Message:  "The edition retains more material than expected for this level.",
				Evidence: []Evidence{
					{
						Location:    EvidenceGeneratedEdition,
						EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
						Excerpt:     "Generated growing-reader edition.",
						Explanation: "The supplied generated passage is the material requiring review.",
					},
				},
			},
		},
	}

	gateway := &semanticGateway{
		result: storygeneration.ResponsesResult{
			ResponseID: "resp_semantic",
			Model:      "validator-model-returned",
			OutputText: assessmentJSON(t, assessment),
			Usage: storygeneration.ResponsesUsage{
				InputTokens:     3000,
				CachedTokens:    500,
				OutputTokens:    600,
				ReasoningTokens: 200,
				TotalTokens:     3600,
			},
		},
	}
	runner := newValidationRunner(t, gateway)

	artifact, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
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
	if call.Model != "validator-model-test" {
		t.Fatalf("model = %q", call.Model)
	}
	if call.ReasoningEffort != storygeneration.ReasoningEffortHigh {
		t.Fatalf("reasoning effort = %q", call.ReasoningEffort)
	}
	if call.MaxOutputTokens != 6000 {
		t.Fatalf("max output tokens = %d", call.MaxOutputTokens)
	}
	if call.Prompt.Version != EditionValidationPromptVersionV2 {
		t.Fatalf("prompt version = %q", call.Prompt.Version)
	}
	if call.StructuredOutput == nil {
		t.Fatal("semantic assessment must use Structured Outputs")
	}
	if call.StructuredOutput.Name != editionAssessmentSchemaNameV2 {
		t.Fatalf("schema name = %q", call.StructuredOutput.Name)
	}
	if string(call.StructuredOutput.Schema) != string(EditionAssessmentJSONSchema()) {
		t.Fatal("edition schema differs from canonical schema")
	}

	if artifact.ValidationVersion != ValidationV2 ||
		artifact.SpecificationVersion != storygeneration.SpecificationV2 ||
		artifact.PromptVersion != EditionValidationPromptVersionV2 ||
		artifact.RequestedModel != "validator-model-test" ||
		artifact.ReturnedModel != "validator-model-returned" ||
		artifact.ResponseID != "resp_semantic" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if len(artifact.EditionBindings) != 1 ||
		artifact.EditionBindings[0].EditionKey != model.AdminStoryEditionGrowingReaders ||
		artifact.EditionBindings[0].ContentSHA256 != generated.ContentSHA256 {
		t.Fatalf("edition bindings = %#v", artifact.EditionBindings)
	}
	if artifact.Assessment.Result != adaptationcontract.ResultNeedsReview {
		t.Fatalf("assessment result = %q", artifact.Assessment.Result)
	}
	if artifact.Usage.TotalTokens != 3600 {
		t.Fatalf("usage = %#v", artifact.Usage)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
}

func TestValidateEditionRejectsWrongTargetAndFabricatedEvidence(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)

	t.Run("wrong target", func(t *testing.T) {
		assessment := Assessment{
			ValidationVersion:    ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           editionKey(model.AdminStoryEditionStoryExplorers),
			Result:               adaptationcontract.ResultPass,
			Findings:             []Finding{},
		}
		gateway := &semanticGateway{
			result: storygeneration.ResponsesResult{
				ResponseID: "resp_wrong_target",
				Model:      "validator-model-returned",
				OutputText: assessmentJSON(t, assessment),
			},
		}
		runner := newValidationRunner(t, gateway)

		_, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: analysis,
			GeneratedEdition: generated,
		})
		if err == nil || !strings.Contains(err.Error(), "wrong edition target") {
			t.Fatalf("ValidateEdition() error = %v", err)
		}
	})

	t.Run("fabricated generated excerpt", func(t *testing.T) {
		assessment := Assessment{
			ValidationVersion:    ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
			Result:               adaptationcontract.ResultNeedsReview,
			Findings: []Finding{
				{
					Code:     adaptationcontract.FindingScopeTooRich,
					Severity: adaptationcontract.FindingSeverityReview,
					Message:  "Review scope.",
					Evidence: []Evidence{
						{
							Location:    EvidenceGeneratedEdition,
							EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
							Excerpt:     "This sentence does not exist.",
							Explanation: "Fabricated evidence.",
						},
					},
				},
			},
		}
		gateway := &semanticGateway{
			result: storygeneration.ResponsesResult{
				ResponseID: "resp_fake_evidence",
				Model:      "validator-model-returned",
				OutputText: assessmentJSON(t, assessment),
			},
		}
		runner := newValidationRunner(t, gateway)

		_, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: analysis,
			GeneratedEdition: generated,
		})
		if err == nil || !strings.Contains(err.Error(), "not present in generated edition") {
			t.Fatalf("ValidateEdition() error = %v", err)
		}
	})

	t.Run("fabricated source excerpt", func(t *testing.T) {
		assessment := validFailAssessment()
		assessment.EditionKey = editionKey(model.AdminStoryEditionGrowingReaders)
		assessment.Findings[0].Evidence[0].Excerpt = "This is not in the canonical source."
		assessment.Findings[0].Evidence[1].Excerpt = "Generated growing-reader edition."
		gateway := &semanticGateway{
			result: storygeneration.ResponsesResult{
				ResponseID: "resp_fake_source",
				Model:      "validator-model-returned",
				OutputText: assessmentJSON(t, assessment),
			},
		}
		runner := newValidationRunner(t, gateway)

		_, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: analysis,
			GeneratedEdition: generated,
		})
		if err == nil || !strings.Contains(err.Error(), "not present in canonical source") {
			t.Fatalf("ValidateEdition() error = %v", err)
		}
	})
}

func TestValidateEditionAcceptsStoryAnalysisEvidenceOnlyWhenExcerptExists(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)

	assessment := Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultFail,
		Findings: []Finding{
			{
				Code:     adaptationcontract.FindingMotivationChanged,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "A source-grounded motivation is changed.",
				Evidence: []Evidence{
					{
						Location:    EvidenceStoryAnalysis,
						Excerpt:     "Improve his and his mother's poverty",
						Explanation: "StoryAnalysis records the explicit motivation.",
					},
					{
						Location:    EvidenceGeneratedEdition,
						EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
						Excerpt:     "Generated growing-reader edition.",
						Explanation: "Generated text is the target of the finding.",
					},
				},
			},
		},
	}

	gateway := &semanticGateway{
		result: storygeneration.ResponsesResult{
			ResponseID: "resp_analysis_evidence",
			Model:      "validator-model-returned",
			OutputText: assessmentJSON(t, assessment),
		},
	}
	runner := newValidationRunner(t, gateway)

	if _, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	}); err != nil {
		t.Fatalf("ValidateEdition() error = %v", err)
	}

	assessment.Findings[0].Evidence[0].Excerpt = "Invented analysis text"
	gateway.result.OutputText = assessmentJSON(t, assessment)

	_, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if err == nil || !strings.Contains(err.Error(), "not present in StoryAnalysis") {
		t.Fatalf("ValidateEdition() error = %v", err)
	}
}

func TestValidateBundleBindsCanonicalTargetsAndEvidence(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGrowing edition keeps the secondary scene.",
	)
	explorers := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionStoryExplorers,
		"# Jack and the Beanstalk\n\nExplorer edition keeps the secondary scene.",
	)

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
				Message:  "The adjacent editions retain effectively the same secondary scope.",
				Evidence: []Evidence{
					{
						Location:    EvidenceGeneratedEdition,
						EditionKey:  editionKey(model.AdminStoryEditionGrowingReaders),
						Excerpt:     "Growing edition keeps the secondary scene.",
						Explanation: "Older edition retains the scene.",
					},
					{
						Location:    EvidenceGeneratedEdition,
						EditionKey:  editionKey(model.AdminStoryEditionStoryExplorers),
						Excerpt:     "Explorer edition keeps the secondary scene.",
						Explanation: "Younger edition also retains the scene.",
					},
				},
			},
		},
	}

	gateway := &semanticGateway{
		result: storygeneration.ResponsesResult{
			ResponseID: "resp_bundle",
			Model:      "validator-model-returned",
			OutputText: assessmentJSON(t, assessment),
		},
	}
	runner := newValidationRunner(t, gateway)

	artifact, err := runner.ValidateBundle(context.Background(), BundleValidationPromptInput{
		Title:             "Jack and the Beanstalk",
		CanonicalSource:   source,
		AnalysisArtifact:  analysis,
		GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing, explorers},
	})
	if err != nil {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
	if len(artifact.EditionBindings) != 2 {
		t.Fatalf("edition bindings = %#v", artifact.EditionBindings)
	}
	if artifact.PromptVersion != BundleValidationPromptVersionV2 {
		t.Fatalf("prompt version = %q", artifact.PromptVersion)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
}

func TestValidateBundleRejectsWrongTargets(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGrowing edition.",
	)
	explorers := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionStoryExplorers,
		"# Jack and the Beanstalk\n\nExplorer edition.",
	)

	assessment := Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			model.AdminStoryEditionConfidentReaders,
			model.AdminStoryEditionGrowingReaders,
		},
		Result:   adaptationcontract.ResultPass,
		Findings: []Finding{},
	}

	gateway := &semanticGateway{
		result: storygeneration.ResponsesResult{
			ResponseID: "resp_wrong_bundle",
			Model:      "validator-model-returned",
			OutputText: assessmentJSON(t, assessment),
		},
	}
	runner := newValidationRunner(t, gateway)

	_, err := runner.ValidateBundle(context.Background(), BundleValidationPromptInput{
		Title:             "Jack and the Beanstalk",
		CanonicalSource:   source,
		AnalysisArtifact:  analysis,
		GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing, explorers},
	})
	if err == nil || !strings.Contains(err.Error(), "wrong edition targets") {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
}

func TestValidationRunnerPreservesGatewayErrors(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)
	sentinel := errors.New("provider failed")
	gateway := &semanticGateway{err: sentinel}
	runner := newValidationRunner(t, gateway)

	_, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ValidateEdition() error = %v, want wrapped sentinel", err)
	}
}

func TestAssessmentArtifactValidateDetectsAssessmentTampering(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)
	assessment := Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKey(model.AdminStoryEditionGrowingReaders),
		Result:               adaptationcontract.ResultPass,
		Findings:             []Finding{},
	}
	gateway := &semanticGateway{
		result: storygeneration.ResponsesResult{
			ResponseID: "resp_pass",
			Model:      "validator-model-returned",
			OutputText: assessmentJSON(t, assessment),
		},
	}
	runner := newValidationRunner(t, gateway)

	artifact, err := runner.ValidateEdition(context.Background(), EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if err != nil {
		t.Fatalf("ValidateEdition() error = %v", err)
	}

	artifact.Assessment.Result = adaptationcontract.ResultNeedsReview
	err = artifact.Validate()
	if err == nil {
		t.Fatal("tampered assessment must fail validation")
	}
}
