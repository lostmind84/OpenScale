package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
)

// The tests of `openscale label`, the demonstration command of L4.
//
// They never look at a file only to check it is not empty: the PDF is parsed back
// and measured, and the PNG is decoded and compared to the render dot by dot. A
// preview that cannot be re-read is a preview nobody can measure, which is the one
// thing this command exists for.

// The two tolerances, and they are not interchangeable.
const (
	// toleranceUM is the 0.1 mm the acceptance criterion of §18 is stated in. A ruler
	// on a printed page reads no finer, and the PAGE is compared at that.
	toleranceUM = 100.0

	// pitchToleranceUM is what the BITMAP is compared at, and it is a hundred times
	// tighter on purpose: its size is arithmetic — dots divided by dots per
	// millimetre — and not a measurement. At 0.1 mm a bitmap stretched to fill the
	// page would pass, since the difference is 25 µm on the shipped template, and
	// that is precisely the defect that would cost the ruler its answer.
	pitchToleranceUM = 1.0
)

var (
	mediaBoxPattern = regexp.MustCompile(`/MediaBox \[0 0 (\S+) (\S+)\]`)
	matrixPattern   = regexp.MustCompile(`q (\S+) 0 0 (\S+) (\S+) (\S+) cm /Im0 Do Q`)
	imagePattern    = regexp.MustCompile(`/Subtype /Image /Width (\d+) /Height (\d+)`)
	rulerPattern    = regexp.MustCompile(`module (\S+) mm · hors-tout du symbole (\S+) mm · barres (\S+) mm`)
)

// TestDemonstrationCriterionOfWorkPackageFour freezes the command line §18 requires
// L4 to answer, and reads its answer back out of the file.
//
// ONE FIGURE OF §18 IS NO LONGER TRUE, AND IT IS THE DOCUMENT THAT MOVED, NOT THE
// CODE. The criterion quotes « barres 11,7 mm », which is the height of the CURRENT
// label; ADR-029 stacks the three text lines and puts the symbol below them, so the
// bars become uniform over the whole width at 10 875 µm — +30 % of readable height
// against the 8 341 µm that are clean today. domain.IdenticalTemplate() ships that
// figure, and this test asserts what is shipped. The module and the over-all width,
// which are what A1 freezes, are unchanged.
func TestDemonstrationCriterionOfWorkPackageFour(t *testing.T) {
	pdf := filepath.Join(t.TempDir(), "output.pdf")

	var out bytes.Buffer
	err := runLabel([]string{"--template", "weighing_identical", "--demo", "--dual", "--pdf", pdf}, &out)
	if err != nil {
		t.Fatalf("runLabel : %v", err)
	}

	if !strings.Contains(out.String(), "CELERI BRANCHE SAF · 1,236 kg · code-barres 0493021012365") {
		t.Errorf("l'étiquette de démonstration n'est pas celle du vecteur T1 :\n%s", out.String())
	}
	if !strings.Contains(out.String(), "A 3,02 €/kg · A 3,73 € · S 4,14 €") {
		t.Errorf("les prix bi-tarif ne sont pas ceux de la grille La Cagette :\n%s", out.String())
	}
	ruler := rulerPattern.FindStringSubmatch(out.String())
	if ruler == nil {
		t.Fatalf("la commande ne dit pas quoi mesurer au réglet :\n%s", out.String())
	}
	for _, c := range []struct{ what, got, want string }{
		{"module", ruler[1], "0,293"},
		{"hors-tout du symbole", ruler[2], "33,125"},
		{"barres (ADR-029, contre les 11,7 de §18)", ruler[3], "10,875"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %s mm, %s attendu", c.what, c.got, c.want)
		}
	}

	g := domain.IdenticalTemplate()
	doc := parsePDF(t, pdf)
	if got := micrometres(doc.pageWidth); !within(got, float64(g.Media.WidthUM)) {
		t.Errorf("largeur de page = %.0f µm, le gabarit déclare %d µm", got, g.Media.WidthUM)
	}
	if got := micrometres(doc.pageHeight); !within(got, float64(g.Media.HeightUM)) {
		t.Errorf("hauteur de page = %.0f µm, le gabarit déclare %d µm", got, g.Media.HeightUM)
	}
	// And the bitmap inside that page is at the pitch of the head, which is what
	// makes the module measurable: 320 dots at 8 dots/mm are 40 mm, whatever the page.
	umPerDot := 1000 / g.Media.DotsPerMM
	if got, want := micrometres(doc.scaleX), float64(doc.imageWidth)*umPerDot; !atPitch(got, want) {
		t.Errorf("le bitmap couvre %.1f µm de large, %d dots au pas de la tête en font %.1f",
			got, doc.imageWidth, want)
	}
	if got, want := micrometres(doc.scaleY), float64(doc.imageHeight)*umPerDot; !atPitch(got, want) {
		t.Errorf("le bitmap couvre %.1f µm de haut, %d dots au pas de la tête en font %.1f",
			got, doc.imageHeight, want)
	}
	if doc.imageWidth != 320 || doc.imageHeight != 203 {
		t.Errorf("bitmap de %d × %d dots, 320 × 203 attendus à 8 dots/mm",
			doc.imageWidth, doc.imageHeight)
	}
}

