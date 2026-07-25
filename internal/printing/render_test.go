package printing

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/math/fixed"

	"openscale/internal/domain"
)

// The tests of the rendering engine of §7.3.
//
// # REGENERATING THE GOLDENS
//
// The goldens of TestTheLabelMatchesItsGoldens are PNGs under testdata/golden/.
// They are rewritten by:
//
//	go test ./internal/printing/ -run TestTheLabelMatchesItsGoldens -update
//
// The flag is the one symbol_test.go declares, so the same invocation with -run
// TestTheSymbolBlockMatchesItsGolden rewrites the symbol golden and nothing else.
//
// Regenerate ONLY when the rendering changed on purpose, and say so in the commit: a
// golden updated to make a test pass is a test that no longer tests anything. The
// pinned toolchain and the exact x/image version of go.mod (§16.4) are what keep
// them stable across machines.
//
// # WHERE THE FIXTURES COME FROM
//
// Nothing here is invented. The products are rows of testdata/catalog/flv.csv, the
// authentic export; the mass is the 1,236 kg of test vector T1, whose barcode
// 0493021012365 is the one symbol_test.go freezes its 95 modules for; and the price
// grid is domain.LaCagetteRules, established from the evidence (A7).

// The three real catalog rows the tests draw with, transcribed from
// testdata/catalog/flv.csv.
var (
	// celeryRow is row id 1153. Its reference carries the 021 the reference barcode
	// of §18 is built on, so the label of the goldens shows the very symbol
	// symbol_test.go decodes.
	celeryRow = domain.Product{
		ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
		CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
	}
	// lentilRow is row id 20. Its name carries U+2665, which Carlito has no glyph
	// for: it is what puts the documented fallback in a golden instead of in a
	// comment.
	lentilRow = domain.Product{
		ID: "20", Name: "LENTILLES VERTES ♥ *", Reference: "0493171000007",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 789,
		CategoryCode: "V", Qualification: domain.Weighable, CSVLine: 20,
	}
	// tommeRow is row id 3511, the LONGEST name of the authentic file at 69
	// characters. It is what the automatic reduction cannot save.
	tommeRow = domain.Product{
		ID: "3511", Name: "♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU PERIGORD-MV",
		Reference: "0493773000009", Mode: domain.ByWeight, PriceSuffix: " €/kg",
		UnitPrice: 3269, CategoryCode: "A", Qualification: domain.Weighable, CSVLine: 3511,
	}
	// riceRow is row id 3526. Measured at 298 dots for a 280 dot box, it overflows by
	// enough to need the reduction and by little enough for the reduction to save it.
	riceRow = domain.Product{
		ID: "3526", Name: "Riz long complet BIO - Agidra", Reference: "0493777000005",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 467,
		CategoryCode: "V", Qualification: domain.Weighable, CSVLine: 3526,
	}
)

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// --- Fixtures --------------------------------------------------------------

// logEntry is one line a render wrote to its journal.
type logEntry struct{ level, source, code, message, detail string }

// recordingLog is the journal a test hands a Rasterizer, so that "it journals" is an
// assertion and not a hope.
type recordingLog struct{ entries []logEntry }

func (l *recordingLog) Technical(level, source, code, message, detail string) {
	l.entries = append(l.entries, logEntry{level, source, code, message, detail})
}

// find returns the first entry carrying a code, or nil.
func (l *recordingLog) find(code string) *logEntry {
	for i := range l.entries {
		if l.entries[i].code == code {
			return &l.entries[i]
		}
	}
	return nil
}

// codes lists what was journalled, for a failure message that says what happened
// instead of what did not.
func (l *recordingLog) codes() []string {
	out := make([]string, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e.code)
	}
	return out
}

// newTestRasterizer builds a renderer whose journal a test can read back.
func newTestRasterizer(t *testing.T) (*Rasterizer, *recordingLog) {
	t.Helper()
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	t.Cleanup(func() { library.Close() })
	log := &recordingLog{}
	r, err := NewRasterizer(library, log)
	if err != nil {
		t.Fatalf("rastériseur : %v", err)
	}
	return r, log
}

// weighing builds the Label one weighing produces, through the single calculation
// path of the application.
func weighing(t *testing.T, product domain.Product, mass domain.Grams, rules domain.PricingRules) domain.Label {
	t.Helper()
	label, err := domain.Price(product, domain.Measurement{Gross: mass}, rules)
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(product.Reference)
	if err != nil {
		t.Fatalf("plan du code %s : %v", product.Reference, err)
	}
	code, err := domain.Generate(product.Reference, int64(mass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "test"
	return label
}

// --- The goldens -----------------------------------------------------------

// TestTheLabelMatchesItsGoldens is the pixel-level record of what §7.3 draws.
//
// Four of them, and each one carries something the others cannot: the production
// label with both tiers, the same one in mono-tarif (where the secondary price must
// be ABSENT, not blank), the same one annotated, and the neutral template with a
// product name Carlito cannot draw on its own.
func TestTheLabelMatchesItsGoldens(t *testing.T) {
	r, log := newTestRasterizer(t)

	identical := domain.IdenticalTemplate()
	neutral := domain.NeutralSingleTemplate()

	for _, c := range []struct {
		name     string
		template *domain.Template
		label    domain.Label
		options  RenderOptions
	}{
		{"label_weighing_identical", &identical,
			weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()), RenderOptions{}},
		{"label_weighing_identical_mono", &identical,
			weighing(t, celeryRow, referenceMass, domain.SingleTierRules()), RenderOptions{}},
		{"label_weighing_identical_annotated", &identical,
			weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()), RenderOptions{Annotate: true}},
		{"label_weighing_neutral_single", &neutral,
			weighing(t, lentilRow, referenceMass, domain.LaCagetteRules()), RenderOptions{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			img, err := r.Rasterize(c.template, c.label, domain.LocaleFrench, c.options)
			if err != nil {
				t.Fatalf("Rasterize : %v", err)
			}
			compareGolden(t, c.name, img)
		})
	}
	// The lentil name carries U+2665; the celery one does not. The fallback must have
	// fired exactly there, and nowhere else.
	if entry := log.find(codeGlyphMissing); entry != nil {
		t.Errorf("caractère manquant journalisé alors que le repli DejaVu doit le couvrir : %+v", entry)
	}
}

