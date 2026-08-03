package station

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
)

// One customer, one cycle, through the loop of loop.go: a bag on the plate, a tile
// tapped, a label. And the four things that interrupt it — a second tap, a weight that
// has expired, a scale that goes and comes back, and a catalog that must not reorder
// the tiles under a finger.
//
// What the EFFECTS of a cycle do is in effects_test.go, what goes out on the wire in
// publish_test.go.

// TestWeighingEndToEnd proves that one tap yields one label, that what is printed
// is what was displayed, and that a repeated idempotency key prints nothing more.
//
// No scale, no printer, no network, no browser, no time.Sleep — and the whole path
// is the real one: the serial channel, the Hub loop, domain.Transition, the print
// worker and the journal worker (§16.3).
func TestWeighingEndToEnd(t *testing.T) {
	b := newBench(t)

	// Five frames at the nominal cadence: two seconds of clock, twenty real ticks,
	// so stability, the observed rate and the expiry are GENUINELY exercised.
	b.feed(1236, 5)
	if !b.hub.State().Weight.Latched {
		t.Fatal("le poids n'est pas figé après cinq trames stables espacées de 400 ms")
	}

	ack := b.tap("01J9F2ABC", 1236)
	if !ack.Accepted {
		t.Fatalf("pesée refusée : %s (%s)", ack.Message, ack.Code)
	}
	if ack.State != domain.Printing {
		t.Fatalf("état %s, attendu printing", ack.State)
	}

	row := b.awaitJournal()
	if string(row.Barcode) != garlicBarcode {
		t.Fatalf("code-barres journalisé %q, attendu %q", row.Barcode, garlicBarcode)
	}
	if row.Result != domain.ResultSent {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultSent)
	}

	jobs := b.printer.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("%d travaux d'impression, attendu 1", len(jobs))
	}
	printed := jobs[0].Label
	if string(printed.Barcode) != garlicBarcode {
		t.Fatalf("IMPRIMÉ (%q) diffère de ce qui était AFFICHÉ (%q)", printed.Barcode, garlicBarcode)
	}
	if printed.NetWeight != 1236 {
		t.Fatalf("poids net imprimé %d g, attendu 1236 g", printed.NetWeight)
	}

	// A6: commercial rounding, half up. The two figures are the reference vector of
	// §16.1, and they are the reason this test is worth all the others.
	assertAmount(t, printed, "MEMBER", 592)
	assertAmount(t, printed, "SOLIDARITY", 658)

	// Double tap: same key, no second label.
	replay := b.tap("01J9F2ABC", 1236)
	if replay.JobID != ack.JobID {
		t.Fatalf("le rejeu rend le job %q, attendu le même que le premier (%q)", replay.JobID, ack.JobID)
	}
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d travaux d'impression après le double toucher, attendu 1", n)
	}
	if n := len(b.journal.rows()); n != 1 {
		t.Fatalf("%d lignes de journal après le double toucher, attendu 1", n)
	}
}

// assertAmount checks one price line of a printed label.
func assertAmount(t *testing.T, label domain.Label, tier string, want domain.Cents) {
	t.Helper()
	line := label.Find(tier)
	if line == nil {
		t.Fatalf("aucune ligne de tarif %q sur l'étiquette", tier)
	}
	if line.Amount != want {
		t.Fatalf("montant %s = %s, attendu %s", tier, line.Amount.Euro(), want.Euro())
	}
}

// TestDoubleTapPrintsOneLabel is failure test 15, stripped to its assertion: two
// POSTs, one key, one label, and the second one replays the answer of the first.
func TestDoubleTapPrintsOneLabel(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	first := b.tap("same-key", 1236)
	second := b.tap("same-key", 1236)

	if !first.Accepted || !second.Accepted {
		t.Fatalf("les deux accusés doivent être identiques et acceptés : %+v / %+v", first, second)
	}
	if first != second {
		t.Fatalf("le second accusé %+v n'est pas le rejeu du premier %+v", second, first)
	}
	// The journal row is written after the print, so waiting for it is waiting for
	// the whole cycle — no sleep, no polling.
	b.awaitJournal()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes, attendu 1", n)
	}
}

