// Package raster is the printer driver of the production path (§8.2, ADR-002).
//
// # Why the DEFAULT driver draws the label itself
//
// The label reproduced by A1 carries a module of 0.293 mm. At the 8 dots/mm of a
// WS408 that is 2.344 dots, and it is NOT a whole number. No printer language can
// express it: a module is declared there in whole dots, so the two values a firmware
// could draw are 2 dots (0.250 mm, 75.8 % of the GS1 reference, BELOW the 80 % floor)
// and 3 dots (0.375 mm, wider than the 40 mm stock allows). Only a bitmap drawn dot
// by dot reproduces the magnification of the label the cooperative prints today.
//
// That is arithmetic, not a preference, and it is the whole reason `raster` is the
// DEFAULT and not a fallback. Nothing on this path is to be "corrected" towards a
// native barcode command (ADR-002, ADR-003, §7.4).
//
// # What this driver does
//
//	printing.Rasterize -> 1 bit per dot -> eleven SBPL commands -> ports.Transport
//
// It consumes the *image.Gray of the engine — the WHOLE label, EAN-13 symbol and HRI
// included — and RECOMPUTES NOTHING: domain.Label arrives complete, amounts and
// barcode included (§8.2). The native <BD> barcode command is never emitted, by any
// driver of this application: it would make the preview lie about what comes out
// (important-1) and reopen seven firmware unknowns A2 closed (§8.1).
//
// The `sbpl` driver of §8.1 shares this entire sequence and changes only the last
// link — a direct write to the device instead of the print queue of the system. The
// two therefore emit THE SAME BYTES for the same label, and they do so because they
// call THE SAME ENCODER: internal/printing/sbpl owns the encapsulation, this package
// owns the output path, and frame.go is the whole of the border between them (§5.1,
// §8.1).
//
// # The three adjustments, and where they live
//
// Darkness, speed and the ±1 dot offset are set ON A REAL PRINT RUN, by a volunteer,
// against a label held over a light table. They are therefore CONFIGURATION
// (printer.options, §11.2) and never constants of this package: Settings carries
// them, Settings.Validate refuses what the manual does not accept, and nothing is
// ever silently clamped — a darkness of 7 quietly turned into 5 is a setting that
// stops meaning anything, and the volunteer would go on turning a knob that no longer
// moves.
package raster

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// ID is the registry key of this driver and the value of printer.type. It is the
// DEFAULT of that key (§8.1).
const ID = "raster"

// Label is what a volunteer reads in the administration screen. French, like
// everything a volunteer reads (§8.2).
const Label = "Imprimante d'étiquettes (rendu image)"

// The three self-tests of §8.6, spelled as the troubleshooting route sends them.
const (
	// SelfTestLabel prints a complete demonstration label. It is the button
	// « Imprimer une étiquette de test », and the gesture that goes with it is to lay
	// the result over a current label on a light table.
	SelfTestLabel = "label"
	// SelfTestAlignment prints a filled square and a cross in each corner of the
	// printable area. It lifts the polarity of <G> (invert_bits), the registration of
	// the media and the area the head really reaches.
	SelfTestAlignment = "alignment"
	// SelfTestRuler prints a millimetre scale on two edges plus the frame of the
	// printable area. It lifts the real pitch of the head against the declared media.
	SelfTestRuler = "ruler"
)

// statusBudget is how long a status probe waits for the printer to say something
// (§8.5, level N3). It bounds a transport that answers, never a weighing.
const statusBudget = 500 * time.Millisecond

// enquiry is the SBPL status request: ENQ, one byte (§8.5). ANY non-empty answer
// means the printer is alive; the fine decoding of the frame is deliberately off
// until one has been captured on the bench, and the raw bytes travel back in
// PrinterStatus.Raw so that decoding can be completed without going to the shop.
var enquiry = []byte{0x05}

