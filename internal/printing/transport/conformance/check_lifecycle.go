package conformance

// This file holds clauses 7 to 11: what a cancelled context leaves behind, what Query
// may claim, and the exits — a Close the print service calls twice, a write that arrives
// after it, and the goroutine count that has to come back to where it started.

import (
	"context"
	"errors"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// checkCancelledContextWritesNothing is the cheap half of failure test 6: a job that
// arrives after the budget has already burnt must not reach the head.
func checkCancelledContextWritesNothing(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	dead, cancel := context.WithCancel(context.Background())
	cancel()

	n, err := tr.Write(dead, payload)
	if !errors.Is(err, context.Canceled) {
		r.Errorf("Write on an ALREADY cancelled context returned (%d, %v), want a context error. The 8 s budget of the print service arrives as this context (§8.2); a transport that writes anyway is a second label going out after the Hub gave up on the first", n, err)
	}
	if n != 0 {
		r.Errorf("Write reported %d bytes on a cancelled context. A job the printer received part of is a job that failed, and a non-zero count invites the caller to read it as progress", n)
	}
	if subject.Delivered != nil {
		if got := subject.Delivered(t, tr); len(got) > 0 {
			r.Errorf("the context was cancelled before Write and %d bytes reached the destination anyway: %x", len(got), got)
		}
	}
}

// checkCancelDuringWriteLeavesNothing is failure test 6 itself, « imprimante qui pend
// 60 s » (§16.2): the caller gets the floor back, and nothing is left running.
//
// The two halves matter equally. Returning is what keeps the Hub alive; leaving nothing
// behind is what keeps the NEXT weighing alive, because the goroutine this would leak
// holds the mutex the print service serializes on (§8.2).
func checkCancelDuringWriteLeavesNothing(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Blocking == nil {
		r.Skipf("Subject.Blocking is nil: this subject offers no destination that parks a write. Supply it for anything that talks to a device — it is failure test 6, and it is the clause whose breach blocks the whole station and not just one label")
	}
	before := settledGoroutines(subject.patience())

	tr := build(t, r, subject.Blocking, fake.NewClock(t0))
	defer closeAndForget(tr)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		n   int
		err error
	}
	returned := make(chan outcome, 1)
	go func() {
		n, err := tr.Write(ctx, payload)
		returned <- outcome{n, err}
	}()

	// Let the write reach the destination and PARK there before pulling the rug out.
	// Cancelling a Write that has not started yet would silently re-run the previous
	// check and credit this one with it.
	//
	// Two goroutines, not one: the one just launched above, and the one the transport
	// spawns to hold the write that no context can interrupt. Seeing the second is what
	// says the write is past its entry guard and inside the destination.
	if !waitUntil(func() bool { return goroutines() >= before+2 }, subject.patience()) {
		r.Logf("the blocking write never showed up as a goroutine of its own; cancelling anyway")
	}
	select {
	case got := <-returned:
		r.Fatalf("Write came back on its own with (%d, %v), before anything was cancelled. Subject.Blocking has to build a transport whose write PARKS until the handle is closed — the printer of failure test 6, which hangs for sixty seconds — or this check verifies nothing", got.n, got.err)
	default:
	}
	cancel()

	select {
	case got := <-returned:
		if !errors.Is(got.err, context.Canceled) {
			r.Errorf("Write returned (%d, %v) after its context was cancelled, want a context error", got.n, got.err)
		}
		if got.n != 0 {
			r.Errorf("Write reported %d bytes after being cancelled mid-flight", got.n)
		}
	case <-realClock.After(subject.patience()):
		r.Fatalf("Write was STILL RUNNING %s after its context was cancelled. Nothing this application writes to honours a context on its own — not os.File, not net.Conn, not WritePrinter — so the transport has to close the handle to unblock it. Without that, failure test 6 freezes the print service and the Hub behind it (§16.2)\n%s", subject.patience(), goroutineDump())
	}

	closeAndForget(tr)
	if !waitUntil(func() bool { return goroutines() <= before }, subject.patience()) {
		r.Errorf("goroutines went from %d to %d and stayed there for %s after a cancelled write. Giving the caller the floor back is only half of it: the write goroutine has to be GONE, because §13.1 claims the inventory of goroutines is exhaustive and because the handle it holds is the one the next label needs\n%s",
			before, goroutines(), subject.patience(), goroutineDump())
	}
}

