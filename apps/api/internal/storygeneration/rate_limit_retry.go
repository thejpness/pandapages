package storygeneration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	maxOpenAIRateLimitRetries  = 4
	initialRateLimitBackoff    = time.Second
	maximumRateLimitBackoff    = 15 * time.Second
	minimumRetryJitterFactor   = 0.8
	maximumRetryJitterFactor   = 1.2
	maximumProviderRetryJitter = time.Second
)

// RetryWaitFunc allows deterministic tests without making callers sleep. A
// production gateway uses a context-aware timer.
type RetryWaitFunc func(context.Context, time.Duration) error

// RetryJitterFunc returns a value in [0, 1]. Out-of-range values are clamped
// so a custom test hook cannot make retry waits unbounded or immediate.
type RetryJitterFunc func() float64

// RateLimitRetryConfig wraps one shared Responses gateway. It is deliberately
// scoped to a single Create operation rather than orchestration or job state.
type RateLimitRetryConfig struct {
	Gateway ResponsesGateway
	Logger  *slog.Logger
	Wait    RetryWaitFunc
	Jitter  RetryJitterFunc
}

// RateLimitRetryGateway retries only positively identified OpenAI 429s. All
// other provider, decoder, and validation errors are returned unchanged.
type RateLimitRetryGateway struct {
	inner  ResponsesGateway
	logger *slog.Logger
	wait   RetryWaitFunc
	jitter RetryJitterFunc
}

func NewRateLimitRetryGateway(cfg RateLimitRetryConfig) (*RateLimitRetryGateway, error) {
	if cfg.Gateway == nil {
		return nil, fmt.Errorf("Responses gateway is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	wait := cfg.Wait
	if wait == nil {
		wait = waitForRateLimitRetry
	}
	jitter := cfg.Jitter
	if jitter == nil {
		jitter = rand.Float64
	}
	return &RateLimitRetryGateway{
		inner:  cfg.Gateway,
		logger: logger,
		wait:   wait,
		jitter: jitter,
	}, nil
}

func (gateway *RateLimitRetryGateway) Create(ctx context.Context, call ResponsesCall) (ResponsesResult, error) {
	operation := responsesOperationName(call)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return ResponsesResult{}, err
		}
		result, err := gateway.inner.Create(ctx, call)
		if !errors.Is(err, ErrOpenAIRateLimited) {
			if err == nil && attempt > 0 {
				gateway.logger.Info("OpenAI operation recovered after rate limiting",
					"operation", operation,
					"retry_attempts", attempt,
					"outcome", "recovered",
				)
			}
			return result, err
		}
		if attempt == maxOpenAIRateLimitRetries {
			logFields := rateLimitLogFields(err, rateLimitTimingSource(err))
			gateway.logger.Warn("OpenAI rate limit retries exhausted",
				append([]any{
					"operation", operation,
					"attempts", attempt + 1,
					"outcome", "exhausted",
					"category", "rate_limited",
				}, logFields...)...,
			)
			return ResponsesResult{}, err
		}

		retryAttempt := attempt + 1
		wait, timingSource := gateway.rateLimitDelay(err, attempt)
		logFields := rateLimitLogFields(err, timingSource)
		gateway.logger.Warn("OpenAI operation rate limited; waiting before retry",
			append([]any{
				"operation", operation,
				"retry_attempt", retryAttempt,
				"wait_duration", wait,
				"outcome", "waiting",
				"category", "rate_limited",
			}, logFields...)...,
		)
		if waitErr := gateway.wait(ctx, wait); waitErr != nil {
			return ResponsesResult{}, waitErr
		}
		if err := ctx.Err(); err != nil {
			return ResponsesResult{}, err
		}
		gateway.logger.Info("retrying OpenAI operation after rate limit",
			"operation", operation,
			"retry_attempt", retryAttempt,
			"outcome", "retrying",
		)
	}
}

func (gateway *RateLimitRetryGateway) rateLimitDelay(err error, retryIndex int) (time.Duration, string) {
	if delay, source, ok := openAIProviderRateLimitDelay(err); ok {
		// Provider timings are minima. Add only bounded positive jitter so a
		// synchronized retry cannot happen before the provider's stated delay.
		return delay + gateway.providerRateLimitJitter(), source
	}
	backoff := initialRateLimitBackoff
	for index := 0; index < retryIndex && backoff < maximumRateLimitBackoff; index++ {
		backoff *= 2
	}
	if backoff > maximumRateLimitBackoff {
		backoff = maximumRateLimitBackoff
	}
	jitter := boundedRetryJitter(gateway.jitter())
	factor := minimumRetryJitterFactor + (maximumRetryJitterFactor-minimumRetryJitterFactor)*jitter
	return time.Duration(float64(backoff) * factor), "local_backoff"
}

