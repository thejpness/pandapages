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
	if !options.live || options.validationRepetitions != 1 || options.maxOutputTokens != 8192 {
		t.Fatalf("options = %#v", options)
	}
	if options.reasoningEffort != storygeneration.ReasoningEffortMedium {
		t.Fatalf("reasoning effort = %q", options.reasoningEffort)
	}
	wantModels := []string{"gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"}
	if strings.Join(options.models, ",") != strings.Join(wantModels, ",") {
		t.Fatalf("models = %#v", options.models)
	}
}

func TestParseOptionsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "zero repetitions", args: []string{"--validation-repetitions=0"}, want: "repetitions must be positive"},
		{name: "too many output tokens", args: []string{"--max-output-tokens=128001"}, want: "between 1 and 128000"},
		{name: "empty model", args: []string{"--models=gpt-5.6-luna,"}, want: "empty model ID"},
		{name: "duplicate model", args: []string{"--models=gpt-5.6-luna,gpt-5.6-luna"}, want: "duplicated"},
		{name: "bad effort", args: []string{"--reasoning-effort=ultra"}, want: "unsupported reasoning effort"},
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

func TestControlledRunIDIsUTCAndPrecise(t *testing.T) {
	value := time.Date(2026, 8, 12, 23, 45, 6, 123456789, time.FixedZone("BST", 3600))
	if got, want := controlledRunID(value), "controlled-20260812T224506Z-123456789"; got != want {
		t.Fatalf("controlledRunID() = %q, want %q", got, want)
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
	manifestDir := filepath.Join(root, controlledFixtureRoot)
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureAPIModuleRoot(); err != nil {
		t.Fatalf("ensureAPIModuleRoot() error = %v", err)
	}
}
