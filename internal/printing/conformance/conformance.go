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

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// Subject is the printer driver submitted to the suite.
//
// Two fields are mandatory, Name and New; every other one widens what the suite can
// reach. That asymmetry is the point: submitting a driver must cost one function
// literal, and a contributor who supplies nothing else still gets the checks that need
// no seam.
type Subject struct {
	// Name is the driver under test, spelled as its registry key: "raster". It names the
	// sub-test group, so it appears in every failure line, and Descriptor().ID must
	// return it.
	Name string

	// New returns ONE fresh driver, built and ready to print.
	//
	// It is called once per check — sometimes several times inside one — because a driver
	// that has been closed is not a fair subject for the next clause. clk is the clock the
	// driver MUST take its instants from: the suite hands over a fake one anchored far in
	// the past and then reads the durations that come back.
	//
	// The driver it builds is COMPLETE, demonstration label included: wire
	// conformance.DemoLabel into whatever option your driver takes for it, and the
	// self-test checks then exercise a real print instead of a refusal.
	New func(t *testing.T, clk ports.Clock) ports.Printer

	// SelfTests are the patterns of §8.6 this driver HONOURS, and the right value is the
	// one its registry entry carries — `SelfTests: raster.Driver().SelfTests`. Handing over
	// the same list twice is what makes the declaration verifiable instead of decorative:
	// a pattern added to the entry and never implemented turns red here, and so does one
	// implemented and never declared, which is a button the screen would not draw.
	//
	// NIL MEANS THE WHOLE CATALOGUE, which is the strongest reading and the one a
	// production driver answers to: a subject that says nothing is held to all three. A
	// driver that honours fewer says so, and the EMPTY slice — `[]printing.SelfTest{}` —
	// is the assertion « none », never an omission.
	SelfTests []printing.SelfTest

	// Template is the label layout the suite prints, and it must be one THIS driver
	// accepts: its media resolution is compared against the DotsPerMM the descriptor
	// declares before a single check runs.
	//
	// The zero value means domain.IdenticalTemplate(), the production label at 8 dots/mm,
	// which is what the whole parc prints today. A driver for a 12 dots/mm head sets it.
	Template domain.Template

	// JobAdvancesTheClock is how far ONE job moves the injected clock through the seam
	// this subject supplies — the write delay a recording transport charges, the time a
	// fake device takes.
	//
	// ZERO IS THE COMMON CASE AND THE STRONGEST FORM of clause 14: a seam that charges
	// nothing means an honest driver reports a duration of EXACTLY zero, and a driver
	// that timed itself on the wall clock cannot.
	JobAdvancesTheClock time.Duration

	// Delivered reports how many complete labels reached the destination over the whole
	// life of the driver New just built: the frames a transport accepted, the files that
	// were written.
	//
	// It is what turns « Print returned nil » into an assertion, and « Print refused »
	// into the proof that NOTHING was emitted before the refusal. Leave it nil when the
	// destination cannot be read back — and know that those assertions then go
	// unverified, and that the refusal checks are the weaker for it.
	Delivered func(t *testing.T, p ports.Printer) int

	// Copies reports how many copies of the label the LAST job asked the destination for:
	// the <Q> field of the frame, the number of files a preview wrote.
	//
	// It is only ever read when a job asking for MORE than the declared bound was
	// ACCEPTED, which is the one case the returned error cannot settle. A driver that
	// refuses out-of-range counts — the raster driver does — never needs it.
	Copies func(t *testing.T, p ports.Printer) int

	// Short builds a driver whose destination accepts FEWER bytes than it is given,
	// without an error of its own. That is WritePrinter's real behaviour, and clause 6 is
	// the one a driver breaks by returning a receipt for a truncated frame.
	//
	// Leave it nil for a driver whose destination cannot be short — a preview writing a
	// file has no such mode — and the check reports itself SKIPPED rather than passed.
	Short func(t *testing.T, clk ports.Clock) ports.Printer

	// WithoutDemoLabel builds the SAME driver with no demonstration label wired into it,
	// which is the configuration of a station whose composition root supplied none.
	//
	// Supply it: it is the clause whose breach puts an invented price on a printed label,
	// and the driver that would do it is one `if` away from the one that refuses.
	WithoutDemoLabel func(t *testing.T, clk ports.Clock) ports.Printer

	// MissingCollaborator calls your CONSTRUCTOR with a mandatory collaborator left out —
	// no transport, no directory — and returns what it answered.
	//
	// It is the other half of clause 17: no configuration file can produce a nil
	// transport, so that message is read by a developer and stays English, exactly as the
	// messages a volunteer reads stay French.
	MissingCollaborator func(t *testing.T) error

	// DrivesAHead declares that this driver addresses a REAL print head whose resolution
	// is a hardware fact.
	//
	// At false, the geometry check reports itself SKIPPED and says what is left
	// unverified. That is not a courtesy: `preview` writes a file at whatever pitch the
	// job's template declares, so no template is foreign to it and refusing one would
	// take a station in factory configuration out of the one thing it can still do.
	DrivesAHead bool

	// Patience is how long the suite waits, ON THE WALL CLOCK, for a driver to do what it
	// said it would: hand its bytes over, let its goroutines go. Zero means
	// defaultPatience.
	//
	// Wall clock, in a repository where everything else runs on an injected fake, because
	// what is bounded here is a goroutine leaving blocking OS I/O. Raise it for a
	// destination that is genuinely slow; do not raise it to make a flaky driver pass.
	Patience time.Duration
}