// compareGolden checks an image against its recorded PNG, or rewrites it under
// -update.
func compareGolden(t *testing.T, name string, img *image.Gray) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".png")
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
			"-run TestTheLabelMatchesItsGoldens -update »", err)
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
		t.Errorf("%d pixels diffèrent du golden %s, le premier en (%d ; %d) — si le rendu a "+
			"changé EXPRÈS, régénérer avec « -update » et le dire dans le commit ; sinon, "+
			"c'est le tracé qui a dérivé", differing, path, firstX, firstY)
	}
}

// --- The symbol keeps its grid inside the whole label ----------------------

// TestTheSymbolKeepsItsGridInTheCompleteRender carries invariant 2 of §7.4 over to
// the finished label.
//
// symbol_test.go proves it on a bitmap the size of the block, at the origin. Here the
// block sits at its real place on a 320 x 203 label, after five text fields and a
// differentiated threshold have been through: if the engine ever shifted the symbol
// by its own arithmetic instead of by SymbolOptions, this is where it would show.
func TestTheSymbolKeepsItsGridInTheCompleteRender(t *testing.T) {
	r, _ := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	img, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}

	o := NewSymbolOptions(template)
	modules, err := domain.Modules(domain.EAN13(referenceCode))
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}

	band := image.Rect(o.XDots, o.YDots, o.XDots+o.TotalWidthDots(), o.YDots+o.BarHeightDots)
	first, last, ok := inkColumnRange(img, band)
	if !ok {
		t.Fatal("aucune barre encrée dans l'étiquette complète")
	}
	if drawn := last - first + 1; drawn != 223 {
		t.Errorf("les barres s'étendent sur %d dots dans l'étiquette complète, attendu 223", drawn)
	}
	if want := o.barsLeft(); first != want {
		t.Errorf("première barre en x=%d, attendue en x=%d", first, want)
	}

	// Every module boundary is where the rounded IDEAL position puts it, on the label
	// and not only in the arithmetic.
	row := o.YDots + o.BarHeightDots/2
	for i, bar := range modules {
		x := o.barsLeft() + o.edge(i)
		if got := isInk(img, x, row); got != bar {
			t.Fatalf("module %d : x=%d %s alors que la séquence dit %s — la grille du "+
				"symbole a glissé dans le rendu complet",
				i, x, colourName(got), colourName(bar))
		}
	}

	// And the drift is still bounded, measured on the drawn edges themselves.
	worst, worstAt := 0.0, 0
	for i := 0; i <= barModules; i++ {
		ideal := float64(i) * float64(o.ModuleMilliDots) / 1000
		err := float64(o.edge(i)) - ideal
		if abs(err) > 0.5 {
			t.Errorf("bord %d : %+.3f dot de la position idéale", i, err)
		}
		if abs(err) > worst {
			worst, worstAt = abs(err), i
		}
	}
	tenth := abs(float64(o.edge(10)) - 10*float64(o.ModuleMilliDots)/1000)
	end := abs(float64(o.edge(barModules)) - float64(barModules)*float64(o.ModuleMilliDots)/1000)
	if end > tenth {
		t.Errorf("erreur au dernier bord %.3f contre %.3f au dixième : l'erreur croît avec "+
			"l'indice, donc les bords sont accumulés", end, tenth)
	}
	t.Logf("symbole posé en (%d ; %d) sur une étiquette de %v · erreur maximale %.3f dot au bord %d",
		o.XDots, o.YDots, img.Bounds().Max, worst, worstAt)
}

// --- The weight of the 7 pt field, both ways -------------------------------

// TestTheSevenPointFieldKeepsTheWeightItsTemplateAsksFor tests the automatic switch
// to bold in BOTH directions, because a rule with a named exception needs both.
//
// weighing_identical carries auto_bold:false on secondary_total_price: the source
// (reports/EtataImprimer.report, label LabelAPayer) carries no FontWeight, so the
// solidarity price prints in REGULAR, and bolding it would be the one visible
// departure from the original — which A1 forbids (§7.2). A template that does not
// opt out gets the rule.
func TestTheSevenPointFieldKeepsTheWeightItsTemplateAsksFor(t *testing.T) {
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	for _, c := range []struct {
		name     string
		autoBold bool
		wantBold bool
	}{
		{"auto_bold false, le gabarit de production", false, false},
		{"auto_bold true, un gabarit qui ne se prononce pas", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, _ := newTestRasterizer(t)
			template := domain.IdenticalTemplate()
			index := elementIndex(t, &template, domain.FieldSecondaryTotalPrice)
			element := &template.Elements[index]
			element.AutoBold = c.autoBold

			// The premise of the whole rule: this field IS under the 20 dot mark.
			if em := template.Media.MilliDots(element.FontSizeUM); em >= autoBoldBelowDots*1000 {
				t.Fatalf("l'em du champ vaut %d milli-dots : il n'est plus sous les %d dots "+
					"et ce test ne démontre plus rien", em, autoBoldBelowDots)
			}

			img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
			if err != nil {
				t.Fatalf("Rasterize : %v", err)
			}
			box := elementBox(&template, *element)
			regular := fieldOnItsOwn(t, r, &template, *element, label, false)
			bold := fieldOnItsOwn(t, r, &template, *element, label, true)

			if sameInk(regular, bold, box) {
				t.Fatal("le gras et le maigre sont indiscernables sur ce champ : ce test ne " +
					"pourrait pas rougir")
			}
			want, other, wanted := regular, bold, "maigre"
			if c.wantBold {
				want, other, wanted = bold, regular, "gras"
			}
			if !sameInk(img, want, box) {
				t.Errorf("le champ n'est pas rendu en %s", wanted)
			}
			if sameInk(img, other, box) {
				t.Errorf("le champ est rendu dans l'autre graisse que %s", wanted)
			}
		})
	}
}

