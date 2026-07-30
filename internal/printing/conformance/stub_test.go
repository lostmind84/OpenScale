package conformance

// The reference printer of this package's own tests, plus every way it can be made to
// betray ports.Printer. One field per clause, each named after what it breaks.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// Compile-time proof that the reference printer really is a ports.Printer.
var _ ports.Printer = (*stubPrinter)(nil)

// What the honest subject declares. The identity is lower-case and blank-free on purpose:
// the descriptor check has a table of its own for the rest.
const (
	stubID    = "stub-printer"
	stubLabel = "Imprimante de référence (banc de conformité)"
	// stubMaxCopies is small where a real <Q> field holds six digits: the bound is what is
	// being tested, not its value, and a small one keeps the failure lines readable.
	stubMaxCopies = 3
	// stubConfiguredCopies is what a job that says NOTHING gets, the way
	// printer.options.copies does.
	stubConfiguredCopies = 1
	// stubDotsPerMM is a WS408, which is the whole parc — and the pitch of the template
	// the suite prints by default.
	stubDotsPerMM = 8
)

// stubWriteDelay is how long the stub's destination pretends to take. It is charged to the
// INJECTED clock, which is what makes the duration on the receipt an arithmetic fact
// instead of a plausible-looking number.
const stubWriteDelay = 40 * time.Millisecond

// stubPrinter is the smallest thing that honours ports.Printer.
//
// It is what proves the suite is PASSABLE — a suite nothing can pass is as worthless as
// one nothing can fail — and it is also the shape of the answer to « what does a driver owe
// the station? »: an identity a configuration can name, a job it either prints or refuses
// by naming the action expected, a status that never pretends, three self-tests that always
// answer, and a Close that can be called twice.
//
// Every field below the divider is a betrayal, set by exactly one case of
// TestSuiteRejectsEveryBrokenPrinter.
type stubPrinter struct {
	clock    ports.Clock
	template domain.Template
	demo     func() (domain.Label, error)
	// advance moves the injected clock the way a real destination charges a write. It
	// stands in for the seam a contributor supplies — a recording transport, a fake device
	// — and it is what Subject.JobAdvancesTheClock declares.
	advance func()

	// mu serialises everything that reaches the destination: ONE label at a time, never
	// interleaved (§8.2).
	mu         sync.Mutex
	closed     bool
	emitted    int
	lastCopies int
	// firstJobID is the field a driver keeping the job in flight in its own state would
	// have. Only stealsTheJobID ever reads it back.
	firstJobID string

	// --- the betrayals ------------------------------------------------------

	// id overrides the identity: wrong, blank-carrying or upper-case. anonymous removes it
	// and unstableID makes it move between two calls.
	anonymous  bool
	id         string
	unstableID bool
	idCalls    atomic.Int64
	// mute answers with no label for the drop-down list.
	mute bool
	// noCopies declares a ceiling of zero, which is a driver that cannot print at all.
	noCopies bool
	// honoursNoBound prints whatever count it is handed, past its own <Q> field.
	honoursNoBound bool
	// bareCopyRefusal refuses without naming the value or the range.
	bareCopyRefusal bool
	// copyRefusalIsTransient invites the print service to retry a count that cannot change.
	copyRefusalIsTransient bool
	// zeroCopiesPrintsNothing reads « unspecified » as « none ».
	zeroCopiesPrintsNothing bool
	// acceptsAnyBarcode prints a symbol no till can scan.
	acceptsAnyBarcode bool
	// barcodeIsInternal misclassifies a product to fix in Odoo as a bug in this binary.
	barcodeIsInternal bool
	// barcodeCheckedAfterTheRender hands the label over and THEN refuses it.
	barcodeCheckedAfterTheRender bool
	// acceptsForeignTemplate prints a layout drawn for another head.
	acceptsForeignTemplate bool
	// foreignTemplateIsData blames the catalog for a geometry.
	foreignTemplateIsData bool
	// sinkShort makes the destination itself accept one byte less, which is what
	// WritePrinter does; shortIsSuccess is the driver calling that a printed label, and
	// shortIsInternal is it refusing in a way nothing retries.
	sinkShort       bool
	shortIsSuccess  bool
	shortIsInternal bool
	// claimsReady says « prête » with nothing to show for it.
	claimsReady bool
	// noStatusCapability declares no return channel, and saysFaulted then reports a fault
	// it cannot have observed.
	noStatusCapability bool
	saysFaulted        bool
	// muteStatus answers with no sentence for the troubleshooting screen.
	muteStatus bool
	// statusAfterCloseSaysReady keeps the light green over a driver that gave up its device.
	statusAfterCloseSaysReady bool
	// englishStatus and englishRefusal put a developer's sentence in front of a volunteer.
	englishStatus  bool
	englishRefusal bool
	// printsAfterClose reopens what the station has already released.
	printsAfterClose bool
	// closedRefusalIsTransient has the print service retry against a device nobody holds.
	closedRefusalIsTransient bool
	// panicsOnSecondClose takes the station down on the shutdown that follows a reload.
	panicsOnSecondClose bool
	closeCalls          atomic.Int64
	// unknownOnACatalogueName answers « auto-test inconnu » about a button the screen offers.
	unknownOnACatalogueName bool
	// refusesADeclaredSelfTest turns down a pattern the registry entry promised, which is
	// a button the administration screen draws and that fails on the click.
	refusesADeclaredSelfTest bool
	// acceptsAnySelfTest prints whatever a mistyped URL asks for.
	acceptsAnySelfTest bool
	// bareUnknownRefusal refuses without listing what exists.
	bareUnknownRefusal bool
	// inventsADemoLabel prints a product and prices nobody supplied.
	inventsADemoLabel bool
	// wallClock reports a duration the injected clock never covered. It is a CONSTANT and
	// not a call to time.Now: the boundary tool forbids that call in internal/..., and what
	// is being simulated is the figure, not the way of getting it.
	wallClock bool
	// stealsTheJobID hands every caller the identifier of the first job this driver saw.
	stealsTheJobID bool
	// leak is closed by the test that installed it, never by the driver: it is how a leaked
	// goroutine is simulated without leaking one into the rest of the binary.
	leak chan struct{}
}