// patience reports the wall-clock budget of one wait.
func (s Subject) patience() time.Duration {
	if s.Patience > 0 {
		return s.Patience
	}
	return defaultPatience
}

// honours reports whether this subject declared that self-test.
//
// A nil declaration is the WHOLE catalogue and never « none »: a contributor who left the
// field out is held to the strongest clause, because the other reading would credit a
// silent subject with three checks nobody ran.
func (s Subject) honours(what printing.SelfTest) bool {
	if s.SelfTests == nil {
		return true
	}
	for _, declared := range s.SelfTests {
		if declared == what {
			return true
		}
	}
	return false
}

// template reports the layout the suite prints, which is the production label unless the
// subject named another.
func (s Subject) template() domain.Template {
	if s.Template.Media.DotsPerMM > 0 {
		return s.Template
	}
	return domain.IdenticalTemplate()
}

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

// checkDescriptor verifies the identity the registry, the configuration file and the
// admin form all read.
//
// Descriptor is called before anything is printed, because that is when the Hub calls
// it: the drop-down list of printer.type is built from drivers nobody has opened yet.
func checkDescriptor(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	descriptor := p.Descriptor()
	switch {
	case descriptor.ID == "":
		r.Errorf("Descriptor().ID is empty. It is the key of the driver registry and the value of printer.type in config.json: an anonymous driver cannot be named by a configuration file, and the admin screen has nothing to generate its form from")
	case descriptor.ID != subject.Name:
		r.Errorf("Descriptor().ID = %q while the subject is submitted as %q. Those two are the same string in config.json, and a driver that answers to a name nobody registered is unreachable", descriptor.ID, subject.Name)
	}
	if descriptor.Label == "" {
		r.Errorf("Descriptor().Label is empty. It is the line a volunteer picks in the drop-down list, and it has to say what the driver DOES: « Aperçu — écrit un fichier, n'imprime rien » is one wrong click away from the production path (§8.2)")
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsSpace); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", descriptor.ID, i)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsUpper); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different driver", descriptor.ID, i, strings.ToLower(descriptor.ID))
	}
	if again := p.Descriptor(); again != descriptor {
		r.Errorf("Descriptor() answered %+v then %+v. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", descriptor, again)
	}
}

// checkCopiesStayInsideTheDeclaredBound holds a driver to the ceiling IT declared.
//
// The ceiling is a fact about the wire — MaxCopies is the width of the <Q> field, six
// digits — and not a shop policy. A job past it is refused with the value it was given,
// never rounded into something that prints: a count quietly turned into one is a volunteer
// pressing a button that no longer does what it says.
func checkCopiesStayInsideTheDeclaredBound(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	bound := p.Descriptor().Capabilities.MaxCopies
	if bound < 1 {
		r.Fatalf("Descriptor().Capabilities.MaxCopies = %d. A driver that cannot print ONE copy cannot print at all: the admin form would offer an empty range, and the print service has no count left to send", bound)
	}

	job := subject.referenceJob(r, "01J9F2ABC")
	job.Copies = bound + 1
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a copy count of %d: %v. An out-of-range count is a caller's mistake, not a programming error, and a panic here takes the station down", job.Copies, panicked)
	}
	if err == nil {
		if subject.Copies == nil {
			r.Skipf("Print ACCEPTED %d copies while the driver declares a ceiling of %d, and Subject.Copies is nil: the suite cannot count what really left, so it took the receipt at its word. Supply it as soon as the destination can be counted — a driver that emits what it was asked for past its own bound is a <Q> field that overflows and a frame the printer cannot read", job.Copies, bound)
		}
		if got := subject.Copies(t, p); got > bound {
			r.Errorf("Print accepted %d copies and %d really left, past the %d this driver declares. MaxCopies is the width of the <Q> field, six digits: a count past it is not a big print run, it is a malformed frame", job.Copies, got, bound)
		}
		_ = receipt
		return
	}

	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Retryable() {
		r.Errorf("Print refused %d copies as %s, which the print service RETRIES twice, 300 ms then 1 s (§8.5). A count out of range will not come back into range on the second attempt: it is two more seconds of a customer in front of a screen that was never going to print", job.Copies, fault.Kind)
	}
	if !strings.Contains(fault.Message, fmt.Sprint(job.Copies)) || !strings.Contains(fault.Message, fmt.Sprint(bound)) {
		r.Errorf("Print refused %d copies with « %s », which names neither the count it was given nor the %d it accepts. « valeur invalide » tells nobody what to type instead (ports.PrintError.Message)", job.Copies, fault.Message, bound)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("Print refused %d copies and %d label(s) reached the destination anyway. A refusal that already printed is worse than a print: the station reports a failure the customer is holding in their hand", job.Copies, delivered)
	}
}

