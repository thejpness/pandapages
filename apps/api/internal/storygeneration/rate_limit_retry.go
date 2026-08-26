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
	maxOpenAIRateLimitRetries = 4
	initialRateLimitBackoff   = time.Second
	maximumRateLimitBackoff   = 15 * time.Second
	minimumRetryJitterFactor  = 0.8
	maximumRetryJitterFactor  = 1.2
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
			gateway.logger.Warn("OpenAI rate limit retries exhausted",
				"operation", operation,
				"attempts", attempt+1,
				"outcome", "exhausted",
				"category", "rate_limited",
			)
			return ResponsesResult{}, err
		}

		retryAttempt := attempt + 1
		wait := gateway.rateLimitDelay(err, attempt)
		gateway.logger.Warn("OpenAI operation rate limited; waiting before retry",
			"operation", operation,
			"retry_attempt", retryAttempt,
			"wait_duration", wait,
			"outcome", "waiting",
			"category", "rate_limited",
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

func (gateway *RateLimitRetryGateway) rateLimitDelay(err error, retryIndex int) time.Duration {
	if delay, ok := openAIRetryAfter(err); ok {
		return delay
	}
	backoff := initialRateLimitBackoff
	for index := 0; index < retryIndex && backoff < maximumRateLimitBackoff; index++ {
		backoff *= 2
	}
	if backoff > maximumRateLimitBackoff {
		backoff = maximumRateLimitBackoff
	}
	jitter := gateway.jitter()
	if jitter < 0 {
		jitter = 0
	} else if jitter > 1 {
		jitter = 1
	}
	factor := minimumRetryJitterFactor + (maximumRetryJitterFactor-minimumRetryJitterFactor)*jitter
	return time.Duration(float64(backoff) * factor)
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
