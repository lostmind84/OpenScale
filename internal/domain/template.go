package domain

import "fmt"

// This file holds the GEOMETRY of a label layout: the closed lists a template may
// name, the head its lengths are counted against, the media, the symbol, the
// elements -- and the arithmetic that says how far the ink reaches.
//
// Every length is in micrometres, except the barcode module, which is in milli-dots
// and says why at SymbolGeometry. What REFUSES a layout is template_validate.go.

// The CLOSED list of field identifiers a template may place.
//
// Closed, and that is the point of rule 7: there is no template engine, therefore
// no injection and no rendering error at print time. A template naming a field
// that does not exist is refused when it is LOADED, not when a customer is waiting.
const (
	FieldProductName         = "product_name"
	FieldQuantity            = "quantity"
	FieldPrimaryUnitPrice    = "primary_unit_price"
	FieldSecondaryTotalPrice = "secondary_total_price"
	FieldPrimaryTotalPrice   = "primary_total_price"
	FieldBarcode             = "barcode"
)

// knownFields is the closed list itself, in printing order.
var knownFields = []string{
	FieldProductName, FieldQuantity, FieldPrimaryUnitPrice,
	FieldSecondaryTotalPrice, FieldPrimaryTotalPrice, FieldBarcode,
}

// Conditions under which an element is drawn. The empty condition means always.
const (
	// WhenAlways is the empty string: the element is always drawn.
	WhenAlways = ""
	// WhenMultiTier draws the element only when the grid holds more than one tier.
	// It is what makes mono-tarif work with NO conditional code in the renderer:
	// the secondary price field simply disappears (§7.2).
	WhenMultiTier = "multi_tier"
)

var knownConditions = []string{WhenAlways, WhenMultiTier}

// Text alignment inside an element box.
const (
	AlignLeft  = "left"
	AlignRight = "right"
)

var knownAlignments = []string{AlignLeft, AlignRight}

// Geometry bounds of rule 9, and the reason each one exists.
const (
	// GS1MinModuleUM and GS1MaxModuleUM bound the barcode module: they are the
	// X-dimension the GS1 General Specifications allow an EAN-13 at general retail
	// POS, 80 % to 200 % of the 0.330 mm nominal.
	//
	// They replaced a [1500, 6000] MILLI-DOT pair on 30/07/2026. That pair had no
	// origin, and being written in units of resolution it meant two different things
	// on two heads: 0.1875-0.750 mm at 8 dots/mm, 0.125-0.500 mm at 12. It therefore
	// accepted a module below the GS1 floor on one head and refused a conforming one
	// on the other. The module is a physical length; the rule that bounds it has to
	// be physical too, which means reading it against the pitch the HEAD declares
	// (ADR-045).
	GS1MinModuleUM = 264
	GS1MaxModuleUM = 660
	// MinFontSizeUM is a HARD floor, 14.4 dots at 8 dots/mm. It is not an
	// "em >= 20 dots" invariant -- there is none, and the shipped template goes
	// down to 19.8 dots on the secondary price (§7.2, mineur-1).
	MinFontSizeUM = 1800
)

// The INKED GEOMETRY OF THE PRODUCTION LABEL, in dots at 8 dots/mm.
//
// This is the reference of rules 3 and 4, and choosing it rather than the declared
// media is a decision, not an approximation. Comparing a template to
// media.height_um would mean knowing the media, therefore measuring the roll
// installed, therefore going on site before L4 could be delivered. But the
// production path sends a raster render to a Windows print queue that is ALREADY
// installed and ALREADY calibrated, and which knows its own media: the physical
// geometry has been solved by the driver for years, and the proof that it is solved
// correctly comes out of the printer every day.
//
// What actually constrains a template is the space the ink occupies on the label we
// are reproducing, and that is known to the hundredth of a millimetre from the test
// PDF: 35.11 x 25.23 mm. We compare INK TO INK, two quantities measured on the same
// reference document, and no delivery waits for a pair of calipers.
//
// # WHAT THE BENCH OF 28/07/2026 CHANGED
//
// The height came from that test PDF — 25.23 mm, 202 dots — and the PDF turned out
// not to be evidence: it was never produced by the printer's driver. The stock is
// 38 × 25 mm under a caliper, and the driver of the parc holds 35 × 25 mm of printable
// area. The printer is the authority, so the inked height is the paper: 200 dots.
//
// The two bounds now coincide with the media on both axes, and that is the honest
// reading rather than a lost distinction — ink may not exceed paper, and nothing else
// was ever measured well enough to say otherwise.
//
// # WHOSE FIGURES THESE ARE
//
// The WS408's. A print head declares its own inked geometry (PrinterCapabilities), and
// what rules 3 and 4 read is that declaration; these three constants are the REFERENCE
// a caller with no head in hand falls back on — Validate, MaxOffsetDots, and any driver
// that inks nothing. The parc is a WS408, so refusing to check a template for want of
// a descriptor would refuse every template this binary ships.
const (
	ReferenceInkedWidthDots  = 280
	ReferenceInkedHeightDots = 200
	// ReferenceDotsPerMM is the pitch the two figures above are counted at. It is what
	// makes them comparable to a template at all.
	ReferenceDotsPerMM = 8
)

