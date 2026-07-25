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
	"context"
	"strings"
	"testing"
	"time"
	"unicode"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// MaxExpressibleGrams is the heaviest mass the frame grammar of §9.2 can express: six
// integer digits of kilograms and the three decimals that survive the padding.
//
// The coherence check bounds Gross by the GRAMMAR and not by any model capacity, on
// purpose. A scale over capacity reports whatever it likes — the corpus holds an
// "OL,GS,+ 99.999KG" — and the suite must not call that a driver bug: the field that
// carries the truth there is Overload, and safeguard rule 1 reads it (§6.4).
const MaxExpressibleGrams domain.Grams = 999_999_999

// Subject is the driver submitted to the suite.
//
// Two fields are mandatory, Name and New; every other one widens what the suite can
// reach. That asymmetry is the point: submitting a driver must cost one function
// literal, and a contributor who supplies nothing else still gets the checks that
// need no device.
type Subject struct {
	// Name is the driver under test, spelled as its registry key: "gram-xfoc-rs". It
	// names the sub-test group, so it appears in every failure line.
	Name string

	// New returns ONE fresh driver, built but not started.
	//
	// It is called once per check, because a driver that has been started, cancelled
	// and closed is not a fair subject for the next one. clk is the clock the driver
	// MUST take its instants from: the suite hands over a fake one and then checks
	// the timestamps that come back.
	New func(t *testing.T, clk ports.Clock) ports.Scale

	// Unstartable returns a driver whose Start is expected to FAIL before it ever
	// launches its goroutine: a port that does not exist, a handle already held.
	//
	// Leave it nil when the driver has no such failure mode — the empty weight source
	// of internal/scale/absent opens nothing — and the check that needs it reports
	// itself SKIPPED rather than passed. Any driver that opens a device should supply
	// it: "done is closed even when Start returns an error" is the clause a driver
	// breaks first, and the only one whose consequence is a screen that never answers
	// (§11.4).
	Unstartable func(t *testing.T, clk ports.Clock) ports.Scale

	// Feed hands raw device bytes to a driver the suite has already started, the way
	// the wire would.
	//
	// It is what turns the measurement checks from "whatever happened to arrive" into
	// an assertion. The closure usually writes into the pipe that the New closure kept
	// a side reference to, and it may also advance the clock it was given, for a driver
	// that paces itself on it.
	//
	// Setting Feed REQUIRES Frames, and Frames without Feed is refused: test data that
	// silently reaches nothing is worse than no test data.
	Feed func(t *testing.T, s ports.Scale, raw []byte)

	// Frames is what Feed injects: a capture of this very model, ideally one straight
	// out of `openscale capture --port COM3 --duration 60s`.
	Frames []byte

	// RequireDisconnectCause also demands a non-nil Err on the last event.
	//
	// Off by default, and deliberately so: ports.Scale does NOT require it. Making the
	// loss of the scale depend on an optional field is exactly the defect that let the
	// signal fall into a default branch and never reach the state machine (défaut 40).
	// internal/scale/serial tightens its OWN contract so that the cause always remains
	// loggable (§9.1) and turns this on to be held to it.
	RequireDisconnectCause bool

	// Patience is how long the suite waits, ON THE WALL CLOCK, for a driver to do what
	// it said it would: close done, publish a measurement, let its goroutines go. Zero
	// means defaultPatience.
	//
	// Wall clock, in a repository where everything else runs on an injected fake,
	// because what is bounded here is a goroutine leaving blocking OS I/O. Raise it for
	// a device that is genuinely slow to release a handle; do not raise it to make a
	// flaky driver pass.
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

// checkDescriptor verifies the identity the registry and the admin form both read.
//
// Descriptor is called before anything is started, because that is when the Hub calls
// it: the drop-down list of scale.type is built from drivers nobody has opened yet.
func checkDescriptor(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	scale := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(scale)

	descriptor := scale.Descriptor()
	if descriptor.ID == "" {
		r.Errorf("Descriptor().ID is empty. It is the key of the driver registry and the value of scale.type in config.json: an anonymous driver cannot be named by a configuration file, and the admin screen has nothing to generate its form from")
	}
	if descriptor.Label == "" {
		r.Errorf("Descriptor().Label is empty. It is what a volunteer replacing the hardware looks for in the menu, and it has to be the name printed on the device: « GRAM XFOC RS »")
	}
	if descriptor.NominalRate <= 0 {
		r.Errorf("Descriptor().NominalRate = %s, want > 0. The rate meter starts from the declared cadence and only leaves it once it holds eight intervals of its own; a zero cadence makes the derived expiry meaningless (§6.5)", descriptor.NominalRate)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsSpace); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", descriptor.ID, i)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsUpper); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different driver — the same trap the legacy application fell into with the case of its frame suffix", descriptor.ID, i, strings.ToLower(descriptor.ID))
	}
	if again := scale.Descriptor(); again != descriptor {
		r.Errorf("Descriptor() answered %+v then %+v. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", descriptor, again)
	}
}

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

