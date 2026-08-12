package storybenchmark

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

type TrialStatus string

const (
	TrialStatusComplete   TrialStatus = "complete"
	TrialStatusIncomplete TrialStatus = "incomplete"
)

type ValidatorConfig struct {
	ID              string                          `json:"id"`
	Model           string                          `json:"model"`
	ReasoningEffort storygeneration.ReasoningEffort `json:"reasoningEffort"`
	MaxOutputTokens int                             `json:"maxOutputTokens"`
}

type ControlledRunConfig struct {
	ValidationRepetitions int               `json:"validationRepetitions"`
	Validators            []ValidatorConfig `json:"validators"`
}

type EndToEndRunConfig struct {
	GenerationRepetitions int               `json:"generationRepetitions"`
	ValidationRepetitions int               `json:"validationRepetitions"`
	Validators            []ValidatorConfig `json:"validators"`
}

// EndToEndSource is benchmark execution input, not publication-eligibility evidence.
// Live callers must establish source eligibility before constructing this value;
// tests may use explicit synthetic fixtures.
type EndToEndSource struct {
	ID              string         `json:"id"`
	Slug            string         `json:"slug"`
	Title           string         `json:"title"`
	Author          string         `json:"author"`
	Language        string         `json:"language"`
	SourceURL       string         `json:"sourceUrl"`
	Rights          map[string]any `json:"rights"`
	CanonicalSource string         `json:"canonicalSource"`
}

type ValidationTrial struct {
	CaseID               string                              `json:"caseId"`
	GenerationRepetition int                                 `json:"generationRepetition,omitempty"`
	ValidationRepetition int                                 `json:"validationRepetition"`
	ValidatorConfigID    string                              `json:"validatorConfigId"`
	AssessmentScope      adaptationcontract.AssessmentScope  `json:"assessmentScope"`
	EditionKey           *model.AdminStoryEditionKey         `json:"editionKey,omitempty"`
	EditionKeys          []model.AdminStoryEditionKey        `json:"editionKeys,omitempty"`
	Status               TrialStatus                         `json:"status"`
	Error                string                              `json:"error,omitempty"`
	AssessmentArtifact   *storyvalidation.AssessmentArtifact `json:"assessmentArtifact,omitempty"`
	Score                *AssessmentScore                    `json:"score,omitempty"`
}

type ControlledRun struct {
	BenchmarkVersion      Version           `json:"benchmarkVersion"`
	Status                TrialStatus       `json:"status"`
	ValidationRepetitions int               `json:"validationRepetitions"`
	Validators            []ValidatorConfig `json:"validators"`
	AttemptedTrials       int               `json:"attemptedTrials"`
	CompletedTrials       int               `json:"completedTrials"`
	IncompleteTrials      int               `json:"incompleteTrials"`
	Trials                []ValidationTrial `json:"trials"`
	QualitySummary        Summary           `json:"qualitySummary"`
}

type GenerationRepetition struct {
	Repetition       int                                        `json:"repetition"`
	GenerationStatus TrialStatus                                `json:"generationStatus"`
	GenerationError  string                                     `json:"generationError,omitempty"`
	ValidationStatus TrialStatus                                `json:"validationStatus"`
	AnalysisArtifact *storygeneration.StoryAnalysisArtifact     `json:"analysisArtifact,omitempty"`
	Editions         []storygeneration.GeneratedEditionArtifact `json:"editions,omitempty"`
	ValidationTrials []ValidationTrial                          `json:"validationTrials,omitempty"`
}

type EndToEndRun struct {
	BenchmarkVersion      Version                `json:"benchmarkVersion"`
	Status                TrialStatus            `json:"status"`
	GenerationRepetitions int                    `json:"generationRepetitions"`
	ValidationRepetitions int                    `json:"validationRepetitions"`
	Validators            []ValidatorConfig      `json:"validators"`
	Generations           []GenerationRepetition `json:"generations"`
}

