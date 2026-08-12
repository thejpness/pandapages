package storyvalidation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func validAnalysisArtifact(t *testing.T, source string) storygeneration.StoryAnalysisArtifact {
	t.Helper()
	analysis := storygeneration.StoryAnalysis{
		CentralPlot: "A protagonist acts, faces escalating consequences, and reaches the source resolution.",
		Characters: []storygeneration.Character{
			{
				Name:                "Jack",
				Role:                "protagonist",
				ExplicitMotivations: []string{"Improve his and his mother's poverty"},
				FlawsOrAmbiguities:  []string{"Impulsive"},
			},
		},
		Relationships: []storygeneration.Relationship{},
		CoreStoryBeats: []storygeneration.StoryBeat{
			{Summary: "Jack begins in poverty."},
			{Summary: "Jack's actions create escalating consequences."},
		},
		DevelopmentBeats:   []storygeneration.StoryBeat{},
		EnrichmentMaterial: []storygeneration.StoryBeat{},
		CausalDependencies: []storygeneration.CausalDependency{},
		IconicMaterial:     []storygeneration.IconicMaterial{},
		IntenseMaterial:    []storygeneration.IntenseMaterial{},
		AdaptationRisks:    []storygeneration.AdaptationRisk{},
	}
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("json.Marshal(analysis) error = %v", err)
	}
	decoded, err := storygeneration.DecodeStoryAnalysisJSON(analysisJSON)
	if err != nil {
		t.Fatalf("DecodeStoryAnalysisJSON() error = %v", err)
	}

	// The digest helpers are intentionally private to storygeneration, so
	// obtain a real bound artefact through the public runner contract.
	gateway := &analysisGateway{output: string(analysisJSON)}
	runner, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
		Gateway:                 gateway,
		AnalysisReasoningEffort: storygeneration.ReasoningEffortHigh,
		AnalysisMaxOutputTokens: 4096,
		EditionReasoningEffort:  storygeneration.ReasoningEffortHigh,
		EditionMaxOutputTokens:  4096,
	})
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}
	artifact, err := runner.AnalyseSource(testContext(), storygeneration.SourceAnalysisPromptInput{
		Title:           "Jack and the Beanstalk",
		Author:          "Traditional",
		CanonicalSource: source,
	})
	if err != nil {
		t.Fatalf("AnalyseSource() error = %v", err)
	}
	artifact.Analysis = decoded
	return artifact
}

type analysisGateway struct {
	output string
}

func (gateway *analysisGateway) Create(_ context.Context, _ storygeneration.ResponsesCall) (storygeneration.ResponsesResult, error) {
	return storygeneration.ResponsesResult{
		ResponseID: "resp_analysis",
		Model:      storygeneration.GenerationModelV2,
		OutputText: gateway.output,
	}, nil
}

type editionGateway struct {
	output string
}

func (gateway *editionGateway) Create(_ context.Context, _ storygeneration.ResponsesCall) (storygeneration.ResponsesResult, error) {
	return storygeneration.ResponsesResult{
		ResponseID: "resp_generation",
		Model:      storygeneration.GenerationModelV2,
		OutputText: gateway.output,
	}, nil
}

func testContext() context.Context {
	return context.Background()
}

func validGeneratedEdition(t *testing.T, source string, analysis storygeneration.StoryAnalysisArtifact, key model.AdminStoryEditionKey, markdown string) storygeneration.GeneratedEditionArtifact {
	t.Helper()
	gateway := &editionGateway{output: markdown}
	runner, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
		Gateway:                 gateway,
		AnalysisReasoningEffort: storygeneration.ReasoningEffortHigh,
		AnalysisMaxOutputTokens: 4096,
		EditionReasoningEffort:  storygeneration.ReasoningEffortHigh,
		EditionMaxOutputTokens:  4096,
	})
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}
	artifact, err := runner.GenerateEdition(testContext(), storygeneration.GenerateEditionInput{
		EditionKey:       key,
		Title:            "Jack and the Beanstalk",
		Author:           "Traditional",
		Slug:             "jack-and-the-beanstalk",
		Language:         "en-GB",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
	})
	if err != nil {
		t.Fatalf("GenerateEdition() error = %v", err)
	}
	return artifact
}

