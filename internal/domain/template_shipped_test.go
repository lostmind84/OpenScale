// This file holds what the SHIPPED layouts are worth: that both pass every rule,
// that the production one reproduces the geometry measured on the label, and that
// the one this repository retired cannot come back.
//
// weighing_identical is the label a till has been reading for years, so what is
// checked here is the label itself and not an idea of it.

package domain

import "testing"

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
		bottomDots, rightDots, ReferenceInkedHeightDots, ReferenceInkedWidthDots)

	if bottomDots > ReferenceInkedHeightDots {
		t.Errorf("hauteur encrée %d > %d", bottomDots, ReferenceInkedHeightDots)
	}
	if rightDots > ReferenceInkedWidthDots {
		t.Errorf("largeur encrée %d > %d", rightDots, ReferenceInkedWidthDots)
	}
	// The bottom is dominated by the symbol block, as on the production label.
	symbolBottom := template.Media.MilliDots(template.Symbol.YUM + template.Symbol.HeightUM())
	if bottom != symbolBottom {
		t.Errorf("le bas encré (%d) devrait être celui du symbole (%d)", bottom, symbolBottom)
	}
}

// TestGabaritBIsRetiredAndCannotComeBack is the other half of removing it: a template
// that no rule refuses comes back the day someone needs a quick experiment, and gabarit
// B's whole problem was that its winning arm could never be adopted (75.8 %, below the
// GS1 floor). Rule 9 now says so, so the removal is enforced rather than merely done.
func TestGabaritBIsRetiredAndCannotComeBack(t *testing.T) {
	shipped := ShippedTemplates()
	if _, found := shipped["weighing_integer_module"]; found {
		t.Error("weighing_integer_module est encore livré")
	}
	if len(shipped) != 2 {
		t.Errorf("%d gabarits livrés, attendu 2 : %v", len(shipped), shipped)
	}

	b := NeutralSingleTemplate()
	b.Symbol.ModuleMilliDots = 2_000 // 2 dots exactly = 250 um at 8 dots/mm
	faults := b.Validate(1)
	if len(faults) == 0 {
		t.Fatal("un module de 2 dots vaut 250 µm, soit 75,8 % : la règle 9 doit le refuser")
	}
	t.Logf("refusé, et la raison est lisible : %v", faults[0])
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
	if inked > ReferenceInkedHeightDots {
		t.Errorf("le contenu encré mesuré (%d dots) dépasse la hauteur encrée de l'étiquette (%d)",
			inked, ReferenceInkedHeightDots)
	}
	t.Logf("géométrie de production mesurée : symbole de %d à %d µm, contenu encré %d dots, "+
		"marge %d dots sous les %d dots de l'étiquette",
		measuredBarTopUM, measuredHRIBottom, inked, ReferenceInkedHeightDots-int(inked), ReferenceInkedHeightDots)
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
	// 11 875 and not 10 875 since 30/07/2026, when the commissioning party reopened A1.
	// The extra 1 000 um came from the leading (277 -> 150) and the HRI band
	// (2 930 -> 2 200), both inherited from the Access report, and NOT from the text:
	// the solidarity price had just been raised to 11 pt for legibility.
	if got := template.Symbol.BarHeightUM; got != 11_375 {
		t.Errorf("barres = %d µm, want 11375 (91 dots exactement à 8 dots/mm)", got)
	}
	// 95 dots exactly: a whole number of dots, so no bar is a fraction of a scan line.
	if milliDots := template.Media.MilliDots(template.Symbol.BarHeightUM); milliDots%1000 != 0 {
		t.Errorf("hauteur de barres = %d milli-dots : elle doit être un compte entier de dots", milliDots)
	}

	// 3. The module has NOT moved, and the reason changed on 30/07/2026: it is no
	//    longer "A1 freezes it" but "no integer module lands in the GS1 range at this
	//    pitch either" (ADR-002). The number survived its own justification.
	if got := template.Symbol.ModuleMilliDots; got != 2_344 {
		t.Errorf("module = %d, want 2344", got)
	}
	if got := template.Symbol.TotalWidthMilliDots(); got != 264_872 {
		t.Errorf("hors-tout = %d milli-dots, want 264872 (33,109 mm)", got)
	}
	// And it is inside the GS1 range measured against the head, which is what rule 9
	// now checks rather than a milli-dot pair that meant nothing physical.
	if um := template.Symbol.ModuleUM(template.Media.DotsPerMM); um < GS1MinModuleUM || um > GS1MaxModuleUM {
		t.Errorf("module = %d µm, hors de la plage GS1 [%d ; %d]", um, GS1MinModuleUM, GS1MaxModuleUM)
	}

	// 4. The HRI survives: it is printed today, and dropping it would take away the
	//    cashier's fallback. Its BAND shrank on 30/07/2026 — 2 930 um was the descent
	//    of the "Code EAN13" font at 34 pt, inherited exactly like the module — but it
	//    stays above the guard descent, without which HeightUM's max() swings back and
	//    the 730 um are given away for nothing.
	if template.Symbol.HRIHeightUM != 2_700 {
		t.Errorf("HRI = %d µm, want 2700", template.Symbol.HRIHeightUM)
	}
	if template.Symbol.HRIHeightUM <= template.Symbol.GuardDescentUM {
		t.Errorf("bande HRI %d µm <= descente des gardes %d µm : le gain de hauteur s'évapore",
			template.Symbol.HRIHeightUM, template.Symbol.GuardDescentUM)
	}

	// 5. The truncation stays a documented decision, so the admin diagnostic stays
	//    informative rather than amber (ADR-003).
	if !template.TruncationAccepted {
		t.Error("truncation_accepted doit rester levé : la troncature est une décision, pas un défaut")
	}

	bottom, right := template.inkedExtent(2)
	t.Logf("contenu encré : %d,%03d dots de haut, %d,%03d de large (limites %d et %d)",
		bottom/1000, bottom%1000, right/1000, right%1000, ReferenceInkedHeightDots, ReferenceInkedWidthDots)
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
	if len(shipped) != 2 {
		t.Errorf("%d gabarits livrés, want 2 (identical, neutral_single)", len(shipped))
	}
	// Every shipped template names itself the way it is keyed.
	for name, template := range shipped {
		if template.Name != name {
			t.Errorf("le gabarit %q se nomme %q", name, template.Name)
		}
	}
}
