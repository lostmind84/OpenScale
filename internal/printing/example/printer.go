package example

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"strings"
	"sync"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// frameHeader is the one readable line that opens a frame when the `header` option is on.
//
// TODO(driver): replace the whole of frame() by your printer's language. The raster driver
// emits eleven SBPL commands around a <G> block; this one writes a line and the bitmap.
const frameHeader = "### example %s — %d copie(s) — %d × %d dots\n"

// Options is everything this driver is given, and nothing it could invent.
//
// A struct rather than positional parameters, for the reason printing.DriverConfig gives:
// the day the driver needs a sixth collaborator, every existing call site would otherwise
// have to be re-typed for a value it ignores.
type Options struct {
	// Clock times a job. There is NO default: time.Now is out of reach of every driver
	// (§5.3), and `go run ./tools/boundary` fails on a call to it anywhere under internal/.
	Clock ports.Clock

	// Log is where the RENDER reports what a volunteer may have to act on: a name the
	// automatic reduction had to truncate, a character no embedded font carries (§7.3).
	// Nil is replaced by ports.NopTechnicalLog — a driver never opens a file.
	Log ports.TechnicalLog

	// Template is the template IN SERVICE (printer.template, §11.2). A print job carries
	// its own and that one is used; this is what a self-test falls back on.
	Template domain.Template

	// Settings is printer.options once ParseOptions has read it.
	Settings Settings

	// DemoLabel supplies the label the `label` self-test prints (§8.6).
	//
	// INJECTED, and this is a boundary rather than a formality: a demonstration label
	// carries a product, a unit price and a pricing grid, which come from the catalog and
	// the configuration of the station. A printing driver that made one up would be
	// printing a number nobody could check — and somebody WILL lay that label over a real
	// one on a light table and read the price off it. Nil is legitimate: the self-test
	// then refuses, in French, naming what is missing.
	DemoLabel func() (domain.Label, error)

	// Sink is where the frames go. Nil means the driver's own buffer in memory.
	//
	// It is THE seam that makes this driver testable, and the precedent is
	// serial.Options.Open on the weighing side: a test owns no printer, so what a device
	// would do — accepting FEWER bytes than it was given, with no error of its own, which
	// is what WritePrinter really does — is done by a writer the test hands over.
	//
	// TODO(driver): a driver that reaches a device declares the `transport` key in its
	// OWN option schema and takes a ports.Transport here instead. The composition root
	// builds it and closes it; a driver that opened a device itself would lose « one
	// frame, four destinations » (§8.4).
	Sink io.Writer
}

// Printer is the example driver. It satisfies ports.Printer.
type Printer struct {
	clock     ports.Clock
	log       ports.TechnicalLog
	fonts     *printing.Library
	render    *printing.Rasterizer
	template  domain.Template
	settings  Settings
	demoLabel func() (domain.Label, error)

	// mu SERIALISES the handing over of a frame: one label at a time, never interleaved
	// (§8.2). The station reaches one driver instance from the weighing path, the reprint
	// bar and the troubleshooting screen, and two frames crossing on the way to a head is
	// how a label comes out as garbage. The legacy guard against it was an `If
	// AllReports(...).IsLoaded Then Exit Sub` that silently ABANDONED the second weighing:
	// serialising is a WAIT, never a refusal.
	mu     sync.Mutex
	buffer bytes.Buffer
	sink   io.Writer
	closed bool
}

// New builds the driver from what a configuration and a composition root hand it.
//
// The messages about a MISSING COLLABORATOR are English, and that is the other half of
// « identifiers in English, contents in French » (§8.2): no configuration file can produce
// a nil clock, so the only person who can ever read this sentence is the one writing Go.
func New(o Options) (*Printer, error) {
	if o.Clock == nil {
		return nil, errors.New("example: New: no clock; a job is dated by the INJECTED clock (§5.3)")
	}

	log := o.Log
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	fonts, err := printing.NewLibrary()
	if err != nil {
		return nil, fmt.Errorf("example: New: %w", err)
	}
	render, err := printing.NewRasterizer(fonts, log)
	if err != nil {
		fonts.Close()
		return nil, fmt.Errorf("example: New: %w", err)
	}

	p := &Printer{
		clock: o.Clock, log: log, fonts: fonts, render: render,
		template: o.Template, settings: o.Settings, demoLabel: o.DemoLabel,
	}
	p.sink = o.Sink
	if p.sink == nil {
		p.sink = &p.buffer
	}
	return p, nil
}