// newStub returns the healthy reference printer, demonstration label included.
func newStub(clk ports.Clock) *stubPrinter {
	return &stubPrinter{
		clock:    clk,
		template: domain.IdenticalTemplate(),
		demo:     DemoLabel,
	}
}

// Descriptor reports the identity the registry and printer.type both read.
func (s *stubPrinter) Descriptor() domain.PrinterDescriptor {
	d := domain.PrinterDescriptor{
		ID:    stubID,
		Label: stubLabel,
		Capabilities: domain.PrinterCapabilities{
			Raster:    true,
			Status:    !s.noStatusCapability,
			Cutter:    false,
			MaxCopies: s.maxCopies(),
			DotsPerMM: stubDotsPerMM,
		},
	}
	switch {
	case s.anonymous:
		d.ID = ""
	case s.unstableID:
		d.ID = fmt.Sprintf("%s-%d", stubID, s.idCalls.Add(1))
	case s.id != "":
		d.ID = s.id
	}
	if s.mute {
		d.Label = ""
	}
	return d
}

// maxCopies reports the ceiling this driver declares.
func (s *stubPrinter) maxCopies() int {
	if s.noCopies {
		return 0
	}
	return stubMaxCopies
}

// Print renders one job and hands it to the destination.
func (s *stubPrinter) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	frame, copies, err := s.compose(job)
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return s.send(job.Label.JobID, frame, copies)
}

// compose turns a job into the bytes the destination will take, refusing what would
// produce a WRONG label rather than passing it on.
func (s *stubPrinter) compose(job ports.PrintJob) ([]byte, int, error) {
	frame := frameFor(job)
	if !s.acceptsForeignTemplate && job.Template.Media.DotsPerMM != stubDotsPerMM {
		kind := ports.KindTemplate
		if s.foreignTemplateIsData {
			kind = ports.KindData
		}
		return nil, 0, &ports.PrintError{Kind: kind, Op: "stub.Print",
			Message: fmt.Sprintf("le gabarit %q est dessiné pour une tête de %g dots/mm et cette "+
				"imprimante en fait %d : l'étiquette sortirait à une autre échelle",
				job.Template.Name, job.Template.Media.DotsPerMM, stubDotsPerMM)}
	}
	if _, err := domain.ParseEAN13(string(job.Label.Barcode)); err != nil && !s.acceptsAnyBarcode {
		if s.barcodeCheckedAfterTheRender {
			s.emit()
		}
		kind := ports.KindData
		if s.barcodeIsInternal {
			kind = ports.KindInternal
		}
		message := fmt.Sprintf("le code-barres %q de ce produit est inutilisable : %v",
			string(job.Label.Barcode), err)
		if s.englishRefusal {
			message = fmt.Sprintf("the barcode %q of this product cannot be used: %v",
				string(job.Label.Barcode), err)
		}
		return nil, 0, &ports.PrintError{Kind: kind, Op: "stub.Print", Err: err, Message: message}
	}
	copies, err := s.copiesFor(job)
	if err != nil {
		return nil, 0, err
	}
	return frame, copies, nil
}