// checkAJobWithoutACopyCountStillPrints is the other end of the same field, and it is the
// one a driver breaks by reading job.Copies literally.
//
// The print service builds its PrintJob WITHOUT a Copies field (§8.2). Zero therefore
// means « unspecified » and printer.options.copies is the answer; a driver that took it
// as « none » would print nothing at all while the screen says « Étiquette envoyée à
// l'imprimante ».
func checkAJobWithoutACopyCountStillPrints(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABD")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on the reference weighing: %v", panicked)
	}
	if err != nil {
		r.Fatalf("Print returned %v on a job the subject declares printable. Subject.New must build a driver that can print in the test environment — a recording transport, a temporary directory — and Copies is left at ZERO here on purpose: that is how the print service sends it (§8.2)", err)
	}
	if receipt.JobID != job.Label.JobID {
		r.Errorf("the receipt carries JobID %q for a job submitted as %q. That identifier is what ties the acknowledgement on screen to the line in the journal and to the reprint bar (§8.2)", receipt.JobID, job.Label.JobID)
	}
	if receipt.Bytes <= 0 {
		r.Errorf("the receipt reports %d bytes for a label that printed. The count is what the journal is kept for, and a zero says an encoder produced nothing", receipt.Bytes)
	}
	if delivered, known := subject.delivered(t, p); known && delivered == 0 {
		r.Errorf("Print returned a receipt and NOTHING reached the destination. Copies == 0 means « unspecified » and takes printer.options.copies (§8.2): a driver that read it as « none » sends a customer away with a bag and no label, while the screen says one was printed")
	}
}

// checkAnUnusableBarcodeIsRefusedAsData is the classification of §8.5 in one job: the
// remedy is a product to fix in Odoo, so the kind is KindData, the product is flagged,
// and nothing is retried.
//
// BEFORE the render, and the destination is what proves it: a driver that drew the label
// first would burn the rendering budget of every unusable product, and — worse — would
// hand over a symbol nobody can scan if it also forgot to check.
func checkAnUnusableBarcodeIsRefusedAsData(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABE")
	job.Label.Barcode = unusableBarcode
	_, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on the barcode %q: %v. A catalog that carries an unusable reference is an ordinary Tuesday, not a programming error", string(unusableBarcode), panicked)
	}
	if err == nil {
		r.Fatalf("Print ACCEPTED the barcode %q, which is not thirteen valid digits. What comes out is a label whose symbol no till can scan, and the customer discovers it at the checkout with a queue behind them (§8.5)", string(unusableBarcode))
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindData {
		r.Errorf("Print refused the barcode %q as %s, want %s. The kind decides the ACTION (§8.5): KindData flags the product and sends somebody to Odoo, where %s points at a template, a setting or a device that has nothing to do with it", string(unusableBarcode), fault.Kind, ports.KindData, fault.Kind)
	}
	if !strings.Contains(fault.Message, string(unusableBarcode)) {
		r.Errorf("the refusal says « %s » without naming the offending code %q. It is the value somebody has to go and correct in the catalog, and a message that omits it sends them looking through 355 products", fault.Message, string(unusableBarcode))
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("the barcode was refused and %d label(s) reached the destination anyway: the check happened AFTER the render and after the write. An unusable reference is caught before anything is drawn (§8.5)", delivered)
	}
}

// checkAForeignTemplateIsRefused is the geometry clause, and it is a clause about a HEAD.
//
// The resolution of the whole application has one source, template.media.dots_per_mm
// (mineur-3), and a driver's capability is what it is COMPARED to. A 12 dots/mm template
// sent to a WS408 prints at two thirds of its size, with a symbol under every GS1 floor,
// and no byte of the frame says so: the label simply comes out wrong.
func checkAForeignTemplateIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if !subject.DrivesAHead {
		r.Skipf("Subject.DrivesAHead is false: this driver addresses no print head, so no template is foreign to it and the suite verifies nothing here. Set it for anything that burns dots — a template drawn for another pitch is the one fault that produces a WRONG label rather than no label at all")
	}
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABF")
	job.Template = foreign(subject.template())
	_, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a template of %g dots/mm: %v", job.Template.Media.DotsPerMM, panicked)
	}
	if err == nil {
		r.Fatalf("Print ACCEPTED a %g dots/mm template on a head this driver declares at %g. The label comes out at another scale, with a symbol below every GS1 floor, and nothing in the frame says so — it is the one fault a volunteer cannot see without a caliper", job.Template.Media.DotsPerMM, subject.template().Media.DotsPerMM)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindTemplate {
		r.Errorf("Print refused a foreign template as %s, want %s. A template that does not fit will not fit any better on a second attempt, and §8.5 keeps the kinds apart by the action each one calls for", fault.Kind, ports.KindTemplate)
	}
	if fault.Retryable() {
		r.Errorf("Print refused a foreign template with a RETRYABLE kind: the print service would try it twice more, 300 ms then 1 s, for a geometry that cannot change in between (§8.2)")
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("the template was refused and %d label(s) reached the destination anyway", delivered)
	}
}

