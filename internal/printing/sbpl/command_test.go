package sbpl_test

import (
	"image"
	"testing"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// The tests of command.go: one bound per field. Every value SBPL can carry has a range,
// and what does not fit is REFUSED at construction rather than truncated on the wire — a
// label printed askew costs more than a refusal somebody can read.

// --- 6. One bound check per field -------------------------------------------

// TestEveryFieldRefusesWhatSBPLCannotCarry is the table §8.3 asks for: one bounds
// test per field, on both sides of every bound.
//
// The zero value is in every table on purpose. It is the ONE malformed value an
// external caller can still forge — the fields are unexported, so a composite
// literal can write nothing else — and every bound of this package excludes it,
// which is what makes "a job Encode accepts is a job every field of which came out
// of a validating constructor" true.
func TestEveryFieldRefusesWhatSBPLCannotCarry(t *testing.T) {
	for _, c := range []struct {
		name    string
		build   func() error
		refused bool
		op      string
		kind    ports.Kind
	}{
		{"média 0×0", func() error { _, err := sbpl.NewMediaSize(0, 0); return err }, true, "sbpl.media", ports.KindConfig},
		{"média 1×1", func() error { _, err := sbpl.NewMediaSize(1, 1); return err }, false, "", 0},
		{"média 9999×9999", func() error { _, err := sbpl.NewMediaSize(9999, 9999); return err }, false, "", 0},
		{"média 10000 de haut", func() error { _, err := sbpl.NewMediaSize(10000, 320); return err }, true, "sbpl.media", ports.KindConfig},
		{"média 10000 de large", func() error { _, err := sbpl.NewMediaSize(203, 10000); return err }, true, "sbpl.media", ports.KindConfig},
		{"média négatif", func() error { _, err := sbpl.NewMediaSize(-1, 320); return err }, true, "sbpl.media", ports.KindConfig},

		{"noircissement 0", func() error { _, err := sbpl.NewDarkness(0); return err }, true, "sbpl.darkness", ports.KindConfig},
		{"noircissement 1", func() error { _, err := sbpl.NewDarkness(1); return err }, false, "", 0},
		{"noircissement 5", func() error { _, err := sbpl.NewDarkness(5); return err }, false, "", 0},
		{"noircissement 6", func() error { _, err := sbpl.NewDarkness(6); return err }, true, "sbpl.darkness", ports.KindConfig},

		{"vitesse 1", func() error { _, err := sbpl.NewSpeed(1); return err }, true, "sbpl.speed", ports.KindConfig},
		{"vitesse 2", func() error { _, err := sbpl.NewSpeed(2); return err }, false, "", 0},
		{"vitesse 6", func() error { _, err := sbpl.NewSpeed(6); return err }, false, "", 0},
		{"vitesse 7", func() error { _, err := sbpl.NewSpeed(7); return err }, true, "sbpl.speed", ports.KindConfig},

		{"0 exemplaire", func() error { _, err := sbpl.NewCopies(0); return err }, true, "sbpl.copies", ports.KindConfig},
		{"1 exemplaire", func() error { _, err := sbpl.NewCopies(1); return err }, false, "", 0},
		{"999999 exemplaires", func() error { _, err := sbpl.NewCopies(999_999); return err }, false, "", 0},
		{"1000000 exemplaires", func() error { _, err := sbpl.NewCopies(1_000_000); return err }, true, "sbpl.copies", ports.KindConfig},

		{"modèle à 0 octet", func() error { _, err := sbpl.NewModel(0); return err }, true, "sbpl.model", ports.KindConfig},
		{"modèle à 1 octet", func() error { _, err := sbpl.NewModel(1); return err }, false, "", 0},
		{"modèle à 999 octets", func() error { _, err := sbpl.NewModel(999); return err }, false, "", 0},
		{"modèle à 1000 octets", func() error { _, err := sbpl.NewModel(1000); return err }, true, "sbpl.model", ports.KindConfig},

		{"modèle forgé", func() error {
			_, err := sbpl.NewGraphic(sbpl.Model{}, 0, 0, smallBitmap(), sbpl.InkIsOne)
			return err
		}, true, "sbpl.model", ports.KindConfig},
		{"aucun bitmap", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, nil, sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindInternal},
		{"bitmap sans surface", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, image.NewGray(image.Rect(0, 0, 0, 0)), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"bloc de 104 octets", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(104*8, 1), sbpl.InkIsOne)
			return err
		}, false, "", 0},
		{"bloc de 105 octets", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(104*8+1, 1), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"bloc de 600 dots de haut", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(8, 600), sbpl.InkIsOne)
			return err
		}, false, "", 0},
		{"bloc de 601 dots de haut", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(8, 601), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"polarité inconnue", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, smallBitmap(), sbpl.InkPolarity(7))
			return err
		}, true, "sbpl.graphic", ports.KindConfig},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.build()
			if !c.refused {
				if err != nil {
					t.Fatalf("valeur refusée à tort : %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("valeur hors bornes acceptée")
			}
			assertPrintError(t, err, c.kind, c.op)
		})
	}
}
