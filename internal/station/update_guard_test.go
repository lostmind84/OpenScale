package station

import (
	"strings"
	"testing"

	"openscale/internal/domain"
)

// TestTheGuardAnswersForEveryStateOfTheMachine covers the whole enumeration, so
// that a state added later cannot fall through silently into « yes, take the
// station down ».
func TestTheGuardAnswersForEveryStateOfTheMachine(t *testing.T) {
	cases := []struct {
		state domain.State
		allow bool
	}{
		// At rest, in one form or another.
		{domain.Initializing, true},
		{domain.Idle, true},
		{domain.ManualMode, true},
		{domain.ScaleLost, true},
		// Cannot serve at all: an update costs nothing there and may be the cure.
		{domain.Faulted, true},
		{domain.OutOfService, true},
		// Somebody is mid-cycle, or reading the result of one.
		{domain.ProductArmed, false},
		{domain.WeightPresent, false},
		{domain.WeightStable, false},
		{domain.AwaitingStability, false},
		{domain.EnteringTare, false},
		{domain.EnteringWeight, false},
		{domain.Validating, false},
		{domain.Printing, false},
		{domain.Succeeded, false},
		{domain.Rejected, false},
	}
	for _, c := range cases {
		allowed, reason := updateGuardFor(c.state, false)
		if allowed != c.allow {
			t.Errorf("état %s : autorisé %v, attendu %v", c.state, allowed, c.allow)
		}
		switch {
		case allowed && reason != "":
			t.Errorf("état %s : autorisé mais donne une raison %q", c.state, reason)
		case !allowed && reason == "":
			t.Errorf("état %s : refuse sans dire pourquoi", c.state)
		}
	}
}

// TestTheGuardSpeaksFrenchAndNamesNoDocument: the sentence is shown to a
// volunteer, so it carries no section number and no ADR.
func TestTheGuardSpeaksFrenchAndNamesNoDocument(t *testing.T) {
	for _, state := range []domain.State{domain.Printing, domain.EnteringTare} {
		_, reason := updateGuardFor(state, false)
		if strings.Contains(reason, "§") || strings.Contains(reason, "ADR-") {
			t.Errorf("état %s : la raison porte un renvoi de dossier : %q", state, reason)
		}
	}
	if _, reason := updateGuardFor(domain.Idle, true); reason == "" {
		t.Fatal("un catalogue en attente refuse sans dire pourquoi")
	}
}

// TestACatalogWaitingToEnterServiceHoldsTheUpdateBack is the failure mode this
// clause exists for, and it is one this project has already paid for.
//
// A pending batch means the CSV has already been read AND DELETED -- the deletion
// is the acknowledgement (§10.1) -- and the products live only in this process's
// memory, waiting for a quiet moment to enter service. Stopping the station there
// does not defer the catalogue: it LOSES it, and nothing will ever offer it again.
func TestACatalogWaitingToEnterServiceHoldsTheUpdateBack(t *testing.T) {
	allowed, reason := updateGuardFor(domain.Idle, true)
	if allowed {
		t.Fatal("un catalogue en attente de mise en service laisse passer une mise à jour")
	}
	if !strings.Contains(strings.ToLower(reason), "catalogue") {
		t.Errorf("la raison ne nomme pas le catalogue : %q", reason)
	}
}

// TestTheGuardAgreesWithTheCatalogSwapOnEveryStateItAllows.
//
// swapIsSafeIn already answers « is anybody mid-cycle? » for the catalogue, and
// the update asks the same question with more at stake. Whatever is too busy for
// a catalogue swap must be too busy for a reboot; the reverse need not hold, and
// does not -- OutOfService and Faulted refuse a swap and allow an update.
func TestTheGuardAgreesWithTheCatalogSwapOnEveryStateItAllows(t *testing.T) {
	for state := domain.Initializing; state <= domain.OutOfService; state++ {
		if !swapIsSafeIn(state) {
			continue
		}
		if allowed, reason := updateGuardFor(state, false); !allowed {
			t.Errorf("état %s : le catalogue peut basculer mais la mise à jour est refusée (%s)",
				state, reason)
		}
	}
}

// TestAStationInTheMiddleOfAWeighingRefuses drives a real Hub rather than the
// pure function: it is the wiring that could be wrong, not the table.
func TestAStationInTheMiddleOfAWeighingRefuses(t *testing.T) {
	b := newBench(t)

	if allowed, reason := b.hub.UpdateGuard(); !allowed {
		t.Fatalf("un poste au repos refuse la mise à jour : %s", reason)
	}

	b.feed(1236, 5)
	ack := b.tap("01J9F2ABC", 1236)
	if !ack.Accepted {
		t.Fatalf("pesée refusée : %s (%s)", ack.Message, ack.Code)
	}
	if ack.State != domain.Printing {
		t.Fatalf("état de l'accusé %s, attendu printing", ack.State)
	}
	b.flush()

	// The published state here is whatever the cycle has reached -- printing while
	// the worker holds the job, weight_stable once it has answered and the bag is
	// still on the plate. What matters is that it is NOT at rest: the bag has not
	// been taken off, so the customer is still in front of the screen.
	if got := b.hub.State().State; got == domain.Idle {
		t.Fatal("le poste est retombé au repos alors que le sac est encore sur le plateau")
	}
	allowed, reason := b.hub.UpdateGuard()
	if allowed {
		t.Fatal("une pesée est en cours et la mise à jour passe")
	}
	if reason == "" {
		t.Error("le refus ne porte aucune phrase")
	}
}

// TestAStationOutOfServiceMayBeUpdated is the escape hatch, and it is deliberate:
// a broken version is exactly the case where the button has to work. The neutral
// profile names a repository for this reason.
func TestAStationOutOfServiceMayBeUpdated(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.outOfService = true })

	if got := b.hub.State().State; got != domain.OutOfService {
		t.Fatalf("état %s, attendu out_of_service", got)
	}
	if allowed, reason := b.hub.UpdateGuard(); !allowed {
		t.Fatalf("un poste hors service refuse sa seule porte de sortie : %s", reason)
	}
}