// Descriptor reports the identity of the driver and the capabilities it declares.
//
// It is called BEFORE anything is printed, because that is when the Hub calls it: the
// drop-down list of printer.type is built from drivers nobody has opened yet. It is also a
// VALUE and not a state — two calls answer the same thing forever.
func (p *Printer) Descriptor() domain.PrinterDescriptor {
	return Driver().Descriptor
}

// Print renders one job and hands the frame over.
//
// Closure is checked BEFORE composing, and send checks it again. Composing first draws
// through a font library Close has already shut, so the render fails and the job comes back
// as KindTemplate — naming printer.template for a template that has nothing wrong, and
// sending a volunteer to look at a setting that is correct. That is a real defect this
// repository paid for twice; the check in send stays, because it is what covers a Close
// landing between here and the write.
func (p *Printer) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	if err := p.refuseWhenClosed("example.Print"); err != nil {
		return ports.PrintReceipt{}, err
	}
	img, copies, err := p.compose(job)
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return p.send(ctx, "example.Print", job.Label.JobID, p.frame(job.Label.JobID, copies, img))
}

// compose turns a job into the bitmap the head would burn and the number of copies of it,
// refusing what would print a WRONG label rather than passing it on.
//
// THE ORDER IS THE TAXONOMY OF §8.5, which classifies a failure by the ACTION expected of a
// human and never by the layer the error came from:
//
//	barcode   KindData      a product to correct in Odoo, and the product is flagged
//	copies    KindConfig    a setting this station cannot honour
//	render    KindTemplate  a layout that does not fit what it was drawn on
//
// Neither of the three is retryable: only KindTransient is, and a template that does not fit
// will not fit any better 300 ms later.
//
// TODO(driver): a driver that DRIVES A HEAD adds a fourth refusal FIRST — a template drawn
// for another pitch, as KindTemplate. It is the one fault that produces a WRONG label rather
// than no label: a 12 dots/mm template on an 8 dots/mm head prints at two thirds of its size,
// with a symbol under every GS1 floor, and no byte of the frame says so. Copy
// raster.checkTemplateHead. This driver declares no head, so no template is foreign to it,
// and refusing one would take a station out of the one thing it can still do.
func (p *Printer) compose(job ports.PrintJob) (*image.Gray, int, error) {
	// BEFORE the render, and the order is load-bearing: an unusable reference is caught
	// before anything is drawn, so a catalog full of them costs no rendering budget and —
	// what matters more — no symbol nobody can scan ever reaches the destination.
	if _, err := domain.ParseEAN13(string(job.Label.Barcode)); err != nil {
		return nil, 0, &ports.PrintError{Kind: ports.KindData, Op: "example.Print", Err: err,
			Message: fmt.Sprintf("le code-barres %q de ce produit est inutilisable : %v",
				string(job.Label.Barcode), err)}
	}
	copies, err := p.copiesFor(job)
	if err != nil {
		return nil, 0, err
	}
	template := job.Template
	img, err := p.render.Rasterize(&template, job.Label, domain.Locale(job.Locale),
		printing.RenderOptions{})
	if err != nil {
		return nil, 0, &ports.PrintError{Kind: ports.KindTemplate, Op: "example.Print", Err: err,
			Message: fmt.Sprintf("l'étiquette n'a pas pu être dessinée : %v", err)}
	}
	return img, copies, nil
}

