package printing

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"openscale/internal/domain"
)

// The six non-regression tests of §7.4, plus what they need to be worth anything: the
// frozen modules, the absence of cumulative drift, the width of the bars, the whole module,
// the golden, and the block with its HRI band still there.
//
// What DrawEAN13 refuses is in symbol_refusals_test.go; the independent decoder that reads
// a symbol back out of its pixels is in symbol_decoder_test.go.
//
// # REGENERATING THE GOLDEN
//
// The golden of TestTheSymbolBlockMatchesItsGolden is a PNG under
// testdata/golden/. It is rewritten by:
//
//	go test ./internal/printing/ -run TestTheSymbolBlockMatchesItsGolden -update
//
// Regenerate it ONLY when the geometry of the symbol changed on purpose, and say so
// in the commit: the file is the pixel-level record of what §7.4 draws, and a golden
// updated to make a test pass is a test that no longer tests anything. The pinned
// toolchain and the exact x/image version of go.mod (§16.4) are what keep it stable
// across machines.

var update = flag.Bool("update", false,
	"réécrire les golden de internal/printing/testdata/golden")

// referenceModules are the 95 modules of referenceCode, FROZEN.
//
// They were obtained once and are checked here by a decoder written from the
// specification, in this file, with its own tables: a golden that is only a recorded
// output agrees with a wrong encoder, and would keep agreeing with it forever.
const referenceModules = "10101000110001011011110100011010010011001100101010111001011001101101100100001010100001001110101"

// --- 1. The frozen modules -------------------------------------------------

// TestTheFrozenModulesDecodeBackToTheReferenceCode is test 1 of §7.4.
func TestTheFrozenModulesDecodeBackToTheReferenceCode(t *testing.T) {
	if len(referenceModules) != barModules {
		t.Fatalf("la constante figée fait %d caractères, il en faut %d", len(referenceModules), barModules)
	}

	decoded, err := decodeSymbol(referenceModules)
	if err != nil {
		t.Fatalf("le décodeur indépendant refuse la chaîne figée : %v", err)
	}
	if decoded != referenceCode {
		t.Fatalf("la chaîne figée code %s, on attend %s — c'est la chaîne qui est fausse, "+
			"pas le décodeur : elle a été relue depuis les jeux de codes de la norme", decoded, referenceCode)
	}

	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	if got := bitString(modules); got != referenceModules {
		t.Errorf("Modules(%s) a changé :\n  obtenu %s\n  figé   %s\n"+
			"la chaîne figée est relue par un décodeur indépendant, donc c'est l'encodeur "+
			"qui a dérivé, pas la référence", referenceCode, got, referenceModules)
	}
}

// bitString renders 95 modules as the '0'/'1' string the golden is frozen as.
func bitString(m [95]bool) string {
	out := make([]byte, len(m))
	for i, bar := range m {
		out[i] = '0'
		if bar {
			out[i] = '1'
		}
	}
	return string(out)
}

// --- 2. No cumulative drift ------------------------------------------------