// TestThePNGIsTheRenderItself: the file carries the very image Rasterize produced,
// at the size the head works in, with no resampling on the way out.
func TestThePNGIsTheRenderItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "label.png")

	var out bytes.Buffer
	if err := runLabel([]string{"--demo", "--dual", "--png", path}, &out); err != nil {
		t.Fatalf("runLabel : %v", err)
	}
	written := decodePNG(t, path)

	want, _, err := renderDemo(domain.IdenticalTemplate(), domain.LaCagetteRules(), printing.RenderOptions{})
	if err != nil {
		t.Fatalf("renderDemo : %v", err)
	}
	if written.Bounds() != want.Bounds() {
		t.Fatalf("le PNG mesure %v, le rendu %v", written.Bounds(), want.Bounds())
	}
	if got, expected := written.Bounds().Dx(), 320; got != expected {
		t.Errorf("largeur du PNG = %d dots, %d attendus", got, expected)
	}
	for y := written.Bounds().Min.Y; y < written.Bounds().Max.Y; y++ {
		for x := written.Bounds().Min.X; x < written.Bounds().Max.X; x++ {
			if written.GrayAt(x, y) != want.GrayAt(x, y) {
				t.Fatalf("dot (%d, %d) : 0x%02X écrit, 0x%02X rendu",
					x, y, written.GrayAt(x, y).Y, want.GrayAt(x, y).Y)
			}
		}
	}
}

// TestDualCarriesTwoTiersAndMonoCarriesOne.
//
// The cardinality of the GRID is the whole of it: the secondary price disappears
// through the `when: multi_tier` condition of the template, with no conditional code
// anywhere (§7.2). So this checks the two places it shows — the prices announced in
// the terminal, and the ink inside the box of the secondary field.
func TestDualCarriesTwoTiersAndMonoCarriesOne(t *testing.T) {
	g := domain.IdenticalTemplate()
	box := secondaryPriceBox(t, g)

	for _, c := range []struct {
		name  string
		args  []string
		rules domain.PricingRules
		tiers int
		grid  string
		ink   bool
	}{
		{"--demo --dual", []string{"--demo", "--dual"}, domain.LaCagetteRules(), 2, "bi-tarif", true},
		{"--demo seul", []string{"--demo"}, domain.SingleTierRules(), 1, "mono-tarif", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			label, err := demoLabel(c.rules)
			if err != nil {
				t.Fatalf("demoLabel : %v", err)
			}
			if len(label.Lines) != c.tiers {
				t.Fatalf("%d ligne(s) de prix, %d attendue(s)", len(label.Lines), c.tiers)
			}

			dir := t.TempDir()
			path := filepath.Join(dir, "label.png")
			var out bytes.Buffer
			if err := runLabel(append(append([]string{}, c.args...), "--png", path), &out); err != nil {
				t.Fatalf("runLabel : %v", err)
			}
			if carries := strings.Contains(out.String(), "S 4,14 €"); carries != (c.tiers > 1) {
				t.Errorf("prix solidaire annoncé = %v pour %d tarif(s) :\n%s",
					carries, c.tiers, out.String())
			}
			if want := "gabarit " + g.Name + ", " + c.grid; !strings.Contains(out.String(), want) {
				t.Errorf("la commande n'annonce pas « %s » :\n%s", want, out.String())
			}
			if inked := hasInk(decodePNG(t, path), box); inked != c.ink {
				t.Errorf("encre dans la boîte du prix secondaire %v = %v, %v attendu",
					box, inked, c.ink)
			}
		})
	}
}

