package storybenchmark

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/storygeneration"
)

const EndToEndSuite = "end_to_end"

type EndToEndGenerationConfig struct {
	Model                   string                          `json:"model"`
	AnalysisReasoningEffort storygeneration.ReasoningEffort `json:"analysisReasoningEffort"`
	AnalysisMaxOutputTokens int                             `json:"analysisMaxOutputTokens"`
	EditionReasoningEffort  storygeneration.ReasoningEffort `json:"editionReasoningEffort"`
	EditionMaxOutputTokens  int                             `json:"editionMaxOutputTokens"`
}

type EndToEndSourceRecord struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Author            string `json:"author"`
	Provider          string `json:"provider"`
	ExternalID        string `json:"externalId"`
	SourceURL         string `json:"sourceUrl"`
	SourceSHA256      string `json:"sourceSha256"`
	EligibilityPolicy string `json:"eligibilityPolicy"`
	EligibilityDate   string `json:"eligibilityDate"`
	USStatus          string `json:"usStatus"`
	UKStatus          string `json:"ukStatus"`
	OverallStatus     string `json:"overallStatus"`
}

type GenerationUsage struct {
	AnalysisResponses int        `json:"analysisResponses"`
	EditionResponses  int        `json:"editionResponses"`
	Usage             TokenUsage `json:"usage"`
}

type EndToEndUsageSummary struct {
	CompletedResponses int              `json:"completedResponses"`
	Usage              TokenUsage       `json:"usage"`
	Generation         GenerationUsage  `json:"generation"`
	ByValidator        []ValidatorUsage `json:"byValidator"`
}

type EndToEndResultDocument struct {
	BenchmarkVersion Version                  `json:"benchmarkVersion"`
	Suite            string                   `json:"suite"`
	StartedAt        string                   `json:"startedAt"`
	FinishedAt       string                   `json:"finishedAt"`
	Source           EndToEndSourceRecord     `json:"source"`
	GenerationConfig EndToEndGenerationConfig `json:"generationConfig"`
	Run              EndToEndRun              `json:"run"`
	Usage            EndToEndUsageSummary     `json:"usage"`
	ResponsesAPI     ResponseTelemetry        `json:"responsesApiTelemetry"`
}

func BuildEndToEndResultDocument(
	startedAt time.Time,
	finishedAt time.Time,
	fixture PublicDomainFixture,
	generationConfig EndToEndGenerationConfig,
	run EndToEndRun,
	responses ...ResponseTelemetry,
) (EndToEndResultDocument, error) {
	if startedAt.IsZero() || finishedAt.IsZero() {
		return EndToEndResultDocument{}, fmt.Errorf("benchmark timestamps are required")
	}
	if finishedAt.Before(startedAt) {
		return EndToEndResultDocument{}, fmt.Errorf("benchmark finishedAt must not precede startedAt")
	}
	if err := validatePublicDomainFixtureForReporting(fixture); err != nil {
		return EndToEndResultDocument{}, err
	}
	if err := validateEndToEndGenerationConfig(generationConfig); err != nil {
		return EndToEndResultDocument{}, err
	}
	if err := validateEndToEndRunForReporting(run, fixture, generationConfig); err != nil {
		return EndToEndResultDocument{}, err
	}
	usage, err := summarizeEndToEndUsage(run)
	if err != nil {
		return EndToEndResultDocument{}, err
	}
	responseTelemetry, err := optionalResponseTelemetry(responses)
	if err != nil {
		return EndToEndResultDocument{}, fmt.Errorf("end-to-end Responses telemetry is invalid: %w", err)
	}

	return EndToEndResultDocument{
		BenchmarkVersion: VersionV1,
		Suite:            EndToEndSuite,
		StartedAt:        startedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt:       finishedAt.UTC().Format(time.RFC3339Nano),
		Source: EndToEndSourceRecord{
			ID:                fixture.Source.ID,
			Title:             fixture.Source.Title,
			Author:            fixture.Source.Author,
			Provider:          string(fixture.Provider),
			ExternalID:        fixture.ExternalID,
			SourceURL:         fixture.Source.SourceURL,
			SourceSHA256:      fixture.CanonicalSourceSHA256,
			EligibilityPolicy: fixture.EligibilityPolicy,
			EligibilityDate:   fixture.EligibilityDate.UTC().Format("2006-01-02"),
			USStatus:          string(fixture.EligibilityAssessment.US.Status),
			UKStatus:          string(fixture.EligibilityAssessment.UK.Status),
			OverallStatus:     string(fixture.EligibilityAssessment.Overall),
		},
		GenerationConfig: generationConfig,
		Run:              run,
		Usage:            usage,
		ResponsesAPI:     responseTelemetry,
	}, nil
}

