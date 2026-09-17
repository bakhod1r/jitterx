package jitterx

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// drainLimit caps how much of a discarded response body is read back before
// closing, so the connection can be reused without a huge error page stalling
// the retry.
const drainLimit = 4 << 10

// Transport is an [http.RoundTripper] that retries failed requests with
// jittered backoff. Wrap it around another transport and every client using it
// retries, with no change at the call sites:
//
//	client := &http.Client{Transport: &jitterx.Transport{
//	    NewBackoff: func() *jitterx.Backoff {
//	        return jitterx.New(jitterx.WithMaxRetries(3))
//	    },
//	}}
//
// When the retries run out, Transport returns the last response rather than an
// error: a caller needs the status and body to decide what to do. It returns
// an error only when the final attempt did, or when the context ended.
//
// A response that will not be returned is drained and closed first, so the
// connection goes back to the pool instead of being dropped.
type Transport struct {
	// Base sends the requests. Nil means [http.DefaultTransport].
	Base http.RoundTripper

	// NewBackoff builds a Backoff for one request. It is called per request
	// because a Backoff is not safe for concurrent use, and a transport is
	// shared by every goroutine using the client. Nil means New().
	NewBackoff func() *Backoff

	// ShouldRetry decides whether a result is worth another attempt. Exactly
	// one of resp and err is non-nil. Nil means [DefaultShouldRetry].
	ShouldRetry func(req *http.Request, resp *http.Response, err error) bool
}

// idempotent methods can be retried without the risk of applying an effect
// twice, per RFC 9110 section 9.2.2.
var idempotentMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
	http.MethodPut:     true,
	http.MethodDelete:  true,
}

// retryableStatus lists the responses worth another attempt: the server either
// said so (429) or failed in a way that is plausibly transient.
var retryableStatus = map[int]bool{
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

// DefaultShouldRetry retries idempotent requests that failed in transport or
// came back 429, 500, 502, 503 or 504.
//
// Non-idempotent methods are never retried: a POST may already have had its
// effect before the error was written, and sending it again would apply that
// effect twice. If yours is safe to repeat — it carries an idempotency key, or
// the endpoint deduplicates — say so with your own [Transport.ShouldRetry].
func DefaultShouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if !idempotentMethods[req.Method] {
		return false
	}
	if err != nil {
		return true
	}
	return retryableStatus[resp.StatusCode]
}

// RoundTrip implements [http.RoundTripper].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	shouldRetry := t.ShouldRetry
	if shouldRetry == nil {
		shouldRetry = DefaultShouldRetry
	}
	b := New()
	if t.NewBackoff != nil {
		b = t.NewBackoff()
	}
	b.Reset()

	ctx := req.Context()

	// A body with no GetBody cannot be rewound, so a second attempt would send
	// an empty one. Better to make a single attempt than a wrong one.
	replayable := req.Body == nil || req.GetBody != nil

	for {
		attempt, err := cloneRequest(req)
		if err != nil {
			return nil, err
		}

		resp, err := base.RoundTrip(attempt)
		if !shouldRetry(req, resp, err) || !replayable {
			return resp, err
		}

		d := b.Next()
		if d == Stop {
			return resp, err
		}
		if resp != nil {
			if after, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
				d = after
				if b.maxRetryAfter > 0 && d > b.maxRetryAfter {
					d = b.maxRetryAfter
				}
			}
		}
		if b.onRetry != nil {
			b.onRetry(b.Attempt(), d, err)
		}

		drainAndClose(resp)

		if err := wait(ctx, defaultClock, d, nil); err != nil {
			return nil, err
		}
	}
}

// cloneRequest builds the request for one attempt, rewinding the body if there
// is one. The caller's request is never modified, as RoundTrip requires.
func cloneRequest(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	if req.GetBody == nil {
		return r, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	r.Body = body
	return r, nil
}

// drainAndClose returns a connection to the pool instead of dropping it.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
	_ = resp.Body.Close()
}

// parseRetryAfter reads a Retry-After header in either of its forms, seconds
// or an HTTP-date, per RFC 9110 section 10.2.3. A date already in the past
// means retry now. It reports false for anything it cannot make sense of, so
// the caller falls back to its own schedule.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}

	at, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	if d := at.Sub(now); d > 0 {
		return d, true
	}
	return 0, true
}
