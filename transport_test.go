package jitterx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastTransport retries quickly, so tests do not spend real time waiting.
func fastTransport(t *testing.T, srv *httptest.Server, opts ...Option) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: &Transport{
			Base: srv.Client().Transport,
			NewBackoff: func() *Backoff {
				base := make([]Option, 0, 4+len(opts))
				base = append(base,
					WithBase(time.Millisecond),
					WithMax(2*time.Millisecond),
					WithStrategy(None()),
					WithMaxRetries(3),
				)
				return New(append(base, opts...)...)
			},
		},
	}
}

// request builds a context-carrying request; the linter rejects the shorthand
// constructors, and every request here wants a context anyway.
func request(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func drain(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(b)
}

func TestTransportRetriesServerErrors(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, err := fastTransport(t, srv).Do(request(t, http.MethodGet, srv.URL, nil))
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got := drain(t, resp); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("hits = %d, want 3", got)
	}
}

func TestTransportReturnsLastResponseWhenRetriesRunOut(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "still broken")
	}))
	defer srv.Close()

	// Giving up must hand back the response, not an error: a caller needs the
	// status and body to decide what to do.
	resp, err := fastTransport(t, srv).Do(request(t, http.MethodGet, srv.URL, nil))
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if got := drain(t, resp); got != "still broken" {
		t.Fatalf("body = %q, want %q", got, "still broken")
	}
	if got := hits.Load(); got != 4 {
		t.Fatalf("hits = %d, want 4 (1 attempt + 3 retries)", got)
	}
}

func TestTransportDoesNotRetrySuccessOrClientErrors(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(code)
		}))

		resp, err := fastTransport(t, srv).Do(request(t, http.MethodGet, srv.URL, nil))
		if err != nil {
			t.Fatalf("status %d: Get() = %v", code, err)
		}
		drain(t, resp)
		if got := hits.Load(); got != 1 {
			t.Fatalf("status %d: hits = %d, want 1", code, got)
		}
		srv.Close()
	}
}

func TestTransportDoesNotRetryNonIdempotentMethods(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// A POST may already have had its effect before the 503 was written.
	resp, err := fastTransport(t, srv).Do(request(t, http.MethodPost, srv.URL, strings.NewReader("payload")))
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	drain(t, resp)
	if got := hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}
}

func TestTransportReplaysTheRequestBody(t *testing.T) {
	var hits atomic.Int32
	bodies := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies <- string(b)
		if hits.Add(1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	// PUT is idempotent, so it is retried, and every attempt must send the
	// same body rather than an empty one.
	resp, err := fastTransport(t, srv).Do(request(t, http.MethodPut, srv.URL, strings.NewReader("payload")))
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	drain(t, resp)

	close(bodies)
	n := 0
	for got := range bodies {
		n++
		if got != "payload" {
			t.Fatalf("attempt %d sent %q, want %q", n, got, "payload")
		}
	}
	if n != 2 {
		t.Fatalf("%d attempts, want 2", n)
	}
}

func TestTransportDoesNotRetryWhenTheBodyCannotBeReplayed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// A bare io.Reader gives http.NewRequest nothing to rewind from, so
	// GetBody is nil and a second attempt would send an empty body.
	req := request(t, http.MethodPut, srv.URL, io.LimitReader(strings.NewReader("payload"), 7))
	if req.GetBody != nil {
		t.Fatal("test precondition: expected GetBody to be nil")
	}

	resp, err := fastTransport(t, srv).Do(req)
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	drain(t, resp)
	if got := hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}
}

func TestTransportHonoursRetryAfterSeconds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	// The backoff would have waited a millisecond; the server asked for a
	// second, and MaxRetryAfter trims that to something a test can wait for.
	start := time.Now()
	resp, err := fastTransport(t, srv, WithMaxRetryAfter(50*time.Millisecond)).Do(request(t, http.MethodGet, srv.URL, nil))
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	drain(t, resp)

	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("waited %v, want at least the capped Retry-After of 50ms", elapsed)
	}
}

func TestTransportStopsOnContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fastTransport(t, srv).Do(req); !errors.Is(err, context.Canceled) { //nolint:bodyclose // no response on error
		t.Fatalf("Do() = %v, want context.Canceled", err)
	}
}

func TestTransportShouldRetryOverride(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	// Opt a POST in: the caller knows it carries an idempotency key.
	client := &http.Client{Transport: &Transport{
		Base: srv.Client().Transport,
		NewBackoff: func() *Backoff {
			return New(WithBase(time.Millisecond), WithMax(2*time.Millisecond), WithStrategy(None()), WithMaxRetries(3))
		},
		ShouldRetry: func(_ *http.Request, resp *http.Response, err error) bool {
			return err != nil || resp.StatusCode == http.StatusServiceUnavailable
		},
	}}

	resp, err := client.Do(request(t, http.MethodPost, srv.URL, strings.NewReader("payload")))
	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if got := drain(t, resp); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("hits = %d, want 3", got)
	}
}

func TestTransportRetriesConnectionFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	var attempts atomic.Int32
	client := &http.Client{Transport: &Transport{
		NewBackoff: func() *Backoff {
			return New(WithBase(time.Millisecond), WithMax(2*time.Millisecond), WithStrategy(None()), WithMaxRetries(2))
		},
		ShouldRetry: func(req *http.Request, resp *http.Response, err error) bool {
			attempts.Add(1)
			return DefaultShouldRetry(req, resp, err)
		},
	}}

	if _, err := client.Do(request(t, http.MethodGet, url, nil)); err == nil { //nolint:bodyclose // no response on error
		t.Fatal("Get() = nil, want a connection error")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("ShouldRetry consulted %d times, want 3 (1 attempt + 2 retries)", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		want   time.Duration
		ok     bool
	}{
		{"delta seconds", "120", 2 * time.Minute, true},
		{"zero seconds", "0", 0, true},
		{"http date in the future", "Thu, 17 Sep 2026 12:01:00 GMT", time.Minute, true},
		{"http date in the past", "Thu, 17 Sep 2026 11:00:00 GMT", 0, true},
		{"empty", "", 0, false},
		{"garbage", "soon", 0, false},
		{"negative seconds", "-5", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.header, now)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseRetryAfter(%q) = (%v, %v), want (%v, %v)", tc.header, got, ok, tc.want, tc.ok)
			}
		})
	}
}