// TestEveryEdgeIsTheRoundedIdealPosition is test 2 of §7.4, and it is the property
// the whole approach stands on.
func TestEveryEdgeIsTheRoundedIdealPosition(t *testing.T) {
	o := NewSymbolOptions(domain.IdenticalTemplate())
	if o.ModuleMilliDots != 2344 {
		t.Fatalf("le gabarit de production porte un module de %d milli-dots, attendu 2344 — "+
			"A1 fige le grandissement", o.ModuleMilliDots)
	}

	ideal := func(i int) float64 { return float64(i) * float64(o.ModuleMilliDots) / 1000 }
	worst, worstAt := 0.0, 0
	for i := 0; i <= barModules; i++ {
		err := float64(o.edge(i)) - ideal(i)
		if err > 0.5 || err < -0.5 {
			t.Errorf("bord %d : %d dots contre %.3f idéaux, soit %+.3f dot — au-delà d'un "+
				"demi-dot le bord n'est plus l'arrondi de la position idéale", i, o.edge(i), ideal(i), err)
		}
		if abs(err) > worst {
			worst, worstAt = abs(err), i
		}
	}

	// The point of the rule: the LAST edge is no worse than an early one. An
	// accumulated step would make the error grow monotonically with i.
	last, tenth := abs(float64(o.edge(barModules))-ideal(barModules)), abs(float64(o.edge(10))-ideal(10))
	if last > tenth {
		t.Errorf("erreur au bord 95 = %.3f dot contre %.3f au bord 10 : l'erreur croît avec "+
			"l'indice, donc les bords sont accumulés et non arrondis", last, tenth)
	}
	t.Logf("erreur maximale %.3f dot au bord %d · bord 10 %.3f · bord 95 %.3f",
		worst, worstAt, tenth, last)

	// What accumulation would cost, measured rather than asserted. Two naive schemes,
	// both of which a reader might think acceptable.
	flat := 0
	for i := 0; i < barModules; i++ {
		flat += (o.ModuleMilliDots + 500) / 1000
	}
	alternating := 0
	for i := 0; i < barModules; i++ {
		if i%2 == 0 {
			alternating += 2
		} else {
			alternating += 3
		}
	}
	if abs(float64(flat)-ideal(barModules)) < 1 || abs(float64(alternating)-ideal(barModules)) < 1 {
		t.Errorf("l'accumulation ne dérive plus (plat %d, alterné %d contre %.3f idéaux) : "+
			"ce test ne démontre plus rien", flat, alternating, ideal(barModules))
	}
	t.Logf("ce que coûterait l'accumulation sur 95 modules : pas arrondi à 2 dots → %d dots "+
		"(%+.2f), alternance stricte 2/3 → %d dots (%+.2f), contre %.3f idéaux",
		flat, float64(flat)-ideal(barModules), alternating,
		float64(alternating)-ideal(barModules), ideal(barModules))
}

// --- 3. The width of the bars ----------------------------------------------

// TestTheBarsAreExactlyTwoHundredAndTwentyThreeDots is test 3 of §7.4, checked on
// the DRAWN pixels and not only on the arithmetic.
func TestTheBarsAreExactlyTwoHundredAndTwentyThreeDots(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	if got := o.BarsWidthDots(); got != 223 {
		t.Errorf("largeur des 95 modules de barres = %d dots, attendu 223 (27,875 mm)", got)
	}
	if got := o.TotalWidthDots(); got != 265 {
		t.Errorf("hors-tout des 113 modules = %d dots, attendu 265 (33,109 mm)", got)
	}

	// The template says the same thing in milli-dots; the two must not drift apart.
	symbol := domain.IdenticalTemplate().Symbol
	if want := int((symbol.TotalWidthMilliDots() + 500) / 1000); o.TotalWidthDots() != want {
		t.Errorf("hors-tout %d dots ici contre %d dots calculés par le gabarit", o.TotalWidthDots(), want)
	}

	first, last, ok := inkColumnRange(img, image.Rect(o.XDots, o.YDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.BarHeightDots))
	if !ok {
		t.Fatal("aucune barre encrée")
	}
	if drawn := last - first + 1; drawn != 223 {
		t.Errorf("les barres réellement encrées s'étendent sur %d dots (colonnes %d à %d), "+
			"attendu 223", drawn, first, last)
	}
	if want := o.barsLeft(); first != want {
		t.Errorf("la première barre est en x=%d, attendue en x=%d (fin de la zone de silence gauche)",
			first, want)
	}
}

// --- 4. The integer module -------------------------------------------------