// copiesFor reports how many copies a job asks for.
//
// A job that says NOTHING gets the configured count: the print service builds its PrintJob
// without a Copies field (§8.2), so zero means « unspecified » and never « none ».
func (s *stubPrinter) copiesFor(job ports.PrintJob) (int, error) {
	if job.Copies == 0 {
		if s.zeroCopiesPrintsNothing {
			return 0, nil
		}
		return stubConfiguredCopies, nil
	}
	if s.honoursNoBound {
		return job.Copies, nil
	}
	if job.Copies < 1 || job.Copies > s.maxCopies() {
		message := fmt.Sprintf("%d exemplaires demandés : le nombre d'exemplaires va de 1 à %d",
			job.Copies, s.maxCopies())
		if s.bareCopyRefusal {
			message = "nombre d'exemplaires invalide"
		}
		kind := ports.KindConfig
		if s.copyRefusalIsTransient {
			kind = ports.KindTransient
		}
		return 0, &ports.PrintError{Kind: kind, Op: "stub.Print", Message: message}
	}
	return job.Copies, nil
}

// send hands one finished frame to the destination and times it on the injected clock.
func (s *stubPrinter) send(jobID string, frame []byte, copies int) (ports.PrintReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed && !s.printsAfterClose {
		kind := ports.KindInternal
		if s.closedRefusalIsTransient {
			kind = ports.KindTransient
		}
		return ports.PrintReceipt{}, &ports.PrintError{Kind: kind, Op: "stub.Print",
			Message: "l'imprimante a été fermée : ce poste ne peut plus imprimer sans redémarrer"}
	}
	if s.leak != nil {
		go func() { <-s.leak }()
	}

	started := s.clock.Now()
	if s.advance != nil {
		s.advance()
	}
	accepted := len(frame)
	if s.sinkShort {
		accepted--
	}
	if copies > 0 {
		s.emitted++
		s.lastCopies = copies
	}
	elapsed := s.clock.Now().Sub(started)
	if s.wallClock {
		elapsed = 7 * time.Millisecond
	}

	if accepted != len(frame) && !s.shortIsSuccess {
		kind := ports.KindTransient
		if s.shortIsInternal {
			kind = ports.KindInternal
		}
		return ports.PrintReceipt{}, &ports.PrintError{Kind: kind, Op: "stub.Print",
			Message: fmt.Sprintf("l'imprimante n'a accepté que %d octets sur %d : "+
				"l'étiquette serait tronquée", accepted, len(frame))}
	}
	if s.firstJobID == "" {
		s.firstJobID = jobID
	}
	if s.stealsTheJobID {
		jobID = s.firstJobID
	}
	return ports.PrintReceipt{JobID: jobID, Bytes: accepted, Duration: elapsed}, nil
}

// Status reports what this driver knows about a device it does not have.
func (s *stubPrinter) Status(context.Context) ports.PrinterStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if s.statusAfterCloseSaysReady {
			return ports.PrinterStatus{Health: ports.PrinterReady, Raw: []byte{0x02, 'A'},
				Detail: "l'imprimante est prête."}
		}
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: "l'imprimante a été fermée par le poste."}
	}
	switch {
	case s.claimsReady:
		return ports.PrinterStatus{Health: ports.PrinterReady, Detail: "l'imprimante est prête."}
	case s.saysFaulted:
		return ports.PrinterStatus{Health: ports.PrinterFaulted, Detail: "l'imprimante est hors ligne."}
	case s.muteStatus:
		return ports.PrinterStatus{Health: ports.PrinterUnknown}
	case s.englishStatus:
		return ports.PrinterStatus{Health: ports.PrinterUnknown, Detail: "the printer did not answer"}
	}
	return ports.PrinterStatus{Health: ports.PrinterUnknown,
		Detail: "état inconnu : ce banc n'interroge aucune imprimante."}
}

// SelfTest answers every name of the catalogue of §8.6, and refuses the others by naming
// them.
//
// The reference printer HONOURS ALL THREE, which is what referenceSubject declares by
// leaving Subject.SelfTests nil. A subject that narrows the declaration without narrowing
// this method is one of the betrayals: it is the pattern nobody can launch.
func (s *stubPrinter) SelfTest(ctx context.Context, what string) error {
	if !printing.SelfTestExists(printing.SelfTest(what)) {
		if s.acceptsAnySelfTest {
			return nil
		}
		return s.unknown(what)
	}
	switch {
	case s.unknownOnACatalogueName:
		return s.unknown(what)
	case s.refusesADeclaredSelfTest:
		return &ports.PrintError{Kind: ports.KindConfig, Op: "stub.SelfTest",
			Message: fmt.Sprintf("l'auto-test « %s » n'est pas produit par ce banc.", what)}
	case printing.SelfTest(what) == printing.SelfTestLabel:
		return s.printDemoLabel(ctx)
	}
	_, err := s.send("stub.SelfTest."+what, []byte("pattern:"+what), 1)
	return err
}

