package storybenchmark

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"pandapages/api/internal/storygeneration"
)

const ControlledSuite = "controlled"

type TokenUsage struct {
	InputTokens     int `json:"inputTokens"`
	CachedTokens    int `json:"cachedTokens"`
	OutputTokens    int `json:"outputTokens"`
	ReasoningTokens int `json:"reasoningTokens"`
	TotalTokens     int `json:"totalTokens"`
}

type ValidatorUsage struct {
	ValidatorConfigID  string                          `json:"validatorConfigId"`
	Model              string                          `json:"model"`
	ReasoningEffort    storygeneration.ReasoningEffort `json:"reasoningEffort"`
	CompletedResponses int                             `json:"completedResponses"`
	Usage              TokenUsage                      `json:"usage"`
}

type UsageSummary struct {
	CompletedResponses int              `json:"completedResponses"`
	Usage              TokenUsage       `json:"usage"`
	ByValidator        []ValidatorUsage `json:"byValidator"`
}

type ControlledResultDocument struct {
	BenchmarkVersion Version           `json:"benchmarkVersion"`
	Suite            string            `json:"suite"`
	StartedAt        string            `json:"startedAt"`
	FinishedAt       string            `json:"finishedAt"`
	Run              ControlledRun     `json:"run"`
	Usage            UsageSummary      `json:"usage"`
	ResponsesAPI     ResponseTelemetry `json:"responsesApiTelemetry"`
}

func BuildControlledResultDocument(
	startedAt time.Time,
	finishedAt time.Time,
	run ControlledRun,
	responses ...ResponseTelemetry,
) (ControlledResultDocument, error) {
	if run.BenchmarkVersion != VersionV1 {
		return ControlledResultDocument{}, fmt.Errorf("controlled run benchmark version must equal %q", VersionV1)
	}
	if startedAt.IsZero() || finishedAt.IsZero() {
		return ControlledResultDocument{}, fmt.Errorf("benchmark timestamps are required")
	}
	if finishedAt.Before(startedAt) {
		return ControlledResultDocument{}, fmt.Errorf("benchmark finishedAt must not precede startedAt")
	}
	if err := validateControlledRunForReporting(run); err != nil {
		return ControlledResultDocument{}, err
	}

	usage, err := summarizeControlledUsage(run)
	if err != nil {
		return ControlledResultDocument{}, err
	}
	responseTelemetry, err := optionalResponseTelemetry(responses)
	if err != nil {
		return ControlledResultDocument{}, fmt.Errorf("controlled Responses telemetry is invalid: %w", err)
	}

	return ControlledResultDocument{
		BenchmarkVersion: VersionV1,
		Suite:            ControlledSuite,
		StartedAt:        startedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt:       finishedAt.UTC().Format(time.RFC3339Nano),
		Run:              run,
		Usage:            usage,
		ResponsesAPI:     responseTelemetry,
	}, nil
}

func MarshalControlledResultJSON(document ControlledResultDocument) ([]byte, error) {
	if document.BenchmarkVersion != VersionV1 {
		return nil, fmt.Errorf("result document benchmark version must equal %q", VersionV1)
	}
	if document.Suite != ControlledSuite {
		return nil, fmt.Errorf("result document suite must equal %q", ControlledSuite)
	}
	if err := document.ResponsesAPI.Validate(); err != nil {
		return nil, fmt.Errorf("controlled Responses telemetry is invalid: %w", err)
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode controlled benchmark result: %w", err)
	}
	return append(encoded, '\n'), nil
}

