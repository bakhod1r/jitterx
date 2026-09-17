package jitterx

import "time"

// Defaults applied by New when an option is missing or out of range.
const (
	DefaultBase       = 100 * time.Millisecond
	DefaultMax        = 30 * time.Second
	DefaultMultiplier = 2.0
)

// Option configures a Backoff. Invalid values are ignored rather than
// returning an error, so New never fails and misconfiguration degrades to the
// documented default.
type Option func(*Backoff)

// WithBase sets the delay before the first retry. Values <= 0 are ignored.
func WithBase(d time.Duration) Option {
	return func(b *Backoff) {
		if d > 0 {
			b.base = d
		}
	}
}

// WithMax caps every delay, jitter included. Values <= 0 are ignored.
func WithMax(d time.Duration) Option {
	return func(b *Backoff) {
		if d > 0 {
			b.max = d
		}
	}
}

// WithMultiplier sets the geometric growth factor. Values < 1 are ignored,
// since they would shrink the delay as attempts pile up.
func WithMultiplier(f float64) Option {
	return func(b *Backoff) {
		if f >= 1 {
			b.multiplier = f
		}
	}
}

// WithStrategy sets the jitter strategy. A nil strategy is ignored.
//
// Stateful strategies (Decorrelated) must not be shared between Backoff
// instances or goroutines.
func WithStrategy(s Strategy) Option {
	return func(b *Backoff) {
		if s != nil {
			b.strategy = s
		}
	}
}

// WithMaxRetries limits the Backoff to n delays, after which Next returns
// Stop. Under Do that means fn is called at most n+1 times: once, then once
// per retry. Values <= 0 mean unlimited, which is the default.
func WithMaxRetries(n int) Option {
	return func(b *Backoff) { b.maxRetries = n }
}