func MarshalEndToEndResultJSON(document EndToEndResultDocument) ([]byte, error) {
	if err := validateEndToEndResultDocument(document); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode end-to-end benchmark result: %w", err)
	}
	return append(encoded, '\n'), nil
}

func RenderEndToEndMarkdown(document EndToEndResultDocument) (string, error) {
	if err := validateEndToEndResultDocument(document); err != nil {
		return "", err
	}

	var completeGenerations, incompleteGenerations int
	var completeValidationTrials, incompleteValidationTrials int
	for _, generation := range document.Run.Generations {
		if generation.GenerationStatus == TrialStatusComplete {
			completeGenerations++
		} else {
			incompleteGenerations++
		}
		for _, trial := range generation.ValidationTrials {
			if trial.Status == TrialStatusComplete {
				completeValidationTrials++
			} else {
				incompleteValidationTrials++
			}
		}
	}

	var builder strings.Builder
	builder.WriteString("# Panda Pages story adaptation benchmark\n\n")
	fmt.Fprintf(&builder, "- Benchmark: `%s`\n", document.BenchmarkVersion)
	fmt.Fprintf(&builder, "- Suite: `%s`\n", document.Suite)
	fmt.Fprintf(&builder, "- Started: `%s`\n", document.StartedAt)
	fmt.Fprintf(&builder, "- Finished: `%s`\n", document.FinishedAt)
	fmt.Fprintf(&builder, "- Technical status: **%s**\n", document.Run.Status)
	fmt.Fprintf(&builder, "- Source: *%s* by %s\n", document.Source.Title, document.Source.Author)
	fmt.Fprintf(&builder, "- Source binding: `%s` / `%s` / `%s`\n", document.Source.Provider, document.Source.ExternalID, document.Source.SourceSHA256)
	fmt.Fprintf(&builder, "- Eligibility snapshot: `%s` on `%s` (US `%s`, UK `%s`, overall `%s`)\n", document.Source.EligibilityPolicy, document.Source.EligibilityDate, document.Source.USStatus, document.Source.UKStatus, document.Source.OverallStatus)
	fmt.Fprintf(&builder, "- Generation model: `%s`\n", document.GenerationConfig.Model)
	fmt.Fprintf(&builder, "- Source analysis: reasoning `%s`, max output %d tokens\n", document.GenerationConfig.AnalysisReasoningEffort, document.GenerationConfig.AnalysisMaxOutputTokens)
	fmt.Fprintf(&builder, "- Edition generation: reasoning `%s`, max output %d tokens\n\n", document.GenerationConfig.EditionReasoningEffort, document.GenerationConfig.EditionMaxOutputTokens)

	builder.WriteString("## Technical execution\n\n")
	fmt.Fprintf(&builder, "- Generation repetitions: %d complete, %d incomplete\n", completeGenerations, incompleteGenerations)
	fmt.Fprintf(&builder, "- Validation trials: %d complete, %d incomplete\n", completeValidationTrials, incompleteValidationTrials)
	fmt.Fprintf(&builder, "- Validation repetitions per generated artifact set: %d\n\n", document.Run.ValidationRepetitions)

	builder.WriteString("## Validator matrix\n\n")
	builder.WriteString("| Config | Model | Reasoning | Max output tokens | Valid assessment artifacts | Retained tokens |\n")
	builder.WriteString("| --- | --- | --- | ---: | ---: | ---: |\n")
	usageByID := make(map[string]ValidatorUsage, len(document.Usage.ByValidator))
	for _, usage := range document.Usage.ByValidator {
		usageByID[usage.ValidatorConfigID] = usage
	}
	for _, config := range document.Run.Validators {
		usage := usageByID[config.ID]
		fmt.Fprintf(&builder, "| `%s` | `%s` | `%s` | %d | %d | %d |\n", config.ID, config.Model, config.ReasoningEffort, config.MaxOutputTokens, usage.CompletedResponses, usage.Usage.TotalTokens)
	}

	builder.WriteString("\n## Responses API telemetry\n\n")
	fmt.Fprintf(&builder, "- Attempted requests: %d\n", document.ResponsesAPI.AttemptedRequests)
	fmt.Fprintf(&builder, "- Successful Responses API results: %d\n", document.ResponsesAPI.SuccessfulResponses)
	fmt.Fprintf(&builder, "- Failed Responses API requests: %d\n", document.ResponsesAPI.FailedRequests)
	fmt.Fprintf(&builder, "- Input tokens across successful API results: %d\n", document.ResponsesAPI.Usage.InputTokens)
	fmt.Fprintf(&builder, "- Cached input tokens across successful API results: %d\n", document.ResponsesAPI.Usage.CachedTokens)
	fmt.Fprintf(&builder, "- Output tokens across successful API results: %d\n", document.ResponsesAPI.Usage.OutputTokens)
	fmt.Fprintf(&builder, "- Reasoning tokens across successful API results: %d\n", document.ResponsesAPI.Usage.ReasoningTokens)
	fmt.Fprintf(&builder, "- Total tokens across successful API results: %d\n\n", document.ResponsesAPI.Usage.TotalTokens)
	if len(document.ResponsesAPI.ByRequestedModel) > 0 {
		builder.WriteString("| Requested model | Attempted | Successful | Failed | Total tokens |\n")
		builder.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
		for _, usage := range document.ResponsesAPI.ByRequestedModel {
			fmt.Fprintf(&builder, "| `%s` | %d | %d | %d | %d |\n", usage.RequestedModel, usage.AttemptedRequests, usage.SuccessfulResponses, usage.FailedRequests, usage.Usage.TotalTokens)
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Retained artifact telemetry\n\n")
	fmt.Fprintf(&builder, "- Generation artifacts retained: %d analysis + %d editions\n", document.Usage.Generation.AnalysisResponses, document.Usage.Generation.EditionResponses)
	fmt.Fprintf(&builder, "- Completed/retained model artifacts represented: %d\n", document.Usage.CompletedResponses)
	fmt.Fprintf(&builder, "- Input tokens represented in retained artifacts: %d\n", document.Usage.Usage.InputTokens)
	fmt.Fprintf(&builder, "- Cached input tokens represented in retained artifacts: %d\n", document.Usage.Usage.CachedTokens)
	fmt.Fprintf(&builder, "- Output tokens represented in retained artifacts: %d\n", document.Usage.Usage.OutputTokens)
	fmt.Fprintf(&builder, "- Reasoning tokens represented in retained artifacts: %d\n", document.Usage.Usage.ReasoningTokens)
	fmt.Fprintf(&builder, "- Total tokens represented in retained artifacts: %d\n\n", document.Usage.Usage.TotalTokens)

	builder.WriteString("## Human review\n\n")
	builder.WriteString("A `human-review-template.json` file is emitted beside this report. It is bound to the exact generated analysis and edition content SHA-256 values. Complete it after editorial review, save it separately, and use the offline human-review scoring mode. A review file for regenerated content is rejected as stale.\n\n")

	builder.WriteString("## Interpretation boundary\n\n")
	builder.WriteString("This benchmark measures generation and semantic-validator behaviour against a reviewed source fixture. It is not publication approval, a publication ticket, or permission to bypass human editorial review.\n")
	return builder.String(), nil
}

func validateEndToEndResultDocument(document EndToEndResultDocument) error {
	if document.BenchmarkVersion != VersionV1 {
		return fmt.Errorf("end-to-end result benchmark version must equal %q", VersionV1)
	}
	if document.Suite != EndToEndSuite {
		return fmt.Errorf("end-to-end result suite must equal %q", EndToEndSuite)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.StartedAt))
	if err != nil {
		return fmt.Errorf("end-to-end result startedAt is invalid: %w", err)
	}
	finishedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.FinishedAt))
	if err != nil {
		return fmt.Errorf("end-to-end result finishedAt is invalid: %w", err)
	}
	if finishedAt.Before(startedAt) {
		return fmt.Errorf("end-to-end result finishedAt must not precede startedAt")
	}
	if strings.TrimSpace(document.Source.ID) == "" || !validFixtureSHA256(strings.TrimSpace(document.Source.SourceSHA256)) {
		return fmt.Errorf("end-to-end result source binding is required and must contain a SHA-256")
	}
	if strings.TrimSpace(document.Source.EligibilityPolicy) == "" || strings.TrimSpace(document.Source.EligibilityDate) == "" {
		return fmt.Errorf("end-to-end result eligibility snapshot is required")
	}
	if document.Source.USStatus != string(copyrighteligibility.JurisdictionEligible) ||
		document.Source.UKStatus != string(copyrighteligibility.JurisdictionEligible) ||
		document.Source.OverallStatus != string(copyrighteligibility.OverallEligible) {
		return fmt.Errorf("end-to-end result source must remain policy eligible in both configured jurisdictions")
	}
	if err := validateEndToEndGenerationConfig(document.GenerationConfig); err != nil {
		return err
	}
	if err := document.ResponsesAPI.Validate(); err != nil {
		return fmt.Errorf("end-to-end Responses telemetry is invalid: %w", err)
	}
	return validateEndToEndRunDocumentShape(document.Run, document.Source.ID, document.Source.SourceSHA256, document.GenerationConfig)
}