// copiesFor reports how many copies a job asks for.
//
// A job that says NOTHING gets the configured count, and that is not indulgence: the print
// service builds its PrintJob WITHOUT a Copies field (§8.2), so zero means « unspecified »
// and printer.options.copies is the answer. A driver that read the field literally would
// print nothing at all while the screen says a label was sent.
//
// Anything else is taken literally and BOUNDED — a count past the declared ceiling is
// refused with the value it was given and the value that is accepted, never rounded into
// something that prints: a count quietly turned into one is a volunteer pressing a button
// that no longer does what it says.
func (p *Printer) copiesFor(job ports.PrintJob) (int, error) {
	if job.Copies == 0 {
		return p.settings.Copies, nil
	}
	if job.Copies < MinCopies || job.Copies > MaxCopies {
		return 0, &ports.PrintError{Kind: ports.KindConfig, Op: "example.Print",
			Message: fmt.Sprintf("%d exemplaires demandés : le nombre d'exemplaires va de %d à %d",
				job.Copies, MinCopies, MaxCopies)}
	}
	return job.Copies, nil
}

// frame turns a bitmap into the bytes this driver hands its destination.
func (p *Printer) frame(jobID string, copies int, img *image.Gray) []byte {
	var out bytes.Buffer
	if p.settings.Header {
		fmt.Fprintf(&out, frameHeader, jobID, copies, img.Bounds().Dx(), img.Bounds().Dy())
	}
	out.Write(img.Pix)
	return out.Bytes()
}

// refuseWhenClosed reports the refusal a closed driver owes its caller, or nil.
//
// KindInternal, and the kind is the taxonomy doing its work: a job sent to a closed driver
// is a bug in this binary, it is never retried, and it says so. KindTransient would have
// the print service try twice more against a device nobody holds.
func (p *Printer) refuseWhenClosed(op string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return &ports.PrintError{Kind: ports.KindInternal, Op: op,
			Message: "l'imprimante d'exemple a été fermée : ce poste ne peut plus écrire " +
				"d'étiquette sans redémarrer"}
	}
	return nil
}

// send hands one finished frame over and times it on the INJECTED clock.
//
// A SHORT WRITE WITH NO ERROR IS A FAILURE, and it is the failure mode that costs the most.
// WritePrinter really does report a short count without an error of its own: the frame is
// truncated, the label comes out blank, and the station journals a success for a label
// nobody ever held (§8.3, §8.5). KindTransient, because a spooler that took a partial frame
// once usually takes the whole one on the next attempt.
func (p *Printer) send(ctx context.Context, op, jobID string, frame []byte) (ports.PrintReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindInternal, Op: op,
			Message: "l'imprimante d'exemple a été fermée : ce poste ne peut plus écrire " +
				"d'étiquette sans redémarrer"}
	}
	if err := ctx.Err(); err != nil {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
			Message: "l'écriture de l'étiquette a été interrompue avant la fin"}
	}

	started := p.clock.Now()
	written, err := p.sink.Write(frame)
	if err != nil {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
			Message: fmt.Sprintf("l'étiquette n'a pas pu être écrite : %v", err)}
	}
	if written != len(frame) {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op,
			Message: fmt.Sprintf("l'étiquette n'a été écrite qu'en partie : %d octets acceptés "+
				"sur %d, la sortie serait tronquée", written, len(frame))}
	}
	return ports.PrintReceipt{
		JobID: jobID, Bytes: written, Duration: p.clock.Now().Sub(started),
	}, nil
}

