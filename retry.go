package jitterx

import (
	"context"
	"errors"
)

// permanentError marks an error that must not be retried.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so Do stops immediately and returns the cause. Use it
// for failures no amount of waiting will fix: a 400, a validation error, a
// missing record. Permanent(nil) is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// Do calls fn until it succeeds, returns a Permanent error, exhausts b, or ctx
// ends. It resets b on entry, so the same Backoff can be reused across calls.
//
// On failure it returns the last error from fn; if ctx ended first, the
// returned error joins ctx.Err() with that last error, so errors.Is matches
// either.
func Do(ctx context.Context, b *Backoff, fn func(context.Context) error) error {
	return do(ctx, defaultClock, b, fn)
}

func do(ctx context.Context, clk clock, b *Backoff, fn func(context.Context) error) error {
	b.Reset()

	var last error
	var tm timer
	defer func() {
		if tm != nil {
			tm.Stop()
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, last)
		}

		last = fn(ctx)
		if last == nil {
			return nil
		}

		var perm *permanentError
		if errors.As(last, &perm) {
			return perm.err
		}

		d := b.Next()
		if d == Stop {
			return last
		}

		if tm == nil {
			tm = clk.NewTimer(d)
		} else {
			tm.Reset(d)
		}
		select {
		case <-ctx.Done():
			tm.Stop()
			return errors.Join(ctx.Err(), last)
		case <-tm.C():
		}
	}
}
