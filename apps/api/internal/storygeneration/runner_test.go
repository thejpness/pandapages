package storygeneration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeResponsesGateway struct {
	calls  []ResponsesCall
	result ResponsesResult
	err    error
}

func (gateway *fakeResponsesGateway) Create(_ context.Context, call ResponsesCall) (ResponsesResult, error) {
	gateway.calls = append(gateway.calls, call)
	if gateway.err != nil {
		return ResponsesResult{}, gateway.err
	}
	return gateway.result, nil
}

func validV2RunnerConfig(gateway ResponsesGateway) V2RunnerConfig {
	return V2RunnerConfig{
		Gateway:                 gateway,
		AnalysisReasoningEffort: ReasoningEffortHigh,
		AnalysisMaxOutputTokens: 12000,
		EditionReasoningEffort:  ReasoningEffortHigh,
		EditionMaxOutputTokens:  32000,
	}
}

func validAnalysisOutputJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(validStoryAnalysis())
	if err != nil {
		t.Fatalf("json.Marshal(validStoryAnalysis()) error = %v", err)
	}
	return string(data)
}

func TestNewV2RunnerRequiresExplicitValidOperationBudgets(t *testing.T) {
	gateway := &fakeResponsesGateway{}

	tests := []struct {
		name   string
		mutate func(*V2RunnerConfig)
		want   string
	}{
		{
			name: "missing gateway",
			mutate: func(cfg *V2RunnerConfig) {
				cfg.Gateway = nil
			},
			want: "gateway is required",
		},
		{
			name: "invalid analysis reasoning",
			mutate: func(cfg *V2RunnerConfig) {
				cfg.AnalysisReasoningEffort = "extreme"
			},
			want: "analysis reasoning effort",
		},
		{
			name: "invalid analysis budget",
			mutate: func(cfg *V2RunnerConfig) {
				cfg.AnalysisMaxOutputTokens = 0
			},
			want: "analysis max output tokens",
		},
		{
			name: "invalid edition reasoning",
			mutate: func(cfg *V2RunnerConfig) {
				cfg.EditionReasoningEffort = "extreme"
			},
			want: "edition reasoning effort",
		},
		{
			name: "invalid edition budget",
			mutate: func(cfg *V2RunnerConfig) {
				cfg.EditionMaxOutputTokens = 0
			},
			want: "edition max output tokens",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validV2RunnerConfig(gateway)
			test.mutate(&cfg)
			_, err := NewV2Runner(cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewV2Runner() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestAnalyseSourceUsesLockedTerraStructuredOutputCall(t *testing.T) {
	source := "# Jack and the Beanstalk\n\nExact canonical source.\n"
	gateway := &fakeResponsesGateway{
		result: ResponsesResult{
			ResponseID: "resp_analysis",
			Model:      "gpt-5.6-terra",
			OutputText: validAnalysisOutputJSON(t),
			Usage: ResponsesUsage{
				InputTokens:     1000,
				CachedTokens:    100,
				OutputTokens:    700,
				ReasoningTokens: 300,
				TotalTokens:     1700,
			},
		},
	}
	runner, err := NewV2Runner(validV2RunnerConfig(gateway))
	if err != nil {
		t.Fatalf("NewV2Runner() error = %v", err)
	}

	artifact, err := runner.AnalyseSource(context.Background(), SourceAnalysisPromptInput{
		Title:           "Jack and the Beanstalk",
		Author:          "Traditional",
		CanonicalSource: source,
	})
	if err != nil {
		t.Fatalf("AnalyseSource() error = %v", err)
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
	if call.MaxOutputTokens != 12000 {
		t.Fatalf("max output tokens = %d", call.MaxOutputTokens)
	}
	if call.Prompt.Version != SourceAnalysisPromptVersionV3 {
		t.Fatalf("prompt version = %q", call.Prompt.Version)
	}
	if call.StructuredOutput == nil {
		t.Fatal("source analysis must use Structured Outputs")
	}
	if call.StructuredOutput.Name != storyAnalysisSchemaNameV2 {
		t.Fatalf("schema name = %q", call.StructuredOutput.Name)
	}
	if string(call.StructuredOutput.Schema) != string(StoryAnalysisJSONSchema()) {
		t.Fatal("source-analysis schema differs from canonical StoryAnalysis schema")
	}

	if artifact.SpecificationVersion != SpecificationV2 ||
		artifact.PromptVersion != SourceAnalysisPromptVersionV3 ||
		artifact.RequestedModel != GenerationModelV2 ||
		artifact.ReturnedModel != "gpt-5.6-terra" ||
		artifact.ResponseID != "resp_analysis" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if artifact.SourceSHA256 != exactStringSHA256(source) {
		t.Fatalf("source digest = %q", artifact.SourceSHA256)
	}
	expectedAnalysisDigest, err := storyAnalysisSHA256(validStoryAnalysis())
	if err != nil {
		t.Fatalf("storyAnalysisSHA256() error = %v", err)
	}
	if artifact.AnalysisSHA256 != expectedAnalysisDigest {
		t.Fatalf("analysis digest = %q, want %q", artifact.AnalysisSHA256, expectedAnalysisDigest)
	}
	if artifact.Usage.TotalTokens != 1700 {
		t.Fatalf("usage = %#v", artifact.Usage)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	if !artifact.MatchesCanonicalSource(source) {
		t.Fatal("artifact must match exact source used for analysis")
	}
	if artifact.MatchesCanonicalSource(strings.TrimSuffix(source, "\n")) {
		t.Fatal("source digest must bind exact canonical source bytes")
	}
}

func TestAnalyseSourceFailsClosed(t *testing.T) {
	t.Run("invalid source rejected before gateway", func(t *testing.T) {
		gateway := &fakeResponsesGateway{}
		runner, err := NewV2Runner(validV2RunnerConfig(gateway))
		if err != nil {
			t.Fatalf("NewV2Runner() error = %v", err)
		}

		_, err = runner.AnalyseSource(context.Background(), SourceAnalysisPromptInput{
			Title:           "Story",
			CanonicalSource: " ",
		})
		if err == nil || !strings.Contains(err.Error(), "canonical source is required") {
			t.Fatalf("AnalyseSource() error = %v", err)
		}
		if len(gateway.calls) != 0 {
			t.Fatalf("gateway calls = %d, want 0", len(gateway.calls))
		}
	})

	t.Run("gateway error preserved", func(t *testing.T) {
		sentinel := errors.New("provider failed")
		gateway := &fakeResponsesGateway{err: sentinel}
		runner, err := NewV2Runner(validV2RunnerConfig(gateway))
		if err != nil {
			t.Fatalf("NewV2Runner() error = %v", err)
		}

		_, err = runner.AnalyseSource(context.Background(), SourceAnalysisPromptInput{
			Title:           "Story",
			CanonicalSource: "# Story\n\nSource.",
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("AnalyseSource() error = %v, want wrapped sentinel", err)
		}
	})

	t.Run("malformed StoryAnalysis rejected", func(t *testing.T) {
		gateway := &fakeResponsesGateway{
			result: ResponsesResult{
				ResponseID: "resp_bad",
				Model:      GenerationModelV2,
				OutputText: `{"centralPlot":"not enough fields"}`,
			},
		}
		runner, err := NewV2Runner(validV2RunnerConfig(gateway))
		if err != nil {
			t.Fatalf("NewV2Runner() error = %v", err)
		}

		_, err = runner.AnalyseSource(context.Background(), SourceAnalysisPromptInput{
			Title:           "Story",
			CanonicalSource: "# Story\n\nSource.",
		})
		if err == nil || !strings.Contains(err.Error(), "decode v2 source analysis") {
			t.Fatalf("AnalyseSource() error = %v", err)
		}
	})

	t.Run("grouped relationship party rejected without retry", func(t *testing.T) {
		analysisJSON, err := json.Marshal(groupedRelationshipPartyStoryAnalysis())
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		gateway := &fakeResponsesGateway{
			result: ResponsesResult{
				ResponseID: "resp_grouped_party",
				Model:      GenerationModelV2,
				OutputText: string(analysisJSON),
			},
		}
		runner, err := NewV2Runner(validV2RunnerConfig(gateway))
		if err != nil {
			t.Fatalf("NewV2Runner() error = %v", err)
		}
		artifact, err := runner.AnalyseSource(context.Background(), SourceAnalysisPromptInput{
			Title:           "Benjamin Bunny",
			CanonicalSource: "# Benjamin Bunny\n\nCanonical source.",
		})
		if err == nil || !strings.Contains(err.Error(), `unknown character "Benjamin Bunny and Peter Rabbit"`) {
			t.Fatalf("AnalyseSource() error = %v, want grouped relationship party rejection", err)
		}
		if len(gateway.calls) != 1 {
			t.Fatalf("gateway calls = %d, want 1", len(gateway.calls))
		}
		if artifact.ResponseID != "" || artifact.Analysis.CentralPlot != "" {
			t.Fatalf("AnalyseSource() returned artifact = %#v, want zero artifact", artifact)
		}
	})
}

func TestStoryAnalysisArtifactValidateDetectsTampering(t *testing.T) {
	analysis := validStoryAnalysis()
	digest, err := storyAnalysisSHA256(analysis)
	if err != nil {
		t.Fatalf("storyAnalysisSHA256() error = %v", err)
	}
	artifact := StoryAnalysisArtifact{
		SpecificationVersion: SpecificationV2,
		PromptVersion:        SourceAnalysisPromptVersionV2,
		RequestedModel:       GenerationModelV2,
		ReturnedModel:        GenerationModelV2,
		ReasoningEffort:      ReasoningEffortHigh,
		SourceSHA256:         exactStringSHA256("# Story\n\nSource."),
		AnalysisSHA256:       digest,
		Analysis:             analysis,
		ResponseID:           "resp_analysis",
	}

	if err := artifact.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	artifact.Analysis.CentralPlot += " changed"
	err = artifact.Validate()
	if err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("Validate() error = %v, want digest mismatch", err)
	}
}

func TestStoryAnalysisArtifactValidateSupportsKnownPromptVersions(t *testing.T) {
	source := "# Story\n\nCanonical source."
	for _, version := range []PromptVersion{SourceAnalysisPromptVersionV2, SourceAnalysisPromptVersionV3} {
		t.Run(string(version), func(t *testing.T) {
			artifact := validStoryAnalysisArtifactForSource(t, source)
			artifact.PromptVersion = version
			if err := artifact.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}

	artifact := validStoryAnalysisArtifactForSource(t, source)
	artifact.PromptVersion = "panda-pages-source-analysis-prompt-unknown"
	err := artifact.Validate()
	if err == nil || !strings.Contains(err.Error(), "prompt version") {
		t.Fatalf("Validate() error = %v, want unknown prompt version rejection", err)
	}
}
