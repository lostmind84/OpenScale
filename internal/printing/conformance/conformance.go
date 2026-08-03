// Package conformance is the test suite every printer driver has to pass.
//
// ports.Printer is the widest interface of this application — five methods, a job that
// carries a whole label, a status a volunteer reads and three self-tests a screen offers
// — and it is the one whose breaches reach a customer directly: a bag with no label, a
// green light over a station where nothing comes out, a price nobody can check. What
// Suite buys is the part a contributor cannot know: not the clauses of ports.Printer
// they thought of, the ones that have already sent somebody away holding a blank sheet.
//
// # Every check is a failure mode, never a style rule
//
//  1. Descriptor is a registry key, and it is read BEFORE anything is opened: the
//     administration screen builds its drop-down list from drivers nobody instantiated.
//     An empty ID, a blank or an upper-case letter in it, and printer.type can no longer
//     name the driver in config.json.
//  2. The copy count STAYS INSIDE the bound the driver itself declares. MaxCopies is the
//     width of the <Q> field, six digits: a job past it is refused with the value it was
//     given, never rounded into something that prints.
//  3. Copies == 0 means « unspecified » and takes the setting of the station. The print
//     service builds its PrintJob WITHOUT that field (§8.2), so a driver reading it
//     literally prints nothing at all and the screen still says a label was sent.
//  4. An unusable barcode is refused BEFORE the render, as KindData. The remedy is a
//     product to fix in Odoo, and §8.5 classifies by the action expected of a human.
//  5. A template drawn for another head is refused as KindTemplate. A 12 dots/mm
//     template on a WS408 prints at two thirds of its size, with a symbol under every
//     GS1 floor, and no byte of the frame says so.
//  6. A SHORT WRITE WITH NO ERROR IS A FAILURE, KindTransient. It is the mode that costs
//     the most: the frame is truncated, the label comes out blank, and the station
//     journals a success (§8.3, §8.5).
//  7. Status NEVER claims PrinterReady without the device's own words. A driver with no
//     return channel answers PrinterUnknown — that value exists for it — and a green
//     light on /readyz over an open head is the failure §14.5 refuses.
//  8. Status after Close is Unknown, with a French sentence: it is read on the
//     troubleshooting screen by whoever is standing at the counter.
//  9. Print after Close is refused as KindInternal, without reopening anything. A
//     station that has given up must not be brought back by a job that arrived late.
//  10. Close is idempotent. The Hub closes on a configuration reload and again on
//     shutdown (§11.4, §13.4).
//  11. Every self-test of the catalogue answers AS THE DRIVER DECLARED IT. A declared one
//     PRINTS; an undeclared one is refused by a French sentence that names it and its
//     reason, and never « auto-test inconnu » — that wording, on a name
//     printing.LookupSelfTest accepts, sends a volunteer looking for a typo they did not
//     make (§8.6). The declaration is what the administration screen builds its buttons
//     from, so a driver that honours something it never declared has a pattern no
//     volunteer can launch, and one that refuses what it declared has a button that
//     fails on the click (ADR-025).
//  12. A name outside the catalogue is refused BY NAMING the ones that exist.
//  13. The `label` self-test with no demonstration label refuses in French, naming what
//     is missing, and INVENTS NO PRICE. A driver that made one up would be printing a
//     number nobody could check.
//  14. The clock is the one the driver was GIVEN. A receipt timed on the wall clock
//     cannot be asserted, and `go run ./tools/boundary` walks OUR files, never a
//     contributor's — this check is what stands in for it outside the repository (§5.3).
//  15. Print is SERIALISED: one label at a time, never interleaved. A status probe or a
//     second job slipped into the middle of a 16 ko frame is how a label comes out as
//     garbage, and the legacy guard against it silently ABANDONED the weighing (§8.2).
//  16. No goroutine survives Close.
//  17. Everything an operator reads is FRENCH, and everything only a developer can read
//     — a collaborator missing from a composition root — stays English (§8.2).
//
// # How this suite proves it bites
//
// A conformance suite that nothing can fail verifies nothing. This package therefore
// carries a reference driver that passes and a table of broken ones that must not, each
// betraying exactly one clause; its own tests assert WHICH check catches WHICH betrayal.
// That is the test of the tests, and it is the reason the checks report through a narrow
// interface instead of *testing.T directly.
//
// # One rule for the caller
//
// Do not call t.Parallel around Suite. The leak check compares a process-wide goroutine
// count, and a second driver running beside it would make that number mean nothing.
package conformance

