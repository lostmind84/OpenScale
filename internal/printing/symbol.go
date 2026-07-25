package printing

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"openscale/internal/domain"
)

// The module counts of an EAN-13 block. They belong to the SYMBOLOGY, not to any
// template, which is why they are constants here and appear in no configuration
// file: 11 modules of left quiet zone, 95 of bars, 7 of right quiet zone (§7.4).
const (
	quietZoneLeftModules  = 11
	barModules            = 95
	quietZoneRightModules = 7
	blockModules          = quietZoneLeftModules + barModules + quietZoneRightModules

	// digitModules is what one encoded digit occupies, and therefore the width of
	// the cell its human-readable twin is centred in.
	digitModules = 7

	leftGuardModules   = 3
	leftGroupFirst     = leftGuardModules
	centreGuardFirst   = leftGroupFirst + 6*digitModules
	centreGuardModules = 5
	rightGroupFirst    = centreGuardFirst + centreGuardModules
	rightGuardFirst    = rightGroupFirst + 6*digitModules

	// hriCellClearanceDots is what FitHRIFace keeps between two neighbouring digits.
	// Two digits that touch are two digits nobody reads -- neither the cashier who
	// falls back on the HRI when the scanner refuses, nor the template matching that
	// proves in test that the line is really there.
	hriCellClearanceDots = 2
)

// SymbolOptions describes the geometry of one EAN-13 symbol, in dots.
//
// XDots and YDots are the top-left corner of the WHOLE BLOCK -- quiet zones
// included -- and not the first bar. That is the same origin the template speaks in:
// domain.SymbolGeometry.XUM is where the 113 modules of rule 4 start, and the
// shipped template puts it at 0. Reading it as the first bar instead would push the
// left quiet zone, and the leading HRI digit that lives in it, off the left edge of
// the label.
type SymbolOptions struct {
	XDots, YDots     int
	ModuleMilliDots  int       // 2344 = 2.344 dots = 0.293 mm
	BarHeightDots    int       // 87 in template A (10 875 um, ADR-029)
	GuardDescentDots int       // 12 in template A (1 465 um, 5 modules)
	HRIFace          font.Face // size derived from hri_height_um, see FitHRIFace
	HRIHeightDots    int       // 23 in template A (2 930 um) -- never 0
}

// edge reports the position of module boundary i, in whole dots, measured from the
// left edge of module 0.
//
//	edge(i) = (i*ModuleMilliDots + 500) / 1000
//
// THE central rule of §7.4, and the reason it is one expression rather than a loop:
// every edge is the rounded IDEAL position, never the accumulation of a rounded
// step. Accumulating a step rounded to 2 dots lands 33 dots short of the ideal after
// 95 modules; a strict 2/3 alternation, which is what the bars look like, lands 14
// dots long. Rounding the ideal keeps every edge within half a dot of where it
// belongs, and the error at the last edge is no worse than at the tenth.
func (o SymbolOptions) edge(i int) int { return (i*o.ModuleMilliDots + 500) / 1000 }

// TotalWidthDots is the over-all width of the block, quiet zones included: 265 dots
// at the shipped module.
func (o SymbolOptions) TotalWidthDots() int { return o.edge(blockModules) }

// BarsWidthDots is the width of the 95 bar modules alone: 223 dots at the shipped
// module.
func (o SymbolOptions) BarsWidthDots() int { return o.edge(barModules) }

// HeightDots is where the symbol block ENDS, and there is only ONE definition of it:
//
//	symbol_height = bar_height + max(guard_descent, hri_height)
//
// NOT the sum of the three. The HRI digits sit inside the descent band, so the
// deeper of the two commands the bottom of the block. It mirrors
// domain.SymbolGeometry.HeightUM, which is the authority; this is the same rule
// expressed in whole dots.
func (o SymbolOptions) HeightDots() int {
	descent := o.GuardDescentDots
	if o.HRIHeightDots > descent {
		descent = o.HRIHeightDots
	}
	return o.BarHeightDots + descent
}

// Bounds is the rectangle the block occupies, quiet zones and HRI band included.
func (o SymbolOptions) Bounds() image.Rectangle {
	return image.Rect(o.XDots, o.YDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.HeightDots())
}

// barsLeft is the x of the first bar: the block origin plus the left quiet zone.
func (o SymbolOptions) barsLeft() int { return o.XDots + o.edge(quietZoneLeftModules) }

// isGuard reports whether bar module i belongs to one of the three guard patterns,
// which run GuardDescentDots lower than the rest.
func isGuard(i int) bool {
	return i < leftGuardModules ||
		(i >= centreGuardFirst && i < centreGuardFirst+centreGuardModules) ||
		i >= rightGuardFirst
}