func (gateway *RateLimitRetryGateway) providerRateLimitJitter() time.Duration {
	return time.Duration(float64(maximumProviderRetryJitter) * boundedRetryJitter(gateway.jitter()))
}

func boundedRetryJitter(jitter float64) float64 {
	if jitter < 0 {
		return 0
	}
	if jitter > 1 {
		return 1
	}
	return jitter
}

func rateLimitTimingSource(err error) string {
	if _, source, ok := openAIProviderRateLimitDelay(err); ok {
		return source
	}
	return "local_backoff"
}

func openAIProviderRateLimitDelay(err error) (time.Duration, string, bool) {
	metadata, ok := openAIRateLimitMetadataFor(err)
	if !ok {
		return 0, "", false
	}

	type resetCandidate struct {
		delay  time.Duration
		source string
	}
	allResets := make([]resetCandidate, 0, 3)
	exhaustedResets := make([]resetCandidate, 0, 3)
	for _, dimension := range []struct {
		name  string
		value openAIRateLimitDimension
	}{
		{name: "request_reset", value: metadata.requests},
		{name: "token_reset", value: metadata.tokens},
		{name: "project_token_reset", value: metadata.projectTokens},
	} {
		if !dimension.value.hasReset {
			continue
		}
		candidate := resetCandidate{delay: dimension.value.reset, source: dimension.name}
		allResets = append(allResets, candidate)
		if dimension.value.hasRemaining && dimension.value.remaining == 0 {
			exhaustedResets = append(exhaustedResets, candidate)
		}
	}

	candidates := exhaustedResets
	sourcePrefix := "exhausted"
	if len(candidates) == 0 {
		// A real 429 with no reported zero may still be token-limited by the
		// current request's reservation. Do not guess: wait for the largest
		// available reset instead of choosing one dimension arbitrarily.
		candidates = allResets
		sourcePrefix = "ambiguous"
	}

	var selected time.Duration
	selectedSource := ""
	for _, candidate := range candidates {
		if candidate.delay > selected {
			selected = candidate.delay
			selectedSource = candidate.source
		}
	}
	if len(candidates) > 1 {
		selectedSource = sourcePrefix + "_resets"
	}
	if selected > 0 {
		if metadata.hasRetryAfter {
			if metadata.retryAfter > selected {
				selected = metadata.retryAfter
			}
			return selected, "retry_after_and_" + selectedSource, true
		}
		return selected, selectedSource, true
	}
	if metadata.hasRetryAfter {
		return metadata.retryAfter, "retry_after", true
	}
	return 0, "", false
}

func rateLimitLogFields(err error, timingSource string) []any {
	fields := []any{"timing_source", timingSource}
	metadata, ok := openAIRateLimitMetadataFor(err)
	if !ok {
		return fields
	}
	if metadata.requestID != "" {
		fields = append(fields, "provider_request_id", metadata.requestID)
	}
	if metadata.hasRetryAfter {
		fields = append(fields, "retry_after", metadata.retryAfter)
	}
	fields = appendRateLimitDimensionLogFields(fields, "request", metadata.requests)
	fields = appendRateLimitDimensionLogFields(fields, "token", metadata.tokens)
	return appendRateLimitDimensionLogFields(fields, "project_token", metadata.projectTokens)
}

func appendRateLimitDimensionLogFields(fields []any, prefix string, dimension openAIRateLimitDimension) []any {
	if dimension.hasLimit {
		fields = append(fields, prefix+"_limit", dimension.limit)
	}
	if dimension.hasRemaining {
		fields = append(fields, prefix+"_remaining", dimension.remaining)
	}
	if dimension.hasReset {
		fields = append(fields, prefix+"_reset", dimension.reset)
	}
	return fields
}

func waitForRateLimitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func responsesOperationName(call ResponsesCall) string {
	if version := strings.TrimSpace(string(call.Prompt.Version)); version != "" {
		return version
	}
	return "responses.create"
}
