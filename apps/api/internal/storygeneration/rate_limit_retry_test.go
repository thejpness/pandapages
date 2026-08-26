package storygeneration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type retrySequenceGateway struct {
	calls  []ResponsesCall
	errors []error
	result ResponsesResult
}

func (gateway *retrySequenceGateway) Create(_ context.Context, call ResponsesCall) (ResponsesResult, error) {
	index := len(gateway.calls)
	gateway.calls = append(gateway.calls, call)
	if index < len(gateway.errors) && gateway.errors[index] != nil {
		return ResponsesResult{}, gateway.errors[index]
	}
	return gateway.result, nil
}

func newTestRateLimitRetryGateway(
	t *testing.T,
	inner ResponsesGateway,
	wait RetryWaitFunc,
	jitter RetryJitterFunc,
) *RateLimitRetryGateway {
	t.Helper()
	gateway, err := NewRateLimitRetryGateway(RateLimitRetryConfig{
		Gateway: inner,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Wait:    wait,
		Jitter:  jitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func retrySuccessResult() ResponsesResult {
	return ResponsesResult{ResponseID: "resp-retry", Model: GenerationModelV2, OutputText: "ok"}
}

func rateLimitHeaders(values map[string]string) http.Header {
	headers := make(http.Header, len(values))
	for name, value := range values {
		headers.Set(name, value)
	}
	return headers
}

func TestNewRateLimitRetryGatewayRequiresGateway(t *testing.T) {
	if _, err := NewRateLimitRetryGateway(RateLimitRetryConfig{}); err == nil {
		t.Fatal("NewRateLimitRetryGateway() unexpectedly succeeded")
	}
}

func TestRateLimitRetryGatewaySuccessDoesNotRetryOrWait(t *testing.T) {
	inner := &retrySequenceGateway{result: retrySuccessResult()}
	var waits []time.Duration
	gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}, func() float64 { return 0.5 })

	result, err := gateway.Create(context.Background(), validResponsesCall())
	if err != nil || result != inner.result || len(inner.calls) != 1 || len(waits) != 0 {
		t.Fatalf("result/error/calls/waits = %#v / %v / %d / %v", result, err, len(inner.calls), waits)
	}
}

func TestRateLimitRetryGatewayRetriesOnlyRateLimits(t *testing.T) {
	t.Run("one rate limit then success", func(t *testing.T) {
		inner := &retrySequenceGateway{errors: []error{ErrOpenAIRateLimited}, result: retrySuccessResult()}
		var waits []time.Duration
		gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}, func() float64 { return 0.5 })

		result, err := gateway.Create(context.Background(), validResponsesCall())
		if err != nil || result != inner.result || len(inner.calls) != 2 || len(waits) != 1 || waits[0] != time.Second {
			t.Fatalf("result/error/calls/waits = %#v / %v / %d / %v", result, err, len(inner.calls), waits)
		}
	})

	t.Run("multiple rate limits then success", func(t *testing.T) {
		inner := &retrySequenceGateway{errors: []error{ErrOpenAIRateLimited, ErrOpenAIRateLimited, ErrOpenAIRateLimited}, result: retrySuccessResult()}
		var waits []time.Duration
		gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}, func() float64 { return 0.5 })

		if _, err := gateway.Create(context.Background(), validResponsesCall()); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
		if len(inner.calls) != 4 || len(waits) != len(want) {
			t.Fatalf("calls/waits = %d/%v, want 4/%v", len(inner.calls), waits, want)
		}
		for index := range want {
			if waits[index] != want[index] {
				t.Fatalf("wait %d = %v, want %v", index, waits[index], want[index])
			}
		}
	})

	t.Run("retry budget exhausted", func(t *testing.T) {
		inner := &retrySequenceGateway{errors: []error{
			ErrOpenAIRateLimited,
			ErrOpenAIRateLimited,
			ErrOpenAIRateLimited,
			ErrOpenAIRateLimited,
			ErrOpenAIRateLimited,
		}}
		var waits []time.Duration
		gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}, func() float64 { return 0.5 })

		_, err := gateway.Create(context.Background(), validResponsesCall())
		if !errors.Is(err, ErrOpenAIRateLimited) || len(inner.calls) != maxOpenAIRateLimitRetries+1 || len(waits) != maxOpenAIRateLimitRetries {
			t.Fatalf("error/calls/waits = %v / %d / %v", err, len(inner.calls), waits)
		}
	})

	for _, err := range []error{
		ErrOpenAIUnauthorized,
		ErrOpenAIUnavailable,
		ErrOpenAIQuotaExceeded,
		ErrOpenAIResponseInvalid,
		ErrOpenAIResponseIncomplete,
		ErrOpenAIResponseRefused,
		errors.New("local schema validation failed"),
	} {
		t.Run(err.Error(), func(t *testing.T) {
			inner := &retrySequenceGateway{errors: []error{err}}
			waits := 0
			gateway := newTestRateLimitRetryGateway(t, inner, func(context.Context, time.Duration) error {
				waits++
				return nil
			}, func() float64 { return 0.5 })
			_, got := gateway.Create(context.Background(), validResponsesCall())
			if !errors.Is(got, err) || len(inner.calls) != 1 || waits != 0 {
				t.Fatalf("error/calls/waits = %v / %d / %d", got, len(inner.calls), waits)
			}
		})
	}
}

