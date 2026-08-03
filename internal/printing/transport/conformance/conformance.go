// Package conformance is the test suite every printer transport has to pass.
//
// A transport is thirty lines of plumbing around one system call, which is exactly why
// it deserves a suite: nobody reviews thirty lines of plumbing twice. What Suite buys is
// the part a contributor cannot know — not the clauses of ports.Transport they thought
// of, the ones whose breach reaches a customer as a blank label and a screen that says
// « Étiquette envoyée à l'imprimante ».
//
// # Every check is a failure mode, never a style rule
//
//  1. Name is a registry key. An empty one, a blank or an upper-case letter in it, and
//     printer.options.transport can no longer name the transport in config.json.
//  2. Describe NAMES THE DESTINATION. It is the one line a volunteer reads on the
//     troubleshooting screen when nothing comes out, and « transport local » tells them
//     nothing they did not already know.
//  3. Write delivers EVERY byte, and reports how many were accepted. A truncated frame
//     prints blank (§8.3, §8.5).
//  4. A PARTIAL WRITE IS AN ERROR. WritePrinter reports a short count with no error of
//     its own, and a transport that passes that on as a success turns a lost label into
//     a confirmed one.
//  5. AN EMPTY PAYLOAD IS REFUSED. Answering « c'est fait » to zero bytes hides an
//     encoder that produced nothing.
//  6. A device that cannot be reached is an ERROR. It is the KindTransient of §8.5, the
//     one the print service retries twice, 300 ms then 1 s.
//  7. A cancelled context writes NOTHING and gives the floor back — and leaves neither a
//     goroutine nor a handle behind. That is failure test 6, « imprimante qui pend 60 s »
//     (§16.2): the Hub is never blocked.
//  8. Close is idempotent. The print service closes on a configuration reload and again
//     on shutdown (§11.4, §13.4).
//  9. A write after Close is REFUSED, not reopened. A station that has given up must not
//     be brought back by a job that arrived late.
//  10. Query answers or DECLARES that it cannot, with ports.ErrUnsupported. « I do not
//     know » is a legitimate answer (ports.PrinterUnknown exists for it); pretending is
//     not (important-7).
//  11. No goroutine survives the transport.
//
// # How this suite proves it bites
//
// A conformance suite that nothing can fail verifies nothing. This package therefore
// carries a reference transport that passes and a table of broken ones that must not,
// each betraying exactly one clause; its own tests assert WHICH check catches WHICH
// betrayal. That is the test of the tests, and it is why the checks report through a
// narrow interface instead of *testing.T directly.
//
// # One rule for the caller
//
// Do not call t.Parallel around Suite. The leak check compares a process-wide goroutine
// count, and a second transport running beside it would make that number mean nothing.
package conformance

// This file is the entry point and the LIST: what the suite sends, the Subject a
// transport is submitted as, Suite, and the order the clauses are read in. The clauses
// themselves live in check_frame.go and check_lifecycle.go.

import (
	"testing"
	"time"

	"openscale/internal/station/ports"
)

// t0 is where the clock the suite injects starts. It sits far in the past for the same
// reason the scale suite does it: anything a transport dated from the wall clock lands
// years away from the window this clock ever covered.
var t0 = time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC)

// defaultPatience is how long the suite waits, ON THE WALL CLOCK, for a transport to do
// what it said it would. Subject.Patience overrides it.
//
// Wall clock, in a repository whose whole temporal strategy is an injected fake, because
// what is bounded here is a goroutine leaving blocking OS I/O — a socket write, a
// CloseHandle on a device — and no fake clock drives an OS handle.
const defaultPatience = 2 * time.Second

// pollInterval is how often a condition with no channel of its own is re-read.
const pollInterval = time.Millisecond

// probeBudget is the status budget the Query check hands over. It is the 500 ms of §8.5,
// and it is measured on the INJECTED clock, so it costs nothing.
const probeBudget = 500 * time.Millisecond

// enquiry is ENQ, the one byte the native status probe of §8.5 sends.
const enquiry = 0x05

// payload is what the suite sends. It is deliberately not text: a transport that
// re-encoded, trimmed or line-ended what it carries would be caught by the round trip,
// and an SBPL frame is binary hexadecimal with ESC bytes in it (§8.3).
var payload = []byte("\x1bA\x1bA1020300320\x1bGH040203ABCDEF\x00\xff\r\n\x1bZ")

