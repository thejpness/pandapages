package storyvalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

const (
	editionAssessmentSchemaNameV2 = "panda_pages_edition_semantic_assessment_v2"
	bundleAssessmentSchemaNameV2  = "panda_pages_bundle_semantic_assessment_v2"
)

type RunnerConfig struct {
	Gateway         storygeneration.ResponsesGateway
	Model           string
	ReasoningEffort storygeneration.ReasoningEffort
	MaxOutputTokens int
}

type Runner struct {
	gateway         storygeneration.ResponsesGateway
	model           string
	reasoningEffort storygeneration.ReasoningEffort
	maxOutputTokens int
}

type EditionBinding struct {
	EditionKey    model.AdminStoryEditionKey
	ContentSHA256 string
}

type AssessmentArtifact struct {
	ValidationVersion    ValidationVersion
	SpecificationVersion storygeneration.SpecificationVersion
	PromptVersion        storygeneration.PromptVersion
	AssessmentScope      adaptationcontract.AssessmentScope
	EditionKey           *model.AdminStoryEditionKey
	EditionKeys          []model.AdminStoryEditionKey
	RequestedModel       string
	ReturnedModel        string
	ReasoningEffort      storygeneration.ReasoningEffort
	SourceSHA256         string
	AnalysisSHA256       string
	EditionBindings      []EditionBinding
	AssessmentSHA256     string
	Assessment           Assessment
	ResponseID           string
	Usage                storygeneration.ResponsesUsage
}

func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Gateway == nil {
		return nil, fmt.Errorf("Responses gateway is required")
	}
	modelName := strings.TrimSpace(cfg.Model)
	if modelName == "" {
		return nil, fmt.Errorf("semantic validator model is required")
	}
	if !validValidationReasoningEffort(cfg.ReasoningEffort) {
		return nil, fmt.Errorf("unsupported semantic validator reasoning effort %q", cfg.ReasoningEffort)
	}
	if cfg.MaxOutputTokens < 1 {
		return nil, fmt.Errorf("semantic validator max output tokens must be positive")
	}

	return &Runner{
		gateway:         cfg.Gateway,
		model:           modelName,
		reasoningEffort: cfg.ReasoningEffort,
		maxOutputTokens: cfg.MaxOutputTokens,
	}, nil
}

func (runner *Runner) ValidateEdition(ctx context.Context, input EditionValidationPromptInput) (AssessmentArtifact, error) {
	prompt, err := BuildEditionValidationPromptV2(input)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build edition semantic-validation prompt: %w", err)
	}

	result, err := runner.gateway.Create(ctx, storygeneration.ResponsesCall{
		Model:           runner.model,
		ReasoningEffort: runner.reasoningEffort,
		MaxOutputTokens: runner.maxOutputTokens,
		Prompt:          prompt,
		StructuredOutput: &storygeneration.StructuredOutput{
			Name:   editionAssessmentSchemaNameV2,
			Schema: EditionAssessmentJSONSchema(),
		},
	})
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("run edition semantic validation: %w", err)
	}

	assessment, err := DecodeAssessmentJSON([]byte(result.OutputText))
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("decode edition semantic assessment: %w", err)
	}
	if assessment.AssessmentScope != adaptationcontract.AssessmentScopeEdition {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic assessor returned scope %q", assessment.AssessmentScope)
	}
	if assessment.EditionKey == nil || *assessment.EditionKey != input.GeneratedEdition.EditionKey {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic assessor returned the wrong edition target")
	}
	if err := validateAssessmentEvidence(
		assessment,
		input.CanonicalSource,
		input.AnalysisArtifact.Analysis,
		[]storygeneration.GeneratedEditionArtifact{input.GeneratedEdition},
	); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic assessment evidence is invalid: %w", err)
	}

	artifact, err := buildAssessmentArtifact(
		prompt.Version,
		runner.model,
		runner.reasoningEffort,
		result,
		input.AnalysisArtifact,
		[]storygeneration.GeneratedEditionArtifact{input.GeneratedEdition},
		assessment,
	)
	if err != nil {
		return AssessmentArtifact{}, err
	}
	return artifact, nil
}

