package conformance

// The test of the tests. A conformance suite that nothing can fail verifies nothing, so
// this file submits one healthy printer and thirty-seven broken ones, and asserts WHICH
// check catches WHICH betrayal.

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// faultPatience is the wall-clock budget the broken-printer table runs on.
//
// It bounds only the two checks that WAIT — the goroutine baseline and the leak — and the
// leaking subject spends it on purpose. §16.4 budgets the whole race-enabled suite at ten
// seconds, and that is a design criterion rather than a wish.
const faultPatience = 100 * time.Millisecond

// TestSuiteAcceptsTheReferencePrinter is the first half of the demonstration: the suite is
// passable, and passable by a driver of about two hundred lines.
//
// It also exercises Suite itself — the subject validation, the template coherence guard and
// the sub-test wiring — and not merely the checks, which is what the recorder-driven tests
// below do.
func TestSuiteAcceptsTheReferencePrinter(t *testing.T) {
	Suite(t, referenceSubject())
}

// TestSuiteRejectsEveryBrokenPrinter is the other half, and the one that matters: each
// broken driver betrays exactly one clause, and the check named beside it must be the one
// that says so.
func TestSuiteRejectsEveryBrokenPrinter(t *testing.T) {
	for _, tc := range []struct {
		betrayal string
		// check is the name of the check that MUST catch it. Others may also fail — a
		// driver with no identity fails wherever the identity is read — but this one may
		// not stay silent.
		check   string
		subject Subject
	}{
		{"an empty identity", "Descriptor", brokenSubject(func(s *stubPrinter) { s.anonymous = true })},
		{"an identity nobody registered", "Descriptor", brokenSubject(func(s *stubPrinter) { s.id = "spooler" })},
		{"a blank inside the identity", "Descriptor", brokenSubject(func(s *stubPrinter) { s.id = "stub printer" })},
		{"an upper-case letter inside the identity", "Descriptor", brokenSubject(func(s *stubPrinter) { s.id = "stub-Printer" })},
		{"no label for the drop-down list", "Descriptor", brokenSubject(func(s *stubPrinter) { s.mute = true })},
		{"an identity that moves between two calls", "Descriptor", brokenSubject(func(s *stubPrinter) { s.unstableID = true })},

		{"a driver that cannot print one copy", "CopiesStayInsideTheDeclaredBound", brokenSubject(func(s *stubPrinter) { s.noCopies = true })},
		{"a copy count past the declared bound, printed", "CopiesStayInsideTheDeclaredBound", brokenSubject(func(s *stubPrinter) { s.honoursNoBound = true })},
		{"a copy count refused without naming the range", "CopiesStayInsideTheDeclaredBound", brokenSubject(func(s *stubPrinter) { s.bareCopyRefusal = true })},
		{"a copy count refused as retryable", "CopiesStayInsideTheDeclaredBound", brokenSubject(func(s *stubPrinter) { s.copyRefusalIsTransient = true })},
		{"an unspecified copy count read as none", "AJobWithoutACopyCountStillPrints", brokenSubject(func(s *stubPrinter) { s.zeroCopiesPrintsNothing = true })},

		{"a barcode no till can scan, printed", "AnUnusableBarcodeIsRefusedAsData", brokenSubject(func(s *stubPrinter) { s.acceptsAnyBarcode = true })},
		{"a product to fix in Odoo reported as a bug in this binary", "AnUnusableBarcodeIsRefusedAsData", brokenSubject(func(s *stubPrinter) { s.barcodeIsInternal = true })},
		{"a barcode checked after the label was handed over", "AnUnusableBarcodeIsRefusedAsData", brokenSubject(func(s *stubPrinter) { s.barcodeCheckedAfterTheRender = true })},

		{"a template drawn for another head, printed", "AForeignTemplateIsRefused", brokenSubject(func(s *stubPrinter) { s.acceptsForeignTemplate = true })},
		{"a geometry blamed on the catalog", "AForeignTemplateIsRefused", brokenSubject(func(s *stubPrinter) { s.foreignTemplateIsData = true })},

		{"a short write reported as a printed label", "AShortWriteIsATransientFailure", brokenSubject(func(s *stubPrinter) { s.shortIsSuccess = true })},
		{"a short write nothing will ever retry", "AShortWriteIsATransientFailure", brokenSubject(func(s *stubPrinter) { s.shortIsInternal = true })},

		{"a readiness nothing observed", "StatusNeverClaimsReadyWithoutProof", brokenSubject(func(s *stubPrinter) { s.claimsReady = true })},
		{"a fault reported by a driver with no return channel", "StatusNeverClaimsReadyWithoutProof", brokenSubject(func(s *stubPrinter) {
			s.noStatusCapability = true
			s.saysFaulted = true
		})},
		{"a status with no sentence for the troubleshooting screen", "StatusNeverClaimsReadyWithoutProof", brokenSubject(func(s *stubPrinter) { s.muteStatus = true })},
		{"a green light over a driver that gave up its device", "StatusAfterCloseIsUnknown", brokenSubject(func(s *stubPrinter) { s.statusAfterCloseSaysReady = true })},

		{"a job that reopens the device after Close", "PrintAfterCloseIsRefused", brokenSubject(func(s *stubPrinter) { s.printsAfterClose = true })},
		{"a closed driver refusing in a way the service retries", "PrintAfterCloseIsRefused", brokenSubject(func(s *stubPrinter) { s.closedRefusalIsTransient = true })},
		{"a Close that panics on the second call", "CloseIsIdempotent", brokenSubject(func(s *stubPrinter) { s.panicsOnSecondClose = true })},

		{"a declared self-test the driver turns down", "EverySelfTestAnswersAsDeclared", brokenSubject(func(s *stubPrinter) { s.refusesADeclaredSelfTest = true })},
		{"« auto-test inconnu » about a name the catalogue carries", "EverySelfTestAnswersAsDeclared", undeclaredSubject(func(s *stubPrinter) { s.unknownOnACatalogueName = true })},
		{"a pattern printed by a driver that never declared it", "EverySelfTestAnswersAsDeclared", undeclaredSubject(func(*stubPrinter) {})},
		{"a self-test that answers to any name", "AnUnknownSelfTestNamesTheOnesThatExist", brokenSubject(func(s *stubPrinter) { s.acceptsAnySelfTest = true })},
		{"a refusal that lists nothing", "AnUnknownSelfTestNamesTheOnesThatExist", brokenSubject(func(s *stubPrinter) { s.bareUnknownRefusal = true })},
		{"a demonstration label the driver made up", "TheDemonstrationLabelIsNeverInvented", brokenSubject(func(s *stubPrinter) { s.inventsADemoLabel = true })},

		{"a duration the injected clock never covered", "TheClockIsTheOneTheDriverWasGiven", brokenSubject(func(s *stubPrinter) { s.wallClock = true })},
		{"a receipt carrying somebody else's job", "PrintIsSerialised", brokenSubject(func(s *stubPrinter) { s.stealsTheJobID = true })},
		{"a goroutine nothing stops", "NoGoroutineLeaks", leakingSubject()},

		{"an English sentence on the troubleshooting screen", "OperatorMessagesAreFrench", brokenSubject(func(s *stubPrinter) { s.englishStatus = true })},
		{"an English refusal in front of a volunteer", "OperatorMessagesAreFrench", brokenSubject(func(s *stubPrinter) { s.englishRefusal = true })},
		{"a composition-root mistake answered in French", "ADeveloperMessageStaysEnglish", frenchDeveloperSubject()},
	} {
		t.Run(tc.betrayal, func(t *testing.T) {
			failed := runChecks(t, tc.subject)
			if !slices.Contains(failed, tc.check) {
				t.Fatalf("%s went through check %q unnoticed ; les contrôles qui ont échoué : %v",
					tc.betrayal, tc.check, failed)
			}
		})
	}
}

