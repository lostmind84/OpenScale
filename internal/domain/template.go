package domain

import "fmt"

// Fault is a single validation error, named by the field that carries it.
//
// Validation returns ALL the faults, not the first one: the admin screen is used
// by volunteers, it must report everything at once, in French, with the offending
// field named and, whenever possible, the list of acceptable values.
type Fault struct {
	Field   string   `json:"field"`
	Message string   `json:"message"`
	Values  []string `json:"values,omitempty"`
}

func (f Fault) String() string { return f.Field + " : " + f.Message }

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
	// MinModuleMilliDots and MaxModuleMilliDots bound the barcode module. Below
	// 1500 no scanner reads it; above 6000 nothing fits on a 40 mm label.
	MinModuleMilliDots = 1500
	MaxModuleMilliDots = 6000
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
const (
	InkedWidthDots  = 280
	InkedHeightDots = 202
)

// Media describes the label stock, for the life-size preview and for nothing else.
//
// No validation rule depends on its exact value (see InkedWidthDots): it aligns the
// preview, it validates nothing.
type Media struct {
	WidthUM  Micrometers `json:"width_um"`
	HeightUM Micrometers `json:"height_um"`
	// DotsPerMM is the SINGLE SOURCE of resolution in the whole application
	// (mineur-3): barcode.resolution_dpi is gone. 8 on a WS408, 12 on a WS412.
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

// Validate reports every hard rule the template breaks; an empty slice means the
// template may be loaded.
//
// tierCount decides which conditional elements are active, because rules 3, 5 and 8
// are about what is actually INKED: a mono-tarif station must not be refused a
// template because of a field that will never be drawn on it.
func (t *Template) Validate(tierCount int) []Fault {
	var faults []Fault
	fail := func(field, format string, args ...any) {
		faults = append(faults, Fault{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if t.Media.DotsPerMM <= 0 {
		fail("media.dots_per_mm", "doit être strictement positif (8 sur une WS408, 12 sur une WS412)")
		// Every geometric rule below divides the world by this number. Carrying on
		// would produce a page of faults that all say the same thing.
		return faults
	}

	// Rule 6: the offset is applied BEFORE validation, so a shift that would push a
	// quiet zone off the label is refused rather than silently cropped. The ±1 dot
	// arrows of the admin screen invite that adjustment; it must be bounded by the
	// geometry and not merely by ±99.
	shifted := t.withOffset()

	inkedWidth := int64(InkedWidthDots) * 1000
	inkedHeight := int64(InkedHeightDots) * 1000

	// Rule 9, checked first: a template with an absurd module or an unreadable type
	// size produces cascades of geometric faults, and naming the cause is kinder
	// than naming ten consequences.
	if s := shifted.Symbol.ModuleMilliDots; s < MinModuleMilliDots || s > MaxModuleMilliDots {
		fail("symbol.module_milli_dots", "%d hors bornes [%d, %d] : en deçà aucune douchette ne lit le symbole, au-delà il ne tient plus sur 40 mm",
			s, MinModuleMilliDots, MaxModuleMilliDots)
	}
	for i, e := range shifted.Elements {
		if e.Field == FieldBarcode {
			continue // the symbol carries no type size of its own
		}
		if e.FontSizeUM < MinFontSizeUM {
			fail(fmt.Sprintf("elements[%d].font_size_um", i),
				"%d µm est sous le plancher de lisibilité de %d µm (%s)",
				e.FontSizeUM, MinFontSizeUM, e.Field)
		}
		if e.MinFontSizeUM != 0 && e.MinFontSizeUM < MinFontSizeUM {
			fail(fmt.Sprintf("elements[%d].min_font_size_um", i),
				"%d µm est sous le plancher de %d µm", e.MinFontSizeUM, MinFontSizeUM)
		}
		if e.MinFontSizeUM > e.FontSizeUM {
			fail(fmt.Sprintf("elements[%d].min_font_size_um", i),
				"%d µm dépasse le corps nominal de %d µm", e.MinFontSizeUM, e.FontSizeUM)
		}
	}

	// Rule 7: closed lists. No template engine, so no injection and no rendering
	// error at print time.
	seen := make(map[string]bool, len(shifted.Elements))
	for i, e := range shifted.Elements {
		if !known(knownFields, e.Field) {
			fail(fmt.Sprintf("elements[%d].field", i), "champ inconnu %q", e.Field)
			faults[len(faults)-1].Values = knownFields
		}
		if !known(knownConditions, e.When) {
			fail(fmt.Sprintf("elements[%d].when", i), "condition inconnue %q", e.When)
			faults[len(faults)-1].Values = knownConditions
		}
		if e.Field != FieldBarcode && !known(knownAlignments, e.Align) {
			fail(fmt.Sprintf("elements[%d].align", i), "alignement inconnu %q", e.Align)
			faults[len(faults)-1].Values = knownAlignments
		}
		if seen[e.Field] {
			fail(fmt.Sprintf("elements[%d].field", i), "le champ %q est placé deux fois", e.Field)
		}
		seen[e.Field] = true
		if e.WidthUM <= 0 || e.HeightUM <= 0 {
			fail(fmt.Sprintf("elements[%d]", i), "le champ %q a une boîte de %d × %d µm",
				e.Field, e.WidthUM, e.HeightUM)
		}
	}

	// Rules 1 and 2: boxes inside the printable area, printable area inside the
	// media. Both bear on the DECLARED geometry, which is the operator's own
	// statement about their printer -- unlike rules 3 and 4, which bear on measured
	// ink.
	if t.PrintableWidthUM > t.Media.WidthUM || t.PrintableHeightUM > t.Media.HeightUM {
		fail("printable_area", "la zone imprimable (%d × %d µm) sort du média (%d × %d µm)",
			t.PrintableWidthUM, t.PrintableHeightUM, t.Media.WidthUM, t.Media.HeightUM)
	}
	for i, e := range shifted.Elements {
		if !e.Active(tierCount) {
			continue
		}
		if e.XUM < 0 || e.YUM < 0 {
			fail(fmt.Sprintf("elements[%d]", i), "le champ %q est placé en (%d ; %d) µm, hors de l'étiquette",
				e.Field, e.XUM, e.YUM)
			continue
		}
		if t.PrintableWidthUM > 0 && e.Right() > t.PrintableWidthUM {
			fail(fmt.Sprintf("elements[%d]", i), "le champ %q dépasse la zone imprimable en largeur (%d > %d µm)",
				e.Field, e.Right(), t.PrintableWidthUM)
		}
		if t.PrintableHeightUM > 0 && e.Bottom() > t.PrintableHeightUM {
			fail(fmt.Sprintf("elements[%d]", i), "le champ %q dépasse la zone imprimable en hauteur (%d > %d µm)",
				e.Field, e.Bottom(), t.PrintableHeightUM)
		}
	}

	// Rule 3: THE INKED CONTENT FITS THE GEOMETRY OF THE EXISTING LABEL.
	bottom, right := shifted.inkedExtent(tierCount)
	if bottom > inkedHeight {
		fail("inked_content", "le contenu encré descend à %s dots, au-delà des %d dots de hauteur encrée de l'étiquette de production",
			formatMilliDots(bottom), InkedHeightDots)
	}
	if right > inkedWidth {
		fail("inked_content", "le contenu encré s'étend à %s dots, au-delà des %d dots de largeur encrée de l'étiquette de production",
			formatMilliDots(right), InkedWidthDots)
	}

	// Rule 4: the over-all width of the symbol, quiet zones included.
	if width := shifted.Symbol.TotalWidthMilliDots(); width > inkedWidth {
		fail("symbol.module_milli_dots",
			"le hors-tout du symbole (113 modules = %s dots) dépasse les %d dots de largeur encrée",
			formatMilliDots(width), InkedWidthDots)
	}

	// Rule 5: nothing may intersect the symbol, QUIET ZONES INCLUDED. A field
	// overlapping a quiet zone does not look wrong on a preview; it makes the
	// symbol unreadable at the till, which is far worse.
	symbolBox := shifted.symbolBox()
	for i, e := range shifted.Elements {
		if e.Field == FieldBarcode || !e.Active(tierCount) {
			continue
		}
		if overlaps(shifted.box(e), symbolBox) {
			fail(fmt.Sprintf("elements[%d]", i),
				"le champ %q recouvre le symbole ou l'une de ses zones de silence", e.Field)
		}
	}

	// Rule 8: no overlap between two active elements. Two fields on top of each
	// other print as an unreadable smudge, and the preview is the only place anyone
	// would notice.
	for i := range shifted.Elements {
		for j := i + 1; j < len(shifted.Elements); j++ {
			a, b := shifted.Elements[i], shifted.Elements[j]
			if a.Field == FieldBarcode || b.Field == FieldBarcode {
				continue // rule 5 owns the symbol
			}
			if !a.Active(tierCount) || !b.Active(tierCount) {
				continue
			}
			if overlaps(shifted.box(a), shifted.box(b)) {
				fail(fmt.Sprintf("elements[%d]", j), "le champ %q recouvre le champ %q", b.Field, a.Field)
			}
		}
	}

	return faults
}

// MaxOffsetDots reports how far the volunteer's adjustment may go in each
// direction before rule 3 or rule 5 would refuse it.
//
// This is what turns "ce décalage rognerait la zone de silence du code-barres" into
// a message that names the admissible maximum instead of just saying no.
func (t *Template) MaxOffsetDots(tierCount int) (x, y int) {
	bottom, right := t.inkedExtent(tierCount)
	remainingX := (int64(InkedWidthDots)*1000 - right) / 1000
	remainingY := (int64(InkedHeightDots)*1000 - bottom) / 1000
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

func known(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
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
