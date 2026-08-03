package sbpl_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// The tests of offset.go: the shift of <A3>. It is bounded by the INK and not by the
// template, it is revalidated on every piece the assembly puts together, and it is never
// measured on a forged graphic — three refusals that keep a one-dot setting from pushing
// the label off the media.

// --- 11. The offset of <A3> -------------------------------------------------

// bitmapWithOneInkedDot is a bare bitmap carrying a single burnt dot.
//
// One dot and not a shape: the admissible offset is read off the EDGES of the ink, so
// a fixture whose four edges are one known coordinate is a fixture whose expected
// range can be written down rather than computed by the code under test.
func bitmapWithOneInkedDot(width, height, atX, atY int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if x == atX && y == atY {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// TestTheOffsetIsBoundedByTheInkAndNotByTheTemplate is the rule of Offset, on a
// fixture whose ink is placed by hand so that the range can be stated instead of
// derived.
//
// smallBitmap inks the full sixteen dots of its width and its first and third rows, on
// a 16 × 24 media: there is nothing to spare horizontally, so the only admissible
// horizontal offset is zero, and vertically the label may drop by the 21 dots of bare
// stock below it.
func TestTheOffsetIsBoundedByTheInkAndNotByTheTemplate(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)

	for _, c := range []struct {
		name    string
		x, y    int
		refused bool
	}{
		{"décalage nul", 0, 0, false},
		{"un dot à droite", 1, 0, true},
		{"un dot à gauche", -1, 0, true},
		{"dernier dot admis vers le bas", 0, 21, false},
		{"un dot de trop vers le bas", 0, 22, true},
		{"un dot vers le haut", 0, -1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := sbpl.NewOffset(c.x, c.y, graphic, media)
			if !c.refused {
				if err != nil {
					t.Fatalf("décalage refusé alors que l'encre tient sur le média : %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("décalage accepté : l'encre sortirait du média")
			}
			assertPrintError(t, err, ports.KindConfig, "sbpl.offset")
			// It NAMES the range instead of saying no: a volunteer nudging a label has
			// to learn where the wall is, or they keep pressing an arrow that does
			// nothing.
			var refusal *ports.PrintError
			errors.As(err, &refusal)
			if !strings.Contains(refusal.Message, "admet de") {
				t.Errorf("le message ne nomme pas la plage admissible : %s", refusal.Message)
			}
		})
	}
}

// TestABitmapWithNoInkIsBoundedOnlyByTheField covers the case a template with nothing
// active on this station produces: there is no ink to push off the paper, so only the
// four digits of <A3> bound the offset.
func TestABitmapWithNoInkIsBoundedOnlyByTheField(t *testing.T) {
	media := mustMedia(t, 24, 16)
	bare := image.NewGray(image.Rect(0, 0, 16, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 16; x++ {
			bare.SetGray(x, y, color.Gray{Y: 0xFF})
		}
	}
	graphic := mustGraphic(t, 0, 0, bare, sbpl.InkIsOne)

	for _, extreme := range [][2]int{{9999, 9999}, {-9999, -9999}} {
		mustOffset(t, extreme[0], extreme[1], graphic, media)
	}
	for _, past := range [][2]int{{10_000, 0}, {0, -10_000}} {
		_, err := sbpl.NewOffset(past[0], past[1], graphic, media)
		if err == nil {
			t.Fatalf("décalage (%+d;%+d) accepté : <A3> ne porte que quatre chiffres", past[0], past[1])
		}
		assertPrintError(t, err, ports.KindConfig, "sbpl.offset")
	}
}

// TestTheGeometricRangeAlwaysFitsTheField is the claim admissibleOffsets makes in
// prose, held on the two extremes the typed constructors can actually reach: the
// widest stock, and a block whose ink sits as far into it as a validated Graphic
// allows. Both ends stay inside the four digits of <A3>.
func TestTheGeometricRangeAlwaysFitsTheField(t *testing.T) {
	widest, err := sbpl.NewModel(999)
	if err != nil {
		t.Fatalf("NewModel(999) : %v", err)
	}
	// 7992 dots of block on 9999 dots of stock, ink on the very last column: the range
	// runs from -7991 to +2007, and neither end needs the field to be looked at.
	block, err := sbpl.NewGraphic(widest, 0, 0, bitmapWithOneInkedDot(999*8, 1, 999*8-1, 0), sbpl.InkIsOne)
	if err != nil {
		t.Fatalf("NewGraphic : %v", err)
	}
	media := mustMedia(t, 9999, 9999)
	mustOffset(t, -7991, 0, block, media)
	mustOffset(t, 2007, 0, block, media)
	for _, past := range [][2]int{{-7992, 0}, {2008, 0}} {
		if _, err := sbpl.NewOffset(past[0], past[1], block, media); err == nil {
			t.Errorf("décalage (%+d;%+d) accepté hors de la plage géométrique", past[0], past[1])
		}
	}
}

// TestAnOffsetMeasuredOnAnotherBitmapIsRefusedAtAssembly is the cross-field check of
// NewJob, and the one hole a per-field validation leaves open.
//
// Each half is valid on its own: the offset was measured against a bitmap with room to
// spare, the graphic fits its media. Together they push ink off the paper, and the
// only place that can see it is the assembly.
func TestAnOffsetMeasuredOnAnotherBitmapIsRefusedAtAssembly(t *testing.T) {
	media := mustMedia(t, 24, 16)
	roomy := mustGraphic(t, 0, 0, bitmapWithOneInkedDot(16, 3, 0, 0), sbpl.InkIsOne)
	offset := mustOffset(t, 8, 0, roomy, media)

	full := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	copies, err := sbpl.NewCopies(1)
	if err != nil {
		t.Fatalf("NewCopies : %v", err)
	}
	job, err := sbpl.NewJob(mustShiftedSetup(t, media, offset), full, copies)
	if err == nil {
		t.Fatal("NewJob a accepté un décalage mesuré sur un autre bitmap")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.offset")

	transport := &countingWriter{}
	if err := sbpl.Encode(transport, job); err == nil {
		t.Fatal("Encode a accepté le même travail")
	}
	if transport.written != 0 {
		t.Errorf("%d octets sont partis avant le refus", transport.written)
	}
}

// TestASetupRevalidatesEveryPartItGathers is the claim NewSetup makes: it validates
// its parts again rather than trusting them.
//
// Two of the four are forgeable as a zero value from outside — Darkness{} is a burn
// level of zero and Speed{} an inch per second of zero, neither of which any bound
// admits. The other two need the value a refusing constructor RETURNS: NewOffset and
// NewMediaSize hand back the thing they refused, which is the only way an external
// caller holds one, and it is exactly what this test needs.
func TestASetupRevalidatesEveryPartItGathers(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	darkness, err := sbpl.NewDarkness(shippedDarkness)
	if err != nil {
		t.Fatalf("NewDarkness : %v", err)
	}
	speed, err := sbpl.NewSpeed(shippedSpeed)
	if err != nil {
		t.Fatalf("NewSpeed : %v", err)
	}
	pastTheField, _ := sbpl.NewOffset(10_000, 0, graphic, media)

	for _, c := range []struct {
		name     string
		media    sbpl.MediaSize
		offset   sbpl.Offset
		darkness sbpl.Darkness
		speed    sbpl.Speed
		op       string
	}{
		{"média forgé", sbpl.MediaSize{}, sbpl.Offset{}, darkness, speed, "sbpl.media"},
		{"décalage hors champ", media, pastTheField, darkness, speed, "sbpl.offset"},
		{"noircissement forgé", media, sbpl.Offset{}, sbpl.Darkness{}, speed, "sbpl.darkness"},
		{"vitesse forgée", media, sbpl.Offset{}, darkness, sbpl.Speed{}, "sbpl.speed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := sbpl.NewSetup(c.media, c.offset, c.darkness, c.speed)
			if err == nil {
				t.Fatal("NewSetup a accepté une partie invalide")
			}
			assertPrintError(t, err, ports.KindConfig, c.op)
		})
	}
}

// TestAnOffsetCannotBeMeasuredOnAForgedGraphic: NewOffset validates what it measures
// against, so that a zero-value Graphic cannot make it read a nil bitmap.
func TestAnOffsetCannotBeMeasuredOnAForgedGraphic(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)

	_, err := sbpl.NewOffset(0, 0, sbpl.Graphic{}, media)
	if err == nil {
		t.Fatal("NewOffset a accepté un graphique forgé")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.model")

	_, err = sbpl.NewOffset(0, 0, graphic, sbpl.MediaSize{})
	if err == nil {
		t.Fatal("NewOffset a accepté un média forgé")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.media")
}

// TestTheOffsetReachesTheFrameSignAndAxisIncluded is the last link of the third
// adjustment of §8.2: the number a volunteer typed comes out in <A3>.
//
// V carries the VERTICAL axis and H the horizontal one — the reverse of the (x;y) of
// every other coordinate of this application, which is exactly the kind of swap that
// survives a review and shifts every label of the parc.
func TestTheOffsetReachesTheFrameSignAndAxisIncluded(t *testing.T) {
	media := mustMedia(t, 24, 32)
	// One dot at (4;4) on a 32 × 24 stock: four dots of slack up and left, so the
	// negative offsets this test needs are legitimate rather than tolerated.
	graphic := mustGraphic(t, 0, 0, bitmapWithOneInkedDot(16, 8, 4, 4), sbpl.InkIsOne)

	for _, c := range []struct {
		x, y int
		want string
	}{
		{0, 0, "\x1bA3V+0000H+0000"},
		{2, -3, "\x1bA3V-0003H+0002"},
		{-2, 3, "\x1bA3V+0003H-0002"},
		{27, 19, "\x1bA3V+0019H+0027"},
	} {
		offset := mustOffset(t, c.x, c.y, graphic, media)
		frame := encode(t, mustJob(t, mustShiftedSetup(t, media, offset), graphic, 1))
		if !bytes.Contains(frame, []byte(c.want)) {
			t.Errorf("décalage (%+d;%+d) : %s attendu dans %s",
				c.x, c.y, readable([]byte(c.want)), readable(excerpt(frame, 15)))
		}
	}
}
