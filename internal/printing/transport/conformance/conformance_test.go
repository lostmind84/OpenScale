package conformance

// The test of the tests. A conformance suite that nothing can fail verifies nothing, so
// this file submits one healthy transport and twenty-one broken ones, and asserts WHICH
// check catches WHICH betrayal.

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"openscale/internal/station/ports"
)

// faultPatience is the wall-clock budget the broken-transport table runs on.
//
// Several of those transports hang ON PURPOSE — a cancellation nobody listens to, a
// probe that waits on the wrong thing — and at the default two seconds they would spend
// a minute proving what they prove in a tenth of a second. §16.4 budgets the WHOLE
// race-enabled suite at ten seconds, and that is a design criterion rather than a wish.
const faultPatience = 100 * time.Millisecond

// TestSuiteAcceptsTheReferenceTransport is the first half of the demonstration: the
// suite is passable, and passable by a transport of about a hundred lines.
//
// It also exercises Suite itself — the subject validation and the sub-test wiring — and
// not merely the checks, which is what the recorder-driven tests below do.
func TestSuiteAcceptsTheReferenceTransport(t *testing.T) {
	Suite(t, referenceSubject())
}

// TestSuiteRejectsEveryBrokenTransport is the other half, and the one that matters: each
// broken transport betrays exactly one clause, and the check named beside it must be the
// one that says so.
func TestSuiteRejectsEveryBrokenTransport(t *testing.T) {
	for _, tc := range []struct {
		betrayal string
		// check is the name of the check that MUST catch it. Others may also fail — a
		// transport that leaks a goroutine leaks it in several checks — but this one may
		// not stay silent.
		check   string
		subject Subject
	}{
		{"an empty name", "Name", brokenSubject(func(s *stubTransport) { s.anonymous = true })},
		{"a name nobody registered", "Name", brokenSubject(func(s *stubTransport) { s.name = "spooler" })},
		{"a blank inside the name", "Name", brokenSubject(func(s *stubTransport) { s.name = "stub transport" })},
		{"an upper-case letter inside the name", "Name", brokenSubject(func(s *stubTransport) { s.name = "stub-Transport" })},
		{"a name that moves between two calls", "Name", brokenSubject(func(s *stubTransport) { s.unstableName = true })},

		{"an empty description", "DescribeNamesTheDestination", brokenSubject(func(s *stubTransport) { s.mute = true })},
		{"a description that names no destination", "DescribeNamesTheDestination", brokenSubject(func(s *stubTransport) { s.describe = "transport local" })},
		{"a description that moves between two calls", "DescribeNamesTheDestination", brokenSubject(func(s *stubTransport) { s.unstableDescribe = true })},

		{"bytes silently dropped on the way", "WriteDeliversEveryByte", brokenSubject(func(s *stubTransport) { s.truncate = 3 })},
		{"a count that under-reports what went through", "WriteDeliversEveryByte", brokenSubject(func(s *stubTransport) { s.miscount = 1 })},

		{"a payload of zero bytes reported as printed", "EmptyPayloadIsRefused", brokenSubject(func(s *stubTransport) { s.acceptsEmpty = true })},
		{"a short write reported as a success", "PartialWriteIsAnError", brokenSubject(func(s *stubTransport) { s.shortIsSuccess = true })},
		{"a destination that cannot be opened, reported as printed", "UnreachableDeviceIsAnError", brokenSubject(func(s *stubTransport) { s.unreachableIsSuccess = true })},

		{"a write that ignores an already dead context", "CancelledContextWritesNothing", brokenSubject(func(s *stubTransport) { s.ignoresCancel = true })},
		{"a cancellation that leaves its own goroutine behind", "CancelDuringWriteLeavesNothing", brokenSubject(func(s *stubTransport) { s.leaksOnCancel = true })},
		{"a write that never comes back from a cancellation", "CancelDuringWriteLeavesNothing", brokenSubject(func(s *stubTransport) { s.hangsOnCancel = true })},

		{"a one-way transport that answers a probe anyway", "QueryAnswersOrDeclares", oneWaySubject(func(s *stubTransport) { s.queryPretends = true })},
		{"a bidirectional transport that declines the probe", "QueryAnswersOrDeclares", brokenSubject(func(s *stubTransport) { s.queryDeclines = true })},
		{"a probe that waits on something other than the injected clock", "QueryAnswersOrDeclares", brokenSubject(func(s *stubTransport) { s.queryHangs = true })},

		{"a Close that panics on the second call", "CloseIsIdempotent", brokenSubject(func(s *stubTransport) { s.panicsOnSecondClose = true })},
		{"a write that reopens the device after Close", "WriteAfterCloseIsRefused", brokenSubject(func(s *stubTransport) { s.writesAfterClose = true })},
		{"a goroutine nothing stops", "NoGoroutineLeaks", leakingSubject()},
	} {
		t.Run(tc.betrayal, func(t *testing.T) {
			failed := runChecks(t, tc.subject)
			if !slices.Contains(failed, tc.check) {
				t.Fatalf("%s went through check %q unnoticed ; les contrôles qui ont échoué : %v", tc.betrayal, tc.check, failed)
			}
		})
	}
}

