package printing

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"openscale/internal/domain"
)

// THE TYPOGRAPHIC MODEL OF AN ELEMENT BOX, stated once because two files depend on
// it saying the same thing.
//
// An element box is an EM BOX: its height is the type size, the baseline sits
// emAscentPerMille of the em below its top, and the remaining quarter is the
// descender space. That is not a convention invented here -- it is the arithmetic
// domain.IdenticalTemplate() already uses to give the 7 pt and the 11 pt prices a
// SHARED baseline: it places the small one at baseline - 750/1000 x body7. Draw with
// any other ascent and the two slide apart, which is the one thing that template
// says it fixed.
//
// It is deliberately NOT face.Metrics().Ascent. Measured on the embedded file, that
// value is 0.952 em for Carlito, because x/image reports the hhea ascender, which
// carries the LINE SPACING of the font and not its typographic ascent. Using it
// would push every field five dots down its box and pull the two prices apart.
const emAscentPerMille = 750

const (
	// autoBoldBelowDots is the automatic switch to bold of §7.3: any element whose em
	// is under 20 dots is drawn in the bold cut of its family -- unless the template
	// opted out, which weighing_identical does on the solidarity price (§7.2).
	autoBoldBelowDots = 20

	// reductionStepUM is the 0.1 mm step the automatic reduction descends by (§7.3).
	reductionStepUM = domain.Micrometers(100)

	// framePaddingDots keeps a framed field off its own rule: one dot for the rule
	// itself, one of clearance. Without the second one the text of
	// primary_unit_price, which is right aligned, ends welded to the frame it is
	// meant to sit inside -- observed on the first render of the shipped template,
	// where the g of "€/kg" touched the right rule.
	framePaddingDots = 2

	// ellipsis marks a field the reduction could not save. One rune rather than three
	// dots: it says "cut here" in a width no reader mistakes for punctuation.
	ellipsis = "…"
)

// labelFont is the family every field of every template is drawn in.
//
// ADR-020 chose it for the production label because it is metrically Calibri. §7.3
// also announces DejaVu Sans Condensed as "the font of the NEUTRAL templates", but
// domain.Element carries no font key to say so with: the closed list of §7.5 rule 7
// covers fields, conditions and alignments, and nothing names a family. Until an
// element can declare one, every template renders in Carlito -- which is the right
// default, since it is the only one A1 has an opinion about.
const labelFont = Carlito

// fallbackFont draws what Carlito cannot, and that is not a theoretical case.
//
// MEASURED on the authentic catalog: Carlito has no glyph for U+2665, and 127 of the
// 355 product names of testdata/catalog/flv.csv carry one. Without this fallback a
// third of the shelf would print with a hole in its name -- silently, because
// font.Drawer skips a glyph it does not have without advancing the pen.
const fallbackFont = DejaVuSansCondensed

// words are the label's own words in one language (§7.3 prints them, §10.2 supplies
// the price suffix, and neither is a template constant).
type words struct {
	kilogram string
	unit     string // singular: "1 unité"
	units    string // plural: "3 unités"
	euro     string
}

// localeWords is the whole of V1's internationalisation: one language, spelled out
// rather than scattered through the drawing code.
var localeWords = map[domain.Locale]words{
	domain.LocaleFrench: {kilogram: "kg", unit: "unité", units: "unités", euro: "€"},
}

// wordsFor reports the words of a locale, and whether that locale is one the binary
// knows.
//
// The empty locale IS French: a PrintJob whose Locale was never filled must print a
// label, not a fault.
func wordsFor(loc domain.Locale) (words, bool) {
	if loc == "" {
		return localeWords[domain.LocaleFrench], true
	}
	w, ok := localeWords[loc]
	if !ok {
		return localeWords[domain.LocaleFrench], false
	}
	return w, true
}

