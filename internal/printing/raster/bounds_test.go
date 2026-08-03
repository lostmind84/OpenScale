package raster

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// What only THIS side of the border can check: a bitmap that came from another template, a
// setting out of bounds, a media larger than its field, a head outside the graphic field, a
// number of copies that overflows.
//
// All of them are REFUSED rather than quietly brought back into range: a clamped value
// prints a label askew, and nobody will know why.

// --- What only this side of the border can check ----------------------------

// TestABitmapFromAnotherTemplateIsRefused is the check that stops a render made for
// one geometry from being sent as another.
//
// A frame declares its own dimensions, so the printer would accept a bitmap one dot
// short without a word and shift every row that follows. The encapsulation cannot make
// this check: it never sees the template, only the bitmap.
func TestABitmapFromAnotherTemplateIsRefused(t *testing.T) {
	template := domain.IdenticalTemplate()
	width, height := mediaDots(template.Media)

	for _, c := range []struct {
		name          string
		width, height int
	}{
		{"un dot de moins en largeur", width - 1, height},
		{"un dot de moins en hauteur", width, height - 1},
		{"un dot de plus en hauteur", width, height + 1},
		{"le gabarit neutre d'une autre tête", 480, 305},
	} {
		t.Run(c.name, func(t *testing.T) {
			img := image.NewGray(image.Rect(0, 0, c.width, c.height))
			_, err := encodeLabel(img, template, DefaultSettings(), WS408(), 1)
			printError(t, err, ports.KindTemplate, "vient d'un autre gabarit")
		})
	}

	t.Run("aucun rendu", func(t *testing.T) {
		_, err := encodeLabel(nil, template, DefaultSettings(), WS408(), 1)
		printError(t, err, ports.KindTemplate, "aucun rendu")
	})
}

// TestTheThreeAdjustmentsEachChangeTheFrame is the assertion behind the three
// buttons: each one really reaches the printer, and none of them touches the ink.
func TestTheThreeAdjustmentsEachChangeTheFrame(t *testing.T) {
	template, rendered := productionLabel(t)

	base := DefaultSettings()
	darker := base
	darker.Darkness = base.Darkness + 1
	faster := base
	faster.Speed = base.Speed + 1
	shifted := base
	// LEFT and not right: since the media was corrected to the paper the label really
	// runs on, the ink fills its width to within a sixth of a dot, so the only
	// horizontal dot still available goes the other way.
	shifted.OffsetXDots = -1

	frames := map[string]string{}
	for name, settings := range map[string]Settings{
		"réglages livrés": base,
		"noircissement+1": darker,
		"vitesse+1":       faster,
		"décalage -1 dot": shifted,
	} {
		frame, err := encodeLabel(rendered, template, settings, WS408(), 1)
		if err != nil {
			t.Fatalf("%s : %v", name, err)
		}
		for other, seen := range frames {
			if seen == string(frame) {
				t.Errorf("« %s » et « %s » produisent la MÊME trame : un des trois boutons ne va nulle part", name, other)
			}
		}
		frames[name] = string(frame)

		// The adjustments are printer settings: the dots are the same in all four.
		compareDots(t, rendered, readFrame(t, frame).graphic)
	}

	// And the offset really is the ±dddd of <A3>, sign included: V carries the vertical
	// axis, H the horizontal one, which is the reverse of the (x;y) of everything else.
	lifted := base
	lifted.OffsetXDots, lifted.OffsetYDots = -1, -3
	frame, err := encodeLabel(rendered, template, lifted, WS408(), 1)
	if err != nil {
		t.Fatalf("décalage (-1;-3) : %v", err)
	}
	if got := commandArg(readFrame(t, frame), "A3"); got != "V-0003H-0001" {
		t.Errorf("<A3>%s pour un décalage x=-1 y=-3, « V-0003H-0001 » attendu", got)
	}
}