// Options is everything the driver needs, and nothing it could invent.
type Options struct {
	// Transport is the byte layer that carries the frame to the head: a Windows queue
	// in RAW, a device node, a socket, a file (§8.4). This driver never opens a device
	// itself, which is exactly what lets one frame reach four destinations.
	Transport ports.Transport

	// Clock is what times a job. time.Now is out of reach here (§5.3): a receipt
	// timed on the wall clock cannot be asserted, and failure test 6 — a printer
	// hanging for 60 s — would cost real seconds in a suite budgeted at ten.
	Clock ports.Clock

	// Log is where the RENDER reports what a volunteer may have to act on: a product
	// name the automatic reduction had to truncate, a character no embedded font
	// carries, a language the binary has no words for (§7.3). A driver holding a real
	// journal is precisely what render.go asks for, so that those anomalies land in
	// the troubleshooting screen instead of a log file nobody opens. Nil is replaced
	// by ports.NopTechnicalLog.
	Log ports.TechnicalLog

	// Template is the template IN SERVICE (printer.template, §11.2).
	//
	// A print job carries its own — Print uses that one, because the job is what
	// decides what is printed. This one is what the self-tests of §8.6 take their
	// geometry from: a pattern of corner crosses has no Label behind it and no job to
	// come from, and it still has to know where the printable area ends.
	Template domain.Template

	// Settings are the three adjustments of §8.2 plus the polarity of <G> and the
	// number of copies a job that says nothing gets.
	Settings Settings

	// Head is the print head the frame is addressed to. The zero value is a WS408,
	// which is the whole parc.
	Head Head

	// DemoLabel supplies the label the `label` self-test prints (§8.6: ail, 1,236 kg,
	// double tarif).
	//
	// It is INJECTED and never built here, and that is a boundary and not a
	// formality: a demonstration label carries a product, a unit price and a pricing
	// grid, which are catalog and configuration. A printing driver that made up a
	// price would be inventing a number nobody could check. Nil is legitimate — the
	// self-test then refuses, in French, naming what is missing.
	DemoLabel func() (domain.Label, error)
}

// Printer is the raster driver. It satisfies ports.Printer.
type Printer struct {
	transport ports.Transport
	clock     ports.Clock
	log       ports.TechnicalLog
	fonts     *printing.Library
	render    *printing.Rasterizer
	template  domain.Template
	settings  Settings
	head      Head
	demoLabel func() (domain.Label, error)

	// mu serialises everything that touches the device: ONE label at a time, never
	// interleaved (§8.2). A status probe slipped into the middle of a 16 ko frame is
	// how a job comes out as garbage, and the legacy guard against interleaving was an
	// `If AllReports(...).IsLoaded Then Exit Sub` that silently ABANDONED the weighing.
	mu     sync.Mutex
	closed bool
}

// New builds the driver from what a configuration and a composition root hand it.
//
// It refuses at CONSTRUCTION what would otherwise fail once a customer is standing at
// the scale: a setting outside the bounds of the manual, a template rendered for
// another head. That is the rule the configuration already follows — an inconsistency
// stops the process at start-up, never at print time (§11.3) — and the faults come
// back ALL AT ONCE, so that a volunteer fixes one file rather than three times the
// same file.
//
// The messages about a missing collaborator are English: only a developer can ever
// read them, since no configuration file can produce a nil transport. Everything that
// comes from config.json is answered in French.
func New(o Options) (*Printer, error) {
	if o.Transport == nil {
		return nil, fmt.Errorf("raster: New: no transport; this driver carries a frame, it does not open a device (§8.4)")
	}
	if o.Clock == nil {
		return nil, fmt.Errorf("raster: New: no clock; the duration of a job is measured by the INJECTED clock (§5.3)")
	}

	head := o.Head
	if head == (Head{}) {
		head = WS408()
	}

	faults := o.Settings.Validate()
	faults = append(faults, head.Validate()...)
	faults = append(faults, checkTemplateHead(o.Template, head)...)
	if len(faults) > 0 {
		return nil, &ports.PrintError{Kind: ports.KindConfig, Op: "raster.New", Message: joinFaults(faults)}
	}

	log := o.Log
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	fonts, err := printing.NewLibrary()
	if err != nil {
		return nil, fmt.Errorf("raster: New: %w", err)
	}
	render, err := printing.NewRasterizer(fonts, log)
	if err != nil {
		fonts.Close()
		return nil, fmt.Errorf("raster: New: %w", err)
	}

	return &Printer{
		transport: o.Transport,
		clock:     o.Clock,
		log:       log,
		fonts:     fonts,
		render:    render,
		template:  o.Template,
		settings:  o.Settings,
		head:      head,
		demoLabel: o.DemoLabel,
	}, nil
}