// NewSymbolOptions derives the drawing geometry of a template's symbol, in whole
// dots, the volunteer's ±1 dot adjustment included.
//
// Every micrometre length is rounded half up on its milli-dot value, so the engine
// converts once, here, and no other place in the package owns an opinion about
// rounding.
func NewSymbolOptions(t domain.Template) SymbolOptions {
	return SymbolOptions{
		XDots:            roundDots(t.Media, t.Symbol.XUM) + t.OffsetXDots,
		YDots:            roundDots(t.Media, t.Symbol.YUM) + t.OffsetYDots,
		ModuleMilliDots:  t.Symbol.ModuleMilliDots,
		BarHeightDots:    roundDots(t.Media, t.Symbol.BarHeightUM),
		GuardDescentDots: roundDots(t.Media, t.Symbol.GuardDescentUM),
		HRIHeightDots:    roundDots(t.Media, t.Symbol.HRIHeightUM),
	}
}

// roundDots converts a length in micrometres to whole dots, half up.
func roundDots(m domain.Media, length domain.Micrometers) int {
	return int((m.MilliDots(length) + 500) / 1000)
}

// DrawEAN13 draws the symbol at a FRACTIONAL module.
//
// ModuleMilliDots is an integer count of milli-dots: 2344 = 2.344 dots = 0.293 mm at
// 8 dots/mm. It is not a whole number of dots, and no printer language can express
// it, since a module is declared there in whole dots. Only a raster render gets
// there, and that is the arithmetic justification of "raster by default" (A2).
// Every module edge is the rounded IDEAL position (see edge), never an accumulation
// of a rounded step.
//
// Consequences, all covered by tests:
//   - the position error of an edge is bounded by 0.5 dot, with no cumulative drift
//     over the 95 modules;
//   - the width of the bars is round(95 * 2.344) = 223 dots = 27.875 mm, and the
//     over-all width round(113 * 2.344) = 265 dots;
//   - bars alternate between 2 and 3 dots, as does ANY render of this module at this
//     resolution. That is the INTENDED behaviour, not a defect.
//
// WHAT THIS DRAWING DOES NOT CLAIM: being bit-for-bit identical to the sequence the
// "Code EAN13" font rasterized by GDI produces today. Drawing geometrically is a
// deliberate choice -- deterministic, testable pixel by pixel, free of any outline
// rasterizer -- and what settles it is PHYSICAL: 50 production labels against 50 new
// ones at the same checkout scanner, refusals and re-reads counted, which is the
// acceptance criterion of L5 (ADR-019, §7.6).
//
// The HRI is part of the symbol and is ALWAYS drawn: it exists on the current label,
// where the font draws it inside its own descent. DrawEAN13 therefore receives the
// 13 digits and a face; without them it could not draw the line it is credited with.
//
// It draws INK ONLY and never clears the block: the caller owns the white background
// of the label, and rule 5 already guarantees nothing else is placed there.
func DrawEAN13(dst *image.Gray, e domain.EAN13, m [95]bool, o SymbolOptions) error {
	if dst == nil {
		return fmt.Errorf("printing: DrawEAN13: no destination image")
	}
	if err := o.validate(); err != nil {
		return err
	}
	// domain.Modules is what validates the code: it already refuses anything that is
	// not 13 digits, and repeating that here would be a second opinion on the same
	// question.
	//
	// The bars and the HRI must then tell the same story. A caller that computed the
	// modules from ANOTHER code would print a label whose bars say one product and
	// whose digits say another -- the one defect neither the cashier nor the customer
	// can see, and the scanner would win.
	want, err := domain.Modules(e)
	if err != nil {
		return fmt.Errorf("printing: DrawEAN13: %w", err)
	}
	if want != m {
		return fmt.Errorf("printing: DrawEAN13: the 95 modules do not encode %s", string(e))
	}
	for i := 0; i < len(e); i++ {
		if _, ok := digitInk(o.HRIFace, e[i]); !ok {
			return fmt.Errorf("printing: DrawEAN13: the HRI face has no glyph for %q: "+
				"the human-readable line is part of the symbol and cannot be skipped",
				string(e[i]))
		}
	}
	if block := o.Bounds(); !block.In(dst.Bounds()) {
		return fmt.Errorf("printing: DrawEAN13: the block %v does not fit the image %v: "+
			"a clipped symbol loses a quiet zone and stops being readable", block, dst.Bounds())
	}

	left := o.barsLeft()
	black := image.NewUniform(color.Gray{Y: 0x00})
	for i, bar := range m {
		if !bar {
			continue
		}
		h := o.BarHeightDots
		if isGuard(i) {
			h += o.GuardDescentDots
		}
		r := image.Rect(left+o.edge(i), o.YDots, left+o.edge(i+1), o.YDots+h)
		draw.Draw(dst, r, black, image.Point{}, draw.Src)
	}
	drawHRI(dst, e, o)
	return nil
}