func TestRateLimitRetryGatewayHonoursBoundedRetryAfterAndDeterministicJitter(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		rateError error
		jitter    float64
		wantWait  time.Duration
	}{
		{name: "valid retry after", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "30"}), now), jitter: 0, wantWait: 30 * time.Second},
		{name: "twenty minute retry after remains intact", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "1200"}), now), jitter: 0, wantWait: 20 * time.Minute},
		{name: "valid retry after HTTP date", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": now.Add(30 * time.Second).Format(httpTimeFormat)}), now), jitter: 0, wantWait: 30 * time.Second},
		{name: "oversized numeric retry after is clamped", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "10800"}), now), jitter: 0, wantWait: maxOpenAIRateLimitDelay},
		{name: "oversized HTTP date retry after is clamped", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": now.Add(3 * time.Hour).Format(httpTimeFormat)}), now), jitter: 0, wantWait: maxOpenAIRateLimitDelay},
		{name: "zero retry after falls back", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "0"}), now), jitter: 0.5, wantWait: time.Second},
		{name: "malformed retry after falls back", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "not a delay"}), now), jitter: 0.5, wantWait: time.Second},
		{name: "past HTTP date retry after falls back", rateError: newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": now.Add(-time.Second).Format(httpTimeFormat)}), now), jitter: 0.5, wantWait: time.Second},
		{name: "jitter lower bound", rateError: ErrOpenAIRateLimited, jitter: 0, wantWait: 800 * time.Millisecond},
		{name: "jitter upper bound", rateError: ErrOpenAIRateLimited, jitter: 1, wantWait: 1200 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			inner := &retrySequenceGateway{errors: []error{test.rateError}, result: retrySuccessResult()}
			var waits []time.Duration
			gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, delay time.Duration) error {
				waits = append(waits, delay)
				return nil
			}, func() float64 { return test.jitter })
			if _, err := gateway.Create(context.Background(), validResponsesCall()); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if len(waits) != 1 || waits[0] != test.wantWait {
				t.Fatalf("waits = %v, want [%v]", waits, test.wantWait)
			}
		})
	}
}

