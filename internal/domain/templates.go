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
// bars become uniform AND taller in usable terms — 11 375 um against 8 341 — so
// point 1 becomes moot: the symbol is placed by its own measured top, not by a
// control box.
//
// The 10 875 um ADR-029 settled on became 11 375 on 30/07/2026, when the commissioning
// party reopened A1: with the over-all width no longer a number to preserve, the
// leading and the HRI band stopped being untouchable too. See IdenticalTemplate.
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
				XUM:   0, YUM: 10_950, WidthUM: 34_978, HeightUM: 11_125 + 2_700,
				Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 0, YUM: 10_950,
			// The same fractional module as the production label: 2.344 dots =
			// 0.293 mm. Keeping it here means the neutral template exercises the one
			// case no printer language can express (A2).
			ModuleMilliDots: 2_344,
			// 89 dots exactly. The neutral profile reproduces nothing, so it has even
			// less reason than the production template to carry a bar height and an HRI
			// band inherited from an Access font: it takes the same 30/07/2026 budget.
			BarHeightUM:    11_125,
			GuardDescentUM: 1_465,
			HRIHeightUM:    2_700,
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
// Stacking the three text lines and placing the symbol BELOW them yields 11 375 um of
// bars — uniform over the full width, +36 % of usable height, and 56 % of the standard
// bar height at this magnification against 41 % today. The conformity improves.
//
// # WHAT CHANGED ON 30/07/2026, AND WHY THE MODULE DID NOT
//
// The commissioning party reopened A1. ADR-003 had forbidden three remedies by name —
// changing the consumable, going to 305 dpi, altering the magnification — and that
// interdiction is lifted. What followed was NOT a free-for-all:
//
//   - the MODULE and the over-all width did not move, and no longer because A1 froze
//     them: no INTEGER module lands inside the GS1 range at this pitch either, at 203,
//     300 or 305 dpi, so the fractional 0.293 mm is not inherited but necessary
//     (ADR-002, amendment). The number outlived its justification;
//   - the TEXT did not move: the solidarity price had just been raised to 11 pt for
//     legibility at arm's length, one day earlier;
//   - the LEADING and the HRI BAND did move, because both were inherited numbers with
//     nothing behind them, and together they were worth 500 um of bars.
//
// A fully conforming symbol is out of reach on this stock by arithmetic and not by
// decision: 22 850 um of bars at 100 % against a budget that tops out near 11 400
// leaves the truncation exactly where it was. Only a taller consumable moves that
// (§7.7), and that is a decision, not a calculation.
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
		// 150 and not 277 since 30/07/2026, when the commissioning party reopened A1
		// and the bar height stopped being a number to preserve. 277 was itself the
		// residue of a budget that had to give 93 um back to the paper; 150 is the floor
		// ADR-029 had already named as its fallback, and the 381 um it frees go to the
		// bars rather than back to the paper.
		leading = 150
		body9   = 3_175 //  9 pt
		body11  = 3_888 // 11 pt, measured on the PDF
	)
	// These four were commented as 3 525 / 7 050 / 10 938 / 11 288 until 30/07/2026:
	// those were the values at leading = 350, and the bench had lowered it to 277
	// without the comments following. Section 7.4 of the architecture had copied the
	// stale origin, and from it derived an inked bottom of 200.7 dots — a template hard
	// rule 3 rejects. The code was right the whole time; only the comments lied.
	line2 := Micrometers(body9 + leading)          //  3 325
	line3 := line2 + Micrometers(body9+leading)    //  6 650
	textBottom := line3 + Micrometers(body11)      // 10 538
	symbolTop := textBottom + Micrometers(leading) // 10 688

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
				XUM:   0, YUM: symbolTop, WidthUM: 34_978, HeightUM: 11_375 + 2_700,
				Align: AlignLeft,
			},
		},
		Symbol: SymbolGeometry{
			XUM: 0, YUM: symbolTop,
			// UNCHANGED, and this is the point: 2.344 dots = 0.293 mm, 88.8 %
			// magnification, inside the GS1 range. Reopening A1 did not move it,
			// because no INTEGER module lands in that range at this pitch either
			// (ADR-002, amendment of 30/07/2026) — the fractional module is not
			// inherited, it is necessary.
			ModuleMilliDots: 2_344,
			// 91 dots exactly at 8 dots/mm, against 87 until 30/07/2026. The extra 500 um
			// came from the leading (381) and the HRI band (230) — inherited numbers both
			// — and NOT from the text: the commissioning party raised the solidarity
			// price to 11 pt on 29/07 for legibility at arm's length, and that decision
			// is a day old.
			BarHeightUM: 11_375,
			// 5 modules, unchanged.
			GuardDescentUM: 1_465,
			// 2 700 and not 2 930 since 30/07/2026. The old value was the DESCENT OF A
			// FONT — 0.244 em of "Code EAN13" at 34 pt — inherited exactly like the
			// module, and GS1 fixes no numeric height for the HRI, only that it be
			// legible. So the band was reopened with the rest, and MEASURED rather than
			// argued, which is what stopped it here:
			//
			//	band 2 930 (23 dots) -> em 3 699, ink 21 dots, 2 dots of clearance
			//	band 2 700 (22 dots) -> em 3 699, ink 21 dots, 1 dot  of clearance
			//	band 2 625 (21 dots) -> em 3 699, ink 21 dots, 0 — digits touch the bars
			//	band 2 200 (18 dots) -> em 3 261, ink 18 dots, 0 — and 12 % smaller
			//
			// FitHRIFace is bound by the CELL (14 dots wide) and not by the band, so
			// shrinking the band buys nothing until 22 dots and then costs the clearance
			// row, then the digit size. Only 2 of the 23 dots were ever free, and one of
			// them has to stay: a cashier reading 13 digits welded to the bar bottoms is
			// the fallback made worse. The band must also stay above GuardDescentUM, or
			// HeightUM's max() swings back to the descent and the gain evaporates.
			HRIHeightUM: 2_700,
		},
	}
}

// ShippedTemplates are the two templates the binary knows, by name.
//
// There were three until 30/07/2026. "weighing_integer_module" — gabarit B of §7.6 —
// carried a module of exactly 2 dots to test the hypothesis that rigorously uniform
// bars scan better than the 2/3 alternation of a fractional module. It is retired,
// and not because the hypothesis was disproved: because the price of testing it was
// a module at 75.8 % magnification, BELOW the 80 % GS1 floor, and rule 9 now refuses
// exactly that. A test whose winning arm cannot be adopted is not a test.
//
// What the hypothesis was really about survives, and moved: the uniformity comes from
// the MODULE, not from the printer language, and the drawing invariant it exercised
// is kept as a unit test of DrawEAN13 rather than as a shippable template.
func ShippedTemplates() map[string]Template {
	return map[string]Template{
		"weighing_identical":      IdenticalTemplate(),
		"weighing_neutral_single": NeutralSingleTemplate(),
	}
}

// DefaultTemplateName is what config-lacagette.json selects.
const DefaultTemplateName = "weighing_identical"
