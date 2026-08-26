// Package storyorchestration composes source analysis, edition generation, and
// semantic validation for an already-authorized immutable canonical source.
// It is deliberately in-memory: persistence, authorization, source
// acquisition, and publication are outside this boundary.
package storyorchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

// GenerationRunner is the generation boundary used by Orchestrator. It keeps
// orchestration independent from Responses transport and model configuration.
type GenerationRunner interface {
	AnalyseSource(
		context.Context,
		storygeneration.SourceAnalysisPromptInput,
	) (storygeneration.StoryAnalysisArtifact, error)

	GenerateEdition(
		context.Context,
		storygeneration.GenerateEditionInput,
	) (storygeneration.GeneratedEditionArtifact, error)
}

// SemanticValidator is the semantic-validation boundary used by Orchestrator.
// It keeps orchestration independent from Responses transport and model
// configuration.
type SemanticValidator interface {
	ValidateEdition(
		context.Context,
		storyvalidation.EditionValidationPromptInput,
	) (storyvalidation.AssessmentArtifact, error)

	ValidateBundle(
		context.Context,
		storyvalidation.BundleValidationPromptInput,
	) (storyvalidation.AssessmentArtifact, error)
}

// Stage is one durable operation boundary in the fixed orchestration flow.
// It is intentionally not a percentage and reveals no prompt or content.
type Stage string

const (
	StageAnalysingSource            Stage = "analysing_source"
	StageGeneratingConfidentReaders Stage = "generating_confident_readers"
	StageGeneratingGrowingReaders   Stage = "generating_growing_readers"
	StageGeneratingStoryExplorers   Stage = "generating_story_explorers"
	StageGeneratingLittleListeners  Stage = "generating_little_listeners"
	StageValidatingConfidentReaders Stage = "validating_confident_readers"
	StageValidatingGrowingReaders   Stage = "validating_growing_readers"
	StageValidatingStoryExplorers   Stage = "validating_story_explorers"
	StageValidatingLittleListeners  Stage = "validating_little_listeners"
	StageValidatingBundle           Stage = "validating_bundle"
)

// StageReporter observes a stage immediately before its corresponding model
// operation begins. Returning an error stops the run before more provider work
// is incurred.
type StageReporter func(Stage) error

// Config supplies the already-configured generation and semantic-validation
// services. Orchestrator deliberately owns no model, token, reasoning, retry,
// or transport configuration.
type Config struct {
	Generator GenerationRunner
	Validator SemanticValidator
}

// Input is an already-authorized immutable canonical-source snapshot.
// SourceIdentity is an opaque immutable identity, such as a canonical source
// version ID. The existing StoryAnalysis artifact supplies the exact
// canonical-source SHA-256 binding after it has been verified against
// CanonicalSource.
type Input struct {
	SourceIdentity  string
	Title           string
	Author          string
	Slug            string
	Language        string
	SourceURL       string
	Rights          map[string]any
	CanonicalSource string
}

// Result is a fully completed in-memory orchestration run. A non-pass
// SemanticResult is an editorial state, not an orchestration error or a
// publication decision.
type Result struct {
	SourceIdentity string
	SourceSHA256   string

	AnalysisArtifact   storygeneration.StoryAnalysisArtifact
	Editions           []storygeneration.GeneratedEditionArtifact
	EditionAssessments []storyvalidation.AssessmentArtifact
	BundleAssessment   storyvalidation.AssessmentArtifact
	SemanticResult     adaptationcontract.Result
}

// PersistedRun is immutable completed orchestration evidence retained against
// one authoritative canonical source version.
type PersistedRun struct {
	ID              string
	SourceVersionID string
	Result          Result
	CreatedAt       time.Time
}

// Orchestrator composes one source analysis, four independent edition
// generations, four edition validations, and one bundle validation.
type Orchestrator struct {
	generator GenerationRunner
	validator SemanticValidator
}

// New constructs an in-memory orchestration service.
func New(cfg Config) (*Orchestrator, error) {
	if cfg.Generator == nil {
		return nil, fmt.Errorf("generation runner is required")
	}
	if cfg.Validator == nil {
		return nil, fmt.Errorf("semantic validator is required")
	}
	return &Orchestrator{generator: cfg.Generator, validator: cfg.Validator}, nil
}

