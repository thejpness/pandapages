package storygeneration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

func validStoryAnalysisArtifactForSource(t *testing.T, source string) StoryAnalysisArtifact {
	t.Helper()
	analysis := validStoryAnalysis()
	digest, err := storyAnalysisSHA256(analysis)
	if err != nil {
		t.Fatalf("storyAnalysisSHA256() error = %v", err)
	}
	return StoryAnalysisArtifact{
		SpecificationVersion: SpecificationV2,
		PromptVersion:        SourceAnalysisPromptVersionV2,
		RequestedModel:       GenerationModelV2,
		ReturnedModel:        GenerationModelV2,
		ReasoningEffort:      ReasoningEffortHigh,
		SourceSHA256:         exactStringSHA256(source),
		AnalysisSHA256:       digest,
		Analysis:             analysis,
		ResponseID:           "resp_analysis",
	}
}

func validGenerateEditionInput(t *testing.T, source string) GenerateEditionInput {
	t.Helper()
	return GenerateEditionInput{
		EditionKey:       model.AdminStoryEditionStoryExplorers,
		Title:            "Jack and the Beanstalk",
		Author:           "Traditional",
		Slug:             "jack-and-the-beanstalk",
		Language:         "en-GB",
		SourceURL:        "https://example.test/source",
		Rights:           map[string]any{"status": "public-domain"},
		CanonicalSource:  source,
		AnalysisArtifact: validStoryAnalysisArtifactForSource(t, source),
	}
}

func TestGenerateEditionUsesOneLockedTerraPlainTextCallAndDeterministicValidation(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source."
	markdown := "# Jack and the Beanstalk\n\nJack climbed the enormous beanstalk."

	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_generation",
			Model:      GenerationModelV2,
			OutputText: markdown,
			Usage: ResponsesUsage{
				InputTokens:     2000,
				CachedTokens:    250,
				OutputTokens:    500,
				ReasoningTokens: 200,
				TotalTokens:     2500,
			},
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	artifact, err := runner.GenerateEdition(context.Background(), validGenerateEditionInput(t, source))
	if err != nil {
		t.Fatalf("GenerateEdition() error = %v", err)
	}

	if len(gateway.calls) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
	}
	call := gateway.calls[0]
	if call.Model != GenerationModelV2 {
		t.Fatalf("model = %q, want %q", call.Model, GenerationModelV2)
	}
	if call.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("reasoning effort = %q", call.ReasoningEffort)
	}
	if call.MaxOutputTokens != 32000 {
		t.Fatalf("max output tokens = %d", call.MaxOutputTokens)
	}
	if call.Prompt.Version != EditionPromptVersionV4 {
		t.Fatalf("prompt version = %q", call.Prompt.Version)
	}
	if call.StructuredOutput != nil {
		t.Fatal("edition Markdown generation must not request JSON Structured Output")
	}
	var promptInput editionUserInput
	if err := json.Unmarshal([]byte(call.Prompt.UserInputJSON), &promptInput); err != nil {
		t.Fatalf("json.Unmarshal(UserInputJSON) error = %v", err)
	}
	if promptInput.CanonicalSource != source {
		t.Fatal("edition prompt must preserve exact canonical source as user data")
	}
	if promptInput.EditionKey != model.AdminStoryEditionStoryExplorers {
		t.Fatalf("edition prompt key = %q", promptInput.EditionKey)
	}

	if artifact.SpecificationVersion != SpecificationV2 ||
		artifact.PromptVersion != EditionPromptVersionV4 ||
		artifact.EditionKey != model.AdminStoryEditionStoryExplorers ||
		artifact.RequestedModel != GenerationModelV2 ||
		artifact.ReturnedModel != GenerationModelV2 ||
		artifact.ResponseID != "resp_generation" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if artifact.SourceSHA256 != exactStringSHA256(source) {
		t.Fatalf("source digest = %q", artifact.SourceSHA256)
	}
	if artifact.ContentSHA256 != exactStringSHA256(markdown) {
		t.Fatalf("content digest = %q", artifact.ContentSHA256)
	}
	if artifact.StructuralValidation.ContractVersion != adaptationcontract.VersionV1 {
		t.Fatalf("structural version = %q", artifact.StructuralValidation.ContractVersion)
	}
	if !artifact.StructuralValidation.Passed() {
		t.Fatalf("structural findings = %#v", artifact.StructuralValidation.Findings)
	}
	if artifact.Usage.TotalTokens != 2500 {
		t.Fatalf("usage = %#v", artifact.Usage)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
}

func TestGenerateEditionAcceptsV3SourceAnalysisArtifact(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nCanonical source."
	markdown := "# Jack and the Beanstalk\n\nGenerated story."
	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_generation",
			Model:      GenerationModelV2,
			OutputText: markdown,
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	input := validGenerateEditionInput(t, source)
	input.AnalysisArtifact.PromptVersion = SourceAnalysisPromptVersionV3
	if _, err := runner.GenerateEdition(context.Background(), input); err != nil {
		t.Fatalf("GenerateEdition() error = %v", err)
	}
	if len(gateway.calls) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
	}
}

