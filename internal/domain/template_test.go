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