func TestBuildEditionValidationPromptV2UsesBoundUntrustedData(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	generated := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGenerated growing-reader edition.",
	)

	prompt, err := BuildEditionValidationPromptV2(EditionValidationPromptInput{
		Title:            "Jack and the Beanstalk",
		Author:           "Traditional",
		CanonicalSource:  source,
		AnalysisArtifact: analysis,
		GeneratedEdition: generated,
	})
	if err != nil {
		t.Fatalf("BuildEditionValidationPromptV2() error = %v", err)
	}
	if prompt.Version != EditionValidationPromptVersionV2 {
		t.Fatalf("prompt version = %q", prompt.Version)
	}
	for _, marker := range []string{
		"ONE generated modern edition",
		"untrusted data",
		"canonical source is authoritative",
		"Compression may remove information. It must not manufacture replacement information.",
		"Do not report structural issues",
		"motivation_changed",
		"coercion_romanticised",
		"scope_too_rich",
		"needs_review",
		"do not provide chain-of-thought",
	} {
		if !strings.Contains(prompt.DeveloperInstructions, marker) {
			t.Fatalf("developer instructions missing %q", marker)
		}
	}
	if strings.Contains(prompt.DeveloperInstructions, source) {
		t.Fatal("canonical source must not be interpolated into developer instructions")
	}
	if strings.Contains(prompt.DeveloperInstructions, generated.Markdown) {
		t.Fatal("generated Markdown must not be interpolated into developer instructions")
	}

	var input editionValidationUserInput
	if err := json.Unmarshal([]byte(prompt.UserInputJSON), &input); err != nil {
		t.Fatalf("json.Unmarshal(UserInputJSON) error = %v", err)
	}
	if input.CanonicalSource != source {
		t.Fatal("canonical source must survive JSON encoding exactly")
	}
	if input.Edition.EditionKey != model.AdminStoryEditionGrowingReaders {
		t.Fatalf("edition key = %q", input.Edition.EditionKey)
	}
	if input.Edition.Markdown != generated.Markdown {
		t.Fatal("generated Markdown must survive JSON encoding exactly")
	}
	if input.StoryAnalysis.CentralPlot != analysis.Analysis.CentralPlot {
		t.Fatal("StoryAnalysis must be supplied as user data")
	}
}

func TestBuildBundleValidationPromptV2UsesCanonicalOrderedEditions(t *testing.T) {
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

	prompt, err := BuildBundleValidationPromptV2(BundleValidationPromptInput{
		Title:             "Jack and the Beanstalk",
		Author:            "Traditional",
		CanonicalSource:   source,
		AnalysisArtifact:  analysis,
		GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing, explorers},
	})
	if err != nil {
		t.Fatalf("BuildBundleValidationPromptV2() error = %v", err)
	}
	if prompt.Version != BundleValidationPromptVersionV2 {
		t.Fatalf("prompt version = %q", prompt.Version)
	}
	for _, marker := range []string{
		"EDITION PROGRESSION",
		"adjacent levels",
		"edition_progression_not_distinct",
		"edition_progression_inverted",
		"edition_progression_questionable",
		"not a repeat of each edition's source-fidelity assessment",
	} {
		if !strings.Contains(prompt.DeveloperInstructions, marker) {
			t.Fatalf("developer instructions missing %q", marker)
		}
	}
	if strings.Contains(prompt.DeveloperInstructions, "motivation_changed") {
		t.Fatal("bundle prompt must not offer edition-only finding codes")
	}

	var input bundleValidationUserInput
	if err := json.Unmarshal([]byte(prompt.UserInputJSON), &input); err != nil {
		t.Fatalf("json.Unmarshal(UserInputJSON) error = %v", err)
	}
	if len(input.Editions) != 2 {
		t.Fatalf("edition count = %d", len(input.Editions))
	}
	if input.Editions[0].EditionKey != model.AdminStoryEditionGrowingReaders ||
		input.Editions[1].EditionKey != model.AdminStoryEditionStoryExplorers {
		t.Fatalf("edition order = %#v", input.Editions)
	}
}