// ReferenceHead is the print head of the parc: a SATO WS408, 8 dots/mm, 280 × 200 dots
// of ink.
//
// It is what every rule bears on when no driver has said otherwise, and it is a
// FALLBACK and not an authority: a station that declares its own head is measured
// against that one.
func ReferenceHead() PrinterCapabilities {
	return PrinterCapabilities{
		DotsPerMM:       ReferenceDotsPerMM,
		InkedWidthDots:  ReferenceInkedWidthDots,
		InkedHeightDots: ReferenceInkedHeightDots,
	}
}

// orReference fills in every figure a head left unsaid with the one of the parc.
//
// Unsaid and not "unlimited": a driver that inks nothing constrains nothing of its own,
// but the label it is previewing is still the label of the parc, and that is what the
// volunteer is adjusting against.
func (c PrinterCapabilities) orReference() PrinterCapabilities {
	reference := ReferenceHead()
	if c.DotsPerMM <= 0 {
		c.DotsPerMM = reference.DotsPerMM
	}
	if c.InkedWidthDots <= 0 {
		c.InkedWidthDots = reference.InkedWidthDots
	}
	if c.InkedHeightDots <= 0 {
		c.InkedHeightDots = reference.InkedHeightDots
	}
	return c
}

// Media describes the label stock, for the life-size preview and for nothing else.
//
// No validation rule depends on its exact value (see ReferenceInkedWidthDots): it
// aligns the preview, it validates nothing.
type Media struct {
	WidthUM  Micrometers `json:"width_um"`
	HeightUM Micrometers `json:"height_um"`
	// DotsPerMM is the SINGLE SOURCE of resolution in the whole application
	// (mineur-3): barcode.resolution_dpi is gone. 8 on a WS408, 12 on a WS412.
	//
	// It is also, and this is what makes it load-bearing, THE HEAD THIS TEMPLATE WAS
	// MEASURED FOR. Every other length of a layout is in micrometres and means the same
	// thing on any printer; SymbolGeometry.ModuleMilliDots alone is expressed in units
	// of RESOLUTION — deliberately, 0.293 mm being 2.344 dots, and that fractional
	// module is the whole technical point of arbitration A2. The unit stays, so the
	// AMBIGUITY has to go: a template that did not say which head it was measured for
	// would change magnification in SILENCE, the same 2 344 milli-dots printing 0.195 mm
	// on a 12 dots/mm head — under every GS1 floor, with no byte of the frame saying so.
	// ValidateOn refuses that pairing by name rather than letting the label come out
	// wrong.
	DotsPerMM float64 `json:"dots_per_mm"`
}

// MilliDots converts a length in micrometres to whole milli-dots.
//
// The conversion is a plain multiplication, and pleasantly so: a micrometre is a
// thousandth of a millimetre, so um x dots/mm is already thousandths of a dot.
// 34 978 um x 8 dots/mm = 279 824 milli-dots. The engine works in whole milli-dots
// throughout (§0), which is what keeps rules 3 and 4 free of any rounding argument.
func (m Media) MilliDots(length Micrometers) int64 {
	return int64(float64(length) * m.DotsPerMM)
}

// SymbolGeometry is where and how big the EAN-13 symbol is drawn.
//
// Every length is in micrometres in the file; the module alone is in milli-dots,
// because it is the one quantity that is NOT a round number of micrometres at this
// resolution: 0.293 mm is 2.344 dots, and that fractional module is the whole
// technical point of arbitration A2.
type SymbolGeometry struct {
	XUM Micrometers `json:"x_um"`
	YUM Micrometers `json:"y_um"`
	// ModuleMilliDots is 2344 in weighing_identical: 2.344 dots = 0.293 mm.
	ModuleMilliDots int         `json:"module_milli_dots"`
	BarHeightUM     Micrometers `json:"bar_height_um"`
	GuardDescentUM  Micrometers `json:"guard_descent_um"`
	// HRIHeightUM is NEVER zero: the human-readable line exists on the current
	// label, drawn by the "Code EAN13" font inside its own descent, and removing it
	// would be a departure from A1. The cashier keeps her fallback when the scanner
	// refuses.
	HRIHeightUM Micrometers `json:"hri_height_um"`
}