// checkLastEventIsDisconnected is défaut 40.
//
// It also proves the other half of clause 2: these events were read from the channel
// the SUITE created and lent out, so a driver publishing on some channel of its own
// would arrive here with nothing to show.
func checkLastEventIsDisconnected(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)
	session.stop(r)

	events := session.collected()
	if len(events) == 0 {
		r.Fatalf("the driver published NOTHING on out over its whole life. A driver that exits emits one last ScaleEvent{StatusDisconnected} (§9.1): without it the Hub never learns the scale is gone and the screen keeps showing the weight of a bag that has left")
	}
	last := events[len(events)-1]
	if last.Status != domain.StatusDisconnected {
		r.Errorf("the last of %d events has Status = %s, want %s. That field ALONE loses the scale on the Hub side, which is why it may never be left to the caller to deduce from Err (défaut 40)", len(events), last.Status, domain.StatusDisconnected)
	}
	if subject.RequireDisconnectCause && last.Err == nil {
		r.Errorf("the last event has Err = nil while this subject asks to be held to the tightened contract of §9.1: the device error when there is one, ctx.Err() on cancellation, ErrLoopStopped otherwise. Nothing CONDITIONS on Err — it has to stay loggable")
	}
}

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

// checkMeasurementsAreCoherent reads every measurement the driver published and holds
// it to what the rest of the application assumes without ever checking again.
func checkMeasurementsAreCoherent(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)

	if subject.Feed != nil && !session.awaitMeasurement() {
		r.Fatalf("%d bytes went in through Subject.Feed and not one Measurement came out within %s. Feed the accumulator of internal/domain/frame rather than a fixed window: a decoder that silently drops what it does not recognise is exactly the 18-byte read that lost one frame in two (§9.1)",
			len(subject.Frames), session.patience)
	}
	session.stop(r)

	// The window the INJECTED clock covered. It closes here and not at t0 because
	// Subject.Feed is allowed to advance that clock for a driver that paces itself on it.
	window := session.clock.Now()

	var previous time.Time
	for i, event := range session.collected() {
		measurement := event.Measurement
		if measurement == nil {
			continue
		}
		switch {
		case measurement.Timestamp.IsZero():
			r.Errorf("event %d: Measurement.Timestamp is the zero instant. The age of a reading is Now - Timestamp (§6.5): a zero instant makes every measurement look expired and no weighing possible at all (bloquant-1)", i)
		case measurement.Timestamp.Before(t0), measurement.Timestamp.After(window):
			r.Errorf("event %d: Measurement.Timestamp = %s falls outside [%s, %s], the window the clock the suite HANDED YOU ever covered: this driver reads a clock of its own. `go run ./tools/boundary` walks our files and cannot see inside yours, so this check stands in for it (§5.3)",
				i, measurement.Timestamp.UTC(), t0.UTC(), window.UTC())
		case measurement.Timestamp.Before(previous):
			r.Errorf("event %d: Measurement.Timestamp = %s goes backwards from %s on the previous measurement. One clock, one direction: an age computed against a wandering instant can come out negative", i, measurement.Timestamp.UTC(), previous.UTC())
		default:
			previous = measurement.Timestamp
		}
		if measurement.Gross > MaxExpressibleGrams || measurement.Gross < -MaxExpressibleGrams {
			r.Errorf("event %d: Measurement.Gross = %d g is outside ±%d g, which is everything the frame grammar of §9.2 can express. A mass no frame could have carried means the decoder invented digits, and the barcode carries five of them", i, measurement.Gross, MaxExpressibleGrams)
		}
		switch measurement.Stability {
		case domain.Stable, domain.Unstable, domain.StabilityUnknown, domain.StabilityNotApplicable:
		default:
			r.Errorf("event %d: Measurement.Stability = %d is outside the vocabulary. A model that does not report the ST/US flag says StabilityUnknown and lets the variation criterion take over; it does not invent a value (§6.5)", i, measurement.Stability)
		}
		if event.Status == domain.StatusDisconnected {
			r.Errorf("event %d carries a Measurement AND Status = %s. Those two contradict each other: this single event both delivers a weight and takes the scale away, and Status is what the Hub acts on (défaut 40)", i, domain.StatusDisconnected)
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