func TestRateLimitRetryGatewayUsesProviderRateLimitTiming(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		headers    map[string]string
		wantDelay  time.Duration
		wantSource string
	}{
		{
			name:       "retry after alone",
			headers:    map[string]string{"Retry-After": "7"},
			wantDelay:  7 * time.Second,
			wantSource: "retry_after",
		},
		{
			name: "request exhaustion",
			headers: map[string]string{
				"X-RateLimit-Remaining-Requests": "0",
				"X-RateLimit-Reset-Requests":     "3s",
				"X-RateLimit-Remaining-Tokens":   "50",
				"X-RateLimit-Reset-Tokens":       "6m0s",
			},
			wantDelay:  3 * time.Second,
			wantSource: "request_reset",
		},
		{
			name: "token exhaustion",
			headers: map[string]string{
				"X-RateLimit-Remaining-Requests": "4",
				"X-RateLimit-Reset-Requests":     "1s",
				"X-RateLimit-Remaining-Tokens":   "0",
				"X-RateLimit-Reset-Tokens":       "20m0s",
			},
			wantDelay:  20 * time.Minute,
			wantSource: "token_reset",
		},
		{
			name: "both dimensions exhausted",
			headers: map[string]string{
				"X-RateLimit-Remaining-Requests": "0",
				"X-RateLimit-Reset-Requests":     "3s",
				"X-RateLimit-Remaining-Tokens":   "0",
				"X-RateLimit-Reset-Tokens":       "6s",
			},
			wantDelay:  6 * time.Second,
			wantSource: "exhausted_resets",
		},
		{
			name: "ambiguous resets use larger delay",
			headers: map[string]string{
				"X-RateLimit-Remaining-Requests": "1",
				"X-RateLimit-Reset-Requests":     "3s",
				"X-RateLimit-Remaining-Tokens":   "1",
				"X-RateLimit-Reset-Tokens":       "6s",
			},
			wantDelay:  6 * time.Second,
			wantSource: "ambiguous_resets",
		},
		{
			name: "retry after larger than reset uses safe maximum",
			headers: map[string]string{
				"Retry-After":                          "8",
				"X-RateLimit-Remaining-Requests":       "0",
				"X-RateLimit-Reset-Requests":           "3s",
				"X-RateLimit-Remaining-Tokens":         "10",
				"X-RateLimit-Reset-Tokens":             "6s",
				"X-RateLimit-Remaining-Project-Tokens": "1",
				"X-RateLimit-Reset-Project-Tokens":     "2s",
			},
			wantDelay:  8 * time.Second,
			wantSource: "retry_after_and_request_reset",
		},
		{
			name: "reset larger than retry after uses safe maximum",
			headers: map[string]string{
				"Retry-After":                    "3",
				"X-RateLimit-Remaining-Requests": "0",
				"X-RateLimit-Reset-Requests":     "8s",
			},
			wantDelay:  8 * time.Second,
			wantSource: "retry_after_and_request_reset",
		},
		{
			name: "project token exhaustion",
			headers: map[string]string{
				"X-RateLimit-Remaining-Project-Tokens": "0",
				"X-RateLimit-Reset-Project-Tokens":     "9s",
			},
			wantDelay:  9 * time.Second,
			wantSource: "project_token_reset",
		},
		{
			name: "no usable metadata uses existing fallback",
			headers: map[string]string{
				"Retry-After":                    "0",
				"X-RateLimit-Reset-Requests":     "0",
				"X-RateLimit-Remaining-Requests": "-1",
				"X-RateLimit-Reset-Tokens":       "invalid",
			},
			wantDelay:  800 * time.Millisecond,
			wantSource: "local_backoff",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := newTestRateLimitRetryGateway(t, &retrySequenceGateway{}, nil, func() float64 { return 0 })
			delay, source := gateway.rateLimitDelay(newOpenAIRateLimitedError(rateLimitHeaders(test.headers), now), 0)
			if delay != test.wantDelay || source != test.wantSource {
				t.Fatalf("delay/source = %v/%q, want %v/%q", delay, source, test.wantDelay, test.wantSource)
			}
		})
	}
}

func TestRateLimitRetryGatewayAddsOnlyPositiveProviderJitter(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	rateError := newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{"Retry-After": "1200"}), now)
	for _, test := range []struct {
		jitter float64
		want   time.Duration
	}{
		{jitter: -1, want: 20 * time.Minute},
		{jitter: 0, want: 20 * time.Minute},
		{jitter: 0.5, want: 20*time.Minute + maximumProviderRetryJitter/2},
		{jitter: 1, want: 20*time.Minute + maximumProviderRetryJitter},
		{jitter: 2, want: 20*time.Minute + maximumProviderRetryJitter},
	} {
		gateway := newTestRateLimitRetryGateway(t, &retrySequenceGateway{}, nil, func() float64 { return test.jitter })
		got, source := gateway.rateLimitDelay(rateError, 0)
		if got != test.want || got < 20*time.Minute || source != "retry_after" {
			t.Fatalf("delay/source = %v/%q, want at least 20m and %v/retry_after", got, source, test.want)
		}
	}
}

func TestRateLimitRetryGatewayRecordsOnlyTrustedSuccessfulResponses(t *testing.T) {
	for _, retryableFailures := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d rate limits then success", retryableFailures), func(t *testing.T) {
			attempts := 0
			client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts++
				w.Header().Set("Content-Type", "application/json")
				if attempts <= retryableFailures {
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprint(w, `{"error":{"type":"rate_limit_exceeded","code":"rate_limit_exceeded"}}`)
					return
				}
				fmt.Fprint(w, completedResponseJSON("# Story\n\nGenerated."))
			}))
			gateway, err := NewRateLimitRetryGateway(RateLimitRetryConfig{
				Gateway: client,
				Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
				Wait:    func(context.Context, time.Duration) error { return nil },
				Jitter:  func() float64 { return 0.5 },
			})
			if err != nil {
				t.Fatal(err)
			}
			recorder := &recordingResponsesUsageRecorder{}
			if _, err := gateway.Create(WithResponsesUsageRecorder(context.Background(), recorder), validResponsesCall()); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if attempts != retryableFailures+1 || len(recorder.events) != 1 {
				t.Fatalf("attempts/events = %d/%#v", attempts, recorder.events)
			}
		})
	}
}