// TestAnUnknownTemplateNamesTheOnesThatExist — same requirement as
// scale.ErrUnknownDriver: a name spelled wrong produces the list of the names that
// work. « inconnu » alone leaves whoever typed it with nothing to try.
func TestAnUnknownTemplateNamesTheOnesThatExist(t *testing.T) {
	var out bytes.Buffer
	err := runLabel([]string{"--template", "weighing_identcal", "--demo"}, &out)
	if err == nil {
		t.Fatal("un gabarit inconnu a été accepté")
	}
	for name := range domain.ShippedTemplates() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("le message ne nomme pas le gabarit disponible %q : %v", name, err)
		}
	}
	if !strings.Contains(err.Error(), "weighing_identcal") {
		t.Errorf("le message ne rappelle pas le nom saisi : %v", err)
	}
}

// TestLabelTakesItsOptionsInAnyOrderAndNoArgument.
//
// parseMixed exists because the standard flag package stops at the first non-flag.
// `label` has no positional argument at all, so what parseMixed buys here is the
// message for the mistake anybody makes once: typing the gabarit without --template.
func TestLabelTakesItsOptionsInAnyOrderAndNoArgument(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"--demo", "--template", "weighing_identical", "--png", filepath.Join(dir, "a.png")},
		{"--template", "weighing_identical", "--png", filepath.Join(dir, "b.png"), "--demo"},
	} {
		var out bytes.Buffer
		if err := runLabel(args, &out); err != nil {
			t.Errorf("runLabel(%v) : %v", args, err)
		}
	}

	var out bytes.Buffer
	err := runLabel([]string{"weighing_identical", "--demo"}, &out)
	if err == nil {
		t.Fatal("un argument positionnel a été accepté")
	}
	if !strings.Contains(err.Error(), "--template") {
		t.Errorf("le message ne dit pas comment donner le gabarit : %v", err)
	}
}

// TestWithNeitherPdfNorPngBothAreWritten: §15.1 spells the command
// `openscale label --template X --demo` and says it « rend un PDF + un PNG grandeur
// nature ». With no path given, both are written, named after the gabarit.
func TestWithNeitherPdfNorPngBothAreWritten(t *testing.T) {
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	if err := runLabel([]string{"--template", "weighing_neutral_single", "--demo"}, &out); err != nil {
		t.Fatalf("runLabel : %v", err)
	}
	for _, name := range []string{"weighing_neutral_single.pdf", "weighing_neutral_single.png"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s n'a pas été écrit : %v", name, err)
		}
		if !strings.Contains(out.String(), name) {
			t.Errorf("la commande ne dit pas qu'elle a écrit %s :\n%s", name, out.String())
		}
	}
}

// TestLabelSaysWhichFileItCouldNotWrite. A volunteer reading this has a directory to
// create or a right to fix, and the message has to name the file.
func TestLabelSaysWhichFileItCouldNotWrite(t *testing.T) {
	for _, option := range []string{"--pdf", "--png"} {
		t.Run(option, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "aucun-repertoire", "label"+option[1:])

			var out bytes.Buffer
			err := runLabel([]string{"--demo", option, missing}, &out)
			if err == nil {
				t.Fatal("l'écriture dans un répertoire inexistant a été acceptée")
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("le message ne nomme pas le fichier : %v", err)
			}
		})
	}
}

// TestAnUnreadableOptionStopsTheCommand: parseMixed hands the flag package's own
// refusal back, and nothing is written.
func TestAnUnreadableOptionStopsTheCommand(t *testing.T) {
	var out bytes.Buffer
	if err := runLabel([]string{"--demo", "--gabarit", "weighing_identical"}, &out); err == nil {
		t.Fatal("une option inconnue a été acceptée")
	}
}