// checkAShortWriteIsATransientFailure is the failure mode that costs the most, and the
// one a driver breaks by returning a receipt for what it handed over instead of comparing
// it to what it built.
//
// WritePrinter really does report a short count with no error of its own. The frame is
// truncated, the label prints blank or not at all, and the station journals result='sent'
// for a label nobody ever held (§8.3, §8.5).
func checkAShortWriteIsATransientFailure(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Short == nil {
		r.Skipf("Subject.Short is nil: this subject offers no way to make its destination accept fewer bytes than it is given. Supply it for anything that reaches a device — a short write with NO error is what WritePrinter does, and a driver that passes it on turns a lost label into a confirmed one")
	}
	p := build(t, r, subject.Short, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2AC0")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a short write: %v", panicked)
	}
	if err == nil {
		r.Fatalf("the destination took fewer bytes than the frame holds and Print reported SUCCESS with %d bytes. A truncated frame prints blank, and the station would journal a label the customer never held (§8.3, §8.5)", receipt.Bytes)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindTransient {
		r.Errorf("Print reported a short write as %s, want %s. It is the ONLY kind the print service retries — two attempts, 300 ms then 1 s (§8.2) — and a spooler that took a partial frame once usually takes the whole one next time", fault.Kind, ports.KindTransient)
	}
}

// checkStatusNeverClaimsReadyWithoutProof is §14.5 in one line: readiness is a claim, and
// a claim needs the device's own words.
//
// PrinterReady means « answered and has NOTHING to report ». A driver with no return
// channel answers PrinterUnknown — that value exists exactly for it — and a green light
// over an open head is how a station reports itself healthy while every customer walks
// away empty-handed.
func checkStatusNeverClaimsReadyWithoutProof(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	declared := p.Descriptor().Capabilities.Status
	status, panicked := statusQuietly(p, context.Background())
	if panicked != nil {
		r.Fatalf("Status PANICKED: %v. It is called by the troubleshooting screen and by /readyz, on a station that is already in trouble", panicked)
	}

	switch status.Health {
	case ports.PrinterUnknown, ports.PrinterReady, ports.PrinterConsumable, ports.PrinterFaulted:
	default:
		r.Errorf("Status().Health = %d is outside the vocabulary. The four values are what the maintenance light and /readyz switch on (§14.5)", status.Health)
	}
	if status.Health == ports.PrinterReady && len(status.Raw) == 0 {
		r.Errorf("Status() answered PrinterReady with an EMPTY Raw. Ready means « the device answered and has nothing to report »: without the frame it answered with, that is a guess, and it is the guess that puts /readyz at green over an empty roll (§14.5, §8.5)")
	}
	if !declared && status.Health != ports.PrinterUnknown {
		r.Errorf("Descriptor() declares Capabilities.Status = false and Status() answered %d. A driver with no return channel knows nothing about the device: PrinterUnknown is the honest answer and the reason that value exists (§8.5, important-7)", status.Health)
	}
	if status.Detail == "" {
		r.Errorf("Status().Detail is empty. It is the sentence a volunteer reads on the troubleshooting screen when nothing comes out, and « impression indisponible » with nothing after it sends them to the wrong printer")
	}
}

// checkStatusAfterCloseIsUnknown covers the window between a configuration reload and the
// next driver: the screen still asks, and the answer may not be a verdict about a device
// nobody holds any more.
func checkStatusAfterCloseIsUnknown(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	if _, panicked := closeQuietly(p); panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}

	status, panicked := statusQuietly(p, context.Background())
	if panicked != nil {
		r.Fatalf("Status PANICKED after Close: %v. The troubleshooting screen polls it, and a reload is exactly when somebody is looking at that screen", panicked)
	}
	if status.Health != ports.PrinterUnknown {
		r.Errorf("Status() answered %d after Close, want PrinterUnknown. A driver that has given up its device knows nothing about it: reporting a fault would send a volunteer to look at a healthy printer, and reporting readiness would put /readyz at green over a station that can no longer print (§14.5)", status.Health)
	}
	if !looksFrench(status.Detail) {
		r.Errorf("Status().Detail after Close is « %s », and a volunteer reads it on the troubleshooting screen. Every line of that screen is French (§8.2)", status.Detail)
	}
}

// checkPrintAfterCloseIsRefused keeps a station that has given up from being brought back
// by a job that arrived late.
//
// KindInternal, and that is the taxonomy doing its work: a job sent to a closed driver is
// a bug in this binary, it is never retried, and it says so — where KindTransient would
// have the print service try twice more against a device nobody holds.
func checkPrintAfterCloseIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	if _, panicked := closeQuietly(p); panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}

	job := subject.referenceJob(r, "01J9F2AC1")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED after Close: %v. A reload is a close followed by a new driver (§11.4), and a job in flight over that boundary is a race, not a programming error", panicked)
	}
	if err == nil {
		r.Fatalf("Print reported %d bytes and no error AFTER Close. A job that reopens the device behind the closed one prints on hardware the station believes it has released, and two drivers then race for the same handle (§11.4)", receipt.Bytes)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindInternal {
		r.Errorf("Print after Close was refused as %s by %s, with « %s », and the kind should be %s.\nThe kind is the only field the print service reads, and it is what the ADMIN screen names: %s sends a volunteer to look at printer.template, which is not what is wrong. A driver that checks `closed` only where it hands its bytes over never reaches that check, because a job composed on resources Close already released fails EARLIER — and the failure it fails with is about whatever Close happened to release first (§8.5)",
			fault.Kind, fault.Op, fault.Message, ports.KindInternal, fault.Kind)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("Print was refused after Close and %d label(s) reached the destination anyway: the driver REOPENED what the station had released", delivered)
	}
}