// This file is the entry point and the LIST: Suite, the order the clauses are read in,
// and the two helpers every verdict goes through. The clauses themselves live one family
// per file — check_job.go, check_lifecycle.go, check_selftest.go, check_invariant.go —
// and what a contributor fills in is in subject.go.

import (
	"errors"
	"strings"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// Suite runs every conformance check against subject, each one as a sub-test of t.
//
// It is the whole public surface of this package, with DemoLabel: a driver's own test
// calls it and nothing else.
//
//	func TestConformance(t *testing.T) {
//		conformance.Suite(t, conformance.Subject{
//			Name:      preview.ID,
//			SelfTests: preview.Driver().SelfTests,
//			New: func(t *testing.T, clk ports.Clock) ports.Printer {
//				return newPreview(t, t.TempDir(), clk)
//			},
//		})
//	}
//
// Suite fails t at once when the subject itself is inconsistent — no Name, no New, a
// template no head of this driver could print — because every check after that would
// report the harness's mistake as the driver's.
func Suite(t *testing.T, subject Subject) {
	t.Helper()
	validate(t, subject)
	t.Run(subject.Name, func(t *testing.T) {
		reportWhatGoesUnverified(t, subject)
		requireTheTemplateFitsTheDriver(t, t, subject)
		for _, c := range checks() {
			t.Run(c.name, func(t *testing.T) { c.run(t, t, subject) })
		}
	})
}

// deliveredReaders are the checks that ask what really reached the destination.
//
// Named rather than counted, because « nine checks are weaker » is a number nobody can act
// on and « these nine » is a list somebody can read.
var deliveredReaders = []string{
	"CopiesStayInsideTheDeclaredBound", "AJobWithoutACopyCountStillPrints",
	"AnUnusableBarcodeIsRefusedAsData", "AForeignTemplateIsRefused",
	"PrintAfterCloseIsRefused", "EverySelfTestAnswersAsDeclared",
	"AnUnknownSelfTestNamesTheOnesThatExist", "TheDemonstrationLabelIsNeverInvented",
	"PrintIsSerialised",
}

// reportWhatGoesUnverified says out loud what a nil Subject.Delivered costs, ONCE, before
// any check runs.
//
// Every other optional field of a Subject reports itself SKIPPED, which is a line in the
// test output naming what nobody verified. Delivered is the exception and it is the worst
// one to be silent about: it does not skip a check, it HOLLOWS OUT nine of them. Each keeps
// running, keeps passing, and quietly stops asking the question that matters on a refusal —
// « and nothing came out anyway? ». A driver that emitted a label and then refused the job
// is the failure this suite exists to catch, and without Delivered the suite takes the
// refusal at its word.
//
// Logf and not Skipf: the nine checks still verify their kind, their message and their
// French. They are weaker, not absent, and calling them skipped would be the second lie.
func reportWhatGoesUnverified(r reporter, subject Subject) {
	r.Helper()
	if subject.Delivered != nil {
		return
	}
	r.Logf("Subject.Delivered is nil: %d of the %d checks below can no longer verify that a "+
		"REFUSAL left nothing behind, and they will pass on a driver that printed a label and "+
		"then reported a failure. They are %s.\nSupply it as soon as the destination can be "+
		"counted — the frames a recording transport kept, the files a directory holds — it is "+
		"one closure and it is what turns « Print refused » into proof that nothing was emitted.",
		len(deliveredReaders), len(checks()), strings.Join(deliveredReaders, ", "))
}

// reporter is the slice of *testing.T that a check needs in order to hand down a verdict.
//
// It exists because testing.TB CANNOT be implemented outside package testing: the
// interface carries an unexported method precisely so that nobody fakes it. The checks
// therefore judge through this narrower interface, which *testing.T satisfies as it is,
// and this package's own tests pass a recording double instead. That double is the only
// way to assert that a broken driver really does make the suite fail — the alternative,
// re-executing the test binary as a subprocess to read its exit code, buys the same
// assertion for a process spawn per case and a helper nobody reads.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
}

