package storybenchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

func TestBuildControlledCaseArtifactsUsesGenerationContracts(t *testing.T) {
	fixtures := loadControlledFixturesForRunnerTest(t)
	fixtureCase := controlledCaseByIDForTest(t, fixtures, "progression-inverted")

	analysis, editions, err := buildControlledCaseArtifacts(context.Background(), fixtures.Story, fixtureCase)
	if err != nil {
		t.Fatalf("buildControlledCaseArtifacts() error = %v", err)
	}
	if err := analysis.Validate(); err != nil {
		t.Fatalf("StoryAnalysis artifact is invalid: %v", err)
	}
	if !analysis.MatchesCanonicalSource(fixtures.Story.CanonicalSource) {
		t.Fatal("StoryAnalysis artifact does not bind to canonical source")
	}
	if len(editions) != len(fixtureCase.Editions) {
		t.Fatalf("len(editions) = %d, want %d", len(editions), len(fixtureCase.Editions))
	}
	for index, edition := range editions {
		if err := edition.Validate(); err != nil {
			t.Fatalf("edition %d artifact is invalid: %v", index+1, err)
		}
		if !edition.StructuralValidation.Passed() {
			t.Fatalf("edition %d structural validation = %#v", index+1, edition.StructuralValidation.Findings)
		}
		if edition.SourceSHA256 != analysis.SourceSHA256 || edition.AnalysisSHA256 != analysis.AnalysisSHA256 {
			t.Fatalf("edition %d bindings do not match StoryAnalysis", index+1)
		}
		if !strings.HasPrefix(edition.ResponseID, "benchmark-fixture-edition-") {
			t.Fatalf("edition %d response ID = %q", index+1, edition.ResponseID)
		}
	}
}

