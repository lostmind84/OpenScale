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

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"openscale/internal/fake"
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

// checkName verifies the identity the registry and config.json both read.
func checkName(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	name := tr.Name()
	switch {
	case name == "":
		r.Errorf("Name() is empty. It is the key of the transport registry and the value of printer.options.transport: an anonymous transport cannot be named by a configuration file, and control 8 of Config.Validate has nothing to check against")
	case name != subject.Name:
		r.Errorf("Name() = %q while the subject is submitted as %q. Those two are the same string in config.json, and a transport that answers to a name nobody registered is unreachable", name, subject.Name)
	}
	if i := strings.IndexFunc(name, unicode.IsSpace); i >= 0 {
		r.Errorf("Name() = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", name, i)
	}
	if i := strings.IndexFunc(name, unicode.IsUpper); i >= 0 {
		r.Errorf("Name() = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different transport", name, i, strings.ToLower(name))
	}
	if again := tr.Name(); again != name {
		r.Errorf("Name() answered %q then %q. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", name, again)
	}
}

// checkDescribeNamesTheDestination is the line a volunteer reads when nothing comes out.
func checkDescribeNamesTheDestination(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	described := tr.Describe()
	if described == "" {
		r.Fatalf("Describe() is empty. It is the wording of the administration screen and of the technical journal: « impression indisponible » with nothing after it sends a volunteer looking at the wrong printer")
	}
	if !strings.Contains(described, subject.Destination) {
		r.Errorf("Describe() = %q and does not name %q, the destination this transport was built for. A description that omits the queue, the address or the path is the same sentence for the four stations of the shop, and it is on that sentence that somebody decides which cable to check", described, subject.Destination)
	}
	if again := tr.Describe(); again != described {
		r.Errorf("Describe() answered %q then %q. It is read by the admin screen and by the journal separately, and a wording that moves between two calls makes two log lines look like two printers", described, again)
	}
}

// checkWriteDeliversEveryByte is the contract in one line: what went in comes out, and
// the count is the truth.
func checkWriteDeliversEveryByte(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err != nil {
		r.Fatalf("Write returned %v on a destination the subject declares healthy. Subject.New must build a transport that can be written to in the test environment — a temporary directory, a pipe, a loopback listener — and a device that is deliberately absent belongs in Subject.Unreachable", err)
	}
	if n != len(payload) {
		r.Errorf("Write reported %d bytes accepted out of %d with a nil error. The count is what the print receipt carries (ports.PrintReceipt.Bytes); a count that does not match the frame makes the journal useless for the one question it is kept for", n, len(payload))
	}
	if subject.Delivered == nil {
		r.Skipf("Subject.Delivered is nil: the suite cannot read the destination back, so it took Write at its word. Supply it as soon as the destination is readable — the round trip is the only check that catches a transport that re-encodes, trims or line-ends what it carries")
	}
	if got := subject.Delivered(t, tr); !bytes.Equal(got, payload) {
		r.Errorf("what arrived is not what was sent.\n  sent    (%d bytes) %x\n  arrived (%d bytes) %x\nAn SBPL frame is binary: an ESC byte dropped, a \\r\\n translated or a trailing zero trimmed and the printer sees a command it does not know (§8.3)", len(payload), payload, len(got), got)
	}
}

// checkEmptyPayloadIsRefused is the same lie as a short write, one layer down.
func checkEmptyPayloadIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), nil)
	if err == nil {
		r.Errorf("Write(ctx, nil) reported %d bytes and NO error. Nothing legitimate hands a printer zero bytes: answering « c'est fait » turns an encoder that produced nothing into a successful weighing, and the customer walks off without a label while the screen says one was sent (§8.5)", n)
	}
	if n != 0 {
		r.Errorf("Write(ctx, nil) reported %d bytes accepted, out of none given", n)
	}
	if subject.Delivered != nil {
		if got := subject.Delivered(t, tr); len(got) > 0 {
			r.Errorf("Write(ctx, nil) was refused and %d bytes still reached the destination: %x", len(got), got)
		}
	}
}

// checkPartialWriteIsAnError is clause 4, and it is the one a transport breaks by
// writing `return w.Write(p)` and thinking the job is done.
func checkPartialWriteIsAnError(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Short == nil {
		r.Skipf("Subject.Short is nil: this subject offers no way to make its destination accept fewer bytes than it is given. Supply it for anything that reaches a device — WritePrinter really does report a short count with a nil error, and a transport that passes that on turns a lost label into a confirmed one")
	}
	tr := build(t, r, subject.Short, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err == nil {
		r.Fatalf("the destination accepted %d bytes out of %d and Write reported SUCCESS. A truncated frame prints blank, and the station would journal result='sent' for a label nobody ever held (§8.3, §8.5)", n, len(payload))
	}
	if n == len(payload) {
		r.Errorf("Write returned an error and still claimed the whole frame (%d bytes) was accepted. The count travels into the print receipt; it has to say what really went through", n)
	}
}

// checkUnreachableDeviceIsAnError is the KindTransient of §8.5: the queue that is not
// there, the printer that is off, the node that came back as lp1.
func checkUnreachableDeviceIsAnError(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Unreachable == nil {
		r.Skipf("Subject.Unreachable is nil: this subject declares no destination that cannot be opened. Supply it for anything that opens something — an unknown queue name, a device that is not there — because it is the failure a station actually meets, and it must arrive as an error the print service can retry")
	}
	tr := build(t, r, subject.Unreachable, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err == nil {
		r.Errorf("Write reported %d bytes and no error on a destination that cannot be opened. The print service reads the error to decide between « 2 réessais, 300 ms puis 1 s » and « pas de réessai » (§8.5); with none, it retries nothing and confirms everything", n)
	}
	if n != 0 {
		r.Errorf("Write reported %d bytes accepted by a destination that was never opened", n)
	}
}

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
