package storygeneration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const storyAnalysisSchemaNameV2 = "panda_pages_story_analysis_v2"

type ResponsesGateway interface {
	Create(context.Context, ResponsesCall) (ResponsesResult, error)
}

type V2RunnerConfig struct {
	Gateway                 ResponsesGateway
	AnalysisReasoningEffort ReasoningEffort
	AnalysisMaxOutputTokens int
	EditionReasoningEffort  ReasoningEffort
	EditionMaxOutputTokens  int
}

type V2Runner struct {
	gateway                 ResponsesGateway
	analysisReasoningEffort ReasoningEffort
	analysisMaxOutputTokens int
	editionReasoningEffort  ReasoningEffort
	editionMaxOutputTokens  int
}

type StoryAnalysisArtifact struct {
	SpecificationVersion SpecificationVersion
	PromptVersion        PromptVersion
	RequestedModel       string
	ReturnedModel        string
	ReasoningEffort      ReasoningEffort
	SourceSHA256         string
	AnalysisSHA256       string
	Analysis             StoryAnalysis
	ResponseID           string
	Usage                ResponsesUsage
}

func NewV2Runner(cfg V2RunnerConfig) (*V2Runner, error) {
	if cfg.Gateway == nil {
		return nil, fmt.Errorf("Responses gateway is required")
	}
	if !validReasoningEffort(cfg.AnalysisReasoningEffort) {
		return nil, fmt.Errorf("unsupported analysis reasoning effort %q", cfg.AnalysisReasoningEffort)
	}
	if cfg.AnalysisMaxOutputTokens < 1 || cfg.AnalysisMaxOutputTokens > maxOutputTokensV2 {
		return nil, fmt.Errorf("analysis max output tokens must be between 1 and %d", maxOutputTokensV2)
	}
	if !validReasoningEffort(cfg.EditionReasoningEffort) {
		return nil, fmt.Errorf("unsupported edition reasoning effort %q", cfg.EditionReasoningEffort)
	}
	if cfg.EditionMaxOutputTokens < 1 || cfg.EditionMaxOutputTokens > maxOutputTokensV2 {
		return nil, fmt.Errorf("edition max output tokens must be between 1 and %d", maxOutputTokensV2)
	}

	return &V2Runner{
		gateway:                 cfg.Gateway,
		analysisReasoningEffort: cfg.AnalysisReasoningEffort,
		analysisMaxOutputTokens: cfg.AnalysisMaxOutputTokens,
		editionReasoningEffort:  cfg.EditionReasoningEffort,
		editionMaxOutputTokens:  cfg.EditionMaxOutputTokens,
	}, nil
}

func (runner *V2Runner) AnalyseSource(ctx context.Context, input SourceAnalysisPromptInput) (StoryAnalysisArtifact, error) {
	prompt, err := BuildSourceAnalysisPromptV2(input)
	if err != nil {
		return StoryAnalysisArtifact{}, fmt.Errorf("build v2 source-analysis prompt: %w", err)
	}

	result, err := runner.gateway.Create(ctx, ResponsesCall{
		Model:           GenerationModelV2,
		ReasoningEffort: runner.analysisReasoningEffort,
		MaxOutputTokens: runner.analysisMaxOutputTokens,
		Prompt:          prompt,
		StructuredOutput: &StructuredOutput{
			Name:   storyAnalysisSchemaNameV2,
			Schema: StoryAnalysisJSONSchema(),
		},
	})
	if err != nil {
		return StoryAnalysisArtifact{}, fmt.Errorf("run v2 source analysis: %w", err)
	}

	analysis, err := DecodeStoryAnalysisJSON([]byte(result.OutputText))
	if err != nil {
		return StoryAnalysisArtifact{}, fmt.Errorf("decode v2 source analysis: %w", err)
	}

	analysisDigest, err := storyAnalysisSHA256(analysis)
	if err != nil {
		return StoryAnalysisArtifact{}, fmt.Errorf("digest v2 source analysis: %w", err)
	}

	return StoryAnalysisArtifact{
		SpecificationVersion: SpecificationV2,
		PromptVersion:        SourceAnalysisPromptVersionV2,
		RequestedModel:       GenerationModelV2,
		ReturnedModel:        result.Model,
		ReasoningEffort:      runner.analysisReasoningEffort,
		SourceSHA256:         exactStringSHA256(input.CanonicalSource),
		AnalysisSHA256:       analysisDigest,
		Analysis:             analysis,
		ResponseID:           result.ResponseID,
		Usage:                result.Usage,
	}, nil
}

func (artifact StoryAnalysisArtifact) Validate() error {
	if artifact.SpecificationVersion != SpecificationV2 {
		return fmt.Errorf("StoryAnalysis artifact specification must equal %q", SpecificationV2)
	}
	if artifact.PromptVersion != SourceAnalysisPromptVersionV2 {
		return fmt.Errorf("StoryAnalysis artifact prompt version must equal %q", SourceAnalysisPromptVersionV2)
	}
	if artifact.RequestedModel != GenerationModelV2 {
		return fmt.Errorf("StoryAnalysis artifact requested model must equal %q", GenerationModelV2)
	}
	if artifact.ReturnedModel == "" {
		return fmt.Errorf("StoryAnalysis artifact returned model is required")
	}
	if !validReasoningEffort(artifact.ReasoningEffort) {
		return fmt.Errorf("StoryAnalysis artifact reasoning effort is invalid")
	}
	if !validSHA256Hex(artifact.SourceSHA256) {
		return fmt.Errorf("StoryAnalysis artifact source SHA-256 is invalid")
	}
	if !validSHA256Hex(artifact.AnalysisSHA256) {
		return fmt.Errorf("StoryAnalysis artifact analysis SHA-256 is invalid")
	}
	if artifact.ResponseID == "" {
		return fmt.Errorf("StoryAnalysis artifact response ID is required")
	}
	if err := artifact.Analysis.Validate(); err != nil {
		return fmt.Errorf("StoryAnalysis artifact analysis is invalid: %w", err)
	}

	expectedAnalysisDigest, err := storyAnalysisSHA256(artifact.Analysis)
	if err != nil {
		return fmt.Errorf("digest StoryAnalysis artifact: %w", err)
	}
	if artifact.AnalysisSHA256 != expectedAnalysisDigest {
		return fmt.Errorf("StoryAnalysis artifact analysis digest does not match analysis")
	}

	return nil
}

func (artifact StoryAnalysisArtifact) MatchesCanonicalSource(canonicalSource string) bool {
	return artifact.SourceSHA256 == exactStringSHA256(canonicalSource)
}

func storyAnalysisSHA256(analysis StoryAnalysis) (string, error) {
	encoded, err := json.Marshal(analysis)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func exactStringSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