// Descriptor reports the identity of the driver and the capabilities it declares.
//
// It is called before anything is opened — the administration screen builds its
// drop-down list from drivers nobody has instantiated — so it reads no device and
// answers the same thing twice.
func (p *Printer) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{
		ID:    ID,
		Label: Label,
		Capabilities: domain.PrinterCapabilities{
			// The whole label is a bitmap, symbol included. That is the driver.
			Raster: true,
			// This driver always ANSWERS Status, and the value of the answer depends on
			// the transport: PrinterUnknown when it cannot ask (§8.5). Declaring the
			// capability is not a promise that the device speaks — PrinterUnknown exists
			// so that no code ever has to say « prête » for lack of a return channel.
			Status: true,
			// None of the eleven commands of §8.3 cuts. A driver that declared a cutter
			// it never drives would offer a volunteer a switch that does nothing.
			Cutter: false,
			// The bound of the <Q> field, and not a shop policy: see MaxCopies.
			MaxCopies: MaxCopies,
			DotsPerMM: p.head.DotsPerMM,
		},
	}
}

// Print renders one job and hands the frame to the transport.
//
// It returns when the BYTES HAVE BEEN ACCEPTED, not when the label comes out: no
// transport guarantees the latter, which is why the screen says « Étiquette envoyée à
// l'imprimante » and why the reprint bar is permanent (important-7, §8.5).
//
// Retries do not live here. The print service owns them, because it owns the budget
// they consume: two retries at 300 ms then 1 s, inside the 8 s of the injected clock
// (§8.2). A driver that retried on its own would multiply the two.
func (p *Printer) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	img, copies, err := p.compose(job)
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	frame, err := encodeLabel(img, job.Template, p.settings, p.head, copies)
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return p.send(ctx, "raster.Print", job.Label.JobID, frame)
}

// compose turns a job into the bitmap the head will burn and the number of copies of
// it, refusing what would print a WRONG label rather than passing it on.
func (p *Printer) compose(job ports.PrintJob) (*image.Gray, int, error) {
	if faults := checkTemplateHead(job.Template, p.head); len(faults) > 0 {
		return nil, 0, &ports.PrintError{Kind: ports.KindTemplate, Op: "raster.Print", Message: joinFaults(faults)}
	}
	// The barcode is checked BEFORE the render, because its failure has a different
	// remedy: an unusable reference is a product to fix in Odoo (KindData, the product
	// is flagged), where a geometry that does not fit is a template to refuse at load
	// time (KindTemplate). §8.5 classifies by the action expected, never by the layer
	// the error came from.
	if _, err := domain.ParseEAN13(string(job.Label.Barcode)); err != nil {
		return nil, 0, &ports.PrintError{Kind: ports.KindData, Op: "raster.Print", Err: err,
			Message: fmt.Sprintf("le code-barres %q de ce produit est inutilisable : %v",
				string(job.Label.Barcode), err)}
	}
	copies, err := p.copiesFor(job)
	if err != nil {
		return nil, 0, err
	}
	template := job.Template
	img, err := p.render.Rasterize(&template, job.Label, domain.Locale(job.Locale), printing.RenderOptions{})
	if err != nil {
		return nil, 0, &ports.PrintError{Kind: ports.KindTemplate, Op: "raster.Print", Err: err,
			Message: fmt.Sprintf("l'étiquette n'a pas pu être dessinée : %v", err)}
	}
	return img, copies, nil
}

