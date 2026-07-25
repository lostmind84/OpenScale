package raster

import (
	"strings"
	"testing"
)

// The tests of the three adjustments of §8.2, taken as CONFIGURATION.
//
// Their whole value is on paper: a volunteer prints, looks, changes one number,
// prints again. What is tested here is therefore not "the number is right" — no test
// can know that — but that the number REACHES the printer, that a number the manual
// refuses never reaches it, and that nothing is ever silently rounded into the range.

// TestDefaultSettingsAreTheOnesShipped freezes the starting point of the adjustment
// against the file of §11.2.
func TestDefaultSettingsAreTheOnesShipped(t *testing.T) {
	s := DefaultSettings()
	if s.Darkness != 3 {
		t.Errorf("darkness = %d, config-lacagette.json en porte 3 (§11.2)", s.Darkness)
	}
	if s.Speed != 4 {
		t.Errorf("speed = %d, config-lacagette.json en porte 4 (§11.2)", s.Speed)
	}
	if s.OffsetXDots != 0 || s.OffsetYDots != 0 {
		t.Errorf("décalage livré (%d;%d), attendu (0;0) : le réglage part de zéro et se fait sur tirage",
			s.OffsetXDots, s.OffsetYDots)
	}
	if s.Copies != 1 {
		t.Errorf("copies = %d, attendu 1 : un client colle une étiquette sur un sac", s.Copies)
	}
	if s.InvertBits {
		t.Error("invert_bits est levé par défaut : la polarité livrée est celle que la mire d'alignement " +
			"confirmera, et lever le drapeau d'avance serait décider sans mesure")
	}
	if faults := s.Validate(); len(faults) != 0 {
		t.Errorf("les réglages livrés sont refusés par leur propre validation : %v", faults)
	}
}

// TestASettingOutOfBoundsIsNamedAndRefused holds every bound of §8.3, on both sides.
//
// The bounds are those of the LANGUAGE, and each fault names the configuration key a
// volunteer edits — never "invalid parameter".
func TestASettingOutOfBoundsIsNamedAndRefused(t *testing.T) {
	for _, c := range []struct {
		name  string
		tune  func(*Settings)
		field string
		says  string
	}{
		{"noircissement 0", func(s *Settings) { s.Darkness = 0 }, "printer.options.darkness", "de 1 à 5"},
		{"noircissement 6", func(s *Settings) { s.Darkness = 6 }, "printer.options.darkness", "de 1 à 5"},
		{"vitesse 1", func(s *Settings) { s.Speed = 1 }, "printer.options.speed", "de 2 à 6"},
		{"vitesse 7", func(s *Settings) { s.Speed = 7 }, "printer.options.speed", "de 2 à 6"},
		{"décalage x hors champ", func(s *Settings) { s.OffsetXDots = MaxOffsetDots + 1 }, "printer.options.offset_x", "quatre chiffres"},
		{"décalage x hors champ négatif", func(s *Settings) { s.OffsetXDots = -MaxOffsetDots - 1 }, "printer.options.offset_x", "quatre chiffres"},
		{"décalage y hors champ", func(s *Settings) { s.OffsetYDots = MaxOffsetDots + 1 }, "printer.options.offset_y", "quatre chiffres"},
		{"zéro exemplaire", func(s *Settings) { s.Copies = 0 }, "printer.options.copies", "de 1 à"},
		{"trop d'exemplaires", func(s *Settings) { s.Copies = MaxCopies + 1 }, "printer.options.copies", "de 1 à"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := DefaultSettings()
			c.tune(&s)
			faults := s.Validate()
			if len(faults) != 1 {
				t.Fatalf("%d fautes, une seule attendue : %v", len(faults), faults)
			}
			if faults[0].Field != c.field {
				t.Errorf("Field = %q, attendu %q : c'est la clé que le bénévole édite", faults[0].Field, c.field)
			}
			if !strings.Contains(faults[0].Message, c.says) {
				t.Errorf("message « %s » : il devait dire « %s », c'est-à-dire la borne admise",
					faults[0].Message, c.says)
			}
		})
	}
}

// TestTheZeroSettingsAreNotAConfiguration is the decision written down: a field
// nobody filled is not a value, and it is not quietly replaced by a default either.
// The file the station runs on then says, in full, what the printer was told.
func TestTheZeroSettingsAreNotAConfiguration(t *testing.T) {
	faults := Settings{}.Validate()
	if len(faults) != 3 {
		t.Fatalf("%d fautes pour des réglages vides, 3 attendues (noircissement, vitesse, exemplaires) : %v",
			len(faults), faults)
	}
	// All at once: a volunteer fixes one file, not three times the same file (§11.3).
	joined := joinFaults(faults)
	for _, field := range []string{"darkness", "speed", "copies"} {
		if !strings.Contains(joined, field) {
			t.Errorf("« %s » ne nomme pas %s", joined, field)
		}
	}
}

// TestTheHeadOfTheParcIsAWS408 freezes the two figures the frame is built against.
func TestTheHeadOfTheParcIsAWS408(t *testing.T) {
	h := WS408()
	if h.DotsPerMM != 8 {
		t.Errorf("dots_per_mm = %g, une WS408 en fait 8 (203 dpi)", h.DotsPerMM)
	}
	if h.MaxWidthBytes != 104 {
		t.Errorf("largeur maximale = %d octets, §8.3 en donne 104 sur la WS408", h.MaxWidthBytes)
	}
	if faults := h.Validate(); len(faults) != 0 {
		t.Errorf("la tête du parc est refusée par sa propre validation : %v", faults)
	}
	if faults := (Head{}).Validate(); len(faults) != 2 {
		t.Errorf("%d fautes pour une tête vide, 2 attendues", len(faults))
	}
}

// The vocabulary of the six kinds and the retry policy that follows from it used to
// be tested here, on a Kind this package declared. They now belong to
// internal/station/ports, with the taxonomy itself: see printerror_test.go there.
