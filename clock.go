package jitterx

import "time"

// timer is the slice of *time.Timer this package needs, so scheduling can be
// driven by a fake clock in tests instead of by real elapsed time.
type timer interface {
	C() <-chan time.Time
	Reset(d time.Duration)
	Stop() bool
}

// clock creates timers and reports the current time.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) timer {
	return &realTimer{t: time.NewTimer(d)}
}

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time { return r.t.C }
func (r *realTimer) Stop() bool          { return r.t.Stop() }

// Reset drains a timer that has already fired before re-arming it, so a stale
// tick cannot satisfy the next wait.
func (r *realTimer) Reset(d time.Duration) {
	if !r.t.Stop() {
		select {
		case <-r.t.C:
		default:
		}
	}
	r.t.Reset(d)
}

// defaultClock is the clock used by every exported entry point.
var defaultClock clock = realClock{}