// validate reports the geometry DrawEAN13 cannot draw. Each condition is a template
// that would print something unreadable rather than a programming slip.
func (o SymbolOptions) validate() error {
	switch {
	case o.ModuleMilliDots <= 0:
		return fmt.Errorf("printing: DrawEAN13: module %d milli-dots: a module has a width",
			o.ModuleMilliDots)
	case o.BarHeightDots <= 0:
		return fmt.Errorf("printing: DrawEAN13: bar height %d dots: bars have a height",
			o.BarHeightDots)
	case o.GuardDescentDots < 0:
		return fmt.Errorf("printing: DrawEAN13: guard descent %d dots: guards run lower, never higher",
			o.GuardDescentDots)
	case o.HRIHeightDots <= 0:
		// Not an oversight to tolerate: dropping the HRI would be a departure from
		// A1, since it has been printed since forever, and it is the cashier's
		// fallback when the scanner refuses (§7.4, important-5).
		return fmt.Errorf("printing: DrawEAN13: HRI band %d dots: the HRI is never optional",
			o.HRIHeightDots)
	case o.HRIFace == nil:
		return fmt.Errorf("printing: DrawEAN13: no HRI face: the HRI is part of the symbol " +
			"and is always drawn")
	}
	return nil
}

// drawHRI draws the human-readable line: one digit LEFT of the symbol inside the
// left quiet zone, six under the left group, six under the right group -- exactly
// the layout the "Code EAN13" font produces today, where each glyph carries seven
// modules of bars AND the digit that reads them.
//
// The baseline sits on the BOTTOM OF THE BLOCK, which is bar_height +
// max(guard_descent, hri_height). Whenever the HRI band is no deeper than the guard
// descent -- the normative case of §7.4 -- that line IS the bottom of the guard
// bars. When it is deeper, which is the shipped case at 23 dots against 12, the HRI
// is what commands the bottom of the block, and aligning on the guards instead would
// leave the lowest 11 dots of the declared block empty.
//
// The digits are drawn on a scratch band and THRESHOLDED before they reach the
// label. That is what makes "the symbol is drawn in pure black and white" true, so
// the differentiated threshold of §7.3 cannot dither the line, and it makes the
// golden a statement about geometry rather than about coverage values.
func drawHRI(dst *image.Gray, e domain.EAN13, o SymbolOptions) {
	band := image.Rect(o.XDots, o.YDots+o.BarHeightDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.HeightDots())
	scratch := image.NewGray(band)
	draw.Draw(scratch, band, image.NewUniform(color.Gray{Y: 0xFF}), image.Point{}, draw.Src)

	baseline := o.YDots + o.HeightDots()
	left := o.barsLeft()

	// The leading digit draws no bar of its own -- it is carried by the parity of the
	// left group -- so it is set OUTSIDE the symbol, one module clear of the left
	// guard, inside the quiet zone. The zone stays quiet where it matters: the
	// scanner reads the bar band, and this digit is below it.
	drawDigitRightAlignedAt(scratch, o.HRIFace, e[0], left-o.edge(1), baseline)
	for k := 0; k < 6; k++ {
		drawDigitInCell(scratch, o.HRIFace, e[1+k], o, left, leftGroupFirst+k*digitModules, baseline)
		drawDigitInCell(scratch, o.HRIFace, e[7+k], o, left, rightGroupFirst+k*digitModules, baseline)
	}

	for y := band.Min.Y; y < band.Max.Y; y++ {
		for x := band.Min.X; x < band.Max.X; x++ {
			if scratch.GrayAt(x, y).Y < 0x80 {
				dst.SetGray(x, y, color.Gray{Y: 0x00})
			}
		}
	}
}

// drawDigitInCell centres a digit under the seven modules that encode it.
//
// The glyph is placed by its INK and not by its advance, so that a digit sits where
// a reader sees it rather than where its side bearings happen to fall.
func drawDigitInCell(dst *image.Gray, face font.Face, digit byte, o SymbolOptions, left, module, baseline int) {
	cellLeft := left + o.edge(module)
	cellRight := left + o.edge(module+digitModules)
	bounds, _ := digitInk(face, digit)
	inkWidth := bounds.Max.X - bounds.Min.X
	drawDigit(dst, face, digit, fixed.I(cellLeft)+(fixed.I(cellRight-cellLeft)-inkWidth)/2, baseline)
}

