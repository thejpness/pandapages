package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pandapages/api/internal/storybenchmark"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

const (
	liveAcknowledgementEnv       = "PP_ALLOW_LIVE_STORY_BENCHMARK"
	openAIAPIKeyEnv              = "OPENAI_API_KEY"
	controlledFixtureRoot        = "internal/storybenchmark/testdata/controlled"
	publicDomainFixtureRoot      = "internal/storybenchmark/testdata/publicdomain/benjamin-bunny"
	defaultValidatorModels       = "gpt-5.6-luna,gpt-5.6-terra,gpt-5.6-sol"
	liveResponsesMaxOutputTokens = 128000
)

type benchmarkMode string

const (
	modeControlled  benchmarkMode = "controlled"
	modeEndToEnd    benchmarkMode = "end-to-end"
	modeHumanReview benchmarkMode = "human-review"
)

type cliOptions struct {
	mode                    benchmarkMode
	live                    bool
	generationRepetitions   int
	validationRepetitions   int
	models                  []string
	reasoningEffort         storygeneration.ReasoningEffort
	maxOutputTokens         int
	analysisReasoningEffort storygeneration.ReasoningEffort
	analysisMaxOutputTokens int
	editionReasoningEffort  storygeneration.ReasoningEffort
	editionMaxOutputTokens  int
	resultJSON              string
	humanReview             string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runCLI(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "storybenchmark: %v\n", err)
		os.Exit(1)
	}
}

func runCLI(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	options, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	if err := ensureAPIModuleRoot(); err != nil {
		return err
	}
	if options.mode == modeHumanReview {
		return runHumanReviewComparison(options, stdout)
	}
	if err := validateLiveGate(options.live, getenv); err != nil {
		return err
	}

	validators, err := buildValidatorConfigs(options)
	if err != nil {
		return err
	}
	client, err := storygeneration.NewResponsesClient(storygeneration.ResponsesClientConfig{
		APIKey: getenv(openAIAPIKeyEnv),
	})
	if err != nil {
		return fmt.Errorf("create OpenAI Responses client: %w", err)
	}
	recordingClient, err := storybenchmark.NewRecordingResponsesGateway(client)
	if err != nil {
		return fmt.Errorf("create benchmark Responses telemetry gateway: %w", err)
	}

	switch options.mode {
	case modeControlled:
		return runControlled(ctx, options, validators, recordingClient, stdout)
	case modeEndToEnd:
		return runEndToEnd(ctx, options, validators, recordingClient, stdout)
	default:
		return fmt.Errorf("unsupported benchmark mode %q", options.mode)
	}
}

