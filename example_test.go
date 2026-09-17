package jitterx_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mrb/jitterx"
)

// Retry an HTTP request, treating 4xx as permanent.
func ExampleDo() {
	b := jitterx.New(
		jitterx.WithBase(200*time.Millisecond),
		jitterx.WithMax(10*time.Second),
		jitterx.WithMaxRetries(4),
		jitterx.WithStrategy(jitterx.Equal(nil)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var body int
	err := jitterx.Do(ctx, b, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
		if err != nil {
			return jitterx.Permanent(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err // transient: retry
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return jitterx.Permanent(fmt.Errorf("status %d", resp.StatusCode))
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		body = resp.StatusCode
		return nil
	})
	_ = body
	_ = errors.Is(err, context.DeadlineExceeded)
}

// Drive the delays yourself instead of handing control to Do.
func ExampleBackoff_Next() {
	b := jitterx.New(
		jitterx.WithBase(time.Second),
		jitterx.WithMax(time.Minute),
		jitterx.WithStrategy(jitterx.None()), // deterministic, for the output below
	)

	for {
		d := b.Next()
		if d == jitterx.Stop {
			break
		}
		fmt.Println(d)
		if b.Attempt() == 4 {
			break
		}
	}
	// Output:
	// 1s
	// 2s
	// 4s
	// 8s
}

// Spread a periodic task so many instances do not tick together.
func ExampleProportional() {
	interval := 30 * time.Second
	spread := jitterx.Proportional(0.1, nil) // +/- 10%

	next := spread.Jitter(interval)
	fmt.Println(next >= 27*time.Second && next <= 33*time.Second)
	// Output:
	// true
}
