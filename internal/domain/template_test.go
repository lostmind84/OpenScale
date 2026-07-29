package domain

import (
	"strings"
	"testing"
)

func faultFields(faults []Fault) []string {
	out := make([]string, len(faults))
	for i, f := range faults {
		out[i] = f.Field
	}
	return out
}

func hasFault(faults []Fault, field string) bool {
	for _, f := range faults {
		if f.Field == field {
			return true
		}
	}
	return false
}

// TestShippedTemplatesPassEveryRule: whatever else changes, a template we ship must
// load. The CI fails otherwise (§7.5).
func TestShippedTemplatesPassEveryRule(t *testing.T) {
	for name, template := range ShippedTemplates() {
		for _, tierCount := range []int{1, 2, 3} {
			if faults := template.Validate(tierCount); len(faults) != 0 {
				t.Errorf("%s with %d tier(s): %d faults", name, tierCount, len(faults))
				for _, f := range faults {
					t.Errorf("    %s", f)
				}
			}
		}
	}
}

// TestNeutralTemplateInkedExtentHasMargin: rule 3 is the one that bit before, when
// the document compared an Access box to an assumed media and failed the CI on the
// very template A1 requires. Here we assert the margin, so a future edit that eats
// it is visible.
func TestNeutralTemplateInkedExtentHasMargin(t *testing.T) {
	template := NeutralSingleTemplate()
	bottom, right := template.inkedExtent(2)

	bottomDots, rightDots := bottom/1000, right/1000
	t.Logf("contenu encré : %d dots de haut, %d dots de large (limites %d et %d)",
		bottomDots, rightDots, InkedHeightDots, InkedWidthDots)

	if bottomDots > InkedHeightDots {
		t.Errorf("hauteur encrée %d > %d", bottomDots, InkedHeightDots)
	}
	if rightDots > InkedWidthDots {
		t.Errorf("largeur encrée %d > %d", rightDots, InkedWidthDots)
	}
	// The bottom is dominated by the symbol block, as on the production label.
	symbolBottom := template.Media.MilliDots(template.Symbol.YUM + template.Symbol.HeightUM())
	if bottom != symbolBottom {
		t.Errorf("le bas encré (%d) devrait être celui du symbole (%d)", bottom, symbolBottom)
	}
}

// TestSymbolHeightUsesMaxAndNotASum is the definition of §7.4 that had to be settled
// once: the HRI digits sit INSIDE the guard descent band, so the block height is
// bars + max(descent, hri), never bars + descent + hri.
//
// The two readings differ by 1 465 um on the shipped geometry — the additive one
// gives 15 270 um against 13 805 — which is two different answers to the same
// blocking rule.
func TestSymbolHeightUsesMaxAndNotASum(t *testing.T) {
	symbol := NeutralSingleTemplate().Symbol

	if got := symbol.HeightUM(); got != 13_805 {
		t.Errorf("hauteur du bloc = %d µm, want 13805", got)
	}
	if additive := symbol.BarHeightUM + symbol.GuardDescentUM + symbol.HRIHeightUM; additive == symbol.HeightUM() {
		t.Error("la lecture additive donne le même résultat : le test ne distingue rien")
	}

	// The shipped HRI is TALLER than the guard descent, so it commands the bottom.
	if symbol.HRIHeightUM <= symbol.GuardDescentUM {
		t.Errorf("HRI %d µm, descente %d µm : sur la géométrie mesurée la HRI est la plus haute",
			symbol.HRIHeightUM, symbol.GuardDescentUM)
	}
	// And max() is written rather than "hri" because an operator template may declare
	// a shorter HRI, and then the guards command it.
	shorter := symbol
	shorter.HRIHeightUM = 500
	if got, want := shorter.HeightUM(), symbol.BarHeightUM+symbol.GuardDescentUM; got != want {
		t.Errorf("avec une HRI courte, hauteur = %d µm, want %d (la descente commande)", got, want)
	}
}

// TestSymbolTotalWidthIsOneHundredAndThirteenModules: 11 quiet + 95 bars + 7 quiet.
func TestSymbolTotalWidthIsOneHundredAndThirteenModules(t *testing.T) {
	symbol := NeutralSingleTemplate().Symbol
	if got := symbol.TotalWidthMilliDots(); got != 113*2344 {
		t.Errorf("hors-tout = %d milli-dots, want %d", got, 113*2344)
	}
	// 264.872 dots at 8 dots/mm, i.e. 33.109 mm: the figure of §7.1.
	if got := symbol.TotalWidthMilliDots(); got != 264_872 {
		t.Errorf("hors-tout = %d milli-dots, want 264872 (33,109 mm)", got)
	}
}

