package jitterx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func recvTick(t *testing.T, tk *Ticker) time.Time {
	t.Helper()
	select {
	case v := <-tk.C:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a tick")
		return time.Time{}
	}
}

func assertNoTick(t *testing.T, tk *Ticker) {
	t.Helper()
	select {
	case v := <-tk.C:
		t.Fatalf("unexpected tick at %v", v)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestTickerTicksOnInterval(t *testing.T) {
	clk := newFakeClock()
	tk := newTicker(clk, time.Minute, None())
	defer tk.Stop()

	assertNoTick(t, tk)

	for i := 0; i < 3; i++ {
		clk.Advance(time.Minute)
		recvTick(t, tk)
	}
}

func TestTickerAppliesJitter(t *testing.T) {
	clk := newFakeClock()
	// Proportional(0.5) with a source pinned at 0 yields interval/2.
	tk := newTicker(clk, time.Minute, Proportional(0.5, fixed(0)))
	defer tk.Stop()

	clk.Advance(29 * time.Second)
	assertNoTick(t, tk)

	clk.Advance(time.Second) // 30s total
	recvTick(t, tk)
}

func TestTickerStopIsIdempotentAndSilencesTicks(t *testing.T) {
	clk := newFakeClock()
	tk := newTicker(clk, time.Minute, None())

	tk.Stop()
	tk.Stop() // must not panic

	clk.Advance(10 * time.Minute)
	assertNoTick(t, tk)
}

func TestTickerResetChangesInterval(t *testing.T) {
	clk := newFakeClock()
	tk := newTicker(clk, time.Hour, None())
	defer tk.Stop()

	tk.Reset(time.Minute)

	clk.Advance(time.Minute)
	recvTick(t, tk)
}

func TestTickerDropsTicksForSlowReceivers(t *testing.T) {
	clk := newFakeClock()
	tk := newTicker(clk, time.Minute, None())
	defer tk.Stop()

	// Like time.Ticker: one tick of room, and sends that would block are
	// dropped rather than queued, so a slow receiver cannot build a backlog.
	if got := cap(tk.C); got != 1 {
		t.Fatalf("cap(tk.C) = %d, want 1", got)
	}

	clk.Advance(time.Minute)
	recvTick(t, tk)
}

func TestNewTickerRejectsNonPositiveInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewTicker(0) should panic, like time.NewTicker")
		}
	}()
	NewTicker(0, nil)
}

func TestNewTickerDefaultsToProportionalJitter(t *testing.T) {
	tk := NewTicker(time.Hour, nil)
	defer tk.Stop()
	if tk.strategy == nil {
		t.Fatal("nil strategy should fall back to a default")
	}
}

func TestEveryRunsUntilContextEnds(t *testing.T) {
	clk := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())

	calls := make(chan int, 8)
	done := make(chan error, 1)
	go func() {
		n := 0
		done <- every(ctx, clk, time.Minute, None(), func(context.Context) error {
			n++
			calls <- n
			return nil
		})
	}()

	clk.waitTimers(1)

	for i := 1; i <= 3; i++ {
		clk.Advance(time.Minute)
		if got := <-calls; got != i {
			t.Fatalf("call %d, want %d", got, i)
		}
	}

	cancel()
	clk.Advance(time.Minute)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("every() = %v, want context.Canceled", err)
	}
}

func TestEveryStopsOnError(t *testing.T) {
	clk := newFakeClock()
	sentinel := errors.New("boom")

	done := make(chan error, 1)
	go func() {
		done <- every(context.Background(), clk, time.Minute, None(), func(context.Context) error {
			return sentinel
		})
	}()

	clk.waitTimers(1)
	clk.Advance(time.Minute)
	select {
	case err := <-done:
		if !errors.Is(err, sentinel) {
			t.Fatalf("every() = %v, want %v", err, sentinel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("every() did not return after fn failed")
	}
}

func TestEveryReturnsImmediatelyIfContextAlreadyDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := every(ctx, newFakeClock(), time.Minute, None(), func(context.Context) error {
		calls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("every() = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}