func TestRateLimitRetryGatewayDoesNotRecordNonSuccessfulProviderErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want error
	}{
		{name: "exhausted rate limits", body: `{"error":{"type":"rate_limit_exceeded","code":"rate_limit_exceeded"}}`, want: ErrOpenAIRateLimited},
		{name: "quota exhaustion", body: `{"error":{"type":"insufficient_quota","code":"insufficient_quota"}}`, want: ErrOpenAIQuotaExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, test.body)
			}))
			gateway, err := NewRateLimitRetryGateway(RateLimitRetryConfig{
				Gateway: client,
				Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
				Wait:    func(context.Context, time.Duration) error { return nil },
				Jitter:  func() float64 { return 0.5 },
			})
			if err != nil {
				t.Fatal(err)
			}
			recorder := &recordingResponsesUsageRecorder{}
			_, err = gateway.Create(WithResponsesUsageRecorder(context.Background(), recorder), validResponsesCall())
			if !errors.Is(err, test.want) || len(recorder.events) != 0 {
				t.Fatalf("error/events = %v/%#v", err, recorder.events)
			}
		})
	}
}

func TestParseOpenAIRateLimitReset(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want time.Duration
	}{
		{raw: "1s", want: time.Second},
		{raw: "6m0s", want: 6 * time.Minute},
		{raw: "20m", want: 20 * time.Minute},
		{raw: "3h", want: maxOpenAIRateLimitDelay},
		{raw: "0"},
		{raw: "-1s"},
		{raw: "invalid"},
		{raw: strings.Repeat("1", maxOpenAIRateLimitMetadataBytes+1)},
	} {
		got, ok := parseOpenAIRateLimitReset(test.raw)
		if got != test.want || ok != (test.want > 0) {
			t.Fatalf("parseOpenAIRateLimitReset(%q) = %v / %v, want %v / %v", test.raw, got, ok, test.want, test.want > 0)
		}
	}
	if maxOpenAIRateLimitDelay <= time.Hour {
		t.Fatalf("provider delay cap = %v, must exceed the one-hour durable job deadline", maxOpenAIRateLimitDelay)
	}
}

func TestRateLimitRetryGatewayLogsOnlyAllowlistedMetadata(t *testing.T) {
	var output bytes.Buffer
	errorBodySecret := "provider-error-message-must-not-log"
	promptSecret := "prompt-must-not-log"
	inner := &retrySequenceGateway{
		errors: []error{newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{
			"X-Request-ID":                   "req_safe",
			"X-RateLimit-Limit-Requests":     "60",
			"X-RateLimit-Remaining-Requests": "0",
			"X-RateLimit-Reset-Requests":     "2s",
			"Authorization":                  "Bearer response-secret-must-not-log",
			"X-Unrelated-Provider-Header":    errorBodySecret,
		}), time.Now())},
		result: retrySuccessResult(),
	}
	gateway, err := NewRateLimitRetryGateway(RateLimitRetryConfig{
		Gateway: inner,
		Logger:  slog.New(slog.NewJSONHandler(&output, nil)),
		Wait:    func(context.Context, time.Duration) error { return nil },
		Jitter:  func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatal(err)
	}
	call := validResponsesCall()
	call.Prompt.DeveloperInstructions = promptSecret
	if _, err := gateway.Create(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	logs := output.String()
	for _, expected := range []string{"provider_request_id", "req_safe", "request_remaining", "request_reset", "timing_source"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("logs missing %q: %s", expected, logs)
		}
	}
	for _, forbidden := range []string{errorBodySecret, promptSecret, "response-secret-must-not-log", "X-Unrelated-Provider-Header"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("logs unexpectedly contain %q: %s", forbidden, logs)
		}
	}
}

