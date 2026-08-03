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
// call THE SAME ENCODER: internal/printing/sbpl owns THE PROTOCOL — the encapsulation
// of a job and the reading of a status frame — while this package owns the output path,
// the settings, the self-tests and the lifecycle (§5.1, §8.1). frame.go and the call to
// sbpl.FaultOfStatusFrame are the whole of the border between them.
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

// This file is the driver itself: what it is given, what it declares, how one job
// becomes a frame, and how it lets go. What it answers about the device is in
// status.go, the three patterns of §8.6 and its answer to them in selftest.go, and
// what New refuses at construction in settings.go.

import (
	"context"
	"fmt"
	"image"
	"sync"

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

// The three self-tests of §8.6 are NOT re-spelled here.
//
// They were, in three constants of this package, and that was the THIRD spelling of the
// same three words: the catalogue of internal/printing carries the names, the wording of
// each button, the screen each one belongs to and the sentence saying what the print
// settles, and `preview` had already dropped its own copy. A driver says WHICH of the
// three it honours — see Driver — and spells none of them.
//
// Three copies is how a fourth diverges: one of them is renamed, the other two keep
// answering the old word, and the button on the screen stops reaching the driver.

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
	// Closure is checked BEFORE composing, and send checks it again. Composing first
	// draws through a font library Close has already shut, so the render fails and the
	// job comes back as KindTemplate — naming printer.template for a template that has
	// nothing wrong, and carrying an English developer sentence into a French one the
	// volunteer reads. The check in send stays: it is what covers a Close landing
	// between here and the write.
	if err := p.refuseWhenClosed("raster.Print"); err != nil {
		return ports.PrintReceipt{}, err
	}
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

// refuseWhenClosed reports the refusal a closed printer owes its caller, or nil.
//
// It exists so that Print can answer that refusal before it draws anything, while send
// keeps answering it under the lock it already holds.
func (p *Printer) refuseWhenClosed(op string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return &ports.PrintError{Kind: ports.KindInternal, Op: op,
			Message: "l'imprimante a été fermée : ce poste ne peut plus imprimer sans redémarrer"}
	}
	return nil
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
