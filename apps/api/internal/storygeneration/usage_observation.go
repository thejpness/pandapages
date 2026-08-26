package storygeneration

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ResponsesOperation is a fixed, non-provider operation identity for one
// billable Responses call. It is never derived from prompt or story content.
type ResponsesOperation string

const (
	ResponsesOperationAnalyseSource            ResponsesOperation = "analyse_source"
	ResponsesOperationGenerateConfidentReaders ResponsesOperation = "generate_confident_readers"
	ResponsesOperationGenerateGrowingReaders   ResponsesOperation = "generate_growing_readers"
	ResponsesOperationGenerateStoryExplorers   ResponsesOperation = "generate_story_explorers"
	ResponsesOperationGenerateLittleListeners  ResponsesOperation = "generate_little_listeners"
	ResponsesOperationValidateConfidentReaders ResponsesOperation = "validate_confident_readers"
	ResponsesOperationValidateGrowingReaders   ResponsesOperation = "validate_growing_readers"
	ResponsesOperationValidateStoryExplorers   ResponsesOperation = "validate_story_explorers"
	ResponsesOperationValidateLittleListeners  ResponsesOperation = "validate_little_listeners"
	ResponsesOperationValidateBundle           ResponsesOperation = "validate_bundle"
)

// ValidResponsesOperation reports whether operation is one of the fixed
// internal generation operations permitted to produce durable usage evidence.
func ValidResponsesOperation(operation ResponsesOperation) bool {
	switch operation {
	case ResponsesOperationAnalyseSource,
		ResponsesOperationGenerateConfidentReaders,
		ResponsesOperationGenerateGrowingReaders,
		ResponsesOperationGenerateStoryExplorers,
		ResponsesOperationGenerateLittleListeners,
		ResponsesOperationValidateConfidentReaders,
		ResponsesOperationValidateGrowingReaders,
		ResponsesOperationValidateStoryExplorers,
		ResponsesOperationValidateLittleListeners,
		ResponsesOperationValidateBundle:
		return true
	default:
		return false
	}
}

// ResponsesUsageObservation is the complete non-secret provider metadata
// required for durable token accounting. It deliberately contains no prompt,
// response output, provider error body, or arbitrary headers.
type ResponsesUsageObservation struct {
	Operation          ResponsesOperation
	ProviderResponseID string
	RequestedModel     string
	ReturnedModel      string
	Usage              ResponsesUsage
}

// RecordedResponsesUsageEvent is one append-only durable accounting event.
// GenerationJobID binds it to the immutable source-version relationship held
// by the durable job; source text and prompt content are never retained here.
type RecordedResponsesUsageEvent struct {
	GenerationJobID string
	ResponsesUsageObservation
	ObservedAt time.Time
}

func (observation ResponsesUsageObservation) Validate() error {
	if !ValidResponsesOperation(observation.Operation) {
		return fmt.Errorf("Responses operation is invalid")
	}
	for _, value := range []struct {
		name string
		data string
	}{
		{name: "provider response ID", data: observation.ProviderResponseID},
		{name: "requested model", data: observation.RequestedModel},
		{name: "returned model", data: observation.ReturnedModel},
	} {
		if strings.TrimSpace(value.data) == "" || value.data != strings.TrimSpace(value.data) || len(value.data) > maxOpenAIRequestIDBytes {
			return fmt.Errorf("Responses usage %s is invalid", value.name)
		}
	}
	if observation.Usage.InputTokens < 0 ||
		observation.Usage.CachedTokens < 0 ||
		observation.Usage.OutputTokens < 0 ||
		observation.Usage.ReasoningTokens < 0 ||
		observation.Usage.TotalTokens < 0 {
		return fmt.Errorf("Responses usage token counts are invalid")
	}
	return nil
}

// ResponsesUsageRecorder durably records one safely observed provider usage
// event. Its implementation is bound to one durable generation job.
type ResponsesUsageRecorder interface {
	RecordResponsesUsage(context.Context, ResponsesUsageObservation) error
}

type responsesUsageRecorderContextKey struct{}

// WithResponsesUsageRecorder binds recorder to one request/job context. The
// recorder is intentionally immutable and scoped to that context tree; no
// package-global mutable state is used for job attribution.
func WithResponsesUsageRecorder(ctx context.Context, recorder ResponsesUsageRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, responsesUsageRecorderContextKey{}, recorder)
}

func recordResponsesUsage(ctx context.Context, observation ResponsesUsageObservation) error {
	recorder, ok := ctx.Value(responsesUsageRecorderContextKey{}).(ResponsesUsageRecorder)
	if !ok || recorder == nil {
		return nil
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	return recorder.RecordResponsesUsage(ctx, observation)
}
