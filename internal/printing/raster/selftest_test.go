package raster

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The tests of the three self-tests of §8.6.
//
// They are read back through the same independent parser as everything else, so what
// is asserted is what would come out of the head — not what a drawing function
// intended.

// TestTheAlignmentPatternCarriesItsSquareAndItsFourCrosses covers the self-test that
// lifts the LAST SBPL unknown: the polarity of <G>.
//
// A square that comes out white inside a black rectangle is invert_bits, and it is
// read at arm's length. The corner crosses are the second half: they say where the
// printable area really ends, and a cross missing an arm says the media is off by
// that much.
func TestTheAlignmentPatternCarriesItsSquareAndItsFourCrosses(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	if err := printer.SelfTest(context.Background(), SelfTestAlignment); err != nil {
		t.Fatalf("auto-test alignment : %v", err)
	}
	pattern := readFrame(t, transport.last(t)).graphic
	template := domain.IdenticalTemplate()
	width, height := mediaDots(template.Media)

	// The 64 × 64 square of §8.6, fully inked, in the middle of the printable area.
	// The side is spelled out rather than borrowed from the package: the figure comes
	// from the document, and a test reading the constant it is supposed to check would
	// follow it wherever it went.
	const wantSquare = 64
	left, top := (width-wantSquare)/2, (height-wantSquare)/2
	for y := top; y < top+wantSquare; y++ {
		for x := left; x < left+wantSquare; x++ {
			if pattern.GrayAt(x, y).Y >= inkThreshold {
				t.Fatalf("le dot (%d;%d) du carré n'est pas encré : le carré de §8.6 est PLEIN", x, y)
			}
		}
	}
	// Just outside it, bare label — otherwise the square is not a square.
	if pattern.GrayAt(left-1, top-1).Y < inkThreshold {
		t.Errorf("le dot (%d;%d) est encré : il est hors du carré", left-1, top-1)
	}

	// The arms and the inset both follow the head: one millimetre each, so eight dots
	// on a WS408. They are spelled out rather than borrowed for the same reason as the
	// square above.
	arm := int(template.Media.DotsPerMM * crossArmMM)
	inset := int(template.Media.DotsPerMM * cornerInsetMM)

	// THE CORNER ITSELF IS BARE, and this is the finding of the L0 bench rather than a
	// detail of drawing: the stock is die-cut with a rounded corner of about a
	// millimetre, so the four extreme dots are not paper. A cross printed there comes
	// out as nothing at all, which is what a volunteer reported on 28/07/2026.
	for _, corner := range []image.Point{
		{X: 0, Y: 0},
		{X: width - 1, Y: 0},
		{X: 0, Y: height - 1},
		{X: width - 1, Y: height - 1},
	} {
		if pattern.GrayAt(corner.X, corner.Y).Y < inkThreshold {
			t.Errorf("le coin (%d;%d) est encré : la découpe arrondie n'y laisse pas de papier",
				corner.X, corner.Y)
		}
	}

	for _, cross := range []image.Point{
		{X: arm + inset, Y: arm + inset},
		{X: width - 1 - arm - inset, Y: arm + inset},
		{X: arm + inset, Y: height - 1 - arm - inset},
		{X: width - 1 - arm - inset, Y: height - 1 - arm - inset},
	} {
		// WHOLE, both arms of both strokes: the outer tip is what a rounded corner used
		// to swallow, and the inner one is what clipping used to leave behind.
		for _, tip := range []image.Point{
			{X: cross.X - arm, Y: cross.Y}, {X: cross.X + arm, Y: cross.Y},
			{X: cross.X, Y: cross.Y - arm}, {X: cross.X, Y: cross.Y + arm},
		} {
			if pattern.GrayAt(tip.X, tip.Y).Y >= inkThreshold {
				t.Errorf("le bras de la croix (%d;%d) n'atteint pas son extrémité (%d;%d)",
					cross.X, cross.Y, tip.X, tip.Y)
			}
		}
		// Two dots thick, because one dot is 0.125 mm and nobody sees it on a label.
		if pattern.GrayAt(cross.X, cross.Y+crossStrokeDots-1).Y >= inkThreshold {
			t.Errorf("la croix (%d;%d) fait moins de %d dots d'épaisseur",
				cross.X, cross.Y, crossStrokeDots)
		}
	}
}

