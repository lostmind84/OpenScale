package conformance

// The test of the tests. A conformance suite that nothing can fail verifies nothing, so
// this file submits one healthy driver and twenty-nine broken ones, and asserts WHICH
// check catches WHICH betrayal.

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// faultPatience is the wall-clock budget the broken-driver table runs on.
//
// Several of those drivers hang ON PURPOSE — a context nobody listens to, a done left
// open — and at the default two seconds they would spend a minute proving what they
// prove in a tenth of a second. §16.4 budgets the WHOLE race-enabled suite at ten
// seconds, and that is a design criterion rather than a wish.
const faultPatience = 100 * time.Millisecond

// decodeBudget is how long Subject.Feed gives the reference decoder to publish what it
// decoded.
//
// It is a quarter of faultPatience because it bounds something else entirely: a channel
// handoff between two goroutines of one process, not a device releasing a handle. The
// three broken drivers that publish nothing on purpose spend this budget once per check,
// and that is what keeps the whole table under a second.
const decodeBudget = 25 * time.Millisecond

// referenceSubject submits the healthy driver exactly the way a contributor submits
// theirs — which makes this function the example to copy.
func referenceSubject() Subject {
	return Subject{
		Name: "stub-scale",
		New: func(t *testing.T, clk ports.Clock) ports.Scale {
			return newStub(clk)
		},
		Unstartable: func(t *testing.T, clk ports.Clock) ports.Scale {
			s := newStub(clk)
			s.startError = errors.New("stub: COM255 does not exist")
			return s
		},
		Feed: func(t *testing.T, s ports.Scale, raw []byte) {
			stub := s.(*stubScale)
			stub.feed(raw)
			stub.awaitPublished(nominalFrameCount, decodeBudget)
			// Advancing the injected clock is allowed here, and this is what it is for:
			// it widens the window the timestamps are checked against, so that "the
			// instants go backwards" becomes a distinguishable fault instead of hiding
			// behind "the instant is outside the window".
			stub.advance(stub.descriptor.NominalRate)
		},
		Frames: nominalFrames,
		// The reference driver holds itself to the tightened contract of §9.1, so the
		// suite is asked to check it.
		RequireDisconnectCause: true,
	}
}

// TestSuiteAcceptsTheReferenceDriver is the first half of the demonstration: the suite
// is passable, and passable by a driver of about a hundred lines.
//
// It also exercises Suite itself — the subject validation and the sub-test wiring — and
// not merely the checks, which is what the recorder-driven tests below do.
func TestSuiteAcceptsTheReferenceDriver(t *testing.T) {
	Suite(t, referenceSubject())
}

