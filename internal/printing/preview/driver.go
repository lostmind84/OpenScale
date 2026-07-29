package preview

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// ID is the registry key of this driver and the value of printer.type (§8.1).
const ID = domain.PrinterPreview

// Label is what a volunteer reads in the drop-down list of the administration screen.
//
// It says what the driver DOES and not only what it is called, and that sentence is the
// guard rail: `preview` is one wrong click away from `raster` in a list, and a station set
// on it goes on saying « Étiquette envoyée à l'imprimante » while nothing comes out. The
// production path is `raster` (ADR-002).
const Label = "Aperçu — écrit un fichier, n'imprime rien"

// The three self-tests of §8.6, spelled as the troubleshooting route sends them.
const (
	// SelfTestLabel writes the demonstration label, which is exactly what this driver
	// exists to produce.
	SelfTestLabel = "label"
	// SelfTestAlignment settles the polarity of the <G> command and the registration of
	// the media under the head. Both are facts about a machine this driver never drives.
	SelfTestAlignment = "alignment"
	// SelfTestRuler settles the pitch the head really prints at, which is the same case.
	SelfTestRuler = "ruler"
)

// maxCopies is what this driver honestly accepts: ONE.
//
// A preview writes a file, and n identical copies of a file are n identical files. A driver
// that produced five of them would be counting a roll that does not exist.
const maxCopies = 1

// The shape of the file names, and it is the one the `file` transport already writes
// (§8.4): the instant with colons replaced by hyphens, because a colon is not a legal
// character in a Windows file name and the parc is Windows.
const (
	stampLayout    = "2006-01-02T15-04-05"
	fileNameFormat = "%s_%03d%s"
)

// The two extensions. They are the whole difference between the two files: same bitmap,
// one at the pitch of the head and one at exact physical scale.
const (
	imageExtension    = ".png"
	documentExtension = ".pdf"
)

// maxNameAttempts bounds the search for a free pair of names inside one second. It is the
// bound the `file` transport uses, for the reason it gives: a thousand collisions in one
// second means something else is wrong, and failing with a name beats looping.
const maxNameAttempts = 1000

// Options is everything the preview driver is given, and nothing it could invent.
type Options struct {
	// Dir is the directory the two files are written to. Required.
	//
	// It is handed down by the composition root and never read from printer.options,
	// because it is a fact about THIS station's data directory. It is a directory of its
	// own and deliberately not the one the `file` transport drops frames into: mixing
	// previews and raw frames would make « envoyez-moi le fichier de la dernière
	// étiquette » an ambiguous sentence, and that sentence is how support works.
	Dir string

	// Clock dates the files and times a job. time.Now is out of reach here (§5.3).
	Clock ports.Clock

	// Log is where the RENDER reports what a volunteer may have to act on: a name the
	// automatic reduction had to truncate, a character no embedded font carries (§7.3).
	// Nil is replaced by ports.NopTechnicalLog.
	Log ports.TechnicalLog

	// Template is the template IN SERVICE (printer.template, §11.2). A print job carries
	// its own and that one is used; this is what the self-test falls back on.
	Template domain.Template

	// DemoLabel supplies the label the `label` self-test writes (§8.6).
	//
	// INJECTED, exactly as the raster driver takes it: a demonstration label carries a
	// product and prices, which come from the catalog and the configuration. A printing
	// driver that made up a price would be inventing a number nobody could check.
	DemoLabel func() (domain.Label, error)
}

// Printer is the preview driver. It satisfies ports.Printer.
//
// IT OPENS NO DEVICE, and that is what it is for. The three values of printer.type share
// the whole rendering and differ only by where the bitmap goes: `raster` hands it to the
// print queue of the system, `sbpl` writes it straight to the device, and this one writes
// the two files anybody can measure — for acceptance, for the ±1 dot adjustment, and for
// support at a distance.
//
// It is also what a station in FACTORY CONFIGURATION prints on (§11.3). The neutral profile
// carries no printer.options at all — darkness, speed and the number of copies are set on a
// real print run, and a factory profile has no business inventing them — so the driver such
// a station falls back on must require none of them. This one requires none.
type Printer struct {
	dir       string
	clock     ports.Clock
	log       ports.TechnicalLog
	fonts     *printing.Library
	render    *printing.Rasterizer
	template  domain.Template
	demoLabel func() (domain.Label, error)

	// mu serialises everything that names a file: two jobs claiming the same instant must
	// not claim the same sequence number.
	mu       sync.Mutex
	sequence int
	closed   bool
}

