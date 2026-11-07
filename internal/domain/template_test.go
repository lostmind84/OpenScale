// This file holds the ARITHMETIC of a layout: micrometres into milli-dots, the one
// definition of where the symbol block ends, the 113 modules of its over-all width,
// and how far a volunteer's offset may still go.
//
// What REFUSES a layout is template_validate_test.go; what the shipped ones are
// worth is template_shipped_test.go.

package domain

import "testing"

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

// ws412Head is a print head this parc does not own: 12 dots/mm, and the SAME label —
// 35 × 25 mm — which at that pitch is 420 × 300 dots of ink.
func ws412Head() PrinterCapabilities {
	return PrinterCapabilities{Raster: true, DotsPerMM: 12, InkedWidthDots: 420, InkedHeightDots: 300}
}

// twelveDotTemplate is the shipped layout MEASURED for a 12 dots/mm head.
//
// Every length of a template is in micrometres and therefore untouched; the module
// alone is expressed in units of resolution, so 0.293 mm reads 2 344 milli-dots at
// 8 dots/mm and 3 516 at 12. That single figure is the whole reason a template has to
// say which head it was measured for (A2).
func twelveDotTemplate() Template {
	template := NeutralSingleTemplate()
	template.Name = "weighing_neutral_single_412"
	template.Media.DotsPerMM = 12
	template.Symbol.ModuleMilliDots = 3_516
	return template
}

// TestMaxOffsetDotsAnswersForTheHeadInService: the ±1 dot arrows are bounded by the
// geometry, and on a taller head there is more room. A margin computed against a
// constant would refuse an adjustment the printer would have accepted.
func TestMaxOffsetDotsAnswersForTheHeadInService(t *testing.T) {
	finer := twelveDotTemplate()
	_, tallMargin := finer.MaxOffsetDotsOn(ws412Head(), 2)

	shipped := NeutralSingleTemplate()
	_, parcMargin := shipped.MaxOffsetDots(2)
	if tallMargin <= parcMargin {
		t.Errorf("marge verticale : %d dots sur la WS412 contre %d sur la WS408 — "+
			"la même étiquette à un pas plus fin laisse plus de dots", tallMargin, parcMargin)
	}

	// A head that declares nothing answers exactly what the no-argument form answers.
	fallbackX, fallbackY := shipped.MaxOffsetDotsOn(PrinterCapabilities{}, 2)
	referenceX, referenceY := shipped.MaxOffsetDots(2)
	if fallbackX != referenceX || fallbackY != referenceY {
		t.Errorf("marge sans tête = (%d, %d), avec la tête de référence = (%d, %d)",
			fallbackX, fallbackY, referenceX, referenceY)
	}
}

// topMarginTemplate is IdenticalTemplate() with every active element, and the
// symbol, pushed 2 mm (16 dots at 8 dots/mm) down from the top of the label —
// a layout a cooperative could plausibly supply, with a margin ADR-063 exists to
// let the volunteer give back as a negative offset.
//
// The barcode element's height is reduced by the same 2 000 um its YUM gains, and
// the symbol's bar height by the same amount, so the BOTTOM of the inked content —
// what MaxOffsetDotsOn already bounds — lands exactly where it did in
// IdenticalTemplate(): this fixture exercises the new top margin alone, not a
// change at the bottom.
func topMarginTemplate() Template {
	template := IdenticalTemplate()
	template.Elements = append([]Element(nil), template.Elements...)
	for i := range template.Elements {
		template.Elements[i].YUM += 2_000
	}
	for i := range template.Elements {
		if template.Elements[i].Field == FieldBarcode {
			template.Elements[i].HeightUM -= 2_000
		}
	}
	template.Symbol.YUM += 2_000
	template.Symbol.BarHeightUM = 9_375
	return template
}

