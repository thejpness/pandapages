package storygeneration

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
		{name: "valid retry after", rateError: newOpenAIRateLimitedError("30", now), jitter: 0, wantWait: 30 * time.Second},
		{name: "valid retry after HTTP date", rateError: newOpenAIRateLimitedError(now.Add(30*time.Second).Format(httpTimeFormat), now), jitter: 0, wantWait: 30 * time.Second},
		{name: "oversized numeric retry after is clamped", rateError: newOpenAIRateLimitedError("90", now), jitter: 0, wantWait: maxOpenAIRetryAfter},
		{name: "larger numeric retry after is clamped", rateError: newOpenAIRateLimitedError("120", now), jitter: 0, wantWait: maxOpenAIRetryAfter},
		{name: "oversized HTTP date retry after is clamped", rateError: newOpenAIRateLimitedError(now.Add(90*time.Second).Format(httpTimeFormat), now), jitter: 0, wantWait: maxOpenAIRetryAfter},
		{name: "zero retry after falls back", rateError: newOpenAIRateLimitedError("0", now), jitter: 0.5, wantWait: time.Second},
		{name: "malformed retry after falls back", rateError: newOpenAIRateLimitedError("not a delay", now), jitter: 0.5, wantWait: time.Second},
		{name: "past HTTP date retry after falls back", rateError: newOpenAIRateLimitedError(now.Add(-time.Second).Format(httpTimeFormat), now), jitter: 0.5, wantWait: time.Second},
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

func TestRateLimitRetryGatewayStopsWhenBackoffContextEnds(t *testing.T) {
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
		{raw: "90", want: maxOpenAIRetryAfter, ok: true},
		{raw: "120", want: maxOpenAIRetryAfter, ok: true},
		{raw: now.Add(30 * time.Second).Format(httpTimeFormat), want: 30 * time.Second, ok: true},
		{raw: now.Add(90 * time.Second).Format(httpTimeFormat), want: maxOpenAIRetryAfter, ok: true},
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
