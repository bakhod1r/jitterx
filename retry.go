package jitterx

import (
	"context"
	"errors"
	"time"
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

// retryAfterError carries a delay the server asked for.
type retryAfterError struct {
	d   time.Duration
	err error
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter wraps err with a delay the server asked for, overriding whatever
// the Backoff would have chosen for that one wait. Use it for a 429 or 503
// carrying a Retry-After header: the server knows when it will be ready and
// the client does not.
//
// The delay is honoured as given unless [WithMaxRetryAfter] caps it. The
// attempt still counts against WithMaxRetries, and the wait still respects the
// context. RetryAfter(d, nil) is nil.
func RetryAfter(d time.Duration, err error) error {
	if err == nil {
		return nil
	}
	return &retryAfterError{d: d, err: err}
}

// Do calls fn until it succeeds, returns a Permanent error, exhausts b, or ctx
// ends. It resets b on entry, so the same Backoff can be reused across calls.
//
// On failure it returns the last error from fn; if ctx ended first, the
// returned error joins ctx.Err() with that last error, so errors.Is matches
// either.
func Do(ctx context.Context, b *Backoff, fn func(context.Context) error) error {
	return do(ctx, b.clk, b, fn)
}

// DoValue is [Do] for a function that produces a value. On success it returns
// the value from the attempt that succeeded; on failure, the zero value and
// the same error Do would return.
func DoValue[T any](ctx context.Context, b *Backoff, fn func(context.Context) (T, error)) (T, error) {
	return doValue(ctx, b.clk, b, fn)
}

func do(ctx context.Context, clk clock, b *Backoff, fn func(context.Context) error) error {
	_, err := doValue(ctx, clk, b, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

func doValue[T any](ctx context.Context, clk clock, b *Backoff, fn func(context.Context) (T, error)) (T, error) {
	var zero T
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
			return zero, errors.Join(err, last)
		}

		v, err := fn(ctx)
		if err == nil {
			return v, nil
		}
		last = err

		var perm *permanentError
		if errors.As(last, &perm) {
			return zero, perm.err
		}

		d := b.Next()
		if d == Stop {
			return zero, last
		}

		// A server that named a time outranks our schedule for this one wait.
		var ra *retryAfterError
		if errors.As(last, &ra) {
			d = ra.d
			if b.maxRetryAfter > 0 && d > b.maxRetryAfter {
				d = b.maxRetryAfter
			}
			if d < 0 {
				d = 0
			}
		}

		if b.onRetry != nil {
			b.onRetry(b.Attempt(), d, last)
		}

		if tm == nil {
			tm = clk.NewTimer(d)
		} else {
			tm.Reset(d)
		}
		select {
		case <-ctx.Done():
			tm.Stop()
			return zero, errors.Join(ctx.Err(), last)
		case <-tm.C():
		}
	}
}