// copiesFor reports how many copies a job asks for.
//
// A job that says NOTHING gets the configured count, and that is not indulgence: the
// print service builds its PrintJob without a Copies field (§8.2), so zero means
// « unspecified » and printer.options.copies is the answer. Anything else is taken
// literally and bounded — a negative count or one past the <Q> field is refused with
// the value it was given, never rounded into something that prints.
func (p *Printer) copiesFor(job ports.PrintJob) (int, error) {
	if job.Copies == 0 {
		return p.settings.Copies, nil
	}
	if job.Copies < 1 || job.Copies > MaxCopies {
		return 0, &ports.PrintError{Kind: ports.KindConfig, Op: "raster.Print",
			Message: fmt.Sprintf("%d exemplaires demandés : le nombre d'exemplaires va de 1 à %d "+
				"(commande SBPL <Q>, six chiffres)", job.Copies, MaxCopies)}
	}
	return job.Copies, nil
}

// send hands one finished frame to the transport and times it on the injected clock.
func (p *Printer) send(ctx context.Context, op, jobID string, frame []byte) (ports.PrintReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindInternal, Op: op,
			Message: "l'imprimante a été fermée : ce poste ne peut plus imprimer sans redémarrer"}
	}

	started := p.clock.Now()
	n, err := p.transport.Write(ctx, frame)
	elapsed := p.clock.Now().Sub(started)
	if err != nil {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
			Message: fmt.Sprintf("l'imprimante ne répond pas (%s) : %d octets sur %d ont été acceptés",
				p.transport.Describe(), n, len(frame))}
	}
	// A short write with no error is the failure mode that costs the most: the frame
	// is truncated, the label comes out wrong or not at all, and the station reports a
	// success. It is treated as a transient failure so that the service retries it.
	if n != len(frame) {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op,
			Message: fmt.Sprintf("l'imprimante n'a accepté que %d octets sur %d (%s) : "+
				"l'étiquette serait tronquée", n, len(frame), p.transport.Describe())}
	}
	return ports.PrintReceipt{JobID: jobID, Bytes: n, Duration: elapsed}, nil
}

// Status reports what the device says about itself, or an honest admission that we do
// not know (§8.5).
//
// It NEVER turns a silence into a failure. A transport that cannot ask answers
// PrinterUnknown, which is the whole reason that value exists, and a printer that
// stays quiet for 500 ms is reported as unknown rather than faulted: confirming a
// physical event with a probe that does not observe it is exactly the mistake
// important-7 removed.
func (p *Printer) Status(ctx context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: "l'imprimante a été fermée par le poste."}
	}

	answer, err := p.transport.Query(ctx, enquiry, statusBudget)
	switch {
	case errors.Is(err, ports.ErrUnsupported):
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: fmt.Sprintf("état inconnu : %s ne peut pas interroger l'imprimante. "+
				"L'étiquette part, la réponse ne revient pas.", p.transport.Describe())}
	case err != nil:
		return ports.PrinterStatus{Health: ports.PrinterFaulted, Raw: answer,
			Detail: fmt.Sprintf("l'imprimante n'a pas répondu (%s) : %v", p.transport.Describe(), err)}
	case len(answer) == 0:
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: fmt.Sprintf("état inconnu : %s n'a rien renvoyé en %s.",
				p.transport.Describe(), statusBudget)}
	}
	// Any non-empty answer means the printer is ALIVE (§8.5) — and alive is not ready.
	// The fine decoding stays OFF until a real frame has been captured, so what came
	// back may well spell PAPER OUT; PrinterReady means « answered and has NOTHING TO
	// REPORT » (ports), and a station that cannot read the report may not claim there is
	// none. It would be a green light on /readyz over an empty roll (§14.5). Raw is what
	// makes completing the decoding possible without travelling to the shop, and the day
	// it is completed this branch gains a real health.
	return ports.PrinterStatus{Health: ports.PrinterUnknown, Raw: answer,
		Detail: fmt.Sprintf("l'imprimante a répondu %d octet(s) (%s) : elle est vivante. "+
			"Le décodage détaillé de la trame n'est pas encore activé, l'état exact reste inconnu.",
			len(answer), p.transport.Describe())}
}