// TestSuiteRejectsEveryBrokenDriver is the other half, and the one that matters: each
// broken driver betrays exactly one clause, and the check named beside it must be the
// one that says so.
func TestSuiteRejectsEveryBrokenDriver(t *testing.T) {
	for _, tc := range []struct {
		betrayal string
		// check is the name of the check that MUST catch it. Others may also fail — a
		// driver that ignores its context fails almost everything — but this one may not
		// stay silent.
		check   string
		subject Subject
	}{
		{"an empty ID", "Descriptor", brokenSubject(func(s *stubScale) { s.descriptor.ID = "" })},
		{"an empty label", "Descriptor", brokenSubject(func(s *stubScale) { s.descriptor.Label = "" })},
		{"a blank inside the ID", "Descriptor", brokenSubject(func(s *stubScale) { s.descriptor.ID = "stub scale" })},
		{"an upper-case letter inside the ID", "Descriptor", brokenSubject(func(s *stubScale) { s.descriptor.ID = "stub-Scale" })},
		{"no nominal rate", "Descriptor", brokenSubject(func(s *stubScale) { s.descriptor.NominalRate = 0 })},
		{"an identity that moves between two calls", "Descriptor", brokenSubject(func(s *stubScale) { s.unstableID = true })},

		{"closing the channel of the Hub on the way out", "OutIsNeverClosed", brokenSubject(func(s *stubScale) { s.closesOut = true })},
		{"closing the channel of the Hub inside Close", "OutIsNeverClosed", brokenSubject(func(s *stubScale) { s.closesOutOnClose = true })},
		{"a driver Subject.New cannot even start", "OutIsNeverClosed", brokenSubject(func(s *stubScale) { s.startError = errors.New("stub: COM255 does not exist") })},
		{"a Start that panics where Subject.New promised a healthy driver", "OutIsNeverClosed", brokenSubject(func(s *stubScale) { s.panicsOnStart = true })},

		{"a context nobody listens to", "DoneClosesWhenTheContextEnds", brokenSubject(func(s *stubScale) { s.ignoresCancel = true })},
		{"a failed Start that leaves done open", "DoneClosesWhenStartFails", unstartableSubject(func(s *stubScale) { s.leavesDoneOpen = true })},
		{"a Start that panics instead of failing", "DoneClosesWhenStartFails", unstartableSubject(func(s *stubScale) { s.panicsOnStart = true })},
		{"a Start that succeeds where it was declared unstartable", "DoneClosesWhenStartFails", unstartableSubject(func(s *stubScale) { s.startError = nil })},

		{"an exit that leaves the last reading standing", "LastEventIsDisconnected", brokenSubject(func(s *stubScale) { s.silentExit = true })},
		{"an exit with no loggable cause", "LastEventIsDisconnected", brokenSubject(func(s *stubScale) { s.noCause = true })},
		{"an exit that reports itself still connected", "LastEventIsDisconnected", brokenSubject(func(s *stubScale) { s.exitsConnected = true })},
		{"a driver that publishes nothing at all", "LastEventIsDisconnected", brokenSubject(func(s *stubScale) { s.silentExit, s.dropMeasurements = true, true })},

		{"a Close that panics on the second call", "CloseIsIdempotent", brokenSubject(func(s *stubScale) { s.panicsOnSecondClose = true })},
		{"a Close that panics once the driver has run", "CloseIsIdempotent", brokenSubject(func(s *stubScale) { s.panicsOnceStarted = true })},

		{"a timestamp from the wall clock", "MeasurementsAreCoherent", brokenSubject(func(s *stubScale) { s.wallClock = true })},
		{"a zero timestamp", "MeasurementsAreCoherent", brokenSubject(taint(func(m *domain.Measurement) { m.Timestamp = time.Time{} }))},
		{"instants that go backwards", "MeasurementsAreCoherent", brokenSubject(backwardsTimestamps())},
		{"a mass the grammar cannot express", "MeasurementsAreCoherent", brokenSubject(taint(func(m *domain.Measurement) { m.Gross = MaxExpressibleGrams + 1 }))},
		{"a stability outside the vocabulary", "MeasurementsAreCoherent", brokenSubject(taint(func(m *domain.Measurement) { m.Stability = 42 }))},
		{"a measurement carried by a disconnected event", "MeasurementsAreCoherent", brokenSubject(measurementOnADisconnect())},
		{"a decoder that drops everything", "MeasurementsAreCoherent", brokenSubject(func(s *stubScale) { s.dropMeasurements = true })},

		{"a goroutine nothing stops", "NoGoroutineLeaks", brokenSubject(func(s *stubScale) { s.leaked = make(chan struct{}) })},
		{"a ticker nobody stops", "NoGoroutineLeaks", brokenSubject(func(s *stubScale) { s.leavesATickerRunning = true })},
	} {
		t.Run(tc.betrayal, func(t *testing.T) {
			failed := runChecks(t, tc.subject)
			if !slices.Contains(failed, tc.check) {
				t.Fatalf("%s went through check %q unnoticed; the checks that did fail: %v", tc.betrayal, tc.check, failed)
			}
		})
	}
}

