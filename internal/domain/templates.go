package domain

// The templates shipped with the binary.
//
// They are Go constructors rather than embedded JSON, and only until L4: the
// rendering engine is what turns a geometry into ink, and a template that no
// renderer has ever drawn is a guess with a schema. When L4 lands, these become
// files under internal/assets and this file becomes their loader — validated by the
// very same Template.Validate.
//
// # WHAT THE TEST PDF TAUGHT US
//
// reference/test_etiquette_EtataImprimer.pdf was decompressed and its content stream
// read. It CONFIRMS the six boxes of §7.2 to within 40 um — under a third of a dot —
// which is a strong result for a table transcribed from twips. It also shows two
// things §7.2 does not say:
//
//  1. THE SYMBOL DOES NOT START AT THE TOP OF ITS BOX. §7.2 gives the block origin
//     as y = 8 996 um, which is the top of the Access control CodeBarre. The glyph
//     actually drawn has its baseline at 21 326 um and rises 0.977 em above it, so
//     the bars begin at 9 604 um — 608 um lower. The block HEIGHT is exactly the
//     14 650 um stated; it is the origin that drifts.
//
//  2. THE TEXT SITS ON THE BARS, and by design. The cooperative runs two price
//     tiers, and there was no room for both above a symbol of that height. Measured:
//     the two prices eat 4 573 um of the 11 722 um of bars, leaving 8 341 um clean
//     across the full width.
//
// ADR-029 answers point 2 by stacking the text and putting the symbol below it. The
// bars become uniform AND taller in usable terms — 10 875 um against 8 341 — so
// point 1 becomes moot: the symbol is placed by its own measured top, not by a
// control box.
func neutralSingleGeometry() Template {
	return Template{
		Name: "weighing_neutral_single",
		// Measured on the bench of 28/07/2026, and in this order of authority: the
		// PRINTER first, then a caliper on the stock. The SATO driver of the parc is
		// configured « Largeur 35 mm / Hauteur 25 mm », and the labels themselves are
		// 38 × 25 mm — the 3 mm of difference are a deliberate margin. The 40 × 25.4 mm
		// this carried until then came from an old test PDF that was never produced by
		// the driver, and it made the station declare a print area FIVE MILLIMETRES
		// wider than the paper: the right-hand corner marks of the alignment self-test
		// fell off the label.
		Media: Media{
			WidthUM:   35_000, // 35 mm — the printable width the printer holds
			HeightUM:  25_000, // 25 mm — the full height of the stock
			DotsPerMM: 8,      // WS408; a WS412 would say 12
		},
		PrintableWidthUM:  35_000,
		PrintableHeightUM: 25_000,
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
				XUM:   0, YUM: 3_450, WidthUM: 15_000, HeightUM: 3_200,
				FontSizeUM: 3_175, AutoBold: true, Align: AlignLeft,
			},
			{
				Field: FieldPrimaryUnitPrice,
				XUM:   15_200, YUM: 3_450, WidthUM: 19_778, HeightUM: 3_200,
				FontSizeUM: 3_175, Bold: true, Framed: true, AutoBold: true, Align: AlignRight,
			},
			{
				// Present but conditional: on a single-tier grid it is not drawn, and
				// that is what makes mono-tarif work with no `if` in the renderer.
				Field: FieldSecondaryTotalPrice,
				XUM:   0, YUM: 7_100, WidthUM: 15_000, HeightUM: 2_600,
				FontSizeUM: 2_469, // 7 pt
				// auto_bold OFF, for the same reason as on the production label: the
				// source carries no FontWeight there, and bolding it would be the one
				// visible departure from the original.
				AutoBold: false, Align: AlignLeft, When: WhenMultiTier,
			},
			{
				Field: FieldPrimaryTotalPrice,
				XUM:   17_978, YUM: 6_900, WidthUM: 17_000, HeightUM: 3_800,
				FontSizeUM: 3_881, Bold: true, AutoBold: true, Align: AlignRight,
			},
			{
				Field: FieldBarcode,
				XUM:   0, YUM: 10_950, WidthUM: 34_978, HeightUM: 13_805,
				Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 0, YUM: 10_950,
			// The same fractional module as the production label: 2.344 dots =
			// 0.293 mm. Keeping it here means the neutral template exercises the one
			// case no printer language can express (A2).
			ModuleMilliDots: 2_344,
			BarHeightUM:     10_875,
			GuardDescentUM:  1_465,
			HRIHeightUM:     2_930,
		},
	}
}