// TestAnAdjustmentOutOfBoundsIsRefusedRatherThanClamped is the second half of the
// same promise. A darkness of 7 quietly turned into 5 is a knob that no longer moves,
// and the volunteer keeps turning it.
func TestAnAdjustmentOutOfBoundsIsRefusedRatherThanClamped(t *testing.T) {
	template, rendered := productionLabel(t)

	for _, c := range []struct {
		name     string
		settings func(Settings) Settings
		kind     ports.Kind
		says     string
	}{
		{"noircissement 0", func(s Settings) Settings { s.Darkness = 0; return s }, ports.KindConfig, "noircissement 0"},
		{"noircissement 6", func(s Settings) Settings { s.Darkness = 6; return s }, ports.KindConfig, "noircissement 6"},
		{"vitesse 1", func(s Settings) Settings { s.Speed = 1; return s }, ports.KindConfig, "vitesse 1"},
		{"vitesse 7", func(s Settings) Settings { s.Speed = 7; return s }, ports.KindConfig, "vitesse 7"},
		{"décalage horizontal hors média", func(s Settings) Settings { s.OffsetXDots = 999; return s }, ports.KindConfig, "décalage horizontal"},
		{"décalage vertical hors média", func(s Settings) Settings { s.OffsetYDots = -999; return s }, ports.KindConfig, "décalage vertical"},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame, err := encodeLabel(rendered, template, c.settings(DefaultSettings()), WS408(), 1)
			if frame != nil {
				t.Errorf("%d octets rendus alors que le réglage est refusé : rien ne doit partir", len(frame))
			}
			printError(t, err, c.kind, c.says)
		})
	}
}

// TestTheOffsetIsBoundedByTheInkOfTheShippedLabel is the refusal a volunteer reads
// while nudging a label back into place: it names the range instead of saying no.
//
// The two ranges are WRITTEN OUT rather than asked of the code under test. They are a
// measurement of the shipped weighing_identical on the 280 × 200 media the L0 bench
// established, and stating them here is what makes this a test of the rule rather than
// a restatement of it. If the drawing of §7.3 moves, these numbers move, and a
// volunteer's arrows stop where they stopped yesterday.
//
// THE HORIZONTAL RANGE IS ONE DOT WIDE, and that is a consequence of the correction,
// not of the drawing: the text boxes are 34 978 um across on 35 000 um of paper, so
// there are 22 um of slack — a sixth of a dot. While the media was declared 40 mm the
// arrows had five millimetres to play with, and they were playing with paper that does
// not exist. Widening that range means narrowing the label, which is a decision about
// the drawing and is recorded as an open question rather than taken here.
func TestTheOffsetIsBoundedByTheInkOfTheShippedLabel(t *testing.T) {
	const (
		lowX, highX = -1, 0
		lowY, highY = -3, 1
	)
	template, rendered := productionLabel(t)

	for _, c := range []struct {
		name    string
		s       Settings
		refused bool
	}{
		{"décalage nul", offsetXY(0, 0), false},
		{"dernier dot admis à droite", offsetXY(highX, 0), false},
		{"un dot de trop à droite", offsetXY(highX+1, 0), true},
		{"dernier dot admis à gauche", offsetXY(lowX, 0), false},
		{"un dot de trop à gauche", offsetXY(lowX-1, 0), true},
		{"dernier dot admis en bas", offsetXY(0, highY), false},
		{"un dot de trop en bas", offsetXY(0, highY+1), true},
		{"dernier dot admis en haut", offsetXY(0, lowY), false},
		{"un dot de trop en haut", offsetXY(0, lowY-1), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := encodeLabel(rendered, template, c.s, WS408(), 1)
			if !c.refused {
				if err != nil {
					t.Fatalf("décalage refusé alors qu'il tient sur le média : %v", err)
				}
				return
			}
			printError(t, err, ports.KindConfig, "admet de")
			// The message names the range, because « décalage invalide » tells a
			// volunteer nothing about which key to press next.
			var refusal *ports.PrintError
			errors.As(err, &refusal)
			if !strings.Contains(refusal.Message, "-1") && !strings.Contains(refusal.Message, "+40") &&
				!strings.Contains(refusal.Message, "-3") && !strings.Contains(refusal.Message, "+3") {
				t.Errorf("le message ne nomme aucune borne : %s", refusal.Message)
			}
		})
	}
}

func offsetXY(x, y int) Settings {
	s := DefaultSettings()
	s.OffsetXDots, s.OffsetYDots = x, y
	return s
}