// TestSuiteAcceptsTheMinimalSubmission is the other end of the range: Name and New, and
// nothing else.
//
// A contributor who cannot inject bytes — no loopback, no pipe, a device that is only
// ever real — still gets every clause that needs no device, and gets it without writing
// a line of harness. That is what keeps the suite from being something people skip.
func TestSuiteAcceptsTheMinimalSubmission(t *testing.T) {
	minimal := Subject{Name: "stub-scale", New: referenceSubject().New, Patience: faultPatience}
	if failed := runChecks(t, minimal); len(failed) > 0 {
		t.Fatalf("the minimal submission failed %v", failed)
	}
}

// TestSettledGoroutinesGivesUp keeps the baseline of the leak check from becoming a
// hang: on a machine whose goroutine count never settles, it answers with what it last
// saw instead of waiting for a quiet that is not coming.
func TestSettledGoroutinesGivesUp(t *testing.T) {
	if n := settledGoroutines(time.Nanosecond); n <= 0 {
		t.Fatalf("settledGoroutines = %d, want the live count", n)
	}
}

// TestCloseMayReturnAnError draws the line the contract draws: Close may fail, it may
// not panic.
//
// A handle already released is not news — the Hub logs it as ERR-SCL-08 and carries on
// with the manual fallback (§11.4) — so a driver that reports it must not be told it is
// non-conformant.
func TestCloseMayReturnAnError(t *testing.T) {
	subject := brokenSubject(func(s *stubScale) { s.closeError = errors.New("stub: handle already released") })
	if failed := runChecks(t, subject); len(failed) > 0 {
		t.Fatalf("a Close that returns an error failed %v; only a panic is a breach", failed)
	}
}

// TestStartFailureCheckSkipsWithoutAnUnstartableDriver asserts that the missing hook
// makes the check report itself SKIPPED and never PASSED.
//
// The difference is the whole value of the check: a driver silently credited with a
// clause nobody verified is worse than a red line, because §11.4 is exactly where it
// would be discovered — on a volunteer's screen, on a morning when something else is
// already broken.
func TestStartFailureCheckSkipsWithoutAnUnstartableDriver(t *testing.T) {
	subject := referenceSubject()
	subject.Unstartable = nil

	rec := &recorder{}
	guard(func() { checkDoneClosesWhenStartFails(t, rec, subject) })

	if !rec.wasSkipped() {
		t.Fatalf("the check did not skip; it must never pass by default")
	}
	if rec.failed() {
		t.Fatalf("the check failed instead of skipping: %v", rec.reasons())
	}
}

// TestSuiteRefusesAnInconsistentSubject covers the harness's own guard rails: a subject
// the suite could only misjudge is refused before a single driver is built, because
// every failure after that would blame the driver for the harness's mistake.
func TestSuiteRefusesAnInconsistentSubject(t *testing.T) {
	healthy := referenceSubject()
	for _, tc := range []struct {
		name    string
		subject Subject
	}{
		{"no name", Subject{New: healthy.New}},
		{"no constructor", Subject{Name: "nameless"}},
		{"a Feed with nothing to feed", Subject{Name: "starving", New: healthy.New, Feed: healthy.Feed}},
		{"frames that reach nothing", Subject{Name: "mute", New: healthy.New, Frames: nominalFrames}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			guard(func() { validate(rec, tc.subject) })
			if !rec.failed() {
				t.Fatalf("the suite accepted a subject with %s", tc.name)
			}
		})
	}
}

// TestSuiteRefusesANilDriver keeps a nil constructor result from surfacing three frames
// deeper as a nil dereference in the middle of a check.
func TestSuiteRefusesANilDriver(t *testing.T) {
	subject := referenceSubject()
	subject.New = func(t *testing.T, clk ports.Clock) ports.Scale { return nil }

	rec := &recorder{}
	guard(func() { checkDescriptor(t, rec, subject) })
	if !rec.failed() {
		t.Fatalf("the suite accepted a nil ports.Scale")
	}
}

// --- the harness of this file ----------------------------------------------