// checkCloseIsIdempotent covers both calls the Hub really makes: the one on a
// configuration reload and the one on shutdown that follows it (§11.4, §13.4).
func checkCloseIsIdempotent(t *testing.T, r reporter, subject Subject) {
	r.Helper()

	// A driver that never printed. The composition root reaches this every time a
	// configuration is refused after the drivers were built.
	idle := build(t, r, subject.New, fake.NewClock(t0))
	for call := 1; call <= 3; call++ {
		if _, panicked := closeQuietly(idle); panicked != nil {
			r.Fatalf("Close PANICKED on call %d of a driver that never printed: %v. Close releases what was taken and says nothing about what was not", call, panicked)
		}
	}

	// And a driver that did a job. Returning an error on the second call is allowed — a
	// handle already released is not news — but a panic takes the whole station down.
	used := build(t, r, subject.New, fake.NewClock(t0))
	if _, err, panicked := printQuietly(used, context.Background(), subject.referenceJob(r, "01J9F2AC2")); panicked != nil || err != nil {
		r.Fatalf("Print returned (%v, %v) on the reference weighing", err, panicked)
	}
	for call := 1; call <= 3; call++ {
		err, panicked := closeQuietly(used)
		if panicked != nil {
			r.Fatalf("Close PANICKED on call %d after a job: %v. The Hub closes on a reload and again on shutdown (§11.4, §13.4)", call, panicked)
		}
		if err != nil {
			// Allowed and logged rather than judged: a handle already released is not news.
			r.Logf("Close returned %v on call %d", err, call)
		}
	}
}

// checkEverySelfTestAnswersAsDeclared holds a driver to the table of §8.6 AND to what its
// registry entry says it does with each line of it.
//
// Every name printing.LookupSelfTest accepts gets an ANSWER, and the declaration decides
// WHICH one. A pattern the driver DECLARES has to come out: that declaration is what the
// Matériel page draws its buttons from, so a refusal there is a button that fails on the
// click, in front of somebody already looking for why nothing prints. A pattern it does
// NOT declare has to be refused, and refused usefully — the sentence names the test and
// says why, « cet auto-test se lit sur une étiquette imprimée » being a complete answer.
// What it may never say is « auto-test inconnu » about a name the catalogue carries: that
// sends a volunteer hunting for a typo they did not make.
//
// The other direction matters as much and is the reason this check reads a declaration at
// all: a driver that prints a pattern it never declared has a self-test no screen offers,
// which is the same fault seen from the other side (ADR-025).
func checkEverySelfTestAnswersAsDeclared(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	for _, known := range printing.SelfTests() {
		p := build(t, r, subject.New, fake.NewClock(t0))
		what := string(known.ID)
		err, panicked := selfTestQuietly(p, context.Background(), what)
		delivered, deliveredKnown := subject.delivered(t, p)
		closeAndForget(p)

		if panicked != nil {
			r.Fatalf("SelfTest(%q) PANICKED: %v. It is a button on the Dépannage page, reachable without a password (ADR-018)", what, panicked)
		}
		if subject.honours(known.ID) {
			checkADeclaredSelfTestPrinted(r, known, err, delivered, deliveredKnown)
			continue
		}
		checkAnUndeclaredSelfTestWasRefused(r, known, err, delivered, deliveredKnown)
	}
}

// checkADeclaredSelfTestPrinted is the verdict on a pattern this driver said it honours.
func checkADeclaredSelfTestPrinted(r reporter, known printing.SelfTestInfo, err error,
	delivered int, deliveredKnown bool) {
	r.Helper()
	what := string(known.ID)
	if err != nil {
		r.Errorf("SelfTest(%q) refused with %v while this driver DECLARES it in its registry entry. The %s screen builds the button « %s » from that declaration (§8.6): declaring a pattern and refusing it is exactly the button that fails on the click, which is what declaring them was for (ADR-025)", what, err, known.Access, known.Button)
		return
	}
	if deliveredKnown && delivered == 0 {
		r.Errorf("SelfTest(%q) reported SUCCESS and NOTHING reached the destination. This driver declares that pattern, so a volunteer pressing « %s » is told a label went out and stands in front of a printer that never moved", what, known.Button)
	}
}

// checkAnUndeclaredSelfTestWasRefused is the verdict on a pattern this driver left out of
// its declaration, and the refusal has to be one a volunteer could act on.
func checkAnUndeclaredSelfTestWasRefused(r reporter, known printing.SelfTestInfo, err error,
	delivered int, deliveredKnown bool) {
	r.Helper()
	what := string(known.ID)
	if err == nil {
		r.Errorf("SelfTest(%q) ANSWERED a pattern this driver does not declare. The declaration is what the %s screen draws its buttons from, so a self-test that works and is not declared is one no volunteer can ever launch — and the day somebody needs it, the button is not there (§8.6, ADR-025)", what, known.Access)
		return
	}
	fault, ok := printErrorOf(r, fmt.Sprintf("SelfTest(%q)", what), err)
	if !ok {
		return
	}
	if strings.Contains(fault.Message, unknownSelfTest) {
		r.Errorf("SelfTest(%q) answered « %s » about a self-test the CATALOGUE carries: %s offers it as the button « %s » (§8.6). A driver that does not produce it says why — « il se lit sur une étiquette imprimée » — and never that it never heard of it. The route is reachable outside the screen, so this sentence is read by whoever typed the name", what, fault.Message, known.Access, known.Button)
	}
	if !strings.Contains(fault.Message, what) {
		r.Errorf("SelfTest(%q) refused with « %s », which does not name the test it refused. A volunteer who pressed one of three buttons has to know which one answered", what, fault.Message)
	}
	if !looksFrench(fault.Message) {
		r.Errorf("SelfTest(%q) refused with « %s ». That sentence is shown on the Dépannage page, in French (§8.2)", what, fault.Message)
	}
	if deliveredKnown && delivered > 0 {
		r.Errorf("SelfTest(%q) was refused and %d label(s) reached the destination anyway. A refusal that already printed is worse than a print: the roll is spent and the screen reports a failure", what, delivered)
	}
}

