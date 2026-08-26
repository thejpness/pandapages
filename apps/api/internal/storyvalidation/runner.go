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
	editionJudgementSchemaNameV3 = "panda_pages_edition_semantic_judgement_v3"
	bundleJudgementSchemaNameV3  = "panda_pages_bundle_semantic_judgement_v3"
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
	if err := validateEditionValidationInput(input); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("validate edition semantic-validation input: %w", err)
	}

	editions := []storygeneration.GeneratedEditionArtifact{input.GeneratedEdition}
	index, err := BuildEvidenceIndex(
		input.CanonicalSource,
		input.AnalysisArtifact.Analysis,
		editions,
	)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build edition semantic evidence index: %w", err)
	}

	prompt, err := BuildEditionJudgementPromptV3(EditionJudgementPromptInput{
		Title:            input.Title,
		Author:           input.Author,
		CanonicalSource:  input.CanonicalSource,
		AnalysisArtifact: input.AnalysisArtifact,
		GeneratedEdition: input.GeneratedEdition,
	}, index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build edition semantic-judgement prompt: %w", err)
	}
	schema, err := EditionJudgementJSONSchema(index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build edition semantic-judgement schema: %w", err)
	}

	result, err := runner.gateway.Create(ctx, storygeneration.ResponsesCall{
		Operation:       validationResponsesOperation(input.GeneratedEdition.EditionKey),
		Model:           runner.model,
		ReasoningEffort: runner.reasoningEffort,
		MaxOutputTokens: runner.maxOutputTokens,
		Prompt:          prompt,
		StructuredOutput: &storygeneration.StructuredOutput{
			Name:   editionJudgementSchemaNameV3,
			Schema: schema,
		},
	})
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("run edition semantic judgement: %w", err)
	}

	judgement, err := DecodeSemanticJudgementJSON([]byte(result.OutputText))
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("decode edition semantic judgement: %w", err)
	}
	if judgement.AssessmentScope != adaptationcontract.AssessmentScopeEdition {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic judge returned scope %q", judgement.AssessmentScope)
	}
	if judgement.EditionKey == nil || *judgement.EditionKey != input.GeneratedEdition.EditionKey {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic judge returned the wrong edition target")
	}

	assessment, err := ResolveSemanticJudgement(judgement, index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("resolve edition semantic judgement: %w", err)
	}
	if err := validateAssessmentEvidence(
		assessment,
		input.CanonicalSource,
		input.AnalysisArtifact.Analysis,
		editions,
	); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("edition semantic assessment evidence is invalid: %w", err)
	}

	artifact, err := buildAssessmentArtifact(
		prompt.Version,
		runner.model,
		runner.reasoningEffort,
		result,
		input.AnalysisArtifact,
		editions,
		assessment,
	)
	if err != nil {
		return AssessmentArtifact{}, err
	}
	return artifact, nil
}