// brokenSubject is the reference subject with one betrayal applied to every driver
// Subject.New builds.
func brokenSubject(betray func(*stubScale)) Subject {
	subject := referenceSubject()
	subject.Patience = faultPatience
	subject.New = func(t *testing.T, clk ports.Clock) ports.Scale {
		s := newStub(clk)
		betray(s)
		if s.leaked != nil {
			// The leak has to be REAL for the check to have anything to catch, and it has
			// to end with the sub-test, or the next case starts from a wrong baseline.
			t.Cleanup(func() { close(s.leaked) })
		}
		return s
	}
	return subject
}

// unstartableSubject applies the betrayal to the driver Subject.Unstartable builds,
// which is the only one whose Start is expected to fail.
func unstartableSubject(betray func(*stubScale)) Subject {
	subject := referenceSubject()
	subject.Patience = faultPatience
	subject.Unstartable = func(t *testing.T, clk ports.Clock) ports.Scale {
		s := newStub(clk)
		s.startError = errors.New("stub: COM255 does not exist")
		betray(s)
		return s
	}
	return subject
}

// taint is the shorthand for the coherence betrayals: one field of every published
// measurement, rewritten on its way out.
func taint(corrupt func(*domain.Measurement)) func(*stubScale) {
	return func(s *stubScale) {
		s.taint = func(event domain.ScaleEvent) domain.ScaleEvent {
			corrupt(event.Measurement)
			return event
		}
	}
}

// backwardsTimestamps makes each reading older than the one before it while keeping all
// of them INSIDE the window the injected clock covered, so that only the monotonicity
// clause can catch them.
func backwardsTimestamps() func(*stubScale) {
	return func(s *stubScale) {
		offset := 3 * time.Millisecond
		s.taint = func(event domain.ScaleEvent) domain.ScaleEvent {
			event.Measurement.Timestamp = t0.Add(offset)
			offset -= time.Millisecond
			return event
		}
	}
}

// measurementOnADisconnect makes a single event both deliver a weight and take the
// scale away.
func measurementOnADisconnect() func(*stubScale) {
	return func(s *stubScale) {
		s.taint = func(event domain.ScaleEvent) domain.ScaleEvent {
			event.Status = domain.StatusDisconnected
			return event
		}
	}
}

// runChecks runs every check of the suite against subject through a recorder, and
// returns the names of those that handed down a failure.
func runChecks(t *testing.T, subject Subject) []string {
	t.Helper()
	var failed []string
	for _, c := range checks() {
		rec := &recorder{}
		guard(func() { c.run(t, rec, subject) })
		if rec.failed() {
			failed = append(failed, c.name)
		}
	}
	return failed
}

// guard runs fn and swallows the sentinel that a recording Fatalf panics with. It is how
// the double imitates the runtime.Goexit of the real *testing.T without taking down the
// test that is watching.
func guard(fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, sentinel := recovered.(abort); sentinel {
				return
			}
			panic(recovered)
		}
	}()
	fn()
}

// abort is what a recording Fatalf or Skipf panics with.
type abort struct{}

// recorder is the double that lets this package assert that the suite FAILED.
//
// It is not a testing.TB, and it cannot be: that interface carries an unexported method
// precisely so that nobody fakes it. That is the whole reason the checks judge through
// the narrow reporter interface — see the type comment in conformance.go.
type recorder struct {
	mu       sync.Mutex
	failures []string
	skipped  bool
}

// Compile-time proof that the double judges through the same interface as *testing.T.
var _ reporter = (*recorder)(nil)

// Helper does nothing: there is no stack to prune when nobody prints the frame.
func (*recorder) Helper() {}

// Logf drops what the real reporter would print.
func (*recorder) Logf(string, ...any) {}

// Errorf records a failure and lets the check carry on, like the real one.
func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// Fatalf records a failure and aborts the check.
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(abort{})
}

// Skipf marks the check skipped and aborts it, without recording a failure.
func (r *recorder) Skipf(string, ...any) {
	r.mu.Lock()
	r.skipped = true
	r.mu.Unlock()
	panic(abort{})
}

func (r *recorder) failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failures) > 0
}

func (r *recorder) wasSkipped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.skipped
}

func (r *recorder) reasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.failures...)
}
