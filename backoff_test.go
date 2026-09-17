package jitterx

import (
	"testing"
	"time"
)

func TestBackoffGrowsGeometrically(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithMultiplier(2),
		WithMax(time.Hour),
		WithStrategy(None()),
	)
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
	}
	for i, w := range want {
		if got := b.Next(); got != w {
			t.Fatalf("Next() #%d = %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoffClampsToMax(t *testing.T) {
	b := New(
		WithBase(time.Second),
		WithMultiplier(10),
		WithMax(5*time.Second),
		WithStrategy(None()),
	)
	b.Next() // 1s
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("Next() = %v, want 5s", got)
	}
	if got := b.Next(); got != 5*time.Second {
		t.Fatalf("Next() = %v, want 5s", got)
	}
}

func TestBackoffNeverExceedsMaxWithJitter(t *testing.T) {
	b := New(
		WithBase(time.Second),
		WithMultiplier(3),
		WithMax(10*time.Second),
		WithStrategy(Decorrelated(time.Second, newDefaultSource())),
	)
	for i := 0; i < 10000; i++ {
		d := b.Next()
		if d < 0 || d > 10*time.Second {
			t.Fatalf("Next() = %v, want within [0,10s]", d)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	b := New(WithBase(time.Second), WithMultiplier(2), WithStrategy(None()))
	b.Next()
	b.Next()
	if b.Attempt() != 2 {
		t.Fatalf("Attempt() = %d, want 2", b.Attempt())
	}

	b.Reset()

	if b.Attempt() != 0 {
		t.Fatalf("after Reset, Attempt() = %d, want 0", b.Attempt())
	}
	if got := b.Next(); got != time.Second {
		t.Fatalf("after Reset, Next() = %v, want 1s", got)
	}
}

func TestBackoffResetsStatefulStrategy(t *testing.T) {
	base := 100 * time.Millisecond
	b := New(
		WithBase(base),
		WithMax(time.Minute),
		WithStrategy(Decorrelated(base, &seq{vals: []float64{1, 1}})),
	)
	if got := b.Next(); got != 300*time.Millisecond {
		t.Fatalf("Next() = %v, want 300ms", got)
	}
	b.Reset()
	if got := b.Next(); got != 300*time.Millisecond {
		t.Fatalf("after Reset, Next() = %v, want 300ms", got)
	}
}

func TestBackoffMaxRetries(t *testing.T) {
	b := New(WithBase(time.Second), WithMaxRetries(2), WithStrategy(None()))
	if got := b.Next(); got == Stop {
		t.Fatal("Next() #1 = Stop, want a duration")
	}
	if got := b.Next(); got == Stop {
		t.Fatal("Next() #2 = Stop, want a duration")
	}
	if got := b.Next(); got != Stop {
		t.Fatalf("Next() #3 = %v, want Stop", got)
	}
}

func TestBackoffDefaults(t *testing.T) {
	b := New()
	d := b.Next()
	if d < 0 || d > DefaultBase {
		t.Fatalf("default Next() = %v, want within [0,%v] (full jitter)", d, DefaultBase)
	}
	if b.Attempt() != 1 {
		t.Fatalf("Attempt() = %d, want 1", b.Attempt())
	}
}

func TestBackoffRejectsBadOptions(t *testing.T) {
	b := New(WithBase(-time.Second), WithMultiplier(0.5), WithMax(-time.Second))
	if b.base != DefaultBase {
		t.Fatalf("base = %v, want %v", b.base, DefaultBase)
	}
	if b.multiplier != DefaultMultiplier {
		t.Fatalf("multiplier = %v, want %v", b.multiplier, DefaultMultiplier)
	}
	if b.max != DefaultMax {
		t.Fatalf("max = %v, want %v", b.max, DefaultMax)
	}
}

func TestBackoffHandlesOverflow(t *testing.T) {
	b := New(WithBase(time.Hour), WithMultiplier(1000), WithMax(24*time.Hour), WithStrategy(None()))
	for i := 0; i < 100; i++ {
		if got := b.Next(); got < 0 || got > 24*time.Hour {
			t.Fatalf("Next() #%d = %v, want within [0,24h]", i+1, got)
		}
	}
}