type GenerationRunner interface {
	AnalyseSource(context.Context, storygeneration.SourceAnalysisPromptInput) (storygeneration.StoryAnalysisArtifact, error)
	GenerateEdition(context.Context, storygeneration.GenerateEditionInput) (storygeneration.GeneratedEditionArtifact, error)
}

type SemanticValidator interface {
	ValidateEdition(context.Context, storyvalidation.EditionValidationPromptInput) (storyvalidation.AssessmentArtifact, error)
	ValidateBundle(context.Context, storyvalidation.BundleValidationPromptInput) (storyvalidation.AssessmentArtifact, error)
}

type ValidatorFactory func(ValidatorConfig) (SemanticValidator, error)

type RunnerConfig struct {
	Generator        GenerationRunner
	ValidatorFactory ValidatorFactory
}

type Runner struct {
	generator        GenerationRunner
	validatorFactory ValidatorFactory
}

func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.ValidatorFactory == nil {
		return nil, fmt.Errorf("validator factory is required")
	}
	return &Runner{
		generator:        cfg.Generator,
		validatorFactory: cfg.ValidatorFactory,
	}, nil
}

func (runner *Runner) RunControlled(
	ctx context.Context,
	fixtures ControlledFixtureSet,
	cfg ControlledRunConfig,
) (ControlledRun, error) {
	if err := validateControlledRunConfig(cfg); err != nil {
		return ControlledRun{}, err
	}
	if fixtures.BenchmarkVersion != VersionV1 {
		return ControlledRun{}, fmt.Errorf("controlled fixture benchmark version must equal %q", VersionV1)
	}
	if fixtures.FixtureKind != FixtureKindSyntheticControlled {
		return ControlledRun{}, fmt.Errorf("controlled run requires synthetic controlled fixtures")
	}
	if err := validateControlledStoryRights(fixtures.Story.Rights); err != nil {
		return ControlledRun{}, fmt.Errorf("controlled fixture publication boundary is invalid: %w", err)
	}
	if len(fixtures.Cases) == 0 {
		return ControlledRun{}, fmt.Errorf("controlled run requires at least one fixture case")
	}

	validators, err := runner.prepareValidators(cfg.Validators)
	if err != nil {
		return ControlledRun{}, err
	}

	run := ControlledRun{
		BenchmarkVersion:      VersionV1,
		Status:                TrialStatusComplete,
		ValidationRepetitions: cfg.ValidationRepetitions,
		Validators:            cloneValidatorConfigs(cfg.Validators),
		Trials:                make([]ValidationTrial, 0, len(fixtures.Cases)*len(cfg.Validators)*cfg.ValidationRepetitions),
	}
	scores := make([]AssessmentScore, 0, cap(run.Trials))

	for _, fixtureCase := range fixtures.Cases {
		analysis, editions, err := buildControlledCaseArtifacts(ctx, fixtures.Story, fixtureCase)
		if err != nil {
			return ControlledRun{}, fmt.Errorf("build controlled fixture artifacts for %q: %w", fixtureCase.ID, err)
		}

		for _, validator := range validators {
			for repetition := 1; repetition <= cfg.ValidationRepetitions; repetition++ {
				trial := validationTrialTarget(
					fixtureCase.ID,
					0,
					repetition,
					validator.config.ID,
					fixtureCase.Expectation.AssessmentScope,
					fixtureCase.Expectation.EditionKey,
					fixtureCase.Expectation.EditionKeys,
				)

				artifact, err := runSemanticTarget(
					ctx,
					validator.validator,
					fixtures.Story.Title,
					fixtures.Story.Author,
					fixtures.Story.CanonicalSource,
					analysis,
					editions,
					fixtureCase.Expectation.AssessmentScope,
				)
				if err != nil {
					trial.Status = TrialStatusIncomplete
					trial.Error = err.Error()
					run.Status = TrialStatusIncomplete
					run.Trials = append(run.Trials, trial)
					continue
				}
				if err := validateAssessmentArtifactBindings(artifact, analysis, editions); err != nil {
					trial.Status = TrialStatusIncomplete
					trial.Error = fmt.Sprintf("semantic assessment artifact is invalid: %v", err)
					run.Status = TrialStatusIncomplete
					run.Trials = append(run.Trials, trial)
					continue
				}

				score, err := ScoreAssessment(fixtureCase.Expectation, artifact.Assessment)
				if err != nil {
					trial.Status = TrialStatusIncomplete
					trial.Error = fmt.Sprintf("score semantic assessment: %v", err)
					run.Status = TrialStatusIncomplete
					run.Trials = append(run.Trials, trial)
					continue
				}

				artifactCopy := artifact
				scoreCopy := score
				trial.Status = TrialStatusComplete
				trial.AssessmentArtifact = &artifactCopy
				trial.Score = &scoreCopy
				run.Trials = append(run.Trials, trial)
				scores = append(scores, score)
			}
		}
	}

	run.AttemptedTrials = len(run.Trials)
	for _, trial := range run.Trials {
		if trial.Status == TrialStatusComplete {
			run.CompletedTrials++
		} else {
			run.IncompleteTrials++
		}
	}
	run.QualitySummary = Summarize(scores)
	return run, nil
}

