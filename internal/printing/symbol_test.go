package printing

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"openscale/internal/domain"
)

// The six non-regression tests of §7.4, plus what they need to be worth anything.
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

// referenceCode is the vector T1 of §18: garlic, reference 021, 1.236 kg.
const referenceCode = "0493021012365"

// referenceModules are the 95 modules of referenceCode, FROZEN.
//
// They were obtained once and are checked here by a decoder written from the
// specification, in this file, with its own tables: a golden that is only a recorded
// output agrees with a wrong encoder, and would keep agreeing with it forever.
const referenceModules = "10101000110001011011110100011010010011001100101010111001011001101101100100001010100001001110101"

// --- An independent decoder ------------------------------------------------

// The three code sets and the parity table, transcribed from the specification and
// NOT derived from the tables of internal/domain: a decoder built out of the
// encoder's own tables proves nothing.
var (
	setA = map[string]byte{
		"0001101": '0', "0011001": '1', "0010011": '2', "0111101": '3', "0100011": '4',
		"0110001": '5', "0101111": '6', "0111011": '7', "0110111": '8', "0001011": '9',
	}
	setB = map[string]byte{
		"0100111": '0', "0110011": '1', "0011011": '2', "0100001": '3', "0011101": '4',
		"0111001": '5', "0000101": '6', "0010001": '7', "0001001": '8', "0010111": '9',
	}
	setC = map[string]byte{
		"1110010": '0', "1100110": '1', "1101100": '2', "1000010": '3', "1011100": '4',
		"1001110": '5', "1010000": '6', "1000100": '7', "1001000": '8', "1110100": '9',
	}
	leadingByParity = map[string]byte{
		"AAAAAA": '0', "AABABB": '1', "AABBAB": '2', "AABBBA": '3', "ABAABB": '4',
		"ABBAAB": '5', "ABBBAA": '6', "ABABAB": '7', "ABABBA": '8', "ABBABA": '9',
	}
)

// decodeSymbol reads a 95 character bit string back into the 13 digits it carries.
func decodeSymbol(bits string) (string, error) {
	if len(bits) != barModules {
		return "", fmt.Errorf("%d modules, il en faut %d", len(bits), barModules)
	}
	for _, guard := range []struct {
		name, want string
		at         int
	}{
		{"gauche", "101", 0},
		{"centrale", "01010", centreGuardFirst},
		{"droite", "101", rightGuardFirst},
	} {
		if got := bits[guard.at : guard.at+len(guard.want)]; got != guard.want {
			return "", fmt.Errorf("garde %s = %s, attendu %s", guard.name, got, guard.want)
		}
	}

	var parity, digits string
	for i := 0; i < 6; i++ {
		chunk := bits[leftGroupFirst+7*i : leftGroupFirst+7*(i+1)]
		switch {
		case setA[chunk] != 0:
			parity, digits = parity+"A", digits+string(setA[chunk])
		case setB[chunk] != 0:
			parity, digits = parity+"B", digits+string(setB[chunk])
		default:
			return "", fmt.Errorf("groupe gauche %d : %s n'est ni dans le jeu A ni dans le jeu B", i, chunk)
		}
	}
	leading, ok := leadingByParity[parity]
	if !ok {
		return "", fmt.Errorf("le motif de parité %s ne correspond à aucun premier chiffre", parity)
	}
	for i := 0; i < 6; i++ {
		chunk := bits[rightGroupFirst+7*i : rightGroupFirst+7*(i+1)]
		digit, ok := setC[chunk]
		if !ok {
			return "", fmt.Errorf("groupe droit %d : %s n'est pas dans le jeu C", i, chunk)
		}
		digits += string(digit)
	}

	decoded := string(leading) + digits
	sum := 0
	for i := 0; i < 12; i++ {
		weight := 1
		if (i+1)%2 == 0 {
			weight = 3
		}
		sum += weight * int(decoded[i]-'0')
	}
	if want := byte('0' + (10-sum%10)%10); decoded[12] != want {
		return "", fmt.Errorf("les chiffres relus %s ne satisfont pas la clé de contrôle", decoded)
	}
	return decoded, nil
}

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

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
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
	template := domain.IntegerModuleTemplate()
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

