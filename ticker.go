package jitterx

import (
	"context"
	"sync"
	"time"
)

// DefaultTickerSpread is the +/- fraction NewTicker jitters by when no
// strategy is given. Ten percent spreads a fleet without meaningfully moving
// the average interval.
const DefaultTickerSpread = 0.1

// Ticker delivers ticks at a jittered interval. It is the drop-in shape of
// [time.Ticker] — a receive-only C, Stop and Reset — for periodic work that
// many processes run on the same schedule: cache refreshes, heartbeats, lease
// renewals, reconcile loops.
//
// Each interval is jittered independently, so the ticker drifts relative to a
// fixed schedule; that is the point. If you need ticks anchored to absolute
// wall-clock times, this is the wrong tool.
//
// Like [time.Ticker], C has room for one tick and sends that would block are
// dropped, so a slow receiver cannot build a backlog. Stop releases the
// ticker's goroutine but does not close C.
type Ticker struct {
	// C delivers the ticks.
	C <-chan time.Time

	c        chan time.Time
	clk      clock
	strategy Strategy

	reset    chan resetReq
	stop     chan struct{}
	stopOnce sync.Once
}

type resetReq struct {
	d   time.Duration
	ack chan struct{}
}

// NewTicker returns a Ticker that fires about every interval, jittered by s.
// A nil strategy means Proportional with [DefaultTickerSpread].
//
// It panics if interval is not positive, matching [time.NewTicker].
//
// Strategies that can return zero, such as [Full], will make the ticker fire
// almost immediately every so often. That is usually not what a scheduler
// wants; Proportional or Equal are the fitting choices here.
func NewTicker(interval time.Duration, s Strategy) *Ticker {
	return newTicker(defaultClock, interval, s)
}

func newTicker(clk clock, interval time.Duration, s Strategy) *Ticker {
	if interval <= 0 {
		panic("jitterx: non-positive interval for NewTicker")
	}
	if s == nil {
		s = Proportional(DefaultTickerSpread, nil)
	}

	c := make(chan time.Time, 1)
	t := &Ticker{
		C:        c,
		c:        c,
		clk:      clk,
		strategy: s,
		reset:    make(chan resetReq),
		stop:     make(chan struct{}),
	}

	// Arm the first timer here rather than in the goroutine, so a caller that
	// advances a fake clock right after construction cannot race the arming.
	tm := clk.NewTimer(t.next(interval))
	go t.run(tm, interval)
	return t
}

// next jitters one interval, with a floor of a nanosecond so a strategy that
// returns zero cannot spin the ticker.
func (t *Ticker) next(interval time.Duration) time.Duration {
	d := t.strategy.Jitter(interval)
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}

func (t *Ticker) run(tm timer, interval time.Duration) {
	defer tm.Stop()

	for {
		select {
		case <-t.stop:
			return

		case req := <-t.reset:
			interval = req.d
			tm.Reset(t.next(interval))
			close(req.ack)

		case now := <-tm.C():
			// select picks at random when both a tick and Stop are ready, so
			// re-check: once Stop returns, no further tick is delivered.
			select {
			case <-t.stop:
				return
			default:
			}

			// Re-arm before delivering, so a receiver that wakes on this tick
			// observes a ticker already waiting for the next one.
			tm.Reset(t.next(interval))
			select {
			case t.c <- now:
			default: // receiver is behind; drop this tick
			}
		}
	}
}

// Stop halts the ticker and releases its goroutine. It does not close C, so a
// tick already buffered stays readable. Stop is idempotent and safe to call
// from any goroutine.
func (t *Ticker) Stop() {
	t.stopOnce.Do(func() { close(t.stop) })
}

// Reset changes the interval. It panics if d is not positive, and returns once
// the new interval is in effect. Reset on a stopped Ticker does nothing.
func (t *Ticker) Reset(d time.Duration) {
	if d <= 0 {
		panic("jitterx: non-positive interval for Ticker.Reset")
	}
	req := resetReq{d: d, ack: make(chan struct{})}
	select {
	case t.reset <- req:
		<-req.ack
	case <-t.stop:
	}
}

// Every calls fn on a jittered interval until fn returns an error or ctx ends,
// then returns that error or ctx.Err(). A nil strategy means Proportional with
// [DefaultTickerSpread].
//
// fn is not called immediately: the first call comes after the first interval.
func Every(ctx context.Context, interval time.Duration, s Strategy, fn func(context.Context) error) error {
	return every(ctx, defaultClock, interval, s, fn)
}

func every(ctx context.Context, clk clock, interval time.Duration, s Strategy, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	t := newTicker(clk, interval, s)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := fn(ctx); err != nil {
				return err
			}
		}
	}
}