// fieldOnItsOwn draws one element, in a forced weight, on an otherwise blank label of
// the same geometry. Comparing a render against a DRAWING rather than against a pixel
// count is what makes "it is not bold" mean something.
func fieldOnItsOwn(t *testing.T, r *Rasterizer, g *domain.Template, e domain.Element, label domain.Label, bold bool) *image.Gray {
	t.Helper()
	forced := e
	forced.Bold = bold
	forced.AutoBold = false

	w, _ := wordsFor(domain.LocaleFrench)
	text, err := fieldText(e.Field, label, w)
	if err != nil {
		t.Fatalf("contenu du champ %q : %v", e.Field, err)
	}
	box := textBox(g, e)
	p, err := r.place(g, forced, text, fixed.I(box.Dx()))
	if err != nil {
		t.Fatalf("placement : %v", err)
	}
	img := image.NewGray(image.Rect(0, 0,
		roundDots(g.Media, g.Media.WidthUM), roundDots(g.Media, g.Media.HeightUM)))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	pen := fixed.I(box.Min.X)
	if e.Align == domain.AlignRight {
		pen = fixed.I(box.Max.X) - p.width
	}
	drawRuns(img, p.runs, pen, baselineDots(g, e))
	applyThreshold(img, img.Bounds(), textThreshold(g))
	return img
}

// sameInk reports whether two renders carry the same dots inside a box.
func sameInk(a, b *image.Gray, box image.Rectangle) bool {
	box = box.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if isInk(a, x, y) != isInk(b, x, y) {
				return false
			}
		}
	}
	return true
}

// elementIndex finds a field in a template, and fails rather than return -1.
func elementIndex(t *testing.T, g *domain.Template, field string) int {
	t.Helper()
	for i, e := range g.Elements {
		if e.Field == field {
			return i
		}
	}
	t.Fatalf("le gabarit %s ne place pas le champ %q", g.Name, field)
	return -1
}

// --- Mono-tarif ------------------------------------------------------------

// TestAMonoTierLabelDropsTheSecondaryPrice: the field DISAPPEARS, and no `if` in the
// rendering code says so — Element.Active does (§7.2).
func TestAMonoTierLabelDropsTheSecondaryPrice(t *testing.T) {
	r, _ := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	secondary := template.Elements[elementIndex(t, &template, domain.FieldSecondaryTotalPrice)]
	box := elementBox(&template, secondary)

	// The template stays valid for a station that runs one tier: rules 3, 5 and 8 are
	// about what is actually inked, so a mono-tarif poste must not be refused a
	// template because of a field it will never draw.
	if faults := template.Validate(1); len(faults) != 0 {
		t.Errorf("le gabarit est refusé en mono-tarif : %v", faults)
	}

	mono, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.SingleTierRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize mono-tarif : %v", err)
	}
	if _, inked := inkBounds(mono, box); inked {
		t.Errorf("la boîte du prix solidaire %v est encrée sur une étiquette mono-tarif", box)
	}

	// And the same box IS inked when the grid has two tiers — without which the test
	// above would pass on a renderer that draws nothing at all.
	dual, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize double tarif : %v", err)
	}
	if _, inked := inkBounds(dual, box); !inked {
		t.Errorf("la boîte du prix solidaire %v est vide en double tarif", box)
	}

	// The rest of the label is untouched: the barcode block is identical either way.
	o := NewSymbolOptions(template)
	if !sameInk(mono, dual, o.Bounds()) {
		t.Error("le symbole diffère entre mono-tarif et double tarif")
	}
}

// --- The final thresholding ------------------------------------------------

// TestTheRenderCarriesNothingButPureBlackAndWhite: the head is binary, and a render
// that kept greys would let the driver dither it into irregular bars (§7.3).
func TestTheRenderCarriesNothingButPureBlackAndWhite(t *testing.T) {
	r, _ := newTestRasterizer(t)
	for name, template := range domain.ShippedTemplates() {
		for _, annotate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/annotate=%v", name, annotate), func(t *testing.T) {
				g := template
				img, err := r.Rasterize(&g, weighing(t, lentilRow, referenceMass, domain.LaCagetteRules()),
					domain.LocaleFrench, RenderOptions{Annotate: annotate})
				if err != nil {
					t.Fatalf("Rasterize : %v", err)
				}
				grey, black, white := 0, 0, 0
				var firstGrey image.Point
				var firstValue uint8
				for i, v := range img.Pix {
					switch v {
					case 0x00:
						black++
					case 0xFF:
						white++
					default:
						if grey == 0 {
							firstGrey = image.Pt(i%img.Stride, i/img.Stride)
							firstValue = v
						}
						grey++
					}
				}
				if grey > 0 {
					t.Errorf("%d dots ne sont ni 0x00 ni 0xFF, le premier en %v vaut 0x%02X — "+
						"le pilote tramerait ces gris et produirait des barres irrégulières",
						grey, firstGrey, firstValue)
				}
				if black == 0 {
					t.Error("aucun dot noir : le seuillage a effacé l'étiquette")
				}
				t.Logf("%d dots noirs, %d blancs", black, white)
			})
		}
	}
}

// TestTheThresholdIsDifferentiated: 0x80 on the symbol, TextThreshold on the rest.
//
// It is checked where it is observable: a grey laid inside the symbol block and a
// grey laid outside it, both between the two thresholds, must come out differently.
func TestTheThresholdIsDifferentiated(t *testing.T) {
	template := domain.IdenticalTemplate()
	if template.TextThreshold != defaultTextThreshold {
		t.Fatalf("le gabarit porte un seuil texte de 0x%02X : ce test suppose 0x%02X",
			template.TextThreshold, defaultTextThreshold)
	}
	o := NewSymbolOptions(template)
	img := image.NewGray(image.Rect(0, 0, 320, 203))
	for i := range img.Pix {
		img.Pix[i] = 0x70 // between 0x68 and 0x80
	}

	applyThreshold(img, o.Bounds(), symbolThreshold)
	for _, rest := range surrounding(img.Bounds(), o.Bounds()) {
		applyThreshold(img, rest, textThreshold(&template))
	}

	inside := image.Pt(o.XDots+1, o.YDots+1)
	outside := image.Pt(o.XDots+1, o.YDots-1)
	if !isInk(img, inside.X, inside.Y) {
		t.Errorf("un gris 0x70 dans le symbole %v est resté blanc : le seuil 0x%02X n'y est pas appliqué",
			o.Bounds(), symbolThreshold)
	}
	if isInk(img, outside.X, outside.Y) {
		t.Errorf("un gris 0x70 hors du symbole est devenu noir : le seuil texte 0x%02X n'y est pas appliqué",
			textThreshold(&template))
	}
}