// unknown is the refusal of a name no self-test answers to.
func (s *stubPrinter) unknown(what string) error {
	message := fmt.Sprintf("%s %q : les auto-tests disponibles sont %s, %s et %s",
		unknownSelfTest, what, printing.SelfTestLabel, printing.SelfTestAlignment, printing.SelfTestRuler)
	if s.bareUnknownRefusal {
		message = fmt.Sprintf("%s %q", unknownSelfTest, what)
	}
	return &ports.PrintError{Kind: ports.KindConfig, Op: "stub.SelfTest", Message: message}
}

// printDemoLabel prints the demonstration label, or says why it cannot.
func (s *stubPrinter) printDemoLabel(ctx context.Context) error {
	demo := s.demo
	if demo == nil {
		if !s.inventsADemoLabel {
			return &ports.PrintError{Kind: ports.KindConfig, Op: "stub.SelfTest.label",
				Message: "aucune étiquette de démonstration n'a été fournie à l'imprimante : " +
					"elle porte un produit et des prix, qui viennent du catalogue et de la " +
					"configuration du poste, jamais du driver"}
		}
		demo = DemoLabel
	}
	label, err := demo()
	if err != nil {
		return &ports.PrintError{Kind: ports.KindData, Op: "stub.SelfTest.label", Err: err,
			Message: fmt.Sprintf("l'étiquette de démonstration n'a pas pu être préparée : %v", err)}
	}
	_, err = s.Print(ctx, ports.PrintJob{
		Label: label, Template: s.template, Locale: string(domain.LocaleFrench), Copies: 1,
	})
	return err
}

// Close gives up the driver.
func (s *stubPrinter) Close() error {
	if s.closeCalls.Add(1) > 1 && s.panicsOnSecondClose {
		panic("stub: close called twice")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// emit records one label reaching the destination, from a path that holds no lock yet.
func (s *stubPrinter) emit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitted++
}

// delivered reports how many labels reached the destination.
func (s *stubPrinter) delivered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emitted
}

// copies reports how many copies the last job asked for.
func (s *stubPrinter) copies() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCopies
}

// frameFor is the byte payload one job produces. Its content carries nothing the suite
// reads; its LENGTH is what a short write takes one byte off.
func frameFor(job ports.PrintJob) []byte {
	return []byte(string(job.Label.Barcode) + "|" + job.Label.Product.Name + "|" + job.Label.JobID)
}

// newStubWithoutClock is the composition-root refusal of this driver: the message only a
// developer can ever read, since no configuration file can produce a nil clock.
func newStubWithoutClock(french bool) error {
	if french {
		return errors.New("stub : aucune horloge n'a été fournie à ce driver")
	}
	return errors.New("stub: New: no clock; the duration of a job is measured by the INJECTED clock (§5.3)")
}

// advancer returns the seam that charges one write to the injected clock.
//
// It is the shape of what a contributor supplies — a recording transport whose Write moves
// the fake clock — and Subject.JobAdvancesTheClock is how they declare it.
func advancer(clk ports.Clock) func() {
	fakeClock, injected := clk.(*fake.Clock)
	if !injected {
		return nil
	}
	return func() { fakeClock.Advance(stubWriteDelay) }
}

// referenceSubject submits the healthy printer exactly the way a contributor submits
// theirs — which makes this function the example to copy.
func referenceSubject() Subject {
	build := func(shape func(*stubPrinter)) func(*testing.T, ports.Clock) ports.Printer {
		return func(t *testing.T, clk ports.Clock) ports.Printer {
			s := newStub(clk)
			s.advance = advancer(clk)
			shape(s)
			return s
		}
	}
	return Subject{
		Name: stubID,
		// Spelled out rather than left nil, because this function is the example a
		// contributor copies: a driver states which patterns of §8.6 it honours, and a real
		// one reads that list off its own registry entry — `raster.Driver().SelfTests`.
		SelfTests: []printing.SelfTest{
			printing.SelfTestLabel, printing.SelfTestAlignment, printing.SelfTestRuler,
		},
		New:                 build(func(*stubPrinter) {}),
		JobAdvancesTheClock: stubWriteDelay,
		Delivered:           func(_ *testing.T, p ports.Printer) int { return p.(*stubPrinter).delivered() },
		Copies:              func(_ *testing.T, p ports.Printer) int { return p.(*stubPrinter).copies() },
		Short:               build(func(s *stubPrinter) { s.sinkShort = true }),
		WithoutDemoLabel:    build(func(s *stubPrinter) { s.demo = nil }),
		MissingCollaborator: func(*testing.T) error { return newStubWithoutClock(false) },
		DrivesAHead:         true,
		Patience:            faultPatience,
	}
}