// fieldText is WHAT one field of the label carries (A7).
//
// The tier prefix is the Abbrev of the tier followed by ": " -- exactly
// "A: 4,79 €/kg", "S: 6,58 €", "A: 5,92 €". The "€/kg" is NOT a constant of the
// template: it is the PriceSuffix of the product, which comes from the `unite`
// column of the CSV and is one of " €/kg", " € le litre" or " € l'unité" (§10.2).
// Hard-coding it here would let the label contradict the catalog.
func fieldText(field string, label domain.Label, w words) (string, error) {
	switch field {
	case domain.FieldProductName:
		return label.Product.Name, nil

	case domain.FieldQuantity:
		return quantityText(label, w)

	case domain.FieldPrimaryUnitPrice:
		line, err := primaryLine(label)
		if err != nil {
			return "", err
		}
		// The DERIVED unit price, the one Price computed for this tier -- never the
		// catalog price. Printing the base price beside a discounted amount would give
		// a customer two numbers that do not multiply out (§6.3).
		return tierPrefix(line.Tier) + line.UnitPrice.Euro() + label.Product.PriceSuffix, nil

	case domain.FieldPrimaryTotalPrice:
		line, err := primaryLine(label)
		if err != nil {
			return "", err
		}
		return tierPrefix(line.Tier) + line.Amount.Euro() + " " + w.euro, nil

	case domain.FieldSecondaryTotalPrice:
		line := secondaryLine(label)
		if line == nil {
			// Unreachable through Rasterize, which skips the field when the grid holds
			// one tier (Element.Active). Reached only by a template that placed the
			// field with no condition, and then the fault belongs to the template.
			return "", fmt.Errorf("printing: le champ %q est placé alors que la grille "+
				"n'a qu'un seul tarif", field)
		}
		return tierPrefix(line.Tier) + line.Amount.Euro() + " " + w.euro, nil
	}
	return "", fmt.Errorf("printing: champ inconnu %q", field)
}

// quantityText is the weight or the count, in the words of the locale.
//
// The legacy report stripped a leading zero from "00,250" to print "0,250 kg";
// Grams.Kilos never pads the kilogram digits in the first place, so there is nothing
// to strip and no place for the arithmetic that did it.
func quantityText(label domain.Label, w words) (string, error) {
	switch label.Mode {
	case domain.ByWeight:
		return label.NetWeight.Kilos() + " " + w.kilogram, nil
	case domain.ByUnit:
		unit := w.units
		if label.Quantity == 1 {
			unit = w.unit
		}
		return strconv.Itoa(label.Quantity) + " " + unit, nil
	}
	return "", fmt.Errorf("printing: mode de vente inconnu %d", label.Mode)
}

// tierPrefix is the "A: " of the printed price, and the empty string in mono-tarif.
//
// A single-tier grid has an empty Abbrev, and a bare ": " in front of a price would
// be a punctuation mark with nothing to introduce.
func tierPrefix(t domain.PriceTier) string {
	if t.Abbrev == "" {
		return ""
	}
	return t.Abbrev + ": "
}

// primaryLine is the big price, and it refuses to invent one.
func primaryLine(label domain.Label) (domain.PriceLine, error) {
	if label.PrimaryLine == nil {
		return domain.PriceLine{}, fmt.Errorf("printing: l'étiquette ne porte aucun tarif principal")
	}
	return *label.PrimaryLine, nil
}

// secondaryLine is the small price, or nil when the grid has a single tier.
//
// The Label carries the lines and the primary one, not the list of secondary codes,
// and a template places exactly ONE secondary field. So it is the first line that is
// not the primary, in the rank order Price appended them in -- which for the shipped
// grid is the solidarity price, exactly as §7.2 prints it.
func secondaryLine(label domain.Label) *domain.PriceLine {
	if label.PrimaryLine == nil {
		return nil
	}
	for i := range label.Lines {
		if label.Lines[i].Tier.Code != label.PrimaryLine.Tier.Code {
			return &label.Lines[i]
		}
	}
	return nil
}

// elementBox is where one field lives, in whole dots, with the operator's ±1 dot
// adjustment applied.
//
// Every edge is rounded ON ITS OWN, and not as "rounded position plus rounded
// width": two boxes that abut in micrometres must still abut in dots, which is how
// the quantity and the unit price of the shipped template share a line.
func elementBox(g *domain.Template, e domain.Element) image.Rectangle {
	return image.Rect(
		roundDots(g.Media, e.XUM)+g.OffsetXDots,
		roundDots(g.Media, e.YUM)+g.OffsetYDots,
		roundDots(g.Media, e.Right())+g.OffsetXDots,
		roundDots(g.Media, e.Bottom())+g.OffsetYDots,
	)
}

// textBox is the room the glyphs actually get: the element box, minus the rule when
// the element is framed.
func textBox(g *domain.Template, e domain.Element) image.Rectangle {
	box := elementBox(g, e)
	if !e.Framed {
		return box
	}
	return image.Rect(box.Min.X+framePaddingDots, box.Min.Y,
		box.Max.X-framePaddingDots, box.Max.Y)
}

