package jitterx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoValueReturnsTheValue(t *testing.T) {
	got, err := DoValue(context.Background(), fastBackoff(), func(context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("DoValue() error = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("DoValue() = %d, want 42", got)
	}
}

func TestDoValueReturnsTheValueFromTheSuccessfulAttempt(t *testing.T) {
	calls := 0
	got, err := DoValue(context.Background(), fastBackoff(), func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "partial", errors.New("transient")
		}
		return "final", nil
	})
	if err != nil {
		t.Fatalf("DoValue() error = %v, want nil", err)
	}
	if got != "final" {
		t.Fatalf("DoValue() = %q, want %q", got, "final")
	}
}

func TestDoValueReturnsZeroValueOnFailure(t *testing.T) {
	sentinel := errors.New("always fails")
	got, err := DoValue(context.Background(), fastBackoff(WithMaxRetries(1)), func(context.Context) (*int, error) {
		n := 7
		return &n, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("DoValue() error = %v, want %v", err, sentinel)
	}
	if got != nil {
		t.Fatalf("DoValue() = %v, want the zero value", got)
	}
}

func TestDoValueStopsOnPermanent(t *testing.T) {
	sentinel := errors.New("bad input")
	calls := 0
	_, err := DoValue(context.Background(), fastBackoff(), func(context.Context) (int, error) {
		calls++
		return 0, Permanent(sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("DoValue() error = %v, want %v", err, sentinel)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryAfterOverridesTheBackoffDelay(t *testing.T) {
	clk := newFakeClock()
	b := New(WithBase(time.Minute), WithMax(time.Minute), WithStrategy(None()), WithMaxRetries(1), withClock(clk))

	calls := make(chan int, 4)
	done := make(chan error, 1)
	go func() {
		n := 0
		done <- do(context.Background(), clk, b, func(context.Context) error {
			n++
			calls <- n
			// The server asked for five minutes; the backoff wanted one.
			return RetryAfter(5*time.Minute, errors.New("rate limited"))
		})
	}()

	<-calls
	clk.waitTimers(1)

	clk.Advance(time.Minute)
	select {
	case n := <-calls:
		t.Fatalf("call %d fired after 1m, ignoring the server's Retry-After", n)
	case <-time.After(20 * time.Millisecond):
	}

	clk.Advance(4 * time.Minute)
	if got := <-calls; got != 2 {
		t.Fatalf("second call = %d, want 2", got)
	}
	<-done
}

func TestRetryAfterIsCappedByMaxRetryAfter(t *testing.T) {
	clk := newFakeClock()
	b := New(
		WithBase(time.Minute),
		WithStrategy(None()),
		WithMaxRetries(1),
		WithMaxRetryAfter(2*time.Minute),
		withClock(clk),
	)

	calls := make(chan int, 4)
	done := make(chan error, 1)
	go func() {
		n := 0
		done <- do(context.Background(), clk, b, func(context.Context) error {
			n++
			calls <- n
			return RetryAfter(time.Hour, errors.New("rate limited"))
		})
	}()

	<-calls
	clk.waitTimers(1)
	clk.Advance(2 * time.Minute)
	if got := <-calls; got != 2 {
		t.Fatalf("second call = %d, want 2 (capped at 2m)", got)
	}
	<-done
}

func TestRetryAfterUnwraps(t *testing.T) {
	sentinel := errors.New("rate limited")
	err := RetryAfter(time.Second, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(RetryAfter(d, e), e) = false")
	}
	if RetryAfter(time.Second, nil) != nil {
		t.Fatal("RetryAfter(d, nil) should be nil")
	}
}

func TestOnRetryReportsEachRetry(t *testing.T) {
	type call struct {
		attempt int
		delay   time.Duration
		err     error
	}
	var got []call

	sentinel := errors.New("transient")
	err := Do(context.Background(), fastBackoff(
		WithMaxRetries(2),
		WithOnRetry(func(attempt int, d time.Duration, err error) {
			got = append(got, call{attempt, d, err})
		}),
	), func(context.Context) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do() = %v, want %v", err, sentinel)
	}

	// Two retries happen, so the hook fires twice; it is not called for the
	// final failure, where nothing is retried.
	if len(got) != 2 {
		t.Fatalf("hook fired %d times, want 2", len(got))
	}
	for i, c := range got {
		if c.attempt != i+1 {
			t.Fatalf("hook %d: attempt = %d, want %d", i, c.attempt, i+1)
		}
		if c.delay <= 0 {
			t.Fatalf("hook %d: delay = %v, want a positive delay", i, c.delay)
		}
		if !errors.Is(c.err, sentinel) {
			t.Fatalf("hook %d: err = %v, want %v", i, c.err, sentinel)
		}
	}
}