func validateEndToEndGenerationConfig(config EndToEndGenerationConfig) error {
	if strings.TrimSpace(config.Model) != storygeneration.GenerationModelV2 {
		return fmt.Errorf("end-to-end generation model must equal %q", storygeneration.GenerationModelV2)
	}
	if !validBenchmarkReasoningEffort(config.AnalysisReasoningEffort) || !validBenchmarkReasoningEffort(config.EditionReasoningEffort) {
		return fmt.Errorf("end-to-end generation reasoning effort is invalid")
	}
	if config.AnalysisMaxOutputTokens < 1 || config.EditionMaxOutputTokens < 1 {
		return fmt.Errorf("end-to-end generation max output token budgets must be positive")
	}
	return nil
}

func validatePublicDomainFixtureForReporting(fixture PublicDomainFixture) error {
	if fixture.BenchmarkVersion != VersionV1 || fixture.FixtureKind != FixtureKindEligiblePublicDomain {
		return fmt.Errorf("public-domain fixture is not a v1 eligible benchmark fixture")
	}
	if fixture.EligibilityAssessment.Overall != copyrighteligibility.OverallEligible ||
		fixture.EligibilityAssessment.US.Status != copyrighteligibility.JurisdictionEligible ||
		fixture.EligibilityAssessment.UK.Status != copyrighteligibility.JurisdictionEligible {
		return fmt.Errorf("public-domain fixture is not eligible in both configured jurisdictions")
	}
	if exactFixtureSHA256(fixture.Source.CanonicalSource) != fixture.CanonicalSourceSHA256 {
		return fmt.Errorf("public-domain fixture source digest no longer matches canonical source")
	}
	return nil
}