func (runner *Runner) ValidateBundle(ctx context.Context, input BundleValidationPromptInput) (AssessmentArtifact, error) {
	prompt, err := BuildBundleValidationPromptV2(input)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build bundle semantic-validation prompt: %w", err)
	}

	result, err := runner.gateway.Create(ctx, storygeneration.ResponsesCall{
		Model:           runner.model,
		ReasoningEffort: runner.reasoningEffort,
		MaxOutputTokens: runner.maxOutputTokens,
		Prompt:          prompt,
		StructuredOutput: &storygeneration.StructuredOutput{
			Name:   bundleAssessmentSchemaNameV2,
			Schema: BundleAssessmentJSONSchema(),
		},
	})
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("run bundle semantic validation: %w", err)
	}

	assessment, err := DecodeAssessmentJSON([]byte(result.OutputText))
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("decode bundle semantic assessment: %w", err)
	}
	if assessment.AssessmentScope != adaptationcontract.AssessmentScopeBundle {
		return AssessmentArtifact{}, fmt.Errorf("bundle semantic assessor returned scope %q", assessment.AssessmentScope)
	}

	expectedKeys := make([]model.AdminStoryEditionKey, 0, len(input.GeneratedEditions))
	for _, edition := range input.GeneratedEditions {
		expectedKeys = append(expectedKeys, edition.EditionKey)
	}
	if !sameEditionKeys(assessment.EditionKeys, expectedKeys) {
		return AssessmentArtifact{}, fmt.Errorf("bundle semantic assessor returned the wrong edition targets")
	}
	if err := validateAssessmentEvidence(
		assessment,
		input.CanonicalSource,
		input.AnalysisArtifact.Analysis,
		input.GeneratedEditions,
	); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("bundle semantic assessment evidence is invalid: %w", err)
	}

	artifact, err := buildAssessmentArtifact(
		prompt.Version,
		runner.model,
		runner.reasoningEffort,
		result,
		input.AnalysisArtifact,
		input.GeneratedEditions,
		assessment,
	)
	if err != nil {
		return AssessmentArtifact{}, err
	}
	return artifact, nil
}

func buildAssessmentArtifact(
	promptVersion storygeneration.PromptVersion,
	requestedModel string,
	reasoningEffort storygeneration.ReasoningEffort,
	result storygeneration.ResponsesResult,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	assessment Assessment,
) (AssessmentArtifact, error) {
	digest, err := assessmentSHA256(assessment)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("digest semantic assessment: %w", err)
	}

	bindings := make([]EditionBinding, 0, len(editions))
	for _, edition := range editions {
		bindings = append(bindings, EditionBinding{
			EditionKey:    edition.EditionKey,
			ContentSHA256: edition.ContentSHA256,
		})
	}

	artifact := AssessmentArtifact{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        promptVersion,
		AssessmentScope:      assessment.AssessmentScope,
		EditionKey:           copyEditionKey(assessment.EditionKey),
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), assessment.EditionKeys...),
		RequestedModel:       requestedModel,
		ReturnedModel:        result.Model,
		ReasoningEffort:      reasoningEffort,
		SourceSHA256:         analysis.SourceSHA256,
		AnalysisSHA256:       analysis.AnalysisSHA256,
		EditionBindings:      bindings,
		AssessmentSHA256:     digest,
		Assessment:           assessment,
		ResponseID:           result.ResponseID,
		Usage:                result.Usage,
	}
	if err := artifact.Validate(); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("constructed semantic-assessment artifact is invalid: %w", err)
	}
	return artifact, nil
}