// TotalWidthMilliDots is the over-all width of the symbol, quiet zones included:
// 113 modules -- 11 left, 95 bars, 7 right.
func (s SymbolGeometry) TotalWidthMilliDots() int64 {
	return int64(113 * s.ModuleMilliDots)
}

// ModuleUM is the X-dimension in micrometres, read at the pitch of a given head.
//
// This is the ONE place where a template's one resolution-relative length becomes a
// physical one, and it takes the head as an argument rather than the template's own
// media because that is the whole point of ADR-045: 2 344 milli-dots are 293 um on a
// WS408 and 195 um on a WS412, and only the head knows which.
func (s SymbolGeometry) ModuleUM(dotsPerMM float64) Micrometers {
	if dotsPerMM <= 0 {
		return 0
	}
	return Micrometers(float64(s.ModuleMilliDots)/dotsPerMM + 0.5)
}

// Magnification is the module as a fraction of the 0.330 mm GS1 nominal (SC2, 100 %).
func (s SymbolGeometry) Magnification(dotsPerMM float64) float64 {
	return float64(s.ModuleUM(dotsPerMM)) / 330.0
}

// HeightUM is where the symbol block ENDS, and there is only ONE definition of it.
//
//	symbol_height = bar_height + max(guard_descent, hri_height)
//
// The HRI digits sit INSIDE the guard descent band -- their baseline is aligned on
// the bottom of the guard bars, as on a standard EAN-13 and as the current font
// does. The height is therefore NOT the sum bars + descent + HRI: that additive
// reading gives 16 115 um for this geometry against 14 650 um for the aligned one,
// which is two different answers to the same blocking validation rule. max() is
// what holds.
func (s SymbolGeometry) HeightUM() Micrometers {
	descent := s.GuardDescentUM
	if s.HRIHeightUM > descent {
		descent = s.HRIHeightUM
	}
	return s.BarHeightUM + descent
}

// Element is one field placed on the label.
type Element struct {
	Field    string      `json:"field"`
	XUM      Micrometers `json:"x_um"`
	YUM      Micrometers `json:"y_um"`
	WidthUM  Micrometers `json:"width_um"`
	HeightUM Micrometers `json:"height_um"`
	// FontSizeUM is the type size: 3175 um is 9 pt, 3881 um is 11 pt.
	FontSizeUM Micrometers `json:"font_size_um"`
	// MinFontSizeUM is how far the automatic reduction may go before the text is
	// truncated with an ellipsis and a technical anomaly is journalled.
	MinFontSizeUM Micrometers `json:"min_font_size_um"`
	Bold          bool        `json:"bold"`
	// AutoBold, when false, opts OUT of the automatic switch to bold below 20 dots
	// of em. weighing_identical sets it false on the secondary price: the source
	// carries no FontWeight there, so the solidarity price prints in REGULAR, and
	// making it bold would be the one visible departure from A1.
	AutoBold bool   `json:"auto_bold"`
	Framed   bool   `json:"framed"`
	Align    string `json:"align"`
	When     string `json:"when"`
}

// Right and Bottom report the far edges of the element box.
func (e Element) Right() Micrometers  { return e.XUM + e.WidthUM }
func (e Element) Bottom() Micrometers { return e.YUM + e.HeightUM }

// Active reports whether this element is drawn for a grid of tierCount tiers.
func (e Element) Active(tierCount int) bool {
	return e.When != WhenMultiTier || tierCount > 1
}

// Template is a label layout: the media it assumes, the printable area, the text
// elements and the symbol.
type Template struct {
	Name  string `json:"name"`
	Media Media  `json:"media"`
	// PrintableWidthUM and PrintableHeightUM are the area the printer will accept
	// ink on, which may be smaller than the media.
	PrintableWidthUM  Micrometers    `json:"printable_width_um"`
	PrintableHeightUM Micrometers    `json:"printable_height_um"`
	Elements          []Element      `json:"elements"`
	Symbol            SymbolGeometry `json:"symbol"`
	// OffsetXDots and OffsetYDots are the volunteer's ±1 dot adjustment, applied
	// BEFORE validation (rule 6).
	OffsetXDots int `json:"offset_x_dots"`
	OffsetYDots int `json:"offset_y_dots"`
	// TextThreshold is the differentiated binarisation threshold for text; the
	// symbol uses 0x80 and is insensitive to it.
	TextThreshold uint8 `json:"text_threshold"`
	// TruncationAccepted turns the symbol diagnostic from an amber WARNING into an
	// informative note. weighing_identical sets it: the truncation is a documented
	// decision of the commissioning party (ADR-003), and this flag is what stops a
	// contributor from "fixing" it out of zeal in six months.
	TruncationAccepted bool `json:"truncation_accepted"`
}