// TestAZeroTextThresholdFallsBackRatherThanBlankingTheLabel: with a threshold of
// zero no dot is ever below it, so obeying the field literally would print a blank.
func TestAZeroTextThresholdFallsBackRatherThanBlankingTheLabel(t *testing.T) {
	template := domain.IdenticalTemplate()
	template.TextThreshold = 0
	if got := textThreshold(&template); got != defaultTextThreshold {
		t.Errorf("seuil 0x%02X pour un gabarit muet, attendu 0x%02X", got, defaultTextThreshold)
	}

	r, _ := newTestRasterizer(t)
	img, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	nameBox := elementBox(&template, template.Elements[elementIndex(t, &template, domain.FieldProductName)])
	if _, inked := inkBounds(img, nameBox); !inked {
		t.Error("le nom du produit est vide : un seuil de texte à zéro a effacé l'étiquette")
	}
}

// --- The automatic reduction, both outcomes --------------------------------

// TestALongNameIsReducedThenTruncated covers the two outcomes of §7.3, on the two
// real names of the authentic catalog that produce them.
func TestALongNameIsReducedThenTruncated(t *testing.T) {
	template := domain.IdenticalTemplate()
	nameElement := template.Elements[elementIndex(t, &template, domain.FieldProductName)]
	box := textBox(&template, nameElement)

	t.Run("réduit et tient", func(t *testing.T) {
		r, log := newTestRasterizer(t)
		p := placeName(t, r, &template, nameElement, riceRow.Name)

		if p.sizeUM >= nameElement.FontSizeUM {
			t.Fatalf("« %s » tient au corps nominal de %d µm : ce nom ne fait plus déborder "+
				"la boîte et ce cas ne teste plus la réduction",
				riceRow.Name, nameElement.FontSizeUM)
		}
		if p.sizeUM < reductionFloor(nameElement) {
			t.Errorf("corps %d µm sous le plancher %d µm", p.sizeUM, reductionFloor(nameElement))
		}
		if p.truncated {
			t.Errorf("« %s » a été tronqué alors que la réduction suffisait", riceRow.Name)
		}
		if p.text != riceRow.Name {
			t.Errorf("le texte rendu est %q, attendu le nom entier", p.text)
		}
		if p.width > fixed.I(box.Dx()) {
			t.Errorf("%.2f dots après réduction pour une boîte de %d", float64(p.width)/64, box.Dx())
		}
		if len(log.entries) != 0 {
			t.Errorf("une réduction qui aboutit ne journalise rien, or : %v", log.codes())
		}

		// A REDUCED FIELD STAYS ON ITS LINE. The baseline comes from the NOMINAL body,
		// so shrinking a name must not drop it a dot below the line it shares with the
		// rest of the label. The reference is drawn at the same reduced body but on the
		// baseline of the ELEMENT, which is exactly the property under test.
		label := weighing(t, riceRow, referenceMass, domain.LaCagetteRules())
		img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
		if err != nil {
			t.Fatalf("Rasterize : %v", err)
		}
		onItsLine := fieldOnItsOwn(t, r, &template, nameElement, label, false)
		if !sameInk(img, onItsLine, elementBox(&template, nameElement)) {
			t.Error("le nom réduit n'est pas tracé sur la ligne de base de son élément : " +
				"réduire un champ le décale de la ligne qu'il partage avec les autres")
		}

		t.Logf("« %s » : %d µm → %d µm, %.2f dots pour %d",
			riceRow.Name, nameElement.FontSizeUM, p.sizeUM, float64(p.width)/64, box.Dx())
	})

	t.Run("réduit, ne tient pas, tronqué et journalisé", func(t *testing.T) {
		r, log := newTestRasterizer(t)
		label := weighing(t, tommeRow, referenceMass, domain.LaCagetteRules())
		img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
		if err != nil {
			t.Fatalf("Rasterize : %v", err)
		}

		entry := log.find(codeFieldTruncated)
		if entry == nil {
			t.Fatalf("aucune anomalie %s journalisée : §7.3 interdit de sortir en silence "+
				"(journalisé : %v)", codeFieldTruncated, log.codes())
		}
		if entry.source != "printer" {
			t.Errorf("source %q, attendu « printer »", entry.source)
		}
		if !strings.Contains(entry.message, domain.FieldProductName) {
			t.Errorf("le message %q ne nomme pas le champ fautif", entry.message)
		}
		if !strings.Contains(entry.detail, tommeRow.Name) {
			t.Errorf("le détail %q ne cite pas le nom d'origine", entry.detail)
		}

		p := placeName(t, r, &template, nameElement, tommeRow.Name)
		if !p.truncated {
			t.Fatal("le plus long nom du fichier authentique n'a pas été tronqué")
		}
		if p.sizeUM != reductionFloor(nameElement) {
			t.Errorf("tronqué au corps %d µm et non au plancher %d µm : §7.3 tronque au "+
				"DERNIER corps valide", p.sizeUM, reductionFloor(nameElement))
		}
		if !strings.HasSuffix(p.text, ellipsis) {
			t.Errorf("le texte tronqué %q ne porte pas d'ellipse", p.text)
		}
		if p.width > fixed.I(box.Dx()) {
			t.Errorf("%.2f dots après troncature pour une boîte de %d", float64(p.width)/64, box.Dx())
		}
		// The ink really stops inside the box: a truncation that only shortened the
		// string would still overflow if the drawing ignored it.
		if ink, ok := inkBounds(img, image.Rect(0, 0, img.Bounds().Dx(), box.Max.Y)); ok && ink.Max.X > box.Max.X {
			t.Errorf("l'encre du nom s'étend jusqu'en x=%d, au-delà de la boîte %v", ink.Max.X, box)
		}
		t.Logf("« %s » → « %s » au corps %d µm", tommeRow.Name, p.text, p.sizeUM)
	})
}

