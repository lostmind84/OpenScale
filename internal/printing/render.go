package printing

// This file is the RENDER ENGINE of §7.3: the two entry points that turn a label into the
// dots a print head burns, the journal a truncation is reported to, and the two ink
// primitives every stroke of this package goes through. What a single field does inside
// its box is in field.go, the final binarisation in threshold.go, the bench overlay in
// annotate.go.

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log/slog"
	"sync"

	"openscale/internal/domain"
)

// The technical anomalies a render can report. None of them stops a label: a
// customer is standing at the scale, and a label that says a little less is worth
// more than no label at all. But none of them is silent either.
const (
	// codeFieldTruncated is raised when the automatic reduction reached the floor of
	// §7.3 and the text still had to be cut.
	codeFieldTruncated = "ERR-PRN-10"
	// codeGlyphMissing is raised when neither the label font nor its fallback carries
	// a character of the catalog.
	codeGlyphMissing = "ERR-PRN-11"
	// codeUnknownLocale is raised when the job named a language the binary has no
	// words for and the label was printed in French.
	codeUnknownLocale = "ERR-PRN-12"
)

// RenderOptions carries the debugging overlays Rasterize may add to a label.
//
// The RenderOptions.WithBarcode field is GONE (A2, important-1): the symbol is in
// the bitmap like everything else, and no consumer delegates it to the firmware any
// more. Nothing else belongs here either -- what a label says comes from the
// template and from the Label, never from a render option.
type RenderOptions struct {
	// Annotate draws the printable area, the barcode quiet zones and a ruler. It is a
	// bench tool: a printed label never carries it.
	Annotate bool
}

// TechnicalLog is the sink a render reports an anomaly to.
//
// Declared here rather than imported from station/ports, which is the Go idiom this
// project follows (§5.3): the interface belongs to its CONSUMER, and printing
// consumes exactly one method of it. A ports.TechnicalLog satisfies this without
// knowing it exists.
type TechnicalLog interface {
	// Technical records one event. level is one of debug, info, warn, error,
	// critical; source is one of scale, printer, catalog, ui, config, http, system;
	// code is an ERR-xxx-nn identifier, and message is FRENCH.
	Technical(level, source, code, message, detail string)
}

// Rasterizer turns a label into the dots a print head burns.
//
// IT EXISTS BECAUSE THE SIGNATURE OF §7.3 HAS NOWHERE TO PUT TWO THINGS a render
// cannot do without. The first is the font library: faces are memoised and closed
// together, and building one per label would parse four TrueType files per customer.
// The second is the journal: §7.3 requires the automatic reduction to truncate "en
// journalisant une anomalie technique", and a function that receives no sink has no
// one to tell. Both live here; the free Rasterize below is the §7.3 entry point and
// borrows a shared one.
type Rasterizer struct {
	fonts *Library
	log   TechnicalLog
}

// NewRasterizer wires a renderer to the fonts it draws with and the journal it
// reports to.
//
// It refuses a nil journal instead of quietly accepting one. A renderer with nobody
// to tell is exactly the silent truncation §7.3 forbids, and a driver always has a
// TechnicalLog to give (§5.3); a test that wants none passes a recorder that
// discards.
func NewRasterizer(fonts *Library, log TechnicalLog) (*Rasterizer, error) {
	if fonts == nil {
		return nil, fmt.Errorf("printing: NewRasterizer: aucune bibliothèque de polices")
	}
	if log == nil {
		return nil, fmt.Errorf("printing: NewRasterizer: aucun journal technique : " +
			"un rendu qui tronque un champ doit avoir quelqu'un à qui le dire")
	}
	return &Rasterizer{fonts: fonts, log: log}, nil
}

