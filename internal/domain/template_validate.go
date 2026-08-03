package domain

import "fmt"

// This file holds the NINE HARD RULES of §7.5 -- everything a label layout has to
// satisfy before it may be loaded, measured against the head that will print it.
//
// A template naming a field that does not exist, or placing one over a quiet zone,
// is refused when it is LOADED and never when a customer is waiting. The geometry
// the rules read is template.go's; what they decide about it is here.

// Validate reports every hard rule the template breaks ON THE HEAD OF THE FLEET; an
// empty slice means the template may be loaded.
//
// tierCount decides which conditional elements are active, because rules 3, 5 and 8
// are about what is actually INKED: a mono-tarif station must not be refused a
// template because of a field that will never be drawn on it.
//
// A station validates against the head ITS OWN DRIVER declares (ValidateOn); this form
// is for every caller that has no descriptor in hand, and it answers for the WS408.
func (t *Template) Validate(tierCount int) []Fault {
	return t.ValidateOn(ReferenceHead(), tierCount)
}

// ValidateOn reports every hard rule the template breaks ON THIS HEAD.
//
// Rules 3 and 4 are the two that need one: they bound the ink by what the head can
// print, and holding that bound as a constant of the core made every station whose
// printer is not the WS408 of the parc fail its own validation at start-up.
//
// A head that declares nothing is measured against ReferenceHead, so a caller with no
// descriptor in hand — a test, a preview — validates exactly what it validated before.
func (t *Template) ValidateOn(head PrinterCapabilities, tierCount int) []Fault {
	head = head.orReference()
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

	// The template and the head must count dots at the SAME pitch, and this is where a
	// template measured for another one is caught.
	//
	// Symbol.ModuleMilliDots is the ONE length of a template expressed in units of
	// resolution, deliberately: 0.293 mm is 2.344 dots, and that fractional module is
	// the whole technical point of arbitration A2. The unit stays, so the AMBIGUITY has
	// to go — the same 2 344 milli-dots print 0.293 mm on a WS408 and 0.195 mm on a
	// WS412, under every GS1 floor, and no byte of the frame says so. The label simply
	// comes out wrong.
	pitchAgrees := head.DotsPerMM == t.Media.DotsPerMM
	if !pitchAgrees {
		fail("media.dots_per_mm",
			"le gabarit est mesuré pour une tête de %g dots/mm et la tête d'impression en fait "+
				"%g dots/mm : à ce module le symbole sortirait à un autre grandissement",
			t.Media.DotsPerMM, head.DotsPerMM)
	}

	inkedWidth := int64(head.InkedWidthDots) * 1000
	inkedHeight := int64(head.InkedHeightDots) * 1000

	// Rule 9, checked first: a template with an absurd module or an unreadable type
	// size produces cascades of geometric faults, and naming the cause is kinder
	// than naming ten consequences.
	//
	// The module is measured against the pitch the HEAD declares, so a template whose
	// module is legal here is legal in millimetres, not merely in dots. Skipped when
	// the two pitches disagree: the module would then be read at a resolution this
	// template was never measured for, and naming ten consequences of a cause already
	// named helps nobody.
	if pitchAgrees {
		if um := shifted.Symbol.ModuleUM(head.DotsPerMM); um < GS1MinModuleUM || um > GS1MaxModuleUM {
			fail("symbol.module_milli_dots",
				"%d milli-dots valent %d µm à %g dots/mm, soit un grandissement de %.1f %% : "+
					"hors de la plage GS1 [%d ; %d] µm (80 %% à 200 %% de la nominale de 330 µm)",
				shifted.Symbol.ModuleMilliDots, um, head.DotsPerMM,
				shifted.Symbol.Magnification(head.DotsPerMM)*100, GS1MinModuleUM, GS1MaxModuleUM)
		}
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

	// Rules 3 and 4 compare dots to dots, so they only mean anything once the two
	// pitches agree. Enumerating them on top of a mismatch would name ten consequences
	// of a cause already named, which is the reasoning the zero resolution above
	// follows.
	if pitchAgrees {
		// Rule 3: THE INKED CONTENT FITS THE GEOMETRY OF THE EXISTING LABEL.
		bottom, right := shifted.inkedExtent(tierCount)
		if bottom > inkedHeight {
			fail("inked_content", "le contenu encré descend à %s dots, au-delà des %d dots de hauteur encrée de l'étiquette",
				formatMilliDots(bottom), head.InkedHeightDots)
		}
		if right > inkedWidth {
			fail("inked_content", "le contenu encré s'étend à %s dots, au-delà des %d dots de largeur encrée de l'étiquette",
				formatMilliDots(right), head.InkedWidthDots)
		}

		// Rule 4: the over-all width of the symbol, quiet zones included.
		if width := shifted.Symbol.TotalWidthMilliDots(); width > inkedWidth {
			fail("symbol.module_milli_dots",
				"le hors-tout du symbole (113 modules = %s dots) dépasse les %d dots de largeur encrée",
				formatMilliDots(width), head.InkedWidthDots)
		}
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