// placeName runs the reduction on one product name, which is what the two cases
// above differ by.
func placeName(t *testing.T, r *Rasterizer, g *domain.Template, e domain.Element, name string) placement {
	t.Helper()
	p, err := r.place(g, e, name, fixed.I(textBox(g, e).Dx()))
	if err != nil {
		t.Fatalf("placement de « %s » : %v", name, err)
	}
	return p
}

// TestTheReductionNeverGoesBelowTheHardFloor: hard rule 9 sets 1800 µm for every
// field, and an element that declares no floor of its own does not escape it.
func TestTheReductionNeverGoesBelowTheHardFloor(t *testing.T) {
	if got := reductionFloor(domain.Element{FontSizeUM: 3175}); got != domain.MinFontSizeUM {
		t.Errorf("plancher %d µm pour un élément muet, attendu %d", got, domain.MinFontSizeUM)
	}
	if got := reductionFloor(domain.Element{FontSizeUM: 3175, MinFontSizeUM: 2200}); got != 2200 {
		t.Errorf("plancher %d µm, attendu le 2200 déclaré par l'élément", got)
	}
	// A floor below the hard one is not honoured; a floor above the nominal body
	// cannot be, since the reduction only ever goes down.
	if got := reductionFloor(domain.Element{FontSizeUM: 3175, MinFontSizeUM: 500}); got != domain.MinFontSizeUM {
		t.Errorf("plancher %d µm pour un élément qui déclare 500 µm, attendu %d",
			got, domain.MinFontSizeUM)
	}
	if got := reductionFloor(domain.Element{FontSizeUM: 2000, MinFontSizeUM: 3000}); got != 2000 {
		t.Errorf("plancher %d µm au-dessus du corps nominal", got)
	}
}

// TestABoxTooNarrowEvenForAnEllipsisStillPrintsTheRestOfTheLabel: a box that narrow
// is a template fault, not a data one, and the customer still gets a barcode.
func TestABoxTooNarrowEvenForAnEllipsisStillPrintsTheRestOfTheLabel(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	index := elementIndex(t, &template, domain.FieldProductName)
	template.Elements[index].WidthUM = 400 // 3.2 dots: not even "…" fits

	img, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if log.find(codeFieldTruncated) == nil {
		t.Errorf("aucune anomalie %s journalisée (journalisé : %v)", codeFieldTruncated, log.codes())
	}
	box := elementBox(&template, template.Elements[index])
	if _, inked := inkBounds(img, box); inked {
		t.Errorf("la boîte %v, trop étroite pour une ellipse, a quand même reçu de l'encre", box)
	}
	// The label is still a label: the symbol is there, whole.
	o := NewSymbolOptions(template)
	if first, last, ok := inkColumnRange(img, image.Rect(o.XDots, o.YDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.BarHeightDots)); !ok || last-first+1 != 223 {
		t.Error("le symbole a souffert d'un champ texte impossible à placer")
	}
}

// TestTheEngineRefusesToInventContent: three questions a field cannot answer, and
// none of them is answered by a plausible-looking string.
func TestTheEngineRefusesToInventContent(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)

	if _, err := fieldText("prix_au_metre", domain.Label{}, w); err == nil {
		t.Error("un champ inconnu a produit un contenu : la liste des FieldID est fermée (règle 7)")
	}
	mono := weighing(t, celeryRow, referenceMass, domain.SingleTierRules())
	if _, err := fieldText(domain.FieldSecondaryTotalPrice, mono, w); err == nil {
		t.Error("le prix secondaire a été produit sur une grille mono-tarif : il n'existe pas")
	}
	if _, err := fieldText(domain.FieldQuantity, domain.Label{Mode: domain.SaleMode(9)}, w); err == nil {
		t.Error("un mode de vente inconnu a produit une quantité")
	}
	if _, err := fieldText(domain.FieldPrimaryUnitPrice, domain.Label{}, w); err == nil {
		t.Error("un prix principal a été produit sans tarif")
	}
}

// --- What the fields carry (A7) --------------------------------------------

// TestTheFieldsCarryWhatArbitrationSevenSays reproduces the three strings §7.2 spells
// out, from the example the legacy help screen states: garlic at 5,32 €/kg, 1,236 kg.
func TestTheFieldsCarryWhatArbitrationSevenSays(t *testing.T) {
	garlic := domain.Product{
		ID: "1", Name: "AIL", Reference: "0493021000003",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 532,
	}
	label := weighing(t, garlic, referenceMass, domain.LaCagetteRules())
	w, _ := wordsFor(domain.LocaleFrench)

	for _, c := range []struct{ field, want string }{
		{domain.FieldProductName, "AIL"},
		{domain.FieldQuantity, "1,236 kg"},
		{domain.FieldPrimaryUnitPrice, "A: 4,79 €/kg"},
		{domain.FieldSecondaryTotalPrice, "S: 6,58 €"},
		{domain.FieldPrimaryTotalPrice, "A: 5,92 €"},
	} {
		got, err := fieldText(c.field, label, w)
		if err != nil {
			t.Fatalf("%s : %v", c.field, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, attendu %q", c.field, got, c.want)
		}
	}
}

// TestThePriceSuffixComesFromTheProduct: « €/kg » is not a constant of the template
// (§7.2). Two products, two suffixes, the same template.
func TestThePriceSuffixComesFromTheProduct(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)
	for _, suffix := range []string{" €/kg", " € le litre", " € l'unité"} {
		product := domain.Product{
			Name: "PRODUIT", Reference: "0493021000003", Mode: domain.ByWeight,
			PriceSuffix: suffix, UnitPrice: 532,
		}
		label := weighing(t, product, referenceMass, domain.LaCagetteRules())
		got, err := fieldText(domain.FieldPrimaryUnitPrice, label, w)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if want := "A: 4,79" + suffix; got != want {
			t.Errorf("prix unitaire = %q, attendu %q", got, want)
		}
	}
}

