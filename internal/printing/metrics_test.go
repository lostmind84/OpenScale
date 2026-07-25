package printing

import (
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// This file holds the acceptance criterion of ADR-020, the one §7.3 calls "measurable
// and not subjective": rendered in Carlito at the body of the template, the five real
// strings of the label must come out within 1 % of the width Access printed them at in
// Calibri. Beyond that, the substitution moves the layout and "superimposed on a
// production label it coincides" stops being true.
//
// THE CALIBRI WIDTHS ARE MEASURED, NOT ESTIMATED, and the way they are obtained is
// what makes this test worth anything. Access splits each price field into two text
// runs — the amount in Calibri, the euro sign in a subset font — and each run carries
// its own absolute text matrix. The difference between two consecutive Tm values IS
// the advance width of the first run, in points, at the exact body printed. The three
// pairs below come from reference/test_etiquette_EtataImprimer.pdf, decompressed and
// read off its content stream.
//
// One subtlety costs the whole result if it is missed. A PDF TJ array interleaves
// strings with NUMBERS — [(S)-6(: 1)8(,)7(2)14( )] — and those numbers shift the pen
// by thousandths of an em, a POSITIVE value shrinking the advance. They are the
// kerning the generator applied, not a property of the font. Comparing the raw Tm
// delta against a font measurement therefore compares kerned text to unkerned text,
// and rejects Carlito at 1.18 % on the 7 pt field for a discrepancy the font is not
// responsible for. The adjustments are added back below, and the true deviations fall
// under a quarter of a percent.

// calibriRun is one text run of the production PDF, with everything needed to
// reconstruct the width of its glyphs alone.
type calibriRun struct {
	field  string
	text   string
	sizePt float64
	bold   bool
	// tmDelta is the distance to the NEXT run's text matrix, in points: the advance of
	// this run as Access actually laid it out.
	tmDelta float64
	// kerns are the numbers of the TJ array, in thousandths of an em. Positive shrinks.
	kerns []int
}

// glyphWidth reports the width of the run's glyphs with the generator's kerning
// removed, which is what a font measurement can legitimately be compared against.
func (r calibriRun) glyphWidth() float64 {
	total := 0
	for _, k := range r.kerns {
		total += k
	}
	return r.tmDelta + float64(total)/1000.0*r.sizePt
}

// productionRuns are the three fields of the reference PDF that carry a following run,
// and are therefore exactly measurable. They cover both weights and the three bodies
// of the template — 7, 9 and 11 pt — including the 7 pt one at the legibility floor.
var productionRuns = []calibriRun{
	{
		field: "primary_unit_price", text: "A: 4,32 ", sizePt: 8.9993, bold: true,
		tmDelta: 99.857 - 71.899, kerns: []int{6, -4, 3},
	},
	{
		field: "secondary_total_price", text: "S: 1,2 ", sizePt: 7.0075, bold: false,
		tmDelta: 35.349 - 18.455, kerns: []int{-6, 8, 7, 14},
	},
	{
		field: "primary_total_price", text: "A: 1,08 ", sizePt: 11.015, bold: true,
		tmDelta: 111.21 - 76.65, kerns: []int{-2, -6, -7, -6},
	},
}

// measurementDPI is deliberately huge.
//
// opentype.NewFace quantises to an integer pixel-per-em, so measuring at the real
// 203.2 dpi would fold a rounding of up to half a pixel into a 17-point string — a
// tenth of the tolerance being tested, spent on an artefact of the rasterizer rather
// than on the font. At a hundred times the resolution the quantisation is a hundredth
// of a percent, and what is left is the metric difference this test is about.
const measurementDPI = 7200

// measurePoints reports the advance of s in typographic points.
func measurePoints(t *testing.T, face font.Face, s string) float64 {
	t.Helper()
	const scale = measurementDPI / 72.0
	return float64(font.MeasureString(face, s)) / 64 / scale
}

// TestCarlitoKeepsTheCalibriLayout is the acceptance criterion of ADR-020.
func TestCarlitoKeepsTheCalibriLayout(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	const tolerance = 1.0 // per cent, §7.3
	for _, run := range productionRuns {
		t.Run(run.field, func(t *testing.T) {
			parsed, err := library.Parsed(Carlito, run.bold)
			if err != nil {
				t.Fatalf("police : %v", err)
			}
			face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
				Size: run.sizePt, DPI: measurementDPI,
			})
			if err != nil {
				t.Fatalf("fonte à %g pt : %v", run.sizePt, err)
			}
			defer face.Close()

			want := run.glyphWidth()
			got := measurePoints(t, face, run.text)
			deviation := (got - want) / want * 100

			if deviation > tolerance || deviation < -tolerance {
				t.Errorf("%q au corps %.2f : Carlito %.3f pt contre Calibri %.3f pt, "+
					"soit %+.2f %% — au-delà de %g %% la substitution déplace la mise en "+
					"page et « superposé à une étiquette de production, il coïncide » "+
					"cesse d'être vrai (ADR-020)",
					run.text, run.sizePt, got, want, deviation, tolerance)
			}
			t.Logf("%q : Calibri %.3f pt · Carlito %.3f pt · %+.2f %%",
				run.text, want, got, deviation)
		})
	}
}