// TestRuleThreeRefusesContentBelowTheInkedHeight is rule 3 exercised in the
// direction that matters: a symbol pushed too low.
func TestRuleThreeRefusesContentBelowTheInkedHeight(t *testing.T) {
	template := NeutralSingleTemplate()
	// 200 dots is 25 000 um. The block is 13 805 um tall, so a symbol at 11 500 um ends
	// at 25 305 um: a little over two dots past the paper.
	template.Symbol.YUM = 11_500
	template.Elements[5].YUM = 11_500

	faults := template.Validate(2)
	if !hasFault(faults, "inked_content") {
		t.Fatalf("faults = %v, want inked_content", faultFields(faults))
	}
	for _, f := range faults {
		if f.Field == "inked_content" {
			if !strings.Contains(f.Message, "hauteur encrée") {
				t.Errorf("message = %q, want the inked HEIGHT named", f.Message)
			}
			// The message must name the geometry of the EXISTING label, not a media.
			if strings.Contains(f.Message, "média") {
				t.Errorf("message = %q : aucune règle ne dépend du média déclaré", f.Message)
			}
		}
	}
}

// TestRuleFourRefusesASymbolWiderThanTheLabel: the over-all width, quiet zones
// included.
func TestRuleFourRefusesASymbolWiderThanTheLabel(t *testing.T) {
	template := NeutralSingleTemplate()
	// 113 modules must stay within 280 dots, so the module must stay under
	// 2 477 milli-dots. 3 000 is well past it and still inside rule 9's bounds.
	template.Symbol.ModuleMilliDots = 3_000

	faults := template.Validate(2)
	if !hasFault(faults, "symbol.module_milli_dots") {
		t.Fatalf("faults = %v, want symbol.module_milli_dots", faultFields(faults))
	}
}

// TestRuleFiveProtectsTheQuietZones: a field overlapping a quiet zone looks fine on
// a preview and makes the symbol unreadable at the till.
func TestRuleFiveProtectsTheQuietZones(t *testing.T) {
	template := NeutralSingleTemplate()
	// Drop the total price onto the top of the symbol.
	template.Elements[4].YUM = 10_500

	faults := template.Validate(2)
	if len(faults) == 0 {
		t.Fatal("un champ posé sur le symbole doit être refusé")
	}
	found := false
	for _, f := range faults {
		if strings.Contains(f.Message, "recouvre le symbole") {
			found = true
		}
	}
	if !found {
		t.Errorf("faults = %v, want a fault naming the symbol overlap", faults)
	}
}

// TestRuleFiveCountsTheQuietZoneAndNotJustTheBars: the right quiet zone is 7 modules
// of nothing, and a field placed there breaks the symbol just as surely as one on a
// bar.
func TestRuleFiveCountsTheQuietZoneAndNotJustTheBars(t *testing.T) {
	template := NeutralSingleTemplate()
	symbol := template.Symbol

	// The bars end at 11 + 95 = 106 modules; the block ends at 113. Place a narrow
	// field inside that last stretch, vertically level with the symbol.
	barsEndUM := Micrometers(float64(106*symbol.ModuleMilliDots) / template.Media.DotsPerMM)
	template.Elements = append(template.Elements, Element{
		Field: FieldQuantity, // any field: rule 7 sees a duplicate, rule 5 the overlap
		XUM:   barsEndUM + 100, YUM: symbol.YUM + 1_000,
		WidthUM: 500, HeightUM: 500, FontSizeUM: 1_800, Align: AlignLeft,
	})

	faults := template.Validate(2)
	overlap := false
	for _, f := range faults {
		if strings.Contains(f.Message, "zones de silence") || strings.Contains(f.Message, "recouvre le symbole") {
			overlap = true
		}
	}
	if !overlap {
		t.Errorf("faults = %v, want the quiet zone to be protected", faults)
	}
}