// RunEndToEnd exercises PR92 generation and PR93 validation without deciding
// whether source material is legally eligible for publication. Source eligibility
// remains an upstream boundary.
func (runner *Runner) RunEndToEnd(
	ctx context.Context,
	source EndToEndSource,
	cfg EndToEndRunConfig,
) (EndToEndRun, error) {
	if runner.generator == nil {
		return EndToEndRun{}, fmt.Errorf("generation runner is required for end-to-end benchmarks")
	}
	if err := source.Validate(); err != nil {
		return EndToEndRun{}, fmt.Errorf("end-to-end source is invalid: %w", err)
	}
	if err := validateEndToEndRunConfig(cfg); err != nil {
		return EndToEndRun{}, err
	}

	validators, err := runner.prepareValidators(cfg.Validators)
	if err != nil {
		return EndToEndRun{}, err
	}

	run := EndToEndRun{
		BenchmarkVersion:      VersionV1,
		Status:                TrialStatusComplete,
		GenerationRepetitions: cfg.GenerationRepetitions,
		ValidationRepetitions: cfg.ValidationRepetitions,
		Validators:            cloneValidatorConfigs(cfg.Validators),
		Generations:           make([]GenerationRepetition, 0, cfg.GenerationRepetitions),
	}

	for generationRepetition := 1; generationRepetition <= cfg.GenerationRepetitions; generationRepetition++ {
		generation := GenerationRepetition{
			Repetition:       generationRepetition,
			GenerationStatus: TrialStatusComplete,
			ValidationStatus: TrialStatusComplete,
			Editions:         make([]storygeneration.GeneratedEditionArtifact, 0, len(storygeneration.DerivedEditionKeysV2())),
			ValidationTrials: make([]ValidationTrial, 0, len(validators)*cfg.ValidationRepetitions*5),
		}

		analysis, err := runner.generator.AnalyseSource(ctx, storygeneration.SourceAnalysisPromptInput{
			Title:           source.Title,
			Author:          source.Author,
			CanonicalSource: source.CanonicalSource,
		})
		if err != nil {
			generation.GenerationStatus = TrialStatusIncomplete
			generation.GenerationError = fmt.Sprintf("analyse canonical source: %v", err)
			generation.ValidationStatus = TrialStatusIncomplete
			run.Status = TrialStatusIncomplete
			run.Generations = append(run.Generations, generation)
			continue
		}
		if err := validateAnalysisArtifact(analysis, source.CanonicalSource); err != nil {
			generation.GenerationStatus = TrialStatusIncomplete
			generation.GenerationError = fmt.Sprintf("analyse canonical source returned invalid artifact: %v", err)
			generation.ValidationStatus = TrialStatusIncomplete
			run.Status = TrialStatusIncomplete
			run.Generations = append(run.Generations, generation)
			continue
		}
		analysisCopy := analysis
		generation.AnalysisArtifact = &analysisCopy

		generationFailed := false
		for _, editionKey := range storygeneration.DerivedEditionKeysV2() {
			edition, err := runner.generator.GenerateEdition(ctx, storygeneration.GenerateEditionInput{
				EditionKey:       editionKey,
				Title:            source.Title,
				Author:           source.Author,
				Slug:             source.Slug,
				Language:         source.Language,
				SourceURL:        source.SourceURL,
				Rights:           cloneStringAnyMap(source.Rights),
				CanonicalSource:  source.CanonicalSource,
				AnalysisArtifact: analysis,
			})
			if err != nil {
				generation.GenerationStatus = TrialStatusIncomplete
				generation.GenerationError = fmt.Sprintf("generate edition %q: %v", editionKey, err)
				generation.ValidationStatus = TrialStatusIncomplete
				run.Status = TrialStatusIncomplete
				generationFailed = true
				break
			}
			if err := validateGeneratedEditionArtifact(edition, editionKey, analysis); err != nil {
				generation.GenerationStatus = TrialStatusIncomplete
				generation.GenerationError = fmt.Sprintf("generate edition %q returned invalid artifact: %v", editionKey, err)
				generation.ValidationStatus = TrialStatusIncomplete
				run.Status = TrialStatusIncomplete
				generationFailed = true
				break
			}
			generation.Editions = append(generation.Editions, edition)
		}
		if generationFailed {
			run.Generations = append(run.Generations, generation)
			continue
		}

		for _, validator := range validators {
			for validationRepetition := 1; validationRepetition <= cfg.ValidationRepetitions; validationRepetition++ {
				for _, edition := range generation.Editions {
					key := edition.EditionKey
					trial := validationTrialTarget(
						source.ID,
						generationRepetition,
						validationRepetition,
						validator.config.ID,
						adaptationcontract.AssessmentScopeEdition,
						&key,
						nil,
					)
					artifact, err := validator.validator.ValidateEdition(ctx, storyvalidation.EditionValidationPromptInput{
						Title:            source.Title,
						Author:           source.Author,
						CanonicalSource:  source.CanonicalSource,
						AnalysisArtifact: analysis,
						GeneratedEdition: edition,
					})
					if err != nil {
						trial.Status = TrialStatusIncomplete
						trial.Error = err.Error()
						generation.ValidationStatus = TrialStatusIncomplete
						run.Status = TrialStatusIncomplete
						generation.ValidationTrials = append(generation.ValidationTrials, trial)
						continue
					}
					if err := validateAssessmentArtifactBindings(artifact, analysis, []storygeneration.GeneratedEditionArtifact{edition}); err != nil {
						trial.Status = TrialStatusIncomplete
						trial.Error = fmt.Sprintf("semantic assessment artifact is invalid: %v", err)
						generation.ValidationStatus = TrialStatusIncomplete
						run.Status = TrialStatusIncomplete
						generation.ValidationTrials = append(generation.ValidationTrials, trial)
						continue
					}
					artifactCopy := artifact
					trial.Status = TrialStatusComplete
					trial.AssessmentArtifact = &artifactCopy
					generation.ValidationTrials = append(generation.ValidationTrials, trial)
				}

				bundleKeys := editionKeys(generation.Editions)
				bundleTrial := validationTrialTarget(
					source.ID,
					generationRepetition,
					validationRepetition,
					validator.config.ID,
					adaptationcontract.AssessmentScopeBundle,
					nil,
					bundleKeys,
				)
				artifact, err := validator.validator.ValidateBundle(ctx, storyvalidation.BundleValidationPromptInput{
					Title:             source.Title,
					Author:            source.Author,
					CanonicalSource:   source.CanonicalSource,
					AnalysisArtifact:  analysis,
					GeneratedEditions: append([]storygeneration.GeneratedEditionArtifact(nil), generation.Editions...),
				})
				if err != nil {
					bundleTrial.Status = TrialStatusIncomplete
					bundleTrial.Error = err.Error()
					generation.ValidationStatus = TrialStatusIncomplete
					run.Status = TrialStatusIncomplete
					generation.ValidationTrials = append(generation.ValidationTrials, bundleTrial)
					continue
				}
				if err := validateAssessmentArtifactBindings(artifact, analysis, generation.Editions); err != nil {
					bundleTrial.Status = TrialStatusIncomplete
					bundleTrial.Error = fmt.Sprintf("semantic assessment artifact is invalid: %v", err)
					generation.ValidationStatus = TrialStatusIncomplete
					run.Status = TrialStatusIncomplete
					generation.ValidationTrials = append(generation.ValidationTrials, bundleTrial)
					continue
				}
				artifactCopy := artifact
				bundleTrial.Status = TrialStatusComplete
				bundleTrial.AssessmentArtifact = &artifactCopy
				generation.ValidationTrials = append(generation.ValidationTrials, bundleTrial)
			}
		}

		run.Generations = append(run.Generations, generation)
	}

	return run, nil
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

func validateAssessmentArtifactBindings(
	artifact storyvalidation.AssessmentArtifact,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
) error {
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("assessment artifact is invalid: %w", err)
	}
	if artifact.SourceSHA256 != analysis.SourceSHA256 {
		return fmt.Errorf("assessment artifact source binding does not match StoryAnalysis")
	}
	if artifact.AnalysisSHA256 != analysis.AnalysisSHA256 {
		return fmt.Errorf("assessment artifact analysis binding does not match StoryAnalysis")
	}
	if len(artifact.EditionBindings) != len(editions) {
		return fmt.Errorf("assessment artifact edition binding count does not match supplied editions")
	}
	for index, edition := range editions {
		binding := artifact.EditionBindings[index]
		if binding.EditionKey != edition.EditionKey {
			return fmt.Errorf("assessment artifact edition binding %d targets %q, want %q", index+1, binding.EditionKey, edition.EditionKey)
		}
		if binding.ContentSHA256 != edition.ContentSHA256 {
			return fmt.Errorf("assessment artifact edition binding %d content digest does not match generated edition", index+1)
		}
	}
	return nil
}