func TestRunControlledUsesValidatorMatrixAndRepetitions(t *testing.T) {
	fixtures := loadControlledFixturesForRunnerTest(t)
	fixtures.Cases = []ControlledCase{controlledCaseByIDForTest(t, fixtures, "clean-control")}

	var gateways []*passValidationGateway
	runner, err := NewRunner(RunnerConfig{
		ValidatorFactory: passValidatorFactoryForTest(&gateways),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunControlled(context.Background(), fixtures, ControlledRunConfig{
		ValidationRepetitions: 2,
		Validators: []ValidatorConfig{
			{
				ID:              "validator-a",
				Model:           "validator-model-a",
				ReasoningEffort: storygeneration.ReasoningEffortMedium,
				MaxOutputTokens: 4096,
			},
			{
				ID:              "validator-b",
				Model:           "validator-model-b",
				ReasoningEffort: storygeneration.ReasoningEffortHigh,
				MaxOutputTokens: 8192,
			},
		},
	})
	if err != nil {
		t.Fatalf("RunControlled() error = %v", err)
	}

	if run.Status != TrialStatusComplete {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.AttemptedTrials != 4 || run.CompletedTrials != 4 || run.IncompleteTrials != 0 {
		t.Fatalf("run trial counts = attempted %d completed %d incomplete %d", run.AttemptedTrials, run.CompletedTrials, run.IncompleteTrials)
	}
	if run.QualitySummary.Trials != 4 || run.QualitySummary.ExpectationsMet != 4 {
		t.Fatalf("run.QualitySummary = %#v", run.QualitySummary)
	}
	if len(run.Trials) != 4 {
		t.Fatalf("len(run.Trials) = %d", len(run.Trials))
	}
	for _, trial := range run.Trials {
		if trial.GenerationRepetition != 0 {
			t.Fatalf("controlled trial generation repetition = %d", trial.GenerationRepetition)
		}
		if trial.Status != TrialStatusComplete || trial.AssessmentArtifact == nil || trial.Score == nil || !trial.Score.ExpectationMet {
			t.Fatalf("controlled trial = %#v", trial)
		}
	}
	if len(gateways) != 2 || gateways[0].calls != 2 || gateways[1].calls != 2 {
		t.Fatalf("validator gateway calls = %#v", []int{gatewayCalls(gateways, 0), gatewayCalls(gateways, 1)})
	}
}

func TestRunControlledKeepsValidatorErrorsOutOfQualityScoring(t *testing.T) {
	fixtures := loadControlledFixturesForRunnerTest(t)
	fixtures.Cases = []ControlledCase{controlledCaseByIDForTest(t, fixtures, "clean-control")}

	runner, err := NewRunner(RunnerConfig{
		ValidatorFactory: func(ValidatorConfig) (SemanticValidator, error) {
			return erroringSemanticValidator{err: errors.New("validator transport unavailable")}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunControlled(context.Background(), fixtures, ControlledRunConfig{
		ValidationRepetitions: 1,
		Validators: []ValidatorConfig{
			validValidatorConfigForTest(),
		},
	})
	if err != nil {
		t.Fatalf("RunControlled() error = %v", err)
	}
	if run.Status != TrialStatusIncomplete || run.AttemptedTrials != 1 || run.CompletedTrials != 0 || run.IncompleteTrials != 1 {
		t.Fatalf("run = %#v", run)
	}
	if run.QualitySummary.Trials != 0 {
		t.Fatalf("quality summary scored an incomplete technical trial: %#v", run.QualitySummary)
	}
	trial := run.Trials[0]
	if trial.Status != TrialStatusIncomplete || trial.Score != nil || trial.AssessmentArtifact != nil {
		t.Fatalf("trial = %#v", trial)
	}
	if !strings.Contains(trial.Error, "transport unavailable") {
		t.Fatalf("trial.Error = %q", trial.Error)
	}
}

func TestRunControlledRejectsInvalidAssessmentArtifactEnvelope(t *testing.T) {
	fixtures := loadControlledFixturesForRunnerTest(t)
	fixtures.Cases = []ControlledCase{controlledCaseByIDForTest(t, fixtures, "clean-control")}

	runner, err := NewRunner(RunnerConfig{
		ValidatorFactory: func(ValidatorConfig) (SemanticValidator, error) {
			return invalidArtifactSemanticValidator{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunControlled(context.Background(), fixtures, ControlledRunConfig{
		ValidationRepetitions: 1,
		Validators:            []ValidatorConfig{validValidatorConfigForTest()},
	})
	if err != nil {
		t.Fatalf("RunControlled() error = %v", err)
	}
	if run.Status != TrialStatusIncomplete || run.CompletedTrials != 0 || run.IncompleteTrials != 1 {
		t.Fatalf("run = %#v", run)
	}
	if run.QualitySummary.Trials != 0 {
		t.Fatalf("quality summary scored an invalid assessment artifact: %#v", run.QualitySummary)
	}
	if !strings.Contains(run.Trials[0].Error, "assessment artifact is invalid") {
		t.Fatalf("trial.Error = %q", run.Trials[0].Error)
	}
}

func TestRunEndToEndSeparatesGenerationAndValidationRepetitions(t *testing.T) {
	fixtures := loadControlledFixturesForRunnerTest(t)
	analysisJSON, err := json.Marshal(fixtures.Story.Analysis)
	if err != nil {
		t.Fatalf("json.Marshal(StoryAnalysis) error = %v", err)
	}
	clean := controlledCaseByIDForTest(t, fixtures, "clean-control").Editions[0].Markdown
	generationGateway := &deterministicGenerationGateway{
		analysisJSON: string(analysisJSON),
		markdown:     clean,
	}
	generationRunner, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
		Gateway:                 generationGateway,
		AnalysisReasoningEffort: storygeneration.ReasoningEffortMedium,
		AnalysisMaxOutputTokens: 4096,
		EditionReasoningEffort:  storygeneration.ReasoningEffortMedium,
		EditionMaxOutputTokens:  4096,
	})
	if err != nil {
		t.Fatalf("storygeneration.NewV2Runner() error = %v", err)
	}

	var validatorGateways []*passValidationGateway
	runner, err := NewRunner(RunnerConfig{
		Generator:        generationRunner,
		ValidatorFactory: passValidatorFactoryForTest(&validatorGateways),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunEndToEnd(context.Background(), EndToEndSource{
		ID:              "synthetic-end-to-end",
		Slug:            fixtures.Story.Slug,
		Title:           fixtures.Story.Title,
		Author:          fixtures.Story.Author,
		Language:        fixtures.Story.Language,
		SourceURL:       fixtures.Story.SourceURL,
		Rights:          cloneStringAnyMap(fixtures.Story.Rights),
		CanonicalSource: fixtures.Story.CanonicalSource,
	}, EndToEndRunConfig{
		GenerationRepetitions: 2,
		ValidationRepetitions: 3,
		Validators: []ValidatorConfig{
			validValidatorConfigForTest(),
		},
	})
	if err != nil {
		t.Fatalf("RunEndToEnd() error = %v", err)
	}

	if run.Status != TrialStatusComplete || len(run.Generations) != 2 {
		t.Fatalf("run.Status = %q, generations = %d", run.Status, len(run.Generations))
	}
	if generationGateway.calls != 10 {
		t.Fatalf("generation gateway calls = %d, want 10", generationGateway.calls)
	}
	if len(validatorGateways) != 1 || validatorGateways[0].calls != 30 {
		t.Fatalf("validator gateway calls = %d, want 30", gatewayCalls(validatorGateways, 0))
	}

	for _, generation := range run.Generations {
		if generation.GenerationStatus != TrialStatusComplete || generation.ValidationStatus != TrialStatusComplete {
			t.Fatalf("generation statuses = %q/%q", generation.GenerationStatus, generation.ValidationStatus)
		}
		if generation.AnalysisArtifact == nil || len(generation.Editions) != 4 {
			t.Fatalf("generation artifacts = analysis %#v editions %d", generation.AnalysisArtifact, len(generation.Editions))
		}
		if len(generation.ValidationTrials) != 15 {
			t.Fatalf("len(generation.ValidationTrials) = %d, want 15", len(generation.ValidationTrials))
		}
		for _, trial := range generation.ValidationTrials {
			if trial.GenerationRepetition != generation.Repetition || trial.Status != TrialStatusComplete || trial.AssessmentArtifact == nil {
				t.Fatalf("validation trial = %#v", trial)
			}
			if trial.AssessmentScope == adaptationcontract.AssessmentScopeEdition {
				if trial.EditionKey == nil || len(trial.AssessmentArtifact.EditionBindings) != 1 {
					t.Fatalf("edition trial target/bindings = %#v", trial)
				}
				generated := generatedEditionByKeyForTest(t, generation.Editions, *trial.EditionKey)
				if trial.AssessmentArtifact.EditionBindings[0].ContentSHA256 != generated.ContentSHA256 {
					t.Fatalf("validator did not bind to exact generated artifact")
				}
			}
		}
	}
}

func TestRunEndToEndGenerationErrorIsIncompleteNotSemanticFail(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		Generator:        failingGenerationRunner{err: errors.New("generation gateway unavailable")},
		ValidatorFactory: passValidatorFactoryForTest(nil),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunEndToEnd(context.Background(), validEndToEndSourceForTest(), EndToEndRunConfig{
		GenerationRepetitions: 1,
		ValidationRepetitions: 1,
		Validators:            []ValidatorConfig{validValidatorConfigForTest()},
	})
	if err != nil {
		t.Fatalf("RunEndToEnd() error = %v", err)
	}
	if run.Status != TrialStatusIncomplete || len(run.Generations) != 1 {
		t.Fatalf("run = %#v", run)
	}
	generation := run.Generations[0]
	if generation.GenerationStatus != TrialStatusIncomplete || generation.ValidationStatus != TrialStatusIncomplete {
		t.Fatalf("generation statuses = %q/%q", generation.GenerationStatus, generation.ValidationStatus)
	}
	if !strings.Contains(generation.GenerationError, "gateway unavailable") {
		t.Fatalf("generation.GenerationError = %q", generation.GenerationError)
	}
	if len(generation.ValidationTrials) != 0 {
		t.Fatalf("generation failure produced semantic trials: %#v", generation.ValidationTrials)
	}
}

func TestRunEndToEndRejectsInvalidAnalysisArtifactBeforeEditionGeneration(t *testing.T) {
	generator := &invalidAnalysisGenerationRunner{}
	runner, err := NewRunner(RunnerConfig{
		Generator:        generator,
		ValidatorFactory: passValidatorFactoryForTest(nil),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	run, err := runner.RunEndToEnd(context.Background(), validEndToEndSourceForTest(), EndToEndRunConfig{
		GenerationRepetitions: 1,
		ValidationRepetitions: 1,
		Validators:            []ValidatorConfig{validValidatorConfigForTest()},
	})
	if err != nil {
		t.Fatalf("RunEndToEnd() error = %v", err)
	}
	if run.Status != TrialStatusIncomplete || len(run.Generations) != 1 {
		t.Fatalf("run = %#v", run)
	}
	generation := run.Generations[0]
	if generation.GenerationStatus != TrialStatusIncomplete || generation.ValidationStatus != TrialStatusIncomplete {
		t.Fatalf("generation statuses = %q/%q", generation.GenerationStatus, generation.ValidationStatus)
	}
	if !strings.Contains(generation.GenerationError, "invalid artifact") {
		t.Fatalf("generation.GenerationError = %q", generation.GenerationError)
	}
	if generator.generateCalls != 0 {
		t.Fatalf("GenerateEdition calls = %d, want 0", generator.generateCalls)
	}
}

func TestRunConfigValidationDoesNotLockValidatorModel(t *testing.T) {
	if err := validateValidatorConfigs([]ValidatorConfig{
		{
			ID:              "experimental-validator",
			Model:           "future-validator-model",
			ReasoningEffort: storygeneration.ReasoningEffortXHigh,
			MaxOutputTokens: 1234,
		},
	}); err != nil {
		t.Fatalf("validateValidatorConfigs() unexpectedly locked model choice: %v", err)
	}

	err := validateValidatorConfigs([]ValidatorConfig{
		validValidatorConfigForTest(),
		validValidatorConfigForTest(),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate config error = %v", err)
	}
}

type passValidationGateway struct {
	calls          int
	includeFinding bool
}

func (gateway *passValidationGateway) Create(
	ctx context.Context,
	call storygeneration.ResponsesCall,
) (storygeneration.ResponsesResult, error) {
	if err := ctx.Err(); err != nil {
		return storygeneration.ResponsesResult{}, err
	}
	gateway.calls++

	var input struct {
		EditionKey        model.AdminStoryEditionKey   `json:"editionKey"`
		EditionKeys       []model.AdminStoryEditionKey `json:"editionKeys"`
		EvidenceCatalogue []struct {
			SegmentID storyvalidation.EvidenceSegmentID `json:"segmentId"`
		} `json:"evidenceCatalogue"`
	}
	if err := json.Unmarshal([]byte(call.Prompt.UserInputJSON), &input); err != nil {
		return storygeneration.ResponsesResult{}, fmt.Errorf("decode validation prompt input: %w", err)
	}
	if len(input.EvidenceCatalogue) == 0 {
		return storygeneration.ResponsesResult{}, fmt.Errorf("validation prompt contains no evidence catalogue")
	}

	var output map[string]any
	switch call.Prompt.Version {
	case storyvalidation.EditionJudgementPromptVersionV3:
		output = map[string]any{
			"validationVersion":    storyvalidation.ValidationV3,
			"specificationVersion": storygeneration.SpecificationV2,
			"assessmentScope":      adaptationcontract.AssessmentScopeEdition,
			"editionKey":           input.EditionKey,
			"result":               adaptationcontract.ResultPass,
			"findings":             []any{},
		}
	case storyvalidation.BundleJudgementPromptVersionV3:
		output = map[string]any{
			"validationVersion":    storyvalidation.ValidationV3,
			"specificationVersion": storygeneration.SpecificationV2,
			"assessmentScope":      adaptationcontract.AssessmentScopeBundle,
			"editionKeys":          input.EditionKeys,
			"result":               adaptationcontract.ResultPass,
			"findings":             []any{},
		}
	default:
		return storygeneration.ResponsesResult{}, fmt.Errorf("unexpected validation prompt version %q", call.Prompt.Version)
	}
	if gateway.includeFinding {
		switch call.Prompt.Version {
		case storyvalidation.EditionJudgementPromptVersionV3:
			output["result"] = adaptationcontract.ResultNeedsReview
			output["findings"] = []any{map[string]any{
				"code":     adaptationcontract.FindingScopeTooRich,
				"severity": adaptationcontract.FindingSeverityReview,
				"message":  "Fixture evidence reference.",
				"evidence": []any{map[string]any{
					"segmentId":   input.EvidenceCatalogue[0].SegmentID,
					"explanation": "The reference came from the supplied catalogue.",
				}},
			}}
		case storyvalidation.BundleJudgementPromptVersionV3:
			output["result"] = adaptationcontract.ResultFail
			output["findings"] = []any{map[string]any{
				"code":     adaptationcontract.FindingEditionProgressionNotDistinct,
				"severity": adaptationcontract.FindingSeverityBlocking,
				"message":  "Fixture evidence reference.",
				"evidence": []any{map[string]any{
					"segmentId":   input.EvidenceCatalogue[0].SegmentID,
					"explanation": "The reference came from the supplied catalogue.",
				}},
			}}
		}
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return storygeneration.ResponsesResult{}, err
	}
	return storygeneration.ResponsesResult{
		ResponseID: fmt.Sprintf("benchmark-validator-response-%d", gateway.calls),
		Model:      call.Model,
		OutputText: string(encoded),
		Usage: storygeneration.ResponsesUsage{
			InputTokens:  100,
			OutputTokens: 20,
			TotalTokens:  120,
		},
	}, nil
}

func TestPassValidationGatewayBuildsV3JudgementWithPermittedReference(t *testing.T) {
	gateway := &passValidationGateway{includeFinding: true}
	result, err := gateway.Create(context.Background(), storygeneration.ResponsesCall{
		Operation: storygeneration.ResponsesOperationValidateGrowingReaders,
		Model:     "validator-model",
		Prompt: storygeneration.Prompt{
			Version:       storyvalidation.EditionJudgementPromptVersionV3,
			UserInputJSON: `{"editionKey":"growing-readers","evidenceCatalogue":[{"segmentId":"src:p0007"}]}`,
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	judgement, err := storyvalidation.DecodeSemanticJudgementJSON([]byte(result.OutputText))
	if err != nil {
		t.Fatalf("DecodeSemanticJudgementJSON() error = %v", err)
	}
	if judgement.Findings[0].Evidence[0].SegmentID != "src:p0007" {
		t.Fatalf("segment ID = %q, want supplied catalogue ID", judgement.Findings[0].Evidence[0].SegmentID)
	}
}

func passValidatorFactoryForTest(gateways *[]*passValidationGateway) ValidatorFactory {
	return func(config ValidatorConfig) (SemanticValidator, error) {
		gateway := &passValidationGateway{}
		if gateways != nil {
			*gateways = append(*gateways, gateway)
		}
		return storyvalidation.NewRunner(storyvalidation.RunnerConfig{
			Gateway:         gateway,
			Model:           config.Model,
			ReasoningEffort: config.ReasoningEffort,
			MaxOutputTokens: config.MaxOutputTokens,
		})
	}
}

type deterministicGenerationGateway struct {
	analysisJSON string
	markdown     string
	calls        int
}

func (gateway *deterministicGenerationGateway) Create(
	ctx context.Context,
	call storygeneration.ResponsesCall,
) (storygeneration.ResponsesResult, error) {
	if err := ctx.Err(); err != nil {
		return storygeneration.ResponsesResult{}, err
	}
	gateway.calls++
	output := ""
	switch call.Prompt.Version {
	case storygeneration.SourceAnalysisPromptVersionV3:
		output = gateway.analysisJSON
	case storygeneration.EditionPromptVersionV4:
		output = gateway.markdown
	default:
		return storygeneration.ResponsesResult{}, fmt.Errorf("unexpected generation prompt version %q", call.Prompt.Version)
	}
	return storygeneration.ResponsesResult{
		ResponseID: fmt.Sprintf("benchmark-generation-response-%d", gateway.calls),
		Model:      call.Model,
		OutputText: output,
		Usage: storygeneration.ResponsesUsage{
			InputTokens:  200,
			OutputTokens: 50,
			TotalTokens:  250,
		},
	}, nil
}

type invalidArtifactSemanticValidator struct{}

func (invalidArtifactSemanticValidator) ValidateEdition(
	context.Context,
	storyvalidation.EditionValidationPromptInput,
) (storyvalidation.AssessmentArtifact, error) {
	key := model.AdminStoryEditionGrowingReaders
	return storyvalidation.AssessmentArtifact{
		Assessment: storyvalidation.Assessment{
			ValidationVersion:    storyvalidation.ValidationV2,
			SpecificationVersion: storygeneration.SpecificationV2,
			AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
			EditionKey:           &key,
			Result:               adaptationcontract.ResultPass,
			Findings:             []storyvalidation.Finding{},
		},
	}, nil
}

func (invalidArtifactSemanticValidator) ValidateBundle(
	context.Context,
	storyvalidation.BundleValidationPromptInput,
) (storyvalidation.AssessmentArtifact, error) {
	return storyvalidation.AssessmentArtifact{}, errors.New("unexpected bundle call")
}

type erroringSemanticValidator struct {
	err error
}

func (validator erroringSemanticValidator) ValidateEdition(
	context.Context,
	storyvalidation.EditionValidationPromptInput,
) (storyvalidation.AssessmentArtifact, error) {
	return storyvalidation.AssessmentArtifact{}, validator.err
}

func (validator erroringSemanticValidator) ValidateBundle(
	context.Context,
	storyvalidation.BundleValidationPromptInput,
) (storyvalidation.AssessmentArtifact, error) {
	return storyvalidation.AssessmentArtifact{}, validator.err
}

type invalidAnalysisGenerationRunner struct {
	generateCalls int
}

func (*invalidAnalysisGenerationRunner) AnalyseSource(
	context.Context,
	storygeneration.SourceAnalysisPromptInput,
) (storygeneration.StoryAnalysisArtifact, error) {
	return storygeneration.StoryAnalysisArtifact{}, nil
}

func (runner *invalidAnalysisGenerationRunner) GenerateEdition(
	context.Context,
	storygeneration.GenerateEditionInput,
) (storygeneration.GeneratedEditionArtifact, error) {
	runner.generateCalls++
	return storygeneration.GeneratedEditionArtifact{}, errors.New("GenerateEdition should not be called after invalid analysis artifact")
}

type failingGenerationRunner struct {
	err error
}

func (runner failingGenerationRunner) AnalyseSource(
	context.Context,
	storygeneration.SourceAnalysisPromptInput,
) (storygeneration.StoryAnalysisArtifact, error) {
	return storygeneration.StoryAnalysisArtifact{}, runner.err
}

func (runner failingGenerationRunner) GenerateEdition(
	context.Context,
	storygeneration.GenerateEditionInput,
) (storygeneration.GeneratedEditionArtifact, error) {
	return storygeneration.GeneratedEditionArtifact{}, runner.err
}

func loadControlledFixturesForRunnerTest(t *testing.T) ControlledFixtureSet {
	t.Helper()
	fixtures, err := LoadControlledFixtureSet("testdata/controlled")
	if err != nil {
		t.Fatalf("LoadControlledFixtureSet() error = %v", err)
	}
	return fixtures
}

func controlledCaseByIDForTest(t *testing.T, fixtures ControlledFixtureSet, id string) ControlledCase {
	t.Helper()
	for _, fixtureCase := range fixtures.Cases {
		if fixtureCase.ID == id {
			return fixtureCase
		}
	}
	t.Fatalf("controlled fixture case %q not found", id)
	return ControlledCase{}
}

func generatedEditionByKeyForTest(
	t *testing.T,
	editions []storygeneration.GeneratedEditionArtifact,
	key model.AdminStoryEditionKey,
) storygeneration.GeneratedEditionArtifact {
	t.Helper()
	for _, edition := range editions {
		if edition.EditionKey == key {
			return edition
		}
	}
	t.Fatalf("generated edition %q not found", key)
	return storygeneration.GeneratedEditionArtifact{}
}

func validValidatorConfigForTest() ValidatorConfig {
	return ValidatorConfig{
		ID:              "validator-a",
		Model:           "validator-model-a",
		ReasoningEffort: storygeneration.ReasoningEffortMedium,
		MaxOutputTokens: 4096,
	}
}

func validEndToEndSourceForTest() EndToEndSource {
	return EndToEndSource{
		ID:              "synthetic-source",
		Slug:            "synthetic-source",
		Title:           "Synthetic Source",
		Author:          "Panda Pages benchmark fixture",
		Language:        "en-GB",
		SourceURL:       "https://example.invalid/pandapages/benchmark/synthetic-source",
		Rights:          map[string]any{"publicationEligible": false},
		CanonicalSource: "# Synthetic Source\n\nA synthetic source used only for orchestration error testing.",
	}
}

func gatewayCalls(gateways []*passValidationGateway, index int) int {
	if index < 0 || index >= len(gateways) {
		return -1
	}
	return gateways[index].calls
}