// checkAnUnknownSelfTestNamesTheOnesThatExist is the same refusal from the other side: a
// name outside the table is refused, and the refusal is USEFUL.
//
// It names what exists, exactly as printing.LookupSelfTest does and as an unknown
// printer.type does (§11.3). A bare « inconnu » leaves whoever typed it with nothing to
// try next.
//
// What the refusal lists is THE CATALOGUE and not this driver's declaration, and the two
// are different lists on `preview`. The name that was typed is not in the table at all, so
// what the person needs is the three spellings that are — the one that fits their driver
// then answers, and the two that do not say why in their own words.
func checkAnUnknownSelfTestNamesTheOnesThatExist(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	for _, what := range []string{"mire", ""} {
		p := build(t, r, subject.New, fake.NewClock(t0))
		err, panicked := selfTestQuietly(p, context.Background(), what)
		delivered, deliveredKnown := subject.delivered(t, p)
		closeAndForget(p)

		if panicked != nil {
			r.Fatalf("SelfTest(%q) PANICKED: %v. The name arrives from an HTTP query parameter, so anything can be in it", what, panicked)
		}
		if err == nil {
			r.Errorf("SelfTest(%q) reported SUCCESS on a name no self-test answers to. The three of §8.6 are a closed table: a driver that accepts a fourth is one that would print whatever a mistyped URL asks for", what)
			if deliveredKnown && delivered > 0 {
				r.Errorf("SelfTest(%q) also burnt %d label(s) doing it", what, delivered)
			}
			continue
		}
		fault, ok := printErrorOf(r, fmt.Sprintf("SelfTest(%q)", what), err)
		if !ok {
			continue
		}
		for _, known := range printing.SelfTests() {
			if !strings.Contains(fault.Message, string(known.ID)) {
				r.Errorf("SelfTest(%q) refused with « %s », which does not name %q. A refusal that lists what EXISTS is what turns a mistyped name into a next attempt, and it is what printing.LookupSelfTest and an unknown printer.type both do (§11.3)", what, fault.Message, string(known.ID))
			}
		}
	}
}