// TestRuleSixAppliesTheOffsetBeforeValidating: the ±1 dot arrows must be bounded by
// the geometry, not merely by ±99.
func TestRuleSixAppliesTheOffsetBeforeValidating(t *testing.T) {
	template := NeutralSingleTemplate()

	// The shipped template has a little room, so a small shift is fine.
	template.OffsetYDots = 1
	if faults := template.Validate(2); len(faults) != 0 {
		t.Errorf("un décalage de 1 dot doit passer : %v", faults)
	}

	// A shift past the remaining margin must be refused, with the margin named.
	unshifted := NeutralSingleTemplate()
	maxX, maxY := unshifted.MaxOffsetDots(2)
	t.Logf("marge disponible : %d dots en X, %d dots en Y", maxX, maxY)
	if maxY <= 0 {
		t.Fatal("le gabarit livré doit avoir de la marge verticale")
	}
	template.OffsetYDots = maxY + 2
	faults := template.Validate(2)
	if !hasFault(faults, "inked_content") {
		t.Errorf("un décalage de %d dots doit être refusé : %v", template.OffsetYDots, faultFields(faults))
	}

	// The offset must NOT mutate the receiver: validating twice must not shift twice.
	before := template.Elements[0].YUM
	template.Validate(2)
	if template.Elements[0].YUM != before {
		t.Error("Validate a déplacé les éléments du gabarit")
	}
}

// TestRuleSevenClosesTheFieldAndConditionLists: no template engine, so no injection
// and no rendering error at print time.
func TestRuleSevenClosesTheFieldAndConditionLists(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Template)
		field  string
	}{
		{"champ inconnu", func(t *Template) { t.Elements[0].Field = "product_photo" }, "elements[0].field"},
		{"condition inconnue", func(t *Template) { t.Elements[0].When = "if_expensive" }, "elements[0].when"},
		{"alignement inconnu", func(t *Template) { t.Elements[0].Align = "center" }, "elements[0].align"},
		{"champ en double", func(t *Template) { t.Elements[1].Field = FieldProductName }, "elements[1].field"},
		{"boîte de largeur nulle", func(t *Template) { t.Elements[0].WidthUM = 0 }, "elements[0]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			template := NeutralSingleTemplate()
			c.mutate(&template)
			faults := template.Validate(2)
			if !hasFault(faults, c.field) {
				t.Errorf("faults = %v, want a fault on %s", faultFields(faults), c.field)
			}
			// A closed list must SAY what it accepts: a volunteer reading "champ
			// inconnu" with no list has nowhere to go.
			for _, f := range faults {
				if f.Field == c.field && strings.Contains(f.Message, "inconnu") && len(f.Values) == 0 {
					t.Errorf("la faute %q ne propose aucune valeur admissible", f.Message)
				}
			}
		})
	}
}

// TestRuleEightRefusesOverlappingFields: two fields on top of each other print as an
// unreadable smudge.
func TestRuleEightRefusesOverlappingFields(t *testing.T) {
	template := NeutralSingleTemplate()
	// Put the quantity on top of the product name.
	template.Elements[1].YUM = 1_000

	faults := template.Validate(2)
	found := false
	for _, f := range faults {
		if strings.Contains(f.Message, "recouvre le champ") {
			found = true
		}
	}
	if !found {
		t.Errorf("faults = %v, want an overlap fault", faults)
	}
}

// TestTouchingEdgesDoNotOverlap: the shipped template lays the quantity and the unit
// price side by side, and abutting boxes must be legal.
func TestTouchingEdgesDoNotOverlap(t *testing.T) {
	template := NeutralSingleTemplate()
	quantity, unitPrice := template.Elements[1], template.Elements[2]

	// Make them exactly abut.
	template.Elements[2].XUM = quantity.Right()
	template.Elements[2].WidthUM = unitPrice.Right() - quantity.Right()

	if faults := template.Validate(2); len(faults) != 0 {
		t.Errorf("des boîtes jointives doivent être légales : %v", faults)
	}
}

