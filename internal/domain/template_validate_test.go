// This file holds the NINE HARD RULES of §7.5, one test per rule, plus the two
// things the rules are measured against: the head the DRIVER declares, and the
// pitch the template was measured for.
//
// A template and a head of different pitches are refused BY NAME, and that is the
// whole of ADR-045: the same 2 344 milli-dots print 0,293 mm on a WS408 and
// 0,195 mm on a WS412, under every GS1 floor, with no byte of the frame saying so.

package domain

import (
	"strings"
	"testing"
)

// TestRulesThreeAndFourBearOnTheHeadAndNotOnAConstant: a station whose printer is not
// the WS408 of the parc used to fail its own validation AT START-UP, on a template
// nobody could make it accept — the two figures rules 3 and 4 compare to were held by
// the core.
func TestRulesThreeAndFourBearOnTheHeadAndNotOnAConstant(t *testing.T) {
	template := twelveDotTemplate()
	if faults := template.ValidateOn(ws412Head(), 2); len(faults) != 0 {
		t.Fatalf("un gabarit à 12 dots/mm sur une tête à 12 dots/mm est refusé :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
	// The same layout is 419,736 dots wide at that pitch, so a head that inks 280 dots
	// refuses it — which is the proof that the DECLARATION is what is read.
	if faults := template.ValidateOn(ReferenceHead(), 2); len(faults) == 0 {
		t.Fatal("un gabarit à 12 dots/mm passe sur une WS408")
	}
}

// TestRuleThreeMeasuresTheInkAgainstTheDeclaredHead: a head that inks a smaller area
// refuses a template the WS408 accepts, and it does so without a line of the core
// naming a number of dots.
func TestRuleThreeMeasuresTheInkAgainstTheDeclaredHead(t *testing.T) {
	narrow := PrinterCapabilities{DotsPerMM: 8, InkedWidthDots: 200, InkedHeightDots: 200}
	template := NeutralSingleTemplate()

	if faults := template.Validate(2); len(faults) != 0 {
		t.Fatalf("le gabarit livré doit passer sur la tête du parc : %v", faults)
	}
	faults := template.ValidateOn(narrow, 2)
	if !hasFault(faults, "inked_content") && !hasFault(faults, "symbol.module_milli_dots") {
		t.Fatalf("une tête plus étroite accepte le gabarit : %v", faultFields(faults))
	}
	for _, fault := range faults {
		if strings.Contains(fault.Message, "200 dots") {
			return
		}
	}
	t.Errorf("aucun message ne nomme la largeur encrée déclarée par la tête :\n%s",
		strings.Join(fieldsOf(faults), "\n"))
}

// TestATemplateAndAHeadOfDifferentPitchesAreRefusedByName.
//
// A template that does not say which head it was measured for changes magnification in
// SILENCE: the module is the one length expressed in dots, so the same 2 344 milli-dots
// print 0.293 mm on a WS408 and 0.195 mm on a WS412 — under every GS1 floor, and no
// byte of the frame says so. The refusal names both figures, because a volunteer has to
// know which of the two to change.
func TestATemplateAndAHeadOfDifferentPitchesAreRefusedByName(t *testing.T) {
	template := twelveDotTemplate()
	faults := template.ValidateOn(ReferenceHead(), 2)

	fault := findFault(faults, "media.dots_per_mm")
	if fault == nil {
		t.Fatalf("l'attelage gabarit/tête n'est pas refusé : %v", faultFields(faults))
	}
	for _, figure := range []string{"12 dots/mm", "8 dots/mm"} {
		if !strings.Contains(fault.Message, figure) {
			t.Errorf("le message ne nomme pas %s : %q", figure, fault.Message)
		}
	}
	// And it stands INSTEAD of the geometric cascade it would cause: rules 3 and 4
	// compare dots counted at two different pitches, so their verdict would be noise.
	if hasFault(faults, "inked_content") {
		t.Errorf("les conséquences sont énumérées avec la cause :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestAHeadThatDeclaresNothingIsMeasuredAgainstTheParc: `preview` inks no paper, so it
// declares no geometry. The rules must not be suspended for it — the preview screen is
// where a volunteer sets the ±1 dot offsets, and one that accepted everything would let
// them settle on an adjustment the production driver refuses.
func TestAHeadThatDeclaresNothingIsMeasuredAgainstTheParc(t *testing.T) {
	inksNothing := PrinterCapabilities{Raster: true}
	template := NeutralSingleTemplate()
	template.Symbol.YUM = 20_000 // squarely past the bottom of the label

	silent := template.ValidateOn(inksNothing, 2)
	parc := template.ValidateOn(ReferenceHead(), 2)
	if len(silent) != len(parc) || len(silent) == 0 {
		t.Fatalf("un driver qui n'imprime rien ne valide pas comme la tête du parc :\n%s\n--- contre ---\n%s",
			strings.Join(fieldsOf(silent), "\n"), strings.Join(fieldsOf(parc), "\n"))
	}
	for i := range silent {
		if silent[i].String() != parc[i].String() {
			t.Errorf("faute %d : %s\nattendu : %s", i, silent[i], parc[i])
		}
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
		// 2112 milli-dots = 264 um at 8 dots/mm, the GS1 floor; 5280 = 660 um, the
		// ceiling. One milli-dot outside either is one micrometre outside the range,
		// which is what rule 9 now measures.
		{"module sous le plancher GS1", func(t *Template) { t.Symbol.ModuleMilliDots = 2_100 }, "symbol.module_milli_dots"},
		{"module au-dessus du plafond GS1", func(t *Template) { t.Symbol.ModuleMilliDots = 5_300 }, "symbol.module_milli_dots"},
		// Gabarit B, retired on 30/07/2026: 2 dots is 250 um, 75.8 % magnification.
		// It used to be a SHIPPED template that Validate accepted.
		{"module entier de 2 dots", func(t *Template) { t.Symbol.ModuleMilliDots = 2_000 }, "symbol.module_milli_dots"},
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

// TestRuleNineMeasuresTheModuleInMicrometresNotDots is what the [1500, 6000] milli-dot
// pair could not do. The SAME template is conforming on one head and not on the other,
// and a rule written in units of resolution cannot tell them apart.
func TestRuleNineMeasuresTheModuleInMicrometresNotDots(t *testing.T) {
	sym := SymbolGeometry{ModuleMilliDots: 2_344}
	if um := sym.ModuleUM(8); um != 293 {
		t.Errorf("2 344 milli-dots à 8 dots/mm = %d µm, attendu 293", um)
	}
	if um := sym.ModuleUM(12); um != 195 {
		t.Errorf("2 344 milli-dots à 12 dots/mm = %d µm, attendu 195", um)
	}
	// 293 um is 88.8 %, inside the range; 195 um is 59.1 %, under every GS1 floor —
	// and the old bounds accepted 2 344 on both heads without a word.
	if m := sym.Magnification(8); m < 0.887 || m > 0.889 {
		t.Errorf("grandissement à 8 dots/mm = %.4f, attendu ~0,888", m)
	}
	if sym.ModuleUM(8) < GS1MinModuleUM {
		t.Error("293 µm doit être dans la plage GS1")
	}
	if sym.ModuleUM(12) >= GS1MinModuleUM {
		t.Error("195 µm doit être sous le plancher GS1")
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