// TestAnIntegerModuleMakesEveryRunEven is test 4 of §7.4: template B, module 2 dots.
//
// It is a consistency check of OUR OWN drawing. The invariant becomes exact the
// moment the module is whole, so a run of odd width there would mean the bars are
// not laid on the module grid at all — and the same defect would be invisible at
// 2.344 dots, where 2 and 3 both look legitimate.
func TestAnIntegerModuleMakesEveryRunEven(t *testing.T) {
	// Built here rather than taken from ShippedTemplates: gabarit B was retired on
	// 30/07/2026 because 2 dots is 75.8 % magnification, below the GS1 floor, so it can
	// no longer be a template anyone selects. The DRAWING invariant it exercised is
	// worth more than the template was — it is the only case where "every run is a
	// whole number of modules" is exactly checkable.
	template := domain.NeutralSingleTemplate()
	template.Symbol.ModuleMilliDots = 2_000
	library, o, img := drawReferenceSymbol(t, template)
	defer library.Close()

	if o.ModuleMilliDots != 2000 {
		t.Fatalf("le gabarit B porte un module de %d milli-dots, attendu 2000", o.ModuleMilliDots)
	}

	row := o.YDots + o.BarHeightDots/2 // inside the bars, above every descender
	left, right := o.barsLeft(), o.barsLeft()+o.BarsWidthDots()
	runs := colourRuns(img, row, left, right)
	if len(runs) < 30 {
		t.Fatalf("%d plages sur la ligne %d : le symbole n'est pas tracé là où on le lit", len(runs), row)
	}
	for i, run := range runs {
		if run.width%2 != 0 {
			t.Errorf("plage %d (x=%d, %d dots, %s) : à module entier toute plage de même "+
				"couleur est un multiple de 2", i, run.at, run.width, colourName(run.black))
		}
	}
	t.Logf("%d plages sur la ligne %d, toutes multiples de 2 dots", len(runs), row)
}

type colourRun struct {
	at, width int
	black     bool
}

// colourRuns splits one row into its runs of constant colour, between x0 and x1.
func colourRuns(img *image.Gray, y, x0, x1 int) []colourRun {
	var runs []colourRun
	current := colourRun{at: x0, black: isInk(img, x0, y)}
	for x := x0; x < x1; x++ {
		if black := isInk(img, x, y); black != current.black {
			current.width = x - current.at
			runs = append(runs, current)
			current = colourRun{at: x, black: black}
		}
	}
	current.width = x1 - current.at
	return append(runs, current)
}

// --- 5. The golden ---------------------------------------------------------

// TestTheSymbolBlockMatchesItsGolden is test 5 of §7.4: the block, pixel for pixel.
func TestTheSymbolBlockMatchesItsGolden(t *testing.T) {
	library, _, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	path := filepath.Join("testdata", "golden", "symbol_weighing_identical.png")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("création de %s : %v", filepath.Dir(path), err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatalf("création de %s : %v", path, err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatalf("encodage de %s : %v", path, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("fermeture de %s : %v", path, err)
		}
		t.Logf("golden réécrit : %s", path)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("golden absent : %v — le régénérer avec « go test ./internal/printing/ "+
			"-run TestTheSymbolBlockMatchesItsGolden -update »", err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("décodage de %s : %v", path, err)
	}
	golden, ok := decoded.(*image.Gray)
	if !ok {
		t.Fatalf("%s n'est pas une image en niveaux de gris mais un %T", path, decoded)
	}

	if golden.Bounds() != img.Bounds() {
		t.Fatalf("le golden mesure %v, le rendu %v", golden.Bounds(), img.Bounds())
	}
	differing, firstX, firstY := 0, -1, -1
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if golden.GrayAt(x, y) != img.GrayAt(x, y) {
				differing++
				if firstX < 0 {
					firstX, firstY = x, y
				}
			}
		}
	}
	if differing > 0 {
		t.Errorf("%d pixels diffèrent du golden, le premier en (%d ; %d) — si la géométrie a "+
			"changé EXPRÈS, régénérer avec « -update » et le dire dans le commit ; sinon, c'est "+
			"le tracé qui a dérivé", differing, firstX, firstY)
	}
}

// --- 6. The block, and the HRI that is always there ------------------------

