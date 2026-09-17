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

// WithMaxElapsed gives the whole retry sequence a time budget. Next returns
// Stop once the budget is spent, or once the delay it is about to hand out
// would sleep past it. Values <= 0 mean unlimited, which is the default.
//
// The clock starts in New and restarts on Reset, which Do calls on entry.
func WithMaxElapsed(d time.Duration) Option {
	return func(b *Backoff) {
		if d > 0 {
			b.maxElapsed = d
		}
	}
}

// WithMaxRetryAfter caps the delay a server can ask for through [RetryAfter].
// Without it a server's value is honoured as given, which is correct but
// leaves how long you wait in someone else's hands. Values <= 0 mean no cap,
// which is the default.
func WithMaxRetryAfter(d time.Duration) Option {
	return func(b *Backoff) {
		if d > 0 {
			b.maxRetryAfter = d
		}
	}
}

// WithOnRetry registers a hook called once per retry, before the wait, with
// the 1-based attempt number, the delay about to be slept, and the error that
// caused it. It is not called for the final failure, where nothing is retried.
//
// The hook runs on the calling goroutine and blocks the retry, so keep it to
// logging or a metric.
func WithOnRetry(fn func(attempt int, delay time.Duration, err error)) Option {
	return func(b *Backoff) { b.onRetry = fn }
}

// withClock swaps the clock a Backoff measures its budget against, so tests
// need not spend real time.
func withClock(c clock) Option {
	return func(b *Backoff) {
		if c != nil {
			b.clk = c
		}
	}
}