func RenderControlledMarkdown(document ControlledResultDocument) (string, error) {
	if document.BenchmarkVersion != VersionV1 || document.Suite != ControlledSuite {
		return "", fmt.Errorf("controlled result document is invalid")
	}
	if err := document.ResponsesAPI.Validate(); err != nil {
		return "", fmt.Errorf("controlled Responses telemetry is invalid: %w", err)
	}

	run := document.Run
	summary := run.QualitySummary
	var builder strings.Builder
	builder.WriteString("# Panda Pages story adaptation benchmark\n\n")
	fmt.Fprintf(&builder, "- Benchmark: `%s`\n", document.BenchmarkVersion)
	fmt.Fprintf(&builder, "- Suite: `%s`\n", document.Suite)
	fmt.Fprintf(&builder, "- Started: `%s`\n", document.StartedAt)
	fmt.Fprintf(&builder, "- Finished: `%s`\n", document.FinishedAt)
	fmt.Fprintf(&builder, "- Technical status: **%s**\n", run.Status)
	fmt.Fprintf(&builder, "- Trials: %d attempted, %d complete, %d incomplete\n\n", run.AttemptedTrials, run.CompletedTrials, run.IncompleteTrials)

	builder.WriteString("## Quality summary\n\n")
	fmt.Fprintf(&builder, "- Expectations met: %d / %d complete trials (%s)\n", summary.ExpectationsMet, summary.Trials, percentage(summary.ExpectationsMet, summary.Trials))
	fmt.Fprintf(&builder, "- Result agreement: %d / %d (%s)\n", summary.ResultMatches, summary.Trials, percentage(summary.ResultMatches, summary.Trials))
	fmt.Fprintf(&builder, "- Required findings detected: %d / %d (%s)\n", summary.RequiredFindingsDetected, summary.RequiredFindingsExpected, percentage(summary.RequiredFindingsDetected, summary.RequiredFindingsExpected))
	fmt.Fprintf(&builder, "- Forbidden findings triggered: %d / %d checked (%s)\n\n", summary.ForbiddenFindingsTriggered, summary.ForbiddenFindingsChecked, percentage(summary.ForbiddenFindingsTriggered, summary.ForbiddenFindingsChecked))

	builder.WriteString("## Validator matrix\n\n")
	builder.WriteString("| Config | Model | Reasoning | Max output tokens | Valid assessment artifacts | Retained tokens |\n")
	builder.WriteString("| --- | --- | --- | ---: | ---: | ---: |\n")
	usageByID := make(map[string]ValidatorUsage, len(document.Usage.ByValidator))
	for _, usage := range document.Usage.ByValidator {
		usageByID[usage.ValidatorConfigID] = usage
	}
	for _, config := range run.Validators {
		usage := usageByID[config.ID]
		fmt.Fprintf(
			&builder,
			"| `%s` | `%s` | `%s` | %d | %d | %d |\n",
			config.ID,
			config.Model,
			config.ReasoningEffort,
			config.MaxOutputTokens,
			usage.CompletedResponses,
			usage.Usage.TotalTokens,
		)
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
	fmt.Fprintf(&builder, "- Technically valid assessment artifacts: %d\n", document.Usage.CompletedResponses)
	fmt.Fprintf(&builder, "- Input tokens represented in retained artifacts: %d\n", document.Usage.Usage.InputTokens)
	fmt.Fprintf(&builder, "- Cached input tokens represented in retained artifacts: %d\n", document.Usage.Usage.CachedTokens)
	fmt.Fprintf(&builder, "- Output tokens represented in retained artifacts: %d\n", document.Usage.Usage.OutputTokens)
	fmt.Fprintf(&builder, "- Reasoning tokens represented in retained artifacts: %d\n", document.Usage.Usage.ReasoningTokens)
	fmt.Fprintf(&builder, "- Total tokens represented in retained artifacts: %d\n\n", document.Usage.Usage.TotalTokens)

	builder.WriteString("## Interpretation boundary\n\n")
	builder.WriteString("A semantic benchmark pass means the output is suitable to progress to human editorial review under the current benchmark expectations. It is not publication approval, a publication ticket, or a legal/source-eligibility determination.\n")
	return builder.String(), nil
}

func validateControlledRunForReporting(run ControlledRun) error {
	if run.Status != TrialStatusComplete && run.Status != TrialStatusIncomplete {
		return fmt.Errorf("controlled run status %q is invalid", run.Status)
	}
	if run.ValidationRepetitions < 1 {
		return fmt.Errorf("controlled run validation repetitions must be positive")
	}
	if err := validateValidatorConfigs(run.Validators); err != nil {
		return fmt.Errorf("controlled run validator configuration is invalid: %w", err)
	}
	if run.AttemptedTrials != len(run.Trials) {
		return fmt.Errorf("controlled run attempted trial count does not match trials")
	}

	completed := 0
	incomplete := 0
	for index, trial := range run.Trials {
		switch trial.Status {
		case TrialStatusComplete:
			completed++
		case TrialStatusIncomplete:
			incomplete++
		default:
			return fmt.Errorf("controlled trial %d status %q is invalid", index+1, trial.Status)
		}
	}
	if run.CompletedTrials != completed || run.IncompleteTrials != incomplete {
		return fmt.Errorf("controlled run technical trial counts do not match trials")
	}
	if run.Status == TrialStatusComplete && incomplete != 0 {
		return fmt.Errorf("complete controlled run must not contain incomplete trials")
	}
	if run.QualitySummary.Trials != completed {
		return fmt.Errorf("controlled run quality trial count must equal completed trials")
	}
	return nil
}

func summarizeControlledUsage(run ControlledRun) (UsageSummary, error) {
	configs := make(map[string]ValidatorConfig, len(run.Validators))
	for _, config := range run.Validators {
		if _, exists := configs[config.ID]; exists {
			return UsageSummary{}, fmt.Errorf("controlled run contains duplicate validator config ID %q", config.ID)
		}
		configs[config.ID] = config
	}

	byID := make(map[string]*ValidatorUsage, len(configs))
	for id, config := range configs {
		byID[id] = &ValidatorUsage{
			ValidatorConfigID: id,
			Model:             config.Model,
			ReasoningEffort:   config.ReasoningEffort,
		}
	}

	summary := UsageSummary{}
	for index, trial := range run.Trials {
		config, exists := configs[trial.ValidatorConfigID]
		if !exists {
			return UsageSummary{}, fmt.Errorf("controlled trial %d references unknown validator config %q", index+1, trial.ValidatorConfigID)
		}
		if trial.Status != TrialStatusComplete {
			continue
		}
		if trial.AssessmentArtifact == nil {
			return UsageSummary{}, fmt.Errorf("controlled trial %d is complete without an assessment artifact", index+1)
		}
		artifact := trial.AssessmentArtifact
		if artifact.RequestedModel != config.Model {
			return UsageSummary{}, fmt.Errorf("controlled trial %d requested model does not match validator config", index+1)
		}
		if artifact.ReasoningEffort != config.ReasoningEffort {
			return UsageSummary{}, fmt.Errorf("controlled trial %d reasoning effort does not match validator config", index+1)
		}

		usage := tokenUsageFromResponses(artifact.Usage)
		summary.CompletedResponses++
		addTokenUsage(&summary.Usage, usage)
		validatorUsage := byID[trial.ValidatorConfigID]
		validatorUsage.CompletedResponses++
		addTokenUsage(&validatorUsage.Usage, usage)
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	summary.ByValidator = make([]ValidatorUsage, 0, len(ids))
	for _, id := range ids {
		summary.ByValidator = append(summary.ByValidator, *byID[id])
	}
	return summary, nil
}

func tokenUsageFromResponses(usage storygeneration.ResponsesUsage) TokenUsage {
	return TokenUsage{
		InputTokens:     usage.InputTokens,
		CachedTokens:    usage.CachedTokens,
		OutputTokens:    usage.OutputTokens,
		ReasoningTokens: usage.ReasoningTokens,
		TotalTokens:     usage.TotalTokens,
	}
}

func addTokenUsage(total *TokenUsage, usage TokenUsage) {
	total.InputTokens += usage.InputTokens
	total.CachedTokens += usage.CachedTokens
	total.OutputTokens += usage.OutputTokens
	total.ReasoningTokens += usage.ReasoningTokens
	total.TotalTokens += usage.TotalTokens
}

func percentage(numerator, denominator int) string {
	if denominator == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(numerator)/float64(denominator))
}