// Run executes the canonical four-edition orchestration flow. Operational or
// artifact failures stop the flow and return an error. Valid semantic pass,
// needs_review, and fail results complete the flow and are returned together.
func (orchestrator *Orchestrator) Run(ctx context.Context, input Input) (Result, error) {
	return orchestrator.RunWithStageReporter(ctx, input, nil)
}

// RunWithStageReporter executes the fixed flow while exposing only its real
// operation boundaries to a durable job runner.
func (orchestrator *Orchestrator) RunWithStageReporter(ctx context.Context, input Input, report StageReporter) (Result, error) {
	if err := validateInput(input); err != nil {
		return Result{}, err
	}

	if err := reportStage(report, StageAnalysingSource); err != nil {
		return Result{}, err
	}
	analysis, err := orchestrator.generator.AnalyseSource(ctx, storygeneration.SourceAnalysisPromptInput{
		Title:           input.Title,
		Author:          input.Author,
		CanonicalSource: input.CanonicalSource,
	})
	if err != nil {
		return Result{}, fmt.Errorf("analyse canonical source: %w", err)
	}
	if err := validateAnalysisArtifact(analysis, input.CanonicalSource); err != nil {
		return Result{}, fmt.Errorf("analyse canonical source returned invalid artifact: %w", err)
	}

	editionKeys := storygeneration.DerivedEditionKeysV2()
	editions := make([]storygeneration.GeneratedEditionArtifact, 0, len(editionKeys))
	for _, editionKey := range editionKeys {
		if err := reportStage(report, generationStageForEdition(editionKey)); err != nil {
			return Result{}, err
		}
		edition, err := orchestrator.generator.GenerateEdition(ctx, storygeneration.GenerateEditionInput{
			EditionKey:       editionKey,
			Title:            input.Title,
			Author:           input.Author,
			Slug:             input.Slug,
			Language:         input.Language,
			SourceURL:        input.SourceURL,
			Rights:           cloneStringAnyMap(input.Rights),
			CanonicalSource:  input.CanonicalSource,
			AnalysisArtifact: cloneStoryAnalysisArtifact(analysis),
		})
		if retainedErr := validateAnalysisArtifact(analysis, input.CanonicalSource); retainedErr != nil {
			return Result{}, fmt.Errorf("generation runner mutated retained StoryAnalysis artifact: %w", retainedErr)
		}
		if err != nil {
			return Result{}, fmt.Errorf("generate edition %q: %w", editionKey, err)
		}
		if err := validateGeneratedEditionArtifact(edition, editionKey, analysis); err != nil {
			return Result{}, fmt.Errorf("generate edition %q returned invalid artifact: %w", editionKey, err)
		}
		editions = append(editions, edition)
	}

	editionAssessments := make([]storyvalidation.AssessmentArtifact, 0, len(editions))
	for _, edition := range editions {
		if err := reportStage(report, validationStageForEdition(edition.EditionKey)); err != nil {
			return Result{}, err
		}
		assessment, err := orchestrator.validator.ValidateEdition(ctx, storyvalidation.EditionValidationPromptInput{
			Title:            input.Title,
			Author:           input.Author,
			CanonicalSource:  input.CanonicalSource,
			AnalysisArtifact: cloneStoryAnalysisArtifact(analysis),
			GeneratedEdition: cloneGeneratedEditionArtifact(edition),
		})
		if retainedErr := validateAnalysisArtifact(analysis, input.CanonicalSource); retainedErr != nil {
			return Result{}, fmt.Errorf("edition validator mutated retained StoryAnalysis artifact: %w", retainedErr)
		}
		if retainedErr := validateGeneratedEditionArtifact(edition, edition.EditionKey, analysis); retainedErr != nil {
			return Result{}, fmt.Errorf("edition validator mutated retained generated edition %q: %w", edition.EditionKey, retainedErr)
		}
		if err != nil {
			return Result{}, fmt.Errorf("validate edition %q: %w", edition.EditionKey, err)
		}
		if err := validateEditionAssessmentArtifact(assessment, analysis, edition); err != nil {
			return Result{}, fmt.Errorf("validate edition %q returned invalid artifact: %w", edition.EditionKey, err)
		}
		editionAssessments = append(editionAssessments, assessment)
	}

	if err := reportStage(report, StageValidatingBundle); err != nil {
		return Result{}, err
	}
	bundleAssessment, err := orchestrator.validator.ValidateBundle(ctx, storyvalidation.BundleValidationPromptInput{
		Title:             input.Title,
		Author:            input.Author,
		CanonicalSource:   input.CanonicalSource,
		AnalysisArtifact:  cloneStoryAnalysisArtifact(analysis),
		GeneratedEditions: cloneGeneratedEditionArtifacts(editions),
	})
	if retainedErr := validateAnalysisArtifact(analysis, input.CanonicalSource); retainedErr != nil {
		return Result{}, fmt.Errorf("bundle validator mutated retained StoryAnalysis artifact: %w", retainedErr)
	}
	for _, edition := range editions {
		if retainedErr := validateGeneratedEditionArtifact(edition, edition.EditionKey, analysis); retainedErr != nil {
			return Result{}, fmt.Errorf("bundle validator mutated retained generated edition %q: %w", edition.EditionKey, retainedErr)
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("validate edition bundle: %w", err)
	}
	if err := validateBundleAssessmentArtifact(bundleAssessment, analysis, editions); err != nil {
		return Result{}, fmt.Errorf("validate edition bundle returned invalid artifact: %w", err)
	}

	return Result{
		SourceIdentity:     input.SourceIdentity,
		SourceSHA256:       analysis.SourceSHA256,
		AnalysisArtifact:   analysis,
		Editions:           editions,
		EditionAssessments: editionAssessments,
		BundleAssessment:   bundleAssessment,
		SemanticResult:     worstSemanticResult(editionAssessments, bundleAssessment),
	}, nil
}

func reportStage(report StageReporter, stage Stage) error {
	if report == nil {
		return nil
	}
	if err := report(stage); err != nil {
		return fmt.Errorf("report orchestration stage %q: %w", stage, err)
	}
	return nil
}

func generationStageForEdition(key model.AdminStoryEditionKey) Stage {
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return StageGeneratingConfidentReaders
	case model.AdminStoryEditionGrowingReaders:
		return StageGeneratingGrowingReaders
	case model.AdminStoryEditionStoryExplorers:
		return StageGeneratingStoryExplorers
	case model.AdminStoryEditionLittleListeners:
		return StageGeneratingLittleListeners
	default:
		return ""
	}
}

