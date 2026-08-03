package sbpl_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// What the tests of this package build their frames with: the bench bitmaps, the shipped
// values of §11.2, and constructors that FAIL THE TEST instead of returning an error.
//
// Those constructors exist so that the assertion stays readable: a test about a frame must
// not spend ten lines assembling a valid job before it reaches what it measures.

// The shipped values of §11.2, and the reason the goldens carry these numbers
// rather than round ones: config-lacagette.json states darkness 3, speed 4, one
// copy. Nothing in this file invents a printer setting.
const (
	shippedDarkness = 3
	shippedSpeed    = 4
	shippedCopies   = 1
)

// The media of weighing_identical, in dots: 35 × 25 mm at 8 dots/mm, as the L0 bench
// measured it — the printer's own configuration first, a caliper on the stock second.
// It is stated here as the two numbers <A1> carries, and asserted against the template.
const (
	productionHeightDots = 200
	productionWidthDots  = 280
)

// --- Fixtures ---------------------------------------------------------------

// smallBitmap is sixteen dots by three, chosen so that its packing is legible in a
// golden a human reviews: a black row, a white row, and a row half of each.
//
// It reads FFFF 0000 FF00 under the shipped polarity — which is the whole point.
// A pattern picked for coverage would produce sixteen hexadecimal characters nobody
// can check by eye, and a golden nobody can check by eye records a bug just as
// faithfully as it records a frame.
func smallBitmap() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, 16, 3))
	ink, bare := color.Gray{Y: 0x00}, color.Gray{Y: 0xFF}
	for x := 0; x < 16; x++ {
		img.SetGray(x, 0, ink)
		img.SetGray(x, 1, bare)
		if x < 8 {
			img.SetGray(x, 2, ink)
		} else {
			img.SetGray(x, 2, bare)
		}
	}
	return img
}

// smallBitmapHex is what smallBitmap must come out as, under the shipped polarity.
const smallBitmapHex = "FFFF" + "0000" + "FF00"

// checkerboard is a bitmap that is not mostly white, so that the packing is
// exercised on both values and on every bit position of a byte.
//
// Its width is thirteen: NOT a multiple of eight, so every row ends on three padding
// bits. Those bits are the one part of the packing no dot of the label ever covers,
// and the polarity flip is where they get forgotten.
func checkerboard(width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if (x+3*y)%7 < 3 || x == 0 || y == height-1 {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// celeryRow is row id 1153 of testdata/catalog/flv.csv, the authentic export. Its
// reference carries the 021 the reference barcode of §18 is built on, so the golden
// frame carries the very symbol internal/printing freezes its 95 modules for.
var celeryRow = domain.Product{
	ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
	Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
	CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
}

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// productionBitmap renders the label of the parc, through the engine the raster
// driver uses, at the pitch of the head.
func productionBitmap(t *testing.T) *image.Gray {
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
	label.JobID = "test"

	img, err := printing.Rasterize(&template, label, domain.LocaleFrench, printing.RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if got := img.Bounds(); got.Dx() != productionWidthDots || got.Dy() != productionHeightDots {
		t.Fatalf("le rendu mesure %d × %d dots, le média de §7.2 en annonce %d × %d",
			got.Dx(), got.Dy(), productionWidthDots, productionHeightDots)
	}
	return img
}

// --- Builders that fail the test instead of returning an error ---------------

func mustMedia(t *testing.T, heightDots, widthDots int) sbpl.MediaSize {
	t.Helper()
	media, err := sbpl.NewMediaSize(heightDots, widthDots)
	if err != nil {
		t.Fatalf("NewMediaSize(%d, %d) : %v", heightDots, widthDots, err)
	}
	return media
}

// mustSetup gathers the shipped settings on a NEUTRAL offset — the zero Offset, which
// is what a station that has never been nudged sends.
func mustSetup(t *testing.T, heightDots, widthDots int) sbpl.Setup {
	t.Helper()
	return mustShiftedSetup(t, mustMedia(t, heightDots, widthDots), sbpl.Offset{})
}

func mustShiftedSetup(t *testing.T, media sbpl.MediaSize, offset sbpl.Offset) sbpl.Setup {
	t.Helper()
	darkness, err := sbpl.NewDarkness(shippedDarkness)
	if err != nil {
		t.Fatalf("NewDarkness(%d) : %v", shippedDarkness, err)
	}
	speed, err := sbpl.NewSpeed(shippedSpeed)
	if err != nil {
		t.Fatalf("NewSpeed(%d) : %v", shippedSpeed, err)
	}
	setup, err := sbpl.NewSetup(media, offset, darkness, speed)
	if err != nil {
		t.Fatalf("NewSetup : %v", err)
	}
	return setup
}

func mustOffset(t *testing.T, xDots, yDots int, g sbpl.Graphic, m sbpl.MediaSize) sbpl.Offset {
	t.Helper()
	offset, err := sbpl.NewOffset(xDots, yDots, g, m)
	if err != nil {
		t.Fatalf("NewOffset(%+d, %+d) : %v", xDots, yDots, err)
	}
	return offset
}

func mustGraphic(t *testing.T, x, y int, img *image.Gray, ink sbpl.InkPolarity) sbpl.Graphic {
	t.Helper()
	g, err := sbpl.NewGraphic(sbpl.WS408(), x, y, img, ink)
	if err != nil {
		t.Fatalf("NewGraphic(%d, %d) : %v", x, y, err)
	}
	return g
}

func mustJob(t *testing.T, setup sbpl.Setup, graphic sbpl.Graphic, copies int) sbpl.Job {
	t.Helper()
	count, err := sbpl.NewCopies(copies)
	if err != nil {
		t.Fatalf("NewCopies(%d) : %v", copies, err)
	}
	job, err := sbpl.NewJob(setup, graphic, count)
	if err != nil {
		t.Fatalf("NewJob : %v", err)
	}
	return job
}

// smallJob is the readable job: a legible bitmap on a small media.
func smallJob(t *testing.T) sbpl.Job {
	t.Helper()
	return mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), shippedCopies)
}

// productionJob is the label of the parc, whole: the frame a station really sends.
func productionJob(t *testing.T) sbpl.Job {
	t.Helper()
	setup := mustSetup(t, productionHeightDots, productionWidthDots)
	return mustJob(t, setup, mustGraphic(t, 0, 0, productionBitmap(t), sbpl.InkIsOne), shippedCopies)
}

func encode(t *testing.T, job sbpl.Job) []byte {
	t.Helper()
	var frame bytes.Buffer
	if err := sbpl.Encode(&frame, job); err != nil {
		t.Fatalf("Encode : %v", err)
	}
	return frame.Bytes()
}

// readable makes a frame quotable in a failure message: the escapes become <ESC>,
// which is how §8.3 spells them, and everything else is already printable.
func readable(p []byte) string {
	return strings.ReplaceAll(string(p), "\x1b", "<ESC>")
}

func assertPrintError(t *testing.T, err error, kind ports.Kind, op string) {
	t.Helper()
	var refusal *ports.PrintError
	if !errors.As(err, &refusal) {
		t.Fatalf("erreur de type %T, attendu *ports.PrintError : %v", err, err)
	}
	if refusal.Kind != kind {
		t.Errorf("genre %s, attendu %s (message : %s)", refusal.Kind, kind, refusal.Message)
	}
	if refusal.Op != op {
		t.Errorf("opération %q, attendue %q", refusal.Op, op)
	}
	if refusal.Message == "" {
		t.Error("message vide : un bénévole doit lire ce qui ne va pas")
	}
}