// TestOffsetBoundsOnRecomputesTheMarginOnTheTemplatesOwnGeometry is ADR-063: a
// gabarit that already carries a margin at its top lets the offset go negative by
// exactly that margin, on the SHIFTED geometry (symbol included), never further —
// that is anti-objectif 1 of the design: an offset that passes on paper must not
// crop the label at print time.
func TestOffsetBoundsOnRecomputesTheMarginOnTheTemplatesOwnGeometry(t *testing.T) {
	topMargin := topMarginTemplate()
	if faults := topMargin.Validate(2); len(faults) != 0 {
		t.Fatalf("le fixture lui-même devrait être légal, faults = %v", faults)
	}

	bounds := topMargin.OffsetBoundsOn(ReferenceHead(), 2)
	if bounds.MinY != -16 {
		t.Errorf("MinY = %d, attendu -16", bounds.MinY)
	}
	if bounds.MinX != 0 {
		t.Errorf("MinX = %d, attendu 0 : aucune marge n'a été ajoutée à gauche", bounds.MinX)
	}
	identical := IdenticalTemplate()
	wantMaxY := identical.OffsetBoundsOn(ReferenceHead(), 2).MaxY
	if bounds.MaxY != wantMaxY {
		t.Errorf("MaxY = %d, attendu %d (celui d'IdenticalTemplate(), le bas de l'encre n'a pas bougé)",
			bounds.MaxY, wantMaxY)
	}

	atTheBound := topMarginTemplate()
	atTheBound.OffsetYDots = bounds.MinY
	if faults := atTheBound.Validate(2); len(faults) != 0 {
		t.Errorf("OffsetYDots = %d (la borne elle-même) devrait valider, faults = %v", bounds.MinY, faults)
	}

	pastTheBound := topMarginTemplate()
	pastTheBound.OffsetYDots = bounds.MinY - 1
	if faults := pastTheBound.Validate(2); len(faults) == 0 {
		t.Errorf("OffsetYDots = %d (un dot au-delà de la borne) devrait être refusé", bounds.MinY-1)
	}
}

// TestOffsetBoundsOnShippedTemplatesIsZeroAtTopAndLeft: weighing_identical and
// weighing_neutral_single both start their ink at the very edge of the label, so
// they must keep exactly the [0, max] bound that already shipped — the regression
// guard of the Review Focus: La Coope's templates do not move.
func TestOffsetBoundsOnShippedTemplatesIsZeroAtTopAndLeft(t *testing.T) {
	for _, template := range []Template{IdenticalTemplate(), NeutralSingleTemplate()} {
		bounds := template.OffsetBoundsOn(ReferenceHead(), 2)
		if bounds.MinX != 0 || bounds.MinY != 0 {
			t.Errorf("%s : MinX, MinY = %d, %d, attendu 0, 0", template.Name, bounds.MinX, bounds.MinY)
		}
		maxX, maxY := template.MaxOffsetDots(2)
		if bounds.MaxX != maxX || bounds.MaxY != maxY {
			t.Errorf("%s : MaxX, MaxY = %d, %d, attendu ce que rend MaxOffsetDots : %d, %d",
				template.Name, bounds.MaxX, bounds.MaxY, maxX, maxY)
		}
	}
}

