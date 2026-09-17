package jitterx

import (
	"context"
	"time"
)

// Wait sleeps for a jittered d, returning early with ctx.Err() if ctx ends
// first. A nil Strategy waits exactly d, so jitter is opt-in.
//
// It is the primitive under [Do] and [Every], exported because a hand-rolled
// loop usually wants the same thing: a sleep that respects cancellation.
func Wait(ctx context.Context, d time.Duration, s Strategy) error {
	return wait(ctx, defaultClock, d, s)
}

func wait(ctx context.Context, clk clock, d time.Duration, s Strategy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		s = None()
	}
	if d = s.Jitter(d); d <= 0 {
		return nil
	}

	tm := clk.NewTimer(d)
	defer tm.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C():
		return nil
	}
}