// checkTheDemonstrationLabelIsNeverInvented is the boundary of §8.6, and it is a boundary
// rather than a formality.
//
// A demonstration label carries a product, a unit price and a pricing grid, which are
// catalog and configuration. A printing driver that made up a price would be printing a
// number nobody could check — and somebody WILL lay that label over a real one on a light
// table and read the price off it.
func checkTheDemonstrationLabelIsNeverInvented(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if !subject.honours(printing.SelfTestLabel) {
		r.Skipf("this driver does not declare the %q self-test, so the refusal it answers here is the one for an undeclared pattern and says nothing about an invented price. Nothing is verified: the clause belongs to a driver that really produces a demonstration label (§8.6)", printing.SelfTestLabel)
	}
	if subject.WithoutDemoLabel == nil {
		r.Skipf("Subject.WithoutDemoLabel is nil: the suite cannot build this driver without the demonstration label it is normally given, so it took the refusal on trust. Supply it — it is one constructor call with one field left out, and it is the clause whose breach puts an invented price on a printed label (§8.6)")
	}
	p := build(t, r, subject.WithoutDemoLabel, fake.NewClock(t0))
	defer closeAndForget(p)

	err, panicked := selfTestQuietly(p, context.Background(), string(printing.SelfTestLabel))
	if panicked != nil {
		r.Fatalf("SelfTest(%q) PANICKED with no demonstration label: %v. A station whose composition root supplied none is a configuration, not a bug", printing.SelfTestLabel, panicked)
	}
	if err == nil {
		r.Fatalf("SelfTest(%q) reported SUCCESS with NO demonstration label wired in. Whatever came out carries a product and prices this driver invented, and the gesture that goes with this self-test is to lay the result over a real label and compare them (§8.6)", printing.SelfTestLabel)
	}
	fault, ok := printErrorOf(r, "SelfTest", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindConfig {
		r.Errorf("the refusal is %s, want %s. Nothing is wrong with the catalog or the printer here: a collaborator is missing from the station's configuration, and that is the kind whose screen shows what is configured against what exists (§8.5)", fault.Kind, ports.KindConfig)
	}
	if !looksFrench(fault.Message) {
		r.Errorf("the refusal reads « %s ». It appears on the Dépannage page under a button a volunteer just pressed, in French (§8.2)", fault.Message)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("no demonstration label was supplied and %d label(s) came out anyway: this driver invented what it could not be given", delivered)
	}
}

// checkTheClockIsTheOneTheDriverWasGiven is the only check that reaches code outside this
// repository.
//
// `go run ./tools/boundary` walks the AST of OUR files and fails on any call to time.Now;
// a contributor's driver is not in it. What stands in for it here is the injected clock:
// the suite anchors it far in the past and hands the driver a seam that moves it by a
// KNOWN amount, so the duration on the receipt is an arithmetic fact. A driver that timed
// itself on the wall clock cannot produce it — and when the seam charges nothing, it
// cannot produce a zero either (§5.3).
func checkTheClockIsTheOneTheDriverWasGiven(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	defer closeAndForget(p)

	receipt, err, panicked := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC3"))
	if panicked != nil {
		r.Fatalf("Print PANICKED: %v", panicked)
	}
	if err != nil {
		r.Fatalf("Print returned %v on the reference weighing", err)
	}
	if receipt.Duration != subject.JobAdvancesTheClock {
		r.Errorf("PrintReceipt.Duration = %s while the clock the suite HANDED YOU moved by %s over the whole job (Subject.JobAdvancesTheClock). A driver that read time.Now cannot report that figure, and one that did read the injected clock cannot report any other. Failure test 6 — a printer hanging for 60 s — is instantaneous only because every budget is measured on this clock (§5.3, §16.4)",
			receipt.Duration, subject.JobAdvancesTheClock)
	}
	if moved := clk.Now().Sub(t0); moved != subject.JobAdvancesTheClock {
		r.Logf("the injected clock moved by %s over the job, and the subject declares %s", moved, subject.JobAdvancesTheClock)
	}
}

// checkPrintIsSerialised is §8.2: ONE label at a time, never interleaved.
//
// Two jobs inside the same driver at once is not a theoretical concern — the reprint bar,
// the troubleshooting screen and the weighing path all reach the same instance — and the
// legacy guard against it was an `If AllReports(...).IsLoaded Then Exit Sub` that silently
// ABANDONED the weighing. What is asserted here is that each caller gets ITS OWN receipt:
// a driver keeping the job in flight in a field of its own hands back somebody else's
// identifier, and that identifier is what the reprint bar reprints.
func checkPrintIsSerialised(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	const jobs = 8
	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	defer closeAndForget(p)

	type outcome struct {
		wanted   string
		receipt  ports.PrintReceipt
		err      error
		panicked any
	}
	results := make(chan outcome, jobs)
	var wg sync.WaitGroup
	for i := range jobs {
		job := subject.referenceJob(r, fmt.Sprintf("01J9F2AD%d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err, panicked := printQuietly(p, context.Background(), job)
			results <- outcome{job.Label.JobID, receipt, err, panicked}
		}()
	}
	wg.Wait()
	close(results)

	for got := range results {
		switch {
		case got.panicked != nil:
			r.Errorf("Print PANICKED while %d jobs were in flight: %v. The station reaches one driver from the weighing path, the reprint bar and the troubleshooting screen (§8.2)", jobs, got.panicked)
		case got.err != nil:
			r.Errorf("Print returned %v for job %q while %d were in flight. Serialising is a wait, never a refusal: the legacy application dropped the second weighing on the floor instead", got.err, got.wanted, jobs)
		case got.receipt.JobID != got.wanted:
			r.Errorf("the caller of job %q got back a receipt for %q. Two jobs crossed inside the driver: the acknowledgement on screen, the line in the journal and the reprint bar all name a label that belongs to somebody else (§8.2)", got.wanted, got.receipt.JobID)
		case got.receipt.Duration != subject.JobAdvancesTheClock:
			r.Errorf("job %q reports a duration of %s while one job moves the injected clock by %s. A window that covers more than its own job is a window that overlapped another one: the frames were interleaved on the way to the head (§8.2)", got.wanted, got.receipt.Duration, subject.JobAdvancesTheClock)
		}
	}
	if delivered, known := subject.delivered(t, p); known && delivered != jobs {
		r.Errorf("%d jobs were printed and %d label(s) reached the destination. One weighing is one label: a job that vanished under concurrency is a customer standing in front of a screen that said « envoyée »", jobs, delivered)
	}
}

// checkNoGoroutineLeaks compares a DIFFERENCE and not an absolute count.
//
// An absolute number would be worthless: the test binary runs goroutines of its own, and
// the runtime may still be retiring those of the previous check. What is asserted is that
// the count comes back to where it was once Close has returned — §13.1 claims the
// inventory of goroutines is exhaustive, and it is only true if every driver takes its own
// away.
func checkNoGoroutineLeaks(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	before := settledGoroutines(subject.patience())

	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	if _, err, panicked := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC4")); panicked != nil || err != nil {
		r.Fatalf("Print returned (%v, %v) on the reference weighing", err, panicked)
	}
	if _, panicked := statusQuietly(p, context.Background()); panicked != nil {
		r.Fatalf("Status PANICKED: %v", panicked)
	}
	if _, panicked := selfTestQuietly(p, context.Background(), string(printing.SelfTestLabel)); panicked != nil {
		r.Fatalf("SelfTest PANICKED: %v", panicked)
	}
	closeAndForget(p)

	if !waitUntil(func() bool { return goroutines() <= before }, subject.patience()) {
		r.Errorf("goroutines went from %d to %d and stayed there for %s after a whole job and a Close. §13.1 claims the inventory of goroutines is exhaustive, and it is only true if every driver takes its own away:\n%s",
			before, goroutines(), subject.patience(), goroutineDump())
	}
	if _, tickers := clk.Pending(); tickers > 0 {
		r.Errorf("%d ticker(s) of the injected clock are still running after Close. The stop function that Clock.Ticker returns is not optional: a ticker nobody stops is a leak the goroutine count cannot always see (§13.1)", tickers)
	}
}

// checkOperatorMessagesAreFrench collects everything this driver puts in front of a human
// and holds it to the language that human speaks.
//
// It is not a style rule. « invalid parameter » on the Dépannage page is a volunteer
// standing in a shop at 9 a.m. with a queue behind them and a sentence they cannot act
// on; the whole taxonomy of §8.5 exists so that a message can name the offending value AND
// what to do about it, and it can only do that in French.
func checkOperatorMessagesAreFrench(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))

	read := func(what, sentence string) {
		r.Helper()
		if !looksFrench(sentence) {
			r.Errorf("%s reads « %s ». It is shown to a volunteer, and every line of the administration and troubleshooting screens is French — identifiers in English, contents in French (§8.2)", what, sentence)
		}
	}

	unusable := subject.referenceJob(r, "01J9F2AC5")
	unusable.Label.Barcode = unusableBarcode
	if _, err, _ := printQuietly(p, context.Background(), unusable); err != nil {
		if fault, ok := printErrorOf(r, "Print", err); ok {
			read("the refusal of an unusable barcode", fault.Message)
		}
	}
	if err, _ := selfTestQuietly(p, context.Background(), "mire"); err != nil {
		if fault, ok := printErrorOf(r, "SelfTest", err); ok {
			read("the refusal of an unknown self-test", fault.Message)
		}
	}
	if status, _ := statusQuietly(p, context.Background()); status.Detail != "" {
		read("Status().Detail", status.Detail)
	}

	closeAndForget(p)
	if _, err, _ := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC6")); err != nil {
		if fault, ok := printErrorOf(r, "Print", err); ok {
			read("the refusal of a job after Close", fault.Message)
		}
	}
}

