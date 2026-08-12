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
	defaultValidatorModels       = "gpt-5.6-luna,gpt-5.6-terra,gpt-5.6-sol"
	liveResponsesMaxOutputTokens = 128000
)

type cliOptions struct {
	live                  bool
	validationRepetitions int
	models                []string
	reasoningEffort       storygeneration.ReasoningEffort
	maxOutputTokens       int
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
	if err := validateLiveGate(options.live, getenv); err != nil {
		return err
	}
	if err := ensureAPIModuleRoot(); err != nil {
		return err
	}

	fixtures, err := storybenchmark.LoadControlledFixtureSet(controlledFixtureRoot)
	if err != nil {
		return fmt.Errorf("load committed controlled fixtures: %w", err)
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
	plannedRequests := len(fixtures.Cases) * len(validators) * options.validationRepetitions
	fmt.Fprintf(stdout, "Live controlled benchmark: %d paid validator requests across %d cases and %d validator configs.\n", plannedRequests, len(fixtures.Cases), len(validators))

	benchmarkRunner, err := storybenchmark.NewRunner(storybenchmark.RunnerConfig{
		ValidatorFactory: func(config storybenchmark.ValidatorConfig) (storybenchmark.SemanticValidator, error) {
			return storyvalidation.NewRunner(storyvalidation.RunnerConfig{
				Gateway:         client,
				Model:           config.Model,
				ReasoningEffort: config.ReasoningEffort,
				MaxOutputTokens: config.MaxOutputTokens,
			})
		},
	})
	if err != nil {
		return fmt.Errorf("create benchmark runner: %w", err)
	}

	startedAt := time.Now().UTC()
	run, err := benchmarkRunner.RunControlled(ctx, fixtures, storybenchmark.ControlledRunConfig{
		ValidationRepetitions: options.validationRepetitions,
		Validators:            validators,
	})
	finishedAt := time.Now().UTC()
	if err != nil {
		return fmt.Errorf("run controlled live benchmark: %w", err)
	}

	document, err := storybenchmark.BuildControlledResultDocument(startedAt, finishedAt, run)
	if err != nil {
		return fmt.Errorf("build controlled result document: %w", err)
	}
	outputDirectory := filepath.Join("tmp", "storybenchmark", controlledRunID(startedAt))
	written, err := storybenchmark.WriteControlledResultArtifacts(outputDirectory, document)
	if err != nil {
		return fmt.Errorf("write controlled benchmark artifacts: %w", err)
	}

	fmt.Fprintf(stdout, "Result JSON: %s\n", written.ResultJSON)
	fmt.Fprintf(stdout, "Report: %s\n", written.ReportMD)
	fmt.Fprintf(stdout, "Technical status: %s (%d complete, %d incomplete)\n", run.Status, run.CompletedTrials, run.IncompleteTrials)
	fmt.Fprintf(stdout, "Expectations met: %d/%d complete trials\n", run.QualitySummary.ExpectationsMet, run.QualitySummary.Trials)
	fmt.Fprintf(stdout, "Total tokens: %d\n", document.Usage.Usage.TotalTokens)

	if run.Status != storybenchmark.TrialStatusComplete {
		return fmt.Errorf("live benchmark contains %d incomplete technical trials; results were preserved in %s", run.IncompleteTrials, written.Directory)
	}
	return nil
}

func parseOptions(args []string, stderr io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("storybenchmark", flag.ContinueOnError)
	fs.SetOutput(stderr)

	live := fs.Bool("live", false, "allow paid live OpenAI benchmark execution")
	validationRepetitions := fs.Int("validation-repetitions", 1, "number of validator repetitions per controlled case")
	models := fs.String("models", defaultValidatorModels, "comma-separated validator model IDs")
	reasoning := fs.String("reasoning-effort", string(storygeneration.ReasoningEffortMedium), "validator reasoning effort: none, low, medium, high, xhigh, or max")
	maxOutputTokens := fs.Int("max-output-tokens", 8192, "maximum output tokens for each semantic validator response")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if fs.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *validationRepetitions < 1 {
		return cliOptions{}, fmt.Errorf("validation repetitions must be positive")
	}
	if *maxOutputTokens < 1 || *maxOutputTokens > liveResponsesMaxOutputTokens {
		return cliOptions{}, fmt.Errorf("max output tokens must be between 1 and %d", liveResponsesMaxOutputTokens)
	}

	parsedModels, err := parseModelList(*models)
	if err != nil {
		return cliOptions{}, err
	}
	effort, err := parseReasoningEffort(*reasoning)
	if err != nil {
		return cliOptions{}, err
	}
	return cliOptions{
		live:                  *live,
		validationRepetitions: *validationRepetitions,
		models:                parsedModels,
		reasoningEffort:       effort,
		maxOutputTokens:       *maxOutputTokens,
	}, nil
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
	moduleFound := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module pandapages/api" {
			moduleFound = true
			break
		}
	}
	if !moduleFound {
		return fmt.Errorf("run storybenchmark from the Panda Pages apps/api module root")
	}
	manifest := filepath.Join(controlledFixtureRoot, "manifest.json")
	if info, err := os.Stat(manifest); err != nil || info.IsDir() {
		if err != nil {
			return fmt.Errorf("controlled fixture manifest is unavailable: %w", err)
		}
		return fmt.Errorf("controlled fixture manifest path is not a file")
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
