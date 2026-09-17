package jitterx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitSleepsForTheJitteredDuration(t *testing.T) {
	clk := newFakeClock()

	done := make(chan error, 1)
	go func() {
		done <- wait(context.Background(), clk, time.Minute, Proportional(0.5, fixed(0)))
	}()
	clk.waitTimers(1)

	clk.Advance(29 * time.Second)
	select {
	case <-done:
		t.Fatal("wait returned before the jittered duration elapsed")
	case <-time.After(20 * time.Millisecond):
	}

	clk.Advance(time.Second) // 30s: interval/2
	if err := <-done; err != nil {
		t.Fatalf("wait() = %v, want nil", err)
	}
}

func TestWaitReturnsOnContextCancel(t *testing.T) {
	clk := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- wait(ctx, clk, time.Hour, None()) }()
	clk.waitTimers(1)

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait() = %v, want context.Canceled", err)
	}
}

func TestWaitReturnsImmediatelyForNonPositiveDuration(t *testing.T) {
	if err := wait(context.Background(), newFakeClock(), 0, None()); err != nil {
		t.Fatalf("wait() = %v, want nil", err)
	}
}

func TestWaitChecksContextBeforeSleeping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := wait(ctx, newFakeClock(), time.Hour, None()); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait() = %v, want context.Canceled", err)
	}
}

func TestWaitNilStrategyDoesNotJitter(t *testing.T) {
	clk := newFakeClock()

	done := make(chan error, 1)
	go func() { done <- wait(context.Background(), clk, time.Minute, nil) }()
	clk.waitTimers(1)

	clk.Advance(time.Minute)
	if err := <-done; err != nil {
		t.Fatalf("wait() = %v, want nil", err)
	}
}