// TestTheBlockMeasuresWhatTheTemplateDeclares is the first half of test 6.
//
// §7.4 states 265 x 117 dots. THAT SECOND FIGURE IS PRE-ADR-029 and no longer
// describes the production template: it is bars of 11 720 um plus an HRI of 2 930,
// and ADR-029 brought the bars down to 10 875. The measurement that settles it is in
// ADR-029 itself, consequence 2: "le contenu encré descend à 200,744 dots". The
// symbol of weighing_identical starts at 11 288 um = 90,304 dots, and 200,744 -
// 90,304 = 110,440 dots of block. A block of 117,2 dots would put the ink at
// 207,5 dots and break the very hard rule 3 that ADR-029 claims to make satisfiable.
//
// So the shipped block is 265 x 110, and 265 x 117 is checked where its geometry
// still lives: weighing_neutral_single, which still carries 11 720 um of bars.
func TestTheBlockMeasuresWhatTheTemplateDeclares(t *testing.T) {
	for _, c := range []struct {
		template      domain.Template
		width, height int
	}{
		{domain.IdenticalTemplate(), 265, 113},
		{domain.NeutralSingleTemplate(), 265, 111},
	} {
		t.Run(c.template.Name, func(t *testing.T) {
			o := NewSymbolOptions(c.template)
			if got := o.TotalWidthDots(); got != c.width {
				t.Errorf("hors-tout %d dots, attendu %d", got, c.width)
			}
			if got := o.HeightDots(); got != c.height {
				t.Errorf("hauteur du bloc %d dots, attendu %d", got, c.height)
			}
			// The ONE definition of §7.4 lives in the domain; ours must be the same
			// rule expressed in whole dots, not a second opinion.
			want := roundDots(c.template.Media, c.template.Symbol.HeightUM())
			if o.HeightDots() != want {
				t.Errorf("hauteur du bloc %d dots ici contre %d dots par SymbolGeometry.HeightUM : "+
					"deux réponses à la même règle bloquante", o.HeightDots(), want)
			}
		})
	}
}

// TestTheRenderedBlockIsInkedWhereItShouldBe is the second half of test 6: the block
// really occupies what it declares, and the quiet zones really are quiet.
func TestTheRenderedBlockIsInkedWhereItShouldBe(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	ink, ok := inkBounds(img, o.Bounds())
	if !ok {
		t.Fatal("le bloc est vide")
	}
	if ink.Min.Y != o.YDots {
		t.Errorf("la première ligne encrée est en y=%d, attendue en y=%d (haut des barres)",
			ink.Min.Y, o.YDots)
	}
	if want := o.YDots + o.HeightDots(); ink.Max.Y != want {
		t.Errorf("l'encre descend à y=%d, attendu %d — la ligne de base de la HRI est le bas "+
			"du bloc, donc la bande basse ne peut pas être vide", ink.Max.Y, want)
	}
	if want := o.barsLeft() + o.BarsWidthDots(); ink.Max.X != want {
		t.Errorf("l'encre s'arrête en x=%d, attendu %d (dernier module de la garde droite)",
			ink.Max.X, want)
	}

	// The right quiet zone carries no ink at all: 7 modules, and nothing is allowed
	// in them.
	right := image.Rect(o.barsLeft()+o.BarsWidthDots(), o.YDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.HeightDots())
	if _, inked := inkBounds(img, right); inked {
		t.Errorf("la zone de silence droite %v est encrée", right)
	}
	// The left quiet zone carries no ink IN THE BAR BAND, which is what a scanner
	// reads — and it carries the leading digit BELOW it, which is exactly what §7.4
	// asks for.
	quietBars := image.Rect(o.XDots, o.YDots, o.barsLeft(), o.YDots+o.BarHeightDots)
	if _, inked := inkBounds(img, quietBars); inked {
		t.Errorf("la zone de silence gauche %v est encrée dans la bande des barres", quietBars)
	}
	quietHRI := image.Rect(o.XDots, o.YDots+o.BarHeightDots, o.barsLeft(), o.YDots+o.HeightDots())
	leading, inked := inkBounds(img, quietHRI)
	if !inked {
		t.Error("aucun chiffre à gauche du symbole : le premier chiffre de la HRI est tracé " +
			"dans la zone de silence gauche (§7.4)")
	} else if leading.Min.X < o.XDots {
		t.Errorf("le premier chiffre déborde du bloc en x=%d", leading.Min.X)
	}
}