func validateEndToEndRunForReporting(run EndToEndRun, fixture PublicDomainFixture, generationConfig EndToEndGenerationConfig) error {
	if err := validateEndToEndRunDocumentShape(run, fixture.Source.ID, fixture.CanonicalSourceSHA256, generationConfig); err != nil {
		return err
	}
	if run.BenchmarkVersion != fixture.BenchmarkVersion {
		return fmt.Errorf("end-to-end run benchmark version does not match fixture")
	}
	return nil
}

func validateEndToEndRunDocumentShape(run EndToEndRun, sourceID, sourceSHA256 string, generationConfig EndToEndGenerationConfig) error {
	if run.BenchmarkVersion != VersionV1 {
		return fmt.Errorf("end-to-end run benchmark version must equal %q", VersionV1)
	}
	if run.Status != TrialStatusComplete && run.Status != TrialStatusIncomplete {
		return fmt.Errorf("end-to-end run status %q is invalid", run.Status)
	}
	if run.GenerationRepetitions < 1 || run.ValidationRepetitions < 1 {
		return fmt.Errorf("end-to-end run repetition counts must be positive")
	}
	if err := validateValidatorConfigs(run.Validators); err != nil {
		return fmt.Errorf("end-to-end validator configuration is invalid: %w", err)
	}
	if len(run.Generations) != run.GenerationRepetitions {
		return fmt.Errorf("end-to-end generation count does not match requested repetitions")
	}

	configs := make(map[string]ValidatorConfig, len(run.Validators))
	for _, config := range run.Validators {
		configs[config.ID] = config
	}
	expectedEditionKeys := storygeneration.DerivedEditionKeysV2()
	anyIncomplete := false

	for index, generation := range run.Generations {
		if generation.Repetition != index+1 {
			return fmt.Errorf("end-to-end generation repetition sequence is invalid")
		}
		if generation.GenerationStatus != TrialStatusComplete && generation.GenerationStatus != TrialStatusIncomplete {
			return fmt.Errorf("generation repetition %d has invalid generation status %q", generation.Repetition, generation.GenerationStatus)
		}
		if generation.ValidationStatus != TrialStatusComplete && generation.ValidationStatus != TrialStatusIncomplete {
			return fmt.Errorf("generation repetition %d has invalid validation status %q", generation.Repetition, generation.ValidationStatus)
		}
		if generation.GenerationStatus == TrialStatusComplete && strings.TrimSpace(generation.GenerationError) != "" {
			return fmt.Errorf("generation repetition %d is complete but has a generation error", generation.Repetition)
		}
		if generation.GenerationStatus == TrialStatusIncomplete && strings.TrimSpace(generation.GenerationError) == "" {
			return fmt.Errorf("generation repetition %d is incomplete without a generation error", generation.Repetition)
		}

		if generation.AnalysisArtifact != nil {
			if err := generation.AnalysisArtifact.Validate(); err != nil {
				return fmt.Errorf("generation repetition %d analysis artifact is invalid: %w", generation.Repetition, err)
			}
			if generation.AnalysisArtifact.SourceSHA256 != sourceSHA256 {
				return fmt.Errorf("generation repetition %d source binding does not match reviewed fixture", generation.Repetition)
			}
			if generation.AnalysisArtifact.RequestedModel != generationConfig.Model || generation.AnalysisArtifact.ReasoningEffort != generationConfig.AnalysisReasoningEffort {
				return fmt.Errorf("generation repetition %d source-analysis configuration does not match benchmark generation config", generation.Repetition)
			}
		}
		for editionIndex, edition := range generation.Editions {
			if err := edition.Validate(); err != nil {
				return fmt.Errorf("generation repetition %d edition %d is invalid: %w", generation.Repetition, editionIndex+1, err)
			}
			if edition.SourceSHA256 != sourceSHA256 {
				return fmt.Errorf("generation repetition %d edition %d source binding does not match reviewed fixture", generation.Repetition, editionIndex+1)
			}
			if generation.AnalysisArtifact != nil && edition.AnalysisSHA256 != generation.AnalysisArtifact.AnalysisSHA256 {
				return fmt.Errorf("generation repetition %d edition %d analysis binding does not match StoryAnalysis", generation.Repetition, editionIndex+1)
			}
			if edition.RequestedModel != generationConfig.Model || edition.ReasoningEffort != generationConfig.EditionReasoningEffort {
				return fmt.Errorf("generation repetition %d edition %d configuration does not match benchmark generation config", generation.Repetition, editionIndex+1)
			}
		}

		if generation.GenerationStatus == TrialStatusIncomplete {
			anyIncomplete = true
			if generation.ValidationStatus != TrialStatusIncomplete {
				return fmt.Errorf("generation repetition %d cannot have complete validation after incomplete generation", generation.Repetition)
			}
			if len(generation.ValidationTrials) != 0 {
				return fmt.Errorf("generation repetition %d must not validate a partial generated artifact set", generation.Repetition)
			}
			continue
		}

		if generation.AnalysisArtifact == nil {
			return fmt.Errorf("generation repetition %d is complete without a StoryAnalysis artifact", generation.Repetition)
		}
		if len(generation.Editions) != len(expectedEditionKeys) {
			return fmt.Errorf("generation repetition %d is complete without all canonical derived editions", generation.Repetition)
		}
		for editionIndex, key := range expectedEditionKeys {
			if generation.Editions[editionIndex].EditionKey != key {
				return fmt.Errorf("generation repetition %d edition order is not canonical", generation.Repetition)
			}
		}

		expectedTrials := len(run.Validators) * run.ValidationRepetitions * (len(expectedEditionKeys) + 1)
		if len(generation.ValidationTrials) != expectedTrials {
			return fmt.Errorf("generation repetition %d validation trial count = %d, want %d", generation.Repetition, len(generation.ValidationTrials), expectedTrials)
		}
		seenTrials := make(map[string]struct{}, expectedTrials)
		generationHasIncompleteValidation := false
		for trialIndex, trial := range generation.ValidationTrials {
			if trial.CaseID != sourceID {
				return fmt.Errorf("generation repetition %d validation trial %d source ID %q does not match result source %q", generation.Repetition, trialIndex+1, trial.CaseID, sourceID)
			}
			if trial.GenerationRepetition != generation.Repetition {
				return fmt.Errorf("generation repetition %d validation trial %d has mismatched generation repetition", generation.Repetition, trialIndex+1)
			}
			if trial.ValidationRepetition < 1 || trial.ValidationRepetition > run.ValidationRepetitions {
				return fmt.Errorf("generation repetition %d validation trial %d has invalid validation repetition", generation.Repetition, trialIndex+1)
			}
			config, exists := configs[trial.ValidatorConfigID]
			if !exists {
				return fmt.Errorf("generation repetition %d validation trial %d references unknown validator config %q", generation.Repetition, trialIndex+1, trial.ValidatorConfigID)
			}
			targetKey := reviewTargetKey(trial.GenerationRepetition, trial.AssessmentScope, trial.EditionKey, trial.EditionKeys)
			trialKey := fmt.Sprintf("%s|%d|%s", trial.ValidatorConfigID, trial.ValidationRepetition, targetKey)
			if _, exists := seenTrials[trialKey]; exists {
				return fmt.Errorf("generation repetition %d contains duplicate validation trial %q", generation.Repetition, trialKey)
			}
			seenTrials[trialKey] = struct{}{}

			var boundEditions []storygeneration.GeneratedEditionArtifact
			switch trial.AssessmentScope {
			case adaptationcontract.AssessmentScopeEdition:
				if trial.EditionKey == nil || len(trial.EditionKeys) != 0 {
					return fmt.Errorf("generation repetition %d validation trial %d has invalid edition target", generation.Repetition, trialIndex+1)
				}
				for _, edition := range generation.Editions {
					if edition.EditionKey == *trial.EditionKey {
						boundEditions = []storygeneration.GeneratedEditionArtifact{edition}
						break
					}
				}
				if len(boundEditions) != 1 {
					return fmt.Errorf("generation repetition %d validation trial %d targets an edition not present in generated artifacts", generation.Repetition, trialIndex+1)
				}
			case adaptationcontract.AssessmentScopeBundle:
				if trial.EditionKey != nil || len(trial.EditionKeys) != len(generation.Editions) {
					return fmt.Errorf("generation repetition %d validation trial %d has invalid bundle target", generation.Repetition, trialIndex+1)
				}
				for editionIndex, edition := range generation.Editions {
					if trial.EditionKeys[editionIndex] != edition.EditionKey {
						return fmt.Errorf("generation repetition %d validation trial %d bundle target order does not match generated editions", generation.Repetition, trialIndex+1)
					}
				}
				boundEditions = generation.Editions
			default:
				return fmt.Errorf("generation repetition %d validation trial %d has unsupported scope %q", generation.Repetition, trialIndex+1, trial.AssessmentScope)
			}

			switch trial.Status {
			case TrialStatusComplete:
				if strings.TrimSpace(trial.Error) != "" || trial.AssessmentArtifact == nil {
					return fmt.Errorf("generation repetition %d validation trial %d has an invalid complete envelope", generation.Repetition, trialIndex+1)
				}
				if trial.Score != nil {
					return fmt.Errorf("generation repetition %d validation trial %d must not contain controlled-suite scoring", generation.Repetition, trialIndex+1)
				}
				if trial.AssessmentArtifact.RequestedModel != config.Model || trial.AssessmentArtifact.ReasoningEffort != config.ReasoningEffort {
					return fmt.Errorf("generation repetition %d validation trial %d model configuration does not match validator config", generation.Repetition, trialIndex+1)
				}
				if err := validateAssessmentArtifactBindings(*trial.AssessmentArtifact, *generation.AnalysisArtifact, boundEditions); err != nil {
					return fmt.Errorf("generation repetition %d validation trial %d artifact binding is invalid: %w", generation.Repetition, trialIndex+1, err)
				}
			case TrialStatusIncomplete:
				generationHasIncompleteValidation = true
				anyIncomplete = true
				if strings.TrimSpace(trial.Error) == "" || trial.AssessmentArtifact != nil || trial.Score != nil {
					return fmt.Errorf("generation repetition %d validation trial %d has an invalid incomplete envelope", generation.Repetition, trialIndex+1)
				}
			default:
				return fmt.Errorf("generation repetition %d validation trial %d has invalid status %q", generation.Repetition, trialIndex+1, trial.Status)
			}
		}
		if generationHasIncompleteValidation && generation.ValidationStatus != TrialStatusIncomplete {
			return fmt.Errorf("generation repetition %d validation status does not reflect incomplete trials", generation.Repetition)
		}
		if !generationHasIncompleteValidation && generation.ValidationStatus != TrialStatusComplete {
			return fmt.Errorf("generation repetition %d validation status is incomplete without an incomplete trial", generation.Repetition)
		}
	}
	if anyIncomplete && run.Status != TrialStatusIncomplete {
		return fmt.Errorf("end-to-end run status does not reflect incomplete work")
	}
	if !anyIncomplete && run.Status != TrialStatusComplete {
		return fmt.Errorf("end-to-end run is incomplete without incomplete work")
	}
	return nil
}

