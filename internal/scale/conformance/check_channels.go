package conformance

import (
	"context"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// THE TWO CHANNELS — clauses 2 and 3, and they are the two that have already frozen a
// station.
//
// out belongs to the Hub for the whole lifetime of the process: a driver that closes it
// breaks the serial -> manual -> serial round trip and makes the degraded state
// IRREVERSIBLE (bloquant-2). done is closed on EVERY exit path, a Start that returned an
// error included — otherwise the wait in restartScale never unblocks, and the volunteer
// who changed a setting watches a screen that never answers (§11.4).

// checkOutIsNeverClosed is bloquant-2, and it is the clause with the worst
// consequence: a driver that closes the Hub's channel makes the manual fallback
// permanent, because the channel it should come back on no longer exists.
func checkOutIsNeverClosed(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)
	session.stop(r)

	if session.sawOutClosed() {
		r.Fatalf("the driver CLOSED out. That channel belongs to the Hub for the lifetime of the process; closing it breaks the serial -> manual -> serial round trip and makes the degraded state irreversible (bloquant-2). Signal your own termination by closing done, which is yours")
	}
	if !session.outStillAcceptsASend() {
		r.Errorf("out no longer accepts a send once the driver is gone: it was closed. The Hub hands THIS SAME channel to the next driver, which is what makes the return to serial possible (bloquant-2)")
	}
}

// checkDoneClosesWhenTheContextEnds verifies the exit the Hub uses on every reload.
func checkDoneClosesWhenTheContextEnds(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)

	session.cancel()
	if !waitClosed(session.done, session.patience) {
		r.Fatalf("done was still open %s after the context was cancelled. Start publishes until ctx is done and THEN closes done (§5.3); the wait in restartScale is what would never unblock, and PUT /admin/api/config would never answer (§11.4)", session.patience)
	}
}

// checkDoneClosesWhenStartFails is the mandatory corollary, and it is test de panne
// 1 ter (b): a driver that returns an error before it ever launched its goroutine
// still owes the Hub a closed done.
func checkDoneClosesWhenStartFails(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Unstartable == nil {
		r.Skipf("Subject.Unstartable is nil: this driver declares no way for Start to fail. Supply it as soon as the driver opens a device — a port name that does not exist is enough — because this is the clause whose breach makes the configuration screen hang (§11.4)")
	}
	scale := build(t, r, subject.Unstartable, fake.NewClock(t0))
	defer closeAndForget(scale)

	out := make(chan domain.ScaleEvent, outBuffer)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err, panicked := startQuietly(scale, ctx, out, done)
	if panicked != nil {
		r.Fatalf("Start PANICKED instead of returning an error: %v. A port that is not there is an ordinary Tuesday, not a programming error", panicked)
	}
	if err == nil {
		r.Errorf("Start SUCCEEDED on the driver Subject.Unstartable built, so the subject is not what it declares; cancelling and checking done anyway")
		cancel()
	}
	if !waitClosed(done, subject.patience()) {
		r.Fatalf("Start returned %v and left done OPEN. done is closed on EVERY exit path, this one included: the wait in restartScale would never unblock, the configuration would never be written, and a volunteer would be left with a screen that does not answer (§11.4, test de panne 1 ter b)", err)
	}
}