// TestSuiteAcceptsTheMinimalSubmission is the other end of the range: Name and New, and
// nothing else.
//
// A contributor who cannot observe their destination — a queue that only ever exists on a
// real machine — still gets every clause that needs no seam, and gets it without writing a
// line of harness. That is what keeps the suite from being something people skip.
func TestSuiteAcceptsTheMinimalSubmission(t *testing.T) {
	healthy := referenceSubject()
	minimal := Subject{
		Name:                healthy.Name,
		New:                 healthy.New,
		JobAdvancesTheClock: stubWriteDelay,
		Patience:            faultPatience,
	}
	if failed := runChecks(t, minimal); len(failed) > 0 {
		t.Fatalf("the minimal submission failed %v", failed)
	}
}

// TestTheFourOptionalChecksSkipRatherThanPass is the difference the whole design rests on:
// a driver silently credited with a clause nobody verified is worse than a red line,
// because the place it gets discovered is a volunteer's screen on a morning when something
// else is already broken.
func TestTheFourOptionalChecksSkipRatherThanPass(t *testing.T) {
	healthy := referenceSubject()
	for _, tc := range []struct {
		hook  string
		strip func(*Subject)
		check func(*testing.T, reporter, Subject)
	}{
		{"Short", func(s *Subject) { s.Short = nil }, checkAShortWriteIsATransientFailure},
		{"WithoutDemoLabel", func(s *Subject) { s.WithoutDemoLabel = nil }, checkTheDemonstrationLabelIsNeverInvented},
		{"MissingCollaborator", func(s *Subject) { s.MissingCollaborator = nil }, checkADeveloperMessageStaysEnglish},
		{"DrivesAHead", func(s *Subject) { s.DrivesAHead = false }, checkAForeignTemplateIsRefused},
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

// TestTheCopyBoundSkipsRatherThanPassesOnADriverThatIgnoresIt is the fifth skip, and it
// is conditional where the four above are structural: a driver that REFUSES an
// out-of-range count is fully judged with no hook at all, and only one that accepts the
// count needs Subject.Copies to be judged at all.
func TestTheCopyBoundSkipsRatherThanPassesOnADriverThatIgnoresIt(t *testing.T) {
	subject := brokenSubject(func(s *stubPrinter) { s.honoursNoBound = true })
	subject.Copies = nil

	rec := &recorder{}
	guard(func() { checkCopiesStayInsideTheDeclaredBound(t, rec, subject) })

	if !rec.wasSkipped() {
		t.Fatal("a driver that accepted a count past its own bound was credited with the clause without Subject.Copies")
	}
	if rec.failed() {
		t.Fatalf("the check failed instead of skipping: %v", rec.reasons())
	}
}

// TestASilentDeclarationIsHeldToTheWholeCatalogue is the reading of a nil Subject.SelfTests,
// and it is a decision the other way round from every optional hook of this suite.
//
// A missing hook SKIPS, because the suite cannot see what it was not given. A missing
// declaration does not: nil is read as « all three », so a contributor who left the field
// out is judged on the strongest clause. The other reading — nil as « none » — would credit
// a silent subject with three checks nobody ran, and the place that gets discovered is the
// Matériel page of a station where two buttons are missing.
func TestASilentDeclarationIsHeldToTheWholeCatalogue(t *testing.T) {
	subject := brokenSubject(func(s *stubPrinter) { s.refusesADeclaredSelfTest = true })
	subject.SelfTests = nil

	rec := &recorder{}
	guard(func() { checkEverySelfTestAnswersAsDeclared(t, rec, subject) })
	if !rec.failed() {
		t.Fatal("a driver refusing every self-test passed a subject that declared none of them explicitly")
	}
}

// TestTheDemonstrationClauseSkipsOnADriverThatDoesNotPrintALabel keeps the one check that
// cannot judge an undeclared pattern from passing on it.
//
// A driver that does not declare `label` refuses that self-test for a reason of its own —
// KindConfig, in French, naming what is missing — which is EXACTLY the shape this check
// looks for. It would then credit the driver with never inventing a price, on a refusal
// that says nothing about prices at all.
func TestTheDemonstrationClauseSkipsOnADriverThatDoesNotPrintALabel(t *testing.T) {
	subject := referenceSubject()
	subject.SelfTests = []printing.SelfTest{printing.SelfTestAlignment}

	rec := &recorder{}
	guard(func() { checkTheDemonstrationLabelIsNeverInvented(t, rec, subject) })
	if !rec.wasSkipped() {
		t.Fatal("the demonstration clause judged a driver that does not declare the label self-test")
	}
	if rec.failed() {
		t.Fatalf("the check failed instead of skipping: %v", rec.reasons())
	}
}

// TestSuiteRefusesAnInconsistentSubject covers the harness's own guard rails: a subject
// the suite could only misjudge is refused before a single driver is built, because every
// failure after that would blame the driver for the harness's mistake.
func TestSuiteRefusesAnInconsistentSubject(t *testing.T) {
	healthy := referenceSubject()
	for _, tc := range []struct {
		name    string
		subject Subject
	}{
		{"no name", Subject{New: healthy.New}},
		{"no constructor", Subject{Name: "nameless"}},
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

// TestSuiteRefusesATemplateThisDriverCouldNotPrint keeps a WS412 subject submitted with the
// production template from failing every printing check with a geometry fault — which would
// send a contributor looking in their driver for a mistake that is in their subject.
func TestSuiteRefusesATemplateThisDriverCouldNotPrint(t *testing.T) {
	subject := referenceSubject()
	subject.Template = foreign(domain.IdenticalTemplate())

	rec := &recorder{}
	guard(func() { requireTheTemplateFitsTheDriver(t, rec, subject) })
	if !rec.failed() {
		t.Fatalf("the suite accepted a %g dots/mm template on a %d dots/mm driver",
			subject.Template.Media.DotsPerMM, stubDotsPerMM)
	}
	if reasons := rec.reasons(); !strings.Contains(reasons[0], "Subject.Template") {
		t.Fatalf("the refusal does not name the field to set: %v", reasons)
	}
}

// TestSuiteRefusesANilPrinter keeps a nil constructor result from surfacing three frames
// deeper as a nil dereference in the middle of a check.
func TestSuiteRefusesANilPrinter(t *testing.T) {
	subject := referenceSubject()
	subject.New = func(*testing.T, ports.Clock) ports.Printer { return nil }

	rec := &recorder{}
	guard(func() { checkDescriptor(t, rec, subject) })
	if !rec.failed() {
		t.Fatalf("the suite accepted a nil ports.Printer")
	}
}

// TestARefusalWithoutAPrintErrorIsAFailureOfItsOwn holds the other half of §8.5: the print
// service reads Kind and nothing else, so an error that carries none is a refusal it cannot
// act on.
func TestARefusalWithoutAPrintErrorIsAFailureOfItsOwn(t *testing.T) {
	rec := &recorder{}
	if _, ok := printErrorOf(rec, "Print", fmt.Errorf("le code-barres est inutilisable")); ok {
		t.Fatal("a bare error was accepted as a classified refusal")
	}
	if !rec.failed() {
		t.Fatal("a bare error was not reported")
	}
}

// TestTheTwoLanguageHeuristicsTellTheTwoAudiencesApart is the test of clause 17's own
// instrument.
//
// A heuristic that answered « French » to everything would credit every driver with the
// clause, and one that answered « English » to a queue name would refuse a correct message
// — which is worse, because it is the kind of failure a contributor works around.
func TestTheTwoLanguageHeuristicsTellTheTwoAudiencesApart(t *testing.T) {
	for _, tc := range []struct {
		sentence string
		french   bool
		english  bool
	}{
		{"l'imprimante a été fermée par le poste.", true, false},
		{"aucune étiquette de démonstration n'a été fournie à l'imprimante", true, false},
		{"état inconnu : la file « SATO WS408_1 » n'a rien renvoyé en 500ms.", true, false},
		{"le code-barres \"049302100000\" de ce produit est inutilisable", true, false},
		{"raster: New: no transport; this driver carries a frame, it does not open a device", false, true},
		{"preview: New: no directory; this driver writes files and the composition root owns it", false, true},
		{"the printer did not answer", false, true},
		{"C:\\Users\\poste\\AppData\\Local\\Temp\\labels", false, false},
	} {
		t.Run(tc.sentence, func(t *testing.T) {
			if got := looksFrench(tc.sentence); got != tc.french {
				t.Errorf("looksFrench = %v, attendu %v", got, tc.french)
			}
			if got := looksEnglish(tc.sentence); got != tc.english {
				t.Errorf("looksEnglish = %v, attendu %v", got, tc.english)
			}
		})
	}
}

// TestSettledGoroutinesGivesUp keeps the baseline of the leak check from becoming a hang:
// on a machine whose goroutine count never settles, it answers with what it last saw
// instead of waiting for a quiet that is not coming.
func TestSettledGoroutinesGivesUp(t *testing.T) {
	if n := settledGoroutines(time.Nanosecond); n <= 0 {
		t.Fatalf("settledGoroutines = %d, want the live count", n)
	}
}

// --- the harness of this file ----------------------------------------------

// brokenSubject is the reference subject with one betrayal applied to every printer its
// constructors build.
func brokenSubject(betray func(*stubPrinter)) Subject {
	subject := referenceSubject()
	for _, constructor := range []*func(*testing.T, ports.Clock) ports.Printer{
		&subject.New, &subject.Short, &subject.WithoutDemoLabel,
	} {
		build := *constructor
		*constructor = func(t *testing.T, clk ports.Clock) ports.Printer {
			p := build(t, clk).(*stubPrinter)
			betray(p)
			return p
		}
	}
	return subject
}

// undeclaredSubject narrows the declaration to `label` WITHOUT narrowing the driver, which
// is the mismatch the whole declaration exists to catch.
//
// It covers the two halves of clause 11 at once, depending on the betrayal applied on top.
// With none, the stub goes on printing `alignment` and `ruler` it no longer declares: a
// pattern that works and that no screen offers, which is how a self-test quietly stops
// being reachable. With unknownOnACatalogueName, it answers « auto-test inconnu » about a
// name the catalogue carries — the sentence a person who typed the route by hand reads.
func undeclaredSubject(betray func(*stubPrinter)) Subject {
	subject := brokenSubject(betray)
	subject.SelfTests = []printing.SelfTest{printing.SelfTestLabel}
	return subject
}

// frenchDeveloperSubject answers a missing collaborator in the language of the volunteers,
// who will never see that sentence.
func frenchDeveloperSubject() Subject {
	subject := referenceSubject()
	subject.MissingCollaborator = func(*testing.T) error { return newStubWithoutClock(true) }
	return subject
}

// leakingSubject leaves one goroutine behind per job.
//
// The leak has to be REAL for the check to have anything to catch, and it has to end with
// the sub-test, or every case after it starts from a wrong baseline.
func leakingSubject() Subject {
	subject := referenceSubject()
	build := subject.New
	subject.New = func(t *testing.T, clk ports.Clock) ports.Printer {
		p := build(t, clk).(*stubPrinter)
		stop := make(chan struct{})
		t.Cleanup(func() { close(stop) })
		p.leak = stop
		return p
	}
	return subject
}

// runChecks runs every check of the suite against subject through a recorder, and returns
// the names of those that handed down a failure.
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
// precisely so that nobody fakes it. That is the whole reason the checks judge through the
// narrow reporter interface — see the type comment in conformance.go.
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
