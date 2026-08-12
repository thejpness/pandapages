package storybenchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

func TestBuildControlledResultDocumentAggregatesUsage(t *testing.T) {
	config := ValidatorConfig{
		ID:              "luna-medium",
		Model:           "gpt-5.6-luna",
		ReasoningEffort: storygeneration.ReasoningEffortMedium,
		MaxOutputTokens: 8192,
	}
	artifact := storyvalidation.AssessmentArtifact{
		RequestedModel:  config.Model,
		ReasoningEffort: config.ReasoningEffort,
		Usage: storygeneration.ResponsesUsage{
			InputTokens:     100,
			CachedTokens:    20,
			OutputTokens:    30,
			ReasoningTokens: 10,
			TotalTokens:     130,
		},
	}
	run := ControlledRun{
		BenchmarkVersion:      VersionV1,
		Status:                TrialStatusIncomplete,
		ValidationRepetitions: 1,
		Validators:            []ValidatorConfig{config},
		AttemptedTrials:       2,
		CompletedTrials:       1,
		IncompleteTrials:      1,
		Trials: []ValidationTrial{
			{
				CaseID:             "clean-control",
				ValidatorConfigID:  config.ID,
				Status:             TrialStatusComplete,
				AssessmentArtifact: &artifact,
			},
			{
				CaseID:            "motivation-changed",
				ValidatorConfigID: config.ID,
				Status:            TrialStatusIncomplete,
				Error:             "synthetic transport failure",
			},
		},
		QualitySummary: Summary{Trials: 1, ExpectationsMet: 1, ResultMatches: 1},
	}

	start := time.Date(2026, 8, 12, 20, 0, 0, 0, time.FixedZone("BST", 3600))
	finish := start.Add(2 * time.Second)
	document, err := BuildControlledResultDocument(start, finish, run)
	if err != nil {
		t.Fatalf("BuildControlledResultDocument() error = %v", err)
	}
	if document.StartedAt != "2026-08-12T19:00:00Z" || document.FinishedAt != "2026-08-12T19:00:02Z" {
		t.Fatalf("timestamps = %q to %q", document.StartedAt, document.FinishedAt)
	}
	if document.Usage.CompletedResponses != 1 || document.Usage.Usage.TotalTokens != 130 {
		t.Fatalf("usage = %#v", document.Usage)
	}
	if len(document.Usage.ByValidator) != 1 || document.Usage.ByValidator[0].Usage.CachedTokens != 20 {
		t.Fatalf("validator usage = %#v", document.Usage.ByValidator)
	}
}

func TestBuildControlledResultDocumentRejectsMalformedCompleteTrial(t *testing.T) {
	run := ControlledRun{
		BenchmarkVersion:      VersionV1,
		Status:                TrialStatusComplete,
		ValidationRepetitions: 1,
		Validators: []ValidatorConfig{
			{ID: "luna-medium", Model: "gpt-5.6-luna", ReasoningEffort: storygeneration.ReasoningEffortMedium, MaxOutputTokens: 8192},
		},
		AttemptedTrials: 1,
		CompletedTrials: 1,
		Trials: []ValidationTrial{
			{CaseID: "clean-control", ValidatorConfigID: "luna-medium", Status: TrialStatusComplete},
		},
		QualitySummary: Summary{Trials: 1},
	}
	start := time.Date(2026, 8, 12, 19, 0, 0, 0, time.UTC)
	_, err := BuildControlledResultDocument(start, start.Add(time.Second), run)
	if err == nil || !strings.Contains(err.Error(), "complete without an assessment artifact") {
		t.Fatalf("BuildControlledResultDocument() error = %v", err)
	}
}

func TestRenderControlledMarkdownStatesPublicationBoundary(t *testing.T) {
	document := ControlledResultDocument{
		BenchmarkVersion: VersionV1,
		Suite:            ControlledSuite,
		StartedAt:        "2026-08-12T19:00:00Z",
		FinishedAt:       "2026-08-12T19:00:01Z",
		Run: ControlledRun{
			BenchmarkVersion: VersionV1,
			Status:           TrialStatusComplete,
		},
	}
	markdown, err := RenderControlledMarkdown(document)
	if err != nil {
		t.Fatalf("RenderControlledMarkdown() error = %v", err)
	}
	for _, required := range []string{"human editorial review", "not publication approval", "not publication", "Token telemetry"} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("markdown does not contain %q:\n%s", required, markdown)
		}
	}
}

func TestMarshalControlledResultJSONUsesStableTopLevelFields(t *testing.T) {
	document := ControlledResultDocument{
		BenchmarkVersion: VersionV1,
		Suite:            ControlledSuite,
		StartedAt:        "2026-08-12T19:00:00Z",
		FinishedAt:       "2026-08-12T19:00:01Z",
		Run:              ControlledRun{BenchmarkVersion: VersionV1},
	}
	encoded, err := MarshalControlledResultJSON(document)
	if err != nil {
		t.Fatalf("MarshalControlledResultJSON() error = %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, field := range []string{"benchmarkVersion", "suite", "startedAt", "finishedAt", "run", "usage"} {
		if _, exists := decoded[field]; !exists {
			t.Fatalf("encoded result is missing %q: %s", field, encoded)
		}
	}
}

func TestWriteControlledResultArtifactsIsExclusiveAndPrivate(t *testing.T) {
	document := ControlledResultDocument{
		BenchmarkVersion: VersionV1,
		Suite:            ControlledSuite,
		StartedAt:        "2026-08-12T19:00:00Z",
		FinishedAt:       "2026-08-12T19:00:01Z",
		Run:              ControlledRun{BenchmarkVersion: VersionV1},
	}
	directory := filepath.Join(t.TempDir(), "run-1")
	written, err := WriteControlledResultArtifacts(directory, document)
	if err != nil {
		t.Fatalf("WriteControlledResultArtifacts() error = %v", err)
	}
	if written.ResultJSON != filepath.Join(directory, ResultJSONFilename) || written.ReportMD != filepath.Join(directory, ReportMDFilename) {
		t.Fatalf("written = %#v", written)
	}
	for _, path := range []string{written.ResultJSON, written.ReportMD} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filepath.Base(path), info.Mode().Perm())
		}
	}
	if _, err := WriteControlledResultArtifacts(directory, document); err == nil {
		t.Fatal("second WriteControlledResultArtifacts() unexpectedly succeeded")
	}
}