// TestAnEncodingThatCannotHappenIsStillReported.
//
// Neither of these is reachable through the command line — the render always comes
// from the very template whose media is handed to the encoder — and both are exactly
// what would happen the day a caller mixes two templates up. The point of the check
// is that the file is NOT written and the failure is not silent.
func TestAnEncodingThatCannotHappenIsStillReported(t *testing.T) {
	g := domain.IdenticalTemplate()
	img, _, err := renderDemo(g, domain.LaCagetteRules(), printing.RenderOptions{})
	if err != nil {
		t.Fatalf("renderDemo : %v", err)
	}
	dir := t.TempDir()

	narrow := g
	narrow.Media.WidthUM = 20_000 // the render is 40 mm wide: the page would crop it
	var out bytes.Buffer
	if err := writePreviews(narrow, img, filepath.Join(dir, "a.pdf"), "", &out); err == nil {
		t.Error("un bitmap plus large que sa page a été écrit")
	}
	if err := writePreviews(g, image.NewGray(image.Rect(0, 0, 0, 0)), "", filepath.Join(dir, "a.png"), &out); err == nil {
		t.Error("une image vide a été écrite en PNG")
	}
	for _, name := range []string{"a.pdf", "a.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s a été écrit alors que l'encodage a échoué", name)
		}
	}
}

// TestATemplateWithNoMediaIsRefusedByTheEngine.
//
// The nine hard rules do NOT catch it, and that is by design: §7.5 states that no
// validation rule depends on the value of the media, which only aligns the life-size
// preview. The engine is where it stops — media.dots_per_mm and the media surface are
// the whole geometry of a render (§7.3) — and the message has to reach the operator
// rather than produce a zero-by-zero image.
func TestATemplateWithNoMediaIsRefusedByTheEngine(t *testing.T) {
	g := domain.IdenticalTemplate()
	g.Media.WidthUM, g.Media.HeightUM = 0, 0
	g.PrintableWidthUM, g.PrintableHeightUM = 0, 0

	if faults := g.Validate(2); len(faults) != 0 {
		t.Fatalf("les règles dures refusent maintenant un média nul, ce test ne prouve plus rien : %v", faults)
	}
	if _, _, err := renderDemo(g, domain.LaCagetteRules(), printing.RenderOptions{}); err == nil {
		t.Fatal("un gabarit sans média a été rendu")
	}
}

// TestLabelWithoutDemoSaysWhatIsMissing. There is no other source of a label before
// L6 and L7, and inventing one silently would be worse than saying so.
func TestLabelWithoutDemoSaysWhatIsMissing(t *testing.T) {
	var out bytes.Buffer
	err := runLabel([]string{"--template", "weighing_identical"}, &out)
	if err == nil {
		t.Fatal("une commande sans --demo a rendu quelque chose")
	}
	if !strings.Contains(err.Error(), "--demo") {
		t.Errorf("le message ne nomme pas l'option manquante : %v", err)
	}
}

// TestAnnotateReachesTheRender: the bench overlay is a render option and nothing
// else, and --annotate has to arrive there rather than being swallowed by the flag
// set.
func TestAnnotateReachesTheRender(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.png")
	annotated := filepath.Join(dir, "annotated.png")

	var out bytes.Buffer
	if err := runLabel([]string{"--demo", "--dual", "--png", plain}, &out); err != nil {
		t.Fatalf("runLabel : %v", err)
	}
	if err := runLabel([]string{"--demo", "--dual", "--annotate", "--png", annotated}, &out); err != nil {
		t.Fatalf("runLabel --annotate : %v", err)
	}

	want, _, err := renderDemo(domain.IdenticalTemplate(), domain.LaCagetteRules(),
		printing.RenderOptions{Annotate: true})
	if err != nil {
		t.Fatalf("renderDemo : %v", err)
	}
	got := decodePNG(t, annotated)
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("le PNG annoté n'est pas le rendu annoté")
	}
	if bytes.Equal(got.Pix, decodePNG(t, plain).Pix) {
		t.Error("--annotate n'a rien changé au rendu")
	}
}

// TestATemplateThatBreaksAHardRuleIsRefusedBeforeItIsDrawn.
//
// The three shipped templates pass their nine rules, so the only way to reach this is
// to hand renderDemo a broken one — which is what a template FILE will be able to be
// as soon as they are loaded from disk rather than built in Go (see templates.go).
// A preview that skipped the validation would show, life size and convincingly,
// something no printer should ever receive.
func TestATemplateThatBreaksAHardRuleIsRefusedBeforeItIsDrawn(t *testing.T) {
	broken := domain.IdenticalTemplate()
	// Under the legibility floor of hard rule 9, and by a lot: 1 mm of em on a
	// thermal head draws nothing anybody can read.
	broken.Elements[0].FontSizeUM = 1_000

	_, _, err := renderDemo(broken, domain.LaCagetteRules(), printing.RenderOptions{})
	if err == nil {
		t.Fatal("un gabarit qui viole une règle dure a été rendu")
	}
	if !strings.Contains(err.Error(), "font_size_um") {
		t.Errorf("le message ne nomme pas la règle violée : %v", err)
	}
}