// TestConditionalFieldIsIgnoredWhenInactive: a mono-tarif station must not be refused
// a template because of a field that will never be drawn on it.
func TestConditionalFieldIsIgnoredWhenInactive(t *testing.T) {
	template := NeutralSingleTemplate()
	// Put the conditional secondary price somewhere illegal.
	for i := range template.Elements {
		if template.Elements[i].Field == FieldSecondaryTotalPrice {
			template.Elements[i].YUM = 11_000 // squarely on the symbol
		}
	}

	if faults := template.Validate(1); len(faults) != 0 {
		t.Errorf("mono-tarif : le champ conditionnel n'est pas dessiné, donc il ne peut pas fauter : %v", faults)
	}
	if faults := template.Validate(2); len(faults) == 0 {
		t.Error("double tarif : le même champ est dessiné, et il doit fauter")
	}
}

// TestRuleNineBoundsTheModuleAndTheTypeSize.
func TestRuleNineBoundsTheModuleAndTheTypeSize(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*Template)
		field  string
	}{
		{"module trop petit", func(t *Template) { t.Symbol.ModuleMilliDots = MinModuleMilliDots - 1 }, "symbol.module_milli_dots"},
		{"module trop grand", func(t *Template) { t.Symbol.ModuleMilliDots = MaxModuleMilliDots + 1 }, "symbol.module_milli_dots"},
		{"corps sous le plancher", func(t *Template) { t.Elements[0].FontSizeUM = MinFontSizeUM - 1 }, "elements[0].font_size_um"},
		{"corps minimal sous le plancher", func(t *Template) { t.Elements[0].MinFontSizeUM = 100 }, "elements[0].min_font_size_um"},
		{"corps minimal au-dessus du nominal", func(t *Template) { t.Elements[0].MinFontSizeUM = 9_000 }, "elements[0].min_font_size_um"},
	} {
		t.Run(c.name, func(t *testing.T) {
			template := NeutralSingleTemplate()
			c.mutate(&template)
			if faults := template.Validate(2); !hasFault(faults, c.field) {
				t.Errorf("faults = %v, want %s", faultFields(faults), c.field)
			}
		})
	}

	// The bounds themselves are reachable: a rule nobody can satisfy is a bug.
	template := NeutralSingleTemplate()
	template.Elements[0].FontSizeUM = MinFontSizeUM
	template.Elements[0].MinFontSizeUM = MinFontSizeUM
	if faults := template.Validate(2); len(faults) != 0 {
		t.Errorf("le plancher exact doit être admis : %v", faults)
	}
}

// TestRulesOneAndTwoBearOnTheDeclaredGeometry: unlike rules 3 and 4, which bear on
// measured ink, these two check the operator's own statement about their printer.
func TestRulesOneAndTwoBearOnTheDeclaredGeometry(t *testing.T) {
	template := NeutralSingleTemplate()
	template.PrintableWidthUM = 50_000 // wider than the media
	if faults := template.Validate(2); !hasFault(faults, "printable_area") {
		t.Errorf("faults = %v, want printable_area", faultFields(faults))
	}

	template = NeutralSingleTemplate()
	template.PrintableHeightUM = 12_000 // half the label
	faults := template.Validate(2)
	if len(faults) == 0 {
		t.Error("des champs hors de la zone imprimable doivent être refusés")
	}
	for _, f := range faults {
		if strings.Contains(f.Message, "zone imprimable") {
			return
		}
	}
	t.Errorf("faults = %v, want a printable-area fault", faults)
}

// TestValidateRefusesAnAbsurdResolutionFirst: every geometric rule divides the world
// by dots_per_mm, so a zero there must produce ONE fault and not a page of them.
func TestValidateRefusesAnAbsurdResolutionFirst(t *testing.T) {
	template := NeutralSingleTemplate()
	template.Media.DotsPerMM = 0

	faults := template.Validate(2)
	if len(faults) != 1 {
		t.Errorf("%d fautes, want 1 : une résolution nulle doit nommer sa cause, pas ses conséquences", len(faults))
	}
	if !hasFault(faults, "media.dots_per_mm") {
		t.Errorf("faults = %v, want media.dots_per_mm", faultFields(faults))
	}
}

