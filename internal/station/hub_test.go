package station

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

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

// TestNoLeakOnCommandWithoutAck is the test §13.2 names.
//
// Five hundred refused commands alternated with five hundred nominal ones, then
// the goroutine count compared with the baseline AT REST, WITH NO CLIENT
// CONNECTED. Without the end-of-cycle safety net, every refusal leaks the
// goroutine of its caller.
func TestNoLeakOnCommandWithoutAck(t *testing.T) {
	b := newBench(t)
	b.push(1236, domain.Stable)
	b.tick()

	baseline := stableCount()

	ctx := context.Background()
	for i := 0; i < 500; i++ {
		// Refused: a product the catalog does not offer, then an event the current
		// state has nothing to say about.
		if _, err := b.hub.Submit(ctx, domain.ProductTapped{ProductID: "inconnu"}, ""); err != nil {
			t.Fatalf("Submit(produit inconnu) : %v", err)
		}
		if _, err := b.hub.Submit(ctx, domain.Dismiss{}, ""); err != nil {
			t.Fatalf("Submit(Dismiss hors Faulted) : %v", err)
		}
		if _, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: "jamais imprimé"}, ""); err != nil {
			t.Fatalf("Submit(réimpression impossible) : %v", err)
		}
	}

	if got := stableCount(); got > baseline {
		t.Fatalf("%d goroutines après 1 500 commandes refusées, ligne de base %d : une commande "+
			"refusée laisse son appelant en attente", got, baseline)
	}
}

// TestARefusedCommandStillAnswers checks the CONTENT of the safety net, not only
// that it fires: the answer names the state reached and refuses in French.
func TestARefusedCommandStillAnswers(t *testing.T) {
	b := newBench(t)
	ctx := context.Background()

	ack, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: "jamais imprimé"}, "")
	if err != nil {
		t.Fatalf("Submit : %v", err)
	}
	if ack.Accepted {
		t.Fatal("une réimpression impossible a été acceptée")
	}
	if ack.Message == "" {
		t.Fatal("un refus sans message : l'écran n'a rien à afficher")
	}
	if !strings.Contains(ack.Message, "réimprim") {
		t.Fatalf("message %q : il doit parler de la réimpression", ack.Message)
	}
}

// TestSubmitAnswersWhenTheHubIsGone proves the symmetric half of the contract: a
// caller never waits on the channel alone.
func TestSubmitAnswersWhenTheHubIsGone(t *testing.T) {
	b := newBench(t)
	b.station.Stop()
	<-b.station.Stopped()

	if _, err := b.hub.Submit(context.Background(), domain.Cancel{}, ""); !errors.Is(err, ErrStopped) {
		t.Fatalf("erreur %v, attendu ErrStopped", err)
	}
}

// TestASaturatedPrintWorkerBecomesAPrintFailure covers the branch of execute that
// only an ABNORMAL station reaches: the machine forbids two prints inside one
// cycle, so a full channel means the worker is stuck on a device.
//
// It is driven directly rather than through the loop, and that is the honest way
// to test it: getting there through the machine would require a third cycle to
// start while the second is still printing, which the machine refuses — the test
// would prove the setup, not the branch.
func TestASaturatedPrintWorkerBecomesAPrintFailure(t *testing.T) {
	h := newHub(Options{
		Clock: fakeClockAt(epoch), Config: loadConfig(t), Catalog: garlicCatalog(),
		Counters: &Counters{},
	})
	h.printJobs <- job{} // the worker is stuck: the one slot is taken

	ev := h.execute(domain.PrintEffect{Label: domain.Label{JobID: "j-42"}}, epoch)
	finished, ok := ev.(domain.PrintFinished)
	if !ok {
		t.Fatalf("execute rend %T, attendu un domain.PrintFinished réinjecté", ev)
	}
	if finished.JobID != "j-42" {
		t.Fatalf("job %q, attendu j-42", finished.JobID)
	}
	if !errors.Is(finished.Err, ErrPrintWorkerBusy) {
		t.Fatalf("erreur %v, attendu ErrPrintWorkerBusy", finished.Err)
	}
	select {
	case entry := <-h.technical:
		if entry.Code != "ERR-PRN-09" {
			t.Fatalf("code technique %q, attendu ERR-PRN-09", entry.Code)
		}
	default:
		t.Fatal("aucune ligne technique : la saturation du worker n'est pas journalisée")
	}
}