// TestSuiteAcceptsTheMinimalSubmission is the other end of the range: Name, Destination
// and New, and nothing else.
//
// A contributor who cannot observe their destination — a queue that only ever exists on
// a real machine — still gets every clause that needs no seam, and gets it without
// writing a line of harness. That is what keeps the suite from being something people
// skip.
func TestSuiteAcceptsTheMinimalSubmission(t *testing.T) {
	healthy := referenceSubject()
	minimal := Subject{
		Name:          healthy.Name,
		Destination:   healthy.Destination,
		New:           healthy.New,
		Bidirectional: true,
		Patience:      faultPatience,
	}
	if failed := runChecks(t, minimal); len(failed) > 0 {
		t.Fatalf("the minimal submission failed %v", failed)
	}
}

// TestTheFourOptionalChecksSkipRatherThanPass is the difference the whole design rests
// on: a transport silently credited with a clause nobody verified is worse than a red
// line, because the place it gets discovered is a volunteer's screen on a morning when
// something else is already broken.
func TestTheFourOptionalChecksSkipRatherThanPass(t *testing.T) {
	healthy := referenceSubject()
	for _, tc := range []struct {
		hook  string
		strip func(*Subject)
		check func(*testing.T, reporter, Subject)
	}{
		{"Delivered", func(s *Subject) { s.Delivered = nil }, checkWriteDeliversEveryByte},
		{"Short", func(s *Subject) { s.Short = nil }, checkPartialWriteIsAnError},
		{"Unreachable", func(s *Subject) { s.Unreachable = nil }, checkUnreachableDeviceIsAnError},
		{"Blocking", func(s *Subject) { s.Blocking = nil }, checkCancelDuringWriteLeavesNothing},
	} {
		t.Run(tc.hook, func(t *testing.T) {
			subject := healthy
			tc.strip(&subject)

			rec := &recorder{}
			guard(func() { tc.check(t, rec, subject) })

			if !rec.wasSkipped() {
				t.Fatalf("the check did not skip without Subject.%s; it must never pass by default", tc.hook)
			}
			if rec.failed() {
				t.Fatalf("the check failed instead of skipping: %v", rec.reasons())
			}
		})
	}
}

// TestSuiteRefusesAnInconsistentSubject covers the harness's own guard rails: a subject
// the suite could only misjudge is refused before a single transport is built, because
// every failure after that would blame the transport for the harness's mistake.
func TestSuiteRefusesAnInconsistentSubject(t *testing.T) {
	healthy := referenceSubject()
	for _, tc := range []struct {
		name    string
		subject Subject
	}{
		{"no name", Subject{Destination: stubDestination, New: healthy.New}},
		{"no constructor", Subject{Name: "nameless", Destination: stubDestination}},
		{"no destination to look for", Subject{Name: stubName, New: healthy.New}},
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

// TestSuiteRefusesANilTransport keeps a nil constructor result from surfacing three
// frames deeper as a nil dereference in the middle of a check.
func TestSuiteRefusesANilTransport(t *testing.T) {
	subject := referenceSubject()
	subject.New = func(*testing.T, ports.Clock) ports.Transport { return nil }

	rec := &recorder{}
	guard(func() { checkName(t, rec, subject) })
	if !rec.failed() {
		t.Fatalf("the suite accepted a nil ports.Transport")
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

// --- the harness of this file ----------------------------------------------

// brokenSubject is the reference subject with one betrayal applied to every transport
// its constructors build.
func brokenSubject(betray func(*stubTransport)) Subject {
	subject := referenceSubject()
	subject.Patience = faultPatience
	for _, constructor := range []*func(*testing.T, ports.Clock) ports.Transport{
		&subject.New, &subject.Short, &subject.Unreachable, &subject.Blocking,
	} {
		build := *constructor
		*constructor = func(t *testing.T, clk ports.Clock) ports.Transport {
			tr := build(t, clk).(*stubTransport)
			betray(tr)
			return tr
		}
	}
	return subject
}

// oneWaySubject is brokenSubject for the betrayals that only mean something on a
// transport which declares it has no return channel.
func oneWaySubject(betray func(*stubTransport)) Subject {
	subject := brokenSubject(betray)
	subject.Bidirectional = false
	return subject
}

// leakingSubject leaves one goroutine behind per write.
//
// The leak has to be REAL for the check to have anything to catch, and it has to end
// with the sub-test, or every case after it starts from a wrong baseline.
func leakingSubject() Subject {
	return brokenSubject(func(*stubTransport) {}).withLeak()
}

// withLeak wires the release channel of the leaked goroutines onto the sub-test.
func (s Subject) withLeak() Subject {
	build := s.New
	s.New = func(t *testing.T, clk ports.Clock) ports.Transport {
		tr := build(t, clk).(*stubTransport)
		stop := make(chan struct{})
		t.Cleanup(func() { close(stop) })
		tr.leak = stop
		return tr
	}
	return s
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
