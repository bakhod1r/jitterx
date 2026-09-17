package jitterx

import "time"

// Strategy turns an intended delay into a randomised one. Every strategy
// returns a non-negative duration, and returns 0 for a non-positive input.
type Strategy interface {
	Jitter(d time.Duration) time.Duration
}

// StrategyFunc adapts a plain function into a Strategy.
type StrategyFunc func(time.Duration) time.Duration

// Jitter implements Strategy.
func (f StrategyFunc) Jitter(d time.Duration) time.Duration { return f(d) }

// resetter is implemented by strategies that carry state between calls, so a
// Backoff can clear that state in Reset.
type resetter interface {
	Reset()
}

// nonNegative clamps a float64 number of nanoseconds into a valid duration.
func nonNegative(ns float64) time.Duration {
	if !(ns > 0) { // also catches NaN
		return 0
	}
	if ns >= float64(maxDuration) {
		return maxDuration
	}
	return time.Duration(ns)
}

const maxDuration = time.Duration(1<<63 - 1)

// Full picks uniformly from [0, d). This is AWS' "full jitter": the best
// choice for spreading a thundering herd, at the cost of occasionally
// retrying almost immediately.
//
// A nil Source means the default process-wide source.
func Full(src Source) Strategy {
	s := orDefault(src)
	return StrategyFunc(func(d time.Duration) time.Duration {
		if d <= 0 {
			return 0
		}
		return nonNegative(float64(d) * s.Float64())
	})
}

// Equal picks uniformly from [d/2, d). Half the delay is guaranteed, so a
// retry never fires immediately, and half is randomised.
//
// A nil Source means the default process-wide source.
func Equal(src Source) Strategy {
	s := orDefault(src)
	return StrategyFunc(func(d time.Duration) time.Duration {
		if d <= 0 {
			return 0
		}
		half := float64(d) / 2
		return nonNegative(half + half*s.Float64())
	})
}

// Proportional picks uniformly from [d-d*f, d+d*f]. Use it for scheduled work
// (cron ticks, cache TTLs, heartbeats) where the interval should stay roughly
// intact but callers must not line up. A factor above 1 is allowed; the result
// is still clamped at 0.
//
// A nil Source means the default process-wide source.
func Proportional(f float64, src Source) Strategy {
	s := orDefault(src)
	return StrategyFunc(func(d time.Duration) time.Duration {
		if d <= 0 {
			return 0
		}
		spread := float64(d) * f * (2*s.Float64() - 1)
		return nonNegative(float64(d) + spread)
	})
}

// None returns the delay unchanged. Useful as a default in tests and for
// disabling jitter without branching at the call site.
func None() Strategy {
	return StrategyFunc(func(d time.Duration) time.Duration {
		if d <= 0 {
			return 0
		}
		return d
	})
}

// decorrelated implements AWS' "decorrelated jitter":
//
//	sleep = random_between(base, prev*3)
//
// It ignores the delay handed to Jitter and drives itself from its own
// previous output, so growth and randomness come from the same draw. Because
// it carries state it must not be shared between Backoff instances or
// goroutines.
type decorrelated struct {
	base time.Duration
	prev time.Duration
	cap  time.Duration
	src  Source
}

// capped is implemented by strategies whose own state needs the Backoff's
// ceiling, so their growth does not run away past it.
type capped interface {
	SetCap(time.Duration)
}

// Decorrelated returns AWS' decorrelated jitter strategy, seeded at base.
//
// Unlike the other strategies it ignores its argument: the next delay is drawn
// from [base, prev*3]. Pair it with a Backoff for the upper clamp — nothing in
// the strategy itself caps growth.
//
// A nil Source means the default process-wide source.
func Decorrelated(base time.Duration, src Source) Strategy {
	if base <= 0 {
		base = DefaultBase
	}
	return &decorrelated{base: base, prev: base, cap: maxDuration, src: orDefault(src)}
}

func (s *decorrelated) Jitter(time.Duration) time.Duration {
	lo := float64(s.base)
	hi := float64(s.prev) * 3
	if hi < lo {
		hi = lo
	}
	next := nonNegative(lo + s.src.Float64()*(hi-lo))
	if next > s.cap {
		next = s.cap
	}
	s.prev = next
	return next
}

// SetCap bounds the state this strategy feeds back into itself. A Backoff
// calls it with its own max so growth stops where the Backoff would clamp
// anyway, instead of saturating.
func (s *decorrelated) SetCap(d time.Duration) {
	if d > 0 {
		s.cap = d
	}
}

func (s *decorrelated) Reset() { s.prev = s.base }