// TestInvertBitsFlipsThePolarityAndNothingElse covers the last SBPL unknown (§8.3),
// as this driver's boolean maps onto it.
func TestInvertBitsFlipsThePolarityAndNothingElse(t *testing.T) {
	template, rendered := productionLabel(t)
	settings := DefaultSettings()
	settings.InvertBits = true

	direct, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage direct : %v", err)
	}
	inverted, err := encodeLabel(rendered, template, settings, WS408(), 1)
	if err != nil {
		t.Fatalf("encodage inversé : %v", err)
	}
	if string(direct) == string(inverted) {
		t.Fatal("invert_bits ne change rien : le réglage qui lève la dernière inconnue SBPL ne va nulle part")
	}

	// Read back through the inverse polarity: every dot comes home.
	read := readFrame(t, inverted)
	for y := 0; y < read.graphic.Bounds().Dy(); y++ {
		for x := 0; x < read.graphic.Bounds().Dx(); x++ {
			shade := uint8(0x00)
			if read.graphic.GrayAt(x, y).Y < inkThreshold {
				shade = 0xFF
			}
			read.graphic.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	compareDots(t, rendered, read.graphic)
}

// TestTheGraphicBlockRefusesWhatTheHeadCannotTake covers the two hard limits of the
// <G> command: the width of the head, and 600 dots per block.
func TestTheGraphicBlockRefusesWhatTheHeadCannotTake(t *testing.T) {
	for _, c := range []struct {
		name  string
		media domain.Media
		says  string
	}{
		{"plus large que la tête", domain.Media{WidthUM: 120_000, HeightUM: 25_400, DotsPerMM: 8}, "maximum 104"},
		{"plus haut qu'un bloc <G>", domain.Media{WidthUM: 40_000, HeightUM: 80_000, DotsPerMM: 8}, "maximum 600"},
	} {
		t.Run(c.name, func(t *testing.T) {
			template := domain.Template{Name: "essai", Media: c.media}
			width, height := mediaDots(c.media)
			img := image.NewGray(image.Rect(0, 0, width, height))
			_, err := encodeLabel(img, template, DefaultSettings(), WS408(), 1)
			printError(t, err, ports.KindTemplate, c.says)
		})
	}
}

// TestAMediaBiggerThanItsFieldIsRefused covers the four digits of <A1>. It is not a
// theoretical bound: it is the first command after <A>, so it is what refuses a
// template built in the wrong unit before anything else has a chance to.
func TestAMediaBiggerThanItsFieldIsRefused(t *testing.T) {
	media := domain.Media{WidthUM: 1_250_000, HeightUM: 25_400, DotsPerMM: 8} // 10 000 dots
	template := domain.Template{Name: "essai", Media: media}
	width, height := mediaDots(media)
	if width != 10_000 {
		t.Fatalf("le média d'essai fait %d dots de large, 10 000 attendus", width)
	}
	_, err := encodeLabel(image.NewGray(image.Rect(0, 0, width, height)), template,
		DefaultSettings(), WS408(), 1)
	printError(t, err, ports.KindConfig, "hors bornes SBPL")
}

// TestAHeadOutsideTheGraphicFieldIsRefused covers the border the other way: the model
// this driver declares travels into the encapsulation as a <G> width, and a head that
// no three-digit field can express must be refused rather than truncated.
func TestAHeadOutsideTheGraphicFieldIsRefused(t *testing.T) {
	template, rendered := productionLabel(t)
	_, err := encodeLabel(rendered, template, DefaultSettings(), Head{DotsPerMM: 8, MaxWidthBytes: 1000}, 1)
	printError(t, err, ports.KindConfig, "hors bornes du champ <G>")
}

// TestTheCopyCountIsBoundedByItsField covers <Q>, six digits.
func TestTheCopyCountIsBoundedByItsField(t *testing.T) {
	template, rendered := productionLabel(t)

	for _, copies := range []int{0, -1, MaxCopies + 1} {
		if _, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), copies); err == nil {
			t.Errorf("%d exemplaires acceptés : le champ <Q> porte six chiffres", copies)
		}
	}
	for _, copies := range []int{1, 2, MaxCopies} {
		frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), copies)
		if err != nil {
			t.Fatalf("%d exemplaires : %v", copies, err)
		}
		read := readFrame(t, frame)
		if got := commandArg(read, "Q"); atoi(t, got) != copies {
			t.Errorf("<Q>%s pour %d exemplaires", got, copies)
		}
	}
}
