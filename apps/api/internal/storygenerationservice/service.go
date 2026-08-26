// Package storygenerationservice composes trusted source loading, in-memory
// generation orchestration, and completed-run persistence. It deliberately
// owns no HTTP, authorization, transport, model configuration, or database
// transaction spanning orchestration.
package storygenerationservice

import (
	"context"
	"fmt"

	"pandapages/api/internal/storyorchestration"
)

// SourceVersionLoader returns an already-validated immutable generation input.
type SourceVersionLoader interface {
	LoadGenerationSourceVersionContext(context.Context, string) (storyorchestration.Input, error)
}

// OrchestrationRunner executes the independent in-memory generation flow.
type OrchestrationRunner interface {
	Run(context.Context, storyorchestration.Input) (storyorchestration.Result, error)
}

type stageReportingOrchestrationRunner interface {
	RunWithStageReporter(context.Context, storyorchestration.Input, storyorchestration.StageReporter) (storyorchestration.Result, error)
}

// Runner is the complete application boundary used by HTTP and durable jobs.
type Runner interface {
	Run(context.Context, string) (storyorchestration.PersistedRun, error)
	Generate(context.Context, string, storyorchestration.StageReporter) (storyorchestration.Result, error)
}

// CompletedRunStore retains a fully completed, validated orchestration result.
type CompletedRunStore interface {
	PersistCompletedStoryOrchestrationRunContext(context.Context, string, storyorchestration.Result) (storyorchestration.PersistedRun, error)
}

// Config supplies the high-level application dependencies.
type Config struct {
	SourceLoader SourceVersionLoader
	Orchestrator OrchestrationRunner
	RunStore     CompletedRunStore
}

// Service is the production application seam between trusted source evidence,
// generation orchestration, and durable completed-run evidence.
type Service struct {
	sourceLoader SourceVersionLoader
	orchestrator OrchestrationRunner
	runStore     CompletedRunStore
}

// New constructs a generation service from high-level dependencies.
func New(cfg Config) (*Service, error) {
	if cfg.SourceLoader == nil {
		return nil, fmt.Errorf("generation source loader is required")
	}
	if cfg.Orchestrator == nil {
		return nil, fmt.Errorf("story orchestration runner is required")
	}
	if cfg.RunStore == nil {
		return nil, fmt.Errorf("completed orchestration run store is required")
	}
	return &Service{
		sourceLoader: cfg.SourceLoader,
		orchestrator: cfg.Orchestrator,
		runStore:     cfg.RunStore,
	}, nil
}

// Run loads one trusted promoted source version, runs orchestration outside a
// database transaction, and persists the completed result. Semantic pass,
// needs_review, and fail are all valid completed states and are persisted.
func (service *Service) Run(ctx context.Context, sourceVersionID string) (storyorchestration.PersistedRun, error) {
	result, err := service.Generate(ctx, sourceVersionID, nil)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	persisted, err := service.runStore.PersistCompletedStoryOrchestrationRunContext(ctx, sourceVersionID, result)
	if err != nil {
		return storyorchestration.PersistedRun{}, fmt.Errorf("persist completed story orchestration run: %w", err)
	}
	if persisted.SourceVersionID != sourceVersionID || persisted.Result.SourceIdentity != sourceVersionID {
		return storyorchestration.PersistedRun{}, fmt.Errorf("persisted story orchestration run does not match requested source version")
	}
	return persisted, nil
}

// Generate performs source loading and in-memory orchestration but intentionally
// does not persist a completed run. Durable job workers use it so their final
// job transition and immutable-run persistence can commit atomically.
func (service *Service) Generate(
	ctx context.Context,
	sourceVersionID string,
	report storyorchestration.StageReporter,
) (storyorchestration.Result, error) {
	input, err := service.sourceLoader.LoadGenerationSourceVersionContext(ctx, sourceVersionID)
	if err != nil {
		return storyorchestration.Result{}, fmt.Errorf("load generation source version: %w", err)
	}
	if input.SourceIdentity != sourceVersionID {
		return storyorchestration.Result{}, fmt.Errorf("loaded generation source identity does not match requested source version")
	}

	var result storyorchestration.Result
	if stageRunner, ok := service.orchestrator.(stageReportingOrchestrationRunner); ok {
		result, err = stageRunner.RunWithStageReporter(ctx, input, report)
	} else {
		result, err = service.orchestrator.Run(ctx, input)
	}
	if err != nil {
		return storyorchestration.Result{}, fmt.Errorf("run story orchestration: %w", err)
	}
	if result.SourceIdentity != sourceVersionID {
		return storyorchestration.Result{}, fmt.Errorf("orchestration result source identity does not match requested source version")
	}
	return result, nil
}
