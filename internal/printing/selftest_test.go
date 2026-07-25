package printing

import (
	"errors"
	"strings"
	"testing"
)

// TestTheCatalogueIsTheThreeSelfTestsOf86 — no more, no fewer, in the order §8.6 lists
// them, each with the screen §14.4 puts its button on.
func TestTheCatalogueIsTheThreeSelfTestsOf86(t *testing.T) {
	tests := SelfTests()
	if len(tests) != 3 {
		t.Fatalf("%d auto-tests, attendu 3 (§8.6)", len(tests))
	}
	want := []struct {
		id     SelfTest
		access Access
		label  bool
	}{
		{SelfTestLabel, AccessVolunteer, true},
		{SelfTestAlignment, AccessExpert, false},
		{SelfTestRuler, AccessExpert, false},
	}
	for i, w := range want {
		got := tests[i]
		if got.ID != w.id {
			t.Errorf("auto-test %d = %q, attendu %q", i, got.ID, w.id)
		}
		if got.Access != w.access {
			t.Errorf("%q est offert en accès %s, attendu %s : « Imprimer une étiquette de test » "+
				"est le geste de recette d'un bénévole, sans mot de passe (ADR-018) ; la mire et "+
				"la réglette sont des auto-tests de mise au point du mode expert (§8.6)",
				got.ID, got.Access, w.access)
		}
		if got.NeedsLabel != w.label {
			t.Errorf("%q déclare NeedsLabel = %v : seule l'étiquette de démonstration porte un "+
				"produit et des prix, qui viennent du catalogue et de la configuration",
				got.ID, got.NeedsLabel)
		}
		for field, value := range map[string]string{
			"Button": got.Button, "Prints": got.Prints, "Lifts": got.Lifts,
		} {
			if value == "" {
				t.Errorf("%q : %s est vide, or c'est ce que lit un bénévole", got.ID, field)
			}
		}
	}
}

// TestTheCatalogueIsACopy: it is handed to a screen and to a route table, and a
// catalogue a caller can reach into has stopped describing this binary.
func TestTheCatalogueIsACopy(t *testing.T) {
	SelfTests()[0].Button = "n'importe quoi"
	if got := SelfTests()[0].Button; got != "Imprimer une étiquette de test" {
		t.Errorf("le catalogue a été modifié par un appelant : %q", got)
	}
}

// TestTheTwoSelfTestsA2DeletedStayDeleted, and say why.
//
// Somebody will type them: they are in the old documentation and one of them was a
// button on the previous screen. « auto-test inconnu » would send that person looking
// for a typing mistake; the answer has to be that the thing itself is gone, and why.
func TestTheTwoSelfTestsA2DeletedStayDeleted(t *testing.T) {
	for _, c := range []struct {
		what string
		says string
	}{
		{"barcode-frame", "<BD>"},
		{"character-table", "table de caractères"},
	} {
		t.Run(c.what, func(t *testing.T) {
			for _, test := range SelfTests() {
				if string(test.ID) == c.what {
					t.Fatalf("%q est revenu dans le catalogue : il est sans objet depuis A2 (§8.1, §19)", c.what)
				}
			}
			_, err := LookupSelfTest(c.what)
			if err == nil {
				t.Fatalf("%q a été accepté", c.what)
			}
			for _, want := range []string{"supprimé", c.says, "A2", "alignment"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message « %s » : il doit contenir « %s »", err, want)
				}
			}
		})
	}
}

// TestAnUnknownSelfTestNamesTheOnesThatExist — never a bare « inconnu » (§11.3).
func TestAnUnknownSelfTestNamesTheOnesThatExist(t *testing.T) {
	_, err := LookupSelfTest("mire")
	if err == nil {
		t.Fatal("« mire » a été accepté : le nom retenu est alignment (glossaire)")
	}
	for _, want := range []string{"label", "alignment", "ruler"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message « %s » : il doit nommer %q", err, want)
		}
	}
}

// TestEveryCatalogueEntryIsReachableByItsRouteValue: the value the HTTP route carries
// (`?what=alignment|ruler`) is the value the catalogue is keyed by, or the screen and
// this package have drifted apart.
func TestEveryCatalogueEntryIsReachableByItsRouteValue(t *testing.T) {
	for _, test := range SelfTests() {
		found, err := LookupSelfTest(string(test.ID))
		if err != nil {
			t.Errorf("%q est au catalogue et introuvable : %v", test.ID, err)
			continue
		}
		if found.ID != test.ID {
			t.Errorf("%q a rendu %q", test.ID, found.ID)
		}
	}
}