func colourName(black bool) string {
	if black {
		return "barre"
	}
	return "espace"
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
		{domain.IdenticalTemplate(), 265, 110},
		{domain.NeutralSingleTemplate(), 265, 117},
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

// --- The guards ------------------------------------------------------------

// TestTheGuardsRunLowerThanTheOtherBars: modules 0-2, 45-49 and 92-94 descend by
// GuardDescentDots, and nothing else does.
//
// The descenders are what a scanner uses to find the edges of the symbol, and they
// are also what keeps the HRI digits in a band of their own instead of hanging under
// a flat row of bars.
func TestTheGuardsRunLowerThanTheOtherBars(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	if o.GuardDescentDots <= 0 {
		t.Fatalf("descente des gardes %d dots : le gabarit de production en déclare 1 465 µm",
			o.GuardDescentDots)
	}
	// One row inside the descent, below every ordinary bar and above the HRI digits.
	row := o.YDots + o.BarHeightDots
	left := o.barsLeft()
	modules, err := domain.Modules(domain.EAN13(referenceCode))
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	for i, bar := range modules {
		x := left + o.edge(i)
		inked := isInk(img, x, row)
		switch {
		case isGuard(i) && bar && !inked:
			t.Errorf("module %d est une barre de garde et ne descend pas sous les barres", i)
		case !isGuard(i) && inked:
			t.Errorf("module %d n'est pas une garde et descend quand même : la ligne %d "+
				"appartient à la bande de la HRI", i, row)
		}
	}

	// And the descent stops where it is declared to stop.
	if bottom := o.YDots + o.BarHeightDots + o.GuardDescentDots; isInk(img, left, bottom) {
		t.Errorf("la garde gauche est encore encrée en y=%d, au-delà des %d dots de descente",
			bottom, o.GuardDescentDots)
	}
}

// --- What DrawEAN13 refuses ------------------------------------------------

// TestDrawEAN13RefusesWhatItCannotDrawCorrectly: every one of these would print
// something, and something wrong.
func TestDrawEAN13RefusesWhatItCannotDrawCorrectly(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	template := domain.IdenticalTemplate()
	sound := NewSymbolOptions(template)
	face, _, err := FitHRIFace(library, Carlito, sound, template.Media.DotsPerMM)
	if err != nil {
		t.Fatalf("fonte de la HRI : %v", err)
	}
	sound.HRIFace = face
	sound.XDots, sound.YDots = 0, 0

	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	full := image.NewGray(image.Rect(0, 0, sound.TotalWidthDots(), sound.HeightDots()))

	other, err := domain.ParseEAN13("0493021000003")
	if err != nil {
		t.Fatalf("second code : %v", err)
	}
	otherModules, err := domain.Modules(other)
	if err != nil {
		t.Fatalf("Modules du second code : %v", err)
	}

	for _, c := range []struct {
		name    string
		dst     *image.Gray
		code    domain.EAN13
		modules [95]bool
		mutate  func(*SymbolOptions)
	}{
		{name: "sans image", dst: nil, code: code, modules: modules},
		{name: "sans HRI", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.HRIHeightDots = 0 }},
		{name: "gardes qui montent au lieu de descendre", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.GuardDescentDots = -4 }},
		{name: "sans fonte de HRI", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.HRIFace = nil }},
		{name: "module nul", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.ModuleMilliDots = 0 }},
		{name: "barres sans hauteur", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.BarHeightDots = 0 }},
		{name: "bloc plus large que l'image", dst: image.NewGray(image.Rect(0, 0, 100, 200)),
			code: code, modules: modules},
		{name: "les barres codent un autre produit", dst: full, code: code, modules: otherModules},
		{name: "code trop court", dst: full, code: domain.EAN13("049302101236"), modules: modules},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := sound
			if c.mutate != nil {
				c.mutate(&o)
			}
			if err := DrawEAN13(c.dst, c.code, c.modules, o); err == nil {
				t.Errorf("accepté sans broncher : ce symbole partirait à l'impression")
			}
		})
	}
}

// --- The HRI face ----------------------------------------------------------

// TestFitHRIFaceStaysAboveTheLegibilityFloor: the line the cashier falls back on is
// held to the same floor hard rule 9 imposes on every other field.
func TestFitHRIFaceStaysAboveTheLegibilityFloor(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	for _, template := range []domain.Template{
		domain.IdenticalTemplate(), domain.NeutralSingleTemplate(), domain.IntegerModuleTemplate(),
	} {
		t.Run(template.Name, func(t *testing.T) {
			o := NewSymbolOptions(template)
			face, sizeUM, err := FitHRIFace(library, Carlito, o, template.Media.DotsPerMM)
			if err != nil {
				t.Fatalf("fonte de la HRI : %v", err)
			}
			if sizeUM < domain.MinFontSizeUM {
				t.Errorf("HRI au corps %d µm, sous le plancher de %d µm de la règle dure 9",
					sizeUM, domain.MinFontSizeUM)
			}
			// It must fit the cell, or two neighbouring digits run into each other.
			cell := o.edge(digitModules)
			for digit := byte('0'); digit <= '9'; digit++ {
				bounds, ok := digitInk(face, digit)
				if !ok {
					t.Fatalf("pas de glyphe pour %q", string(digit))
				}
				if w := ceilDots(bounds.Max.X - bounds.Min.X); w > cell-hriCellClearanceDots {
					t.Errorf("le chiffre %q fait %d dots de large pour une cellule de %d dots",
						string(digit), w, cell)
				}
				if h := ceilDots(bounds.Max.Y - bounds.Min.Y); h > o.HRIHeightDots {
					t.Errorf("le chiffre %q fait %d dots de haut pour une bande de %d dots",
						string(digit), h, o.HRIHeightDots)
				}
			}
		})
	}
}

