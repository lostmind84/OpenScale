package conformance

import (
	"context"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// THE EXITS — clauses 5, 7 and 8: Close is idempotent because the Hub closes on a reload
// and again on shutdown; a context already cancelled BEFORE Start is a real start-up
// race, a reload overlapping a shutdown, and not a theoretical one; and no goroutine
// survives the driver.

// checkCloseIsIdempotent covers both calls the Hub really makes: the one after a
// failed Start, and the one on shutdown that follows a reload.
func checkCloseIsIdempotent(t *testing.T, r reporter, subject Subject) {
	r.Helper()

	// A driver that was never started. The Hub reaches this every time Start fails: it
	// builds, it fails, it closes.
	unstarted := build(t, r, subject.New, fake.NewClock(t0))
	for call := 1; call <= 3; call++ {
		if _, panicked := closeQuietly(unstarted); panicked != nil {
			r.Fatalf("Close PANICKED on call %d of a driver that was never started: %v. Close releases what was taken and says nothing about what was not", call, panicked)
		}
	}

	// And a driver that ran. Returning an error on the second call is allowed — a
	// handle already released is not news, and the Hub logs it as ERR-SCL-08 — but a
	// panic takes the whole station down with it.
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)
	session.quiesce(r)
	for call := 1; call <= 3; call++ {
		if _, panicked := closeQuietly(session.scale); panicked != nil {
			r.Fatalf("Close PANICKED on call %d after a normal exit: %v. The Hub closes on a reload and again on shutdown (§11.4, §13.4)", call, panicked)
		}
	}
}

// checkStartSurvivesACancelledContext is the start-up race of a reload that overlaps
// the shutdown of the driver it replaces.
//
// Start is free to return an error or nil here — the context is already dead, both
// are honest. What it may not do is panic, leave done open, or close out.
func checkStartSurvivesACancelledContext(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	session := newSession(t, r, subject, dead)
	defer session.release()
	if !waitClosed(session.done, session.patience) {
		r.Fatalf("done was still open %s after Start was handed a context that was ALREADY cancelled. Start returned %v; whichever way it leaves, it closes done (§5.3)", session.patience, session.startErr)
	}
	session.stopCollecting()

	if session.sawOutClosed() {
		r.Errorf("the driver closed out on the cancelled-context path. out belongs to the Hub on every path, this one included (bloquant-2)")
	}
	if events := session.collected(); len(events) > 0 {
		if last := events[len(events)-1]; last.Status != domain.StatusDisconnected {
			r.Errorf("the last event has Status = %s, want %s: whatever path a driver leaves by, it leaves Disconnected (défaut 40)", last.Status, domain.StatusDisconnected)
		}
	}
	if _, panicked := closeQuietly(session.scale); panicked != nil {
		r.Fatalf("Close PANICKED after a start on a cancelled context: %v", panicked)
	}
}

// checkNoGoroutineLeaks compares a DIFFERENCE and not an absolute count.
//
// An absolute number would be worthless: the test binary runs goroutines of its own,
// and the runtime may still be retiring those of the previous check. What is asserted
// is that the count comes back to where it was, once done has been closed and Close
// has returned — which is why the suite waits for those two before looking.
func checkNoGoroutineLeaks(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	before := settledGoroutines(subject.patience())

	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)
	session.stop(r)

	if !waitUntil(func() bool { return goroutines() <= before }, session.patience) {
		r.Errorf("goroutines went from %d to %d and stayed there for %s after done was closed and Close returned. §13.1 claims the inventory of goroutines is exhaustive, and it is only true if every driver takes its own away:\n%s",
			before, goroutines(), session.patience, goroutineDump())
	}
	if _, tickers := session.clock.Pending(); tickers > 0 {
		r.Errorf("%d ticker(s) of the injected clock are still running after Close. The stop function that Clock.Ticker returns is not optional: a ticker nobody stops is a leak the goroutine count cannot always see (§13.1)", tickers)
	}
}
