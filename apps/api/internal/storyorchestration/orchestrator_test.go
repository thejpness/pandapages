package storyorchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

func TestNewRequiresHighLevelServices(t *testing.T) {
	_, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)

	for _, test := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing generator", Config{Validator: validator}, "generation runner"},
		{"missing validator", Config{Generator: generator}, "semantic validator"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateCompletedResult(t *testing.T) {
	input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
	result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := ValidateCompletedResult(result, input.SourceIdentity, input.CanonicalSource); err != nil {
		t.Fatalf("ValidateCompletedResult() error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*Result)
	}{
		{"source identity", func(value *Result) { value.SourceIdentity = "other-source-version" }},
		{"source SHA", func(value *Result) { value.SourceSHA256 = sha256Hex("other source") }},
		{"missing edition", func(value *Result) { value.Editions = value.Editions[:3] }},
		{"wrong edition order", func(value *Result) { value.Editions[0], value.Editions[1] = value.Editions[1], value.Editions[0] }},
		{"invalid edition assessment", func(value *Result) { value.EditionAssessments[0] = storyvalidation.AssessmentArtifact{} }},
		{"invalid bundle assessment", func(value *Result) { value.BundleAssessment = storyvalidation.AssessmentArtifact{} }},
		{"semantic result", func(value *Result) { value.SemanticResult = adaptationcontract.ResultFail }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			value, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			test.mutate(&value)
			if err := ValidateCompletedResult(value, input.SourceIdentity, input.CanonicalSource); err == nil {
				t.Fatal("ValidateCompletedResult() unexpectedly succeeded")
			}
		})
	}
}

func TestRunCompletesCanonicalFlowInOrderAndKeepsEditionsIndependent(t *testing.T) {
	input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
	orchestrator := newOrchestrator(t, generator, validator)

	result, err := orchestrator.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	keys := storygeneration.DerivedEditionKeysV2()
	if len(generator.analysisInputs) != 1 {
		t.Fatalf("AnalyseSource calls = %d, want 1", len(generator.analysisInputs))
	}
	if got := generator.analysisInputs[0]; got.Title != input.Title || got.Author != input.Author || got.CanonicalSource != input.CanonicalSource {
		t.Fatalf("AnalyseSource input = %#v, want exact canonical source input", got)
	}
	if len(generator.editionInputs) != len(keys) {
		t.Fatalf("GenerateEdition calls = %d, want %d", len(generator.editionInputs), len(keys))
	}
	if len(validator.editionInputs) != len(keys) {
		t.Fatalf("ValidateEdition calls = %d, want %d", len(validator.editionInputs), len(keys))
	}
	if len(validator.bundleInputs) != 1 {
		t.Fatalf("ValidateBundle calls = %d, want 1", len(validator.bundleInputs))
	}

	for index, key := range keys {
		generationInput := generator.editionInputs[index]
		if generationInput.EditionKey != key {
			t.Fatalf("generation key %d = %q, want %q", index+1, generationInput.EditionKey, key)
		}
		if generationInput.CanonicalSource != input.CanonicalSource {
			t.Fatalf("generation %q did not receive exact canonical source", key)
		}
		if !reflect.DeepEqual(generationInput.AnalysisArtifact, generator.analysis) {
			t.Fatalf("generation %q did not receive the shared analysis artifact", key)
		}
		for _, sibling := range generator.editions {
			if strings.Contains(generationInput.CanonicalSource, sibling.Markdown) {
				t.Fatalf("generation %q canonical source contains generated sibling content", key)
			}
		}

		validationInput := validator.editionInputs[index]
		if validationInput.GeneratedEdition.EditionKey != key || validationInput.CanonicalSource != input.CanonicalSource {
			t.Fatalf("edition validation input %d = %#v", index+1, validationInput)
		}
		if !reflect.DeepEqual(validationInput.AnalysisArtifact, generator.analysis) {
			t.Fatalf("edition validation %q did not receive the shared analysis artifact", key)
		}

		if result.Editions[index].EditionKey != key {
			t.Fatalf("result edition %d = %q, want %q", index+1, result.Editions[index].EditionKey, key)
		}
		if result.EditionAssessments[index].EditionKey == nil || *result.EditionAssessments[index].EditionKey != key {
			t.Fatalf("result assessment %d does not target %q", index+1, key)
		}
	}

	bundleInput := validator.bundleInputs[0]
	if bundleInput.CanonicalSource != input.CanonicalSource || !reflect.DeepEqual(bundleInput.AnalysisArtifact, generator.analysis) {
		t.Fatalf("bundle validation input = %#v", bundleInput)
	}
	if got := editionKeys(bundleInput.GeneratedEditions); !reflect.DeepEqual(got, keys) {
		t.Fatalf("bundle generation order = %v, want %v", got, keys)
	}
	if result.SourceIdentity != input.SourceIdentity || result.SourceSHA256 != generator.analysis.SourceSHA256 {
		t.Fatalf("result source binding = %#v", result)
	}
	if result.SemanticResult != adaptationcontract.ResultPass {
		t.Fatalf("SemanticResult = %q, want pass", result.SemanticResult)
	}
}

func TestRunCompletesValidEditorialResultsAndAggregatesWorstOutcome(t *testing.T) {
	keys := storygeneration.DerivedEditionKeysV2()
	tests := []struct {
		name           string
		editionResults map[model.AdminStoryEditionKey]adaptationcontract.Result
		bundleResult   adaptationcontract.Result
		want           adaptationcontract.Result
	}{
		{
			name:         "pass",
			bundleResult: adaptationcontract.ResultPass,
			want:         adaptationcontract.ResultPass,
		},
		{
			name: "edition needs review",
			editionResults: map[model.AdminStoryEditionKey]adaptationcontract.Result{
				keys[1]: adaptationcontract.ResultNeedsReview,
			},
			bundleResult: adaptationcontract.ResultPass,
			want:         adaptationcontract.ResultNeedsReview,
		},
		{
			name:         "bundle needs review",
			bundleResult: adaptationcontract.ResultNeedsReview,
			want:         adaptationcontract.ResultNeedsReview,
		},
		{
			name: "edition fail",
			editionResults: map[model.AdminStoryEditionKey]adaptationcontract.Result{
				keys[2]: adaptationcontract.ResultFail,
			},
			bundleResult: adaptationcontract.ResultPass,
			want:         adaptationcontract.ResultFail,
		},
		{
			name: "fail outranks needs review",
			editionResults: map[model.AdminStoryEditionKey]adaptationcontract.Result{
				keys[0]: adaptationcontract.ResultNeedsReview,
			},
			bundleResult: adaptationcontract.ResultFail,
			want:         adaptationcontract.ResultFail,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, test.editionResults, test.bundleResult)
			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.SemanticResult != test.want {
				t.Fatalf("SemanticResult = %q, want %q", result.SemanticResult, test.want)
			}
			if len(result.Editions) != len(keys) || len(result.EditionAssessments) != len(keys) {
				t.Fatalf("incomplete result = %#v", result)
			}
			if len(validator.editionInputs) != len(keys) || len(validator.bundleInputs) != 1 {
				t.Fatalf("valid editorial result did not complete validation: editions=%d bundle=%d", len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunFailsClosedBeforeEditionGeneration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeGenerationRunner)
	}{
		{
			name: "analysis operation error",
			mutate: func(generator *fakeGenerationRunner) {
				generator.analysisErr = errors.New("analysis unavailable")
			},
		},
		{
			name: "invalid analysis artifact",
			mutate: func(generator *fakeGenerationRunner) {
				generator.analysis = storygeneration.StoryAnalysisArtifact{}
			},
		},
		{
			name: "misbound analysis artifact",
			mutate: func(generator *fakeGenerationRunner) {
				generator.analysis.SourceSHA256 = sha256Hex("a different canonical source")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			test.mutate(generator)
			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			assertZeroResult(t, result)
			if len(generator.analysisInputs) != 1 || len(generator.editionInputs) != 0 || len(validator.editionInputs) != 0 || len(validator.bundleInputs) != 0 {
				t.Fatalf("calls after failed analysis: analysis=%d generation=%d edition-validation=%d bundle-validation=%d", len(generator.analysisInputs), len(generator.editionInputs), len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunFailsClosedForEditionGenerationOrArtifactFailures(t *testing.T) {
	keys := storygeneration.DerivedEditionKeysV2()
	tests := []struct {
		name              string
		mutate            func(*fakeGenerationRunner)
		wantGenerationOps int
	}{
		{
			name: "generation operation error",
			mutate: func(generator *fakeGenerationRunner) {
				generator.editionErrs[keys[1]] = errors.New("generation unavailable")
			},
			wantGenerationOps: 2,
		},
		{
			name: "invalid generated artifact",
			mutate: func(generator *fakeGenerationRunner) {
				generator.editions[keys[0]] = storygeneration.GeneratedEditionArtifact{}
			},
			wantGenerationOps: 1,
		},
		{
			name: "misbound generated artifact",
			mutate: func(generator *fakeGenerationRunner) {
				generator.editions[keys[0]] = generator.editions[keys[1]]
			},
			wantGenerationOps: 1,
		},
		{
			name: "wrong generated source binding",
			mutate: func(generator *fakeGenerationRunner) {
				artifact := generator.editions[keys[0]]
				artifact.SourceSHA256 = sha256Hex("a different canonical source")
				generator.editions[keys[0]] = artifact
			},
			wantGenerationOps: 1,
		},
		{
			name: "wrong generated analysis binding",
			mutate: func(generator *fakeGenerationRunner) {
				artifact := generator.editions[keys[0]]
				artifact.AnalysisSHA256 = sha256Hex("a different StoryAnalysis artifact")
				generator.editions[keys[0]] = artifact
			},
			wantGenerationOps: 1,
		},
		{
			name: "structurally failing generated artifact",
			mutate: func(generator *fakeGenerationRunner) {
				artifact := generator.editions[keys[0]]
				artifact.StructuralValidation.Findings = []adaptationcontract.Finding{{Code: adaptationcontract.FindingMissingH1Title}}
				generator.editions[keys[0]] = artifact
			},
			wantGenerationOps: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			test.mutate(generator)
			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			assertZeroResult(t, result)
			if len(generator.editionInputs) != test.wantGenerationOps || len(validator.editionInputs) != 0 || len(validator.bundleInputs) != 0 {
				t.Fatalf("calls after failed generation: generation=%d edition-validation=%d bundle-validation=%d", len(generator.editionInputs), len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunFailsClosedForEditionSemanticFailures(t *testing.T) {
	keys := storygeneration.DerivedEditionKeysV2()
	tests := []struct {
		name              string
		mutate            func(*fakeSemanticValidator)
		wantValidationOps int
	}{
		{
			name: "validator operation error",
			mutate: func(validator *fakeSemanticValidator) {
				validator.editionErrs[keys[1]] = errors.New("validator unavailable")
			},
			wantValidationOps: 2,
		},
		{
			name: "invalid assessment artifact",
			mutate: func(validator *fakeSemanticValidator) {
				validator.editionArtifacts[keys[0]] = storyvalidation.AssessmentArtifact{}
			},
			wantValidationOps: 1,
		},
		{
			name: "misbound assessment artifact",
			mutate: func(validator *fakeSemanticValidator) {
				artifact := validator.editionArtifacts[keys[0]]
				artifact.SourceSHA256 = sha256Hex("a different canonical source")
				validator.editionArtifacts[keys[0]] = artifact
			},
			wantValidationOps: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			test.mutate(validator)
			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			assertZeroResult(t, result)
			if len(generator.editionInputs) != len(keys) || len(validator.editionInputs) != test.wantValidationOps || len(validator.bundleInputs) != 0 {
				t.Fatalf("calls after failed edition validation: generation=%d edition-validation=%d bundle-validation=%d", len(generator.editionInputs), len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunFailsClosedForBundleSemanticFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeSemanticValidator)
	}{
		{
			name: "validator operation error",
			mutate: func(validator *fakeSemanticValidator) {
				validator.bundleErr = errors.New("bundle validator unavailable")
			},
		},
		{
			name: "invalid bundle assessment artifact",
			mutate: func(validator *fakeSemanticValidator) {
				validator.bundleArtifact = storyvalidation.AssessmentArtifact{}
			},
		},
		{
			name: "misbound bundle assessment artifact",
			mutate: func(validator *fakeSemanticValidator) {
				validator.bundleArtifact.SourceSHA256 = sha256Hex("a different canonical source")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			test.mutate(validator)
			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			assertZeroResult(t, result)
			if len(generator.editionInputs) != len(storygeneration.DerivedEditionKeysV2()) || len(validator.editionInputs) != len(storygeneration.DerivedEditionKeysV2()) || len(validator.bundleInputs) != 1 {
				t.Fatalf("calls after failed bundle validation: generation=%d edition-validation=%d bundle-validation=%d", len(generator.editionInputs), len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunRejectsAssessmentArtifactsWithWrongTargets(t *testing.T) {
	t.Run("edition target", func(t *testing.T) {
		input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
		keys := storygeneration.DerivedEditionKeysV2()
		validator.editionArtifacts[keys[0]] = validator.editionArtifacts[keys[1]]

		result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
		if err == nil {
			t.Fatal("Run() unexpectedly succeeded")
		}
		assertZeroResult(t, result)
		if len(validator.editionInputs) != 1 || len(validator.bundleInputs) != 0 {
			t.Fatalf("calls after wrong edition target: edition-validation=%d bundle-validation=%d", len(validator.editionInputs), len(validator.bundleInputs))
		}
	})

	t.Run("bundle target set", func(t *testing.T) {
		input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
		keys := storygeneration.DerivedEditionKeysV2()
		subset := make([]storygeneration.GeneratedEditionArtifact, 0, len(keys)-1)
		for _, key := range keys[:len(keys)-1] {
			subset = append(subset, generator.editions[key])
		}
		validator.bundleArtifact = testBundleAssessmentArtifact(t, generator.analysis, subset, adaptationcontract.ResultPass)

		result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
		if err == nil {
			t.Fatal("Run() unexpectedly succeeded")
		}
		assertZeroResult(t, result)
		if len(validator.editionInputs) != len(keys) || len(validator.bundleInputs) != 1 {
			t.Fatalf("calls after wrong bundle target: edition-validation=%d bundle-validation=%d", len(validator.editionInputs), len(validator.bundleInputs))
		}
	})
}

func TestCloneStoryAnalysisArtifactPreservesSliceStateAndSHA(t *testing.T) {
	input, _, _ := testServices(t, nil, adaptationcontract.ResultPass)
	artifact := testAnalysisArtifact(t, input.CanonicalSource)
	artifact.Analysis.Characters[0].ExplicitMotivations = []string{}
	artifact.Analysis.Characters[0].FlawsOrAmbiguities = []string{}
	artifact.Analysis.Characters = append(artifact.Analysis.Characters, storygeneration.Character{
		Name:                "The guide",
		Role:                "companion",
		ExplicitMotivations: []string{},
		FlawsOrAmbiguities:  []string{},
	})
	artifact.Analysis.Relationships = []storygeneration.Relationship{{
		Parties:       []string{"The traveller", "The guide"},
		Nature:        "companions",
		PowerDynamics: "equal",
	}}
	artifact.Analysis.DevelopmentBeats = []storygeneration.StoryBeat{}
	artifact.Analysis.EnrichmentMaterial = []storygeneration.StoryBeat{}
	artifact.Analysis.CausalDependencies = []storygeneration.CausalDependency{}
	artifact.Analysis.IconicMaterial = []storygeneration.IconicMaterial{}
	artifact.Analysis.IntenseMaterial = []storygeneration.IntenseMaterial{}
	artifact.Analysis.AdaptationRisks = []storygeneration.AdaptationRisk{}
	artifact.AnalysisSHA256 = jsonSHA256(t, artifact.Analysis)
	if err := artifact.Validate(); err != nil {
		t.Fatalf("source artifact.Validate() error = %v", err)
	}

	clone := cloneStoryAnalysisArtifact(artifact)
	if err := clone.Validate(); err != nil {
		t.Fatalf("cloned artifact.Validate() error = %v", err)
	}
	if clone.AnalysisSHA256 != artifact.AnalysisSHA256 || !clone.MatchesCanonicalSource(input.CanonicalSource) {
		t.Fatalf("cloned artifact does not retain exact SHA bindings: %#v", clone)
	}

	emptySlices := []struct {
		name  string
		isNil bool
		len   int
	}{
		{"character explicit motivations", clone.Analysis.Characters[0].ExplicitMotivations == nil, len(clone.Analysis.Characters[0].ExplicitMotivations)},
		{"character flaws", clone.Analysis.Characters[0].FlawsOrAmbiguities == nil, len(clone.Analysis.Characters[0].FlawsOrAmbiguities)},
		{"development beats", clone.Analysis.DevelopmentBeats == nil, len(clone.Analysis.DevelopmentBeats)},
		{"enrichment material", clone.Analysis.EnrichmentMaterial == nil, len(clone.Analysis.EnrichmentMaterial)},
		{"causal dependencies", clone.Analysis.CausalDependencies == nil, len(clone.Analysis.CausalDependencies)},
		{"iconic material", clone.Analysis.IconicMaterial == nil, len(clone.Analysis.IconicMaterial)},
		{"intense material", clone.Analysis.IntenseMaterial == nil, len(clone.Analysis.IntenseMaterial)},
		{"adaptation risks", clone.Analysis.AdaptationRisks == nil, len(clone.Analysis.AdaptationRisks)},
	}
	for _, emptySlice := range emptySlices {
		if emptySlice.isNil || emptySlice.len != 0 {
			t.Fatalf("%s = nil:%t len:%d, want non-nil empty", emptySlice.name, emptySlice.isNil, emptySlice.len)
		}
	}

	clone.Analysis.Characters[0].Role = "mutated"
	clone.Analysis.Relationships[0].Parties[0] = "mutated"
	clone.Analysis.CoreStoryBeats[0].Summary = "mutated"
	clone.Analysis.Characters[0].ExplicitMotivations = append(clone.Analysis.Characters[0].ExplicitMotivations, "mutated")
	if artifact.Analysis.Characters[0].Role != "protagonist" || artifact.Analysis.Relationships[0].Parties[0] != "The traveller" || artifact.Analysis.CoreStoryBeats[0].Summary != "The traveller follows the lantern." || len(artifact.Analysis.Characters[0].ExplicitMotivations) != 0 {
		t.Fatalf("clone mutation changed retained analysis: %#v", artifact.Analysis)
	}

	nilClone := cloneStoryAnalysis(storygeneration.StoryAnalysis{
		Characters:    []storygeneration.Character{{}},
		Relationships: []storygeneration.Relationship{{}},
	})
	if nilClone.DevelopmentBeats != nil || nilClone.Characters[0].ExplicitMotivations != nil || nilClone.Relationships[0].Parties != nil {
		t.Fatalf("clone changed nil slice state: %#v", nilClone)
	}
}

func TestRunIsolatesRetainedAnalysisFromGenerationRunner(t *testing.T) {
	input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
	generator.mutateAnalysisDuringGeneration = true

	result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := result.AnalysisArtifact.Validate(); err != nil {
		t.Fatalf("retained StoryAnalysis artifact.Validate() error = %v", err)
	}
	if got := result.AnalysisArtifact.Analysis.Characters[0].Role; got != "protagonist" {
		t.Fatalf("retained StoryAnalysis role = %q, want original role", got)
	}
	for index, generationInput := range generator.editionInputs {
		if got := generationInput.AnalysisArtifact.Analysis.Characters[0].Role; got != "mutated by generation runner" {
			t.Fatalf("generation input %d was not mutated by fake: role = %q", index+1, got)
		}
	}
	if len(validator.editionInputs) != len(storygeneration.DerivedEditionKeysV2()) || len(validator.bundleInputs) != 1 {
		t.Fatalf("isolation did not complete validation: edition-validation=%d bundle-validation=%d", len(validator.editionInputs), len(validator.bundleInputs))
	}
}

func TestRunIsolatesRetainedAnalysisFromSemanticValidator(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*fakeSemanticValidator)
		mutatedRole func(*fakeSemanticValidator) string
	}{
		{
			name: "edition validation",
			mutate: func(validator *fakeSemanticValidator) {
				validator.mutateAnalysisDuringEditionValidation = true
			},
			mutatedRole: func(validator *fakeSemanticValidator) string {
				return validator.editionInputs[0].AnalysisArtifact.Analysis.Characters[0].Role
			},
		},
		{
			name: "bundle validation",
			mutate: func(validator *fakeSemanticValidator) {
				validator.mutateAnalysisDuringBundleValidation = true
			},
			mutatedRole: func(validator *fakeSemanticValidator) string {
				return validator.bundleInputs[0].AnalysisArtifact.Analysis.Characters[0].Role
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
			test.mutate(validator)

			result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if err := result.AnalysisArtifact.Validate(); err != nil {
				t.Fatalf("retained StoryAnalysis artifact.Validate() error = %v", err)
			}
			if got := result.AnalysisArtifact.Analysis.Characters[0].Role; got != "protagonist" {
				t.Fatalf("retained StoryAnalysis role = %q, want original role", got)
			}
			if got := test.mutatedRole(validator); got == "protagonist" {
				t.Fatal("semantic-validator fake did not mutate its isolated analysis input")
			}
			if len(generator.editionInputs) != len(storygeneration.DerivedEditionKeysV2()) || len(validator.editionInputs) != len(storygeneration.DerivedEditionKeysV2()) || len(validator.bundleInputs) != 1 {
				t.Fatalf("isolation did not complete flow: generation=%d edition-validation=%d bundle-validation=%d", len(generator.editionInputs), len(validator.editionInputs), len(validator.bundleInputs))
			}
		})
	}
}

func TestRunRejectsInputWithoutImmutableIdentity(t *testing.T) {
	input, generator, validator := testServices(t, nil, adaptationcontract.ResultPass)
	input.SourceIdentity = " "
	result, err := newOrchestrator(t, generator, validator).Run(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Run() error = %v", err)
	}
	assertZeroResult(t, result)
	if len(generator.analysisInputs) != 0 {
		t.Fatalf("AnalyseSource calls = %d, want 0", len(generator.analysisInputs))
	}
}

type fakeGenerationRunner struct {
	analysis                       storygeneration.StoryAnalysisArtifact
	analysisErr                    error
	editions                       map[model.AdminStoryEditionKey]storygeneration.GeneratedEditionArtifact
	editionErrs                    map[model.AdminStoryEditionKey]error
	mutateAnalysisDuringGeneration bool
	analysisInputs                 []storygeneration.SourceAnalysisPromptInput
	editionInputs                  []storygeneration.GenerateEditionInput
}

func (runner *fakeGenerationRunner) AnalyseSource(_ context.Context, input storygeneration.SourceAnalysisPromptInput) (storygeneration.StoryAnalysisArtifact, error) {
	runner.analysisInputs = append(runner.analysisInputs, input)
	if runner.analysisErr != nil {
		return storygeneration.StoryAnalysisArtifact{}, runner.analysisErr
	}
	return runner.analysis, nil
}

func (runner *fakeGenerationRunner) GenerateEdition(_ context.Context, input storygeneration.GenerateEditionInput) (storygeneration.GeneratedEditionArtifact, error) {
	runner.editionInputs = append(runner.editionInputs, input)
	if runner.mutateAnalysisDuringGeneration && len(input.AnalysisArtifact.Analysis.Characters) > 0 {
		input.AnalysisArtifact.Analysis.Characters[0].Role = "mutated by generation runner"
	}
	if err := runner.editionErrs[input.EditionKey]; err != nil {
		return storygeneration.GeneratedEditionArtifact{}, err
	}
	artifact, ok := runner.editions[input.EditionKey]
	if !ok {
		return storygeneration.GeneratedEditionArtifact{}, fmt.Errorf("unexpected generated edition key %q", input.EditionKey)
	}
	return artifact, nil
}

type fakeSemanticValidator struct {
	editionArtifacts                      map[model.AdminStoryEditionKey]storyvalidation.AssessmentArtifact
	editionErrs                           map[model.AdminStoryEditionKey]error
	bundleArtifact                        storyvalidation.AssessmentArtifact
	bundleErr                             error
	mutateAnalysisDuringEditionValidation bool
	mutateAnalysisDuringBundleValidation  bool
	editionInputs                         []storyvalidation.EditionValidationPromptInput
	bundleInputs                          []storyvalidation.BundleValidationPromptInput
}

func (validator *fakeSemanticValidator) ValidateEdition(_ context.Context, input storyvalidation.EditionValidationPromptInput) (storyvalidation.AssessmentArtifact, error) {
	validator.editionInputs = append(validator.editionInputs, input)
	if validator.mutateAnalysisDuringEditionValidation && len(input.AnalysisArtifact.Analysis.Characters) > 0 {
		input.AnalysisArtifact.Analysis.Characters[0].Role = "mutated by edition validator"
	}
	if err := validator.editionErrs[input.GeneratedEdition.EditionKey]; err != nil {
		return storyvalidation.AssessmentArtifact{}, err
	}
	artifact, ok := validator.editionArtifacts[input.GeneratedEdition.EditionKey]
	if !ok {
		return storyvalidation.AssessmentArtifact{}, fmt.Errorf("unexpected validation edition key %q", input.GeneratedEdition.EditionKey)
	}
	return artifact, nil
}

func (validator *fakeSemanticValidator) ValidateBundle(_ context.Context, input storyvalidation.BundleValidationPromptInput) (storyvalidation.AssessmentArtifact, error) {
	validator.bundleInputs = append(validator.bundleInputs, input)
	if validator.mutateAnalysisDuringBundleValidation && len(input.AnalysisArtifact.Analysis.Characters) > 0 {
		input.AnalysisArtifact.Analysis.Characters[0].Role = "mutated by bundle validator"
	}
	if validator.bundleErr != nil {
		return storyvalidation.AssessmentArtifact{}, validator.bundleErr
	}
	return validator.bundleArtifact, nil
}

func testServices(
	t *testing.T,
	editionResults map[model.AdminStoryEditionKey]adaptationcontract.Result,
	bundleResult adaptationcontract.Result,
) (Input, *fakeGenerationRunner, *fakeSemanticValidator) {
	t.Helper()
	input := Input{
		SourceIdentity:  "source-version-test-1",
		Title:           "The Lantern Tale",
		Author:          "A. Author",
		Slug:            "the-lantern-tale",
		Language:        "en-GB",
		SourceURL:       "https://example.test/sources/lantern",
		Rights:          map[string]any{"status": "public-domain"},
		CanonicalSource: "# The Lantern Tale\n\nAn exact canonical source follows the lantern home.\n",
	}
	analysis := testAnalysisArtifact(t, input.CanonicalSource)
	keys := storygeneration.DerivedEditionKeysV2()
	editions := make(map[model.AdminStoryEditionKey]storygeneration.GeneratedEditionArtifact, len(keys))
	orderedEditions := make([]storygeneration.GeneratedEditionArtifact, 0, len(keys))
	for _, key := range keys {
		edition := testGeneratedEditionArtifact(t, input, analysis, key)
		editions[key] = edition
		orderedEditions = append(orderedEditions, edition)
	}

	generator := &fakeGenerationRunner{
		analysis:    analysis,
		editions:    editions,
		editionErrs: make(map[model.AdminStoryEditionKey]error),
	}
	validator := &fakeSemanticValidator{
		editionArtifacts: make(map[model.AdminStoryEditionKey]storyvalidation.AssessmentArtifact, len(keys)),
		editionErrs:      make(map[model.AdminStoryEditionKey]error),
		bundleArtifact:   testBundleAssessmentArtifact(t, analysis, orderedEditions, bundleResult),
	}
	for _, edition := range orderedEditions {
		result := adaptationcontract.ResultPass
		if editionResults != nil {
			if configured, ok := editionResults[edition.EditionKey]; ok {
				result = configured
			}
		}
		validator.editionArtifacts[edition.EditionKey] = testEditionAssessmentArtifact(t, analysis, edition, result)
	}
	return input, generator, validator
}

func newOrchestrator(t *testing.T, generator GenerationRunner, validator SemanticValidator) *Orchestrator {
	t.Helper()
	orchestrator, err := New(Config{Generator: generator, Validator: validator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return orchestrator
}

func testAnalysisArtifact(t *testing.T, source string) storygeneration.StoryAnalysisArtifact {
	t.Helper()
	analysis := storygeneration.StoryAnalysis{
		CentralPlot: "A traveller follows a lantern home.",
		Characters: []storygeneration.Character{{
			Name:                "The traveller",
			Role:                "protagonist",
			ExplicitMotivations: []string{"Find home"},
		}},
		CoreStoryBeats: []storygeneration.StoryBeat{{Summary: "The traveller follows the lantern."}},
	}
	artifact := storygeneration.StoryAnalysisArtifact{
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        storygeneration.SourceAnalysisPromptVersionV3,
		RequestedModel:       storygeneration.GenerationModelV2,
		ReturnedModel:        storygeneration.GenerationModelV2,
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         sha256Hex(source),
		AnalysisSHA256:       jsonSHA256(t, analysis),
		Analysis:             analysis,
		ResponseID:           "resp-analysis-test",
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("test analysis artifact.Validate() error = %v", err)
	}
	return artifact
}

func testGeneratedEditionArtifact(
	t *testing.T,
	input Input,
	analysis storygeneration.StoryAnalysisArtifact,
	key model.AdminStoryEditionKey,
) storygeneration.GeneratedEditionArtifact {
	t.Helper()
	markdown := fmt.Sprintf("# %s\n\nThe traveller follows the lantern in the %s edition.\n", input.Title, key)
	structural := adaptationcontract.ValidateGeneratedEdition(adaptationcontract.GeneratedEditionInput{
		EditionKey: key,
		Slug:       input.Slug,
		Title:      input.Title,
		Author:     input.Author,
		Markdown:   markdown,
		Language:   input.Language,
		SourceURL:  input.SourceURL,
		Rights:     input.Rights,
	})
	if !structural.Passed() {
		t.Fatalf("test structural validation for %q failed: %#v", key, structural.Findings)
	}
	artifact := storygeneration.GeneratedEditionArtifact{
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        storygeneration.EditionPromptVersionV4,
		EditionKey:           key,
		RequestedModel:       storygeneration.GenerationModelV2,
		ReturnedModel:        storygeneration.GenerationModelV2,
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         analysis.SourceSHA256,
		AnalysisSHA256:       analysis.AnalysisSHA256,
		ContentSHA256:        sha256Hex(markdown),
		Markdown:             markdown,
		ResponseID:           "resp-" + string(key),
		StructuralValidation: structural,
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("test generated artifact.Validate() error = %v", err)
	}
	return artifact
}

func testEditionAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	edition storygeneration.GeneratedEditionArtifact,
	result adaptationcontract.Result,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	key := edition.EditionKey
	assessment := storyvalidation.Assessment{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &key,
		Result:               result,
		Findings:             findingsForEditionResult(result, key),
	}
	return testAssessmentArtifact(t, analysis, []storygeneration.GeneratedEditionArtifact{edition}, assessment)
}

func testBundleAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	result adaptationcontract.Result,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	keys := editionKeys(editions)
	assessment := storyvalidation.Assessment{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys:          keys,
		Result:               result,
		Findings:             findingsForBundleResult(result, keys),
	}
	return testAssessmentArtifact(t, analysis, editions, assessment)
}

func testAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	assessment storyvalidation.Assessment,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	promptVersion := storyvalidation.EditionJudgementPromptVersionV3
	if assessment.AssessmentScope == adaptationcontract.AssessmentScopeBundle {
		promptVersion = storyvalidation.BundleJudgementPromptVersionV3
	}
	bindings := make([]storyvalidation.EditionBinding, 0, len(editions))
	for _, edition := range editions {
		bindings = append(bindings, storyvalidation.EditionBinding{EditionKey: edition.EditionKey, ContentSHA256: edition.ContentSHA256})
	}
	artifact := storyvalidation.AssessmentArtifact{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        promptVersion,
		AssessmentScope:      assessment.AssessmentScope,
		EditionKey:           assessment.EditionKey,
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), assessment.EditionKeys...),
		RequestedModel:       "semantic-validator-test",
		ReturnedModel:        "semantic-validator-test",
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         analysis.SourceSHA256,
		AnalysisSHA256:       analysis.AnalysisSHA256,
		EditionBindings:      bindings,
		AssessmentSHA256:     jsonSHA256(t, assessment),
		Assessment:           assessment,
		ResponseID:           "resp-semantic-test",
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("test assessment artifact.Validate() error = %v", err)
	}
	return artifact
}

func findingsForEditionResult(result adaptationcontract.Result, key model.AdminStoryEditionKey) []storyvalidation.Finding {
	switch result {
	case adaptationcontract.ResultPass:
		return []storyvalidation.Finding{}
	case adaptationcontract.ResultNeedsReview:
		return []storyvalidation.Finding{{
			Code:     adaptationcontract.FindingScopeTooRich,
			Severity: adaptationcontract.FindingSeverityReview,
			Message:  "Editorial review is required.",
			Evidence: []storyvalidation.Evidence{{
				Location:    storyvalidation.EvidenceGeneratedEdition,
				EditionKey:  &key,
				Excerpt:     "traveller",
				Explanation: "The generated edition needs editorial review.",
			}},
		}}
	case adaptationcontract.ResultFail:
		return []storyvalidation.Finding{{
			Code:     adaptationcontract.FindingMotivationChanged,
			Severity: adaptationcontract.FindingSeverityBlocking,
			Message:  "The source-grounded motivation changed.",
			Evidence: []storyvalidation.Evidence{{
				Location:    storyvalidation.EvidenceGeneratedEdition,
				EditionKey:  &key,
				Excerpt:     "traveller",
				Explanation: "The generated edition changes a source-grounded motivation.",
			}},
		}}
	default:
		panic("unsupported test edition result")
	}
}

func findingsForBundleResult(result adaptationcontract.Result, keys []model.AdminStoryEditionKey) []storyvalidation.Finding {
	switch result {
	case adaptationcontract.ResultPass:
		return []storyvalidation.Finding{}
	case adaptationcontract.ResultNeedsReview:
		return []storyvalidation.Finding{{
			Code:     adaptationcontract.FindingEditionProgressionQuestionable,
			Severity: adaptationcontract.FindingSeverityReview,
			Message:  "The progression needs editorial review.",
			Evidence: []storyvalidation.Evidence{{
				Location:    storyvalidation.EvidenceGeneratedEdition,
				EditionKey:  &keys[0],
				Excerpt:     "traveller",
				Explanation: "The compared edition needs editorial review.",
			}},
		}}
	case adaptationcontract.ResultFail:
		return []storyvalidation.Finding{{
			Code:     adaptationcontract.FindingEditionProgressionNotDistinct,
			Severity: adaptationcontract.FindingSeverityBlocking,
			Message:  "The progression is not distinct enough.",
			Evidence: []storyvalidation.Evidence{{
				Location:    storyvalidation.EvidenceGeneratedEdition,
				EditionKey:  &keys[0],
				Excerpt:     "traveller",
				Explanation: "The compared edition establishes the blocking finding.",
			}},
		}}
	default:
		panic("unsupported test bundle result")
	}
}

func editionKeys(editions []storygeneration.GeneratedEditionArtifact) []model.AdminStoryEditionKey {
	keys := make([]model.AdminStoryEditionKey, 0, len(editions))
	for _, edition := range editions {
		keys = append(keys, edition.EditionKey)
	}
	return keys
}

func jsonSHA256(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return sha256Hex(string(encoded))
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func assertZeroResult(t *testing.T, result Result) {
	t.Helper()
	if !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("Run() result = %#v, want zero result on failure", result)
	}
}