// TestTheHRICarriesTheThirteenDigits is what gives test 6 its teeth.
//
// It does NOT count black pixels: it segments the band below the guard bars into
// marks, requires exactly thirteen of them, and reads each one back by matching it
// against a rendered template of the ten digits. A render whose bottom band is
// empty, or that draws the wrong code, or that draws twelve digits, fails here.
func TestTheHRICarriesTheThirteenDigits(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	guardBottom := o.YDots + o.BarHeightDots + o.GuardDescentDots
	blockBottom := o.YDots + o.HeightDots()
	if guardBottom >= blockBottom {
		t.Fatalf("la bande sous les gardes est haute de %d dots : ce test ne peut rien lire",
			blockBottom-guardBottom)
	}

	// Below the guard descenders only the HRI can be inked, so a column carrying ink
	// there is a digit and nothing else.
	marks := columnClusters(img, image.Rect(o.XDots, guardBottom,
		o.XDots+o.TotalWidthDots(), blockBottom))
	if len(marks) != 13 {
		t.Fatalf("%d marques dans la bande basse du symbole, attendu 13 chiffres — "+
			"une bande vide ou incomplète est un écart à A1, la caissière y perd son "+
			"filet de secours (§7.4)", len(marks))
	}

	templates := renderDigitTemplates(t, o.HRIFace)
	bandTop := o.YDots + o.BarHeightDots
	cell := o.edge(digitModules)

	var read string
	for i, mark := range marks {
		centre := (mark.from + mark.to) / 2
		window := image.Rect(centre-cell/2-1, bandTop, centre+cell/2+2, blockBottom)
		digit, score := bestDigit(img, window, templates)
		if score < 0 {
			t.Fatalf("marque %d : aucun gabarit de chiffre ne rentre dans la fenêtre %v", i, window)
		}
		read += string(digit)
	}
	if read != referenceCode {
		t.Errorf("la HRI relue sur le rendu donne %q, attendu %q — les chiffres sont relus "+
			"par comparaison de gabarits, donc c'est le tracé qui est faux", read, referenceCode)
	}
	t.Logf("HRI relue : %s (13 marques, %d dots de bande sous les gardes)",
		read, blockBottom-guardBottom)
}

// TestABlankHRIBandFailsTheReadBack proves the read-back can go red.
//
// A test that passes whatever happens is worth nothing, and this one is the reason
// the previous test is not just counting pixels: erase the HRI band and the
// segmentation must find zero marks, not thirteen.
func TestABlankHRIBandFailsTheReadBack(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	guardBottom := o.YDots + o.BarHeightDots + o.GuardDescentDots
	blockBottom := o.YDots + o.HeightDots()
	band := image.Rect(o.XDots, o.YDots+o.BarHeightDots, o.XDots+o.TotalWidthDots(), blockBottom)
	for y := band.Min.Y; y < band.Max.Y; y++ {
		for x := band.Min.X; x < band.Max.X; x++ {
			img.SetGray(x, y, color.Gray{Y: 0xFF})
		}
	}

	if marks := columnClusters(img, image.Rect(o.XDots, guardBottom,
		o.XDots+o.TotalWidthDots(), blockBottom)); len(marks) != 0 {
		t.Fatalf("%d marques après avoir effacé la bande : la segmentation lit autre chose "+
			"que la HRI, et le test précédent passerait sans HRI", len(marks))
	}
}