func TestValidationPromptBuildersFailClosedBeforeModelUse(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source text."
	analysis := validAnalysisArtifact(t, source)
	growing := validGeneratedEdition(
		t,
		source,
		analysis,
		model.AdminStoryEditionGrowingReaders,
		"# Jack and the Beanstalk\n\nGrowing edition.",
	)

	t.Run("source mismatch", func(t *testing.T) {
		_, err := BuildEditionValidationPromptV2(EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source + "\nchanged",
			AnalysisArtifact: analysis,
			GeneratedEdition: growing,
		})
		if err == nil || !strings.Contains(err.Error(), "does not match canonical source") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("tampered analysis", func(t *testing.T) {
		tampered := analysis
		tampered.Analysis.CentralPlot += " changed"
		_, err := BuildEditionValidationPromptV2(EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: tampered,
			GeneratedEdition: growing,
		})
		if err == nil || !strings.Contains(err.Error(), "StoryAnalysis artifact is invalid") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("tampered generated edition", func(t *testing.T) {
		tampered := growing
		tampered.Markdown += " changed"
		_, err := BuildEditionValidationPromptV2(EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: analysis,
			GeneratedEdition: tampered,
		})
		if err == nil || !strings.Contains(err.Error(), "generated-edition artifact is invalid") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("structural failure rejected", func(t *testing.T) {
		failed := growing
		failed.StructuralValidation.Findings = append(
			failed.StructuralValidation.Findings,
			adaptationcontract.Finding{
				Code:     adaptationcontract.FindingRawHTMLPresent,
				Severity: adaptationcontract.FindingSeverityBlocking,
				Message:  "raw HTML",
			},
		)
		_, err := BuildEditionValidationPromptV2(EditionValidationPromptInput{
			Title:            "Jack and the Beanstalk",
			CanonicalSource:  source,
			AnalysisArtifact: analysis,
			GeneratedEdition: failed,
		})
		if err == nil {
			t.Fatal("expected structural failure rejection")
		}
	})

	t.Run("bundle needs at least two", func(t *testing.T) {
		_, err := BuildBundleValidationPromptV2(BundleValidationPromptInput{
			Title:             "Jack and the Beanstalk",
			CanonicalSource:   source,
			AnalysisArtifact:  analysis,
			GeneratedEditions: []storygeneration.GeneratedEditionArtifact{growing},
		})
		if err == nil || !strings.Contains(err.Error(), "at least two") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("bundle canonical order enforced", func(t *testing.T) {
		explorers := validGeneratedEdition(
			t,
			source,
			analysis,
			model.AdminStoryEditionStoryExplorers,
			"# Jack and the Beanstalk\n\nExplorer edition.",
		)
		_, err := BuildBundleValidationPromptV2(BundleValidationPromptInput{
			Title:             "Jack and the Beanstalk",
			CanonicalSource:   source,
			AnalysisArtifact:  analysis,
			GeneratedEditions: []storygeneration.GeneratedEditionArtifact{explorers, growing},
		})
		if err == nil || !strings.Contains(err.Error(), "canonical modern edition order") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidationPromptVersionsAreLocked(t *testing.T) {
	if EditionValidationPromptVersionV2 != "panda-pages-edition-validation-prompt-v2" {
		t.Fatalf("EditionValidationPromptVersionV2 = %q", EditionValidationPromptVersionV2)
	}
	if BundleValidationPromptVersionV2 != "panda-pages-bundle-validation-prompt-v2" {
		t.Fatalf("BundleValidationPromptVersionV2 = %q", BundleValidationPromptVersionV2)
	}
}