// baselineDots is the dot row the glyphs of an element sit on.
//
// It is derived from the NOMINAL type size and never from the reduced one: a field
// the automatic reduction shrank must stay on the line it shares with its
// neighbours, or reducing one price would visibly drop it below the other.
func baselineDots(g *domain.Template, e domain.Element) int {
	return roundDots(g.Media, e.YUM+e.FontSizeUM*emAscentPerMille/1000) + g.OffsetYDots
}

// isBold decides the weight one element is drawn in at a given size (§7.3).
//
// The size is a PARAMETER and not e.FontSizeUM, because the automatic reduction can
// carry an element below the 20 dot mark: the weight is then decided again at the
// size that will really be drawn, and the width is measured in that weight. A loop
// that chose the weight once, at the nominal size, would measure regular and print
// bold.
func isBold(m domain.Media, e domain.Element, sizeUM domain.Micrometers) bool {
	if e.Bold {
		return true
	}
	return e.AutoBold && m.MilliDots(sizeUM) < autoBoldBelowDots*1000
}

// reductionFloor is how small the automatic reduction may take one element.
//
// The element states its own floor; hard rule 9 states the floor of every element.
// The smaller of the two never wins.
func reductionFloor(e domain.Element) domain.Micrometers {
	floor := e.MinFontSizeUM
	if floor < domain.MinFontSizeUM {
		floor = domain.MinFontSizeUM
	}
	if floor > e.FontSizeUM {
		floor = e.FontSizeUM
	}
	return floor
}

// textRun is a maximal stretch of text one face can draw.
type textRun struct {
	face font.Face
	text string
}

// placement is everything drawing one field needs, once the automatic reduction has
// had its say.
type placement struct {
	runs      []textRun
	width     fixed.Int26_6
	sizeUM    domain.Micrometers
	bold      bool
	text      string
	truncated bool
	missing   []rune
}

// splitRuns cuts s into the stretches primary can draw and the stretches fallback
// has to, and reports the runes NEITHER carries.
//
// A string every rune of which is in primary comes back as ONE run, and that is not
// an optimisation: measured as one string it is measured WITH its kerning, by the
// very font.MeasureString the acceptance criterion of ADR-020 compares against. Cut
// into runes it would be measured unkerned, and the criterion would start failing on
// a difference the font is not responsible for (see metrics_test.go).
func splitRuns(s string, primary, fallback font.Face) (runs []textRun, missing []rune) {
	var pending strings.Builder
	var current font.Face
	flush := func() {
		if pending.Len() > 0 {
			runs = append(runs, textRun{face: current, text: pending.String()})
			pending.Reset()
		}
	}
	for _, r := range s {
		var face font.Face
		switch {
		case hasGlyph(primary, r):
			face = primary
		case hasGlyph(fallback, r):
			face = fallback
		default:
			flush()
			current = nil
			missing = append(missing, r)
			continue
		}
		if face != current {
			flush()
			current = face
		}
		pending.WriteRune(r)
	}
	flush()
	return runs, missing
}

// hasGlyph reports whether a face can draw a rune.
func hasGlyph(f font.Face, r rune) bool {
	if f == nil {
		return false
	}
	_, ok := f.GlyphAdvance(r)
	return ok
}

// runsWidth is the advance of a whole field, faces included.
//
// The advance and not the ink box: right alignment on the ink would move a field's
// end position by the side bearing of whatever character happens to be last, and it
// is the advance that ADR-020 pins to within 1 % of Calibri.
func runsWidth(runs []textRun) fixed.Int26_6 {
	var total fixed.Int26_6
	for _, r := range runs {
		total += font.MeasureString(r.face, r.text)
	}
	return total
}

// drawRuns sets a field with its pen starting at penX, on baseline.
func drawRuns(dst *image.Gray, runs []textRun, penX fixed.Int26_6, baseline int) {
	black := image.NewUniform(color.Gray{Y: 0x00})
	for _, r := range runs {
		d := font.Drawer{
			Dst:  dst,
			Src:  black,
			Face: r.face,
			Dot:  fixed.Point26_6{X: penX, Y: fixed.I(baseline)},
		}
		d.DrawString(r.text)
		penX = d.Dot.X
	}
}