func TestRateLimitRetryGatewayExhaustionLogsFinalTimingSource(t *testing.T) {
	var output bytes.Buffer
	rateLimitError := newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{
		"X-RateLimit-Remaining-Tokens": "0",
		"X-RateLimit-Reset-Tokens":     "20m0s",
	}), time.Now())
	inner := &retrySequenceGateway{errors: []error{
		rateLimitError,
		rateLimitError,
		rateLimitError,
		rateLimitError,
		rateLimitError,
	}}
	waits := 0
	gateway, err := NewRateLimitRetryGateway(RateLimitRetryConfig{
		Gateway: inner,
		Logger:  slog.New(slog.NewJSONHandler(&output, nil)),
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
		Jitter: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = gateway.Create(context.Background(), validResponsesCall())
	if !errors.Is(err, ErrOpenAIRateLimited) || len(inner.calls) != maxOpenAIRateLimitRetries+1 || waits != maxOpenAIRateLimitRetries {
		t.Fatalf("error/calls/waits = %v / %d / %d", err, len(inner.calls), waits)
	}

	var exhaustionLog string
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if strings.Contains(line, `"outcome":"exhausted"`) {
			exhaustionLog = line
			break
		}
	}
	if exhaustionLog == "" {
		t.Fatalf("exhaustion log missing from %s", output.String())
	}
	if !strings.Contains(exhaustionLog, `"timing_source":"token_reset"`) || strings.Contains(exhaustionLog, `"timing_source":"exhausted"`) {
		t.Fatalf("unexpected exhaustion timing source: %s", exhaustionLog)
	}
}

func TestRateLimitRetryGatewayStopsWhenBackoffContextEnds(t *testing.T) {
	t.Run("provider-derived cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		inner := &retrySequenceGateway{errors: []error{newOpenAIRateLimitedError(rateLimitHeaders(map[string]string{
			"X-RateLimit-Remaining-Tokens": "0",
			"X-RateLimit-Reset-Tokens":     "3h",
		}), time.Now())}}
		var gotDelay time.Duration
		gateway := newTestRateLimitRetryGateway(t, inner, func(ctx context.Context, delay time.Duration) error {
			gotDelay = delay
			cancel()
			return ctx.Err()
		}, func() float64 { return 0 })
		_, err := gateway.Create(ctx, validResponsesCall())
		if !errors.Is(err, context.Canceled) || len(inner.calls) != 1 || gotDelay != maxOpenAIRateLimitDelay {
			t.Fatalf("error/calls/delay = %v / %d / %v", err, len(inner.calls), gotDelay)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		inner := &retrySequenceGateway{errors: []error{ErrOpenAIRateLimited}}
		gateway := newTestRateLimitRetryGateway(t, inner, func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		}, func() float64 { return 0.5 })
		_, err := gateway.Create(ctx, validResponsesCall())
		if !errors.Is(err, context.Canceled) || len(inner.calls) != 1 {
			t.Fatalf("error/calls = %v / %d", err, len(inner.calls))
		}
	})

	t.Run("deadline", func(t *testing.T) {
		ctx := newDeadlineDuringWaitContext()
		inner := &retrySequenceGateway{errors: []error{ErrOpenAIRateLimited}}
		gateway := newTestRateLimitRetryGateway(t, inner, func(_ context.Context, _ time.Duration) error {
			ctx.expire()
			return ctx.Err()
		}, func() float64 { return 0.5 })
		_, err := gateway.Create(ctx, validResponsesCall())
		if !errors.Is(err, context.DeadlineExceeded) || len(inner.calls) != 1 {
			t.Fatalf("error/calls = %v / %d", err, len(inner.calls))
		}
	})
}

func TestParseOpenAIRetryAfter(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{raw: "5", want: 5 * time.Second, ok: true},
		{raw: "1200", want: 20 * time.Minute, ok: true},
		{raw: "10800", want: maxOpenAIRateLimitDelay, ok: true},
		{raw: now.Add(30 * time.Second).Format(httpTimeFormat), want: 30 * time.Second, ok: true},
		{raw: now.Add(3 * time.Hour).Format(httpTimeFormat), want: maxOpenAIRateLimitDelay, ok: true},
		{raw: "0"},
		{raw: "-1"},
		{raw: now.Add(-time.Second).Format(httpTimeFormat)},
		{raw: "not-a-delay"},
	} {
		got, ok := parseOpenAIRetryAfter(test.raw, now)
		if ok != test.ok || got != test.want {
			t.Fatalf("parseOpenAIRetryAfter(%q) = %v / %v, want %v / %v", test.raw, got, ok, test.want, test.ok)
		}
	}
}

const httpTimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

type deadlineDuringWaitContext struct {
	done chan struct{}
}

func newDeadlineDuringWaitContext() *deadlineDuringWaitContext {
	return &deadlineDuringWaitContext{done: make(chan struct{})}
}

func (ctx *deadlineDuringWaitContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *deadlineDuringWaitContext) Done() <-chan struct{}       { return ctx.done }
func (ctx *deadlineDuringWaitContext) Value(any) any               { return nil }
func (ctx *deadlineDuringWaitContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (ctx *deadlineDuringWaitContext) expire() {
	close(ctx.done)
}