// New builds the driver from what a configuration and a composition root hand it.
//
// The messages about a missing collaborator are English: no configuration file can produce
// a nil clock, so only a developer can ever read them.
func New(o Options) (*Printer, error) {
	if strings.TrimSpace(o.Dir) == "" {
		return nil, errors.New("preview: New: no directory; this driver writes files and the " +
			"composition root owns the data directory")
	}
	if o.Clock == nil {
		return nil, errors.New("preview: New: no clock; a job is dated by the INJECTED clock (§5.3)")
	}

	log := o.Log
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	fonts, err := printing.NewLibrary()
	if err != nil {
		return nil, fmt.Errorf("preview: New: %w", err)
	}
	render, err := printing.NewRasterizer(fonts, log)
	if err != nil {
		fonts.Close()
		return nil, fmt.Errorf("preview: New: %w", err)
	}
	return &Printer{
		dir: o.Dir, clock: o.Clock, log: log, fonts: fonts, render: render,
		template: o.Template, demoLabel: o.DemoLabel,
	}, nil
}

// Descriptor reports the identity of the driver and the capabilities it declares.
//
// Status is FALSE, and it is the one capability worth arguing about: there is no device to
// interrogate, so a driver claiming the capability would be claiming a return channel it
// invented. See Status for what it answers instead.
func (p *Printer) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{
		ID:    ID,
		Label: Label,
		Capabilities: domain.PrinterCapabilities{
			Raster:    true,
			Status:    false,
			Cutter:    false,
			MaxCopies: maxCopies,
			DotsPerMM: p.template.Media.DotsPerMM,
		},
	}
}

// Print renders one job and writes the two files.
//
// The copy count is deliberately IGNORED rather than refused: a preview writes one file,
// and the print service builds its job without a Copies field anyway (§8.2). Refusing a job
// over a number no file can honour would take a station in factory configuration out of the
// one thing it can still do.
func (p *Printer) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	img, err := p.compose(job)
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return p.write(ctx, "preview.Print", job.Label.JobID, img, job.Template.Media)
}

// compose turns a job into the bitmap the head WOULD have burnt, refusing what would show a
// wrong label rather than writing it.
//
// The barcode is checked before the render, for the reason the raster driver checks it
// there: an unusable reference is a product to fix in Odoo, and §8.5 classifies a failure by
// the action expected of a human.
func (p *Printer) compose(job ports.PrintJob) (*image.Gray, error) {
	if _, err := domain.ParseEAN13(string(job.Label.Barcode)); err != nil {
		return nil, &ports.PrintError{Kind: ports.KindData, Op: "preview.Print", Err: err,
			Message: fmt.Sprintf("le code-barres %q de ce produit est inutilisable : %v",
				string(job.Label.Barcode), err)}
	}
	template := job.Template
	img, err := p.render.Rasterize(&template, job.Label, domain.Locale(job.Locale), printing.RenderOptions{})
	if err != nil {
		return nil, &ports.PrintError{Kind: ports.KindTemplate, Op: "preview.Print", Err: err,
			Message: fmt.Sprintf("l'étiquette n'a pas pu être dessinée : %v", err)}
	}
	return img, nil
}

// write encodes the bitmap twice and reports how many bytes reached the disk.
//
// The PNG goes down FIRST and the PDF second, and both are written before the receipt is
// returned: the pair is what a volunteer is asked to send, and half of it is a support
// request that has to be made twice.
func (p *Printer) write(ctx context.Context, op, jobID string, img *image.Gray, media domain.Media) (ports.PrintReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindInternal, Op: op,
			Message: "l'aperçu a été fermé : ce poste ne peut plus écrire d'étiquette sans redémarrer"}
	}
	if err := ctx.Err(); err != nil {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
			Message: "l'aperçu a été interrompu avant d'écrire l'étiquette"}
	}

	started := p.clock.Now()
	base, err := p.reserve()
	if err != nil {
		return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
			Message: fmt.Sprintf("l'étiquette n'a pas pu être écrite dans %s : %v", p.dir, err)}
	}

	written := 0
	for _, target := range []struct {
		path   string
		encode func(*os.File) error
	}{
		{base + imageExtension, func(f *os.File) error { return EncodePNG(f, img) }},
		{base + documentExtension, func(f *os.File) error { return EncodePDF(f, img, media) }},
	} {
		bytes, err := writeFile(target.path, target.encode)
		if err != nil {
			return ports.PrintReceipt{}, &ports.PrintError{Kind: ports.KindTransient, Op: op, Err: err,
				Message: fmt.Sprintf("l'étiquette n'a pas pu être écrite dans %s : %v", target.path, err)}
		}
		written += bytes
	}
	return ports.PrintReceipt{
		JobID: jobID, Bytes: written, Duration: p.clock.Now().Sub(started),
	}, nil
}