// check is one clause of ports.Printer.
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
// then what a job carries, then what goes wrong with it, then the status, then the
// exits, then the self-tests, and last the three clauses that hold for all of them.
func checks() []check {
	return []check{
		{"Descriptor", checkDescriptor},
		{"CopiesStayInsideTheDeclaredBound", checkCopiesStayInsideTheDeclaredBound},
		{"AJobWithoutACopyCountStillPrints", checkAJobWithoutACopyCountStillPrints},
		{"AnUnusableBarcodeIsRefusedAsData", checkAnUnusableBarcodeIsRefusedAsData},
		{"AForeignTemplateIsRefused", checkAForeignTemplateIsRefused},
		{"AShortWriteIsATransientFailure", checkAShortWriteIsATransientFailure},
		{"StatusNeverClaimsReadyWithoutProof", checkStatusNeverClaimsReadyWithoutProof},
		{"StatusAfterCloseIsUnknown", checkStatusAfterCloseIsUnknown},
		{"PrintAfterCloseIsRefused", checkPrintAfterCloseIsRefused},
		{"CloseIsIdempotent", checkCloseIsIdempotent},
		{"EverySelfTestAnswersAsDeclared", checkEverySelfTestAnswersAsDeclared},
		{"AnUnknownSelfTestNamesTheOnesThatExist", checkAnUnknownSelfTestNamesTheOnesThatExist},
		{"TheDemonstrationLabelIsNeverInvented", checkTheDemonstrationLabelIsNeverInvented},
		{"TheClockIsTheOneTheDriverWasGiven", checkTheClockIsTheOneTheDriverWasGiven},
		{"PrintIsSerialised", checkPrintIsSerialised},
		{"NoGoroutineLeaks", checkNoGoroutineLeaks},
		{"OperatorMessagesAreFrench", checkOperatorMessagesAreFrench},
		{"ADeveloperMessageStaysEnglish", checkADeveloperMessageStaysEnglish},
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
	case subject.Template.Media.DotsPerMM < 0:
		r.Fatalf("conformance: Subject.Template declares %g dots/mm; leave it zero to print the production label",
			subject.Template.Media.DotsPerMM)
	}
}

// requireTheTemplateFitsTheDriver refuses to print a template this driver was never going
// to accept.
//
// Without it, a WS412 driver submitted with the default template would fail EVERY
// printing check with a geometry fault, and a contributor would go looking in their
// driver for a mistake that is in their subject.
func requireTheTemplateFitsTheDriver(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	declared := p.Descriptor().Capabilities.DotsPerMM
	if declared <= 0 {
		return
	}
	if wanted := subject.template().Media.DotsPerMM; declared != wanted {
		r.Fatalf("conformance: this driver declares %g dots/mm and the suite would print a %g dots/mm template. Set Subject.Template to a layout this head can burn — the resolution of the whole application has ONE source, template.media.dots_per_mm (mineur-3)",
			declared, wanted)
	}
}

// build calls one of the subject's constructors and refuses a nil driver, which would
// otherwise surface as a nil dereference three frames deeper.
func build(t *testing.T, r reporter, constructor func(*testing.T, ports.Clock) ports.Printer,
	clk ports.Clock) ports.Printer {
	r.Helper()
	p := constructor(t, clk)
	if p == nil {
		r.Fatalf("the Subject constructor returned a nil ports.Printer")
	}
	return p
}

// printErrorOf reports the ports.PrintError inside err.
//
// Every refusal of a printer driver is one, and that is the contract rather than a habit:
// the print service reads Kind ALONE to decide between two retries and none (§8.5). A bare
// error leaves it with nothing to read, so it is a failure of its own and the caller stops
// there.
func printErrorOf(r reporter, op string, err error) (*ports.PrintError, bool) {
	r.Helper()
	var fault *ports.PrintError
	if !errors.As(err, &fault) {
		r.Errorf("%s refused with %T (%v) instead of a *ports.PrintError. The print service reads Kind and nothing else to decide whether to retry, what the customer screen says and what the admin screen names (§8.5): an error without one is classified KindInternal by default, which never retries and blames this binary", op, err, err)
		return nil, false
	}
	return fault, true
}
