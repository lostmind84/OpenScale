package fake

import (
	"context"
	"testing"
	"time"

	"openscale/internal/station/ports"
)

var origin = time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)

// TestClockOnlyMovesWhenToldTo is the whole point: a frozen clock makes a temporal
// assertion reproducible, where a real one makes it a race.
func TestClockOnlyMovesWhenToldTo(t *testing.T) {
	clk := NewClock(origin)
	if !clk.Now().Equal(origin) {
		t.Fatalf("Now() = %v, want %v", clk.Now(), origin)
	}
	// A real clock would have moved by now. This one has not.
	if !clk.Now().Equal(origin) {
		t.Error("the clock moved on its own")
	}
	clk.Advance(400 * time.Millisecond)
	if want := origin.Add(400 * time.Millisecond); !clk.Now().Equal(want) {
		t.Errorf("Now() = %v, want %v", clk.Now(), want)
	}
}

// TestAfterFiresAtItsDeadlineAndNotBefore covers the boundary, because everything
// built on top of it uses >= or > deliberately.
func TestAfterFiresAtItsDeadlineAndNotBefore(t *testing.T) {
	clk := NewClock(origin)
	deadline := clk.After(time.Second)

	clk.Advance(999 * time.Millisecond)
	select {
	case instant := <-deadline:
		t.Fatalf("fired at %v, one millisecond early", instant)
	default:
	}

	clk.Advance(time.Millisecond)
	select {
	case instant := <-deadline:
		if want := origin.Add(time.Second); !instant.Equal(want) {
			t.Errorf("fired with %v, want the DEADLINE %v and not the current instant", instant, want)
		}
	default:
		t.Fatal("did not fire at its deadline")
	}
}

// TestAfterZeroFiresAtOnce: ports.WithBudget(ctx, clk, 0) must mean "already over"
// rather than "never", or a zero budget would hang instead of failing.
func TestAfterZeroFiresAtOnce(t *testing.T) {
	clk := NewClock(origin)
	for _, d := range []time.Duration{0, -time.Second} {
		select {
		case <-clk.After(d):
		default:
			t.Errorf("After(%v) did not fire immediately", d)
		}
	}
}

// TestAfterIsBufferedSoAdvanceNeverDeadlocks. Advance delivers while holding the
// lock; an unbuffered channel would deadlock against a caller that has not reached
// its receive yet — which is exactly what happens when a budget expires before the
// work it bounds gets scheduled.
func TestAfterIsBufferedSoAdvanceNeverDeadlocks(t *testing.T) {
	clk := NewClock(origin)
	for i := 0; i < 50; i++ {
		clk.After(time.Second)
	}
	done := make(chan struct{})
	go func() {
		clk.Advance(2 * time.Second) // must not block on fifty un-read waiters
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Advance deadlocked on waiters nobody had read yet")
	}
}

// TestTickerReallyProducesTheTwentyTicks is the claim §5.3 makes: with the fake
// clock, Advance(2*time.Second) on a 100 ms ticker REALLY produces the ticks, so
// every time-dependent test genuinely exercises what it says.
//
// It also pins the honest limit: the channel holds one, like a real time.Ticker, so a
// consumer that was busy receives the latest tick and not a backlog of twenty.
func TestTickerReallyProducesTheTwentyTicks(t *testing.T) {
	clk := NewClock(origin)
	ticks, stop := clk.Ticker(100 * time.Millisecond)
	defer stop()

	// Drained as they come: the twenty ticks are all observed.
	seen := 0
	for i := 0; i < 20; i++ {
		clk.Advance(100 * time.Millisecond)
		select {
		case <-ticks:
			seen++
		default:
		}
	}
	if seen != 20 {
		t.Errorf("%d ticks observed while draining, want 20", seen)
	}

	// Not drained: one jump of two seconds leaves ONE tick, like a real ticker.
	clk.Advance(2 * time.Second)
	pending := 0
	for {
		select {
		case <-ticks:
			pending++
			continue
		default:
		}
		break
	}
	if pending != 1 {
		t.Errorf("%d ticks pending after an undrained two-second jump, want 1 — a real "+
			"time.Ticker has a capacity of one and drops the rest", pending)
	}
}

func TestStoppedTickerStopsFiring(t *testing.T) {
	clk := NewClock(origin)
	ticks, stop := clk.Ticker(100 * time.Millisecond)
	stop()
	clk.Advance(time.Second)
	select {
	case <-ticks:
		t.Error("a stopped ticker fired")
	default:
	}
	if _, tickers := clk.Pending(); tickers != 0 {
		t.Errorf("%d live tickers after stop, want 0", tickers)
	}
}

func TestTickerRejectsANonPositiveInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Ticker(0) returned instead of panicking: a zero interval is a programming defect")
		}
	}()
	NewClock(origin).Ticker(0)
}

// TestClockRefusesToGoBackwardsThroughAdvance: Advance is monotonic by contract, so a
// negative jump is a defect and not a scenario. Set is the door for a clock that
// JUMPED, which §15.4 watches for as ERR-SYS-07.
func TestClockRefusesToGoBackwardsThroughAdvance(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Advance(-1s) returned instead of panicking")
		}
	}()
	NewClock(origin).Advance(-time.Second)
}

func TestSetMovesForwardsAndBackwards(t *testing.T) {
	clk := NewClock(origin)
	deadline := clk.After(time.Second)

	// Forwards past the deadline: it fires, exactly as Advance would.
	clk.Set(origin.Add(2 * time.Second))
	select {
	case <-deadline:
	default:
		t.Error("Set forwards did not fire a due waiter")
	}

	// Backwards: the system clock jumped, and nothing is due.
	back := origin.Add(-time.Hour)
	clk.Set(back)
	if !clk.Now().Equal(back) {
		t.Errorf("Now() = %v, want %v", clk.Now(), back)
	}
	later := clk.After(time.Second)
	select {
	case <-later:
		t.Error("a waiter fired after the clock jumped backwards")
	default:
	}
}

// TestWithBudgetIsMeasuredByTheInjectedClock is failure test 6 of §16.2 in miniature:
// a printer hanging for sixty seconds must be cancelled at its eight-second budget, in
// MICROSECONDS of wall time. Burning eight real seconds would break the ten-second
// budget of the whole race-enabled suite.
func TestWithBudgetIsMeasuredByTheInjectedClock(t *testing.T) {
	clk := NewClock(origin)
	started := time.Now()

	ctx, cancel := ports.WithBudget(context.Background(), clk, 8*time.Second)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("the budget expired before the clock moved")
	default:
	}

	clk.Advance(8 * time.Second)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the budget did not expire when the injected clock passed it")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Errorf("wall time %v: an eight-second budget must cost microseconds, not seconds", elapsed)
	}
}

// TestWithBudgetLeavesNoWaiterBehind: §13.1 claims the goroutine inventory is
// exhaustive, and names the one WithBudget spawns as TRANSIENT. A cancelled budget
// must therefore leave nothing pending.
func TestWithBudgetLeavesNoWaiterBehind(t *testing.T) {
	clk := NewClock(origin)
	for i := 0; i < 10; i++ {
		_, cancel := ports.WithBudget(context.Background(), clk, time.Minute)
		cancel()
	}
	// The waiters themselves stay registered until their deadline — that is the fake's
	// bookkeeping, not a leak — but nothing must fire into a cancelled context.
	waiters, _ := clk.Pending()
	if waiters != 10 {
		t.Errorf("%d waiters, want the 10 registered", waiters)
	}
	clk.Advance(2 * time.Minute)
	if waiters, _ = clk.Pending(); waiters != 0 {
		t.Errorf("%d waiters after their deadline, want 0", waiters)
	}
}

// TestClockIsSafeUnderConcurrentUse: the Hub reads it from its loop goroutine while a
// test advances it from another. Run with -race, this is the assertion.
func TestClockIsSafeUnderConcurrentUse(t *testing.T) {
	clk := NewClock(origin)
	ticks, stop := clk.Ticker(10 * time.Millisecond)
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = clk.Now()
			select {
			case <-ticks:
			default:
			}
		}
	}()
	for i := 0; i < 200; i++ {
		clk.Advance(time.Millisecond)
	}
	<-done
}

// TestFakeAndRealClockShareOneContract: if the two ever diverge, every temporal test
// in the repository is measuring the fake and nothing else.
func TestFakeAndRealClockShareOneContract(t *testing.T) {
	var _ ports.Clock = NewClock(origin)
	// The real one is asserted in its own package; here we check the fake honours the
	// same three-method shape at run time.
	var clk ports.Clock = NewClock(origin)
	if !clk.Now().Equal(origin) {
		t.Error("Now through the interface disagrees with the concrete type")
	}
	if clk.After(time.Second) == nil {
		t.Error("After returned a nil channel")
	}
	ch, stop := clk.Ticker(time.Second)
	if ch == nil || stop == nil {
		t.Error("Ticker returned a nil channel or a nil stop function")
	}
	stop()
}
