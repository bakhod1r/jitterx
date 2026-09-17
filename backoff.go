package jitterx

import (
	"math"
	"time"
)

// Stop is returned by Next once the configured attempt limit is spent.
const Stop = time.Duration(-1)

// Backoff produces a sequence of jittered delays: the raw delay grows
// geometrically from base, is clamped to max, and is then handed to the
// Strategy.
//
// A Backoff is not safe for concurrent use. Give each retry loop its own.
type Backoff struct {
	base       time.Duration
	max        time.Duration
	multiplier float64
	strategy   Strategy
	maxRetries int
	attempt    int
}

// New builds a Backoff. Without options it uses DefaultBase, DefaultMax,
// DefaultMultiplier, full jitter and no attempt limit.
func New(opts ...Option) *Backoff {
	b := &Backoff{
		base:       DefaultBase,
		max:        DefaultMax,
		multiplier: DefaultMultiplier,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.strategy == nil {
		b.strategy = Full(nil)
	}
	if b.max < b.base {
		b.max = b.base
	}
	if c, ok := b.strategy.(capped); ok {
		c.SetCap(b.max)
	}
	return b
}

// Next advances the attempt counter and returns the next delay, or Stop once
// the attempt limit set by WithMaxRetries is spent.
func (b *Backoff) Next() time.Duration {
	if b.maxRetries > 0 && b.attempt >= b.maxRetries {
		return Stop
	}

	raw := float64(b.base) * math.Pow(b.multiplier, float64(b.attempt))
	b.attempt++

	d := nonNegative(raw)
	if d > b.max {
		d = b.max
	}

	d = b.strategy.Jitter(d)
	if d > b.max {
		d = b.max
	}
	if d < 0 {
		d = 0
	}
	return d
}

// Attempt reports how many delays have been handed out since the last Reset.
func (b *Backoff) Attempt() int { return b.attempt }

// Reset returns the Backoff, and any stateful Strategy it holds, to its
// initial state.
func (b *Backoff) Reset() {
	b.attempt = 0
	if r, ok := b.strategy.(resetter); ok {
		r.Reset()
	}
}