func (source EndToEndSource) Validate() error {
	for label, value := range map[string]string{
		"ID":               source.ID,
		"slug":             source.Slug,
		"title":            source.Title,
		"author":           source.Author,
		"language":         source.Language,
		"source URL":       source.SourceURL,
		"canonical source": source.CanonicalSource,
	} {
		if !utf8.ValidString(value) {
			return fmt.Errorf("source %s must be valid UTF-8", label)
		}
	}
	if !fixtureIDPattern.MatchString(strings.TrimSpace(source.ID)) {
		return fmt.Errorf("source ID is invalid")
	}
	if !fixtureIDPattern.MatchString(strings.TrimSpace(source.Slug)) {
		return fmt.Errorf("source slug is invalid")
	}
	if strings.TrimSpace(source.Title) == "" {
		return fmt.Errorf("source title is required")
	}
	if strings.TrimSpace(source.Language) == "" {
		return fmt.Errorf("source language is required")
	}
	if strings.TrimSpace(source.SourceURL) == "" {
		return fmt.Errorf("source URL is required")
	}
	if len(source.Rights) == 0 {
		return fmt.Errorf("source rights are required")
	}
	if strings.TrimSpace(source.CanonicalSource) == "" {
		return fmt.Errorf("canonical source is required")
	}
	return nil
}