// TestMilliDotConversionIsAPlainMultiplication: a micrometre is a thousandth of a
// millimetre, so um x dots/mm is already thousandths of a dot. This is what keeps
// rules 3 and 4 free of any rounding argument.
func TestMilliDotConversionIsAPlainMultiplication(t *testing.T) {
	eightDots := Media{DotsPerMM: 8}
	cases := []struct {
		um   Micrometers
		want int64
	}{
		{34_978, 279_824}, // the widest field of the production label
		{1_000, 8_000},    // 1 mm is 8 dots
		{125, 1_000},      // one dot is 125 um at this resolution
		{0, 0},
	}
	for _, c := range cases {
		if got := eightDots.MilliDots(c.um); got != c.want {
			t.Errorf("MilliDots(%d µm) = %d, want %d", c.um, got, c.want)
		}
	}
	// And a WS412 doubles nothing by magic: 12 dots/mm gives 12 milli-dots per um.
	if got := (Media{DotsPerMM: 12}).MilliDots(1_000); got != 12_000 {
		t.Errorf("à 12 dots/mm, 1 mm = %d milli-dots, want 12000", got)
	}
}

// TestIntegerModuleTemplateIsGabaritB documents what it is for, and what it is NOT
// for: it produces a measurement, never an automatic switch (§7.6).
func TestIntegerModuleTemplateIsGabaritB(t *testing.T) {
	b := IntegerModuleTemplate()
	if b.Symbol.ModuleMilliDots != 2_000 {
		t.Errorf("module = %d, want 2000 (exactly 2 dots)", b.Symbol.ModuleMilliDots)
	}
	// At an integer module the over-all width is a whole number of dots, which is the
	// point: every bar is exactly 2 dots and the bars are rigorously uniform.
	if width := b.Symbol.TotalWidthMilliDots(); width%1000 != 0 {
		t.Errorf("hors-tout = %d milli-dots : à module entier il doit être un compte entier de dots", width)
	}
	// And it is NARROWER than the production symbol, which is the trade-off: 75.8 %
	// magnification, below the 80 % GS1 floor.
	if b.Symbol.TotalWidthMilliDots() >= NeutralSingleTemplate().Symbol.TotalWidthMilliDots() {
		t.Error("le gabarit B doit être plus étroit que le gabarit à module fractionnaire")
	}
	if faults := b.Validate(1); len(faults) != 0 {
		t.Errorf("le gabarit B doit être valide : %v", faults)
	}
}

// TestMeasuredProductionGeometry freezes what was read out of the test PDF.
//
// reference/test_etiquette_EtataImprimer.pdf was decompressed and its content stream
// parsed. The six control boxes of §7.2 are confirmed to within 40 um — under a third
// of a dot. Two things the document does not say came out of it, and both belong to
// L4:
//
//   - the symbol does NOT start at the top of its control box. §7.2 gives 8 996 um,
//     which is the box; the glyph's baseline is at 21 326 um and rises 0.977 em, so
//     the bars begin at 9 604 um. The block height is exactly the stated 14 650 um —
//     it is the origin that is off by 608 um, and the inked content therefore reaches
//     194 dots rather than 189;
//   - the production boxes overlap each other and the symbol box, because an Access
//     control carries its leading inside its height.
//
// This test does not exercise production code. It exists so the figures survive in
// the repository rather than in a terminal, and so that L4 starts from a measurement
// instead of from a transcription.
func TestMeasuredProductionGeometry(t *testing.T) {
	const (
		measuredBaselineUM = 21_326 // baseline of the 34 pt barcode glyph
		measuredBarTopUM   = 9_604  // baseline - 0.977 em
		measuredHRIBottom  = 24_254 // baseline + 0.244 em
		documentedOriginUM = 8_996  // §7.2: the top of the CodeBarre control box
	)

	if got := measuredHRIBottom - measuredBarTopUM; got != 14_650 {
		t.Errorf("hauteur mesurée du bloc = %d µm, la documentation dit 14650", got)
	}
	if drift := measuredBarTopUM - documentedOriginUM; drift != 608 {
		t.Errorf("écart origine documentée / origine mesurée = %d µm, want 608", drift)
	}

	// What that means for rule 3, on the production geometry: 194 dots, not 189.
	inked := Media{DotsPerMM: 8}.MilliDots(measuredHRIBottom) / 1000
	if inked != 194 {
		t.Errorf("contenu encré mesuré = %d dots, want 194", inked)
	}
	if inked > InkedHeightDots {
		t.Errorf("le contenu encré mesuré (%d dots) dépasse la hauteur encrée de l'étiquette (%d)",
			inked, InkedHeightDots)
	}
	t.Logf("géométrie de production mesurée : symbole de %d à %d µm, contenu encré %d dots, "+
		"marge %d dots sous les %d dots de l'étiquette",
		measuredBarTopUM, measuredHRIBottom, inked, InkedHeightDots-int(inked), InkedHeightDots)
}