func validationStageForEdition(key model.AdminStoryEditionKey) Stage {
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return StageValidatingConfidentReaders
	case model.AdminStoryEditionGrowingReaders:
		return StageValidatingGrowingReaders
	case model.AdminStoryEditionStoryExplorers:
		return StageValidatingStoryExplorers
	case model.AdminStoryEditionLittleListeners:
		return StageValidatingLittleListeners
	default:
		return ""
	}
}

func validateInput(input Input) error {
	if strings.TrimSpace(input.SourceIdentity) == "" {
		return fmt.Errorf("canonical source identity is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return fmt.Errorf("canonical source title is required")
	}
	if strings.TrimSpace(input.CanonicalSource) == "" {
		return fmt.Errorf("canonical source text is required")
	}
	return nil
}

// ValidateCompletedResult verifies a fully completed orchestration result
// against the exact immutable canonical source and source-version identity.
// Valid semantic pass, needs_review, and fail outcomes are all complete
// editorial states; this does not make a publication decision.
func ValidateCompletedResult(result Result, sourceIdentity, canonicalSource string) error {
	if strings.TrimSpace(sourceIdentity) == "" {
		return fmt.Errorf("canonical source identity is required")
	}
	if result.SourceIdentity != sourceIdentity {
		return fmt.Errorf("orchestration result source identity does not match canonical source version")
	}
	if err := validateAnalysisArtifact(result.AnalysisArtifact, canonicalSource); err != nil {
		return fmt.Errorf("orchestration result StoryAnalysis artifact is invalid: %w", err)
	}
	if result.SourceSHA256 != result.AnalysisArtifact.SourceSHA256 {
		return fmt.Errorf("orchestration result source SHA-256 does not match StoryAnalysis")
	}

	editionKeys := storygeneration.DerivedEditionKeysV2()
	if len(result.Editions) != len(editionKeys) {
		return fmt.Errorf("orchestration result edition count is %d, want %d", len(result.Editions), len(editionKeys))
	}
	if len(result.EditionAssessments) != len(editionKeys) {
		return fmt.Errorf("orchestration result edition assessment count is %d, want %d", len(result.EditionAssessments), len(editionKeys))
	}
	for index, editionKey := range editionKeys {
		edition := result.Editions[index]
		if err := validateGeneratedEditionArtifact(edition, editionKey, result.AnalysisArtifact); err != nil {
			return fmt.Errorf("orchestration result edition %d is invalid: %w", index+1, err)
		}
		if err := validateEditionAssessmentArtifact(result.EditionAssessments[index], result.AnalysisArtifact, edition); err != nil {
			return fmt.Errorf("orchestration result edition assessment %d is invalid: %w", index+1, err)
		}
	}
	if err := validateBundleAssessmentArtifact(result.BundleAssessment, result.AnalysisArtifact, result.Editions); err != nil {
		return fmt.Errorf("orchestration result bundle assessment is invalid: %w", err)
	}
	if expected := worstSemanticResult(result.EditionAssessments, result.BundleAssessment); result.SemanticResult != expected {
		return fmt.Errorf("orchestration result semantic result %q does not match assessments %q", result.SemanticResult, expected)
	}
	return nil
}

func validateAnalysisArtifact(artifact storygeneration.StoryAnalysisArtifact, canonicalSource string) error {
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("StoryAnalysis artifact is invalid: %w", err)
	}
	if !artifact.MatchesCanonicalSource(canonicalSource) {
		return fmt.Errorf("StoryAnalysis artifact does not match canonical source")
	}
	return nil
}