// Status reports what this driver honestly knows about itself.
//
// PrinterUnknown, and NEVER PrinterReady. PrinterReady means « the device answered and has
// nothing to report », and there is no device here to answer: a green light over a station
// where no label ever comes out is the failure §14.5 refuses, and PrinterUnknown exists
// exactly so that « je ne sais pas » is a sayable answer.
//
// The Detail is a French sentence a volunteer reads on the troubleshooting screen while
// standing at the counter, and it says where the bytes are going.
//
// TODO(driver): a driver whose transport is bidirectional asks the device — ENQ out, the
// answer in, on a budget measured on the INJECTED clock — and maps what comes back onto the
// four values. internal/printing/raster/status.go is that case. Anything it cannot ask
// about stays PrinterUnknown.
func (p *Printer) Status(context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: "l'imprimante d'exemple a été fermée par le poste."}
	}
	return ports.PrinterStatus{Health: ports.PrinterUnknown,
		Detail: "exemple : aucune étiquette n'est imprimée, chacune est écrite en mémoire."}
}

// SelfTest answers EVERY name of the catalogue of §8.6, and the answer follows what
// Driver() declares.
//
// A pattern this driver DECLARES has to come out — the Matériel page builds its button from
// that declaration, so a refusal there is a button that fails on the click. A pattern it
// does not declare is refused, and refused USEFULLY: the sentence names the test and says
// why, because the route stays reachable outside the screen. What it may never answer about
// a name the catalogue carries is « auto-test inconnu » — that wording sends a volunteer
// hunting for a typo they did not make.
func (p *Printer) SelfTest(ctx context.Context, what string) error {
	switch printing.SelfTest(what) {
	case printing.SelfTestLabel:
		return p.printDemoLabel(ctx)
	case printing.SelfTestAlignment, printing.SelfTestRuler:
		return &ports.PrintError{Kind: ports.KindConfig, Op: "example.SelfTest",
			Message: fmt.Sprintf("l'auto-test « %s » n'est pas produit par ce driver "+
				"d'exemple : la mire et la règle se lisent sur une étiquette imprimée, et "+
				"celui-ci ne dessine que l'étiquette de démonstration.", what)}
	}
	return &ports.PrintError{Kind: ports.KindConfig, Op: "example.SelfTest",
		Message: fmt.Sprintf("auto-test inconnu %q : les auto-tests disponibles sont %s, %s et %s",
			what, printing.SelfTestLabel, printing.SelfTestAlignment, printing.SelfTestRuler)}
}

// printDemoLabel prints the demonstration label of the `label` self-test, and INVENTS NO
// PRICE when it was given none.
func (p *Printer) printDemoLabel(ctx context.Context) error {
	if p.demoLabel == nil {
		return &ports.PrintError{Kind: ports.KindConfig, Op: "example.SelfTest.label",
			Message: "aucune étiquette de démonstration n'a été fournie à ce driver : " +
				"l'étiquette de test porte un produit et des prix, qui viennent du catalogue " +
				"et de la configuration du poste, jamais du driver"}
	}
	label, err := p.demoLabel()
	if err != nil {
		return &ports.PrintError{Kind: ports.KindData, Op: "example.SelfTest.label", Err: err,
			Message: fmt.Sprintf("l'étiquette de démonstration n'a pas pu être préparée : %v", err)}
	}
	_, err = p.Print(ctx, ports.PrintJob{
		Label: label, Template: p.template, Locale: string(domain.LocaleFrench), Copies: 1,
	})
	return err
}

// Buffered reports a COPY of what this driver wrote into its own buffer.
//
// A copy, because a caller that could reach into the buffer could also empty it under a job
// in flight. It is empty on a driver built with an Options.Sink of its own.
func (p *Printer) Buffered() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.buffer.Bytes()...)
}

// Frames reports how many frames this driver wrote into its own buffer.
//
// It counts headers, which is why the `header` option exists at all: a buffer with no
// separator in it is a run of bytes nobody can count.
func (p *Printer) Frames() int {
	return strings.Count(string(p.Buffered()), "### example ")
}

// Close releases what was taken, and nothing else. It is IDEMPOTENT because the Hub closes
// on a configuration reload and again on shutdown (§11.4, §13.4).
//
// The font library is a real resource even on a driver that opens no device: the renderer
// memoises faces, and a reload replaces the printer.
func (p *Printer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.fonts.Close()
	return nil
}

var _ ports.Printer = (*Printer)(nil)
