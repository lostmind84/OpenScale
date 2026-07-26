package station

import (
	"errors"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// TestTheManualEntrySwitchGoesBothWays is the button « Basculer en saisie manuelle » of
// §14.4, and the pair of assertions that makes it a switch rather than a one-way door.
//
// Going in releases the port — a volunteer who switches to manual entry usually does it
// because they are about to unplug something — and coming back rebuilds the driver the
// FILE declares, which the running configuration no longer carries.
func TestTheManualEntrySwitchGoesBothWays(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock
	asked := b.hub.Config()

	if err := b.station.ManualEntry(true, domain.Config{}); err != nil {
		t.Fatalf("bascule en saisie manuelle : %v", err)
	}
	if !b.scale.Closed() {
		t.Fatal("la balance n'a pas été fermée : le port reste tenu alors que le poste " +
			"est en saisie manuelle")
	}
	live := b.hub.Config()
	if live.Scale.Present {
		t.Fatal("la configuration en mémoire déclare encore une balance")
	}
	if !live.Scale.ManualEntryAllowed {
		t.Fatal("la saisie manuelle n'est pas autorisée : le poste ne peut plus rien peser")
	}
	b.tick()
	if got := b.hub.State().State; got != domain.ManualMode {
		t.Fatalf("état %s, attendu manual_mode", got)
	}

	if err := b.station.ManualEntry(false, asked); err != nil {
		t.Fatalf("retour à la balance : %v", err)
	}
	if forge.count() != 1 {
		t.Fatalf("%d balance(s) instanciée(s) au retour, attendu 1", forge.count())
	}
	if !b.hub.Config().Scale.Present {
		t.Fatal("la configuration en mémoire ne déclare pas la balance demandée par le fichier")
	}
	// And the SAME channel still carries measurements, from the NEW driver: that is what
	// makes the degraded mode reversible (bloquant-2).
	forge.last().Push(1236, domain.Stable)
	b.awaitIntake()
	b.tick()
	if got := b.hub.State().Weight.Gross; got != 1236 {
		t.Fatalf("poids %d g après l'aller-retour, attendu 1236 g", got)
	}
}

// TestTheManualEntrySwitchWritesNoConfiguration is the criterion of ADR-018 made into an
// assertion.
//
// The route is unauthenticated BECAUSE it writes no configuration, so the property has to
// be checked and not asserted in a comment: the station changes what it is doing, and the
// file keeps saying what the operator asked for. The file is not even reachable from here
// — no ConfigStore is involved — which is the strongest form the check can take.
func TestTheManualEntrySwitchWritesNoConfiguration(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	before := b.hub.Config()
	if err := b.station.ManualEntry(true, domain.Config{}); err != nil {
		t.Fatalf("bascule : %v", err)
	}
	// Everything but the scale block is untouched: the switch is not a reconfiguration.
	after := b.hub.Config()
	for _, c := range []struct {
		what          string
		before, after any
	}{
		{"printer", before.Printer, after.Printer},
		{"catalog", before.Catalog, after.Catalog},
		{"pricing", before.Pricing, after.Pricing},
		{"network", before.Network, after.Network},
	} {
		if BlockFingerprint(c.before) != BlockFingerprint(c.after) {
			t.Fatalf("le bloc %s a bougé : la bascule en saisie manuelle a reconfiguré le poste",
				c.what)
		}
	}
}

// TestAnUnconfirmedManualEntryDoesNotComeBackOnItsOwn is the difference between this
// switch and a configuration change, and it is deliberate.
//
// The 60 s countdown of §11.4 protects an operator from cutting the branch they are
// sitting on WHILE EDITING a configuration. Here a volunteer pressed a troubleshooting
// button on the station itself; a station that silently went back to a scale that does not
// answer one minute later would be the opposite of a remedy.
func TestAnUnconfirmedManualEntryDoesNotComeBackOnItsOwn(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	if err := b.station.ManualEntry(true, domain.Config{}); err != nil {
		t.Fatalf("bascule : %v", err)
	}
	// Well past the confirmation window, and past the supervisor tick that would enforce
	// it: nothing comes back.
	b.clock.Advance(2 * confirmationWindow)
	b.tick()
	if b.hub.Config().Scale.Present {
		t.Fatal("la saisie manuelle est revenue en arrière toute seule : ce n'est pas une " +
			"fenêtre de confirmation, c'est une décision de bénévole")
	}
}

// TestSwitchingTwiceIsNotAToggle is what a bad connection makes happen: the button is
// pressed, the answer is lost, the button is pressed again.
//
// The route carries the WANTED STATE and not « basculer », so the second press changes
// nothing at all — and in particular it does not open and close the serial port again.
func TestSwitchingTwiceIsNotAToggle(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	for attempt := 1; attempt <= 2; attempt++ {
		if err := b.station.ManualEntry(true, domain.Config{}); err != nil {
			t.Fatalf("bascule n° %d : %v", attempt, err)
		}
	}
	if forge.count() != 0 {
		t.Fatalf("%d balance(s) instanciée(s) : le second appui a rouvert le port", forge.count())
	}
	if b.hub.Config().Scale.Present {
		t.Fatal("le second appui a remis la balance en service : le bouton est une bascule")
	}
}

// TestAStationDeclaredWithoutAScaleHasNothingToComeBackTo keeps the answer honest.
//
// scale.present false is the EXPLICIT declaration of a station without a scale (§9.3), and
// manual entry is its nominal mode. « Revenir à la balance » there names a device that
// does not exist, and the screen has to say so rather than pretend it did something.
func TestAStationDeclaredWithoutAScaleHasNothingToComeBackTo(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Scale.Present = false
			cfg.Scale.ManualEntryAllowed = true
		}
	})
	err := b.station.ManualEntry(false, b.hub.Config())
	if !errors.Is(err, ErrNoScaleToComeBackTo) {
		t.Fatalf("erreur %v, attendu ErrNoScaleToComeBackTo", err)
	}
}

