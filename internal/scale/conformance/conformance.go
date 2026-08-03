// Package conformance is the test suite every scale driver has to pass.
//
// Adding a model is one package and three files, of which seventy lines are tests
// (§9.3): capture a stream, write a domain.Decoder, wire it into the shared serial
// loop, and call Suite from your own test. What Suite buys is the part a contributor
// cannot know — not the clauses of ports.Scale they thought of, the ones that have
// already frozen a station for seven minutes.
//
// # Every check is a failure mode, never a style rule
//
//  1. Descriptor is a registry key. An empty ID, a blank or an upper-case letter in
//     it, and scale.type can no longer name the driver in config.json.
//  2. out belongs to the Hub for the whole lifetime of the process. A driver that
//     closes it breaks the serial -> manual -> serial round trip, and the degraded
//     state becomes IRREVERSIBLE (bloquant-2).
//  3. done is closed on EVERY exit path, Start returning an error included.
//     Otherwise the wait in restartScale never unblocks, and the volunteer who
//     changed a setting watches a configuration screen that never answers (§11.4,
//     test de panne 1 ter).
//  4. The last event carries StatusDisconnected. That field ALONE is what loses the
//     scale on the Hub side (défaut 40); a driver whose exit is silent leaves the
//     screen showing the weight of a bag that is no longer there.
//  5. Close is idempotent. The Hub closes on a reload and again on shutdown.
//  6. Every Measurement is coherent, and its Timestamp comes from the clock the
//     driver was GIVEN. The age of a measurement is Now - Timestamp: a driver on the
//     wall clock defeats that computation (bloquant-1), and `go run ./tools/boundary`
//     walks OUR files, never a contributor's.
//  7. A context already cancelled before Start is a real start-up race — a reload
//     that overlaps a shutdown — and not a theoretical one.
//  8. No goroutine survives the driver.
//
// # How this suite proves it bites
//
// A conformance suite that nothing can fail verifies nothing. This package therefore
// carries a reference driver that passes and twenty-nine broken ones that must not, each
// betraying exactly one clause; its own tests assert WHICH check catches WHICH betrayal.
// That is the test of the tests, and it is the reason the checks report through a narrow
// interface instead of *testing.T directly.
//
// # One rule for the caller
//
// Do not call t.Parallel around Suite. The leak check compares a process-wide
// goroutine count, and a second driver running beside it would make that number mean
// nothing.
package conformance

import (
	"testing"

	"openscale/internal/station/ports"
)

// This file is the HARNESS and the LIST: what a subject must declare, how a verdict is
// handed down, and the nine cases in the order a contributor wants to read them. The
// cases themselves are in the four check_*.go files, one per family, and what a subject
// IS is in subject.go.
//
// Adding a case means adding it to checks() below AND to a family file. A case that
// exists but is not listed here is a case the suite never runs.

// Suite runs every conformance check against subject, each one as a sub-test of t.
//
// It is the whole public surface of this package: a driver's own test calls it and
// nothing else.
//
//	func TestConformance(t *testing.T) {
//		conformance.Suite(t, conformance.Subject{
//			Name: "gram-xfoc-rs",
//			New: func(t *testing.T, clk ports.Clock) ports.Scale {
//				return gramxfoc.New(gramxfoc.RS, loopbackPort(t), clk)
//			},
//		})
//	}
//
// Suite fails t at once when the subject itself is inconsistent — no Name, no New,
// Feed without Frames — because every check after that would report the harness's
// mistake as the driver's.
func Suite(t *testing.T, subject Subject) {
	t.Helper()
	validate(t, subject)
	t.Run(subject.Name, func(t *testing.T) {
		for _, c := range checks() {
			t.Run(c.name, func(t *testing.T) { c.run(t, t, subject) })
		}
	})
}

// reporter is the slice of *testing.T that a check needs in order to hand down a
// verdict.
//
// It exists because testing.TB CANNOT be implemented outside package testing: the
// interface carries an unexported method precisely so that nobody fakes it. The checks
// therefore judge through this narrower interface, which *testing.T satisfies as it
// is, and this package's own tests pass a recording double instead. That double is the
// only way to assert that a broken driver really does make the suite fail — the
// alternative, re-executing the test binary as a subprocess to read its exit code,
// buys the same assertion for a process spawn per case and a helper nobody reads.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
}

// check is one clause of ports.Scale.
//
// The two parameters of run are not a duplicate. t BUILDS: it is what Subject.New
// receives, so that a contributor keeps the whole testing API — t.TempDir, t.Cleanup,
// t.Fatalf — inside their own constructor. r JUDGES: it is where the verdict goes.
// Splitting them is what lets this package run a check against a deliberately broken
// driver and assert that the verdict was a failure.
type check struct {
	name string
	run  func(t *testing.T, r reporter, subject Subject)
}

// checks lists the suite in the order a contributor wants to read it: identity first,
// then the two channels, then the exits, then what travels on them.
func checks() []check {
	return []check{
		{"Descriptor", checkDescriptor},
		{"OutIsNeverClosed", checkOutIsNeverClosed},
		{"DoneClosesWhenTheContextEnds", checkDoneClosesWhenTheContextEnds},
		{"DoneClosesWhenStartFails", checkDoneClosesWhenStartFails},
		{"LastEventIsDisconnected", checkLastEventIsDisconnected},
		{"CloseIsIdempotent", checkCloseIsIdempotent},
		{"MeasurementsAreCoherent", checkMeasurementsAreCoherent},
		{"StartSurvivesACancelledContext", checkStartSurvivesACancelledContext},
		{"NoGoroutineLeaks", checkNoGoroutineLeaks},
	}
}

// validate refuses a subject the suite could only misjudge.
func validate(r reporter, subject Subject) {
	r.Helper()
	switch {
	case subject.Name == "":
		r.Fatalf("conformance: Subject.Name is empty; it names the sub-test group and every failure line")
	case subject.New == nil:
		r.Fatalf("conformance: Subject.New is nil; the suite has nothing to build")
	case subject.Feed != nil && len(subject.Frames) == 0:
		r.Fatalf("conformance: Subject.Feed is set and Subject.Frames is empty; there is nothing to feed")
	case subject.Feed == nil && len(subject.Frames) > 0:
		r.Fatalf("conformance: Subject.Frames holds %d bytes and Subject.Feed is nil; they would never reach the driver", len(subject.Frames))
	}
}

// requireStarted refuses to judge the rest of the contract on a driver that never
// started: every clause that follows would report the environment's fault as the
// driver's.
func requireStarted(r reporter, session *session) {
	r.Helper()
	if session.startErr != nil {
		r.Fatalf("Start returned %v. The driver that Subject.New builds must be startable in the test environment — open a loopback, a pipe, a temporary file — and a device that is deliberately absent belongs in Subject.Unstartable", session.startErr)
	}
}

// build calls one of the subject's constructors and refuses a nil driver, which would
// otherwise surface as a nil dereference three frames deeper.
func build(t *testing.T, r reporter, constructor func(*testing.T, ports.Clock) ports.Scale, clk ports.Clock) ports.Scale {
	r.Helper()
	scale := constructor(t, clk)
	if scale == nil {
		r.Fatalf("the Subject constructor returned a nil ports.Scale")
	}
	return scale
}
