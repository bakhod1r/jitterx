package jitterx

import (
	"testing"
	"time"
)

func BenchmarkFull(b *testing.B) {
	s := Full(nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = s.Jitter(time.Second)
	}
}

func BenchmarkEqual(b *testing.B) {
	s := Equal(nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = s.Jitter(time.Second)
	}
}

func BenchmarkDecorrelated(b *testing.B) {
	s := Decorrelated(time.Millisecond, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = s.Jitter(time.Second)
	}
}

func BenchmarkBackoffNext(b *testing.B) {
	bo := New(WithMax(time.Hour))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if i%32 == 0 {
			bo.Reset()
		}
		sink = bo.Next()
	}
}

var sink time.Duration
