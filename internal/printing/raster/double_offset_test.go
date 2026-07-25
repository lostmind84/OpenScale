package raster

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file guards the one trap the merge of the two encoders left behind: two offsets
// that answer the same complaint and could be fed from the same configuration key.
//
// docs/02-architecture.md §11.2 annotates printer.options.offset_x with « <A3> H, dots »,
// while rules 29 and 38 of domain.Config.Validate recompose that same key into the
// TEMPLATE geometry and bound it against it — [0, maxX], measured on the ink. The two
// readings are both defensible and they cannot both be wired.
//
// Nothing is broken today: the shipped values are zero on both sides and there is no
// composition root yet. That is exactly why the guard is written NOW — the day
// cmd/openscale grows a drivers.go, the naive wiring is one line and its only symptom
// on site is a label printed two dots off with a spoiled roll to show for it.

// shiftedTemplate returns the shipped template with a layout offset applied.
func shiftedTemplate(x, y int) domain.Template {
	t := domain.IdenticalTemplate()
	t.OffsetXDots, t.OffsetYDots = x, y
	return t
}

// blankRender returns a BLANK label of exactly the media size.
//
// Blank means white, and it has to be said out loud: the zero value of an image.Gray
// pixel is 0, which is BLACK. A freshly allocated image is therefore entirely inked,
// admissibleOffsets measures no free margin on it, and every offset is legitimately
// refused — the encoder is right and the test would be wrong. Rasterize fills with
// white before drawing for the same reason.
func blankRender(t domain.Template) *image.Gray {
	w, h := mediaDots(t.Media)
	img := image.NewGray(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{Y: 0xFF}), image.Point{}, draw.Src)
	return img
}

// requireConfigFault asserts the refusal is the operator-facing kind. A double offset
// is a WIRING mistake: no retry can fix it, and the person who can is the one holding
// the configuration file.
func requireConfigFault(t *testing.T, err error) *ports.PrintError {
	t.Helper()
	if err == nil {
		t.Fatal("le double décalage a été accepté : l'étiquette sortirait déplacée de la " +
			"somme des deux, et personne ne le saurait avant d'avoir gâché un rouleau")
	}
	var fault *ports.PrintError
	if !errors.As(err, &fault) {
		t.Fatalf("erreur %T, attendu *ports.PrintError", err)
	}
	if fault.Kind != ports.KindConfig {
		t.Errorf("kind %v, attendu KindConfig : aucun réessai ne corrigera un câblage", fault.Kind)
	}
	if fault.Retryable() {
		t.Error("une faute de câblage a été déclarée réessayable")
	}
	return fault
}

func TestTheSameOffsetAskedTwiceIsRefused(t *testing.T) {
	tpl := shiftedTemplate(2, 3)
	settings := DefaultSettings()
	settings.OffsetXDots, settings.OffsetYDots = 2, 3

	fault := requireConfigFault(t, func() error {
		_, err := encodeLabel(blankRender(tpl), tpl, settings, WS408(), 1)
		return err
	}())

	// The message must name the TOTAL, because that is the number the volunteer sees on
	// the label and the only one that lets them recognise their own mistake.
	for _, want := range []string{"(4 ; 6)", "le gabarit"} {
		if !strings.Contains(fault.Message, want) {
			t.Errorf("le message ne contient pas %q : %s", want, fault.Message)
		}
	}
}

// TestOneOffsetAloneIsAccepted, on both sides: the guard must refuse the SUM, never the
// setting itself. A guard that refused a legitimate adjustment would push whoever hits
// it to delete the guard.
func TestOneOffsetAloneIsAccepted(t *testing.T) {
	t.Run("le gabarit seul décale", func(t *testing.T) {
		tpl := shiftedTemplate(2, 3)
		if _, err := encodeLabel(blankRender(tpl), tpl, DefaultSettings(), WS408(), 1); err != nil {
			t.Fatalf("un décalage de gabarit seul a été refusé : %v", err)
		}
	})

	t.Run("l'imprimante seule décale", func(t *testing.T) {
		tpl := domain.IdenticalTemplate()
		settings := DefaultSettings()
		settings.OffsetXDots, settings.OffsetYDots = 2, 3
		if _, err := encodeLabel(blankRender(tpl), tpl, settings, WS408(), 1); err != nil {
			t.Fatalf("un décalage <A3> seul a été refusé : %v", err)
		}
	})

	t.Run("personne ne décale, le cas livré", func(t *testing.T) {
		tpl := domain.IdenticalTemplate()
		if tpl.OffsetXDots != 0 || tpl.OffsetYDots != 0 {
			t.Fatalf("le gabarit livré décale de (%d ; %d) : il ne devrait pas, "+
				"le décalage est un réglage de poste et non une propriété du gabarit",
				tpl.OffsetXDots, tpl.OffsetYDots)
		}
		s := DefaultSettings()
		if s.OffsetXDots != 0 || s.OffsetYDots != 0 {
			t.Fatalf("les réglages livrés décalent de (%d ; %d)", s.OffsetXDots, s.OffsetYDots)
		}
		if _, err := encodeLabel(blankRender(tpl), tpl, s, WS408(), 1); err != nil {
			t.Fatalf("la configuration livrée est refusée : %v", err)
		}
	})
}

// TestAnAxisAtATimeStillCounts: the trap does not need both axes to bite. A wiring that
// feeds offset_x to both and leaves offset_y alone is exactly as wrong, and rarer to
// spot because only one direction drifts.
func TestAnAxisAtATimeStillCounts(t *testing.T) {
	for _, c := range []struct {
		name                   string
		tplX, tplY, setX, setY int
	}{
		{"horizontal seulement", 2, 0, 2, 0},
		{"vertical seulement", 0, 3, 0, 3},
		{"axes croisés", 2, 0, 0, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			tpl := shiftedTemplate(c.tplX, c.tplY)
			s := DefaultSettings()
			s.OffsetXDots, s.OffsetYDots = c.setX, c.setY
			_, err := encodeLabel(blankRender(tpl), tpl, s, WS408(), 1)
			requireConfigFault(t, err)
		})
	}
}