func (runner *Runner) ValidateBundle(ctx context.Context, input BundleValidationPromptInput) (AssessmentArtifact, error) {
	if err := validateBundleValidationInput(input); err != nil {
		return AssessmentArtifact{}, fmt.Errorf("validate bundle semantic-validation input: %w", err)
	}

	index, err := BuildEvidenceIndex(
		input.CanonicalSource,
		input.AnalysisArtifact.Analysis,
		input.GeneratedEditions,
	)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build bundle semantic evidence index: %w", err)
	}

	prompt, err := BuildBundleJudgementPromptV3(BundleJudgementPromptInput{
		Title:             input.Title,
		Author:            input.Author,
		CanonicalSource:   input.CanonicalSource,
		AnalysisArtifact:  input.AnalysisArtifact,
		GeneratedEditions: input.GeneratedEditions,
	}, index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build bundle semantic-judgement prompt: %w", err)
	}
	schema, err := BundleJudgementJSONSchema(index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("build bundle semantic-judgement schema: %w", err)
	}

	result, err := runner.gateway.Create(ctx, storygeneration.ResponsesCall{
		Operation:       storygeneration.ResponsesOperationValidateBundle,
		Model:           runner.model,
		ReasoningEffort: runner.reasoningEffort,
		MaxOutputTokens: runner.maxOutputTokens,
		Prompt:          prompt,
		StructuredOutput: &storygeneration.StructuredOutput{
			Name:   bundleJudgementSchemaNameV3,
			Schema: schema,
		},
	})
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("run bundle semantic judgement: %w", err)
	}

	judgement, err := DecodeSemanticJudgementJSON([]byte(result.OutputText))
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("decode bundle semantic judgement: %w", err)
	}
	if judgement.AssessmentScope != adaptationcontract.AssessmentScopeBundle {
		return AssessmentArtifact{}, fmt.Errorf("bundle semantic judge returned scope %q", judgement.AssessmentScope)
	}

	expectedKeys := make([]model.AdminStoryEditionKey, 0, len(input.GeneratedEditions))
	for _, edition := range input.GeneratedEditions {
		expectedKeys = append(expectedKeys, edition.EditionKey)
	}
	if !sameEditionKeys(judgement.EditionKeys, expectedKeys) {
		return AssessmentArtifact{}, fmt.Errorf("bundle semantic judge returned the wrong edition targets")
	}

	assessment, err := ResolveSemanticJudgement(judgement, index)
	if err != nil {
		return AssessmentArtifact{}, fmt.Errorf("resolve bundle semantic judgement: %w", err)
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

func validationResponsesOperation(key model.AdminStoryEditionKey) storygeneration.ResponsesOperation {
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return storygeneration.ResponsesOperationValidateConfidentReaders
	case model.AdminStoryEditionGrowingReaders:
		return storygeneration.ResponsesOperationValidateGrowingReaders
	case model.AdminStoryEditionStoryExplorers:
		return storygeneration.ResponsesOperationValidateStoryExplorers
	case model.AdminStoryEditionLittleListeners:
		return storygeneration.ResponsesOperationValidateLittleListeners
	default:
		return ""
	}
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
		ValidationVersion:    assessment.ValidationVersion,
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
	var expectedAssessmentVersion ValidationVersion
	switch artifact.ValidationVersion {
	case ValidationV2:
		expectedAssessmentVersion = ValidationV2
	case ValidationV3:
		expectedAssessmentVersion = ValidationV3
	default:
		return fmt.Errorf(
			"assessment artifact validation version must equal %q or %q",
			ValidationV2,
			ValidationV3,
		)
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
	if artifact.Assessment.ValidationVersion != expectedAssessmentVersion {
		return fmt.Errorf(
			"assessment artifact assessment validation version must equal artifact validation version %q",
			expectedAssessmentVersion,
		)
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

	expectedPromptVersion, err := assessmentArtifactPromptVersion(
		artifact.ValidationVersion,
		artifact.AssessmentScope,
	)
	if err != nil {
		return err
	}

	switch artifact.AssessmentScope {
	case adaptationcontract.AssessmentScopeEdition:
		if artifact.PromptVersion != expectedPromptVersion {
			return fmt.Errorf("edition assessment artifact prompt version must equal %q", expectedPromptVersion)
		}
		if len(artifact.EditionBindings) != 1 {
			return fmt.Errorf("edition assessment artifact requires exactly one edition binding")
		}
		if artifact.EditionKey == nil || artifact.EditionBindings[0].EditionKey != *artifact.EditionKey {
			return fmt.Errorf("edition assessment artifact binding does not match edition target")
		}
	case adaptationcontract.AssessmentScopeBundle:
		if artifact.PromptVersion != expectedPromptVersion {
			return fmt.Errorf("bundle assessment artifact prompt version must equal %q", expectedPromptVersion)
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

func validateEditionValidationInput(input EditionValidationPromptInput) error {
	if err := validateCommonValidationInput(
		input.Title,
		input.Author,
		input.CanonicalSource,
		input.AnalysisArtifact,
	); err != nil {
		return err
	}
	return validateGeneratedEditionForSemanticAssessment(
		input.GeneratedEdition,
		input.AnalysisArtifact,
	)
}

func validateBundleValidationInput(input BundleValidationPromptInput) error {
	if err := validateCommonValidationInput(
		input.Title,
		input.Author,
		input.CanonicalSource,
		input.AnalysisArtifact,
	); err != nil {
		return err
	}
	if len(input.GeneratedEditions) < 2 {
		return fmt.Errorf("bundle semantic validation requires at least two generated editions")
	}

	rank := make(map[model.AdminStoryEditionKey]int)
	for position, key := range storygeneration.DerivedEditionKeysV2() {
		rank[key] = position
	}

	lastRank := -1
	for position, edition := range input.GeneratedEditions {
		if err := validateGeneratedEditionForSemanticAssessment(
			edition,
			input.AnalysisArtifact,
		); err != nil {
			return fmt.Errorf("generated edition %d: %w", position+1, err)
		}
		currentRank, ok := rank[edition.EditionKey]
		if !ok {
			return fmt.Errorf(
				"generated edition %d has invalid edition key %q",
				position+1,
				edition.EditionKey,
			)
		}
		if currentRank <= lastRank {
			return fmt.Errorf("bundle generated editions must follow canonical modern edition order without duplicates")
		}
		lastRank = currentRank
	}

	return nil
}

func assessmentArtifactPromptVersion(
	validationVersion ValidationVersion,
	scope adaptationcontract.AssessmentScope,
) (storygeneration.PromptVersion, error) {
	switch validationVersion {
	case ValidationV2:
		switch scope {
		case adaptationcontract.AssessmentScopeEdition:
			return EditionValidationPromptVersionV2, nil
		case adaptationcontract.AssessmentScopeBundle:
			return BundleValidationPromptVersionV2, nil
		}
	case ValidationV3:
		switch scope {
		case adaptationcontract.AssessmentScopeEdition:
			return EditionJudgementPromptVersionV3, nil
		case adaptationcontract.AssessmentScopeBundle:
			return BundleJudgementPromptVersionV3, nil
		}
	}
	return "", fmt.Errorf(
		"assessment artifact has unsupported validation version %q or scope %q",
		validationVersion,
		scope,
	)
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