// reserve claims a base name whose two files do not exist yet.
//
// EXCLUSIVE, like the `file` transport: a preview that silently replaced the one before it
// would lose the very label somebody asked for, and a support directory shared by two
// stations is exactly where that happens.
func (p *Printer) reserve() (string, error) {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return "", err
	}
	stamp := p.clock.Now().Format(stampLayout)
	for attempt := 0; attempt < maxNameAttempts; attempt++ {
		p.sequence++
		base := filepath.Join(p.dir, fmt.Sprintf(fileNameFormat, stamp, p.sequence, ""))
		if free, err := bothNamesAreFree(base); err != nil {
			return "", err
		} else if free {
			return base, nil
		}
	}
	return "", fmt.Errorf("aucun nom libre après %d essais pour l'horodatage %s",
		maxNameAttempts, stamp)
}

// bothNamesAreFree reports that neither of the two files of one preview exists.
//
// BOTH, and not one: a directory holding the PNG of a previous run and not its PDF would
// otherwise produce a pair whose two halves describe different labels.
func bothNamesAreFree(base string) (bool, error) {
	for _, extension := range []string{imageExtension, documentExtension} {
		_, err := os.Stat(base + extension)
		switch {
		case err == nil:
			return false, nil
		case !errors.Is(err, os.ErrNotExist):
			return false, err
		}
	}
	return true, nil
}

// writeFile creates one file and reports how many bytes it holds.
//
// O_EXCL, so that the freshness of a preview is a property of the file system and not of
// the sequence number this driver keeps in memory.
func writeFile(path string, encode func(*os.File) error) (int, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	if err := encode(file); err != nil {
		file.Close()
		return 0, err
	}
	// The size is read BEFORE the close and from the handle: a caller reporting a byte
	// count it took from a second stat would be reporting on a file somebody else may have
	// touched in between.
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

// Status reports what this driver honestly knows about itself.
//
// PrinterUnknown, and never PrinterReady. There is no device here, so there is nothing that
// can be offline or jammed — but there is nothing that can be declared ready either, and a
// green light over a station where no label ever comes out is the failure mode this whole
// driver has to be careful about (§14.5). The detail says, in one sentence a volunteer can
// act on, that nothing is being printed and where the files are going.
func (p *Printer) Status(context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: "l'aperçu a été fermé par le poste."}
	}
	return ports.PrinterStatus{Health: ports.PrinterUnknown,
		Detail: fmt.Sprintf("aperçu : aucune étiquette n'est imprimée, chacune est écrite "+
			"dans %s.", p.dir)}
}

// SelfTest writes the demonstration label, and refuses the two patterns that settle facts
// about a print head (§8.6).
//
// The refusal is not a gap to be filled later. `alignment` lifts the polarity of the <G>
// command and the registration of the media, `ruler` the pitch the head really prints at:
// both are read off PAPER, and a file that claimed to answer them would answer about
// itself.
func (p *Printer) SelfTest(ctx context.Context, what string) error {
	switch what {
	case SelfTestLabel:
		return p.writeDemoLabel(ctx)
	case SelfTestAlignment, SelfTestRuler:
		return &ports.PrintError{Kind: ports.KindConfig, Op: "preview.SelfTest",
			Message: fmt.Sprintf("l'auto-test « %s » se lit sur une étiquette imprimée : il "+
				"règle la tête d'impression, et ce poste écrit un fichier sans rien imprimer. "+
				"Choisissez le pilote « %s » sur une vraie imprimante.", what, domain.PrinterRaster)}
	}
	return &ports.PrintError{Kind: ports.KindConfig, Op: "preview.SelfTest",
		Message: fmt.Sprintf("auto-test inconnu %q : les auto-tests disponibles sont %s, %s et %s",
			what, SelfTestLabel, SelfTestAlignment, SelfTestRuler)}
}

// writeDemoLabel writes the demonstration label of the `label` self-test.
func (p *Printer) writeDemoLabel(ctx context.Context) error {
	if p.demoLabel == nil {
		return &ports.PrintError{Kind: ports.KindConfig, Op: "preview.SelfTest.label",
			Message: "aucune étiquette de démonstration n'a été fournie à l'aperçu : " +
				"l'étiquette de test porte un produit et des prix, qui viennent du catalogue et de la " +
				"configuration du poste, jamais du driver"}
	}
	label, err := p.demoLabel()
	if err != nil {
		return &ports.PrintError{Kind: ports.KindData, Op: "preview.SelfTest.label", Err: err,
			Message: fmt.Sprintf("l'étiquette de démonstration n'a pas pu être préparée : %v", err)}
	}
	_, err = p.Print(ctx, ports.PrintJob{
		Label: label, Template: p.template, Locale: string(domain.LocaleFrench), Copies: 1,
	})
	return err
}

// Close releases the font faces the renderer memoised, and nothing else.
//
// There is no handle to give up — that is the point of this driver — but the font library is
// a real resource and a reload replaces the printer (§11.4). It is idempotent, because the
// Hub closes on a reload and again on shutdown.
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