// TestFitHRIFaceRefusesAGeometryWithNoRoomForTheLine: a template that leaves no room
// for a legible HRI is refused at load time, not silently given digits nobody reads.
func TestFitHRIFaceRefusesAGeometryWithNoRoomForTheLine(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	for _, c := range []struct {
		name string
		o    SymbolOptions
	}{
		{"module nul", SymbolOptions{ModuleMilliDots: 0, HRIHeightDots: 23}},
		{"bande HRI nulle", SymbolOptions{ModuleMilliDots: 2344, HRIHeightDots: 0}},
		{"cellule plus étroite qu'un chiffre", SymbolOptions{ModuleMilliDots: 200, HRIHeightDots: 23}},
		{"bande trop basse au plancher de lisibilité",
			SymbolOptions{ModuleMilliDots: domain.MinModuleMilliDots, HRIHeightDots: 5}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, sizeUM, err := FitHRIFace(library, Carlito, c.o, 8); err == nil {
				t.Errorf("accepté au corps %d µm : la HRI serait illisible", sizeUM)
			}
		})
	}

	// A font the binary does not carry must fail here too, rather than be substituted
	// on the one line a cashier reads when the scanner refuses.
	sound := NewSymbolOptions(domain.IdenticalTemplate())
	if _, _, err := FitHRIFace(library, "calibri", sound, 8); err == nil {
		t.Error("« calibri » acceptée pour la HRI : c'est la police qu'on ne peut pas redistribuer")
	}
}

// faceWithoutDigits is a face that carries no digit glyph, which is what a subset
// font supplied by an operator could turn out to be.
type faceWithoutDigits struct{ font.Face }

func (faceWithoutDigits) GlyphBounds(rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return fixed.Rectangle26_6{}, 0, false
}

// TestDrawEAN13RefusesAFaceThatCannotDrawDigits: better no label than a symbol whose
// human-readable line is silently missing.
func TestDrawEAN13RefusesAFaceThatCannotDrawDigits(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	o.HRIFace = faceWithoutDigits{o.HRIFace}
	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	if err := DrawEAN13(img, code, modules, o); err == nil {
		t.Error("accepté : le symbole partirait sans sa ligne lisible, et la caissière " +
			"perdrait son filet de secours (§7.4, important-5)")
	}
}

// --- Fixtures --------------------------------------------------------------

// drawReferenceSymbol renders the reference code with the geometry of a template,
// at the origin, on a white image the exact size of the block.
func drawReferenceSymbol(t *testing.T, template domain.Template) (*Library, SymbolOptions, *image.Gray) {
	t.Helper()

	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	o := NewSymbolOptions(template)
	o.XDots, o.YDots = 0, 0

	// Carlito rather than the "Code EAN13" font of the legacy report: ADR-019 draws
	// the symbol geometrically and does not embed that font, and Carlito is the font
	// of every other field of the label (ADR-020).
	face, sizeUM, err := FitHRIFace(library, Carlito, o, template.Media.DotsPerMM)
	if err != nil {
		library.Close()
		t.Fatalf("fonte de la HRI : %v", err)
	}
	o.HRIFace = face
	t.Logf("%s : module %d milli-dots · barres %d dots · gardes +%d · bande HRI %d dots · "+
		"HRI en Carlito à %d µm · bloc %d × %d dots",
		template.Name, o.ModuleMilliDots, o.BarHeightDots, o.GuardDescentDots,
		o.HRIHeightDots, sizeUM, o.TotalWidthDots(), o.HeightDots())

	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		library.Close()
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		library.Close()
		t.Fatalf("Modules : %v", err)
	}

	img := image.NewGray(image.Rect(0, 0, o.TotalWidthDots(), o.HeightDots()))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	if err := DrawEAN13(img, code, modules, o); err != nil {
		library.Close()
		t.Fatalf("DrawEAN13 : %v", err)
	}
	return library, o, img
}

// --- Reading pixels back ---------------------------------------------------

// isInk reports whether a dot is burnt. The head is binary and DrawEAN13 thresholds
// its own HRI, so there is nothing in between to arbitrate.
func isInk(img *image.Gray, x, y int) bool {
	return img.GrayAt(x, y).Y < 0x80
}