func validateGeneratedEditionArtifact(
	artifact storygeneration.GeneratedEditionArtifact,
	expectedKey model.AdminStoryEditionKey,
	analysis storygeneration.StoryAnalysisArtifact,
) error {
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("generated-edition artifact is invalid: %w", err)
	}
	if artifact.EditionKey != expectedKey {
		return fmt.Errorf("generated-edition artifact returned edition key %q, want %q", artifact.EditionKey, expectedKey)
	}
	if !artifact.StructuralValidation.Passed() {
		return fmt.Errorf("generated-edition artifact did not pass deterministic validation")
	}
	if artifact.SourceSHA256 != analysis.SourceSHA256 {
		return fmt.Errorf("generated-edition artifact source binding does not match StoryAnalysis")
	}
	if artifact.AnalysisSHA256 != analysis.AnalysisSHA256 {
		return fmt.Errorf("generated-edition artifact analysis binding does not match StoryAnalysis")
	}
	return nil
}

func validateEditionAssessmentArtifact(
	artifact storyvalidation.AssessmentArtifact,
	analysis storygeneration.StoryAnalysisArtifact,
	edition storygeneration.GeneratedEditionArtifact,
) error {
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("edition assessment artifact is invalid: %w", err)
	}
	if artifact.AssessmentScope != adaptationcontract.AssessmentScopeEdition {
		return fmt.Errorf("edition assessment artifact scope %q is invalid", artifact.AssessmentScope)
	}
	if artifact.EditionKey == nil || *artifact.EditionKey != edition.EditionKey {
		return fmt.Errorf("edition assessment artifact target does not match generated edition")
	}
	if artifact.SourceSHA256 != analysis.SourceSHA256 {
		return fmt.Errorf("edition assessment artifact source binding does not match StoryAnalysis")
	}
	if artifact.AnalysisSHA256 != analysis.AnalysisSHA256 {
		return fmt.Errorf("edition assessment artifact analysis binding does not match StoryAnalysis")
	}
	if len(artifact.EditionBindings) != 1 {
		return fmt.Errorf("edition assessment artifact binding count does not match generated edition")
	}
	binding := artifact.EditionBindings[0]
	if binding.EditionKey != edition.EditionKey {
		return fmt.Errorf("edition assessment artifact binding targets %q, want %q", binding.EditionKey, edition.EditionKey)
	}
	if binding.ContentSHA256 != edition.ContentSHA256 {
		return fmt.Errorf("edition assessment artifact content binding does not match generated edition")
	}
	return nil
}