// TestFaultStringNamesItsField: a fault reaches a volunteer through the admin
// screen and reaches whoever answers the telephone through a log line. Both need
// the field named.
func TestFaultStringNamesItsField(t *testing.T) {
	f := Fault{Field: "symbol.module_milli_dots", Message: "hors bornes"}
	if got := f.String(); got != "symbol.module_milli_dots : hors bornes" {
		t.Errorf("String() = %q", got)
	}
}

// TestMaxOffsetDotsClampsAtZero: a template already at the limit offers no room,
// and the answer must be 0 rather than a negative number a screen would render as
// "-3 dots admissibles".
func TestMaxOffsetDotsClampsAtZero(t *testing.T) {
	template := NeutralSingleTemplate()
	// Push the content past the limit, then ask how much room is left.
	template.Symbol.YUM = 20_000
	x, y := template.MaxOffsetDots(2)
	if x < 0 || y < 0 {
		t.Errorf("marge = (%d, %d), want des valeurs jamais négatives", x, y)
	}
	if y != 0 {
		t.Errorf("marge verticale = %d, want 0 sur un gabarit déjà hors limites", y)
	}
}

// TestFormatMilliDotsReadsAsDots: a message says "279,824 dots", not "279824".
func TestFormatMilliDotsReadsAsDots(t *testing.T) {
	cases := []struct {
		milliDots int64
		want      string
	}{
		{279_824, "279,824"},
		{264_872, "264,872"},
		{1_000, "1,000"},
		{7, "0,007"},
		{0, "0,000"},
		{-1_500, "-1,500"},
	}
	for _, c := range cases {
		if got := formatMilliDots(c.milliDots); got != c.want {
			t.Errorf("formatMilliDots(%d) = %q, want %q", c.milliDots, got, c.want)
		}
	}
}

// TestNegativeCoordinatesAreRefused: an offset can push an element off the top-left
// of the label, and rule 1 must say so rather than letting the renderer clip it.
func TestNegativeCoordinatesAreRefused(t *testing.T) {
	template := NeutralSingleTemplate()
	template.OffsetYDots = -5

	faults := template.Validate(2)
	if len(faults) == 0 {
		t.Fatal("un décalage négatif qui sort de l'étiquette doit être refusé")
	}
	named := false
	for _, f := range faults {
		if strings.Contains(f.Message, "hors de l'étiquette") {
			named = true
		}
	}
	if !named {
		t.Errorf("faults = %v, want a fault naming the element as being off the label", faults)
	}
}