// TestExpiredMeasurementRejectsWeighing is failure test 3 ter, and it is the test
// bloquant-1 exists for.
//
// The clock is frozen, one valid stable reading is pushed, nothing else is pushed,
// and time moves. At 1 199 ms of age the weight is still good; at 1 600 ms it is
// not, in BOTH stability modes, advisory included.
func TestExpiredMeasurementRejectsWeighing(t *testing.T) {
	for _, mode := range []string{domain.ModeAdvisory, domain.ModeBlocking} {
		t.Run(mode, func(t *testing.T) {
			started := time.Now()
			b := newBench(t, func(o *benchOptions) {
				o.config = func(c *domain.Config) { c.Stability.Mode = mode }
			})

			// Eight intervals of 400 ms give the rate meter a median, so the
			// derived expiry is 3 × 400 = 1 200 ms, the floor.
			for i := 0; i < 9; i++ {
				b.push(1236, domain.Stable)
				b.advance(400 * time.Millisecond)
			}
			if got := b.hub.State().Weight.Expiry; got != 1200*time.Millisecond {
				t.Fatalf("péremption dérivée %s, attendu 1,2 s", got)
			}

			// 400 ms have already gone by since the last frame; 799 more make 1 199.
			b.advance(799 * time.Millisecond)
			if s := b.hub.State(); s.Expired {
				t.Fatalf("à %s d'âge la mesure est encore valide, elle est déclarée périmée", s.Weight.Age)
			}
			if ack := b.tap("fresh", 1236); !ack.Accepted {
				t.Fatalf("pesée refusée à 1 199 ms d'âge : %s (%s)", ack.Message, ack.Code)
			}
			b.awaitJournal()

			// The bag leaves, another one arrives and settles, and THEN the scale
			// goes silent. The weight is latched when it starts to age, which is
			// the situation of failure test 3 ter: a reading that was good and is
			// no longer.
			b.push(0, domain.Stable)
			b.tick()
			b.feed(1236, 3)
			b.advance(1600 * time.Millisecond)

			s := b.hub.State()
			if !s.Expired {
				t.Fatalf("à %s d'âge (péremption %s) la mesure doit être périmée", s.Weight.Age, s.Weight.Expiry)
			}
			ack := b.tap("stale", 1236)
			if ack.Accepted {
				t.Fatal("une pesée sur un poids périmé a été acceptée")
			}
			if ack.Code != domain.CodeMeasurementExpired {
				t.Fatalf("code de refus %q, attendu %q", ack.Code, domain.CodeMeasurementExpired)
			}
			if n := len(b.printer.Jobs()); n != 1 {
				t.Fatalf("%d étiquettes, attendu 1 : le poids périmé en a produit une", n)
			}
			// The clock is injected: none of this waited.
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("durée murale %s : l'horloge n'est pas injectée quelque part", elapsed)
			}
		})
	}
}