func (artifact AssessmentArtifact) Validate() error {
	if artifact.ValidationVersion != ValidationV2 {
		return fmt.Errorf("assessment artifact validation version must equal %q", ValidationV2)
	}
	if artifact.SpecificationVersion != storygeneration.SpecificationV2 {
		return fmt.Errorf("assessment artifact specification version must equal %q", storygeneration.SpecificationV2)
	}
	if strings.TrimSpace(artifact.RequestedModel) == "" {
		return fmt.Errorf("assessment artifact requested model is required")
	}
	if strings.TrimSpace(artifact.ReturnedModel) == "" {
		return fmt.Errorf("assessment artifact returned model is required")
	}
	if !validValidationReasoningEffort(artifact.ReasoningEffort) {
		return fmt.Errorf("assessment artifact reasoning effort is invalid")
	}
	if !validSHA256Hex(artifact.SourceSHA256) {
		return fmt.Errorf("assessment artifact source SHA-256 is invalid")
	}
	if !validSHA256Hex(artifact.AnalysisSHA256) {
		return fmt.Errorf("assessment artifact analysis SHA-256 is invalid")
	}
	if !validSHA256Hex(artifact.AssessmentSHA256) {
		return fmt.Errorf("assessment artifact assessment SHA-256 is invalid")
	}
	if strings.TrimSpace(artifact.ResponseID) == "" {
		return fmt.Errorf("assessment artifact response ID is required")
	}
	if err := artifact.Assessment.Validate(); err != nil {
		return fmt.Errorf("assessment artifact assessment is invalid: %w", err)
	}
	if artifact.AssessmentScope != artifact.Assessment.AssessmentScope {
		return fmt.Errorf("assessment artifact scope does not match assessment")
	}
	if !sameOptionalEditionKey(artifact.EditionKey, artifact.Assessment.EditionKey) {
		return fmt.Errorf("assessment artifact edition target does not match assessment")
	}
	if !sameEditionKeys(artifact.EditionKeys, artifact.Assessment.EditionKeys) {
		return fmt.Errorf("assessment artifact bundle targets do not match assessment")
	}

	switch artifact.AssessmentScope {
	case adaptationcontract.AssessmentScopeEdition:
		if artifact.PromptVersion != EditionValidationPromptVersionV2 {
			return fmt.Errorf("edition assessment artifact prompt version must equal %q", EditionValidationPromptVersionV2)
		}
		if len(artifact.EditionBindings) != 1 {
			return fmt.Errorf("edition assessment artifact requires exactly one edition binding")
		}
		if artifact.EditionKey == nil || artifact.EditionBindings[0].EditionKey != *artifact.EditionKey {
			return fmt.Errorf("edition assessment artifact binding does not match edition target")
		}
	case adaptationcontract.AssessmentScopeBundle:
		if artifact.PromptVersion != BundleValidationPromptVersionV2 {
			return fmt.Errorf("bundle assessment artifact prompt version must equal %q", BundleValidationPromptVersionV2)
		}
		if len(artifact.EditionBindings) != len(artifact.EditionKeys) {
			return fmt.Errorf("bundle assessment artifact bindings do not match target count")
		}
		for index, binding := range artifact.EditionBindings {
			if binding.EditionKey != artifact.EditionKeys[index] {
				return fmt.Errorf("bundle assessment artifact binding %d does not match target order", index+1)
			}
		}
	default:
		return fmt.Errorf("assessment artifact scope %q is unsupported", artifact.AssessmentScope)
	}

	for index, binding := range artifact.EditionBindings {
		if !validSHA256Hex(binding.ContentSHA256) {
			return fmt.Errorf("assessment artifact edition binding %d content SHA-256 is invalid", index+1)
		}
	}

	expectedDigest, err := assessmentSHA256(artifact.Assessment)
	if err != nil {
		return fmt.Errorf("digest assessment artifact: %w", err)
	}
	if artifact.AssessmentSHA256 != expectedDigest {
		return fmt.Errorf("assessment artifact assessment digest does not match assessment")
	}

	return nil
}

func validateAssessmentEvidence(
	assessment Assessment,
	canonicalSource string,
	analysis storygeneration.StoryAnalysis,
	editions []storygeneration.GeneratedEditionArtifact,
) error {
	for findingIndex, finding := range assessment.Findings {
		for evidenceIndex, evidence := range finding.Evidence {
			excerpt := strings.TrimSpace(evidence.Excerpt)
			switch evidence.Location {
			case EvidenceCanonicalSource:
				if !strings.Contains(canonicalSource, excerpt) {
					return fmt.Errorf(
						"finding %d evidence %d excerpt is not present in canonical source",
						findingIndex+1,
						evidenceIndex+1,
					)
				}
			case EvidenceStoryAnalysis:
				if !storyAnalysisContainsExcerpt(analysis, excerpt) {
					return fmt.Errorf(
						"finding %d evidence %d excerpt is not present in StoryAnalysis",
						findingIndex+1,
						evidenceIndex+1,
					)
				}
			case EvidenceGeneratedEdition:
				if evidence.EditionKey == nil {
					return fmt.Errorf("finding %d evidence %d generated edition key is missing", findingIndex+1, evidenceIndex+1)
				}
				edition, ok := findGeneratedEdition(editions, *evidence.EditionKey)
				if !ok {
					return fmt.Errorf("finding %d evidence %d generated edition is not supplied", findingIndex+1, evidenceIndex+1)
				}
				if !strings.Contains(edition.Markdown, excerpt) {
					return fmt.Errorf(
						"finding %d evidence %d excerpt is not present in generated edition %q",
						findingIndex+1,
						evidenceIndex+1,
						*evidence.EditionKey,
					)
				}
			default:
				return fmt.Errorf("finding %d evidence %d location %q is unsupported", findingIndex+1, evidenceIndex+1, evidence.Location)
			}
		}
	}
	return nil
}