func runControlled(
	ctx context.Context,
	options cliOptions,
	validators []storybenchmark.ValidatorConfig,
	client *storybenchmark.RecordingResponsesGateway,
	stdout io.Writer,
) error {
	fixtures, err := storybenchmark.LoadControlledFixtureSet(controlledFixtureRoot)
	if err != nil {
		return fmt.Errorf("load committed controlled fixtures: %w", err)
	}
	startedAt := time.Now().UTC()
	outputDirectory := filepath.Join("tmp", "storybenchmark", controlledRunID(startedAt))
	if err := preflightBenchmarkOutputDirectory(outputDirectory); err != nil {
		return fmt.Errorf("preflight controlled benchmark output: %w", err)
	}

	plannedRequests := len(fixtures.Cases) * len(validators) * options.validationRepetitions
	fmt.Fprintf(stdout, "Live controlled benchmark: %d paid validator requests across %d cases and %d validator configs.\n", plannedRequests, len(fixtures.Cases), len(validators))

	benchmarkRunner, err := storybenchmark.NewRunner(storybenchmark.RunnerConfig{
		ValidatorFactory: validatorFactory(client),
	})
	if err != nil {
		return fmt.Errorf("create benchmark runner: %w", err)
	}

	run, err := benchmarkRunner.RunControlled(ctx, fixtures, storybenchmark.ControlledRunConfig{
		ValidationRepetitions: options.validationRepetitions,
		Validators:            validators,
	})
	finishedAt := time.Now().UTC()
	if err != nil {
		return fmt.Errorf("run controlled live benchmark: %w", err)
	}

	document, err := storybenchmark.BuildControlledResultDocument(startedAt, finishedAt, run, client.Snapshot())
	if err != nil {
		return fmt.Errorf("build controlled result document: %w", err)
	}
	written, err := storybenchmark.WriteControlledResultArtifacts(outputDirectory, document)
	if err != nil {
		return fmt.Errorf("write controlled benchmark artifacts: %w", err)
	}

	fmt.Fprintf(stdout, "Result JSON: %s\n", written.ResultJSON)
	fmt.Fprintf(stdout, "Report: %s\n", written.ReportMD)
	fmt.Fprintf(stdout, "Technical status: %s (%d complete, %d incomplete)\n", run.Status, run.CompletedTrials, run.IncompleteTrials)
	fmt.Fprintf(stdout, "Expectations met: %d/%d complete trials\n", run.QualitySummary.ExpectationsMet, run.QualitySummary.Trials)
	fmt.Fprintf(stdout, "Responses API: %d attempted, %d successful, %d failed\n", document.ResponsesAPI.AttemptedRequests, document.ResponsesAPI.SuccessfulResponses, document.ResponsesAPI.FailedRequests)
	fmt.Fprintf(stdout, "Responses API total tokens: %d\n", document.ResponsesAPI.Usage.TotalTokens)
	fmt.Fprintf(stdout, "Retained-artifact total tokens: %d\n", document.Usage.Usage.TotalTokens)

	if run.Status != storybenchmark.TrialStatusComplete {
		return fmt.Errorf("live benchmark contains %d incomplete technical trials; results were preserved in %s", run.IncompleteTrials, written.Directory)
	}
	return nil
}