// inkBounds reports the tight box around the ink inside r.
func inkBounds(img *image.Gray, r image.Rectangle) (image.Rectangle, bool) {
	box := image.Rectangle{Min: image.Pt(r.Max.X, r.Max.Y), Max: image.Pt(r.Min.X, r.Min.Y)}
	found := false
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !isInk(img, x, y) {
				continue
			}
			found = true
			box.Min.X = min(box.Min.X, x)
			box.Min.Y = min(box.Min.Y, y)
			box.Max.X = max(box.Max.X, x+1)
			box.Max.Y = max(box.Max.Y, y+1)
		}
	}
	return box, found
}

// inkColumnRange reports the first and last inked column of r.
func inkColumnRange(img *image.Gray, r image.Rectangle) (first, last int, ok bool) {
	box, found := inkBounds(img, r)
	if !found {
		return 0, 0, false
	}
	return box.Min.X, box.Max.X - 1, true
}

// cluster is a run of consecutive columns carrying ink.
type cluster struct{ from, to int }

// columnClusters splits r into the groups of adjacent inked columns it contains.
func columnClusters(img *image.Gray, r image.Rectangle) []cluster {
	var out []cluster
	open := false
	for x := r.Min.X; x < r.Max.X; x++ {
		inked := false
		for y := r.Min.Y; y < r.Max.Y && !inked; y++ {
			inked = isInk(img, x, y)
		}
		switch {
		case inked && !open:
			out = append(out, cluster{from: x, to: x})
			open = true
		case inked:
			out[len(out)-1].to = x
		default:
			open = false
		}
	}
	return out
}

// digitTemplate is one digit rendered alone, cropped to its ink.
type digitTemplate struct {
	digit byte
	ink   []bool // row-major, w x h
	w, h  int
}

// renderDigitTemplates draws the ten digits with the very face the HRI was drawn
// with, and crops each to its ink. Matching against these is what turns "there are
// pixels down there" into "these are the thirteen digits of the code".
func renderDigitTemplates(t *testing.T, face font.Face) []digitTemplate {
	t.Helper()
	out := make([]digitTemplate, 0, 10)
	for digit := byte('0'); digit <= '9'; digit++ {
		const pad = 8
		canvas := image.NewGray(image.Rect(0, 0, 4*pad, 4*pad))
		for i := range canvas.Pix {
			canvas.Pix[i] = 0xFF
		}
		if _, ok := digitInk(face, digit); !ok {
			t.Fatalf("la fonte n'a pas de glyphe pour %q", string(digit))
		}
		drawDigit(canvas, face, digit, fixed.I(pad), 3*pad)
		box, found := inkBounds(canvas, canvas.Bounds())
		if !found {
			t.Fatalf("le gabarit du chiffre %q est vide", string(digit))
		}
		tmpl := digitTemplate{digit: digit, w: box.Dx(), h: box.Dy()}
		tmpl.ink = make([]bool, tmpl.w*tmpl.h)
		for y := 0; y < tmpl.h; y++ {
			for x := 0; x < tmpl.w; x++ {
				tmpl.ink[y*tmpl.w+x] = isInk(canvas, box.Min.X+x, box.Min.Y+y)
			}
		}
		out = append(out, tmpl)
	}
	return out
}

// bestDigit reports which of the ten digits the ink inside window looks most like,
// and the number of pixels that still differ once it is best placed.
func bestDigit(img *image.Gray, window image.Rectangle, templates []digitTemplate) (byte, int) {
	best, bestScore := byte(0), -1
	for _, tmpl := range templates {
		score := matchTemplate(img, window, tmpl)
		if score < 0 {
			continue
		}
		if bestScore < 0 || score < bestScore {
			best, bestScore = tmpl.digit, score
		}
	}
	return best, bestScore
}

// matchTemplate slides one digit over the window and reports the smallest number of
// differing pixels, or -1 when the digit does not fit at all.
func matchTemplate(img *image.Gray, window image.Rectangle, tmpl digitTemplate) int {
	if tmpl.w > window.Dx() || tmpl.h > window.Dy() {
		return -1
	}
	best := -1
	for dy := 0; dy+tmpl.h <= window.Dy(); dy++ {
		for dx := 0; dx+tmpl.w <= window.Dx(); dx++ {
			differing := 0
			for y := 0; y < window.Dy(); y++ {
				for x := 0; x < window.Dx(); x++ {
					want := false
					if y >= dy && y < dy+tmpl.h && x >= dx && x < dx+tmpl.w {
						want = tmpl.ink[(y-dy)*tmpl.w+(x-dx)]
					}
					if isInk(img, window.Min.X+x, window.Min.Y+y) != want {
						differing++
					}
				}
			}
			if best < 0 || differing < best {
				best = differing
			}
		}
	}
	return best
}
