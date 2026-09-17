package jitterx

import (
	"testing"
	"time"
)

// fixed returns the same float for every call, so strategy output is exact.
type fixed float64

func (f fixed) Float64() float64 { return float64(f) }

// seq walks a list of floats, repeating the last one once exhausted.
type seq struct {
	vals []float64
	i    int
}

func (s *seq) Float64() float64 {
	if s.i >= len(s.vals) {
		return s.vals[len(s.vals)-1]
	}
	v := s.vals[s.i]
	s.i++
	return v
}

func TestFull(t *testing.T) {
	tests := []struct {
		name string
		r    float64
		in   time.Duration
		want time.Duration
	}{
		{"zero random", 0, time.Second, 0},
		{"half random", 0.5, time.Second, 500 * time.Millisecond},
		{"near one", 0.75, time.Second, 750 * time.Millisecond},
		{"zero duration", 0.5, 0, 0},
		{"negative duration", 0.5, -time.Second, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Full(fixed(tc.r)).Jitter(tc.in)
			if got != tc.want {
				t.Fatalf("Full(%v).Jitter(%v) = %v, want %v", tc.r, tc.in, got, tc.want)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		r    float64
		in   time.Duration
		want time.Duration
	}{
		{"zero random", 0, time.Second, 500 * time.Millisecond},
		{"half random", 0.5, time.Second, 750 * time.Millisecond},
		{"zero duration", 0.5, 0, 0},
		{"negative duration", 0.5, -time.Second, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Equal(fixed(tc.r)).Jitter(tc.in)
			if got != tc.want {
				t.Fatalf("Equal(%v).Jitter(%v) = %v, want %v", tc.r, tc.in, got, tc.want)
			}
		})
	}
}

func TestProportional(t *testing.T) {
	tests := []struct {
		name string
		f    float64
		r    float64
		in   time.Duration
		want time.Duration
	}{
		{"midpoint is identity", 0.2, 0.5, time.Second, time.Second},
		{"lower bound", 0.2, 0, time.Second, 800 * time.Millisecond},
		{"upper bound", 0.2, 1, time.Second, 1200 * time.Millisecond},
		{"factor over one clamps at zero", 2, 0, time.Second, 0},
		{"zero duration", 0.2, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Proportional(tc.f, fixed(tc.r)).Jitter(tc.in)
			if got != tc.want {
				t.Fatalf("Proportional(%v,%v).Jitter(%v) = %v, want %v", tc.f, tc.r, tc.in, got, tc.want)
			}
		})
	}
}

func TestNone(t *testing.T) {
	if got := None().Jitter(time.Second); got != time.Second {
		t.Fatalf("None().Jitter(1s) = %v, want 1s", got)
	}
	if got := None().Jitter(-time.Second); got != 0 {
		t.Fatalf("None().Jitter(-1s) = %v, want 0", got)
	}
}

func TestDecorrelated(t *testing.T) {
	base := 100 * time.Millisecond
	// random_between(base, prev*3) == base + r*(prev*3 - base)
	s := Decorrelated(base, &seq{vals: []float64{0, 1, 0.5}})

	// prev starts at base: range [100ms, 300ms]
	if got := s.Jitter(time.Second); got != base {
		t.Fatalf("first Jitter = %v, want %v", got, base)
	}
	// prev == 100ms, r == 1 -> 300ms
	if got := s.Jitter(time.Second); got != 300*time.Millisecond {
		t.Fatalf("second Jitter = %v, want 300ms", got)
	}
	// prev == 300ms, r == 0.5 -> 100ms + 0.5*(900ms-100ms) == 500ms
	if got := s.Jitter(time.Second); got != 500*time.Millisecond {
		t.Fatalf("third Jitter = %v, want 500ms", got)
	}
}

func TestDecorrelatedResets(t *testing.T) {
	base := 100 * time.Millisecond
	s := Decorrelated(base, &seq{vals: []float64{1, 1}})
	s.Jitter(time.Second) // 300ms, prev == 300ms

	r, ok := s.(resetter)
	if !ok {
		t.Fatal("Decorrelated strategy does not implement resetter")
	}
	r.Reset()

	if got := s.Jitter(time.Second); got != 300*time.Millisecond {
		t.Fatalf("after Reset, Jitter = %v, want 300ms", got)
	}
}

func TestStrategiesNeverNegative(t *testing.T) {
	src := newDefaultSource()
	strategies := map[string]Strategy{
		"full":         Full(src),
		"equal":        Equal(src),
		"proportional": Proportional(0.9, src),
		"decorrelated": Decorrelated(time.Millisecond, src),
		"none":         None(),
	}
	for name, s := range strategies {
		for i := 0; i < 10000; i++ {
			if got := s.Jitter(time.Second); got < 0 {
				t.Fatalf("%s.Jitter(1s) = %v, want >= 0", name, got)
			}
		}
	}
}

func TestFullAndEqualStayWithinBounds(t *testing.T) {
	src := newDefaultSource()
	const d = time.Second
	for i := 0; i < 10000; i++ {
		if got := Full(src).Jitter(d); got < 0 || got > d {
			t.Fatalf("Full out of [0,%v]: %v", d, got)
		}
		if got := Equal(src).Jitter(d); got < d/2 || got > d {
			t.Fatalf("Equal out of [%v,%v]: %v", d/2, d, got)
		}
	}
}