// Rasterize renders the WHOLE label -- text, frames, EAN-13 symbol and its HRI line
// -- at the exact pitch of the print head.
//
// ONE RENDER, FOUR CONSUMERS (§7.3): the preview screen, the PDF export, the raster
// driver and the SBPL encapsulation all read the SAME image.Gray.
//
// Media dimensions come from the TEMPLATE (media.width_um, media.height_um,
// media.dots_per_mm), never from a constant of the engine, and the resolution handed
// to the font rasterizer is derived from media.dots_per_mm ALONE -- 8 dots/mm is
// 203.2 dpi (mineur-3). The value shipped in weighing_identical is 40 x 25.4 mm,
// i.e. 320 x 203 dots at 8 dots/mm. It only aligns the life-size preview; it
// validates nothing. Hard rule 3 compares the inked content to the geometry of the
// EXISTING label, which is measured, so no roll measurement is required and the
// template passes whichever media is assumed.
//
// The order of the strokes is text, then frames, then symbol, and it is deliberate:
// the symbol goes down LAST, on ink only, so that nothing this engine draws can end
// up over the bars. Rule 5 already forbids it; drawing in this order means a
// template that somehow slipped past would still not cut a scan line -- the failure
// mode ADR-029 exists to remove.
//
// It journals rather than fails on what a label can survive: a name too long for its
// box, a character no embedded font carries, a language nobody taught it. It fails
// on what would print a WRONG label: a template with no resolution, a barcode that
// is not thirteen valid digits, a symbol block that does not fit the media.
func (r *Rasterizer) Rasterize(g *domain.Template, label domain.Label, loc domain.Locale, o RenderOptions) (*image.Gray, error) {
	if g == nil {
		return nil, fmt.Errorf("printing: Rasterize: aucun gabarit")
	}
	if g.Media.DotsPerMM <= 0 {
		return nil, fmt.Errorf("printing: Rasterize: media.dots_per_mm = %g : "+
			"la résolution de la tête est la source unique de toute la géométrie",
			g.Media.DotsPerMM)
	}
	width, height := roundDots(g.Media, g.Media.WidthUM), roundDots(g.Media, g.Media.HeightUM)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("printing: Rasterize: média de %d × %d dots : "+
			"une étiquette a une surface", width, height)
	}

	w, known := wordsFor(loc)
	if !known {
		r.anomaly(codeUnknownLocale, "langue inconnue : l'étiquette est imprimée en français",
			fmt.Sprintf("langue demandée : %q", string(loc)))
	}

	// Everything below draws with the library's faces, which cannot be shared between
	// goroutines (see Library.drawing). The four consumers of this render reach it from
	// four different places — the preview screen refreshes on every keystroke of the
	// template editor — so exclusivity is taken here, once, for the whole label.
	r.fonts.drawing.Lock()
	defer r.fonts.drawing.Unlock()

	img := image.NewGray(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{Y: 0xFF}), image.Point{}, draw.Src)

	// The cardinality of the grid is what makes mono-tarif work with no conditional
	// code anywhere else: the secondary price simply stops being active (§7.2).
	tierCount := len(label.Lines)
	for _, e := range g.Elements {
		if e.Field == domain.FieldBarcode || !e.Active(tierCount) {
			continue
		}
		if err := r.drawElement(img, g, e, label, w); err != nil {
			return nil, err
		}
	}

	symbol, err := r.drawSymbol(img, g, label)
	if err != nil {
		return nil, err
	}

	// FINAL thresholding, mandatory: the head is binary. Keeping grays would let the
	// driver dither the render and produce irregular bars -- precisely the defect we
	// refuse to introduce. The threshold is DIFFERENTIATED (mineur-1).
	applyThreshold(img, symbol.Bounds(), symbolThreshold)
	for _, rest := range surrounding(img.Bounds(), symbol.Bounds()) {
		applyThreshold(img, rest, textThreshold(g))
	}

	if o.Annotate {
		annotate(img, g, symbol)
	}
	return img, nil
}