func storyAnalysisContainsExcerpt(analysis storygeneration.StoryAnalysis, excerpt string) bool {
	if excerpt == "" {
		return false
	}
	if strings.Contains(analysis.CentralPlot, excerpt) {
		return true
	}
	for _, character := range analysis.Characters {
		if strings.Contains(character.Name, excerpt) ||
			strings.Contains(character.Role, excerpt) ||
			containsString(character.ExplicitMotivations, excerpt) ||
			containsString(character.FlawsOrAmbiguities, excerpt) {
			return true
		}
	}
	for _, relationship := range analysis.Relationships {
		if containsString(relationship.Parties, excerpt) ||
			strings.Contains(relationship.Nature, excerpt) ||
			strings.Contains(relationship.PowerDynamics, excerpt) {
			return true
		}
	}
	for _, beat := range analysis.CoreStoryBeats {
		if strings.Contains(beat.Summary, excerpt) {
			return true
		}
	}
	for _, beat := range analysis.DevelopmentBeats {
		if strings.Contains(beat.Summary, excerpt) {
			return true
		}
	}
	for _, beat := range analysis.EnrichmentMaterial {
		if strings.Contains(beat.Summary, excerpt) {
			return true
		}
	}
	for _, dependency := range analysis.CausalDependencies {
		if strings.Contains(dependency.Cause, excerpt) ||
			strings.Contains(dependency.Effect, excerpt) ||
			strings.Contains(dependency.WhyItMatters, excerpt) {
			return true
		}
	}
	for _, iconic := range analysis.IconicMaterial {
		if strings.Contains(iconic.Kind, excerpt) ||
			strings.Contains(iconic.TextOrDescription, excerpt) ||
			strings.Contains(iconic.Importance, excerpt) {
			return true
		}
	}
	for _, intense := range analysis.IntenseMaterial {
		if strings.Contains(string(intense.Kind), excerpt) ||
			strings.Contains(intense.Description, excerpt) ||
			strings.Contains(intense.NarrativeFunction, excerpt) {
			return true
		}
	}
	for _, risk := range analysis.AdaptationRisks {
		if strings.Contains(string(risk.Kind), excerpt) ||
			strings.Contains(risk.Description, excerpt) ||
			strings.Contains(risk.WhatMustBePreserved, excerpt) {
			return true
		}
	}
	return false
}

func containsString(values []string, excerpt string) bool {
	for _, value := range values {
		if strings.Contains(value, excerpt) {
			return true
		}
	}
	return false
}

func findGeneratedEdition(
	editions []storygeneration.GeneratedEditionArtifact,
	key model.AdminStoryEditionKey,
) (storygeneration.GeneratedEditionArtifact, bool) {
	for _, edition := range editions {
		if edition.EditionKey == key {
			return edition, true
		}
	}
	return storygeneration.GeneratedEditionArtifact{}, false
}

func assessmentSHA256(assessment Assessment) (string, error) {
	encoded, err := json.Marshal(assessment)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validValidationReasoningEffort(effort storygeneration.ReasoningEffort) bool {
	switch effort {
	case storygeneration.ReasoningEffortNone,
		storygeneration.ReasoningEffortLow,
		storygeneration.ReasoningEffortMedium,
		storygeneration.ReasoningEffortHigh,
		storygeneration.ReasoningEffortXHigh,
		storygeneration.ReasoningEffortMax:
		return true
	default:
		return false
	}
}

func sameEditionKeys(left, right []model.AdminStoryEditionKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameOptionalEditionKey(left, right *model.AdminStoryEditionKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func copyEditionKey(key *model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	if key == nil {
		return nil
	}
	value := *key
	return &value
}