// drawDigitRightAlignedAt sets a digit so that its ink ENDS at x.
func drawDigitRightAlignedAt(dst *image.Gray, face font.Face, digit byte, x, baseline int) {
	bounds, _ := digitInk(face, digit)
	drawDigit(dst, face, digit, fixed.I(x)-(bounds.Max.X-bounds.Min.X), baseline)
}

// drawDigit sets one digit with its ink starting at inkLeft and sitting on baseline.
//
// The glyph is known to exist: DrawEAN13 checks the face carries the thirteen digits
// before any of them is drawn, which is the one place that question is asked.
func drawDigit(dst *image.Gray, face font.Face, digit byte, inkLeft fixed.Int26_6, baseline int) {
	bounds, _ := digitInk(face, digit)
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.Gray{Y: 0x00}),
		Face: face,
		Dot:  fixed.Point26_6{X: inkLeft - bounds.Min.X, Y: fixed.I(baseline)},
	}
	d.DrawString(string(digit))
}

// digitInk reports the ink box of one digit, relative to the dot.
func digitInk(face font.Face, digit byte) (fixed.Rectangle26_6, bool) {
	bounds, _, ok := face.GlyphBounds(rune(digit))
	return bounds, ok
}

// FitHRIFace returns the largest face of family whose digits fit the HRI layout of a
// symbol drawn with o, and the type size it settled on, in micrometres.
//
// TWO CONSTRAINTS, AND THE TIGHTER ONE IS NOT THE OBVIOUS ONE. The band is
// HRIHeightDots deep -- 23 at the shipped geometry -- but every digit also has to sit
// under the seven modules that encode it, which is 16 dots. It is the CELL that
// decides, and sizing on the band alone would produce digits that touch.
//
// The engine derives the face rather than reading a size from the template because
// the two quantities that fix it, the module and the HRI band, are already in the
// template: a third number would be a chance to contradict them.
func FitHRIFace(l *Library, family Font, o SymbolOptions, dotsPerMM float64) (font.Face, int, error) {
	if o.ModuleMilliDots <= 0 || o.HRIHeightDots <= 0 {
		return nil, 0, fmt.Errorf("printing: FitHRIFace: module %d milli-dots and HRI band %d dots: "+
			"both must be positive", o.ModuleMilliDots, o.HRIHeightDots)
	}
	maxWidth := o.edge(digitModules) - hriCellClearanceDots
	if maxWidth < 1 {
		return nil, 0, fmt.Errorf("printing: FitHRIFace: a cell of %d dots leaves no room for a digit "+
			"and its clearance", o.edge(digitModules))
	}

	// The bounds of the search, in micrometres of em. The floor is the one hard rule 9
	// imposes on every other field of the label, and there is no reason for the HRI to
	// escape it: a template whose module leaves no room for a legible line must be
	// refused, not silently given microscopic digits nobody can fall back on. The
	// ceiling is twice the body of the whole label.
	const smallest, largest = domain.MinFontSizeUM, 20_000

	fits := func(sizeUM int) (bool, error) {
		face, err := l.Face(family, sizeUM, dotsPerMM, false)
		if err != nil {
			return false, err
		}
		for digit := byte('0'); digit <= '9'; digit++ {
			bounds, ok := digitInk(face, digit)
			if !ok {
				return false, fmt.Errorf("printing: FitHRIFace: font %s has no glyph for %q",
					family, string(digit))
			}
			if ceilDots(bounds.Max.X-bounds.Min.X) > maxWidth ||
				ceilDots(bounds.Max.Y-bounds.Min.Y) > o.HRIHeightDots {
				return false, nil
			}
		}
		return true, nil
	}

	ok, err := fits(smallest)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, fmt.Errorf("printing: FitHRIFace: a cell of %d dots and a band of %d dots "+
			"leave no room for a %s digit at the %d um legibility floor",
			maxWidth, o.HRIHeightDots, family, smallest)
	}
	// The largest fitting size in [smallest, largest]. The invariant is that lo always
	// fits, so the search cannot return a size the caller must check again.
	lo, hi := smallest, largest
	for lo < hi {
		mid := (lo + hi + 1) / 2
		ok, err := fits(mid)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	face, err := l.Face(family, lo, dotsPerMM, false)
	if err != nil {
		return nil, 0, err
	}
	return face, lo, nil
}

// ceilDots rounds a 26.6 fixed-point length UP to whole dots. Up, because a fit is
// only a fit when the last covered dot is inside.
func ceilDots(v fixed.Int26_6) int { return int((v + 63) / 64) }