// TestAMonoTierGridPrintsNoPrefix: with one tier the Abbrev is empty, and a bare
// « : » in front of a price would introduce nothing.
func TestAMonoTierGridPrintsNoPrefix(t *testing.T) {
	label := weighing(t, celeryRow, referenceMass, domain.SingleTierRules())
	w, _ := wordsFor(domain.LocaleFrench)
	got, err := fieldText(domain.FieldPrimaryTotalPrice, label, w)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if strings.Contains(got, ":") {
		t.Errorf("prix mono-tarif = %q : il porte un préfixe alors que l'Abbrev est vide", got)
	}
	if want := "4,14 €"; got != want {
		t.Errorf("prix mono-tarif = %q, attendu %q", got, want)
	}
}

// TestAProductSoldByUnitCountsItsUnits: "1 unité", "3 unités" — the legacy wording,
// kept.
func TestAProductSoldByUnitCountsItsUnits(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)
	for _, c := range []struct {
		quantity int
		want     string
	}{{1, "1 unité"}, {3, "3 unités"}} {
		label := domain.Label{Mode: domain.ByUnit, Quantity: c.quantity}
		got, err := fieldText(domain.FieldQuantity, label, w)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if got != c.want {
			t.Errorf("quantité pour %d = %q, attendu %q", c.quantity, got, c.want)
		}
	}
}

// TestAnUnknownLocaleFallsBackToFrenchAndSaysSo: a customer is waiting; a label in
// French beats no label, and silence beats neither.
func TestAnUnknownLocaleFallsBackToFrenchAndSaysSo(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	if _, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.Locale("nl-BE"), RenderOptions{}); err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if log.find(codeUnknownLocale) == nil {
		t.Errorf("aucune anomalie %s journalisée (journalisé : %v)", codeUnknownLocale, log.codes())
	}

	// And the empty locale IS French: a PrintJob whose field was never filled must
	// print a label, not a fault.
	empty, _ := newTestRasterizer(t)
	if _, err := empty.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		"", RenderOptions{}); err != nil {
		t.Fatalf("Rasterize avec une langue vide : %v", err)
	}
}

// --- The media comes from the template -------------------------------------

// TestTheMediaAndTheResolutionComeFromTheTemplate: no constant of the engine decides
// how big a label is, and dots_per_mm is the SINGLE SOURCE of resolution (mineur-3).
func TestTheMediaAndTheResolutionComeFromTheTemplate(t *testing.T) {
	r, _ := newTestRasterizer(t)
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())
	nameField := domain.FieldProductName

	widths := map[float64]int{}
	for _, dotsPerMM := range []float64{8, 12} {
		template := domain.IdenticalTemplate()
		template.Media.DotsPerMM = dotsPerMM
		img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
		if err != nil {
			t.Fatalf("Rasterize à %g dots/mm : %v", dotsPerMM, err)
		}
		wantW := roundDots(template.Media, template.Media.WidthUM)
		wantH := roundDots(template.Media, template.Media.HeightUM)
		if got := img.Bounds().Max; got.X != wantW || got.Y != wantH {
			t.Errorf("%g dots/mm : image %v, attendu %d × %d", dotsPerMM, got, wantW, wantH)
		}
		box := elementBox(&template, template.Elements[elementIndex(t, &template, nameField)])
		ink, ok := inkBounds(img, box)
		if !ok {
			t.Fatalf("%g dots/mm : le nom du produit n'est pas tracé", dotsPerMM)
		}
		widths[dotsPerMM] = ink.Dx()
	}

	// 8 -> 320 x 203 dots, 12 -> 480 x 305: the numbers are the template's, not the
	// engine's. And the type size follows, because the DPI handed to the face is
	// derived from dots_per_mm alone.
	ratio := float64(widths[12]) / float64(widths[8])
	if ratio < 1.4 || ratio > 1.6 {
		t.Errorf("le nom mesure %d dots à 8 dots/mm et %d à 12, soit un rapport de %.3f : "+
			"le corps ne suit pas la résolution du gabarit", widths[8], widths[12], ratio)
	}
	t.Logf("nom du produit : %d dots à 8 dots/mm, %d à 12 (rapport %.3f)",
		widths[8], widths[12], ratio)
}

// --- The frame -------------------------------------------------------------

// TestTheFramedFieldCarriesItsOneDotRule: primary_unit_price is framed on the
// production label (§7.2), and the rule is one dot.
func TestTheFramedFieldCarriesItsOneDotRule(t *testing.T) {
	r, _ := newTestRasterizer(t)
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	framed := domain.IdenticalTemplate()
	index := elementIndex(t, &framed, domain.FieldPrimaryUnitPrice)
	if !framed.Elements[index].Framed {
		t.Fatal("le gabarit de production n'encadre plus le prix au kilo (§7.2)")
	}
	box := elementBox(&framed, framed.Elements[index])

	bare := domain.IdenticalTemplate()
	bare.Elements[index].Framed = false

	with, err := r.Rasterize(&framed, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	without, err := r.Rasterize(&bare, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize sans cadre : %v", err)
	}

	// The four sides are inked over their whole length -- a rule with a gap is not a
	// rule.
	for _, side := range []struct {
		name string
		r    image.Rectangle
	}{
		{"haut", image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+1)},
		{"bas", image.Rect(box.Min.X, box.Max.Y-1, box.Max.X, box.Max.Y)},
		{"gauche", image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y)},
		{"droite", image.Rect(box.Max.X-1, box.Min.Y, box.Max.X, box.Max.Y)},
	} {
		for y := side.r.Min.Y; y < side.r.Max.Y; y++ {
			for x := side.r.Min.X; x < side.r.Max.X; x++ {
				if !isInk(with, x, y) {
					t.Fatalf("le côté %s du cadre n'est pas encré en (%d ; %d)", side.name, x, y)
				}
			}
		}
	}

	// It really is the FRAME that draws them, and not the text: the left column and
	// the four corners are places a right-aligned price never reaches, and they are
	// blank as soon as framed is false.
	left := image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y)
	if _, inked := inkBounds(without, left); inked {
		t.Errorf("la colonne gauche %v est encrée alors que framed est faux : ce test "+
			"confondrait le cadre avec le texte", left)
	}
	for _, corner := range []image.Point{
		{X: box.Min.X, Y: box.Min.Y}, {X: box.Max.X - 1, Y: box.Min.Y},
		{X: box.Min.X, Y: box.Max.Y - 1}, {X: box.Max.X - 1, Y: box.Max.Y - 1},
	} {
		if isInk(without, corner.X, corner.Y) {
			t.Errorf("le coin %v est encré sans cadre", corner)
		}
	}

	// One dot thick, and no more: the column just inside the left rule carries
	// nothing between the two horizontal rules. The text of a right-aligned price
	// starts far to the right of it, so anything found there is a second stroke.
	inner := image.Rect(box.Min.X+1, box.Min.Y+1, box.Min.X+2, box.Max.Y-1)
	if _, inked := inkBounds(with, inner); inked {
		t.Errorf("la colonne %v contre le trait du cadre est encrée : le cadre fait plus "+
			"d'un dot", inner)
	}
}

