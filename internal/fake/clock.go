// Package fake holds the test doubles the whole repository shares.
//
// There is no mocking framework here and there will not be one: everything that
// DECIDES in this application is pure, and everything with a side effect is trivial,
// so there is nothing to simulate. What is left is a clock somebody has to advance by
// hand, a scale somebody has to push frames into, and a printer somebody has to read
// jobs out of.
package fake

import (
	"sync"
	"time"

	"openscale/internal/station/ports"
)

// Clock is a clock that only moves when a test tells it to.
//
// This is the component the whole temporal test strategy rests on. With it,
// Advance(2*time.Second) REALLY produces the twenty 100 ms ticks the Hub expects, so
// stability, expiry, interface timeouts, the reprint window and the print budget are
// genuinely exercised — in microseconds of wall time. Without it, a test that wants to
// observe a 5-second expiry either sleeps five seconds or tests nothing, and §16.4
// budgets the WHOLE race-enabled suite at ten seconds.
//
// It is safe for concurrent use: the Hub reads it from its loop goroutine while a test
// advances it from another.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	tickers []*fakeTicker
}

// waiter is one pending After.
type waiter struct {
	at time.Time
	ch chan time.Time
}

// fakeTicker is one pending Ticker, which fires repeatedly.
type fakeTicker struct {
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  bool
}

// NewClock returns a clock frozen at start.
func NewClock(start time.Time) *Clock { return &Clock{now: start} }

// Now reports the instant the clock is frozen at.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After registers a one-shot waiter.
//
// The channel is BUFFERED, and it has to be: Advance delivers while holding the lock,
// and an unbuffered send would deadlock against a caller that has not reached its
// receive yet — which is precisely what happens when a budget expires before the work
// it bounds gets scheduled.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	deadline := c.now.Add(d)
	if d <= 0 {
		// A non-positive duration has already elapsed. Firing at once rather than
		// waiting for the next Advance keeps ports.WithBudget(0) meaningful.
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at: deadline, ch: ch})
	return ch
}

// Ticker registers a repeating waiter and returns it with its stop function.
func (c *Clock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	if d <= 0 {
		panic("fake: a ticker interval must be positive")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTicker{interval: d, next: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.tickers = append(c.tickers, t)
	return t.ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		t.stopped = true
	}
}

// Advance moves the clock forward and fires everything that comes due.
//
// It fires tickers REPEATEDLY: advancing two seconds on a 100 ms ticker delivers as
// many ticks as the channel can hold, and drops the rest. Dropping is the honest
// model of a real ticker, whose channel has a capacity of one and which documents
// exactly that behaviour — a Hub that was busy for two seconds did not receive twenty
// ticks in the real world either.
func (c *Clock) Advance(d time.Duration) {
	if d < 0 {
		panic("fake: a clock does not go backwards")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)

	// One-shot waiters that are now due.
	remaining := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- w.at
			continue
		}
		remaining = append(remaining, w)
	}
	c.waiters = remaining

	// Tickers, as many times as they fit inside the jump.
	for _, t := range c.tickers {
		if t.stopped {
			continue
		}
		for !t.next.After(c.now) {
			select {
			case t.ch <- t.next:
			default: // capacity 1, like a real ticker: the stale tick is dropped
			}
			t.next = t.next.Add(t.interval)
		}
	}
}

// Set moves the clock to an absolute instant.
//
// It exists for one scenario the application must survive: a system clock that JUMPS,
// which §15.4 watches for and reports as ERR-SYS-07. A journal timestamp only has
// value for reconciliation with the till if the hour is right, and no NTP dependency
// is guaranteed offline.
func (c *Clock) Set(instant time.Time) {
	c.mu.Lock()
	behind := instant.Before(c.now)
	c.mu.Unlock()
	if behind {
		// Backwards: nothing fires, because nothing came due. Waiters registered for a
		// later instant simply wait longer.
		c.mu.Lock()
		c.now = instant
		c.mu.Unlock()
		return
	}
	c.Advance(instant.Sub(c.Now()))
}

// Pending reports how many one-shot waiters and live tickers are registered.
//
// It is what lets a test assert that nothing was leaked: ports.WithBudget spawns a
// transient goroutine per call, and §13.1 claims the inventory of goroutines is
// exhaustive.
func (c *Clock) Pending() (waiters, tickers int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tickers {
		if !t.stopped {
			tickers++
		}
	}
	return len(c.waiters), tickers
}

// Compile-time proof that the fake satisfies the same contract as the real clock.
var _ ports.Clock = (*Clock)(nil)
