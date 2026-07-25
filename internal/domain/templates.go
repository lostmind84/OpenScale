package domain

// The templates shipped with the binary.
//
// They are Go constructors rather than embedded JSON, and only until L4: the
// rendering engine is what turns a geometry into ink, and a template that no
// renderer has ever drawn is a guess with a schema. When L4 lands, these become
// files under internal/assets and this file becomes their loader — validated by the
// very same Template.Validate.
//
// # WHY THE PRODUCTION TEMPLATE IS NOT HERE YET
//
// weighing_identical reproduces a measured object, and the measurement was done:
// reference/test_etiquette_EtataImprimer.pdf was decompressed and its content
// stream read. It CONFIRMS the six boxes of §7.2 to within 40 um — under a third of
// a dot — which is a strong result for a table transcribed from twips.
//
// It also shows two things §7.2 does not say, and both matter to L4:
//
//  1. THE SYMBOL DOES NOT START AT THE TOP OF ITS BOX. §7.2 gives the block origin
//     as y = 8 996 um, which is the top of the Access control CodeBarre. The glyph
//     actually drawn has its baseline at y = 21 326 um and rises 0.977 em above it,
//     so the bars begin at 9 604 um — 608 um lower. The block height is exactly the
//     14 650 um the document states; it is the ORIGIN that is off. The inked content
//     therefore reaches 194 dots, not 189, and rule 3 passes with 8 dots of margin
//     rather than 13.
//
//  2. THE BOXES OF THE PRODUCTION LABEL OVERLAP EACH OTHER, and overlap the symbol
//     box. That is not a defect of the label: an Access report control carries its
//     line spacing inside its height, and the text inside it is short and aligned
//     left or right, so the INK does not collide. But rules 5 and 8 bear on boxes,
//     because ink extent is only known once a font has measured a string — which is
//     L4's job, not L2's.
//
// Transcribing those boxes literally would therefore produce a template that fails
// its own hard rules. The answer is not to weaken the rules: it is to declare a
// NATIVE geometry that places ink, which is precisely the test CLAUDE.md prescribes
// — trace the element back to the legacy application, then ask whether it would
// exist starting from a blank page. Overlapping boxes with built-in leading would
// not.
//
// So L4 gets the measurements, and L2 ships the validator plus a template that
// exercises it.
func neutralSingleGeometry() Template {
	return Template{
		Name: "weighing_neutral_single",
		Media: Media{
			WidthUM:   40_000, // 40 mm
			HeightUM:  25_400, // 25.4 mm
			DotsPerMM: 8,      // WS408; a WS412 would say 12
		},
		PrintableWidthUM:  40_000,
		PrintableHeightUM: 25_400,
		TextThreshold:     0x68,
		// The neutral template makes no claim about a truncated symbol: it is not
		// reproducing anything, so an out-of-standard symbol here IS worth a warning.
		TruncationAccepted: false,
		Elements: []Element{
			{
				Field: FieldProductName,
				XUM:   0, YUM: 0, WidthUM: 34_978, HeightUM: 3_200,
				FontSizeUM: 3_175, MinFontSizeUM: 2_200, // 9 pt, reducible to ~6 pt
				AutoBold: true, Align: AlignLeft,
			},
			{
				Field: FieldQuantity,
				XUM:   0, YUM: 3_400, WidthUM: 15_000, HeightUM: 3_200,
				FontSizeUM: 3_175, AutoBold: true, Align: AlignLeft,
			},
			{
				Field: FieldPrimaryUnitPrice,
				XUM:   15_200, YUM: 3_400, WidthUM: 19_778, HeightUM: 3_200,
				FontSizeUM: 3_175, Bold: true, Framed: true, AutoBold: true, Align: AlignRight,
			},
			{
				// Present but conditional: on a single-tier grid it is not drawn, and
				// that is what makes mono-tarif work with no `if` in the renderer.
				Field: FieldSecondaryTotalPrice,
				XUM:   0, YUM: 6_800, WidthUM: 15_000, HeightUM: 2_600,
				FontSizeUM: 2_469, // 7 pt
				// auto_bold OFF, for the same reason as on the production label: the
				// source carries no FontWeight there, and bolding it would be the one
				// visible departure from the original.
				AutoBold: false, Align: AlignLeft, When: WhenMultiTier,
			},
			{
				Field: FieldPrimaryTotalPrice,
				XUM:   17_978, YUM: 6_600, WidthUM: 17_000, HeightUM: 3_800,
				FontSizeUM: 3_881, Bold: true, AutoBold: true, Align: AlignRight,
			},
			{
				Field: FieldBarcode,
				XUM:   0, YUM: 10_450, WidthUM: 34_978, HeightUM: 14_650,
				Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 0, YUM: 10_450,
			// The same fractional module as the production label: 2.344 dots =
			// 0.293 mm. Keeping it here means the neutral template exercises the one
			// case no printer language can express (A2).
			ModuleMilliDots: 2_344,
			BarHeightUM:     11_720,
			GuardDescentUM:  1_465,
			HRIHeightUM:     2_930,
		},
	}
}

// NeutralSingleTemplate is the mono-tarif template of the neutral profile.
//
// It is the only template L2 ships, and it is deliberately NOT a transposition of
// the production label: its boxes place ink, so it satisfies all nine hard rules.
func NeutralSingleTemplate() Template { return neutralSingleGeometry() }

// IntegerModuleTemplate is gabarit B of §7.6: the same layout with a module of
// exactly 2 dots.
//
// Its purpose is a measurement, not a delivery. At an integer module every bar is
// exactly 2 dots wide, so the bars are rigorously uniform — and that uniformity
// comes from the MODULE, not from the printer language, which is why the hypothesis
// needs no special code, only a second template.
//
// It is 75.8 % magnification, BELOW the 80 % GS1 floor. Whatever the field count
// says, adopting it would be two explicit decisions of the commissioning party, not
// a conclusion drawn by a test (§7.6).
func IntegerModuleTemplate() Template {
	t := neutralSingleGeometry()
	t.Name = "weighing_integer_module"
	t.Symbol.ModuleMilliDots = 2_000
	return t
}

// ShippedTemplates are the templates the binary knows, by name.
func ShippedTemplates() map[string]Template {
	return map[string]Template{
		"weighing_neutral_single": NeutralSingleTemplate(),
		"weighing_integer_module": IntegerModuleTemplate(),
	}
}