// --- The shared baseline ---------------------------------------------------

// TestTheTwoPricesShareABaseline is the guard on emAscentPerMille.
//
// domain.IdenticalTemplate places the 7 pt price by subtracting 750/1000 of its em
// from the baseline of the 11 pt one. If this package ever placed baselines with
// another ascent — face.Metrics().Ascent, say, which is 0.952 em for Carlito — the
// two prices would print on two different lines, which is precisely what that
// template says it fixed.
func TestTheTwoPricesShareABaseline(t *testing.T) {
	template := domain.IdenticalTemplate()
	primary := template.Elements[elementIndex(t, &template, domain.FieldPrimaryTotalPrice)]
	secondary := template.Elements[elementIndex(t, &template, domain.FieldSecondaryTotalPrice)]

	if primary.FontSizeUM == secondary.FontSizeUM {
		t.Fatal("les deux prix sont au même corps : ce test ne démontre plus rien")
	}
	a, b := baselineDots(&template, primary), baselineDots(&template, secondary)
	if a != b {
		t.Errorf("le prix adhérent est sur la ligne de base %d et le solidaire sur %d : "+
			"le moteur ne place pas ses lignes de base avec l'ascendante que le gabarit "+
			"a utilisée pour les aligner", a, b)
	}
	t.Logf("les deux prix partagent la ligne de base %d dots", a)
}

// --- The volunteer's ±1 dot adjustment -------------------------------------

// TestTheOffsetMovesTheWHOLELabel: the ±1 dot arrows of the admin screen move the
// text AND the symbol, or the adjustment would tear the label in two.
//
// Rule 6 already applies the offset before validation; this is the other half —
// applying it when drawing. It is checked as an exact translation rather than as
// "something moved", because a renderer that shifted the text and not the barcode
// would still pass a weaker test.
func TestTheOffsetMovesTheWHOLELabel(t *testing.T) {
	r, _ := newTestRasterizer(t)
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	plain := domain.IdenticalTemplate()
	shifted := domain.IdenticalTemplate()
	shifted.OffsetXDots, shifted.OffsetYDots = 1, 1

	before, err := r.Rasterize(&plain, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	after, err := r.Rasterize(&shifted, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize décalé : %v", err)
	}

	moved, differing := 0, 0
	b := before.Bounds()
	for y := b.Min.Y; y < b.Max.Y-1; y++ {
		for x := b.Min.X; x < b.Max.X-1; x++ {
			if isInk(before, x, y) != isInk(after, x+1, y+1) {
				differing++
			}
			if isInk(before, x, y) {
				moved++
			}
		}
	}
	if differing != 0 {
		t.Errorf("%d dots ne sont pas à leur place après un décalage de (1 ; 1) : le "+
			"décalage ne translate pas l'étiquette entière", differing)
	}
	if moved == 0 {
		t.Fatal("l'étiquette de référence est vide : ce test ne compare rien")
	}
	t.Logf("%d dots translatés à l'identique", moved)
}

// --- What Rasterize refuses ------------------------------------------------

// TestRasterizeRefusesWhatWouldPrintAWrongLabel: each of these would produce
// something, and something wrong.
func TestRasterizeRefusesWhatWouldPrintAWrongLabel(t *testing.T) {
	r, _ := newTestRasterizer(t)
	sound := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	noResolution := domain.IdenticalTemplate()
	noResolution.Media.DotsPerMM = 0

	noMedia := domain.IdenticalTemplate()
	noMedia.Media.WidthUM = 0

	tooSmall := domain.IdenticalTemplate()
	tooSmall.Media.HeightUM = 10_000 // 80 dots: the symbol block no longer fits

	noBarcode := sound
	noBarcode.Barcode = ""

	wrongBarcode := sound
	wrongBarcode.Barcode = domain.EAN13("049302101236A")

	noTier := sound
	noTier.PrimaryLine = nil

	for _, c := range []struct {
		name     string
		template *domain.Template
		label    domain.Label
	}{
		{"aucun gabarit", nil, sound},
		{"média sans résolution", &noResolution, sound},
		{"média sans largeur", &noMedia, sound},
		{"média trop court pour le symbole", &tooSmall, sound},
		{"étiquette sans code-barres", ptr(domain.IdenticalTemplate()), noBarcode},
		{"code-barres non numérique", ptr(domain.IdenticalTemplate()), wrongBarcode},
		{"étiquette sans tarif principal", ptr(domain.IdenticalTemplate()), noTier},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := r.Rasterize(c.template, c.label, domain.LocaleFrench, RenderOptions{}); err == nil {
				t.Error("accepté sans broncher : cette étiquette partirait à l'impression")
			}
		})
	}
}