type preparedValidator struct {
	config    ValidatorConfig
	validator SemanticValidator
}

func (runner *Runner) prepareValidators(configs []ValidatorConfig) ([]preparedValidator, error) {
	if err := validateValidatorConfigs(configs); err != nil {
		return nil, err
	}
	validators := make([]preparedValidator, 0, len(configs))
	for _, config := range configs {
		validator, err := runner.validatorFactory(config)
		if err != nil {
			return nil, fmt.Errorf("create validator config %q: %w", config.ID, err)
		}
		if validator == nil {
			return nil, fmt.Errorf("create validator config %q: validator is nil", config.ID)
		}
		validators = append(validators, preparedValidator{config: config, validator: validator})
	}
	return validators, nil
}

func validateControlledRunConfig(cfg ControlledRunConfig) error {
	if cfg.ValidationRepetitions < 1 {
		return fmt.Errorf("validation repetitions must be positive")
	}
	return validateValidatorConfigs(cfg.Validators)
}

func validateEndToEndRunConfig(cfg EndToEndRunConfig) error {
	if cfg.GenerationRepetitions < 1 {
		return fmt.Errorf("generation repetitions must be positive")
	}
	if cfg.ValidationRepetitions < 1 {
		return fmt.Errorf("validation repetitions must be positive")
	}
	return validateValidatorConfigs(cfg.Validators)
}