// NeutralSingleTemplate is the mono-tarif template of the neutral profile.
//
// It is deliberately NOT a transposition of the production label: its boxes place
// ink, so it satisfies all nine hard rules.
func NeutralSingleTemplate() Template { return neutralSingleGeometry() }

// IdenticalTemplate is the production label: same product, same five fields, same
// module, same over-all width — with the bars UNIFORM instead of half-covered by
// text (ADR-029).
//
// WHY THIS DIFFERS FROM §7.2, AND WHY IT IS BETTER
//
// The current label superimposes the two prices ON the bars. That was a deliberate
// choice of the commissioning party, not an accident: the cooperative runs two price
// tiers, member and solidarity, and there was no room for both above a symbol of
// that height. It works at the till because a linear scanner needs only ONE clean
// scan line and finds it below the text.
//
// Measured on the test PDF, the cost of that choice:
//
//	bars declared                              11 722 um
//	  minus the solidarity price (7 pt)          1 192 um
//	  minus the member price (11 pt)             3 381 um
//	bars clean across the WHOLE width            8 341 um   -- 71 % of the declared
//
// Stacking the three text lines with a 350 um leading and placing the symbol BELOW
// them yields 10 875 um of bars — uniform over the full width, +30 % of usable
// height, and 54 % of the standard bar height at this magnification against 41 %
// today. The conformity improves.
//
// Nothing A1 freezes moves: the module stays at 0.293 mm (2 344 milli-dots) and the
// over-all width at 33.109 mm. ADR-003 forbids three specific remedies — changing
// the consumable, going to 305 dpi, altering the magnification — and this touches
// none of them. The bar height was already declared deliberately truncated; it is
// truncated differently, and better.
//
// Three technical consequences, all of them wanted:
//   - hard rule 5 becomes SATISFIABLE, so the nine rules finally apply to the
//     production template rather than being suspended for it;
//   - the on-screen preview becomes faithful without reservation;
//   - a scanner can no longer land on a scan line that is cut off at the top.
func IdenticalTemplate() Template {
	// The typographic grid, in micrometres. Three lines, 350 um of leading, then the
	// symbol. Every number below is derived from these four, so changing the leading
	// changes one constant.
	const (
		// 277 and not 350 since the bench of 28/07/2026 corrected the media: the label
		// is 25 mm, not the 25.4 an old test PDF suggested, and at 350 the symbol block
		// ended 93 um BELOW the paper. The leading is where those micrometres were
		// taken, because the alternative was shortening the bars, and the symbol is the
		// one thing ADR-003 protects: module, bar height, guard descent and HRI are all
		// untouched.
		//
		// It buys 1 dot of vertical margin and not zero, deliberately. The ±1 dot
		// arrows of §7.5 are how a volunteer corrects a roll that sits a hair off, and
		// the bench needed them on the very first evening; a template that filled the
		// label exactly would leave that adjustment nowhere to go, and hard rule 6
		// applies the offset BEFORE validating.
		leading = 277
		body9  = 3_175 //  9 pt
		body11 = 3_888 // 11 pt, measured on the PDF
	)
	line2 := Micrometers(body9 + leading)          //  3 525
	line3 := line2 + Micrometers(body9+leading)    //  7 050
	textBottom := line3 + Micrometers(body11)      // 10 938
	symbolTop := textBottom + Micrometers(leading) // 11 288

	// The two prices of line 3 share a BODY since 29/07/2026, on the commissioning
	// party's request: the solidarity price was 7 pt against 11 for the member price,
	// and it was too small to read on a label held at arm's length. Sharing the body
	// means sharing the baseline by construction, so the arithmetic that used to align
	// two different sizes is gone rather than left computing an identity.

	return Template{
		Name: "weighing_identical",
		// The same measurement as weighing_neutral_single, and for the same reason.
		Media: Media{
			WidthUM:   35_000,
			HeightUM:  25_000,
			DotsPerMM: 8,
		},
		PrintableWidthUM:  35_000,
		PrintableHeightUM: 25_000,
		TextThreshold:     0x68,
		// The truncation remains a documented decision of the commissioning party.
		// The flag keeps the admin screen INFORMATIVE about it rather than amber, so
		// that no contributor "corrects" it out of zeal in six months (ADR-003).
		TruncationAccepted: true,
		Elements: []Element{
			{
				Field: FieldProductName,
				XUM:   0, YUM: 0, WidthUM: 34_978, HeightUM: body9,
				FontSizeUM: body9, MinFontSizeUM: 2_200,
				AutoBold: true, Align: AlignLeft,
			},
			{
				Field: FieldQuantity,
				XUM:   0, YUM: line2, WidthUM: 15_000, HeightUM: body9,
				FontSizeUM: body9, AutoBold: true, Align: AlignLeft,
			},
			{
				// Bold and framed, as on the current label: the price per kilo is what a
				// customer checks against the shelf.
				Field: FieldPrimaryUnitPrice,
				XUM:   15_200, YUM: line2, WidthUM: 19_778, HeightUM: body9,
				// Framed: false since 29/07/2026, on the commissioning party's request.
				// The box around the price per kilo came from the Access report; the
				// price reads as well without it, and the ink it saves is the quiet zone
				// the symbol below never had to spare.
				FontSizeUM: body9, Bold: true, AutoBold: true, Align: AlignRight,
			},
			{
				Field: FieldSecondaryTotalPrice,
				XUM:   0, YUM: line3, WidthUM: 15_000, HeightUM: body11,
				FontSizeUM: body11,
				// auto_bold OFF: the source carries no FontWeight on LabelAPayer, so the
				// solidarity price prints in REGULAR. Bolding it would be the one visible
				// departure from the original, which A1 forbids.
				AutoBold: false, Align: AlignLeft, When: WhenMultiTier,
			},
			{
				Field: FieldPrimaryTotalPrice,
				XUM:   17_978, YUM: line3, WidthUM: 17_000, HeightUM: body11,
				FontSizeUM: body11, Bold: true, AutoBold: true, Align: AlignRight,
			},
			{
				Field: FieldBarcode,
				XUM:   0, YUM: symbolTop, WidthUM: 34_978, HeightUM: 10_875 + 2_930,
				Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 0, YUM: symbolTop,
			// UNCHANGED, and this is the point: 2.344 dots = 0.293 mm, 88.8 %
			// magnification, inside the GS1 range. A1 is respected where A1 speaks.
			ModuleMilliDots: 2_344,
			// 87 dots exactly at 8 dots/mm. Uniform over the full width, against
			// 8 341 um clean today.
			BarHeightUM: 10_875,
			// 5 modules, unchanged.
			GuardDescentUM: 1_465,
			// Measured on the PDF: 0.244 em at 34 pt. The HRI exists on the current
			// label and is never dropped — the cashier keeps her fallback.
			HRIHeightUM: 2_930,
		},
	}
}

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

// ShippedTemplates are the three templates the binary knows, by name.
func ShippedTemplates() map[string]Template {
	return map[string]Template{
		"weighing_identical":      IdenticalTemplate(),
		"weighing_neutral_single": NeutralSingleTemplate(),
		"weighing_integer_module": IntegerModuleTemplate(),
	}
}

// DefaultTemplateName is what config-lacagette.json selects.
const DefaultTemplateName = "weighing_identical"