// checkQueryAnswersOrDeclares holds a transport to the honesty of §8.5: an unknown
// status is a legitimate answer, a pretended one is not.
func checkQueryAnswersOrDeclares(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	clk := fake.NewClock(t0)
	tr := build(t, r, subject.New, clk)
	defer closeAndForget(tr)

	raw, err, returned := probe(tr, clk, subject.patience())
	if !returned {
		r.Fatalf("Query was still running %s after the injected clock passed its %s budget. The budget of the native probe is measured on the clock the transport was GIVEN (§5.3): a transport that timed itself on the wall clock would hang the troubleshooting screen here and burn half a second per call in production\n%s", subject.patience(), probeBudget, goroutineDump())
	}

	if !subject.Bidirectional {
		if !errors.Is(err, ports.ErrUnsupported) {
			r.Errorf("Query returned (%x, %v) on a transport submitted as ONE-WAY, want an error wrapping ports.ErrUnsupported. That sentinel is what lets the printer driver fall back to level N1 instead of showing a volunteer the result of a probe that never happened (§8.5)", raw, err)
		}
		return
	}
	if errors.Is(err, ports.ErrUnsupported) {
		r.Errorf("Query declined with ports.ErrUnsupported on a transport submitted as BIDIRECTIONAL. Set Subject.Bidirectional to false, or carry the probe: a subject that does not match its transport makes every other check ambiguous")
	}
	if err == nil && len(raw) == 0 {
		r.Logf("Query answered nothing within the budget, which §8.5 reads as « on ne sait pas » and not as a failure")
	}
}

// checkCloseIsIdempotent covers both calls the print service really makes: the one on a
// configuration reload and the one on shutdown.
func checkCloseIsIdempotent(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))

	for call := 1; call <= 3; call++ {
		err, panicked := closeQuietly(tr)
		if panicked != nil {
			r.Fatalf("Close PANICKED on call %d: %v. The print service closes on a reload and again on shutdown (§11.4, §13.4), and a panic there takes the whole station down", call, panicked)
		}
		if err != nil {
			// Allowed and logged rather than judged: a handle already released is not news.
			r.Logf("Close returned %v on call %d", err, call)
		}
	}
}

// checkWriteAfterCloseIsRefused keeps a station that has given up from being brought back
// by a job that arrived late.
func checkWriteAfterCloseIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	if _, panicked := closeQuietly(tr); panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}

	n, err := tr.Write(context.Background(), payload)
	if err == nil {
		r.Errorf("Write reported %d bytes and no error AFTER Close. A reload is a close followed by a new transport (§11.4): a job that reopens the device behind the closed one prints on hardware the station believes it has released, and two transports then race for the same handle", n)
	}
	if n != 0 {
		r.Errorf("Write reported %d bytes accepted after Close", n)
	}
	if subject.Delivered != nil {
		if got := subject.Delivered(t, tr); len(got) > 0 {
			r.Errorf("%d bytes reached the destination after Close: %x", len(got), got)
		}
	}
}

// checkNoGoroutineLeaks compares a DIFFERENCE and not an absolute count: the test binary
// runs goroutines of its own, and the runtime may still be retiring those of the previous
// check.
func checkNoGoroutineLeaks(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	before := settledGoroutines(subject.patience())

	clk := fake.NewClock(t0)
	tr := build(t, r, subject.New, clk)
	if _, err := tr.Write(context.Background(), payload); err != nil {
		r.Fatalf("Write returned %v on a destination the subject declares healthy", err)
	}
	if _, _, returned := probe(tr, clk, subject.patience()); !returned {
		r.Fatalf("Query never came back; the leak this check looks for is hidden behind it\n%s", goroutineDump())
	}
	closeAndForget(tr)

	if !waitUntil(func() bool { return goroutines() <= before }, subject.patience()) {
		r.Errorf("goroutines went from %d to %d and stayed there for %s after a whole job and a Close. §13.1 claims the inventory of goroutines is exhaustive, and it is only true if every transport takes its own away:\n%s",
			before, goroutines(), subject.patience(), goroutineDump())
	}
	if _, tickers := clk.Pending(); tickers > 0 {
		r.Errorf("%d ticker(s) of the injected clock are still running after Close. The stop function that Clock.Ticker returns is not optional: a ticker nobody stops is a leak the goroutine count cannot always see (§13.1)", tickers)
	}
}