// TestTheHubKeepsAnsweringWhileThePrinterHangs is failure test 6 seen from the
// Hub: the device is stuck, and neither the loop nor a caller waits on it.
func TestTheHubKeepsAnsweringWhileThePrinterHangs(t *testing.T) {
	b := newBench(t)
	b.printer.Hang()
	defer b.printer.Release()

	b.feed(1236, 2)
	if ack := b.tap("hung", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée : %s", ack.Message)
	}
	// The loop keeps turning and keeps answering while the device holds the worker.
	for i := 0; i < 10; i++ {
		b.tick()
	}
	if got := b.hub.State().State; got != domain.Printing {
		t.Fatalf("état %s : le cycle ne devrait pas se terminer sans réponse de l'imprimante", got)
	}
}

// TestTheJournalDegradesAndTheServiceDoesNot is ADR-013 from the failing side: the
// store refuses, the label still came out, the weighing lands in the RAM ring and
// the counter goes up.
func TestTheJournalDegradesAndTheServiceDoesNot(t *testing.T) {
	b := newBench(t)
	b.journal.mu.Lock()
	b.journal.err = errors.New("disque plein")
	b.journal.mu.Unlock()

	b.feed(1236, 2)
	if ack := b.tap("disk-full", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée alors que seul le journal est en panne : %s", ack.Message)
	}
	b.awaitPrint()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes : la pesée doit sortir même quand le journal ne suit pas", n)
	}
	// The store refuses, so the counter is what says so. It is written by the
	// journal worker, which is why the assertion converges instead of snapshotting.
	counters := b.station.Counters()
	for i := 0; i < 20000 && counters.UnloggedWeighings.Load() == 0; i++ {
		b.tick()
		if counters.UnloggedWeighings.Load() != 0 {
			break
		}
	}
	if got := counters.UnloggedWeighings.Load(); got != 1 {
		t.Fatalf("compteur de pesées non journalisées = %d, attendu 1", got)
	}
	// The row goes to the RAM ring, exactly as it does when the channel is
	// saturated: a disk that is full and a channel nobody drains lose the same row
	// for the same customer, and failure test 7 asks for the ring on the first of
	// the two (§16.2, ADR-013). The counter says HOW MANY were lost; only the ring
	// says WHICH ONES.
	if entries := b.hub.Entries(); len(entries) != 1 {
		t.Fatalf("%d pesée(s) dans l'anneau RAM, attendu 1", len(entries))
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

// TestAChangeInsideTheThrottleGoesOutOnTheNextTick pins both halves of the
// publication rule at once: nothing is emitted twice inside 100 ms, and what was
// held back is emitted on the very next tick rather than waiting for the 500 ms
// heartbeat.
func TestAChangeInsideTheThrottleGoesOutOnTheNextTick(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()
	<-snapshots // the state a new subscriber gets at once

	b.tick() // a publication happens here, and fixes the throttle window
	drain(snapshots)

	// Same instant as the last publication: the change is held back.
	b.push(1236, domain.Stable)
	b.flush()
	if len(snapshots) != 0 {
		t.Fatal("un changement a été publié dans les 100 ms de la publication précédente")
	}

	b.tick()
	if len(snapshots) != 1 {
		t.Fatal("le snapshot retenu par le throttle n'est jamais parti")
	}
	if got := (<-snapshots).Weight.Gross; got != 1236 {
		t.Fatalf("poids publié %d g, attendu 1236 g", got)
	}
}

// drain empties a subscriber channel without blocking.
func drain(snapshots <-chan Snapshot) {
	for {
		select {
		case <-snapshots:
		default:
			return
		}
	}
}

// TestPublicationIsThrottledAndStillBeats checks both halves of §13.3 at once:
// nothing is published twice in 100 ms, and something is published at least every
// 500 ms even when nothing changes.
func TestPublicationIsThrottledAndStillBeats(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()
	<-snapshots // the state a new subscriber gets at once

	// Nothing changes for two seconds: the forced heartbeat must still fire, and
	// it must not fire ten times a second.
	beats := 0
	for i := 0; i < 20; i++ {
		b.tick()
		select {
		case <-snapshots:
			beats++
		default:
		}
	}
	if beats == 0 {
		t.Fatal("aucun battement en 2 s : un navigateur reconnecté resterait sur un bandeau figé")
	}
	if beats > 8 {
		t.Fatalf("%d battements en 2 s alors que rien ne change : le throttle ne tient pas", beats)
	}
}

// TestASlowSubscriberNeverHoldsTheHub proves the drop-old rule: a subscriber that
// stops reading gets the stale snapshot dropped, never the loop blocked.
func TestASlowSubscriberNeverHoldsTheHub(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()

	// Never read from snapshots, and keep the station busy.
	for i := 0; i < 50; i++ {
		b.push(domain.Grams(1000+i), domain.Stable)
		b.tick()
	}
	if got := b.hub.State().Weight.Gross; got != 1049 {
		t.Fatalf("dernier poids %d g, attendu 1049 g : la boucle a été retenue par un abonné", got)
	}
	if n := len(snapshots); n > subscriberDepth {
		t.Fatalf("%d snapshots en attente, capacité %d", n, subscriberDepth)
	}
}

// TestARefusedWeighingAnswersWithItsSafeguardCode covers the end-of-cycle safety
// net where it matters most: a blocking safeguard produces no AckEffect of its own
// in some paths, and the answer must still name the code the screen shows.
func TestARefusedWeighingAnswersWithItsSafeguardCode(t *testing.T) {
	b := newBench(t)
	// Eight grams: under min_weight_g, which is 10 on the shipped file.
	b.feed(8, 2)
	ack := b.tap("too-light", 8)
	if ack.Accepted {
		t.Fatal("une pesée de 8 g a été acceptée alors que le plancher est à 10 g")
	}
	if ack.Code != domain.CodeWeightTooLow {
		t.Fatalf("code %q, attendu %q", ack.Code, domain.CodeWeightTooLow)
	}
	if ack.Message == "" {
		t.Fatal("un refus sans message : le client ne sait pas quoi corriger")
	}
	if n := len(b.printer.Jobs()); n != 0 {
		t.Fatalf("%d étiquettes pour une pesée refusée", n)
	}

	// And the refusal IS journalled, with what it would have cost.
	row := b.awaitJournal()
	if row.Result != domain.ResultRejected {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultRejected)
	}
	if row.Detail == "" {
		t.Fatal("le journal ne dit pas pourquoi la pesée a été refusée")
	}
}