// TestIdenticalTemplateHasUniformBars is ADR-029 made checkable.
//
// The decision the commissioning party took: stop putting the two prices ON the bars,
// stack the text instead, and put the symbol below it. The bars become uniform over
// the full width AND taller in usable terms.
func TestIdenticalTemplateHasUniformBars(t *testing.T) {
	template := IdenticalTemplate()

	// 1. Nothing overlaps the symbol any more — which is what makes rule 5
	//    satisfiable for the production template at all.
	if faults := template.Validate(2); len(faults) != 0 {
		t.Errorf("double tarif : %d fautes", len(faults))
		for _, f := range faults {
			t.Errorf("    %s", f)
		}
	}
	if faults := template.Validate(1); len(faults) != 0 {
		t.Errorf("mono-tarif : %d fautes", len(faults))
		for _, f := range faults {
			t.Errorf("    %s", f)
		}
	}

	// 2. The usable bar height BEATS what the current label achieves. Measured on the
	//    test PDF: 11 722 um of bars declared, of which the solidarity price eats
	//    1 192 um and the member price 3 381 um, leaving 8 341 um clean.
	const cleanBarsToday = 10_875 - 2_534 // 8 341 um, spelled out from the measurement
	if got := template.Symbol.BarHeightUM; got <= cleanBarsToday {
		t.Errorf("barres = %d µm, want plus que les %d µm réellement propres aujourd'hui",
			got, cleanBarsToday)
	}
	if got := template.Symbol.BarHeightUM; got != 10_875 {
		t.Errorf("barres = %d µm, want 10875 (87 dots exactement à 8 dots/mm)", got)
	}
	// 87 dots exactly: a whole number of dots, so no bar is a fraction of a scan line.
	if milliDots := template.Media.MilliDots(template.Symbol.BarHeightUM); milliDots%1000 != 0 {
		t.Errorf("hauteur de barres = %d milli-dots : elle doit être un compte entier de dots", milliDots)
	}

	// 3. What A1 freezes has NOT moved.
	if got := template.Symbol.ModuleMilliDots; got != 2_344 {
		t.Errorf("module = %d, want 2344 : A1 fige le grandissement", got)
	}
	if got := template.Symbol.TotalWidthMilliDots(); got != 264_872 {
		t.Errorf("hors-tout = %d milli-dots, want 264872 (33,109 mm) : A1 le fige aussi", got)
	}

	// 4. The HRI survives: it is printed today, and dropping it would take away the
	//    cashier's fallback.
	if template.Symbol.HRIHeightUM != 2_930 {
		t.Errorf("HRI = %d µm, want 2930 : elle est imprimée aujourd'hui", template.Symbol.HRIHeightUM)
	}

	// 5. The truncation stays a documented decision, so the admin diagnostic stays
	//    informative rather than amber (ADR-003).
	if !template.TruncationAccepted {
		t.Error("truncation_accepted doit rester levé : la troncature est une décision, pas un défaut")
	}

	bottom, right := template.inkedExtent(2)
	t.Logf("contenu encré : %d,%03d dots de haut, %d,%03d de large (limites %d et %d)",
		bottom/1000, bottom%1000, right/1000, right%1000, InkedHeightDots, InkedWidthDots)
	t.Logf("barres uniformes : %d µm contre %d µm propres aujourd'hui (+%.0f %%)",
		template.Symbol.BarHeightUM, cleanBarsToday,
		100*float64(int(template.Symbol.BarHeightUM)-cleanBarsToday)/cleanBarsToday)
}

// TestIdenticalTemplateTextDoesNotReachTheSymbol is the geometric heart of ADR-029,
// asserted on the ink rather than on a rule verdict.
func TestIdenticalTemplateTextDoesNotReachTheSymbol(t *testing.T) {
	template := IdenticalTemplate()
	symbolTop := template.Symbol.YUM

	for _, e := range template.Elements {
		if e.Field == FieldBarcode {
			continue
		}
		if e.Bottom() > symbolTop {
			t.Errorf("le champ %s descend à %d µm, le symbole commence à %d µm : "+
				"le texte recouvre encore les barres", e.Field, e.Bottom(), symbolTop)
		}
	}

	// And the two prices share a BASELINE, which the legacy report did not do — its
	// two prices sat 774 um apart for no reason anyone could state.
	var secondary, primary Element
	for _, e := range template.Elements {
		switch e.Field {
		case FieldSecondaryTotalPrice:
			secondary = e
		case FieldPrimaryTotalPrice:
			primary = e
		}
	}
	const ascent = 750 // per mille
	secondaryBaseline := secondary.YUM + secondary.FontSizeUM*ascent/1000
	primaryBaseline := primary.YUM + primary.FontSizeUM*ascent/1000
	if drift := secondaryBaseline - primaryBaseline; drift > 2 || drift < -2 {
		t.Errorf("lignes de base : solidaire à %d µm, adhérent à %d µm (écart %d) — "+
			"les deux prix doivent partager leur ligne de base",
			secondaryBaseline, primaryBaseline, drift)
	}
}

// TestIdenticalTemplateIsTheShippedDefault: config-lacagette.json selects it, so a
// rename must break here rather than at start-up on a station.
func TestIdenticalTemplateIsTheShippedDefault(t *testing.T) {
	shipped := ShippedTemplates()
	template, ok := shipped[DefaultTemplateName]
	if !ok {
		t.Fatalf("%q absent des gabarits livrés : %v", DefaultTemplateName, shipped)
	}
	if template.Name != DefaultTemplateName {
		t.Errorf("le gabarit livré sous %q se nomme %q", DefaultTemplateName, template.Name)
	}
	if len(shipped) != 3 {
		t.Errorf("%d gabarits livrés, want 3 (identical, neutral_single, integer_module)", len(shipped))
	}
	// Every shipped template names itself the way it is keyed.
	for name, template := range shipped {
		if template.Name != name {
			t.Errorf("le gabarit %q se nomme %q", name, template.Name)
		}
	}
}
