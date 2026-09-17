package jitterx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fastBackoff(opts ...Option) *Backoff {
	base := make([]Option, 0, 3+len(opts))
	base = append(base,
		WithBase(time.Millisecond),
		WithMax(2*time.Millisecond),
		WithStrategy(None()),
	)
	return New(append(base, opts...)...)
}

func TestDoSucceedsFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), fastBackoff(), func(context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), fastBackoff(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoStopsAtMaxRetries(t *testing.T) {
	sentinel := errors.New("always fails")
	calls := 0
	err := Do(context.Background(), fastBackoff(WithMaxRetries(3)), func(context.Context) error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do() = %v, want wrapped %v", err, sentinel)
	}
	// 3 retries means the initial call plus 3 more.
	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
}

func TestDoStopsOnPermanent(t *testing.T) {
	sentinel := errors.New("bad request")
	calls := 0
	err := Do(context.Background(), fastBackoff(), func(context.Context) error {
		calls++
		return Permanent(sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do() = %v, want wrapped %v", err, sentinel)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoReturnsContextErrorWhenCancelledUpFront(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Do(ctx, fastBackoff(), func(context.Context) error {
		calls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestDoReturnsContextAndLastError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sentinel := errors.New("transient")
	calls := 0
	err := Do(ctx, New(WithBase(time.Hour), WithStrategy(None())), func(context.Context) error {
		calls++
		cancel()
		return sentinel
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = %v, want context.Canceled", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do() = %v, want it to also wrap %v", err, sentinel)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoResetsBackoffOnEntry(t *testing.T) {
	b := fastBackoff(WithMaxRetries(2))
	b.Next()
	b.Next() // exhausted

	calls := 0
	err := Do(context.Background(), b, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestPermanentUnwraps(t *testing.T) {
	sentinel := errors.New("nope")
	err := Permanent(sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(Permanent(e), e) = false")
	}
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil) should be nil")
	}
}

// Do must actually wait out the backoff delay, not just loop. Driven by a fake
// clock, so the test proves the wait without spending a minute on it.
func TestDoWaitsForTheBackoffDelay(t *testing.T) {
	clk := newFakeClock()
	b := New(WithBase(time.Minute), WithStrategy(None()), WithMaxRetries(1))

	calls := make(chan int, 4)
	done := make(chan error, 1)
	go func() {
		n := 0
		done <- do(context.Background(), clk, b, func(context.Context) error {
			n++
			calls <- n
			return errors.New("transient")
		})
	}()

	if got := <-calls; got != 1 {
		t.Fatalf("first call = %d, want 1", got)
	}
	clk.waitTimers(1)

	select {
	case n := <-calls:
		t.Fatalf("call %d happened before the delay elapsed", n)
	case <-time.After(20 * time.Millisecond):
	}

	clk.Advance(time.Minute)
	if got := <-calls; got != 2 {
		t.Fatalf("second call = %d, want 2", got)
	}
	if err := <-done; err == nil {
		t.Fatal("Do() = nil, want the last error")
	}
}