func validateValidatorConfigs(configs []ValidatorConfig) error {
	if len(configs) == 0 {
		return fmt.Errorf("at least one validator config is required")
	}
	seen := make(map[string]struct{}, len(configs))
	for index, config := range configs {
		id := strings.TrimSpace(config.ID)
		if !fixtureIDPattern.MatchString(id) {
			return fmt.Errorf("validator config %d ID %q is invalid", index+1, config.ID)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("validator config ID %q is duplicated", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(config.Model) == "" {
			return fmt.Errorf("validator config %q model is required", id)
		}
		if !validBenchmarkReasoningEffort(config.ReasoningEffort) {
			return fmt.Errorf("validator config %q reasoning effort %q is invalid", id, config.ReasoningEffort)
		}
		if config.MaxOutputTokens < 1 {
			return fmt.Errorf("validator config %q max output tokens must be positive", id)
		}
	}
	return nil
}

func validBenchmarkReasoningEffort(effort storygeneration.ReasoningEffort) bool {
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

func runSemanticTarget(
	ctx context.Context,
	validator SemanticValidator,
	title string,
	author string,
	canonicalSource string,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	scope adaptationcontract.AssessmentScope,
) (storyvalidation.AssessmentArtifact, error) {
	switch scope {
	case adaptationcontract.AssessmentScopeEdition:
		if len(editions) != 1 {
			return storyvalidation.AssessmentArtifact{}, fmt.Errorf("edition semantic target requires exactly one generated edition")
		}
		return validator.ValidateEdition(ctx, storyvalidation.EditionValidationPromptInput{
			Title:            title,
			Author:           author,
			CanonicalSource:  canonicalSource,
			AnalysisArtifact: analysis,
			GeneratedEdition: editions[0],
		})
	case adaptationcontract.AssessmentScopeBundle:
		return validator.ValidateBundle(ctx, storyvalidation.BundleValidationPromptInput{
			Title:             title,
			Author:            author,
			CanonicalSource:   canonicalSource,
			AnalysisArtifact:  analysis,
			GeneratedEditions: append([]storygeneration.GeneratedEditionArtifact(nil), editions...),
		})
	default:
		return storyvalidation.AssessmentArtifact{}, fmt.Errorf("unsupported semantic assessment scope %q", scope)
	}
}

func validationTrialTarget(
	caseID string,
	generationRepetition int,
	validationRepetition int,
	validatorConfigID string,
	scope adaptationcontract.AssessmentScope,
	editionKey *model.AdminStoryEditionKey,
	editionKeys []model.AdminStoryEditionKey,
) ValidationTrial {
	return ValidationTrial{
		CaseID:               caseID,
		GenerationRepetition: generationRepetition,
		ValidationRepetition: validationRepetition,
		ValidatorConfigID:    validatorConfigID,
		AssessmentScope:      scope,
		EditionKey:           copyEditionKey(editionKey),
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), editionKeys...),
	}
}

func editionKeys(editions []storygeneration.GeneratedEditionArtifact) []model.AdminStoryEditionKey {
	keys := make([]model.AdminStoryEditionKey, 0, len(editions))
	for _, edition := range editions {
		keys = append(keys, edition.EditionKey)
	}
	return keys
}

func cloneValidatorConfigs(configs []ValidatorConfig) []ValidatorConfig {
	return append([]ValidatorConfig(nil), configs...)
}