// ptr is the address of a template built inline.
func ptr(t domain.Template) *domain.Template { return &t }

// TestNewRasterizerRefusesToRenderWithoutAJournal: §7.3 requires the automatic
// reduction to journal, and a renderer with nobody to tell would truncate in silence.
func TestNewRasterizerRefusesToRenderWithoutAJournal(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	if _, err := NewRasterizer(library, nil); err == nil {
		t.Error("un rastériseur sans journal a été accepté : il tronquerait un nom en silence")
	}
	if _, err := NewRasterizer(nil, &recordingLog{}); err == nil {
		t.Error("un rastériseur sans polices a été accepté")
	}
}

// --- The §7.3 entry point --------------------------------------------------

// TestTheFreeRasterizeRendersAndStaysAudible: the signature of §7.3 carries neither
// library nor journal, and it must still not truncate a product name in silence.
func TestTheFreeRasterizeRendersAndStaysAudible(t *testing.T) {
	template := domain.IdenticalTemplate()

	img, err := Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if got, want := img.Bounds().Max, image.Pt(320, 203); got != want {
		t.Errorf("image %v, attendu %v", got, want)
	}

	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	if _, err := Rasterize(&template, weighing(t, tommeRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{}); err != nil {
		t.Fatalf("Rasterize du nom le plus long : %v", err)
	}
	if !strings.Contains(captured.String(), codeFieldTruncated) {
		t.Errorf("le journal ne porte pas %s après une troncature : le point d'entrée de "+
			"§7.3 sort en silence.\nJournal : %s", codeFieldTruncated, captured.String())
	}
}

// --- The annotation --------------------------------------------------------

// TestTheAnnotationIsAnOverlayAndNothingMore: it adds dots, it never removes any, and
// it survives the thresholding it is drawn after.
func TestTheAnnotationIsAnOverlayAndNothingMore(t *testing.T) {
	r, _ := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	plain, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	marked, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{Annotate: true})
	if err != nil {
		t.Fatalf("Rasterize annoté : %v", err)
	}

	added := 0
	for y := plain.Bounds().Min.Y; y < plain.Bounds().Max.Y; y++ {
		for x := plain.Bounds().Min.X; x < plain.Bounds().Max.X; x++ {
			switch {
			case isInk(plain, x, y) && !isInk(marked, x, y):
				t.Fatalf("l'annotation a effacé le dot (%d ; %d) de l'étiquette", x, y)
			case !isInk(plain, x, y) && isInk(marked, x, y):
				added++
			}
		}
	}
	if added == 0 {
		t.Fatal("l'annotation n'a rien tracé")
	}

	// The ruler starts at the origin of the label, which is what makes it useful for
	// checking an offset: a tick sits on every millimetre of the top edge.
	for mm := 1; mm <= 3; mm++ {
		x := int(float64(mm)*template.Media.DotsPerMM + 0.5)
		if !isInk(marked, x, 0) {
			t.Errorf("aucune graduation en x=%d (%d mm) sur le bord haut", x, mm)
		}
	}
	t.Logf("%d dots ajoutés par l'annotation", added)
}

// --- The fallback font -----------------------------------------------------

// TestTheFallbackDrawsWhatCarlitoCannot is not a theoretical case: 127 of the 355
// names of testdata/catalog/flv.csv carry U+2665, and Carlito has no glyph for it.
func TestTheFallbackDrawsWhatCarlitoCannot(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	carlito, err := library.Face(labelFont, 3175, 8, false)
	if err != nil {
		t.Fatalf("fonte : %v", err)
	}
	dejavu, err := library.Face(fallbackFont, 3175, 8, false)
	if err != nil {
		t.Fatalf("fonte de repli : %v", err)
	}
	const heart = '♥'
	if hasGlyph(carlito, heart) {
		t.Fatalf("Carlito dessine désormais U+%04X : ce repli n'a plus d'objet et le "+
			"commentaire de fallbackFont est faux", heart)
	}
	if !hasGlyph(dejavu, heart) {
		t.Fatalf("DejaVu Sans Condensed ne dessine pas U+%04X non plus : un tiers du "+
			"catalogue s'imprimerait avec un trou dans son nom", heart)
	}

	runs, missing := splitRuns(lentilRow.Name, carlito, dejavu)
	if len(missing) != 0 {
		t.Errorf("caractères perdus dans « %s » : %s", lentilRow.Name, describeRunes(missing))
	}
	if len(runs) < 3 {
		t.Errorf("« %s » découpé en %d plages : le cœur doit en ouvrir une à lui seul",
			lentilRow.Name, len(runs))
	}
	used := 0
	for _, run := range runs {
		if run.face == dejavu {
			used++
		}
	}
	if used != 1 {
		t.Errorf("%d plages tracées en repli, attendu 1", used)
	}

	// A string Carlito covers entirely stays ONE run, and that is what keeps the
	// measurement kerned — the acceptance criterion of ADR-020 depends on it.
	whole, _ := splitRuns("A: 4,32 €/kg", carlito, dejavu)
	if len(whole) != 1 {
		t.Errorf("« A: 4,32 €/kg » découpé en %d plages : mesurée par morceaux, la chaîne "+
			"perdrait son crénage et le critère d'ADR-020 se mettrait à échouer", len(whole))
	}
}

// TestACharacterNoEmbeddedFontCarriesIsJournalled: dropped rather than drawn as a
// box, but never dropped quietly.
func TestACharacterNoEmbeddedFontCarriesIsJournalled(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	product := celeryRow
	product.Name = "CELERI 天 SAF" // a Han character neither embedded font carries

	if _, err := r.Rasterize(&template, weighing(t, product, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{}); err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	entry := log.find(codeGlyphMissing)
	if entry == nil {
		t.Fatalf("aucune anomalie %s journalisée (journalisé : %v)", codeGlyphMissing, log.codes())
	}
	if !strings.Contains(entry.detail, "U+5929") {
		t.Errorf("le détail %q ne nomme pas le point de code fautif", entry.detail)
	}
}