// Rasterize renders one label with the process-wide renderer.
//
// It is the signature of §7.3, and it carries neither a font library nor a journal.
// The library it borrows is built once and lives as long as the process, which is
// what the memoisation of §7.3 is for; the journal it borrows is log/slog, so that a
// truncated product name still reaches whatever the binary configured (§5.1) instead
// of disappearing.
//
// A driver, which holds a ports.TechnicalLog of its own (§5.3), should build a
// Rasterizer with NewRasterizer and use its method: the anomaly then lands in the
// technical journal a volunteer actually reads.
func Rasterize(g *domain.Template, label domain.Label, loc domain.Locale, o RenderOptions) (*image.Gray, error) {
	r, err := sharedRasterizer()
	if err != nil {
		return nil, err
	}
	return r.Rasterize(g, label, loc, o)
}

// shared is the renderer the free Rasterize draws with.
//
// It is never closed, and that is bounded rather than leaky: the faces it memoises
// are keyed by family, ppem and weight, and the reduction loop of one template can
// only ask for the handful of sizes between its nominal body and its floor.
var shared struct {
	once sync.Once
	r    *Rasterizer
	err  error
}

func sharedRasterizer() (*Rasterizer, error) {
	shared.once.Do(func() {
		fonts, err := NewLibrary()
		if err != nil {
			shared.err = err
			return
		}
		shared.r, shared.err = NewRasterizer(fonts, slogLog{})
	})
	return shared.r, shared.err
}

// slogLog is the journal of last resort, for the entry point that receives none.
type slogLog struct{}

// Technical routes one anomaly to the default structured logger.
func (slogLog) Technical(level, source, code, message, detail string) {
	args := []any{"source", source, "code", code}
	if detail != "" {
		args = append(args, "detail", detail)
	}
	switch level {
	case "debug":
		slog.Debug(message, args...)
	case "info":
		slog.Info(message, args...)
	case "error", "critical":
		slog.Error(message, args...)
	default:
		slog.Warn(message, args...)
	}
}

// anomaly reports something a volunteer may have to act on. The message is FRENCH:
// it is read on the troubleshooting screen (§5.3).
func (r *Rasterizer) anomaly(code, message, detail string) {
	r.log.Technical("warn", "printer", code, message, detail)
}

// drawSymbol lays the EAN-13 block down and reports the geometry it used, which is
// what the differentiated threshold needs afterwards.
//
// The order is the one symbol.go asks for: NewSymbolOptions first, because it knows
// nothing about fonts, then FitHRIFace, then the face.
func (r *Rasterizer) drawSymbol(dst *image.Gray, g *domain.Template, label domain.Label) (SymbolOptions, error) {
	o := NewSymbolOptions(*g)
	face, _, err := FitHRIFace(r.fonts, labelFont, o, g.Media.DotsPerMM)
	if err != nil {
		return SymbolOptions{}, err
	}
	o.HRIFace = face

	modules, err := domain.Modules(label.Barcode)
	if err != nil {
		return SymbolOptions{}, fmt.Errorf("printing: Rasterize: le code-barres de l'étiquette : %w", err)
	}
	if err := DrawEAN13(dst, label.Barcode, modules, o); err != nil {
		return SymbolOptions{}, err
	}
	return o, nil
}

// drawFrame outlines a box with the one dot rule §7.2 gives primary_unit_price, and
// which annotate reuses for its own boxes.
func drawFrame(dst *image.Gray, box image.Rectangle) {
	if box.Dx() <= 0 || box.Dy() <= 0 {
		return
	}
	fill(dst, image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+1))
	fill(dst, image.Rect(box.Min.X, box.Max.Y-1, box.Max.X, box.Max.Y))
	fill(dst, image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y))
	fill(dst, image.Rect(box.Max.X-1, box.Min.Y, box.Max.X, box.Max.Y))
}

// fill burns a rectangle black, clipped to the image.
func fill(dst *image.Gray, r image.Rectangle) {
	draw.Draw(dst, r, image.NewUniform(color.Gray{Y: 0x00}), image.Point{}, draw.Src)
}
