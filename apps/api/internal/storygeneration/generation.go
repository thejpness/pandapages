package storygeneration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

var ErrGeneratedEditionStructurallyInvalid = errors.New("generated edition failed deterministic validation")

type GenerateEditionInput struct {
	EditionKey       model.AdminStoryEditionKey
	Title            string
	Author           string
	Slug             string
	Language         string
	SourceURL        string
	Rights           map[string]any
	CanonicalSource  string
	AnalysisArtifact StoryAnalysisArtifact
}

type GeneratedEditionArtifact struct {
	SpecificationVersion SpecificationVersion
	PromptVersion        PromptVersion
	EditionKey           model.AdminStoryEditionKey
	RequestedModel       string
	ReturnedModel        string
	ReasoningEffort      ReasoningEffort
	SourceSHA256         string
	AnalysisSHA256       string
	ContentSHA256        string
	Markdown             string
	ResponseID           string
	Usage                ResponsesUsage
	StructuralValidation adaptationcontract.StructuralValidation
}

func (runner *V2Runner) GenerateEdition(ctx context.Context, input GenerateEditionInput) (GeneratedEditionArtifact, error) {
	if !ValidV2DerivedEditionKey(input.EditionKey) {
		return GeneratedEditionArtifact{}, fmt.Errorf("edition key must be a canonical v2 derived edition key")
	}
	if err := input.AnalysisArtifact.Validate(); err != nil {
		return GeneratedEditionArtifact{}, fmt.Errorf("StoryAnalysis artifact is invalid: %w", err)
	}
	if !input.AnalysisArtifact.MatchesCanonicalSource(input.CanonicalSource) {
		return GeneratedEditionArtifact{}, fmt.Errorf("StoryAnalysis artifact does not match canonical source")
	}

	prompt, err := BuildEditionPromptV4(EditionPromptInput{
		EditionKey:      input.EditionKey,
		Title:           input.Title,
		Author:          input.Author,
		CanonicalSource: input.CanonicalSource,
		StoryAnalysis:   input.AnalysisArtifact.Analysis,
	})
	if err != nil {
		return GeneratedEditionArtifact{}, fmt.Errorf("build v4 edition prompt: %w", err)
	}

	result, err := runner.gateway.Create(ctx, ResponsesCall{
		Operation:        generationResponsesOperation(input.EditionKey),
		Model:            GenerationModelV2,
		ReasoningEffort:  runner.editionReasoningEffort,
		MaxOutputTokens:  runner.editionMaxOutputTokens,
		Prompt:           prompt,
		StructuredOutput: nil,
	})
	if err != nil {
		return GeneratedEditionArtifact{}, fmt.Errorf("run v2 edition generation: %w", err)
	}

	structural := adaptationcontract.ValidateGeneratedEdition(adaptationcontract.GeneratedEditionInput{
		EditionKey: input.EditionKey,
		Slug:       input.Slug,
		Title:      input.Title,
		Author:     input.Author,
		Markdown:   result.OutputText,
		Language:   input.Language,
		SourceURL:  input.SourceURL,
		Rights:     input.Rights,
	})

	artifact := GeneratedEditionArtifact{
		SpecificationVersion: SpecificationV2,
		PromptVersion:        EditionPromptVersionV4,
		EditionKey:           input.EditionKey,
		RequestedModel:       GenerationModelV2,
		ReturnedModel:        result.Model,
		ReasoningEffort:      runner.editionReasoningEffort,
		SourceSHA256:         input.AnalysisArtifact.SourceSHA256,
		AnalysisSHA256:       input.AnalysisArtifact.AnalysisSHA256,
		ContentSHA256:        structural.ContentSHA256,
		Markdown:             result.OutputText,
		ResponseID:           result.ResponseID,
		Usage:                result.Usage,
		StructuralValidation: structural,
	}

	if err := artifact.Validate(); err != nil {
		return GeneratedEditionArtifact{}, fmt.Errorf("constructed generated-edition artifact is invalid: %w", err)
	}
	if !structural.Passed() {
		return artifact, fmt.Errorf("%w: %s", ErrGeneratedEditionStructurallyInvalid, summarizeStructuralFindings(structural.Findings))
	}

	return artifact, nil
}

func generationResponsesOperation(key model.AdminStoryEditionKey) ResponsesOperation {
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return ResponsesOperationGenerateConfidentReaders
	case model.AdminStoryEditionGrowingReaders:
		return ResponsesOperationGenerateGrowingReaders
	case model.AdminStoryEditionStoryExplorers:
		return ResponsesOperationGenerateStoryExplorers
	case model.AdminStoryEditionLittleListeners:
		return ResponsesOperationGenerateLittleListeners
	default:
		return ""
	}
}

func (artifact GeneratedEditionArtifact) Validate() error {
	if artifact.SpecificationVersion != SpecificationV2 {
		return fmt.Errorf("generated-edition artifact specification must equal %q", SpecificationV2)
	}
	if !validEditionPromptVersion(artifact.PromptVersion) {
		return fmt.Errorf("generated-edition artifact prompt version %q is unsupported", artifact.PromptVersion)
	}
	if !ValidV2DerivedEditionKey(artifact.EditionKey) {
		return fmt.Errorf("generated-edition artifact edition key is invalid")
	}
	if artifact.RequestedModel != GenerationModelV2 {
		return fmt.Errorf("generated-edition artifact requested model must equal %q", GenerationModelV2)
	}
	if strings.TrimSpace(artifact.ReturnedModel) == "" {
		return fmt.Errorf("generated-edition artifact returned model is required")
	}
	if !validReasoningEffort(artifact.ReasoningEffort) {
		return fmt.Errorf("generated-edition artifact reasoning effort is invalid")
	}
	if !validSHA256Hex(artifact.SourceSHA256) {
		return fmt.Errorf("generated-edition artifact source SHA-256 is invalid")
	}
	if !validSHA256Hex(artifact.AnalysisSHA256) {
		return fmt.Errorf("generated-edition artifact analysis SHA-256 is invalid")
	}
	if !validSHA256Hex(artifact.ContentSHA256) {
		return fmt.Errorf("generated-edition artifact content SHA-256 is invalid")
	}
	if artifact.ResponseID == "" {
		return fmt.Errorf("generated-edition artifact response ID is required")
	}

	structural := artifact.StructuralValidation
	if structural.ContractVersion != adaptationcontract.VersionV1 {
		return fmt.Errorf("generated-edition artifact structural contract must equal %q", adaptationcontract.VersionV1)
	}
	if structural.EditionKey != artifact.EditionKey {
		return fmt.Errorf("generated-edition artifact structural edition key does not match")
	}
	if structural.ContentSHA256 != artifact.ContentSHA256 {
		return fmt.Errorf("generated-edition artifact structural digest does not match content digest")
	}

	expectedContentDigest := exactStringSHA256(artifact.Markdown)
	if artifact.ContentSHA256 != expectedContentDigest {
		return fmt.Errorf("generated-edition artifact content digest does not match Markdown")
	}

	return nil
}

func validEditionPromptVersion(version PromptVersion) bool {
	switch version {
	case EditionPromptVersionV2, EditionPromptVersionV3, EditionPromptVersionV4:
		return true
	default:
		return false
	}
}

func summarizeStructuralFindings(findings []adaptationcontract.Finding) string {
	if len(findings) == 0 {
		return "unknown structural validation failure"
	}
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, string(finding.Code))
	}
	return strings.Join(codes, ", ")
}
