package raster

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// What the tests of this package share: the production label, the ink threshold, the
// authentic catalog row, and the three readers that turn a failure into a sentence — which
// dot differs, which argument a command carries, what a print error says.
//
// They used to live in frame_test.go and already served four files.

// inkThreshold is where a grey becomes a burnt dot, read exactly as
// printing.applyThreshold reads it: STRICTLY below is ink. It is the reader's own
// spelling of the rule, deliberately not borrowed from the encoder it checks.
const inkThreshold = 0x80

// The three real catalog rows are not needed here — one is. celeryRow is row 1153 of
// testdata/catalog/flv.csv, and its reference carries the 021 that the reference
// barcode of §18 and the symbol golden of §7.4 are both built on.
var celeryRow = domain.Product{
	ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
	Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
	CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
}

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// --- Fixtures ---------------------------------------------------------------

// productionLabel renders the label of the reference weighing through the single
// calculation path of the application: celery, 1,236 kg, the La Cagette grid.
func productionLabel(t *testing.T) (domain.Template, *image.Gray) {
	t.Helper()
	template := domain.IdenticalTemplate()
	label, err := domain.Price(celeryRow, domain.Measurement{Gross: referenceMass}, domain.LaCagetteRules())
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(celeryRow.Reference)
	if err != nil {
		t.Fatalf("plan du code %s : %v", celeryRow.Reference, err)
	}
	code, err := domain.Generate(celeryRow.Reference, int64(referenceMass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "01J9F2ABC"

	img, err := printing.Rasterize(&template, label, domain.LocaleFrench, printing.RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	return template, img
}

// checkerboard is the synthetic bitmap that catches what a photograph of a label
// cannot: one dot on, one dot off, so that a bit reversed, a row swapped or a byte
// shifted shows up immediately.
func checkerboard(t domain.Template) *image.Gray {
	width, height := mediaDots(t.Media)
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if (x+y)%2 == 0 {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// compareDots holds two bitmaps to bit-for-bit equality over the width AND height of
// the original, and requires the padding of the frame — the columns that fill the last
// byte of a row, and the rows that fill the last byte of the height — to be bare label.
//
// Both paddings burn the same way if they are forgotten: a black band down the right
// edge, or across the bottom, of every label the station prints.
func compareDots(t *testing.T, want, got *image.Gray) {
	t.Helper()
	paddedHeight := (want.Bounds().Dy() + 7) / 8 * 8
	if got.Bounds().Dy() != paddedHeight {
		t.Fatalf("%d lignes relues, %d écrites complétées à %d — <G> compte sa hauteur en octets",
			got.Bounds().Dy(), want.Bounds().Dy(), paddedHeight)
	}
	if got.Bounds().Dx() < want.Bounds().Dx() {
		t.Fatalf("%d colonnes relues, %d écrites : la trame en a perdu",
			got.Bounds().Dx(), want.Bounds().Dx())
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			inked := want.GrayAt(want.Bounds().Min.X+x, want.Bounds().Min.Y+y).Y < inkThreshold
			back := got.GrayAt(x, y).Y < inkThreshold
			if inked != back {
				t.Fatalf("dot (%d;%d) : encré=%v à l'aller, %v au retour", x, y, inked, back)
			}
		}
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := want.Bounds().Dx(); x < got.Bounds().Dx(); x++ {
			if got.GrayAt(x, y).Y < inkThreshold {
				t.Fatalf("le bit de bourrage (%d;%d) est encré : la fin de ligne imprimerait une bande noire", x, y)
			}
		}
	}
	for y := want.Bounds().Dy(); y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			if got.GrayAt(x, y).Y < inkThreshold {
				t.Fatalf("la ligne de bourrage (%d;%d) est encrée : le bas de l'étiquette "+
					"imprimerait une bande noire", x, y)
			}
		}
	}
}

// commandArg returns the argument of the first command of that name.
func commandArg(f sbplFrame, name string) string {
	for _, c := range f.commands {
		if c.name == name {
			return c.arg
		}
	}
	return ""
}

// printError holds an error to being the typed, French, correctly classified failure
// the taxonomy of §8.5 promises.
func printError(t *testing.T, err error, kind ports.Kind, says string) {
	t.Helper()
	if err == nil {
		t.Fatalf("aucune erreur, une *ports.PrintError{%s} était attendue", kind)
	}
	var printErr *ports.PrintError
	if !errors.As(err, &printErr) {
		t.Fatalf("erreur %T (%v), *ports.PrintError attendue : le service d'impression décide des réessais sur le Kind", err, err)
	}
	if printErr.Kind != kind {
		t.Errorf("Kind = %s, attendu %s — c'est lui qui décide du message client et des réessais (§8.5) : %v",
			printErr.Kind, kind, err)
	}
	if !strings.Contains(printErr.Message, says) {
		t.Errorf("message « %s » : il devait contenir « %s ». Il est lu par un bénévole sur l'écran d'administration",
			printErr.Message, says)
	}
	if printErr.Op == "" {
		t.Error("Op est vide : c'est ce qui situe la panne dans un rapport de bug")
	}
	if kind != ports.KindTransient && printErr.Retryable() {
		t.Errorf("Kind %s déclaré réessayable : réessayer deux fois une faute de gabarit, c'est deux secondes "+
			"de plus devant un écran qui n'imprimera pas", kind)
	}
}
