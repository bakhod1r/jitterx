package jitterx

import "math/rand/v2"

// Source supplies the randomness a Strategy draws on. Implementations must
// return a value in [0, 1). The default source is safe for concurrent use;
// a custom one only needs to be safe if the Backoff using it is shared.
type Source interface {
	Float64() float64
}

// SourceFunc adapts a plain function into a Source.
type SourceFunc func() float64

// Float64 implements Source.
func (f SourceFunc) Float64() float64 { return f() }

type globalSource struct{}

func (globalSource) Float64() float64 { return rand.Float64() }

// newDefaultSource returns the process-wide, concurrency-safe random source
// used whenever a Strategy is constructed with a nil Source.
func newDefaultSource() Source { return globalSource{} }

// orDefault substitutes the default source for a nil one.
func orDefault(s Source) Source {
	if s == nil {
		return newDefaultSource()
	}
	return s
}