// TestTheAlignmentSelfTestSettlesThePolarityInOnePrint is the criterion of §18, L5:
// « l'auto-test alignment lève la polarité de <G> en 10 min ».
//
// What that sentence means, and it is easy to read as something weaker: the polarity of
// <G> — whether a set bit is a burnt dot or a bare one — is a property of the FIRMWARE
// that no document of this project states. It cannot be derived, and getting it wrong
// prints every label as its own negative. One print settles it, because a 64 × 64 dot
// square is either black on white or white on black and there is no third appearance.
//
// So the trial takes what the volunteer SAW and the polarity the pattern was PRINTED
// with, and does the arithmetic. Both starting polarities are covered, because the
// station may already be carrying either one.
func TestTheAlignmentSelfTestSettlesThePolarityInOnePrint(t *testing.T) {
	for _, c := range []struct {
		name        string
		printedWith bool
		reading     PolarityReading
		want        bool
	}{
		{"tirage normal, carré noir : on garde", false, ReadingBlackOnWhite, false},
		{"tirage normal, négatif : on inverse", false, ReadingWhiteOnBlack, true},
		{"tirage déjà inversé, carré noir : on garde l'inversion", true, ReadingBlackOnWhite, true},
		{"tirage déjà inversé, négatif : on revient", true, ReadingWhiteOnBlack, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolvePolarity(c.printedWith, c.reading)
			if err != nil {
				t.Fatalf("ResolvePolarity : %v", err)
			}
			if got != c.want {
				t.Errorf("invert_bits = %v, attendu %v", got, c.want)
			}
		})
	}
}

// TestABlankLabelIsNotAPolarityAnswer: « rien n'est sorti » is unknown n° 4 falling the
// other way — the firmware did not take <G> through the queue at all — and the remedy
// is the documented GDI fallback, not another flag in the configuration. Answering
// « invert_bits = true » there would send a volunteer round a loop that cannot end.
func TestABlankLabelIsNotAPolarityAnswer(t *testing.T) {
	_, err := ResolvePolarity(false, ReadingNothing)
	if !errors.Is(err, ErrGraphicNotPrinted) {
		t.Fatalf("erreur = %v, attendu ErrGraphicNotPrinted", err)
	}
	for _, want := range []string{"RAW", "calibrée", "GDI"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message « %s » : il doit contenir « %s » — c'est ce que le bénévole "+
				"doit vérifier ensuite", err, want)
		}
	}
}

// TestAnUnansweredTrialSettlesNothing: the zero value of a reading is « nobody looked »,
// and it must never pass for an answer. This is the guess the trial exists to prevent.
func TestAnUnansweredTrialSettlesNothing(t *testing.T) {
	got, err := ResolvePolarity(true, ReadingNone)
	if !errors.Is(err, ErrNoPolarityReading) {
		t.Fatalf("erreur = %v, attendu ErrNoPolarityReading", err)
	}
	if got {
		t.Error("une réponse a été rendue malgré l'erreur")
	}
	if !strings.Contains(err.Error(), "devine") {
		t.Errorf("message « %s » : il doit dire que la polarité se lit, elle ne se devine pas", err)
	}
}

// TestTheQuestionIsClosedAndDescribesAppearances: the volunteer reports what came out
// of the printer, not what they think it means. A « est-ce correct ? » would settle a
// hardware ambiguity with an opinion.
func TestTheQuestionIsClosedAndDescribesAppearances(t *testing.T) {
	answers := PolarityAnswers()
	if len(answers) != 3 {
		t.Fatalf("%d réponses possibles, attendu 3", len(answers))
	}
	if strings.Contains(PolarityQuestion, "correct") || !strings.Contains(PolarityQuestion, "voyez") {
		t.Errorf("question « %s » : elle doit demander CE QUE LE BÉNÉVOLE VOIT", PolarityQuestion)
	}
	for _, want := range []string{"NOIR", "BLANC", "rien"} {
		found := false
		for _, a := range answers {
			if strings.Contains(a.Text, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("aucune réponse ne décrit « %s »", want)
		}
	}
	// Every offered answer must resolve to something — a value or a named error.
	for _, a := range answers {
		if _, err := ResolvePolarity(false, a.Reading); err != nil &&
			!errors.Is(err, ErrGraphicNotPrinted) {
			t.Errorf("la réponse « %s » ne mène nulle part : %v", a.Text, err)
		}
		if a.Reading == ReadingNone {
			t.Errorf("la réponse « %s » est le zéro « personne n'a répondu »", a.Text)
		}
	}
}

// TestAccessSpellsItselfOnce, for the route table and the journal.
func TestAccessSpellsItselfOnce(t *testing.T) {
	if AccessVolunteer.String() != "volunteer" || AccessExpert.String() != "expert" {
		t.Errorf("accès : %q et %q", AccessVolunteer, AccessExpert)
	}
}