// TestScaleLossTriggeredByStatusAlone is failure test 1, and it carries the three
// clauses §16.2 gives that line.
//
// The trigger is the Status field ALONE. The last event a driver emits does carry
// a non-nil Err, but making the loss depend on an optional field is what let the
// signal fall into a default branch and never reach the machine (défaut 40) — so
// BOTH variants, with and without a reason, must reach it.
//
// No label comes out, and the TWENTY StatusDisconnected the reconnection backoff
// emits cost ONE transition: the message is not re-armed, the snapshot does not
// move, and the loop is not spinning on its own signal.
func TestScaleLossTriggeredByStatusAlone(t *testing.T) {
	for _, reason := range []error{errors.New("port fermé"), nil} {
		b := newBench(t)
		b.disconnect(reason)
		b.tick()

		lost := b.hub.State()
		if lost.State != domain.ScaleLost {
			t.Fatalf("Err=%v : état %s, attendu scale_lost", reason, lost.State)
		}
		if lost.Message == nil {
			t.Fatalf("Err=%v : aucun message : l'écran ne dit pas pourquoi il n'y a plus de poids", reason)
		}
		if n := len(b.printer.Jobs()); n != 0 {
			t.Fatalf("Err=%v : %d étiquettes, attendu aucune", reason, n)
		}

		// The backoff of §9.1 emits one StatusDisconnected per attempt. The clock is
		// deliberately NOT advanced here: the supervisor observes the printer once a
		// second and its observation instant is part of the snapshot, so moving the
		// clock would make the revision change for a reason that has nothing to do
		// with the scale.
		awaitCondition(t, func() bool { return b.technical.count("ERR-SCL-02") == 1 },
			"la perte de balance n'a pas été journalisée")
		for i := 0; i < 20; i++ {
			b.disconnect(reason)
		}
		b.tick()

		after := b.hub.State()
		if after.Revision != lost.Revision {
			t.Fatalf("Err=%v : la révision est passée de %d à %d : vingt répétitions du "+
				"backoff ont produit vingt transitions", reason, lost.Revision, after.Revision)
		}
		if got := b.technical.count("ERR-SCL-02"); got != 1 {
			t.Fatalf("Err=%v : %d ligne(s) ERR-SCL-02 pour vingt-et-une déconnexions, "+
				"attendu 1 : le signal n'est pas idempotent", reason, got)
		}
		if n := len(b.printer.Jobs()); n != 0 {
			t.Fatalf("Err=%v : %d étiquettes après le backoff", reason, n)
		}
	}
}

// TestTheScaleComesBack is failure test 1 bis: the round trip works, and the
// channel is never lost.
func TestTheScaleComesBack(t *testing.T) {
	b := newBench(t)
	b.disconnect(errors.New("débranchée"))
	b.tick()
	if got := b.hub.State().State; got != domain.ScaleLost {
		t.Fatalf("état %s, attendu scale_lost", got)
	}

	b.reconnect()
	b.tick()
	if got := b.hub.State().State; got != domain.Idle {
		t.Fatalf("état %s après retour de la balance, attendu idle", got)
	}

	// And the SAME channel still carries measurements.
	b.push(1236, domain.Stable)
	b.tick()
	if got := b.hub.State().Weight.Gross; got != 1236 {
		t.Fatalf("poids %d g après reconnexion, attendu 1236 g", got)
	}
}

// TestArmingExpiresBeforeNextCustomerBag is failure test 17 and ADR-022.
func TestArmingExpiresBeforeNextCustomerBag(t *testing.T) {
	started := time.Now()

	t.Run("le client s'en va", func(t *testing.T) {
		b := newBench(t)
		b.push(0, domain.Stable)
		b.tick()
		if ack := b.tap("armed", 0); ack.State != domain.ProductArmed {
			t.Fatalf("état %s, attendu product_armed", ack.State)
		}
		b.advance(10*time.Second + 100*time.Millisecond)

		s := b.hub.State()
		if s.State != domain.Idle {
			t.Fatalf("état %s après expiration, attendu idle", s.State)
		}
		if s.Product != nil {
			t.Fatalf("produit %v encore armé après expiration", s.Product)
		}
		b.push(800, domain.Stable)
		b.tick()
		if n := len(b.printer.Jobs()); n != 0 {
			t.Fatalf("%d étiquettes : le sac du client suivant a imprimé l'étiquette du premier", n)
		}
	})

	t.Run("(a) le sac arrive à 9,9 s", func(t *testing.T) {
		b := newBench(t)
		b.push(0, domain.Stable)
		b.tick()
		b.tap("armed", 0)
		b.advance(9900 * time.Millisecond)
		b.push(1236, domain.Stable)
		b.tick()
		b.awaitJournal()
		jobs := b.printer.Jobs()
		if len(jobs) != 1 {
			t.Fatalf("%d étiquettes, attendu 1", len(jobs))
		}
		if got := jobs[0].Label.Product.ID; got != garlicID {
			t.Fatalf("étiquette du produit %q, attendu %q", got, garlicID)
		}
	})

	t.Run("(b) un second produit touché à 5 s", func(t *testing.T) {
		b := newBench(t, func(o *benchOptions) { o.catalog = twoProductCatalog() })
		b.push(0, domain.Stable)
		b.tick()
		b.tap("armé-ail", 0)

		b.advance(5 * time.Second)
		if ack := b.tapProduct(leekID, "armé-poireau", 0); ack.State != domain.ProductArmed {
			t.Fatalf("état %s après le second toucher, attendu product_armed", ack.State)
		}

		// Nine seconds and nine tenths after the SECOND tap — fourteen after the
		// first. Without a rearmed timer the selection died four seconds ago and the
		// bag would print nothing at all.
		b.advance(9900 * time.Millisecond)
		if got := b.hub.State().State; got != domain.ProductArmed {
			t.Fatalf("état %s : le minuteur n'a pas été réarmé par le second toucher", got)
		}

		b.push(1236, domain.Stable)
		b.tick()
		b.awaitJournal()
		jobs := b.printer.Jobs()
		if len(jobs) != 1 {
			t.Fatalf("%d étiquette(s), attendu 1", len(jobs))
		}
		if got := jobs[0].Label.Product.ID; got != leekID {
			t.Fatalf("étiquette du produit %q, attendu %q : la dernière intention "+
				"exprimée doit gagner", got, leekID)
		}
	})

	t.Run("(c) Cancel pendant l'armement", func(t *testing.T) {
		b := newBench(t)
		b.push(0, domain.Stable)
		b.tick()
		b.tap("armed", 0)
		ctx, cancel := context.WithTimeout(context.Background(), hang)
		defer cancel()
		if _, err := b.hub.Submit(ctx, domain.Cancel{}, ""); err != nil {
			t.Fatalf("Submit(Cancel) : %v", err)
		}
		// The answer PRECEDES the publication by design (§13.2), and the
		// publication is throttled on the clock: one tick is what makes the new
		// state visible from outside.
		b.tick()
		if got := b.hub.State().State; got != domain.Idle {
			t.Fatalf("état %s après Cancel, attendu idle immédiatement", got)
		}
	})

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("durée murale %s : l'horloge n'est pas injectée quelque part", elapsed)
	}
}

