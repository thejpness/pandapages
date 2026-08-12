package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/storygeneration"
)

func TestParseOptionsDefaults(t *testing.T) {
	options, err := parseOptions([]string{"--live"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if options.mode != modeControlled || !options.live || options.generationRepetitions != 1 || options.validationRepetitions != 1 || options.maxOutputTokens != 8192 {
		t.Fatalf("options = %#v", options)
	}
	if options.reasoningEffort != storygeneration.ReasoningEffortMedium ||
		options.analysisReasoningEffort != storygeneration.ReasoningEffortMedium ||
		options.editionReasoningEffort != storygeneration.ReasoningEffortMedium {
		t.Fatalf("reasoning efforts = %#v", options)
	}
	if options.analysisMaxOutputTokens != 16384 || options.editionMaxOutputTokens != 32768 {
		t.Fatalf("generation token budgets = %#v", options)
	}
	wantModels := []string{"gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"}
	if strings.Join(options.models, ",") != strings.Join(wantModels, ",") {
		t.Fatalf("models = %#v", options.models)
	}
}

func TestParseOptionsEndToEnd(t *testing.T) {
	options, err := parseOptions([]string{
		"--mode=end-to-end",
		"--live",
		"--generation-repetitions=2",
		"--validation-repetitions=3",
		"--analysis-reasoning-effort=high",
		"--edition-reasoning-effort=low",
		"--analysis-max-output-tokens=12000",
		"--edition-max-output-tokens=24000",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if options.mode != modeEndToEnd || options.generationRepetitions != 2 || options.validationRepetitions != 3 {
		t.Fatalf("options = %#v", options)
	}
	if options.analysisReasoningEffort != storygeneration.ReasoningEffortHigh || options.editionReasoningEffort != storygeneration.ReasoningEffortLow {
		t.Fatalf("generation efforts = %#v", options)
	}
}

func TestParseOptionsHumanReviewIsOffline(t *testing.T) {
	options, err := parseOptions([]string{
		"--mode=human-review",
		"--result-json=tmp/storybenchmark/end-to-end-x/result.json",
		"--human-review=tmp/storybenchmark/end-to-end-x/human-review.json",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if options.live || options.mode != modeHumanReview {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseOptionsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "bad mode", args: []string{"--mode=magic"}, want: "unsupported benchmark mode"},
		{name: "zero generation repetitions", args: []string{"--generation-repetitions=0"}, want: "repetitions must be positive"},
		{name: "zero validation repetitions", args: []string{"--validation-repetitions=0"}, want: "repetitions must be positive"},
		{name: "too many validator output tokens", args: []string{"--max-output-tokens=128001"}, want: "between 1 and 128000"},
		{name: "too many analysis output tokens", args: []string{"--analysis-max-output-tokens=128001"}, want: "between 1 and 128000"},
		{name: "empty model", args: []string{"--models=gpt-5.6-luna,"}, want: "empty model ID"},
		{name: "duplicate model", args: []string{"--models=gpt-5.6-luna,gpt-5.6-luna"}, want: "duplicated"},
		{name: "bad validator effort", args: []string{"--reasoning-effort=ultra"}, want: "unsupported reasoning effort"},
		{name: "bad analysis effort", args: []string{"--analysis-reasoning-effort=ultra"}, want: "analysis unsupported reasoning effort"},
		{name: "human review live", args: []string{"--mode=human-review", "--live", "--result-json=x", "--human-review=y"}, want: "offline"},
		{name: "human review missing files", args: []string{"--mode=human-review"}, want: "requires --result-json and --human-review"},
		{name: "review args in live mode", args: []string{"--result-json=x"}, want: "only valid in human-review mode"},
		{name: "positional argument", args: []string{"surprise"}, want: "unexpected positional arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseOptions(test.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseOptions() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateLiveGate(t *testing.T) {
	env := map[string]string{}
	getenv := func(key string) string { return env[key] }

	if err := validateLiveGate(false, getenv); err == nil || !strings.Contains(err.Error(), "--live") {
		t.Fatalf("missing --live error = %v", err)
	}
	if err := validateLiveGate(true, getenv); err == nil || !strings.Contains(err.Error(), liveAcknowledgementEnv) {
		t.Fatalf("missing acknowledgement error = %v", err)
	}
	env[liveAcknowledgementEnv] = "1"
	if err := validateLiveGate(true, getenv); err == nil || !strings.Contains(err.Error(), openAIAPIKeyEnv) {
		t.Fatalf("missing API key error = %v", err)
	}
	env[openAIAPIKeyEnv] = "test-key-never-sent"
	env["CI"] = "true"
	if err := validateLiveGate(true, getenv); err == nil || !strings.Contains(err.Error(), "disabled when CI is set") {
		t.Fatalf("CI error = %v", err)
	}
	delete(env, "CI")
	if err := validateLiveGate(true, getenv); err != nil {
		t.Fatalf("valid live gate error = %v", err)
	}
}

func TestBuildValidatorConfigsUsesStableSafeIDs(t *testing.T) {
	configs, err := buildValidatorConfigs(cliOptions{
		models:          []string{"gpt-5.6-luna", "vendor/model.preview"},
		reasoningEffort: storygeneration.ReasoningEffortHigh,
		maxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatalf("buildValidatorConfigs() error = %v", err)
	}
	if len(configs) != 2 || configs[0].ID != "gpt-5-6-luna-high" || configs[1].ID != "vendor-model-preview-high" {
		t.Fatalf("configs = %#v", configs)
	}
}

func TestRunIDsAreUTCAndPrecise(t *testing.T) {
	value := time.Date(2026, 8, 12, 23, 45, 6, 123456789, time.FixedZone("BST", 3600))
	if got, want := controlledRunID(value), "controlled-20260812T224506Z-123456789"; got != want {
		t.Fatalf("controlledRunID() = %q, want %q", got, want)
	}
	if got, want := endToEndRunID(value), "end-to-end-20260812T224506Z-123456789"; got != want {
		t.Fatalf("endToEndRunID() = %q, want %q", got, want)
	}
}

func TestPreflightBenchmarkOutputDirectoryCreatesAndReleasesReservation(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "tmp", "storybenchmark", "controlled-test")
	if err := preflightBenchmarkOutputDirectory(directory); err != nil {
		t.Fatalf("preflightBenchmarkOutputDirectory() error = %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("reserved directory still exists after preflight: %v", err)
	}
	parent := filepath.Dir(directory)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("output parent stat error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("output parent is not a directory: %s", parent)
	}
}

func TestPreflightBenchmarkOutputDirectoryFailsBeforeLiveWorkWhenParentCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "tmp")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(blocker, "storybenchmark", "controlled-test")
	err := preflightBenchmarkOutputDirectory(directory)
	if err == nil || !strings.Contains(err.Error(), "prepare benchmark output parent") {
		t.Fatalf("preflightBenchmarkOutputDirectory() error = %v, want parent failure", err)
	}
}

func TestEnsureAPIModuleRoot(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(original) }()

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("go.mod", []byte("module pandapages/api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureAPIModuleRoot(); err != nil {
		t.Fatalf("ensureAPIModuleRoot() error = %v", err)
	}
}