// TestTheRulerPatternFramesTheAreaAndTicksEveryMillimetre covers the self-test that
// turns « l'étiquette a l'air un peu courte » into a number.
func TestTheRulerPatternFramesTheAreaAndTicksEveryMillimetre(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	if err := printer.SelfTest(context.Background(), SelfTestRuler); err != nil {
		t.Fatalf("auto-test ruler : %v", err)
	}
	pattern := readFrame(t, transport.last(t)).graphic
	template := domain.IdenticalTemplate()
	width, height := mediaDots(template.Media)

	// The frame of the printable area, on all four sides.
	for _, dot := range []image.Point{
		{X: width / 2, Y: 0}, {X: width / 2, Y: height - 1},
		{X: 0, Y: height / 2}, {X: width - 1, Y: height / 2},
	} {
		if pattern.GrayAt(dot.X, dot.Y).Y >= inkThreshold {
			t.Errorf("le cadre de la zone imprimable manque en (%d;%d)", dot.X, dot.Y)
		}
	}

	// The ticks grow every fifth and every tenth millimetre. At 8 dots/mm the 10 mm
	// mark is at dot 80 and reaches 6 dots down, where the 11 mm mark reaches 2.
	tenth := int(10 * template.Media.DotsPerMM)
	eleventh := int(11 * template.Media.DotsPerMM)
	if pattern.GrayAt(tenth, tickTenDots-1).Y >= inkThreshold {
		t.Errorf("la graduation des 10 mm (dot %d) n'atteint pas %d dots", tenth, tickTenDots)
	}
	if pattern.GrayAt(eleventh, tickTenDots-1).Y < inkThreshold {
		t.Errorf("la graduation des 11 mm (dot %d) est aussi longue que celle des 10 mm : "+
			"une réglette sans repère décennal ne se lit pas", eleventh)
	}
}

// TestTheLabelSelfTestNeedsALabelSomebodyElseBuilt is the boundary, stated as a
// test: a demonstration label carries a product and prices, which come from the
// catalog and the configuration, never from a printing driver.
func TestTheLabelSelfTestNeedsALabelSomebodyElseBuilt(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)

	err := printer.SelfTest(context.Background(), SelfTestLabel)
	printError(t, err, ports.KindConfig, "aucune étiquette de démonstration")
	if len(transport.frames) != 0 {
		t.Error("une trame est partie sans étiquette de démonstration")
	}
}

// TestTheLabelSelfTestPrintsWhatItWasGiven: with a provider, the button of the
// troubleshooting page prints a real label, through the normal path.
func TestTheLabelSelfTestPrintsWhatItWasGiven(t *testing.T) {
	demo := job(t).Label
	printer, transport, _ := newPrinter(t, func(o *Options) {
		o.DemoLabel = func() (domain.Label, error) { return demo, nil }
	})

	if err := printer.SelfTest(context.Background(), SelfTestLabel); err != nil {
		t.Fatalf("auto-test label : %v", err)
	}
	_, rendered := productionLabel(t)
	compareDots(t, rendered, readFrame(t, transport.last(t)).graphic)
}

// TestASelfTestThatCannotBePreparedIsARefusalAboutTheDATA keeps the taxonomy honest
// when the provider itself fails.
func TestASelfTestThatCannotBePreparedIsARefusalAboutTheData(t *testing.T) {
	printer, _, _ := newPrinter(t, func(o *Options) {
		o.DemoLabel = func() (domain.Label, error) {
			return domain.Label{}, errors.New("aucun produit dans le catalogue")
		}
	})
	err := printer.SelfTest(context.Background(), SelfTestLabel)
	printError(t, err, ports.KindData, "démonstration")
}