// TestDefaultAckNamesTheStateAndNeverAJobID is the unit of the safety net.
func TestDefaultAckNamesTheStateAndNeverAJobID(t *testing.T) {
	rejected := domain.Model{
		State: domain.Rejected,
		Diagnostics: []domain.Diagnostic{
			{Code: domain.CodeWeightTooLow, Severity: domain.Blocking,
				Message: domain.DefaultMessage(domain.CodeWeightTooLow)},
		},
	}
	ack := defaultAck(rejected, domain.ProductTapped{})
	if ack.Accepted {
		t.Fatal("le filet de fin de cycle prétend accepter : il n'est pas un accusé d'acceptation")
	}
	if ack.State != domain.Rejected || ack.Code != domain.CodeWeightTooLow {
		t.Fatalf("accusé %+v : il doit porter l'état atteint et le code bloquant", ack)
	}
	if ack.JobID != "" {
		t.Fatal("le filet de fin de cycle a inventé un JobID : aucune étiquette n'est partie")
	}

	silent := defaultAck(domain.Model{State: domain.Idle}, domain.Dismiss{})
	if silent.Message == "" {
		t.Fatal("un événement ignoré rend un accusé muet : l'appelant n'a rien à afficher")
	}
}

// TestABannerExpiresOnTheInjectedClock: what really ends a message is the physical
// signal, and these durations only bound a message nobody came back for.
func TestABannerExpiresOnTheInjectedClock(t *testing.T) {
	b := newBench(t)
	b.feed(8, 2)
	b.tap("too-light", 8)
	b.tick()

	message := b.hub.State().Message
	if message == nil {
		t.Fatal("aucun bandeau après un refus")
	}
	if message.Code != domain.CodeWeightTooLow {
		t.Fatalf("code du bandeau %q, attendu %q", message.Code, domain.CodeWeightTooLow)
	}

	b.advance(domain.RejectMessageDuration + time.Second)
	if got := b.hub.State().Message; got != nil {
		t.Fatalf("le bandeau %q survit à sa durée", got.Text)
	}
}