// TestTheCatalogNeverSwapsUnderAFinger is failure test 13.
func TestTheCatalogNeverSwapsUnderAFinger(t *testing.T) {
	initial := garlicCatalog()
	b := newBench(t, func(o *benchOptions) { o.catalog = initial })

	next := domain.NewCatalog(
		[]domain.Product{{
			ID: "9999", Name: "POIREAU", Reference: "0493022000002",
			Mode: domain.ByWeight, UnitPrice: 300, Qualification: domain.Weighable,
		}},
		nil,
	)

	// A mass on the plate: the station is weighing.
	b.push(1236, domain.Stable)
	b.tick()
	if err := b.hub.PushCatalog(context.Background(), &CatalogBatch{Catalog: next}); err != nil {
		t.Fatalf("PushCatalog : %v", err)
	}
	b.advance(domain.MaxSwitchIdle + time.Second)
	if b.hub.Catalog() != initial {
		t.Fatal("le catalogue a basculé alors qu'une masse est sur le plateau")
	}

	// The bag leaves, and then ten seconds of quiet go by.
	b.push(0, domain.Stable)
	b.tick()
	b.advance(domain.MaxSwitchIdle - time.Second)
	if b.hub.Catalog() != initial {
		t.Fatal("le catalogue a basculé avant les 10 s d'inactivité")
	}
	// A touch that the machine REFUSES is still a customer at the screen: a
	// product withdrawn this morning, tapped twice by someone who has not given
	// up. The state stays Idle, and the swap must still be postponed — otherwise
	// the grid reorders itself under the finger that is tapping it.
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	if _, err := b.hub.Submit(ctx, domain.ProductTapped{ProductID: "retiré ce matin"}, ""); err != nil {
		t.Fatalf("Submit : %v", err)
	}
	b.advance(2 * time.Second)
	if b.hub.Catalog() != initial {
		t.Fatal("le catalogue a basculé alors qu'un client vient de toucher l'écran")
	}

	b.advance(domain.MaxSwitchIdle)
	if b.hub.Catalog() != next {
		t.Fatal("le catalogue n'a jamais basculé alors que le poste est au repos depuis 10 s")
	}
}