func summarizeEndToEndUsage(run EndToEndRun) (EndToEndUsageSummary, error) {
	configs := make(map[string]ValidatorConfig, len(run.Validators))
	byID := make(map[string]*ValidatorUsage, len(run.Validators))
	for _, config := range run.Validators {
		if _, exists := configs[config.ID]; exists {
			return EndToEndUsageSummary{}, fmt.Errorf("end-to-end run contains duplicate validator config ID %q", config.ID)
		}
		configs[config.ID] = config
		byID[config.ID] = &ValidatorUsage{ValidatorConfigID: config.ID, Model: config.Model, ReasoningEffort: config.ReasoningEffort}
	}

	summary := EndToEndUsageSummary{}
	for _, generation := range run.Generations {
		if generation.AnalysisArtifact != nil {
			usage := tokenUsageFromResponses(generation.AnalysisArtifact.Usage)
			summary.Generation.AnalysisResponses++
			summary.CompletedResponses++
			addTokenUsage(&summary.Generation.Usage, usage)
			addTokenUsage(&summary.Usage, usage)
		}
		for _, edition := range generation.Editions {
			usage := tokenUsageFromResponses(edition.Usage)
			summary.Generation.EditionResponses++
			summary.CompletedResponses++
			addTokenUsage(&summary.Generation.Usage, usage)
			addTokenUsage(&summary.Usage, usage)
		}
		for trialIndex, trial := range generation.ValidationTrials {
			config, exists := configs[trial.ValidatorConfigID]
			if !exists {
				return EndToEndUsageSummary{}, fmt.Errorf("validation trial %d references unknown validator config %q", trialIndex+1, trial.ValidatorConfigID)
			}
			if trial.Status != TrialStatusComplete {
				continue
			}
			if trial.AssessmentArtifact == nil {
				return EndToEndUsageSummary{}, fmt.Errorf("complete validation trial %d has no assessment artifact", trialIndex+1)
			}
			artifact := trial.AssessmentArtifact
			if artifact.RequestedModel != config.Model || artifact.ReasoningEffort != config.ReasoningEffort {
				return EndToEndUsageSummary{}, fmt.Errorf("validation trial %d model configuration does not match validator config", trialIndex+1)
			}
			usage := tokenUsageFromResponses(artifact.Usage)
			summary.CompletedResponses++
			addTokenUsage(&summary.Usage, usage)
			validatorUsage := byID[trial.ValidatorConfigID]
			validatorUsage.CompletedResponses++
			addTokenUsage(&validatorUsage.Usage, usage)
		}
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		summary.ByValidator = append(summary.ByValidator, *byID[id])
	}
	return summary, nil
}