// SelfTest prints one built-in pattern (§8.6).
//
// `alignment` and `ruler` are drawn HERE, from the geometry of the template in
// service: they carry no business data, so nothing has to be injected for them to
// exist. `label` is a real label and therefore needs a real Label, which only the
// station can build.
func (p *Printer) SelfTest(ctx context.Context, what string) error {
	switch what {
	case SelfTestLabel:
		return p.printDemoLabel(ctx)
	case SelfTestAlignment:
		return p.printPattern(ctx, "raster.SelfTest.alignment", alignmentPattern(p.template))
	case SelfTestRuler:
		return p.printPattern(ctx, "raster.SelfTest.ruler", rulerPattern(p.template))
	}
	return &ports.PrintError{Kind: ports.KindConfig, Op: "raster.SelfTest",
		Message: fmt.Sprintf("auto-test inconnu %q : les auto-tests disponibles sont %s, %s et %s",
			what, SelfTestLabel, SelfTestAlignment, SelfTestRuler)}
}

// printDemoLabel prints the demonstration label of the `label` self-test.
func (p *Printer) printDemoLabel(ctx context.Context) error {
	if p.demoLabel == nil {
		return &ports.PrintError{Kind: ports.KindConfig, Op: "raster.SelfTest.label",
			Message: "aucune étiquette de démonstration n'a été fournie à l'imprimante : " +
				"l'étiquette de test porte un produit et des prix, qui viennent du catalogue et de la " +
				"configuration du poste, jamais du driver"}
	}
	label, err := p.demoLabel()
	if err != nil {
		return &ports.PrintError{Kind: ports.KindData, Op: "raster.SelfTest.label", Err: err,
			Message: fmt.Sprintf("l'étiquette de démonstration n'a pas pu être préparée : %v", err)}
	}
	_, err = p.Print(ctx, ports.PrintJob{
		Label:    label,
		Template: p.template,
		Locale:   string(domain.LocaleFrench),
		Copies:   1,
	})
	return err
}

// printPattern encapsulates one built-in pattern and sends it.
func (p *Printer) printPattern(ctx context.Context, op string, img *image.Gray) error {
	frame, err := encodeLabel(img, p.template, p.settings, p.head, 1)
	if err != nil {
		return err
	}
	_, err = p.send(ctx, op, op, frame)
	return err
}

// Close releases the transport and the font faces the renderer memoised.
//
// It is idempotent: the Hub closes on a configuration reload and again on shutdown
// (§11.4, §13.4). What it does NOT do is invent a reason to fail — a handle already
// released is not news.
func (p *Printer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	err := p.transport.Close()
	if fontErr := p.fonts.Close(); err == nil {
		err = fontErr
	}
	return err
}

// checkTemplateHead reports whether a template can be printed by this head.
//
// The resolution of the whole application has ONE source, template.media.dots_per_mm
// (mineur-3), and the capability of a driver is what it is COMPARED to. A 12 dots/mm
// template sent to a WS408 prints at two thirds of its size, with a symbol under every
// GS1 floor, and no byte of the frame says so: the label simply comes out wrong.
func checkTemplateHead(t domain.Template, h Head) []domain.Fault {
	if t.Media.DotsPerMM == h.DotsPerMM {
		return nil
	}
	if t.Media.DotsPerMM <= 0 {
		return []domain.Fault{{Field: "printer.template",
			Message: fmt.Sprintf("le gabarit %q ne déclare aucune résolution (media.dots_per_mm = %g) : "+
				"c'est elle qui donne au bitmap sa taille physique", t.Name, t.Media.DotsPerMM)}}
	}
	return []domain.Fault{{Field: "printer.template",
		Message: fmt.Sprintf("le gabarit %q est dessiné pour une tête de %g dots/mm et cette imprimante "+
			"en fait %g : l'étiquette sortirait à une autre échelle",
			t.Name, t.Media.DotsPerMM, h.DotsPerMM)}}
}

// joinFaults gathers every fault into the single French message an operator reads on
// the administration screen, one per line, each naming its own key.
func joinFaults(faults []domain.Fault) string {
	lines := make([]string, 0, len(faults))
	for _, f := range faults {
		lines = append(lines, f.String())
	}
	return strings.Join(lines, " ; ")
}