// TestTheSwitchIsJournalledWithItsOwnCode is what makes « pourquoi ce poste est-il en
// saisie manuelle ce matin ? » a decidable question (§11.4).
//
// ERR-SCL-09 says « quelqu'un l'a demandé » where ERR-SCL-03 says « le port ne s'ouvre
// pas ». One code for both would make the technical journal unable to tell an accident
// from a decision, which is the only thing that question is asking.
func TestTheSwitchIsJournalledWithItsOwnCode(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	switched := b.clock.Now()
	if err := b.station.ManualEntry(true, domain.Config{}); err != nil {
		t.Fatalf("bascule : %v", err)
	}
	awaitCondition(t, func() bool { return b.technical.count(codeManualEntryRequested) == 1 },
		"la bascule en saisie manuelle n'est pas journalisée sous "+codeManualEntryRequested)

	// One turn, because the degradation is written OUTSIDE the loop and reaches a screen
	// through the next published snapshot (§13.3).
	b.tick()
	degraded := b.hub.State().Degraded
	if degraded == nil {
		t.Fatal("aucune dégradation n'est publiée : l'écran ne peut pas dire pourquoi le " +
			"poste est en saisie manuelle")
	}
	if degraded.Code != codeManualEntryRequested {
		t.Fatalf("code de dégradation %q, attendu %q", degraded.Code, codeManualEntryRequested)
	}
	if !degraded.Since.Equal(switched) {
		t.Fatalf("horodate de dégradation %s, attendu l'instant de la bascule (%s) : c'est elle "+
			"qui rend la question décidable", degraded.Since, switched)
	}
}

// Compile-time proof that the switch is what internal/web consumes, with no adapter in
// between beyond the configuration the file declares.
var _ = func(s *Station, cfg domain.Config) error { return s.ManualEntry(true, cfg) }

// unused keeps the fake import honest if a case above is ever removed.
var _ = fake.NewClock