// TestTheScaleLossBannerHasNoExpiry: a station with no scale does not stop having
// no scale because five seconds went by.
func TestTheScaleLossBannerHasNoExpiry(t *testing.T) {
	b := newBench(t)
	b.disconnect(errors.New("câble débranché"))
	b.tick()

	message := b.hub.State().Message
	if message == nil {
		t.Fatal("aucun bandeau après la perte de la balance")
	}
	if !message.ExpiresAt.IsZero() {
		t.Fatalf("le bandeau de perte de balance expire à %s", message.ExpiresAt)
	}
	b.advance(time.Hour)
	if b.hub.State().Message == nil {
		t.Fatal("le bandeau de perte de balance a disparu tout seul")
	}
}

// TestTheNumberOfCopiesComesFromTheConfiguration, and a count left at zero is ONE:
// a station that prints nothing because a field is empty is a station nobody can
// debug.
func TestTheNumberOfCopiesComesFromTheConfiguration(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Printer.Options["copies"] = json.RawMessage(`3`)
		}
	})
	b.feed(1236, 2)
	b.tap("three-copies", 1236)
	b.awaitPrint()
	if got := b.printer.Jobs()[0].Copies; got != 3 {
		t.Fatalf("%d exemplaires, attendu 3", got)
	}

	zero := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Printer.Options["copies"] = json.RawMessage(`0`)
		}
	})
	zero.feed(1236, 2)
	zero.tap("zero-copies", 1236)
	zero.awaitPrint()
	if got := zero.printer.Jobs()[0].Copies; got != 1 {
		t.Fatalf("%d exemplaires pour un réglage à zéro, attendu 1", got)
	}
}

// TestTheReprintBarIsPermanentInsideItsWindow is §8.5: one reprint per label,
// marked, inside reprint_window_s.
func TestTheReprintBarIsPermanentInsideItsWindow(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)
	first := b.tap("original", 1236)
	b.awaitJournal()
	b.tick()

	s := b.hub.State()
	if !s.ReprintAvailable {
		t.Fatal("la barre de réimpression n'est pas active juste après une étiquette")
	}

	ctx := context.Background()
	ack, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: first.JobID, Key: "reprint-1"}, "reprint-1")
	if err != nil {
		t.Fatalf("Submit(ReprintRequested) : %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("réimpression refusée dans sa fenêtre : %s", ack.Message)
	}
	row := b.awaitJournal()
	if row.Result != domain.ResultReprint {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultReprint)
	}

	// One reprint only.
	again, err := b.hub.Submit(ctx, domain.ReprintRequested{Key: "reprint-2"}, "reprint-2")
	if err != nil {
		t.Fatalf("Submit : %v", err)
	}
	if again.Accepted {
		t.Fatal("une seconde réimpression a été acceptée")
	}
	if n := len(b.printer.Jobs()); n != 2 {
		t.Fatalf("%d étiquettes, attendu 2 (l'originale et sa réimpression)", n)
	}
}