// TestMillimetresRoundsToTheMicrometre.
//
// Every length the shipped templates produce happens to be a whole number of
// micrometres, so truncating instead of rounding would print the same figures today
// and a wrong one the day a media of 12 dots/mm arrives — 320 dots are 26 666,67 µm
// there. The rule is stated here rather than left to a coincidence of the data.
func TestMillimetresRoundsToTheMicrometre(t *testing.T) {
	for _, c := range []struct {
		um   float64
		want string
	}{
		{0, "0,000"},
		{292.6, "0,293"},
		{293, "0,293"},
		{1000.5, "1,001"},
		{25_375, "25,375"},
		{26_666.67, "26,667"},
		{40_000, "40,000"},
	} {
		if got := millimetres(c.um); got != c.want {
			t.Errorf("millimetres(%.2f) = %s, %s attendu", c.um, got, c.want)
		}
	}
}

// --- Reading the files back ------------------------------------------------

// pdfDocument is what a reader finds in the file.
type pdfDocument struct {
	pageWidth, pageHeight   float64 // points
	scaleX, scaleY          float64 // points — the physical size of the bitmap
	imageWidth, imageHeight int     // dots
}

// parsePDF re-opens the file the command wrote and reads its geometry back.
func parsePDF(t *testing.T, path string) pdfDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("le PDF écrit ne peut pas être relu : %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("%s ne commence pas par un en-tête PDF", path)
	}
	page := mediaBoxPattern.FindSubmatch(data)
	matrix := matrixPattern.FindSubmatch(data)
	bitmap := imagePattern.FindSubmatch(data)
	if page == nil || matrix == nil || bitmap == nil {
		t.Fatalf("%s ne porte pas une page, une matrice et une image", path)
	}
	return pdfDocument{
		pageWidth:   number(t, page[1]),
		pageHeight:  number(t, page[2]),
		scaleX:      number(t, matrix[1]),
		scaleY:      number(t, matrix[2]),
		imageWidth:  int(number(t, bitmap[1])),
		imageHeight: int(number(t, bitmap[2])),
	}
}

func number(t *testing.T, p []byte) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(string(p), 64)
	if err != nil {
		t.Fatalf("nombre illisible %q : %v", p, err)
	}
	return v
}

// decodePNG reads back a PNG the command wrote, as the grey image it was.
func decodePNG(t *testing.T, path string) *image.Gray {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("le PNG écrit ne peut pas être relu : %v", err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("%s n'est pas un PNG lisible : %v", path, err)
	}
	gray, ok := decoded.(*image.Gray)
	if !ok {
		t.Fatalf("%s n'est pas en niveaux de gris mais un %T", path, decoded)
	}
	return gray
}

// micrometres converts a length in points back to the unit a template speaks.
func micrometres(pt float64) float64 { return pt * 25_400.0 / 72.0 }

// within compares two lengths in micrometres at the resolution a ruler has.
func within(got, want float64) bool { return apart(got, want) <= toleranceUM }

// atPitch compares two lengths that arithmetic, not a ruler, is supposed to make
// equal.
func atPitch(got, want float64) bool { return apart(got, want) <= pitchToleranceUM }

func apart(got, want float64) float64 {
	if got < want {
		return want - got
	}
	return got - want
}

// secondaryPriceBox is where the template puts the solidarity price, in whole dots.
func secondaryPriceBox(t *testing.T, g domain.Template) image.Rectangle {
	t.Helper()
	for _, e := range g.Elements {
		if e.Field != domain.FieldSecondaryTotalPrice {
			continue
		}
		dots := func(um domain.Micrometers) int { return int((g.Media.MilliDots(um) + 500) / 1000) }
		return image.Rect(dots(e.XUM), dots(e.YUM), dots(e.Right()), dots(e.Bottom()))
	}
	t.Fatalf("le gabarit %s ne place aucun prix secondaire", g.Name)
	return image.Rectangle{}
}

// hasInk reports whether anything was drawn inside a box.
func hasInk(img *image.Gray, box image.Rectangle) bool {
	box = box.Intersect(img.Bounds())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if img.GrayAt(x, y).Y < 0x80 {
				return true
			}
		}
	}
	return false
}
