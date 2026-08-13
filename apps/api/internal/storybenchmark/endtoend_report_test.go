package storybenchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/storygeneration"
)

func TestBuildEndToEndResultDocumentAggregatesGenerationAndValidationUsage(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	if document.Suite != EndToEndSuite || document.Run.Status != TrialStatusComplete {
		t.Fatalf("document = %#v", document)
	}
	if document.Source.ExternalID != "14407" || document.Source.OverallStatus != "eligible" {
		t.Fatalf("source = %#v", document.Source)
	}
	if document.Usage.Generation.AnalysisResponses != 1 || document.Usage.Generation.EditionResponses != 4 {
		t.Fatalf("generation usage = %#v", document.Usage.Generation)
	}
	if document.Usage.CompletedResponses != 10 {
		t.Fatalf("completed responses = %d, want 10", document.Usage.CompletedResponses)
	}
	if document.Usage.Usage.TotalTokens != 1850 {
		t.Fatalf("total tokens = %d, want 1850", document.Usage.Usage.TotalTokens)
	}
}

func TestMarshalAndRenderEndToEndResult(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	encoded, err := MarshalEndToEndResultJSON(document)
	if err != nil {
		t.Fatalf("MarshalEndToEndResultJSON() error = %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, field := range []string{"benchmarkVersion", "suite", "startedAt", "finishedAt", "source", "generationConfig", "run", "usage", "responsesApiTelemetry"} {
		if _, exists := decoded[field]; !exists {
			t.Fatalf("encoded end-to-end result is missing %q", field)
		}
	}
	markdown, err := RenderEndToEndMarkdown(document)
	if err != nil {
		t.Fatalf("RenderEndToEndMarkdown() error = %v", err)
	}
	for _, required := range []string{"human-review-template.json", "not publication approval", "14407", "Responses API telemetry", "Retained artifact telemetry"} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("markdown does not contain %q:\n%s", required, markdown)
		}
	}
}

func TestWriteEndToEndResultArtifactsIncludesHumanReviewTemplate(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	directory := filepath.Join(t.TempDir(), "run")
	written, err := WriteEndToEndResultArtifacts(directory, document)
	if err != nil {
		t.Fatalf("WriteEndToEndResultArtifacts() error = %v", err)
	}
	for _, path := range []string{written.ResultJSON, written.ReportMD, written.HumanReviewTemplate} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filepath.Base(path), info.Mode().Perm())
		}
	}
	reviewData, err := os.ReadFile(written.HumanReviewTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var review HumanReviewDocument
	if err := decodeStrictJSON(reviewData, &review); err != nil {
		t.Fatalf("decode review template: %v", err)
	}
	if len(review.Targets) != 5 {
		t.Fatalf("review target count = %d, want 5", len(review.Targets))
	}
	for _, target := range review.Targets {
		if target.ReviewStatus != HumanReviewPending {
			t.Fatalf("review target status = %q", target.ReviewStatus)
		}
	}
}

func TestBuildEndToEndResultRejectsSourceBindingDrift(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	document.Run.Generations[0].AnalysisArtifact.SourceSHA256 = strings.Repeat("a", 64)
	if _, err := MarshalEndToEndResultJSON(document); err == nil || !strings.Contains(err.Error(), "source binding") {
		t.Fatalf("MarshalEndToEndResultJSON() error = %v", err)
	}
}

func TestEndToEndResultRejectsCompleteGenerationMissingEdition(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	document.Run.Generations[0].Editions = document.Run.Generations[0].Editions[:3]
	if _, err := MarshalEndToEndResultJSON(document); err == nil || !strings.Contains(err.Error(), "all canonical derived editions") {
		t.Fatalf("MarshalEndToEndResultJSON() error = %v", err)
	}
}

func TestEndToEndResultRejectsValidationTrialSourceIDDrift(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	document.Run.Generations[0].ValidationTrials[0].CaseID = "other-source"
	if _, err := MarshalEndToEndResultJSON(document); err == nil || !strings.Contains(err.Error(), "does not match result source") {
		t.Fatalf("MarshalEndToEndResultJSON() error = %v", err)
	}
}

func TestEndToEndResultRejectsGenerationConfigDrift(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	document.GenerationConfig.AnalysisReasoningEffort = storygeneration.ReasoningEffortHigh
	if _, err := MarshalEndToEndResultJSON(document); err == nil || !strings.Contains(err.Error(), "source-analysis configuration") {
		t.Fatalf("MarshalEndToEndResultJSON() error = %v", err)
	}
}

func TestLoadEndToEndResultDocumentRoundTripsStrictResult(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	directory := filepath.Join(t.TempDir(), "run")
	written, err := WriteEndToEndResultArtifacts(directory, document)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEndToEndResultDocument(written.ResultJSON)
	if err != nil {
		t.Fatalf("LoadEndToEndResultDocument() error = %v", err)
	}
	if loaded.Source.SourceSHA256 != document.Source.SourceSHA256 || loaded.GenerationConfig != document.GenerationConfig {
		t.Fatalf("loaded result binding changed: %#v", loaded)
	}
}

func endToEndDocumentForHumanReviewTest(t *testing.T) EndToEndResultDocument {
	t.Helper()
	fixture, err := LoadPublicDomainFixture("testdata/publicdomain/benjamin-bunny")
	if err != nil {
		t.Fatalf("LoadPublicDomainFixture() error = %v", err)
	}
	controlled := loadControlledFixturesForRunnerTest(t)
	analysisJSON, err := json.Marshal(controlled.Story.Analysis)
	if err != nil {
		t.Fatalf("json.Marshal(StoryAnalysis) error = %v", err)
	}
	clean := controlledCaseByIDForTest(t, controlled, "clean-control").Editions[0].Markdown
	generationGateway := &deterministicGenerationGateway{analysisJSON: string(analysisJSON), markdown: clean}
	generator, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
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
	runner, err := NewRunner(RunnerConfig{Generator: generator, ValidatorFactory: passValidatorFactoryForTest(&validatorGateways)})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	run, err := runner.RunEndToEnd(context.Background(), fixture.Source, EndToEndRunConfig{
		GenerationRepetitions: 1,
		ValidationRepetitions: 1,
		Validators:            []ValidatorConfig{validValidatorConfigForTest()},
	})
	if err != nil {
		t.Fatalf("RunEndToEnd() error = %v", err)
	}
	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	document, err := BuildEndToEndResultDocument(start, start.Add(time.Second), fixture, endToEndGenerationConfigForTest(), run)
	if err != nil {
		t.Fatalf("BuildEndToEndResultDocument() error = %v", err)
	}
	return document
}

func endToEndGenerationConfigForTest() EndToEndGenerationConfig {
	return EndToEndGenerationConfig{
		Model:                   storygeneration.GenerationModelV2,
		AnalysisReasoningEffort: storygeneration.ReasoningEffortMedium,
		AnalysisMaxOutputTokens: 4096,
		EditionReasoningEffort:  storygeneration.ReasoningEffortMedium,
		EditionMaxOutputTokens:  4096,
	}
}