func validateBundleAssessmentArtifact(
	artifact storyvalidation.AssessmentArtifact,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
) error {
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("bundle assessment artifact is invalid: %w", err)
	}
	if artifact.AssessmentScope != adaptationcontract.AssessmentScopeBundle {
		return fmt.Errorf("bundle assessment artifact scope %q is invalid", artifact.AssessmentScope)
	}
	if artifact.EditionKey != nil {
		return fmt.Errorf("bundle assessment artifact must not target one edition")
	}
	if artifact.SourceSHA256 != analysis.SourceSHA256 {
		return fmt.Errorf("bundle assessment artifact source binding does not match StoryAnalysis")
	}
	if artifact.AnalysisSHA256 != analysis.AnalysisSHA256 {
		return fmt.Errorf("bundle assessment artifact analysis binding does not match StoryAnalysis")
	}
	if len(artifact.EditionKeys) != len(editions) || len(artifact.EditionBindings) != len(editions) {
		return fmt.Errorf("bundle assessment artifact edition bindings do not match generated editions")
	}
	for index, edition := range editions {
		if artifact.EditionKeys[index] != edition.EditionKey {
			return fmt.Errorf("bundle assessment artifact target %d is %q, want %q", index+1, artifact.EditionKeys[index], edition.EditionKey)
		}
		binding := artifact.EditionBindings[index]
		if binding.EditionKey != edition.EditionKey {
			return fmt.Errorf("bundle assessment artifact binding %d targets %q, want %q", index+1, binding.EditionKey, edition.EditionKey)
		}
		if binding.ContentSHA256 != edition.ContentSHA256 {
			return fmt.Errorf("bundle assessment artifact content binding %d does not match generated edition", index+1)
		}
	}
	return nil
}

func worstSemanticResult(
	editionAssessments []storyvalidation.AssessmentArtifact,
	bundleAssessment storyvalidation.AssessmentArtifact,
) adaptationcontract.Result {
	result := adaptationcontract.ResultPass
	for _, assessment := range editionAssessments {
		result = worseSemanticResult(result, assessment.Assessment.Result)
	}
	return worseSemanticResult(result, bundleAssessment.Assessment.Result)
}

func worseSemanticResult(current, candidate adaptationcontract.Result) adaptationcontract.Result {
	if current == adaptationcontract.ResultFail || candidate == adaptationcontract.ResultFail {
		return adaptationcontract.ResultFail
	}
	if current == adaptationcontract.ResultNeedsReview || candidate == adaptationcontract.ResultNeedsReview {
		return adaptationcontract.ResultNeedsReview
	}
	return adaptationcontract.ResultPass
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneStoryAnalysisArtifact(source storygeneration.StoryAnalysisArtifact) storygeneration.StoryAnalysisArtifact {
	clone := source
	clone.Analysis = cloneStoryAnalysis(source.Analysis)
	return clone
}

func cloneStoryAnalysis(source storygeneration.StoryAnalysis) storygeneration.StoryAnalysis {
	clone := source
	clone.Characters = cloneSlice(source.Characters)
	for index := range clone.Characters {
		clone.Characters[index].ExplicitMotivations = cloneSlice(source.Characters[index].ExplicitMotivations)
		clone.Characters[index].FlawsOrAmbiguities = cloneSlice(source.Characters[index].FlawsOrAmbiguities)
	}
	clone.Relationships = cloneSlice(source.Relationships)
	for index := range clone.Relationships {
		clone.Relationships[index].Parties = cloneSlice(source.Relationships[index].Parties)
	}
	clone.CoreStoryBeats = cloneSlice(source.CoreStoryBeats)
	clone.DevelopmentBeats = cloneSlice(source.DevelopmentBeats)
	clone.EnrichmentMaterial = cloneSlice(source.EnrichmentMaterial)
	clone.CausalDependencies = cloneSlice(source.CausalDependencies)
	clone.IconicMaterial = cloneSlice(source.IconicMaterial)
	clone.IntenseMaterial = cloneSlice(source.IntenseMaterial)
	clone.AdaptationRisks = cloneSlice(source.AdaptationRisks)
	return clone
}

func cloneGeneratedEditionArtifact(source storygeneration.GeneratedEditionArtifact) storygeneration.GeneratedEditionArtifact {
	clone := source
	clone.StructuralValidation.Findings = cloneSlice(source.StructuralValidation.Findings)
	return clone
}

func cloneGeneratedEditionArtifacts(source []storygeneration.GeneratedEditionArtifact) []storygeneration.GeneratedEditionArtifact {
	clone := cloneSlice(source)
	for index, artifact := range source {
		clone[index] = cloneGeneratedEditionArtifact(artifact)
	}
	return clone
}

func cloneSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	clone := make([]T, len(source))
	copy(clone, source)
	return clone
}
