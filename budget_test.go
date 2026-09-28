package jitterx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// WithMaxElapsed promises to stop rather than sleep past the budget. A
// server-named delay must not get around that.
func TestDoRetryAfterRespectsMaxElapsed(t *testing.T) {
	sentinel := errors.New("rate limited")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	err := Do(ctx, fastBackoff(WithMaxElapsed(100*time.Millisecond)), func(context.Context) error {
		return RetryAfter(time.Hour, sentinel)
	})
	if errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("Do slept past its budget on a Retry-After (err %v after %v)", err, time.Since(start))
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the last error", err)
	}
}

func TestTransportRetryAfterRespectsMaxElapsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	start := time.Now()
	resp, err := fastTransport(t, srv, WithMaxElapsed(100*time.Millisecond)).Do(req)
	if err != nil {
		t.Fatalf("err = %v after %v: the transport slept past its budget", err, time.Since(start))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestParseRetryAfterHugeSecondsDoesNotWrap(t *testing.T) {
	d, ok := parseRetryAfter("9300000000", time.Now())
	if !ok || d <= 0 {
		t.Fatalf("parseRetryAfter = %v, %v; want a large positive delay", d, ok)
	}
}

type trackBody struct {
	io.Reader
	closed bool
}

func (b *trackBody) Close() error { b.closed = true; return nil }

// RoundTrip must close the request body. With GetBody set, every attempt reads
// a fresh copy and the original was never closed.
func TestTransportClosesOriginalBodyWhenReplaying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	body := &trackBody{Reader: bytes.NewReader([]byte("x"))}
	req, _ := http.NewRequest(http.MethodPost, srv.URL, body)
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader([]byte("x"))), nil }
	tr := &Transport{Base: srv.Client().Transport}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !body.closed {
		t.Fatal("original request body was never closed")
	}
}