func TestGenerateEditionRejectsSourceOrAnalysisMismatchBeforeGateway(t *testing.T) {
	source := "# Story\n\nCanonical source."
	gateway := &fakeResponsesGateway{}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	t.Run("classic rejected", func(t *testing.T) {
		input := validGenerateEditionInput(t, source)
		input.EditionKey = model.AdminStoryEditionClassic

		_, err := runner.GenerateEdition(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "canonical v2 derived edition key") {
			t.Fatalf("GenerateEdition() error = %v", err)
		}
	})

	t.Run("source mismatch rejected", func(t *testing.T) {
		input := validGenerateEditionInput(t, source)
		input.CanonicalSource += "\nchanged"

		_, err := runner.GenerateEdition(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "does not match canonical source") {
			t.Fatalf("GenerateEdition() error = %v", err)
		}
	})

	t.Run("tampered analysis rejected", func(t *testing.T) {
		input := validGenerateEditionInput(t, source)
		input.AnalysisArtifact.Analysis.CentralPlot += " changed"

		_, err := runner.GenerateEdition(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "StoryAnalysis artifact is invalid") {
			t.Fatalf("GenerateEdition() error = %v", err)
		}
	})

	if len(gateway.calls) != 0 {
		t.Fatalf("gateway calls = %d, want 0", len(gateway.calls))
	}
}

func TestGenerateEditionPreservesProviderError(t *testing.T) {
	source := "# Story\n\nCanonical source."
	sentinel := errors.New("provider failed")
	gateway := &fakeResponsesGateway{err: sentinel}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	_, err = runner.GenerateEdition(context.Background(), validGenerateEditionInput(t, source))
	if !errors.Is(err, sentinel) {
		t.Fatalf("GenerateEdition() error = %v, want wrapped sentinel", err)
	}
}

func TestGenerateEditionReturnsFailedArtifactForStructuralInspection(t *testing.T) {
	source := "# Story\n\nCanonical source."
	invalidMarkdown := "No H1 here.\n\n<p>raw html</p>"

	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_bad_generation",
			Model:      GenerationModelV2,
			OutputText: invalidMarkdown,
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	input := validGenerateEditionInput(t, source)
	input.Title = "Story"
	input.Slug = "story"

	artifact, err := runner.GenerateEdition(context.Background(), input)
	if !errors.Is(err, ErrGeneratedEditionStructurallyInvalid) {
		t.Fatalf("GenerateEdition() error = %v, want structural failure", err)
	}
	if artifact.Markdown != invalidMarkdown {
		t.Fatal("failed artifact must preserve generated Markdown for inspection")
	}
	if artifact.ResponseID != "resp_bad_generation" {
		t.Fatalf("response ID = %q", artifact.ResponseID)
	}
	if artifact.StructuralValidation.Passed() {
		t.Fatal("structurally invalid generation must not pass")
	}

	codes := make(map[adaptationcontract.FindingCode]bool)
	for _, finding := range artifact.StructuralValidation.Findings {
		codes[finding.Code] = true
	}
	if !codes[adaptationcontract.FindingMissingH1Title] {
		t.Fatalf("findings = %#v, want missing_h1_title", artifact.StructuralValidation.Findings)
	}
	if !codes[adaptationcontract.FindingRawHTMLPresent] {
		t.Fatalf("findings = %#v, want raw_html_present", artifact.StructuralValidation.Findings)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("failed artifact must remain internally valid for inspection: %v", err)
	}
}

func TestGeneratedEditionArtifactValidateDetectsMarkdownTampering(t *testing.T) {
	source := "# Story\n\nCanonical source."
	markdown := "# Story\n\nGenerated."

	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_generation",
			Model:      GenerationModelV2,
			OutputText: markdown,
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	input := validGenerateEditionInput(t, source)
	input.Title = "Story"
	input.Slug = "story"

	artifact, err := runner.GenerateEdition(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateEdition() error = %v", err)
	}
	artifact.Markdown += " changed"

	err = artifact.Validate()
	if err == nil || !strings.Contains(err.Error(), "content digest does not match Markdown") {
		t.Fatalf("Validate() error = %v, want digest mismatch", err)
	}
}

func TestGeneratedEditionArtifactValidateSupportsKnownPromptVersions(t *testing.T) {
	source := "# Story\n\nCanonical source."
	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_generation",
			Model:      GenerationModelV2,
			OutputText: "# Story\n\nGenerated story.",
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	input := validGenerateEditionInput(t, source)
	input.Title = "Story"
	input.Slug = "story"
	artifact, err := runner.GenerateEdition(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateEdition() error = %v", err)
	}
	if artifact.PromptVersion != EditionPromptVersionV4 {
		t.Fatalf("active artifact prompt version = %q", artifact.PromptVersion)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("V4 artifact.Validate() error = %v", err)
	}

	for _, version := range []PromptVersion{EditionPromptVersionV2, EditionPromptVersionV3} {
		historical := artifact
		historical.PromptVersion = version
		if err := historical.Validate(); err != nil {
			t.Fatalf("historical %s artifact.Validate() error = %v", version, err)
		}
	}

	unknown := artifact
	unknown.PromptVersion = "panda-pages-edition-generation-prompt-unknown"
	if err := unknown.Validate(); err == nil || !strings.Contains(err.Error(), "prompt version") {
		t.Fatalf("unknown prompt artifact.Validate() error = %v", err)
	}
}