// syntheticLeftMarginTemplate is built, not measured, for exactly one property: its
// text element and its symbol both start 1 000 um (8 dots at 8 dots/mm) in from the
// left edge of the label -- the X mirror of topMarginTemplate(). Neither shipped
// template ever has room to spare on this axis (both reach MaxX == 0, see
// TestOffsetBoundsOnShippedTemplatesIsZeroAtTopAndLeft below), so building on top of
// one of them would mean shrinking the module or every element's width just to make
// space; a synthetic fixture says the same thing in four fields.
func syntheticLeftMarginTemplate() Template {
	return Template{
		Name:              "gabarit_synthetique_marge_gauche",
		Media:             Media{WidthUM: 40_000, HeightUM: 25_000, DotsPerMM: 8},
		PrintableWidthUM:  40_000,
		PrintableHeightUM: 25_000,
		Elements: []Element{
			{
				Field: FieldProductName,
				XUM:   1_000, YUM: 0, WidthUM: 10_000, HeightUM: 3_175,
				FontSizeUM: 3_175, Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 1_000, YUM: 5_000,
			// The same fractional module as weighing_identical: a known-GS1-legal value,
			// so this fixture's own legality never has to be argued separately.
			ModuleMilliDots: 2_344,
			BarHeightUM:     11_375,
			GuardDescentUM:  1_465,
			HRIHeightUM:     2_700,
		},
	}
}

// syntheticSymbolLeftmostTemplate is syntheticLeftMarginTemplate() with its text
// element pushed to 2 000 um instead of 1 000 -- twice the symbol's own margin --
// so the SYMBOL, not the text, is the leftmost ink. OffsetBoundsOn answering -8 and
// not -16 here is what an inkedOrigin that ignored the symbol (ADR-063's "symbole
// compris") would get wrong.
func syntheticSymbolLeftmostTemplate() Template {
	template := syntheticLeftMarginTemplate()
	template.Elements[0].XUM = 2_000
	return template
}

// TestOffsetBoundsOnRecomputesTheLeftMarginOnTheTemplatesOwnGeometry is the X mirror
// of TestOffsetBoundsOnRecomputesTheMarginOnTheTemplatesOwnGeometry: before this
// test, every fixture in this package had MinX == 0, so a MinX forced to zero, or an
// inkedOrigin that never looked at X at all, passed the whole package unnoticed.
func TestOffsetBoundsOnRecomputesTheLeftMarginOnTheTemplatesOwnGeometry(t *testing.T) {
	leftMargin := syntheticLeftMarginTemplate()
	if faults := leftMargin.Validate(2); len(faults) != 0 {
		t.Fatalf("le fixture lui-même devrait être légal, faults = %v", faults)
	}

	bounds := leftMargin.OffsetBoundsOn(ReferenceHead(), 2)
	if bounds.MinX != -8 {
		t.Errorf("MinX = %d, attendu -8 (1 000 µm = 8 dots à 8 dots/mm)", bounds.MinX)
	}
	if bounds.MinY != 0 {
		t.Errorf("MinY = %d, attendu 0 : aucune marge n'a été ajoutée en haut", bounds.MinY)
	}

	atTheBound := syntheticLeftMarginTemplate()
	atTheBound.OffsetXDots = bounds.MinX
	if faults := atTheBound.Validate(2); len(faults) != 0 {
		t.Errorf("OffsetXDots = %d (la borne elle-même) devrait valider, faults = %v", bounds.MinX, faults)
	}

	pastTheBound := syntheticLeftMarginTemplate()
	pastTheBound.OffsetXDots = bounds.MinX - 1
	if faults := pastTheBound.Validate(2); len(faults) == 0 {
		t.Errorf("OffsetXDots = %d (un dot au-delà de la borne) devrait être refusé", bounds.MinX-1)
	}
}

// TestOffsetBoundsOnCountsTheSymbolAsTheLeftmostInk is the Review Focus case the
// plan names explicitly, "symbole compris" : the symbol sits closer to the left edge
// than the text element, so the bound must follow the SYMBOL (-8) and not the text
// (-16). An inkedOrigin that ignored symbolBox() here would silently answer -16,
// which this package's other fixtures -- symbol and text always flush together --
// could never catch.
func TestOffsetBoundsOnCountsTheSymbolAsTheLeftmostInk(t *testing.T) {
	symbolLeftmost := syntheticSymbolLeftmostTemplate()
	if faults := symbolLeftmost.Validate(2); len(faults) != 0 {
		t.Fatalf("le fixture lui-même devrait être légal, faults = %v", faults)
	}

	bounds := symbolLeftmost.OffsetBoundsOn(ReferenceHead(), 2)
	if bounds.MinX != -8 {
		t.Errorf("MinX = %d, attendu -8 (celle du symbole, le plus à gauche, pas -16 : "+
			"le champ texte a deux fois sa marge)", bounds.MinX)
	}

	atTheBound := syntheticSymbolLeftmostTemplate()
	atTheBound.OffsetXDots = -8
	if faults := atTheBound.Validate(2); len(faults) != 0 {
		t.Errorf("OffsetXDots = -8 (la borne du symbole) devrait valider, faults = %v", faults)
	}

	pastTheBound := syntheticSymbolLeftmostTemplate()
	pastTheBound.OffsetXDots = -9
	if faults := pastTheBound.Validate(2); len(faults) == 0 {
		t.Error("OffsetXDots = -9 devrait être refusé : le symbole sortirait de l'étiquette")
	}
}

// TestOffsetBoundsOnStopsAtTheMediaAndNotOnlyAtTheHead: MaxX and MaxY are the margin
// the LABEL leaves, and a label narrower or shorter than the head leaves less. Measured
// against the head alone, a 34 × 24.9 mm roll whose text stops at 30 mm answered
// MaxX = 15 and MaxY = 1, and every arrow press past 7 or 0 printed a symbol the
// renderer refuses -- the bound said "dans les clous" while the label came out
// refused, which is anti-objectif 1 of the lot.
func TestOffsetBoundsOnStopsAtTheMediaAndNotOnlyAtTheHead(t *testing.T) {
	build := func() Template {
		template := starterOnMedia(34_000, 24_900)
		for i := range template.Elements {
			if right := template.Elements[i].Right(); right > 30_000 {
				template.Elements[i].WidthUM -= right - 30_000
			}
		}
		return template
	}
	template := build()
	if faults := template.Validate(2); len(faults) != 0 {
		t.Fatalf("le fixture lui-même devrait être légal, faults = %v", faults)
	}

	// 34 mm = 272 dots, symbol right edge 264.872 dots -> 7. 24.9 mm = 199.2 dots,
	// floored to the 199 the renderer allocates, symbol bottom 198.104 dots -> 0.
	bounds := template.OffsetBoundsOn(ReferenceHead(), 2)
	if bounds.MaxX != 7 || bounds.MaxY != 0 {
		t.Fatalf("MaxX, MaxY = %d, %d, attendu 7, 0 (la marge du média, pas celle de la tête)",
			bounds.MaxX, bounds.MaxY)
	}

	atTheBound := build()
	atTheBound.OffsetXDots, atTheBound.OffsetYDots = bounds.MaxX, bounds.MaxY
	if faults := atTheBound.Validate(2); len(faults) != 0 {
		t.Errorf("le décalage à la borne (%d ; %d) devrait valider, faults = %v",
			bounds.MaxX, bounds.MaxY, faults)
	}
	for _, past := range []struct{ x, y int }{{bounds.MaxX + 1, 0}, {0, bounds.MaxY + 1}} {
		pastTheBound := build()
		pastTheBound.OffsetXDots, pastTheBound.OffsetYDots = past.x, past.y
		if faults := pastTheBound.Validate(2); len(faults) == 0 {
			t.Errorf("le décalage (%d ; %d), un dot au-delà de la borne, devrait être refusé", past.x, past.y)
		}
	}
}

// TestSymbolHeightUsesMaxAndNotASum is the definition of §7.4 that had to be settled
// once: the HRI digits sit INSIDE the guard descent band, so the block height is
// bars + max(descent, hri), never bars + descent + hri.
//
// The two readings differ by GuardDescentUM on any shipped geometry — the additive one
// gives 15 290 um against 13 825 — which is two different answers to the same
// blocking rule.
func TestSymbolHeightUsesMaxAndNotASum(t *testing.T) {
	symbol := NeutralSingleTemplate().Symbol

	if got := symbol.HeightUM(); got != 13_825 {
		t.Errorf("hauteur du bloc = %d µm, want 13825", got)
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