// Subject is the transport submitted to the suite.
//
// Three fields are mandatory — Name, Destination and New; every other one widens what
// the suite can reach. That asymmetry is the point: submitting a transport must cost a
// few function literals, and a contributor who supplies nothing else still gets the
// checks that need no device.
type Subject struct {
	// Name is the transport under test, spelled as its registry key: "winspool". It
	// names the sub-test group, so it appears in every failure line, and Name() must
	// return it.
	Name string

	// Destination is what the operator is told the bytes go to: the queue, the node, the
	// address, the directory. Describe() has to contain it.
	Destination string

	// New returns ONE fresh transport, ready to write and not yet closed.
	//
	// It is called once per check, because a transport that has been cancelled and
	// closed is not a fair subject for the next one. clk is the clock the transport MUST
	// measure its delays on.
	New func(t *testing.T, clk ports.Clock) ports.Transport

	// Delivered reports the bytes the transport actually handed over, for the transport
	// New just built.
	//
	// It is what turns « Write returned nil » into an assertion. Leave it nil when the
	// destination cannot be read back — and know that the round-trip check then reports
	// itself SKIPPED rather than passed.
	Delivered func(t *testing.T, tr ports.Transport) []byte

	// Short builds a transport whose destination accepts FEWER bytes than it is given,
	// without an error of its own. That is WritePrinter's real behaviour, and clause 4 is
	// the one a transport breaks by writing `return w.Write(p)`.
	Short func(t *testing.T, clk ports.Clock) ports.Transport

	// Unreachable builds a transport whose destination cannot be opened: a queue nobody
	// installed, a device that is not there, a directory that refuses.
	Unreachable func(t *testing.T, clk ports.Clock) ports.Transport

	// Blocking builds a transport whose write PARKS until the handle is closed — the
	// printer of failure test 6, which hangs for sixty seconds.
	//
	// Supply it for anything that talks to a device. It is the clause with the worst
	// consequence, because the goroutine it leaks holds the print service's mutex and
	// takes the Hub with it.
	Blocking func(t *testing.T, clk ports.Clock) ports.Transport

	// Bidirectional declares that this transport can carry the native status probe of
	// §8.5 (level N3). At false, the suite REQUIRES Query to decline with
	// ports.ErrUnsupported instead of quietly answering nothing.
	Bidirectional bool

	// Patience is the wall-clock budget of one wait. Zero means defaultPatience. Raise
	// it for a destination that is genuinely slow to release a handle; do not raise it
	// to make a flaky transport pass.
	Patience time.Duration
}

// patience reports the wall-clock budget of one wait.
func (s Subject) patience() time.Duration {
	if s.Patience > 0 {
		return s.Patience
	}
	return defaultPatience
}

// Suite runs every conformance check against subject, each one as a sub-test of t.
//
// It is the whole public surface of this package: a transport's own test calls it and
// nothing else.
//
//	func TestFileConformance(t *testing.T) {
//		conformance.Suite(t, conformance.Subject{
//			Name:        domain.TransportFile,
//			Destination: dir,
//			New: func(t *testing.T, clk ports.Clock) ports.Transport {
//				return newFile(t, dir, clk)
//			},
//		})
//	}
//
// Suite fails t at once when the subject itself is inconsistent, because every check
// after that would report the harness's mistake as the transport's.
func Suite(t *testing.T, subject Subject) {
	t.Helper()
	validate(t, subject)
	t.Run(subject.Name, func(t *testing.T) {
		for _, c := range checks() {
			t.Run(c.name, func(t *testing.T) { c.run(t, t, subject) })
		}
	})
}

// reporter is the slice of *testing.T that a check needs in order to hand down a verdict.
//
// It exists because testing.TB CANNOT be implemented outside package testing: the
// interface carries an unexported method precisely so that nobody fakes it. The checks
// therefore judge through this narrower interface, which *testing.T satisfies as it is,
// and this package's own tests pass a recording double instead. That double is the only
// way to assert that a broken transport really does make the suite fail.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
}

// check is one clause of ports.Transport.
//
// The two parameters of run are not a duplicate. t BUILDS: it is what Subject.New
// receives, so a contributor keeps t.TempDir and t.Cleanup inside their own constructor.
// r JUDGES. Splitting them is what lets this package run a check against a deliberately
// broken transport and assert that the verdict was a failure.
type check struct {
	name string
	run  func(t *testing.T, r reporter, subject Subject)
}

// checks lists the suite in the order a contributor wants to read it: identity first,
// then what travels, then what goes wrong, then the exits.
func checks() []check {
	return []check{
		{"Name", checkName},
		{"DescribeNamesTheDestination", checkDescribeNamesTheDestination},
		{"WriteDeliversEveryByte", checkWriteDeliversEveryByte},
		{"EmptyPayloadIsRefused", checkEmptyPayloadIsRefused},
		{"PartialWriteIsAnError", checkPartialWriteIsAnError},
		{"UnreachableDeviceIsAnError", checkUnreachableDeviceIsAnError},
		{"CancelledContextWritesNothing", checkCancelledContextWritesNothing},
		{"CancelDuringWriteLeavesNothing", checkCancelDuringWriteLeavesNothing},
		{"QueryAnswersOrDeclares", checkQueryAnswersOrDeclares},
		{"CloseIsIdempotent", checkCloseIsIdempotent},
		{"WriteAfterCloseIsRefused", checkWriteAfterCloseIsRefused},
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
	case subject.Destination == "":
		r.Fatalf("conformance: Subject.Destination is empty; the suite has nothing to look for in Describe(), which is the one line a volunteer reads when nothing comes out")
	}
}

// build calls one of the subject's constructors and refuses a nil transport, which would
// otherwise surface as a nil dereference three frames deeper.
func build(t *testing.T, r reporter, constructor func(*testing.T, ports.Clock) ports.Transport,
	clk ports.Clock) ports.Transport {
	r.Helper()
	tr := constructor(t, clk)
	if tr == nil {
		r.Fatalf("the Subject constructor returned a nil ports.Transport")
	}
	return tr
}