// TestTheKerningCorrectionIsWhatMakesTheCriterionPass documents the trap, and fails if
// somebody ever "simplifies" glyphWidth back to the raw Tm delta.
//
// Without the correction the 7 pt field deviates by 1.18 % and Carlito is rejected —
// for kerning Access applied, which no font metric can account for. A test that
// silently started comparing kerned text to unkerned text would look like a font
// regression and send the next reader hunting for a substitute that does not exist.
func TestTheKerningCorrectionIsWhatMakesTheCriterionPass(t *testing.T) {
	const seven = 1 // secondary_total_price, the 7 pt field
	run := productionRuns[seven]

	if run.glyphWidth() <= run.tmDelta {
		t.Fatalf("la correction de crénage devrait ÉLARGIR ce champ : %.3f contre %.3f — "+
			"les nombres du tableau TJ y sont globalement positifs, donc Access a resserré "+
			"le texte", run.glyphWidth(), run.tmDelta)
	}

	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()
	parsed, err := library.Parsed(Carlito, run.bold)
	if err != nil {
		t.Fatalf("police : %v", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: run.sizePt, DPI: measurementDPI})
	if err != nil {
		t.Fatalf("fonte : %v", err)
	}
	defer face.Close()

	got := measurePoints(t, face, run.text)
	naive := (got - run.tmDelta) / run.tmDelta * 100
	corrected := (got - run.glyphWidth()) / run.glyphWidth() * 100

	if naive <= 1.0 {
		t.Errorf("la mesure naïve donne %+.2f %%, donc elle passerait le critère et ce "+
			"test ne protège plus rien : vérifier que les nombres de crénage sont toujours "+
			"ceux du PDF", naive)
	}
	if corrected > 0.5 || corrected < -0.5 {
		t.Errorf("mesure corrigée %+.2f %% : attendue bien à l'intérieur du critère", corrected)
	}
	t.Logf("champ à 7 pt — mesure naïve %+.2f %% (rejetterait Carlito à tort) · "+
		"crénage du PDF retiré %+.2f %%", naive, corrected)
}

// TestFacesAreMemoisedAndClosedTogether pins the second engineering correction of §7.3.
func TestFacesAreMemoisedAndClosedTogether(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}

	// The automatic reduction loop asks for the same face over and over.
	for i := 0; i < 20; i++ {
		if _, err := library.Face(Carlito, 3175, 8, false); err != nil {
			t.Fatalf("fonte : %v", err)
		}
	}
	if got := library.FaceCount(); got != 1 {
		t.Errorf("%d fontes pour vingt demandes identiques, attendu 1 — la boucle de "+
			"réduction en crée jusqu'à vingt par champ et par étiquette", got)
	}

	// Same body, other weight: a DIFFERENT face, or the automatic switch to Bold below
	// 20 dots of em would hand one style back for the other.
	if _, err := library.Face(Carlito, 3175, 8, true); err != nil {
		t.Fatalf("fonte grasse : %v", err)
	}
	if got := library.FaceCount(); got != 2 {
		t.Errorf("%d fontes après avoir demandé le gras au même corps, attendu 2 — une clé "+
			"réduite au seul ppem confondrait Regular et Bold", got)
	}

	if err := library.Close(); err != nil {
		t.Errorf("fermeture : %v", err)
	}
	if got := library.FaceCount(); got != 0 {
		t.Errorf("%d fontes après fermeture, attendu 0", got)
	}
	if _, err := library.Face(Carlito, 3175, 8, false); err == nil {
		t.Error("une bibliothèque fermée a rendu une fonte : elle serait adossée à un " +
			"rastériseur libéré")
	}
}

// TestAnUnknownFontIsRefusedRatherThanSubstituted: a template naming a font the binary
// does not carry must fail loudly. Substituting silently is how a label starts printing
// in the wrong width without anybody being told.
func TestAnUnknownFontIsRefusedRatherThanSubstituted(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	if _, err := library.Face("calibri", 3175, 8, false); err == nil {
		t.Fatal("« calibri » a été acceptée : c'est précisément la police qu'on ne peut " +
			"pas redistribuer, et l'accepter en silence rendrait une étiquette différente " +
			"selon le poste")
	}
}

// TestDejaVuIsNarrowerThanCarlito is why ADR-020 rejected it, checked rather than
// asserted: on the same string at the same body it does not occupy the same width, so
// it cannot be the font of the production label.
func TestDejaVuIsNarrowerThanCarlito(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	widths := map[Font]float64{}
	for _, family := range []Font{Carlito, DejaVuSansCondensed} {
		parsed, err := library.Parsed(family, false)
		if err != nil {
			t.Fatalf("police %s : %v", family, err)
		}
		face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 9, DPI: measurementDPI})
		if err != nil {
			t.Fatalf("fonte %s : %v", family, err)
		}
		widths[family] = measurePoints(t, face, "A: 4,32 €/kg")
		face.Close()
	}

	deviation := (widths[DejaVuSansCondensed] - widths[Carlito]) / widths[Carlito] * 100
	if deviation > -1.0 && deviation < 1.0 {
		t.Errorf("DejaVu Sans Condensed est à %+.2f %% de Carlito : si les deux étaient "+
			"interchangeables, le choix de police d'ADR-020 n'aurait aucun objet et ce "+
			"test ne dirait plus rien", deviation)
	}
	t.Logf("DejaVu Sans Condensed est à %+.2f %% de Carlito sur la même chaîne : c'est "+
		"pourquoi elle est la police des gabarits NEUTRES et d'aucun champ de "+
		"l'étiquette de production", deviation)
}

// compile-time guard: fixed.Int26_6 is what MeasureString returns, and the conversion
// above depends on it.
var _ = fixed.Int26_6(0)