// checkADeveloperMessageStaysEnglish is the other half of clause 17, and the reason the
// rule reads « identifiers in English, contents in French » rather than « everything in
// French ».
//
// No configuration file can produce a nil transport or an empty directory: those come from
// a composition root, so the only person who can ever read that sentence is the one
// writing Go. Answering it in French would be answering the wrong audience — and it would
// blur the one distinction that tells an operator's fault from a developer's.
func checkADeveloperMessageStaysEnglish(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.MissingCollaborator == nil {
		r.Skipf("Subject.MissingCollaborator is nil: the suite cannot see what your constructor answers when a collaborator is left out. Supply it — it is one call with one field missing, and it is what keeps « identifiers in English, contents in French » from drifting into « everything in French » (§8.2)")
	}
	err := subject.MissingCollaborator(t)
	if err == nil {
		r.Fatalf("the constructor ACCEPTED a missing collaborator. An inconsistency stops the process at start-up, never with a customer standing at the scale (§11.3)")
	}
	var fault *ports.PrintError
	if errors.As(err, &fault) {
		r.Logf("the constructor answered a ports.PrintError; only its Message is read below")
		err = errors.New(fault.Message)
	}
	if !looksEnglish(err.Error()) {
		r.Errorf("the constructor answered « %v » to a missing collaborator. No configuration file can produce one, so that sentence is read by a developer and stays English; the French is for what a volunteer can act on (§8.2).\n"+
			"HOW THIS CHECK DECIDES, because it is lexical and it will refuse a message that IS English: it looks for one of the function words a sentence cannot avoid — the, this, is, are, of, with, not, from, cannot, must — and it finds none in a telegram. « %s: New: nil sink » is English and fails here; « %s: New: no sink; this driver writes its frames somewhere and the composition root owns the destination » passes and also says what to do.\n"+
			"So write a SENTENCE, not a label. That is not a formality: the constructor error is the whole diagnosis a developer gets, and « nil sink » names the field they are already looking at while saying nothing about what should have filled it",
			err, subject.Name, subject.Name)
	}
}

// referenceJob is the weighing every check prints: the demonstration product of §8.6, at
// the mass §8.6 states, on the template this subject declared.
func (s Subject) referenceJob(r reporter, jobID string) ports.PrintJob {
	r.Helper()
	label, err := DemoLabel()
	if err != nil {
		r.Fatalf("conformance: the demonstration label could not be built: %v", err)
	}
	label.JobID = jobID
	return ports.PrintJob{
		Label:    label,
		Template: s.template(),
		Locale:   string(domain.LocaleFrench),
	}
}

// delivered reports what reached the destination, and whether the subject can say at all.
//
// The second result is not a detail. « Zero labels came out » is the verdict of half the
// refusal checks and the failure of the nominal one, so a subject with no way to look must
// not be read as either: it simply does not strengthen that half of the verdict.
func (s Subject) delivered(t *testing.T, p ports.Printer) (count int, known bool) {
	if s.Delivered == nil {
		return 0, false
	}
	return s.Delivered(t, p), true
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
