package jitterx

import (
	"sync"
	"time"
)

// fakeClock drives timers by hand so scheduling tests never sleep.
//
// A timer whose deadline is already past fires the moment it is created or
// reset. That removes the race between a goroutine arming its timer and a test
// advancing the clock: the test can advance first and the arm still fires.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) timer {
	t := &fakeTimer{c: make(chan time.Time, 1), clk: c}
	c.mu.Lock()
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	t.Reset(d)
	return t
}

// Advance moves the clock forward and fires every timer now due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	due := make([]*fakeTimer, 0, len(c.timers))
	for _, t := range c.timers {
		if t.active && !t.deadline.After(now) {
			t.active = false
			due = append(due, t)
		}
	}
	c.mu.Unlock()

	for _, t := range due {
		t.fire(now)
	}
}

// waitTimers blocks until at least n timers have been created, so a test can
// advance the clock only once the code under test has armed its timer.
func (c *fakeClock) waitTimers(n int) {
	for i := 0; i < 2000; i++ {
		c.mu.Lock()
		got := len(c.timers)
		c.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	panic("jitterx: timed out waiting for timers to be armed")
}

type fakeTimer struct {
	c        chan time.Time
	deadline time.Time
	active   bool
	clk      *fakeClock
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Reset(d time.Duration) {
	t.clk.mu.Lock()
	now := t.clk.now
	t.deadline = now.Add(d)
	overdue := !t.deadline.After(now)
	t.active = !overdue
	t.clk.mu.Unlock()

	if overdue {
		t.fire(now)
	}
}

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	was := t.active
	t.active = false
	return was
}

func (t *fakeTimer) fire(now time.Time) {
	select {
	case t.c <- now:
	default: // a pending tick is already queued; drop this one, like time.Timer
	}
}