func runEndToEnd(
	ctx context.Context,
	options cliOptions,
	validators []storybenchmark.ValidatorConfig,
	client *storybenchmark.RecordingResponsesGateway,
	stdout io.Writer,
) error {
	fixture, err := storybenchmark.LoadPublicDomainFixture(publicDomainFixtureRoot)
	if err != nil {
		return fmt.Errorf("load reviewed public-domain benchmark fixture: %w", err)
	}
	generator, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
		Gateway:                 client,
		AnalysisReasoningEffort: options.analysisReasoningEffort,
		AnalysisMaxOutputTokens: options.analysisMaxOutputTokens,
		EditionReasoningEffort:  options.editionReasoningEffort,
		EditionMaxOutputTokens:  options.editionMaxOutputTokens,
	})
	if err != nil {
		return fmt.Errorf("create v2 generation runner: %w", err)
	}
	benchmarkRunner, err := storybenchmark.NewRunner(storybenchmark.RunnerConfig{
		Generator:        generator,
		ValidatorFactory: validatorFactory(client),
	})
	if err != nil {
		return fmt.Errorf("create benchmark runner: %w", err)
	}

	startedAt := time.Now().UTC()
	outputDirectory := filepath.Join("tmp", "storybenchmark", endToEndRunID(startedAt))
	if err := preflightBenchmarkOutputDirectory(outputDirectory); err != nil {
		return fmt.Errorf("preflight end-to-end benchmark output: %w", err)
	}

	generationCallsPerRepetition := 1 + len(storygeneration.DerivedEditionKeysV2())
	validationCallsPerRepetition := (len(storygeneration.DerivedEditionKeysV2()) + 1) * len(validators) * options.validationRepetitions
	plannedRequests := options.generationRepetitions * (generationCallsPerRepetition + validationCallsPerRepetition)
	fmt.Fprintf(
		stdout,
		"Live end-to-end benchmark: %d paid requests for reviewed source %s/%s (%d generation repetitions, %d validator configs).\n",
		plannedRequests,
		fixture.Provider,
		fixture.ExternalID,
		options.generationRepetitions,
		len(validators),
	)
	fmt.Fprintf(stdout, "Generation model is fixed by PR92 to %s.\n", storygeneration.GenerationModelV2)

	run, err := benchmarkRunner.RunEndToEnd(ctx, fixture.Source, storybenchmark.EndToEndRunConfig{
		GenerationRepetitions: options.generationRepetitions,
		ValidationRepetitions: options.validationRepetitions,
		Validators:            validators,
	})
	finishedAt := time.Now().UTC()
	if err != nil {
		return fmt.Errorf("run end-to-end live benchmark: %w", err)
	}

	document, err := storybenchmark.BuildEndToEndResultDocument(startedAt, finishedAt, fixture, storybenchmark.EndToEndGenerationConfig{
		Model:                   storygeneration.GenerationModelV2,
		AnalysisReasoningEffort: options.analysisReasoningEffort,
		AnalysisMaxOutputTokens: options.analysisMaxOutputTokens,
		EditionReasoningEffort:  options.editionReasoningEffort,
		EditionMaxOutputTokens:  options.editionMaxOutputTokens,
	}, run, client.Snapshot())
	if err != nil {
		return fmt.Errorf("build end-to-end result document: %w", err)
	}
	written, err := storybenchmark.WriteEndToEndResultArtifacts(outputDirectory, document)
	if err != nil {
		return fmt.Errorf("write end-to-end benchmark artifacts: %w", err)
	}
	fmt.Fprintf(stdout, "Result JSON: %s\n", written.ResultJSON)
	fmt.Fprintf(stdout, "Report: %s\n", written.ReportMD)
	fmt.Fprintf(stdout, "Human-review template: %s\n", written.HumanReviewTemplate)
	fmt.Fprintf(stdout, "Technical status: %s\n", run.Status)
	fmt.Fprintf(stdout, "Responses API: %d attempted, %d successful, %d failed\n", document.ResponsesAPI.AttemptedRequests, document.ResponsesAPI.SuccessfulResponses, document.ResponsesAPI.FailedRequests)
	fmt.Fprintf(stdout, "Responses API total tokens: %d\n", document.ResponsesAPI.Usage.TotalTokens)
	fmt.Fprintf(stdout, "Retained-artifact total tokens: %d\n", document.Usage.Usage.TotalTokens)

	if run.Status != storybenchmark.TrialStatusComplete {
		return fmt.Errorf("live end-to-end benchmark contains incomplete technical work; results were preserved in %s", written.Directory)
	}
	return nil
}

func runHumanReviewComparison(options cliOptions, stdout io.Writer) error {
	result, err := storybenchmark.LoadEndToEndResultDocument(options.resultJSON)
	if err != nil {
		return err
	}
	review, err := storybenchmark.LoadHumanReviewDocument(options.humanReview)
	if err != nil {
		return err
	}
	score, err := storybenchmark.ScoreHumanReview(result, review)
	if err != nil {
		return err
	}
	resultDirectory := filepath.Dir(options.resultJSON)
	written, err := storybenchmark.WriteHumanReviewScoreArtifacts(resultDirectory, score)
	if err != nil {
		return fmt.Errorf("write human-review comparison artifacts: %w", err)
	}
	fmt.Fprintf(stdout, "Human-review score JSON: %s\n", written.ScoreJSON)
	fmt.Fprintf(stdout, "Human-review report: %s\n", written.ReportMD)
	fmt.Fprintf(stdout, "Full agreement: %d/%d validation trials\n", score.Summary.Agreements, score.Summary.Trials)
	return nil
}

func validatorFactory(client storygeneration.ResponsesGateway) storybenchmark.ValidatorFactory {
	return func(config storybenchmark.ValidatorConfig) (storybenchmark.SemanticValidator, error) {
		return storyvalidation.NewRunner(storyvalidation.RunnerConfig{
			Gateway:         client,
			Model:           config.Model,
			ReasoningEffort: config.ReasoningEffort,
			MaxOutputTokens: config.MaxOutputTokens,
		})
	}
}