// MaxOffsetDots reports how far the volunteer's adjustment may go in each
// direction before rule 3 or rule 5 would refuse it.
//
// This is what turns "ce décalage rognerait la zone de silence du code-barres" into
// a message that names the admissible maximum instead of just saying no.
func (t *Template) MaxOffsetDots(tierCount int) (x, y int) {
	return t.MaxOffsetDotsOn(ReferenceHead(), tierCount)
}

// MaxOffsetDotsOn reports the same margin ON THIS HEAD.
//
// A margin computed against a constant would refuse, on a taller head, an adjustment
// the printer would have accepted — and the ±1 dot arrows are how a volunteer corrects
// a roll that sits a hair off.
func (t *Template) MaxOffsetDotsOn(head PrinterCapabilities, tierCount int) (x, y int) {
	head = head.orReference()
	bottom, right := t.inkedExtent(tierCount)
	remainingX := (int64(head.InkedWidthDots)*1000 - right) / 1000
	remainingY := (int64(head.InkedHeightDots)*1000 - bottom) / 1000
	if remainingX < 0 {
		remainingX = 0
	}
	if remainingY < 0 {
		remainingY = 0
	}
	return int(remainingX), int(remainingY)
}

// withOffset returns a copy of the template with the operator's shift applied to
// every coordinate. A copy, because validating a mutated receiver would leave the
// loaded template shifted twice on the next call.
func (t *Template) withOffset() *Template {
	if t.OffsetXDots == 0 && t.OffsetYDots == 0 {
		return t
	}
	shifted := *t
	shifted.Elements = make([]Element, len(t.Elements))
	copy(shifted.Elements, t.Elements)

	dx := Micrometers(float64(t.OffsetXDots) * 1000 / t.Media.DotsPerMM)
	dy := Micrometers(float64(t.OffsetYDots) * 1000 / t.Media.DotsPerMM)
	for i := range shifted.Elements {
		shifted.Elements[i].XUM += dx
		shifted.Elements[i].YUM += dy
	}
	shifted.Symbol.XUM += dx
	shifted.Symbol.YUM += dy
	return &shifted
}

// inkedExtent reports how far down and how far right the ink actually goes, in
// milli-dots.
//
// It compares the bottom of every ACTIVE text element with the bottom of the symbol
// block, and the right edge of the widest active element with the right edge of the
// symbol INCLUDING its quiet zones.
func (t *Template) inkedExtent(tierCount int) (bottom, right int64) {
	for _, e := range t.Elements {
		if e.Field == FieldBarcode || !e.Active(tierCount) {
			continue
		}
		if b := t.Media.MilliDots(e.Bottom()); b > bottom {
			bottom = b
		}
		if r := t.Media.MilliDots(e.Right()); r > right {
			right = r
		}
	}
	if b := t.Media.MilliDots(t.Symbol.YUM + t.Symbol.HeightUM()); b > bottom {
		bottom = b
	}
	if r := t.Media.MilliDots(t.Symbol.XUM) + t.Symbol.TotalWidthMilliDots(); r > right {
		right = r
	}
	return bottom, right
}

// rectangle is a box in milli-dots, used by the overlap rules.
type rectangle struct{ left, top, right, bottom int64 }

func (t *Template) box(e Element) rectangle {
	return rectangle{
		left:   t.Media.MilliDots(e.XUM),
		top:    t.Media.MilliDots(e.YUM),
		right:  t.Media.MilliDots(e.Right()),
		bottom: t.Media.MilliDots(e.Bottom()),
	}
}

// symbolBox is the symbol WITH its quiet zones, which is what rule 5 protects.
func (t *Template) symbolBox() rectangle {
	left := t.Media.MilliDots(t.Symbol.XUM)
	return rectangle{
		left:   left,
		top:    t.Media.MilliDots(t.Symbol.YUM),
		right:  left + t.Symbol.TotalWidthMilliDots(),
		bottom: t.Media.MilliDots(t.Symbol.YUM + t.Symbol.HeightUM()),
	}
}

// overlaps reports whether two boxes share any area. Touching edges do not overlap:
// two fields whose boxes abut print side by side, which is exactly how the shipped
// template lays out the quantity and the unit price.
func overlaps(a, b rectangle) bool {
	return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom
}

// formatMilliDots renders a milli-dot count as dots with three decimals, so a
// message says "279,824 dots" rather than "279824".
func formatMilliDots(milliDots int64) string {
	negative := milliDots < 0
	if negative {
		milliDots = -milliDots
	}
	out := fmt.Sprintf("%d,%03d", milliDots/1000, milliDots%1000)
	if negative {
		return "-" + out
	}
	return out
}