// TestAnUnknownSelfTestIsRefusedAndListsTheRealOnes: a configuration that spells a
// name wrong gets the list of what exists, never a bare refusal (§11.3).
func TestAnUnknownSelfTestIsRefusedAndListsTheRealOnes(t *testing.T) {
	printer, _, _ := newPrinter(t, nil)

	// barcode-frame and character-table are the two self-tests A2 removed: without a
	// <BD> command and without native text they have no object left (§8.6).
	for _, gone := range []string{"barcode-frame", "character-table", "mire"} {
		err := printer.SelfTest(context.Background(), gone)
		printError(t, err, ports.KindConfig, "auto-test inconnu")
		var printErr *ports.PrintError
		errors.As(err, &printErr)
		for _, real := range []string{SelfTestLabel, SelfTestAlignment, SelfTestRuler} {
			if !strings.Contains(printErr.Message, real) {
				t.Errorf("le refus de %q ne nomme pas l'auto-test %q qui, lui, existe", gone, real)
			}
		}
	}
}

// TestASelfTestOnAClosedPrinterIsRefused: the same door as Print, closed the same
// way.
func TestASelfTestOnAClosedPrinterIsRefused(t *testing.T) {
	printer, _, _ := newPrinter(t, nil)
	if err := printer.Close(); err != nil {
		t.Fatalf("Close : %v", err)
	}
	if err := printer.SelfTest(context.Background(), SelfTestRuler); err == nil {
		t.Error("un auto-test a été accepté après Close")
	}
}

// TestAPatternIsAlwaysTheSizeOfTheMedia guards the check that catches a bitmap from
// another template: the patterns must pass it, and they only do if they are drawn on
// the media rather than on the printable area.
func TestAPatternIsAlwaysTheSizeOfTheMedia(t *testing.T) {
	template := domain.NeutralSingleTemplate()
	width, height := mediaDots(template.Media)

	for name, pattern := range map[string]*image.Gray{
		"alignment": alignmentPattern(template),
		"ruler":     rulerPattern(template),
	} {
		if got := pattern.Bounds(); got.Dx() != width || got.Dy() != height {
			t.Errorf("la mire %s fait %d × %d dots, le média %d × %d", name, got.Dx(), got.Dy(), width, height)
		}
	}
}

// TestTheSelfTestsGoThroughTheTransportLikeALabel: nothing about a self-test bypasses
// the path a real label takes, which is what makes it a test of that path.
func TestTheSelfTestsGoThroughTheTransportLikeALabel(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	for _, what := range []string{SelfTestAlignment, SelfTestRuler} {
		if err := printer.SelfTest(context.Background(), what); err != nil {
			t.Fatalf("auto-test %s : %v", what, err)
		}
	}
	if len(transport.frames) != 2 {
		t.Fatalf("%d trames remises au transport, 2 attendues", len(transport.frames))
	}
	for i, frame := range transport.frames {
		read := readFrame(t, frame)
		if commandArg(read, "Q") != "000001" {
			t.Errorf("trame %d : un auto-test tire un exemplaire, pas %s", i, commandArg(read, "Q"))
		}
	}
}

// TestASelfTestFailureCarriesTheTransportFailure keeps a broken queue readable
// through the self-test button, which is where a volunteer will press first.
func TestASelfTestFailureCarriesTheTransportFailure(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	transport.writeErr = errors.New("file inconnue")

	err := printer.SelfTest(context.Background(), SelfTestAlignment)
	printError(t, err, ports.KindTransient, "ne répond pas")
}

// TestTheStatusOfAOneWayTransportIsUnknownAndNotAFailure is stated once more here
// because it is the sentence the whole of §8.5 turns on, and because a
// PrinterUnknown quietly changed into PrinterFaulted would light a red lamp on every
// station of the parc.
func TestTheStatusOfAOneWayTransportIsUnknownAndNotAFailure(t *testing.T) {
	printer, _, _ := newPrinter(t, nil)
	if status := printer.Status(context.Background()); status.Health != ports.PrinterUnknown {
		t.Errorf("Health = %d sur un transport unidirectionnel, PrinterUnknown attendu", status.Health)
	}
}