func parseOptions(args []string, stderr io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("storybenchmark", flag.ContinueOnError)
	fs.SetOutput(stderr)

	mode := fs.String("mode", string(modeControlled), "benchmark mode: controlled, end-to-end, or human-review")
	live := fs.Bool("live", false, "allow paid live OpenAI benchmark execution")
	generationRepetitions := fs.Int("generation-repetitions", 1, "number of independent generation repetitions for end-to-end mode")
	validationRepetitions := fs.Int("validation-repetitions", 1, "number of validator repetitions per benchmark target")
	models := fs.String("models", defaultValidatorModels, "comma-separated validator model IDs")
	reasoning := fs.String("reasoning-effort", string(storygeneration.ReasoningEffortMedium), "validator reasoning effort: none, low, medium, high, xhigh, or max")
	maxOutputTokens := fs.Int("max-output-tokens", 8192, "maximum output tokens for each semantic validator response")
	analysisReasoning := fs.String("analysis-reasoning-effort", string(storygeneration.ReasoningEffortMedium), "end-to-end source-analysis reasoning effort")
	analysisMaxOutputTokens := fs.Int("analysis-max-output-tokens", 16384, "maximum output tokens for each end-to-end source-analysis response")
	editionReasoning := fs.String("edition-reasoning-effort", string(storygeneration.ReasoningEffortMedium), "end-to-end edition-generation reasoning effort")
	editionMaxOutputTokens := fs.Int("edition-max-output-tokens", 32768, "maximum output tokens for each end-to-end edition-generation response")
	resultJSON := fs.String("result-json", "", "end-to-end result.json path for offline human-review scoring")
	humanReview := fs.String("human-review", "", "completed SHA-bound human-review JSON path for offline scoring")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if fs.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	parsedMode := benchmarkMode(strings.TrimSpace(*mode))
	if parsedMode != modeControlled && parsedMode != modeEndToEnd && parsedMode != modeHumanReview {
		return cliOptions{}, fmt.Errorf("unsupported benchmark mode %q", *mode)
	}
	if *generationRepetitions < 1 || *validationRepetitions < 1 {
		return cliOptions{}, fmt.Errorf("generation and validation repetitions must be positive")
	}
	for label, value := range map[string]int{
		"validator max output tokens": *maxOutputTokens,
		"analysis max output tokens":  *analysisMaxOutputTokens,
		"edition max output tokens":   *editionMaxOutputTokens,
	} {
		if value < 1 || value > liveResponsesMaxOutputTokens {
			return cliOptions{}, fmt.Errorf("%s must be between 1 and %d", label, liveResponsesMaxOutputTokens)
		}
	}

	parsedModels, err := parseModelList(*models)
	if err != nil {
		return cliOptions{}, err
	}
	validatorEffort, err := parseReasoningEffort(*reasoning)
	if err != nil {
		return cliOptions{}, err
	}
	analysisEffort, err := parseReasoningEffort(*analysisReasoning)
	if err != nil {
		return cliOptions{}, fmt.Errorf("analysis %w", err)
	}
	editionEffort, err := parseReasoningEffort(*editionReasoning)
	if err != nil {
		return cliOptions{}, fmt.Errorf("edition %w", err)
	}

	options := cliOptions{
		mode:                    parsedMode,
		live:                    *live,
		generationRepetitions:   *generationRepetitions,
		validationRepetitions:   *validationRepetitions,
		models:                  parsedModels,
		reasoningEffort:         validatorEffort,
		maxOutputTokens:         *maxOutputTokens,
		analysisReasoningEffort: analysisEffort,
		analysisMaxOutputTokens: *analysisMaxOutputTokens,
		editionReasoningEffort:  editionEffort,
		editionMaxOutputTokens:  *editionMaxOutputTokens,
		resultJSON:              strings.TrimSpace(*resultJSON),
		humanReview:             strings.TrimSpace(*humanReview),
	}
	if parsedMode == modeHumanReview {
		if options.live {
			return cliOptions{}, fmt.Errorf("human-review mode is offline and must not use --live")
		}
		if options.resultJSON == "" || options.humanReview == "" {
			return cliOptions{}, fmt.Errorf("human-review mode requires --result-json and --human-review")
		}
	} else if options.resultJSON != "" || options.humanReview != "" {
		return cliOptions{}, fmt.Errorf("--result-json and --human-review are only valid in human-review mode")
	}
	return options, nil
}

