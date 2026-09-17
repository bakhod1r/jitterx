package jitterx_test

import (
	"context"
	"errors"
	"fmt"
	"log"
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

// Refresh a cache on a jittered schedule, so many instances do not all
// stampede the origin at the same second.
func ExampleNewTicker() {
	t := jitterx.NewTicker(30*time.Second, nil) // +/- 10% by default
	defer t.Stop()

	for range t.C {
		refreshCache()
	}
}

// Every is the same loop without the ticker bookkeeping, and it stops on
// context cancellation.
func ExampleEvery() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := jitterx.Every(ctx, time.Minute, nil, sendHeartbeat)
	fmt.Println(errors.Is(err, context.Canceled))
}

func refreshCache()                       {}
func sendHeartbeat(context.Context) error { return nil }

// DoValue retries a call that produces a value, and honours a Retry-After
// header when the server sends one.
func ExampleDoValue() {
	b := jitterx.New(
		jitterx.WithBase(200*time.Millisecond),
		jitterx.WithMaxElapsed(30*time.Second),
		jitterx.WithMaxRetryAfter(5*time.Minute),
		jitterx.WithOnRetry(func(attempt int, d time.Duration, err error) {
			log.Printf("attempt %d failed (%v), retrying in %v", attempt, err, d)
		}),
	)

	user, err := jitterx.DoValue(context.Background(), b, func(ctx context.Context) (*User, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/me", nil)
		if err != nil {
			return nil, jitterx.Permanent(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			after, _ := time.ParseDuration(resp.Header.Get("Retry-After") + "s")
			return nil, jitterx.RetryAfter(after, errors.New("rate limited"))
		}
		return decodeUser(resp)
	})
	_, _ = user, err
}

type User struct{}

func decodeUser(*http.Response) (*User, error) { return &User{}, nil }

// Early keeps a jittered delay under the deadline it protects, so a lease is
// always renewed before it expires.
func ExampleEarly() {
	lease := time.Minute
	renewIn := jitterx.Early(0.2, nil).Jitter(lease) // (48s, 60s]

	fmt.Println(renewIn <= lease)
	// Output:
	// true
}

// Transport puts retries underneath an http.Client, so call sites do not
// change.
func ExampleTransport() {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &jitterx.Transport{
			NewBackoff: func() *jitterx.Backoff {
				return jitterx.New(
					jitterx.WithBase(200*time.Millisecond),
					jitterx.WithMaxRetries(3),
					jitterx.WithMaxRetryAfter(time.Minute),
				)
			},
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// Retries are spent by now: this is the final status, whatever it is.
	fmt.Println(resp.StatusCode >= 200)
}
