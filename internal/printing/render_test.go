package printing

import (
	"bytes"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The tests of the rendering engine of §7.3.
//
// This file keeps what judges the WHOLE RENDER: the goldens, the symbol held inside the
// complete label, the media that comes from the template, the volunteer's offset, and what
// Rasterize refuses. The fields, the layout, the thresholding, the annotation and the
// fallback font each have their own file, next to their production file; the shared
// fixtures are in harness_test.go.
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
	if got, want := img.Bounds().Max, image.Pt(280, 200); got != want {
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