func validateLiveGate(live bool, getenv func(string) string) error {
	if !live {
		return fmt.Errorf("live benchmark execution requires --live")
	}
	if strings.TrimSpace(getenv(liveAcknowledgementEnv)) != "1" {
		return fmt.Errorf("live benchmark execution requires %s=1", liveAcknowledgementEnv)
	}
	if strings.TrimSpace(getenv(openAIAPIKeyEnv)) == "" {
		return fmt.Errorf("live benchmark execution requires %s", openAIAPIKeyEnv)
	}
	if strings.TrimSpace(getenv("CI")) != "" {
		return fmt.Errorf("live benchmark execution is disabled when CI is set")
	}
	return nil
}

func ensureAPIModuleRoot() error {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return fmt.Errorf("run storybenchmark from apps/api: read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module pandapages/api" {
			return nil
		}
	}
	return fmt.Errorf("run storybenchmark from the Panda Pages apps/api module root")
}

func preflightBenchmarkOutputDirectory(directory string) error {
	if filepath.Clean(directory) == "." || strings.TrimSpace(directory) == "" {
		return fmt.Errorf("benchmark output directory is required")
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("prepare benchmark output parent: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return fmt.Errorf("reserve benchmark output directory: %w", err)
	}
	if err := os.Remove(directory); err != nil {
		return fmt.Errorf("release benchmark output reservation: %w", err)
	}
	return nil
}

func parseModelList(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	models := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, raw := range parts {
		model := strings.TrimSpace(raw)
		if model == "" {
			return nil, fmt.Errorf("validator model list contains an empty model ID")
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("validator model %q is duplicated", model)
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("at least one validator model is required")
	}
	return models, nil
}

func parseReasoningEffort(value string) (storygeneration.ReasoningEffort, error) {
	effort := storygeneration.ReasoningEffort(strings.TrimSpace(value))
	switch effort {
	case storygeneration.ReasoningEffortNone,
		storygeneration.ReasoningEffortLow,
		storygeneration.ReasoningEffortMedium,
		storygeneration.ReasoningEffortHigh,
		storygeneration.ReasoningEffortXHigh,
		storygeneration.ReasoningEffortMax:
		return effort, nil
	default:
		return "", fmt.Errorf("unsupported reasoning effort %q", value)
	}
}

func buildValidatorConfigs(options cliOptions) ([]storybenchmark.ValidatorConfig, error) {
	configs := make([]storybenchmark.ValidatorConfig, 0, len(options.models))
	seenIDs := make(map[string]struct{}, len(options.models))
	for _, model := range options.models {
		id := validatorConfigID(model, options.reasoningEffort)
		if id == "" {
			return nil, fmt.Errorf("cannot derive validator config ID from model %q", model)
		}
		if _, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("validator models produce duplicate config ID %q", id)
		}
		seenIDs[id] = struct{}{}
		configs = append(configs, storybenchmark.ValidatorConfig{
			ID:              id,
			Model:           model,
			ReasoningEffort: options.reasoningEffort,
			MaxOutputTokens: options.maxOutputTokens,
		})
	}
	return configs, nil
}

func validatorConfigID(model string, effort storygeneration.ReasoningEffort) string {
	var builder strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(model)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if builder.Len() != 0 && !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	base := strings.Trim(builder.String(), "-")
	if base == "" {
		return ""
	}
	return base + "-" + string(effort)
}

func controlledRunID(startedAt time.Time) string {
	utc := startedAt.UTC()
	return fmt.Sprintf("controlled-%s-%09d", utc.Format("20060102T150405Z"), utc.Nanosecond())
}

func endToEndRunID(startedAt time.Time) string {
	utc := startedAt.UTC()
	return fmt.Sprintf("end-to-end-%s-%09d", utc.Format("20060102T150405Z"), utc.Nanosecond())
}
